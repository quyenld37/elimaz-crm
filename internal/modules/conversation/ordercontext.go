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
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
)

// vietnamOffset: giờ Việt Nam (UTC+7, không đổi giờ) của khung giờ live.
const vietnamOffset = 7 * 3600

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
	if out.TrafficSourceID != nil {
		err := order.ActiveSource(ctx, tx, *out.TrafficSourceID)
		switch {
		case errors.Is(err, order.ErrTrafficSourceInactive):
			out.TrafficSourceID = nil
		case err != nil:
			return OrderContextResult{}, fmt.Errorf("đọc nguồn đơn tự điền: %w", err)
		}
	}
	return out, nil
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
