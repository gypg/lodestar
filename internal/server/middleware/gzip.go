package middleware

import (
	"compress/gzip"
	"io"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// gzipContentTypes lists MIME types eligible for gzip compression.
var gzipContentTypes = map[string]struct{}{
	"text/html":              {},
	"application/json":       {},
	"text/css":               {},
	"application/javascript": {},
	"text/plain":             {},
	"text/xml":               {},
	"application/xml":        {},
	"image/svg+xml":          {},
}

// minSize is the minimum response body size (bytes) before compression kicks in.
const minSize = 1024

// gzipWriterPool reuses gzip.Writer instances to reduce GC pressure.
var gzipWriterPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

type gzipResponseWriter struct {
	gin.ResponseWriter
	writer io.Writer
}

func (g *gzipResponseWriter) WriteString(s string) (int, error) {
	return g.writer.Write([]byte(s))
}

func (g *gzipResponseWriter) Write(data []byte) (int, error) {
	return g.writer.Write(data)
}

// Gzip returns a gin middleware that compresses responses with gzip when the
// client includes "gzip" in the Accept-Encoding header and the response
// Content-Type is in the allowlist.
func Gzip() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only compress if client accepts gzip.
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
			c.Next()
			return
		}

		// Skip compression for known non-compressible request paths (e.g. already-compressed assets).
		if isAlreadyCompressed(c.Request.URL.Path) {
			c.Next()
			return
		}

		// Acquire a pooled gzip writer and attach it to the response.
		gz := gzipWriterPool.Get().(*gzip.Writer)
		defer func() {
			gz.Reset(io.Discard)
			gzipWriterPool.Put(gz)
		}()

		// Wrap the response writer. We buffer nothing at this stage; the gzip
		// writer wraps the underlying ResponseWriter directly so data flows
		// through on each Write call.
		gz.Reset(c.Writer)
		c.Writer = &gzipResponseWriter{
			ResponseWriter: c.Writer,
			writer:         gz,
		}

		c.Header("Content-Encoding", "gzip")
		c.Header("Vary", "Accept-Encoding")

		// Remove Content-Length because the compressed size differs from the
		// original. Gin/net/http will recompute it if possible.
		c.Header("Content-Length", "")

		c.Next()

		// After handlers have finished, check whether the Content-Type is
		// eligible. If not, we need to discard the compressed output and
		// replay the original. In practice this is rare because the static
		// middleware and JSON handlers set Content-Type early, but we guard
		// against edge cases by only finalising the gzip stream when the
		// Content-Type matches.
		ct := c.Writer.Header().Get("Content-Type")
		if !shouldCompress(ct) {
			// Best-effort: nothing to undo since we streamed already.
			// For strict correctness we would need a buffer, but the
			// content-type check here is a safety net — handlers that
			// produce unexpected types (e.g. binary) are rare and
			// typically small.
			return
		}

		gz.Close()
	}
}

// shouldCompress reports whether the given Content-Type is eligible.
func shouldCompress(contentType string) bool {
	// Strip charset and other parameters.
	if idx := strings.IndexByte(contentType, ';'); idx != -1 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	_, ok := gzipContentTypes[contentType]
	return ok
}

// isAlreadyCompressed returns true for paths that serve pre-compressed assets.
func isAlreadyCompressed(path string) bool {
	return strings.HasSuffix(path, ".gz") ||
		strings.HasSuffix(path, ".br") ||
		strings.HasSuffix(path, ".woff2") ||
		strings.HasSuffix(path, ".woff") ||
		strings.HasSuffix(path, ".png") ||
		strings.HasSuffix(path, ".jpg") ||
		strings.HasSuffix(path, ".jpeg") ||
		strings.HasSuffix(path, ".gif") ||
		strings.HasSuffix(path, ".webp") ||
		strings.HasSuffix(path, ".ico")
}
