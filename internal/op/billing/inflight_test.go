package billing

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/user"
)

// 并发闸门（inflight.go）的契约。
//
// 这些测试一律钉死"放行几条"的精确条数，而不是"是否放行"。因为这个洞的本体就是
// 条数：旧闸门的每一条断言都能通过 —— 余额为正才放行，也确实只放行余额为正的请求 ——
// 唯独没人数过同一时刻有多少条在途。断言写成 ok/not-ok 的话，洞会原样活下来。

// setMaxExpectedRequestCost 改写"单次请求假定最坏成本"，并回读确认闸门真的看到了
// 这个值。回读不是多余的：设置读不出来时 maxExpectedRequestCost 会返回 0，也就是
// 静默关闭并发闸门，此时下面所有 want 都会变成"全放行"而测试自己不会喊。
func setMaxExpectedRequestCost(t *testing.T, v float64) {
	t.Helper()
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if err := setting.SetString(model.SettingKeyMaxExpectedRequestCost, s); err != nil {
		t.Fatalf("set %s=%s: %v", model.SettingKeyMaxExpectedRequestCost, s, err)
	}
	if got := maxExpectedRequestCost(); got != v {
		t.Fatalf("闸门读到的假定成本 = %v, want %v（设置没生效，并发闸门实际是关着的）", got, v)
	}
}

// freshInflight 清空包级在途计数，并在测试结束时要求归零。
// 归零这条断言同时钉死 inflight.go 里"计数归零就删表项"的承诺：漏掉 release 或者
// 只减不删，都会让这里红，也不会把脏状态漏给同包的下一条测试。
func freshInflight(t *testing.T) {
	t.Helper()
	inflightMu.Lock()
	inflightByUser = make(map[uint]int)
	inflightMu.Unlock()
	t.Cleanup(func() {
		inflightMu.Lock()
		defer inflightMu.Unlock()
		if len(inflightByUser) != 0 {
			t.Errorf("在途表收尾未归零: %v（release 漏调，或计数归零后没删表项）", inflightByUser)
		}
	})
}

// burst 连续申请 n 次且一律不释放，模拟"上游还没返回时下一条就打进来"。
// 返回被放行的条数，以及一次性释放全部槽位的函数。
func burst(t *testing.T, keyID, n int) (admitted int, releaseAll func()) {
	t.Helper()
	var releases []func()
	for i := 0; i < n; i++ {
		release, ok := AcquireForKey(keyID, context.Background())
		if release == nil {
			t.Fatalf("第 %d 条: release 返回 nil —— 调用方 defer 它，nil 会直接 panic", i+1)
		}
		if ok {
			admitted++
			releases = append(releases, release)
		}
	}
	return admitted, func() {
		for _, r := range releases {
			r()
		}
	}
}

// TestAcquireForKey_thinBalanceBurstIsSerialized 钉死本次修复的洞本体。
//
// 修复前实测（scripts/verify-payment-chain.mjs 步骤 11，上游带真实延迟）：余额
// $0.005 的用户一次并发打 20 条，20 条全部被服务，结算完停在 -$0.205 —— 预付金额
// 的 41 倍。20 条都在第一条结算之前通过了 `remaining > 0` 这道纯谓词闸门。敞口不是
// "一次请求"，而是"并发数 × 一次请求"，并发数由调用方选。
//
// 修复后同一台服务器同一个探针，只改设置：闸门开 → 20 条里服务 1 条，余额 -$0.0055
// （一次请求的透支）；闸门关 → 仍是 20/20、-$0.205。
//
// 这条测试是那个探针的进程内版本：$0.005 盖不住一次假定 $0.5 的成本，所以 20 条里
// 只放行 1 条，剩下 19 条被 402 —— 是串行化，不是封号：释放后它照样能再发一条。
func TestAcquireForKey_thinBalanceBurstIsSerialized(t *testing.T) {
	uid, keyID := initBillingTestDB(t, 0.005)
	freshInflight(t)
	setMaxExpectedRequestCost(t, 0.5)

	admitted, releaseAll := burst(t, keyID, 20)
	if admitted != 1 {
		t.Fatalf("20 条并发放行了 %d 条, want 1（并发闸门没生效，透支敞口 = 并发数 × 单次成本）", admitted)
	}
	if got := InflightForUser(uid); got != 1 {
		t.Fatalf("在途计数 = %d, want 1", got)
	}

	releaseAll()

	if got := InflightForUser(uid); got != 0 {
		t.Fatalf("释放后在途计数 = %d, want 0（槽位没还回去，用户会被自己上一条请求永久挡住）", got)
	}
	// 余额仍是正的，欠款也还没记上，所以这个账户必须还能再发一条 —— 闸门的作用是
	// 排队，不是把薄余额账户拉黑。
	release, ok := AcquireForKey(keyID, context.Background())
	if !ok {
		t.Fatal("在途归零后仍被拒 —— 薄余额账户被永久挡住了，它还有钱可花")
	}
	release()
}

