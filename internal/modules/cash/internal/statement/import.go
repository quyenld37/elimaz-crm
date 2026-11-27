package statement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/cash/internal/formats"
	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Trạng thái bảng kê (cột status).
const (
	StatusImported    = "imported"
	StatusParsing     = "parsing"
	StatusParseFailed = "parse_failed"
	StatusParsed      = "parsed"
)

// Lỗi của đường nhập, đọc bảng kê (cash bọc lại thành sentinel của gói gốc).
var (
	// ErrDuplicate: cùng tệp (băm nội dung) của cùng hãng đã nhập; lỗi cụ thể là *DuplicateError.
	ErrDuplicate = errors.New("cash: bảng kê đã nhập")
	// ErrAccountNotFound: tài khoản hãng không có (hay của tenant khác).
	ErrAccountNotFound = errors.New("cash: không có tài khoản hãng")
	// ErrUnknownFormat: định dạng không có cho hãng của tài khoản.
	ErrUnknownFormat = errors.New("cash: không có định dạng bảng kê này")
	// ErrNotFound: bảng kê không có (hay của tenant khác).
	ErrNotFound = errors.New("cash: không có bảng kê")
)

// DuplicateError là chi tiết của ErrDuplicate: bảng kê đã nhập trước với cùng tệp (chỉ mã).
type DuplicateError struct{ StatementID uuid.UUID }

func (e *DuplicateError) Error() string {
	return "cash: bảng kê đã nhập: " + e.StatementID.String()
}

// Unwrap cho errors.Is(err, ErrDuplicate).
func (*DuplicateError) Unwrap() error { return ErrDuplicate }

// Statement là đầu một bảng kê. ParseError có khi status = parse_failed.
type Statement struct {
	ID, CarrierAccountID, FileID uuid.UUID
	CarrierKey, Format, Status   string
	PeriodFrom, PeriodTo         *time.Time
	DeclaredTotal                *int64
	ParseError                   *ParseError
	LineCount                    int32
	ImportedBy                   uuid.UUID
	ConfirmedBy                  *uuid.UUID
	ConfirmedAt                  *time.Time
	CreatedAt                    time.Time
	Version                      int64
}

// StoredLine là một dòng bảng kê đã ghi, kèm kết quả khớp (Task M3-32 điền).
type StoredLine struct {
	ID                         uuid.UUID
	LineNo                     int32
	TrackingCode               string
	COD, Fee, OtherFee, Net    int64
	PaidOn                     *time.Time
	MatchStatus                string
	ShipmentID                 *uuid.UUID
	ExpectedAmount, DiffAmount *int64
	PostedTransferID           *uuid.UUID
	// OrderID, OrderCode: đơn của vận đơn đã khớp (nil, rỗng khi chưa khớp vận đơn nào).
	OrderID   *uuid.UUID
	OrderCode string
}

// MatchTotal là số dòng, tổng COD, tổng phí hãng (phí + phí khác) và tổng phần hãng trả thiếu (chênh âm, đổi dấu)
// của một kết quả khớp.
type MatchTotal struct {
	MatchStatus string
	Lines       int32
	COD         int64
	Fee         int64
	Short       int64
}

// ImportInput là một lần nhập: tệp (đã đọc, băm SHA-256 hex của nội dung ngoài transaction), tài khoản hãng, định
// dạng (rỗng: mặc định của hãng), người nhập.
type ImportInput struct {
	CarrierAccountID, FileID uuid.UUID
	FileSHA256               string
	Format                   string
	ImportedBy               uuid.UUID
}

type parsePayload struct {
	StatementID uuid.UUID `json:"statementId"`
}

// Import ghi bảng kê imported trong tx (tenancy.WithTenant / WriteTx của tenant) và xếp lệnh đọc tệp ParseKind cùng
// tx. Tài khoản không có → ErrAccountNotFound; định dạng không dùng được cho hãng → ErrUnknownFormat; cùng tệp của cùng
// hãng đã nhập → *DuplicateError (ErrDuplicate) kèm mã bảng kê cũ. Chạy lại được trong RetryTx.
func Import(ctx context.Context, tx pgx.Tx, q queue.JobQueue, reg *formats.Registry, in ImportInput) (Statement, error) {
	s := store.New(tx)
	carrierKey, err := s.CarrierAccountKey(ctx, in.CarrierAccountID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Statement{}, fmt.Errorf("nhập bảng kê: %w", ErrAccountNotFound)
	case err != nil:
		return Statement{}, fmt.Errorf("nhập bảng kê: đọc tài khoản hãng: %w", err)
	}
	f, err := reg.For(carrierKey, in.Format)
	if err != nil {
		return Statement{}, fmt.Errorf("nhập bảng kê: %w: %w", ErrUnknownFormat, err)
	}
	row, err := s.InsertStatement(ctx, store.InsertStatementParams{
		CarrierKey: carrierKey, CarrierAccountID: in.CarrierAccountID, FileID: in.FileID, FileSha256: in.FileSHA256,
		Format: f.Key, ImportedBy: in.ImportedBy,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		old, err := s.StatementIDByFile(ctx, store.StatementIDByFileParams{CarrierKey: carrierKey, FileSha256: in.FileSHA256})
		if err != nil {
			return Statement{}, fmt.Errorf("nhập bảng kê: đọc bảng kê trùng: %w", err)
		}
		return Statement{}, fmt.Errorf("nhập bảng kê: %w", &DuplicateError{StatementID: old})
	}
	if err != nil {
		return Statement{}, fmt.Errorf("nhập bảng kê: ghi bảng kê: %w", err)
	}
	tenant := row.TenantID
	if _, _, err := q.Enqueue(ctx, tx, ParseKind(), queue.NewJob{
		TenantID: &tenant, Payload: parsePayload{StatementID: row.ID}, DedupeKey: row.ID.String(),
	}); err != nil {
		return Statement{}, fmt.Errorf("nhập bảng kê: xếp lệnh đọc tệp: %w", err)
	}
	return statementOf(row), nil
}

// Get đọc bảng kê id trong tx của tenant; không có → ErrNotFound.
func Get(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Statement, error) {
	row, err := store.New(tx).GetStatement(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Statement{}, fmt.Errorf("đọc bảng kê %s: %w", id, ErrNotFound)
	case err != nil:
		return Statement{}, fmt.Errorf("đọc bảng kê %s: %w", id, err)
	}
	return statementOf(row), nil
}

// ListFilter lọc danh sách bảng kê: trạng thái, tài khoản hãng (rỗng: không lọc); Before là id của dòng cuối trang
// trước (nil: trang đầu).
type ListFilter struct {
	Status           string
	CarrierAccountID *uuid.UUID
	Before           *uuid.UUID
	Limit            int
}

// List đọc bảng kê mới nhất trước theo f.
func List(ctx context.Context, tx pgx.Tx, f ListFilter) ([]Statement, error) {
	rows, err := store.New(tx).ListStatements(ctx, store.ListStatementsParams{
		Status: optText(f.Status), CarrierAccountID: db.NullUUID(f.CarrierAccountID), BeforeID: db.NullUUID(f.Before),
		Lim: int32(min(max(f.Limit, 1), 1000)),
	})
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách bảng kê: %w", err)
	}
	out := make([]Statement, 0, len(rows))
	for _, r := range rows {
		out = append(out, statementOf(r))
	}
	return out, nil
}

