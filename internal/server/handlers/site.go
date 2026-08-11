package handlers

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/apperror"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
	sitesvc "github.com/gypg/lodestar/internal/site"
	"github.com/gypg/lodestar/internal/sitesync"
	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/gypg/lodestar/internal/utils/safe"
	"github.com/gypg/lodestar/internal/utils/xurl"
)

func refreshAccountRandomCheckinScheduleBestEffort(ctx context.Context, accountID int) {
	if err := sitesvc.RefreshAccountRandomCheckinSchedule(ctx, accountID); err != nil {
		log.Warnf("failed to refresh random checkin schedule (account=%d): %v", accountID, err)
	}
}

func init() {
	router.NewGroupRouter("/api/v1/site").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermSitesRead)).
		AddRoute(router.NewRoute("/list", http.MethodGet).Handle(listSite)).
		AddRoute(router.NewRoute("/archived", http.MethodGet).Handle(listArchivedSites)).
		AddRoute(router.NewRoute("/import/all-api-hub", http.MethodPost).
			Use(middleware.RequirePermission(auth.PermSitesWrite)).
			Handle(importAllAPIHub)).
		AddRoute(router.NewRoute("/import/metapi", http.MethodPost).
			Use(middleware.RequirePermission(auth.PermSitesWrite)).
			Handle(importMetAPI)).
		AddRoute(router.NewRoute("/account/sync/:id", http.MethodPost).
			Use(middleware.RequirePermission(auth.PermSitesWrite)).
			Handle(syncSiteAccount)).
		AddRoute(router.NewRoute("/account/checkin/:id", http.MethodPost).
			Use(middleware.RequirePermission(auth.PermSitesWrite)).
			Handle(checkinSiteAccount)).
		AddRoute(router.NewRoute("/sync-all", http.MethodPost).
			Use(middleware.RequirePermission(auth.PermSitesWrite)).
			Handle(syncAllSiteAccounts)).
		AddRoute(router.NewRoute("/checkin-all", http.MethodPost).
			Use(middleware.RequirePermission(auth.PermSitesWrite)).
			Handle(checkinAllSiteAccounts)).
		AddRoute(router.NewRoute("/:id/available-models", http.MethodGet).Handle(getSiteAvailableModels))

	router.NewGroupRouter("/api/v1/site").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermSitesWrite)).
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/create", http.MethodPost).Handle(createSite)).
		AddRoute(router.NewRoute("/update", http.MethodPost).Handle(updateSite)).
		AddRoute(router.NewRoute("/enable", http.MethodPost).Handle(enableSite)).
		AddRoute(router.NewRoute("/detect", http.MethodPost).Handle(detectSitePlatform)).
		AddRoute(router.NewRoute("/batch", http.MethodPost).Handle(batchSite)).
		AddRoute(router.NewRoute("/account/create", http.MethodPost).Handle(createSiteAccount)).
		AddRoute(router.NewRoute("/account/update", http.MethodPost).Handle(updateSiteAccount)).
		AddRoute(router.NewRoute("/account/enable", http.MethodPost).Handle(enableSiteAccount))

	router.NewGroupRouter("/api/v1/site").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermSitesWrite)).
		AddRoute(router.NewRoute("/delete/:id", http.MethodDelete).Handle(deleteSite)).
		AddRoute(router.NewRoute("/archive/:id", http.MethodPost).Handle(archiveSite)).
		AddRoute(router.NewRoute("/restore/:id", http.MethodPost).Handle(restoreSite)).
		AddRoute(router.NewRoute("/account/delete/:id", http.MethodDelete).Handle(deleteSiteAccount))
}

func listSite(c *gin.Context) {
	sites, err := op.SiteList(c.Request.Context())
	if err != nil {
		log.Errorf("listSite failed: %v", err)
		resp.InternalError(c)
		return
	}
	// Mask account credentials for ALL roles — the edit dialog fetches the
	// single-site endpoint for raw values; the list never needs plaintext.
	for i := range sites {
		maskSiteAccountCredentials(sites[i].Accounts)
	}
	if isViewerRole(c.GetString("user_role")) {
		redactSiteBaseURLsForViewer(sites)
	}
	resp.Success(c, sites)
}

