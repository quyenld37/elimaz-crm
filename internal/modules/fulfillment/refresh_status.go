package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// RefreshCommand là lệnh fulfillment.refresh_status (commands.Handler, Looker, DoneHook): đọc trạng thái thật của vận
// đơn qua API có xác thực của hãng (B10) rồi áp lên vận đơn, đơn, tồn. Đọc là an toàn nên không rõ thì coi như chưa
// gửi (tự thử lại).
type RefreshCommand struct{ commandBase }

// NewRefreshCommand tạo lệnh tra trạng thái vận đơn.
func NewRefreshCommand(o CommandOptions) *RefreshCommand { return &RefreshCommand{newBase(o)} }

// Kind trả RefreshCommandKind().
func (*RefreshCommand) Kind() queue.Kind { return RefreshCommandKind() }

// AutoRetryNotSent trả true: đọc lại an toàn.
func (*RefreshCommand) AutoRetryNotSent() bool { return true }

type refreshResult struct {
	TrackingCode string    `json:"trackingCode,omitempty"`
	RawStatus    string    `json:"rawStatus"`
	Status       Status    `json:"status"`
	EventKey     string    `json:"eventKey"`
	OccurredAt   time.Time `json:"occurredAt"`
}

// Execute đọc trạng thái thật: vận đơn chưa có mã hãng (lệnh tạo xong do tra lại) → LookupByCode; ngược lại GetStatus. Trạng thái thô lạ →
// rejected (dead, người có quyền xem); hãng từ chối → rejected; mọi lỗi khác, quá giờ → not_sent (thử lại).
func (r *RefreshCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	s, ad, out, ok := r.loadOutcome(ctx, j)
	if !ok {
		return out, nil
	}
	res, reason := r.read(ctx, ad, s)
	switch reason {
	case "":
	case reasonRejected:
		return commands.Outcome{Status: commands.StatusRejected, Reason: reason}, nil
	default:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reason}, nil
	}
	st, ok := ad.MapStatus(res.RawStatus)
	if !ok {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonUnknownStatus}, nil
	}
	res.Status = st
	b, err := json.Marshal(res)
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả tra vận đơn: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

// read gọi hãng; reason khác rỗng khi không đọc được (reasonRejected khi hãng từ chối).
func (r *RefreshCommand) read(ctx context.Context, ad CarrierAdapter, s Shipment) (refreshResult, string) {
	if s.TrackingCode == "" {
		lr, err := ad.LookupByCode(ctx, s.account(), s.Code)
		switch {
		case errors.Is(err, ErrRejected):
			return refreshResult{}, reasonRejected
		case err != nil:
			return refreshResult{}, reasonReadFailed
		case !lr.Found || lr.TrackingCode == "":
			return refreshResult{}, reasonNotFound
		}
		return refreshResult{
			TrackingCode: lr.TrackingCode, RawStatus: lr.RawStatus, EventKey: "lookup:" + lr.RawStatus,
			OccurredAt: time.Now().UTC(),
		}, ""
	}
	sr, err := ad.GetStatus(ctx, s.account(), s.TrackingCode)
	switch {
	case errors.Is(err, ErrRejected):
		return refreshResult{}, reasonRejected
	case err != nil:
		return refreshResult{}, reasonReadFailed
	}
	return refreshResult{RawStatus: sr.RawStatus, EventKey: sr.EventKey, OccurredAt: sr.OccurredAt.UTC()}, ""
}

// Lookup luôn not_sent: tra trạng thái chỉ đọc, chạy lại an toàn (lệnh in_doubt vì hết giữ chỗ về hàng chờ).
func (*RefreshCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// OnDone (trong transaction đổi lệnh sang done): khóa đơn (order.Lock, nhóm 7) trước, rồi vận đơn (nhóm 8); chèn
// shipment_events (trùng event_key → bỏ qua); điền mã hãng nếu còn trống; Advance: tiến thì cập nhật vận đơn, áp bước
// lên đơn và tồn (order.Writer.ApplyShipmentProgress; chỉ vận đơn hiện hành đổi được), phát shipment.status_changed
// (Seq = version mới). Không tiến (lùi, bằng): chỉ thêm dòng sự kiện.
func (r *RefreshCommand) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	if result == nil { // done do tra lại: Lookup không bao giờ trả done
		return nil
	}
	p, err := parsePayload(j)
	if err != nil {
		return err
	}
	var res refreshResult
	if err := json.Unmarshal(result, &res); err != nil || res.EventKey == "" || res.RawStatus == "" {
		return fmt.Errorf("lệnh %s: kết quả tra vận đơn không đọc được: %w", j.ID, queue.ErrPermanent)
	}
	q := store.New(tx)
	s, err := Get(ctx, tx, p.ShipmentID)
	if err != nil {
		return err
	}
	if err := order.Lock(ctx, tx, s.SellerID, s.OrderID); err != nil {
		return fmt.Errorf("khóa đơn của vận đơn: %w", err)
	}
	cur, err := q.LockShipment(ctx, s.ID)
	if err != nil {
		return fmt.Errorf("khóa vận đơn: %w", err)
	}
	if _, err := q.InsertShipmentEvent(ctx, store.InsertShipmentEventParams{
		ShipmentID: cur.ID, EventKey: res.EventKey, RawStatus: res.RawStatus, Status: string(res.Status),
		OccurredAt: res.OccurredAt, Raw: result,
	}); err != nil {
		return fmt.Errorf("ghi sự kiện vận đơn: %w", err)
	}
	next, advanced := Advance(Status(cur.Status), res.Status)
	fillTracking := !cur.TrackingCode.Valid && res.TrackingCode != ""
	if !advanced && !fillTracking {
		return nil
	}
	version, err := q.UpdateShipmentProgress(ctx, store.UpdateShipmentProgressParams{
		Status: string(next), TrackingCode: pgtype.Text{String: res.TrackingCode, Valid: res.TrackingCode != ""},
		ID: cur.ID,
	})
	if err != nil {
		return fmt.Errorf("cập nhật vận đơn: %w", err)
	}
	if !advanced {
		return nil
	}
	if step, ok := next.Step(); ok {
		w := order.Writer{Events: r.o.Events, Queue: r.o.Queue, Realtime: r.o.Realtime}
		if _, err := w.ApplyShipmentProgress(ctx, tx, s.SellerID, cur.OrderID, cur.ID, step); err != nil {
			return fmt.Errorf("áp bước vận đơn lên đơn: %w", err)
		}
	}
	return r.emitStatus(ctx, tx, *j.TenantID, cur.ID, next, cur.CodAmount, version)
}
