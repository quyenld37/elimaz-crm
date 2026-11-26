package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// sourceFacebook là khóa nguồn (và cột channel_accounts.channel) của kênh Facebook: echo của CRM mang app_id =
// META_APP_ID. Nguồn khác (kênh giả fakechat, chỉ ngoài production) không có app id: echo của CRM mang app id là chính
// khóa nguồn (tệp task-153, Quyết định 3).
const sourceFacebook = "facebook"

// permConversationRead là quyền báo hiệu hội thoại đòi (Q36: theo Page).
const permConversationRead identity.Permission = "conversation.read"

// Chỉ số của đường vào.
const (
	metricIgnored         = "inbound_ignored"      // nhãn: loại sự kiện chưa xử lý hay lý do bỏ qua
	metricWebhookToSignal = "webhook_to_signal_ms" // nhãn: nguồn; từ lúc CRM nhận tới lúc phát báo hiệu
)

// handler là handler sự kiện vào của một nguồn kênh nhắn tin (Task 153): tin, echo, đã nhận, đã xem, cảm xúc, sửa,
// đến từ quảng cáo; comment thêm, sửa, xóa, ẩn, hiện (Task 154). Live, chính sách do task sau thêm nhánh.
type handler struct {
	d        deps.Deps
	source   string
	ownAppID string
	now      func() time.Time
}

// NewHandler trả handler sự kiện vào của nguồn source (khóa adapter trong d.Registry: facebook, fakechat). Handle chạy
// trong tx tenant của việc inbound.<source>: đọc Inbound.Type, giải Data thành kiểu chuẩn hóa của channel, gọi đúng
// hàm; loại chưa xử lý → chỉ số inbound_ignored (nhãn loại), xong việc. Data hỏng → queue.ErrPermanent.
func NewHandler(d deps.Deps, source string) webhooks.InboundHandler { //nolint:ireturn // handler vào là interface của nền tảng
	own := source
	if source == sourceFacebook {
		own = d.Config.Meta.AppID
	}
	return &handler{d: d, source: source, ownAppID: own, now: d.Clock()}
}

func (h *handler) Source() string { return h.source }

func (h *handler) Handle(ctx context.Context, e webhooks.Inbound, tx pgx.Tx) error {
	var err error
	switch e.Type {
	case channel.EventMessageReceived:
		err = with(e, func(p channel.MessagePayload) error { return h.messageReceived(ctx, tx, e, p) })
	case channel.EventMessageEcho:
		err = with(e, func(p channel.MessagePayload) error { return h.echo(ctx, tx, e, p) })
	case channel.EventMessageDelivered, channel.EventMessageRead:
		err = with(e, func(p channel.WatermarkPayload) error {
			return h.watermark(ctx, tx, e, p, e.Type == channel.EventMessageRead)
		})
	case channel.EventMessageReaction:
		err = with(e, func(p channel.ReactionPayload) error { return h.reaction(ctx, tx, e, p) })
	case channel.EventMessageEdited:
		err = with(e, func(p channel.EditPayload) error { return h.edit(ctx, tx, e, p) })
	case channel.EventMessageReferral:
		err = with(e, func(p channel.MessagePayload) error { return h.referral(ctx, tx, e, p) })
	case channel.EventCommentChanged:
		err = with(e, func(p channel.CommentPayload) error { return h.comment(ctx, tx, e, p) })
	case channel.EventPolicyEnforcement:
		err = HandlePolicy(ctx, tx, h.d, h.source, e)
	default:
		h.ignore(e.Type)
		return nil
	}
	if err != nil {
		return fmt.Errorf("xử lý sự kiện %s: %w", e.Type, err)
	}
	return nil
}

// with giải e.Data thành T rồi gọi fn; Data hỏng → lỗi vĩnh viễn (không chép nội dung vào lỗi).
func with[T any](e webhooks.Inbound, fn func(T) error) error {
	var p T
	if err := json.Unmarshal(e.Data, &p); err != nil {
		return fmt.Errorf("giải dữ liệu sự kiện %s: %w", e.Type, queue.ErrPermanent)
	}
	return fn(p)
}

func (h *handler) ignore(label string) {
	if h.d.Metrics != nil {
		h.d.Metrics.Inc(metricIgnored, label, 1)
	}
}

