package cash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash/internal/formats"
	"elimaz.com.vn/crm/internal/modules/cash/internal/statement"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng statements (kế hoạch M3, Task M3-31, M3-32): nhập bảng kê COD, tự khớp và chốt, "Lệch đối soát", "Quá hạn chưa có tiền".
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.
//
// Task M3-31: nhập bảng kê (PrepareStatementImport ngoài transaction, ImportStatement trong WriteTx), lệnh đọc tệp
// command.cash.statement_parse (statementsHandlers), đọc bảng kê và dòng.

// Kiểu của bảng kê (gói con statement).
type (
	// Statement là đầu một bảng kê COD.
	Statement = statement.Statement
	// StatementLine là một dòng bảng kê đã ghi kèm kết quả khớp.
	StatementLine = statement.StoredLine
	// MatchTotal là số dòng, tổng COD của một kết quả khớp.
	MatchTotal = statement.MatchTotal
	// StatementParseError là lỗi định dạng tệp {code, column?, line?}.
	StatementParseError = statement.ParseError
	// StatementListFilter lọc danh sách bảng kê.
	StatementListFilter = statement.ListFilter
	// DuplicateStatementError là chi tiết của ErrStatementDuplicate (mã bảng kê đã nhập trước với cùng tệp).
	DuplicateStatementError = statement.DuplicateError
)

// Lỗi của vùng bảng kê.
var (
	// ErrStatementDuplicate: cùng tệp (băm nội dung) của cùng hãng đã nhập (lỗi cụ thể *DuplicateStatementError).
	ErrStatementDuplicate = statement.ErrDuplicate
	// ErrCarrierAccountNotFound: tài khoản hãng không có hay của tenant khác.
	ErrCarrierAccountNotFound = statement.ErrAccountNotFound
	// ErrUnknownStatementFormat: định dạng không có cho hãng của tài khoản.
	ErrUnknownStatementFormat = statement.ErrUnknownFormat
	// ErrStatementNotFound: bảng kê không có hay của tenant khác.
	ErrStatementNotFound = statement.ErrNotFound
	// ErrStatementFileNotReady: tệp chưa tải xong, sai mục đích hay mất trên kho.
	ErrStatementFileNotReady = errors.New("cash: tệp bảng kê chưa dùng được")
	// ErrInvalidInput: tham số đọc sai dạng (con trỏ trang).
	ErrInvalidInput = errors.New("cash: tham số sai dạng")
	// ErrFilesNotWired: Deps.Files chưa nối (lỗi dựng).
	ErrFilesNotWired = errors.New("cash: chưa nối kho tệp")
)

// statementFormats dựng sổ định dạng bảng kê từ elimaz_v1 và định dạng của các adapter hãng (r nil: chỉ elimaz_v1).
func statementFormats(r *fulfillment.Registry) (*formats.Registry, error) {
	var fs []fulfillment.StatementFormat
	if r != nil {
		fs = r.StatementFormats()
	}
	reg, err := formats.NewRegistry(fs)
	if err != nil {
		return nil, fmt.Errorf("dựng sổ định dạng bảng kê: %w", err)
	}
	return reg, nil
}

// CheckStatementFormats kiểm định dạng bảng kê của mọi adapter trong r (trùng khóa, khai sai → lỗi); cmd/crm gọi lúc
// khởi động.
func CheckStatementFormats(r *fulfillment.Registry) error {
	_, err := statementFormats(r)
	return err
}

// statementsHandlers: lệnh đọc tệp bảng kê. Sổ định dạng lỗi → không có lệnh (CheckStatementFormats đã chặn khởi động).
func (m *Module) statementsHandlers() []commands.Handler {
	reg, err := statementFormats(m.d.Carriers)
	if err != nil {
		return nil
	}
	return []commands.Handler{statement.NewParser(m.d.DB, m.d.Files, reg, StatementPurposeKey)}
}

func (*Module) statementsInbound() []webhooks.InboundHandler { return nil }
func (*Module) statementsConsumers() []events.Consumer       { return nil }
func (*Module) statementsSchedules() []schedules.Schedule    { return nil }
func (*Module) statementsCleanups() []schedules.CleanupFunc  { return nil }

// StatementFile là tệp bảng kê đã đọc và băm (PrepareStatementImport), dùng cho ImportStatement của cùng tenant.
type StatementFile struct {
	tenantID, fileID uuid.UUID
	sha256           string
}

