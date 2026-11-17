// Tệp mẫu cho dynsql_test: SQL ngoài sqlc và ngoài *_dynsql.go (phải bị báo).
package sample

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func count(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var n int
	err := pool.QueryRow(ctx, "SELECT count(*) FROM platform.tenants").Scan(&n)
	return n, err
}
