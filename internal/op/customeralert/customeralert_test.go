package customeralert

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"gorm.io/gorm"

	"github.com/gypg/lodestar/internal/utils/cache"
)

// setupAlertTestDB 独立内存库 + settings 缓存，与 modelprobe_test 同款纪律：
// 每个 t 一套 DSN，避免 shared-cache 串扰。
func setupAlertTestDB(t *testing.T) context.Context {
	t.Helper()
	ctx := context.Background()
	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_ = cache.New[model.SettingKey, string](16)
	if err := setting.RefreshCache(ctx); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}
	if err := db.GetDB().WithContext(ctx).AutoMigrate(&model.UserAlertState{}); err != nil {
		t.Fatalf("migrate user_alert_states: %v", err)
	}
	return ctx
}

func setAlertSetting(t *testing.T, key model.SettingKey, value string) {
	t.Helper()
	if err := setting.SetString(key, value); err != nil {
		t.Fatalf("set setting %s=%s: %v", key, value, err)
	}
}

// spySender 记录每次发送（收件人 + 正文），可编程失败。
type spySender struct {
	mu     sync.Mutex
	calls  []string
	failAt map[int]error // 第 N 次调用（0 起）返回的错误
}

func (s *spySender) fn() SendFn {
	return func(_ context.Context, u *model.User, message string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls = append(s.calls, u.Email+"|"+message)
		if err, ok := s.failAt[len(s.calls)-1]; ok {
			return err
		}
		return nil
	}
}

