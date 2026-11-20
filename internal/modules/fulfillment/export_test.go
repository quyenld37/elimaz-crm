package fulfillment

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
)

// Cửa cho test gói fulfillment_test: hai hàm phân loại lỗi của lệnh tra (bảng ca ở track_command_test.go), lệnh báo
// phí có hạn gọi hãng ngắn và hàm dọn báo phí (quote_flow_test.go).

// TransientDBError là transientDBError.
func TransientDBError(err error) bool { return transientDBError(err) }

// ClassifyTrackError là classifyTrackError.
func ClassifyTrackError(err error) (commands.Outcome, string) { return classifyTrackError(err) }

// NewQuoteCommandTimeout là NewQuoteCommand với hạn gọi hãng d (test hãng treo không phải chờ 10 s).
func NewQuoteCommandTimeout(s *Service, d time.Duration) *QuoteCommand {
	return &QuoteCommand{s: s, callTimeout: d}
}

// CleanupQuotes là cleanupQuotes (hàm dọn của vùng quotes).
func CleanupQuotes(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error) {
	return cleanupQuotes(ctx, tx, now)
}
