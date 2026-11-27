package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/correction"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Đảo "Thành công" (kế hoạch M3, Task M3-27; TD10, TI08, Q-1). Hãng sửa lại (tra API): máy trạng thái và ApplyTracking
// đã đảo vận đơn, đơn, tồn; tác động delivery_reversed còn ghi lệnh cảnh báo thường carrier-corrected:<mã vận đơn>
// (CorrectedAlertKind). Nhân viên sửa nhầm: PreviewCorrection (không đổi dữ liệu) → ApplyCorrection theo token, cùng
// ApplyTracking với một sự kiện tổng hợp nguồn staff. Tiền COD do cash ghi khi nghe shipment.status_changed; tiền đã
// về shop thì cash mở finding remitted_then_returned — fulfillment chỉ đọc sổ (view của cash) để chặn nhân viên.

// CorrectedAlertKind là tên loại lệnh cảnh báo thường "hãng sửa Thành công sang Đang chuyển hoàn"
// (key carrier-corrected:<mã vận đơn nội bộ>).
const CorrectedAlertKind = "fulfillment.carrier_corrected_alert"

const (
	correctedAlertTimeout = 10 * time.Second
	correctionRawStatus   = "staff_correction"
)

// Lỗi của việc đảo "Thành công".
var (
	// ErrCorrectionInvalid: lý do trống hay quá 1000 ký tự, đích không phải returning, áp thiếu token.
	ErrCorrectionInvalid = errors.New("fulfillment: yêu cầu đảo Thành công không hợp lệ")
	// ErrCorrectionNotAllowed: vận đơn không đảo được (chưa giao, đã giao một phần, không phải vận đơn hiện hành, máy
	// trạng thái không nhận).
	ErrCorrectionNotAllowed = errors.New("fulfillment: vận đơn không đảo Thành công được")
	// ErrCorrectionBlocked: tiền COD đã về shop (bảng kê đã chốt) — kế toán điều chỉnh sổ COD trước.
	ErrCorrectionBlocked = errors.New("fulfillment: tiền COD đã về shop, chưa đảo Thành công được")
	// ErrCorrectionStale: token của bản xem trước không khớp bản tính lại dưới khóa.
	ErrCorrectionStale = errors.New("fulfillment: bản xem trước đảo Thành công đã cũ")
)

// CorrectionInput là một yêu cầu đảo "Thành công": đích To (chỉ returning), lý do Reason (bắt buộc, ≤ 1000 ký tự; bên
// gọi đã che SĐT, địa chỉ), token của bản xem trước PreviewToken (chỉ khi áp).
type CorrectionInput struct {
	To           Status
	Reason       string
	PreviewToken string
}

// CorrectionPreview là bản xem trước: câu tác động, câu chặn (khác rỗng thì không áp được), token để xác nhận.
type CorrectionPreview struct {
	ShipmentID uuid.UUID
	From, To   Status
	Effects    []string
	Blocking   []string
	Token      string
}

// PreviewCorrection tính bản xem trước việc đảo "Thành công" của vận đơn id trong tx của bên gọi (không đổi dữ liệu;
// khóa đơn rồi vận đơn như lúc áp, ADR-46 nhóm 7 → 8). Lỗi: đầu vào sai → ErrCorrectionInvalid; không có vận đơn →
// ErrNotFound; vận đơn không đảo được → ErrCorrectionNotAllowed.
func (s *Service) PreviewCorrection(ctx context.Context, tx pgx.Tx, id uuid.UUID, in CorrectionInput,
) (CorrectionPreview, error) {
	if err := validCorrection(in); err != nil {
		return CorrectionPreview{}, err
	}
	r, err := s.lockRun(ctx, tx, ApplyInput{ShipmentID: id}, order.Actor{})
	if err != nil {
		return CorrectionPreview{}, err
	}
	return r.previewCorrection(ctx)
}

