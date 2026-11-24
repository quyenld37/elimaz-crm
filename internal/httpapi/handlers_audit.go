package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
)

// ListAuditLogs (GET /api/v1/identity/audit-logs, quyền audit.read; kế hoạch M5, Task 457): một trang nhật ký thao tác,
// mới nhất trước. Con trỏ hỏng, bộ lọc sai → 400 (identity.ErrInvalidInput). Không trả giá trị trước/sau, IP.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListAuditLogs(ctx context.Context, params gen.ListAuditLogsParams) (gen.ListAuditLogsRes, error) {
	f := identity.AuditFilter{
		Action: params.Action.Or(""), ActorID: params.ActorId.Or(""), EntityType: params.EntityType.Or(""),
		EntityID: params.EntityId.Or(""), From: params.From.Or(time.Time{}), To: params.To.Or(time.Time{}),
	}
	if s, ok := params.Cursor.Get(); ok {
		c, err := identity.ParseAuditCursor(s)
		if err != nil {
			return nil, fmt.Errorf("nhật ký thao tác: %w", err)
		}
		f.After = &c
	}
	out := &gen.AuditLogPage{Items: []gen.AuditLog{}}
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		rows, next, err := identity.ListAuditLogs(ctx, tx, f)
		if err != nil {
			return err //nolint:wrapcheck // lỗi của identity đã bọc
		}
		if err := h.auditEntityNames(ctx, tx, rows); err != nil {
			return err
		}
		out.Items = out.Items[:0]
		for _, r := range rows {
			out.Items = append(out.Items, gen.AuditLog{
				ID: r.ID, ActorType: gen.AuditLogActorType(r.ActorType), ActorId: nonEmpty(r.ActorID),
				ActorName: nonEmpty(r.ActorName), Action: r.Action, EntityType: r.EntityType,
				EntityId: nonEmpty(r.EntityID), EntityName: nonEmpty(r.EntityName), ChangedFields: r.ChangedFields, CreatedAt: r.CreatedAt,
			})
		}
		out.NextCursor = gen.OptString{}
		if next != nil {
			out.NextCursor = gen.NewOptString(next.String())
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("nhật ký thao tác: %w", err)
	}
	return out, nil
}

// ListAuditActors (GET /api/v1/identity/audit-actors, quyền audit.read; Task 488): nhân viên và khóa dịch vụ của shop
// cho ô lọc người làm, chỉ tên.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListAuditActors(ctx context.Context) (gen.ListAuditActorsRes, error) {
	out := &gen.AuditActorList{Items: []gen.AuditActor{}}
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		rows, err := identity.ListAuditActors(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // identity đã bọc
		}
		out.Items = make([]gen.AuditActor, 0, len(rows))
		for _, r := range rows {
			out.Items = append(out.Items, gen.AuditActor{
				ActorType: gen.AuditActorActorType(r.ActorType), ID: r.ID, Name: r.Name, Active: r.Active,
			})
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("người làm của nhật ký: %w", err)
	}
	return out, nil
}

// auditEntityNames điền EntityName (Task 488) cho dòng về yêu cầu đơn (mã YC…), phiếu kho (mã phiếu), tài khoản hãng
// (tên) qua public API của module sở hữu (identity không đọc module khác); mỗi thực thể đọc một lần. Không còn tìm thấy
// → không có tên; lỗi khác trả lên.
func (h *handler) auditEntityNames(ctx context.Context, tx pgx.Tx, rows []identity.AuditLog) error {
	names := map[string]string{}
	var seller *uuid.UUID
	for i := range rows {
		r := &rows[i]
		id, err := uuid.Parse(r.EntityID)
		if err != nil {
			continue
		}
		key := r.EntityType + "/" + id.String()
		if n, done := names[key]; done {
			r.EntityName = n
			continue
		}
		n, err := h.auditEntityName(ctx, tx, r.EntityType, id, &seller)
		if err != nil {
			return err
		}
		names[key], r.EntityName = n, n
	}
	return nil
}

// auditEntityName đọc tên của một thực thể; loại không có tên → "". seller nhớ người bán mặc định (đọc lần đầu cần).
func (h *handler) auditEntityName(ctx context.Context, tx pgx.Tx, typ string, id uuid.UUID, seller **uuid.UUID,
) (string, error) {
	switch typ {
	case "order_request":
		if *seller == nil {
			s, err := catalog.DefaultSeller(ctx, tx)
			if err != nil {
				return "", fmt.Errorf("người bán của yêu cầu trong nhật ký: %w", err)
			}
			*seller = &s
		}
		d, err := order.GetRequest(ctx, tx, **seller, id)
		if errors.Is(err, order.ErrRequestNotFound) {
			return "", nil
		}
		return d.Ref, err //nolint:wrapcheck // order đã bọc
	case "inventory.stock_document":
		d, err := inventory.GetDocument(ctx, tx, id)
		if errors.Is(err, inventory.ErrNotFound) {
			return "", nil
		}
		return d.Code, err //nolint:wrapcheck // inventory đã bọc
	case "carrier_account":
		s, err := h.fulfillmentService()
		if err != nil {
			return "", nil //nolint:nilerr // chưa nối fulfillment (cấu hình thử): không có tên, vẫn trả nhật ký
		}
		a, err := s.GetAccount(ctx, tx, id)
		if errors.Is(err, fulfillment.ErrAccountNotFound) {
			return "", nil
		}
		return a.Name, err //nolint:wrapcheck // fulfillment đã bọc
	}
	return "", nil
}
