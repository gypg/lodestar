package relay

/*
R-5 调用点守卫 — relay_max_total_attempts 必须真的封顶"发往上游的转发次数"。

缺陷（2026-08-08 复现确认，与记忆里的描述不一致，见下）：
retry_shared.go 两处闸门原本写的是 `len(allAttempts) >= maxTotalAttempts`，
而 allAttempts 在整个 route round 里**一次都不会被 append**——
它只在 route round 收尾（retry_shared.go:259 附近）和几条立即 return 的终态
分支上才追加。于是 route round 1 期间 len(allAttempts) 恒为 0，闸门恒不触发，
配额被完全绕过。实测：cap=2、3 渠道 × 2 key，上游被真打 6 次。

方向要说清楚：**这不是"计数偏大导致提前放弃"，而是"计数恒 0 导致根本不封顶"**。
记忆 [[lodestar-source-bugs-open]] 里 R-5 记的是"混进 skip/circuit_break 致计数偏大"，
那只是次要的单位错误，且方向相反（偏大只会更早停）。主症状是配额失效。
两者都修：闸门改用 balancer.Iterator.ForwardedAttempts()（iterator.go:157），
它按 metrics.go:243 countForwardedAttempts 的同一口径排除 skipped/circuit_break。

跨 route round 的坑：每个 route round 都 NewIterator（retry_shared.go:112），
新迭代器计数归零，所以单看 routeIter.ForwardedAttempts() 会让配额按轮重置。
修复用 forwardedBefore 累加已完成轮次的转发数，闸门看的是
forwardedBefore + routeIter.ForwardedAttempts()。TestMaxTotalAttempts_capSpansRouteRounds
就是守这条的，M4 变异（删掉累加）能抓住。

入口一律走 MediaHandler，不直接调 retryWithChannels：守的是闸门在真实调用链上
生效，而不是"这个函数被调用时会返回什么"（[[lodestar-worker-false-evidence]] 第七变体）。
断言落在**假上游实际被打的次数**（副作用），不只看 HTTP 状态码或日志里的
TotalAttempts——位置类变异下 502 照样返回、日志照样写，只断言返回值会全绿放行。

反向守卫：TestMaxTotalAttempts_zeroMeansUnlimited 和
TestMaxTotalAttempts_doesNotCountSkippedChannels 守"别改过严"。
只写"不超过 N"的断言，把闸门改成恒 true（第一次就停）也能全绿。
*/

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	chpkg "github.com/gypg/lodestar/internal/op/channel"
	grppkg "github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/relay/balancer"
)

// capChannelSpec 描述一个参与配额测试的假渠道。
// keys > 1 时该渠道有多个可用 key，用来让 key 轮次也消耗配额。
//
// keysDisabled=true 表示渠道本身 Enabled 但它的 key 全部 Enabled=false：
// 这是在 retryWithChannels 内部制造 skipped 记录的正确姿势。
// 用 Channel.Enabled=false 造不出来 —— finalizeMatchedGroup（group.go:333）
// 在进入重试循环之前就把禁用渠道从候选里剔掉了，retry_shared.go:134 那条
// "channel disabled" 分支对走 GroupGetEnabledMapByEndpoint 的请求实际不可达。
// key 全禁的渠道能过前置过滤，然后在 retry_shared.go:180 命中
// "no available key" 记一条 skipped。
// statusCode 为 0 时按 500 处理（ScopeNextChannel，最容易烧配额）。
// 显式给 200 用于构造"最后一次允许的转发正好成功"的场景。
// emptyModel=true 让 GroupItem.ModelName 为空，命中 retry_shared.go:170 的
// "resolved upstream model is empty" 跳过分支。这是 media 路径上唯一一个
// 位于外层闸门（:137）与内层闸门（:177）之间、且可观测的副作用
// （FilterChannel 在 media 侧是 nil），用来验证"配额耗尽后是否还在推进候选"。
type capChannelSpec struct {
	name         string
	channelID    int
	keys         int
	keysDisabled bool
	emptyModel   bool
	statusCode   int
}

// capEnv 记录假上游被真实打到的次数与顺序。
// 这是本文件的核心观测点：配额管的是"发往上游的请求"，
// 只有上游 handler 被执行才算消耗了一次配额。
type capEnv struct {
	mu       sync.Mutex
	hitOrder []string
}

func (e *capEnv) hits() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.hitOrder...)
}

func (e *capEnv) hitCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.hitOrder)
}

