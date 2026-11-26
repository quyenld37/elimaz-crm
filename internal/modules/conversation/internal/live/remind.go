package live

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Nhắc một chạm khách còn đơn nháp gửi địa chỉ (kế hoạch M4, Task 173; LV05).

// Kết quả nhắc một đơn (RemindOutcome.Outcome).
const (
	RemindQueued  = "queued"
	RemindSkipped = "skipped"
)

// Lý do bỏ nhắc ngoài chính sách gửi.
const (
	// NotDraftText: đơn đã xác nhận, hủy hay hết hạn.
	NotDraftText = "Đơn không còn là nháp"
	// NotInSessionText: đơn không có ghi nhận chốt trong phiên.
	NotInSessionText = "Đơn không có trong phiên live này"
	// NoChannelText: hộp thư quá 24 giờ và comment đã nhắn riêng (send.RemindText).
	NoChannelText = send.RemindText
)

// RemindInput là một lần bấm nhắc: các đơn (rỗng: mọi đơn còn nháp của phiên) và mẫu tin nhắc.
type RemindInput struct {
	OrderIDs   []uuid.UUID
	TemplateID uuid.UUID
}

// RemindOutcome là kết quả nhắc một đơn: queued (MessageID là tin vừa vào hàng gửi) hay skipped kèm lý do.
type RemindOutcome struct {
	OrderID         uuid.UUID
	Outcome, Reason string
	MessageID       *uuid.UUID
}

// RemindEnv là phụ thuộc của Remind: đường gửi tin và điền mẫu templateID cho hội thoại comment conversationID với
// đơn orderID, người gửi actor (nil: tên Page) (templates.Render).
type RemindEnv struct {
	Send   send.Env
	Render func(ctx context.Context, tx pgx.Tx, templateID, conversationID, orderID uuid.UUID, actor *identity.AuthUser) (string, error)
}