func importAllAPIHub(c *gin.Context) {
	defer cleanupSiteImportMultipartForm(c)
	body, err := readImportPayload(c)
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusBadRequest, err)
		return
	}

	result, syncAccountIDs, err := op.SiteImportAllAPIHub(c.Request.Context(), body)
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusBadRequest, err)
		return
	}

	if len(syncAccountIDs) > 0 {
		ids := append([]int(nil), syncAccountIDs...)
		safe.Go("site-import-sync", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			sitesvc.SyncAccountsWithOptions(ctx, ids, sitesync.SiteBatchOptions{Trigger: sitesync.SiteBatchTriggerImport})
		})
	}

	resp.Success(c, result)
}

func importMetAPI(c *gin.Context) {
	defer cleanupSiteImportMultipartForm(c)
	body, err := readImportPayload(c)
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusBadRequest, err)
		return
	}

	result, err := op.SiteImportMetAPI(c.Request.Context(), body)
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusBadRequest, err)
		return
	}

	resp.Success(c, result)
}

// S-7：站点导入原先两条路径都无体积上限——raw JSON 直接 io.ReadAll(Body)，
// multipart 走 gin 默认 32MB 内存 + 溢写 TMPDIR 后再 io.ReadAll 整份进堆。
// 实测 256MB 请求体 → 堆涨 256MB；128MB multipart → 堆 128MB + tmpfs 128MB
// 共 256MB 峰值，而容器上限 512MiB 且 /tmp 是 tmpfs（占的也是内存）。
// 再叠加 json.Unmarshal 进 map[string]any 的约 4.5 倍放大，单个请求即可 OOM。
// 上限与 DB 导入（maxDBImportBytes）取齐，multipart 多留一份 MIME 框架余量。
var (
	maxSiteImportBytes               int64 = 64 << 20
	maxSiteImportMultipartExtraBytes int64 = 1 << 20
)

func readImportPayload(c *gin.Context) ([]byte, error) {
	contentType := c.GetHeader("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		// 闸门必须在 c.FormFile 之前——FormFile 内部会解析整个 multipart 体，
		// 一旦解析完成内存与临时文件都已经吃下去了，之后再判大小为时已晚。
		limitSiteImportRequestBody(c)
		fileHeader, err := c.FormFile("file")
		if err != nil {
			return nil, normalizeSiteImportReadError(err)
		}
		if fileHeader.Size > maxSiteImportBytes {
			return nil, newSiteImportTooLargeError()
		}
		file, err := fileHeader.Open()
		if err != nil {
			return nil, apperror.Wrap(op.CodeSiteImportEmptyPayload, "site import empty payload", err).WithStatus(http.StatusBadRequest)
		}
		defer file.Close()
		return readAllSiteImportLimited(file)
	}
	limitSiteImportRequestBody(c)
	return readAllSiteImportLimited(c.Request.Body)
}

func limitSiteImportRequestBody(c *gin.Context) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSiteImportBytes+maxSiteImportMultipartExtraBytes)
}

// readAllSiteImportLimited 读满 maxSiteImportBytes+1 字节即判超限。
// 多读 1 字节是为了区分"恰好等于上限"（合法）与"超过上限"（拒绝）。
func readAllSiteImportLimited(r io.Reader) ([]byte, error) {
	limited := &io.LimitedReader{R: r, N: maxSiteImportBytes + 1}
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, normalizeSiteImportReadError(err)
	}
	if limited.N <= 0 {
		return nil, newSiteImportTooLargeError()
	}
	return body, nil
}

func normalizeSiteImportReadError(err error) error {
	if err == nil {
		return nil
	}
	if isHTTPMaxBytesError(err) {
		return newSiteImportTooLargeError()
	}
	return apperror.Wrap(op.CodeSiteImportEmptyPayload, "site import empty payload", err).WithStatus(http.StatusBadRequest)
}

func newSiteImportTooLargeError() error {
	return op.NewSiteImportTooLargeError(formatDBImportLimit(maxSiteImportBytes))
}

// cleanupSiteImportMultipartForm 主动删掉 multipart 溢写的临时文件。
// net/http 在请求收尾时也会 RemoveAll（server.go:1683），但那要等 handler
// 返回并走完响应写出；导入这条路径随后还要做 JSON 解析与整库事务，
// 期间临时文件一直占着 /tmp（生产是 tmpfs，即内存）。提前释放。
func cleanupSiteImportMultipartForm(c *gin.Context) {
	if c == nil || c.Request == nil || c.Request.MultipartForm == nil {
		return
	}
	_ = c.Request.MultipartForm.RemoveAll()
}

