package inventory_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

func staffActor() inventory.Actor { return inventory.Actor{Type: inventory.ActorStaff, ID: uuid.New()} }

func (e *invEnv) createSupplier(in inventory.SupplierInput) (inventory.Supplier, error) {
	var s inventory.Supplier
	err := e.app(func(tx pgx.Tx) error {
		var err error
		s, err = inventory.CreateSupplier(e.t.Context(), tx, in, staffActor())
		return err //nolint:wrapcheck // test
	})
	return s, err
}

func (e *invEnv) updateSupplier(id uuid.UUID, version int64, p inventory.SupplierPatch) (inventory.Supplier, error) {
	var s inventory.Supplier
	err := e.app(func(tx pgx.Tx) error {
		var err error
		s, err = inventory.UpdateSupplier(e.t.Context(), tx, id, version, p, staffActor())
		return err //nolint:wrapcheck // test
	})
	return s, err
}

func (e *invEnv) listSuppliers(q string, archived bool) []inventory.Supplier {
	e.t.Helper()
	var out []inventory.Supplier
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = inventory.ListSuppliers(e.t.Context(), tx, q, archived)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("ListSuppliers: %v", err)
	}
	return out
}

func ptr[T any](v T) *T { return &v }

// C5, Q33: danh bạ nhà cung cấp — trùng tên, chỉ phiếu nhập, đã lưu trữ, phiếu cũ vẫn đọc ra tên, SĐT được che.
func TestSupplier_OnImportOnly(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	s, err := e.createSupplier(inventory.SupplierInput{Name: "  Xưởng Hoa ", Phone: "0911 222 333", Note: "Lụa"})
	if err != nil {
		t.Fatalf("CreateSupplier: %v", err)
	}
	if s.Name != "Xưởng Hoa" || string(s.Phone) != "0911222333" || s.Status != inventory.SupplierActive || s.Version != 1 {
		t.Fatalf("nhà cung cấp tạo sai: %+v", s)
	}
	if _, err := e.createSupplier(inventory.SupplierInput{Name: "Xưởng Hoa"}); !errors.Is(err, inventory.ErrSupplierNameTaken) {
		t.Fatalf("trùng tên: mong ErrSupplierNameTaken, có %v", err)
	}
	for _, bad := range []inventory.SupplierInput{{Name: ""}, {Name: "A", Phone: "12ab"}, {Name: strings.Repeat("x", 201)}} {
		if _, err := e.createSupplier(bad); !errors.Is(err, inventory.ErrInvalidInput) {
			t.Errorf("dữ liệu sai %+v: mong ErrInvalidInput, có %v", bad, err)
		}
	}

	// Phiếu nhập dùng được; phiếu xuất có nhà cung cấp bị từ chối (Go và CHECK của database).
	v := e.stock(5)
	imp := e.newDoc(inventory.DocumentInput{Kind: inventory.KindImport, SupplierID: &s.ID}, line(v, 3))
	if _, err := e.createDoc(inventory.DocumentInput{Kind: inventory.KindExport, SupplierID: &s.ID}); !errors.Is(err, inventory.ErrSupplierNotForKind) {
		t.Fatalf("phiếu xuất có NCC: mong ErrSupplierNotForKind, có %v", err)
	}
	err = e.app(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO inventory.stock_documents (tenant_id, warehouse_id, code, kind, supplier_id)
			VALUES ($1, $2, 'XK-TEST', 'export', $3)`, e.tenant, e.warehouse, s.ID)
		return err //nolint:wrapcheck // test
	})
	if pe, ok := errors.AsType[*pgconn.PgError](err); !ok || pe.Code != "23514" {
		t.Fatalf("CHECK supplier_id chỉ cho import: mong 23514, có %v", err)
	}

	// Lưu trữ: không dùng cho phiếu mới; phiếu cũ vẫn đọc ra tên.
	if _, err := e.updateSupplier(s.ID, 2, inventory.SupplierPatch{Status: ptr(inventory.SupplierArchived)}); !errors.Is(err, inventory.ErrVersionConflict) {
		t.Fatalf("version cũ: mong ErrVersionConflict, có %v", err)
	}
	arch, err := e.updateSupplier(s.ID, 1, inventory.SupplierPatch{Status: ptr(inventory.SupplierArchived)})
	if err != nil || arch.Status != inventory.SupplierArchived || arch.Version != 2 || arch.Name != "Xưởng Hoa" {
		t.Fatalf("lưu trữ: %+v %v", arch, err)
	}
	if _, err := e.createDoc(inventory.DocumentInput{Kind: inventory.KindImport, SupplierID: &s.ID}); !errors.Is(err, inventory.ErrSupplierArchived) {
		t.Fatalf("NCC đã lưu trữ: mong ErrSupplierArchived, có %v", err)
	}
	if got := e.getDoc(imp.ID); got.SupplierName != "Xưởng Hoa" || got.SupplierID == nil || *got.SupplierID != s.ID {
		t.Fatalf("phiếu cũ phải đọc ra tên NCC đã lưu trữ: %+v", got)
	}
	if _, err := e.createDoc(inventory.DocumentInput{Kind: inventory.KindImport, SupplierID: ptr(uuid.New())}); !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("NCC không có: mong ErrNotFound, có %v", err)
	}
}

// Danh sách (tìm bỏ dấu, lọc lưu trữ), đổi tên trùng, SĐT che trong log, tenant khác không thấy.
func TestSupplier_ListAndRedact(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	s, err := e.createSupplier(inventory.SupplierInput{Name: "Xưởng Hoa", Phone: "0911222333"})
	if err != nil {
		t.Fatal(err)
	}
	arch, err := e.updateSupplier(s.ID, 1, inventory.SupplierPatch{Status: ptr(inventory.SupplierArchived)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.createSupplier(inventory.SupplierInput{Name: "Xưởng May Bình"}); err != nil {
		t.Fatal(err)
	}
	if got := e.listSuppliers("xuong", false); len(got) != 1 || got[0].Name != "Xưởng May Bình" {
		t.Fatalf("tìm không gồm lưu trữ: %+v", got)
	}
	if got := e.listSuppliers("XUONG", true); len(got) != 2 {
		t.Fatalf("tìm gồm lưu trữ: %+v", got)
	}
	if got := e.listSuppliers("", true); len(got) != 2 || got[0].Name > got[1].Name {
		t.Fatalf("danh sách theo tên: %+v", got)
	}
	if _, err := e.updateSupplier(arch.ID, 2, inventory.SupplierPatch{Name: ptr("Xưởng May Bình")}); !errors.Is(err, inventory.ErrSupplierNameTaken) {
		t.Fatalf("đổi tên trùng: mong ErrSupplierNameTaken, có %v", err)
	}
	if _, err := e.updateSupplier(uuid.New(), 1, inventory.SupplierPatch{}); !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("NCC không có: mong ErrNotFound, có %v", err)
	}

	// SĐT là dữ liệu cá nhân: log của cả Supplier không chứa số.
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("ncc", slog.Any("supplier", s), slog.Any("phone", s.Phone))
	if strings.Contains(buf.String(), "0911222333") {
		t.Fatalf("log lộ SĐT: %s", buf.String())
	}

	// Tenant khác không thấy.
	other := newInvEnvOn(t, e.staff, "B")
	if got := other.listSuppliers("", true); len(got) != 0 {
		t.Fatalf("tenant B thấy NCC của A: %+v", got)
	}
}