// Remind nhắc khách của các đơn in.OrderIDs (rỗng: mọi đơn còn nháp) có ghi nhận trong phiên live sessionID gửi địa
// chỉ, bằng nhân viên u lúc now, trong tx tenant của bên gọi (WriteTx). Cần live.operate trên Page của phiên (không có,
// phiên lạ → ErrNotFound). Mỗi đơn một kết quả và (khi gửi) một tin, một lệnh gửi riêng; đơn bị bỏ không chặn đơn khác:
//
//   - đơn không có ghi nhận trong phiên → skipped NotInSessionText; không còn nháp → skipped NotDraftText;
//   - đã nhắc trong RemindEvery → skipped "Đã nhắc lúc HH:MM";
//   - chính sách gửi (send.PlanRemind: hộp thư còn cửa sổ → tin thường; không thì nhắn riêng comment chính nếu còn
//     lượt) không cho → skipped kèm lý do;
//   - còn lại: ghi lần nhắc vào ghi nhận chính (câu UPDATE có điều kiện 60 phút; 0 dòng → skipped "Đã nhắc lúc") rồi
//     gửi tin đã điền mẫu (người gửi là u).
//
// Thứ tự khóa (ADR-46 nhóm 6 rồi bảng của conversation, rồi tin, việc): mọi hội thoại comment chính và hộp thư của các
// đơn nháp trong một câu ORDER BY id FOR UPDATE → mỗi đơn: dòng ghi nhận chính → tin, lệnh (dòng mới). Mẫu không có →
// ErrNotFound (kiểm trước mọi đơn); lỗi hạ tầng → trả lỗi, bên gọi lùi cả lần.
func Remind(ctx context.Context, tx pgx.Tx, env RemindEnv, u identity.AuthUser, sessionID uuid.UUID, in RemindInput,
	now time.Time,
) ([]RemindOutcome, error) {
	now = now.UTC()
	q := store.New(tx)
	ses, err := sessionFor(ctx, q, u, permOperate, sessionID)
	if err != nil {
		return nil, err
	}
	switch _, err := q.GetReplyTemplate(ctx, in.TemplateID); {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("đọc mẫu tin nhắc: %w", err)
	}
	want := uniq(in.OrderIDs)
	rows, err := q.SessionOrderClaims(ctx, store.SessionOrderClaimsParams{OrderIds: want, LiveSessionID: sessionID})
	if err != nil {
		return nil, fmt.Errorf("đọc đơn của phiên để nhắc: %w", err)
	}
	inboxes, err := lockForRemind(ctx, tx, q, rows)
	if err != nil {
		return nil, err
	}
	var actor *identity.AuthUser
	if identity.Can(u, permRead, access.PageScope(ses.ChannelAccountID)) {
		actor = &u
	}
	out := make([]RemindOutcome, 0, max(len(rows), len(want)))
	seen := make(map[uuid.UUID]bool, len(rows))
	for _, r := range rows {
		seen[r.OrderID] = true
		res, err := remindOne(ctx, tx, env, u, actor, in.TemplateID, r, inboxes[r.OrderID], now)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	for _, id := range want {
		if !seen[id] {
			out = append(out, RemindOutcome{OrderID: id, Outcome: RemindSkipped, Reason: NotInSessionText})
		}
	}
	return out, nil
}

// lockForRemind khóa hội thoại comment chính và hộp thư (InboxFor) của mọi đơn nháp trong rows theo id tăng dần; trả
// hộp thư theo đơn (uuid.Nil: khách chưa có hộp thư).
func lockForRemind(ctx context.Context, tx pgx.Tx, q *store.Queries, rows []store.SessionOrderClaimsRow,
) (map[uuid.UUID]uuid.UUID, error) {
	inboxes := make(map[uuid.UUID]uuid.UUID, len(rows))
	ids := make([]uuid.UUID, 0, 2*len(rows))
	for _, r := range rows {
		if r.Status != order.StatusDraft {
			continue
		}
		inbox, err := send.InboxFor(ctx, tx, r.ConversationID)
		if err != nil {
			return nil, err //nolint:wrapcheck // send đã bọc việc đang làm
		}
		inboxes[r.OrderID] = inbox
		ids = append(ids, r.ConversationID)
		if inbox != uuid.Nil {
			ids = append(ids, inbox)
		}
	}
	if len(ids) == 0 {
		return inboxes, nil
	}
	if _, err := q.LockConversationsByID(ctx, uniq(ids)); err != nil {
		return nil, fmt.Errorf("khóa hội thoại để nhắc: %w", err)
	}
	return inboxes, nil
}

// remindOne nhắc một đơn r (xem Remind).
func remindOne(ctx context.Context, tx pgx.Tx, env RemindEnv, u identity.AuthUser, actor *identity.AuthUser,
	tpl uuid.UUID, r store.SessionOrderClaimsRow, inbox uuid.UUID, now time.Time,
) (RemindOutcome, error) {
	skip := func(why string) (RemindOutcome, error) {
		return RemindOutcome{OrderID: r.OrderID, Outcome: RemindSkipped, Reason: why}, nil
	}
	if r.Status != order.StatusDraft {
		return skip(NotDraftText)
	}
	q := store.New(tx)
	last, err := q.LastRemindedAt(ctx, r.MessageID) // đọc lại sau khi khóa hội thoại
	if err != nil {
		return RemindOutcome{}, fmt.Errorf("đọc lần nhắc của đơn: %w", err)
	}
	if why := remindedRecently(last, now); why != "" {
		return skip(why)
	}
	plan, err := send.PlanRemind(ctx, tx, env.Send.Registry, r.MessageID, inbox, now)
	if err != nil {
		return RemindOutcome{}, err //nolint:wrapcheck // send đã bọc việc đang làm
	}
	if !plan.Allowed() {
		return skip(plan.Reason())
	}
	text, err := env.Render(ctx, tx, tpl, r.ConversationID, r.OrderID, actor)
	if err != nil {
		return RemindOutcome{}, err
	}
	_, err = q.MarkReminded(ctx, store.MarkRemindedParams{
		Now: pgtype.Timestamptz{Time: now, Valid: true}, MessageID: r.MessageID,
		Since: pgtype.Timestamptz{Time: now.Add(-RemindEvery), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		last, err := q.LastRemindedAt(ctx, r.MessageID)
		if err != nil {
			return RemindOutcome{}, fmt.Errorf("đọc lần nhắc của đơn: %w", err)
		}
		return skip(remindedRecently(last, now))
	}
	if err != nil {
		return RemindOutcome{}, fmt.Errorf("ghi lần nhắc của đơn: %w", err)
	}
	a, err := plan.Deliver(ctx, tx, env.Send, u, text, now)
	if err != nil {
		return RemindOutcome{}, err //nolint:wrapcheck // send đã bọc việc đang làm
	}
	res := RemindOutcome{OrderID: r.OrderID, Outcome: RemindQueued}
	if a.Message != nil {
		id := a.Message.ID
		res.MessageID = &id
	}
	return res, nil
}

// uniq trả các mã của ids đã sắp tăng dần, bỏ trùng (bản sao không nil, không đổi ids: mảng rỗng của SQL là "mọi
// đơn", NULL thì không khớp gì).
func uniq(ids []uuid.UUID) []uuid.UUID {
	out := append(make([]uuid.UUID, 0, len(ids)), ids...)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return slices.Compact(out)
}
