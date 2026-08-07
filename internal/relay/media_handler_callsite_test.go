package relay

/*
MediaHandler 调用点守卫 — 堵住 media_relay.go:81 的测试盲点。

为什么需要这个文件（`media_handler_integration_test.go` 不够）：
那个文件里的测试**显式调用** extractBodyForBilling，所以它守的是"这个函数行为对不对"，
而不是"MediaHandler 还调不调它"。把 media_relay.go:81 整行删掉，relay 包全绿——
实测确认（2026-08-07）。原因是全仓库零测试驱动 MediaHandler，所有测试都从
recordMediaRelayLog 往下切入，绕过了 :81。

本文件的做法：真的跑 MediaHandler，入口在 :81 的**上游**。
  1. 起 httptest 假上游（真的收 multipart 请求、回 200 JSON）；
  2. 用 group/channel 缓存注册一个指向假上游的渠道（无需真 DB 表）；
  3. 给用户可见模型挂 param('size') 计费表达式；
  4. 发 multipart 请求（bodyBytes==nil，param 只能靠 :81 的提取拿到 size）；
  5. 断言 ChargeKey 收到的 cost == 表达式对 size 求值的结果。

删掉 media_relay.go:81 → billingBody 为 nil → param('size') 返回 nil → 命中 else 分支
→ cost 从 7 变 1 → 断言红。这才是调用点守卫。
*/

import (
	"bytes"
	"fmt"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	billing "github.com/gypg/lodestar/internal/op/billing"
	chpkg "github.com/gypg/lodestar/internal/op/channel"
	grppkg "github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/op/setting"
)

// mediaCallsiteEnv 是驱动 MediaHandler 所需的最小环境。
type mediaCallsiteEnv struct {
	upstreamGotFields map[string]string
	charges           []float64
}

// initMediaCallsiteEnv 搭起 DB + 缓存 + 假上游 + group/channel 缓存条目。
// 返回 env 供断言。requestModel 是用户可见模型名（计费表达式挂在它上面）。
func initMediaCallsiteEnv(t *testing.T, requestModel, expr string) *mediaCallsiteEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	env := &mediaCallsiteEnv{upstreamGotFields: map[string]string{}}

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := db.GetDB().AutoMigrate(&dbmodel.Setting{}); err != nil {
		t.Fatalf("migrate setting: %v", err)
	}
	if err := setting.RefreshCache(nil); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	if err := setting.SetString(dbmodel.SettingKeyBillingExpr,
		fmt.Sprintf(`{%q:%q}`, requestModel, expr)); err != nil {
		t.Fatalf("set billing expr: %v", err)
	}

	// 假上游：断言它真的收到了 multipart，并回一个合法的图片响应。
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(8 << 20); err == nil && r.MultipartForm != nil {
			for k, v := range r.MultipartForm.Value {
				if len(v) > 0 {
					env.upstreamGotFields[k] = v[0]
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGk="}]}`))
	}))
	t.Cleanup(upstream.Close)

	// 渠道走缓存（ch.Get 只读 chCache，不查库）。
	const channelID = 4101
	chpkg.GetCache().Clear()
	chpkg.GetCache().Set(channelID, dbmodel.Channel{
		ID:       channelID,
		Name:     "callsite-upstream",
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}},
		Keys: []dbmodel.ChannelKey{
			{ID: 91, ChannelID: channelID, Enabled: true, ChannelKey: "sk-callsite"},
		},
	})

	// 分组同样走缓存 + RebuildIndexes（GroupGetEnabledMapByEndpoint 读 groupMap）。
	grppkg.GetCache().Clear()
	grppkg.GetCache().Set(7101, dbmodel.Group{
		ID:           7101,
		Name:         requestModel,
		EndpointType: dbmodel.EndpointTypeImageGeneration,
		Mode:         dbmodel.GroupModeRoundRobin,
		Items: []dbmodel.GroupItem{
			{ID: 1, GroupID: 7101, ChannelID: channelID, ModelName: "upstream-image-model", Priority: 1, Weight: 1},
		},
	})
	grppkg.RebuildIndexes()

	// 观测 ChargeKey 的实参 —— 这是 :81 的效果可见的地方。
	prev := billing.CallRecorder
	billing.CallRecorder = func(_ int, _ string, _, _ int, cost float64) {
		env.charges = append(env.charges, cost)
	}
	t.Cleanup(func() { billing.CallRecorder = prev })

	t.Cleanup(func() {
		chpkg.GetCache().Clear()
		grppkg.GetCache().Clear()
		grppkg.RebuildIndexes()
		_ = db.Close()
	})

	return env
}

// newMultipartImageEditRequest 造一个 images/edits 的 multipart 请求。
func newMultipartImageEditRequest(t *testing.T, model, size string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("model", model); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	if err := w.WriteField("size", size); err != nil {
		t.Fatalf("write size field: %v", err)
	}
	part, err := w.CreateFormFile("image", "in.png")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write([]byte("fake-png-bytes")); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	contentType := w.FormDataContentType()
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	req.Header.Set("Content-Type", contentType)
	return req
}

