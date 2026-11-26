package live

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Danh sách "đã chốt, thiếu địa chỉ" của phiên live (kế hoạch M4, Task 173; LV04, LV05, LV06). Hoàn tất đơn là
// updateOrder + confirmOrder của M2; nháp quá hạn do lịch order.expire_drafts của M2 hủy — danh sách chỉ đọc lại.

// RemindEvery là khoảng tối thiểu giữa hai lần nhắc cùng một đơn.
const RemindEvery = 60 * time.Minute

// remindedFmt là lý do khi đơn đã được nhắc trong RemindEvery (giờ Việt Nam).
const remindedFmt = "Đã nhắc lúc %s"

// Trạng thái gửi của tin xin địa chỉ không tới khách (messages.send_status).
const (
	sendFailed  = "failed"
	sendExpired = "expired"
)

// CanRemind là kết quả xét nhắc một đơn: được hay không, lý do (câu tiếng Việt).
type CanRemind struct {
	Allowed bool
	Reason  string
}

// PendingRow là một đơn còn nháp của phiên: đơn, khách, dòng, tổng, hạn nháp, ghi nhận chính (người chốt, lúc chốt,
// kết quả xin địa chỉ — Reason là lý do bỏ, hay lý do tin xin địa chỉ không tới khách), khách đã nhắn vào hộp thư sau lúc
// chốt, xét nhắc lúc này.
type PendingRow struct {
	OrderID                 uuid.UUID
	OrderCode               string
	CustomerID              uuid.UUID
	CustomerName            string
	Items                   []DraftItem
	GrandTotal              int64
	ExpiresAt               *time.Time
	ClaimedBy               uuid.UUID
	ClaimedByName           string
	ClaimedAt               time.Time
	Ask                     Ask
	CustomerRepliedAfterAsk bool
	CanRemind               CanRemind
}

// PendingList là danh sách đơn còn nháp của phiên và số đơn của phiên đã xác nhận, đã hủy vì quá hạn nháp.
type PendingList struct {
	Items                        []PendingRow
	ExpiredCount, ConfirmedCount int
}

// Pending trả danh sách đơn còn nháp có ghi nhận trong phiên live sessionID (theo giờ chốt, tối đa 500) cho u lúc now
// (cần conversation.read trên Page; không có, phiên lạ → ErrNotFound), trong tx tenant của bên gọi; không khóa. reg
// là bộ adapter để xét nhắc theo chính sách của kênh (như send.PlanRemind, không khóa, không ghi).
func Pending(ctx context.Context, tx pgx.Tx, reg *channel.Registry, u identity.AuthUser, sessionID uuid.UUID, now time.Time,
) (PendingList, error) {
	q := store.New(tx)
	if _, err := sessionFor(ctx, q, u, permRead, sessionID); err != nil {
		return PendingList{}, err
	}
	now = now.UTC()
	rows, err := q.PendingDraftsOfSession(ctx, sessionID)
	if err != nil {
		return PendingList{}, fmt.Errorf("đọc đơn nháp của phiên: %w", err)
	}
	counts, err := q.SessionClaimCounts(ctx, sessionID)
	if err != nil {
		return PendingList{}, fmt.Errorf("đếm đơn của phiên: %w", err)
	}
	out := PendingList{Items: make([]PendingRow, 0, len(rows)), ConfirmedCount: int(counts.Confirmed), ExpiredCount: int(counts.Expired)}
	ids := make([]uuid.UUID, 0, len(rows))
	at := make(map[uuid.UUID]int, len(rows))
	for _, r := range rows {
		p, err := pendingRow(ctx, tx, reg, r, now)
		if err != nil {
			return PendingList{}, err
		}
		at[r.OrderID] = len(out.Items)
		out.Items = append(out.Items, p)
		ids = append(ids, r.OrderID)
	}
	if len(ids) == 0 {
		return out, nil
	}
	items, err := q.DraftItems(ctx, ids)
	if err != nil {
		return PendingList{}, fmt.Errorf("đọc dòng đơn nháp: %w", err)
	}
	for _, it := range items {
		i := at[it.OrderID]
		out.Items[i].Items = append(out.Items[i].Items, DraftItem{VariantID: it.VariantID, Name: it.NameSnapshot, Qty: it.Quantity})
	}
	return out, nil
}

// pendingRow dựng một dòng (chưa có dòng hàng) và xét nhắc lúc now.
func pendingRow(ctx context.Context, tx pgx.Tx, reg *channel.Registry, r store.PendingDraftsOfSessionRow, now time.Time,
) (PendingRow, error) {
	p := PendingRow{
		OrderID: r.OrderID, OrderCode: r.Code, CustomerID: r.CustomerID, CustomerName: r.CustomerName, Items: []DraftItem{},
		GrandTotal: r.GrandTotal, ClaimedBy: r.ClaimedBy, ClaimedByName: r.ClaimedByName, ClaimedAt: r.ClaimedAt.UTC(),
		Ask: Ask{Status: r.AskStatus, Reason: r.AskReason.String}, CustomerRepliedAfterAsk: r.RepliedAfter,
	}
	if r.DraftExpiresAt.Valid {
		t := r.DraftExpiresAt.Time.UTC()
		p.ExpiresAt = &t
	}
	if r.AskSendStatus == sendFailed || r.AskSendStatus == sendExpired {
		p.Ask.Reason = send.FailReasonText(r.AskFailReason)
	}
	if why := remindedRecently(r.LastRemindedAt, now); why != "" {
		p.CanRemind = CanRemind{Reason: why}
		return p, nil
	}
	inbox, err := send.InboxFor(ctx, tx, r.ConversationID)
	if err != nil {
		return PendingRow{}, err //nolint:wrapcheck // send đã bọc việc đang làm
	}
	ok, why, err := send.RemindCheck(ctx, tx, reg, r.MessageID, inbox, now)
	if err != nil {
		return PendingRow{}, err //nolint:wrapcheck // send đã bọc việc đang làm
	}
	p.CanRemind = CanRemind{Allowed: ok, Reason: why}
	return p, nil
}

// remindedRecently trả "Đã nhắc lúc HH:MM" (giờ Việt Nam) khi lần nhắc cuối last cách now chưa tới RemindEvery; rỗng
// khi chưa nhắc hay đã đủ lâu.
func remindedRecently(last pgtype.Timestamptz, now time.Time) string {
	if !last.Valid || !last.Time.After(now.Add(-RemindEvery)) {
		return ""
	}
	return fmt.Sprintf(remindedFmt, last.Time.In(vietnam()).Format("15:04"))
}

// sessionFor đọc phiên live id và đòi quyền perm trên Page của phiên (không có, phiên lạ → ErrNotFound).
func sessionFor(ctx context.Context, q *store.Queries, u identity.AuthUser, perm identity.Permission, id uuid.UUID,
) (store.ConversationLiveSession, error) {
	row, err := q.GetLiveSession(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return store.ConversationLiveSession{}, ErrNotFound
	case err != nil:
		return store.ConversationLiveSession{}, fmt.Errorf("đọc phiên live: %w", err)
	}
	if err := access.Require(u, perm, row.ChannelAccountID); err != nil {
		return store.ConversationLiveSession{}, err //nolint:wrapcheck // sentinel của phần (404)
	}
	return row, nil
}

// vietnam là múi giờ Việt Nam (UTC+7, không đổi giờ) để hiện giờ nhắc.
func vietnam() *time.Location { return time.FixedZone("ICT", 7*3600) }