// Lines đọc dòng của bảng kê id theo số dòng tăng dần, sau số dòng after, lọc theo matchStatus (rỗng: mọi dòng).
func Lines(ctx context.Context, tx pgx.Tx, id uuid.UUID, matchStatus string, after int32, limit int) ([]StoredLine, error) {
	rows, err := store.New(tx).ListStatementLines(ctx, store.ListStatementLinesParams{
		StatementID: id, MatchStatus: optText(matchStatus), AfterLineNo: after,
		Lim: int32(min(max(limit, 1), 1000)),
	})
	if err != nil {
		return nil, fmt.Errorf("đọc dòng bảng kê %s: %w", id, err)
	}
	out := make([]StoredLine, 0, len(rows))
	for _, r := range rows {
		out = append(out, StoredLine{
			ID: r.ID, LineNo: r.LineNo, TrackingCode: r.TrackingCode, COD: r.CodAmount, Fee: r.FeeAmount,
			OtherFee: r.OtherFee, Net: r.NetAmount, PaidOn: datePtr(r.PaidOn), MatchStatus: r.MatchStatus,
			ShipmentID: uuidPtr(r.ShipmentID), ExpectedAmount: int8Ptr(r.ExpectedAmount), DiffAmount: int8Ptr(r.DiffAmount),
			PostedTransferID: uuidPtr(r.PostedTransferID), OrderID: uuidPtr(r.OrderID), OrderCode: r.OrderCode.String,
		})
	}
	return out, nil
}

// AccountNames đọc tên tài khoản hãng theo mã qua fulfillment.carrier_accounts_v; mã không có vắng trong kết quả.
func AccountNames(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	rows, err := store.New(tx).CarrierAccountNames(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("đọc tên tài khoản hãng: %w", err)
	}
	names := make(map[uuid.UUID]string, len(rows))
	for _, r := range rows {
		names[r.ID] = r.Name
	}
	return names, nil
}

// Totals đọc số dòng và tổng COD theo kết quả khớp của mọi dòng của bảng kê id.
func Totals(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]MatchTotal, error) {
	rows, err := store.New(tx).StatementTotals(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("đọc tổng bảng kê %s: %w", id, err)
	}
	out := make([]MatchTotal, 0, len(rows))
	for _, r := range rows {
		out = append(out, MatchTotal{
			MatchStatus: r.MatchStatus, Lines: r.Lines, COD: r.CodAmount, Fee: r.FeeAmount, Short: r.ShortAmount,
		})
	}
	return out, nil
}

func statementOf(r store.CashCodStatement) Statement {
	s := Statement{
		ID: r.ID, CarrierAccountID: r.CarrierAccountID, FileID: r.FileID, CarrierKey: r.CarrierKey, Format: r.Format,
		Status: r.Status, PeriodFrom: datePtr(r.PeriodFrom), PeriodTo: datePtr(r.PeriodTo),
		DeclaredTotal: int8Ptr(r.DeclaredTotal), LineCount: r.LineCount, ImportedBy: r.ImportedBy,
		ConfirmedBy: uuidPtr(r.ConfirmedBy), CreatedAt: r.CreatedAt, Version: r.Version,
	}
	if r.ConfirmedAt.Valid {
		t := r.ConfirmedAt.Time
		s.ConfirmedAt = &t
	}
	if r.ParseError.Valid {
		var pe ParseError
		if json.Unmarshal([]byte(r.ParseError.String), &pe) == nil {
			s.ParseError = &pe
		}
	}
	return s
}

// ptrIf trả con trỏ tới v khi ok (cột có giá trị), nil khi không.
func ptrIf[T any](v T, ok bool) *T {
	if ok {
		return new(v)
	}
	return nil
}

func datePtr(d pgtype.Date) *time.Time { return ptrIf(d.Time, d.Valid) }

func int8Ptr(v pgtype.Int8) *int64 { return ptrIf(v.Int64, v.Valid) }

func uuidPtr(v pgtype.UUID) *uuid.UUID { return ptrIf(uuid.UUID(v.Bytes), v.Valid) }

func optText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }
