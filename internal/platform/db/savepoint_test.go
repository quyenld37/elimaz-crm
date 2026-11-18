package db_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
)

// Savepoint: fn lỗi (kể cả lỗi SQL làm transaction hỏng) → chỉ phần trong savepoint lùi, transaction dùng tiếp được
// và phần ghi trước savepoint còn; fn không lỗi → phần trong savepoint ở lại sau commit.
func TestSavepoint(t *testing.T) {
	d, owner := openApp(t, db.PoolOptions{})
	probe(t, owner)
	errBoom := errors.New("lỗi của hàm")
	var inner error
	err := d.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), "INSERT INTO probe.items VALUES (1)"); err != nil {
			return err //nolint:wrapcheck // test
		}
		inner = db.Savepoint(t.Context(), tx, func(sp pgx.Tx) error {
			if _, err := sp.Exec(t.Context(), "INSERT INTO probe.items VALUES (2)"); err != nil {
				return err //nolint:wrapcheck // test
			}
			_, err := sp.Exec(t.Context(), "INSERT INTO probe.items VALUES (2)") // trùng khóa: transaction hỏng
			return errors.Join(errBoom, err)
		})
		if err := db.Savepoint(t.Context(), tx, func(sp pgx.Tx) error {
			_, err := sp.Exec(t.Context(), "INSERT INTO probe.items VALUES (3)")
			return err //nolint:wrapcheck // test
		}); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), "INSERT INTO probe.items VALUES (4)")
		return err //nolint:wrapcheck // test
	})
	if err != nil || !errors.Is(inner, errBoom) {
		t.Fatalf("RetryTx: %v, lỗi trong savepoint: %v", err, inner)
	}
	var ids []int32
	rows, err := d.Pool.Query(t.Context(), "SELECT id FROM probe.items ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	if ids, err = pgx.CollectRows(rows, pgx.RowTo[int32]); err != nil || len(ids) != 3 || ids[0] != 1 || ids[1] != 3 ||
		ids[2] != 4 {
		t.Fatalf("còn %v (%v), muốn [1 3 4]", ids, err)
	}
}
