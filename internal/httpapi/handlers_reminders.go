package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// CreateReminder (POST /api/v1/conversations/{id}/reminders, conversation.reply trên Page của hội thoại): nhắc việc
// "nhắc tôi lúc…" (Task 169). Giao người khác cần conversation.assign (403); hội thoại không có hay Page không được
// trả lời → 404; giờ nhắc không ở tương lai hay quá 90 ngày → 400. 201 kèm nhắc.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateReminder(ctx context.Context, req *gen.CreateReminderRequest, params gen.CreateReminderParams,
) (gen.CreateReminderRes, error) {
	in := conversation.ReminderInput{RemindAt: req.RemindAt, Note: req.Note.Or(""), UserID: optUUID(req.UserId)}
	return h.reminderWrite(ctx, http.StatusCreated, "tạo nhắc việc", func(tx pgx.Tx, u identity.AuthUser) (conversation.Reminder, error) {
		return conversation.CreateReminder(ctx, tx, u, params.ID, in, h.now())
	})
}

// CompleteReminder (POST /api/v1/reminders/{id}/done): đánh dấu xong; đã đóng (kể cả lần song song thắng trước) → 409.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CompleteReminder(ctx context.Context, params gen.CompleteReminderParams) (gen.CompleteReminderRes, error) {
	return h.reminderWrite(ctx, http.StatusOK, "nhắc việc xong", func(tx pgx.Tx, u identity.AuthUser) (conversation.Reminder, error) {
		return conversation.CompleteReminder(ctx, tx, u, params.ID)
	})
}

// CancelReminder (POST /api/v1/reminders/{id}/cancel): hủy nhắc; đã đóng → 409.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CancelReminder(ctx context.Context, params gen.CancelReminderParams) (gen.CancelReminderRes, error) {
	return h.reminderWrite(ctx, http.StatusOK, "hủy nhắc việc", func(tx pgx.Tx, u identity.AuthUser) (conversation.Reminder, error) {
		return conversation.CancelReminder(ctx, tx, u, params.ID)
	})
}

// ListMyReminders (GET /api/v1/me/reminders?status=pending|fired, mặc định pending): nhắc của chính người gọi.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListMyReminders(ctx context.Context, params gen.ListMyRemindersParams) (gen.ListMyRemindersRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	status := string(params.Status.Or(gen.ListMyRemindersStatusPending))
	var items []conversation.Reminder
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		items, err = conversation.ListMyReminders(ctx, tx, u.UserID, status)
		return err //nolint:wrapcheck // bọc ở dưới
	})
	if err != nil {
		return nil, fmt.Errorf("nhắc việc của tôi: %w", err)
	}
	out := &gen.ReminderList{Items: make([]gen.Reminder, 0, len(items))}
	for _, r := range items {
		out.Items = append(out.Items, *reminderOut(r))
	}
	return out, nil
}

// reminderWrite chạy change trong refusableWrite (lỗi nghiệp vụ hộp chat không lưu theo khóa) và trả thân Reminder.
func (h *handler) reminderWrite(ctx context.Context, status int, what string,
	change func(tx pgx.Tx, u identity.AuthUser) (conversation.Reminder, error),
) (*gen.Reminder, error) {
	out, err := h.refusableWrite(ctx, status, isConversationRefusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		r, err := change(tx, u)
		if err != nil {
			return nil, err
		}
		return reminderOut(r), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Reminder), nil //nolint:forcetypeassert // fn trên trả *gen.Reminder
}

func reminderOut(r conversation.Reminder) *gen.Reminder {
	out := &gen.Reminder{
		ID: r.ID, ConversationId: r.ConversationID, UserId: r.UserID, RemindAt: r.RemindAt,
		Status: gen.ReminderStatus(r.Status), Source: gen.ReminderSource(r.Source), Version: r.Version, CreatedAt: r.CreatedAt,
	}
	if r.Note != "" {
		out.Note = gen.NewOptString(r.Note)
	}
	if r.FiredAt != nil {
		out.FiredAt = gen.NewOptDateTime(*r.FiredAt)
	}
	return out
}
