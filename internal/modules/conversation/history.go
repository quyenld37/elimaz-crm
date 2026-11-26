package conversation

import "elimaz.com.vn/crm/internal/modules/conversation/internal/history"

// Lịch sử tin cũ và hạn giữ hội thoại (Task 164, Q33, Q17).
const (
	// FetchHistoryKind là lệnh chỉ đọc lấy 20 tin gần nhất của hội thoại hộp thư mới.
	FetchHistoryKind = history.FetchHistoryKind
	// RetentionSchedule là lịch dọn hội thoại quá hạn giữ (CONVERSATION_RETENTION_MONTHS).
	RetentionSchedule = history.RetentionSchedule
)
