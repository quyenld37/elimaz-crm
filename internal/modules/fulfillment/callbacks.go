package fulfillment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/lists"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Danh sách "đơn giao thất bại cần gọi lại" (kế hoạch M3, Task M3-19; VC07). Việc mở do áp kết quả tra ghi khi giao
// thất bại (apply_effects.go: openCallback) và tự đóng khi hãng hoàn hay giao được (closeCallbacks); ở đây nhân viên đọc
// danh sách, ghi kết quả cuộc gọi, giao việc. Thứ tự khóa ADR-46 nhóm 7 → 8: đơn → vận đơn → callback_tasks →
// address_change_requests; callback_task_logs chỉ ghi thêm.

// Trạng thái và kết quả của việc gọi lại (CHECK của fulfillment.callback_tasks, migration 0300).
const (
	CallbackOpen       = "open"
	CallbackInProgress = "in_progress"
	CallbackDone       = "done"
	CallbackClosed     = "closed"

	OutcomeRedeliver           = "redeliver"
	OutcomeRedeliverNewAddress = "redeliver_new_address"
	OutcomeCustomerCancel      = "customer_cancel"
	OutcomeNoAnswer            = "no_answer"
	OutcomeReschedule          = "reschedule"

	maxCallbackNote = 1000 // maxLength của note (RecordCallbackOutcomeRequest)
	// redeliverManualNote đứng đầu nhật ký khi hãng không nhận yêu cầu giao lại qua API.
	redeliverManualNote = "Nhân viên báo hãng giao lại (hãng không nhận yêu cầu qua API)"
)

// Lỗi của việc gọi lại.
var (
	// ErrCallbackNotFound: không có việc gọi lại mang mã đó (hay của tenant khác).
	ErrCallbackNotFound = errors.New("fulfillment: không có việc gọi lại")
	// ErrCallbackClosed: việc đã xong (done) hay đã tự đóng (closed: hãng hoàn, đã giao) — không ghi kết quả, không giao
	// việc được nữa.
	ErrCallbackClosed = errors.New("fulfillment: việc gọi lại đã xong hoặc đã đóng")
	// ErrCallbackAssignee: người được giao không phải nhân viên đang hoạt động có quyền fulfillment.callbacks.work.
	ErrCallbackAssignee = errors.New("fulfillment: người được giao không làm được việc gọi lại")
)

// CallbackFilter là trang việc gọi lại cần đọc. Status rỗng: việc còn mở (open, in_progress); khác: đúng trạng thái
// đó (open, in_progress, done). DueBefore: chỉ việc có hạn trước mốc đó. AssigneeID: chỉ việc giao cho người đó.
// Cursor: Next của trang trước ("" trang đầu). Limit ≤ 0 → 50, tối đa 100.
type CallbackFilter struct {
	Status     string
	DueBefore  *time.Time
	AssigneeID *uuid.UUID
	Cursor     string
	Limit      int
}

// CallbackTask là một việc gọi lại cho màn hình: không SĐT, không địa chỉ, không tên khách (CustomerID: khách của đơn,
// để bên gọi đọc tên và số nhận/hoàn qua customer; fulfillment không gọi customer).
// Overdue: còn mở và đã quá hạn lúc đọc.
type CallbackTask struct {
	ID, ShipmentID, OrderID, CustomerID uuid.UUID
	OrderCode, CarrierKey, TrackingCode string
	FailedReason, Status, Outcome       string
	AttemptNo                           int32
	FailedAt, DueAt                     time.Time
	NextCallAt                          *time.Time
	AssigneeID                          *uuid.UUID
	Overdue                             bool
	Version                             int64
}

// CallbackPage là một trang việc gọi lại, con trỏ trang sau ("" khi hết) và số việc còn mở (trần 1.000).
type CallbackPage struct {
	Items     []CallbackTask
	Next      string
	OpenCount int
}

