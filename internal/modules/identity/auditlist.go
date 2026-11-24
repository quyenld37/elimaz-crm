package identity

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/identity/internal/store"
)

// AuditPageSize là số dòng mỗi trang của ListAuditLogs (kế hoạch M5, Task 457). Câu ListAuditLogs đọc hằng
// AuditPageSize + 1 dòng (LIMIT 51 trong queries/identity/audit_list.sql); đổi một chỗ thì đổi cả hai.
const AuditPageSize = 50

// AuditCursor là vị trí sau dòng cuối của một trang nhật ký thao tác (created_at giảm, rồi id giảm).
type AuditCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// String mã hóa con trỏ thành chuỗi mờ cho API: micro giây Unix (hệ 16) "_" id.
func (c AuditCursor) String() string {
	return strconv.FormatInt(c.CreatedAt.UnixMicro(), 16) + "_" + c.ID.String()
}

// ParseAuditCursor đọc chuỗi do AuditCursor.String tạo. Hỏng → ErrInvalidInput.
func ParseAuditCursor(s string) (AuditCursor, error) {
	micro, id, ok := strings.Cut(s, "_")
	us, err := strconv.ParseInt(micro, 16, 64)
	if !ok || err != nil || us < 0 {
		return AuditCursor{}, fmt.Errorf("đọc con trỏ nhật ký: %w", ErrInvalidInput)
	}
	parsed, err := uuid.Parse(id)
	if err != nil || len(id) != len(uuid.Nil.String()) {
		return AuditCursor{}, fmt.Errorf("đọc con trỏ nhật ký: %w", ErrInvalidInput)
	}
	return AuditCursor{CreatedAt: time.UnixMicro(us).UTC(), ID: parsed}, nil
}

// AuditFilter là bộ lọc của ListAuditLogs. Chuỗi rỗng, giờ zero là không lọc. ActorID so đúng mã người làm (mã nhân
// viên, mã khóa dịch vụ). EntityID cần EntityType (index theo thực thể bắt đầu bằng loại). Khoảng giờ [From, To). After:
// trang sau.
type AuditFilter struct {
	Action, ActorID, EntityType, EntityID string
	From, To                              time.Time
	After                                 *AuditCursor
}

// AuditLog là một dòng nhật ký thao tác cho màn Nhật ký: người làm (ActorType một trong staff, system, carrier,
// customer, service; ActorID rỗng khi không có), ActorName (tên nhân viên hay tên khóa dịch vụ; rỗng với loại khác hay
// khi không tìm thấy), hành động, thực thể, tên các trường đổi (không nil). Không mang giá trị before/after, IP.
// EntityName (Task 488) là tên khóa dịch vụ khi thực thể là khóa dịch vụ của identity (kể cả đã thu hồi); rỗng với loại
// khác — tên thực thể của module khác do bên gọi đọc qua public API của module đó.
type AuditLog struct {
	ID                            uuid.UUID
	ActorType, ActorID, ActorName string
	Action, EntityType, EntityID  string
	EntityName                    string
	ChangedFields                 []string
	CreatedAt                     time.Time
}

