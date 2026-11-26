package automation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/customer"
)

// Outcome là kết quả một lượt quy tắc gặp sự kiện (cột automation_runs.outcome). OutcomeNone và OutcomeAlreadyRun
// không ghi dòng nào.
type Outcome string

// Kết quả của Run và của lệnh tự động.
const (
	// OutcomeNone: quy tắc tắt, hay sự kiện của chính Page / tin lịch sử (ADR-13): không làm gì, không ghi.
	OutcomeNone Outcome = ""
	// OutcomeAlreadyRun: sự kiện đã có lượt chạy (xử lý lại): dừng, không ghi thêm.
	OutcomeAlreadyRun Outcome = "already_run"

	OutcomeEnqueued        Outcome = "enqueued"
	OutcomeDone            Outcome = "done"
	OutcomeFailed          Outcome = "failed"
	OutcomeDropped         Outcome = "dropped"
	OutcomeSkippedStale    Outcome = "skipped_stale"
	OutcomeSkippedDNC      Outcome = "skipped_dnc"
	OutcomeSkippedLimit    Outcome = "skipped_limit"
	OutcomeSkippedPolicy   Outcome = "skipped_policy"
	OutcomeSkippedReadOnly Outcome = "skipped_read_only"
	OutcomeSkippedLive     Outcome = "skipped_live"
)

// TriggerCommentCreated là Trigger.Kind của comment khách mới.
const TriggerCommentCreated = "comment_created"

// defaultMaxAge là tuổi tối đa của sự kiện khi Page chưa lưu cài đặt (cột page_settings.automation_max_age_minutes).
const defaultMaxAge = 15 * time.Minute

// Trigger là sự kiện có thể kích hoạt quy tắc (kế hoạch M4, Task 165). ExternalRef là khóa chống chạy hai lần của
// lượt (vd "hide:<comment_id>"); SubjectRef là đối tượng để đếm giới hạn theo loại (không chứa dữ liệu cá nhân).
type Trigger struct {
	Kind                                    string
	Account                                 channel.ChannelAccount
	ConversationID, CustomerID              uuid.UUID
	ExternalRef, SubjectRef                 string
	OccurredAt                              time.Time
	FromPage, IsHistory, IsLive             bool
	Text, CommentExternalID, PostExternalID string
}

// skipError là lỗi act trả để đổi lượt vừa ghi sang kết quả bỏ qua của riêng loại quy tắc (giới hạn, live, chính
// sách) mà không ghi lệnh.
type skipError struct{ outcome Outcome }

func (e *skipError) Error() string { return "automation: bỏ qua (" + string(e.outcome) + ")" }

// Skip trả lỗi act dùng để báo lượt bị bỏ qua với kết quả o (OutcomeSkippedLimit, OutcomeSkippedLive,
// OutcomeSkippedPolicy): Run ghi o vào lượt, không lùi transaction, không trả lỗi.
func Skip(o Outcome) error { return &skipError{outcome: o} }

// Run chạy quy tắc rule với sự kiện t trong tx của sự kiện (kế hoạch M4, Task 165; 04 mục 5.3, 05 mục 3). Cổng theo
// thứ tự: quy tắc tắt → OutcomeNone (không ghi); FromPage hoặc IsHistory → OutcomeNone (chống vòng lặp, ADR-13); sự
// kiện cũ hơn automation_max_age_minutes của Page → skipped_stale; Page read_only → skipped_read_only; quy tắc gửi tin
// (không phải ẩn) và khách do_not_contact → skipped_dnc; rồi chèn lượt (ON CONFLICT DO NOTHING: đã có → dừng,
// OutcomeAlreadyRun); cổng bỏ qua cũng ghi lượt. Qua mọi cổng: gọi act (ghi lệnh trong tx) rồi gắn mã lệnh vào lượt
// (enqueued); act trả Skip(o) → lượt thành o. Lỗi của act, database: trả lên (tx lùi, sự kiện chạy lại).
func Run(ctx context.Context, tx pgx.Tx, d deps.Deps, rule Rule, t Trigger,
	act func(ctx context.Context, tx pgx.Tx) (jobID uuid.UUID, err error),
) (Outcome, error) {
	if !rule.Enabled || rule.ID == uuid.Nil || t.FromPage || t.IsHistory {
		return OutcomeNone, nil
	}
	gate, err := gateOutcome(ctx, tx, d, rule, t)
	if err != nil {
		return OutcomeNone, err
	}
	q := store.New(tx)
	runID, err := q.InsertAutomationRun(ctx, store.InsertAutomationRunParams{
		RuleID: rule.ID, ExternalRef: t.ExternalRef, SubjectRef: pgtype.Text{String: t.SubjectRef, Valid: t.SubjectRef != ""},
		Outcome: string(gate),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return OutcomeAlreadyRun, nil
	case err != nil:
		return OutcomeNone, fmt.Errorf("ghi lượt quy tắc tự động: %w", err)
	case gate != OutcomeEnqueued:
		return gate, nil
	}
	jobID, err := act(ctx, tx)
	out := OutcomeEnqueued
	if se, ok := errors.AsType[*skipError](err); ok {
		out, jobID, err = se.outcome, uuid.Nil, nil
	}
	if err != nil {
		return OutcomeNone, err
	}
	if err := q.SetAutomationRunJob(ctx, store.SetAutomationRunJobParams{
		CommandJobID: pgtype.UUID{Bytes: jobID, Valid: jobID != uuid.Nil}, Outcome: string(out), ID: runID,
	}); err != nil {
		return OutcomeNone, fmt.Errorf("gắn lệnh vào lượt quy tắc tự động: %w", err)
	}
	return out, nil
}

// gateOutcome là kết quả của các cổng chung (cũ, chỉ xem, không liên hệ); qua hết → OutcomeEnqueued.
func gateOutcome(ctx context.Context, tx pgx.Tx, d deps.Deps, rule Rule, t Trigger) (Outcome, error) {
	maxAge, err := maxEventAge(ctx, tx, t.Account.ID)
	if err != nil {
		return OutcomeNone, err
	}
	switch {
	case d.Clock()().Sub(t.OccurredAt) > maxAge:
		return OutcomeSkippedStale, nil
	case t.Account.SendMode != channel.SendModeEnabled:
		return OutcomeSkippedReadOnly, nil
	case rule.Kind == KindAutoHide || t.CustomerID == uuid.Nil:
		return OutcomeEnqueued, nil
	}
	dnc, err := customer.IsDoNotContact(ctx, tx, t.CustomerID)
	if err != nil {
		return OutcomeNone, err //nolint:wrapcheck // customer đã bọc việc đang làm
	}
	if dnc {
		return OutcomeSkippedDNC, nil
	}
	return OutcomeEnqueued, nil
}

// maxEventAge là tuổi tối đa sự kiện tự động của Page (cài đặt Page, chưa lưu → 15 phút).
func maxEventAge(ctx context.Context, tx pgx.Tx, pageID uuid.UUID) (time.Duration, error) {
	s, err := store.New(tx).GetPageSettings(ctx, pageID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return defaultMaxAge, nil
	case err != nil:
		return 0, fmt.Errorf("đọc tuổi tối đa sự kiện tự động: %w", err)
	}
	return time.Duration(s.AutomationMaxAgeMinutes) * time.Minute, nil
}
