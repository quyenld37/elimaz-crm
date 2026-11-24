package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Lệnh yêu cầu hãng giao lại sau khi gọi khách (kế hoạch M3, Task M3-19). Kết quả hãng không đổi bảng nào của CRM:
// vận đơn đi tiếp theo kết quả tra trạng thái (out_for_delivery → shipping) như mọi lần.

const (
	redeliverCommandName = "command.shipment.redeliver"
	// redeliverTimeout là hạn của một lần gọi hãng giao lại.
	redeliverTimeout = 20 * time.Second
	// redeliverKeyPrefix + mã dòng nhật ký là khóa chống trùng của lệnh.
	redeliverKeyPrefix = "redeliver:"

	reasonNotFailed        = "shipment_not_delivery_failed"
	reasonRedeliverOutcome = "redeliver_outcome_unknown"
	reasonCarrierRefused   = "carrier_refused_redeliver"
)

// RedeliverKind là loại lệnh command.shipment.redeliver: không hết hạn khi chờ, mỗi lần gọi hãng tối đa 20 s.
func RedeliverKind() queue.Kind {
	return commands.CommandKind(redeliverCommandName, 0, redeliverTimeout)
}

// redeliverPayload chỉ mang mã dòng nhật ký gọi lại (không ghi chú, không SĐT).
type redeliverPayload struct {
	LogID uuid.UUID `json:"logId"`
}

type redeliverResult struct {
	Result string `json:"result"`
}

// RedeliverCommand là lệnh giao lại ở hãng (commands.Handler, Looker).
type RedeliverCommand struct {
	s *Service
}

// NewRedeliverCommand tạo lệnh giao lại dùng adapter, database của s.
func NewRedeliverCommand(s *Service) *RedeliverCommand { return &RedeliverCommand{s: s} }

// Kind trả RedeliverKind().
func (*RedeliverCommand) Kind() queue.Kind { return RedeliverKind() }

// AutoRetryNotSent luôn false: chắc chưa gửi → dead (cảnh báo của commands), nhân viên báo hãng tay.
func (*RedeliverCommand) AutoRetryNotSent() bool { return false }

// Lookup (commands.Looker) luôn unknown: không tra được hãng đã nhận yêu cầu giao lại hay chưa.
func (*RedeliverCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusUnknown, nil
}

// redeliverTarget là dòng nhật ký của lệnh kèm vận đơn, tài khoản (giải mã), adapter; refused khác nil: không gọi hãng.
type redeliverTarget struct {
	row     store.RedeliverTargetRow
	acc     Account
	ad      CarrierAdapter
	refused *commands.Outcome
}

// Execute (ngoài transaction): vận đơn còn delivery_failed, tài khoản api, adapter khai Redeliver, có mã hãng →
// adapter.Redeliver(mã hãng, ghi chú của nhật ký). UpdateDone, UpdatePending → done; UpdateRejected → rejected; kết
// quả lạ, lỗi không rõ → lỗi (in_doubt, không gửi lại); lỗi chắc → như lệnh tạo (classifyCreateError).
func (c *RedeliverCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p redeliverPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.LogID == uuid.Nil || j.TenantID == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	t, err := c.target(ctx, *j.TenantID, p.LogID)
	if stop, failed := targetFailed(err); failed {
		return stop, nil
	}
	if t.refused != nil {
		return *t.refused, nil
	}
	res, err := t.ad.Redeliver(ctx, t.acc, t.row.TrackingCode.String, t.row.Note)
	if err != nil {
		out, _, known := classifyCreateError(err)
		if !known {
			return commands.Outcome{}, fmt.Errorf("giao lại ở hãng: %w", err)
		}
		return out, nil
	}
	switch res {
	case UpdateDone, UpdatePending:
	case UpdateRejected:
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonCarrierRefused}, nil
	default:
		return commands.Outcome{}, fmt.Errorf("giao lại ở hãng: %s", reasonRedeliverOutcome) //nolint:err113 // in_doubt
	}
	b, err := json.Marshal(redeliverResult{Result: string(res)})
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả giao lại: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

// target đọc nhật ký, vận đơn, tài khoản trong một transaction ngắn (ngoài transaction của lệnh). Không có nhật ký →
// ErrNotFound.
func (c *RedeliverCommand) target(ctx context.Context, tenant, logID uuid.UUID) (redeliverTarget, error) {
	var t redeliverTarget
	err := tenancy.WithTenant(ctx, c.s.d.DB, tenant, func(tx pgx.Tx) error {
		row, err := store.New(tx).RedeliverTarget(ctx, logID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNotFound
		case err != nil:
			return fmt.Errorf("đọc nhật ký của lệnh giao lại: %w", err)
		}
		ad, ok := c.s.adapter(row.CarrierKey)
		t = redeliverTarget{row: row, ad: ad, refused: redeliverRefusal(row, ad, ok)} // gán trọn: RetryTx có thể chạy lại
		if t.refused == nil {
			t.acc, t.refused, err = c.s.commandAccount(ctx, tx, row.CarrierAccountID, row.CarrierKey, false)
		}
		return err
	}, db.TxWorker)
	return t, err //nolint:wrapcheck // lỗi đã bọc trong thân
}

// redeliverRefusal: lệnh không gọi hãng được cho vận đơn của row → kết quả dừng; nil: gọi được. Không có adapter →
// not_sent; vận đơn không còn delivery_failed, hãng không giao lại qua API, chưa có mã hãng → rejected.
func redeliverRefusal(row store.RedeliverTargetRow, ad CarrierAdapter, ok bool) *commands.Outcome {
	reason := ""
	switch {
	case !ok:
		return &commands.Outcome{Status: commands.StatusNotSent, Reason: reasonNoAdapter}
	case row.ShipmentStatus != string(StatusDeliveryFailed):
		reason = reasonNotFailed
	case row.Mode != ModeAPI || !ad.Capabilities().Redeliver:
		reason = reasonNotSupported
	case !row.TrackingCode.Valid:
		reason = reasonNoTracking
	default:
		return nil
	}
	return &commands.Outcome{Status: commands.StatusRejected, Reason: reason}
}
