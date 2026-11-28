package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Đợt flash sale của nhân viên (kế hoạch M5, Task 199): đọc promotion.flash.read, ghi promotion.flash.manage (bảng
// route). Soạn ở trạng thái nháp (Task 199); lên lịch, thêm suất, đóng, số liệu đang chạy (Task 200). Audit ghi ở đây, cùng tx (promotion không nhập identity).

// ListFlashSales (GET /api/v1/flash-sales).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListFlashSales(ctx context.Context, params gen.ListFlashSalesParams) (gen.ListFlashSalesRes, error) {
	after, err := couponCursor(params.Cursor)
	if err != nil {
		return nil, err
	}
	f := promotion.FlashSaleFilter{
		Status: string(params.Status.Value), After: after, Limit: int(params.Limit.Or(defaultDocLimit)),
	}
	var ss []promotion.FlashSale
	var next *uuid.UUID
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		ss, next, err = promotion.ListFlashSales(ctx, tx, f)
		return err //nolint:wrapcheck // promotion đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc danh sách đợt flash sale: %w", err)
	}
	page := &gen.FlashSalePage{Items: make([]gen.FlashSaleSummary, 0, len(ss))}
	for _, s := range ss {
		page.Items = append(page.Items, flashSummaryOut(s))
	}
	if next != nil {
		page.NextCursor = gen.NewOptString(next.String())
	}
	return page, nil
}

// GetFlashSale (GET /api/v1/flash-sales/{flashSaleId}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetFlashSale(ctx context.Context, params gen.GetFlashSaleParams) (gen.GetFlashSaleRes, error) {
	var s promotion.FlashSale
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		s, err = promotion.GetFlashSale(ctx, tx, params.FlashSaleId)
		return err //nolint:wrapcheck // sentinel của promotion
	}); err != nil {
		return nil, fmt.Errorf("đọc đợt flash sale: %w", err)
	}
	return flashSaleOut(s), nil
}

// CreateFlashSale (POST /api/v1/flash-sales) → 201 nháp.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateFlashSale(ctx context.Context, req *gen.FlashSaleInput, _ gen.CreateFlashSaleParams) (gen.CreateFlashSaleRes, error) {
	in := promotion.FlashSaleInput{Name: req.Name, StartsAt: req.StartsAt, EndsAt: req.EndsAt}
	out, err := h.flashWrite(ctx, http.StatusCreated, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		s, err := promotion.CreateFlashSale(ctx, tx, promotionActor(u), in)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		s.Items = []promotion.FlashItem{}
		return flashSaleOut(s), h.audit(ctx, tx, u, "promotion.flash_sale.create", "promotion.flash_sale", s.ID, nil,
			s.AuditFields())
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.FlashSale), nil //nolint:forcetypeassert // fn trên trả *gen.FlashSale
}

// UpdateFlashSale (PATCH /api/v1/flash-sales/{flashSaleId}): trường bỏ trống giữ giá trị hiện tại.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateFlashSale(ctx context.Context, req *gen.UpdateFlashSaleRequest, params gen.UpdateFlashSaleParams) (gen.UpdateFlashSaleRes, error) {
	id := params.FlashSaleId
	out, err := h.flashWrite(ctx, http.StatusOK, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		cur, err := promotion.GetFlashSale(ctx, tx, id)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		in := promotion.FlashSaleInput{
			Name: req.Name.Or(cur.Name), StartsAt: req.StartsAt.Or(cur.StartsAt), EndsAt: req.EndsAt.Or(cur.EndsAt),
		}
		ch, err := promotion.UpdateFlashSale(ctx, tx, id, in, int(req.Version))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		after := ch.After
		after.Items, after.ItemCount, after.Quantity, after.Sold = cur.Items, cur.ItemCount, cur.Quantity, cur.Sold
		return flashSaleOut(after), h.audit(ctx, tx, u, "promotion.flash_sale.update", "promotion.flash_sale", id,
			ch.Before.AuditFields(), ch.After.AuditFields())
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.FlashSale), nil //nolint:forcetypeassert // fn trên trả *gen.FlashSale
}

// AddFlashItem (POST /api/v1/flash-sales/{flashSaleId}/items) → 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AddFlashItem(ctx context.Context, req *gen.FlashItemInput, params gen.AddFlashItemParams) (gen.AddFlashItemRes, error) {
	in := promotion.FlashItemInput{
		VariantID: req.VariantId, SalePrice: int64(req.SalePrice), Quantity: int(req.Quantity),
		PerCustomerLimit: int(req.PerCustomerLimit.Or(1)), HoldMinutes: int(req.HoldMinutes.Or(10)),
	}
	out, err := h.flashWrite(ctx, http.StatusCreated, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		it, err := promotion.AddFlashItem(ctx, tx, params.FlashSaleId, in)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		o := flashItemOut(it)
		return &o, h.audit(ctx, tx, u, "promotion.flash_item.add", "promotion.flash_item", it.ID, nil, it.AuditFields())
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.FlashItem), nil //nolint:forcetypeassert // fn trên trả *gen.FlashItem
}

