package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// customerImportTemplateName là tên tệp khi tải mẫu nhập khách về.
const customerImportTemplateName = "mau-nhap-khach.xlsx"

// GetCustomerImportTemplate (GET /api/v1/customer-imports/template, customer.import): tệp mẫu Excel nhập khách.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCustomerImportTemplate(_ context.Context) (gen.GetCustomerImportTemplateRes, error) {
	body, err := customer.ImportTemplate()
	if err != nil {
		return nil, fmt.Errorf("sinh mẫu nhập khách: %w", err)
	}
	return &gen.GetCustomerImportTemplateOKHeaders{
		ContentDisposition: gen.NewOptString(`attachment; filename="` + customerImportTemplateName + `"`),
		Response:           gen.GetCustomerImportTemplateOK{Data: bytes.NewReader(body)},
	}, nil
}

// StartCustomerImport (POST /api/v1/customer-imports, customer.import): kiểm tệp trước (đọc kho ngoài transaction:
// quá 5 MiB hay 20.000 dòng → 422 import_too_large; tệp không dùng được → 422 import_file_invalid), rồi tạo lần nhập
// pending và xếp lệnh customer.import → 202 {importId}.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) StartCustomerImport(ctx context.Context, req *gen.StartCustomerImportRequest, _ gen.StartCustomerImportParams) (gen.StartCustomerImportRes, error) {
	u, ok := authUser(ctx)
	switch {
	case !ok:
		return nil, errNoAuthUser
	case h.d.Files == nil:
		return nil, errNoFileStore
	}
	if err := customer.CheckImportFile(ctx, h.d.DB, h.d.Files, u.TenantID, req.FileId); err != nil {
		return nil, fmt.Errorf("kiểm tệp nhập khách: %w", err)
	}
	w := customer.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	out, err := h.refusableWrite(ctx, http.StatusAccepted, isCustomerRefusal, "bắt đầu nhập khách",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			a := customer.Actor{Type: customer.ActorStaff, ID: u.UserID, RequestID: RequestID(ctx)}
			imp, err := w.StartImport(ctx, tx, req.FileId, a)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của customer
			}
			return &gen.StartCustomerImportResult{ImportId: imp.ID}, nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.StartCustomerImportResult), nil //nolint:forcetypeassert // fn trên trả *gen.StartCustomerImportResult
}

// GetCustomerImport (GET /api/v1/customer-imports/{importId}, customer.import): trạng thái, số đếm và lỗi từng dòng
// (không giá trị ô); không có hay của tenant khác → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCustomerImport(ctx context.Context, params gen.GetCustomerImportParams) (gen.GetCustomerImportRes, error) {
	var imp customer.Import
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		imp, err = customer.GetImport(ctx, tx, params.ImportId)
		return err //nolint:wrapcheck // lỗi của customer đã bọc
	})
	if err != nil {
		return nil, fmt.Errorf("đọc lần nhập khách: %w", err)
	}
	return customerImportOut(imp), nil
}

func customerImportOut(imp customer.Import) *gen.CustomerImport {
	out := &gen.CustomerImport{
		ID: imp.ID, Status: gen.CustomerImportStatus(imp.Status), CreatedAt: imp.CreatedAt,
		Errors: make([]gen.CustomerImportError, 0, len(imp.Errors)),
	}
	if imp.Status != customer.ImportPending {
		out.TotalRows = gen.NewOptInt32(count32(imp.TotalRows))
		out.OkRows = gen.NewOptInt32(count32(imp.OKRows))
		out.SkippedRows = gen.NewOptInt32(count32(imp.SkippedRows))
		out.ErrorRows = gen.NewOptInt32(count32(imp.ErrorRows))
	}
	if imp.FinishedAt != nil {
		out.FinishedAt = gen.NewOptDateTime(*imp.FinishedAt)
	}
	for _, e := range imp.Errors {
		ge := gen.CustomerImportError{Row: count32(e.Row), Code: e.Code}
		if e.Column != "" {
			ge.Column = gen.NewOptString(e.Column)
		}
		out.Errors = append(out.Errors, ge)
	}
	return out
}
