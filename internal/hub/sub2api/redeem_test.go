package sub2api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// errorEnvelope writes a Sub2API failure envelope. On failure Sub2API puts the
// HTTP status in `code` and the stable identifier in `reason`.
func errorEnvelope(w http.ResponseWriter, status int, reason, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"code":    status,
		"reason":  reason,
		"message": message,
	})
}

// TestRedeemCodeBalancePostsCodeAndConvertsValue locks the request contract
// (POST, JSON body with `code`, Bearer auth) and the conversion of the credited
// `value` field — Sub2API has no `quota` field on this response.
func TestRedeemCodeBalancePostsCodeAndConvertsValue(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		envelope(w, map[string]interface{}{
			"id":     42,
			"code":   "GIFT-CODE-0001",
			"type":   "balance",
			"value":  12.5,
			"status": "used",
		})
	}))
	defer server.Close()

	result, err := (&Adapter{}).RedeemCode(context.Background(), newTestSite(t, server.URL), "GIFT-CODE-0001")
	if err != nil {
		t.Fatalf("RedeemCode: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/redeem" {
		t.Errorf("path = %q, want /api/v1/redeem", gotPath)
	}
	if gotAuth != "Bearer jwt-access-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer jwt-access-token")
	}
	// The code must travel in the JSON body, not the query string or path.
	var sent map[string]interface{}
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("request body %q is not JSON: %v", gotBody, err)
	}
	if sent["code"] != "GIFT-CODE-0001" {
		t.Errorf(`request body code = %v, want "GIFT-CODE-0001"`, sent["code"])
	}

	if !result.Success {
		t.Errorf("Success = false, want true")
	}
	if result.AlreadyUsed {
		t.Errorf("AlreadyUsed = true, want false for a fresh code")
	}
	// 12.5 USD * 500000 = 6250000 quota units.
	if want := 12.5 * quotaPerUSD; result.QuotaAwarded != want {
		t.Errorf("QuotaAwarded = %v, want %v (from `value`, not `quota`)", result.QuotaAwarded, want)
	}
}

// TestRedeemCodeIgnoresQuotaField guards against copying the New-API-family
// shape: Sub2API credits `value`, so a stray `quota` field must not be read.
func TestRedeemCodeIgnoresQuotaField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		envelope(w, map[string]interface{}{
			"type":  "balance",
			"value": 3.0,
			"quota": 999.0, // must be ignored
		})
	}))
	defer server.Close()

	result, err := (&Adapter{}).RedeemCode(context.Background(), newTestSite(t, server.URL), "C")
	if err != nil {
		t.Fatalf("RedeemCode: %v", err)
	}
	if want := 3.0 * quotaPerUSD; result.QuotaAwarded != want {
		t.Errorf("QuotaAwarded = %v, want %v (must read `value`, not `quota`)", result.QuotaAwarded, want)
	}
}

// TestRedeemCodeNonBalanceTypesAwardNoQuota locks the type-dependent meaning of
// `value`: for concurrency it is a slot count and for subscription it is unused,
// so neither may be scaled by the USD rate.
func TestRedeemCodeNonBalanceTypesAwardNoQuota(t *testing.T) {
	cases := []struct {
		name        string
		data        map[string]interface{}
		wantMessage string
	}{
		{
			name:        "concurrency",
			data:        map[string]interface{}{"type": "concurrency", "value": 5.0},
			wantMessage: "Redemption successful: 5 concurrency slots",
		},
		{
			name:        "subscription",
			data:        map[string]interface{}{"type": "subscription", "value": 0.0, "validity_days": 90},
			wantMessage: "Redemption successful: subscription for 90 days",
		},
		{
			// Sub2API substitutes 30 when validity_days is zero.
			name:        "subscription default validity",
			data:        map[string]interface{}{"type": "subscription", "value": 0.0, "validity_days": 0},
			wantMessage: "Redemption successful: subscription for 30 days",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				envelope(w, tc.data)
			}))
			defer server.Close()

			result, err := (&Adapter{}).RedeemCode(context.Background(), newTestSite(t, server.URL), "C")
			if err != nil {
				t.Fatalf("RedeemCode: %v", err)
			}
			if !result.Success {
				t.Errorf("Success = false, want true")
			}
			if result.QuotaAwarded != 0 {
				t.Errorf("QuotaAwarded = %v, want 0 (`value` is not USD for type %q)", result.QuotaAwarded, tc.name)
			}
			if result.Message != tc.wantMessage {
				t.Errorf("Message = %q, want %q", result.Message, tc.wantMessage)
			}
		})
	}
}

