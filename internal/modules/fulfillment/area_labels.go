package fulfillment

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/labels"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng labels (kế hoạch M3, Task M3-16): in nhãn theo lô mỗi tài khoản hãng (lệnh tải nhãn chạy nền, labels_command.go).
// In không đổi trạng thái vận đơn hay đóng gói (VC04, AT08): chỉ label_file_id, label_printed_at được ghi. Phiếu nhặt
// hàng (ghép view của catalog) ở internal/queries; đóng gói là order.Writer.SetPackingStatus (lớp HTTP gọi).

const (
	// MaxLabelShipments là số vận đơn lớn nhất của một yêu cầu in (và một lô).
	MaxLabelShipments = 200
	// labelURLTTL: hạn của đường dẫn tải nhãn ký sẵn.
	labelURLTTL = 5 * time.Minute
	// labelFileName: tên tệp khi trình duyệt tải nhãn về.
	labelFileName = "nhan-van-don.pdf"

	// LabelFileExpired là errorCode của lô ready mà tệp nhãn đã quá hạn giữ hay mất (không có URL).
	LabelFileExpired = "file_expired"

	batchPending = "pending"
	batchReady   = "ready"
)

// ErrLabelBatchInvalid là lỗi khi yêu cầu in không có vận đơn nào hay quá MaxLabelShipments mã.
var ErrLabelBatchInvalid = errors.New("fulfillment: yêu cầu in nhãn phải có 1–200 vận đơn")

// LabelBatchSummary là một lô vừa tạo: tài khoản hãng, số vận đơn.
type LabelBatchSummary struct {
	ID, CarrierAccountID uuid.UUID
	Count                int
}

// LabelSkip là vận đơn không vào lô nào kèm lý do (no_carrier_label, no_tracking_code, cancelled).
type LabelSkip struct {
	ShipmentID uuid.UUID
	Reason     string
}

// LabelBatchCreated là kết quả CreateLabelBatch.
type LabelBatchCreated struct {
	Batches []LabelBatchSummary
	Skipped []LabelSkip
}

// LabelBatchView là trạng thái một lô cho màn hình; URL (ký sẵn 5 phút) chỉ có khi ready, ErrorCode khi failed.
type LabelBatchView struct {
	ID, CarrierAccountID uuid.UUID
	Status               string
	Count                int
	URL, ErrorCode       string
	CreatedAt            time.Time
}

// CreateLabelBatch tạo lô in nhãn (người tạo a) cho shipmentIDs (1–200, mã trùng tính một lần) trong tx của bên gọi (WriteTx):
// mỗi tài khoản hãng một lô pending và một lệnh LabelsKind (hãng không in nhiều vận đơn một lần → mỗi vận đơn một lô,
// Q-18); vận đơn nhập tay, của hãng không in nhãn, chưa có mã hãng, đã hủy → Skipped. authorize kiểm phạm vi theo kho
// của đơn của từng vận đơn: một vận đơn bị từ chối hay không có → ErrNotFound (cả yêu cầu không ghi gì, không lộ vận
// đơn nào). Không khóa, không đổi vận đơn (AT08).
func (s *Service) CreateLabelBatch(ctx context.Context, tx pgx.Tx, shipmentIDs []uuid.UUID, a Actor,
	authorize Authorize,
) (LabelBatchCreated, error) {
	ids := uniqueIDs(shipmentIDs)
	if len(ids) == 0 || len(shipmentIDs) > MaxLabelShipments {
		return LabelBatchCreated{}, ErrLabelBatchInvalid
	}
	q := store.New(tx)
	rows, err := q.LabelShipments(ctx, ids)
	if err != nil {
		return LabelBatchCreated{}, fmt.Errorf("đọc vận đơn cần in: %w", err)
	}
	if len(rows) != len(ids) {
		return LabelBatchCreated{}, ErrNotFound
	}
	plan := make([]labels.Shipment, 0, len(rows))
	for _, r := range rows {
		if authorize == nil || authorize(r.WarehouseID) != nil {
			return LabelBatchCreated{}, ErrNotFound
		}
		sh := labels.Shipment{
			ID: r.ID, AccountID: r.CarrierAccountID, Mode: r.Mode, Status: r.Status,
			HasTracking: r.TrackingCode.Valid && r.TrackingCode.String != "",
		}
		if ad, ok := s.adapter(r.CarrierKey); ok {
			c := ad.Capabilities()
			sh.Labels, sh.Batch = c.Labels, c.LabelsBatch
		}
		plan = append(plan, sh)
	}
	groups, skips := labels.Plan(plan)
	out := LabelBatchCreated{Batches: make([]LabelBatchSummary, 0, len(groups)), Skipped: make([]LabelSkip, 0, len(skips))}
	for _, sk := range skips {
		out.Skipped = append(out.Skipped, LabelSkip{ShipmentID: sk.ShipmentID, Reason: sk.Reason})
	}
	for _, g := range groups {
		id, err := s.enqueueLabels(ctx, tx, q, g, a)
		if err != nil {
			return LabelBatchCreated{}, err
		}
		out.Batches = append(out.Batches, LabelBatchSummary{ID: id, CarrierAccountID: g.AccountID, Count: len(g.ShipmentIDs)})
	}
	return out, nil
}

