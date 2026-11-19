package customer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// ImportPurposeKey là mục đích tệp của tệp Excel nhập khách (dòng của platform.file_purposes, migration 0208).
const ImportPurposeKey = "customer_import"

// Giới hạn của một lần nhập khách (kế hoạch M2, Task M2-13).
const (
	// MaxImportBytes là dung lượng tối đa của tệp nhập (5 MiB).
	MaxImportBytes int64 = 5 << 20
	// MaxImportRows là số dòng dữ liệu tối đa của tệp nhập.
	MaxImportRows = 20_000
	// MaxImportErrors là số lỗi dòng lưu tối đa của một lần nhập (error_rows vẫn đếm đủ).
	MaxImportErrors = 1000
	// ImportBatchSize là số dòng của một lô (một transaction).
	ImportBatchSize = 500
)

// Trạng thái của lần nhập (CHECK của customer.customer_imports.status).
const (
	ImportPending = "pending"
	ImportRunning = "running"
	ImportDone    = "done"
	ImportFailed  = "failed"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// Tiêu đề cột của mẫu tệp nhập.
const (
	colName     = "Họ tên*"
	colPhone    = "SĐT*"
	colBirthday = "Ngày sinh (dd/mm/yyyy)"
	colProvince = "Tỉnh/thành"
	colWard     = "Xã/phường"
	colStreet   = "Số nhà, đường"
	colTags     = "Nhãn (cách nhau bằng ;)"
	colNote     = "Ghi chú"
)

// Mã lỗi cả tệp (ImportError.Row = 0).
const (
	codeImportTooLarge   = "import_too_large"
	codeImportFileBroken = "import_file_invalid"
	codeImportAbandoned  = "import_failed"
)

// ImportHeader là dòng tiêu đề của mẫu tệp nhập, theo thứ tự cột.
func ImportHeader() []string {
	return []string{colName, colPhone, colBirthday, colProvince, colWard, colStreet, colTags, colNote}
}

// ImportPurpose là mục đích tệp customer_import: bucket private, chỉ tệp .xlsx, tối đa 5 MiB, nhân viên tải lên được.
func ImportPurpose() files.Purpose {
	return files.Purpose{
		Key: ImportPurposeKey, Bucket: files.BucketPrivate, AllowedTypes: []string{xlsxContentType},
		MaxBytes: MaxImportBytes, Upload: true,
	}
}

// ImportTemplate trả mẫu tệp nhập khách (.xlsx): một trang, dòng tiêu đề ImportHeader, không dòng dữ liệu.
func ImportTemplate() ([]byte, error) {
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, []xlsx.Sheet{{Name: "Khách", Header: ImportHeader()}}); err != nil {
		return nil, fmt.Errorf("ghi mẫu nhập khách: %w", err)
	}
	return buf.Bytes(), nil
}

// ImportError là lỗi của một dòng (Row là số dòng Excel, tiêu đề là dòng 1; 0: cả tệp): tiêu đề cột và mã, không bao
// giờ kèm giá trị ô.
type ImportError struct {
	Row    int    `json:"row"`
	Column string `json:"column,omitempty"`
	Code   string `json:"code"`
}

// Import là một lần nhập khách. Số đếm là 0 khi lần nhập chưa chạy; FinishedAt khác nil khi done hay failed.
type Import struct {
	ID, FileID, CreatedBy                     uuid.UUID
	Status                                    string
	TotalRows, OKRows, SkippedRows, ErrorRows int
	Errors                                    []ImportError
	CreatedAt                                 time.Time
	FinishedAt                                *time.Time
}

func importOf(r store.CustomerCustomerImport) (Import, error) {
	out := Import{
		ID: r.ID, FileID: r.FileID, CreatedBy: r.CreatedBy, Status: r.Status, TotalRows: int(r.TotalRows.Int32),
		OKRows: int(r.OkRows.Int32), SkippedRows: int(r.SkippedRows.Int32), ErrorRows: int(r.ErrorRows.Int32),
		Errors: []ImportError{}, CreatedAt: r.CreatedAt,
	}
	if r.FinishedAt.Valid {
		t := r.FinishedAt.Time
		out.FinishedAt = &t
	}
	if err := json.Unmarshal(r.Errors, &out.Errors); err != nil {
		return Import{}, fmt.Errorf("đọc lỗi của lần nhập: %w", err)
	}
	return out, nil
}

// processed là số dòng lần nhập đã xử lý (đã commit).
func (imp Import) processed() int { return imp.OKRows + imp.SkippedRows + imp.ErrorRows }

