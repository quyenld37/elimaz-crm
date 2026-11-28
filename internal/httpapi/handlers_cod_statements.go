package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vùng cod_statements (kế hoạch M3, Task M3-31, M3-32): bảng kê COD, lệch đối soát, quá hạn chưa có tiền.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.
// Task M3-31: importCodStatement, listCodStatements, getCodStatement. Task M3-32: confirmCodStatement,
// listCodDiscrepancies, listCodOverdue.

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
	out, err := h.cashSellerWrite(ctx, http.StatusAccepted, "nhập bảng kê COD", cash.ScopeCarrierAccount, req.CarrierAccountId,
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			st, err := h.d.Cash.ImportStatement(ctx, tx, u.TenantID, file, cash.ImportStatementInput{
				CarrierAccountID: req.CarrierAccountId, Format: req.Format.Or(""), ImportedBy: u.UserID,
			})
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của cash
			}
			body, err := h.codStatementOut(ctx, tx, st)
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
	err := h.sellerRead(ctx, func(tx pgx.Tx, seller uuid.UUID) error {
		f.SellerID = seller
		items, next, err := h.d.Cash.ListStatements(ctx, tx, f)
		if err != nil {
			return err //nolint:wrapcheck // cash đã bọc
		}
		page.Items, page.NextCursor = make([]gen.CodStatement, 0, len(items)), gen.OptString{}
		for _, st := range items {
			body, err := h.codStatementOut(ctx, tx, st)
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
		if err := cashSellerOwns(ctx, tx, cash.ScopeStatement, params.ID); err != nil {
			return err
		}
		d, err := h.d.Cash.GetStatement(ctx, tx, params.ID, string(params.MatchStatus.Or("")), after,
			int(params.Limit.Or(defaultStatementLimit)))
		if err != nil {
			return err //nolint:wrapcheck // cash đã bọc
		}
		head, err := h.codStatementOut(ctx, tx, d.Statement)
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
				FeeAmount: gen.NewOptSignedMoney(gen.SignedMoney(t.Fee)), ShortAmount: gen.NewOptSignedMoney(gen.SignedMoney(t.Short)),
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

// codStatementOut dựng thân CodStatement, đọc tên người nhập, người chốt, tên tài khoản hãng cùng tx.
func (h *handler) codStatementOut(ctx context.Context, tx pgx.Tx, st cash.Statement) (*gen.CodStatement, error) {
	ids := []uuid.UUID{st.ImportedBy}
	if st.ConfirmedBy != nil {
		ids = append(ids, *st.ConfirmedBy)
	}
	names, err := staffNames(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	accounts, err := h.d.Cash.CarrierAccountNames(ctx, tx, []uuid.UUID{st.CarrierAccountID})
	if err != nil {
		return nil, fmt.Errorf("đọc tên tài khoản hãng của bảng kê: %w", err)
	}
	out := &gen.CodStatement{
		ID: st.ID, CarrierKey: st.CarrierKey, CarrierAccountId: st.CarrierAccountID, FileId: st.FileID, Format: st.Format,
		Status: gen.CodStatementStatus(st.Status), LineCount: st.LineCount, CreatedAt: st.CreatedAt, Version: st.Version,
		ImportedBy: gen.NewOptActor(gen.Actor{ID: st.ImportedBy, Name: names[st.ImportedBy]}),
	}
	if name, ok := accounts[st.CarrierAccountID]; ok {
		out.CarrierAccountName = gen.NewOptString(name)
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
	if l.OrderID != nil {
		out.OrderId = gen.NewOptUUID(*l.OrderID)
	}
	if l.OrderCode != "" {
		out.OrderCode = gen.NewOptString(l.OrderCode)
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

// ConfirmCodStatement (POST /api/v1/cod-statements/{id}/confirm, cash.statements.confirm): chốt bảng kê đã khớp khi
// version còn khớp (WriteTx: đổi trạng thái, phát sự kiện ghi tiền theo lô chạy sau commit, audit) → 200 kèm cảnh báo
// không chặn (tổng thực nhận lệch tổng khai). Version khác hay chưa khớp xong → 409 statement_changed (TI07); không có
// → 404. Từ chối không lưu theo khóa chống trùng.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ConfirmCodStatement(ctx context.Context, req *gen.VersionRequest, params gen.ConfirmCodStatementParams) (gen.ConfirmCodStatementRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	out, err := h.cashSellerWrite(ctx, http.StatusOK, "chốt bảng kê COD", cash.ScopeStatement, params.ID,
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			c, err := h.d.Cash.ConfirmStatement(ctx, tx, u.TenantID, params.ID, req.Version, u.UserID)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của cash
			}
			head, err := h.codStatementOut(ctx, tx, c.Statement)
			if err != nil {
				return nil, err
			}
			return &gen.CodStatementConfirmed{Statement: *head, Warnings: c.Warnings},
				h.audit(ctx, tx, u, "cash.statement.confirm", "cash.cod_statement", c.Statement.ID, nil, map[string]any{
					"version": c.Statement.Version, "warnings": len(c.Warnings),
				})
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CodStatementConfirmed), nil //nolint:forcetypeassert // fn trên trả *gen.CodStatementConfirmed
}

// uuidCursor đọc con trỏ trang là một mã (sai dạng → cash.ErrInvalidInput, 400).
func uuidCursor(c gen.OptString, what string) (*uuid.UUID, error) {
	v, ok := c.Get()
	if !ok {
		return nil, nil //nolint:nilnil // không có con trỏ: trang đầu
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, fmt.Errorf("con trỏ trang %s: %w", what, cash.ErrInvalidInput)
	}
	return &id, nil
}

// ListCodDiscrepancies (GET /api/v1/cod/discrepancies, cash.cod.read): "Lệch đối soát" mới nhất trước — dòng lệch
// tiền (vận đơn còn lệch), trùng, ngoài hệ thống của bảng kê đã chốt; phần còn mở; tiền về rồi mới hoàn. Con trỏ là mã
// mục cuối trang trước.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCodDiscrepancies(ctx context.Context, params gen.ListCodDiscrepanciesParams) (gen.ListCodDiscrepanciesRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	before, err := uuidCursor(params.Cursor, "lệch đối soát")
	if err != nil {
		return nil, err
	}
	page := &gen.CodDiscrepancyPage{Items: []gen.CodDiscrepancy{}}
	err = h.sellerRead(ctx, func(tx pgx.Tx, seller uuid.UUID) error {
		items, next, err := h.d.Cash.ListDiscrepancies(ctx, tx, seller, before, int(params.Limit.Or(defaultStatementLimit)))
		if err != nil {
			return err //nolint:wrapcheck // cash đã bọc
		}
		page.Items, page.NextCursor = make([]gen.CodDiscrepancy, 0, len(items)), gen.OptString{}
		for _, d := range items {
			page.Items = append(page.Items, codDiscrepancyOut(d))
		}
		if next != nil {
			page.NextCursor = gen.NewOptString(next.String())
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc lệch đối soát: %w", err)
	}
	return page, nil
}

func codDiscrepancyOut(d cash.Discrepancy) gen.CodDiscrepancy {
	out := gen.CodDiscrepancy{Kind: gen.CodDiscrepancyKind(d.Kind)}
	if d.StatementID != nil {
		out.StatementId = gen.NewOptUUID(*d.StatementID)
	}
	if d.LineNo > 0 {
		out.LineNo = gen.NewOptInt32(d.LineNo)
	}
	if d.ShipmentID != nil {
		out.ShipmentId = gen.NewOptUUID(*d.ShipmentID)
	}
	if d.OrderCode != "" {
		out.OrderCode = gen.NewOptString(d.OrderCode)
	}
	if d.OrderID != nil {
		out.OrderId = gen.NewOptUUID(*d.OrderID)
	}
	if d.CarrierKey != "" {
		out.CarrierKey = gen.NewOptString(d.CarrierKey)
	}
	if d.TrackingCode != "" {
		out.TrackingCode = gen.NewOptString(d.TrackingCode)
	}
	if d.Kind != cash.DiscrepancyRemittedThenReturned || d.ShipmentID != nil {
		out.CodAmount = gen.NewOptMoney(gen.Money(d.COD))
	}
	if d.Expected != nil {
		out.ExpectedAmount = gen.NewOptSignedMoney(gen.SignedMoney(*d.Expected))
	}
	if d.Diff != nil {
		out.Diff = gen.NewOptSignedMoney(gen.SignedMoney(*d.Diff))
	}
	if d.MatchStatus != "" {
		out.MatchStatus = gen.NewOptCodMatchStatus(gen.CodMatchStatus(d.MatchStatus))
	}
	return out
}

// ListCodOverdue (GET /api/v1/cod/overdue, cash.cod.read): vận đơn giao xong quá số ngày codOverdueDays (cài đặt vận
// chuyển) mà hãng chưa trả tiền COD; con trỏ là mã vận đơn cuối trang trước.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCodOverdue(ctx context.Context, params gen.ListCodOverdueParams) (gen.ListCodOverdueRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	after, err := uuidCursor(params.Cursor, "quá hạn chưa có tiền")
	if err != nil {
		return nil, err
	}
	page := &gen.CodOverduePage{Items: []gen.CodOverdue{}}
	err = h.sellerRead(ctx, func(tx pgx.Tx, seller uuid.UUID) error {
		items, next, err := h.d.Cash.ListOverdue(ctx, tx, seller, after, int(params.Limit.Or(defaultStatementLimit)))
		if err != nil {
			return err //nolint:wrapcheck // cash đã bọc
		}
		page.Items, page.NextCursor = make([]gen.CodOverdue, 0, len(items)), gen.OptString{}
		for _, o := range items {
			page.Items = append(page.Items, gen.CodOverdue{
				ShipmentId: o.ShipmentID, OrderId: gen.NewOptUUID(o.OrderID), OrderCode: o.OrderCode,
				CarrierKey: o.CarrierKey, TrackingCode: o.TrackingCode, DeliveredAt: o.DeliveredAt,
				CarrierHolding: gen.SignedMoney(o.CarrierHolding), Days: o.Days,
			})
		}
		if next != nil {
			page.NextCursor = gen.NewOptString(next.String())
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc quá hạn chưa có tiền: %w", err)
	}
	return page, nil
}
