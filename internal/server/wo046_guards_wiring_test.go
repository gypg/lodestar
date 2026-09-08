package server

/*
WO-046：WO-044 四个接线守卫的接线测试（生产路由链）。

这四个实现都是对的，WO-044 验收时的接线变异（删守卫后测试全绿）证明缺的是
守卫测试：
  1. QuotaAdmit 调用点（middleware/auth.go APIKeyAuth）—— MaxCost 在途准入
  2. audit 门（PermAuditRead）—— viewer 不再读操作轨迹
  3. ChangePasswordRateLimit —— 改密爆破限流
  4. 迁移端点 admin-only —— editor 403

每条都测正向（守卫放行合法请求）和反向（守卫挡住非法请求），删守卫必红。
*/

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/billing"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/stats"
	serverauth "github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
)

func initWO046Env(t *testing.T) (*gin.Engine, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := getProductionEngine(t)
	if productionEngineErr != nil {
		t.Fatalf("production engine: %v", productionEngineErr)
	}
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conf.AppConfig.Auth.JWTSecret = "test-jwt-secret-wo046-guards"
	if err := op.UserInit(); err != nil {
		t.Fatalf("user init: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	if err := setting.RefreshCache(nil); err != nil {
		t.Fatalf("refresh settings: %v", err)
	}
	editor := dbmodel.User{Username: "wo046-editor-" + t.Name(), Password: "x", Role: dbmodel.UserRoleEditor, Quota: 0}
	if err := db.GetDB().Create(&editor).Error; err != nil {
		t.Fatal(err)
	}
	viewer := dbmodel.User{Username: "wo046-viewer-" + t.Name(), Password: "x", Role: dbmodel.UserRoleViewer, Quota: 0}
	if err := db.GetDB().Create(&viewer).Error; err != nil {
		t.Fatal(err)
	}
	editorToken, _, err := serverauth.GenerateJWTToken(60, editor.ID, dbmodel.UserRoleEditor)
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, _, err := serverauth.GenerateJWTToken(60, viewer.ID, dbmodel.UserRoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	return engine, editorToken, viewerToken
}

func wo046Request(engine *gin.Engine, method, path, token, body string) (int, string) {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// ── 守卫 2：audit 门（PermAuditRead）──
// 正向：editor（持 audit:read）200；反向：viewer（仅 logs:read）必须 403。
// 若把 PermAuditRead 换回 PermLogsRead，viewer 会拿到 200 → 红。
func TestWO046AuditGateViewerDenied(t *testing.T) {
	engine, editorToken, viewerToken := initWO046Env(t)
	code, body := wo046Request(engine, http.MethodGet, "/api/v1/audit/list", editorToken, "")
	if code != http.StatusOK {
		t.Fatalf("editor /audit/list = %d %s, want 200 (editor holds audit:read)", code, body)
	}
	code, body = wo046Request(engine, http.MethodGet, "/api/v1/audit/list", viewerToken, "")
	if code != http.StatusForbidden {
		t.Fatalf("viewer /audit/list = %d, want 403 — viewer holds logs:read but NOT audit:read; 200 means the gate was reverted to logs:read", code)
	}
}

// ── 守卫 4：迁移端点 admin-only ──
// 正向：admin 过角色门（非 403 的任何业务层错误都算通过）；反向：editor 403。
// 删掉 admin-only 判定后 editor 会拿到业务层错误 → 非 403 → 红。
func TestWO046MigrationAdminOnly(t *testing.T) {
	engine, editorToken, _ := initWO046Env(t)
	admin := dbmodel.User{Username: "wo046-admin-" + t.Name(), Password: "x", Role: dbmodel.UserRoleAdmin, Quota: 0}
	if err := db.GetDB().Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := serverauth.GenerateJWTToken(60, admin.ID, dbmodel.UserRoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	// 畸形 JSON 判据是角色门：admin 不该拿 403，editor 必须拿 403。
	acode, _ := wo046Request(engine, http.MethodPost, "/api/v1/setting/database/test", adminToken, "{")
	if acode == http.StatusForbidden {
		t.Fatalf("admin /setting/database/test = 403 — admin must pass the role gate (any business-layer error is fine)")
	}
	ecode, ebody := wo046Request(engine, http.MethodPost, "/api/v1/setting/database/test", editorToken, "{")
	if ecode != http.StatusForbidden {
		t.Fatalf("editor /setting/database/test = %d %s, want 403 — migration is admin-only; non-403 means the gate was deleted", ecode, ebody)
	}
	mcode, _ := wo046Request(engine, http.MethodPost, "/api/v1/setting/database/migrate", editorToken, "{")
	if mcode != http.StatusForbidden {
		t.Fatalf("editor /setting/database/migrate = %d, want 403", mcode)
	}
}

// ── 守卫 3：改密限流 ──
// 反向主体：错旧密码连打到阈值之上必须 429；摘掉 ChangePasswordRateLimit
// 后失败不计数 → 永不 429 → 红。成功路径在独立测试（限流计数按 IP 命名空间，
// httptest 同引擎内共享计数）。
func TestWO046ChangePasswordRateLimit(t *testing.T) {
	engine, _, _ := initWO046Env(t)
	u := dbmodel.User{Username: "wo046-chpw-" + t.Name(), Password: "old-password-wo046", Role: dbmodel.UserRoleUser, Quota: 1}
	if err := u.HashPassword(); err != nil {
		t.Fatal(err)
	}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := serverauth.GenerateJWTToken(60, u.ID, u.Role)
	if err != nil {
		t.Fatal(err)
	}
	// 失败计数按 chpw:<ClientIP> 命名空间，httptest 的 ClientIP 恒为
	// 192.0.2.1（gin 对空 RemoteAddr 的合成值）→ key 是 "chpw:192.0.2.1"。
	// 前后都清一次：别的测试的连打不能挡住本测试的合法请求（前置清），
	// 本测试的连打也不能串给后续（收尾清）。
	const chpwKey = "chpw:192.0.2.1"
	middleware.ClearLoginFailures(chpwKey)
	t.Cleanup(func() { middleware.ClearLoginFailures(chpwKey) })
	hit429 := false
	for i := 0; i < 30; i++ {
		code, _ := wo046Request(engine, http.MethodPost, "/api/v1/user/change-password", token,
			`{"old_password":"wrong-password-xxx","new_password":"new-password-012345"}`)
		if code == http.StatusTooManyRequests {
			hit429 = true
			break
		}
		if code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: wrong old password = %d, want 401 until throttled", i, code)
		}
	}
	if !hit429 {
		t.Fatal("30 wrong-password attempts never got 429 — change-password rate limit is not wired")
	}
}

// 守卫 3 正向：正确改密能成功（限流不误伤合法第一击）。
func TestWO046ChangePasswordSucceeds(t *testing.T) {
	engine, _, _ := initWO046Env(t)
	u := dbmodel.User{Username: "wo046-chpw-ok-" + t.Name(), Password: "old-password-wo046", Role: dbmodel.UserRoleUser, Quota: 1}
	if err := u.HashPassword(); err != nil {
		t.Fatal(err)
	}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := serverauth.GenerateJWTToken(60, u.ID, u.Role)
	if err != nil {
		t.Fatal(err)
	}
	// 同一 192.0.2.1 命名空间：前置清计数，别的测试的连打不得挡住合法第一击。
	middleware.ClearLoginFailures("chpw:192.0.2.1")
	code, body := wo046Request(engine, http.MethodPost, "/api/v1/user/change-password", token,
		`{"old_password":"old-password-wo046","new_password":"new-password-012345"}`)
	if code != http.StatusOK {
		t.Fatalf("correct change-password = %d %s, want 200 — the rate limit must not block the first legitimate attempt", code, body)
	}
}

// WO-047 走查 B-1：短新密码的拒绝形态必须是 400 + i18n key，不是 500「数据库失败」。
// 强度闸本体（WO-045 顺手 7）只在 op 层接过，handler 没识别 ErrBootstrapCredentials →
// 落 500。该 handler 的测试此前只有 op 层（TestChangePasswordEnforcesStrength）——
// 又一例「测试守错了地方」。
func TestWO047ChangePasswordWeakNewPasswordIs400(t *testing.T) {
	engine, _, _ := initWO046Env(t)
	u := dbmodel.User{Username: "wo047-weak-" + t.Name(), Password: "old-password-wo047", Role: dbmodel.UserRoleUser, Quota: 1}
	if err := u.HashPassword(); err != nil {
		t.Fatal(err)
	}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	token, _, err := serverauth.GenerateJWTToken(60, u.ID, u.Role)
	if err != nil {
		t.Fatal(err)
	}
	middleware.ClearLoginFailures("chpw:192.0.2.1")
	t.Cleanup(func() { middleware.ClearLoginFailures("chpw:192.0.2.1") })
	code, body := wo046Request(engine, http.MethodPost, "/api/v1/user/change-password", token,
		`{"old_password":"old-password-wo047","new_password":"short12"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("weak new password = %d %s, want 400 — a validation rejection must not surface as a 500 database failure", code, body)
	}
	if !strings.Contains(body, "passwordTooWeak") && !strings.Contains(body, "at least 12") {
		t.Fatalf("400 body must carry the passwordTooWeak message key or the reason, got: %s", body)
	}
	// 对照：正确旧密码 + 合法新密码仍成功（上面分支不误伤）。
	code2, body2 := wo046Request(engine, http.MethodPost, "/api/v1/user/change-password", token,
		`{"old_password":"old-password-wo047","new_password":"new-password-012345"}`)
	if code2 != http.StatusOK {
		t.Fatalf("valid change after weak rejection = %d %s, want 200", code2, body2)
	}
}

// ── 守卫 1：QuotaAdmit 调用点 + MaxCost 静态检查 ──
// 走 /v1/models（APIKeyAuth 生产链）。两道闸必须分别打各自的独有行为，
// 否则删掉任一道另一道都兜住（变异存活）：
//
//	静态腿：used == MaxCost 且零在途 → "reached the max cost"（静态专属文案，
//	        QuotaAdmit 对 headroom<=0 的文案是 "reserved by in-flight"）。
//	在途腿：零已结算 + 测试侧直接持 2 个 in-flight 槽位（knob 种子 0.5，
//	        2×0.5 >= MaxCost 1.0）→ "reserved by in-flight"（QuotaAdmit 专属）。
func TestWO046QuotaAdmitWiring(t *testing.T) {
	engine, _, _ := initWO046Env(t)
	owner := dbmodel.User{Username: "wo046-quota-owner-" + t.Name(), Password: "x", Role: dbmodel.UserRoleUser, Quota: 5}
	if err := db.GetDB().Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	key := dbmodel.APIKey{UserID: owner.ID, Name: "wo046-quota-key", APIKey: "sk-lodestar-wo046-quota", Enabled: true, MaxCost: 1.0}
	if err := db.GetDB().Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	if err := apikey.RefreshCache(nil); err != nil {
		t.Fatal(err)
	}
	// 无已结算成本、零在途：第一击必须放行。
	code, body := wo046Request(engine, http.MethodGet, "/v1/models", "sk-lodestar-wo046-quota", "")
	if code != http.StatusOK {
		t.Fatalf("first request with zero used cost = %d %s, want 200 — headroom>0 must admit", code, body)
	}

	// 在途腿：测试侧直接持 2 个槽位（等同两个并发在途请求未释放）。
	// middleware 删掉 QuotaAdmit 调用后这里不设阻 → 请求 200 → 测试红。
	release1, ok1 := billing.QuotaAdmit(int(key.ID), 1.0, 0)
	release2, ok2 := billing.QuotaAdmit(int(key.ID), 1.0, 0)
	if !ok1 || !ok2 {
		t.Fatalf("holding slots: ok1=%v ok2=%v — expected both admitted with inflight*est(0.5) < headroom(1.0)", ok1, ok2)
	}
	t.Cleanup(func() { release1(); release2() })
	code, body = wo046Request(engine, http.MethodGet, "/v1/models", "sk-lodestar-wo046-quota", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("request with 2 held in-flight slots = %d %s, want 401 — the QuotaAdmit in-flight leg is not wired", code, body)
	}
	if !strings.Contains(body, "in-flight") {
		t.Fatalf("in-flight leg rejected with %d %s — expected the reserved-by-in-flight message, not the static check (this masks which gate fired)", code, body)
	}
	release1()
	release2()

	// 静态腿：结算 1.0 = MaxCost、零在途 → "reached the max cost"。
	// middleware 删掉静态检查后 QuotaAdmit 的 headroom<=0 兜住 401，但文案
	// 是 in-flight 的 → 断言专属文案才能把静态腿的删除变红。
	if err := stats.APIKeyUpdate(int(key.ID), dbmodel.StatsMetrics{InputCost: 1.0}); err != nil {
		t.Fatal(err)
	}
	code, body = wo046Request(engine, http.MethodGet, "/v1/models", "sk-lodestar-wo046-quota", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("exhausted key = %d %s, want 401 — MaxCost admission is not wired in APIKeyAuth", code, body)
	}
	if !strings.Contains(body, "reached the max cost") {
		t.Fatalf("static leg rejected with %d %s — expected 'reached the max cost'; the in-flight message here means the static check was deleted and QuotaAdmit is masking it", code, body)
	}
}
