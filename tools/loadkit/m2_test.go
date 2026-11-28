package main

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/db"
)

// seedAdminUnits chèn tỉnh, xã thử của bài lên đơn (customer.ValidateUnits nhận cặp đó), chạy lại không lỗi.
func TestSeedAdminUnits(t *testing.T) {
	owner := openOwnerDB(t)
	for range 2 {
		if err := seedAdminUnits(t.Context(), owner); err != nil {
			t.Fatal(err)
		}
	}
	if err := owner.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
		_, err := customer.ValidateUnits(t.Context(), tx, m2ProvinceCode, m2WardCode)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Errorf("cặp tỉnh, xã thử: %v", err)
	}
}
