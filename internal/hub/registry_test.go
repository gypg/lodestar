package hub

import (
	"context"
	"testing"

	"github.com/lingyuins/octopus/internal/model"
)

// mockAdapter is a minimal SiteAdapter implementation for registry tests.
type mockAdapter struct {
	name string
}

func (m *mockAdapter) FetchUserInfo(_ context.Context, _ *model.RemoteSite) (*UserInfoResult, error) {
	return nil, nil
}

func (m *mockAdapter) PerformCheckIn(_ context.Context, _ *model.RemoteSite) (*CheckInResult, error) {
	return nil, nil
}

func (m *mockAdapter) FetchCheckInStatus(_ context.Context, _ *model.RemoteSite) (*bool, error) {
	return nil, nil
}

func (m *mockAdapter) FetchModels(_ context.Context, _ *model.RemoteSite) ([]string, error) {
	return nil, nil
}

func (m *mockAdapter) FetchModelPricing(_ context.Context, _ *model.RemoteSite) ([]ModelPricingEntry, error) {
	return nil, nil
}

func (m *mockAdapter) FetchTokens(_ context.Context, _ *model.RemoteSite) ([]RemoteToken, error) {
	return nil, nil
}

func (m *mockAdapter) CreateToken(_ context.Context, _ *model.RemoteSite, _ CreateTokenRequest) error {
	return nil
}

func (m *mockAdapter) ListChannels(_ context.Context, _ *model.RemoteSite) ([]RemoteChannel, error) {
	return nil, nil
}

func (m *mockAdapter) CreateChannel(_ context.Context, _ *model.RemoteSite, _ RemoteChannelCreateReq) error {
	return nil
}

func (m *mockAdapter) UpdateChannel(_ context.Context, _ *model.RemoteSite, _ RemoteChannelUpdateReq) error {
	return nil
}

func (m *mockAdapter) DeleteChannel(_ context.Context, _ *model.RemoteSite, _ int) error {
	return nil
}

func (m *mockAdapter) FetchAnnouncement(_ context.Context, _ *model.RemoteSite) (string, error) {
	return "", nil
}

func (m *mockAdapter) FetchSiteStatus(_ context.Context, _ *model.RemoteSite) (*SiteStatusInfo, error) {
	return nil, nil
}

func (m *mockAdapter) RedeemCode(_ context.Context, _ *model.RemoteSite, _ string) (*RedeemResult, error) {
	return nil, nil
}

func (m *mockAdapter) FetchUsageLogs(_ context.Context, _ *model.RemoteSite, _, _ int) ([]RemoteUsageLog, error) {
	return nil, nil
}

// withCleanRegistry saves the current global adapters map, replaces it with an
// empty copy for the duration of the test, then restores the original.
func withCleanRegistry(t *testing.T) {
	t.Helper()

	mu.Lock()
	saved := make(map[string]SiteAdapter, len(adapters))
	for k, v := range adapters {
		saved[k] = v
	}
	adapters = make(map[string]SiteAdapter)
	mu.Unlock()

	t.Cleanup(func() {
		mu.Lock()
		adapters = saved
		mu.Unlock()
	})
}

func TestGetReturnsRegisteredAdapter(t *testing.T) {
	withCleanRegistry(t)

	mock := &mockAdapter{name: "custom"}
	Register("custom-type", mock)

	got, err := Get("custom-type")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != mock {
		t.Fatalf("expected registered adapter, got %v", got)
	}
}

func TestGetFallbackToNewAPI(t *testing.T) {
	withCleanRegistry(t)

	fallback := &mockAdapter{name: "new-api-fallback"}
	Register(model.SiteTypeNewAPI, fallback)

	got, err := Get(model.SiteTypeUnknown)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != fallback {
		t.Fatalf("expected fallback adapter for unknown type, got %v", got)
	}
}

func TestGetErrorWhenNoFallback(t *testing.T) {
	withCleanRegistry(t)

	_, err := Get("nonexistent")
	if err == nil {
		t.Fatal("expected error when no adapter or fallback is registered, got nil")
	}
}

func TestMustGetPanicsOnMissing(t *testing.T) {
	withCleanRegistry(t)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected MustGet to panic when adapter is not registered")
		}
	}()

	MustGet("missing-type")
}