// UpdateFlashItem (PATCH /api/v1/flash-sales/{flashSaleId}/items/{flashItemId}): trường bỏ trống giữ giá trị hiện tại.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateFlashItem(ctx context.Context, req *gen.UpdateFlashItemRequest, params gen.UpdateFlashItemParams) (gen.UpdateFlashItemRes, error) {
	out, err := h.flashWrite(ctx, http.StatusOK, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		cur, err := flashItemOf(ctx, tx, params.FlashSaleId, params.FlashItemId)
		if err != nil {
			return nil, err
		}
		in := promotion.FlashItemInput{
			SalePrice: int64(req.SalePrice.Or(gen.Money(cur.SalePrice))), Quantity: int(req.Quantity.Or(int32(cur.Quantity))), //nolint:gosec // G115: int4 của bảng
			PerCustomerLimit: int(req.PerCustomerLimit.Or(int32(cur.PerCustomerLimit))), //nolint:gosec // G115: 1–10
			HoldMinutes:      int(req.HoldMinutes.Or(int32(cur.HoldMinutes))),           //nolint:gosec // G115: 5–30
		}
		ch, err := promotion.UpdateFlashItem(ctx, tx, params.FlashSaleId, params.FlashItemId, in, int(req.Version))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		o := flashItemOut(ch.After)
		return &o, h.audit(ctx, tx, u, "promotion.flash_item.update", "promotion.flash_item", ch.After.ID,
			ch.Before.AuditFields(), ch.After.AuditFields())
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.FlashItem), nil //nolint:forcetypeassert // fn trên trả *gen.FlashItem
}

// RemoveFlashItem (DELETE /api/v1/flash-sales/{flashSaleId}/items/{flashItemId}) → đợt sau khi bỏ.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RemoveFlashItem(ctx context.Context, params gen.RemoveFlashItemParams) (gen.RemoveFlashItemRes, error) {
	out, err := h.flashWrite(ctx, http.StatusOK, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		before, err := promotion.RemoveFlashItem(ctx, tx, params.FlashSaleId, params.FlashItemId)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		s, err := promotion.GetFlashSale(ctx, tx, params.FlashSaleId)
		if err != nil {
			return nil, err //nolint:wrapcheck // promotion đã bọc
		}
		return flashSaleOut(s), h.audit(ctx, tx, u, "promotion.flash_item.remove", "promotion.flash_item", before.ID,
			before.AuditFields(), map[string]any{"removed": true})
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.FlashSale), nil //nolint:forcetypeassert // fn trên trả *gen.FlashSale
}

// ScheduleFlashSale (POST /api/v1/flash-sales/{flashSaleId}/schedule): lên lịch đợt nháp, giữ suất mọi món (Task 200).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ScheduleFlashSale(ctx context.Context, req *gen.FlashScheduleRequest, params gen.ScheduleFlashSaleParams) (gen.ScheduleFlashSaleRes, error) {
	id := params.FlashSaleId
	out, err := h.flashWrite(ctx, http.StatusOK, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		ch, err := h.flashWriter().ScheduleFlashSale(ctx, tx, id, int(req.Version), promotionActor(u), h.now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		return h.flashSaleAfter(ctx, tx, u, "promotion.flash_sale.schedule", ch)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.FlashSale), nil //nolint:forcetypeassert // fn trên trả *gen.FlashSale
}

// CloseFlashSale (POST /api/v1/flash-sales/{flashSaleId}/close): đóng tay, suất còn trống trả về tồn (Task 200).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CloseFlashSale(ctx context.Context, params gen.CloseFlashSaleParams) (gen.CloseFlashSaleRes, error) {
	id := params.FlashSaleId
	out, err := h.flashWrite(ctx, http.StatusOK, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		ch, err := h.flashWriter().CloseFlashSale(ctx, tx, id, promotion.FlashClosedManual, promotionActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		return h.flashSaleAfter(ctx, tx, u, "promotion.flash_sale.close", ch)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.FlashSale), nil //nolint:forcetypeassert // fn trên trả *gen.FlashSale
}

// AddFlashSlots (POST /api/v1/flash-sales/{flashSaleId}/items/{flashItemId}/add-slots): thêm suất (Task 200).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AddFlashSlots(ctx context.Context, req *gen.FlashAddSlotsRequest, params gen.AddFlashSlotsParams) (gen.AddFlashSlotsRes, error) {
	out, err := h.flashWrite(ctx, http.StatusOK, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		ch, err := h.flashWriter().AddSlots(ctx, tx, params.FlashSaleId, params.FlashItemId, int(req.Add), promotionActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		after, err := flashItemOf(ctx, tx, params.FlashSaleId, params.FlashItemId)
		if err != nil {
			return nil, err
		}
		o := flashItemOut(after)
		return &o, h.audit(ctx, tx, u, "promotion.flash_item.add_slots", "promotion.flash_item", after.ID,
			ch.Before.AuditFields(), after.AuditFields())
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.FlashItem), nil //nolint:forcetypeassert // fn trên trả *gen.FlashItem
}