// enqueueLabels chèn lô pending của nhóm g và lệnh tải nhãn của nó (chống trùng theo mã lô) trong tx.
func (s *Service) enqueueLabels(ctx context.Context, tx pgx.Tx, q *store.Queries, g labels.Group, a Actor,
) (uuid.UUID, error) {
	id, err := q.InsertLabelBatch(ctx, store.InsertLabelBatchParams{
		CarrierAccountID: g.AccountID, ShipmentIds: g.ShipmentIDs, CreatedBy: a.UserID,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi lô nhãn: %w", err)
	}
	tenant := a.TenantID
	if _, _, err := s.queue().Enqueue(ctx, tx, LabelsKind(), queue.NewJob{
		TenantID: &tenant, Payload: labelsPayload{BatchID: id}, DedupeKey: id.String(),
	}); err != nil {
		return uuid.Nil, fmt.Errorf("ghi lệnh tải nhãn: %w", err)
	}
	return id, nil
}

// LabelBatch đọc lô id cho màn hình; authorize kiểm mọi kho của các đơn trong lô (một kho bị từ chối hay không có lô →
// ErrNotFound). Lô ready → URL ký sẵn 5 phút tới tệp nhãn (files.DownloadURL); không đổi gì (AT08).
func (s *Service) LabelBatch(ctx context.Context, tx pgx.Tx, id uuid.UUID, authorize Authorize) (LabelBatchView, error) {
	q := store.New(tx)
	b, err := q.GetLabelBatch(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return LabelBatchView{}, ErrNotFound
	}
	if err != nil {
		return LabelBatchView{}, fmt.Errorf("đọc lô nhãn: %w", err)
	}
	whs, err := q.LabelBatchWarehouses(ctx, b.ShipmentIds)
	if err != nil {
		return LabelBatchView{}, fmt.Errorf("đọc kho của lô nhãn: %w", err)
	}
	for _, wh := range whs {
		if authorize == nil || authorize(wh) != nil {
			return LabelBatchView{}, ErrNotFound
		}
	}
	v := LabelBatchView{
		ID: b.ID, CarrierAccountID: b.CarrierAccountID, Status: b.Status, Count: len(b.ShipmentIds),
		ErrorCode: b.ErrorCode.String, CreatedAt: b.CreatedAt.UTC(),
	}
	if b.Status == batchReady && b.FileID.Valid {
		if s.d.Files == nil {
			return LabelBatchView{}, errFilesNotWired
		}
		v.URL, err = files.DownloadURL(ctx, tx, s.d.Files, b.FileID.Bytes, labelURLTTL, labelFileName)
		switch {
		case errors.Is(err, files.ErrExpired), errors.Is(err, files.ErrNotFound):
			// Tệp quá hạn giữ 30 ngày hay đã mất: lô vẫn ready, không có đường dẫn; in lại tạo lô mới.
			v.URL, v.ErrorCode = "", LabelFileExpired
		case err != nil:
			return LabelBatchView{}, fmt.Errorf("ký đường dẫn nhãn: %w", err)
		}
	}
	return v, nil
}

// uniqueIDs trả ids bỏ trùng, theo thứ tự tăng dần (bản sao, không đổi ids).
func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	out := slices.SortedFunc(slices.Values(ids), func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
	return slices.Compact(out)
}

// labelsHandlers: lệnh tải nhãn command.shipment.labels khi có database, kho tệp và adapter hãng.
func (m *Module) labelsHandlers() []commands.Handler {
	if m.d.DB == nil || m.d.Files == nil || m.d.Carriers == nil || len(m.d.Carriers.Keys()) == 0 {
		return nil
	}
	return []commands.Handler{NewLabelsCommand(m.Service)}
}

func (*Module) labelsInbound() []webhooks.InboundHandler { return nil }
func (*Module) labelsSources() []webhooks.Source         { return nil }
func (*Module) labelsConsumers() []events.Consumer       { return nil }
func (*Module) labelsSchedules() []schedules.Schedule    { return nil }
func (*Module) labelsCleanups() []schedules.CleanupFunc  { return nil }
