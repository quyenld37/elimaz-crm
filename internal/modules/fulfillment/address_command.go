package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/address"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Lệnh sửa địa chỉ ở hãng (kế hoạch M3, Task M3-17).

const (
	updateAddressCommandName = "command.shipment.update_address"
	// updateAddressTimeout là hạn của một lần gọi hãng sửa địa chỉ.
	updateAddressTimeout = 20 * time.Second

	reasonChangeNotPending   = "address_change_not_pending"
	reasonNotChangeable      = "shipment_not_changeable"
	reasonUpdateOutcome      = "update_address_outcome_unknown"
	reasonBadAddressInRecord = "bad_address"
)

// UpdateAddressKind là loại lệnh command.shipment.update_address: không hết hạn khi chờ, mỗi lần gọi hãng tối đa 20 s.
func UpdateAddressKind() queue.Kind {
	return commands.CommandKind(updateAddressCommandName, 0, updateAddressTimeout)
}

// addressPayload là payload của lệnh sửa địa chỉ: chỉ mã yêu cầu (không địa chỉ).
type addressPayload struct {
	RequestID uuid.UUID `json:"requestId"`
}

func parseAddressPayload(j queue.Job) (addressPayload, error) {
	var p addressPayload
	err := json.Unmarshal(j.Payload, &p)
	if err == nil && p.RequestID != uuid.Nil && j.TenantID != nil {
		return p, nil
	}
	return addressPayload{}, fmt.Errorf("lệnh %s: payload hay tenant của lệnh sửa địa chỉ hỏng: %w", j.ID,
		queue.ErrPermanent)
}

type updateAddressResult struct {
	Result string `json:"result"`
}

// UpdateAddressCommand là lệnh sửa địa chỉ ở hãng (commands.Handler, Looker, DoneHook, queue.TerminalHook). Địa chỉ đơn
// chỉ đổi khi hãng nhận (UpdateDone), trong OnDone, kèm audit ghi bằng hàm audit.
type UpdateAddressCommand struct {
	s     *Service
	audit AuditFunc
}

// NewUpdateAddressCommand tạo lệnh sửa địa chỉ dùng adapter, database, đường ghi của s; audit ghi dòng audit đơn khi địa
// chỉ đơn đổi (bắt buộc; Module.AddressCommand kiểm).
func NewUpdateAddressCommand(s *Service, audit AuditFunc) *UpdateAddressCommand {
	return &UpdateAddressCommand{s: s, audit: audit}
}

// AddressCommand là lệnh sửa địa chỉ để cmd/crm nối khi có adapter hãng (cùng điều kiện với lệnh tạo, lệnh hủy); không
// có adapter → nil. audit nil → lỗi (địa chỉ đơn đổi trong hook thì phải có audit).
func (m *Module) AddressCommand(audit AuditFunc) (commands.Handler, error) { //nolint:ireturn // Runner nhận commands.Handler
	if audit == nil {
		return nil, errNoAudit
	}
	if m.d.Carriers == nil || len(m.d.Carriers.Keys()) == 0 {
		return nil, nil
	}
	return NewUpdateAddressCommand(m.Service, audit), nil
}

// Kind trả UpdateAddressKind().
func (*UpdateAddressCommand) Kind() queue.Kind { return UpdateAddressKind() }

// AutoRetryNotSent luôn false (kế hoạch Task M3-17): chắc chưa gửi → dead → yêu cầu rejected, nhân viên tạo yêu cầu mới.
func (*UpdateAddressCommand) AutoRetryNotSent() bool { return false }

// addressTarget là yêu cầu của lệnh đã đọc kèm vận đơn, tài khoản (giải mã, bảng địa giới), adapter; refused khác nil:
// không gọi hãng được.
type addressTarget struct {
	row     store.AddressChangeTargetRow
	to      Address
	acc     Account
	ad      CarrierAdapter
	refused *commands.Outcome
}

