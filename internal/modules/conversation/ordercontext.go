package conversation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/query"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
)

// vietnamOffset: giờ Việt Nam (UTC+7, không đổi giờ) của khung giờ live.
const vietnamOffset = 7 * 3600

// ErrOrderCustomerMismatch là lỗi: đơn lên từ hội thoại cho khách không cùng nhóm hồ sơ với khách của hội thoại (422,
// Task 501).
var ErrOrderCustomerMismatch = errors.New("khách của đơn không phải khách của hội thoại")

// OrderContextResult là bối cảnh lên đơn từ một hội thoại (01 mục 2): Page, hội thoại, khách, người bán của Page, nguồn
// đơn tự điền (nil: không có cài đặt, hay nguồn đã tắt).
type OrderContextResult struct {
	ChannelAccountID, ConversationID, CustomerID, SellerID uuid.UUID
	TrafficSourceID                                        *uuid.UUID
}

// OrderContext trả bối cảnh lên đơn của hội thoại id (cần conversation.read trên Page). Nguồn đơn theo cài đặt Page:
// hộp thư → nguồn hộp thư; comment trên bài live có giờ bắt đầu → nguồn của khung giờ live chứa giờ bắt đầu (giờ Việt
// Nam), không khung nào chứa → nguồn comment; comment khác → nguồn comment. Nguồn không còn bật (order.ActiveSource) →
// nil.
func OrderContext(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID) (OrderContextResult, error) {
	c, err := query.ConversationRef(ctx, tx, actor, id)
	if err != nil {
		return OrderContextResult{}, err //nolint:wrapcheck // sentinel của module
	}
	page, err := channel.Account(ctx, tx, c.ChannelAccountID)
	if err != nil {
		return OrderContextResult{}, err //nolint:wrapcheck // channel đã bọc
	}
	s, err := readSettings(ctx, tx, c.ChannelAccountID)
	if err != nil {
		return OrderContextResult{}, err
	}
	var start *time.Time
	if c.PostKind == "live" && c.PostCreatedTime.Valid {
		start = &c.PostCreatedTime.Time
	}
	out := OrderContextResult{
		ChannelAccountID: c.ChannelAccountID, ConversationID: c.ID, CustomerID: c.CustomerID, SellerID: page.SellerID,
		TrafficSourceID: autoSource(s, c.Kind, start),
	}
	if out.TrafficSourceID, err = activeSource(ctx, tx, out.TrafficSourceID); err != nil {
		return OrderContextResult{}, err
	}
	return out, nil
}

// activeSource trả id khi nguồn đơn id còn bật (order.ActiveSource); nil hay đã tắt → nil.
func activeSource(ctx context.Context, tx pgx.Tx, id *uuid.UUID) (*uuid.UUID, error) {
	if id == nil {
		return nil, nil
	}
	err := order.ActiveSource(ctx, tx, *id)
	switch {
	case errors.Is(err, order.ErrTrafficSourceInactive):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("đọc nguồn đơn tự điền: %w", err)
	}
	return id, nil
}

// autoSource chọn nguồn đơn tự điền từ cài đặt s cho hội thoại loại kind; liveStart khác nil: comment trên bài live
// bắt đầu lúc đó.
func autoSource(s PageSettings, kind string, liveStart *time.Time) *uuid.UUID {
	if kind != "comment" {
		return s.InboxTrafficSourceID
	}
	if liveStart != nil {
		hm := liveStart.In(time.FixedZone("ICT", vietnamOffset)).Format(clockLayout)
		for _, slot := range s.LiveSlots {
			if slot.From <= hm && hm < slot.To {
				id := slot.TrafficSourceID
				return &id
			}
		}
	}
	return s.CommentTrafficSourceID
}

// CheckOrderCustomer kiểm khách customerID của đơn sắp lên từ hội thoại c (Task 501): nil (đơn gửi khách mới hay tìm
// theo SĐT — kết quả có thể là người khác) hay hồ sơ không cùng nhóm (root_id) với khách của hội thoại →
// ErrOrderCustomerMismatch; hồ sơ không có → customer.ErrNotFound. Đọc gốc không khóa: gộp hồ sơ chạy song song chỉ
// làm hai hồ sơ cùng nhóm hơn (tệ nhất là từ chối nhầm, bấm lại được); khóa gốc do order.Writer.Create lấy sau đó.
func CheckOrderCustomer(ctx context.Context, tx pgx.Tx, c OrderContextResult, customerID *uuid.UUID) error {
	if customerID == nil {
		return ErrOrderCustomerMismatch
	}
	if *customerID == c.CustomerID {
		return nil
	}
	want, err := customer.RootOf(ctx, tx, c.CustomerID)
	if err != nil {
		return fmt.Errorf("đọc nhóm khách của hội thoại: %w", err)
	}
	got, err := customer.RootOf(ctx, tx, *customerID)
	if err != nil {
		return fmt.Errorf("đọc nhóm khách của đơn: %w", err)
	}
	if got != want {
		return ErrOrderCustomerMismatch
	}
	return nil
}
