package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	appmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/transformer/outbound"
)

// newProbeOKUpstream serves a well-formed chat completion so the probe passes.
func newProbeOKUpstream(t *testing.T, model string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-agg","object":"chat.completion","created":1,"model":"` + model +
			`","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// newProbeStatusUpstream always answers with the given status code.
func newProbeStatusUpstream(t *testing.T, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"message":"model not found"}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func openAIProbeChannel(id int, name, baseURL string) appmodel.Channel {
	return appmodel.Channel{
		ID:       id,
		Name:     name,
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []appmodel.BaseUrl{{URL: baseURL}},
		Keys:     []appmodel.ChannelKey{{Enabled: true, ChannelKey: "sk-probe"}},
	}
}

// TestGroupModelsMixedResultsDoNotReportPassed pins the aggregation rule that
// was actually broken in production: one healthy item used to set
// summary.Passed = true for the whole group, so a group whose second channel
// answered 404 was shown as PASS in the UI even though the server log said
// `passed=false status=404 message=upstream error: 404` for that item.
//
// Two items, one healthy and one 404, is the minimum shape that distinguishes
// all-passed from any-passed. A single-item group (which is all the other tests
// in this package use) cannot tell the two rules apart.
func TestGroupModelsMixedResultsDoNotReportPassed(t *testing.T) {
	ctx := initGroupProbeLogTestEnv(t)

	okUpstream := newProbeOKUpstream(t, "good-model")
	badUpstream := newProbeStatusUpstream(t, http.StatusNotFound)

	const okChannelID, badChannelID = 920001, 920002
	channels := map[int]appmodel.Channel{
		okChannelID:  openAIProbeChannel(okChannelID, "probe-ok", okUpstream.URL),
		badChannelID: openAIProbeChannel(badChannelID, "probe-404", badUpstream.URL),
	}
	group := &appmodel.Group{
		Name:         "mixed-group",
		EndpointType: appmodel.EndpointTypeAll,
		Items: []appmodel.GroupItem{
			{ID: 1, ChannelID: okChannelID, ModelName: "good-model", Priority: 1, Weight: 1},
			{ID: 2, ChannelID: badChannelID, ModelName: "bad-model", Priority: 2, Weight: 1},
		},
	}

	summary, err := TestGroupModels(ctx, group, channels)
	if err != nil {
		t.Fatalf("TestGroupModels() error = %v", err)
	}

	if summary.Passed {
		t.Fatalf("summary.Passed = true, want false: item 2 returned 404, results = %+v", summary.Results)
	}
	if summary.Completed != 2 || summary.Total != 2 {
		t.Fatalf("summary completed/total = %d/%d, want 2/2", summary.Completed, summary.Total)
	}

	// Per-item detail must survive; collapsing the group verdict is not allowed
	// to flatten the individual results the UI lists.
	byItem := make(map[int]GroupModelTestResult, len(summary.Results))
	for _, result := range summary.Results {
		byItem[result.ItemID] = result
	}
	if got := byItem[1]; !got.Passed || got.StatusCode != http.StatusOK {
		t.Fatalf("item 1 = {passed:%v status:%d}, want {true 200}", got.Passed, got.StatusCode)
	}
	if got := byItem[2]; got.Passed || got.StatusCode != http.StatusNotFound {
		t.Fatalf("item 2 = {passed:%v status:%d}, want {false 404}", got.Passed, got.StatusCode)
	}
}

// TestGroupModelsAllPassedReportsPassed is the paired positive control. Without
// it, `summary.Passed = false` unconditionally would satisfy the test above.
func TestGroupModelsAllPassedReportsPassed(t *testing.T) {
	ctx := initGroupProbeLogTestEnv(t)

	first := newProbeOKUpstream(t, "model-a")
	second := newProbeOKUpstream(t, "model-b")

	const firstID, secondID = 921001, 921002
	channels := map[int]appmodel.Channel{
		firstID:  openAIProbeChannel(firstID, "probe-a", first.URL),
		secondID: openAIProbeChannel(secondID, "probe-b", second.URL),
	}
	group := &appmodel.Group{
		Name:         "all-good-group",
		EndpointType: appmodel.EndpointTypeAll,
		Items: []appmodel.GroupItem{
			{ID: 1, ChannelID: firstID, ModelName: "model-a", Priority: 1, Weight: 1},
			{ID: 2, ChannelID: secondID, ModelName: "model-b", Priority: 2, Weight: 1},
		},
	}

	summary, err := TestGroupModels(ctx, group, channels)
	if err != nil {
		t.Fatalf("TestGroupModels() error = %v", err)
	}
	if !summary.Passed {
		t.Fatalf("summary.Passed = false, want true: both items healthy, results = %+v", summary.Results)
	}
}

// TestAllGroupTestResultsPassedTreatsEmptyAsFailed guards the empty-slice case
// directly. "Nothing was probed" must not read as "everything passed", which is
// what a bare `for` loop over an empty slice would produce.
func TestAllGroupTestResultsPassedTreatsEmptyAsFailed(t *testing.T) {
	if allGroupTestResultsPassed(nil) {
		t.Fatal("allGroupTestResultsPassed(nil) = true, want false")
	}
	if allGroupTestResultsPassed([]GroupModelTestResult{}) {
		t.Fatal("allGroupTestResultsPassed([]) = true, want false")
	}
	if !allGroupTestResultsPassed([]GroupModelTestResult{{Passed: true}, {Passed: true}}) {
		t.Fatal("allGroupTestResultsPassed(all true) = false, want true")
	}
	if allGroupTestResultsPassed([]GroupModelTestResult{{Passed: true}, {Passed: false}}) {
		t.Fatal("allGroupTestResultsPassed(one false) = true, want false")
	}
	// Order must not matter: a leading failure has to sink the verdict too.
	if allGroupTestResultsPassed([]GroupModelTestResult{{Passed: false}, {Passed: true}}) {
		t.Fatal("allGroupTestResultsPassed(leading false) = true, want false")
	}
}
