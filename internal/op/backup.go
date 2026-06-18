package op

import (
	"context"

	"github.com/gypg/lodestar/internal/op/backup"
	"github.com/gypg/lodestar/internal/model"
)

// Deprecated: Use backup.ExportAll from internal/op/backup instead.
func DBExportAll(ctx context.Context, includeLogs, includeStats bool) (*model.DBDump, error) {
	return backup.ExportAll(ctx, includeLogs, includeStats)
}

// Deprecated: Use backup.ImportIncremental from internal/op/backup instead.
func DBImportIncremental(ctx context.Context, dump *model.DBDump) (*model.DBImportResult, error) {
	return backup.ImportIncremental(ctx, dump)
}
