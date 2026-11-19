package files

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Trạng thái tệp (05 mục 7.3; Q15 thêm awaiting_upload).
const (
	StatusAwaitingUpload = "awaiting_upload"
	StatusUploaded       = "uploaded"
	StatusProcessing     = "processing"
	StatusReady          = "ready"
	StatusRejected       = "rejected"
	StatusLost           = "lost"
)

// UploadTTL là hạn của đường dẫn PUT ký sẵn.
const UploadTTL = 15 * time.Minute

// maxNameLen khớp CHECK length(original_name) <= 255 (ký tự).
const maxNameLen = 255

// Lỗi của đường tải lên và đọc tệp.
var (
	// ErrTypeNotAllowed: loại tệp không nằm trong AllowedTypes của mục đích.
	ErrTypeNotAllowed = errors.New("files: loại tệp không được phép")
	// ErrInvalidFile: tham số sai (kích thước ≤ 0, tên quá 255 ký tự, khóa không đúng tenant, mã băm sai độ dài).
	ErrInvalidFile = errors.New("files: thông tin tệp không hợp lệ")
	// ErrUploadMissing: complete khi kho chưa có tệp.
	ErrUploadMissing = errors.New("files: chưa có tệp trên kho")
	// ErrNotFound: tệp không có, của tenant khác, đã xóa hay đã mất.
	ErrNotFound = errors.New("files: không có tệp")
	// ErrExpired: tệp tạm do máy chủ ghi đã quá expires_at.
	ErrExpired = errors.New("files: tệp đã hết hạn")
	// ErrNotReady: tệp chưa xử lý xong (awaiting_upload, uploaded, processing).
	ErrNotReady = errors.New("files: tệp chưa sẵn sàng")
	// ErrRejected: tệp đã bị từ chối khi kiểm.
	ErrRejected = errors.New("files: tệp đã bị từ chối")
)

// ProcessKind là lệnh xử lý ảnh công khai file.process (Task M1-43 viết handler): hạn 30 s một lần, 3 lượt cho lần
// chạy có tính lượt (in_doubt rồi chạy lại); không hạn sống — lỗi hạ tầng tạm không tính lượt và handler giữ trần thời
// gian ProcessorOptions.RetryMax (Task 409).
func ProcessKind() queue.Kind {
	k := commands.CommandKind("file.process", 0, 30*time.Second)
	k.Policy.MaxAttempts = 3
	return k
}

// ProcessPrivateKind là lệnh xử lý ảnh riêng tư file.process_private (R10, Task M1-43 viết handler), cùng chính sách
// với ProcessKind.
func ProcessPrivateKind() queue.Kind {
	k := commands.CommandKind("file.process_private", 0, 30*time.Second)
	k.Policy.MaxAttempts = 3
	return k
}

// File là một dòng platform.files.
type File struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	Purpose      string
	Bucket       Bucket
	Key          string
	OriginalName string
	DeclaredType string
	SizeBytes    int64
	Status       string
	RejectReason string
	Width        int // 0 khi chưa biết
	Height       int
	ExpiresAt    *time.Time
	Deleted      bool
	CreatedAt    time.Time
}

func fileOf(r store.PlatformFile) File {
	f := File{
		ID: r.ID, TenantID: r.TenantID, Purpose: r.Purpose, Bucket: Bucket(r.Bucket), Key: r.Key,
		OriginalName: r.OriginalName, DeclaredType: r.DeclaredType, SizeBytes: r.SizeBytes, Status: r.Status,
		RejectReason: r.RejectReason.String, Width: int(r.Width.Int32), Height: int(r.Height.Int32),
		Deleted: r.DeletedAt.Valid, CreatedAt: r.CreatedAt,
	}
	if r.ExpiresAt.Valid {
		t := r.ExpiresAt.Time
		f.ExpiresAt = &t
	}
	return f
}

// UploadInput là yêu cầu tải lên của nhân viên.
type UploadInput struct {
	TenantID    uuid.UUID
	Purpose     string
	FileName    string
	ContentType string
	SizeBytes   int64
}

// checkLimits kiểm loại và dung lượng theo mục đích.
func checkLimits(p Purpose, contentType string, size int64) error {
	switch {
	case size <= 0:
		return fmt.Errorf("%w: kích thước %d", ErrInvalidFile, size)
	case size > p.MaxBytes:
		return fmt.Errorf("%s: %d byte > %d: %w", p.Key, size, p.MaxBytes, ErrTooLarge)
	case !p.allows(contentType):
		return fmt.Errorf("%s: %w", p.Key, ErrTypeNotAllowed)
	}
	return nil
}

