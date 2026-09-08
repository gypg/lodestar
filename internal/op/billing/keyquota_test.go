package billing

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
)

// WO-044 #234：per-key MaxCost in-flight 准入。纯 check-then-act 下，并发 N 个
// 请求会在首个请求结算前全部通过闸门，配额被突破 N×单请求成本。本组测试钉死：
// 放行条数 = headroom / est 的边界、旋钮未配置时退化为串行（安全默认）、
// release 归还槽位、无 MaxCost 的 key 不受影响。

// initKeyQuotaTestDB 与 initBillingTestDB 同构：settings 表存在且缓存已刷新，
// SetString 才能生效（QuotaAdmit 的 est 读的是 max_expected_request_cost）。
func initKeyQuotaTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.User{}, &model.APIKey{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func freshKeyQuota(t *testing.T) {
	t.Helper()
	keyQuotaMu.Lock()
	inflightByKey = make(map[int]int)
	keyQuotaMu.Unlock()
	t.Cleanup(func() {
		keyQuotaMu.Lock()
		defer keyQuotaMu.Unlock()
		if len(inflightByKey) != 0 {
			t.Errorf("key 在途表收尾未归零: %v（release 漏调）", inflightByKey)
		}
	})
}

func TestQuotaAdmit_boundsConcurrencyByExpectedCost(t *testing.T) {
	initKeyQuotaTestDB(t)
	freshKeyQuota(t)
	setMaxExpectedRequestCost(t, 0.5)

	// MaxCost=10, used=0 → headroom=10。准入条件 inflight×0.5 < 10 → 最多 20 条。
	admitted := 0
	var releases []func()
	for i := 0; i < 25; i++ {
		release, ok := QuotaAdmit(1, 10.0, 0)
		if release == nil {
			t.Fatalf("第 %d 条: release 为 nil", i+1)
		}
		if ok {
			admitted++
			releases = append(releases, release)
		}
	}
	if admitted != 20 {
		t.Fatalf("放行 %d 条, want 20（headroom/est 边界漂移）", admitted)
	}

	for _, r := range releases {
		r()
	}
	if got := InflightForKey(1); got != 0 {
		t.Fatalf("释放后在途 = %d, want 0", got)
	}
	// 全部释放后必须还能再准入——闸门是排队，不是拉黑。
	release, ok := QuotaAdmit(1, 10.0, 0)
	if !ok {
		t.Fatal("在途归零后仍被拒——闸门把有额度的 key 永久挡住了")
	}
	release()
}

func TestQuotaAdmit_unsetKnobSerializes(t *testing.T) {
	initKeyQuotaTestDB(t)
	freshKeyQuota(t)
	// 旋钮未配置（默认 0）：est 退化为全部余量 → 并发串行，防突发全过闸。
	if err := setting.SetString(model.SettingKeyMaxExpectedRequestCost, "0"); err != nil {
		t.Fatalf("reset knob: %v", err)
	}
	if got := maxExpectedRequestCost(); got != 0 {
		t.Fatalf("knob = %v, want 0", got)
	}

	release, ok := QuotaAdmit(2, 10.0, 3.0)
	if !ok {
		t.Fatal("headroom 7 > 0 却被拒——首条必须放行")
	}
	// 第二条：est = headroom(7)，1×7 >= 7 → 拒。
	if _, ok := QuotaAdmit(2, 10.0, 3.0); ok {
		t.Fatal("旋钮未配置时第二条被放行——串行默认失效，突发敞口回归")
	}
	release()
	// 释放后 headroom 不变，又能准入。
	release2, ok := QuotaAdmit(2, 10.0, 3.0)
	if !ok {
		t.Fatal("释放后仍被拒——槽位没归还")
	}
	release2()
}

func TestQuotaAdmit_exhaustedAndUncappedKeys(t *testing.T) {
	initKeyQuotaTestDB(t)
	freshKeyQuota(t)
	setMaxExpectedRequestCost(t, 0.5)

	// 已超限（used >= MaxCost）：headroom <= 0，无论在途多少都拒。
	if _, ok := QuotaAdmit(3, 10.0, 10.0); ok {
		t.Fatal("used == MaxCost 被放行——headroom<=0 必须拒")
	}
	if _, ok := QuotaAdmit(3, 10.0, 12.0); ok {
		t.Fatal("used > MaxCost 被放行")
	}

	// 未设 MaxCost 的 key：无条件放行（结算期检查照旧），且不进在途表。
	release, ok := QuotaAdmit(4, 0, 999.0)
	if !ok {
		t.Fatal("无 MaxCost 的 key 被拒——回归了旧缺陷面")
	}
	if got := InflightForKey(4); got != 0 {
		t.Fatalf("无 MaxCost key 进了在途表: %d", got)
	}
	release()
}

func TestQuotaAdmit_settledUsageEatsHeadroom(t *testing.T) {
	initKeyQuotaTestDB(t)
	freshKeyQuota(t)
	setMaxExpectedRequestCost(t, 0.5)

	// used=9.7, MaxCost=10 → headroom=0.3 → 准入 1 条（0 < 0.3），第 2 条拒。
	release, ok := QuotaAdmit(5, 10.0, 9.7)
	if !ok {
		t.Fatal("headroom 0.3 首条被拒")
	}
	if _, ok := QuotaAdmit(5, 10.0, 9.7); ok {
		t.Fatal("headroom 0.3 放行了两条——0.5×1 >= 0.3 应拒")
	}
	release()
	if v, err := strconv.ParseFloat("0.5", 64); err != nil || v != 0.5 {
		t.Fatalf("sanity: %v %v", v, err)
	}
}
