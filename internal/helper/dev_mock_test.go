package helper

import (
	"context"
	"sync"
	"testing"

	"github.com/gypg/lodestar/internal/model"
)

func TestFetchModelsReturnsMockCatalogWhenDevMockEnabled(t *testing.T) {
	t.Setenv("GGZERO_DEV_MOCK_SUCCESS", "true")

	models, err := FetchModels(context.Background(), model.Channel{})
	if err != nil {
		t.Fatalf("FetchModels() error = %v", err)
	}
	if len(models) == 0 {
		t.Fatal("FetchModels() returned no models in dev mock mode")
	}
}

func TestTestChannelReturnsMockSuccessWhenDevMockEnabled(t *testing.T) {
	t.Setenv("GGZERO_DEV_MOCK_SUCCESS", "true")

	summary, err := TestChannel(context.Background(), model.Channel{})
	if err != nil {
		t.Fatalf("TestChannel() error = %v", err)
	}
	if !summary.Passed {
		t.Fatal("TestChannel() Passed = false, want true in dev mock mode")
	}
	if len(summary.Results) == 0 {
		t.Fatal("TestChannel() returned no results in dev mock mode")
	}
}

func TestStartGenerateAIRouteReturnsCompletedProgressWhenDevMockEnabled(t *testing.T) {
	t.Setenv("GGZERO_DEV_MOCK_SUCCESS", "true")

	aiRouteProgress = sync.Map{}

	progress, err := StartGenerateAIRoute(model.GenerateAIRouteRequest{
		Scope: model.AIRouteScopeTable,
	})
	if err != nil {
		t.Fatalf("StartGenerateAIRoute() error = %v", err)
	}
	if !progress.Done {
		t.Fatal("progress.Done = false, want true")
	}
	if progress.Status != model.AIRouteTaskStatusCompleted {
		t.Fatalf("progress.Status = %q, want %q", progress.Status, model.AIRouteTaskStatusCompleted)
	}
	if !progress.ResultReady || progress.Result == nil {
		t.Fatal("progress.ResultReady/result not populated")
	}
	if _, ok := GetGenerateAIRouteProgress(progress.ID); !ok {
		t.Fatal("GetGenerateAIRouteProgress() ok = false, want true")
	}
}
