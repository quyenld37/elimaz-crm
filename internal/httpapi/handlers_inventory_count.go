package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Kho: đếm cộng dồn nhiều người, nhập số đếm bằng Excel (Task M7-18 = 263). Quyền inventory.stocktake ở kho của phiên
// (guard kiểm có grant; handler kiểm đích bằng stocktakeAt trong cùng transaction). Chỉ phiên mở sổ (phiên thường của
// M1 đếm bằng recordStocktakeCount). Chống trùng, lỗi nghiệp vụ không lưu, audit: inventoryWrite, h.audit như kiểm kho.

// IncrementStocktakeLine (POST /api/v1/inventory/stocktakes/{id}/increments, inventory.stocktake ở kho của phiên):
// cộng hoặc trừ số đếm của một mã quét.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) IncrementStocktakeLine(ctx context.Context, req *gen.IncrementStocktakeLineRequest,
	params gen.IncrementStocktakeLineParams,
) (gen.IncrementStocktakeLineRes, error) {
	out, err := h.inventoryWrite(ctx, http.StatusOK, func(tx pgx.Tx, _ inventory.Writer, u identity.AuthUser) (any, error) {
		if _, _, err := stocktakeAt(ctx, tx, u, params.ID, permStocktake); err != nil {
			return nil, err
		}
		v, n, err := inventory.IncrementLine(ctx, tx, params.ID, req.Code, req.By, staffActor(u), time.Now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		return &gen.StocktakeIncrementResult{VariantId: v, Counted: n}, h.audit(ctx, tx, u, "inventory.stocktake.increment",
			stocktakeEntity, params.ID, nil, map[string]any{"variantId": v.String(), "by": req.By, "counted": n})
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.StocktakeIncrementResult), nil //nolint:forcetypeassert // fn trên trả *gen.StocktakeIncrementResult
}

// CreateStocktakeCountImport (POST …/stocktakes/{id}/count-imports, inventory.stocktake ở kho của phiên): đọc, băm và
// phân tích tệp ngoài transaction (ADR-46), rồi ghi bản nhập và xếp việc nền → 202. Cùng tệp replace lần hai → bản cũ;
// add lần hai → 409 import_already_applied.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateStocktakeCountImport(ctx context.Context, req *gen.CreateStocktakeCountImportRequest,
	params gen.CreateStocktakeCountImportParams,
) (gen.CreateStocktakeCountImportRes, error) {
	u, ok := authUser(ctx)
	switch {
	case !ok:
		return nil, errNoAuthUser
	case h.d.Files == nil:
		return nil, errNoFileStore
	}
	f, err := inventory.PrepareCountImport(ctx, h.d.DB, h.d.Files, u.TenantID, req.FileId)
	if err != nil {
		return nil, fmt.Errorf("đọc tệp số đếm: %w", err)
	}
	out, err := h.inventoryWrite(ctx, http.StatusAccepted, func(tx pgx.Tx, _ inventory.Writer, u identity.AuthUser) (any, error) {
		if _, _, err := stocktakeAt(ctx, tx, u, params.ID, permStocktake); err != nil {
			return nil, err
		}
		imp, created, err := inventory.CreateCountImport(ctx, tx, queue.NewQueue(), params.ID, f, string(req.Mode), staffActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		if !created {
			return countImportOut(imp), nil
		}
		return countImportOut(imp), h.audit(ctx, tx, u, "inventory.count_import.create", stocktakeEntity, params.ID, nil,
			map[string]any{"importId": imp.ID.String(), "fileId": imp.FileID.String(), "mode": imp.Mode})
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.StocktakeCountImport), nil //nolint:forcetypeassert // fn trên trả *gen.StocktakeCountImport
}

// GetStocktakeCountImport (GET /api/v1/inventory/count-imports/{id}, inventory.stocktake ở kho của phiên của bản nhập).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetStocktakeCountImport(ctx context.Context, params gen.GetStocktakeCountImportParams) (gen.GetStocktakeCountImportRes, error) {
	var imp inventory.CountImport
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		if imp, err = inventory.GetCountImport(ctx, tx, params.ID); err != nil {
			return err //nolint:wrapcheck // sentinel của inventory
		}
		_, _, err = stocktakeAt(ctx, tx, u, imp.StocktakeID, permStocktake)
		return err
	}); err != nil {
		return nil, fmt.Errorf("đọc bản nhập số đếm: %w", err)
	}
	return countImportOut(imp), nil
}

func countImportOut(imp inventory.CountImport) *gen.StocktakeCountImport {
	out := &gen.StocktakeCountImport{
		ID: imp.ID, StocktakeId: imp.StocktakeID, Mode: gen.CountImportMode(imp.Mode),
		Status: gen.BackgroundImportStatus(imp.Status), AppliedRows: count32(imp.AppliedRows), CreatedAt: imp.CreatedAt,
		ErrorRows: make([]gen.CountImportRowError, 0, len(imp.ErrorRows)),
	}
	for _, e := range imp.ErrorRows {
		out.ErrorRows = append(out.ErrorRows, gen.CountImportRowError{Row: count32(e.Row), Code: gen.CountImportRowErrorCode(e.Code)})
	}
	if imp.FinishedAt != nil {
		out.FinishedAt = gen.NewOptDateTime(*imp.FinishedAt)
	}
	return out
}