// PrepareStatementImport đọc tệp fileID của tenantID (mục đích cod_statement, đã tải xong) **ngoài** transaction
// (ADR-46), tối đa 10 MiB, và băm SHA-256 nội dung (khóa chống nhập trùng: đổi tên tệp vẫn cùng băm). Tệp không có
// hay của tenant khác → files.ErrNotFound; quá 10 MiB → files.ErrTooLarge; chưa tải xong, sai mục đích, mất trên kho
// → ErrStatementFileNotReady.
func (s *Service) PrepareStatementImport(ctx context.Context, tenantID, fileID uuid.UUID) (StatementFile, error) {
	if s.d.Files == nil {
		return StatementFile{}, ErrFilesNotWired
	}
	body, err := files.ReadContent(ctx, s.d.DB, s.d.Files, tenantID, fileID, StatementPurposeKey, MaxStatementBytes)
	switch {
	case errors.Is(err, files.ErrNotReady), errors.Is(err, files.ErrObjectNotFound):
		return StatementFile{}, fmt.Errorf("đọc tệp bảng kê: %w", ErrStatementFileNotReady)
	case err != nil:
		return StatementFile{}, fmt.Errorf("đọc tệp bảng kê: %w", err)
	}
	sum := sha256.Sum256(body)
	return StatementFile{tenantID: tenantID, fileID: fileID, sha256: hex.EncodeToString(sum[:])}, nil
}

// ImportStatementInput là phần còn lại của một lần nhập: tài khoản hãng, định dạng (rỗng: mặc định của hãng), người
// nhập.
type ImportStatementInput struct {
	CarrierAccountID uuid.UUID
	Format           string
	ImportedBy       uuid.UUID
}

// errFileOtherTenant: tệp đã đọc của tenant khác tenant của transaction (lỗi lập trình của bên gọi).
var errFileOtherTenant = errors.New("cash: tệp bảng kê của tenant khác")

// ImportStatement ghi bảng kê imported từ f và xếp lệnh đọc tệp, trong tx (WriteTx của tenantID của f): tài khoản
// không có → ErrCarrierAccountNotFound; định dạng không dùng được cho hãng → ErrUnknownStatementFormat; cùng tệp của
// cùng hãng đã nhập → *DuplicateStatementError (ErrStatementDuplicate). Chạy lại được trong RetryTx.
func (s *Service) ImportStatement(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, f StatementFile, in ImportStatementInput) (Statement, error) {
	if f.tenantID != tenantID || f.sha256 == "" {
		return Statement{}, errFileOtherTenant
	}
	reg, err := statementFormats(s.d.Carriers)
	if err != nil {
		return Statement{}, err
	}
	st, err := statement.Import(ctx, tx, s.d.Queue, reg, statement.ImportInput{
		CarrierAccountID: in.CarrierAccountID, FileID: f.fileID, FileSHA256: f.sha256, Format: in.Format,
		ImportedBy: in.ImportedBy,
	})
	if err != nil {
		return Statement{}, err //nolint:wrapcheck // statement đã bọc; sentinel là bí danh của gói này
	}
	return st, nil
}

// StatementDetail là một trang của bảng kê: đầu bảng kê, tổng theo kết quả khớp (mọi dòng), dòng theo số dòng; Next
// là số dòng cuối của trang khi còn trang sau (0: hết).
type StatementDetail struct {
	Statement Statement
	Totals    []MatchTotal
	Lines     []StatementLine
	Next      int32
}

// GetStatement đọc bảng kê id trong tx của tenant với limit dòng sau số dòng after, lọc theo matchStatus (rỗng: mọi
// dòng). Không có → ErrStatementNotFound.
func (*Service) GetStatement(ctx context.Context, tx pgx.Tx, id uuid.UUID, matchStatus string, after int32, limit int) (StatementDetail, error) {
	limit = max(limit, 1)
	st, err := statement.Get(ctx, tx, id)
	if err != nil {
		return StatementDetail{}, err //nolint:wrapcheck // statement đã bọc
	}
	totals, err := statement.Totals(ctx, tx, id)
	if err != nil {
		return StatementDetail{}, err //nolint:wrapcheck // statement đã bọc
	}
	lines, err := statement.Lines(ctx, tx, id, matchStatus, after, limit+1)
	if err != nil {
		return StatementDetail{}, err //nolint:wrapcheck // statement đã bọc
	}
	d := StatementDetail{Statement: st, Totals: totals, Lines: lines}
	if len(lines) > limit {
		d.Lines = lines[:limit]
		d.Next = d.Lines[limit-1].LineNo
	}
	return d, nil
}

// ListStatements đọc bảng kê mới nhất trước theo f (f.Limit dòng); next là mã của dòng cuối khi còn trang sau.
func (*Service) ListStatements(ctx context.Context, tx pgx.Tx, f StatementListFilter) (items []Statement, next *uuid.UUID, err error) {
	limit := max(f.Limit, 1)
	f.Limit = limit + 1
	items, err = statement.List(ctx, tx, f)
	if err != nil {
		return nil, nil, err //nolint:wrapcheck // statement đã bọc
	}
	if len(items) > limit {
		items = items[:limit]
		id := items[limit-1].ID
		next = &id
	}
	return items, next, nil
}
