package helper

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	appmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/transformer/outbound"
)

// initGroupProbeLogTestEnv brings up the minimum needed for TestGroupModels to
// reach the logging call sites: a database with the settings table (RelayLogAdd
// reads relay_log_keep_enabled) and the op caches.
func initGroupProbeLogTestEnv(t *testing.T) context.Context {
	t.Helper()

	ctx := context.Background()
	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)

	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	if err := setting.RefreshCache(ctx); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Start from an empty in-memory log cache so assertions only see this test's
	// entries. SetCacheForTest returns a restore func.
	t.Cleanup(relaylog.SetCacheForTest(nil))

	return ctx
}

// cachedTestLogs returns the buffered relay logs produced by this test.
func cachedTestLogs(t *testing.T) []appmodel.RelayLog {
	t.Helper()
	logs, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	defer lock.Unlock()
	out := make([]appmodel.RelayLog, 0, len(logs))
	for _, l := range logs {
		if l.IsTest {
			out = append(out, l)
		}
	}
	return out
}

// TestGroupModelsRecordsSuccessfulProbeAttempt drives TestGroupModels — the
// exported entry point above the recordTestLog call site — rather than calling
// recordTestLog directly. A test that called it directly would guard the
// function's body while leaving the call site deletable, which is exactly the
// gap this file exists to close.
func TestGroupModelsRecordsSuccessfulProbeAttempt(t *testing.T) {
	ctx := initGroupProbeLogTestEnv(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	const channelID = 910001
	channels := map[int]appmodel.Channel{
		channelID: {
			ID:       channelID,
			Name:     "probe-channel-ok",
			Type:     outbound.OutboundTypeOpenAIChat,
			Enabled:  true,
			BaseUrls: []appmodel.BaseUrl{{URL: upstream.URL}},
			Keys:     []appmodel.ChannelKey{{Enabled: true, ChannelKey: "sk-probe"}},
		},
	}
	group := &appmodel.Group{
		Name:         "probe-group",
		EndpointType: appmodel.EndpointTypeAll,
		Items: []appmodel.GroupItem{
			{ID: 1, ChannelID: channelID, ModelName: "gpt-4o-mini", Priority: 1, Weight: 1},
		},
	}

	summary, err := TestGroupModels(ctx, group, channels)
	if err != nil {
		t.Fatalf("TestGroupModels() error = %v", err)
	}
	if !summary.Passed {
		t.Fatalf("TestGroupModels() summary.Passed = false, results = %+v", summary.Results)
	}

	logs := cachedTestLogs(t)
	if len(logs) != 1 {
		t.Fatalf("buffered test logs = %d, want exactly 1: the probe result must reach the log cache", len(logs))
	}
	got := logs[0]

	// Exact expected values, not "non-empty": a log written with the wrong
	// channel or a placeholder model would satisfy a loose assertion.
	if got.ChannelId != channelID {
		t.Fatalf("log ChannelId = %d, want %d", got.ChannelId, channelID)
	}
	if got.RequestModelName != "gpt-4o-mini" {
		t.Fatalf("log RequestModelName = %q, want gpt-4o-mini", got.RequestModelName)
	}
	if got.RequestAPIKeyName != "[test]" {
		t.Fatalf("log RequestAPIKeyName = %q, want [test]", got.RequestAPIKeyName)
	}
	if !got.IsTest {
		t.Fatal("log IsTest = false, want true: probe logs must stay distinguishable from real traffic")
	}
	if got.Error != "" {
		t.Fatalf("log Error = %q, want empty on a passing probe", got.Error)
	}
	// Attempt detail must be carried on the log itself. R-9 made the attempt rows
	// flush alongside the parent log, so an empty Attempts slice here means the
	// per-attempt breakdown is lost for good.
	if got.TotalAttempts != 1 {
		t.Fatalf("log TotalAttempts = %d, want 1", got.TotalAttempts)
	}
	if len(got.Attempts) != 1 {
		t.Fatalf("log Attempts len = %d, want 1", len(got.Attempts))
	}
	if got.Attempts[0].Status != appmodel.AttemptSuccess {
		t.Fatalf("attempt Status = %q, want %q", got.Attempts[0].Status, appmodel.AttemptSuccess)
	}
	if got.Attempts[0].ChannelID != channelID {
		t.Fatalf("attempt ChannelID = %d, want %d", got.Attempts[0].ChannelID, channelID)
	}
	if got.RequestContent == "" {
		t.Fatal("log RequestContent = empty, want the serialised probe request")
	}
}

// TestGroupModelsRecordsRetriedFailureAttempts pins the retry breakdown: a
// failing channel is probed three times and every attempt must be recorded, in
// order. Asserting only "Attempts is non-empty" would not notice a call site
// that dropped all but the last attempt.
func TestGroupModelsRecordsRetriedFailureAttempts(t *testing.T) {
	ctx := initGroupProbeLogTestEnv(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	}))
	defer upstream.Close()

	const channelID = 910002
	channels := map[int]appmodel.Channel{
		channelID: {
			ID:       channelID,
			Name:     "probe-channel-429",
			Type:     outbound.OutboundTypeOpenAIChat,
			Enabled:  true,
			BaseUrls: []appmodel.BaseUrl{{URL: upstream.URL}},
			Keys:     []appmodel.ChannelKey{{Enabled: true, ChannelKey: "sk-probe"}},
		},
	}
	group := &appmodel.Group{
		Name:         "probe-group-failing",
		EndpointType: appmodel.EndpointTypeAll,
		Items: []appmodel.GroupItem{
			{ID: 1, ChannelID: channelID, ModelName: "gpt-4o-mini", Priority: 1, Weight: 1},
		},
	}

	summary, err := TestGroupModels(ctx, group, channels)
	if err != nil {
		t.Fatalf("TestGroupModels() error = %v", err)
	}
	if summary.Passed {
		t.Fatal("TestGroupModels() summary.Passed = true, want false for a 429-only upstream")
	}

	// R-9 ordering guard: attempt rows must not be written here. The parent log is
	// still sitting in the in-memory cache at this point, so any relay_log_attempts
	// row that already exists is an orphan — it references a relay_logs id that has
	// not been (and may never be) persisted. Verified by probe: restoring the eager
	// RelayLogAttemptsAdd call produces 3 attempt rows against 0 parent rows.
	var orphanAttempts int64
	if err := db.GetDB().Model(&appmodel.RelayLogAttempt{}).Count(&orphanAttempts).Error; err != nil {
		t.Fatalf("count relay_log_attempts: %v", err)
	}
	if orphanAttempts != 0 {
		t.Fatalf("relay_log_attempts rows = %d, want 0: attempt detail must flush with the parent log, not ahead of it", orphanAttempts)
	}

	logs := cachedTestLogs(t)
	if len(logs) != 1 {
		t.Fatalf("buffered test logs = %d, want exactly 1", len(logs))
	}
	got := logs[0]

	if got.Error == "" {
		t.Fatal("log Error = empty, want the upstream failure recorded")
	}
	if got.TotalAttempts != 3 {
		t.Fatalf("log TotalAttempts = %d, want 3 (the probe retries three times)", got.TotalAttempts)
	}
	if len(got.Attempts) != 3 {
		t.Fatalf("log Attempts len = %d, want 3", len(got.Attempts))
	}
	for i, attempt := range got.Attempts {
		if attempt.Status != appmodel.AttemptFailed {
			t.Fatalf("attempt[%d] Status = %q, want %q", i, attempt.Status, appmodel.AttemptFailed)
		}
		if attempt.AttemptNum != i+1 {
			t.Fatalf("attempt[%d] AttemptNum = %d, want %d: attempts must be recorded in order", i, attempt.AttemptNum, i+1)
		}
		if attempt.ChannelID != channelID {
			t.Fatalf("attempt[%d] ChannelID = %d, want %d", i, attempt.ChannelID, channelID)
		}
	}
}

