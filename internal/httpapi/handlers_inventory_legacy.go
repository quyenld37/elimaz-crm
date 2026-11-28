package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Kho: hàng hoàn của đơn cũ (MO08, kế hoạch M7, Task M7-19 = 264). Quyền inventory.legacy_return.create theo kho:
// tạo kiểm ở kho của yêu cầu, đọc ở kho của phiếu, danh sách chỉ các kho được giao. Audit inventory.legacy_return ghi
// tham chiếu, kho, dòng; không ghi ghi chú (có thể chứa dữ liệu khách).

const permLegacyReturn = identity.Permission("inventory.legacy_return.create")

// vietnam là giờ Việt Nam (UTC+7, không đổi giờ mùa hè): ngày from, to của danh sách tính theo giờ này.
var vietnam = time.FixedZone("ICT", 7*3600) //nolint:gochecknoglobals // múi giờ cố định, chỉ đọc

// CreateLegacyReturn (POST /api/v1/inventory/legacy-returns).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateLegacyReturn(ctx context.Context, req *gen.CreateLegacyReturnRequest, _ gen.CreateLegacyReturnParams) (gen.CreateLegacyReturnRes, error) {
	in := inventory.LegacyReturnInput{
		WarehouseID: req.WarehouseId, Reference: req.Reference, Note: req.Note.Value,
		Lines: make([]inventory.LegacyLine, 0, len(req.Lines)),
	}
	for _, l := range req.Lines {
		in.Lines = append(in.Lines, inventory.LegacyLine{VariantID: l.VariantId, Qty: l.Qty, Condition: string(l.Condition)})
	}
	out, err := h.inventoryWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w inventory.Writer, u identity.AuthUser) (any, error) {
		if err := canAt(u, in.WarehouseID, permLegacyReturn); err != nil {
			return nil, err
		}
		r, err := w.CreateLegacyReturn(ctx, tx, in, staffActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		if err := h.audit(ctx, tx, u, "inventory.legacy_return", "inventory.legacy_return", r.ID, nil, legacyAudit(r)); err != nil {
			return nil, err
		}
		return legacyOut(ctx, tx, r)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.LegacyReturn), nil //nolint:forcetypeassert // fn trên trả *gen.LegacyReturn
}

// ListLegacyReturns (GET /api/v1/inventory/legacy-returns): theo khoảng ngày giờ Việt Nam [from 00:00, hết ngày to),
// mới nhất trước; không có warehouseId → mọi kho người gọi được giao; có mà không có quyền ở kho đó → 403.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListLegacyReturns(ctx context.Context, params gen.ListLegacyReturnsParams) (gen.ListLegacyReturnsRes, error) {
	f, err := legacyFilter(params)
	if err != nil {
		return nil, err
	}
	var page *gen.LegacyReturnPage
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) error {
		page = nil // read có thể chạy lại: gán lại mỗi lần
		if params.WarehouseId.Set {
			if err := canAt(u, params.WarehouseId.Value, permLegacyReturn); err != nil {
				return err
			}
			f.WarehouseIDs = []uuid.UUID{params.WarehouseId.Value}
		} else {
			f.AllWarehouses, f.WarehouseIDs = readableWarehouses(u, permLegacyReturn)
		}
		page, err = legacyPage(ctx, tx, f)
		return err
	}); err != nil {
		return nil, fmt.Errorf("đọc danh sách hàng hoàn: %w", err)
	}
	return page, nil
}

// legacyFilter đọc khoảng ngày (giờ Việt Nam: from 00:00 tới hết ngày to; from sau to → 400), con trỏ, limit.
func legacyFilter(params gen.ListLegacyReturnsParams) (inventory.LegacyReturnFilter, error) {
	f := inventory.LegacyReturnFilter{Limit: params.Limit.Or(defaultDocLimit)}
	if d, ok := params.From.Get(); ok {
		from := vnDayStart(d)
		f.From = &from
	}
	if d, ok := params.To.Get(); ok {
		to := vnDayStart(d).AddDate(0, 0, 1)
		f.To = &to
	}
	if f.From != nil && f.To != nil && !f.From.Before(*f.To) {
		return f, fmt.Errorf("khoảng ngày hàng hoàn: %w", inventory.ErrInvalidInput)
	}
	cursor, err := idCursor(params.Cursor, inventory.ErrInvalidInput)
	f.Cursor = cursor
	return f, err
}

