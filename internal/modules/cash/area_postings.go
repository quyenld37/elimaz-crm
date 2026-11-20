package cash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng postings (kế hoạch M3, Task M3-29): ghi sổ COD theo sự kiện vận đơn. Luật ở internal/postings, consumer cash của
// M1 (consumer.go) gọi luật; vùng này thêm lệnh cảnh báo "tiền đã về rồi hãng sửa thành hoàn" và đường đọc tiền COD
// của một vận đơn (getShipmentCod). Consumer giữ ở Module.Consumers (wire.go) như M1, không thêm consumer thứ hai.

// RemittedReturnedAlertKind là tên loại lệnh cảnh báo thường "cod-remitted-returned:<mã vận đơn>" (Q-1).
const RemittedReturnedAlertKind = "cash.cod_remitted_returned_alert"

const (
	alertTimeout       = 10 * time.Second
	reasonBadPayload   = "bad_payload"
	reasonAlerter      = "alerter_missing"
	reasonAlertFailed  = "alert_failed"
	alertSeverityUsual = observability.SeverityNormal
)

// ErrShipmentNotFound báo vận đơn không có hay của tenant khác (không cho biết là có).
var ErrShipmentNotFound = errors.New("cash: không có vận đơn")

func (*Module) postingsHandlers() []commands.Handler       { return nil }
func (*Module) postingsInbound() []webhooks.InboundHandler { return nil }
func (*Module) postingsConsumers() []events.Consumer       { return nil }
func (*Module) postingsSchedules() []schedules.Schedule    { return nil }
func (*Module) postingsCleanups() []schedules.CleanupFunc  { return nil }

func remittedReturnedAlertKind() queue.Kind {
	return commands.CommandKind(RemittedReturnedAlertKind, 0, alertTimeout)
}

// remittedReturnedPayload chỉ mang mã: vận đơn và mã vận đơn nội bộ (không mã đơn, không dữ liệu khách).
type remittedReturnedPayload struct {
	ShipmentID uuid.UUID `json:"shipmentId"`
	Code       string    `json:"code"`
}

// decodeRemittedReturned đọc payload của lệnh; thiếu vận đơn hay mã → ok = false.
func decodeRemittedReturned(raw []byte) (remittedReturnedPayload, bool) {
	var p remittedReturnedPayload
	err := json.Unmarshal(raw, &p)
	return p, err == nil && p.ShipmentID != uuid.Nil && p.Code != ""
}

// remittedReturnedAlert là lệnh RemittedReturnedAlertKind: Notify(key "cod-remitted-returned:<mã vận đơn>", normal).
// n nil → rejected alerter_missing; Notify lỗi → not_sent và tự gửi lại (Alerter chống lặp theo key); Telegram 429 →
// chờ đúng thời gian Telegram yêu cầu. Lỗi không log ở đây: kết quả lệnh (Reason) đã ghi ở platform.commands.
type remittedReturnedAlert struct {
	n observability.Alerter
}

func (remittedReturnedAlert) Kind() queue.Kind       { return remittedReturnedAlertKind() }
func (remittedReturnedAlert) AutoRetryNotSent() bool { return true }

func (a remittedReturnedAlert) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	p, ok := decodeRemittedReturned(j.Payload)
	switch {
	case !ok:
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	case a.n == nil:
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonAlerter}, nil
	}
	text := fmt.Sprintf("Vận đơn %s: hãng sửa Thành công sang Đang chuyển hoàn sau khi tiền COD đã về shop. "+
		"Sổ COD không tự đổi; kế toán xem ở Lệch đối soát và điều chỉnh có xem trước.", p.Code)
	if err := a.n.Notify(ctx, "cod-remitted-returned:"+p.Code, alertSeverityUsual, text); err != nil {
		out := commands.Outcome{Status: commands.StatusNotSent, Reason: reasonAlertFailed}
		if re, ok := errors.AsType[*observability.TelegramRetryError](err); ok {
			out.RetryAfter = re.After
		}
		return out, nil
	}
	return commands.Outcome{Status: commands.StatusDone}, nil
}

// NewRemittedReturnedAlertCommand trả lệnh RemittedReturnedAlertKind gửi qua n. cmd/crm đăng ký cùng các lệnh cảnh
// báo khác (alertSchedules, dùng chung Alerter của worker), không qua Module.CommandHandlers.
func NewRemittedReturnedAlertCommand(n observability.Alerter) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return remittedReturnedAlert{n: n}
}

// ShipmentCOD là tiền COD của một vận đơn: số dư từng ngăn và các bút toán theo thứ tự ghi.
type ShipmentCOD struct {
	Balances  Balances
	Transfers []CODTransfer
}

// CODTransfer là một bút toán cho màn hình: vế, lý do (adjustment, reversal), tác nhân, dòng bảng kê, bút toán bị đảo.
type CODTransfer struct {
	ID                 uuid.UUID
	Kind               string
	OccurredAt         time.Time
	Entries            []Entry
	Reason             string
	Actor              Actor
	StatementLineID    *uuid.UUID
	ReversesTransferID *uuid.UUID
}

// ShipmentCOD đọc tiền COD của vận đơn shipmentID trong tx của bên gọi (tenancy.WithTenant). Vận đơn không có hay của
// tenant khác → ErrShipmentNotFound; chưa có dòng sổ → mọi ngăn 0, không bút toán.
func (*Service) ShipmentCOD(ctx context.Context, tx pgx.Tx, shipmentID uuid.UUID) (ShipmentCOD, error) {
	q := store.New(tx)
	ok, err := q.ShipmentExists(ctx, shipmentID)
	if err != nil {
		return ShipmentCOD{}, fmt.Errorf("đọc vận đơn: %w", err)
	}
	if !ok {
		return ShipmentCOD{}, ErrShipmentNotFound
	}
	b, err := BalancesFor(ctx, tx, shipmentID)
	if err != nil {
		return ShipmentCOD{}, err
	}
	rows, err := q.ShipmentTransferList(ctx, shipmentID)
	if err != nil {
		return ShipmentCOD{}, fmt.Errorf("đọc bút toán của vận đơn: %w", err)
	}
	lines, err := q.ShipmentEntries(ctx, shipmentID)
	if err != nil {
		return ShipmentCOD{}, fmt.Errorf("đọc dòng sổ của vận đơn: %w", err)
	}
	byTransfer := make(map[uuid.UUID][]Entry, len(rows))
	for _, l := range lines {
		byTransfer[l.TransferID] = append(byTransfer[l.TransferID], Entry{Account: l.Account, Amount: l.Amount})
	}
	out := ShipmentCOD{Balances: b, Transfers: make([]CODTransfer, len(rows))}
	for i, r := range rows {
		t := CODTransfer{
			ID: r.ID, Kind: r.Kind, OccurredAt: r.OccurredAt, Entries: byTransfer[r.ID], Reason: r.Reason,
			Actor: Actor{Type: r.ActorType},
		}
		if r.ActorID.Valid {
			t.Actor.ID = r.ActorID.Bytes
		}
		if r.StatementLineID.Valid {
			id := uuid.UUID(r.StatementLineID.Bytes)
			t.StatementLineID = &id
		}
		if r.ReversesTransferID.Valid {
			id := uuid.UUID(r.ReversesTransferID.Bytes)
			t.ReversesTransferID = &id
		}
		out.Transfers[i] = t
	}
	return out, nil
}
