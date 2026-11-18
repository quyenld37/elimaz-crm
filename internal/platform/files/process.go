package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/imager"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// MaxPixels là số điểm ảnh tối đa của một ảnh (Q12 của kế hoạch M1: 40 triệu); imager từ chối trước khi giải mã.
const MaxPixels int64 = 40_000_000

const (
	// sourceTTL, outputTTL: hạn của đường dẫn ký sẵn đưa imager (nguồn 5 phút, PUT biến thể 15 phút).
	sourceTTL = 5 * time.Minute
	outputTTL = 15 * time.Minute
	// timeoutRetry: imager quá giờ → chạy lại sau 30 s (không dồn tải lên imager đang chậm).
	timeoutRetry = 30 * time.Second
	// maxManifest: bản kê của một bộ kích thước (12 biến thể) nhỏ hơn nhiều.
	maxManifest  = 64 << 10
	manifestName = "manifest.json"
	// signalTopic: báo hiệu tệp đổi trạng thái cho màn hình (Requires files.upload).
	signalTopic = "files.file"
)

// Lý do ghi vào reject_reason (nội dung ảnh) hay last_error của lệnh (lỗi của chính lệnh). Không chứa dữ liệu
// người dùng.
const (
	reasonProcessingFailed = "processing_failed" // hết lượt thử (lỗi tạm mãi) hoặc lệnh hết hạn
	reasonSourceMissing    = "source_missing"    // ảnh thô không còn trên kho
	reasonPurposePrivate   = "purpose_private"   // file.process nhận tệp của mục đích private (R10)
	reasonPurposeMode      = "purpose_mode"      // file.process_private nhận tệp của mục đích standard
	reasonNotFound         = "not_found"         // tệp không có trong tenant của lệnh
	reasonBadPayload       = "bad_payload"
	reasonUnknownPurpose   = "unknown_purpose"
	reasonNotImagePurpose  = "not_image_purpose"
	reasonNotProcessable   = "not_processable" // tệp đã xóa, chưa tải xong
)

// Imager là imager.Client (cắm bản giả trong test).
type Imager interface {
	Process(ctx context.Context, req imager.Request) (imager.Result, error)
}

// Publisher phát báo hiệu trong transaction (realtime.PGHub hiện thực).
type Publisher interface {
	Publish(ctx context.Context, tx pgx.Tx, s realtime.Signal) error
}

// ProcessorOptions là phụ thuộc của hai lệnh xử lý ảnh.
type ProcessorOptions struct {
	DB *db.DB
	// Store là kho tệp của worker (Open, Head, CopyIfAbsent, Put).
	Store FileStore
	// Signer ký đường dẫn đưa imager, với địa chỉ kho mà imager tới được (S3_IMAGER_ENDPOINT); nil → Store.
	Signer FileStore
	Imager Imager
	// PresetVersion là IMAGE_PRESET_VERSION; 0 → 1.
	PresetVersion int
	// Bus nil: không phát báo hiệu.
	Bus Publisher
	// Metrics nil: không ghi chỉ số (image_process_ms, image_rejected).
	Metrics *metrics.Registry
}

// Processor là lệnh file.process (ảnh công khai) hoặc file.process_private (ảnh riêng tư của khách, R10): commands.Handler
// có Looker, DoneHook, queue.TerminalHook. Execute chạy ngoài transaction: đọc tệp, băm, tìm trùng, gọi imager; OnDone
// ghi kết quả trong transaction của Runner.
type Processor struct {
	o       ProcessorOptions
	private bool
	timeout time.Duration // 0: hạn của loại (30 s); test đổi được
}

// NewProcessHandler trả lệnh file.process.
func NewProcessHandler(o ProcessorOptions) *Processor { return newProcessor(o, false) }

// NewProcessPrivateHandler trả lệnh file.process_private (R10).
func NewProcessPrivateHandler(o ProcessorOptions) *Processor { return newProcessor(o, true) }

func newProcessor(o ProcessorOptions, private bool) *Processor {
	if o.Signer == nil {
		o.Signer = o.Store
	}
	if o.PresetVersion == 0 {
		o.PresetVersion = 1
	}
	return &Processor{o: o, private: private}
}

// Kind implements commands.Handler.
func (p *Processor) Kind() queue.Kind {
	k := ProcessKind()
	if p.private {
		k = ProcessPrivateKind()
	}
	if p.timeout > 0 {
		k.Policy.Timeout = p.timeout
	}
	return k
}