// target đọc yêu cầu, vận đơn và tài khoản trong một transaction ngắn (ngoài transaction của lệnh).
func (c *UpdateAddressCommand) target(ctx context.Context, j queue.Job, id uuid.UUID) (addressTarget, error) {
	var t addressTarget
	err := tenancy.WithTenant(ctx, c.s.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		t = addressTarget{} // RetryTx có thể chạy lại
		row, err := store.New(tx).AddressChangeTarget(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("đọc yêu cầu của lệnh sửa địa chỉ: %w", err)
		}
		t.row = row
		var addr order.ShipAddress
		if err := json.Unmarshal(row.NewAddress, &addr); err != nil {
			t.refused = &commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadAddressInRecord}
			return nil
		}
		t.to = shipAddress(addr)
		ad, ok := c.s.adapter(row.CarrierKey)
		if t.refused = addressRefusal(row, ad, ok); t.refused != nil {
			return nil
		}
		t.ad = ad
		t.acc, t.refused, err = c.s.commandAccount(ctx, tx, row.CarrierAccountID, row.CarrierKey,
			ad.Capabilities().Locations)
		return err
	}, db.TxWorker)
	return t, err //nolint:wrapcheck // lỗi đã bọc trong thân
}

// addressRefusal: lệnh không gọi hãng được cho yêu cầu row (adapter ad, có hay không ok) → kết quả dừng; nil: gọi được.
// Yêu cầu không còn pending, vận đơn không đổi được địa chỉ, hãng không sửa qua API, chưa có mã hãng → rejected; không
// có adapter → not_sent.
func addressRefusal(row store.AddressChangeTargetRow, ad CarrierAdapter, ok bool) *commands.Outcome {
	reason := ""
	switch {
	case row.Status != address.StatusPending:
		reason = reasonChangeNotPending
	case !address.Changeable(row.ShipmentStatus):
		reason = reasonNotChangeable
	case !ok:
		return &commands.Outcome{Status: commands.StatusNotSent, Reason: reasonNoAdapter}
	case row.Mode != ModeAPI || !ad.Capabilities().UpdateAddress:
		reason = reasonNotSupported
	case !row.TrackingCode.Valid:
		reason = reasonNoTracking
	default:
		return nil
	}
	return &commands.Outcome{Status: commands.StatusRejected, Reason: reason}
}

// Execute (ngoài transaction): yêu cầu còn pending, vận đơn còn đổi được địa chỉ → adapter.UpdateAddress theo mã hãng.
// UpdateDone, UpdateRejected, UpdatePending → done kèm kết quả (OnDone đổi yêu cầu); kết quả lạ, quá giờ, lỗi không rõ →
// lỗi (in_doubt, không gửi lại); lỗi chắc chắn → như lệnh tạo (classifyCreateError: chưa gửi → not_sent, từ chối →
// rejected; cả hai thành dead, OnTerminal đổi yêu cầu sang rejected). Yêu cầu không còn pending, không có → rejected,
// không gọi hãng.
func (c *UpdateAddressCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	p, err := parseAddressPayload(j)
	if err != nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	t, err := c.target(ctx, j, p.RequestID)
	if stop, failed := targetFailed(err); failed {
		return stop, nil
	}
	if t.refused != nil {
		return *t.refused, nil
	}
	res, err := t.ad.UpdateAddress(ctx, t.acc, t.row.TrackingCode.String, t.to)
	if err != nil {
		out, _, known := classifyCreateError(err)
		if !known {
			return commands.Outcome{}, fmt.Errorf("sửa địa chỉ ở hãng: %w", err)
		}
		return out, nil
	}
	if _, ok := address.FromCarrier(string(res)); !ok {
		return commands.Outcome{}, fmt.Errorf("sửa địa chỉ ở hãng: %s", reasonUpdateOutcome) //nolint:err113 // in_doubt
	}
	b, err := json.Marshal(updateAddressResult{Result: string(res)})
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả sửa địa chỉ: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

// Lookup (commands.Looker, ResolveInDoubt) luôn unknown: không tra được kết quả sửa địa chỉ ở mức chung (kế hoạch Task
// M3-17); yêu cầu ở unknown chờ nhân viên xác nhận (ResolveAddressChange).
func (*UpdateAddressCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusUnknown, nil
}

// OnDone (trong transaction đổi lệnh sang done): khóa đơn, vận đơn rồi yêu cầu; yêu cầu còn mở → theo kết quả hãng:
// done → confirmed (ship_to, địa chỉ đơn mới, audit người làm system), rejected → rejected, pending hay không có kết quả
// → sent (nhân viên xác nhận). Yêu cầu đã đóng, không có → không đổi gì.
func (c *UpdateAddressCommand) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	if c.audit == nil {
		return errNoAudit // đổi địa chỉ đơn mà không ghi được audit: không cho thành done
	}
	p, err := parseAddressPayload(j)
	if err != nil {
		return err
	}
	var r updateAddressResult
	if len(result) > 0 && string(result) != "null" {
		if err := json.Unmarshal(result, &r); err != nil {
			return fmt.Errorf("lệnh %s: kết quả sửa địa chỉ không đọc được: %w", j.ID, queue.ErrPermanent)
		}
	}
	status, ok := address.FromCarrier(r.Result)
	if !ok {
		status = address.StatusSent
	}
	res, err := c.close(ctx, tx, j, p.RequestID, status)
	if err != nil || res.OrderAudit == nil {
		return err
	}
	if err := c.audit(ctx, tx, *res.OrderAudit); err != nil {
		return fmt.Errorf("ghi audit đổi địa chỉ đơn: %w", err)
	}
	return nil
}

