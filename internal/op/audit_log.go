package op

import (
	"context"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/audit"
)

// Deprecated: Use audit.Create from internal/op/audit instead.
func AuditLogCreate(ctx context.Context, entry *model.AuditLog) error {
	return audit.Create(ctx, entry)
}

// Deprecated: Use audit.List from internal/op/audit instead.
func AuditLogList(ctx context.Context, page, pageSize int) ([]model.AuditLog, error) {
	return audit.List(ctx, page, pageSize)
}

// Deprecated: Use audit.GetByID from internal/op/audit instead.
func AuditLogGetByID(ctx context.Context, id int64) (*model.AuditLog, error) {
	return audit.GetByID(ctx, id)
}
