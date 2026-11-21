package fulfillment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/create"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/manual"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng manual (kế hoạch M3, Task M3-15): nhập mã tay và hãng "tự giao". Tài khoản mode manual (của hãng bất kỳ) không
// gọi adapter của hãng: Ship tạo vận đơn rồi gắn mã nhân viên dán (self: mã vận đơn nội bộ) qua AttachTrackingCode,
// không lệnh gửi hãng; SetManualStatus ghi tay trạng thái bằng một sự kiện tổng hợp qua ApplyTracking (nguồn staff) —
// cùng máy trạng thái, cùng tác động lên đơn, tồn, sổ COD như vận đơn API. Quyết định thuần ở internal/manual. Vùng
// không có việc nền.

func (*Module) manualHandlers() []commands.Handler       { return nil }
func (*Module) manualInbound() []webhooks.InboundHandler { return nil }
func (*Module) manualSources() []webhooks.Source         { return nil }
func (*Module) manualConsumers() []events.Consumer       { return nil }
func (*Module) manualSchedules() []schedules.Schedule    { return nil }
func (*Module) manualCleanups() []schedules.CleanupFunc  { return nil }

// Lỗi của ghi tay trạng thái vận đơn.
var (
	// ErrManualReasonRequired: ghi tay trạng thái mà lý do trống (hay chỉ khoảng trắng).
	ErrManualReasonRequired = errors.New("fulfillment: thiếu lý do ghi tay trạng thái vận đơn")
	// ErrManualNotAllowed: vận đơn không ghi tay được trạng thái đó — vận đơn api không có việc hủy tay mở, trạng
	// thái đích không ghi tay được (returned chỉ qua nhận hàng hoàn), hay máy trạng thái không nhận (cạnh không hợp
	// lệ, trùng trạng thái, giờ cũ hơn sự kiện đã xét).
	ErrManualNotAllowed = errors.New("fulfillment: vận đơn không ghi tay được trạng thái này")
)

const (
	// manualClockSkew: giờ nhân viên ghi được đi trước đồng hồ của Service tối đa chừng này (lệch đồng hồ máy).
	manualClockSkew = 5 * time.Minute
	maxManualReason = 1000 // maxLength của SetShipmentManualStatusRequest.reason
	// manualEventPrefix là tiền tố khóa sự kiện ghi tay (mỗi lần một khóa mới; chống bấm hai lần do Idempotency-Key).
	manualEventPrefix = "staff:"
)

// ManualStatusInput là một lần nhân viên ghi tay trạng thái vận đơn: To (shipping, delivery_failed, delivered,
// returning, cancelled), giờ hãng hay shop báo At, lý do Reason (bắt buộc), tiền COD đã thu CODCollected (chỉ khi
// delivered), Version (có thì phải khớp). Authorize nil: không kiểm phạm vi (test).
type ManualStatusInput struct {
	To           Status
	At           time.Time
	Reason       string
	CODCollected *int64
	Version      *int64
	Authorize    Authorize
}

// SetManualStatus ghi tay trạng thái in.To cho vận đơn id trong tx của bên gọi: chỉ vận đơn của tài khoản tay (mọi
// hãng, cả self), hay vận đơn api có việc hủy tay mở (manual_cancel, in_doubt_cancel; chỉ cancelled "hãng đã hủy",
// shipping "hãng không cho hủy"). Khóa đơn rồi vận đơn (ADR-46 nhóm 7 → 8), Authorize với kho của tài khoản hãng, so
// version, rồi dựng một CarrierEvent tổng hợp (khóa "staff:<uuid>") và gọi ApplyTracking (nguồn staff, người làm a) —
// cùng máy trạng thái, cùng tác động, cùng shipment.status_changed như vận đơn API. Sau khi áp mà vận đơn không còn
// cancel_requested: đóng việc hủy tay (resolution "staff_<trạng thái>"). Audit do bên gọi ghi.
//
// Lỗi (bên gọi lùi transaction, không để lại lịch sử hay việc cần xem): lý do trống → ErrManualReasonRequired; giờ
// trống hay ở tương lai, COD âm hay COD khi không phải delivered, lý do quá dài → ErrShipInvalid; không có vận đơn →
// ErrNotFound; version lệch → ErrVersionConflict; không được ghi tay hay máy trạng thái không nhận →
// ErrManualNotAllowed; lỗi của Authorize, của ApplyTracking trả nguyên.
func (s *Service) SetManualStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, in ManualStatusInput, a order.Actor,
) (ShipmentView, error) {
	if s.d.Events == nil || s.d.Queue == nil {
		return ShipmentView{}, errNotWired
	}
	now := s.d.Now().UTC()
	if err := validManualInput(in, now); err != nil {
		return ShipmentView{}, err
	}
	q := store.New(tx)
	l, err := lockForCancel(ctx, tx, q, id, in.Authorize)
	if err != nil {
		return ShipmentView{}, err
	}
	if in.Version != nil && *in.Version != l.Version {
		return ShipmentView{}, fmt.Errorf("vận đơn %s version %d: %w", id, l.Version, ErrVersionConflict)
	}
	open, err := q.StaffCancelTaskOpen(ctx, id)
	if err != nil {
		return ShipmentView{}, fmt.Errorf("đọc việc hủy tay của vận đơn: %w", err)
	}
	to := shipstate.Status(in.To)
	switch manual.Check(manual.Shipment{Mode: l.Mode, CarrierKey: l.CarrierKey, StaffCancelOpen: open}, to, in.Reason) {
	case manual.OK:
	case manual.ReasonRequired:
		return ShipmentView{}, ErrManualReasonRequired
	case manual.NotManual, manual.TargetNotAllowed:
		return ShipmentView{}, fmt.Errorf("ghi tay %s lên vận đơn %s (%s): %w", to, l.Status, l.Mode, ErrManualNotAllowed)
	}
	canon, _ := manual.Canonical(to)
	rep, err := s.ApplyTracking(ctx, tx, ApplyInput{
		ShipmentID: id, Source: SrcStaff, Actor: a, SyncedAt: now,
		Tracking: Tracking{
			Events: []CarrierEvent{{
				Key: manualEventPrefix + uuid.NewString(), RawStatus: "staff_" + string(to), Canonical: canon,
				Reason: in.Reason, OccurredAt: in.At.UTC(),
			}},
			CODCollected: in.CODCollected,
		},
	})
	if err != nil {
		return ShipmentView{}, fmt.Errorf("áp trạng thái ghi tay: %w", err)
	}
	if rep.Applied == 0 {
		return ShipmentView{}, fmt.Errorf("ghi tay %s lên vận đơn %s: máy trạng thái không nhận: %w", to, l.Status,
			ErrManualNotAllowed)
	}
	if open && rep.To != shipstate.CancelRequested {
		if err := q.ResolveStaffCancelAnomalies(ctx, store.ResolveStaffCancelAnomaliesParams{
			ShipmentID: id, Resolution: pgtype.Text{String: "staff_" + string(to), Valid: true},
			ResolvedBy: pgtype.UUID{Bytes: a.UserID, Valid: a.UserID != uuid.Nil},
		}); err != nil {
			return ShipmentView{}, fmt.Errorf("đóng việc hủy tay: %w", err)
		}
	}
	return s.ShipmentView(ctx, tx, id)
}

