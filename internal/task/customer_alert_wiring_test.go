package task

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/subscription"
)

// startFakeSMTP 起一个最小 SMTP 会话服务器（只走到 DATA 收尾），返回捕获的正文。
// WO-030 T-B4：CustomerAlertTask 的真实接线测试必须证明"任务真的把邮件发了出去"
// 且内容是最早到期那条——纯函数级测试守不住查询→任务→邮件这条链。
func startFakeSMTP(t *testing.T) (addr string, bodies func() []string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var mu = make(chan []string, 1)
	mu <- nil

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				write := func(s string) { _, _ = conn.Write([]byte(s)) }
				write("220 test ready\r\n")
				scanner := bufio.NewScanner(conn)
				scanner.Buffer(make([]byte, 64*1024), 1024*1024)
				inData := false
				var body strings.Builder
				for scanner.Scan() {
					line := scanner.Text()
					upper := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case inData:
						if line == "." {
							b := <-mu
							mu <- append(b, body.String())
							body.Reset()
							inData = false
							write("250 ok\r\n")
						} else {
							body.WriteString(line + "\n")
						}
					case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
						write("250-test\r\n250 OK\r\n")
					case strings.HasPrefix(upper, "MAIL FROM"), strings.HasPrefix(upper, "RCPT TO"):
						write("250 OK\r\n")
					case strings.HasPrefix(upper, "DATA"):
						inData = true
						write("354 end with .\r\n")
					case strings.HasPrefix(upper, "QUIT"):
						write("221 bye\r\n")
						return
					default:
						write("250 OK\r\n")
					}
				}
			}()
		}
	}()

	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String(), func() []string { b := <-mu; v := append([]string(nil), b...); mu <- b; return v }
}

func setCustomerAlertSetting(t *testing.T, key model.SettingKey, value string) {
	t.Helper()
	if err := setting.SetString(key, value); err != nil {
		t.Fatalf("set setting %s=%s: %v", key, value, err)
	}
}

