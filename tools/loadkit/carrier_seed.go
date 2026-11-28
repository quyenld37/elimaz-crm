package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// seedCarrierAccount (chủ bảng, public API của module): danh mục hành chính có tỉnh, phường thử
// (customer.EnsureFixtureAdminUnits), tài khoản hãng giả fake-1 của người bán mặc định ở kho wh, tuyến webhook của nó
// và hai mã địa giới đã khớp với hãng giả (fulfillment.EnsureSeedLocations). Trả tài khoản, khóa tuyến.
func seedCarrierAccount(ctx context.Context, owner *db.DB, tenantID, wh uuid.UUID) (uuid.UUID, string, error) {
	var (
		account uuid.UUID
		route   string
	)
	err := tenancy.WithTenant(ctx, owner, tenantID, func(tx pgx.Tx) error {
		if _, conflicts, err := customer.EnsureFixtureAdminUnits(ctx, tx, []customer.AdminUnit{
			{Code: carrierProvince, Level: customer.AdminProvince, Name: "Thành phố Hồ Chí Minh"},
			{Code: carrierWard, Level: customer.AdminWard, ParentCode: carrierProvince, Name: "Phường Sài Gòn"},
		}); err != nil || len(conflicts) > 0 {
			return fmt.Errorf("danh mục hành chính thử (mã trùng khác nội dung: %v): %w", conflicts, err)
		}
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // đã bọc trong catalog
		}
		if account, err = fulfillment.EnsureCarrierAccount(ctx, tx, fulfillment.SeedAccountInput{
			SellerID: seller, WarehouseID: wh, CarrierKey: "fake", Name: "Hãng giả", ExternalAccountRef: "fake-1",
		}); err != nil {
			return err //nolint:wrapcheck // đã bọc trong fulfillment
		}
		if route, err = fulfillment.EnsureAccountRoute(ctx, tx, account); err != nil {
			return err //nolint:wrapcheck // đã bọc trong fulfillment
		}
		_, err = fulfillment.EnsureSeedLocations(ctx, tx, "fake", []fulfillment.SeedLocation{
			{Code: carrierProvince, Name: "TP Hồ Chí Minh"}, {Code: carrierWard, Name: "Phường Sài Gòn", Ward: true},
		})
		return err //nolint:wrapcheck // đã bọc trong fulfillment
	})
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("tài khoản hãng giả thử: %w", err)
	}
	return account, route, nil
}

// awaitTrackings chờ (tối đa wait) mọi vận đơn ids chờ thu gom có mã hãng; trả mã hãng theo thứ tự của ids.
func awaitTrackings(ctx context.Context, owner *db.DB, tenantID uuid.UUID, ids []uuid.UUID, wait time.Duration) ([]string, error) {
	deadline := time.Now().Add(wait)
	for {
		out, err := readyTrackings(ctx, owner, tenantID, ids)
		if err != nil {
			return nil, err
		}
		if len(out) == len(ids) {
			return out, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: sau %v mới có %d / %d vận đơn chờ thu gom", errCarrierSeed, wait, len(out), len(ids))
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("chờ vận đơn: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

// readyTrackings đọc (fulfillment.Get, chủ bảng trong tenant) mã hãng của các vận đơn đầu của ids đã chờ thu gom có mã
// hãng, dừng ở vận đơn đầu tiên chưa sẵn sàng.
func readyTrackings(ctx context.Context, owner *db.DB, tenantID uuid.UUID, ids []uuid.UUID) ([]string, error) {
	var out []string
	err := tenancy.WithTenant(ctx, owner, tenantID, func(tx pgx.Tx) error {
		out = make([]string, 0, len(ids))
		for _, id := range ids {
			sh, err := fulfillment.Get(ctx, tx, id)
			if err != nil {
				return err //nolint:wrapcheck // bọc ở dưới
			}
			if sh.Status != fulfillment.StatusAwaitingPickup || sh.TrackingCode == "" {
				return nil
			}
			out = append(out, sh.TrackingCode)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc vận đơn: %w", err)
	}
	return out, nil
}
