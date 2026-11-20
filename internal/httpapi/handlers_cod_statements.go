package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vùng cod_statements (kế hoạch M3, Task M3-31, M3-32): bảng kê COD, lệch đối soát, quá hạn chưa có tiền.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.
// Task M3-31: importCodStatement, listCodStatements, getCodStatement.

// errNoCash: Deps.Cash chưa nối (lỗi dựng; cmd/crm luôn nối cho api).
var errNoCash = errors.New("httpapi: chưa nối module tiền")

// defaultStatementLimit: số dòng mỗi trang khi không gửi limit (mặc định của hợp đồng).
const defaultStatementLimit = 50

// isCashRefusal: lỗi nghiệp vụ của bảng kê (tài khoản, định dạng, trùng tệp, tệp chưa dùng được): không lưu theo khóa
// chống trùng.
func isCashRefusal(err error) bool {
	if _, ok := errors.AsType[*cash.DuplicateStatementError](err); ok {
		return true
	}
	_, _, ok := cashError(err)
	return ok
}

// ImportCodStatement (POST /api/v1/cod-statements, cash.statements.import): đọc tệp đã tải lên (mục đích
// cod_statement) và băm nội dung ngoài transaction, rồi trong WriteTx ghi bảng kê imported, xếp lệnh đọc tệp, audit
// → 202. Cùng tệp (băm nội dung) của cùng hãng đã nhập → 409 statement_duplicate kèm statementId; tài khoản hãng
// không có → 404; định dạng lạ → 422 invalid_input; tệp chưa tải xong → 409 file_not_ready; quá 10 MiB → 422
// file_too_large. Từ chối không lưu theo khóa chống trùng.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ImportCodStatement(ctx context.Context, req *gen.ImportCodStatementRequest, _ gen.ImportCodStatementParams) (gen.ImportCodStatementRes, error) {
	u, ok := authUser(ctx)
	switch {
	case !ok:
		return nil, errNoAuthUser
	case h.d.Cash == nil:
		return nil, errNoCash
	}
	file, err := h.d.Cash.PrepareStatementImport(ctx, u.TenantID, req.FileId)
	if err != nil {
		return nil, fmt.Errorf("đọc tệp bảng kê: %w", err)
	}
	out, err := h.refusableWrite(ctx, http.StatusAccepted, isCashRefusal, "nhập bảng kê COD",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			st, err := h.d.Cash.ImportStatement(ctx, tx, u.TenantID, file, cash.ImportStatementInput{
				CarrierAccountID: req.CarrierAccountId, Format: req.Format.Or(""), ImportedBy: u.UserID,
			})
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của cash
			}
			body, err := codStatementOut(ctx, tx, st)
			if err != nil {
				return nil, err
			}
			return body, h.audit(ctx, tx, u, "cash.statement.import", "cash.cod_statement", st.ID, nil, map[string]any{
				"carrier_account_id": st.CarrierAccountID.String(), "file_id": st.FileID.String(), "format": st.Format,
			})
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CodStatement), nil //nolint:forcetypeassert // fn trên trả *gen.CodStatement
}

// ListCodStatements (GET /api/v1/cod-statements, cash.cod.read): bảng kê mới nhất trước, lọc theo trạng thái, tài
// khoản hãng; con trỏ là mã bảng kê cuối trang trước (sai dạng → 400 invalid_input).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCodStatements(ctx context.Context, params gen.ListCodStatementsParams) (gen.ListCodStatementsRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	f := cash.StatementListFilter{
		Status: string(params.Status.Or("")), CarrierAccountID: optPtr[uuid.UUID](params.CarrierAccountId),
		Limit: int(params.Limit.Or(defaultStatementLimit)),
	}
	if c, ok := params.Cursor.Get(); ok {
		id, err := uuid.Parse(c)
		if err != nil {
			return nil, fmt.Errorf("con trỏ trang bảng kê: %w", cash.ErrInvalidInput)
		}
		f.Before = &id
	}
	page := &gen.CodStatementPage{Items: []gen.CodStatement{}}
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		items, next, err := h.d.Cash.ListStatements(ctx, tx, f)
		if err != nil {
			return err //nolint:wrapcheck // cash đã bọc
		}
		page.Items, page.NextCursor = make([]gen.CodStatement, 0, len(items)), gen.OptString{}
		for _, st := range items {
			body, err := codStatementOut(ctx, tx, st)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, *body)
		}
		if next != nil {
			page.NextCursor = gen.NewOptString(next.String())
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách bảng kê: %w", err)
	}
	return page, nil
}