func checkName(name string) error {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxNameLen {
		return fmt.Errorf("%w: tên tệp quá %d ký tự hoặc không phải UTF-8", ErrInvalidFile, maxNameLen)
	}
	return nil
}

// CreateUpload kiểm giới hạn của mục đích, chèn dòng awaiting_upload (bucket tmp, khóa TmpKey) và ký PUT hạn
// UploadTTL có content-length, content-type, If-None-Match. tx là tenancy.WithTenant của in.TenantID. Ký là tính
// tại chỗ, không gọi mạng, nên chạy lại được trong RetryTx. Mục đích chưa đăng ký → ErrUnknownPurpose; không có
// Upload → ErrUploadNotAllowed; quá lớn → ErrTooLarge; loại lạ → ErrTypeNotAllowed.
func CreateUpload(ctx context.Context, tx pgx.Tx, fs FileStore, in UploadInput, actor uuid.UUID) (File, PresignedPut, error) {
	p, ok := PurposeOf(in.Purpose)
	switch {
	case !ok:
		return File{}, PresignedPut{}, fmt.Errorf("tải lên: %w", ErrUnknownPurpose)
	case !p.Upload:
		return File{}, PresignedPut{}, fmt.Errorf("tải lên %s: %w", p.Key, ErrUploadNotAllowed)
	}
	if err := checkLimits(p, in.ContentType, in.SizeBytes); err != nil {
		return File{}, PresignedPut{}, fmt.Errorf("tải lên: %w", err)
	}
	if err := checkName(in.FileName); err != nil {
		return File{}, PresignedPut{}, fmt.Errorf("tải lên: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return File{}, PresignedPut{}, fmt.Errorf("tải lên: sinh mã tệp: %w", err)
	}
	row, err := store.New(tx).InsertUploadFile(ctx, store.InsertUploadFileParams{
		ID: id, TenantID: in.TenantID, Purpose: p.Key, Key: TmpKey(in.TenantID, id), OriginalName: in.FileName,
		DeclaredType: in.ContentType, SizeBytes: in.SizeBytes, CreatedBy: optUUID(actor),
	})
	if err != nil {
		return File{}, PresignedPut{}, fmt.Errorf("tải lên: ghi tệp: %w", err)
	}
	put, err := fs.PresignPut(ctx, BucketTmp, row.Key, in.SizeBytes, in.ContentType, UploadTTL)
	if err != nil {
		return File{}, PresignedPut{}, fmt.Errorf("tải lên: %w", err)
	}
	return fileOf(row), put, nil
}

func optUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }

// UploadCheck là kết quả kiểm kho của InspectUpload, dùng một lần cho ApplyUpload trong transaction.
type UploadCheck struct {
	tenantID, fileID uuid.UUID
	inspected        bool // dòng còn awaiting_upload lúc kiểm nên đã hỏi kho
	found            bool
	info             ObjectInfo
}

// InspectUpload đọc dòng tệp (transaction đọc ngắn) và, khi tệp còn chờ tải, hỏi kho (Head) **ngoài** transaction
// (ADR-46). Tệp không có hay của tenant khác → ErrNotFound.
func InspectUpload(ctx context.Context, d *db.DB, fs FileStore, tenantID, fileID uuid.UUID) (UploadCheck, error) {
	var row store.PlatformFile
	if err := tenancy.WithTenant(ctx, d, tenantID, func(tx pgx.Tx) error {
		var err error
		row, err = store.New(tx).GetFile(ctx, fileID)
		return err //nolint:wrapcheck // bọc ở dưới
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UploadCheck{}, fmt.Errorf("complete %s: %w", fileID, ErrNotFound)
		}
		return UploadCheck{}, fmt.Errorf("complete %s: đọc tệp: %w", fileID, err)
	}
	c := UploadCheck{tenantID: tenantID, fileID: fileID}
	if row.DeletedAt.Valid || row.Status != StatusAwaitingUpload {
		return c, nil
	}
	info, err := fs.Head(ctx, Bucket(row.Bucket), row.Key)
	switch {
	case errors.Is(err, ErrObjectNotFound):
	case err != nil:
		return UploadCheck{}, fmt.Errorf("complete %s: %w", fileID, err)
	default:
		c.found, c.info = true, info
	}
	c.inspected = true
	return c, nil
}

