package task

import (
	"testing"
	"time"
)

// TestFailureTracker_CooldownExpiryResetsCount verifies that when a channel's
// cooldown expires, ShouldSkip resets consecutiveFailures so a single new
// failure does not immediately re-trigger a 30-minute cooldown.
//
// 场景：连续失败 3 次 → 冷却 → 冷却到期（手动把 cooldownUntil 设为过去）→
// ShouldSkip 返回 false 并重置计数 → 再失败 1 次不应进入冷却（ShouldSkip 仍 false）。
//
// 修复前（无重置）：冷却到期后 consecutiveFailures 仍为 3，1 次失败加到 4 即
// >= maxConsecutiveFailures，立刻续 30m 冷却 → 死循环。
func TestFailureTracker_CooldownExpiryResetsCount(t *testing.T) {
	ft := NewFailureTracker()
	const chID = 42

	// 连续失败 3 次 → 进入冷却。
	for i := 0; i < maxConsecutiveFailures; i++ {
		ft.RecordFailure(chID, "test-channel")
	}
	state := ft.states[chID]
	if state == nil {
		t.Fatal("state not created after RecordFailure")
	}
	if state.consecutiveFailures != maxConsecutiveFailures {
		t.Fatalf("consecutiveFailures: want %d, got %d", maxConsecutiveFailures, state.consecutiveFailures)
	}
	if state.cooldownUntil.IsZero() {
		t.Fatal("cooldownUntil not set after 3 failures")
	}
	if !ft.ShouldSkip(chID) {
		t.Fatal("ShouldSkip: want true while in cooldown, got false")
	}

	// 模拟冷却到期：把 cooldownUntil 设为过去，不真等 30 分钟。
	ft.mu.Lock()
	state.cooldownUntil = time.Now().Add(-1 * time.Minute)
	ft.mu.Unlock()

	// 冷却到期：ShouldSkip 应返回 false 并重置计数。
	if ft.ShouldSkip(chID) {
		t.Fatal("ShouldSkip: want false after cooldown expired, got true")
	}
	state = ft.states[chID]
	if state.consecutiveFailures != 0 {
		t.Fatalf("consecutiveFailures: want 0 after cooldown expiry reset, got %d", state.consecutiveFailures)
	}
	if !state.cooldownUntil.IsZero() {
		t.Fatalf("cooldownUntil: want zero after reset, got %v", state.cooldownUntil)
	}

	// 再失败 1 次：不应立即进入冷却（需累计 3 次才冷却）。
	ft.RecordFailure(chID, "test-channel")
	state = ft.states[chID]
	if state.consecutiveFailures != 1 {
		t.Fatalf("consecutiveFailures: want 1 after single post-cooldown failure, got %d", state.consecutiveFailures)
	}
	if !state.cooldownUntil.IsZero() {
		t.Fatalf("cooldownUntil: want zero (no immediate cooldown) after single failure, got %v", state.cooldownUntil)
	}
	if ft.ShouldSkip(chID) {
		t.Fatal("ShouldSkip: want false after single post-cooldown failure (no immediate cooldown), got true")
	}

	// 累计够 3 次才再进冷却。
	for i := 0; i < maxConsecutiveFailures-1; i++ {
		ft.RecordFailure(chID, "test-channel")
	}
	state = ft.states[chID]
	if state.consecutiveFailures != maxConsecutiveFailures {
		t.Fatalf("consecutiveFailures: want %d after accumulating, got %d", maxConsecutiveFailures, state.consecutiveFailures)
	}
	if state.cooldownUntil.IsZero() {
		t.Fatal("cooldownUntil: want set after accumulating 3 failures, got zero")
	}
	if !ft.ShouldSkip(chID) {
		t.Fatal("ShouldSkip: want true after 3 failures re-triggered cooldown, got false")
	}
}

// TestFailureTracker_RecordSuccessResetsCount is a baseline guard: RecordSuccess
// must still reset consecutiveFailures (the pre-existing path the fix relies on
// being unchanged).
func TestFailureTracker_RecordSuccessResetsCount(t *testing.T) {
	ft := NewFailureTracker()
	const chID = 7

	for i := 0; i < 2; i++ {
		ft.RecordFailure(chID, "test-channel")
	}
	if ft.states[chID].consecutiveFailures != 2 {
		t.Fatalf("consecutiveFailures: want 2, got %d", ft.states[chID].consecutiveFailures)
	}

	ft.RecordSuccess(chID)
	if ft.states[chID].consecutiveFailures != 0 {
		t.Fatalf("consecutiveFailures: want 0 after RecordSuccess, got %d", ft.states[chID].consecutiveFailures)
	}
	if !ft.states[chID].cooldownUntil.IsZero() {
		t.Fatalf("cooldownUntil: want zero after RecordSuccess, got %v", ft.states[chID].cooldownUntil)
	}
}