// OnTerminal (queue.TerminalHook, trong transaction đổi lệnh sang trạng thái kết thúc): yêu cầu còn mở → in_doubt:
// unknown (nhân viên xác nhận); dead, expired: rejected (hãng chưa nhận). Khác → không đổi gì.
func (c *UpdateAddressCommand) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	p, err := parseAddressPayload(j)
	if err != nil {
		return nil //nolint:nilerr // payload hỏng: không có yêu cầu để đổi, lệnh vẫn kết thúc
	}
	to := address.Terminal(status)
	if to == "" {
		return nil
	}
	_, err = c.close(ctx, tx, j, p.RequestID, to)
	return err
}

// close khóa đơn, vận đơn rồi yêu cầu id của lệnh j và đổi yêu cầu còn mở sang status (closeAddressChange, người làm
// system). Không có yêu cầu, yêu cầu đã đóng → kết quả rỗng, nil.
func (c *UpdateAddressCommand) close(ctx context.Context, tx pgx.Tx, j queue.Job, id uuid.UUID, status string,
) (AddressChangeResult, error) {
	q := store.New(tx)
	l, r, err := lockAddressChange(ctx, tx, q, id, nil)
	switch {
	case errors.Is(err, ErrNotFound):
		return AddressChangeResult{}, nil
	case err != nil:
		return AddressChangeResult{}, err
	case !address.Open(r.Status) || r.Status == status:
		return AddressChangeResult{}, nil
	}
	return c.s.closeAddressChange(ctx, tx, q, l, r, status, pgtype.Text{}, auditActor{tenant: *j.TenantID, kind: actorSystem})
}

// commandAccount đọc tài khoản id (giải mã) cho một lệnh gọi hãng, kèm bảng mã địa giới của hãng carrierKey khi
// locations: tài khoản không active, không có → rejected (account_unavailable); thiếu khóa, khóa không đọc được →
// not_sent (credentials_unreadable); lỗi khác trả lên. refused khác nil: không gọi hãng.
func (s *Service) commandAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID, carrierKey string, locations bool,
) (Account, *commands.Outcome, error) {
	acc, err := s.ResolveAccount(ctx, tx, id)
	switch {
	case errors.Is(err, ErrAccountUnavailable), errors.Is(err, ErrAccountNotFound):
		return Account{}, &commands.Outcome{Status: commands.StatusRejected, Reason: reasonUnavailable}, nil
	case errors.Is(err, ErrKeysMissing), errors.Is(err, ErrCredentialsUnreadable):
		return Account{}, &commands.Outcome{Status: commands.StatusNotSent, Reason: reasonKeys}, nil
	case err != nil:
		return Account{}, nil, err
	}
	if locations {
		if acc.Locations, err = s.LocationMap(ctx, tx, carrierKey); err != nil {
			return Account{}, nil, err
		}
	}
	return acc, nil, nil
}