// ListCallbacks trả một trang việc gọi lại, hạn sớm nhất trước (việc còn mở quá hạn đứng đầu), kèm số việc còn mở; tx
// của bên gọi. Con trỏ hỏng → ErrInvalidPage; trạng thái lạ → ErrShipInvalid.
func (s *Service) ListCallbacks(ctx context.Context, tx pgx.Tx, f CallbackFilter) (CallbackPage, error) {
	statuses := []string{CallbackOpen, CallbackInProgress}
	switch f.Status {
	case "":
	case CallbackOpen, CallbackInProgress, CallbackDone:
		statuses = []string{f.Status}
	default:
		return CallbackPage{}, fmt.Errorf("%w: trạng thái việc gọi lại %q", ErrShipInvalid, f.Status)
	}
	after, err := parseListCursor(f.Cursor)
	if err != nil {
		return CallbackPage{}, err
	}
	limit := pageLimit(f.Limit)
	p := store.ListCallbackTasksParams{
		Statuses: statuses, AssigneeID: db.NullUUID(f.AssigneeID), Lim: int32(limit + 1), //nolint:gosec // limit ≤ listMax
	}
	if f.DueBefore != nil {
		p.DueBefore = pgtype.Timestamptz{Time: *f.DueBefore, Valid: true}
	}
	if after != nil {
		p.AfterAt, p.AfterID = pgtype.Timestamptz{Time: after.At, Valid: true}, pgtype.UUID{Bytes: after.ID, Valid: true}
	}
	q := store.New(tx)
	rows, err := q.ListCallbackTasks(ctx, p)
	if err != nil {
		return CallbackPage{}, fmt.Errorf("đọc việc gọi lại: %w", err)
	}
	n, err := q.CountOpenCallbacks(ctx)
	if err != nil {
		return CallbackPage{}, fmt.Errorf("đếm việc gọi lại còn mở: %w", err)
	}
	page := CallbackPage{OpenCount: int(n)}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		page.Next = lists.Cursor{At: last.DueAt.UTC(), ID: last.ID}.String()
	}
	now := s.d.Now().UTC()
	page.Items = make([]CallbackTask, 0, len(rows))
	for _, r := range rows {
		page.Items = append(page.Items, callbackView(store.CallbackTaskViewRow(r), now))
	}
	return page, nil
}

// callbackView đổi một dòng việc gọi lại sang CallbackTask (quá hạn tính theo now).
func callbackView(r store.CallbackTaskViewRow, now time.Time) CallbackTask {
	t := CallbackTask{
		ID: r.ID, ShipmentID: r.ShipmentID, OrderID: r.OrderID, CustomerID: r.CustomerID, OrderCode: r.OrderCode,
		CarrierKey: r.CarrierKey, TrackingCode: r.TrackingCode.String,
		FailedReason: r.FailedReason, Status: r.Status, Outcome: r.Outcome.String, AttemptNo: r.AttemptNo,
		FailedAt: r.FailedAt.UTC(), DueAt: r.DueAt.UTC(), NextCallAt: tsPtr(r.NextCallAt), Version: r.Version,
		Overdue: callbackOpen(r.Status) && r.DueAt.Before(now),
	}
	if r.AssigneeID.Valid {
		id := uuid.UUID(r.AssigneeID.Bytes)
		t.AssigneeID = &id
	}
	return t
}

// callbackOpen: việc còn mở (nhận kết quả, giao việc được).
func callbackOpen(status string) bool { return status == CallbackOpen || status == CallbackInProgress }

// CallbackOutcomeInput là kết quả một cuộc gọi: Outcome (no_answer, reschedule, redeliver, redeliver_new_address,
// customer_cancel), ghi chú (≤ 1.000 ký tự, che SĐT trước khi lưu), hẹn gọi lại (bắt buộc với reschedule), địa chỉ mới
// (bắt buộc với redeliver_new_address), Version của việc (phải khớp). Authorize: kiểm kho của vận đơn khi đổi địa chỉ
// (nil: không kiểm).
type CallbackOutcomeInput struct {
	Outcome    string
	Note       string
	NextCallAt *time.Time
	NewAddress *order.ShipAddress
	Version    int64
	Authorize  Authorize
}

// CallbackOutcomeResult là việc sau khi ghi; AddressChange khác nil khi đã tạo yêu cầu đổi địa chỉ (bên gọi ghi
// audit); Redeliver: đã ghi lệnh giao lại ở hãng (false với redeliver: nhân viên tự báo hãng).
type CallbackOutcomeResult struct {
	Task          CallbackTask
	Before        string
	AddressChange *AddressChange
	Redeliver     bool
}

