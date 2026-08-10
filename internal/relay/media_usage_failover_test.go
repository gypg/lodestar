package relay

/*
P1 #11 —— 失败转移时 usage 不得串台（per-attempt 重置守卫）。

★ 这个文件是 M8 变异存活后靠探针挖出来的，不是预先想到的。M8 = 删掉
ForwardRequest 里的 `else { billedUsage = mediaUsage{} }` 重置分支。
删掉后全部既有测试照绿，因为最直觉的拓扑（A 返 500 带 usage → B 成功）
**根本到不了扫描器**：非 2xx 在 forwardMediaRequestJSON 里就带着错误提前 return，
响应体只被 LimitReader 读去做错误消息，从不进 usage.Scan。

真正可达的路径是 **"2xx 然后可重试地失败"**（type.go:345
`statusCode >= 200 && statusCode < 300 && err != nil` → ScopeNextChannel）：
MiMo TTS 上游返 200 且带 usage，但 choices 为空 → handleMimoTTSResponse
**先 Scan 了 usage 再报错** → 换渠道重试 → B 成功且不带 usage。
无重置时实测 relay_logs 记成 in=7777 out=6666（A 的数字算到了 B 的成功头上），
有重置时为 0/0。

判据来源：存活变异必须探针实测危害，不许推理（前两个拓扑推理都会得出"无害"的错误结论）。
*/

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestMediaHandler_failoverDoesNotBillPreviousAttemptUsage
// 渠道 A 返 200 + usage 但内容不可用（可重试错误）→ 渠道 B 成功且不带 usage。
// 断言写死"必须是 0/0"，因为 B 这一跳上游确实没报 usage。
// 若 per-attempt 重置被删 → 记成 7777/6666 → 红。
func TestMediaHandler_failoverDoesNotBillPreviousAttemptUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const requestModel = "usage-failover-tts"

	if err := db.InitDB("sqlite", "file:usagefailover?mode=memory&cache=shared", false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := db.GetDB().AutoMigrate(&dbmodel.Setting{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := setting.RefreshCache(nil); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("cache: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// A: 200 + usage, but choices[] empty -> handleMimoTTSResponse errors AFTER scanning.
	upA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[],"usage":{"input_tokens":7777,"output_tokens":6666}}`))
	}))
	t.Cleanup(upA.Close)
	// B: 200 + real audio, no usage.
	upB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"audio":{"data":"aGk="}}}]}`))
	}))
	t.Cleanup(upB.Close)

	const chA, chB = 4901, 4902
	for id, u := range map[int]string{chA: upA.URL, chB: upB.URL} {
		balancer.RemoveChannelEntries(id)
		balancer.RemoveChannelStats(id)
		chpkg.GetCache().Set(id, dbmodel.Channel{
			ID: id, Name: fmt.Sprintf("tts-%d", id), Enabled: true,
			BaseUrls: []dbmodel.BaseUrl{{URL: u}},
			Keys:     []dbmodel.ChannelKey{{ID: id * 10, ChannelID: id, Enabled: true, ChannelKey: "sk-x"}},
		})
	}
	grppkg.GetCache().Clear()
	// EndpointProvider "mimo" routes audio/speech through the chat-completions shim.
	grppkg.GetCache().Set(7901, dbmodel.Group{
		ID: 7901, Name: requestModel, EndpointType: dbmodel.EndpointTypeAudioSpeech,
		Mode: dbmodel.GroupModeFailover, EndpointProvider: "mimo",
		Items: []dbmodel.GroupItem{
			{ID: 1, GroupID: 7901, ChannelID: chA, ModelName: "up-a", Priority: 1, Weight: 1},
			{ID: 2, GroupID: 7901, ChannelID: chB, ModelName: "up-b", Priority: 2, Weight: 1},
		},
	})
	grppkg.RebuildIndexes()
	t.Cleanup(func() {
		for _, id := range []int{chA, chB} {
			balancer.RemoveChannelEntries(id)
			balancer.RemoveChannelStats(id)
		}
		chpkg.GetCache().Clear()
		grppkg.GetCache().Clear()
		grppkg.RebuildIndexes()
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech",
		strings.NewReader(fmt.Sprintf(`{"model":%q,"input":"hello","voice":"alloy"}`, requestModel)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key_id", 66097)
	MediaHandler(MediaEndpointAudioSpeech, c)

	if rec.Code != http.StatusOK {
		t.Fatalf("failover should end in success: got status %d (body len %d)", rec.Code, rec.Body.Len())
	}

	logs, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	last := logs[len(logs)-1]
	lock.Unlock()

	// 前置校验：确实发生了两跳，否则下面的断言是空转（A 没被打过的话 0/0 自然成立）。
	if last.TotalAttempts != 2 {
		t.Fatalf("expected 2 attempts (A fails, B succeeds), got %d — topology did not exercise failover", last.TotalAttempts)
	}
	// B 这一跳上游没报 usage，所以必须恰好是 0/0。
	if last.InputTokens != 0 || last.OutputTokens != 0 {
		t.Errorf("failover leaked the failed attempt's usage: want in=0 out=0 (channel B reported none), "+
			"got in=%d out=%d — 7777/6666 means the per-attempt usage reset in ForwardRequest is gone",
			last.InputTokens, last.OutputTokens)
	}
}
