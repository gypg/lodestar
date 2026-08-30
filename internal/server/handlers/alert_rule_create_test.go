package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
)

// AlertRule.Enabled 也是 default:true 裸 bool：创建非启用态的规则会被 create
// 回调吞成 true（规则立即开始评估告警）。三条测试真走 JSON 绑定；
// 断言读 op.AlertRules（rulesCache 失效式缓存：失效后重载自 DB）。

func setupAlertRuleCreateTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := "file:" + testName + "?mode=memory&cache=shared"
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
}

func createAlertRuleViaHandler(t *testing.T, body string) model.AlertRule {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/alert/rule/create", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	createAlertRule(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		Code int             `json:"code"`
		Data model.AlertRule `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode create response failed: %v", err)
	}
	return response.Data
}

func loadAlertRuleRow(t *testing.T, id int) model.AlertRule {
	t.Helper()
	var saved model.AlertRule
	if err := db.GetDB().First(&saved, id).Error; err != nil {
		t.Fatalf("load alert rule %d failed: %v", id, err)
	}
	return saved
}

func TestCreateAlertRulePersistsExplicitlyDisabledRuleAsDisabled(t *testing.T) {
	setupAlertRuleCreateTest(t)

	created := createAlertRuleViaHandler(t, `{"name":"b4-disabled","enabled":false,"condition_type":"cost_threshold","threshold":10,"cooldown_sec":60}`)

	// 响应体必须如实
	if created.Enabled {
		t.Fatalf("create response reports enabled=true; caller asked for false")
	}

	// DB（权威持久层）
	saved := loadAlertRuleRow(t, created.ID)
	if saved.Enabled {
		t.Fatalf("user asked for enabled=false; stored enabled=true")
	}

	// 缓存失效后经 op 读路径再取：拿到的必须是停用态
	rules, err := op.AlertRuleList(context.Background())
	if err != nil {
		t.Fatalf("AlertRules failed: %v", err)
	}
	for _, r := range rules {
		if r.ID == created.ID && r.Enabled {
			t.Fatalf("op read path reports the explicitly disabled rule as enabled")
		}
	}
}

func TestCreateAlertRulePersistsExplicitlyEnabledRuleAsEnabled(t *testing.T) {
	setupAlertRuleCreateTest(t)

	created := createAlertRuleViaHandler(t, `{"name":"b4-enabled","enabled":true,"condition_type":"cost_threshold","threshold":5,"cooldown_sec":60}`)

	saved := loadAlertRuleRow(t, created.ID)
	if !saved.Enabled {
		t.Fatalf("user asked for enabled=true; stored enabled=false")
	}

	rules, err := op.AlertRuleList(context.Background())
	if err != nil {
		t.Fatalf("AlertRules failed: %v", err)
	}
	found := false
	for _, r := range rules {
		if r.ID == created.ID {
			found = true
			if !r.Enabled {
				t.Fatalf("user asked for enabled=true; op read path reports enabled=false")
			}
		}
	}
	if !found {
		t.Fatalf("created rule %d missing from op read path", created.ID)
	}
}

func TestCreateAlertRuleWithoutEnabledFieldDefaultsToEnabled(t *testing.T) {
	setupAlertRuleCreateTest(t)

	// 老客户端的形状：JSON 里没有 "enabled" 键，必须保持默认启用。
	created := createAlertRuleViaHandler(t, `{"name":"b4-missing","condition_type":"cost_threshold","threshold":1,"cooldown_sec":60}`)

	saved := loadAlertRuleRow(t, created.ID)
	if !saved.Enabled {
		t.Fatalf("expected rule without explicit enabled to default to enabled=true, got enabled=false")
	}
}
