package airoute

import (
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/model"
)

// localTestChannel builds a channel the way RefreshCache loads it: Keys and
// BaseUrls populated, Model/CustomModel as comma lists.
func localTestChannel(id int, name string, models string, baseUrls []string, keys []model.ChannelKey) model.Channel {
	ch := model.Channel{
		ID:      id,
		Name:    name,
		Enabled: true,
		Model:   models,
	}
	for _, url := range baseUrls {
		ch.BaseUrls = append(ch.BaseUrls, model.BaseUrl{URL: url})
	}
	ch.Keys = keys
	return ch
}

func localTestKey(enabled bool, secret string) model.ChannelKey {
	return model.ChannelKey{Enabled: enabled, ChannelKey: secret}
}

// TestBuildLocalAIRouteServicesPreferredModel pins the core local-mode promise:
// the analysis runs on the operator's chosen model via the channel that serves
// it, with no hand-filled base_url/api_key anywhere in the flow.
func TestBuildLocalAIRouteServicesPreferredModel(t *testing.T) {
	channels := []model.Channel{
		localTestChannel(1, "other", "gpt-4o", []string{"https://other.example.com"}, []model.ChannelKey{localTestKey(true, "sk-other")}),
		localTestChannel(2, "stepfun", "stepfun-ai/step-3.7-flash", []string{"https://api.stepfun.example/v1"}, []model.ChannelKey{localTestKey(true, "sk-step")}),
	}

	services, err := buildLocalAIRouteServices(channels, "stepfun-ai/step-3.7-flash")
	if err != nil {
		t.Fatalf("buildLocalAIRouteServices() error = %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("got %d services, want 1", len(services))
	}
	svc := services[0]
	if svc.BaseURL != "https://api.stepfun.example/v1" || svc.APIKey != "sk-step" || svc.Model != "stepfun-ai/step-3.7-flash" {
		t.Fatalf("wrong service derived: %+v", svc)
	}
	if !strings.Contains(svc.Name, "stepfun") {
		t.Fatalf("service name %q should carry the channel name", svc.Name)
	}
}

// TestBuildLocalAIRouteServicesPreferredModelMissingChannel: a model no enabled
// channel serves must be an error that names the model — silently swapping in
// another model would run analysis with credentials the operator never chose.
func TestBuildLocalAIRouteServicesPreferredModelMissingChannel(t *testing.T) {
	channels := []model.Channel{
		localTestChannel(1, "other", "gpt-4o", []string{"https://other.example.com"}, []model.ChannelKey{localTestKey(true, "sk-other")}),
	}

	_, err := buildLocalAIRouteServices(channels, "stepfun-ai/step-3.7-flash")
	if err == nil {
		t.Fatal("want an error for a model with no serving channel")
	}
	if !strings.Contains(err.Error(), "stepfun-ai/step-3.7-flash") {
		t.Fatalf("error %q should name the missing model", err.Error())
	}
}

// TestBuildLocalAIRouteServicesAutoPick: with no preferred model the first
// usable model in channel order wins — channels whose credentials are missing
// or whose keys are disabled are skipped, not fatal.
func TestBuildLocalAIRouteServicesAutoPick(t *testing.T) {
	channels := []model.Channel{
		// No base URL: unusable, must be skipped.
		localTestChannel(1, "no-url", "model-a", nil, []model.ChannelKey{localTestKey(true, "sk-a")}),
		// Keys all disabled: unusable, must be skipped.
		localTestChannel(2, "no-keys", "model-b", []string{"https://b.example.com"}, []model.ChannelKey{localTestKey(false, "sk-b")}),
		// First usable one.
		localTestChannel(3, "good", "model-c", []string{"https://c.example.com"}, []model.ChannelKey{localTestKey(true, "sk-c")}),
		// Disabled channel: skipped even though it serves model-d.
		localTestChannel(4, "disabled", "model-d", []string{"https://d.example.com"}, []model.ChannelKey{localTestKey(true, "sk-d")}),
	}
	channels[3].Enabled = false

	services, err := buildLocalAIRouteServices(channels, "")
	if err != nil {
		t.Fatalf("buildLocalAIRouteServices() error = %v", err)
	}
	if len(services) != 1 || services[0].Model != "model-c" {
		t.Fatalf("want the first usable model (model-c), got %+v", services)
	}
}

// TestBuildLocalAIRouteServicesAutoPickNothingUsable: an all-broken channel
// list must produce the explicit "no usable model" error, not an empty service
// that fails later with something cryptic.
func TestBuildLocalAIRouteServicesAutoPickNothingUsable(t *testing.T) {
	channels := []model.Channel{
		localTestChannel(1, "no-keys", "model-a", []string{"https://a.example.com"}, []model.ChannelKey{localTestKey(false, "sk-a")}),
	}

	_, err := buildLocalAIRouteServices(channels, "")
	if err == nil {
		t.Fatal("want an error when no channel is usable")
	}
}

// TestBuildLocalAIRouteServicesModelCaseInsensitive pins that model matching
// against the channel's comma list is case-insensitive, matching how
// collectAIRouteModelInputs treats model names.
func TestBuildLocalAIRouteServicesModelCaseInsensitive(t *testing.T) {
	channels := []model.Channel{
		localTestChannel(1, "c", "StepFun-AI/Step-3.7-Flash", []string{"https://c.example.com"}, []model.ChannelKey{localTestKey(true, "sk")}),
	}

	services, err := buildLocalAIRouteServices(channels, "stepfun-ai/step-3.7-flash")
	if err != nil {
		t.Fatalf("buildLocalAIRouteServices() error = %v", err)
	}
	if services[0].Model != "StepFun-AI/Step-3.7-Flash" {
		t.Fatalf("service model = %q, want the channel's own spelling", services[0].Model)
	}
}
