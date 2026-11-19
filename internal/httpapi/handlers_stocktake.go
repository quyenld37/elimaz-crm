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
	"elimaz.com.vn/crm/internal/modules/order"
)

// Kiểm kho (kế hoạch M1, Task M1-35). Quyền inventory.stocktake, inventory.stocktake.apply, inventory.read theo kho:
// guard kiểm có grant; handler kiểm đích bằng canAt — kho của yêu cầu (mở phiên) hoặc kho của phiên (đọc trong cùng
// transaction). Chống trùng, lỗi nghiệp vụ không lưu, audit: dùng inventoryWrite, audit của handlers_inventory.go.

const (
	permStocktake      = identity.Permission("inventory.stocktake")
	permStocktakeApply = identity.Permission("inventory.stocktake.apply")
	stocktakeEntity    = "inventory.stocktake"
)

// stocktakeAt đọc phiên id kèm dòng (trong tx) và kiểm u có mọi quyền perms ở kho của phiên.
func stocktakeAt(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID, perms ...identity.Permission,
) (inventory.Stocktake, []inventory.StocktakeLine, error) {
	s, ls, err := inventory.GetStocktake(ctx, tx, id)
	if err != nil {
		return inventory.Stocktake{}, nil, err //nolint:wrapcheck // sentinel của inventory
	}
	return s, ls, canAt(u, s.WarehouseID, perms...)
}

// StartStocktake (POST /api/v1/inventory/stocktakes, inventory.stocktake ở kho của yêu cầu) → 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) StartStocktake(ctx context.Context, req *gen.StartStocktakeRequest, _ gen.StartStocktakeParams) (gen.StartStocktakeRes, error) {
	out, err := h.inventoryWrite(ctx, http.StatusCreated, func(tx pgx.Tx, _ inventory.Writer, u identity.AuthUser) (any, error) {
		if err := canAt(u, req.WarehouseId, permStocktake); err != nil {
			return nil, err
		}
		s, err := inventory.StartStocktake(ctx, tx, req.WarehouseId, staffActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		return stocktakeOut(s, nil), h.audit(ctx, tx, u, "inventory.stocktake.start", stocktakeEntity, s.ID, nil, stocktakeAudit(s, 0))
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Stocktake), nil //nolint:forcetypeassert // fn trên trả *gen.Stocktake
}

// RecordStocktakeCount (PUT …/stocktakes/{id}/lines/{variantId}, inventory.stocktake ở kho của phiên).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RecordStocktakeCount(ctx context.Context, req *gen.RecordStocktakeCountRequest,
	params gen.RecordStocktakeCountParams,
) (gen.RecordStocktakeCountRes, error) {
	var lineVersion *int64
	if req.LineVersion.Set {
		lineVersion = &req.LineVersion.Value
	}
	out, err := h.inventoryWrite(ctx, http.StatusOK, func(tx pgx.Tx, _ inventory.Writer, u identity.AuthUser) (any, error) {
		if _, _, err := stocktakeAt(ctx, tx, u, params.ID, permStocktake); err != nil {
			return nil, err
		}
		l, err := inventory.RecordCount(ctx, tx, params.ID, params.VariantId, req.Counted, lineVersion, staffActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		return stocktakeLineOut(l), h.audit(ctx, tx, u, "inventory.stocktake.count", stocktakeEntity, params.ID, nil,
			map[string]any{
				"variantId": l.VariantID.String(), "counted": l.Counted, "expectedOnShelf": l.ExpectedOnShelf,
				"lineVersion": l.Version,
			})
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.StocktakeLine), nil //nolint:forcetypeassert // fn trên trả *gen.StocktakeLine
}

// ApplyStocktake (POST …/stocktakes/{id}/apply, inventory.stocktake.apply ở kho của phiên): cộng chênh lệch vào tồn.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ApplyStocktake(ctx context.Context, req *gen.VersionRequest, params gen.ApplyStocktakeParams) (gen.ApplyStocktakeRes, error) {
	out, err := h.inventoryWrite(ctx, http.StatusOK, func(tx pgx.Tx, w inventory.Writer, u identity.AuthUser) (any, error) {
		cur, ls, err := stocktakeAt(ctx, tx, u, params.ID, permStocktakeApply)
		if err != nil {
			return nil, err
		}
		r, err := w.ApplyStocktake(ctx, tx, params.ID, req.Version, staffActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		res := &gen.ApplyStocktakeResult{
			Stocktake: *stocktakeOut(r.Stocktake, nil), Adjusted: int32(r.Adjusted), //nolint:gosec // tối đa số dòng của một phiên
			Shortages: make([]gen.StocktakeShortage, 0, len(r.Shortages)),
		}
		for _, s := range r.Shortages {
			orders, err := shortOrders(ctx, tx, s)
			if err != nil {
				return nil, err
			}
			res.Shortages = append(res.Shortages, gen.StocktakeShortage{
				VariantId: s.VariantID, Available: s.Available, Orders: orders,
			})
		}
		after := stocktakeAudit(r.Stocktake, len(ls))
		after["adjusted"], after["shortages"] = r.Adjusted, len(r.Shortages)
		return res, h.audit(ctx, tx, u, "inventory.stocktake.apply", stocktakeEntity, params.ID, stocktakeAudit(cur, len(ls)), after)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ApplyStocktakeResult), nil //nolint:forcetypeassert // fn trên trả *gen.ApplyStocktakeResult
}

// CancelStocktake (POST …/stocktakes/{id}/cancel, inventory.stocktake ở kho của phiên): hủy phiên đang mở.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CancelStocktake(ctx context.Context, req *gen.VersionRequest, params gen.CancelStocktakeParams) (gen.CancelStocktakeRes, error) {
	out, err := h.inventoryWrite(ctx, http.StatusOK, func(tx pgx.Tx, _ inventory.Writer, u identity.AuthUser) (any, error) {
		cur, ls, err := stocktakeAt(ctx, tx, u, params.ID, permStocktake)
		if err != nil {
			return nil, err
		}
		if err := inventory.CancelStocktake(ctx, tx, params.ID, req.Version, staffActor(u)); err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		s, ls2, err := inventory.GetStocktake(ctx, tx, params.ID)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		return stocktakeOut(s, ls2), h.audit(ctx, tx, u, "inventory.stocktake.cancel", stocktakeEntity, s.ID,
			stocktakeAudit(cur, len(ls)), stocktakeAudit(s, len(ls2)))
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Stocktake), nil //nolint:forcetypeassert // fn trên trả *gen.Stocktake
}

// GetStocktake (GET …/stocktakes/{id}, inventory.read ở kho của phiên): phiên kèm dòng đếm và xem trước.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetStocktake(ctx context.Context, params gen.GetStocktakeParams) (gen.GetStocktakeRes, error) {
	var s inventory.Stocktake
	var ls []inventory.StocktakeLine
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		s, ls, err = stocktakeAt(ctx, tx, u, params.ID, permInvRead)
		return err
	}); err != nil {
		return nil, fmt.Errorf("đọc phiên kiểm kho: %w", err)
	}
	return stocktakeOut(s, ls), nil
}

