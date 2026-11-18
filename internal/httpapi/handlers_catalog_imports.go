package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// importTemplateName là tên tệp khi tải mẫu nhập về.
const importTemplateName = "mau-nhap-san-pham.xlsx"

// CreateCatalogImport (POST /api/v1/catalog/imports, catalog.import) tạo bản nhập validating của người bán mặc định
// cho tệp đã tải lên và xếp lệnh kiểm → 202.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateCatalogImport(ctx context.Context, req *gen.CreateCatalogImportRequest, _ gen.CreateCatalogImportParams) (gen.CreateCatalogImportRes, error) {
	out, err := h.catalogWrite(ctx, http.StatusAccepted, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		imp, err := w.CreateImport(ctx, tx, seller, req.FileId, u)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		return catalogImportOut(imp), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CatalogImport), nil //nolint:forcetypeassert // fn trên trả *gen.CatalogImport
}

// ApplyCatalogImport (POST /api/v1/catalog/imports/{id}/apply, catalog.import) chuyển bản nhập ready của người bán
// mặc định sang applying và xếp việc áp → 202.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ApplyCatalogImport(ctx context.Context, req *gen.VersionRequest, params gen.ApplyCatalogImportParams) (gen.ApplyCatalogImportRes, error) {
	out, err := h.catalogWriteAsDefaultSeller(ctx, http.StatusAccepted,
		func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser, seller uuid.UUID) (any, error) {
			imp, err := w.ApplyImport(ctx, tx, seller, params.ID, req.Version, u)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của catalog
			}
			return catalogImportOut(imp), nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CatalogImport), nil //nolint:forcetypeassert // fn trên trả *gen.CatalogImport
}

// GetCatalogImport (GET /api/v1/catalog/imports/{id}, catalog.import): trạng thái, tóm tắt, lỗi từng dòng (không trả
// plan).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCatalogImport(ctx context.Context, params gen.GetCatalogImportParams) (gen.GetCatalogImportRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var imp catalog.Import
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		imp, err = catalog.GetImport(ctx, tx, seller, params.ID)
		return err //nolint:wrapcheck // sentinel của catalog
	})
	if err != nil {
		return nil, fmt.Errorf("đọc bản nhập: %w", err)
	}
	return catalogImportOut(imp), nil
}

// GetCatalogImportTemplate (GET /api/v1/catalog/imports/template, catalog.import): tệp mẫu Excel sản phẩm.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCatalogImportTemplate(ctx context.Context) (gen.GetCatalogImportTemplateRes, error) {
	var body []byte
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		body, err = catalog.ImportTemplate(ctx, tx)
		return err //nolint:wrapcheck // catalog đã bọc
	})
	if err != nil {
		return nil, fmt.Errorf("sinh mẫu nhập: %w", err)
	}
	return &gen.GetCatalogImportTemplateOKHeaders{
		ContentDisposition: gen.NewOptString(`attachment; filename="` + importTemplateName + `"`),
		Response:           gen.GetCatalogImportTemplateOK{Data: bytes.NewReader(body)},
	}, nil
}

func catalogImportOut(imp catalog.Import) *gen.CatalogImport {
	out := &gen.CatalogImport{
		ID: imp.ID, FileId: imp.FileID, Status: gen.CatalogImportStatus(imp.Status), Version: imp.Version,
		CreatedAt: imp.CreatedAt,
		Summary: gen.CatalogImportSummary{
			Models: count32(imp.Summary.Models), Create: count32(imp.Summary.Create), Update: count32(imp.Summary.Update),
			Unchanged: count32(imp.Summary.Unchanged),
		},
		Errors: make([]gen.CatalogImportError, 0, len(imp.Errors)),
	}
	if imp.Summary.Errors > 0 {
		out.Summary.Errors = gen.NewOptInt32(count32(imp.Summary.Errors))
	}
	for _, e := range imp.Errors {
		out.Errors = append(out.Errors, gen.CatalogImportError{Row: count32(e.Row), Column: e.Column, Code: e.Code, Message: e.Message})
	}
	if imp.DuplicateOf != nil {
		out.DuplicateOf = gen.NewOptUUID(*imp.DuplicateOf)
	}
	return out
}

// count32 đổi một số đếm (không âm, nhỏ: dòng Excel, số biến thể) sang int32 của API, chặn ở 2^31 − 1.
func count32(n int) int32 { return int32(min(max(n, 0), 1<<31-1)) }