// ApplyUpload ghi kết quả của InspectUpload trong tx (tenancy.WithTenant của cùng tenant): khóa dòng tệp; chỉ đổi khi
// còn awaiting_upload. Kho chưa có → ErrUploadMissing (không đổi gì); khác kích thước hay loại đã khai → rejected
// size_mismatch / type_mismatch; đúng → uploaded, ảnh thì xếp file.process (mục đích private: file.process_private),
// DedupeKey = mã tệp. Tệp đã qua bước này → trả trạng thái hiện tại, không xếp thêm. Chạy lại được trong RetryTx.
func ApplyUpload(ctx context.Context, tx pgx.Tx, q queue.JobQueue, c UploadCheck) (File, error) {
	s := store.New(tx)
	row, err := s.LockFile(ctx, c.fileID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return File{}, fmt.Errorf("complete %s: %w", c.fileID, ErrNotFound)
	case err != nil:
		return File{}, fmt.Errorf("complete %s: khóa tệp: %w", c.fileID, err)
	case row.DeletedAt.Valid, row.TenantID != c.tenantID:
		return File{}, fmt.Errorf("complete %s: %w", c.fileID, ErrNotFound)
	case row.Status != StatusAwaitingUpload:
		return fileOf(row), nil
	case !c.inspected:
		// Lúc kiểm tệp chưa ở trạng thái chờ mà giờ lại chờ: không thể với luồng hiện có; đòi kiểm lại.
		return File{}, fmt.Errorf("complete %s: %w", c.fileID, ErrUploadMissing)
	case !c.found:
		return File{}, fmt.Errorf("complete %s: %w", c.fileID, ErrUploadMissing)
	}
	params := store.SetUploadResultParams{ID: c.fileID, Status: StatusUploaded}
	switch {
	case c.info.Size != row.SizeBytes:
		params.Status, params.RejectReason = StatusRejected, pgtype.Text{String: "size_mismatch", Valid: true}
	case c.info.ContentType != row.DeclaredType:
		params.Status, params.RejectReason = StatusRejected, pgtype.Text{String: "type_mismatch", Valid: true}
	}
	updated, err := s.SetUploadResult(ctx, params)
	if err != nil {
		return File{}, fmt.Errorf("complete %s: ghi trạng thái: %w", c.fileID, err)
	}
	if params.Status == StatusUploaded {
		if err := enqueueProcess(ctx, tx, q, row); err != nil {
			return File{}, err
		}
	}
	return fileOf(updated), nil
}

// enqueueProcess xếp lệnh xử lý ảnh cho tệp vừa uploaded (không phải ảnh → không làm gì). Mục đích đã bỏ khỏi sổ
// (dòng cũ) → ErrUnknownPurpose: không đoán cách xử lý.
func enqueueProcess(ctx context.Context, tx pgx.Tx, q queue.JobQueue, row store.PlatformFile) error {
	p, ok := PurposeOf(row.Purpose)
	if !ok {
		return fmt.Errorf("complete %s: %w", row.ID, ErrUnknownPurpose)
	}
	if !p.Image {
		return nil
	}
	kind := ProcessKind()
	if p.Private {
		kind = ProcessPrivateKind()
	}
	tenant := row.TenantID
	if _, _, err := q.Enqueue(ctx, tx, kind, queue.NewJob{
		TenantID: &tenant, Payload: map[string]string{"fileId": row.ID.String()}, DedupeKey: row.ID.String(),
	}); err != nil {
		return fmt.Errorf("complete %s: xếp %s: %w", row.ID, kind.Name, err)
	}
	return nil
}

// CompleteUpload = InspectUpload (Head ngoài transaction) rồi ApplyUpload trong tenancy.WithTenant. Đường HTTP dùng
// hai bước riêng để ghi kết quả cùng khóa chống trùng (httpapi.WriteTx).
func CompleteUpload(ctx context.Context, d *db.DB, fs FileStore, q queue.JobQueue, tenantID, fileID uuid.UUID) (File, error) {
	c, err := InspectUpload(ctx, d, fs, tenantID, fileID)
	if err != nil {
		return File{}, err
	}
	var f File
	err = tenancy.WithTenant(ctx, d, tenantID, func(tx pgx.Tx) error {
		var err error
		f, err = ApplyUpload(ctx, tx, q, c)
		return err
	})
	if err != nil {
		return File{}, err //nolint:wrapcheck // ApplyUpload đã bọc; lỗi của RetryTx trả nguyên
	}
	return f, nil
}
