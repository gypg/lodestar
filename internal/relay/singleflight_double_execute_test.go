package relay

/*
BUG-005 守卫 —— singleflight 失败路径重复执行整条 relay。

发现经过（2026-08-08）：写 R-3 的 LLM 调用点守卫时，"上游恰好命中 1 次"这条断言
在修完 R-3 后仍然报 2 次。追下去发现跟 R-3 无关，是 relay.go 里一处独立的控制流洞：

	if inflightEnabled {
		result, sfErr, shared := relayInflightGroup.Do(...)   // ← executeRelay 跑第 1 遍
		if sfErr == nil { ... return }                        // ← 失败时 sfErr != nil，跌穿
	}
	executeRelay(...)                                          // ← 跑第 2 遍

executeRelay 失败时返回非 nil error，singleflight 把它当 sfErr 原样抛出，于是
`if sfErr == nil` 整块被跳过，控制流跌到函数末尾的兜底 executeRelay 上再跑一遍。

两个后果：
 1. 上游被打 2N 次（N = 单次重试链长度）。失败请求的成本、限流、熔断计数全部翻倍。
 2. 第一遍已经把错误响应写给客户端了，第二遍的错误又追加写在后面，
    客户端收到两个顶层 JSON 对象拼接的非法 body：{"error":{...}}{"code":502,...}

触发条件是默认开的：requestSingleflightKey 只要求非流式 + 无 tools + 有 api_key_id，
不需要开语义缓存。也就是说线上每个失败的非流式请求都在走这条路。

本文件驱动真实的 Handler 入口，两条正交断言分别锁住上面两个后果。
*/

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/transformer/inbound"
)

// TestHandler_failedRequest_writesExactlyOneJSONBody
// ★ 断言 1：客户端收到的必须是**一个**合法 JSON 文档。
//
// 跌穿 bug 在时，body 是两个顶层对象首尾相接 —— encoding/json 解到第一个对象结尾
// 就会在剩余字节上报 "invalid character '{' after top-level value"。
// 用 Decoder 显式检查尾部有没有第二个文档，比 Unmarshal 更能说清楚失败原因。
func TestHandler_failedRequest_writesExactlyOneJSONBody(t *testing.T) {
	const requestModel = "bug005-model"
	initLLMScopeNoneEnv(t, requestModel, http.StatusBadRequest, upstream400Body)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = newChatCompletionRequest(t, requestModel)
	c.Set("api_key_id", 55301)

	Handler(dbmodel.EndpointTypeChat, inbound.InboundTypeOpenAIChat, c)

	dec := json.NewDecoder(rec.Body)
	var first json.RawMessage
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("response body is not valid JSON: %v; body=%s", err, rec.Body.String())
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err == nil {
		t.Errorf("response body contains a SECOND JSON document — the relay ran twice and "+
			"both writes landed in the same response (BUG-005 regression).\n first=%s\n second=%s",
			first, trailing)
	}
}

// TestHandler_failedRequest_doesNotRunRelayTwice
// ★ 断言 2：整条重试链只跑一遍。
//
// 这条独立于断言 1：即便有一天错误回写改成"只写一次"，跌穿仍会让上游白挨一整轮，
// 成本和熔断计数照样翻倍。所以要单独锁上游命中次数。
//
// 用 500（ScopeNextChannel，可重试）而不是 400，是为了跟 R-3 彻底解耦：
// 400 那条路上"命中 1 次"同时受 shouldTryAdapterFallback 影响，两个缺陷会混在一起。
// 500 会跑满一整轮重试，跌穿与否的差别就是"一轮 vs 两轮"，只反映 BUG-005。
func TestHandler_failedRequest_doesNotRunRelayTwice(t *testing.T) {
	const requestModel = "bug005-model-2"
	hits := initLLMScopeNoneEnv(t, requestModel, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = newChatCompletionRequest(t, requestModel)
	c.Set("api_key_id", 55302)

	Handler(dbmodel.EndpointTypeChat, inbound.InboundTypeOpenAIChat, c)

	// 一轮重试链的实测长度（maxRouteRetries × adapter 数）。跌穿时会翻倍。
	// 断言 "<= singleRoundHits" 而不是 "== 4"：将来调重试次数默认值时这个测试
	// 不该无故变红，但翻倍一定会被抓住。
	const singleRoundHits = 4
	if *hits > singleRoundHits {
		t.Errorf("upstream hit count = %d, want <= %d (one retry chain). "+
			"Roughly double means executeRelay ran twice: the singleflight branch fell "+
			"through to the fallback call (BUG-005 regression).", *hits, singleRoundHits)
	}
	if *hits == 0 {
		t.Fatal("upstream never received the request — the test is not exercising the relay")
	}
}
