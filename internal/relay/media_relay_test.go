package relay

import (
	"fmt"
	"mime/multipart"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/relay/balancer"
)

func TestRecordPreparedCandidateSkip_DoesNotDuplicateCircuitBreak(t *testing.T) {
	modelName := fmt.Sprintf("gpt-4o-%s", t.Name())
	group := dbmodel.Group{
		Items: []dbmodel.GroupItem{
			{ChannelID: 11, ModelName: modelName},
		},
	}
	iter := balancer.NewIterator(group, 0, modelName, nil)
	if !iter.Next() {
		t.Fatal("iterator should have one candidate")
	}

	for i := 0; i < 5; i++ {
		balancer.RecordFailure(11, 22, modelName)
	}
	if !iter.SkipCircuitBreak(11, 22, "test-channel", modelName) {
		t.Fatal("SkipCircuitBreak() = false, want true")
	}

	recordPreparedCandidateSkip(iter, iter.Item(), PrepareCandidateResult{
		Channel:    &dbmodel.Channel{ID: 11, Name: "test-channel"},
		UsedKey:    dbmodel.ChannelKey{ID: 22},
		SkipReason: "circuit breaker tripped",
		SkipStatus: dbmodel.AttemptCircuitBreak,
	})

	attempts := iter.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("len(Attempts()) = %d, want 1", len(attempts))
	}
	if attempts[0].Status != dbmodel.AttemptCircuitBreak {
		t.Fatalf("Attempts()[0].Status = %s, want %s", attempts[0].Status, dbmodel.AttemptCircuitBreak)
	}
}

func TestRecordPreparedCandidateSkip_RecordsSkippedCandidate(t *testing.T) {
	group := dbmodel.Group{
		Items: []dbmodel.GroupItem{
			{ChannelID: 11, ModelName: "gpt-4o"},
		},
	}
	iter := balancer.NewIterator(group, 0, "gpt-4o", nil)
	if !iter.Next() {
		t.Fatal("iterator should have one candidate")
	}

	recordPreparedCandidateSkip(iter, iter.Item(), PrepareCandidateResult{
		Channel:    &dbmodel.Channel{ID: 11, Name: "test-channel"},
		UsedKey:    dbmodel.ChannelKey{ID: 22},
		SkipReason: "no available key",
		SkipStatus: dbmodel.AttemptSkipped,
	})

	attempts := iter.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("len(Attempts()) = %d, want 1", len(attempts))
	}
	if attempts[0].Status != dbmodel.AttemptSkipped {
		t.Fatalf("Attempts()[0].Status = %s, want %s", attempts[0].Status, dbmodel.AttemptSkipped)
	}
	if attempts[0].Msg != "no available key" {
		t.Fatalf("Attempts()[0].Msg = %q, want %q", attempts[0].Msg, "no available key")
	}
}

// TestForwardMediaRequestMultipart_ChannelHttpClientError_ClosesPipeReader
// 场景：multipart 转发时 helper.ChannelHttpClient 失败（channel 配置 pool 代理但
// ProxyConfigID 缺失）。修复前 bodyReader 不被 Close，写 goroutine 永久阻塞在
// io.Pipe 的 Write（字段体超过 64KiB 管道缓冲必然阻塞）。
//
// 观测点：writer goroutine 退出。用大字段强制 WriteField 阻塞在管道写——
// 修复后 bodyReader.Close() 让 writer 收到 ErrClosedPipe 并 return；
// 删除 Close() 的变异会让 goroutine 残留，最终超时判红。
//
// 变异自检（删掉 bodyReader.Close()）：测试超时变红——writer goroutine 永不退出，
// waitForGoroutineDrain 在 5s 内等不到 NumGoroutine 回落。能抓到。
func TestForwardMediaRequestMultipart_ChannelHttpClientError_ClosesPipeReader(t *testing.T) {
	// 大字段体（>64KiB io.Pipe 缓冲）让 WriteField 必然阻塞，直到 reader 被关闭。
	bigValue := strings.Repeat("x", 128*1024)

	c, _ := gin.CreateTestContext(nil)
	c.Request = &http.Request{
		Method: http.MethodPost,
		Header: http.Header{"Content-Type": {`multipart/form-data; boundary=x`}},
	}
	c.Request.MultipartForm = &multipart.Form{
		Value: map[string][]string{
			"model":  {"gpt-image-1"},
			"prompt": {bigValue},
		},
	}

	// ProxyMode=pool 且 ProxyConfigID=nil → helper.ChannelHttpClient 直接返回错误，
	// 不触网，不依赖代理配置表。命中修复点（media_relay.go 的 ChannelHttpClient 失败分支）。
	zero := 0
	channel := &dbmodel.Channel{
		ID:            1,
		ProxyMode:     dbmodel.ProxyUsageModePool,
		ProxyConfigID: &zero, // 0 触发 "proxy config id is required" 错误
	}

	cfg := mediaEndpointConfig{
		UpstreamPath:   "/v1/images/edits",
		MultipartInput: true,
	}

	usage := &usageScanner{}

	before := runtime.NumGoroutine()
	start := time.Now()
	statusCode, err := forwardMediaRequestMultipart(
		c, cfg, channel, "sk-test", "gpt-image-1", "gpt-image-1", false, c.Request.Context(), usage,
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("forwardMediaRequestMultipart: want error from ChannelHttpClient, got nil (status=%d)", statusCode)
	}
	if statusCode != 0 {
		t.Fatalf("statusCode: want 0, got %d", statusCode)
	}

	// 修复后 writer goroutine 应在 bodyReader.Close() 后迅速退出。
	// 超时返回 true 表示 goroutine 残留（writer 永久阻塞）。
	if !waitForGoroutineDrain(before, 5*time.Second) {
		t.Fatalf("writer goroutine leaked: NumGoroutine did not return to baseline %d within 5s (err=%v, elapsed=%v)",
			before, err, elapsed)
	}
}

// waitForGoroutineDrain 轮询 NumGoroutine，等待其回落到 baseline 及以下。
// 超时返回 false。用于验证写 goroutine 是否真正退出而非残留阻塞。
func waitForGoroutineDrain(baseline int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		runtime.GC()
		if runtime.NumGoroutine() <= baseline {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return runtime.NumGoroutine() <= baseline
}