// TestAcquireForKey_headroomBoundsConcurrency 钉死放行条数 = 可用额度 / 假定成本
// 的边界。上界取整方式、等号该落在哪一侧、以及关掉并发闸门后是否退回旧行为，全在
// 这张表里，任何一个改动都会打中至少一行。
func TestAcquireForKey_headroomBoundsConcurrency(t *testing.T) {
	const attempts = 25
	for _, tt := range []struct {
		name    string
		balance float64
		limit   float64
		want    int
		why     string
	}{
		{
			name: "wallet10_cost0.5", balance: 10, limit: 0.5, want: 20,
			why: "10 > 19×0.5 放行第 20 条; 10 > 20×0.5 不成立, 第 21 条必须拦",
		},
		{
			name: "wallet1_cost0.5", balance: 1, limit: 0.5, want: 2,
			why: "1 > 1×0.5 放行第 2 条; 1 > 2×0.5 不成立",
		},
		{
			name: "wallet0.5_cost0.5", balance: 0.5, limit: 0.5, want: 1,
			why: "可用额度恰好等于一条在途的假定成本 —— 等号必须算盖不住, 否则每个账户都白拿一条",
		},
		{
			name: "wallet0.005_cost0.5", balance: 0.005, limit: 0.5, want: 1,
			why: "线上探针那个账户",
		},
		{
			name: "wallet0_cost0.5", balance: 0, limit: 0.5, want: 0,
			why: "在途 0 时规则退化成 headroom > 0, 空钱包一条都不放",
		},
		{
			name: "wallet_negative_cost0.5", balance: -1, limit: 0.5, want: 0,
			why: "已欠款: 闸门必须拦住下一条, 这是透支循环的出口",
		},
		{
			name: "wallet0.005_cost0_gateOff", balance: 0.005, limit: 0, want: attempts,
			why: "假定成本 0 = 关闭并发闸门(旧行为), 只剩余额为正这一条要求",
		},
		{
			name: "wallet0_cost0_gateOff", balance: 0, limit: 0, want: 0,
			why: "关掉并发闸门不等于关掉余额闸门",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			uid, keyID := initBillingTestDB(t, tt.balance)
			freshInflight(t)
			setMaxExpectedRequestCost(t, tt.limit)

			admitted, releaseAll := burst(t, keyID, attempts)
			defer releaseAll()

			if admitted != tt.want {
				t.Errorf("余额 %v / 假定成本 %v: %d 条并发放行了 %d 条, want %d —— %s",
					tt.balance, tt.limit, attempts, admitted, tt.want, tt.why)
			}
			if got := InflightForUser(uid); got != tt.want {
				t.Errorf("在途计数 = %d, want %d（放行了但没计数，闸门对下一条就失效了）", got, tt.want)
			}
		})
	}
}

// TestAcquireForKey_walletDebtDoesNotEatTheSubscriptionPool
//
// 钱包欠 $5、订阅池还剩 $3。池是用户已经付过钱的额度，不能被钱包欠款抵掉，所以
// 可用额度 = max(-5, 0) + 3 = 3，假定成本 $0.5 → 恰好 6 条并发。
//
// 去掉 headroomForUser 里那个"钱包为负按 0 算"的下限，可用额度会算成 -2，一条都不
// 放行 —— 等于把已售额度没收，是退款纠纷而不是省钱。
func TestAcquireForKey_walletDebtDoesNotEatTheSubscriptionPool(t *testing.T) {
	uid, keyID := initBillingTestDB(t, -5.0)
	freshInflight(t)
	setMaxExpectedRequestCost(t, 0.5)
	grantPool(t, uid, 3.0, 0)

	admitted, releaseAll := burst(t, keyID, 10)
	defer releaseAll()

	if admitted != 6 {
		t.Fatalf("钱包 -$5 + 池剩 $3: 放行了 %d 条, want 6（可用额度应为 0+3=3, 欠款不得吃掉已售池额度）", admitted)
	}
	if got := InflightForUser(uid); got != 6 {
		t.Fatalf("在途计数 = %d, want 6", got)
	}
}

