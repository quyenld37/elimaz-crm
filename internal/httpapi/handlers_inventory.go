package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Kho: nhà cung cấp và chứng từ kho (kế hoạch M1, Task M1-29). Quyền inventory.* (trừ suppliers.manage) là quyền theo
// kho: guard chỉ kiểm có grant; handler kiểm đích bằng identity.Can với Scope{warehouse, <kho>} — kho của yêu cầu
// (tạo phiếu) hoặc kho của phiếu (đọc trong cùng transaction). Audit ghi ở đây (inventory không được nhập identity).

const (
	permInvRead     = identity.Permission("inventory.read")
	permDocCreate   = identity.Permission("inventory.documents.create")
	permDocPost     = identity.Permission("inventory.documents.post")
	defaultDocLimit = 50
)

func warehouseScope(id uuid.UUID) *identity.Scope {
	return &identity.Scope{Type: identity.ScopeWarehouse, ID: id.String()}
}

// canAt kiểm u có mọi quyền perms ở kho warehouseID; thiếu → errForbiddenTarget.
func canAt(u identity.AuthUser, warehouseID uuid.UUID, perms ...identity.Permission) error {
	for _, p := range perms {
		if !identity.Can(u, p, warehouseScope(warehouseID)) {
			return errForbiddenTarget
		}
	}
	return nil
}

// documentAt đọc phiếu id (trong tx) và kiểm u có mọi quyền perms ở kho của phiếu.
func documentAt(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID, perms ...identity.Permission) (inventory.Document, error) {
	d, err := inventory.GetDocument(ctx, tx, id)
	if err != nil {
		return inventory.Document{}, err //nolint:wrapcheck // sentinel của inventory
	}
	return d, canAt(u, d.WarehouseID, perms...)
}

func staffActor(u identity.AuthUser) inventory.Actor {
	return inventory.Actor{Type: inventory.ActorStaff, ID: u.UserID}
}

// ListWarehouses (GET /api/v1/inventory/warehouses, inventory.read): chỉ kho người gọi có inventory.read (toàn shop
// hoặc đúng kho đó).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListWarehouses(ctx context.Context) (gen.ListWarehousesRes, error) {
	out := gen.ListWarehousesOKApplicationJSON{}
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) error {
		ws, err := inventory.ListWarehouses(ctx, tx)
		for _, w := range ws {
			if canAt(u, w.ID, permInvRead) == nil {
				out = append(out, gen.Warehouse{ID: w.ID, Code: w.Code, Name: w.Name, Status: gen.WarehouseStatus(w.Status)})
			}
		}
		return err //nolint:wrapcheck // inventory đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc danh sách kho: %w", err)
	}
	return &out, nil
}

// ListSuppliers (GET /api/v1/inventory/suppliers, inventory.read không cần đích).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListSuppliers(ctx context.Context, params gen.ListSuppliersParams) (gen.ListSuppliersRes, error) {
	var ss []inventory.Supplier
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, _ identity.AuthUser) (err error) {
		ss, err = inventory.ListSuppliers(ctx, tx, params.Q.Value, params.IncludeArchived.Value)
		return err //nolint:wrapcheck // inventory đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc nhà cung cấp: %w", err)
	}
	out := make(gen.ListSuppliersOKApplicationJSON, 0, len(ss))
	for _, s := range ss {
		out = append(out, *supplierOut(s))
	}
	return &out, nil
}