// TestMediaHandler_multipartBillingExtraction_reachesCallsite
// ★ 这是真正的调用点守卫：入口是 MediaHandler，不是 extractBodyForBilling。
//
// 表达式三分支：'1024x1024' → 7.0、'512x512' → 3.0、nil(取不到) → 1.0。
// 三档而非两档是刻意的：nil 必须与任何真实 size 值区分开，否则"接线被删"和
// "传了别的 size"会落到同一个价格，测试就退化成反向锁而不是调用点守卫。
// multipart 端点的 bodyBytes 恒为 nil，param('size') 唯一的数据来源就是
// media_relay.go:81 的提取。所以：
//   - 接线在   → cost == 7.0
//   - 接线删掉 → param('size') == nil → 落到兜底分支 cost == 1.0 → 本测试红
func TestMediaHandler_multipartBillingExtraction_reachesCallsite(t *testing.T) {
	const requestModel = "callsite-image-model"
	const expr = `param('size') == '1024x1024' ? 7.0 : (param('size') == '512x512' ? 3.0 : 1.0)`
	env := initMediaCallsiteEnv(t, requestModel, expr)

	req := newMultipartImageEditRequest(t, requestModel, "1024x1024")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key_id", 55001)

	MediaHandler(MediaEndpointImageEdit, c)

	if rec.Code != http.StatusOK {
		t.Fatalf("MediaHandler status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	// 前置校验：请求真的转发到了假上游（否则下面的计费断言是空转）。
	if got := env.upstreamGotFields["size"]; got != "1024x1024" {
		t.Fatalf("upstream did not receive the multipart size field, got %q — request never forwarded", got)
	}
	if len(env.charges) != 1 {
		t.Fatalf("ChargeKey call count: want exactly 1, got %d (%v)", len(env.charges), env.charges)
	}
	// ★ 核心断言：7.0 只可能来自 :81 提取出的 body。
	if math.Abs(env.charges[0]-7.0) > 1e-9 {
		t.Errorf("charged cost: want 7.0 (param('size') resolved via media_relay.go:81), got %.9f — "+
			"1.0 means param('size') was nil, i.e. the extractBodyForBilling wiring at the "+
			"MediaHandler call site is gone (BUG-004b regression)",
			env.charges[0])
	}
}

// TestMediaHandler_multipartBillingExtraction_sizeSensitive
// 反方向锁定：换一个 size 必须换一个价，证明上面的 7.0 不是常量凑出来的，
// 而是真的按请求体字段求值（防止"表达式恒返回 7"这种等价实现骗过守卫）。
func TestMediaHandler_multipartBillingExtraction_sizeSensitive(t *testing.T) {
	const requestModel = "callsite-image-model-2"
	const expr = `param('size') == '1024x1024' ? 7.0 : (param('size') == '512x512' ? 3.0 : 1.0)`
	env := initMediaCallsiteEnv(t, requestModel, expr)

	req := newMultipartImageEditRequest(t, requestModel, "512x512")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key_id", 55002)

	MediaHandler(MediaEndpointImageEdit, c)

	if rec.Code != http.StatusOK {
		t.Fatalf("MediaHandler status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if len(env.charges) != 1 {
		t.Fatalf("ChargeKey call count: want exactly 1, got %d (%v)", len(env.charges), env.charges)
	}
	// 1.0 是 param('size')==nil 的兜底分支：若 :81 接线被删，这里会拿到 1.0 而不是 3.0，
	// 所以本测试同时也是调用点守卫（而不只是"换 size 换价"的反向锁）。
	if math.Abs(env.charges[0]-3.0) > 1e-9 {
		t.Errorf("charged cost for size=512x512: want 3.0, got %.9f "+
			"(1.0 means param('size') was nil — extractBodyForBilling wiring gone)", env.charges[0])
	}
}

// TestMediaHandler_multipartForward_replacesModelWithUpstreamName
// 顺带锁住 MediaHandler 的另一条无守卫接线：转发给上游的 model 字段必须被替换成
// 映射后的上游模型名（buildMultipartForwardBody 的 resolvedModel 分支），
// 而计费仍按用户可见名。两套口径混用过就是 BUG-003 的附带缺陷之一。
func TestMediaHandler_multipartForward_replacesModelWithUpstreamName(t *testing.T) {
	const requestModel = "callsite-image-model-3"
	const expr = `param('size') == '1024x1024' ? 7.0 : (param('size') == '512x512' ? 3.0 : 1.0)`
	env := initMediaCallsiteEnv(t, requestModel, expr)

	req := newMultipartImageEditRequest(t, requestModel, "1024x1024")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key_id", 55003)

	MediaHandler(MediaEndpointImageEdit, c)

	if rec.Code != http.StatusOK {
		t.Fatalf("MediaHandler status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if got := env.upstreamGotFields["model"]; got != "upstream-image-model" {
		t.Errorf("upstream model field: want %q (mapped name), got %q", "upstream-image-model", got)
	}
	// 计费用的是用户可见名：表达式挂在 requestModel 上，命中即证明口径正确。
	if len(env.charges) != 1 || math.Abs(env.charges[0]-7.0) > 1e-9 {
		t.Errorf("billing should key on the user-facing model name (%s): charges=%v", requestModel, env.charges)
	}
}
