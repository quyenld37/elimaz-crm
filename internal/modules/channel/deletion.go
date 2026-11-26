package channel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/observability/alertjobs"
)

// Xóa dữ liệu đăng nhập Facebook theo yêu cầu của Meta và nhắc Data Use Checkup (kế hoạch M4, Task 179; spec 02 mục 5;
// Q30: chỉ dữ liệu đăng nhập của người dùng app, tức phiên OAuth của nhân viên; xóa dữ liệu khách là việc của customer).

const (
	// DataUseCheckupSchedule là lịch hằng ngày nhắc Data Use Checkup hằng năm của app Meta.
	DataUseCheckupSchedule = "channel.data_use_checkup"
	// DataUseCheckupEvery là chu kỳ của lịch.
	DataUseCheckupEvery = 24 * time.Hour
	// checkupLeadDays: còn chừng này ngày (hoặc ít hơn, kể cả đã quá hạn) thì nhắc.
	checkupLeadDays = 30
	// checkupRepeat: đã nhắc trong khoảng này thì không nhắc lại.
	checkupRepeat   = 7 * 24 * time.Hour
	checkupKeyStart = "data-use-checkup:"
)

// deletionParts: phần chạy nền của vùng này đăng ký ở cmd/crm (lịch platform.data_deletion của
// internal/platform/datadeletion gọi DeleteOAuthDataFor; lịch DataUseCheckupSchedule cùng lệnh của nó cần Alerter, dựng
// một lần ở cmd/crm), nên không có phần nào ở đây.
func deletionParts(_ Deps) parts { return parts{} }

// DeleteOAuthDataFor xóa mọi phiên đăng nhập Facebook (channel.oauth_sessions, kể cả token người dùng mã hóa) của
// người dùng app có băm userRefHash (datadeletion.UserRefHash) trong tenant của tx; trả số dòng đã xóa. Là
// datadeletion.Deleter: chạy lại được, không gọi mạng. Khóa ADR-46 nhóm 3.
func DeleteOAuthDataFor(ctx context.Context, tx pgx.Tx, userRefHash string) (int64, error) {
	n, err := store.New(tx).DeleteOAuthSessionsByUserRef(ctx, userRefHash)
	if err != nil {
		return 0, fmt.Errorf("xóa phiên đăng nhập Facebook theo yêu cầu xóa: %w", err)
	}
	return n, nil
}

// NewDataUseCheckupCommand trả lệnh của lịch DataUseCheckupSchedule (alertjobs.NewScheduledCommand, Kind
// "task.channel.data_use_checkup"): hạn due (META_DATA_USE_CHECKUP_DUE) còn ≤ 30 ngày (kể cả đã quá hạn) và key
// "data-use-checkup:<hạn>" chưa gửi trong 7 ngày → cảnh báo thường; hạn chưa đặt hay còn xa → done, không gửi. Lỗi đọc
// mốc gửi trước hay lỗi gửi → not_sent (chạy lại trong kỳ). a nil → không gửi được, lỗi. now nil → giờ chạy của lệnh.
// cmd/crm định nghĩa lịch và đăng ký lệnh cùng Alerter.
func NewDataUseCheckupCommand(d *db.DB, a observability.Alerter, due time.Time, now func() time.Time, //nolint:ireturn // Runner nhận Handler
	logger *slog.Logger,
) commands.Handler {
	return alertjobs.NewScheduledCommand(DataUseCheckupSchedule, DataUseCheckupEvery, logger,
		func(ctx context.Context, at time.Time) (any, error) {
			if now != nil {
				at = now()
			}
			return dataUseCheckup(ctx, d, a, due, at)
		})
}

// dataUseCheckup làm một lượt nhắc lúc at; trả kết quả của lệnh.
func dataUseCheckup(ctx context.Context, d *db.DB, a observability.Alerter, due, at time.Time) (any, error) {
	if due.IsZero() {
		return map[string]any{"due": false}, nil
	}
	today := at.UTC().Truncate(24 * time.Hour)
	days := int(due.UTC().Truncate(24*time.Hour).Sub(today).Hours() / 24)
	if days > checkupLeadDays {
		return map[string]any{"due": false, "daysLeft": days}, nil
	}
	if a == nil {
		return nil, errNoAlerter
	}
	key := checkupKeyStart + due.UTC().Format(time.DateOnly)
	recent, err := observability.AlertSentWithin(ctx, d, key, checkupRepeat)
	if err != nil {
		return nil, err //nolint:wrapcheck // observability đã bọc
	}
	if recent {
		return map[string]any{"due": true, "daysLeft": days, "skipped": true}, nil
	}
	date := due.UTC().Format("02/01/2006")
	text := fmt.Sprintf("Data Use Checkup của app Meta còn %d ngày (hạn %s): quản trị app vào App Dashboard hoàn tất "+
		"kiểm tra, không thì app có thể mất quyền", days, date)
	if days < 0 {
		text = fmt.Sprintf("Data Use Checkup của app Meta đã quá hạn %d ngày (hạn %s): quản trị app hoàn tất ngay "+
			"trong App Dashboard", -days, date)
	}
	if err := a.Notify(ctx, key, observability.SeverityNormal, text); err != nil {
		return nil, err //nolint:wrapcheck // Alerter đã bọc (giữ TelegramRetryError cho RetryAfter)
	}
	return map[string]any{"due": true, "daysLeft": days}, nil
}

// errNoAlerter báo tiến trình không có Alerter (cmd/crm luôn truyền; chỉ để không panic).
var errNoAlerter = errors.New("channel: thiếu Alerter để nhắc Data Use Checkup")
