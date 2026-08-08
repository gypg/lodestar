package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
	_ "github.com/gypg/lodestar/internal/server/handlers"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/gypg/lodestar/static"
)

var httpSrv http.Server

func Start() error {
	if conf.IsDebug() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()

	// S-3：gin 默认信任全部代理（0.0.0.0/0 + ::/0），任何直连客户端都能用
	// X-Forwarded-For 改写 c.ClientIP()，绕过 API key 的 IP 白名单、按 IP 的
	// 登录/邮件验证码限流与 Turnstile 校验。必须显式收窄。
	// 配错 CIDR 时 gin 会把已解析的前几条留在 trustedCIDRs 里（gin.go:452-456
	// 无条件赋值后才返回 error），所以这里必须 fail-closed，不能只记日志。
	if err := r.SetTrustedProxies(conf.TrustedProxies()); err != nil {
		return fmt.Errorf("invalid server.trusted_proxies: %w", err)
	}

	r.Use(gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		resp.Error(c, http.StatusInternalServerError, resp.ErrInternalServer)
		c.Abort()
	}))

	if conf.IsDebug() {
		r.Use(middleware.Logger())
	}
	r.Use(middleware.SecurityHeaders())
	r.Use(middleware.Cors())
	r.Use(middleware.MaintenanceGuard())
	r.Use(middleware.AuditManagementWrite())
	if localStaticDir, ok := resolveLocalStaticDir(); ok {
		log.Infof("serving frontend static assets from local directory: %s", localStaticDir)
		r.Use(middleware.StaticLocal("/", localStaticDir))
	} else if static.StaticFS != nil {
		r.Use(middleware.StaticEmbed("/", static.StaticFS))
	} else {
		log.Warnf("frontend static assets are not embedded; API endpoints remain available, but the management UI requires building the web app first")
	}

	if err := router.RegisterAll(r); err != nil {
		return fmt.Errorf("register routes: %w", err)
	}

	httpSrv.Addr = fmt.Sprintf("%s:%d", conf.AppConfig.Server.Host, conf.AppConfig.Server.Port)
	httpSrv.Handler = r
	ln, err := net.Listen("tcp", httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", httpSrv.Addr, err)
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("http server panic recovered: %v", r)
			}
		}()
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Errorf("http server listen and serve error: %v", err)
		}
	}()
	return nil
}

func Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return httpSrv.Shutdown(ctx)
}

// ListenSignal waits for SIGINT/SIGTERM and then calls Close for graceful shutdown.
func ListenSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Infof("received signal: %v, shutting down gracefully", sig)
	if err := Close(); err != nil {
		log.Errorf("shutdown error: %v", err)
	}
}

func resolveLocalStaticDir() (string, bool) {
	if !conf.IsDebug() {
		return "", false
	}

	for _, dir := range []string{"web/out", "static/out"} {
		indexPath := filepath.Join(dir, "index.html")
		if info, err := os.Stat(indexPath); err == nil && !info.IsDir() {
			return dir, true
		}
	}

	return "", false
}
