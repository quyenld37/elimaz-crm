// Package presence là phần của module conversation: ai đang xem, đang gõ, đang lên đơn trong một hội thoại (kế hoạch
// M4, Task 163; spec 04 mục 6). Màn hình gửi nhịp tim mỗi 10 s; dòng tự hết hạn sau 30 s; bảng UNLOGGED được phép mất.
// Báo hiệu conversation.presence chỉ phát khi trạng thái của một người đổi hoặc mới xuất hiện, và khi lịch dọn xóa dòng
// hết hạn của một hội thoại.
package presence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// State là việc một nhân viên đang làm trong hội thoại.
type State string

// Ba trạng thái (CHECK của conversation.presence, PresenceState của openapi.yaml).
const (
	Viewing       State = "viewing"
	Typing        State = "typing"
	DraftingOrder State = "drafting_order"
)

const (
	// TTL là thời gian một nhịp tim giữ trạng thái 30 s (màn hình gửi mỗi 10 s).
	TTL = 30 * time.Second
	// CleanupSchedule là tên lịch xóa dòng hết hạn (platform.schedules), mỗi 60 s cho từng tenant.
	CleanupSchedule = "conversation.presence_cleanup"
	cleanupEvery    = time.Minute
	// topic bằng conversation.TopicPresence (gói gốc nhập gói này nên không nhập ngược; test gói gốc giữ hai hằng khớp).
	topic = "conversation.presence"
	read  = identity.Permission("conversation.read")
)

// errInvalidState: trạng thái ngoài ba giá trị (lớp HTTP đã kiểm theo hợp đồng; tới đây là lỗi lập trình).
var errInvalidState = errors.New("presence: trạng thái không hợp lệ")

// errNoBus: tiến trình chưa nối realtime.Bus (lỗi cấu hình; trả lỗi thay vì panic, tx lùi).
var errNoBus = errors.New("presence: chưa nối bus báo hiệu")

// Entry là một người khác đang có mặt trong hội thoại.
type Entry struct {
	UserID      uuid.UUID
	DisplayName string
	State       State
}

func (s State) valid() bool { return s == Viewing || s == Typing || s == DraftingOrder }

// page trả Page của hội thoại convID khi u có conversation.read trên Page đó; hội thoại không có (kể cả của tenant
// khác: RLS) hoặc không có quyền → access.ErrNotFound.
func page(ctx context.Context, q *store.Queries, u identity.AuthUser, convID uuid.UUID) (uuid.UUID, error) {
	acc, err := q.ConversationPage(ctx, convID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, access.ErrNotFound
	case err != nil:
		return uuid.Nil, fmt.Errorf("đọc Page của hội thoại: %w", err)
	}
	if err := access.Require(u, read, acc); err != nil {
		return uuid.Nil, err //nolint:wrapcheck // sentinel của access, bên gọi kiểm bằng errors.Is
	}
	return acc, nil
}

// Set ghi nhịp tim của u trong hội thoại convID với trạng thái s lúc now (hạn now+TTL), trong tx của bên gọi (tenant
// của u). Phát một báo hiệu conversation.presence (trong tx) khi trạng thái của u đổi, hoặc u mới xuất hiện (chưa có
// dòng hay dòng đã hết hạn); nhịp tim cùng trạng thái chỉ gia hạn. Không có quyền trên Page → access.ErrNotFound.
func Set(ctx context.Context, tx pgx.Tx, bus realtime.Bus, u identity.AuthUser, convID uuid.UUID, s State, now time.Time) error {
	if !s.valid() {
		return fmt.Errorf("%w: %q", errInvalidState, s)
	}
	now = now.UTC()
	q := store.New(tx)
	acc, err := page(ctx, q, u, convID)
	if err != nil {
		return err
	}
	n, err := q.RefreshPresence(ctx, store.RefreshPresenceParams{
		ExpiresAt: now.Add(TTL), ConversationID: convID, UserID: u.UserID, State: string(s), Now: now,
	})
	if err != nil {
		return fmt.Errorf("gia hạn có mặt: %w", err)
	}
	if n > 0 {
		return nil
	}
	if err := q.UpsertPresence(ctx, store.UpsertPresenceParams{
		TenantID: u.TenantID, ConversationID: convID, UserID: u.UserID, State: string(s), ExpiresAt: now.Add(TTL),
	}); err != nil {
		return fmt.Errorf("ghi có mặt: %w", err)
	}
	return publish(ctx, tx, bus, u.TenantID, convID, acc)
}