// GetCodStatement (GET /api/v1/cod-statements/{id}, cash.cod.read): đầu bảng kê, tổng theo kết quả khớp (mọi dòng) và
// dòng theo số dòng (lọc matchStatus); con trỏ là số dòng cuối trang trước. Không có hay của tenant khác → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCodStatement(ctx context.Context, params gen.GetCodStatementParams) (gen.GetCodStatementRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	var after int32
	if c, ok := params.Cursor.Get(); ok {
		n, err := strconv.ParseInt(c, 10, 32)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("con trỏ trang dòng bảng kê: %w", cash.ErrInvalidInput)
		}
		after = int32(n)
	}
	var out *gen.CodStatementDetail
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		d, err := h.d.Cash.GetStatement(ctx, tx, params.ID, string(params.MatchStatus.Or("")), after,
			int(params.Limit.Or(defaultStatementLimit)))
		if err != nil {
			return err //nolint:wrapcheck // cash đã bọc
		}
		head, err := codStatementOut(ctx, tx, d.Statement)
		if err != nil {
			return err
		}
		out = &gen.CodStatementDetail{
			Statement: *head, Totals: make([]gen.CodMatchTotal, 0, len(d.Totals)),
			Lines: make([]gen.CodStatementLine, 0, len(d.Lines)),
		}
		for _, t := range d.Totals {
			out.Totals = append(out.Totals, gen.CodMatchTotal{
				MatchStatus: gen.CodMatchStatus(t.MatchStatus), Count: t.Lines, CodAmount: gen.Money(t.COD),
			})
		}
		for _, l := range d.Lines {
			out.Lines = append(out.Lines, codStatementLineOut(l))
		}
		if d.Next > 0 {
			out.NextCursor = gen.NewOptString(strconv.FormatInt(int64(d.Next), 10))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc bảng kê: %w", err)
	}
	return out, nil
}

// codStatementOut dựng thân CodStatement, đọc tên người nhập, người chốt cùng tx.
func codStatementOut(ctx context.Context, tx pgx.Tx, st cash.Statement) (*gen.CodStatement, error) {
	ids := []uuid.UUID{st.ImportedBy}
	if st.ConfirmedBy != nil {
		ids = append(ids, *st.ConfirmedBy)
	}
	names, err := staffNames(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	out := &gen.CodStatement{
		ID: st.ID, CarrierKey: st.CarrierKey, CarrierAccountId: st.CarrierAccountID, FileId: st.FileID, Format: st.Format,
		Status: gen.CodStatementStatus(st.Status), LineCount: st.LineCount, CreatedAt: st.CreatedAt, Version: st.Version,
		ImportedBy: gen.NewOptActor(gen.Actor{ID: st.ImportedBy, Name: names[st.ImportedBy]}),
	}
	if st.PeriodFrom != nil {
		out.PeriodFrom = gen.NewOptDate(*st.PeriodFrom)
	}
	if st.PeriodTo != nil {
		out.PeriodTo = gen.NewOptDate(*st.PeriodTo)
	}
	if st.DeclaredTotal != nil {
		out.DeclaredTotal = gen.NewOptSignedMoney(gen.SignedMoney(*st.DeclaredTotal))
	}
	if pe := st.ParseError; pe != nil {
		e := gen.CodStatementParseError{Code: pe.Code}
		if pe.Column != "" {
			e.Column = gen.NewOptString(pe.Column)
		}
		if pe.Line > 0 {
			e.Line = gen.NewOptInt32(int32(min(pe.Line, 1<<31-1)))
		}
		out.ParseError = gen.NewOptCodStatementParseError(e)
	}
	if st.ConfirmedBy != nil {
		out.ConfirmedBy = gen.NewOptActor(gen.Actor{ID: *st.ConfirmedBy, Name: names[*st.ConfirmedBy]})
	}
	if st.ConfirmedAt != nil {
		out.ConfirmedAt = gen.NewOptDateTime(*st.ConfirmedAt)
	}
	return out, nil
}

func codStatementLineOut(l cash.StatementLine) gen.CodStatementLine {
	out := gen.CodStatementLine{
		ID: l.ID, LineNo: l.LineNo, TrackingCode: l.TrackingCode, CodAmount: gen.Money(l.COD),
		FeeAmount: gen.SignedMoney(l.Fee), OtherFee: gen.SignedMoney(l.OtherFee), NetAmount: gen.SignedMoney(l.Net),
		MatchStatus: gen.CodMatchStatus(l.MatchStatus),
	}
	if l.ShipmentID != nil {
		out.ShipmentId = gen.NewOptUUID(*l.ShipmentID)
	}
	if l.PaidOn != nil {
		out.PaidOn = gen.NewOptDate(*l.PaidOn)
	}
	if l.ExpectedAmount != nil {
		out.ExpectedAmount = gen.NewOptSignedMoney(gen.SignedMoney(*l.ExpectedAmount))
	}
	if l.DiffAmount != nil {
		out.DiffAmount = gen.NewOptSignedMoney(gen.SignedMoney(*l.DiffAmount))
	}
	if l.PostedTransferID != nil {
		out.PostedTransferId = gen.NewOptUUID(*l.PostedTransferID)
	}
	return out
}

// ConfirmCodStatement (POST /api/v1/cod-statements/{id}/confirm).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ConfirmCodStatement(_ context.Context, _ *gen.VersionRequest, _ gen.ConfirmCodStatementParams) (gen.ConfirmCodStatementRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListCodDiscrepancies (GET /api/v1/cod/discrepancies).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListCodDiscrepancies(_ context.Context, _ gen.ListCodDiscrepanciesParams) (gen.ListCodDiscrepanciesRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListCodOverdue (GET /api/v1/cod/overdue).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListCodOverdue(_ context.Context, _ gen.ListCodOverdueParams) (gen.ListCodOverdueRes, error) {
	return nil, ht.ErrNotImplemented
}
