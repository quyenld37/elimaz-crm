// Package loadstats đọc số đo của bài thử tải M4 (kế hoạch M4 Task 180, tools/loadkit fbstats): câu chỉ đọc bằng chủ
// bảng qua nhiều schema (conversation, inventory, platform), trong ngữ cảnh tenant thử (RLS áp cả chủ bảng). Là hạ tầng
// thử như internal/testkit: không dùng trong tiến trình ứng dụng.
//
// Khi nào không dùng: mã sản phẩm, báo cáo (module khác đọc qua view _v đã công bố).
package loadstats

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// queryTimeout là hạn của mỗi lần đọc.
const queryTimeout = 30 * time.Second

const (
	sqlFBEvents = `SELECT external_id, conversation_id::text FROM conversation.messages
WHERE direction = 'in' AND (starts_with(external_id, $1) OR starts_with(external_id, $2))`
	sqlFBMessages = `SELECT
	count(*) FILTER (WHERE starts_with(external_id, $1)),
	count(DISTINCT external_id) FILTER (WHERE starts_with(external_id, $1)),
	count(*) FILTER (WHERE starts_with(external_id, $2)),
	count(DISTINCT external_id) FILTER (WHERE starts_with(external_id, $2)),
	count(*) FILTER (WHERE starts_with(external_id, $2) AND is_live)
FROM conversation.messages WHERE direction = 'in' AND (starts_with(external_id, $1) OR starts_with(external_id, $2))`
	sqlFBSends = `SELECT count(*), count(*) FILTER (WHERE send_status = 'sent'),
	count(*) FILTER (WHERE send_status IN ('queued', 'checking')),
	count(*) FILTER (WHERE send_status IN ('in_doubt', 'failed', 'expired'))
FROM conversation.messages WHERE origin IN ('staff', 'automation') AND kind <> 'note' AND created_at >= $1`
	sqlFBStock = `SELECT coalesce(sum(on_hand), 0), coalesce(sum(held), 0), count(*) FILTER (WHERE held > on_hand)
FROM inventory.stock_balances`
	sqlFBReserved = `SELECT coalesce(sum(quantity), 0) FROM inventory.stock_reservations WHERE state = 'held'`
	sqlFBClaims   = `SELECT count(*), count(DISTINCT message_id) FROM conversation.live_comment_claims WHERE claimed_at >= $1`
	sqlFBJobs     = `SELECT count(*) FILTER (WHERE status = 'dead'), count(*) FILTER (WHERE status = 'in_doubt'),
	count(*) FILTER (WHERE status IN ('pending', 'running'))
FROM platform.jobs WHERE created_at >= $1`
	sqlFBDeadKinds = `SELECT kind, count(*) FROM platform.jobs WHERE status = 'dead' AND created_at >= $1 GROUP BY kind ORDER BY kind`
	sqlFBDeadlocks = `SELECT deadlocks FROM pg_catalog.pg_stat_database WHERE datname = current_database()`
	sqlFBHist      = `SELECT name, hist FROM platform.metrics_minute
WHERE name = ANY($1) AND hist IS NOT NULL AND bucket >= date_trunc('minute', $2::timestamptz)`
)

// HistNames là các chỉ số histogram bài M4 đọc từ platform.metrics_minute.
func HistNames() []string {
	return []string{"webhook_to_signal_ms", "live_comment_to_signal_ms", "send_click_to_api_ms", "job_pickup_ms"}
}

// Stats là số đo database của một lượt (tin, comment theo tiền tố mã) từ một lúc.
type Stats struct {
	At              time.Time        `json:"at"`
	InboxRows       int64            `json:"inbox_rows"`
	InboxDistinct   int64            `json:"inbox_distinct"`
	CommentRows     int64            `json:"comment_rows"`
	CommentDistinct int64            `json:"comment_distinct"`
	CommentLive     int64            `json:"comment_live"` // comment của lượt gắn phiên live (is_live)
	Sends           int64            `json:"sends"`        // tin gửi ra của nhân viên, tự động từ lúc bắt đầu
	SendsSent       int64            `json:"sends_sent"`
	SendsPending    int64            `json:"sends_pending"`
	SendsBad        int64            `json:"sends_bad"` // in_doubt, failed, expired
	OnHand          int64            `json:"on_hand"`
	Held            int64            `json:"held"`
	OverHeld        int64            `json:"over_held"` // dòng số dư có held > on_hand
	Reserved        int64            `json:"reserved"`  // tổng giữ đang hiệu lực ở sổ giữ
	Claims          int64            `json:"claims"`
	ClaimedComments int64            `json:"claimed_comments"`
	Dead            int64            `json:"dead"`
	DeadKinds       map[string]int64 `json:"dead_kinds"`
	InDoubt         int64            `json:"in_doubt"`
	Backlog         int64            `json:"backlog"` // việc pending, running
	Deadlocks       int64            `json:"deadlocks"`
	Hist            map[string][]int `json:"hist"`
}