// validManualInput kiểm phần không cần database của lần ghi tay (lý do, giờ, COD) theo đồng hồ now của Service.
func validManualInput(in ManualStatusInput, now time.Time) error {
	switch {
	case strings.TrimSpace(in.Reason) == "":
		return ErrManualReasonRequired
	case utf8.RuneCountInString(in.Reason) > maxManualReason:
		return fmt.Errorf("%w: lý do ghi tay quá dài", ErrShipInvalid)
	case in.At.IsZero() || in.At.After(now.Add(manualClockSkew)):
		return fmt.Errorf("%w: giờ ghi tay trống hoặc ở tương lai", ErrShipInvalid)
	case in.CODCollected != nil && (*in.CODCollected < 0 || in.To != StatusDelivered):
		return fmt.Errorf("%w: tiền COD đã thu chỉ khi giao xong, không âm", ErrShipInvalid)
	}
	return nil
}

// manualTracking là mã hãng của vận đơn tạo bằng tài khoản acc: tài khoản api không nhận mã, phí nhập tay (rỗng);
// tài khoản tay của hãng thật bắt buộc mã nhân viên dán; self không nhận mã nhập (mã = mã vận đơn nội bộ, điền sau khi
// chèn). Đầu vào sai → ErrShipInvalid.
func manualTracking(acc store.ShareAccountForShipRow, in ShipInput) error {
	if acc.Mode != ModeManual {
		if in.TrackingCode != "" || in.FeeActual != nil {
			return fmt.Errorf("%w: mã hãng, phí nhập tay chỉ cho tài khoản nhập tay", ErrShipInvalid)
		}
		return nil
	}
	if in.FeeActual != nil && *in.FeeActual < 0 {
		return fmt.Errorf("%w: phí thật âm", ErrShipInvalid)
	}
	if _, ok := create.ManualTrackingCode(acc.CarrierKey == SelfCarrierKey, "", in.TrackingCode); !ok {
		return fmt.Errorf("%w: mã hãng của tài khoản nhập tay (hãng %s)", ErrShipInvalid, acc.CarrierKey)
	}
	return nil
}

// attachManual gắn mã hãng cho vận đơn tay id (mã nội bộ code; vừa chèn creating, đã gắn vào đơn) bằng
// AttachTrackingCode — creating → awaiting_pickup, shipment.created, status_changed như vận đơn API — rồi đặt
// create_state manual và phí thật. Mã đã thuộc vận đơn khác cùng hãng → ErrTrackingCodeConflict (bên gọi lùi cả
// transaction, việc tracking_conflict vừa mở cũng lùi).
func (s *Service) attachManual(ctx context.Context, tx pgx.Tx, q *store.Queries, id uuid.UUID, code string,
	acc store.ShareAccountForShipRow, in ShipInput,
) (ShipmentView, error) {
	tracking, _ := create.ManualTrackingCode(acc.CarrierKey == SelfCarrierKey, code, in.TrackingCode)
	res, err := s.AttachTrackingCode(ctx, tx, id, tracking, nil)
	switch {
	case err != nil:
		return ShipmentView{}, err
	case res.Conflict:
		return ShipmentView{}, ErrTrackingCodeConflict
	}
	if err := q.MarkShipmentManual(ctx, store.MarkShipmentManualParams{FeeActual: optInt8(in.FeeActual), ID: id}); err != nil {
		return ShipmentView{}, fmt.Errorf("ghi vận đơn nhập tay: %w", err)
	}
	return s.ShipmentView(ctx, tx, id)
}
