package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
	"github.com/gypg/lodestar/internal/utils/log"
)

const cspReportMaxBody = 8192 // 8 KiB

func init() {
	router.NewGroupRouter("/api/v1").
		AddRoute(
			router.NewRoute("/csp-report", http.MethodPost).
				Handle(handleCSPReport),
		)
}

// cspViolation mirrors the browser-generated CSP violation report structure.
// The outer object wraps a "csp-report" key per the W3C spec.
type cspViolation struct {
	CSPReport cspReportBody `json:"csp-report"`
}

type cspReportBody struct {
	DocumentURI        string `json:"document-uri"`
	Referrer           string `json:"referrer"`
	BlockedURI         string `json:"blocked-uri"`
	ViolatedDirective  string `json:"violated-directive"`
	EffectiveDirective string `json:"effective-directive"`
	OriginalPolicy     string `json:"original-policy"`
	Disposition        string `json:"disposition"`
	StatusCode         int    `json:"status-code"`
	ScriptSample       string `json:"script-sample"`
	SourceFile         string `json:"source-file"`
	LineNumber         int    `json:"line-number"`
	ColumnNumber       int    `json:"column-number"`
}

func handleCSPReport(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, cspReportMaxBody))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrBadRequest)
		return
	}

	var report cspViolation
	if err := json.Unmarshal(body, &report); err != nil {
		// Some browsers send {"csp-report":null} or plain text; log raw and accept.
		log.Warnw("csp-report: unparseable violation report",
			"raw", string(body),
			"remote_addr", c.ClientIP(),
		)
		resp.Success(c, nil)
		return
	}

	r := report.CSPReport
	log.Warnw("csp-violation",
		"document_uri", r.DocumentURI,
		"blocked_uri", r.BlockedURI,
		"violated_directive", r.ViolatedDirective,
		"effective_directive", r.EffectiveDirective,
		"disposition", r.Disposition,
		"source_file", r.SourceFile,
		"line_number", r.LineNumber,
		"column_number", r.ColumnNumber,
		"script_sample", r.ScriptSample,
		"remote_addr", c.ClientIP(),
	)

	resp.Success(c, nil)
}
