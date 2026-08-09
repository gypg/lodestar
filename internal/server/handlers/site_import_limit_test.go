package handlers

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	serverauth "github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
)

// S-7 调用点守卫。
//
// 入口一律是 router.RegisterAll 注册出来的**生产路由表**，经 engine.ServeHTTP
// 打真实 HTTP 请求，不直接调 readImportPayload —— 直接调被测函数只能守住函数
// 自身，守不住"生产 handler 是否真的接了闸门"。
//
// 断言落在**副作用**（上游被读走多少字节 / 临时文件是否残留 / 库里有没有落数据），
// 不只看状态码：校验类修复若只断言"返回了 error"，把闸门挪到 sink 之后仍会全绿。

func setSiteImportLimitForTest(t *testing.T, limit, extra int64) {
	t.Helper()
	origLimit := maxSiteImportBytes
	origExtra := maxSiteImportMultipartExtraBytes
	maxSiteImportBytes = limit
	maxSiteImportMultipartExtraBytes = extra
	t.Cleanup(func() {
		maxSiteImportBytes = origLimit
		maxSiteImportMultipartExtraBytes = origExtra
	})
}

// newSiteImportTestEngine 手工挂两条导入路由，middleware 链照生产 init() 复制。
//
// ★ 不能用 router.RegisterAll：它在结尾把全局 registeredRouters 置 nil
// （router.go:124），**整个进程只能成功注册一次**。同包的 rbac_test 已经调过它，
// 谁先跑谁拿到路由，其余全部 404。我第一版就踩了：4 个测试里第 1 个拿到路由通过、
// 第 2/3 个 404 失败，而第 4 个因为断言写的是"不等于 413"，被 404 蒙对了假绿。
// 手工挂路由的入口仍在 handler（importMetAPI/importAllAPIHub）之上，
// 而体积闸门在 handler 内部的 readImportPayload，故调用点守卫成立。
func newSiteImportTestEngine(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	conf.AppConfig.Auth.JWTSecret = "test-jwt-secret"
	if err := op.UserInit(); err != nil {
		t.Fatalf("user init: %v", err)
	}
	if err := op.UserBootstrapCreate("admin", "super-secret-123"); err != nil {
		t.Fatalf("bootstrap user: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}

	engine := gin.New()
	chain := []gin.HandlerFunc{
		middleware.Auth(),
		middleware.RequirePermission(serverauth.PermSitesRead),
		middleware.RequirePermission(serverauth.PermSitesWrite),
	}
	engine.POST("/api/v1/site/import/metapi", append(append([]gin.HandlerFunc(nil), chain...), importMetAPI)...)
	engine.POST("/api/v1/site/import/all-api-hub", append(append([]gin.HandlerFunc(nil), chain...), importAllAPIHub)...)

	admin := op.UserGet()
	token, _, err := serverauth.GenerateJWTToken(60, admin.ID, model.UserRoleAdmin)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return engine, token
}

// countingReader 记录上游实际被读走多少字节。
// 这是本组测试的核心观测点：闸门生效意味着服务端**停止读取**，
// 而不仅仅是"最后返回了 413"。闸门若被挪到读取之后，字节数会暴涨。
type countingReader struct {
	remaining int64
	read      int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > r.remaining {
		n = r.remaining
	}
	for i := int64(0); i < n; i++ {
		p[i] = 'a'
	}
	r.remaining -= n
	r.read += n
	return int(n), nil
}

// 超限的 raw JSON 体必须被拒成 413，且服务端读入量被闸门截断，
// 不能把整个 body 吞进内存。
func TestSiteImportRejectsOversizedRawBody(t *testing.T) {
	engine, token := newSiteImportTestEngine(t)
	const limit int64 = 4 << 10
	setSiteImportLimitForTest(t, limit, 1<<10)

	// 上传量远大于上限：闸门有效时读入应停在 limit+extra 附近，
	// 而不是 upload 的全量。
	const upload = 4 << 20
	body := &countingReader{remaining: upload}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/site/import/metapi", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1 // chunked：迫使服务端真去读，而不是靠 Content-Length 早退
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	// 副作用断言：闸门必须真的截断读取。放宽到 4 倍上限以容纳 bufio 预读，
	// 但仍远小于 4MiB 上传量——闸门被删或后移时读入会达到 upload 量级。
	if body.read > (limit+1<<10)*4 {
		t.Fatalf("server read %d bytes, want <= %d (limit not enforced during read)",
			body.read, (limit+1<<10)*4)
	}
	if body.read >= upload {
		t.Fatalf("server read the entire %d-byte upload; body limit is not enforced", upload)
	}
}

// ★ 灰区：体积超过 maxSiteImportBytes 但仍在 MaxBytesReader 上限
// （limit + multipartExtra）以内。两道闸门上限不同，故这段区间**只有**
// readAllSiteImportLimited 里的 LimitedReader 能拦下。
//
// 这个测试是 M4 变异（拆掉 LimitedReader 只留 MaxBytesReader）第一轮存活后补的：
// 原先几个测试都用远超两道上限的体积（4MiB vs 5KiB），MaxBytesReader 单独就够，
// 所以整个第二道防线可以被删除而无人察觉。多档上限的修复必须为**每一档**留断言。
func TestSiteImportRejectsSizeInGapBetweenBothLimits(t *testing.T) {
	engine, token := newSiteImportTestEngine(t)
	const limit int64 = 1 << 10
	const extra int64 = 1 << 20
	setSiteImportLimitForTest(t, limit, extra)

	// 8 KiB：> limit(1KiB)，≪ limit+extra(≈1MiB) ⇒ MaxBytesReader 不触发。
	const gray = 8 << 10
	body := &countingReader{remaining: gray}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/site/import/metapi", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; a body of %d bytes exceeds the %d-byte limit "+
			"even though it stays under the MaxBytesReader ceiling (%d); body=%s",
			rec.Code, http.StatusRequestEntityTooLarge, gray, limit, limit+extra, rec.Body.String())
	}
}