// CheckImportFile đọc tệp fileID của tenantID (files.ReadContent, ngoài transaction) và kiểm bằng xlsx.Read với giới
// hạn của lần nhập, để từ chối trước khi tạo lần nhập: quá 5 MiB hay quá 20.000 dòng → ErrImportTooLarge; tệp không
// có, của tenant khác, sai mục đích, chưa tải xong, không phải .xlsx, sai tiêu đề → ErrImportFileInvalid.
func CheckImportFile(ctx context.Context, d *db.DB, fs files.FileStore, tenantID, fileID uuid.UUID) error {
	_, _, err := readImportFile(ctx, d, fs, tenantID, fileID)
	return err
}

// readImportFile đọc và phân tích tệp nhập; lỗi đổi sang ErrImportTooLarge, ErrImportFileInvalid, lỗi khác bọc nguyên.
func readImportFile(ctx context.Context, d *db.DB, fs files.FileStore, tenantID, fileID uuid.UUID,
) ([]xlsx.Row, []xlsx.CellError, error) {
	b, err := files.ReadContent(ctx, d, fs, tenantID, fileID, ImportPurposeKey, MaxImportBytes)
	switch {
	case errors.Is(err, files.ErrTooLarge):
		return nil, nil, ErrImportTooLarge
	case errors.Is(err, files.ErrNotFound), errors.Is(err, files.ErrNotReady), errors.Is(err, files.ErrObjectNotFound):
		return nil, nil, ErrImportFileInvalid
	case err != nil:
		return nil, nil, fmt.Errorf("đọc tệp nhập: %w", err)
	}
	rows, cellErrs, err := xlsx.Read(bytes.NewReader(b), int64(len(b)),
		xlsx.Limits{MaxBytes: MaxImportBytes, MaxRows: MaxImportRows}, ImportHeader())
	switch {
	case errors.Is(err, xlsx.ErrTooLarge), errors.Is(err, xlsx.ErrTooManyRows):
		return nil, nil, ErrImportTooLarge
	case errors.Is(err, xlsx.ErrNotXlsx), errors.Is(err, xlsx.ErrHeaderMismatch):
		return nil, nil, ErrImportFileInvalid
	case err != nil:
		return nil, nil, fmt.Errorf("đọc tệp nhập: %w", err)
	}
	return rows, cellErrs, nil
}

// StartImport tạo lần nhập pending cho tệp fileID và xếp lệnh customer.import (DedupeKey = mã lần nhập) trong tx của
// bên gọi. Tệp không có, của tenant khác, đã xóa, sai mục đích hay chưa ở trạng thái uploaded → ErrImportFileInvalid.
// Không audit ở đây: lần nhập ghi đúng một dòng audit khi xong (customer.import).
func (w Writer) StartImport(ctx context.Context, tx pgx.Tx, fileID uuid.UUID, a Actor) (Import, error) {
	if err := w.check(); err != nil {
		return Import{}, err
	}
	if a.ID == uuid.Nil {
		return Import{}, ErrInvalidInput
	}
	st, err := files.Status(ctx, tx, fileID)
	switch {
	case errors.Is(err, files.ErrNotFound):
		return Import{}, ErrImportFileInvalid
	case err != nil:
		return Import{}, fmt.Errorf("đọc tệp nhập: %w", err)
	case st.Deleted, st.Purpose != ImportPurposeKey, st.Status != files.StatusUploaded:
		return Import{}, ErrImportFileInvalid
	}
	row, err := store.New(tx).InsertImport(ctx, store.InsertImportParams{FileID: fileID, CreatedBy: a.ID})
	if err != nil {
		return Import{}, fmt.Errorf("ghi lần nhập: %w", err)
	}
	tenant := row.TenantID
	if _, _, err := w.Queue.Enqueue(ctx, tx, ImportKind(), queue.NewJob{
		TenantID: &tenant, Payload: importPayload{ImportID: row.ID}, DedupeKey: row.ID.String(),
	}); err != nil {
		return Import{}, fmt.Errorf("xếp lệnh nhập khách: %w", err)
	}
	return importOf(row)
}

// GetImport đọc lần nhập id; không có hay của tenant khác → ErrNotFound.
func GetImport(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Import, error) {
	row, err := store.New(tx).GetImport(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Import{}, ErrNotFound
	case err != nil:
		return Import{}, fmt.Errorf("đọc lần nhập: %w", err)
	}
	return importOf(row)
}
