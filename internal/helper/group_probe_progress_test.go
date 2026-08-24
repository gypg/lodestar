package helper

import (
	"context"
	"net/http"
	"testing"
	"time"

	appmodel "github.com/gypg/lodestar/internal/model"
)

// waitGroupTestDone polls the progress store until the run is published as done.
// StartGroupModelTest returns immediately and finishes in a goroutine, so the
// test has to wait for the terminal state rather than reading the handle it got
// back (which is a snapshot taken before any item was probed).
func waitGroupTestDone(t *testing.T, id string) *GroupModelTestProgress {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		progress, ok := GetGroupModelTestProgress(id)
		if ok && progress.Done {
			return progress
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("group test %s did not finish before deadline", id)
	return nil
}

// TestStartGroupModelTestPublishesPerItemResults pins the contract the UI
// actually depends on: the progress record polled from
// GET /api/v1/group/test/progress/:id must carry one result per item.
//
// The group toast in web/src/components/modules/group/Card.tsx does NOT read
// progress.passed — it computes
//
//	failedResults = (testProgress.results ?? []).filter((r) => !r.passed)
//	if (failedResults.length === 0) toast.success('分组内模型均可用')
//
// so an empty Results slice renders as "every model is available" no matter what
// the upstream said. A single-item group whose only channel answers 404 is the
// minimal reproduction of the bug the user reported, and it is NOT covered by
// the all-passed aggregation fix: with zero results there is nothing to fold.
func TestStartGroupModelTestPublishesPerItemResults(t *testing.T) {
	initGroupProbeLogTestEnv(t)

	badUpstream := newProbeStatusUpstream(t, http.StatusNotFound)

	const channelID = 930001
	channels := map[int]appmodel.Channel{
		channelID: openAIProbeChannel(channelID, "probe-404-only", badUpstream.URL),
	}
	group := &appmodel.Group{
		Name:         "single-404-group",
		EndpointType: appmodel.EndpointTypeAll,
		Items: []appmodel.GroupItem{
			{ID: 1, ChannelID: channelID, ModelName: "missing-model", Priority: 1, Weight: 1},
		},
	}

	started, err := StartGroupModelTest(group, channels)
	if err != nil {
		t.Fatalf("StartGroupModelTest() error = %v", err)
	}

	final := waitGroupTestDone(t, started.ID)

	if final.Passed {
		t.Fatalf("progress.Passed = true, want false: the only channel answered 404")
	}
	// The real defect: per-item results must reach the polled progress, because
	// that is what the toast and the per-model list read.
	if len(final.Results) != 1 {
		t.Fatalf("progress.Results len = %d, want 1: the UI derives PASS/FAIL by filtering this slice, so an empty slice reads as 'all models available'", len(final.Results))
	}
	if final.Completed != 1 {
		t.Fatalf("progress.Completed = %d, want 1", final.Completed)
	}
	got := final.Results[0]
	if got.Passed {
		t.Fatalf("result.Passed = true, want false")
	}
	if got.StatusCode != http.StatusNotFound {
		t.Fatalf("result.StatusCode = %d, want 404", got.StatusCode)
	}
	if got.ItemID != 1 || got.ChannelID != channelID {
		t.Fatalf("result identity = {item:%d channel:%d}, want {1 %d}", got.ItemID, got.ChannelID, channelID)
	}
}

// TestStartDraftGroupModelTestPublishesPerItemResults covers the draft path,
// which the group editor uses before a group is saved. It has its own terminal
// publish block (it overwrites Results from the summary), so it needs its own
// guard: a fix applied only to the saved-group path would leave this one broken.
func TestStartDraftGroupModelTestPublishesPerItemResults(t *testing.T) {
	initGroupProbeLogTestEnv(t)

	badUpstream := newProbeStatusUpstream(t, http.StatusNotFound)

	const channelID = 930002
	channels := map[int]appmodel.Channel{
		channelID: openAIProbeChannel(channelID, "draft-probe-404", badUpstream.URL),
	}
	items := []GroupModelDraftTestItem{
		{ClientID: "draft-item-1", ChannelID: channelID, ModelName: "missing-model"},
	}

	started, err := StartDraftGroupModelTest(appmodel.EndpointTypeAll, items, channels)
	if err != nil {
		t.Fatalf("StartDraftGroupModelTest() error = %v", err)
	}

	final := waitGroupTestDone(t, started.ID)

	if final.Passed {
		t.Fatal("progress.Passed = true, want false")
	}
	if len(final.Results) != 1 {
		t.Fatalf("progress.Results len = %d, want 1", len(final.Results))
	}
	// ClientID round-trip matters for the draft path: the editor matches results
	// back to unsaved rows by client_id, so losing it detaches every verdict.
	if final.Results[0].ClientID != "draft-item-1" {
		t.Fatalf("result.ClientID = %q, want draft-item-1", final.Results[0].ClientID)
	}
	if final.Results[0].StatusCode != http.StatusNotFound {
		t.Fatalf("result.StatusCode = %d, want 404", final.Results[0].StatusCode)
	}
}

// TestAppendGroupTestResultStoresEveryResultSoFar guards the incremental store
// write directly, because the terminal-state tests cannot see it: runGroupModelTest
// folds every result in one tight loop after all workers have finished, so a
// polling test would have to win a race to observe a partial record.
//
// The rule: after folding the Nth result, the record in the progress store must
// hold all N results, not just the newest one. `progress` is deliberately never
// mutated (StartGroupModelTest clones it concurrently), so the accumulated set
// has to be copied off the summary. Appending to a clone of `progress` instead
// yields a store record with exactly one result and Completed=1 forever.
func TestAppendGroupTestResultStoresEveryResultSoFar(t *testing.T) {
	summary := &GroupModelTestSummary{Total: 3, Results: make([]GroupModelTestResult, 0, 3)}
	progress := &GroupModelTestProgress{ID: "append-accumulation-probe", Total: 3}
	storeGroupModelProgress(progress)

	incoming := []GroupModelTestResult{
		{ItemID: 1, ChannelID: 11, ModelName: "m1", Passed: true, StatusCode: 200},
		{ItemID: 2, ChannelID: 22, ModelName: "m2", Passed: false, StatusCode: 404, Message: "upstream error: 404"},
		{ItemID: 3, ChannelID: 33, ModelName: "m3", Passed: true, StatusCode: 200},
	}
	// Expected verdict after each fold: PASS, then FAIL once the 404 lands, and
	// it must stay FAIL even though the third item passes.
	wantPassed := []bool{true, false, false}

	for index, result := range incoming {
		appendGroupTestResult(summary, progress, result)

		stored, ok := GetGroupModelTestProgress(progress.ID)
		if !ok {
			t.Fatalf("after fold %d: progress not found in store", index+1)
		}
		if len(stored.Results) != index+1 {
			t.Fatalf("after fold %d: stored Results len = %d, want %d (the store must carry every result so far, not only the newest)", index+1, len(stored.Results), index+1)
		}
		if stored.Completed != index+1 {
			t.Fatalf("after fold %d: stored Completed = %d, want %d", index+1, stored.Completed, index+1)
		}
		if stored.Passed != wantPassed[index] {
			t.Fatalf("after fold %d: stored Passed = %v, want %v", index+1, stored.Passed, wantPassed[index])
		}
		// Identity of every result folded so far must survive, in order.
		for i := 0; i <= index; i++ {
			if stored.Results[i].ItemID != incoming[i].ItemID {
				t.Fatalf("after fold %d: stored.Results[%d].ItemID = %d, want %d", index+1, i, stored.Results[i].ItemID, incoming[i].ItemID)
			}
			if stored.Results[i].StatusCode != incoming[i].StatusCode {
				t.Fatalf("after fold %d: stored.Results[%d].StatusCode = %d, want %d", index+1, i, stored.Results[i].StatusCode, incoming[i].StatusCode)
			}
		}
	}

	// The caller's progress handle must stay untouched: StartGroupModelTest
	// clones it from another goroutine, so mutating it here would be a data race.
	if len(progress.Results) != 0 {
		t.Fatalf("progress.Results len = %d, want 0: the shared handle must not be mutated (the race detector would flag it)", len(progress.Results))
	}
}

// TestGroupTestProgressAccumulatesDuringRun pins the incremental behaviour the
// progress bar depends on: as items complete, the stored progress must grow.
// A two-item group where the first channel is missing entirely (fast reject) and
// the second is healthy verifies that partial state is observable and that the
// mixed verdict stays FAIL all the way to the terminal record.
func TestGroupTestProgressAccumulatesDuringRun(t *testing.T) {
	initGroupProbeLogTestEnv(t)

	okUpstream := newProbeOKUpstream(t, "good-model")

	const okChannelID = 930003
	channels := map[int]appmodel.Channel{
		okChannelID: openAIProbeChannel(okChannelID, "probe-ok", okUpstream.URL),
	}
	group := &appmodel.Group{
		Name:         "mixed-progress-group",
		EndpointType: appmodel.EndpointTypeAll,
		Items: []appmodel.GroupItem{
			// 999999 is deliberately absent from the channels map: the probe
			// rejects it as "channel not found" without any HTTP traffic.
			{ID: 1, ChannelID: 999999, ModelName: "orphan-model", Priority: 1, Weight: 1},
			{ID: 2, ChannelID: okChannelID, ModelName: "good-model", Priority: 2, Weight: 1},
		},
	}

	started, err := StartGroupModelTest(group, channels)
	if err != nil {
		t.Fatalf("StartGroupModelTest() error = %v", err)
	}

	final := waitGroupTestDone(t, started.ID)

	if final.Passed {
		t.Fatal("progress.Passed = true, want false: item 1 has no channel")
	}
	if len(final.Results) != 2 {
		t.Fatalf("progress.Results len = %d, want 2", len(final.Results))
	}
	if final.Completed != 2 || final.Total != 2 {
		t.Fatalf("progress completed/total = %d/%d, want 2/2", final.Completed, final.Total)
	}

	byItem := make(map[int]GroupModelTestResult, len(final.Results))
	for _, result := range final.Results {
		byItem[result.ItemID] = result
	}
	if got, ok := byItem[1]; !ok || got.Passed || got.Message != "channel not found" {
		t.Fatalf("item 1 = %+v, want passed=false message=\"channel not found\"", got)
	}
	if got, ok := byItem[2]; !ok || !got.Passed {
		t.Fatalf("item 2 = %+v, want passed=true", got)
	}
}

// TestRunGroupModelTestDefersDoneWhenCallerMustAmend 直接钉住修复的机制。
//
// 背景：runGroupModelTest 会自己发布一份终态进度记录。draft 路径随后还要再发一份，
// 只为把 ClientID 盖上（编辑器靠 client_id 把判定对回未保存的行）。两次 store 之间
// 存在一个窗口：记录已经 Done=true，但 ClientID 还是空的。任何轮询
// GET /api/v1/group/test/progress/:id 的读者落在窗口里，拿到的每条判定都对不上行。
//
// 那个窗口就是 TestStartDraftGroupModelTestPublishesPerItemResults 在 CI 里偶发变红的原因。
// 端到端测试只能概率性地撞上它，所以这里直接断言不变量：deferDone=true 时发布的记录
// 必须带结果但**不能**是 Done。
func TestRunGroupModelTestDefersDoneWhenCallerMustAmend(t *testing.T) {
	initGroupProbeLogTestEnv(t)

	upstream := newProbeStatusUpstream(t, http.StatusNotFound)
	const channelID = 930101
	channels := map[int]appmodel.Channel{
		channelID: openAIProbeChannel(channelID, "defer-done-probe", upstream.URL),
	}
	group := &appmodel.Group{
		Name:         "defer-done-group",
		EndpointType: appmodel.EndpointTypeAll,
		Items: []appmodel.GroupItem{
			{ID: 1, ChannelID: channelID, ModelName: "some-model", Priority: 1, Weight: 1},
		},
	}

	for _, tt := range []struct {
		name      string
		deferDone bool
		wantDone  bool
	}{
		{name: "caller amends afterwards", deferDone: true, wantDone: false},
		{name: "caller publishes nothing more", deferDone: false, wantDone: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			progress := &GroupModelTestProgress{
				ID:      "defer-done-" + tt.name,
				Total:   len(group.Items),
				Results: make([]GroupModelTestResult, 0, len(group.Items)),
			}
			storeGroupModelProgress(progress)

			if _, err := runGroupModelTest(context.Background(), group, channels, progress, tt.deferDone); err != nil {
				t.Fatalf("runGroupModelTest() error = %v", err)
			}

			published, ok := GetGroupModelTestProgress(progress.ID)
			if !ok {
				t.Fatal("progress record was not published")
			}
			if published.Done != tt.wantDone {
				t.Fatalf("published Done = %t, want %t (deferDone=%t)", published.Done, tt.wantDone, tt.deferDone)
			}
			// 无论是否 defer，结果都必须已经可见——推迟的只是 Done，不是结果。
			if len(published.Results) != 1 {
				t.Fatalf("published Results len = %d, want 1; deferring Done must not hide the verdicts", len(published.Results))
			}
		})
	}
}