// CreateSupplier (POST /api/v1/inventory/suppliers, inventory.suppliers.manage) → 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateSupplier(ctx context.Context, req *gen.CreateSupplierRequest, _ gen.CreateSupplierParams) (gen.CreateSupplierRes, error) {
	in := inventory.SupplierInput{Name: req.Name, Phone: req.Phone.Value, Note: req.Note.Value}
	out, err := h.inventoryWrite(ctx, http.StatusCreated, func(tx pgx.Tx, _ inventory.Writer, u identity.AuthUser) (any, error) {
		s, err := inventory.CreateSupplier(ctx, tx, in, staffActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		return supplierOut(s), h.audit(ctx, tx, u, "inventory.supplier.create", "inventory.supplier", s.ID, nil, supplierAudit(s))
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Supplier), nil //nolint:forcetypeassert // fn trên trả *gen.Supplier
}

// UpdateSupplier (PATCH /api/v1/inventory/suppliers/{id}, inventory.suppliers.manage): sửa, lưu trữ khi version khớp.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateSupplier(ctx context.Context, req *gen.UpdateSupplierRequest, params gen.UpdateSupplierParams) (gen.UpdateSupplierRes, error) {
	p := inventory.SupplierPatch{
		Name: optString(req.Name.Set, req.Name.Value), Phone: optString(req.Phone.Set, req.Phone.Value),
		Note: optString(req.Note.Set, req.Note.Value), Status: optString(req.Status.Set, string(req.Status.Value)),
	}
	out, err := h.inventoryWrite(ctx, http.StatusOK, func(tx pgx.Tx, _ inventory.Writer, u identity.AuthUser) (any, error) {
		s, err := inventory.UpdateSupplier(ctx, tx, params.ID, req.Version, p, staffActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		return supplierOut(s), h.audit(ctx, tx, u, "inventory.supplier.update", "inventory.supplier", s.ID, nil, supplierAudit(s))
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Supplier), nil //nolint:forcetypeassert // fn trên trả *gen.Supplier
}

// CreateStockDocument (POST /api/v1/inventory/documents, inventory.documents.create theo kho; phiếu điều chỉnh cần cả
// inventory.documents.post) → 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateStockDocument(ctx context.Context, req *gen.CreateStockDocumentRequest, _ gen.CreateStockDocumentParams) (gen.CreateStockDocumentRes, error) {
	in := inventory.DocumentInput{WarehouseID: req.WarehouseId, Kind: string(req.Kind), Note: req.Note.Value}
	if req.SupplierId.Set {
		in.SupplierID = &req.SupplierId.Value
	}
	perms := []identity.Permission{permDocCreate}
	if in.Kind == inventory.KindAdjust {
		perms = append(perms, permDocPost)
	}
	out, err := h.inventoryWrite(ctx, http.StatusCreated, func(tx pgx.Tx, _ inventory.Writer, u identity.AuthUser) (any, error) {
		if err := canAt(u, in.WarehouseID, perms...); err != nil {
			return nil, err
		}
		d, err := inventory.CreateDocument(ctx, tx, in, staffActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		return documentOut(d), h.audit(ctx, tx, u, "inventory.document.create", "inventory.stock_document", d.ID, nil, documentAudit(d))
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.StockDocument), nil //nolint:forcetypeassert // fn trên trả *gen.StockDocument
}

// ReplaceStockDocumentLines (PUT …/documents/{id}/lines, inventory.documents.create ở kho của phiếu; điều chỉnh: cả
// inventory.documents.post).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ReplaceStockDocumentLines(ctx context.Context, req *gen.ReplaceStockDocumentLinesRequest,
	params gen.ReplaceStockDocumentLinesParams,
) (gen.ReplaceStockDocumentLinesRes, error) {
	lines := make([]inventory.DocumentLine, 0, len(req.Lines))
	for _, l := range req.Lines {
		lines = append(lines, inventory.DocumentLine{VariantID: l.VariantId, Quantity: l.Quantity, UnitCost: optMoney(l.UnitCost)})
	}
	out, err := h.inventoryWrite(ctx, http.StatusOK, func(tx pgx.Tx, _ inventory.Writer, u identity.AuthUser) (any, error) {
		cur, err := inventory.GetDocument(ctx, tx, params.ID)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		perms := []identity.Permission{permDocCreate}
		if cur.Kind == inventory.KindAdjust {
			perms = append(perms, permDocPost)
		}
		if err := canAt(u, cur.WarehouseID, perms...); err != nil {
			return nil, err
		}
		d, err := inventory.ReplaceLines(ctx, tx, params.ID, req.Version, lines)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		return documentOut(d), h.audit(ctx, tx, u, "inventory.document.lines", "inventory.stock_document", d.ID,
			documentAudit(cur), documentAudit(d))
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.StockDocument), nil //nolint:forcetypeassert // fn trên trả *gen.StockDocument
}

// PostStockDocument (POST …/documents/{id}/post, inventory.documents.post ở kho của phiếu): ghi sổ, đổi tồn.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) PostStockDocument(ctx context.Context, req *gen.StockDocumentVersion, params gen.PostStockDocumentParams) (gen.PostStockDocumentRes, error) {
	out, err := h.inventoryWrite(ctx, http.StatusOK, func(tx pgx.Tx, w inventory.Writer, u identity.AuthUser) (any, error) {
		cur, err := documentAt(ctx, tx, u, params.ID, permDocPost)
		if err != nil {
			return nil, err
		}
		d, err := w.PostDocument(ctx, tx, params.ID, req.Version, staffActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		return documentOut(d), h.audit(ctx, tx, u, "inventory.document.post", "inventory.stock_document", d.ID,
			documentAudit(cur), documentAudit(d))
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.StockDocument), nil //nolint:forcetypeassert // fn trên trả *gen.StockDocument
}

// CancelStockDocument (POST …/documents/{id}/cancel, inventory.documents.create ở kho của phiếu): hủy phiếu nháp.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CancelStockDocument(ctx context.Context, req *gen.StockDocumentVersion, params gen.CancelStockDocumentParams) (gen.CancelStockDocumentRes, error) {
	out, err := h.inventoryWrite(ctx, http.StatusOK, func(tx pgx.Tx, _ inventory.Writer, u identity.AuthUser) (any, error) {
		cur, err := documentAt(ctx, tx, u, params.ID, permDocCreate)
		if err != nil {
			return nil, err
		}
		if err := inventory.CancelDocument(ctx, tx, params.ID, req.Version, staffActor(u)); err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		d, err := inventory.GetDocument(ctx, tx, params.ID)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của inventory
		}
		return documentOut(d), h.audit(ctx, tx, u, "inventory.document.cancel", "inventory.stock_document", d.ID,
			documentAudit(cur), documentAudit(d))
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.StockDocument), nil //nolint:forcetypeassert // fn trên trả *gen.StockDocument
}

// GetStockDocument (GET …/documents/{id}, inventory.read ở kho của phiếu).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetStockDocument(ctx context.Context, params gen.GetStockDocumentParams) (gen.GetStockDocumentRes, error) {
	var d inventory.Document
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		if d, err = inventory.GetDocument(ctx, tx, params.ID); err != nil {
			return err //nolint:wrapcheck // sentinel của inventory
		}
		return canAt(u, d.WarehouseID, permInvRead)
	}); err != nil {
		return nil, fmt.Errorf("đọc phiếu kho: %w", err)
	}
	return documentOut(d), nil
}

