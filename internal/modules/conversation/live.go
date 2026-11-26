package conversation

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/live"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/observability"
)

// Phiên live (Task 170; Q3, Q5, LV01, LV10). Hiện thực ở internal/live: ba đường nhận biết (webhook live_videos, lịch
// live.detect hỏi kênh mỗi 30 s, bấm tay), khóa thuê cho luồng đọc comment (chạy trong worker qua Background).

// LiveSession là một phiên live của Page.
type LiveSession = live.Session

// Trạng thái phiên live dùng ở bộ lọc.
const (
	LiveStatusLive  = live.StatusLive
	LiveStatusEnded = live.StatusEnded
)

// Lỗi của phiên live.
var (
	// ErrLiveVideoInvalid là lỗi: chuỗi theo dõi tay không có mã video (400).
	ErrLiveVideoInvalid = live.ErrVideoInvalid
	// ErrLiveStatusInvalid là lỗi: trạng thái lọc không phải live hay ended (400).
	ErrLiveStatusInvalid = live.ErrStatusInvalid
)

// StartLiveManually theo dõi tay phiên live của Page accountID theo mã video hay đường dẫn video (cần live.operate trên
// Page; không có Page hay quyền → ErrNotFound). Video đã có phiên → trả phiên đó.
func StartLiveManually(ctx context.Context, tx pgx.Tx, u identity.AuthUser, accountID uuid.UUID, videoIDOrURL string,
	now time.Time,
) (LiveSession, error) {
	return live.StartManual(ctx, tx, u, accountID, videoIDOrURL, now) //nolint:wrapcheck // sentinel của phần
}

// EndLiveManually kết thúc tay phiên id (cần live.operate trên Page của phiên); đã kết thúc → trả nguyên phiên.
func EndLiveManually(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID, now time.Time) (LiveSession, error) {
	return live.EndManual(ctx, tx, u, id, now) //nolint:wrapcheck // sentinel của phần
}

// ListLiveSessions trả phiên trên các Page u được đọc, lọc trạng thái (rỗng = mọi trạng thái) và mốc bắt đầu từ since.
func ListLiveSessions(ctx context.Context, tx pgx.Tx, u identity.AuthUser, status string, since *time.Time) ([]LiveSession, error) {
	return live.List(ctx, tx, u, status, since) //nolint:wrapcheck // sentinel của phần
}

// NewLiveAlertCommand trả lệnh cảnh báo của phiên live conversation.live_alert (live-source: luồng comment hỏng, đã
// chuyển sang hỏi định kỳ; live-missing: đối chiếu cuối phiên thiếu comment), mức thường, văn bản chỉ có mã phiên và
// con số; cmd/crm đăng ký cùng các lệnh cảnh báo khác (dùng chung Alerter).
func NewLiveAlertCommand(a observability.Alerter, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return live.NewAlertCommand(a, logger)
}
