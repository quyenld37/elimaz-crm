package cash_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// TestCheckSeller: bảng kê (theo tài khoản hãng), tài khoản hãng, nghĩa vụ hoàn (theo đơn) của người bán mình → nil;
// của người bán khác hay mã lạ → sentinel "không có" của loại (ADR-29, Task 531); loại lạ → lỗi khác sentinel.
func TestCheckSeller(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	type owned struct{ statement, account, obligation uuid.UUID }
	of := map[uuid.UUID]owned{}
	for _, s := range []uuid.UUID{e.shop.Seller, e.shop.Partner} {
		o := owned{account: e.shop.AccountOf(s)}
		var ord order.Order
		if err := e.app(func(tx pgx.Tx) (err error) {
			ord, err = e.shop.Order(t.Context(), tx, s, e.user, 0, order.ItemInput{VariantID: e.variants[s], Qty: 1})
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			if err := tx.QueryRow(t.Context(), `INSERT INTO cash.cod_statements (tenant_id, carrier_key, carrier_account_id,
				file_id, file_sha256, format, status, imported_by)
				VALUES ($1, $2, $3, gen_random_uuid(), md5(random()::text) || md5(random()::text), 'elimaz_v1', 'parsed', $4)
				RETURNING id`, e.tenant, shiptest.CarrierKey, o.account, e.user).Scan(&o.statement); err != nil {
				return err //nolint:wrapcheck // test
			}
			return tx.QueryRow(t.Context(), `INSERT INTO cash.refund_obligations (tenant_id, order_id, source, amount)
				VALUES ($1, $2, 'order_cancel', 1000) RETURNING id`, e.tenant, ord.ID).Scan(&o.obligation)
		})
		of[s] = o
	}
	mine, other := of[e.shop.Seller], of[e.shop.Partner]
	cases := []struct {
		kind        cash.ScopeKind
		mine, other uuid.UUID
		hidden      error
	}{
		{cash.ScopeStatement, mine.statement, other.statement, cash.ErrStatementNotFound},
		{cash.ScopeCarrierAccount, mine.account, other.account, cash.ErrCarrierAccountNotFound},
		{cash.ScopeRefundObligation, mine.obligation, other.obligation, cash.ErrObligationNotFound},
	}
	check := func(kind cash.ScopeKind, id uuid.UUID) error {
		return e.app(func(tx pgx.Tx) error { return cash.CheckSeller(t.Context(), tx, e.shop.Seller, kind, id) })
	}
	for _, c := range cases {
		if err := check(c.kind, c.mine); err != nil {
			t.Errorf("%s của người bán mình: %v", c.kind, err)
		}
		for name, id := range map[string]uuid.UUID{"người bán khác": c.other, "mã lạ": uuid.New()} {
			if err := check(c.kind, id); !errors.Is(err, c.hidden) {
				t.Errorf("%s %s: %v, muốn %v", c.kind, name, err, c.hidden)
			}
		}
	}
	if err := check("la", mine.statement); err == nil || errors.Is(err, cash.ErrStatementNotFound) {
		t.Errorf("loại lạ: %v", err)
	}
}
