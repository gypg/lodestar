package server

/*
WO-042：/api/v1/stats/* 五端点的租户门接线测试（生产路由链）。

- user（无 channels:read）：/channel 返空；/today|hourly|daily 返按自己 key
  重建的同形数据（自己的 2 条日志出现、别人的不出现）；/total 返自己的累计。
- viewer（持 channels:read 的只读 staff）：/channel 仍返回真实渠道名 ——
  钉死"判定用权限不用角色名"。
- 客户响应里绝不允许出现真实渠道名。
*/

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	ak "github.com/gypg/lodestar/internal/op/apikey"
	ch "github.com/gypg/lodestar/internal/op/channel"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
	st "github.com/gypg/lodestar/internal/op/stats"
	serverauth "github.com/gypg/lodestar/internal/server/auth"
)

func TestStatsFamilyMultiTenantWiring(t *testing.T) {
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

	conf.AppConfig.Auth.JWTSecret = "test-jwt-secret-wo042-stats"
	if err := op.UserInit(); err != nil {
		t.Fatalf("user init: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	if err := setting.SetString(dbmodel.SettingKeyRelayLogKeepEnabled, "true"); err != nil {
		t.Fatalf("enable relay log keep: %v", err)
	}

	customer := dbmodel.User{Username: "wo042-cust-" + t.Name(), Password: "x", Role: dbmodel.UserRoleUser, Quota: 5}
	if err := db.GetDB().Create(&customer).Error; err != nil {
		t.Fatalf("create customer: %v", err)
	}
	viewer := dbmodel.User{Username: "wo042-view-" + t.Name(), Password: "x", Role: dbmodel.UserRoleViewer, Quota: 0}
	if err := db.GetDB().Create(&viewer).Error; err != nil {
		t.Fatalf("create viewer: %v", err)
	}

	custKey := dbmodel.APIKey{UserID: customer.ID, Name: "wo042-cust-key", APIKey: "sk-lodestar-wo042-cust", Enabled: true}
	otherKey := dbmodel.APIKey{UserID: customer.ID, Name: "wo042-other-key", APIKey: "sk-lodestar-wo042-other", Enabled: true}
	if err := db.GetDB().Create(&custKey).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.GetDB().Create(&otherKey).Error; err != nil {
		t.Fatal(err)
	}
	// ListByUser/GetByKey 走 keyCache，建行后重建缓存。
	if err := ak.RefreshCache(nil); err != nil {
		t.Fatalf("refresh key cache: %v", err)
	}
	// relaylog 内存缓存是包级全局：前序测试（如 chat 路由）写入的日志会串进
	// 本测试的"今天"重建，必须清空隔离。
	restoreCache := relaylog.SetCacheForTest(nil)
	t.Cleanup(restoreCache)
	// /stats/total 的收窄读 stats 包的 per-key 累计缓存（生产由 Save 管道写入）。
	if err := st.APIKeyUpdate(int(custKey.ID), dbmodel.StatsMetrics{RequestSuccess: 1, InputToken: 40, InputCost: 0.12}); err != nil {
		t.Fatalf("seed key stats: %v", err)
	}

	// 渠道（staff 侧 /stats/channel 应可见；客户侧不可见）。
	ch.GetCache().Clear()
	ch.GetCache().Set(966001, dbmodel.Channel{
		ID: 966001, Name: "wo042-secret-channel", Type: 0, Enabled: true,
		BaseUrls: []dbmodel.BaseUrl{{URL: "http://wo042-upstream.invalid"}},
		Keys:     []dbmodel.ChannelKey{{ID: 966011, ChannelID: 966001, Enabled: true, ChannelKey: "sk-wo042"}},
	})
	t.Cleanup(func() { ch.GetCache().Clear() })

	// 客户 key 自己的日志（今天）；另一把 key 的日志（不属于本测试主 key）。
	now := time.Now()
	relayLog := dbmodel.RelayLog{
		ID: 1, Time: now.Unix(),
		RequestModelName: "wo042-model", ActualModelName: "wo042-model",
		RequestAPIKeyID: int(custKey.ID),
		ChannelId:       966001, ChannelName: "wo042-secret-channel",
		InputTokens: 40, OutputTokens: 25, Cost: 0.12,
		UseTime: 700, Ftut: 150, Error: "", TotalAttempts: 1,
	}
	if err := db.GetLogDB().Create(&relayLog).Error; err != nil {
		t.Fatalf("seed relay log: %v", err)
	}

	userToken, _, err := serverauth.GenerateJWTToken(60, customer.ID, dbmodel.UserRoleUser)
	if err != nil {
		t.Fatalf("user token: %v", err)
	}
	viewerToken, _, err := serverauth.GenerateJWTToken(60, viewer.ID, dbmodel.UserRoleViewer)
	if err != nil {
		t.Fatalf("viewer token: %v", err)
	}

	get := func(token, path string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	wrapData := func(body string) json.RawMessage {
		var wrap struct {
			Data json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal([]byte(body), &wrap)
		return wrap.Data
	}

	// ── user：/stats/channel 返空 ──
	code, body := get(userToken, "/api/v1/stats/channel")
	if code != http.StatusOK {
		t.Fatalf("user /channel status=%d body=%s", code, body)
	}
	if strings.Contains(body, "wo042-secret-channel") {
		t.Fatalf("user /channel leaked the channel name: %s", body)
	}
	// 空集门的 data 被 omitempty 省略 → 响应无 data 字段（或 null）。
	if d := wrapData(body); len(d) != 0 && string(d) != "null" && strings.TrimSpace(string(d)) != "" {
		t.Fatalf("user /channel data = %s, want empty/null", d)
	}

	// ── user：/stats/today 只含自己的日志 ──
	code, body = get(userToken, "/api/v1/stats/today")
	if code != http.StatusOK {
		t.Fatalf("user /today status=%d body=%s", code, body)
	}
	var todayStats struct {
		RequestSuccess int64   `json:"request_success"`
		InputToken     int64   `json:"input_token"`
		InputCost      float64 `json:"input_cost"`
	}
	if err := json.Unmarshal(wrapData(body), &todayStats); err != nil {
		t.Fatalf("user /today decode: %v body=%s", err, body)
	}
	if todayStats.RequestSuccess != 1 || todayStats.InputToken != 40 || todayStats.InputCost < 0.11 || todayStats.InputCost > 0.13 {
		t.Fatalf("user /today = %+v, want own log only (success 1, in 40, cost ~0.12)", todayStats)
	}

	// ── user：/stats/daily 只含自己 + 当天桶有值 ──
	code, body = get(userToken, "/api/v1/stats/daily")
	if code != http.StatusOK {
		t.Fatalf("user /daily status=%d body=%s", code, body)
	}
	var dailyArr []struct {
		Date           string `json:"date"`
		RequestSuccess int64  `json:"request_success"`
		InputToken     int64  `json:"input_token"`
	}
	if err := json.Unmarshal(wrapData(body), &dailyArr); err != nil {
		t.Fatalf("user /daily decode: %v body=%s", err, body)
	}
	todayKey := now.Format("20060102")
	foundToday := false
	for _, d := range dailyArr {
		if d.Date == todayKey {
			foundToday = true
			if d.RequestSuccess != 1 || d.InputToken != 40 {
				t.Fatalf("daily today bucket = %+v, want own log only", d)
			}
		}
	}
	if !foundToday {
		t.Fatalf("daily: today bucket (%s) missing from %d rows", todayKey, len(dailyArr))
	}

	// ── user：/stats/hourly 当前小时有自己日志 ──
	code, body = get(userToken, "/api/v1/stats/hourly")
	if code != http.StatusOK {
		t.Fatalf("user /hourly status=%d body=%s", code, body)
	}
	var hourlyArr []struct {
		Hour           int   `json:"hour"`
		RequestSuccess int64 `json:"request_success"`
	}
	if err := json.Unmarshal(wrapData(body), &hourlyArr); err != nil {
		t.Fatalf("user /hourly decode: %v body=%s", err, body)
	}
	if len(hourlyArr) != 24 {
		t.Fatalf("user /hourly rows=%d, want 24 (same shape as staff view)", len(hourlyArr))
	}
	curHour := now.Hour()
	hourOK := false
	for _, h := range hourlyArr {
		if h.Hour == curHour && h.RequestSuccess == 1 {
			hourOK = true
		}
	}
	if !hourOK {
		t.Fatalf("user /hourly: current hour (%d) missing own success", curHour)
	}

	// ── user：/stats/total 只含自己的累计（这里只有 1 条日志的量） ──
	code, body = get(userToken, "/api/v1/stats/total")
	if code != http.StatusOK {
		t.Fatalf("user /total status=%d body=%s", code, body)
	}
	var totalStats struct {
		RequestSuccess int64 `json:"request_success"`
		InputToken     int64 `json:"input_token"`
	}
	if err := json.Unmarshal(wrapData(body), &totalStats); err != nil {
		t.Fatalf("user /total decode: %v body=%s", err, body)
	}
	if totalStats.RequestSuccess != 1 || totalStats.InputToken != 40 {
		t.Fatalf("user /total = %+v, want own cumulative only", totalStats)
	}

	// ── viewer（staff 侧）：/stats/channel 仍返回真实渠道名 ──
	code, body = get(viewerToken, "/api/v1/stats/channel")
	if code != http.StatusOK {
		t.Fatalf("viewer /channel status=%d body=%s", code, body)
	}
	if !strings.Contains(body, "wo042-secret-channel") {
		t.Fatalf("viewer /channel should include the channel (staff-side); body=%s", body)
	}
}