// ListAuditLogs trả một trang (AuditPageSize dòng) nhật ký thao tác của tenant của tx, mới nhất trước, và con trỏ trang
// sau (nil khi hết). EntityID thiếu EntityType, hay From không trước To (khi có cả hai) → ErrInvalidInput. Chạy trong
// tenancy.WithTenant.
//
// Khi nào không dùng: lịch sử thao tác của một đơn (ListEntityAudit: đọc cả after.fields); không để đọc giá trị trước/sau
// (hàm không trả).
func ListAuditLogs(ctx context.Context, tx pgx.Tx, f AuditFilter) ([]AuditLog, *AuditCursor, error) {
	if (f.EntityID != "" && f.EntityType == "") || (!f.From.IsZero() && !f.To.IsZero() && !f.From.Before(f.To)) {
		return nil, nil, fmt.Errorf("lọc nhật ký: %w", ErrInvalidInput)
	}
	p := store.ListAuditLogsParams{
		Action: optText(f.Action), ActorID: optText(f.ActorID), EntityType: optText(f.EntityType),
		EntityID: optText(f.EntityID), FromAt: tsOrNull(f.From), ToAt: tsOrNull(f.To),
	}
	if f.After != nil {
		p.AfterCreated = tsOrNull(f.After.CreatedAt)
		p.AfterID = pgtype.UUID{Bytes: f.After.ID, Valid: true}
	}
	q := store.New(tx)
	rows, err := q.ListAuditLogs(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc nhật ký thao tác: %w", err)
	}
	var next *AuditCursor
	if len(rows) > AuditPageSize {
		rows = rows[:AuditPageSize]
		last := rows[AuditPageSize-1]
		next = &AuditCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	names, err := auditActorNames(ctx, q, rows)
	if err != nil {
		return nil, nil, err
	}
	keyNames, err := auditKeyNames(ctx, q, rows)
	if err != nil {
		return nil, nil, err
	}
	out := make([]AuditLog, 0, len(rows))
	for _, r := range rows {
		changed := r.ChangedFields
		if changed == nil {
			changed = []string{}
		}
		out = append(out, AuditLog{
			ID: r.ID, ActorType: r.ActorType, ActorID: r.ActorID, ActorName: names[actorKey(r.ActorType, r.ActorID)],
			Action: r.Action, EntityType: r.EntityType, EntityID: r.EntityID, ChangedFields: changed,
			CreatedAt: r.CreatedAt,
		})
		if r.EntityType == auditServiceKey {
			out[len(out)-1].EntityName = keyNames[actorKey("service", r.EntityID)]
		}
	}
	return out, next, nil
}

// auditActorNames đọc tên người làm (nhân viên, khóa dịch vụ) của rows bằng một câu; khóa của map là actorKey. Mã
// không phải uuid bỏ qua (không có tên).
func auditActorNames(ctx context.Context, q *store.Queries, rows []store.ListAuditLogsRow) (map[string]string, error) {
	p := store.AuditActorNamesParams{StaffIds: []uuid.UUID{}, KeyIds: []uuid.UUID{}}
	for _, r := range rows {
		id, err := uuid.Parse(r.ActorID)
		if err != nil {
			continue
		}
		switch r.ActorType {
		case "staff":
			p.StaffIds = append(p.StaffIds, id)
		case "service":
			p.KeyIds = append(p.KeyIds, id)
		}
	}
	return namesByKey(ctx, q, p, "đọc tên người làm của nhật ký")
}

// auditKeyNames đọc tên khóa dịch vụ là thực thể của rows (Task 488, cùng câu AuditActorNames); khóa của map là
// actorKey "service/<mã>".
func auditKeyNames(ctx context.Context, q *store.Queries, rows []store.ListAuditLogsRow) (map[string]string, error) {
	p := store.AuditActorNamesParams{StaffIds: []uuid.UUID{}, KeyIds: []uuid.UUID{}}
	for _, r := range rows {
		if id, err := uuid.Parse(r.EntityID); err == nil && r.EntityType == auditServiceKey {
			p.KeyIds = append(p.KeyIds, id)
		}
	}
	return namesByKey(ctx, q, p, "đọc tên khóa dịch vụ của nhật ký")
}

// namesByKey chạy AuditActorNames cho p (rỗng thì không chạy); khóa của map là actorKey; what là việc đang làm (bọc lỗi).
func namesByKey(ctx context.Context, q *store.Queries, p store.AuditActorNamesParams, what string) (map[string]string, error) {
	out := map[string]string{}
	if len(p.StaffIds)+len(p.KeyIds) == 0 {
		return out, nil
	}
	named, err := q.AuditActorNames(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	for _, n := range named {
		out[actorKey(n.Kind, n.ID.String())] = n.Name
	}
	return out, nil
}

// AuditActor là một người làm cho ô lọc của màn nhật ký: ActorType "staff" hay "service", mã (gửi lại ở
// AuditFilter.ActorID), tên, Active (nhân viên chưa khóa, khóa chưa thu hồi).
type AuditActor struct {
	ActorType string
	ID        uuid.UUID
	Name      string
	Active    bool
}

// ListAuditActors trả mọi nhân viên (kể cả đã khóa) và khóa dịch vụ (kể cả đã thu hồi) của tenant của tx, xếp theo tên
// (Task 488). Chỉ tên, không email. Chạy trong tenancy.WithTenant.
//
// Khi nào không dùng: danh sách nhân viên để quản lý (ListStaff: có email, quyền).
func ListAuditActors(ctx context.Context, tx pgx.Tx) ([]AuditActor, error) {
	rows, err := store.New(tx).AuditActors(ctx)
	out := make([]AuditActor, len(rows))
	for i := range rows {
		out[i] = AuditActor(rows[i])
	}
	if err != nil {
		return nil, fmt.Errorf("đọc người làm của nhật ký: %w", err)
	}
	return out, nil
}

// actorKey là khóa "loại/mã uuid dạng chuẩn" của người làm; mã không phải uuid → "" (không có tên).
func actorKey(kind, id string) string {
	u, err := uuid.Parse(id)
	if err != nil {
		return ""
	}
	return kind + "/" + u.String()
}

func optText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func tsOrNull(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.UTC(), Valid: !t.IsZero()}
}
