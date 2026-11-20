package cash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash/internal/lists"
	"elimaz.com.vn/crm/internal/modules/cash/internal/statement"
	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Task M3-32: tự khớp dòng bảng kê sau khi đọc tệp, chốt (version), ghi tiền theo lô, "Lệch đối soát", "Quá hạn chưa
// có tiền". Khớp và ghi tiền là việc của consumer StatementsConsumerName (sự kiện nội bộ của bảng kê, thứ tự nghiêm
// theo bảng kê); chốt là đường HTTP (WriteTx) chỉ đổi trạng thái và phát sự kiện.

// Sự kiện của bảng kê (aggregate cod_statement, Seq = version của bảng kê) và cod.settled.
const (
	// TopicStatementParsed: bảng kê vừa đọc xong tệp → khớp dòng.
	TopicStatementParsed = "cod_statement.parsed"
	// TopicStatementConfirmed: bảng kê vừa chốt → lô ghi tiền đầu.
	TopicStatementConfirmed = "cod_statement.confirmed"
	// TopicStatementPosting: lô ghi tiền kế tiếp (payload afterLineNo).
	TopicStatementPosting = "cod_statement.posting"
	// TopicCODSettled: carrier_holding của vận đơn về 0 nhờ một dòng bảng kê (aggregate cod_statement_line, Seq 1).
	TopicCODSettled = "cod.settled"
	// StatementsConsumerName là tên consumer khớp và ghi tiền bảng kê (việc event.cash_statements); đổi tên làm mất tiến độ.
	StatementsConsumerName = "cash_statements"
	// SignalStatementChanged là báo hiệu realtime khi bảng kê đổi trạng thái hay vừa ghi tiền xong (cash.cod.read).
	SignalStatementChanged = "cod_statement.changed"
)

const (
	aggregateStatement = "cod_statement"
	aggregateLine      = "cod_statement_line"
	permCODRead        = "cash.cod.read" // identity.Permission
)

// ErrStatementChanged báo bảng kê đã đổi (version khác, hay không còn ở trạng thái đã khớp) từ lúc người chốt đọc (TI07).
var ErrStatementChanged = statement.ErrChanged

// statementEvent là payload sự kiện bảng kê: chỉ mã và số dòng.
type statementEvent struct {
	StatementID uuid.UUID `json:"statementId"`
	AfterLineNo int32     `json:"afterLineNo,omitempty"`
}

// CODSettled là payload cod.settled: chỉ mã.
type CODSettled struct {
	ShipmentID      uuid.UUID `json:"shipmentId"`
	StatementID     uuid.UUID `json:"statementId"`
	StatementLineID uuid.UUID `json:"statementLineId"`
}

// emitStatement phát sự kiện topic của bảng kê id (Seq = version mới) trong tx.
func (s *Service) emitStatement(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, version int64, topic string, after int32) error {
	return s.emit(ctx, tx, events.Event{
		TenantID: tenantID, AggregateType: aggregateStatement, AggregateID: id.String(), Seq: version, Topic: topic,
		Payload: statementEvent{StatementID: id, AfterLineNo: after},
	})
}

// emit phát e bằng sổ sự kiện của module trong tx; Events chưa nối → errEventsNotWired.
func (s *Service) emit(ctx context.Context, tx pgx.Tx, e events.Event) error {
	if s.d.Events == nil {
		return errEventsNotWired
	}
	if _, _, err := s.d.Events.Emit(ctx, tx, s.d.Queue, e); err != nil {
		return fmt.Errorf("phát %s: %w", e.Topic, err)
	}
	return nil
}

// errEventsNotWired: Deps.Events chưa nối (lỗi dựng).
var errEventsNotWired = errors.New("cash: chưa nối sổ sự kiện")

// statementParsed (statement.ParsedFunc): bảng kê vừa đọc xong → phát cod_statement.parsed (consumer khớp dòng).
func (s *Service) statementParsed(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, version int64) error {
	return s.emitStatement(ctx, tx, tenantID, id, version, TopicStatementParsed, 0)
}