// Event là một sự kiện khách đã có trong database: mã ngoài (mid, comment_id), hội thoại.
type Event struct {
	ExternalID   string
	Conversation string
}

// Events trả các tin vào của tenant có mã ngoài bắt đầu bằng inbox hay comment (tiền tố mã của lượt).
func Events(ctx context.Context, owner *db.DB, tenant uuid.UUID, inbox, comment string) ([]Event, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*queryTimeout)
	defer cancel()
	var out []Event
	err := tenancy.WithTenant(ctx, owner, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlFBEvents, inbox, comment)
		if err != nil {
			return fmt.Errorf("đọc sự kiện của lượt: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) {
			var e Event
			if err := r.Scan(&e.ExternalID, &e.Conversation); err != nil {
				return e, fmt.Errorf("đọc một sự kiện: %w", err)
			}
			return e, nil
		})
		if err != nil {
			return fmt.Errorf("đọc sự kiện của lượt: %w", err)
		}
		return nil
	})
	return out, err //nolint:wrapcheck // đã bọc bên trong
}

// Read đọc số đo trong tenant từ lúc from: tin vào có mã bắt đầu bằng inbox, comment (tiền tố mã của lượt), tin gửi
// ra, tồn, chốt, việc nền; deadlock của cả database; histogram HistNames của mọi tiến trình.
func Read(ctx context.Context, owner *db.DB, tenant uuid.UUID, inbox, comment string, from time.Time) (Stats, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	s := Stats{At: time.Now().UTC(), DeadKinds: map[string]int64{}, Hist: map[string][]int{}}
	if err := owner.Pool.QueryRow(ctx, sqlFBDeadlocks).Scan(&s.Deadlocks); err != nil {
		return s, fmt.Errorf("đọc số deadlock: %w", err)
	}
	if err := readHist(ctx, owner, from, s.Hist); err != nil {
		return s, err
	}
	in, lc := inbox, comment
	err := tenancy.WithTenant(ctx, owner, tenant, func(tx pgx.Tx) error {
		for _, q := range []struct {
			what string
			sql  string
			args []any
			dst  []any
		}{
			{
				"đếm tin, comment của lượt", sqlFBMessages,
				[]any{in, lc},
				[]any{&s.InboxRows, &s.InboxDistinct, &s.CommentRows, &s.CommentDistinct, &s.CommentLive},
			},
			{"đếm tin gửi ra", sqlFBSends, []any{from}, []any{&s.Sends, &s.SendsSent, &s.SendsPending, &s.SendsBad}},
			{"đọc số dư tồn", sqlFBStock, nil, []any{&s.OnHand, &s.Held, &s.OverHeld}},
			{"đọc sổ giữ hàng", sqlFBReserved, nil, []any{&s.Reserved}},
			{"đếm comment đã chốt", sqlFBClaims, []any{from}, []any{&s.Claims, &s.ClaimedComments}},
			{"đếm việc nền", sqlFBJobs, []any{from}, []any{&s.Dead, &s.InDoubt, &s.Backlog}},
		} {
			if err := tx.QueryRow(ctx, q.sql, q.args...).Scan(q.dst...); err != nil {
				return fmt.Errorf("%s: %w", q.what, err)
			}
		}
		rows, err := tx.Query(ctx, sqlFBDeadKinds, from)
		if err != nil {
			return fmt.Errorf("đọc loại việc dead: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			var n int64
			if err := rows.Scan(&k, &n); err != nil {
				return fmt.Errorf("đọc loại việc dead: %w", err)
			}
			s.DeadKinds[k] = n
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("đọc loại việc dead: %w", err)
		}
		return nil
	})
	return s, err //nolint:wrapcheck // đã bọc bên trong
}

// readHist cộng histogram của các chỉ số HistNames (mọi tiến trình, mọi nhãn) từ phút của from vào dst.
func readHist(ctx context.Context, owner *db.DB, from time.Time, dst map[string][]int) error {
	rows, err := owner.Pool.Query(ctx, sqlFBHist, HistNames(), from)
	if err != nil {
		return fmt.Errorf("đọc histogram: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var h []int
		if err := rows.Scan(&name, &h); err != nil {
			return fmt.Errorf("đọc một dòng histogram: %w", err)
		}
		acc := dst[name]
		if acc == nil {
			acc = make([]int, len(metrics.HistBoundsMs)+1)
		}
		for i := range min(len(h), len(acc)) {
			acc[i] += h[i]
		}
		dst[name] = acc
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("đọc histogram: %w", err)
	}
	return nil
}