// AutoRetryNotSent implements commands.Handler: chạy lại an toàn (khóa đầu ra cố định, không ghi đè).
func (*Processor) AutoRetryNotSent() bool { return true }

// Lookup implements commands.Looker: luôn not_sent. Execute chạy lại là an toàn và rẻ (bỏ qua biến thể đã có, đọc bản
// kê thì không gọi imager), còn done do tra lại thì DoneHook nhận result nil, không đủ để ghi tệp ready (Q3).
func (*Processor) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// processResult là Result của lệnh done và cũng là nội dung bản kê manifest.json cạnh biến thể.
type processResult struct {
	Reuse        string          `json:"reuse,omitempty"` // mã tệp trùng nội dung đã ready (chép biến thể của nó)
	SHA256       string          `json:"sha256,omitempty"`
	DetectedType string          `json:"detectedType,omitempty"`
	Width        int             `json:"width,omitempty"`
	Height       int             `json:"height,omitempty"`
	Preset       int             `json:"preset,omitempty"`
	Variants     []variantResult `json:"variants,omitempty"`
	Skipped      string          `json:"skipped,omitempty"` // tệp đã ready: không làm gì
}

type variantResult struct {
	Width  int    `json:"width"`
	Format string `json:"format"`
	Key    string `json:"key"`
	Size   int64  `json:"size"`
}

// job là phần của một lệnh mà Execute cần.
type job struct {
	tenant, fileID uuid.UUID
	row            store.PlatformFile
	purpose        Purpose
}

// Execute implements commands.Handler (ngoài transaction, ctx hết hạn sau 30 s).
func (p *Processor) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	jb, out, ok := p.load(ctx, j)
	if !ok {
		return out, nil
	}
	srcB, srcK := Bucket(jb.row.Bucket), jb.row.Key
	if p.private {
		srcB, srcK = BucketTmp, TmpKey(jb.tenant, jb.fileID)
	}
	sum, err := p.hash(ctx, srcB, srcK, jb.purpose.MaxBytes)
	switch {
	case errors.Is(err, ErrObjectNotFound):
		return p.reject(ctx, jb, reasonSourceMissing)
	case errors.Is(err, ErrTooLarge):
		return p.reject(ctx, jb, imager.ReasonTooLarge)
	case err != nil:
		return notSent("store", 0), nil
	}
	dup, err := p.findDuplicate(ctx, jb, sum)
	switch {
	case err != nil:
		return notSent("db", 0), nil
	case dup != uuid.Nil:
		return done(processResult{Reuse: dup.String()})
	}
	if !p.private {
		// Ảnh gốc theo mã băm (giữ cho tạo lại kích thước, TP07); ảnh riêng tư không giữ gốc (R10).
		if err := p.o.Store.CopyIfAbsent(ctx, srcB, srcK, BucketOriginals, OriginalKey(sum)); err != nil {
			return notSent("store", 0), nil
		}
		srcB, srcK = BucketOriginals, OriginalKey(sum)
	}
	return p.makeVariants(ctx, jb, sum, srcB, srcK)
}

