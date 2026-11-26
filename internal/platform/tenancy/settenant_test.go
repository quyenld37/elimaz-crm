package tenancy_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// SetTenant đổi tenant giữa transaction (việc nền toàn hệ thống): mỗi lần chỉ thấy dòng của tenant đang đặt; uuid.Nil
// bỏ đặt (0 dòng, không lỗi); không rò sang transaction sau trên cùng kết nối.
func TestSetTenantSwitchesWithinTx(t *testing.T) {
	t.Parallel()
	e := newEnv(t, db.PoolOptions{MaxConns: 1})
	probeRLS(t, e.owner)
	a, b := uuid.New(), uuid.New()
	seedParent(t, e.app, a, "a1")
	seedParent(t, e.app, b, "b1")
	var seenA, seenB, seenNone []string
	if err := e.app.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
		var err error
		if err = tenancy.SetTenant(t.Context(), tx, a); err != nil {
			return err //nolint:wrapcheck // test
		}
		if seenA, err = parentNames(t.Context(), tx); err != nil {
			return err
		}
		if err = tenancy.SetTenant(t.Context(), tx, b); err != nil {
			return err //nolint:wrapcheck // test
		}
		if seenB, err = parentNames(t.Context(), tx); err != nil {
			return err
		}
		if err = tenancy.SetTenant(t.Context(), tx, uuid.Nil); err != nil {
			return err //nolint:wrapcheck // test
		}
		seenNone, err = parentNames(t.Context(), tx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seenA, []string{"a1"}) || !slices.Equal(seenB, []string{"b1"}) || len(seenNone) != 0 {
		t.Errorf("thấy A=%v B=%v rỗng=%v", seenA, seenB, seenNone)
	}
	var after []string
	if err := e.app.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) (err error) {
		return tenancy.SetTenant(t.Context(), tx, a)
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.app.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) (err error) {
		after, err = parentNames(t.Context(), tx)
		return err
	}); err != nil || len(after) != 0 {
		t.Errorf("transaction sau thấy %v, %v; muốn rỗng", after, err)
	}
}