// ApplyCorrection đảo "Thành công" của vận đơn id theo bản xem trước (in.PreviewToken) trong tx của bên gọi: khóa đơn
// → vận đơn, tính lại bản xem trước dưới khóa; token khác → ErrCorrectionStale; có câu chặn → ErrCorrectionBlocked;
// rồi ApplyTracking một sự kiện tổng hợp returning (khóa "staff:<uuid>", nguồn staff, lý do in.Reason, người làm a) —
// cùng tác động như hãng sửa lại (order.ReverseDelivery lý do staff_corrected, phiếu hoàn, shipment.status_changed).
// Không dòng sổ nào bị sửa hay xóa. Audit do bên gọi ghi. Lỗi khác như PreviewCorrection.
func (s *Service) ApplyCorrection(ctx context.Context, tx pgx.Tx, id uuid.UUID, in CorrectionInput, a order.Actor,
) (ShipmentView, error) {
	if s.d.Events == nil || s.d.Queue == nil {
		return ShipmentView{}, errNotWired
	}
	if err := validCorrection(in); err != nil {
		return ShipmentView{}, err
	}
	if in.PreviewToken == "" {
		return ShipmentView{}, fmt.Errorf("%w: thiếu token xem trước", ErrCorrectionInvalid)
	}
	r, err := s.lockRun(ctx, tx, ApplyInput{ShipmentID: id}, a)
	if err != nil {
		return ShipmentView{}, err
	}
	p, err := r.previewCorrection(ctx)
	switch {
	case err != nil:
		return ShipmentView{}, err
	case p.Token != in.PreviewToken:
		return ShipmentView{}, ErrCorrectionStale
	case len(p.Blocking) > 0:
		return ShipmentView{}, ErrCorrectionBlocked
	}
	rep, err := s.ApplyTracking(ctx, tx, ApplyInput{
		ShipmentID: id, Source: SrcStaff, Actor: a, SyncedAt: r.now,
		Tracking: Tracking{Events: []CarrierEvent{{
			Key: manualEventPrefix + uuid.NewString(), RawStatus: correctionRawStatus, Canonical: CanonReturning,
			Reason: in.Reason, OccurredAt: r.now,
		}}},
	})
	if err != nil {
		return ShipmentView{}, fmt.Errorf("áp đảo Thành công: %w", err)
	}
	if rep.Applied == 0 {
		return ShipmentView{}, fmt.Errorf("đảo Thành công: máy trạng thái không nhận: %w", ErrCorrectionNotAllowed)
	}
	return s.ShipmentView(ctx, tx, id)
}

func validCorrection(in CorrectionInput) error {
	switch {
	case in.To != StatusReturning:
		return fmt.Errorf("%w: đích %q", ErrCorrectionInvalid, in.To)
	case strings.TrimSpace(in.Reason) == "":
		return fmt.Errorf("%w: thiếu lý do", ErrCorrectionInvalid)
	case utf8.RuneCountInString(in.Reason) > maxManualReason:
		return fmt.Errorf("%w: lý do quá dài", ErrCorrectionInvalid)
	}
	return nil
}

// previewCorrection tính bản xem trước trên vận đơn, đơn đã khóa: chỉ vận đơn delivered mà máy trạng thái nhận cạnh
// returning nguồn staff có đảo phiếu xuất (cùng luật Decide với lúc áp: giao một phần, không phải vận đơn hiện hành, tin
// cũ đều từ chối).
func (r *applyRun) previewCorrection(ctx context.Context) (CorrectionPreview, error) {
	if r.sh.Status != string(StatusDelivered) {
		return CorrectionPreview{}, fmt.Errorf("%w: vận đơn %s", ErrCorrectionNotAllowed, r.sh.Status)
	}
	var last *time.Time
	if r.sh.LastCarrierEventAt.Valid {
		last = &r.sh.LastCarrierEventAt.Time
	}
	d := shipstate.Decide(shipstate.Input{
		Current: shipstate.Delivered, LastEventAt: last, PickedUp: r.sh.PickedUpAt.Valid, IsCurrent: r.isCurrent,
		Partial: r.sh.Partial, Event: CanonReturning, EventAt: r.now, Source: SrcStaff,
	})
	if !d.Apply || !slices.Contains(d.Effects, shipstate.EffDeliveryReversed) {
		return CorrectionPreview{}, fmt.Errorf("%w: máy trạng thái không nhận (%s)", ErrCorrectionNotAllowed, d.Ignored)
	}
	facts, err := r.q.CorrectionFacts(ctx, r.sh.ID)
	if err != nil {
		return CorrectionPreview{}, fmt.Errorf("đọc tiền COD của vận đơn: %w", err)
	}
	o, err := order.Get(ctx, r.tx, r.sh.SellerID, r.sh.OrderID)
	if err != nil {
		return CorrectionPreview{}, fmt.Errorf("đọc đơn của vận đơn: %w", err)
	}
	lines := make([]correction.Line, len(o.Items))
	for i, it := range o.Items {
		lines[i] = correction.Line{SKU: it.SKU, Qty: it.Qty}
	}
	collected := r.sh.CodAmount // cash: tiền thu = tiền hãng báo, không báo thì bằng COD (postings/rules.go)
	if facts.CodCollectedAmount.Valid && collected > 0 {
		collected = facts.CodCollectedAmount.Int64
	}
	p := correction.Decide(correction.Facts{
		ShipmentID: r.sh.ID, Version: r.sh.Version, From: r.sh.Status, To: string(StatusReturning), Lines: lines,
		Collected: collected, ShopReceived: facts.ShopReceived, Remitted: facts.Remitted,
	})
	return CorrectionPreview{
		ShipmentID: r.sh.ID, From: StatusDelivered, To: StatusReturning, Effects: p.Effects, Blocking: p.Blocking,
		Token: p.Token,
	}, nil
}