// load đọc payload và dòng tệp (transaction đọc ngắn), kiểm mục đích; ok = false thì out là kết quả trả ngay.
func (p *Processor) load(ctx context.Context, j queue.Job) (job, commands.Outcome, bool) {
	var payload struct {
		FileID uuid.UUID `json:"fileId"`
	}
	if j.TenantID == nil || json.Unmarshal(j.Payload, &payload) != nil || payload.FileID == uuid.Nil {
		return job{}, rejected(reasonBadPayload), false
	}
	jb := job{tenant: *j.TenantID, fileID: payload.FileID}
	err := tenancy.WithTenant(ctx, p.o.DB, jb.tenant, func(tx pgx.Tx) error {
		var err error
		jb.row, err = store.New(tx).GetFile(ctx, jb.fileID)
		return err //nolint:wrapcheck // xử lý ở dưới
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return jb, rejected(reasonNotFound), false
	case err != nil:
		return jb, notSent("db", 0), false
	}
	purpose, ok := PurposeOf(jb.row.Purpose)
	switch {
	case !ok:
		return jb, rejected(reasonUnknownPurpose), false
	case purpose.Private && !p.private:
		return jb, rejected(reasonPurposePrivate), false
	case !purpose.Private && p.private:
		return jb, rejected(reasonPurposeMode), false
	case !purpose.Image:
		return jb, rejected(reasonNotImagePurpose), false
	}
	jb.purpose = purpose
	switch {
	case jb.row.DeletedAt.Valid:
		return jb, rejected(reasonNotProcessable), false
	case jb.row.Status == StatusReady:
		return jb, commands.Outcome{Status: commands.StatusDone, Result: json.RawMessage(`{"skipped":"ready"}`)}, false
	case jb.row.Status == StatusRejected:
		return jb, rejected(jb.row.RejectReason.String), false
	case !processable(jb.row.Status):
		return jb, rejected(reasonNotProcessable), false
	}
	return jb, commands.Outcome{}, true
}

func processable(status string) bool {
	return status == StatusUploaded || status == StatusProcessing || status == StatusLost
}

// hash băm nội dung (không giải mã: việc của imager, ADR-45).
func (p *Processor) hash(ctx context.Context, b Bucket, key string, maxBytes int64) (string, error) {
	rc, err := p.o.Store.Open(ctx, b, key, maxBytes)
	if err != nil {
		return "", fmt.Errorf("băm ảnh: %w", err)
	}
	defer func() { _ = rc.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "", fmt.Errorf("băm ảnh: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// layout là chỗ của các biến thể của một tệp: bucket, khóa từng biến thể, bản kê.
type layout struct {
	bucket   Bucket
	specs    []VariantSpec
	keys     []string
	manifest string
}

func (p *Processor) layoutOf(jb job, sum string) (layout, error) {
	specs, ok := Preset(p.o.PresetVersion)
	if !ok {
		return layout{}, fmt.Errorf("bộ kích thước v%d chưa có", p.o.PresetVersion)
	}
	l := layout{bucket: BucketPublic, specs: specs}
	for _, s := range specs {
		if p.private {
			l.keys = append(l.keys, PrivateVariantKey(jb.tenant, jb.fileID, p.o.PresetVersion, s.Width, s.Format))
		} else {
			l.keys = append(l.keys, VariantKey(sum, p.o.PresetVersion, s.Width, s.Format))
		}
	}
	dir := "img/" + sum + "/v" + strconv.Itoa(p.o.PresetVersion) + "/"
	if p.private {
		l.bucket = BucketPrivate
		dir = ServerKey(jb.tenant, jb.fileID) + "/v" + strconv.Itoa(p.o.PresetVersion) + "/"
	}
	l.manifest = dir + manifestName
	return l, nil
}

// makeVariants: bản kê đã có (lần trước xong) → done không gọi imager; ngược lại chỉ xin imager các biến thể chưa có
// trên kho (khóa đã có thì PUT bị 412), đủ cả mà thiếu bản kê thì xin một đầu ra thử vào crm-tmp để có kết quả kiểm.
func (p *Processor) makeVariants(ctx context.Context, jb job, sum string, srcB Bucket, srcK string) (commands.Outcome, error) {
	l, err := p.layoutOf(jb, sum)
	if err != nil {
		return commands.Outcome{}, err
	}
	if m, ok := p.readManifest(ctx, l, sum); ok {
		if reason := contentReason(m, jb.purpose); reason != "" {
			return p.reject(ctx, jb, reason)
		}
		return done(m)
	}
	have := map[int]int64{} // chỉ số biến thể → cỡ đã có trên kho
	var outs []imager.Output
	var asked []int
	for i, key := range l.keys {
		info, err := p.o.Store.Head(ctx, l.bucket, key)
		switch {
		case err == nil:
			have[i] = info.Size
			continue
		case !errors.Is(err, ErrObjectNotFound):
			return notSent("store", 0), nil
		}
		o, err := p.output(ctx, l.bucket, key, l.specs[i])
		if err != nil {
			return notSent("sign", 0), nil
		}
		outs, asked = append(outs, o), append(asked, i)
	}
	if len(outs) == 0 {
		probe := TmpKey(jb.tenant, jb.fileID) + "/probe-" + uuid.NewString() + ".jpeg"
		o, err := p.output(ctx, BucketTmp, probe, VariantSpec{Width: l.specs[0].Width, Format: "jpeg"})
		if err != nil {
			return notSent("sign", 0), nil
		}
		outs = []imager.Output{o}
	}
	src, err := p.o.Signer.PresignGet(ctx, srcB, srcK, sourceTTL, "")
	if err != nil {
		return notSent("sign", 0), nil
	}
	res, out, ok := p.callImager(ctx, imager.Request{
		SourceURL: src, MaxBytes: jb.purpose.MaxBytes, MaxPixels: MaxPixels, Allowed: jb.purpose.AllowedTypes, Outputs: outs,
	})
	if !ok {
		return out, nil
	}
	if res.Status != imager.StatusOK {
		return p.reject(ctx, jb, res.Reason)
	}
	m, ok := assemble(res, l, sum, p.o.PresetVersion, have, asked)
	if !ok {
		return notSent(imager.ReasonInternal, 0), nil
	}
	p.writeManifest(ctx, l, m)
	return done(m)
}

// output ký PUT không ký kích thước cho một biến thể.
func (p *Processor) output(ctx context.Context, b Bucket, key string, s VariantSpec) (imager.Output, error) {
	put, err := p.o.Signer.PresignPutAnySize(ctx, b, key, "image/"+s.Format, outputTTL)
	if err != nil {
		return imager.Output{}, fmt.Errorf("ký biến thể: %w", err)
	}
	return imager.Output{Width: s.Width, Format: s.Format, PutURL: put.URL, Headers: put.Headers}, nil
}

// callImager gọi imager; ok = false thì out là not_sent (lỗi tạm: bận, quá giờ, lỗi HTTP, lý do tạm của imager).
// Kết quả trả về (ok = true) là imager.StatusOK hoặc từ chối vì nội dung.
func (p *Processor) callImager(ctx context.Context, req imager.Request) (imager.Result, commands.Outcome, bool) {
	start := time.Now()
	res, err := p.o.Imager.Process(ctx, req)
	if be, isBusy := errors.AsType[*imager.BusyError](err); isBusy {
		return res, notSent("busy", be.RetryAfter), false
	}
	if err != nil {
		if ctx.Err() != nil {
			return res, notSent(imager.ReasonTimeout, timeoutRetry), false
		}
		return res, notSent("imager_unreachable", 0), false
	}
	label := "rejected"
	if res.Status == imager.StatusOK {
		label = "ok"
	}
	p.observe("image_process_ms", float64(time.Since(start).Milliseconds()), label)
	switch {
	case res.Status == imager.StatusOK:
		return res, commands.Outcome{}, true
	case res.Status != imager.StatusRejected:
		return res, notSent(imager.ReasonInternal, 0), false
	case res.Reason == imager.ReasonTimeout:
		return res, notSent(imager.ReasonTimeout, timeoutRetry), false
	case transient(res.Reason):
		return res, notSent(res.Reason, 0), false
	case !contentReject(res.Reason):
		res.Reason = reasonProcessingFailed // lý do lạ: không tin chuỗi từ bên ngoài
	}
	return res, commands.Outcome{}, true
}

// transient: lý do tạm của imager (không do nội dung ảnh; Task 42, việc (1)).
func transient(reason string) bool {
	switch reason {
	case imager.ReasonTimeout, imager.ReasonUploadFailed, imager.ReasonSourceFailed, imager.ReasonInternal:
		return true
	}
	return false
}

// contentReject: lý do từ chối vì nội dung ảnh (vĩnh viễn).
func contentReject(reason string) bool {
	switch reason {
	case imager.ReasonNotImage, imager.ReasonTypeNotAllowed, imager.ReasonTooLarge, imager.ReasonTooManyPixels,
		imager.ReasonDecodeFailed:
		return true
	}
	return false
}

// assemble dựng kết quả đủ bộ từ biến thể đã có (have: cỡ trên kho) và kết quả imager cho các biến thể đã xin
// (asked, theo thứ tự yêu cầu). Kết quả imager thiếu, lạ → ok = false.
func assemble(res imager.Result, l layout, sum string, preset int, have map[int]int64, asked []int) (processResult, bool) {
	if res.Width <= 0 || res.Height <= 0 || res.DetectedType == "" || (len(asked) > 0 && len(res.Outputs) != len(asked)) {
		return processResult{}, false
	}
	sizes := maps.Clone(have)
	if sizes == nil {
		sizes = map[int]int64{}
	}
	for n, i := range asked {
		o := res.Outputs[n]
		if o.Width != l.specs[i].Width || o.Format != l.specs[i].Format || o.Bytes <= 0 {
			return processResult{}, false
		}
		sizes[i] = o.Bytes
	}
	m := processResult{SHA256: sum, DetectedType: res.DetectedType, Width: res.Width, Height: res.Height, Preset: preset}
	for i, s := range l.specs {
		m.Variants = append(m.Variants, variantResult{Width: s.Width, Format: s.Format, Key: l.keys[i], Size: sizes[i]})
	}
	return m, true
}

// readManifest đọc bản kê của lần xử lý trước; thiếu, hỏng, khác bộ kích thước, khác khóa → ok = false (làm lại).
func (p *Processor) readManifest(ctx context.Context, l layout, sum string) (processResult, bool) {
	rc, err := p.o.Store.Open(ctx, l.bucket, l.manifest, maxManifest)
	if err != nil {
		return processResult{}, false
	}
	defer func() { _ = rc.Close() }()
	var m processResult
	if err := json.NewDecoder(rc).Decode(&m); err != nil || m.SHA256 != sum || m.Preset != p.o.PresetVersion ||
		m.Width <= 0 || m.Height <= 0 || len(m.Variants) != len(l.keys) {
		return processResult{}, false
	}
	for i, v := range m.Variants {
		if v.Key != l.keys[i] || v.Width != l.specs[i].Width || v.Format != l.specs[i].Format || v.Size <= 0 {
			return processResult{}, false
		}
	}
	return m, true
}

// contentReason kiểm lại kết quả kiểm của bản kê theo mục đích của tệp này (bản kê công khai dùng chung giữa mục
// đích và tenant: mục đích khác có thể cho loại khác, Q7).
func contentReason(m processResult, purpose Purpose) string {
	switch {
	case !purpose.allows(m.DetectedType):
		return imager.ReasonTypeNotAllowed
	case int64(m.Width)*int64(m.Height) > MaxPixels:
		return imager.ReasonTooManyPixels
	}
	return ""
}

// writeManifest ghi bản kê (không ghi đè). Lỗi được bỏ qua: bản kê chỉ để lần sau khỏi gọi imager, thiếu nó thì lần
// sau làm lại bằng đầu ra thử.
func (p *Processor) writeManifest(ctx context.Context, l layout, m processResult) {
	body, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = p.o.Store.Put(ctx, l.bucket, l.manifest, bytes.NewReader(body), int64(len(body)), "application/json")
}

// reject ghi tệp rejected với lý do nội dung (transaction ngắn, chỉ khi tệp còn chờ xử lý) rồi trả rejected; lệnh
// thành dead và OnTerminal không đổi gì thêm (Q5). Ghi lỗi → not_sent (chạy lại ra đúng kết quả này).
func (p *Processor) reject(ctx context.Context, jb job, reason string) (commands.Outcome, error) {
	err := tenancy.WithTenant(ctx, p.o.DB, jb.tenant, func(tx pgx.Tx) error {
		return p.markRejected(ctx, tx, jb.tenant, jb.fileID, reason)
	})
	if err != nil {
		return notSent("db", 0), nil
	}
	return rejected(reason), nil
}

// markRejected khóa dòng tệp, đổi sang rejected khi còn chờ xử lý, phát báo hiệu, đếm image_rejected.
func (p *Processor) markRejected(ctx context.Context, tx pgx.Tx, tenant, fileID uuid.UUID, reason string) error {
	s := store.New(tx)
	if _, err := s.LockFile(ctx, fileID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("khóa tệp %s: %w", fileID, err)
	}
	n, err := s.SetFileRejected(ctx, store.SetFileRejectedParams{ID: fileID, RejectReason: text(reason)})
	if err != nil {
		return fmt.Errorf("từ chối tệp %s: %w", fileID, err)
	}
	if n == 0 {
		return nil
	}
	p.count("image_rejected", reason)
	return p.publish(ctx, tx, tenant, fileID)
}

// OnDone implements commands.DoneHook: khóa tệp (ADR-46 nhóm 12b), ghi ready, chèn biến thể, phát báo hiệu. Tệp đã
// đổi trạng thái (lệnh khác đã xong, đã xóa) → không làm gì.
func (p *Processor) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	var payload struct {
		FileID uuid.UUID `json:"fileId"`
	}
	var res processResult
	if j.TenantID == nil || len(result) == 0 || json.Unmarshal(j.Payload, &payload) != nil || json.Unmarshal(result, &res) != nil {
		return nil
	}
	if res.Skipped != "" {
		return nil
	}
	s := store.New(tx)
	row, err := s.LockFile(ctx, payload.FileID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("khóa tệp %s: %w", payload.FileID, err)
	case row.DeletedAt.Valid || !processable(row.Status):
		return nil
	}
	if res.Reuse != "" {
		if res, err = reuseResult(ctx, s, res.Reuse); err != nil {
			return err
		}
	}
	return p.markReady(ctx, tx, row, res)
}

func (p *Processor) markReady(ctx context.Context, tx pgx.Tx, row store.PlatformFile, res processResult) error {
	sum, err := hex.DecodeString(res.SHA256)
	if err != nil || len(sum) != sha256.Size {
		return fmt.Errorf("ghi tệp %s: mã băm sai", row.ID)
	}
	outBucket, bucket, key := BucketPublic, row.Bucket, row.Key
	if p.private {
		outBucket, bucket, key = BucketPrivate, string(BucketPrivate), ServerKey(row.TenantID, row.ID)
	}
	s := store.New(tx)
	if _, err := s.SetFileReady(ctx, store.SetFileReadyParams{
		ID: row.ID, DetectedType: text(res.DetectedType), Width: int4(res.Width), Height: int4(res.Height),
		ContentSha256: sum, PresetVersion: int4(res.Preset), Bucket: bucket, Key: key,
	}); err != nil {
		return fmt.Errorf("ghi tệp %s ready: %w", row.ID, err)
	}
	for _, v := range res.Variants {
		if err := s.InsertFileVariant(ctx, store.InsertFileVariantParams{
			TenantID: row.TenantID, FileID: row.ID, PresetVersion: int32(res.Preset), Width: int32(v.Width), //nolint:gosec // G115: preset, chiều rộng nhỏ
			Format: v.Format, Bucket: string(outBucket), Key: v.Key, SizeBytes: v.Size,
		}); err != nil {
			return fmt.Errorf("ghi biến thể của %s: %w", row.ID, err)
		}
	}
	return p.publish(ctx, tx, row.TenantID, row.ID)
}

// OnTerminal implements queue.TerminalHook: dead (hết lượt, hay rejected đã ghi lý do ở Execute) hoặc expired → tệp
// còn chờ thì rejected processing_failed. in_doubt: giữ nguyên (tra lại sẽ chạy tiếp). Tệp của chế độ khác (lệnh
// xếp nhầm) không bị đụng tới.
func (p *Processor) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	if status != queue.StatusDead && status != queue.StatusExpired {
		return nil
	}
	var payload struct {
		FileID uuid.UUID `json:"fileId"`
	}
	if j.TenantID == nil || json.Unmarshal(j.Payload, &payload) != nil {
		return nil
	}
	row, err := store.New(tx).GetFile(ctx, payload.FileID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("đọc tệp %s: %w", payload.FileID, err)
	}
	if purpose, ok := PurposeOf(row.Purpose); !ok || purpose.Private != p.private || !purpose.Image {
		return nil
	}
	return p.markRejected(ctx, tx, *j.TenantID, payload.FileID, reasonProcessingFailed)
}

func (p *Processor) publish(ctx context.Context, tx pgx.Tx, tenant, fileID uuid.UUID) error {
	if p.o.Bus == nil {
		return nil
	}
	if err := p.o.Bus.Publish(ctx, tx, realtime.Signal{
		TenantID: tenant, Topic: signalTopic, Requires: identity.Permission("files.upload"),
		Entity: realtime.Entity{Type: "file", ID: fileID.String()},
	}); err != nil {
		return fmt.Errorf("phát báo hiệu tệp %s: %w", fileID, err)
	}
	return nil
}

func (p *Processor) observe(name string, v float64, label string) {
	if p.o.Metrics != nil {
		p.o.Metrics.Observe(name, v, label)
	}
}

func (p *Processor) count(name, label string) {
	if p.o.Metrics != nil {
		p.o.Metrics.Inc(name, label, 1)
	}
}

func notSent(reason string, after time.Duration) commands.Outcome {
	return commands.Outcome{Status: commands.StatusNotSent, Reason: reason, RetryAfter: after}
}

func rejected(reason string) commands.Outcome {
	return commands.Outcome{Status: commands.StatusRejected, Reason: reason}
}

func done(r processResult) (commands.Outcome, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả xử lý ảnh: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: body}, nil
}

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func int4(n int) pgtype.Int4 {
	return pgtype.Int4{Int32: int32(n), Valid: n > 0} //nolint:gosec // G115: kích thước ảnh, số bộ kích thước nhỏ
}