func (s *spySender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func alertUser(id uint, email string, quota float64) *model.User {
	return &model.User{ID: id, Email: email, Quota: quota}
}

func readState(t *testing.T, ctx context.Context, userID uint) model.UserAlertState {
	t.Helper()
	var row model.UserAlertState
	if err := db.GetDB().WithContext(ctx).Where("user_id = ?", userID).First(&row).Error; err != nil {
		t.Fatalf("read user_alert_states for %d: %v", userID, err)
	}
	return row
}

// T-C1 跨过阈值发一次：余额从阈值上方降到下方 → 恰好一封。
// 同测试覆盖结案语义：回升后再次跌穿同一阈值 → 新 episode 再发一封。
func TestBalanceAlertFiresOncePerCrossing(t *testing.T) {
	ctx := setupAlertTestDB(t)
	setAlertSetting(t, model.SettingKeyCustomerAlertBalanceThreshold, "10")

	spy := &spySender{}
	u := alertUser(960101, "customer-a@example.com", 15)

	// 余额充足：不发。
	if err := CheckUserBalance(ctx, u, spy.fn()); err != nil {
		t.Fatalf("healthy balance check: %v", err)
	}
	if got := spy.count(); got != 0 {
		t.Fatalf("healthy balance sent %d mails, want 0 (T-C3 leg)", got)
	}

	// 跌穿阈值：发一封。
	u.Quota = 9
	if err := CheckUserBalance(ctx, u, spy.fn()); err != nil {
		t.Fatalf("crossing check: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("after crossing sent %d mails, want exactly 1 (T-C1)", got)
	}
	if !strings.Contains(spy.calls[0], "customer-a@example.com") {
		t.Fatalf("mail went to wrong recipient: %q", spy.calls[0])
	}
	if strings.Contains(spy.calls[0], "sk-") || strings.Contains(spy.calls[0], "http") {
		t.Fatalf("mail carries sensitive info (key/url): %q — only balance facts may be included", spy.calls[0])
	}

	// 同档位再跑一个周期：不再发（T-C2 leg，M-C1 杀腿）。
	if err := CheckUserBalance(ctx, u, spy.fn()); err != nil {
		t.Fatalf("same-tier recheck: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("same tier re-run sent again: %d mails, want still 1 (T-C2: same tier must not re-send)", got)
	}

	// 回升结案 + 再次跌穿同档：新 episode，再发一封。
	u.Quota = 20
	if err := CheckUserBalance(ctx, u, spy.fn()); err != nil {
		t.Fatalf("recovery check: %v", err)
	}
	u.Quota = 8
	if err := CheckUserBalance(ctx, u, spy.fn()); err != nil {
		t.Fatalf("re-crossing check: %v", err)
	}
	if got := spy.count(); got != 2 {
		t.Fatalf("re-crossing after recovery sent %d mails, want 2 (new episode must re-arm)", got)
	}
}

// T-C2 专测：更深层级（跌穿一半阈值）是新档位 → 发；同档位重复 → 不发。
// 这是 M-C1（去掉防重记录）的精确杀腿：没有防重记录时第二轮必然再发。
func TestBalanceAlertDeeperTierFiresButSameTierDoesNot(t *testing.T) {
	ctx := setupAlertTestDB(t)
	setAlertSetting(t, model.SettingKeyCustomerAlertBalanceThreshold, "10")

	spy := &spySender{}
	u := alertUser(960102, "customer-b@example.com", 9)

	if err := CheckUserBalance(ctx, u, spy.fn()); err != nil {
		t.Fatalf("first crossing: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("first crossing sent %d, want 1", got)
	}

	// 跌到更深档位（<5）：更紧急，允许再发一封。
	u.Quota = 4
	if err := CheckUserBalance(ctx, u, spy.fn()); err != nil {
		t.Fatalf("deeper tier: %v", err)
	}
	if got := spy.count(); got != 2 {
		t.Fatalf("deeper tier sent %d, want 2 (a lower tier is a new, more urgent alert)", got)
	}

	// 深档位内再跑：不发。
	u.Quota = 3
	if err := CheckUserBalance(ctx, u, spy.fn()); err != nil {
		t.Fatalf("same deep tier: %v", err)
	}
	if got := spy.count(); got != 2 {
		t.Fatalf("same deep tier re-run sent again: %d, want still 2 (T-C2)", got)
	}
}

// T-C3 余额充足从不发（独立用户、独立断言，防止 T-C1 里健康腿被后续语句掩盖）。
func TestBalanceAlertNeverFiresWhenHealthy(t *testing.T) {
	ctx := setupAlertTestDB(t)
	setAlertSetting(t, model.SettingKeyCustomerAlertBalanceThreshold, "10")

	spy := &spySender{}
	u := alertUser(960103, "customer-c@example.com", 100)
	for round := 0; round < 3; round++ {
		if err := CheckUserBalance(ctx, u, spy.fn()); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
	}
	if got := spy.count(); got != 0 {
		t.Fatalf("healthy balance sent %d mails over 3 rounds, want 0 (T-C3)", got)
	}
}

// T-A1（WO-030 缺陷 A 核心腿）：最后 24 小时 daysLeft=0——首次发 1 封；同一状态
// 第二轮仍为 0 天，邮件数必须仍是 1。修复前哨兵撞值导致每小时重发（P0）。
// 产品语义写死：1 天提醒 + 0 天提醒共 2 封，之后同紧迫度不再发。
func TestExpiryAlertZeroDayNoResend(t *testing.T) {
	ctx := setupAlertTestDB(t)
	setAlertSetting(t, model.SettingKeyCustomerAlertExpiryDays, "3")

	spy := &spySender{}
	u := alertUser(960104, "customer-d@example.com", 100)
	// daysLeft = int64(hours/24) 向下取整（WO-030 缺陷 A 的语义土壤）：
	// "N 天剩余"必须落在桶内——now()+N*24h+6h 才算出 N；now()+N*24h 算出 N-1。
	day := int64(24 * 3600)
	inBucket := func(days int64) int64 { return now() + days*day + 6*3600 }

	// 1 天剩余：发第一封。
	subs := []model.UserSubscription{{ID: 7, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: inBucket(1)}}
	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("1-day check: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("1-day remaining sent %d mails, want 1", got)
	}

	// T-A2：降到 0 天（更紧迫）→ 允许再发一封。
	subs[0].ExpiresAt = now() + 3600 // 1 小时后 → daysLeft=0
	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("0-day check: %v", err)
	}
	if got := spy.count(); got != 2 {
		t.Fatalf("0-day (more urgent) sent %d mails, want 2 (1-day + 0-day, no more)", got)
	}

	// T-A1 核心断言：0 天状态第二轮，绝不再发（修复前这里每小时 +1 封）。
	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("0-day second round: %v", err)
	}
	if got := spy.count(); got != 2 {
		t.Fatalf("0-day second round sent again: %d mails, want still 2 (T-A1: daysLeft=0 must not collide with the never-sent sentinel)", got)
	}

	// 标记形态写死：days=0 且 sentAt 非零。
	st := readState(t, ctx, u.ID)
	if st.LastExpiryDaysSent != 0 || st.LastExpirySentAt == 0 {
		t.Fatalf("after 0-day send, want LastExpiryDaysSent=0 with LastExpirySentAt>0, got days=%d sentAt=%d", st.LastExpiryDaysSent, st.LastExpirySentAt)
	}
}

// T-A5 旧兼容行：历史行 days 非零、sentAt 为零（旧版本只写 days）——视为已发，
// 不补发；同紧迫度不重发。
func TestExpiryAlertLegacyRowTreatedAsSent(t *testing.T) {
	ctx := setupAlertTestDB(t)
	setAlertSetting(t, model.SettingKeyCustomerAlertExpiryDays, "3")

	spy := &spySender{}
	u := alertUser(960114, "customer-legacy@example.com", 100)
	day := int64(24 * 3600)

	// 手工造旧版本形态的行：days=2、sentAt=0。当前紧迫度取同桶（2 天）：
	// 兼容策略 = 视为已发，同紧迫度不补发；若当前比记录时更紧迫则按新 episode 重发
	//（与正常 episode 语义一致）。
	subs := []model.UserSubscription{{ID: 17, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: now() + 2*day + 6*3600}}
	if err := db.GetDB().WithContext(ctx).Create(&model.UserAlertState{
		UserID:             u.ID,
		LastExpiryDaysSent: 2,
		LastExpirySentAt:   0,
	}).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("legacy-row check: %v", err)
	}
	if got := spy.count(); got != 0 {
		t.Fatalf("legacy row (days!=0, sentAt==0) must be treated as already-sent, got %d mails", got)
	}
}

// T-A4 续期清案 + 再跌回窗口：允许新 episode；清案必须从 DB 读回验证。
func TestExpiryAlertFiresOncePerEpisode(t *testing.T) {
	ctx := setupAlertTestDB(t)
	setAlertSetting(t, model.SettingKeyCustomerAlertExpiryDays, "3")

	spy := &spySender{}
	u := alertUser(960124, "customer-d2@example.com", 100)
	day := int64(24 * 3600)
	subs := []model.UserSubscription{{ID: 27, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: now() + 2*day}}

	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("first expiry check: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("expiring subscription sent %d mails, want 1", got)
	}
	if strings.Contains(spy.calls[0], "http") || strings.Contains(spy.calls[0], "sk-") {
		t.Fatalf("expiry mail carries sensitive info: %q", spy.calls[0])
	}

	// 同紧迫度再跑：不发。
	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("same-expiry recheck: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("same expiry re-run sent again: %d, want still 1 (防重：同一档位只发一封)", got)
	}

	// 续期（到期时间推远出窗口）：双标记清零且必须落库（T-A4 从 DB 读回验证）。
	subs[0].ExpiresAt = now() + 30*day
	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("renewal check: %v", err)
	}
	st := readState(t, ctx, u.ID)
	if st.LastExpiryDaysSent != 0 || st.LastExpirySentAt != 0 {
		t.Fatalf("renewal must clear BOTH expiry markers in DB, got days=%d sentAt=%d", st.LastExpiryDaysSent, st.LastExpirySentAt)
	}

	// 再次逼近：新 episode 再发。
	subs[0].ExpiresAt = now() + 1*day
	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("re-approach check: %v", err)
	}
	if got := spy.count(); got != 2 {
		t.Fatalf("re-approach sent %d, want 2 (renewal must re-arm the alert)", got)
	}
}

