package fulfillment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/labels"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Lệnh tải nhãn command.shipment.labels (kế hoạch M3, Task M3-16): một lệnh mỗi lô. Execute đọc lô pending, tài khoản
// (đã giải mã), mã hãng trong một transaction ngắn, gọi adapter.Labels cho cả lô ngoài transaction, kiểm %PDF- và
// ≤ 20 MiB, ghi tệp lên bucket riêng tư bằng FileStore.Put với khóa cố định priv/<tenant>/<mã lô> (chạy lại gặp khóa
// đã có thì dùng lại đối tượng). Tải nhãn chỉ đọc: Looker luôn not_sent, AutoRetryNotSent. OnDone (ADR-46: shipments
// theo id tăng dần → platform.files → label_batches) ghi tệp, gắn nhãn cho vận đơn, lô ready; câu trả lời chắc chắn mà
// không dùng được (PDF sai, quá lớn, hãng từ chối) → lô failed kèm mã. OnTerminal (dead, expired) → failed timeout.

const (
	// LabelsKindName là tên loại lệnh tải nhãn.
	LabelsKindName       = "command.shipment.labels"
	labelsExpires        = 10 * time.Minute
	labelsCommandTimeout = 25 * time.Second
	// labelRetention: hạn giữ tệp nhãn (files.NewFile.ExpiresAt).
	labelRetention = 30 * 24 * time.Hour
	labelMIME      = "application/pdf"

	labelOutReady  = "ready"
	labelOutFailed = "failed"

	signalLabelBatch = "label_batch.ready"
	permPrint        = "fulfillment.print" // identity.Permission; fulfillment không nhập identity

	reasonCarrierUnavailable = "carrier_unavailable"
	reasonFilesUnavailable   = "files_unavailable"
)

var errFilesNotWired = errors.New("fulfillment: chưa nối kho tệp")

// LabelsKind là Kind của lệnh tải nhãn: hết hạn sau 10 phút chờ, mỗi lần chạy tối đa 25 s.
func LabelsKind() queue.Kind {
	return commands.CommandKind(LabelsKindName, labelsExpires, labelsCommandTimeout)
}

// labelsPayload là payload của lệnh: chỉ mã lô.
type labelsPayload struct {
	BatchID uuid.UUID `json:"batchId"`
}

// labelsOutcome là Outcome.Result: tệp đã ghi (ready) hay mã lỗi (failed); không có nội dung tệp, mã hãng.
type labelsOutcome struct {
	Status    string    `json:"status"`
	FileID    uuid.UUID `json:"fileId,omitzero"`
	Key       string    `json:"key,omitempty"`
	Size      int64     `json:"size,omitempty"`
	SHA256    []byte    `json:"sha256,omitempty"`
	ErrorCode string    `json:"errorCode,omitempty"`
	AuthCode  string    `json:"authCode,omitempty"`
}

// LabelsCommand là lệnh command.shipment.labels (commands.Handler, Looker, DoneHook, queue.TerminalHook) của Service s.
type LabelsCommand struct {
	s *Service
}

// NewLabelsCommand tạo lệnh tải nhãn dùng adapter, khóa, kho tệp, báo hiệu của s.
func NewLabelsCommand(s *Service) *LabelsCommand { return &LabelsCommand{s: s} }

// Kind trả LabelsKind().
func (*LabelsCommand) Kind() queue.Kind { return LabelsKind() }

// AutoRetryNotSent trả true: tải nhãn chỉ đọc, hỏi lại an toàn.
func (*LabelsCommand) AutoRetryNotSent() bool { return true }