// publishStatement báo hiệu SignalStatementChanged cho bảng kê id (Bus nil → bỏ qua).
func (s *Service) publishStatement(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	if s.d.Bus == nil {
		return nil
	}
	if err := s.d.Bus.Publish(ctx, tx, realtime.Signal{
		TenantID: tenantID, Topic: SignalStatementChanged, Requires: permCODRead,
		Entity: realtime.Entity{Type: aggregateStatement, ID: id.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu %s: %w", SignalStatementChanged, err)
	}
	return nil
}

// statementsConsumer là consumer StatementsConsumerName: cod_statement.parsed → khớp mọi dòng (statement.Match, xem
// trước) rồi matched; cod_statement.confirmed, cod_statement.posting → một lô ghi tiền (statement.PostBatch: khóa
// tư vấn sổ COD theo vận đơn như consumer cash, ghi bằng PostWithID khóa remit:<dòng>, phát cod.settled cho vận đơn
// có carrier_holding về 0), còn lô sau thì phát cod_statement.posting, hết thì báo hiệu. Sự kiện giao lại cho cùng
// kết quả (trạng thái có điều kiện, dòng đã ghi bị bỏ qua). Payload hỏng → queue.ErrPermanent.
type statementsConsumer struct{ s *Service }

func (statementsConsumer) Name() string { return StatementsConsumerName }

func (statementsConsumer) Topics() []string {
	return []string{TopicStatementParsed, TopicStatementConfirmed, TopicStatementPosting}
}

func (c statementsConsumer) Handle(ctx context.Context, e events.StoredEvent, tx pgx.Tx) error {
	var p statementEvent
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.StatementID == uuid.Nil {
		return fmt.Errorf("sự kiện %s: payload không hợp lệ: %w", e.ID, queue.ErrPermanent)
	}
	if e.Topic == TopicStatementParsed {
		_, ok, err := statement.Match(ctx, tx, p.StatementID)
		if err != nil || !ok {
			return err //nolint:wrapcheck // statement đã bọc
		}
		return c.s.publishStatement(ctx, tx, e.TenantID, p.StatementID)
	}
	b, err := statement.PostBatch(ctx, tx, p.StatementID, p.AfterLineNo, lockShipmentCOD, postStatementLine)
	if err != nil || !b.Done {
		return err //nolint:wrapcheck // statement đã bọc
	}
	for _, st := range b.Settled {
		if err := c.s.emit(ctx, tx, events.Event{
			TenantID: e.TenantID, AggregateType: aggregateLine, AggregateID: st.LineID.String(), Seq: 1,
			Topic: TopicCODSettled, Payload: CODSettled{ShipmentID: st.ShipmentID, StatementID: p.StatementID, StatementLineID: st.LineID},
		}); err != nil {
			return err
		}
	}
	if b.More {
		return c.s.emitStatement(ctx, tx, e.TenantID, p.StatementID, b.Version, TopicStatementPosting, b.Last)
	}
	return c.s.publishStatement(ctx, tx, e.TenantID, p.StatementID)
}

// lockShipmentCOD (statement.LockFunc): khóa tư vấn sổ COD của vận đơn, cùng khóa với Consumer (ADR-46).
func lockShipmentCOD(ctx context.Context, tx pgx.Tx, shipmentID uuid.UUID) error {
	return store.New(tx).LockShipmentCOD(ctx, shipmentID) //nolint:wrapcheck // statement bọc
}

// postStatementLine (statement.PostFunc): ghi bút toán của một dòng bảng kê bằng PostWithID (tác nhân người chốt).
func postStatementLine(ctx context.Context, tx pgx.Tx, p statement.Posting) (uuid.UUID, error) {
	line := p.LineID
	t := Transfer{
		ShipmentID: p.ShipmentID, Kind: p.Kind, IdempotencyKey: p.Key, Actor: Actor{Type: ActorStaff, ID: p.ActorID},
		StatementLineID: &line, Entries: make([]Entry, len(p.Entries)),
	}
	for i, en := range p.Entries {
		t.Entries[i] = Entry{Account: en.Account, Amount: en.Amount}
	}
	id, _, err := PostWithID(ctx, tx, t)
	return id, err
}

// ConfirmedStatement là bảng kê vừa chốt cùng cảnh báo không chặn (tổng thực nhận lệch tổng khai trên tệp).
type ConfirmedStatement struct {
	Statement Statement
	Warnings  []string
}

// ConfirmStatement chốt bảng kê id của tenantID khi version còn khớp và đang matched (TI07), người chốt by, trong tx
// (WriteTx của tenant): không có → ErrStatementNotFound; version khác hay không còn matched → ErrStatementChanged.
// Phát cod_statement.confirmed (lô ghi tiền chạy sau commit) và báo hiệu. Tổng thực nhận của các dòng sẽ ghi tiền khác
// tổng khai trên tệp → cảnh báo trong kết quả (không chặn). Chạy lại được trong RetryTx.
func (s *Service) ConfirmStatement(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, version int64, by uuid.UUID) (ConfirmedStatement, error) {
	c, err := statement.Confirm(ctx, tx, id, version, by)
	if err != nil {
		return ConfirmedStatement{}, err //nolint:wrapcheck // statement đã bọc; sentinel là bí danh của gói này
	}
	if err := s.emitStatement(ctx, tx, tenantID, id, c.Statement.Version, TopicStatementConfirmed, 0); err != nil {
		return ConfirmedStatement{}, err
	}
	if err := s.publishStatement(ctx, tx, tenantID, id); err != nil {
		return ConfirmedStatement{}, err
	}
	out := ConfirmedStatement{Statement: c.Statement, Warnings: []string{}}
	if d := c.Statement.DeclaredTotal; d != nil && *d != c.PostableNet {
		out.Warnings = append(out.Warnings, "Tổng thực nhận của các dòng ghi tiền ("+strconv.FormatInt(c.PostableNet, 10)+
			") khác tổng khai trên tệp ("+strconv.FormatInt(*d, 10)+")")
	}
	return out, nil
}

// Discrepancy là một mục "Lệch đối soát" (lists.Kind*).
type Discrepancy = lists.Discrepancy

// Overdue là một vận đơn "Quá hạn chưa có tiền".
type Overdue = lists.Overdue

// Loại mục của "Lệch đối soát".
const (
	DiscrepancyStatementLine        = lists.KindStatementLine
	DiscrepancyOpenRemainder        = lists.KindOpenRemainder
	DiscrepancyRemittedThenReturned = lists.KindRemittedThenReturned
)

// ListDiscrepancies đọc "Lệch đối soát" (mới nhất trước, limit mục sau mã mục before) trong tx của tenant; next là mã
// mục cuối khi còn trang sau.
func (*Service) ListDiscrepancies(ctx context.Context, tx pgx.Tx, before *uuid.UUID, limit int) (items []Discrepancy, next *uuid.UUID, err error) {
	limit = max(limit, 1)
	items, err = lists.Discrepancies(ctx, tx, before, limit+1)
	if err != nil {
		return nil, nil, err //nolint:wrapcheck // lists đã bọc
	}
	if len(items) > limit {
		items = items[:limit]
		next = new(items[limit-1].ID)
	}
	return items, next, nil
}

// ListOverdue đọc "Quá hạn chưa có tiền" trong tx của tenant: vận đơn giao xong trước Now − CODOverdueDays (cài đặt
// vận chuyển) còn carrier_holding > 0, theo mã vận đơn sau after; next là mã vận đơn cuối khi còn trang sau.
func (s *Service) ListOverdue(ctx context.Context, tx pgx.Tx, after *uuid.UUID, limit int) (items []Overdue, next *uuid.UUID, err error) {
	set, _, err := fulfillment.LoadSettings(ctx, tx)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc số ngày quá hạn COD: %w", err)
	}
	limit = max(limit, 1)
	items, err = lists.OverdueList(ctx, tx, s.d.Now(), set.CODOverdueDays, after, limit+1)
	if err != nil {
		return nil, nil, err //nolint:wrapcheck // lists đã bọc
	}
	if len(items) > limit {
		items = items[:limit]
		next = new(items[limit-1].ShipmentID)
	}
	return items, next, nil
}
