package customer_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
)

func (e *writeEnv) setDNC(tenant, id uuid.UUID, on bool) error {
	return e.try(tenant, func(tx pgx.Tx) error {
		return e.w.SetDoNotContact(e.t.Context(), tx, id, on, e.actor, e.now)
	})
}

func (e *writeEnv) isDNC(id uuid.UUID) bool {
	e.t.Helper()
	var on bool
	e.inTx(func(tx pgx.Tx) error {
		var err error
		on, err = customer.IsDoNotContact(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	})
	return on
}

func (e *writeEnv) nameFromChannel(id uuid.UUID, name string) (bool, error) {
	var changed bool
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		var err error
		changed, err = e.w.SetNameFromChannel(e.t.Context(), tx, id, name)
		return err //nolint:wrapcheck // test
	})
	return changed, err
}

// Migration 0403 (R3): cột của customers_v trước là tiền tố đúng thứ tự của cột sau; hai cột cuối là do_not_contact,
// name_source. Khách có sẵn nhận do_not_contact = false, name_source = 'staff'.
func TestMigration0403CustomersView(t *testing.T) {
	t.Parallel()
	_, ownerURL, _ := testkit.CreateDatabase(t)
	testkit.MigrateBefore(t, ownerURL, "0403")
	owner := testkit.OpenDB(t, ownerURL)
	var tenant uuid.UUID
	if err := owner.Pool.QueryRow(t.Context(), "INSERT INTO platform.tenants (name) VALUES ('A') RETURNING id").
		Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO customer.customers (tenant_id, full_name, name_search, lifecycle)
			VALUES ($1, 'Chị Lan', 'chi lan', 'lead')`, tenant)
		return err //nolint:wrapcheck // test
	})
	before := testkit.Columns(t, owner, "customer", "customers_v")
	if _, err := db.Migrate(t.Context(), ownerURL); err != nil {
		t.Fatalf("migrate 0403: %v", err)
	}
	after := testkit.Columns(t, owner, "customer", "customers_v")
	// Mốc sau nối cột sau name_source (R3; 0605 của M6 Task 238 có test riêng): chỉ xét tới hai cột của 0403.
	if len(after) < len(before)+2 {
		t.Fatalf("customers_v trước %v, sau %v", before, after)
	}
	after = after[:len(before)+2]
	expectColumnsAppended(t, before, after, 2)
	if got := after[len(before):]; got[0].Name != "do_not_contact" || got[1].Name != "name_source" {
		t.Errorf("hai cột cuối: %v", got)
	}
	var n int
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM customer.customers_v
			WHERE do_not_contact = false AND name_source = 'staff'`).Scan(&n)
	})
	if n != 1 {
		t.Errorf("khách có sẵn sau 0403: %d", n)
	}
}