// Lookup luôn not_sent: tải nhãn không đổi gì ở hãng (lệnh in_doubt về hàng chờ, chạy lại).
func (*LabelsCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// labelsTarget là thứ Execute đọc trước khi gọi hãng; gone: lô không còn pending; failCode khác rỗng: lô không in
// được (tài khoản không dùng được, hãng không in nhãn, không còn mã hãng).
type labelsTarget struct {
	acc      Account
	ad       CarrierAdapter
	codes    []string
	gone     bool
	failCode string
}

// Execute tải nhãn của lô trong lệnh j. Lỗi đọc database, hãng chưa trả lời chắc chắn (quá giờ, 5xx, chưa gửi), 429,
// lỗi ghi kho tệp → not_sent (thử lại); còn lại → done kèm kết quả ready hay failed.
func (c *LabelsCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	p, ok := parseLabelsPayload(j)
	if !ok {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	tenant := *j.TenantID
	t, err := c.target(ctx, tenant, p.BatchID)
	switch {
	case err != nil:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil //nolint:nilerr // chưa gọi hãng: đọc lại sau
	case t.gone:
		return commands.Outcome{Status: commands.StatusDone}, nil
	case t.failCode != "":
		return labelsOutcome{Status: labelOutFailed, ErrorCode: t.failCode}.outcome(), nil
	case c.s.d.Files == nil:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonFilesUnavailable}, nil
	}
	lbl, err := t.ad.Labels(ctx, t.acc, t.codes)
	t.acc.Creds = nil
	if err != nil {
		out, failed := classifyLabelsError(err)
		if failed != nil {
			return failed.outcome(), nil
		}
		return out, nil
	}
	if code := labels.CheckPDF(lbl.PDF, MaxLabelBytes); code != "" {
		return labelsOutcome{Status: labelOutFailed, ErrorCode: code}.outcome(), nil
	}
	return c.put(ctx, tenant, p.BatchID, lbl.PDF), nil
}

// put ghi tệp nhãn pdf của lô batchID lên bucket của mục đích carrier_label với khóa cố định (mã tệp = mã lô). Khóa đã
// có (lần chạy trước đã ghi rồi chết trước khi xong) → dùng lại đối tượng đó (kích thước đọc bằng Head, không băm).
// Lỗi kho tệp → not_sent.
func (c *LabelsCommand) put(ctx context.Context, tenant, batchID uuid.UUID, pdf []byte) commands.Outcome {
	bucket := LabelPurpose().Bucket
	key := files.ServerKey(tenant, batchID)
	out := labelsOutcome{Status: labelOutReady, FileID: batchID, Key: key, Size: int64(len(pdf))}
	sum := sha256.Sum256(pdf)
	out.SHA256 = sum[:]
	err := c.s.d.Files.Put(ctx, bucket, key, bytes.NewReader(pdf), int64(len(pdf)), labelMIME)
	if errors.Is(err, files.ErrObjectExists) {
		info, herr := c.s.d.Files.Head(ctx, bucket, key)
		if herr != nil {
			return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonFilesUnavailable}
		}
		out.Size, out.SHA256, err = info.Size, nil, nil
	}
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonFilesUnavailable}
	}
	return out.outcome()
}

// classifyLabelsError đổi lỗi gọi hãng: 429 → not_sent theo Retry-After; hãng từ chối khóa → failed auth (kèm mã của
// hãng); hãng từ chối, lỗi vĩnh viễn → failed carrier_rejected; không hỗ trợ → failed not_supported; còn lại (quá
// giờ, mạng, 5xx, chưa gửi, không rõ: tải nhãn chỉ đọc) → not_sent. failed khác nil khi lô phải failed.
func classifyLabelsError(err error) (commands.Outcome, *labelsOutcome) {
	ce, isCE := errors.AsType[*CarrierError](err)
	switch {
	case isCE && ce.Class == ClassRateLimited:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: syncRateLimited, RetryAfter: ce.RetryAfter}, nil
	case isCE && ce.Class == ClassAuth:
		code := ce.Code
		if code == "" {
			code = labels.ErrAuth
		}
		return commands.Outcome{}, &labelsOutcome{Status: labelOutFailed, ErrorCode: labels.ErrAuth, AuthCode: code}
	case errors.Is(err, ErrNotSupported):
		return commands.Outcome{}, &labelsOutcome{Status: labelOutFailed, ErrorCode: labels.ErrNotSupported}
	case isCE && (ce.Class == ClassRejected || ce.Class == ClassPermanent), !isCE && errors.Is(err, ErrRejected):
		return commands.Outcome{}, &labelsOutcome{Status: labelOutFailed, ErrorCode: labels.ErrCarrierRejected}
	}
	return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonCarrierUnavailable}, nil
}

// outcome là lệnh done kèm o (doneJSON).
func (o labelsOutcome) outcome() commands.Outcome { return doneJSON(o) }

// doneJSON là lệnh done kèm v mã hóa JSON làm Outcome.Result (lệnh báo phí, lệnh tải nhãn). v của các lệnh chỉ có
// chuỗi, số, byte, map, thời gian nên Marshal không lỗi; nếu có thì not_sent (chạy lại).
func doneJSON(v any) commands.Outcome {
	b, err := json.Marshal(v)
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonBadPayload}
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}
}

