package cash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/cash/internal/postings"
	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// ConsumerName là tên consumer của sổ COD (việc event.cash); đổi tên làm mất tiến độ.
const ConsumerName = "cash"

// Loại phát hiện consumer mở theo sự kiện vận đơn (cash.reconcile_findings.kind, run_id rỗng).
const (
	// FindingRemittedThenReturned: hãng sửa "đã giao" thành "hoàn" khi tiền đã về shop — sổ không tự đổi (Q-1).
	FindingRemittedThenReturned = postings.FindingRemittedThenReturned
	// FindingUnexpectedCancelAfterPickup: hãng xác nhận hủy khi sổ đã ghi lấy hàng.
	FindingUnexpectedCancelAfterPickup = postings.FindingUnexpectedCancelAfterPickup
)

// Lý do của bút toán đảo collected khi "đã giao" thành "hoàn": hãng sửa lại, hay nhân viên đảo vì bấm nhầm (nguồn staff
// trong payload, Task 138).
const (
	reasonCarrierCorrected = "Hãng sửa trạng thái từ Thành công sang Đang chuyển hoàn"
	reasonStaffCorrected   = "Nhân viên sửa trạng thái từ Thành công sang Đang chuyển hoàn"
)

// sourceStaff là nguồn "nhân viên" của bước vận đơn (fulfillment.SrcStaff).
const sourceStaff = string(fulfillment.SrcStaff)

// reversalReason là lý do bút toán đảo theo nguồn source của bước.
func reversalReason(source string) string {
	if source == sourceStaff {
		return reasonStaffCorrected
	}
	return reasonCarrierCorrected
}

// Consumer ghi sổ COD theo shipment.status_changed (events.Consumer; cmd/crm đăng ký qua Module.Consumers). Mỗi sự
// kiện: khóa tư vấn sổ COD của vận đơn (LockShipmentCOD), đọc COD X qua fulfillment.shipments_v, số dư và bút toán của
// vận đơn trong cùng tx, rồi ghi theo luật postings.Rules (tác động trong payload; sự kiện M1 không có effects thì suy
// từ status): expected, collected, returned (khóa theo vận đơn và Seq = shipments.version), đảo collected bằng
// Reverse, phát hiện remitted_then_returned (kèm lệnh cảnh báo RemittedReturnedAlertKind) hay
// unexpected_cancel_after_pickup. Bút toán do consumer M1 đã ghi (khóa evt:<sự kiện>:<loại>) không bị ghi lặp: luật
// chỉ ghi expected, collected khi vận đơn chưa có bút toán cùng loại chưa bị đảo. Việc event.cash chạy thứ tự nghiêm
// theo vận đơn; sự kiện giao lại cho cùng kết quả. COD 0 → không ghi gì. Payload hỏng, vận đơn không có, COD hay tiền
// hãng báo đã thu âm → queue.ErrPermanent.
type Consumer struct{}

// Name trả ConsumerName.
func (Consumer) Name() string { return ConsumerName }

// Topics trả shipment.status_changed.
func (Consumer) Topics() []string { return []string{fulfillment.TopicShipmentStatusChanged} }

// Handle ghi bút toán của sự kiện e trong tx của worker.
func (Consumer) Handle(ctx context.Context, e events.StoredEvent, tx pgx.Tx) error {
	var p fulfillment.StatusChanged
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.ShipmentID == uuid.Nil {
		return fmt.Errorf("sự kiện %s: payload không hợp lệ: %w", e.ID, queue.ErrPermanent)
	}
	q := store.New(tx)
	if err := q.LockShipmentCOD(ctx, p.ShipmentID); err != nil {
		return fmt.Errorf("khóa sổ COD của vận đơn: %w", err)
	}
	sh, err := q.ShipmentCOD(ctx, p.ShipmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("vận đơn %s không có: %w", p.ShipmentID, queue.ErrPermanent)
	}
	if err != nil {
		return fmt.Errorf("đọc COD của vận đơn: %w", err)
	}
	if sh.CodAmount == 0 {
		return nil
	}
	in := postings.Input{
		ShipmentID: p.ShipmentID, Seq: e.Seq, Status: string(p.Status), Effects: p.Effects, COD: sh.CodAmount,
		CODCollected: p.CODCollected,
	}
	if in.Balances, in.Transfers, err = shipmentLedger(ctx, tx, p.ShipmentID); err != nil {
		return err
	}
	acts, err := postings.Rules(in)
	if err != nil {
		return fmt.Errorf("sự kiện %s: %w: %w", e.ID, err, queue.ErrPermanent)
	}
	for _, a := range acts {
		if err := apply(ctx, tx, e, p, sh.Code, a); err != nil {
			return err
		}
	}
	return nil
}

