package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ensurePlatformSchema tạo schema platform trước khi goose chạy, vì bảng phiên bản của goose nằm trong đó
// (platform.goose_db_version). Câu DDL không đi qua sqlc nên nằm ở tệp *_dynsql.go (ADR-44 chốt 3);
// test trên PostgreSQL thật: migrate_dynsql_test.go, migrate_test.go.
func ensurePlatformSchema(ctx context.Context, conn *pgx.Conn) error {
	if _, err := conn.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS platform"); err != nil {
		return fmt.Errorf("tạo schema platform: %w", err)
	}
	return nil
}