// ListStockDocuments (GET /api/v1/inventory/documents, inventory.read): không có warehouseId → mọi kho người gọi được
// xem; có warehouseId mà không có quyền ở kho đó → 403.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListStockDocuments(ctx context.Context, params gen.ListStockDocumentsParams) (gen.ListStockDocumentsRes, error) {
	f := inventory.DocumentFilter{
		Status: string(params.Status.Value), Kind: string(params.Kind.Value), Limit: params.Limit.Or(defaultDocLimit),
	}
	if params.Cursor.Set {
		f.Cursor = &params.Cursor.Value
	}
	var ds []inventory.Document
	var next *uuid.UUID
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		if params.WarehouseId.Set {
			if err := canAt(u, params.WarehouseId.Value, permInvRead); err != nil {
				return err
			}
			f.WarehouseIDs = []uuid.UUID{params.WarehouseId.Value}
		} else {
			f.AllWarehouses, f.WarehouseIDs = readableWarehouses(u, permInvRead)
		}
		ds, next, err = inventory.ListDocuments(ctx, tx, f)
		return err //nolint:wrapcheck // inventory đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc danh sách phiếu kho: %w", err)
	}
	page := &gen.StockDocumentPage{Items: make([]gen.StockDocument, 0, len(ds))}
	for _, d := range ds {
		page.Items = append(page.Items, *documentOut(d))
	}
	if next != nil {
		page.NextCursor = gen.NewOptUUID(*next)
	}
	return page, nil
}

// readableWarehouses: kho người gọi có quyền perm (grant all → mọi kho; mã đích không phải uuid bị bỏ).
func readableWarehouses(u identity.AuthUser, perm identity.Permission) (bool, []uuid.UUID) {
	all, ids := identity.ScopesFor(u, perm)
	if all {
		return true, nil
	}
	out := make([]uuid.UUID, 0, len(ids))
	for _, s := range ids {
		if id, err := uuid.Parse(s); err == nil {
			out = append(out, id)
		}
	}
	return false, out
}

