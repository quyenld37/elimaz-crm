package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
)

// TriggerInboxMessage là Trigger.Kind của tin khách mới trong hộp thư.
const TriggerInboxMessage = "inbox_message"

// Giới hạn của cấu hình greeting (openapi.yaml GreetingConfig, QuickReply; giới hạn của Messenger).
const (
	maxGreetingText   = 640
	maxGreetButtons   = 13
	maxGreetTitle     = 20
	maxGreetPayload   = 1000
	greetingRefPrefix = "greet:"
)

// GreetingQuickReply là một nút trả lời nhanh của tin chào.
type GreetingQuickReply struct {
	Title   string `json:"title"`
	Payload string `json:"payload"`
}

// GreetingConfig là cấu hình greeting: chữ (1–640 ký tự) và tối đa 13 nút (tiêu đề 1–20 ký tự, payload 1–1 000).
type GreetingConfig struct {
	Kind         string               `json:"kind"`
	Text         string               `json:"text"`
	QuickReplies []GreetingQuickReply `json:"quickReplies"`
}

// valid kiểm giới hạn của cấu hình (hợp đồng đã chặn ở HTTP; đây chặn bên gọi khác).
func (c GreetingConfig) valid() bool {
	if strings.TrimSpace(c.Text) == "" || utf8.RuneCountInString(c.Text) > maxGreetingText || len(c.QuickReplies) > maxGreetButtons {
		return false
	}
	for _, q := range c.QuickReplies {
		if t := utf8.RuneCountInString(q.Title); t == 0 || t > maxGreetTitle {
			return false
		}
		if p := utf8.RuneCountInString(q.Payload); p == 0 || p > maxGreetPayload {
			return false
		}
	}
	return true
}

// OnInboxMessage chạy quy tắc tin chào cho tin khách msgID vừa ghi vào hội thoại hộp thư t.ConversationID (hook của
// đường tin vào, Task 153; trong tx của sự kiện, hội thoại đã khóa). Task 167, Q11: chỉ chào khi đây là lần đầu khách
// nhắn Page — ngoài tin này hội thoại chỉ có tin hệ thống (đến từ quảng cáo) và khách không có hội thoại hộp thư nào
// khác với Page — rồi Run với ExternalRef "greet:<conversation_id>" (một lần mỗi hội thoại, kể cả khi xử lý lại) và
// ghi tin origin automation có nút trả lời nhanh cùng lệnh command.fb.auto_send. Tin lịch sử, của Page: Run bỏ qua.
func OnInboxMessage(ctx context.Context, tx pgx.Tx, d deps.Deps, msgID uuid.UUID, t Trigger) error {
	t.Kind = TriggerInboxMessage
	if t.FromPage || t.IsHistory {
		return nil // không đọc gì thêm (Run cũng bỏ qua)
	}
	rule, err := LoadRule(ctx, tx, t.Account.ID, KindGreeting)
	if err != nil || !rule.Enabled {
		return err
	}
	var c GreetingConfig
	if json.Unmarshal(rule.Config, &c) != nil || !c.valid() {
		return nil // cấu hình đã kiểm khi lưu; đọc lỗi coi như tắt
	}
	g, err := store.New(tx).GreetingEligible(ctx, store.GreetingEligibleParams{MessageID: msgID, ID: t.ConversationID})
	if err != nil {
		return fmt.Errorf("kiểm khách nhắn Page lần đầu: %w", err)
	}
	if !g.Eligible {
		return nil
	}
	t.CustomerID = g.CustomerID
	t.ExternalRef, t.SubjectRef = greetingRefPrefix+t.ConversationID.String(), t.ConversationID.String()
	in := send.AutoInput{Text: c.Text, QuickReplies: make([]channel.QuickReply, 0, len(c.QuickReplies))}
	for _, q := range c.QuickReplies {
		in.QuickReplies = append(in.QuickReplies, channel.QuickReply{Title: q.Title, Payload: q.Payload})
	}
	_, err = Run(ctx, tx, d, rule, t, func(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
		return send.EnqueueAuto(ctx, tx, d, t.Account, t.ConversationID, in, d.Clock()())
	})
	return err
}
