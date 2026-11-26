package conversation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/live"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Danh sách "đã chốt, thiếu địa chỉ" và nhắc một chạm của phiên live (Task 173; LV04, LV05, LV06). Hiện thực ở
// internal/live (pending.go, remind.go); chọn đường gửi tin nhắc ở internal/send (ask.go). Hoàn tất đơn là
// updateOrder + confirmOrder của M2; nháp quá hạn do lịch order.expire_drafts của M2 hủy.

// Kết quả nhắc một đơn và lý do bỏ ngoài chính sách gửi.
const (
	RemindQueued           = live.RemindQueued
	RemindSkipped          = live.RemindSkipped
	RemindNotDraftText     = live.NotDraftText
	RemindNotInSessionText = live.NotInSessionText
	RemindNoChannelText    = live.NoChannelText
)

// PendingDraftList là danh sách đơn còn nháp của phiên và số đơn đã xác nhận, đã hủy vì quá hạn nháp.
type PendingDraftList = live.PendingList

// PendingDraft là một đơn còn nháp của phiên live.
type PendingDraft = live.PendingRow

// RemindInput là một lần bấm nhắc (đơn, rỗng: mọi nháp; mẫu).
type RemindInput = live.RemindInput

// RemindOutcome là kết quả nhắc một đơn.
type RemindOutcome = live.RemindOutcome

// ListPendingDrafts trả đơn còn nháp có ghi nhận trong phiên live sessionID cho u lúc now (cần conversation.read trên
// Page; không có → ErrNotFound), kèm xét nhắc theo chính sách của kênh (env.Registry), trong tx tenant của bên gọi.
func ListPendingDrafts(ctx context.Context, tx pgx.Tx, env SendEnv, u identity.AuthUser, sessionID uuid.UUID, now time.Time,
) (PendingDraftList, error) {
	return live.Pending(ctx, tx, env.Registry, u, sessionID, now) //nolint:wrapcheck // sentinel của phần
}

// RemindPendingDrafts nhắc khách còn đơn nháp của phiên live sessionID gửi địa chỉ (xem live.Remind), bằng nhân viên u
// lúc now, trong tx tenant của bên gọi (WriteTx): cần live.operate trên Page (không có → ErrNotFound); mẫu không có →
// ErrNotFound.
func RemindPendingDrafts(ctx context.Context, tx pgx.Tx, env SendEnv, u identity.AuthUser, sessionID uuid.UUID,
	in RemindInput, now time.Time,
) ([]RemindOutcome, error) {
	return live.Remind(ctx, tx, live.RemindEnv{Send: env, Render: renderForOrder}, u, sessionID, in, now) //nolint:wrapcheck // sentinel của phần
}