// T-A3（到期维度）：发送失败后 days 与 sentAt 均未落库，下一轮可重试。
func TestExpirySendFailureDoesNotArmDedup(t *testing.T) {
	ctx := setupAlertTestDB(t)
	setAlertSetting(t, model.SettingKeyCustomerAlertExpiryDays, "3")

	spy := &spySender{failAt: map[int]error{0: errors.New("smtp down")}}
	u := alertUser(960134, "customer-exp-fail@example.com", 100)
	day := int64(24 * 3600)
	subs := []model.UserSubscription{{ID: 37, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: now() + 2*day}}

	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("failing expiry send: %v", err)
	}
	var row model.UserAlertState
	if err := db.GetDB().WithContext(ctx).Where("user_id = ?", u.ID).First(&row).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("failed expiry send must not arm any marker (err=%v row=%+v)", err, row)
	}
	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("retry expiry send: %v", err)
	}
	if got := spy.count(); got != 2 {
		t.Fatalf("retry should send (2 attempts), got %d", got)
	}
}

// T-B1（缺陷 B op 级腿）：3 天与 30 天两条活跃订阅，窗口 7 天——邮件必须讲 3 天
// 那条（ID 501），而不是 30 天那条。传入乱序列表，消费端防御选择必须兜底。
func TestExpiryAlertPicksSoonestOfMultipleActiveSubs(t *testing.T) {
	ctx := setupAlertTestDB(t)
	setAlertSetting(t, model.SettingKeyCustomerAlertExpiryDays, "7")

	spy := &spySender{}
	u := alertUser(960144, "customer-multi@example.com", 100)
	day := int64(24 * 3600)
	inBucket := func(days int64) int64 { return now() + days*day + 6*3600 }
	// 乱序：30 天那条在前。
	subs := []model.UserSubscription{
		{ID: 502, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: inBucket(30)},
		{ID: 501, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: inBucket(3)},
	}

	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("multi-sub check: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("multi-sub sent %d mails, want 1", got)
	}
	if !strings.Contains(spy.calls[0], "ID 501") {
		t.Fatalf("mail must name the SOONEST subscription (ID 501, 3 days), got: %q", spy.calls[0])
	}
	if strings.Contains(spy.calls[0], "ID 502") {
		t.Fatalf("mail names the later subscription (ID 502): soonest selection regressed")
	}
}