func createSite(c *gin.Context) {
	var site model.Site
	if err := c.ShouldBindJSON(&site); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := site.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := op.SiteCreate(&site, c.Request.Context()); err != nil {
		log.Errorf("createSite failed: %v", err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, site)
}

func updateSite(c *gin.Context) {
	var req model.SiteUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	site, err := op.SiteUpdate(&req, c.Request.Context())
	if err != nil {
		log.Errorf("updateSite failed: %v", err)
		resp.InternalError(c)
		return
	}
	siteID := site.ID
	safe.Go("site-update-project", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := sitesvc.ProjectSite(ctx, siteID); err != nil {
			log.Warnf("background ProjectSite failed (site=%d): %v", siteID, err)
		}
	})
	resp.Success(c, site)
}

func enableSite(c *gin.Context) {
	var request struct {
		ID      int  `json:"id"`
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.SiteEnabled(request.ID, request.Enabled, c.Request.Context()); err != nil {
		log.Errorf("enableSite failed (id=%d): %v", request.ID, err)
		resp.InternalError(c)
		return
	}
	siteID := request.ID
	safe.Go("site-enable-project", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := sitesvc.ProjectSite(ctx, siteID); err != nil {
			log.Warnf("background ProjectSite failed (site=%d): %v", siteID, err)
		}
	})
	resp.Success(c, nil)
}

