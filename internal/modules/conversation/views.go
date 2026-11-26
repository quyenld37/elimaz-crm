package conversation

// View công bố của conversation (migration 0402, Task 151; cột và mốc đọc ở docs/views.md). Module khác và
// internal/queries chỉ đọc dữ liệu hội thoại qua các view này (security_invoker: RLS của bảng gốc áp cho người đọc).
const (
	// ViewConversations: tenant_id, id, channel_account_id, customer_id, kind, status, assignee_id, post_id,
	// last_message_at, last_customer_message_at, created_at (M6, M7, dòng thời gian khách).
	ViewConversations = "conversation.conversations_v"
	// ViewTags: tenant_id, id, name, color, sort; chỉ nhãn chưa xóa (M7 báo cáo sẵn sàng).
	ViewTags = "conversation.tags_v"
	// ViewReplyTemplates: tenant_id, id, channel_account_id, shortcut, title, created_at; chỉ mẫu chưa xóa, không có
	// thân (M7 báo cáo sẵn sàng, Task 161).
	ViewReplyTemplates = "conversation.reply_templates_v"
)