// account đọc tài khoản Page pageID của nguồn và ghi mốc có sự kiện vào (nhóm 3 ADR-46, trước mọi khóa khác của đường
// vào). Chỉ sự kiện tới qua webhook (Via = webhooks.ViaWebhook) mới ghi mốc last_webhook_at; comment qua webhook ghi
// cả mốc feed (last_feed_at, chế độ đọc comment auto của Task 150). Sự kiện hỏi bù, đọc comment, lịch sử không ghi
// mốc: nếu ghi sẽ che mất "im lặng bất thường" (Task 150). Tài khoản read_only, disconnected vẫn trả về (vẫn lưu tin).
// Không có trong shop → ok = false, đếm inbound_ignored{account_missing}.
func (h *handler) account(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, pageID string) (channel.ChannelAccount, bool, error) {
	acc, err := channel.AccountByExternal(ctx, tx, h.source, pageID)
	if errors.Is(err, channel.ErrAccountNotFound) {
		h.ignore("account_missing")
		return channel.ChannelAccount{}, false, nil
	}
	if err != nil {
		return channel.ChannelAccount{}, false, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	if e.Via == webhooks.ViaWebhook {
		if err := channel.NoteInbound(ctx, tx, acc.ID, e.ReceivedAt, e.Type == channel.EventCommentChanged); err != nil {
			return channel.ChannelAccount{}, false, err //nolint:wrapcheck // channel đã bọc việc đang làm
		}
	}
	return acc, true, nil
}

// thread tìm hoặc tạo khách theo PSID user của Page (nhóm 5, khóa tư vấn danh tính trước) rồi hội thoại inbox (nhóm 6,
// khóa dòng); inserted = hội thoại vừa tạo. Khách, hội thoại vừa tạo gọi hook của task sau.
func (h *handler) thread(ctx context.Context, tx pgx.Tx, acc channel.ChannelAccount, user string, at time.Time,
) (id uuid.UUID, inserted bool, err error) {
	cc, err := h.customerFor(ctx, tx, acc, user, "")
	if err != nil {
		return uuid.Nil, false, err
	}
	row, err := store.New(tx).UpsertInboxConversation(ctx, store.UpsertInboxConversationParams{
		ChannelAccountID: acc.ID, CustomerID: cc.CustomerID, ExternalThreadRef: user, At: at.UTC(),
	})
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("ghi hội thoại inbox: %w", err)
	}
	if cc.Created {
		if err := OnNewCustomer(ctx, tx, cc.CustomerID, row.ID); err != nil {
			return uuid.Nil, false, err
		}
	}
	if row.Inserted {
		if err := h.onNewConversation(ctx, tx, acc, row.ID, true); err != nil {
			return uuid.Nil, false, err
		}
	}
	return row.ID, row.Inserted, nil
}

// customerFor tìm hoặc tạo khách theo PSID user của Page acc (nhóm 5 ADR-46, khóa tư vấn danh tính trước). name rỗng →
// tên tạm placeholderName (tin nhắn); khác rỗng → tên thật kênh gửi kèm (comment). Chỉ dùng tên khi tạo mới.
func (h *handler) customerFor(ctx context.Context, tx pgx.Tx, acc channel.ChannelAccount, user, name string,
) (customer.ChannelCustomer, error) {
	in := customer.ChannelIdentity{
		Kind: string(customer.IdentityFBPSID), Scope: acc.ExternalID, Value: user, DisplayName: name,
	}
	if name == "" {
		in.DisplayName, in.NameIsPlaceholder = placeholderName, true
	}
	w := customer.Writer{Events: h.d.Events, Queue: h.d.Queue, Realtime: h.d.Bus}
	cc, err := w.FindOrCreateByIdentity(ctx, tx, in, customer.Actor{Type: customer.ActorSystem}, h.now().UTC())
	if err != nil {
		return customer.ChannelCustomer{}, err //nolint:wrapcheck // customer đã bọc việc đang làm
	}
	return cc, nil
}

// publish phát conversation.changed cho hội thoại id (đòi conversation.read trên Page; chỉ đi khi tx commit) và ghi
// độ trễ từ lúc CRM nhận sự kiện.
func (h *handler) publish(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, acc channel.ChannelAccount, id uuid.UUID) error {
	if err := h.d.Bus.Publish(ctx, tx, realtime.Signal{
		TenantID: e.TenantID, Topic: deps.TopicConversationChanged, Requires: permConversationRead,
		Scope: access.PageScope(acc.ID), Entity: realtime.Entity{Type: deps.EntityConversation, ID: id.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu hội thoại đổi: %w", err)
	}
	if h.d.Metrics != nil && !e.ReceivedAt.IsZero() {
		h.d.Metrics.Observe(metricWebhookToSignal, float64(h.now().Sub(e.ReceivedAt).Milliseconds()), h.source)
	}
	return nil
}