func stocktakeAudit(s inventory.Stocktake, lines int) map[string]any {
	return map[string]any{
		"code": s.Code, "status": s.Status, "warehouseId": s.WarehouseID.String(), "version": s.Version, "lines": lines,
	}
}

func stocktakeOut(s inventory.Stocktake, ls []inventory.StocktakeLine) *gen.Stocktake {
	out := &gen.Stocktake{
		ID: s.ID, Code: s.Code, WarehouseId: s.WarehouseID, Status: gen.StocktakeStatus(s.Status), Version: s.Version,
		CreatedAt: s.CreatedAt, Lines: make([]gen.StocktakeLine, 0, len(ls)),
	}
	if s.AppliedAt != nil {
		out.AppliedAt = gen.NewOptDateTime(*s.AppliedAt)
	}
	for _, l := range ls {
		out.Lines = append(out.Lines, *stocktakeLineOut(l))
	}
	return out
}

func stocktakeLineOut(l inventory.StocktakeLine) *gen.StocktakeLine {
	return &gen.StocktakeLine{
		VariantId: l.VariantID, ExpectedOnShelf: l.ExpectedOnShelf, Counted: l.Counted, Delta: l.Delta,
		CountedAt: l.CountedAt, Version: l.Version, OnHand: l.Current.OnHand, Held: l.Current.Held,
		InTransit: l.Current.InTransit, Damaged: l.Current.Damaged, Available: l.Current.Available,
		AvailableAfter: l.AvailableAfter,
	}
}

// shortOrders gộp các dòng giữ hàng held của biến thể thiếu s theo đơn (Task 406): mã đơn qua order.ItemOrders (người
// bán của biến thể, ADR-29), số lượng cộng theo đơn, giữ thứ tự giữ hàng (đơn giữ trước đứng trước).
func shortOrders(ctx context.Context, tx pgx.Tx, s inventory.Shortage) ([]gen.StocktakeShortOrder, error) {
	items := make([]uuid.UUID, 0, len(s.Holds))
	for _, h := range s.Holds {
		items = append(items, h.OrderItemID)
	}
	refs, err := order.ItemOrders(ctx, tx, s.SellerID, items)
	if err != nil {
		return nil, err //nolint:wrapcheck // lỗi của order đã bọc
	}
	out := make([]gen.StocktakeShortOrder, 0, len(s.Holds))
	at := make(map[uuid.UUID]int, len(s.Holds))
	for _, h := range s.Holds {
		ref, ok := refs[h.OrderItemID]
		if !ok {
			continue // dòng đơn không còn (không xảy ra với giữ hàng held)
		}
		if i, seen := at[ref.OrderID]; seen {
			out[i].Quantity += h.Quantity
			continue
		}
		at[ref.OrderID] = len(out)
		out = append(out, gen.StocktakeShortOrder{OrderId: ref.OrderID, OrderCode: ref.Code, Quantity: h.Quantity})
	}
	return out, nil
}