// TestAcquireForKey_doubleReleaseDoesNotMintCapacity
//
// release 会被重复调用：中间件 defer 一次，将来若有人在 handler 里再补一次、或者
// 重试/恢复路径上走两遍，都会发生。重复调用只能还回一个槽位。
// 少了 sync.Once，第二次调用会把计数再减一，凭空造出一个不存在的槽位 —— 那正是
// 并发上限被悄悄抬高的方式。
func TestAcquireForKey_doubleReleaseDoesNotMintCapacity(t *testing.T) {
	_, keyID := initBillingTestDB(t, 1.0) // 假定成本 0.5 → 并发上限 2
	freshInflight(t)
	setMaxExpectedRequestCost(t, 0.5)
	ctx := context.Background()

	r1, ok := AcquireForKey(keyID, ctx)
	if !ok {
		t.Fatal("第 1 条被拒")
	}
	r2, ok := AcquireForKey(keyID, ctx)
	if !ok {
		t.Fatal("第 2 条被拒（余额 $1 能盖住 2 条 $0.5）")
	}
	if _, ok := AcquireForKey(keyID, ctx); ok {
		t.Fatal("第 3 条被放行 —— 上限应为 2")
	}

	r1()
	r1() // 重复释放

	r3, ok := AcquireForKey(keyID, ctx)
	if !ok {
		t.Fatal("释放一个槽位后仍拒绝新请求")
	}
	if _, extra := AcquireForKey(keyID, ctx); extra {
		t.Fatal("重复 release 凭空造出了第二个槽位 —— 并发上限被抬高了")
	}

	r2()
	r3()
}

// TestAcquireForKey_parallelBurstRespectsTheBound
//
// 上面几条都是顺序调用，只能证明规则算得对，证明不了它在真并发下守得住：算可用额度
// 和加计数之间如果没有同一把锁，60 条同时进来会各自读到 inflight=0 然后全部放行 ——
// 那就是原样把洞放回去。这条在 -race 下跑，既查条数也查数据竞争。
func TestAcquireForKey_parallelBurstRespectsTheBound(t *testing.T) {
	uid, keyID := initBillingTestAheadOfBurst(t)
	const goroutines = 60
	const wantAdmitted = 20 // 余额 $10 / 假定成本 $0.5

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		releases []func()
		start    = make(chan struct{})
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			release, ok := AcquireForKey(keyID, context.Background())
			mu.Lock()
			defer mu.Unlock()
			if ok {
				releases = append(releases, release)
			}
		}()
	}
	close(start)
	wg.Wait()

	if len(releases) != wantAdmitted {
		t.Fatalf("%d 条同时申请放行了 %d 条, want %d（多于预期 = 算额度与加计数之间没有互斥, 或查库出错走了 fail-open）",
			goroutines, len(releases), wantAdmitted)
	}
	if got := InflightForUser(uid); got != wantAdmitted {
		t.Fatalf("在途计数 = %d, want %d", got, wantAdmitted)
	}
	for _, r := range releases {
		r()
	}
	if got := InflightForUser(uid); got != 0 {
		t.Fatalf("全部释放后在途计数 = %d, want 0", got)
	}
}

// initBillingTestAheadOfBurst 建库 + 预热：先跑一次 Acquire/release 把 apikey 缓存
// 和 sqlite 连接踩热，免得并发那一刻的首次查库开销被误读成竞态。
func initBillingTestAheadOfBurst(t *testing.T) (uint, int) {
	t.Helper()
	uid, keyID := initBillingTestDB(t, 10.0)
	freshInflight(t)
	setMaxExpectedRequestCost(t, 0.5)
	release, ok := AcquireForKey(keyID, context.Background())
	if !ok {
		t.Fatal("预热请求被拒")
	}
	release()
	return uid, keyID
}

// TestAcquireForKey_failOpenPathsAdmitWithoutCounting
//
// 三条 fail-open 路径（计费关闭、key 无主）必须放行，且不得往在途表里塞计数 ——
// 塞了就会泄漏，把一个本不受限的账户越勒越紧。同时钉死这些路径也返回非 nil 的
// release：调用方无条件 defer 它。
func TestAcquireForKey_failOpenPathsAdmitWithoutCounting(t *testing.T) {
	t.Run("billing_off", func(t *testing.T) {
		_, keyID := initBillingTestDB(t, 0) // 空钱包
		freshInflight(t)
		if err := setting.SetString(model.SettingKeyCommercialMode, "false"); err != nil {
			t.Fatal(err)
		}
		release, ok := AcquireForKey(keyID, context.Background())
		if !ok || release == nil {
			t.Fatalf("计费关闭时 ok=%v release==nil=%v, want true/false（自用模式不该拦任何请求）", ok, release == nil)
		}
		release()
		assertNoInflightEntries(t)
	})

	t.Run("unknown_key", func(t *testing.T) {
		initBillingTestDB(t, 0)
		freshInflight(t)
		setMaxExpectedRequestCost(t, 0.5)
		release, ok := AcquireForKey(999999, context.Background())
		if !ok || release == nil {
			t.Fatalf("查不到的 key: ok=%v release==nil=%v, want true/false（查库失败不能拖垮中继）", ok, release == nil)
		}
		release()
		assertNoInflightEntries(t)
	})
}