// legacyPage đọc một trang theo f kèm tên người tạo.
func legacyPage(ctx context.Context, tx pgx.Tx, f inventory.LegacyReturnFilter) (*gen.LegacyReturnPage, error) {
	rs, next, err := inventory.ListLegacyReturns(ctx, tx, f)
	if err != nil {
		return nil, err //nolint:wrapcheck // inventory đã bọc
	}
	ids := make([]uuid.UUID, 0, len(rs))
	for _, r := range rs {
		ids = append(ids, r.CreatedBy)
	}
	names, err := staffNames(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	page := &gen.LegacyReturnPage{Items: make([]gen.LegacyReturn, 0, len(rs))}
	for _, r := range rs {
		page.Items = append(page.Items, *legacyGen(r, names))
	}
	if next != nil {
		page.NextCursor = gen.NewOptString(next.String())
	}
	return page, nil
}

// GetLegacyReturn (GET /api/v1/inventory/legacy-returns/{id}): quyền ở kho của phiếu; không có → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetLegacyReturn(ctx context.Context, params gen.GetLegacyReturnParams) (gen.GetLegacyReturnRes, error) {
	var out *gen.LegacyReturn
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) error {
		r, err := inventory.GetLegacyReturn(ctx, tx, params.ID)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của inventory
		}
		if err := canAt(u, r.WarehouseID, permLegacyReturn); err != nil {
			return err
		}
		out, err = legacyOut(ctx, tx, r)
		return err
	}); err != nil {
		return nil, fmt.Errorf("đọc hàng hoàn: %w", err)
	}
	return out, nil
}

// vnDayStart: 00:00 giờ Việt Nam của ngày d (ogen đọc format: date thành 00:00 UTC của ngày đó).
func vnDayStart(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, vietnam)
}

// legacyAudit: tham chiếu, kho, dòng; không ghi chú.
func legacyAudit(r inventory.LegacyReturn) map[string]any {
	lines := make([]map[string]any, 0, len(r.Lines))
	for _, l := range r.Lines {
		lines = append(lines, map[string]any{"variantId": l.VariantID.String(), "qty": l.Qty, "condition": l.Condition})
	}
	m := map[string]any{
		"reference": r.Reference, "warehouseId": r.WarehouseID.String(), "lines": lines,
		"stockDocumentId": r.StockDocumentID.String(), "damageDocumentId": nil,
	}
	if r.DamageDocumentID != nil {
		m["damageDocumentId"] = r.DamageDocumentID.String()
	}
	return m
}

// legacyOut: phiếu r kèm tên người tạo.
func legacyOut(ctx context.Context, tx pgx.Tx, r inventory.LegacyReturn) (*gen.LegacyReturn, error) {
	names, err := staffNames(ctx, tx, []uuid.UUID{r.CreatedBy})
	if err != nil {
		return nil, err
	}
	return legacyGen(r, names), nil
}

func legacyGen(r inventory.LegacyReturn, names map[uuid.UUID]string) *gen.LegacyReturn {
	out := &gen.LegacyReturn{
		ID: r.ID, WarehouseId: r.WarehouseID, Reference: r.Reference, StockDocumentId: r.StockDocumentID,
		CreatedBy: gen.Actor{ID: r.CreatedBy, Name: names[r.CreatedBy]}, CreatedAt: r.CreatedAt,
		Lines: make([]gen.LegacyReturnLine, 0, len(r.Lines)),
	}
	if r.DamageDocumentID != nil {
		out.DamageDocumentId = gen.NewOptUUID(*r.DamageDocumentID)
	}
	for _, l := range r.Lines {
		out.Lines = append(out.Lines, gen.LegacyReturnLine{
			VariantId: l.VariantID, Qty: l.Qty, Condition: gen.LegacyReturnCondition(l.Condition),
		})
	}
	return out
}
