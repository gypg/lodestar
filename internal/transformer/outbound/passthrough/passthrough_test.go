package passthrough

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	transformermodel "github.com/gypg/lodestar/internal/transformer/model"
)

// TestTransformRequest_RewritesOnlyTopLevelModel 断言 passthrough 只改写顶层 model，
// 其余客户端字段（包括 inbound 结构未建模的自定义字段 custom_field）原样保留。
// 变异「把其它字段也改写或丢失」→ 本测试红。
func TestTransformRequest_RewritesOnlyTopLevelModel(t *testing.T) {
	// 客户端原始请求体：含 messages、temperature、stream、以及 inbound 未建模的 custom_field。
	rawBody := `{"model":"client-gpt","messages":[{"role":"user","content":"hi"}],"temperature":0.7,"stream":true,"custom_field":{"nested":[1,2,3]},"top_p":0.9}`
	request := &transformermodel.InternalLLMRequest{
		Model:      "upstream-resolved-model",
		RawRequest: []byte(rawBody),
		RawPath:    "/v1/chat/completions",
	}

	httpReq, err := (&Outbound{}).TransformRequest(context.Background(), request, "https://upstream.example.com", "sk-test-key")
	if err != nil {
		t.Fatalf("TransformRequest error: %v", err)
	}

	// URL 保留原始路径
	if got, want := httpReq.URL.String(), "https://upstream.example.com/v1/chat/completions"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}

	// 鉴权头
	if got := httpReq.Header.Get("Authorization"); got != "Bearer sk-test-key" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer sk-test-key")
	}
	if got := httpReq.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("body is not a JSON object: %v\nbody: %s", err, string(body))
	}

	// model 被重写为上游模型名
	var modelVal string
	if err := json.Unmarshal(parsed["model"], &modelVal); err != nil {
		t.Fatalf("model field unmarshal: %v", err)
	}
	if modelVal != "upstream-resolved-model" {
		t.Fatalf("model = %q, want %q", modelVal, "upstream-resolved-model")
	}

	// 其余字段原样保留
	for _, key := range []string{"messages", "temperature", "stream", "custom_field", "top_p"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("field %q was lost; body: %s", key, string(body))
		}
	}
	// custom_field 的嵌套结构未被破坏
	var custom map[string]json.RawMessage
	if err := json.Unmarshal(parsed["custom_field"], &custom); err != nil {
		t.Fatalf("custom_field unmarshal: %v", err)
	}
	var nested []int
	if err := json.Unmarshal(custom["nested"], &nested); err != nil || len(nested) != 3 || nested[0] != 1 || nested[2] != 3 {
		t.Fatalf("custom_field.nested lost: %v", err)
	}
	// temperature 数值未被改动
	var temp float64
	if err := json.Unmarshal(parsed["temperature"], &temp); err != nil || temp != 0.7 {
		t.Fatalf("temperature changed: %v", err)
	}
}