// initCapEnv 起 N 个恒 500 的假上游并注册成同一分组的候选。
// 500 → ScopeNextChannel（type.go classifyHTTPError），既换 key 也换渠道，
// 是把配额烧光的最短路径。
//
// 固定用 Failover + 递增 Priority：RoundRobin 的 Candidates 带进程级计数器
// （balancer.go:48），跨测试会漂移，断言顺序时不能用。
func initCapEnv(t *testing.T, requestModel string, groupID int, specs []capChannelSpec, maxTotalAttempts int) *capEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	env := &capEnv{}

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := db.GetDB().AutoMigrate(&dbmodel.Setting{}); err != nil {
		t.Fatalf("migrate setting: %v", err)
	}
	if err := setting.RefreshCache(nil); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}

	// 被测配额。写完立刻读回确认生效：这个值是整组断言的前提，
	// 若 SetInt 静默失败，后面"没超过 cap"的断言就变成自证。
	if err := setting.SetInt(dbmodel.SettingKeyRelayMaxTotalAttempts, maxTotalAttempts); err != nil {
		t.Fatalf("set %s: %v", dbmodel.SettingKeyRelayMaxTotalAttempts, err)
	}
	if got := getMaxTotalAttempts(); got != maxTotalAttempts {
		t.Fatalf("getMaxTotalAttempts() = %d, want %d (setting did not take effect)", got, maxTotalAttempts)
	}

	// 熔断器与 Auto 统计是进程级全局状态：别的测试在同一 channelID 上打出的失败
	// 会让本测试的候选被 SkipCircuitBreak 直接跳掉（记录变成 circuit_break、
	// 上游一次都不打），配额断言随之失去意义。每个测试用独占 channelID 并前后各清一次。
	clearRuntime := func() {
		for _, spec := range specs {
			balancer.RemoveChannelEntries(spec.channelID)
			balancer.RemoveChannelStats(spec.channelID)
		}
	}
	clearRuntime()
	t.Cleanup(clearRuntime)

	chpkg.GetCache().Clear()
	grppkg.GetCache().Clear()

	items := make([]dbmodel.GroupItem, 0, len(specs))
	for i, spec := range specs {
		spec := spec
		status := spec.statusCode
		if status == 0 {
			status = http.StatusInternalServerError
		}
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			env.mu.Lock()
			env.hitOrder = append(env.hitOrder, spec.name)
			env.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			if status == http.StatusOK {
				_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGk="}]}`))
			} else {
				_, _ = w.Write([]byte(`{"error":{"message":"upstream refused"}}`))
			}
		}))
		t.Cleanup(upstream.Close)

		keys := make([]dbmodel.ChannelKey, 0, spec.keys)
		for k := 1; k <= spec.keys; k++ {
			keys = append(keys, dbmodel.ChannelKey{
				ID:         spec.channelID*10 + k,
				ChannelID:  spec.channelID,
				Enabled:    !spec.keysDisabled,
				ChannelKey: fmt.Sprintf("sk-%s-%d", spec.name, k),
			})
		}
		chpkg.GetCache().Set(spec.channelID, dbmodel.Channel{
			ID:       spec.channelID,
			Name:     spec.name,
			Enabled:  true,
			BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}},
			Keys:     keys,
		})
		upstreamModel := "upstream-" + spec.name
		if spec.emptyModel {
			upstreamModel = ""
		}
		items = append(items, dbmodel.GroupItem{
			ID:        i + 1,
			GroupID:   groupID,
			ChannelID: spec.channelID,
			ModelName: upstreamModel,
			Priority:  i + 1,
			Weight:    1,
		})
	}

	grppkg.GetCache().Set(groupID, dbmodel.Group{
		ID:           groupID,
		Name:         requestModel,
		EndpointType: dbmodel.EndpointTypeImageGeneration,
		Mode:         dbmodel.GroupModeFailover,
		Items:        items,
	})
	grppkg.RebuildIndexes()

	restore := relaylog.SetCacheForTest(nil)
	t.Cleanup(restore)

	t.Cleanup(func() {
		chpkg.GetCache().Clear()
		grppkg.GetCache().Clear()
		grppkg.RebuildIndexes()
		_ = db.Close()
	})

	return env
}

// runCapRequest 发一次 images/generations 请求走完整 MediaHandler 链路。
// 路径必须落在 /v1 前缀内（middleware/static.go:32 只放行 /api 与 /v1）。
func runCapRequest(t *testing.T, requestModel string, apiKeyID int) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"model": requestModel, "prompt": "a cat", "size": "1024x1024"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key_id", apiKeyID)
	MediaHandler(MediaEndpointImageGeneration, c)
	return rec
}