// inventoryRead chạy read trong tenancy.WithTenant của người gọi (db.TxAPI).
func (h *handler) inventoryRead(ctx context.Context, read func(tx pgx.Tx, u identity.AuthUser) error) error {
	u, ok := authUser(ctx)
	if !ok {
		return errNoAuthUser
	}
	return tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error { return read(tx, u) }, db.TxAPI) //nolint:wrapcheck // lỗi của read
}

// inventoryWrite chạy change trong refusableWrite với một inventory.Writer của yêu cầu: lỗi nghiệp vụ của inventory và
// thiếu quyền ở kho là câu trả lời không đổi gì (không lưu vào khóa chống trùng).
func (h *handler) inventoryWrite(ctx context.Context, status int,
	change func(tx pgx.Tx, w inventory.Writer, u identity.AuthUser) (any, error),
) (any, error) {
	w := inventory.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	return h.refusableWrite(ctx, status, isInventoryRefusal, "ghi kho", func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		return change(tx, w, u)
	})
}

func isInventoryRefusal(err error) bool {
	_, _, ok := inventoryError(err)
	return ok
}

// audit ghi một dòng audit của nhân viên u trong tx (không dữ liệu cá nhân).
func (h *handler) audit(ctx context.Context, tx pgx.Tx, u identity.AuthUser, action, entityType string, id uuid.UUID,
	before, after map[string]any,
) error {
	return recordAudit(ctx, tx, identity.AuditEntry{
		TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), Action: action,
		EntityType: entityType, EntityID: id.String(), Before: before, After: after, RequestID: RequestID(ctx),
	})
}

// recordAudit ghi dòng audit e trong tx (h.audit của nhân viên, auditService của khóa dịch vụ).
func recordAudit(ctx context.Context, tx pgx.Tx, e identity.AuditEntry) error {
	if err := identity.RecordAudit(ctx, tx, e); err != nil {
		return fmt.Errorf("ghi audit %s: %w", e.Action, err)
	}
	return nil
}

// supplierAudit: không ghi SĐT, ghi chú (dữ liệu cá nhân có thể có), chỉ có hay không.
func supplierAudit(s inventory.Supplier) map[string]any {
	return map[string]any{
		"name": s.Name, "status": s.Status, "version": s.Version, "hasPhone": s.Phone != "", "noteLength": len([]rune(s.Note)),
	}
}

func documentAudit(d inventory.Document) map[string]any {
	m := map[string]any{
		"code": d.Code, "kind": d.Kind, "status": d.Status, "warehouseId": d.WarehouseID.String(), "version": d.Version,
		"lines": len(d.Lines), "supplierId": nil,
	}
	if d.SupplierID != nil {
		m["supplierId"] = d.SupplierID.String()
	}
	return m
}

func supplierOut(s inventory.Supplier) *gen.Supplier {
	return &gen.Supplier{
		ID: s.ID, Name: s.Name, Phone: string(s.Phone), Note: s.Note, Status: gen.SupplierStatus(s.Status), Version: s.Version,
	}
}

func documentOut(d inventory.Document) *gen.StockDocument {
	out := &gen.StockDocument{
		ID: d.ID, Code: d.Code, WarehouseId: d.WarehouseID, Kind: gen.StockDocumentKind(d.Kind),
		Status: gen.StockDocumentStatus(d.Status), SupplierName: d.SupplierName, Note: d.Note, Version: d.Version,
		CreatedAt: d.CreatedAt, Lines: make([]gen.StockDocumentLine, 0, len(d.Lines)),
	}
	if d.SupplierID != nil {
		out.SupplierId = gen.NewOptUUID(*d.SupplierID)
	}
	if d.PostedAt != nil {
		out.PostedAt = gen.NewOptDateTime(*d.PostedAt)
	}
	for _, l := range d.Lines {
		gl := gen.StockDocumentLine{VariantId: l.VariantID, Quantity: l.Quantity}
		if l.UnitCost != nil {
			gl.UnitCost = gen.NewOptMoney(gen.Money(*l.UnitCost))
		}
		out.Lines = append(out.Lines, gl)
	}
	return out
}