// Cờ "không liên hệ" theo hồ sơ đại diện: bật trên A; B gộp vào A → B cũng bị chặn; hoàn tác gộp → B hết bị chặn.
// Bật trên hồ sơ con (B đang gộp) là bật trên gốc A. Bật lại khi đã bật → không ghi thêm audit, version giữ nguyên.
// Audit chỉ tên trường; tenant khác → ErrNotFound.
func TestDoNotContactFollowsRoot(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a, b := e.pair("Chị Lan Gốc", "Chị Lan Phụ", "")
	if err := e.setDNC(e.tenant, a.ID, true); err != nil {
		t.Fatal(err)
	}
	if !e.isDNC(a.ID) || e.isDNC(b.ID) {
		t.Fatalf("trước gộp: A %v, B %v", e.isDNC(a.ID), e.isDNC(b.ID))
	}
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE id = $1 AND do_not_contact AND do_not_contact_at = $2
		AND version = 2`, a.ID, e.now); n != 1 {
		t.Errorf("dòng A sau khi bật: %d", n)
	}
	if err := e.setDNC(e.tenant, a.ID, true); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.do_not_contact' AND entity_id = $1
		AND changed_fields = ARRAY['do_not_contact']`, a.ID.String()); n != 1 {
		t.Errorf("audit customer.do_not_contact: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.do_not_contact'
		AND row_to_json(audit_logs)::text LIKE '%Lan%'`); n != 0 {
		t.Errorf("audit chứa tên: %d", n)
	}

	m := e.mustMerge(a.ID, b.ID)
	if !e.isDNC(b.ID) {
		t.Error("B đã gộp vào A phải bị chặn")
	}
	if err := e.undo(m.ID, e.now); err != nil {
		t.Fatal(err)
	}
	if e.isDNC(b.ID) || !e.isDNC(a.ID) {
		t.Errorf("sau hoàn tác: A %v, B %v", e.isDNC(a.ID), e.isDNC(b.ID))
	}

	// Tắt qua hồ sơ con khi đang gộp → tắt trên gốc A.
	e.mustMerge(a.ID, b.ID)
	if err := e.setDNC(e.tenant, b.ID, false); err != nil {
		t.Fatal(err)
	}
	if e.isDNC(a.ID) || e.count(`SELECT count(*) FROM customer.customers WHERE id = $1 AND do_not_contact_at IS NULL`, a.ID) != 1 {
		t.Error("tắt qua hồ sơ con không tắt gốc")
	}

	if err := e.setDNC(e.other, a.ID, true); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("tenant khác: %v", err)
	}
	if err := e.setDNC(e.tenant, uuid.New(), true); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("khách không có: %v", err)
	}
}

// Tên từ kênh (CH08): placeholder → đổi, name_source channel, name_search theo tên mới, version tăng; gọi lại tên khác →
// đổi; cùng tên → không đổi; hồ sơ nhân viên tạo hay nhân viên đã sửa tên (staff) → không bao giờ bị đè.
func TestSetNameFromChannel(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	c := e.mustFindOrCreate(psid("page1", "u-1"))
	if changed, err := e.nameFromChannel(c.CustomerID, "Nguyễn Lan Anh"); err != nil || !changed {
		t.Fatalf("placeholder → tên kênh: %v, %v", changed, err)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE id = $1 AND full_name = 'Nguyễn Lan Anh'
		AND name_search = 'nguyen lan anh' AND name_source = 'channel' AND version = 2`, c.CustomerID); n != 1 {
		t.Errorf("sau lần đầu: %d", n)
	}
	if changed, err := e.nameFromChannel(c.CustomerID, "Lan Chi"); err != nil || !changed {
		t.Fatalf("tên kênh khác: %v, %v", changed, err)
	}
	if changed, err := e.nameFromChannel(c.CustomerID, " Lan Chi "); err != nil || changed {
		t.Errorf("cùng tên: %v, %v", changed, err)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE id = $1 AND full_name = 'Lan Chi' AND version = 3`,
		c.CustomerID); n != 1 {
		t.Errorf("sau lần hai: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.name_from_channel'
		AND entity_id = $1 AND changed_fields = ARRAY['full_name'] AND actor_type = 'system'`, c.CustomerID.String()); n != 2 {
		t.Errorf("audit tên từ kênh: %d", n)
	}

	// Nhân viên sửa tên → staff; tên kênh không đè.
	name := "Chị Chi VIP"
	e.inTx(func(tx pgx.Tx) error {
		_, err := e.w.Update(t.Context(), tx, c.CustomerID, 3, customer.UpdateInput{FullName: &name}, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	if changed, err := e.nameFromChannel(c.CustomerID, "Lan Chi Mới"); err != nil || changed {
		t.Errorf("hồ sơ nhân viên đã sửa: %v, %v", changed, err)
	}
	staff := e.create(customer.CreateInput{FullName: "Anh Bình"})
	if changed, err := e.nameFromChannel(staff.ID, "Bình FB"); err != nil || changed {
		t.Errorf("hồ sơ nhân viên tạo: %v, %v", changed, err)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE (id = $1 AND full_name = 'Chị Chi VIP' AND name_source = 'staff')
		OR (id = $2 AND full_name = 'Anh Bình' AND name_source = 'staff')`, c.CustomerID, staff.ID); n != 2 {
		t.Errorf("tên nhân viên bị đè: %d", n)
	}
	if _, err := e.nameFromChannel(c.CustomerID, strings.Repeat("a", 121)); !errors.Is(err, customer.ErrInvalidInput) {
		t.Errorf("tên quá dài: %v", err)
	}
	if _, err := e.nameFromChannel(uuid.New(), "X"); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("khách không có: %v", err)
	}
}
