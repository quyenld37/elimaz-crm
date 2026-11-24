package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Tạm dừng nhận đơn web (kế hoạch M5, Task 456): một dòng ord.web_intake mỗi tenant. Chưa có dòng = đang nhận đơn,
// version 1. Khi tạm dừng, CreateFromWeb từ chối đơn gửi thẳng (sync) mới; đơn từ hàng đợi của website (queued: khách
// đã nhận trang kết quả trước khi tạm dừng) vẫn được nhận.
const (
	// TopicWebIntakeChanged: tạm dừng hay mở lại nhận đơn web (aggregate web_intake, mã = tenant, Seq = version mới).
	// Consumer webcache làm mới vùng web_intake của bộ đệm website.
	TopicWebIntakeChanged = "order.web_intake.changed"
	// MaxWebIntakeReasonRunes là độ dài tối đa của lý do (CHECK của bảng).
	MaxWebIntakeReasonRunes = 300

	aggregateWebIntake   = "web_intake"
	webIntakeSignalTopic = "order.web_intake"
)

// WebIntake là trạng thái nhận đơn web của shop: lý do (rỗng khi không có), nhân viên đổi lần cuối, lúc đổi; Version
// để sửa có kiểm phiên bản.
type WebIntake struct {
	Paused    bool
	Reason    string
	ChangedBy *uuid.UUID
	ChangedAt *time.Time
	Version   int
}

// AuditFields là giá trị đưa vào audit: trạng thái, lý do (chữ của nhân viên, không phải dữ liệu khách), version.
func (wi WebIntake) AuditFields() map[string]any {
	return map[string]any{"paused": wi.Paused, "reason": wi.Reason, "version": wi.Version}
}

// WebIntakeChange là trạng thái trước và sau một lần đổi (bên gọi ghi audit; order không nhập identity).
type WebIntakeChange struct {
	Before, After WebIntake
}

// WebIntakeChanged là payload của TopicWebIntakeChanged (không lý do, không người).
type WebIntakeChanged struct {
	Paused  bool `json:"paused"`
	Version int  `json:"version"`
}

// GetWebIntake đọc trạng thái nhận đơn web của tenant đang chạy (tx trong tenancy.WithTenant), không khóa. Chưa có
// dòng → đang nhận đơn, version 1.
func GetWebIntake(ctx context.Context, tx pgx.Tx) (WebIntake, error) {
	r, err := store.New(tx).GetWebIntake(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return WebIntake{Version: 1}, nil
	}
	if err != nil {
		return WebIntake{}, fmt.Errorf("đọc trạng thái nhận đơn web: %w", err)
	}
	return webIntakeOf(store.LockWebIntakeRow(r)), nil
}

// SetWebIntake tạm dừng (paused) hay mở lại nhận đơn web khi version còn khớp, trong tx của bên gọi. reason cắt khoảng
// trắng hai đầu: tạm dừng mà rỗng → ErrReasonRequired; quá MaxWebIntakeReasonRunes ký tự → ErrInvalidInput; mở lại thì
// lý do không bắt buộc (rỗng → xóa). Chèn dòng mặc định nếu chưa có, khóa dòng (nhóm 7 ADR-46), so version (khác →
// ErrVersionConflict), ghi người a và giờ now, version + 1; phát TopicWebIntakeChanged và báo hiệu màn hình trong tx.
// Chạy lại được trong db.RetryTx.
func (w Writer) SetWebIntake(ctx context.Context, tx pgx.Tx, paused bool, reason string, version int, a Actor,
	now time.Time,
) (WebIntakeChange, error) {
	reason = strings.TrimSpace(reason)
	switch {
	case paused && reason == "":
		return WebIntakeChange{}, ErrReasonRequired
	case !utf8.ValidString(reason) || utf8.RuneCountInString(reason) > MaxWebIntakeReasonRunes:
		return WebIntakeChange{}, fmt.Errorf("%w: lý do tạm dừng", ErrInvalidInput)
	}
	if w.Events == nil || w.Queue == nil {
		return WebIntakeChange{}, errWriterNotWired
	}
	q := store.New(tx)
	if err := q.EnsureWebIntake(ctx, a.TenantID); err != nil {
		return WebIntakeChange{}, fmt.Errorf("tạo dòng nhận đơn web: %w", err)
	}
	cur, err := q.LockWebIntake(ctx)
	if err != nil {
		return WebIntakeChange{}, fmt.Errorf("khóa trạng thái nhận đơn web: %w", err)
	}
	if int(cur.Version) != version {
		return WebIntakeChange{}, ErrVersionConflict
	}
	r, err := q.UpdateWebIntake(ctx, store.UpdateWebIntakeParams{
		Paused: paused, Reason: pgtype.Text{String: reason, Valid: reason != ""}, ChangedBy: a.UserID,
		ChangedAt: now.UTC(), Version: cur.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return WebIntakeChange{}, ErrVersionConflict // không xảy ra khi đã giữ khóa dòng; giữ để an toàn
	}
	if err != nil {
		return WebIntakeChange{}, fmt.Errorf("ghi trạng thái nhận đơn web: %w", err)
	}
	before, after := webIntakeOf(cur), webIntakeOf(store.LockWebIntakeRow(r))
	if err := w.webIntakeChanged(ctx, tx, a.TenantID, after); err != nil {
		return WebIntakeChange{}, err
	}
	return WebIntakeChange{Before: before, After: after}, nil
}

// webIntakeChanged phát TopicWebIntakeChanged (Seq = version mới) và báo hiệu cho màn hình nhân viên (order.read).
func (w Writer) webIntakeChanged(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, wi WebIntake) error {
	if _, _, err := w.Events.Emit(ctx, tx, w.Queue, events.Event{
		TenantID: tenant, AggregateType: aggregateWebIntake, AggregateID: tenant.String(), Seq: int64(wi.Version),
		Topic: TopicWebIntakeChanged, Payload: WebIntakeChanged{Paused: wi.Paused, Version: wi.Version},
	}); err != nil {
		return fmt.Errorf("phát %s: %w", TopicWebIntakeChanged, err)
	}
	if w.Realtime == nil {
		return nil
	}
	if err := w.Realtime.Publish(ctx, tx, realtime.Signal{
		TenantID: tenant, Topic: webIntakeSignalTopic, Requires: permRead,
		Entity: realtime.Entity{Type: aggregateWebIntake, ID: tenant.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu nhận đơn web: %w", err)
	}
	return nil
}

// checkWebIntake: đơn gửi thẳng (sync) khi shop đang tạm dừng → ErrWebIntakePaused; queued luôn qua. Đọc không khóa:
// đơn đã đọc "đang nhận" trước khi lệnh tạm dừng commit vẫn được tạo (Soát thiết kế 6.5 của Task 456).
func checkWebIntake(ctx context.Context, tx pgx.Tx, submittedVia string) error {
	if submittedVia != SubmittedSync {
		return nil
	}
	wi, err := GetWebIntake(ctx, tx)
	if err != nil {
		return err
	}
	if wi.Paused {
		return ErrWebIntakePaused
	}
	return nil
}

func webIntakeOf(r store.LockWebIntakeRow) WebIntake {
	wi := WebIntake{Paused: r.Paused, Reason: r.Reason.String, Version: int(r.Version)}
	if r.ChangedBy.Valid {
		id := uuid.UUID(r.ChangedBy.Bytes)
		wi.ChangedBy = &id
	}
	if r.ChangedAt.Valid {
		t := r.ChangedAt.Time.UTC()
		wi.ChangedAt = &t
	}
	return wi
}
