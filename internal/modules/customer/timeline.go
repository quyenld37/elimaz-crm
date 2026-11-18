package customer

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
)

// Loại mục của dòng thời gian khách (kind là chuỗi mở trong openapi.yaml: M4 thêm conversation, C7).
const (
	TimelineOrderCreated = "order_created"
	TimelineOrderStatus  = "order_status"
	TimelineNote         = "note"
	TimelineMerge        = "merge"
	TimelineMergeUndone  = "merge_undone"
	TimelineTag          = "tag"
)

// defaultTimelineLimit khớp mặc định của tham số limit trong openapi.yaml.
const defaultTimelineLimit = 50

// TimelineItem là một mục của dòng thời gian khách. RefID là mã của đơn (order_created), dòng đổi trạng thái
// (order_status), ghi chú, lần gộp (merge, merge_undone) hay nhãn (tag); (Kind, RefID) không trùng giữa các mục.
// ActorID nil khi không rõ người làm. Summary chỉ mang mã, trạng thái, tên nhãn (không dữ liệu cá nhân); Note là nội
// dung ghi chú (chỉ ở kind note).
type TimelineItem struct {
	At      time.Time
	Kind    string
	RefID   uuid.UUID
	ActorID *uuid.UUID
	Summary map[string]string
	Note    NoteBody
}

// TimelineCursor là vị trí sau mục cuối của một trang dòng thời gian (at giảm, rồi kind, rồi ref_id giảm).
type TimelineCursor struct {
	At    time.Time
	Kind  string
	RefID uuid.UUID
}

// timelineCursorMin: 8 byte micro giây Unix (big-endian) + 16 byte ref_id, rồi kind (≥ 1 byte).
const timelineCursorMin = 8 + 16 + 1

// String mã hóa con trỏ thành chuỗi mờ cho API (base64url của micro giây Unix, ref_id, kind).
func (c TimelineCursor) String() string {
	b := binary.BigEndian.AppendUint64(make([]byte, 0, timelineCursorMin+len(c.Kind)), uint64(c.At.UnixMicro()))
	b = append(b, c.RefID[:]...)
	return base64.RawURLEncoding.EncodeToString(append(b, c.Kind...))
}

// ParseTimelineCursor đọc chuỗi do TimelineCursor.String tạo. Hỏng → ErrInvalidInput.
func ParseTimelineCursor(s string) (TimelineCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) < timelineCursorMin || len(raw) > timelineCursorMin+63 {
		return TimelineCursor{}, fmt.Errorf("đọc con trỏ dòng thời gian: %w", ErrInvalidInput)
	}
	c := TimelineCursor{At: time.UnixMicro(int64(binary.BigEndian.Uint64(raw[:8]))).UTC()} //nolint:gosec // bit do String ghi
	copy(c.RefID[:], raw[8:24])
	c.Kind = string(raw[24:])
	return c, nil
}

// Timeline trả một trang dòng thời gian của **cả nhóm** của khách id (gốc hay hồ sơ con): đơn và đổi trạng thái đơn
// (đọc view của ord), ghi chú, gộp và hoàn tác gộp, nhãn đang gắn; mới nhất trước, keyset (at, kind, ref_id) giảm dần
// (after nil: trang đầu), và con trỏ trang sau (nil khi hết). limit ≤ 0: 50; tối đa MaxListLimit. Khách không có (hay
// của tenant khác) → ErrNotFound. Mục mới thêm sau lần đọc trang trước chỉ hiện ở trang đầu khi đọc lại.
func Timeline(ctx context.Context, tx pgx.Tx, id uuid.UUID, after *TimelineCursor, limit int,
) ([]TimelineItem, *TimelineCursor, error) {
	root, err := RootOf(ctx, tx, id)
	if err != nil {
		return nil, nil, err
	}
	limit = clampLimit(limit, defaultTimelineLimit)
	p := store.ListTimelineParams{RootID: root, Lim: int32(limit + 1)} //nolint:gosec // đã chặn ở clampLimit
	if after != nil {
		p.AfterAt = pgtype.Timestamptz{Time: after.At, Valid: true}
		p.AfterKind = pgtype.Text{String: after.Kind, Valid: true}
		p.AfterRef = pgtype.UUID{Bytes: after.RefID, Valid: true}
	}
	rows, err := store.New(tx).ListTimeline(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc dòng thời gian của khách: %w", err)
	}
	var next *TimelineCursor
	if len(rows) > limit {
		last := rows[limit-1]
		next = &TimelineCursor{At: last.At, Kind: last.Kind, RefID: last.RefID}
		rows = rows[:limit]
	}
	out := make([]TimelineItem, 0, len(rows))
	for _, r := range rows {
		it := TimelineItem{At: r.At, Kind: r.Kind, RefID: r.RefID, Summary: map[string]string{}, Note: NoteBody(r.Note.String)}
		if r.ActorID.Valid {
			a := uuid.UUID(r.ActorID.Bytes)
			it.ActorID = &a
		}
		if err := json.Unmarshal(r.Summary, &it.Summary); err != nil {
			return nil, nil, fmt.Errorf("đọc tóm tắt mục %s: %w", r.Kind, err)
		}
		out = append(out, it)
	}
	return out, next, nil
}