func assertNoInflightEntries(t *testing.T) {
	t.Helper()
	inflightMu.Lock()
	defer inflightMu.Unlock()
	if len(inflightByUser) != 0 {
		t.Fatalf("fail-open 路径往在途表塞了计数: %v（会泄漏并越勒越紧）", inflightByUser)
	}
}

// TestMaxExpectedRequestCost_badValuesDisableTheBound 钉死配置解析：读不出、解析
// 失败、负数、非有限值，一律当 0（关闭并发闸门）处理，而不是让负数把比较式反过来。
//
// NaN / Inf 这几行不是凑数：strconv.ParseFloat("NaN") 和 ("Inf") 都是**成功**返回的，
// 所以它们躲得过 `err != nil`，也躲得过 `v < 0`（NaN 的任何比较都是 false）。躲过之后
// 会发生什么见 TestMaxExpectedRequestCost_nonFiniteValuesCannotBypassTheGate。
func TestMaxExpectedRequestCost_badValuesDisableTheBound(t *testing.T) {
	initBillingTestDB(t, 1.0)
	for _, tt := range []struct {
		raw  string
		want float64
	}{
		{raw: "0.5", want: 0.5},
		{raw: "0", want: 0},
		{raw: "-1", want: 0},
		{raw: "abc", want: 0},
		{raw: "", want: 0},
		{raw: "NaN", want: 0},
		{raw: "nan", want: 0},
		{raw: "Inf", want: 0},
		{raw: "+Inf", want: 0},
		{raw: "-Inf", want: 0},
		{raw: "Infinity", want: 0},
		// 1e400 溢出 float64：ParseFloat 返回 (+Inf, ErrRange)，靠 err 就挡住了，
		// 和上面几个"解析成功的非有限值"不是同一条路径，两条都要钉。
		{raw: "1e400", want: 0},
	} {
		if err := setting.SetString(model.SettingKeyMaxExpectedRequestCost, tt.raw); err != nil {
			t.Fatalf("set %q: %v", tt.raw, err)
		}
		if got := maxExpectedRequestCost(); got != tt.want {
			t.Errorf("配置值 %q → 假定成本 %v, want %v", tt.raw, got, tt.want)
		}
	}
}

// TestMaxExpectedRequestCost_nonFiniteValuesCannotBypassTheGate 钉死：配置里的
// 非有限值不得把余额闸门整个顶开。
//
// 这不是理论洁癖，是一条可达的提权路径。max_expected_request_cost 归 settings:write
// 管，而 settings:write **editor 角色也持有**（internal/server/auth/permissions.go），
// 且这个键在 Setting.Validate() 里原本没有对应分支 —— 落到函数末尾的 return nil，
// 任意字符串都存得进去。
//
// 于是 "NaN" 一存，闸门的比较式 `headroom <= inflight*limit` 就变成 `headroom <= NaN`，
// 对任何 headroom 都是 false（IEEE-754：与 NaN 的任何比较都为假）→ 无条件放行。
// "Inf" 走的是另一条：inflight=0 时 0*(+Inf) = NaN，同样恒 false。
//
// 恒 false 的后果不是"并发闸门失效"这么轻 —— 是连"余额为负必须拒"都失效，也就是把
// f6c0128 修掉的无限白嫖洞原样重开。所以这里用一个**已经欠款**的账户（headroom 被
// wallet floor 夹到 0）来测：任何配置值下都必须拒。
func TestMaxExpectedRequestCost_nonFiniteValuesCannotBypassTheGate(t *testing.T) {
	uid, keyID := initBillingTestDB(t, -1.0) // 已欠款：wallet floor 后 headroom = 0
	freshInflight(t)
	ctx := context.Background()

	for _, raw := range []string{"NaN", "nan", "Inf", "+Inf", "Infinity", "-Inf", "1e400", "0", "0.5"} {
		if err := setting.SetString(model.SettingKeyMaxExpectedRequestCost, raw); err != nil {
			t.Fatalf("set %q: %v", raw, err)
		}
		release, ok := AcquireForKey(keyID, ctx)
		if ok {
			release()
			t.Errorf("配置值 %q：欠款账户被放行了 —— 余额闸门被配置顶开，无限白嫖洞重开", raw)
		}
	}

	// 反向对照：上面全拒不能是因为 AcquireForKey 恒拒。给账户充钱后必须放行，
	// 否则这条测试是空过的 —— 一个永远返回 false 的闸门也能让上面的循环全绿。
	if err := setting.SetString(model.SettingKeyMaxExpectedRequestCost, "0.5"); err != nil {
		t.Fatal(err)
	}
	if err := user.AddQuota(uid, 10.0, ctx); err != nil {
		t.Fatal(err)
	}
	release, ok := AcquireForKey(keyID, ctx)
	if !ok {
		t.Fatal("充值到 $9 后仍被拒 —— 闸门恒拒，上面那圈断言全是空过的")
	}
	release()
}