// loggedCapAttempts 取本次请求写下的 relay 日志里的 attempts 记录。
// 用来分离"上游被打了几次"和"记录里有几条"——配额只该管前者。
func loggedCapAttempts(t *testing.T, requestModel string) []dbmodel.ChannelAttempt {
	t.Helper()
	logs, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	defer lock.Unlock()
	var found []dbmodel.RelayLog
	for _, l := range logs {
		if l.RequestModelName == requestModel {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("relay log count for model %s: want exactly 1, got %d", requestModel, len(found))
	}
	return found[0].Attempts
}

// countStatus 数指定状态的 attempt 条数。
func countStatus(attempts []dbmodel.ChannelAttempt, status dbmodel.AttemptStatus) int {
	n := 0
	for _, a := range attempts {
		if a.Status == status {
			n++
		}
	}
	return n
}

// TestMaxTotalAttempts_capsRealUpstreamForwards
// ★ R-5 核心守卫：配额必须真的拦住上游转发。
//
// 拓扑：3 渠道 × 2 key，全部恒 500，cap=2。
// 不设配额时这套拓扑会打满 6 次（3 渠道 × 2 key，实测过）。
// 期望：上游只被打 2 次，且都落在第一个渠道（Failover 按 Priority 升序）。
//
// 断言落在假上游的命中次数（副作用），不是 HTTP 状态码：
// 闸门失效时状态码同样是 502、日志同样会写，只看返回值抓不到。
func TestMaxTotalAttempts_capsRealUpstreamForwards(t *testing.T) {
	const requestModel = "cap-r5-basic"
	env := initCapEnv(t, requestModel, 7301, []capChannelSpec{
		{name: "cap-a", channelID: 9311, keys: 2},
		{name: "cap-b", channelID: 9312, keys: 2},
		{name: "cap-c", channelID: 9313, keys: 2},
	}, 2)

	runCapRequest(t, requestModel, 59101)

	hits := env.hits()
	if len(hits) != 2 {
		t.Fatalf("upstream forwards = %d %v, want exactly 2 (cap=2); "+
			"more means the cap never fired, fewer means it fired too early", len(hits), hits)
	}
	// 配额耗尽应发生在第一个渠道内部（2 个 key 正好吃满），不该摸到 cap-b/cap-c。
	for i, name := range hits {
		if name != "cap-a" {
			t.Fatalf("forward %d hit %q, want all forwards on cap-a: %v", i+1, name, hits)
		}
	}
}

// TestMaxTotalAttempts_capSpansRouteRounds
// ★ 守跨 route round 累加。每个 route round 都新建 Iterator（retry_shared.go:112），
// 计数器归零；若闸门只看当前迭代器，配额就按轮重置，cap=3 在 maxRouteRetries=2
// 下会变成实际 6 次。
//
// 拓扑：2 渠道 × 1 key，恒 500，cap=3，route retries=2。
// 单轮上限 2 次转发，所以 cap=3 必然跨到第二轮才耗尽 —— 这正是能区分
// "累加"与"按轮重置"的取值。期望恰好 3 次。
func TestMaxTotalAttempts_capSpansRouteRounds(t *testing.T) {
	const requestModel = "cap-r5-rounds"
	env := initCapEnv(t, requestModel, 7302, []capChannelSpec{
		{name: "rnd-a", channelID: 9321, keys: 1},
		{name: "rnd-b", channelID: 9322, keys: 1},
	}, 3)

	// 显式钉住重试参数：默认值变了本测试的取值就不再跨轮，会退化成同轮测试。
	if err := setting.SetInt(dbmodel.SettingKeyRelayRetryCount, 1); err != nil {
		t.Fatalf("set retry count: %v", err)
	}
	if err := setting.SetInt(dbmodel.SettingKeyRelayRouteRetries, 2); err != nil {
		t.Fatalf("set route retries: %v", err)
	}
	if got := getMaxAttemptsPerCandidate(); got != 2 {
		t.Fatalf("maxAttemptsPerCandidate = %d, want 2", got)
	}
	if got := getMaxRouteRetries(); got != 2 {
		t.Fatalf("maxRouteRetries = %d, want 2", got)
	}

	runCapRequest(t, requestModel, 59102)

	hits := env.hits()
	if len(hits) != 3 {
		t.Fatalf("upstream forwards = %d %v, want exactly 3 (cap=3 spanning two route rounds); "+
			"4+ means the cap resets per route round, 2 means round 2 never started", len(hits), hits)
	}
	// 第 3 次必须发生在第二轮（又回到 rnd-a），确认真的跨轮了而不是同轮多打一次。
	if hits[0] != "rnd-a" || hits[1] != "rnd-b" || hits[2] != "rnd-a" {
		t.Fatalf("forward order = %v, want [rnd-a rnd-b rnd-a] (round 2 restarts at the first candidate)", hits)
	}
}

// TestMaxTotalAttempts_accumulatorIsAdditiveAcrossRounds
// ★ 累加器必须累加，不能覆盖（M11 守卫）。
//
// retry_shared.go 轮末的 `forwardedBefore += routeIter.ForwardedAttempts()`
// 若写成 `=`，需要**每轮转发数固定为 1 且配额跨过第 3 轮**才会分叉：
// 每轮 1 次时，累加版进入第 N 轮时是 N-1，覆盖版恒为 1，于是覆盖版直到
// 轮次用尽都不会触发闸门。
//
// 取值经实测校准（别按直觉改）：单渠道单 key，routeRetries=5、cap=3。
//
//	累加版 = 3 次（第 4 轮入口 forwardedBefore=3 命中闸门）
//	覆盖版 = 5 次（闸门永不触发，被 routeRetries 耗尽兜住）
//
// cap=2 / routeRetries=3 这组两版都是 2 次，区分不出来——初版就栽在这，
// 是 M11 存活逼出来的取值。
func TestMaxTotalAttempts_accumulatorIsAdditiveAcrossRounds(t *testing.T) {
	const requestModel = "cap-r5-additive"
	env := initCapEnv(t, requestModel, 7307, []capChannelSpec{
		{name: "acc-a", channelID: 9371, keys: 1},
	}, 3)

	if err := setting.SetInt(dbmodel.SettingKeyRelayRetryCount, 1); err != nil {
		t.Fatalf("set retry count: %v", err)
	}
	if err := setting.SetInt(dbmodel.SettingKeyRelayRouteRetries, 5); err != nil {
		t.Fatalf("set route retries: %v", err)
	}
	if got := getMaxRouteRetries(); got != 5 {
		t.Fatalf("maxRouteRetries = %d, want 5", got)
	}
	if got := getMaxAttemptsPerCandidate(); got != 2 {
		t.Fatalf("maxAttemptsPerCandidate = %d, want 2 (one forward per round)", got)
	}

	runCapRequest(t, requestModel, 59107)

	hits := env.hits()
	if len(hits) != 3 {
		t.Fatalf("upstream forwards = %d %v, want exactly 3 (cap=3, 1 forward per round, 5 rounds); "+
			"5 means forwardedBefore is assigned instead of accumulated, so the cap never fires",
			len(hits), hits)
	}
}

// TestMaxTotalAttempts_zeroMeansUnlimited
// ★ 反向守卫：cap=0 表示不限制（setting.go:148 默认值就是 "0"）。
// 只有"不超过 N"的断言时，把闸门改成恒 true 也能全绿；这条测试要求
// 配额关闭时所有候选都必须被真打，抓的是"改过严"的方向。
func TestMaxTotalAttempts_zeroMeansUnlimited(t *testing.T) {
	const requestModel = "cap-r5-unlimited"
	env := initCapEnv(t, requestModel, 7303, []capChannelSpec{
		{name: "unl-a", channelID: 9331, keys: 1},
		{name: "unl-b", channelID: 9332, keys: 1},
		{name: "unl-c", channelID: 9333, keys: 1},
	}, 0)

	if err := setting.SetInt(dbmodel.SettingKeyRelayRetryCount, 1); err != nil {
		t.Fatalf("set retry count: %v", err)
	}
	if err := setting.SetInt(dbmodel.SettingKeyRelayRouteRetries, 1); err != nil {
		t.Fatalf("set route retries: %v", err)
	}

	runCapRequest(t, requestModel, 59103)

	hits := env.hits()
	if len(hits) != 3 {
		t.Fatalf("upstream forwards = %d %v, want 3 (cap=0 must not limit anything)", len(hits), hits)
	}
}

// TestMaxTotalAttempts_doesNotCountSkippedChannels
// ★ 单位守卫 + 反向守卫：跳过的候选不消耗配额。
//
// Iterator.Attempts() 里混着 AttemptSkipped / AttemptCircuitBreak
// （iterator.go:96 与 :111），它们一次上游请求都没发。若闸门按记录条数计数，
// 前面被禁用的渠道会白吃配额，能真打的渠道反而被提前掐掉。
//
// 拓扑：两个"key 全禁"的渠道（各产生 1 条 skipped）在前，两个可用渠道在后，cap=2。
// 期望：两个可用渠道都被真打（2 次），且日志里确实有 2 条 skipped —— 后一条
// 断言守住"skip 真的发生了"，否则拓扑没生效，这个测试就变成自证。
//
// 这条断言不是装饰：初版用 Channel.Enabled=false 造 skip，被它抓出来是 0 条
// （禁用渠道在 group.go:333 就被剔掉了，压根没进重试循环），改成 key 全禁才生效。
func TestMaxTotalAttempts_doesNotCountSkippedChannels(t *testing.T) {
	const requestModel = "cap-r5-skips"
	env := initCapEnv(t, requestModel, 7304, []capChannelSpec{
		{name: "skp-off1", channelID: 9341, keys: 1, keysDisabled: true},
		{name: "skp-off2", channelID: 9342, keys: 1, keysDisabled: true},
		{name: "skp-live1", channelID: 9343, keys: 1},
		{name: "skp-live2", channelID: 9344, keys: 1},
	}, 2)

	if err := setting.SetInt(dbmodel.SettingKeyRelayRetryCount, 1); err != nil {
		t.Fatalf("set retry count: %v", err)
	}
	if err := setting.SetInt(dbmodel.SettingKeyRelayRouteRetries, 1); err != nil {
		t.Fatalf("set route retries: %v", err)
	}

	runCapRequest(t, requestModel, 59104)

	hits := env.hits()
	if len(hits) != 2 {
		t.Fatalf("upstream forwards = %d %v, want 2 — skipped candidates must not consume the budget", len(hits), hits)
	}
	if hits[0] != "skp-live1" || hits[1] != "skp-live2" {
		t.Fatalf("forward order = %v, want [skp-live1 skp-live2]", hits)
	}

	// 守拓扑本身：确认那两个 key 全禁的渠道真的产生了 skipped 记录。
	// 少了这条，哪怕 disabled 渠道压根没进候选列表，上面的断言也照样绿。
	attempts := loggedCapAttempts(t, requestModel)
	if got := countStatus(attempts, dbmodel.AttemptSkipped); got != 2 {
		t.Fatalf("skipped attempts = %d, want 2 (summary=%v); "+
			"the disabled channels never entered the candidate list, so this case proves nothing",
			got, attemptSummary(attempts))
	}
}

// TestMaxTotalAttempts_lastAllowedForwardStillSucceeds
// ★ 闸门必须在转发**之前**判定（M8 守卫）。
//
// 把闸门挪到 cbs.ForwardRequest 之后（retry_shared.go:236 之下、成功分支
// :243 之上），配额一样不会被超——转发次数照样是 cap 次，所以只数
// "上游被打了几次" 的测试全绿放行。真正的危害在别处：第 cap 次转发若成功，
// goto exhausted 会跳过成功分支，把一个 200 的上游响应变成 502，
// 且 OnSuccess 不执行（不写粘性、不记成功统计）。
//
// 实测下的真实症状（不是我预想的那样）：状态码仍是 200、attempts 里那条
// success 也在（媒体成功响应先写给下游、OnSuccess 也已执行），
// 唯一的破坏在**响应体**——闸门在成功分支之前 goto exhausted，
// OnExhausted 又往同一个已写出的响应后面追加了一段 502 JSON，
// 客户端收到 `{"data":[...]}{"code":502,...}` 这种拼接垃圾，JSON 解析直接失败。
// 所以断言必须落在 body 上；只断言 rec.Code==200 或 success 记录数会全绿放行。
//
// 拓扑：ch 恒 500 打掉第 1 次配额，第 2 个渠道返回 200，cap=2。
func TestMaxTotalAttempts_lastAllowedForwardStillSucceeds(t *testing.T) {
	const requestModel = "cap-r5-lastok"
	env := initCapEnv(t, requestModel, 7305, []capChannelSpec{
		{name: "lst-fail", channelID: 9351, keys: 1},
		{name: "lst-ok", channelID: 9352, keys: 1, statusCode: http.StatusOK},
		{name: "lst-extra", channelID: 9353, keys: 1},
	}, 2)

	if err := setting.SetInt(dbmodel.SettingKeyRelayRetryCount, 1); err != nil {
		t.Fatalf("set retry count: %v", err)
	}
	if err := setting.SetInt(dbmodel.SettingKeyRelayRouteRetries, 1); err != nil {
		t.Fatalf("set route retries: %v", err)
	}

	rec := runCapRequest(t, requestModel, 59105)

	hits := env.hits()
	if len(hits) != 2 || hits[0] != "lst-fail" || hits[1] != "lst-ok" {
		t.Fatalf("upstream forwards = %v, want [lst-fail lst-ok]", hits)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	// ★ 核心断言：响应体必须是上游那一份、且**只有**那一份。
	// 闸门若排在成功分支之后，这里会变成 200 响应体后面拼一段 502 JSON。
	body := rec.Body.String()
	if !json.Valid([]byte(body)) {
		t.Fatalf("response body is not valid JSON: %q; the cap fired after the forward and "+
			"OnExhausted appended an error payload onto an already-written success response", body)
	}
	if strings.Contains(body, "max total attempts") || strings.Contains(body, "all channels failed") {
		t.Fatalf("response body carries a cap/exhausted error alongside the success payload: %q", body)
	}
	attempts := loggedCapAttempts(t, requestModel)
	if got := countStatus(attempts, dbmodel.AttemptSuccess); got != 1 {
		t.Fatalf("success attempts = %d, want 1 (summary=%v); OnSuccess never ran",
			got, attemptSummary(attempts))
	}
}

// TestMaxTotalAttempts_stopsAdvancingCandidatesWhenExhausted
// ★ 通道循环入口的闸门必须存在（M10 守卫）。
//
// 只留 key 循环里的内层闸门、删掉通道循环入口那道，转发次数依然不超 cap
// （内层闸门会在下一次转发前拦住），所以只数转发次数的断言全绿放行。
// 真实差别是"配额耗尽后还在推进候选"：继续 ch.Get、继续 ResolveModel，
// 并把后续候选的 skipped 记录写进 attempts —— 日志里凭空多出从未发生的跳过，
// 且每个 route round 各来一遍。
//
// 拓扑经实测校准：候选 A 有 2 key（cap=2 正好在 key 循环**自然结束**时耗尽，
// 不是被内层闸门 goto 打断——否则控制流压根到不了外层闸门），
// 候选 B 的 GroupItem.ModelName 为空，一旦被推进就会记一条 skipped。
// 实测：有外层闸门 skipped=0；删掉后 skipped=2（两个 route round 各一次）。
//
// 用"key 全禁"的 B 造不出这个分叉（实测两版都是 0）：无可用 key 的跳过发生在
// key 循环内部，位置在内层闸门之后。必须用空 model 这条位于两道闸门**之间**
// 的分支（retry_shared.go:170）。
func TestMaxTotalAttempts_stopsAdvancingCandidatesWhenExhausted(t *testing.T) {
	const requestModel = "cap-r5-boundary"
	env := initCapEnv(t, requestModel, 7306, []capChannelSpec{
		{name: "bnd-burn", channelID: 9361, keys: 2},
		{name: "bnd-nomodel", channelID: 9362, keys: 1, emptyModel: true},
	}, 2)

	if err := setting.SetInt(dbmodel.SettingKeyRelayRetryCount, 1); err != nil {
		t.Fatalf("set retry count: %v", err)
	}
	if err := setting.SetInt(dbmodel.SettingKeyRelayRouteRetries, 1); err != nil {
		t.Fatalf("set route retries: %v", err)
	}
	// key 循环必须靠 for 条件正常退出（2 key 用满 2 次尝试），
	// 否则内层闸门先 goto，外层闸门这条路走不到，本测试就失去意义。
	if got := getMaxAttemptsPerCandidate(); got != 2 {
		t.Fatalf("maxAttemptsPerCandidate = %d, want 2", got)
	}

	runCapRequest(t, requestModel, 59106)

	hits := env.hits()
	if len(hits) != 2 || hits[0] != "bnd-burn" || hits[1] != "bnd-burn" {
		t.Fatalf("upstream forwards = %v, want [bnd-burn bnd-burn] (cap=2)", hits)
	}
	// 配额耗尽后不该再推进到下一个候选，所以那个空 model 的渠道
	// 不该留下任何 skipped 记录。
	attempts := loggedCapAttempts(t, requestModel)
	if got := countStatus(attempts, dbmodel.AttemptSkipped); got != 0 {
		t.Fatalf("skipped attempts = %d, want 0 (summary=%v); the cap did not stop candidate "+
			"advancement at the channel-loop entry, so a candidate that was never tried "+
			"got logged as skipped", got, attemptSummary(attempts))
	}
}