// T-B2：混入 expired / inactive 订阅不得影响选择；无 active 订阅不发且清标记。
func TestExpiryAlertIgnoresInactiveAndExpiredRows(t *testing.T) {
	ctx := setupAlertTestDB(t)
	setAlertSetting(t, model.SettingKeyCustomerAlertExpiryDays, "7")

	spy := &spySender{}
	u := alertUser(960154, "customer-mixed@example.com", 100)
	day := int64(24 * 3600)
	inBucket := func(days int64) int64 { return now() + days*day + 6*3600 }

	// 先武装一个标记，模拟"上一轮已发"。
	armed := []model.UserSubscription{{ID: 611, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: inBucket(2)}}
	if err := CheckUserExpiry(ctx, u, armed, spy.fn()); err != nil {
		t.Fatalf("arm marker: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("precondition: armed episode sent %d, want 1", got)
	}

	// 混入 expired + cancelled 行（防御输入），3 天那条仍是唯一活跃项。
	mixed := []model.UserSubscription{
		{ID: 612, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: now() - day}, // 已过期
		{ID: 613, UserID: u.ID, Status: "cancelled", ExpiresAt: inBucket(1)},           // 非活跃
		{ID: 611, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: inBucket(2)}, // 唯一活跃
	}
	if err := CheckUserExpiry(ctx, u, mixed, spy.fn()); err != nil {
		t.Fatalf("mixed check: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("mixed list must still resolve to the single active sub without re-send, got %d mails", got)
	}
	st := readState(t, ctx, u.ID)
	if st.LastExpiryDaysSent == 0 && st.LastExpirySentAt == 0 {
		t.Fatalf("single active sub still in window must KEEP the marker (same episode), got cleared")
	}

	// 无活跃订阅（全部过期/取消）：不发，且标记必须清（episode 结案）。
	none := []model.UserSubscription{
		{ID: 612, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: now() - day},
	}
	if err := CheckUserExpiry(ctx, u, none, spy.fn()); err != nil {
		t.Fatalf("no-active check: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("no-active must not send, got %d mails", got)
	}
	st = readState(t, ctx, u.ID)
	if st.LastExpiryDaysSent != 0 || st.LastExpirySentAt != 0 {
		t.Fatalf("no active subs must clear markers in DB, got days=%d sentAt=%d", st.LastExpiryDaysSent, st.LastExpirySentAt)
	}
}

// T-B3：同一到期时间的多条订阅，选择必须稳定（ID tie-break 最小）。
func TestExpiryAlertTieBreakStableLowestID(t *testing.T) {
	ctx := setupAlertTestDB(t)
	setAlertSetting(t, model.SettingKeyCustomerAlertExpiryDays, "7")

	spy := &spySender{}
	u := alertUser(960164, "customer-tie@example.com", 100)
	expires := now() + 3*24*3600 + 6*3600
	// 乱序：大 ID 在前。
	subs := []model.UserSubscription{
		{ID: 622, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: expires},
		{ID: 621, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: expires},
	}
	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("tie check: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("tie sent %d mails, want 1", got)
	}
	if !strings.Contains(spy.calls[0], "ID 621") {
		t.Fatalf("tie must pick the lowest ID (621) deterministically, got: %q", spy.calls[0])
	}
}

// ---- WO-030 缺陷 C：NaN/Inf 双层防御 ----

// T-C1（写入侧）：Setting.Validate 对 NaN/+Inf/-Inf/负数/0/正常值的结果写死。
func TestValidateBalanceThresholdRejectsNonFinite(t *testing.T) {
	cases := []struct {
		value   string
		wantErr bool
	}{
		{"NaN", true},
		{"+Inf", true},
		{"Inf", true},
		{"-Inf", true},
		{"-1", true},
		{"0", false},
		{"1", false},
		{"2.5", false},
		{" 3 ", false}, // trim 后合法
		{"abc", true},
		{"", true},
	}
	for _, tc := range cases {
		st := &model.Setting{Key: model.SettingKeyCustomerAlertBalanceThreshold, Value: tc.value}
		err := st.Validate()
		if tc.wantErr && err == nil {
			t.Errorf("Validate(balance threshold %q) = nil, want error (NaN/Inf must never enter tier math)", tc.value)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("Validate(balance threshold %q) = %v, want nil", tc.value, err)
		}
	}
}

// T-C4（写入侧）：到期天数 key 的 Validate 结果。
func TestValidateExpiryDaysRejectsBadInput(t *testing.T) {
	cases := []struct {
		value   string
		wantErr bool
	}{
		{"abc", true},
		{"-1", true},
		{"0", false},
		{"3", false},
		{"1.5", true}, // 整数 key 不收浮点
	}
	for _, tc := range cases {
		st := &model.Setting{Key: model.SettingKeyCustomerAlertExpiryDays, Value: tc.value}
		err := st.Validate()
		if tc.wantErr && err == nil {
			t.Errorf("Validate(expiry days %q) = nil, want error", tc.value)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("Validate(expiry days %q) = %v, want nil", tc.value, err)
		}
	}
}

// T-C2（运行时侧）：缓存直塞 NaN/Inf/负数，LowBalanceThreshold 必须快速报错。
func TestLowBalanceThresholdRuntimeRejectsNonFinite(t *testing.T) {
	ctx := setupAlertTestDB(t)
	for _, bad := range []string{"NaN", "Inf", "+Inf", "-Inf", "-1"} {
		if err := db.GetDB().WithContext(ctx).Model(&model.Setting{}).
			Where("key = ?", string(model.SettingKeyCustomerAlertBalanceThreshold)).
			Update("value", bad).Error; err != nil {
			t.Fatalf("seed raw bad value %q: %v", bad, err)
		}
		if err := setting.RefreshCache(ctx); err != nil {
			t.Fatalf("refresh cache with %q: %v", bad, err)
		}
		v, err := LowBalanceThreshold()
		if err == nil {
			t.Fatalf("raw %q bypassing write validation must be rejected at runtime, got threshold=%v (NaN comparison leakage or negative accepted)", bad, v)
		}
	}
}

// T-C3（纵深防御）：balanceTier(+Inf) 必须在明确 deadline 内返回且不 panic。
func TestBalanceTierInfiniteThresholdTerminates(t *testing.T) {
	done := make(chan float64, 1)
	go func() { done <- balanceTier(5, math.Inf(1)) }()
	select {
	case tier := <-done:
		if tier != 0 {
			t.Fatalf("balanceTier(+Inf) = %v, want 0 (non-finite thresholds are disabled)", tier)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("balanceTier(+Inf) did not return within 2s: the halving loop is not bounded (P0 hang reproduction)")
	}
	// NaN 与 0 同样必须立即返回。
	if got := balanceTier(5, math.NaN()); got != 0 {
		t.Fatalf("balanceTier(NaN) = %v, want 0", got)
	}
	if got := balanceTier(5, 0); got != 0 {
		t.Fatalf("balanceTier(0) = %v, want 0", got)
	}
}

// T-C5（真实 task 路径）：缓存被直塞 NaN 后，整轮任务对余额维度 no-op 不发信。
func TestTaskPathWithNaNThresholdIsNoOp(t *testing.T) {
	ctx := setupAlertTestDB(t)
	setAlertSetting(t, model.SettingKeyCustomerAlertBalanceThreshold, "NaN")

	spy := &spySender{}
	u := alertUser(960174, "customer-nan@example.com", 0.001)
	if err := CheckUserBalance(ctx, u, spy.fn()); err != nil {
		t.Fatalf("NaN threshold must be swallowed as disabled (return err would retry forever), got: %v", err)
	}
	if got := spy.count(); got != 0 {
		t.Fatalf("NaN threshold sent %d mails, want 0 (a healthy user must not be alerted by broken config)", got)
	}
}

// 发送失败不落防重标记：下一轮重试。
func TestSendFailureDoesNotArmDedup(t *testing.T) {
	ctx := setupAlertTestDB(t)
	setAlertSetting(t, model.SettingKeyCustomerAlertBalanceThreshold, "10")

	spy := &spySender{failAt: map[int]error{0: errors.New("smtp down")}}
	u := alertUser(960105, "customer-e@example.com", 9)

	if err := CheckUserBalance(ctx, u, spy.fn()); err != nil {
		t.Fatalf("failing send: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("want exactly 1 (failed) attempt, got %d", got)
	}
	var row model.UserAlertState
	if err := db.GetDB().WithContext(ctx).Where("user_id = ?", u.ID).First(&row).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("failed send must not create an armed state row (err=%v, row=%+v)", err, row)
	}

	// SMTP 恢复：下一轮成功发出。
	if err := CheckUserBalance(ctx, u, spy.fn()); err != nil {
		t.Fatalf("retry send: %v", err)
	}
	if got := spy.count(); got != 2 {
		t.Fatalf("retry should have sent (2 attempts total), got %d", got)
	}
}

// 阈值关闭（0）时整个维度停用。
func TestDisabledWhenThresholdZero(t *testing.T) {
	ctx := setupAlertTestDB(t)
	setAlertSetting(t, model.SettingKeyCustomerAlertBalanceThreshold, "0")
	setAlertSetting(t, model.SettingKeyCustomerAlertExpiryDays, "0")

	spy := &spySender{}
	u := alertUser(960106, "customer-f@example.com", 0.01)
	subs := []model.UserSubscription{{ID: 8, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: now() + 3600}}

	if err := CheckUserBalance(ctx, u, spy.fn()); err != nil {
		t.Fatalf("balance check disabled: %v", err)
	}
	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("expiry check disabled: %v", err)
	}
	if got := spy.count(); got != 0 {
		t.Fatalf("disabled dimensions sent %d mails, want 0", got)
	}
}

func now() int64 { return time.Now().Unix() }

// ---- WO-032：空列表必须结案清标记 ----

// armExpiryMarker 用一次真实发送武装到期标记（T1/T2/T3 的公共前置）。
func armExpiryMarker(t *testing.T, ctx context.Context, u *model.User) {
	t.Helper()
	setAlertSetting(t, model.SettingKeyCustomerAlertExpiryDays, "3")
	spy := &spySender{}
	day := int64(24 * 3600)
	subs := []model.UserSubscription{{ID: 901, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: now() + 2*day + 6*3600}}
	if err := CheckUserExpiry(ctx, u, subs, spy.fn()); err != nil {
		t.Fatalf("arm marker: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("arm precondition: want 1 mail, got %d", got)
	}
	st := readState(t, ctx, u.ID)
	if st.LastExpiryDaysSent == 0 && st.LastExpirySentAt == 0 {
		t.Fatalf("arm precondition: markers not armed (days=%d sentAt=%d)", st.LastExpiryDaysSent, st.LastExpirySentAt)
	}
}

// T1（生产形态）：先武装标记，再传空切片——标记必须双清且从 DB 读回。
// 修复前形态 = CC 验收复现：got days=2 sentAt≠0，episode 会吞掉下一张订阅。
func TestExpiryAlertEmptyListSettlesMarkers(t *testing.T) {
	ctx := setupAlertTestDB(t)
	u := alertUser(960184, "customer-empty@example.com", 100)
	armExpiryMarker(t, ctx, u)

	if err := CheckUserExpiry(ctx, u, nil, (&spySender{}).fn()); err != nil {
		t.Fatalf("empty-list check (nil): %v", err)
	}
	st := readState(t, ctx, u.ID)
	if st.LastExpiryDaysSent != 0 || st.LastExpirySentAt != 0 {
		t.Fatalf("after empty list, markers must BOTH be 0 in DB, got days=%d sentAt=%d (episode will swallow the next subscription)", st.LastExpiryDaysSent, st.LastExpirySentAt)
	}

	// 空切片（非 nil）同样必须结案。
	armExpiryMarker(t, ctx, u)
	if err := CheckUserExpiry(ctx, u, []model.UserSubscription{}, (&spySender{}).fn()); err != nil {
		t.Fatalf("empty-list check (slice): %v", err)
	}
	st = readState(t, ctx, u.ID)
	if st.LastExpiryDaysSent != 0 || st.LastExpirySentAt != 0 {
		t.Fatalf("after empty slice, markers must BOTH be 0 in DB, got days=%d sentAt=%d", st.LastExpiryDaysSent, st.LastExpirySentAt)
	}
}

// T2（客户后果）：T1 清案后，一张窗口内的新订阅必须能再发（新 episode）。
func TestExpiryAlertNewSubscriptionAfterEmptySettle(t *testing.T) {
	ctx := setupAlertTestDB(t)
	u := alertUser(960194, "customer-rebuy@example.com", 100)
	armExpiryMarker(t, ctx, u)

	if err := CheckUserExpiry(ctx, u, nil, (&spySender{}).fn()); err != nil {
		t.Fatalf("empty-list settle: %v", err)
	}

	spy := &spySender{}
	day := int64(24 * 3600)
	newSubs := []model.UserSubscription{{ID: 902, UserID: u.ID, Status: model.SubStatusActive, ExpiresAt: now() + 2*day + 6*3600}}
	if err := CheckUserExpiry(ctx, u, newSubs, spy.fn()); err != nil {
		t.Fatalf("new-subscription check: %v", err)
	}
	if got := spy.count(); got != 1 {
		t.Fatalf("new subscription after settle must send exactly 1 mail (fresh episode), got %d — old marker swallowed the alert if 0", got)
	}
}

// T4（从未发过的用户）：无 user_alert_states 行，空列表不得凭空建行、不得发信。
func TestExpiryAlertEmptyListNeverCreatesRow(t *testing.T) {
	ctx := setupAlertTestDB(t)
	u := alertUser(960204, "customer-fresh@example.com", 100)
	spy := &spySender{}

	if err := CheckUserExpiry(ctx, u, nil, spy.fn()); err != nil {
		t.Fatalf("empty list for never-alerted user: %v", err)
	}
	if got := spy.count(); got != 0 {
		t.Fatalf("no mail may be sent on empty list, got %d", got)
	}
	var row model.UserAlertState
	if err := db.GetDB().WithContext(ctx).Where("user_id = ?", u.ID).First(&row).Error; err == nil {
		t.Fatalf("empty list must NOT create a user_alert_states row for a never-alerted user, got %+v", row)
	}
}

// T5（维度关闭）：按 WO-032 2.2 产品决定——daysAhead=0 也清标记（否则将来重新
// 打开时旧标记吞新 episode），但同样不建行、不发信。
func TestExpiryAlertDisabledDimensionSettlesMarkers(t *testing.T) {
	ctx := setupAlertTestDB(t)
	u := alertUser(960214, "customer-closed@example.com", 100)
	armExpiryMarker(t, ctx, u)

	setAlertSetting(t, model.SettingKeyCustomerAlertExpiryDays, "0")
	if err := CheckUserExpiry(ctx, u, nil, (&spySender{}).fn()); err != nil {
		t.Fatalf("disabled-dimension check: %v", err)
	}
	st := readState(t, ctx, u.ID)
	if st.LastExpiryDaysSent != 0 || st.LastExpirySentAt != 0 {
		t.Fatalf("disabled dimension must settle markers (re-enabling must not inherit stale episode), got days=%d sentAt=%d", st.LastExpiryDaysSent, st.LastExpirySentAt)
	}

	// 从未发过的用户：维度关闭同样不建行。
	fresh := alertUser(960224, "customer-closed-fresh@example.com", 100)
	if err := CheckUserExpiry(ctx, fresh, nil, (&spySender{}).fn()); err != nil {
		t.Fatalf("disabled dimension, fresh user: %v", err)
	}
	var row model.UserAlertState
	if err := db.GetDB().WithContext(ctx).Where("user_id = ?", fresh.ID).First(&row).Error; err == nil {
		t.Fatalf("disabled dimension must NOT create a row for a never-alerted user, got %+v", row)
	}
}
