package db

import (
	"context"
	"io/fs"
)

// MigrateFS cho test chạy Migrate trên một bộ tệp khác migrations.FS.
func MigrateFS(ctx context.Context, ownerURL string, fsys fs.FS) ([]string, error) {
	return migrate(ctx, ownerURL, fsys)
}