// apply làm một việc của luật trong tx cho sự kiện p của vận đơn.
func apply(ctx context.Context, tx pgx.Tx, e events.StoredEvent, p fulfillment.StatusChanged, code string,
	a postings.Action,
) error {
	shipmentID := p.ShipmentID
	carrier := Actor{Type: ActorCarrier}
	switch {
	case a.Post != nil:
		t := Transfer{
			ShipmentID: shipmentID, Kind: a.Post.Kind, IdempotencyKey: a.Post.Key, Actor: carrier,
			Entries: make([]Entry, len(a.Post.Entries)),
		}
		for i, en := range a.Post.Entries {
			t.Entries[i] = Entry{Account: en.Account, Amount: en.Amount}
		}
		if _, _, err := PostWithID(ctx, tx, t); err != nil {
			return fmt.Errorf("ghi bút toán %s: %w", a.Post.Kind, err)
		}
	case a.Reverse != uuid.Nil:
		if _, err := Reverse(ctx, tx, a.Reverse, reversalReason(p.Source), carrier); err != nil {
			return fmt.Errorf("đảo bút toán collected: %w", err)
		}
	case a.Finding != "":
		return openFinding(ctx, tx, e, shipmentID, code, a)
	}
	return nil
}

// openFinding mở phát hiện a.Finding của vận đơn (khóa a.FindingKey: sự kiện giao lại không mở thêm); phát hiện mới
// loại remitted_then_returned kèm lệnh cảnh báo thường (chỉ mã vận đơn nội bộ).
func openFinding(ctx context.Context, tx pgx.Tx, e events.StoredEvent, shipmentID uuid.UUID, code string, a postings.Action) error {
	detail, err := json.Marshal(map[string]any{"eventId": e.ID, "seq": e.Seq})
	if err != nil {
		return fmt.Errorf("mã hóa chi tiết phát hiện: %w", err)
	}
	n, err := store.New(tx).InsertEventFinding(ctx, store.InsertEventFindingParams{
		Kind: a.Finding, ShipmentID: pgtype.UUID{Bytes: shipmentID, Valid: true}, Detail: detail,
		SourceKey: pgtype.Text{String: a.FindingKey, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("ghi phát hiện %s: %w", a.Finding, err)
	}
	if n == 0 || a.Finding != FindingRemittedThenReturned {
		return nil
	}
	tenant := e.TenantID
	if _, _, err := queue.NewQueue().Enqueue(ctx, tx, remittedReturnedAlertKind(), queue.NewJob{
		TenantID: &tenant, Payload: remittedReturnedPayload{ShipmentID: shipmentID, Code: code},
		DedupeKey: a.FindingKey,
	}); err != nil {
		return fmt.Errorf("ghi lệnh cảnh báo tiền đã về rồi hoàn: %w", err)
	}
	return nil
}

// shipmentLedger đọc số dư và bút toán (kèm vế, đã bị đảo chưa) của vận đơn trong tx.
func shipmentLedger(ctx context.Context, tx pgx.Tx, shipmentID uuid.UUID) (postings.Balances, []postings.Transfer, error) {
	b, err := BalancesFor(ctx, tx, shipmentID)
	if err != nil {
		return postings.Balances{}, nil, err
	}
	q := store.New(tx)
	rows, err := q.ShipmentTransfers(ctx, shipmentID)
	if err != nil {
		return postings.Balances{}, nil, fmt.Errorf("đọc bút toán của vận đơn: %w", err)
	}
	lines, err := q.ShipmentEntries(ctx, shipmentID)
	if err != nil {
		return postings.Balances{}, nil, fmt.Errorf("đọc dòng sổ của vận đơn: %w", err)
	}
	byTransfer := make(map[uuid.UUID][]postings.Entry, len(rows))
	for _, l := range lines {
		byTransfer[l.TransferID] = append(byTransfer[l.TransferID], postings.Entry{Account: l.Account, Amount: l.Amount})
	}
	ts := make([]postings.Transfer, len(rows))
	for i, r := range rows {
		ts[i] = postings.Transfer{
			ID: r.ID, Kind: r.Kind, Key: r.IdempotencyKey, Reversed: r.Reversed, Entries: byTransfer[r.ID],
		}
	}
	return postings.Balances{
		ToCollect: b.ToCollect, CarrierHolding: b.CarrierHolding, ShopReceived: b.ShopReceived,
		CarrierFee: b.CarrierFee, SalesClearing: b.SalesClearing,
	}, ts, nil
}
