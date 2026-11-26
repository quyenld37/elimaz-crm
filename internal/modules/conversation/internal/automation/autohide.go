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
// command.fb.auto_hide_comment. Comment của Page, tin lịch sử không bao giờ kích hoạt (Run).
func OnCommentCreated(ctx context.Context, tx pgx.Tx, d deps.Deps, msgID uuid.UUID, t Trigger) error {
	t.Kind = TriggerCommentCreated
	if t.CustomerID == uuid.Nil && t.ConversationID != uuid.Nil {
		c, err := store.New(tx).ConversationCustomer(ctx, t.ConversationID)
		if err != nil {
			return err //nolint:wrapcheck // lỗi database của câu đọc một dòng
		}
		t.CustomerID = c
	}
	return autoHide(ctx, tx, d, msgID, t)
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
		return enqueueHide(ctx, tx, d, t.Account.TenantID, msgID)
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

// hidePayload là payload của lệnh ẩn tự động: chỉ mã tin (không nội dung).
type hidePayload struct {
	MessageID uuid.UUID `json:"messageId"`
}

// enqueueHide ghi lệnh ẩn tự động comment msgID (chống trùng theo tin).
func enqueueHide(ctx context.Context, tx pgx.Tx, d deps.Deps, tenant, msgID uuid.UUID) (uuid.UUID, error) {
	id, _, err := d.Queue.Enqueue(ctx, tx, HideCommandKind(), queue.NewJob{
		TenantID: &tenant, Payload: hidePayload{MessageID: msgID}, DedupeKey: "auto_hide:" + msgID.String(),
	})
	if err != nil {
		return uuid.Nil, err //nolint:wrapcheck // queue đã bọc
	}
	return id, nil
}