// RecordCallbackOutcome ghi kết quả cuộc gọi cho việc id trong tx của bên gọi: khóa đơn, vận đơn rồi việc; việc phải
// còn mở, Version khớp. no_answer, reschedule → in_progress (hẹn gọi mới; no_answer không có hẹn thì giữ hẹn cũ);
// redeliver → done, kèm lệnh giao lại ở hãng (RedeliverKind) khi vận đơn delivery_failed của tài khoản api có mã hãng
// và adapter khai Redeliver, không thì nhật ký ghi nhân viên tự báo hãng; redeliver_new_address → done và
// RequestAddressChange trong cùng tx; customer_cancel → done (ghi chú cho kho nằm ở nhật ký; khi hãng hoàn, đơn đi
// luồng hoàn). Mỗi lần ghi một dòng callback_task_logs, báo hiệu callback_task.changed.
//
// Lỗi (không đổi gì): kết quả lạ, thiếu hẹn hay địa chỉ, ghi chú quá dài → ErrShipInvalid; không có việc →
// ErrCallbackNotFound; việc đã xong/đóng → ErrCallbackClosed; lệch Version → ErrVersionConflict; lỗi của
// RequestAddressChange trả nguyên.
func (s *Service) RecordCallbackOutcome(ctx context.Context, tx pgx.Tx, id uuid.UUID, in CallbackOutcomeInput,
	a order.Actor,
) (CallbackOutcomeResult, error) {
	if s.d.Queue == nil {
		return CallbackOutcomeResult{}, errNotWired
	}
	status, err := checkCallbackOutcome(in)
	if err != nil {
		return CallbackOutcomeResult{}, err
	}
	note, err := callbackNote(in.Note)
	if err != nil {
		return CallbackOutcomeResult{}, err
	}
	q := store.New(tx)
	l, row, err := lockCallback(ctx, tx, q, id)
	if err != nil {
		return CallbackOutcomeResult{}, err
	}
	if !callbackOpen(row.Status) {
		return CallbackOutcomeResult{}, ErrCallbackClosed
	}
	if row.Version != in.Version {
		return CallbackOutcomeResult{}, ErrVersionConflict
	}
	res := CallbackOutcomeResult{Before: row.Status}
	byAPI := in.Outcome == OutcomeRedeliver && s.redeliverByAPI(l)
	if in.Outcome == OutcomeRedeliver && !byAPI {
		note = joinNote(redeliverManualNote, note)
	}
	var next pgtype.Timestamptz
	if status == CallbackInProgress && in.NextCallAt != nil {
		next = pgtype.Timestamptz{Time: in.NextCallAt.UTC(), Valid: true}
	}
	if err := q.SetCallbackOutcome(ctx, store.SetCallbackOutcomeParams{
		Status: status, Outcome: pgtype.Text{String: in.Outcome, Valid: true}, NextCallAt: next, ID: id,
	}); err != nil {
		return CallbackOutcomeResult{}, fmt.Errorf("ghi kết quả việc gọi lại: %w", err)
	}
	logID, err := q.InsertCallbackLog(ctx, store.InsertCallbackLogParams{
		TaskID: id, Outcome: in.Outcome, Note: note, NextCallAt: next, ActorID: a.UserID,
	})
	if err != nil {
		return CallbackOutcomeResult{}, fmt.Errorf("ghi nhật ký gọi lại: %w", err)
	}
	if err := s.callbackFollowUp(ctx, tx, &res, l.ID, logID, byAPI, in, a); err != nil {
		return CallbackOutcomeResult{}, err
	}
	if res.Task, err = s.callbackTask(ctx, q, id); err != nil {
		return CallbackOutcomeResult{}, err
	}
	res.Redeliver = byAPI
	return res, s.publishCallback(ctx, tx, a.TenantID, id)
}

// callbackFollowUp làm phần sau của kết quả: lệnh giao lại ở hãng (byAPI) hay yêu cầu đổi địa chỉ.
func (s *Service) callbackFollowUp(ctx context.Context, tx pgx.Tx, res *CallbackOutcomeResult, shipmentID, logID uuid.UUID,
	byAPI bool, in CallbackOutcomeInput, a order.Actor,
) error {
	switch {
	case byAPI:
		if _, _, err := s.d.Queue.Enqueue(ctx, tx, RedeliverKind(), queue.NewJob{
			TenantID: &a.TenantID, Payload: redeliverPayload{LogID: logID}, DedupeKey: redeliverKeyPrefix + logID.String(),
		}); err != nil {
			return fmt.Errorf("ghi lệnh giao lại: %w", err)
		}
	case in.Outcome == OutcomeRedeliverNewAddress:
		c, err := s.RequestAddressChange(ctx, tx, shipmentID, AddressChangeInput{
			Address: *in.NewAddress, Note: in.Note, Authorize: in.Authorize,
		}, a)
		if err != nil {
			return err
		}
		res.AddressChange = &c
	}
	return nil
}

// checkCallbackOutcome kiểm kết quả in và trả trạng thái mới của việc.
func checkCallbackOutcome(in CallbackOutcomeInput) (string, error) {
	switch in.Outcome {
	case OutcomeNoAnswer:
		return CallbackInProgress, nil
	case OutcomeReschedule:
		if in.NextCallAt == nil {
			return "", fmt.Errorf("%w: hẹn gọi lại thiếu giờ hẹn", ErrShipInvalid)
		}
		return CallbackInProgress, nil
	case OutcomeRedeliverNewAddress:
		if in.NewAddress == nil {
			return "", fmt.Errorf("%w: giao lại địa chỉ mới thiếu địa chỉ", ErrShipInvalid)
		}
		return CallbackDone, nil
	case OutcomeRedeliver, OutcomeCustomerCancel:
		return CallbackDone, nil
	}
	return "", fmt.Errorf("%w: kết quả gọi lại %q", ErrShipInvalid, in.Outcome)
}