// target đọc lô, tài khoản (đã giải mã), mã hãng của lô trong một transaction ngắn; lỗi chỉ là lỗi đọc.
func (c *LabelsCommand) target(ctx context.Context, tenant, batchID uuid.UUID) (labelsTarget, error) {
	var t labelsTarget
	err := tenancy.WithTenant(ctx, c.s.d.DB, tenant, func(tx pgx.Tx) error {
		t = labelsTarget{} // RetryTx có thể chạy lại
		q := store.New(tx)
		b, err := q.GetLabelBatch(ctx, batchID)
		if errors.Is(err, pgx.ErrNoRows) {
			t.gone = true
			return nil
		}
		if err != nil {
			return fmt.Errorf("đọc lô nhãn: %w", err)
		}
		if b.Status != batchPending {
			t.gone = true
			return nil
		}
		if t.failCode, err = c.loadAccount(ctx, q, b.CarrierAccountID, &t); err != nil || t.failCode != "" {
			return err
		}
		codes, err := q.LabelTrackingCodes(ctx, b.ShipmentIds)
		if err != nil {
			return fmt.Errorf("đọc mã hãng của lô: %w", err)
		}
		for _, r := range codes {
			t.codes = append(t.codes, r.TrackingCode)
		}
		if len(t.codes) == 0 {
			t.failCode = labels.ReasonNoTrackingCode
		}
		return nil
	}, db.TxWorker)
	if err != nil {
		return labelsTarget{}, fmt.Errorf("đọc dữ liệu in nhãn: %w", err)
	}
	return t, nil
}

// loadAccount đọc tài khoản hãng accountID (đã giải mã) và adapter của nó vào t; trả mã lỗi của lô khi tài khoản
// không dùng được (không có, không active, không phải api, khóa không đọc được) hay hãng không in nhãn. Lỗi chỉ là lỗi
// đọc.
func (c *LabelsCommand) loadAccount(ctx context.Context, q *store.Queries, accountID uuid.UUID, t *labelsTarget,
) (string, error) {
	row, err := getAccount(ctx, q, accountID)
	switch {
	case errors.Is(err, ErrAccountNotFound):
		return labels.ErrAccountUnavailable, nil
	case err != nil:
		return "", err
	case row.Status != AccountActive || row.Mode != ModeAPI:
		return labels.ErrAccountUnavailable, nil
	}
	ad, ok := c.s.adapter(row.CarrierKey)
	if !ok || !ad.Capabilities().Labels {
		return labels.ErrNotSupported, nil
	}
	t.ad = ad
	t.acc, err = c.s.account(ctx, q, row)
	switch {
	case errors.Is(err, ErrKeysMissing), errors.Is(err, ErrCredentialsUnreadable):
		return labels.ErrAccountUnavailable, nil
	case err != nil:
		return "", err
	}
	return "", nil
}

// parseLabelsPayload đọc payload của lệnh j; false khi payload hỏng, thiếu mã lô hay lệnh không có tenant.
func parseLabelsPayload(j queue.Job) (labelsPayload, bool) {
	var p labelsPayload
	ok := j.TenantID != nil && json.Unmarshal(j.Payload, &p) == nil && p.BatchID != uuid.Nil
	return p, ok
}

// OnDone (trong transaction đổi lệnh sang done): ready → khóa vận đơn của lô theo id tăng dần, files.Register (mục
// đích carrier_label, hạn 30 ngày), gắn tệp và label_printed_at cho mọi vận đơn của lô, lô ready kèm file_id; failed
// (hãng từ chối khóa → MarkAuthFailed trước, carrier_accounts đứng đầu nhóm 8) → lô failed kèm mã. Lô không còn
// pending → không đổi gì. Kết quả không đọc được → failed carrier_rejected. Sau khi ghi: báo hiệu label_batch.ready.
func (c *LabelsCommand) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	if result == nil { // lô đã có kết quả, hay done do tra lại: không có gì để ghi
		return nil
	}
	p, ok := parseLabelsPayload(j)
	if !ok {
		return nil
	}
	var out labelsOutcome
	if err := json.Unmarshal(result, &out); err != nil || (out.Status == labelOutReady && out.FileID == uuid.Nil) {
		out = labelsOutcome{Status: labelOutFailed, ErrorCode: labels.ErrCarrierRejected}
	}
	q := store.New(tx)
	b, ok, err := pendingBatch(ctx, q, p.BatchID)
	if err != nil || !ok {
		return err
	}
	var wrote bool
	if out.Status == labelOutReady {
		wrote, err = c.ready(ctx, tx, q, b, out)
	} else {
		if out.ErrorCode == labels.ErrAuth {
			if err := MarkAuthFailed(ctx, tx, b.CarrierAccountID, out.AuthCode); err != nil {
				return err
			}
		}
		wrote, err = failBatch(ctx, q, b.ID, out.ErrorCode)
	}
	if err != nil || !wrote {
		return err
	}
	return c.s.publishLabelBatch(ctx, tx, q, *j.TenantID, b)
}