// FlashSaleLive (GET /api/v1/flash-sales/{flashSaleId}/live): số liệu đang chạy của từng món (Task 200).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) FlashSaleLive(ctx context.Context, params gen.FlashSaleLiveParams) (gen.FlashSaleLiveRes, error) {
	var l promotion.FlashLive
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		l, err = promotion.Live(ctx, tx, params.FlashSaleId)
		return err //nolint:wrapcheck // sentinel của promotion
	}); err != nil {
		return nil, fmt.Errorf("đọc số liệu đợt flash sale: %w", err)
	}
	out := &gen.FlashLive{Items: make([]gen.FlashLiveItem, 0, len(l.Items))}
	for _, it := range l.Items {
		out.Items = append(out.Items, gen.FlashLiveItem{
			ItemId: it.ItemID, Quantity: int32(it.Quantity), Available: int32(it.Available), Held: int32(it.Held), //nolint:gosec // G115: ≤ 20000
			Sold: int32(it.Sold), Released: int32(it.Released), Buyers: int32(it.Buyers), //nolint:gosec // G115: ≤ 20000
		})
	}
	return out, nil
}

// flashWriter là đường ghi vòng đời đợt: sự kiện đợt và stock.changed qua sổ sự kiện, báo hiệu số dư.
func (h *handler) flashWriter() promotion.Writer {
	return promotion.Writer{Realtime: h.d.Realtime, Events: h.d.Events, Queue: queue.NewQueue()}
}

// flashSaleAfter đọc lại đợt sau đổi trạng thái (kèm món) và ghi audit action (trước, sau).
func (h *handler) flashSaleAfter(ctx context.Context, tx pgx.Tx, u identity.AuthUser, action string,
	ch promotion.FlashSaleChange,
) (any, error) {
	s, err := promotion.GetFlashSale(ctx, tx, ch.After.ID)
	if err != nil {
		return nil, err //nolint:wrapcheck // promotion đã bọc
	}
	return flashSaleOut(s), h.audit(ctx, tx, u, action, "promotion.flash_sale", s.ID, ch.Before.AuditFields(),
		ch.After.AuditFields())
}

// flashItemOf là món itemID của đợt saleID (đọc cả đợt); không có → promotion.ErrNotFound.
func flashItemOf(ctx context.Context, tx pgx.Tx, saleID, itemID uuid.UUID) (promotion.FlashItem, error) {
	s, err := promotion.GetFlashSale(ctx, tx, saleID)
	if err != nil {
		return promotion.FlashItem{}, err //nolint:wrapcheck // sentinel của promotion
	}
	for _, it := range s.Items {
		if it.ID == itemID {
			return it, nil
		}
	}
	return promotion.FlashItem{}, promotion.ErrNotFound
}

// flashWrite chạy change trong refusableWrite (lỗi nghiệp vụ của promotion không lưu vào khóa chống trùng).
func (h *handler) flashWrite(ctx context.Context, status int,
	change func(tx pgx.Tx, u identity.AuthUser) (any, error),
) (any, error) {
	return h.refusableWrite(ctx, status, isPromotionRefusal, "ghi đợt flash sale", change)
}

func flashSummaryOut(s promotion.FlashSale) gen.FlashSaleSummary {
	out := gen.FlashSaleSummary{
		ID: s.ID, Name: s.Name, StartsAt: s.StartsAt, EndsAt: s.EndsAt, Status: gen.FlashSaleStatus(s.Status),
		Version: int32(s.Version), ItemCount: int32(s.ItemCount), CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, //nolint:gosec // G115: int4 của bảng
		Quantity: int32(s.Quantity), Sold: int32(s.Sold), //nolint:gosec // G115: tổng int4 của câu SQL
	}
	if s.ClosedReason != "" {
		out.ClosedReason = gen.NewOptString(s.ClosedReason)
	}
	return out
}

func flashSaleOut(s promotion.FlashSale) *gen.FlashSale {
	out := &gen.FlashSale{Sale: flashSummaryOut(s), Items: make([]gen.FlashItem, 0, len(s.Items))}
	for _, it := range s.Items {
		out.Items = append(out.Items, flashItemOut(it))
	}
	return out
}

func flashItemOut(it promotion.FlashItem) gen.FlashItem {
	return gen.FlashItem{
		ID: it.ID, VariantId: it.VariantID, SellerId: it.SellerID, Sku: it.SKU, Name: it.Name,
		ListPrice: gen.Money(it.ListPrice), SalePrice: gen.Money(it.SalePrice), Quantity: int32(it.Quantity), //nolint:gosec // G115: int4 của bảng
		PerCustomerLimit: int32(it.PerCustomerLimit), HoldMinutes: int32(it.HoldMinutes), Version: int32(it.Version), //nolint:gosec // G115: int4 của bảng
		Sellable: int32(it.Sellable), //nolint:gosec // G115: available là int4 của số dư
	}
}