// callbackNote che SĐT, địa chỉ có số trong ghi chú s (logging.ScrubText); quá 1.000 ký tự → ErrShipInvalid.
func callbackNote(s string) (string, error) {
	if len([]rune(s)) > maxCallbackNote {
		return "", fmt.Errorf("%w: ghi chú gọi lại quá dài", ErrShipInvalid)
	}
	clean := []rune(logging.ScrubText(s))
	return string(clean[:min(len(clean), maxCallbackNote)]), nil
}

func joinNote(head, note string) string {
	if note == "" {
		return head
	}
	return head + ": " + note
}

// redeliverByAPI: gọi hãng giao lại qua API được cho vận đơn l (đang delivery_failed, tài khoản api, có mã hãng,
// adapter khai Redeliver).
func (s *Service) redeliverByAPI(l lockedCancel) bool {
	ad, ok := s.adapter(l.CarrierKey)
	return ok && l.Mode == ModeAPI && ad.Capabilities().Redeliver && l.TrackingCode.Valid &&
		l.Status == string(StatusDeliveryFailed)
}

// lockCallback khóa đơn, vận đơn của việc id rồi việc (ADR-46 nhóm 7 → 8). Không có → ErrCallbackNotFound.
func lockCallback(ctx context.Context, tx pgx.Tx, q *store.Queries, id uuid.UUID,
) (lockedCancel, store.LockCallbackTaskRow, error) {
	shipID, err := q.CallbackTaskShipment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedCancel{}, store.LockCallbackTaskRow{}, ErrCallbackNotFound
	}
	if err != nil {
		return lockedCancel{}, store.LockCallbackTaskRow{}, fmt.Errorf("đọc vận đơn của việc gọi lại: %w", err)
	}
	l, err := lockForCancel(ctx, tx, q, shipID, nil)
	if errors.Is(err, ErrNotFound) {
		return lockedCancel{}, store.LockCallbackTaskRow{}, ErrCallbackNotFound
	}
	if err != nil {
		return lockedCancel{}, store.LockCallbackTaskRow{}, err
	}
	row, err := q.LockCallbackTask(ctx, id)
	if err != nil {
		return lockedCancel{}, store.LockCallbackTaskRow{}, fmt.Errorf("khóa việc gọi lại: %w", err)
	}
	return l, row, nil
}

// callbackTask đọc lại việc id cho câu trả lời.
func (s *Service) callbackTask(ctx context.Context, q *store.Queries, id uuid.UUID) (CallbackTask, error) {
	r, err := q.CallbackTaskView(ctx, id)
	if err != nil {
		return CallbackTask{}, fmt.Errorf("đọc lại việc gọi lại: %w", err)
	}
	return callbackView(r, s.d.Now().UTC()), nil
}

// CallbackAssignment là kết quả giao việc: việc sau khi giao và người được giao trước đó (nil: chưa giao ai).
type CallbackAssignment struct {
	Task           CallbackTask
	BeforeAssignee *uuid.UUID
}

// AssignCallback giao việc id cho assignee trong tx của bên gọi (bên gọi đã kiểm assignee có quyền làm việc gọi lại,
// không thì trả ErrCallbackAssignee): khóa đơn, vận đơn rồi việc; việc phải còn mở; version khác nil thì phải khớp.
// Báo hiệu callback_task.changed. Lỗi: ErrCallbackNotFound, ErrCallbackClosed, ErrVersionConflict.
func (s *Service) AssignCallback(ctx context.Context, tx pgx.Tx, id, assignee uuid.UUID, version *int64, a Actor,
) (CallbackAssignment, error) {
	q := store.New(tx)
	_, row, err := lockCallback(ctx, tx, q, id)
	if err != nil {
		return CallbackAssignment{}, err
	}
	if !callbackOpen(row.Status) {
		return CallbackAssignment{}, ErrCallbackClosed
	}
	if version != nil && *version != row.Version {
		return CallbackAssignment{}, ErrVersionConflict
	}
	var out CallbackAssignment
	if row.AssigneeID.Valid {
		before := uuid.UUID(row.AssigneeID.Bytes)
		out.BeforeAssignee = &before
	}
	if err := q.AssignCallbackTask(ctx, store.AssignCallbackTaskParams{
		AssigneeID: pgtype.UUID{Bytes: assignee, Valid: true}, ID: id,
	}); err != nil {
		return CallbackAssignment{}, fmt.Errorf("giao việc gọi lại: %w", err)
	}
	if out.Task, err = s.callbackTask(ctx, q, id); err != nil {
		return CallbackAssignment{}, err
	}
	return out, s.publishCallback(ctx, tx, a.TenantID, id)
}