// ready ghi kết quả ready của lô b (pending) trong tx theo thứ tự khóa ADR-46: vận đơn theo id tăng dần, tệp, lô.
func (c *LabelsCommand) ready(ctx context.Context, tx pgx.Tx, q *store.Queries, b store.GetLabelBatchRow,
	out labelsOutcome,
) (bool, error) {
	if _, err := q.LockLabelShipments(ctx, b.ShipmentIds); err != nil {
		return false, fmt.Errorf("khóa vận đơn của lô nhãn: %w", err)
	}
	expires := c.s.d.Now().UTC().Add(labelRetention)
	createdBy := b.CreatedBy
	fileID, err := files.Register(ctx, tx, files.NewFile{
		ID: out.FileID, Purpose: LabelPurposeKey, Key: out.Key, SHA256: out.SHA256, Size: out.Size,
		ContentType: labelMIME, FileName: labelFileName, ExpiresAt: &expires, CreatedBy: &createdBy,
	})
	if err != nil {
		return false, fmt.Errorf("ghi tệp nhãn: %w", err)
	}
	fid := pgtype.UUID{Bytes: fileID, Valid: true}
	if err := q.SetShipmentsLabel(ctx, store.SetShipmentsLabelParams{FileID: fid, Ids: b.ShipmentIds}); err != nil {
		return false, fmt.Errorf("gắn nhãn cho vận đơn: %w", err)
	}
	n, err := q.ReadyLabelBatch(ctx, store.ReadyLabelBatchParams{FileID: fid, ID: b.ID})
	if err != nil {
		return false, fmt.Errorf("ghi lô nhãn ready: %w", err)
	}
	return n == 1, nil
}

// pendingBatch đọc lô id; ok = false khi không có lô hay lô đã có kết quả (không còn pending).
func pendingBatch(ctx context.Context, q *store.Queries, id uuid.UUID) (store.GetLabelBatchRow, bool, error) {
	b, err := q.GetLabelBatch(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, false, nil
	}
	if err != nil {
		return b, false, fmt.Errorf("đọc lô nhãn: %w", err)
	}
	return b, b.Status == batchPending, nil
}

// failBatch đổi lô pending sang failed kèm code; false khi lô không còn pending.
func failBatch(ctx context.Context, q *store.Queries, id uuid.UUID, code string) (bool, error) {
	n, err := q.FailLabelBatch(ctx, store.FailLabelBatchParams{ErrorCode: pgtype.Text{String: code, Valid: true}, ID: id})
	if err != nil {
		return false, fmt.Errorf("ghi lô nhãn failed: %w", err)
	}
	return n == 1, nil
}

// OnTerminal (queue.TerminalHook): lệnh dead hay expired khi lô còn pending (hãng không trả lời tới hết lượt, payload
// hỏng không có lô) → failed timeout. in_doubt không đổi gì: Looker trả not_sent nên lệnh được chạy lại.
func (c *LabelsCommand) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	if status != queue.StatusDead && status != queue.StatusExpired {
		return nil
	}
	p, ok := parseLabelsPayload(j)
	if !ok {
		return nil
	}
	q := store.New(tx)
	b, ok, err := pendingBatch(ctx, q, p.BatchID)
	if err != nil || !ok {
		return err
	}
	wrote, err := failBatch(ctx, q, b.ID, labels.ErrTimeout)
	if err != nil || !wrote {
		return err
	}
	return c.s.publishLabelBatch(ctx, tx, q, *j.TenantID, b)
}

// publishLabelBatch báo hiệu label_batch.ready (lô có kết quả, ready hay failed) cho người có fulfillment.print ở
// từng kho của các đơn trong lô; Bus nil → bỏ qua.
func (s *Service) publishLabelBatch(ctx context.Context, tx pgx.Tx, q *store.Queries, tenant uuid.UUID,
	b store.GetLabelBatchRow,
) error {
	if s.d.Bus == nil {
		return nil
	}
	whs, err := q.LabelBatchWarehouses(ctx, b.ShipmentIds)
	if err != nil {
		return fmt.Errorf("đọc kho của lô nhãn: %w", err)
	}
	for _, wh := range whs {
		if err := s.d.Bus.Publish(ctx, tx, realtime.Signal{
			TenantID: tenant, Topic: signalLabelBatch, Requires: permPrint, Scope: realtime.WarehouseScope(wh.String()),
			Entity: realtime.Entity{Type: "label_batch", ID: b.ID.String()},
		}); err != nil {
			return fmt.Errorf("báo hiệu %s: %w", signalLabelBatch, err)
		}
	}
	return nil
}