// T-B4（缺陷 B 接线腿）：真实 CustomerAlertTask → ListActiveUserSubscriptions →
// CheckUserExpiry → SendCustom 全链。同一用户两条活跃订阅（3 天 / 30 天），
// SMTP 未启用时任务静默跳过——所以这里把 SMTP 配置指向假服务器。
func TestCustomerAlertTaskSendsSoonestSubscriptionAlert(t *testing.T) {
	ctx := setupProbeTaskTestEnv(t)

	smtpAddr, bodies := startFakeSMTP(t)
	setCustomerAlertSetting(t, model.SettingKeySMTPEnabled, "true")
	setCustomerAlertSetting(t, model.SettingKeySMTPHost, strings.Split(smtpAddr, ":")[0])
	setCustomerAlertSetting(t, model.SettingKeySMTPPort, strings.Split(smtpAddr, ":")[1])
	setCustomerAlertSetting(t, model.SettingKeySMTPFrom, "alerts@lodestar.test")
	setCustomerAlertSetting(t, model.SettingKeyCustomerAlertEnabled, "true")
	setCustomerAlertSetting(t, model.SettingKeyCustomerAlertExpiryDays, "7")

	// 建用户（直插，绕开 Create 的密码复杂度；本测试只关心 ID/Email/Quota）。
	u := model.User{Username: "customer-b4", Password: "x", Role: model.UserRoleUser,
		Email: "customer-b4@example.com", Quota: 100}
	if err := db.GetDB().WithContext(ctx).Create(&u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// 两条活跃订阅：30 天 + 3 天（先建远的再建近的，ID 顺序与到期时间相反）。
	day := int64(24 * 3600)
	inBucket := func(days int64) int64 { return time.Now().Unix() + days*day + 6*3600 }
	for _, sub := range []model.UserSubscription{
		{UserID: u.ID, PlanID: 1, Status: model.SubStatusActive, ExpiresAt: inBucket(30), StartsAt: time.Now().Unix()},
		{UserID: u.ID, PlanID: 2, Status: model.SubStatusActive, ExpiresAt: inBucket(3), StartsAt: time.Now().Unix()},
	} {
		row := sub
		if err := db.GetDB().WithContext(ctx).Create(&row).Error; err != nil {
			t.Fatalf("seed subscription: %v", err)
		}
	}

	subs, err := subscription.ListActiveUserSubscriptions(u.ID, ctx)
	if err != nil {
		t.Fatalf("ListActiveUserSubscriptions: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("precondition: want 2 active subscriptions, got %d", len(subs))
	}
	if subs[0].ExpiresAt > subs[1].ExpiresAt {
		t.Fatalf("query ordering regressed: expected soonest-first, got %d then %d", subs[0].ExpiresAt, subs[1].ExpiresAt)
	}

	CustomerAlertTask()

	// 任务在 SQLite 下走 EnqueueWrite：若本进程先前的测试已启动串行写器，这里的
	// 执行是异步的（队列未启动时 EnqueueWrite 同步兜底）。轮询等待邮件到达，
	// 两种路径都确定性覆盖。
	deadline := time.Now().Add(5 * time.Second)
	var got []string
	for {
		got = bodies()
		if len(got) >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(got) != 1 {
		t.Fatalf("task must send exactly 1 expiry alert mail, got %d (bodies=%q)", len(got), got)
	}
	if !strings.Contains(got[0], "customer-b4@example.com") {
		t.Fatalf("mail must go to the customer, body=%q", got[0])
	}
	soon := subs[0].ID
	if !strings.Contains(got[0], fmt.Sprintf("ID %d", soon)) {
		t.Fatalf("mail must name the soonest subscription (ID %d), got: %q", soon, got[0])
	}
	later := subs[1].ID
	if strings.Contains(got[0], fmt.Sprintf("ID %d", later)) {
		t.Fatalf("mail names the later subscription (ID %d) — soonest selection regressed", later)
	}
}

// T-B2 查询层腿：ListActiveUserSubscriptions 必须滤掉 expired / cancelled 行，
// 只返回 active 且未过期（M-B3 杀腿：过滤器被删则本测试红）。
func TestListActiveUserSubscriptionsFiltersNonActive(t *testing.T) {
	ctx := setupProbeTaskTestEnv(t)

	u := model.User{Username: "customer-b2q", Password: "x", Role: model.UserRoleUser, Email: "b2q@example.com"}
	if err := db.GetDB().WithContext(ctx).Create(&u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	now := time.Now().Unix()
	day := int64(24 * 3600)
	for _, sub := range []model.UserSubscription{
		{UserID: u.ID, PlanID: 1, Status: model.SubStatusActive, ExpiresAt: now - day, StartsAt: now - 2*day}, // 已过期
		{UserID: u.ID, PlanID: 2, Status: "cancelled", ExpiresAt: now + day, StartsAt: now},                   // 已取消
		{UserID: u.ID, PlanID: 3, Status: model.SubStatusActive, ExpiresAt: now + 3*day, StartsAt: now},       // 活跃
	} {
		row := sub
		if err := db.GetDB().WithContext(ctx).Create(&row).Error; err != nil {
			t.Fatalf("seed subscription: %v", err)
		}
	}

	subs, err := subscription.ListActiveUserSubscriptions(u.ID, ctx)
	if err != nil {
		t.Fatalf("ListActiveUserSubscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("query must return only the single active unexpired subscription, got %d rows: %+v", len(subs), subs)
	}
	if subs[0].PlanID != 3 {
		t.Fatalf("query returned wrong row: want plan 3 (the active one), got %+v", subs[0])
	}
}

// WO-032 T3（接线腿）：生产走 ListActiveUserSubscriptions，过期行在查询层被滤掉，
// 任务传给 CheckUserExpiry 的是空切片——结案必须发生在真实任务路径上，而不是
// 只在纯函数里。场景：窗口内订阅先发一封（武装标记）→ 订阅过期 → 再跑任务 →
// DB 标记双零。轮询等待写队列（WO-030 T-B4 的教训：同进程先前的 task.Init()
// 会把 EnqueueWrite 变成异步）。
func TestCustomerAlertTaskSettlesMarkersWhenSubscriptionExpires(t *testing.T) {
	ctx := setupProbeTaskTestEnv(t)

	smtpAddr, bodies := startFakeSMTP(t)
	setCustomerAlertSetting(t, model.SettingKeySMTPEnabled, "true")
	setCustomerAlertSetting(t, model.SettingKeySMTPHost, strings.Split(smtpAddr, ":")[0])
	setCustomerAlertSetting(t, model.SettingKeySMTPPort, strings.Split(smtpAddr, ":")[1])
	setCustomerAlertSetting(t, model.SettingKeySMTPFrom, "alerts@lodestar.test")
	setCustomerAlertSetting(t, model.SettingKeyCustomerAlertEnabled, "true")
	setCustomerAlertSetting(t, model.SettingKeyCustomerAlertExpiryDays, "7")
	setCustomerAlertSetting(t, model.SettingKeyCustomerAlertBalanceThreshold, "0")

	u := model.User{Username: "customer-t3", Password: "x", Role: model.UserRoleUser,
		Email: "customer-t3@example.com", Quota: 100}
	if err := db.GetDB().WithContext(ctx).Create(&u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	day := int64(24 * 3600)
	sub := model.UserSubscription{UserID: u.ID, PlanID: 9, Status: model.SubStatusActive,
		ExpiresAt: time.Now().Unix() + 2*day + 6*3600, StartsAt: time.Now().Unix()}
	if err := db.GetDB().WithContext(ctx).Create(&sub).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	// 第一轮：窗口内订阅 → 发 1 封武装标记。
	CustomerAlertTask()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if len(bodies()) >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := len(bodies()); got != 1 {
		t.Fatalf("first run must send 1 alert, got %d", got)
	}
	// 等标记落库（异步写队列）。
	waitFor := func(cond func() bool, what string) {
		dl := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(dl) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	var st model.UserAlertState
	waitFor(func() bool {
		if err := db.GetDB().WithContext(ctx).Where("user_id = ?", u.ID).First(&st).Error; err != nil {
			return false
		}
		return st.LastExpiryDaysSent != 0 || st.LastExpirySentAt != 0
	}, "armed expiry marker in DB")

	// 订阅过期（status 仍 active，expires_at 翻过去时——与真实过期翻状态前的形态一致）。
	if err := db.GetDB().WithContext(ctx).Model(&model.UserSubscription{}).
		Where("id = ?", sub.ID).Update("expires_at", time.Now().Unix()-day).Error; err != nil {
		t.Fatalf("expire subscription: %v", err)
	}

	// 第二轮：查询层滤掉过期行 → 空切片 → 结案清标记。
	CustomerAlertTask()
	waitFor(func() bool {
		var row model.UserAlertState
		if err := db.GetDB().WithContext(ctx).Where("user_id = ?", u.ID).First(&row).Error; err != nil {
			return false
		}
		return row.LastExpiryDaysSent == 0 && row.LastExpirySentAt == 0
	}, "markers settled to 0 after subscription expiry")

	if got := len(bodies()); got != 1 {
		t.Fatalf("settle run must not send, total mails = %d, want 1", got)
	}
}