func deleteSite(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	if err := sitesvc.DeleteSite(c.Request.Context(), idNum); err != nil {
		log.Errorf("deleteSite failed (id=%d): %v", idNum, err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func archiveSite(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	if err := sitesvc.ArchiveSite(c.Request.Context(), idNum); err != nil {
		log.Errorf("archiveSite failed (id=%d): %v", idNum, err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func restoreSite(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	if err := sitesvc.RestoreSite(c.Request.Context(), idNum); err != nil {
		log.Errorf("restoreSite failed (id=%d): %v", idNum, err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func listArchivedSites(c *gin.Context) {
	sites, err := sitesvc.ListArchivedSites(c.Request.Context())
	if err != nil {
		log.Errorf("listArchivedSites failed: %v", err)
		resp.InternalError(c)
		return
	}
	if isViewerRole(c.GetString("user_role")) {
		redactSiteBaseURLsForViewer(sites)
	}
	resp.Success(c, sites)
}

func createSiteAccount(c *gin.Context) {
	var account model.SiteAccount
	if err := c.ShouldBindJSON(&account); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := account.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := op.SiteAccountCreate(&account, c.Request.Context()); err != nil {
		log.Errorf("createSiteAccount failed: %v", err)
		resp.InternalError(c)
		return
	}
	refreshAccountRandomCheckinScheduleBestEffort(c.Request.Context(), account.ID)
	createdAccount, err := op.SiteAccountGet(account.ID, c.Request.Context())
	if err != nil {
		log.Errorf("createSiteAccount: re-fetch failed (id=%d): %v", account.ID, err)
		resp.InternalError(c)
		return
	}
	accountID := account.ID
	if account.Enabled && account.AutoSync {
		safe.Go("site-account-create-sync", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if _, err := sitesvc.SyncAccount(ctx, accountID); err != nil {
				log.Debugf("background SyncAccount failed (account=%d): %v", accountID, err)
			}
		})
	}
	resp.Success(c, createdAccount)
}

func updateSiteAccount(c *gin.Context) {
	var req model.SiteAccountUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	account, err := op.SiteAccountUpdate(&req, c.Request.Context())
	if err != nil {
		log.Errorf("updateSiteAccount failed: %v", err)
		resp.InternalError(c)
		return
	}
	refreshAccountRandomCheckinScheduleBestEffort(c.Request.Context(), account.ID)
	account, err = op.SiteAccountGet(account.ID, c.Request.Context())
	if err != nil {
		log.Errorf("updateSiteAccount: re-fetch failed (id=%d): %v", account.ID, err)
		resp.InternalError(c)
		return
	}
	accountID := account.ID
	autoSync := account.AutoSync
	safe.Go("site-account-update-project-sync", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if _, err := sitesvc.ProjectAccount(ctx, accountID); err != nil {
			log.Warnf("background ProjectAccount failed (account=%d): %v", accountID, err)
		}
		if autoSync {
			if _, err := sitesvc.SyncAccount(ctx, accountID); err != nil {
				log.Debugf("background SyncAccount failed (account=%d): %v", accountID, err)
			}
		}
	})
	resp.Success(c, account)
}

func enableSiteAccount(c *gin.Context) {
	var request struct {
		ID      int  `json:"id"`
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.SiteAccountEnabled(request.ID, request.Enabled, c.Request.Context()); err != nil {
		log.Errorf("enableSiteAccount failed (id=%d): %v", request.ID, err)
		resp.InternalError(c)
		return
	}
	refreshAccountRandomCheckinScheduleBestEffort(c.Request.Context(), request.ID)
	accountID := request.ID
	safe.Go("site-account-enable-project", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if _, err := sitesvc.ProjectAccount(ctx, accountID); err != nil {
			log.Warnf("background ProjectAccount failed (account=%d): %v", accountID, err)
		}
	})
	resp.Success(c, nil)
}

func deleteSiteAccount(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	if err := sitesvc.DeleteSiteAccount(c.Request.Context(), idNum); err != nil {
		log.Errorf("deleteSiteAccount failed (id=%d): %v", idNum, err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func syncSiteAccount(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	result, err := sitesvc.SyncAccount(c.Request.Context(), idNum)
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, err)
		return
	}
	resp.Success(c, result)
}

func checkinSiteAccount(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	result, err := sitesvc.CheckinAccount(c.Request.Context(), idNum)
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, err)
		return
	}
	resp.Success(c, result)
}

func syncAllSiteAccounts(c *gin.Context) {
	safe.Go("site-sync-all", func() {
		sitesvc.SyncAllWithOptions(context.Background(), sitesync.SiteBatchOptions{Trigger: sitesync.SiteBatchTriggerManual})
	})
	resp.Success(c, nil)
}

func checkinAllSiteAccounts(c *gin.Context) {
	safe.Go("site-checkin-all", func() {
		sitesvc.CheckinAllWithOptions(context.Background(), sitesync.SiteBatchOptions{Trigger: sitesync.SiteBatchTriggerManual})
	})
	resp.Success(c, nil)
}

func detectSitePlatform(c *gin.Context) {
	var request struct {
		URL string `json:"url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	// URL 直接来自请求体，服务器会据此抓取页面探测平台，必须做 SSRF 防护。
	if err := xurl.AssertSafeURL(request.URL); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	platform, err := sitesvc.DetectPlatform(ctx, request.URL)
	if err != nil {
		log.Errorf("detectSitePlatform failed (url=%s): %v", request.URL, err)
		resp.Error(c, http.StatusBadRequest, "Failed to detect site platform")
		return
	}
	resp.Success(c, gin.H{"platform": platform})
}

func batchSite(c *gin.Context) {
	var req model.SiteBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	validActions := map[string]bool{
		"enable": true, "disable": true, "delete": true,
	}
	if !validActions[req.Action] {
		resp.Error(c, http.StatusBadRequest, "invalid action")
		return
	}
	if len(req.IDs) == 0 {
		resp.Error(c, http.StatusBadRequest, "ids is required")
		return
	}

	result := model.SiteBatchResult{
		SuccessIDs:  make([]int, 0),
		FailedItems: make([]model.SiteBatchFailure, 0),
	}
	ctx := c.Request.Context()

	for _, id := range req.IDs {
		var batchErr error
		switch req.Action {
		case "enable":
			batchErr = op.SiteEnabled(id, true, ctx)
		case "disable":
			batchErr = op.SiteEnabled(id, false, ctx)
		case "delete":
			batchErr = sitesvc.DeleteSite(ctx, id)
		}
		if batchErr != nil {
			result.FailedItems = append(result.FailedItems, model.SiteBatchFailure{ID: id, Message: batchErr.Error()})
		} else {
			result.SuccessIDs = append(result.SuccessIDs, id)
		}
	}

	// Project affected sites asynchronously
	if req.Action == "enable" || req.Action == "disable" {
		for _, id := range result.SuccessIDs {
			siteID := id
			safe.Go("site-batch-project", func() {
				projCtx, projCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer projCancel()
				if err := sitesvc.ProjectSite(projCtx, siteID); err != nil {
					log.Warnf("background ProjectSite failed (site=%d): %v", siteID, err)
				}
			})
		}
	}

	resp.Success(c, result)
}

func getSiteAvailableModels(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	models, err := op.SiteAvailableModels(idNum, c.Request.Context())
	if err != nil {
		log.Errorf("getSiteAvailableModels failed (id=%d): %v", idNum, err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, gin.H{"site_id": idNum, "models": models})
}
