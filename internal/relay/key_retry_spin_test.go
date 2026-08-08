package relay

/*
BUG-006 守卫 —— 单 key 渠道触发 keyRound 无限自旋（请求永不返回）。

发现经过（2026-08-08）：给 BUG-005 做变异测试时，几个 Handler 测试放在一起跑会挂
2 分钟不返回。goroutine dump 显示线程处于 [runnable] 而不是阻塞 —— 是自旋，不是死锁，
栈顶固定在 retry_shared.go:186（failure-hint skip）。随后在**未变异的干净树**上用
探针复现，确认与变异无关，是既有缺陷。

机制（retry_shared.go 的 key 级重试循环）：
  for keyRound := 1; keyRound == 1 || keyRound <= max; keyRound++ {
      if keyRound == 1 { usedKey = ...ExcludingWithCooldown(nil, ...) }   // ← 排除表写死 nil
      ...
      if hint命中 { failedKeyIDs = append(...); keyRound--; continue }     // ← 与 keyRound++ 抵消
  }
`keyRound--` 的本意是"跳过不消耗重试预算"，但 keyRound==1 这条分支把排除表写死成 nil，
于是 failedKeyIDs 白攒：回到 round 1 → 重选同一把 key → 再次命中同一条 hint → 再 rewind。
循环体内没有 I/O 也没有 sleep，直接 100% CPU 空转，请求永不返回。

触发条件（生产可达，不需要任何异常配置）：
  单 key 渠道 + 一次 401/403（写 failure hint，TTL 内有效）→ 下一个同 key 请求直接挂死。
429 和网络错误同样会写 hint。SkipCircuitBreak 那条分支（:192）是完全一样的形状。

修复：keyRound==1 时也传 failedKeyIDs。真正首次进入时它是 nil，行为不变；
只有被 rewind 回来时才非空，此时正好把已跳过的 key 排除掉，循环得以推进。

本文件用带超时的守卫锁住"Handler 必须在有限时间内返回"。
*/

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/transformer/inbound"
)

// runHandlerWithDeadline 在独立 goroutine 里跑 Handler，超时即判定自旋。
// 自旋时 Handler 永不返回，那个 goroutine 会一直空转到测试进程退出 —— 这是刻意的：
// 只有让测试失败并留下证据，才比"整包 go test 神秘挂住"更容易定位。
func runHandlerWithDeadline(t *testing.T, c *gin.Context, budget time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		Handler(dbmodel.EndpointTypeChat, inbound.InboundTypeOpenAIChat, c)
	}()
	select {
	case <-done:
	case <-time.After(budget):
		t.Fatalf("Handler did not return within %s — the key-retry loop is spinning "+
			"(BUG-006 regression: keyRound-- rewinds to round 1, which re-picks the same "+
			"key because the exclusion list is ignored there)", budget)
	}
}

// TestHandler_singleKeyChannel_authFailure_terminates
// ★ 核心守卫：单 key 渠道 + 401 必须终止。
//
// 401 会写 failure hint（shouldStoreFailureHint 对 401/403/429/网络错误返回 true），
// 单 key 渠道又没有别的 key 可选，正是自旋的最短触发路径。
// 把 retry_shared.go 的 failedKeyIDs 改回 nil → 本测试挂死到超时 → 红。
func TestHandler_singleKeyChannel_authFailure_terminates(t *testing.T) {
	const requestModel = "bug006-model"
	hits := initLLMScopeNoneEnv(t, requestModel, http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = newChatCompletionRequest(t, requestModel)
	c.Set("api_key_id", 55401)

	runHandlerWithDeadline(t, c, 15*time.Second)

	if *hits == 0 {
		t.Fatal("upstream never received the request — the test is not exercising the relay")
	}
	if rec.Code < 400 {
		t.Errorf("client status = %d, want a failure status", rec.Code)
	}
}

// TestHandler_singleKeyChannel_authFailure_doesNotBurnUpstream
// 自旋的另一面：跳过分支本身不打上游，所以光看命中数看不出自旋。
// 但反过来必须确认修复没有把"跳过"变成"每次都真发一遍请求" ——
// 那样虽然不挂死了，却会把上游打爆。两条断言合起来才完整。
func TestHandler_singleKeyChannel_authFailure_doesNotBurnUpstream(t *testing.T) {
	const requestModel = "bug006-model-2"
	hits := initLLMScopeNoneEnv(t, requestModel, http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = newChatCompletionRequest(t, requestModel)
	c.Set("api_key_id", 55402)

	runHandlerWithDeadline(t, c, 15*time.Second)

	// 一轮重试链的上限（maxRouteRetries × adapter 数）。跳过分支不发请求，
	// 所以实际会更少；这里只要挡住"自旋期间疯狂重发"这种退化实现。
	const maxReasonableHits = 8
	if *hits > maxReasonableHits {
		t.Errorf("upstream hit count = %d, want <= %d — the skip path should not forward "+
			"a request every rewind", *hits, maxReasonableHits)
	}
}
