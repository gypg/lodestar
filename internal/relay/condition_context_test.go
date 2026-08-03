package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	ak "github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/relay/condition"
)

func newConditionTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("X-Tenant-Id", "acme")
	req.Header.Set("User-Agent", "probe/1.0")
	c.Request = req
	return c
}

// TestBuildConditionContextPopulatesEveryKey guards against a field the
// evaluator knows about being left unset at the call site. An unset field is
// indistinguishable from a genuinely empty value, so a rule like
// `api_key_name not_equals X` would silently match every request.
func TestBuildConditionContextPopulatesEveryKey(t *testing.T) {
	ak.GetCache().Set(42, model.APIKey{ID: 42, Name: "prod-key"})
	t.Cleanup(func() { ak.GetCache().Del(42) })

	ctx := buildConditionContext(newConditionTestContext(), "gpt-4o", 42)

	if ctx.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", ctx.Model, "gpt-4o")
	}
	if ctx.APIKeyID != 42 {
		t.Errorf("APIKeyID = %d, want 42", ctx.APIKeyID)
	}
	if ctx.APIKeyName != "prod-key" {
		t.Errorf("APIKeyName = %q, want %q", ctx.APIKeyName, "prod-key")
	}
	// c.ContentType() strips parameters, so the charset is not part of the value.
	if ctx.ContentType != "application/json" {
		t.Errorf("ContentType = %q, want %q", ctx.ContentType, "application/json")
	}
	if ctx.Hour < 0 || ctx.Hour > 23 {
		t.Errorf("Hour = %d, want 0-23", ctx.Hour)
	}
	// resolveValue lowercases the part after "header_", so the map must be
	// keyed by lowercase names rather than Go's canonical header form.
	if got := ctx.Headers["x-tenant-id"]; got != "acme" {
		t.Errorf("Headers[x-tenant-id] = %q, want %q", got, "acme")
	}
}

func TestBuildConditionContextEvaluatesEveryRuleKey(t *testing.T) {
	ak.GetCache().Set(42, model.APIKey{ID: 42, Name: "prod-key"})
	t.Cleanup(func() { ak.GetCache().Del(42) })

	ctx := buildConditionContext(newConditionTestContext(), "gpt-4o", 42)

	tests := []struct {
		name      string
		condition string
		want      bool
	}{
		{"model", `[{"key":"model","op":"equals","value":"gpt-4o"}]`, true},
		{"api_key_id", `[{"key":"api_key_id","op":"equals","value":"42"}]`, true},
		{"api_key_name", `[{"key":"api_key_name","op":"equals","value":"prod-key"}]`, true},
		// The regression: before the call sites were wired, APIKeyName was always
		// "" and this negated rule matched every request.
		{"api_key_name negated", `[{"key":"api_key_name","op":"not_equals","value":"prod-key"}]`, false},
		{"content_type", `[{"key":"content_type","op":"contains","value":"application/json"}]`, true},
		{"header lowercase", `[{"key":"header_x-tenant-id","op":"equals","value":"acme"}]`, true},
		{"header canonical", `[{"key":"header_X-Tenant-Id","op":"equals","value":"acme"}]`, true},
		{"header prefix op", `[{"key":"header_user-agent","op":"starts_with","value":"probe/"}]`, true},
		{"absent header", `[{"key":"header_x-missing","op":"equals","value":""}]`, true},
		{"unknown key fails closed", `[{"key":"typo_key","op":"equals","value":"x"}]`, false},
		{"all rules must pass", `[{"key":"model","op":"equals","value":"gpt-4o"},{"key":"api_key_name","op":"equals","value":"prod-key"}]`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := condition.Evaluate(tt.condition, ctx)
			if err != nil {
				t.Fatalf("Evaluate returned error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Evaluate(%s) = %v, want %v", tt.condition, got, tt.want)
			}
		})
	}
}

// An API key missing from the cache must leave the name empty rather than
// abort the request; routing falls back to the other keys.
func TestBuildConditionContextToleratesMissingAPIKey(t *testing.T) {
	ctx := buildConditionContext(newConditionTestContext(), "gpt-4o", 999999)
	if ctx.APIKeyName != "" {
		t.Errorf("APIKeyName = %q, want empty for an uncached key", ctx.APIKeyName)
	}
	if ctx.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", ctx.Model, "gpt-4o")
	}
}
