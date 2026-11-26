package automation

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// OnCommentCreated chạy các quy tắc tự động cho comment khách mới msgID (hook của đường comment vào, Task 154; chạy
// trong tx của sự kiện, sau khi chèn tin). Task 165: tự ẩn (auto_hide) — chế độ phone_only ẩn comment có SĐT, all ẩn
// mọi comment của khách, trừ bài trong exceptPostIds; qua Run với ExternalRef "hide:<comment_id>" rồi ghi lệnh
// command.fb.auto_hide_comment. Task 166: tự trả lời comment, tự nhắn riêng (auto_reply_comment, autoreply.go). Comment
// của Page, tin lịch sử không bao giờ kích hoạt (Run).
func OnCommentCreated(ctx context.Context, tx pgx.Tx, d deps.Deps, msgID uuid.UUID, t Trigger) error {
	t.Kind = TriggerCommentCreated
	if t.CustomerID == uuid.Nil && t.ConversationID != uuid.Nil {
		c, err := store.New(tx).ConversationCustomer(ctx, t.ConversationID)
		if err != nil {
			return err //nolint:wrapcheck // lỗi database của câu đọc một dòng
		}
		t.CustomerID = c
	}
	if err := autoHide(ctx, tx, d, msgID, t); err != nil {
		return err
	}
	return autoReply(ctx, tx, d, msgID, t)
}

// autoHide áp quy tắc auto_hide của Page cho comment msgID.
func autoHide(ctx context.Context, tx pgx.Tx, d deps.Deps, msgID uuid.UUID, t Trigger) error {
	rule, err := LoadRule(ctx, tx, t.Account.ID, KindAutoHide)
	if err != nil || !rule.Enabled {
		return err
	}
	var c HideConfig
	if json.Unmarshal(rule.Config, &c) != nil || !c.hides(t) {
		return nil // cấu hình đã kiểm khi lưu; đọc lỗi coi như tắt
	}
	t.ExternalRef, t.SubjectRef = "hide:"+t.CommentExternalID, t.ConversationID.String()
	_, err = Run(ctx, tx, d, rule, t, func(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
		return enqueueHide(ctx, tx, d, t.Account.TenantID, msgID, c.reason())
	})
	return err
}

// hides cho biết cấu hình c ẩn comment của t.
func (c HideConfig) hides(t Trigger) bool {
	if slices.Contains(c.ExceptPostIDs, t.PostExternalID) {
		return false
	}
	switch c.Mode {
	case HideAll:
		return true
	case HidePhoneOnly:
		return ContainsPhone(t.Text)
	}
	return false
}

// Lý do tự ẩn ghi vào dòng lịch sử (message_revisions.reason, Task 501).
const (
	hideReasonPhone = "phone"
	hideReasonAll   = "all"
)

// reason là lý do tự ẩn theo chế độ của c (gọi khi c.hides đã đúng): all → hideReasonAll, phone_only → hideReasonPhone.
func (c HideConfig) reason() string {
	if c.Mode == HideAll {
		return hideReasonAll
	}
	return hideReasonPhone
}

// hidePayload là payload của lệnh ẩn tự động: mã tin và lý do (không nội dung). Reason rỗng: lệnh ghi trước Task 501.
type hidePayload struct {
	MessageID uuid.UUID `json:"messageId"`
	Reason    string    `json:"reason,omitempty"`
}

// enqueueHide ghi lệnh ẩn tự động comment msgID vì reason (chống trùng theo tin).
func enqueueHide(ctx context.Context, tx pgx.Tx, d deps.Deps, tenant, msgID uuid.UUID, reason string) (uuid.UUID, error) {
	id, _, err := d.Queue.Enqueue(ctx, tx, HideCommandKind(), queue.NewJob{
		TenantID: &tenant, Payload: hidePayload{MessageID: msgID, Reason: reason}, DedupeKey: "auto_hide:" + msgID.String(),
	})
	if err != nil {
		return uuid.Nil, err //nolint:wrapcheck // queue đã bọc
	}
	return id, nil
}
