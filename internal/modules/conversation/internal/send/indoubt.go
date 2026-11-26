package send

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Không rõ đã gửi (Task 157; Q24, 04 mục 5.3): lệnh gửi in_doubt → tin checking (OnTerminal, merge.go); 60 giây đầu
// chờ echo (echo có metadata đặt tin sent, inbound/echo.go); quá 60 giây lịch send_reveal đổi tin sang in_doubt
// ("không rõ đã gửi chưa") để nhân viên chọn gửi lại hay bỏ (resend.go). Không bao giờ tự gửi lại.

// RevealSchedule là lịch hiện tin "không rõ đã gửi chưa" (theo tenant).
const RevealSchedule = "conversation.send_reveal"

const (
	// revealEvery: chu kỳ của lịch (kế hoạch M4-17).
	revealEvery = 15 * time.Second
	// revealAfter: tin checking chờ echo chừng này rồi mới hiện in_doubt.
	revealAfter = 60 * time.Second
	// revealBatch: số hội thoại tối đa mỗi kỳ (kỳ sau làm tiếp).
	revealBatch = 200
	// statusInDoubt là trạng thái tin "không rõ đã gửi chưa".
	statusInDoubt = "in_doubt"
)

// Lookup (commands.Looker) tra lại lệnh gửi in_doubt: tin đã có mã ngoài hay đã sent (echo đã khớp) → done; còn lại
// → unknown (giữ in_doubt, nhân viên quyết). Không bao giờ trả not_sent: không chắc kênh chưa nhận. Tin không còn
// hay payload hỏng → unknown.
func (c sendCommand) Lookup(ctx context.Context, j queue.Job) (string, error) {
	var p payload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.MessageID == uuid.Nil || j.TenantID == nil {
		return commands.StatusUnknown, nil //nolint:nilerr // payload hỏng: không tra được, giữ in_doubt
	}
	var confirmed bool
	err := tenancy.WithTenant(ctx, c.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		var err error
		confirmed, err = store.New(tx).GetSendLookup(ctx, p.MessageID)
		return err //nolint:wrapcheck // bọc ở dưới
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return commands.StatusUnknown, nil
	case err != nil:
		return commands.StatusUnknown, fmt.Errorf("tra lại tin gửi: %w", err)
	case confirmed:
		return commands.StatusDone, nil
	}
	return commands.StatusUnknown, nil
}

// RevealInDoubt đổi tin checking có send_checking_since trước now − 60 s sang in_doubt, trong tx tenant của bên gọi:
// khóa các hội thoại theo mã tăng dần (ADR-46 nhóm 6) rồi đổi tin có điều kiện (echo khớp trước đó thì tin đã sent,
// không đổi); báo hiệu mỗi hội thoại có tin đổi. Trả số tin đã đổi.
func RevealInDoubt(ctx context.Context, tx pgx.Tx, d deps.Deps, tenant uuid.UUID, now time.Time) (int, error) {
	q := store.New(tx)
	before := pgtype.Timestamptz{Time: now.UTC().Add(-revealAfter), Valid: true}
	ids, err := q.LockConversationsDueReveal(ctx, store.LockConversationsDueRevealParams{Before: before, Lim: revealBatch})
	if err != nil {
		return 0, fmt.Errorf("khóa hội thoại có tin chờ echo: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	rows, err := q.RevealInDoubtMessages(ctx, store.RevealInDoubtMessagesParams{ConversationIds: ids, Before: before})
	if err != nil {
		return 0, fmt.Errorf("hiện tin không rõ đã gửi: %w", err)
	}
	seen := map[uuid.UUID]bool{}
	for _, r := range rows {
		if seen[r.ConversationID] {
			continue
		}
		seen[r.ConversationID] = true
		if err := publish(ctx, tx, d.Bus, tenant, r.ChannelAccountID, r.ConversationID); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

// revealKind: việc của lịch; lỗi thì không chạy lại (kỳ sau có việc mới), quá một kỳ chưa chạy thì expired.
func revealKind() queue.Kind {
	return queue.Kind{Name: "task." + RevealSchedule, Policy: queue.Policy{MaxAttempts: 1, ExpiresAfter: revealEvery}}
}

func revealSchedule() schedules.Schedule {
	return schedules.Schedule{Name: RevealSchedule, Every: revealEvery, PerTenant: true, Kind: revealKind()}
}

// revealHandler chạy RevealInDoubt với đồng hồ của Deps (cùng đồng hồ với OnTerminal ghi send_checking_since) trong tx
// của việc (worker đặt WithTenant của tenant của việc).
type revealHandler struct{ d deps.Deps }

func (revealHandler) Kind() queue.Kind { return revealKind() }

// Handle hiện tin không rõ đã gửi của tenant của việc; kết quả {"revealed": n}.
func (h revealHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	if j.TenantID == nil {
		return nil, fmt.Errorf("%w: việc hiện tin không rõ đã gửi thiếu tenant", queue.ErrPermanent)
	}
	n, err := RevealInDoubt(ctx, tx, h.d, *j.TenantID, h.d.Clock()())
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(map[string]int{"revealed": n})
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả hiện tin không rõ đã gửi: %w", err)
	}
	return out, nil
}
