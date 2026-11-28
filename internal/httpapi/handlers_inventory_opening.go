package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Kho: phiên kiểm kho mở sổ (MO02, Task M7-17 = 262). Quyền inventory.stocktake.opening (mở, đăng, kiểm lại) và
// inventory.read (đọc, danh sách) theo kho: guard kiểm có grant; handler kiểm đích bằng canAt — kho của yêu cầu (mở)
// hoặc kho của phiên (đọc trong cùng transaction). Đếm dùng recordStocktakeCount của M1. Chống trùng, lỗi nghiệp vụ
// không lưu, audit: inventoryWrite, h.audit như kiểm kho M1.

const permStocktakeOpening = identity.Permission("inventory.stocktake.opening")

// openingAt đọc phiên mở sổ id (trong tx) và kiểm u có quyền perm ở kho của phiên.
func openingAt(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID, perm identity.Permission,
) (inventory.OpeningStatus, error) {
	o, err := inventory.GetOpening(ctx, tx, id)
	if err != nil {
		return inventory.OpeningStatus{}, err //nolint:wrapcheck // sentinel của inventory
	}
	return o, canAt(u, o.WarehouseID, perm)
}

// CreateOpeningStocktake (POST /api/v1/inventory/opening-stocktakes, inventory.stocktake.opening ở kho của yêu cầu) → 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateOpeningStocktake(ctx context.Context, req *gen.CreateOpeningStocktakeRequest,
	_ gen.CreateOpeningStocktakeParams,
) (gen.CreateOpeningStocktakeRes, error) {
	out, err := h.inventoryWrite(ctx, http.StatusCreated, func(tx pgx.Tx, _ inventory.Writer, u identity.AuthUser) (any, error) {
		if err := canAt(u, req.WarehouseId, permStocktakeOpening); err != nil {
			return nil, err
		}
		s, n, err := inventory.CreateOpening(ctx, tx, req.WarehouseId, staffActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		after := stocktakeAudit(s, 0)
		after["linesExpected"] = n
		return &gen.OpeningStocktake{
			StocktakeId: s.ID, WarehouseId: s.WarehouseID, Status: gen.StocktakeStatus(s.Status),
			LinesExpected: int32(n), //nolint:gosec // số biến thể của tenant
		}, h.audit(ctx, tx, u, "inventory.opening.start", stocktakeEntity, s.ID, nil, after)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.OpeningStocktake), nil //nolint:forcetypeassert // fn trên trả *gen.OpeningStocktake
}

// ListOpeningStocktakes (GET /api/v1/inventory/opening-stocktakes, inventory.read): chỉ phiên ở kho người gọi có
// inventory.read; warehouseId lọc một kho.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListOpeningStocktakes(ctx context.Context, params gen.ListOpeningStocktakesParams) (gen.ListOpeningStocktakesRes, error) {
	var wh *uuid.UUID
	if params.WarehouseId.Set {
		wh = &params.WarehouseId.Value
	}
	out := &gen.OpeningStocktakeList{Items: []gen.OpeningStocktake{}}
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) error {
		list, err := inventory.ListOpenings(ctx, tx, wh)
		for _, o := range list {
			if canAt(u, o.WarehouseID, permInvRead) == nil {
				out.Items = append(out.Items, *openingOut(o))
			}
		}
		return err //nolint:wrapcheck // inventory đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc danh sách phiên mở sổ: %w", err)
	}
	return out, nil
}

// GetOpeningStocktake (GET …/opening-stocktakes/{id}, inventory.read ở kho của phiên).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetOpeningStocktake(ctx context.Context, params gen.GetOpeningStocktakeParams) (gen.GetOpeningStocktakeRes, error) {
	var o inventory.OpeningStatus
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		o, err = openingAt(ctx, tx, u, params.ID, permInvRead)
		return err
	}); err != nil {
		return nil, fmt.Errorf("đọc phiên mở sổ: %w", err)
	}
	return openingOut(o), nil
}

// PostOpeningStocktake (POST …/opening-stocktakes/{id}/post, inventory.stocktake.opening ở kho của phiên): tồn mở sổ
// = số đếm, ghi sổ đúng một lần.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) PostOpeningStocktake(ctx context.Context, params gen.PostOpeningStocktakeParams) (gen.PostOpeningStocktakeRes, error) {
	out, err := h.inventoryWrite(ctx, http.StatusOK, func(tx pgx.Tx, w inventory.Writer, u identity.AuthUser) (any, error) {
		if _, err := openingAt(ctx, tx, u, params.ID, permStocktakeOpening); err != nil {
			return nil, err
		}
		r, err := w.PostOpening(ctx, tx, params.ID, staffActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		after := stocktakeAudit(r.Stocktake, r.CountedLines)
		after["linesExpected"], after["units"] = r.LinesExpected, r.Units
		return &gen.OpeningPostResult{
			StocktakeId: params.ID, LinesExpected: int32(r.LinesExpected), //nolint:gosec // số biến thể của tenant
			CountedLines: int32(r.CountedLines), Units: r.Units, //nolint:gosec // số dòng của một phiên
		}, h.audit(ctx, tx, u, "inventory.opening_applied", stocktakeEntity, params.ID, nil, after)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.OpeningPostResult), nil //nolint:forcetypeassert // fn trên trả *gen.OpeningPostResult
}

// CheckOpeningStocktake (GET …/opening-stocktakes/{id}/check, inventory.stocktake.opening ở kho của phiên): số đếm
// khớp sổ và số dư (mốc A của đêm mở).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CheckOpeningStocktake(ctx context.Context, params gen.CheckOpeningStocktakeParams) (gen.CheckOpeningStocktakeRes, error) {
	var c inventory.CheckResult
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		if _, err = openingAt(ctx, tx, u, params.ID, permStocktakeOpening); err != nil {
			return err
		}
		c, err = inventory.CheckOpening(ctx, tx, params.ID)
		return err //nolint:wrapcheck // sentinel của inventory
	}); err != nil {
		return nil, fmt.Errorf("kiểm lại phiên mở sổ: %w", err)
	}
	out := &gen.OpeningCheckResult{Ok: c.OK, ChangedSince: c.ChangedSince, Mismatches: make([]gen.OpeningMismatch, 0, len(c.Mismatches))}
	for _, m := range c.Mismatches {
		out.Mismatches = append(out.Mismatches, gen.OpeningMismatch{
			VariantId: m.VariantID, Sku: m.SKU, Counted: m.Counted, LedgerOpening: m.LedgerOpening, OnHand: m.OnHand,
		})
	}
	return out, nil
}

func openingOut(o inventory.OpeningStatus) *gen.OpeningStocktake {
	out := &gen.OpeningStocktake{
		StocktakeId: o.StocktakeID, WarehouseId: o.WarehouseID, Status: gen.StocktakeStatus(o.Status),
		LinesExpected: o.LinesExpected, LinesCounted: o.LinesCounted, UnitsCounted: o.UnitsCounted,
	}
	if o.AppliedAt != nil {
		out.AppliedAt = gen.NewOptDateTime(*o.AppliedAt)
	}
	return out
}