// List trả những người khác (trừ u) còn hạn lúc now trong hội thoại convID, theo tên hiển thị. Không có quyền trên
// Page → access.ErrNotFound.
func List(ctx context.Context, tx pgx.Tx, u identity.AuthUser, convID uuid.UUID, now time.Time) ([]Entry, error) {
	q := store.New(tx)
	if _, err := page(ctx, q, u, convID); err != nil {
		return nil, err
	}
	rows, err := q.ListPresence(ctx, store.ListPresenceParams{ConversationID: convID, ViewerID: u.UserID, Now: now.UTC()})
	if err != nil {
		return nil, fmt.Errorf("đọc người có mặt: %w", err)
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, Entry{UserID: r.UserID, DisplayName: r.DisplayName, State: State(r.State)})
	}
	return out, nil
}

// Cleanup xóa mọi dòng hết hạn lúc now của tenant trong tx và phát một báo hiệu cho mỗi hội thoại bị đổi; trả số hội
// thoại bị đổi.
func Cleanup(ctx context.Context, tx pgx.Tx, bus realtime.Bus, tenantID uuid.UUID, now time.Time) (int, error) {
	rows, err := store.New(tx).DeleteExpiredPresence(ctx, now.UTC())
	if err != nil {
		return 0, fmt.Errorf("xóa có mặt hết hạn: %w", err)
	}
	for _, r := range rows {
		if err := publish(ctx, tx, bus, tenantID, r.ID, r.ChannelAccountID); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

// publish phát báo hiệu conversation.presence của hội thoại convID (chỉ mã, đòi conversation.read trên Page acc).
func publish(ctx context.Context, tx pgx.Tx, bus realtime.Bus, tenantID, convID, acc uuid.UUID) error {
	if bus == nil {
		return errNoBus
	}
	if err := bus.Publish(ctx, tx, realtime.Signal{
		TenantID: tenantID, Topic: topic, Requires: read, Scope: access.PageScope(acc),
		Entity: realtime.Entity{Type: "conversation", ID: convID.String()},
	}); err != nil {
		return fmt.Errorf("phát báo hiệu có mặt: %w", err)
	}
	return nil
}

// cleanupKind: việc của lịch; lỗi thì không chạy lại (kỳ sau có việc mới), quá một kỳ chưa chạy thì expired.
func cleanupKind() queue.Kind {
	return queue.Kind{Name: "task." + CleanupSchedule, Policy: queue.Policy{MaxAttempts: 1, ExpiresAfter: cleanupEvery}}
}

// cleanupHandler chạy Cleanup trong tx của việc (worker đặt WithTenant của tenant của việc).
type cleanupHandler struct{ d deps.Deps }

func (cleanupHandler) Kind() queue.Kind { return cleanupKind() }

// Handle xóa dòng hết hạn của tenant của việc; kết quả {"conversations": n}.
func (h cleanupHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	if j.TenantID == nil {
		return nil, fmt.Errorf("%w: việc dọn có mặt thiếu tenant", queue.ErrPermanent)
	}
	now := time.Now
	if h.d.Now != nil {
		now = h.d.Now
	}
	n, err := Cleanup(ctx, tx, h.d.Bus, *j.TenantID, now())
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(map[string]int{"conversations": n})
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả dọn có mặt: %w", err)
	}
	return out, nil
}

// Parts là phần chạy nền của presence: lịch conversation.presence_cleanup (60 s, theo tenant) và việc của nó.
func Parts(d deps.Deps) deps.Parts {
	return deps.Parts{
		Jobs: []queue.Handler{cleanupHandler{d: d}},
		Schedules: []schedules.Schedule{{
			Name: CleanupSchedule, Every: cleanupEvery, PerTenant: true, Kind: cleanupKind(),
		}},
	}
}