// manualCorrectionBlocked (QĐ-118-2): ghi tay returning lên vận đơn delivered theo cùng luật chặn tiền với
// ApplyCorrection — tiền đã về shop → ErrCorrectionBlocked.
func manualCorrectionBlocked(ctx context.Context, q *store.Queries, id uuid.UUID) error {
	facts, err := q.CorrectionFacts(ctx, id)
	if err != nil {
		return fmt.Errorf("đọc tiền COD của vận đơn: %w", err)
	}
	if len(correction.Blocking(facts.ShopReceived, facts.Remitted)) > 0 {
		return ErrCorrectionBlocked
	}
	return nil
}

// correctedAlert ghi lệnh cảnh báo carrier-corrected cho vận đơn của lần áp (hãng sửa lại, tra API), trong tx.
func (r *applyRun) correctedAlert(ctx context.Context, eventID uuid.UUID) error {
	facts, err := r.q.CorrectionFacts(ctx, r.sh.ID)
	if err != nil {
		return fmt.Errorf("đọc mã vận đơn cho cảnh báo: %w", err)
	}
	if _, _, err := queue.NewQueue().Enqueue(ctx, r.tx, correctedAlertKind(), queue.NewJob{
		TenantID: &r.sh.TenantID, Payload: correctedPayload{ShipmentID: r.sh.ID, Code: facts.Code},
		DedupeKey: "carrier-corrected:" + r.sh.ID.String() + ":" + eventID.String(),
	}); err != nil {
		return fmt.Errorf("ghi lệnh cảnh báo hãng sửa Thành công: %w", err)
	}
	return nil
}

func correctedAlertKind() queue.Kind {
	return commands.CommandKind(CorrectedAlertKind, 0, correctedAlertTimeout)
}

// correctedPayload chỉ mang mã: vận đơn và mã vận đơn nội bộ (không dữ liệu khách).
type correctedPayload struct {
	ShipmentID uuid.UUID `json:"shipmentId"`
	Code       string    `json:"code"`
}

// NewCorrectedAlertCommand trả lệnh CorrectedAlertKind: Notify(key "carrier-corrected:<mã vận đơn>", normal, văn bản
// chỉ có mã). Payload hỏng → rejected bad_payload; n nil → rejected alerter_missing; Notify lỗi → log một lần ở đây
// (logger nil: không log), not_sent và tự gửi lại. cmd/crm đăng ký cùng các lệnh cảnh báo khác (dùng chung Alerter).
func NewCorrectedAlertCommand(n Notifier, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return correctedAlert{n: n, log: logger}
}

type correctedAlert struct {
	n   Notifier
	log *slog.Logger
}

func (correctedAlert) Kind() queue.Kind       { return correctedAlertKind() }
func (correctedAlert) AutoRetryNotSent() bool { return true }

func (c correctedAlert) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p correctedPayload
	switch err := json.Unmarshal(j.Payload, &p); {
	case err != nil || p.ShipmentID == uuid.Nil || p.Code == "":
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	case c.n == nil:
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonAlerter}, nil
	}
	text := fmt.Sprintf("Vận đơn %s: hãng sửa trạng thái từ Thành công sang Đang chuyển hoàn. Đơn đã bỏ Thành công, "+
		"chờ kho nhận hàng hoàn; tiền COD do sổ COD xử lý (xem Lệch đối soát nếu tiền đã về).", p.Code)
	return notifyNormal(ctx, c.n, c.log, "carrier-corrected:"+p.Code, text, "gửi cảnh báo hãng sửa Thành công lỗi",
		slog.String("shipment_id", p.ShipmentID.String())), nil
}