// TestGroupModelsRecordsEarlyRejection covers the five early-return call sites,
// which never send an HTTP request. They pass attempts=nil, so a log is still
// expected — an early rejection that logged nothing would be invisible in the UI.
func TestGroupModelsRecordsEarlyRejection(t *testing.T) {
	ctx := initGroupProbeLogTestEnv(t)

	const channelID = 910003
	channels := map[int]appmodel.Channel{
		channelID: {
			ID:       channelID,
			Name:     "probe-channel-disabled",
			Type:     outbound.OutboundTypeOpenAIChat,
			Enabled:  false,
			BaseUrls: []appmodel.BaseUrl{{URL: "http://127.0.0.1:1"}},
			Keys:     []appmodel.ChannelKey{{Enabled: true, ChannelKey: "sk-probe"}},
		},
	}
	group := &appmodel.Group{
		Name:         "probe-group-disabled",
		EndpointType: appmodel.EndpointTypeAll,
		Items: []appmodel.GroupItem{
			{ID: 1, ChannelID: channelID, ModelName: "gpt-4o-mini", Priority: 1, Weight: 1},
		},
	}

	summary, err := TestGroupModels(ctx, group, channels)
	if err != nil {
		t.Fatalf("TestGroupModels() error = %v", err)
	}
	if summary.Passed {
		t.Fatal("TestGroupModels() summary.Passed = true, want false for a disabled channel")
	}

	logs := cachedTestLogs(t)
	if len(logs) != 1 {
		t.Fatalf("buffered test logs = %d, want exactly 1: the early rejection must be logged too", len(logs))
	}
	got := logs[0]
	if got.Error != "channel disabled" {
		t.Fatalf("log Error = %q, want \"channel disabled\"", got.Error)
	}
	if got.ChannelId != channelID {
		t.Fatalf("log ChannelId = %d, want %d", got.ChannelId, channelID)
	}
	if got.TotalAttempts != 0 {
		t.Fatalf("log TotalAttempts = %d, want 0: no upstream request was made", got.TotalAttempts)
	}
}
