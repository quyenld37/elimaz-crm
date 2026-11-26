package conversation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/calls"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Kết quả cuộc gọi khách và hội thoại của cả nhóm khách (Task 169; Q14, CH17). Hiện thực ở internal/calls.

// CallLog là một cuộc gọi đã ghi (Note là dữ liệu cá nhân).
type CallLog = calls.CallLog

// CallInput là yêu cầu ghi cuộc gọi.
type CallInput = calls.Input

// CustomerConversation là một hội thoại trong danh sách hội thoại của khách.
type CustomerConversation = calls.CustomerConversation

// PageCursor là con trỏ trang (thời điểm, mã) của danh sách cuộc gọi, hội thoại của khách: dùng lại con trỏ danh sách
// của customer (String, customer.ParseListCursor; hỏng → customer.ErrInvalidInput, 400).
type PageCursor = customer.ListCursor

// ErrCallInvalid là lỗi dữ liệu cuộc gọi sai (400).
var ErrCallInvalid = calls.ErrInvalidInput

// Calls ghi cuộc gọi; Bus nil: không báo hiệu.
type Calls struct {
	Bus realtime.Bus
}

// Record ghi cuộc gọi của u với khách customerID (xem calls.Record): call_logs, tin hệ thống trong hội thoại, nhắc gọi
// lại khi hẹn gọi lại, audit chỉ tên trường.
func (c Calls) Record(ctx context.Context, tx pgx.Tx, u identity.AuthUser, customerID uuid.UUID, in CallInput,
	now time.Time,
) (CallLog, error) {
	return calls.Record(ctx, tx, c.Bus, u, customerID, in, now) //nolint:wrapcheck // sentinel của phần
}

// ListCustomerCalls trả một trang cuộc gọi của cả nhóm khách mà u được xem (mới nhất trước).
func ListCustomerCalls(ctx context.Context, tx pgx.Tx, u identity.AuthUser, customerID uuid.UUID, after *PageCursor,
	limit int,
) ([]CallLog, *PageCursor, error) {
	return calls.List(ctx, tx, u, customerID, after, limit) //nolint:wrapcheck // sentinel của phần
}

// ListCustomerConversations trả một trang hội thoại của cả nhóm khách trên các Page u được đọc.
func ListCustomerConversations(ctx context.Context, tx pgx.Tx, u identity.AuthUser, customerID uuid.UUID,
	after *PageCursor, limit int,
) ([]CustomerConversation, *PageCursor, error) {
	return calls.Conversations(ctx, tx, u, customerID, after, limit) //nolint:wrapcheck // sentinel của phần
}
