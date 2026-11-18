package db

import (
	"context"
	"io/fs"
	"time"
)

// Sleep cho test gọi thẳng lúc chờ giữa hai lần chạy của RetryTx.
func Sleep(ctx context.Context, d time.Duration) error { return sleep(ctx, d) }

// MigrateFS cho test chạy Migrate trên một bộ tệp khác migrations.FS.
func MigrateFS(ctx context.Context, ownerURL string, fsys fs.FS) ([]string, error) {
	return migrate(ctx, ownerURL, fsys)
}
