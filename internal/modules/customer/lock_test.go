package customer_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// LockRoot(B) với B đã gộp vào A → trả A và giữ khóa dòng A tới hết transaction (transaction khác FOR UPDATE NOWAIT
// dòng A → 55P03); khách không có hay của tenant khác → ErrNotFound.
func TestLockRoot(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a, b := e.pair("Gốc A", "Con B", "")
	e.mustMerge(a.ID, b.ID)
	e.inTx(func(tx pgx.Tx) error {
		root, err := customer.LockRoot(t.Context(), tx, b.ID)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if root != a.ID {
			t.Errorf("LockRoot(B) = %s, mong %s", root, a.ID)
		}
		other := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx2 pgx.Tx) error {
			_, err := tx2.Exec(t.Context(), `SELECT id FROM customer.customers WHERE id = $1 FOR UPDATE NOWAIT`, a.ID)
			return err //nolint:wrapcheck // test
		})
		if pgErr, ok := errors.AsType[*pgconn.PgError](other); !ok || pgErr.Code != "55P03" {
			t.Errorf("dòng A không bị khóa: %v", other)
		}
		return nil
	})
	for name, tenant := range map[string]uuid.UUID{"tenant khác": e.other, "không có": e.tenant} {
		id := a.ID
		if name == "không có" {
			id = uuid.New()
		}
		err := e.try(tenant, func(tx pgx.Tx) error {
			_, err := customer.LockRoot(t.Context(), tx, id)
			return err //nolint:wrapcheck // test
		})
		if !errors.Is(err, customer.ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
