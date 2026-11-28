package main

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// carrierseed trên PostgreSQL thật (SQL chỉ ở phần kiểm của test): tài khoản hãng giả, tuyến, hai mã địa giới khớp sẵn; chạy lại cùng tài khoản,
// cùng tuyến, không thêm dòng. Chờ vận đơn: 0 vận đơn trả ngay; vận đơn không có → lỗi ErrNotFound.
func TestCarrierSeedAccount(t *testing.T) {
	t.Parallel()
	owner := openOwnerDB(t)
	ctx := t.Context()
	tenantID, err := tenancy.SeedTenant(ctx, owner, loadTenantName)
	if err != nil {
		t.Fatal(err)
	}
	wh, err := seedShop(ctx, owner, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	acc, route, err := seedCarrierAccount(ctx, owner, tenantID, wh)
	if err != nil || route == "" {
		t.Fatalf("lần đầu: %v %q %v", acc, route, err)
	}
	again, route2, err := seedCarrierAccount(ctx, owner, tenantID, wh)
	if err != nil || again != acc || route2 != route {
		t.Fatalf("chạy lại: %v %q %v, muốn %v %q", again, route2, err, acc, route)
	}
	var accounts, locations int
	if err := tenancy.WithTenant(ctx, owner, tenantID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM fulfillment.carrier_accounts WHERE carrier_key = 'fake'
			AND status = 'active'`).Scan(&accounts); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM fulfillment.carrier_locations WHERE carrier_key = 'fake'
			AND our_code IN ($1, $2)`, carrierProvince, carrierWard).Scan(&locations)
	}); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 || locations != 2 {
		t.Fatalf("%d tài khoản, %d mã địa giới; muốn 1, 2", accounts, locations)
	}
	if got, err := awaitTrackings(ctx, owner, tenantID, nil, time.Millisecond); err != nil || len(got) != 0 {
		t.Fatalf("chờ 0 vận đơn: %v %v", got, err)
	}
	if _, err := awaitTrackings(ctx, owner, tenantID, []uuid.UUID{uuid.New()}, time.Millisecond); err == nil ||
		!errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("vận đơn không có: %v, muốn ErrNotFound", err)
	}
}
