package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
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
		out.Items = out.Items[:0]
		for _, r := range rows {
			out.Items = append(out.Items, gen.AuditLog{
				ID: r.ID, ActorType: gen.AuditLogActorType(r.ActorType), ActorId: nonEmpty(r.ActorID),
				ActorName: nonEmpty(r.ActorName), Action: r.Action, EntityType: r.EntityType,
				EntityId: nonEmpty(r.EntityID), ChangedFields: r.ChangedFields, CreatedAt: r.CreatedAt,
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