// TestTransformRequest_PreservesOriginalPath 断言 RawPath 非空时保留原始路径。
func TestTransformRequest_PreservesOriginalPath(t *testing.T) {
	tests := []struct {
		name    string
		rawPath string
		baseURL string
		want    string
	}{
		{name: "chat completions path", rawPath: "/v1/chat/completions", baseURL: "https://upstream.example.com", want: "https://upstream.example.com/v1/chat/completions"},
		{name: "responses path", rawPath: "/v1/responses", baseURL: "https://upstream.example.com/", want: "https://upstream.example.com/v1/responses"},
		{name: "omp custom path", rawPath: "/omp/v1/generate", baseURL: "https://omp.example.com", want: "https://omp.example.com/omp/v1/generate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &transformermodel.InternalLLMRequest{
				Model:      "m",
				RawRequest: []byte(`{"model":"x"}`),
				RawPath:    tt.rawPath,
			}
			httpReq, err := (&Outbound{}).TransformRequest(context.Background(), request, tt.baseURL, "k")
			if err != nil {
				t.Fatalf("TransformRequest error: %v", err)
			}
			if got := httpReq.URL.String(); got != tt.want {
				t.Fatalf("URL = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTransformRequest_NoRawPathUsesBaseURLOnly 断言 RawPath 为空时不追加路径。
func TestTransformRequest_NoRawPathUsesBaseURLOnly(t *testing.T) {
	request := &transformermodel.InternalLLMRequest{
		Model:      "m",
		RawRequest: []byte(`{"model":"x"}`),
		RawPath:    "",
	}
	httpReq, err := (&Outbound{}).TransformRequest(context.Background(), request, "https://upstream.example.com/api", "k")
	if err != nil {
		t.Fatalf("TransformRequest error: %v", err)
	}
	if got, want := httpReq.URL.String(), "https://upstream.example.com/api"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
}

// TestTransformRequest_NilRequest 错误路径。
func TestTransformRequest_NilRequest(t *testing.T) {
	_, err := (&Outbound{}).TransformRequest(context.Background(), nil, "https://x", "k")
	if err == nil {
		t.Fatal("TransformRequest(nil) = nil error, want error")
	}
}

// TestTransformRequest_EmptyModel 错误路径：model 必须非空。
func TestTransformRequest_EmptyModel(t *testing.T) {
	request := &transformermodel.InternalLLMRequest{
		Model:      "  ",
		RawRequest: []byte(`{"model":"x"}`),
	}
	_, err := (&Outbound{}).TransformRequest(context.Background(), request, "https://x", "k")
	if err == nil {
		t.Fatal("TransformRequest(empty model) = nil error, want error")
	}
}

// TestTransformRequest_NonObjectBody 非对象 JSON body 报错。
func TestTransformRequest_NonObjectBody(t *testing.T) {
	request := &transformermodel.InternalLLMRequest{
		Model:      "m",
		RawRequest: []byte(`[1,2,3]`),
	}
	_, err := (&Outbound{}).TransformRequest(context.Background(), request, "https://x", "k")
	if err == nil {
		t.Fatal("TransformRequest(array body) = nil error, want error")
	}
}

// TestTransformResponse_EmptyBodyTriggersFake200
// ★ fake200 纵深防御：上游 200 + 空响应体 → 返回空 InternalLLMResponse，
// 被 relay 层 isFake200Response 拦为失败、计费层 isUnbillableFake200Response 兜底不扣费。
// 变异「让 TransformResponse 对空体返回 error」→ handleResponse 会因 err 直接返回，
// 跳过 isFake200Response 判定，fake200 防御被绕过 → 本测试红。
func TestTransformResponse_EmptyBodyTriggersFake200(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader("")),
	}
	out, err := (&Outbound{}).TransformResponse(context.Background(), resp)
	if err != nil {
		t.Fatalf("TransformResponse error: %v; empty 200 body must NOT error so fake200 guard can fire", err)
	}
	if out == nil {
		t.Fatal("TransformResponse returned nil; fake200 guard needs an empty-but-non-nil InternalLLMResponse")
	}
	if len(out.Choices) != 0 || len(out.EmbeddingData) != 0 {
		t.Fatalf("empty 200 body produced Choices=%d EmbeddingData=%d; want both empty so isFake200Response fires", len(out.Choices), len(out.EmbeddingData))
	}
}

// TestTransformResponse_InvalidJSONTriggersFake200
// 上游 200 + 非法 JSON（如纯文本错误体）→ 同样返回空 InternalLLMResponse，走 fake200。
func TestTransformResponse_InvalidJSONTriggersFake200(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader("该接口未接入")),
	}
	out, err := (&Outbound{}).TransformResponse(context.Background(), resp)
	if err != nil {
		t.Fatalf("TransformResponse error: %v; invalid 200 body must NOT error so fake200 guard can fire", err)
	}
	if len(out.Choices) != 0 {
		t.Fatalf("invalid 200 body produced Choices=%d; want empty so isFake200Response fires", len(out.Choices))
	}
}

// TestTransformResponse_LegalResponsePreservesChoices
// ★ 防过度防御：上游返回合法 OpenAI 结构 → Choices 正常填充，不被 fake200 误伤。
func TestTransformResponse_LegalResponsePreservesChoices(t *testing.T) {
	legalBody := `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(legalBody)),
	}
	out, err := (&Outbound{}).TransformResponse(context.Background(), resp)
	if err != nil {
		t.Fatalf("TransformResponse error: %v", err)
	}
	if len(out.Choices) != 1 {
		t.Fatalf("Choices len = %d, want 1; body: %s", len(out.Choices), legalBody)
	}
	if out.Usage == nil || out.Usage.PromptTokens != 5 || out.Usage.CompletionTokens != 2 {
		t.Fatalf("Usage not preserved: %+v", out.Usage)
	}
}

// TestTransformResponse_4xxReturnsResponseError
// 上游 4xx + {"error":{...}} → 返回 ResponseError，不进 fake200 路径。
func TestTransformResponse_4xxReturnsResponseError(t *testing.T) {
	errBody := `{"error":{"message":"rate limited","type":"rate_limit_error"}}`
	resp := &http.Response{
		StatusCode: 429,
		Body:       io.NopCloser(strings.NewReader(errBody)),
	}
	_, err := (&Outbound{}).TransformResponse(context.Background(), resp)
	if err == nil {
		t.Fatal("TransformResponse(429) = nil error, want error")
	}
	if _, ok := err.(*transformermodel.ResponseError); !ok {
		t.Fatalf("error type = %T, want *ResponseError", err)
	}
}

// TestTransformStream_DoneMarker
func TestTransformStream_DoneMarker(t *testing.T) {
	out, err := (&Outbound{}).TransformStream(context.Background(), []byte(doneMarker))
	if err != nil {
		t.Fatalf("TransformStream( error: %v", err)
	}
	if out == nil || out.Object != doneMarker {
		t.Fatalf("TransformStream( = %+v, want Object=%q", out, doneMarker)
	}
}

// TestTransformStream_ErrorChunk
func TestTransformStream_ErrorChunk(t *testing.T) {
	chunk := `{"error":{"message":"upstream boom","type":"server_error"}}`
	_, err := (&Outbound{}).TransformStream(context.Background(), []byte(chunk))
	if err == nil {
		t.Fatal("TransformStream(error chunk) = nil error, want error")
	}
}

// TestTransformStream_LegalChunk
func TestTransformStream_LegalChunk(t *testing.T) {
	chunk := `{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}]}`
	out, err := (&Outbound{}).TransformStream(context.Background(), []byte(chunk))
	if err != nil {
		t.Fatalf("TransformStream error: %v", err)
	}
	if len(out.Choices) != 1 {
		t.Fatalf("Choices len = %d, want 1", len(out.Choices))
	}
}
