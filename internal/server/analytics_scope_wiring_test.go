package server

/*
WO-040 ②：analytics 两族端点的多租户接线测试。

- /api/v1/analytics/overview：user 角色的 provider_count 必须为 0（站点规模不外泄）、
  api_key_count 只数自己的 key。若 handler 忘了把 userID 传进 op 层，
  provider_count 会等于渠道缓存里的渠道数 → 红。
- /api/v1/analytics/utilization：user 角色拿到 null（B 族空集门），且响应里
  绝不出现渠道名；viewer（持 channels:read 的只读 staff）拿到真实数据 ——
  钉死"判定用权限不用角色名"。
*/

import (
	"context"
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
	serverauth "github.com/gypg/lodestar/internal/server/auth"
)

// decodeData 解析 resp.Success 的 {code, data} 包装，把 data 解到 out。
func decodeData(body string, out any) error {
	var wrap struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &wrap); err != nil {
		return err
	}
	return json.Unmarshal(wrap.Data, out)
}

// decodeWrapper 只解出 data 字段（原始 JSON）。
func decodeWrapper(body string, out *json.RawMessage) error {
	var wrap struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	return json.Unmarshal([]byte(body), &wrap)
}

func TestAnalyticsMultiTenantWiring(t *testing.T) {
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

	conf.AppConfig.Auth.JWTSecret = "test-jwt-secret-wo040-analytics"

	if err := op.UserInit(); err != nil {
		t.Fatalf("user init: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}

	customer := dbmodel.User{Username: "wo040-cust-" + t.Name(), Password: "x", Role: dbmodel.UserRoleUser, Quota: 5}
	if err := db.GetDB().Create(&customer).Error; err != nil {
		t.Fatalf("create customer: %v", err)
	}
	key := dbmodel.APIKey{UserID: customer.ID, Name: "wo040-cust-key", APIKey: "sk-lodestar-wo040-analytics-cust", Enabled: true}
	if err := db.GetDB().Create(&key).Error; err != nil {
		t.Fatalf("create key: %v", err)
	}
	// GetByKey/ListByUser 走 keyCache，建行后重建缓存。
	if err := ak.RefreshCache(context.Background()); err != nil {
		t.Fatalf("refresh key cache: %v", err)
	}

	viewer := dbmodel.User{Username: "wo040-view-" + t.Name(), Password: "x", Role: dbmodel.UserRoleViewer, Quota: 0}
	if err := db.GetDB().Create(&viewer).Error; err != nil {
		t.Fatalf("create viewer: %v", err)
	}

	// 一个真实渠道：staff 侧 provider_count 应为 1，user 侧必须为 0。
	ch.GetCache().Clear()
	ch.GetCache().Set(955001, dbmodel.Channel{
		ID: 955001, Name: "wo040-secret-channel", Type: 0, Enabled: true,
		BaseUrls: []dbmodel.BaseUrl{{URL: "http://wo040-upstream.invalid"}},
		Keys:     []dbmodel.ChannelKey{{ID: 955011, ChannelID: 955001, Enabled: true, ChannelKey: "sk-wo040"}},
	})
	t.Cleanup(func() { ch.GetCache().Clear() })

	// 一条挂在 customer key 上的中继日志：utilization/breakdown 的渠道维度
	// 数据来自 relay_logs 流量统计（不是渠道配置表），staff 视角据此看到渠道。
	relayLog := dbmodel.RelayLog{
		ID: 1, Time: time.Now().Unix(),
		RequestModelName: "wo040-wiring-model", ActualModelName: "wo040-wiring-model",
		RequestAPIKeyID: int(key.ID),
		ChannelId:       955001, ChannelName: "wo040-secret-channel",
		InputTokens: 10, OutputTokens: 5, TotalAttempts: 1,
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

	// ── user：overview 隔离 ──
	code, body := get(userToken, "/api/v1/analytics/overview?range=7d")
	if code != http.StatusOK {
		t.Fatalf("user overview status = %d, want 200; body=%s", code, body)
	}
	var ov struct {
		ProviderCount int `json:"provider_count"`
		APIKeyCount   int `json:"api_key_count"`
	}
	if err := decodeData(body, &ov); err != nil {
		t.Fatalf("user overview decode: %v; body=%s", err, body)
	}
	if ov.ProviderCount != 0 {
		t.Fatalf("user provider_count = %d, want 0 — site scale must not leak (channel cache holds 1)", ov.ProviderCount)
	}
	if ov.APIKeyCount != 1 {
		t.Fatalf("user api_key_count = %d, want 1 (own key only); customer.ID=%d", ov.APIKeyCount, customer.ID)
	}

	// ── user：utilization 空集门 ──
	code, body = get(userToken, "/api/v1/analytics/utilization?range=7d")
	if code != http.StatusOK {
		t.Fatalf("user utilization status = %d, want 200; body=%s", code, body)
	}
	if strings.Contains(body, "wo040-secret-channel") {
		t.Fatal("user utilization leaked the channel name")
	}
	var data json.RawMessage
	if err := decodeWrapper(body, &data); err != nil {
		t.Fatalf("user utilization decode: %v; body=%s", err, body)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		// resp.Success(c, nil) → data: null，即空集门生效
	} else {
		t.Fatalf("user utilization data = %s, want null (B-family empty gate)", trimmed)
	}

	// ── viewer（持 channels:read 的只读 staff）：全站数据可达 ──
	code, body = get(viewerToken, "/api/v1/analytics/utilization?range=7d")
	if code != http.StatusOK {
		t.Fatalf("viewer utilization status = %d, want 200; body=%s", code, body)
	}
	if !strings.Contains(body, "wo040-secret-channel") {
		t.Fatalf("viewer utilization should include the channel (staff-side view); body=%s", body)
	}

	// ── user：B 族其余代表端点同样空集 ──
	for _, path := range []string{
		"/api/v1/analytics/provider-breakdown?range=7d",
		"/api/v1/analytics/channel-model?range=7d",
		"/api/v1/analytics/latency-models?range=7d",
	} {
		code, body = get(userToken, path)
		if code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200; body=%s", path, code, body)
		}
		if strings.Contains(body, "wo040-secret-channel") {
			t.Fatalf("%s leaked the channel name", path)
		}
		var data json.RawMessage
		if err := decodeWrapper(body, &data); err != nil {
			t.Fatalf("%s decode: %v; body=%s", path, err, body)
		}
		// resp.Success(c, nil) 的 data 带 omitempty：响应体可能没有 data 字段
		// （空串）或显式 data:null，两者都是空集门的合法形态。
		if trimmed := strings.TrimSpace(string(data)); trimmed != "" && trimmed != "null" {
			t.Fatalf("%s data = %q, want empty/null (empty gate)", path, trimmed)
		}
	}
}