// TestRedeemCodeAlreadyUsedIsClassifiedByReason locks classification onto the
// stable `reason` identifier. The envelope's `code` is the HTTP status (409), so
// it cannot distinguish "already used" from other conflicts, and `message` is
// localized.
func TestRedeemCodeAlreadyUsedIsClassifiedByReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errorEnvelope(w, http.StatusConflict, "REDEEM_CODE_USED", "兑换码已被使用")
	}))
	defer server.Close()

	result, err := (&Adapter{}).RedeemCode(context.Background(), newTestSite(t, server.URL), "USED-CODE")
	// A rejected code is a recordable outcome, not a transport error.
	if err != nil {
		t.Fatalf("RedeemCode returned error %v, want a non-nil result with Success=false", err)
	}
	if result == nil {
		t.Fatal("result = nil; the upper layer treats nil as \"redemption unsupported\"")
	}
	if result.Success {
		t.Errorf("Success = true, want false")
	}
	if !result.AlreadyUsed {
		t.Errorf("AlreadyUsed = false, want true for reason %q", "REDEEM_CODE_USED")
	}
	if result.QuotaAwarded != 0 {
		t.Errorf("QuotaAwarded = %v, want 0 for a rejected code", result.QuotaAwarded)
	}
}

// TestRedeemCodeOtherFailuresAreNotAlreadyUsed pins the negative direction: only
// REDEEM_CODE_USED sets AlreadyUsed. Marking an expired or rate-limited code as
// "already used" would tell the operator the credit was consumed when it wasn't.
func TestRedeemCodeOtherFailuresAreNotAlreadyUsed(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reason string
		msg    string
	}{
		{"not found", http.StatusNotFound, "REDEEM_CODE_NOT_FOUND", "redeem code not found"},
		{"expired", http.StatusConflict, "REDEEM_CODE_EXPIRED", "redeem code expired"},
		{"locked", http.StatusConflict, "REDEEM_CODE_LOCKED", "redeem code is being processed"},
		{"rate limited", http.StatusTooManyRequests, "REDEEM_RATE_LIMITED", "too many failed attempts"},
		// Invitation codes are rejected by the redeem endpoint entirely.
		{"invitation rejected", http.StatusBadRequest, "REDEEM_CODE_UNSUPPORTED_TYPE", "invitation codes can only be used during registration"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				errorEnvelope(w, tc.status, tc.reason, tc.msg)
			}))
			defer server.Close()

			result, err := (&Adapter{}).RedeemCode(context.Background(), newTestSite(t, server.URL), "C")
			if err != nil {
				t.Fatalf("RedeemCode returned error %v, want a result", err)
			}
			if result.Success {
				t.Errorf("Success = true, want false for reason %q", tc.reason)
			}
			if result.AlreadyUsed {
				t.Errorf("AlreadyUsed = true for reason %q, want false (only REDEEM_CODE_USED sets it)", tc.reason)
			}
		})
	}
}

// TestRedeemCodeAlreadyUsedIgnoresLocalizedMessage proves classification does not
// fall back to substring matching: a message containing "已使用" with a different
// reason must not be reported as already-used.
func TestRedeemCodeAlreadyUsedIgnoresLocalizedMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errorEnvelope(w, http.StatusConflict, "REDEEM_CODE_EXPIRED", "该兑换码已使用或已过期")
	}))
	defer server.Close()

	result, err := (&Adapter{}).RedeemCode(context.Background(), newTestSite(t, server.URL), "C")
	if err != nil {
		t.Fatalf("RedeemCode: %v", err)
	}
	if result.AlreadyUsed {
		t.Error("AlreadyUsed = true, want false: the reason is REDEEM_CODE_EXPIRED regardless of the message text")
	}
}

// TestRedeemCodeClassifiesEnvelopeLevelFailure covers the second failure branch:
// a site (or a proxy that normalizes status codes) that reports the failure in the
// envelope's `code` while returning HTTP 200. Classification must still work.
func TestRedeemCodeClassifiesEnvelopeLevelFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// HTTP 200 with a non-zero envelope code.
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    http.StatusConflict,
			"reason":  "REDEEM_CODE_USED",
			"message": "redeem code already used",
		})
	}))
	defer server.Close()

	result, err := (&Adapter{}).RedeemCode(context.Background(), newTestSite(t, server.URL), "C")
	if err != nil {
		t.Fatalf("RedeemCode: %v", err)
	}
	if result.Success {
		t.Error("Success = true, want false for a non-zero envelope code")
	}
	if !result.AlreadyUsed {
		t.Error("AlreadyUsed = false, want true: reason must be read from the envelope too")
	}
}

// TestRedeemCodeSurfacesFailureMessage verifies the upstream message reaches the
// result, since the upper layer stores it on the redemption record.
func TestRedeemCodeSurfacesFailureMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errorEnvelope(w, http.StatusNotFound, "REDEEM_CODE_NOT_FOUND", "redeem code not found")
	}))
	defer server.Close()

	result, err := (&Adapter{}).RedeemCode(context.Background(), newTestSite(t, server.URL), "C")
	if err != nil {
		t.Fatalf("RedeemCode: %v", err)
	}
	if result.Message == "" {
		t.Fatal("Message is empty; the redemption record would carry no diagnosis")
	}
	if !containsAll(result.Message, "404", "REDEEM_CODE_NOT_FOUND") {
		t.Errorf("Message = %q, want it to carry the status and upstream body", result.Message)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
