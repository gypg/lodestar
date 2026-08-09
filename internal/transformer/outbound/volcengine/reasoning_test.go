package volcengine

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/gypg/lodestar/internal/transformer/model"
)

// reasoningModel is on supportedReasoningEffortModel, so the reasoning block survives
// the whitelist trim in TransformRequest.
const reasoningModel = "doubao-seed-1-8-251228"

// thinkingTypeFromTransformRequest drives the real outbound call site and reads the
// thinking type back off the serialized wire body.
// The second return reports whether the thinking block was emitted at all —
// the Thinking field is omitzero, so an unset type disappears from the request.
func thinkingTypeFromTransformRequest(t *testing.T, modelName, effort string) (string, bool) {
	t.Helper()

	outbound := &ResponseOutbound{}
	httpReq, err := outbound.TransformRequest(
		context.Background(),
		&model.InternalLLMRequest{Model: modelName, ReasoningEffort: effort},
		"https://ark.cn-beijing.volces.com/api/v3",
		"test-key",
	)
	if err != nil {
		t.Fatalf("TransformRequest(effort=%q) err = %v", effort, err)
	}

	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("read body err = %v", err)
	}

	var parsed struct {
		Thinking *struct {
			Type string `json:"type"`
		} `json:"thinking"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal body %s err = %v", string(body), err)
	}

	if parsed.Thinking == nil {
		return "", false
	}
	return parsed.Thinking.Type, true
}

// R-7: xhigh/max previously hit the empty default branch, so no thinking field was set
// at all and the upstream fell back to its own default — a caller requesting the top
// reasoning tier silently got whatever the provider chose.
func TestTransformRequestThinkingTypeExtendedEffort(t *testing.T) {
	cases := []struct {
		name     string
		effort   string
		wantType string
		wantSet  bool
	}{
		{"minimal", "minimal", string(ThinkingTypeDisabled), true},
		{"low", "low", string(ThinkingTypeEnabled), true},
		{"medium", "medium", string(ThinkingTypeEnabled), true},
		{"high", "high", string(ThinkingTypeEnabled), true},
		{"xhigh", "xhigh", string(ThinkingTypeEnabled), true},
		{"max", "max", string(ThinkingTypeEnabled), true},
		{"paddedMixedCaseXHigh", "  XHigh  ", string(ThinkingTypeEnabled), true},
		{"paddedMixedCaseMax", "\tMAX\n", string(ThinkingTypeEnabled), true},
		{"paddedMixedCaseMinimal", " Minimal ", string(ThinkingTypeDisabled), true},
		{"emptyLeavesThinkingUnset", "", "", false},
		{"bogusLeavesThinkingUnset", "turbo", "", false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := thinkingTypeFromTransformRequest(t, reasoningModel, tt.effort)
			if ok != tt.wantSet {
				t.Fatalf("thinking block present = %v, want %v (effort %q)", ok, tt.wantSet, tt.effort)
			}
			if got != tt.wantType {
				t.Fatalf("thinking.type(%q) = %q, want %q", tt.effort, got, tt.wantType)
			}
		})
	}
}

// R-7 regression guard: xhigh/max must land on the same enabled type as high.
// If either regresses to the default branch the thinking block vanishes, which is
// exactly the silent failure mode this fix removes.
func TestTransformRequestExtendedEffortEnablesThinkingLikeHigh(t *testing.T) {
	high, ok := thinkingTypeFromTransformRequest(t, reasoningModel, "high")
	if !ok {
		t.Fatal("thinking block absent for high")
	}

	for _, effort := range []string{"xhigh", "max"} {
		got, ok := thinkingTypeFromTransformRequest(t, reasoningModel, effort)
		if !ok {
			t.Fatalf("thinking block absent for effort %q; the silent drop is back", effort)
		}
		if got != high {
			t.Fatalf("thinking.type(%q) = %q, want %q like high", effort, got, high)
		}
	}
}

// The thinking type is set regardless of the reasoning-effort whitelist, which only
// trims the nested reasoning block. Pinning current behavior so the whitelist and the
// thinking switch cannot silently drift apart.
func TestTransformRequestThinkingTypeSetForNonWhitelistedModel(t *testing.T) {
	got, ok := thinkingTypeFromTransformRequest(t, "doubao-pro-32k", "xhigh")
	if !ok {
		t.Fatal("thinking block absent for non-whitelisted model, want enabled")
	}
	if got != string(ThinkingTypeEnabled) {
		t.Fatalf("thinking.type = %q, want %q", got, ThinkingTypeEnabled)
	}
}
