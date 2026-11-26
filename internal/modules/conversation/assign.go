package conversation

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Lỗi nghiệp vụ của conversation (lớp HTTP đổi mã ở internal/httpapi/errors_conversation.go).
var (
	// ErrVersionConflict: version gửi lên khác version hiện tại.
	ErrVersionConflict = assign.ErrVersionConflict
	// ErrAlreadyAssigned: hội thoại đang do người khác phụ trách.
	ErrAlreadyAssigned = assign.ErrAlreadyAssigned
)

// Lý do của dòng lịch sử phân công.
const (
	AssignReasonSelf       = assign.ReasonSelf
	AssignReasonManager    = assign.ReasonManager
	AssignReasonRoundRobin = assign.ReasonRoundRobin
	AssignReasonUnassign   = assign.ReasonUnassign
)

// Assignment là một dòng lịch sử phân công.
type Assignment = assign.Assignment

// Person là nhân viên kèm tên hiển thị.
type Person = assign.Person

// Writer là đường ghi của hộp chat do nhân viên làm (người phụ trách, nhãn, cài đặt Page): mỗi lần ghi khóa dòng, so
// version, audit, báo hiệu (và sự kiện conversation.assigned) trong tx của bên gọi. Dựng mới cho mỗi yêu cầu.
type Writer struct {
	Events    *events.Registry
	Queue     queue.JobQueue
	Realtime  realtime.Bus
	RequestID string
}

func (w Writer) assign() assign.Writer {
	return assign.Writer{Events: w.Events, Queue: w.Queue, Realtime: w.Realtime, RequestID: w.RequestID}
}

// Claim nhận hội thoại id về actor (CH06): chưa ai nhận → actor; actor đang nhận → không đổi; người khác →
// ErrAlreadyAssigned. Cần conversation.reply trên Page (không có → ErrNotFound).
func (w Writer) Claim(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID) error {
	return w.assign().Claim(ctx, tx, actor, id) //nolint:wrapcheck // assign đã bọc
}

// Assign giao hội thoại id cho to (nil: bỏ giao) khi expectedVersion khớp; cần conversation.assign trên Page.
func (w Writer) Assign(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID, to *uuid.UUID,
	expectedVersion int32,
) error {
	return w.assign().Assign(ctx, tx, actor, id, to, expectedVersion) //nolint:wrapcheck // assign đã bọc
}

// ListAssignments trả lịch sử phân công của hội thoại id, cũ trước (cần conversation.read trên Page).
func ListAssignments(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID) ([]Assignment, error) {
	return assign.History(ctx, tx, actor, id) //nolint:wrapcheck // assign đã bọc
}

// AssignableUsers trả nhân viên active có conversation.read trên Page pageID (danh sách "Giao cho…"); người gọi cần
// conversation.read trên Page; Page không có → channel.ErrAccountNotFound.
func AssignableUsers(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, pageID uuid.UUID) ([]identity.User, error) {
	if err := access.Require(actor, assign.PermRead, pageID); err != nil {
		return nil, err //nolint:wrapcheck // sentinel của module
	}
	if _, err := channel.Account(ctx, tx, pageID); err != nil {
		return nil, err //nolint:wrapcheck // channel đã bọc
	}
	users, err := identity.StaffWithPermissionAt(ctx, tx, assign.PermRead, *access.PageScope(pageID))
	if err != nil {
		return nil, fmt.Errorf("đọc người được giao: %w", err)
	}
	return users, nil
}

// NewRoundRobin trả hook xoay vòng cho đường tin vào (gọi khi hội thoại vừa được tạo, trong tx của sự kiện vào): giao
// cho người kế tiếp của danh sách xoay vòng Page nếu bật (Q15). Sự kiện conversation.assigned ghi bằng ev, q; báo hiệu
// do đường vào phát sau hook.
func NewRoundRobin(ev *events.Registry, q queue.JobQueue) func(context.Context, pgx.Tx, uuid.UUID) error {
	return assign.RoundRobin{W: assign.Writer{Events: ev, Queue: q}}.OnNewConversation
}