// 恰好等于上限的合法体必须放行（反向守卫）：证明闸门不是"一律拒绝"，
// 否则把上限误写成 0 或把判断写成 >= 都会照绿。
func TestSiteImportAcceptsBodyExactlyAtLimit(t *testing.T) {
	engine, token := newSiteImportTestEngine(t)

	payload := []byte(`{"accounts":[]}`)
	setSiteImportLimitForTest(t, int64(len(payload)), 1<<10)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/site/import/metapi", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	// 该体积合法，故不能是 413。业务上因为 accounts 为空会返 400，
	// 这正是"通过了体积闸门、进到了解析层"的证据。
	if rec.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("body exactly at limit was rejected as too large; body=%s", rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (empty accounts payload); body=%s",
			rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "metapi") {
		t.Fatalf("expected the payload to reach the import parser, got body=%s", rec.Body.String())
	}
}

// multipart 路径：超限必须拒，且**不留下溢写临时文件**。
// 临时文件残留是这条路径独有的副作用——生产 /tmp 是 tmpfs，残留即占内存。
func TestSiteImportRejectsOversizedMultipartAndLeavesNoTempFiles(t *testing.T) {
	engine, token := newSiteImportTestEngine(t)
	const limit int64 = 4 << 10
	setSiteImportLimitForTest(t, limit, 1<<10)

	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	t.Setenv("TMP", tmpDir)
	t.Setenv("TEMP", tmpDir)

	// ★ 观测点必须是"上游实际被读走多少字节"，不能只看状态码，也不能只看
	// 临时文件残留：
	//   - 只看状态码：闸门被删后 fileHeader.Size 检查照样返 413（变异存活）；
	//   - 只看临时文件：小于 gin 默认 32MB 内存阈值时压根不溢写，两种情况都是 0；
	//     且成功路径的 defer 清理也会把残留擦掉。
	// 唯一能区分"闸门是否在解析之前生效"的是读入量——闸门在前则读取被截断，
	// 闸门被删或后移则整个 multipart 体已被 FormFile 吞完。
	// 这两条是 M2/M3 变异第一轮存活后补上的。
	const upload = 2 << 20
	prefix, suffix, contentType := buildMultipartFrame(t, "sites.json")
	body := &multipartStreamReader{
		prefix: prefix,
		filler: &countingReader{remaining: upload},
		suffix: suffix,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/site/import/all-api-hub", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = -1 // chunked：迫使服务端边读边解析
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if body.filler.read >= upload {
		t.Fatalf("server read the entire %d-byte multipart upload (%d bytes); "+
			"the size gate must take effect before the form is parsed",
			upload, body.filler.read)
	}
	if n, size := countMultipartTempFiles(t, tmpDir); n != 0 {
		t.Fatalf("%d multipart temp file(s) left behind (%d bytes); spill must be cleaned up", n, size)
	}
}

// buildMultipartFrame 生成一个 multipart 体的头尾框架，中间的文件内容留给
// 流式 filler 现填，避免为了造大体积而先在内存里放一整份。
func buildMultipartFrame(t *testing.T, filename string) (prefix, suffix []byte, contentType string) {
	t.Helper()
	var head bytes.Buffer
	w := multipart.NewWriter(&head)
	if _, err := w.CreateFormFile("file", filename); err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	contentType = w.FormDataContentType()
	boundary := w.Boundary()
	if err := w.Close(); err != nil {
		t.Fatalf("writer close: %v", err)
	}
	full := head.Bytes()
	tail := []byte("\r\n--" + boundary + "--\r\n")
	prefix = append([]byte(nil), full[:len(full)-len(tail)]...)
	return prefix, tail, contentType
}

// multipartStreamReader 依次吐出 prefix、filler（可计量的大块内容）、suffix。
type multipartStreamReader struct {
	prefix []byte
	filler *countingReader
	suffix []byte
	stage  int
}

func (r *multipartStreamReader) Read(p []byte) (int, error) {
	switch r.stage {
	case 0:
		if len(r.prefix) == 0 {
			r.stage = 1
			return 0, nil
		}
		n := copy(p, r.prefix)
		r.prefix = r.prefix[n:]
		return n, nil
	case 1:
		n, err := r.filler.Read(p)
		if err == io.EOF {
			r.stage = 2
			return n, nil
		}
		return n, err
	default:
		if len(r.suffix) == 0 {
			return 0, io.EOF
		}
		n := copy(p, r.suffix)
		r.suffix = r.suffix[n:]
		return n, nil
	}
}

// 合法 multipart 走完全程后也不能留临时文件（成功路径的清理守卫）。
func TestSiteImportCleansUpMultipartTempFilesOnSuccessPath(t *testing.T) {
	engine, token := newSiteImportTestEngine(t)
	setSiteImportLimitForTest(t, 64<<20, 1<<20)

	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	t.Setenv("TMP", tmpDir)
	t.Setenv("TEMP", tmpDir)

	// 超过 gin 默认 32MB 内存阈值才会溢写到磁盘；用 33MB 迫使产生临时文件。
	var payload bytes.Buffer
	w := multipart.NewWriter(&payload)
	fw, err := w.CreateFormFile("file", "sites.json")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := io.WriteString(fw, `{"accounts":[]}`); err != nil {
		t.Fatalf("write json: %v", err)
	}
	if _, err := io.Copy(fw, io.LimitReader(&countingReader{remaining: 33 << 20}, 33<<20)); err != nil {
		t.Fatalf("write padding: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("writer close: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/site/import/all-api-hub", bytes.NewReader(payload.Bytes()))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	// 体积在上限内，故必须进到解析层并被拒成 400（padding 不是合法导入 JSON）。
	// ★ 这里必须写死 400，不能只写"不等于 413"：我第一版就是那么写的，
	// 结果路由 404 时也满足，测试假绿。精确期望值才能同时排除 404/500。
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (in-limit but unparsable payload); body=%s",
			rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if n, size := countMultipartTempFiles(t, tmpDir); n != 0 {
		t.Fatalf("%d multipart temp file(s) left behind (%d bytes) after handler returned", n, size)
	}
}

func countMultipartTempFiles(t *testing.T, dir string) (int, int64) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	var n int
	var total int64
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "multipart-") {
			continue
		}
		n++
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	return n, total
}
