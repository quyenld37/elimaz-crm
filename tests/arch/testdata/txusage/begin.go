// Tệp mẫu cho txusage_test: mở transaction ngoài db.RetryTx (phải bị báo).
package sample

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func open(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
