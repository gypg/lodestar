package task

import (
	"context"

	"github.com/gypg/lodestar/internal/op/errorlog"
	"github.com/gypg/lodestar/internal/utils/log"
)

// ErrorLogCleanup 周期清理错误日志：超过保留上限（5000 条）时删除最旧一半。
func ErrorLogCleanup() {
	if err := errorlog.Cleanup(context.Background()); err != nil {
		log.Warnf("error log cleanup task failed: %v", err)
	}
}
