package server

/*
WO-040 ⑦-1：landing_ambient_mode 的值域白名单接线测试。

model/setting.go:92 注释声明四个合法值 photo|classic|color4bg|pretext，但
handlers/site_public.go 把 != "color4bg" 一律收敛成 "photo" —— classic 与
pretext 永远到不了前端，home/index.tsx 的 pretext（报刊风落地页）分支不可达。

本测试在生产路由链上（getPublicOverview 无鉴权）断言四个合法值原样透传、
未知值回退 photo。
*/

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/setting"
)

func TestPublicOverviewPassesAllLegalAmbientModes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := getProductionEngine(t)
	if productionEngineErr != nil {
		t.Fatalf("production engine: %v", productionEngineErr)
	}

	dsn := "file:wo040-ambient-wiring?mode=memory&cache=shared"
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conf.AppConfig.Auth.JWTSecret = "test-jwt-secret-wo040-ambient"
	if err := op.UserInit(); err != nil {
		t.Fatalf("user init: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	if err := setting.RefreshCache(nil); err != nil {
		t.Fatalf("refresh settings: %v", err)
	}

	cases := map[string]string{
		"photo":    "photo",
		"classic":  "classic",
		"color4bg": "color4bg",
		"pretext":  "pretext",
		"":         "photo", // 未设置 → 回退
		"bogus":    "photo", // 未知值 → 回退
	}
	for set, want := range cases {
		if err := setting.SetString(model.SettingKeyLandingAmbientMode, set); err != nil {
			t.Fatalf("set %q: %v", set, err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/public/overview", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("set %q: status = %d, want 200", set, rec.Code)
		}
		var wrap struct {
			Data struct {
				LandingAmbientMode string `json:"landing_ambient_mode"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &wrap); err != nil {
			t.Fatalf("set %q: decode: %v; body=%s", set, err, rec.Body.String())
		}
		if wrap.Data.LandingAmbientMode != want {
			t.Fatalf("set %q: landing_ambient_mode = %q, want %q", set, wrap.Data.LandingAmbientMode, want)
		}
	}
}
