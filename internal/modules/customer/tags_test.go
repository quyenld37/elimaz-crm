package customer_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

func (e *writeEnv) createTag(name, color string) (customer.Tag, error) {
	var tag customer.Tag
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		var err error
		tag, err = e.w.CreateTag(e.t.Context(), tx, name, color, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	return tag, err
}

func (e *writeEnv) mustTag(name, color string) customer.Tag {
	e.t.Helper()
	tag, err := e.createTag(name, color)
	if err != nil {
		e.t.Fatal(err)
	}
	return tag
}

func (e *writeEnv) updateTag(id uuid.UUID, version int64, name, color *string) (customer.Tag, error) {
	var tag customer.Tag
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		var err error
		tag, err = e.w.UpdateTag(e.t.Context(), tx, id, version, name, color, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	return tag, err
}

func (e *writeEnv) addTag(tenant, customerID, tagID uuid.UUID, source string) error {
	return e.try(tenant, func(tx pgx.Tx) error {
		return e.w.AddTag(e.t.Context(), tx, customerID, tagID, source, e.actor, e.now)
	})
}

func (e *writeEnv) removeTag(customerID, tagID uuid.UUID) error {
	return e.try(e.tenant, func(tx pgx.Tx) error {
		return e.w.RemoveTag(e.t.Context(), tx, customerID, tagID, e.actor)
	})
}

func (e *writeEnv) links(tagID uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM customer.customer_tag_links WHERE tag_id = $1`, tagID)
}

// Tạo nhãn: bỏ khoảng trắng hai đầu, tên trùng → ErrTagNameTaken, màu ngoài 10 khóa của database → ErrInvalidInput
// (không chạm CHECK), tên rỗng/quá 50 → ErrInvalidInput; đủ 10 màu tạo được.
func TestTagCreate(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	vip := e.mustTag("  VIP ", "red")
	if vip.Name != "VIP" || vip.Color != "red" || vip.Version != 1 {
		t.Fatalf("nhãn mới: %+v", vip)
	}
	if _, err := e.createTag("VIP", "blue"); !errors.Is(err, customer.ErrTagNameTaken) {
		t.Fatalf("tên trùng: %v", err)
	}
	for _, c := range []struct{ name, color string }{
		{"Mới", "magenta"}, {"Mới", ""}, {"  ", "red"}, {strings.Repeat("ữ", 51), "red"}, {"a\x00b", "red"},
	} {
		if _, err := e.createTag(c.name, c.color); !errors.Is(err, customer.ErrInvalidInput) {
			t.Errorf("tên %q màu %q: %v", c.name, c.color, err)
		}
	}
	for _, c := range customer.TagColors() {
		if _, err := e.createTag("màu "+c, c); err != nil {
			t.Errorf("màu %s: %v", c, err)
		}
	}
	if n := len(customer.TagColors()); n != 10 {
		t.Fatalf("số màu: %d", n)
	}
	e.mustTag(strings.Repeat("ữ", 50), "gray") // đúng 50 ký tự
}

// Sửa nhãn khi version khớp (tên, màu từng phần), lệch → ErrVersionConflict, đổi sang tên đã có → ErrTagNameTaken,
// màu lạ → ErrInvalidInput, không có → ErrNotFound; ListTags theo tên; tenant khác không thấy; audit
// customer_tag.update chỉ tên trường.
func TestTagUpdate(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	vip := e.mustTag("VIP", "red")
	sale := e.mustTag("Hay đổi", "gray")

	got, err := e.updateTag(vip.ID, 1, new("Khách VIP"), nil)
	if err != nil || got.Name != "Khách VIP" || got.Color != "red" || got.Version != 2 {
		t.Fatalf("sửa tên: %+v %v", got, err)
	}
	if got, err = e.updateTag(vip.ID, 2, nil, new("purple")); err != nil || got.Color != "purple" || got.Version != 3 {
		t.Fatalf("sửa màu: %+v %v", got, err)
	}
	if _, err := e.updateTag(vip.ID, 2, new("X"), nil); !errors.Is(err, customer.ErrVersionConflict) {
		t.Fatalf("version cũ: %v", err)
	}
	if _, err := e.updateTag(sale.ID, 1, new("Khách VIP"), nil); !errors.Is(err, customer.ErrTagNameTaken) {
		t.Fatalf("đổi sang tên đã có: %v", err)
	}
	if _, err := e.updateTag(sale.ID, 1, nil, new("neon")); !errors.Is(err, customer.ErrInvalidInput) {
		t.Fatalf("màu lạ: %v", err)
	}
	if _, err := e.updateTag(uuid.New(), 1, new("X"), nil); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("không có: %v", err)
	}

	var tags []customer.Tag
	e.inTx(func(tx pgx.Tx) error {
		var err error
		tags, err = customer.ListTags(t.Context(), tx)
		return err //nolint:wrapcheck // test
	})
	if len(tags) != 2 || tags[0].Name != "Hay đổi" || tags[1].Name != "Khách VIP" {
		t.Fatalf("danh sách nhãn: %d %+v", len(tags), tags[:2])
	}
	_ = e.try(e.other, func(tx pgx.Tx) error {
		other, err := customer.ListTags(t.Context(), tx)
		if err != nil || len(other) != 0 {
			t.Errorf("tenant khác thấy %d nhãn (%v)", len(other), err)
		}
		return nil
	})
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer_tag.update'
		AND entity_type = 'customer_tag' AND entity_id = $1 AND after ? 'name' AND NOT after::text LIKE '%Khách VIP%'`,
		vip.ID.String()); n != 1 {
		t.Fatalf("audit sửa nhãn (chỉ tên trường): %d", n)
	}
}

// Gắn nhãn: vào **gốc**; gắn hai lần → một dòng; nhãn gắn vào hồ sơ con (B đã gộp vào A) → hiện trên A (Get theo
// gốc và theo con đều thấy); nhãn có ở nhiều hồ sơ của nhóm → một dòng, nguồn staff nếu có dòng gắn tay; gỡ chỉ xóa dòng
// staff của cả nhóm; nhãn rule → ErrTagRuleManaged, dòng còn; gỡ nhãn chưa gắn → không lỗi; nhãn hay khách không có
// (hay của tenant khác) → ErrNotFound; nguồn lạ → ErrInvalidInput.
func TestTagLinks(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	vip, auto := e.mustTag("VIP", "red"), e.mustTag("Hay mua", "green")
	a := e.create(customer.CreateInput{FullName: "A"})
	b := e.create(customer.CreateInput{FullName: "B"})
	e.mustMerge(a.ID, b.ID)

	for range 2 {
		if err := e.addTag(e.tenant, b.ID, vip.ID, customer.TagSourceStaff); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_tag_links WHERE tag_id = $1 AND customer_id = $2
		AND source = 'staff' AND created_by = $3`, vip.ID, a.ID, e.actor.ID); n != 1 || e.links(vip.ID) != 1 {
		t.Fatalf("gắn hai lần vào gốc: %d dòng ở gốc, %d tổng", n, e.links(vip.ID))
	}
	// Dòng ở hồ sơ con (đã gắn trước khi gộp, hay do gộp song song) vẫn hiện trên gốc.
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO customer.customer_tag_links (tenant_id, customer_id, tag_id, source)
			VALUES ($1, $2, $3, 'rule'), ($1, $2, $4, 'rule')`, e.tenant, b.ID, auto.ID, vip.ID)
		return err //nolint:wrapcheck // test
	})
	for _, id := range []uuid.UUID{a.ID, b.ID} {
		p := e.get(id)
		if len(p.Tags) != 2 || p.Tags[0].Tag.ID != auto.ID || p.Tags[0].Source != customer.TagSourceRule ||
			p.Tags[1].Tag.ID != vip.ID || p.Tags[1].Source != customer.TagSourceStaff || p.Tags[1].Tag.Name != "VIP" {
			t.Fatalf("nhãn của nhóm qua %s: %+v", id, p.Tags)
		}
	}
	if err := e.removeTag(b.ID, auto.ID); !errors.Is(err, customer.ErrTagRuleManaged) {
		t.Fatalf("gỡ nhãn rule: %v", err)
	}
	if e.links(auto.ID) != 1 {
		t.Fatal("dòng rule phải còn")
	}
	if err := e.removeTag(b.ID, vip.ID); err != nil {
		t.Fatal(err)
	}
	if p := e.get(a.ID); e.links(vip.ID) != 1 || len(p.Tags) != 2 || p.Tags[1].Source != customer.TagSourceRule {
		t.Fatalf("gỡ chỉ dòng staff, còn dòng rule: %d %+v", e.links(vip.ID), p.Tags)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action IN ('customer.tag_add', 'customer.tag_remove')
		AND entity_id = $1 AND after->>'tag_id' = $2`, a.ID.String(), vip.ID.String()); n != 2 {
		t.Fatalf("audit gắn, gỡ: %d", n)
	}
	other := e.mustTag("Khác", "blue")
	if err := e.removeTag(a.ID, other.ID); err != nil {
		t.Fatalf("gỡ nhãn chưa gắn: %v", err)
	}
	for name, err := range map[string]error{
		"nhãn không có":    e.addTag(e.tenant, a.ID, uuid.New(), customer.TagSourceStaff),
		"khách không có":   e.addTag(e.tenant, uuid.New(), vip.ID, customer.TagSourceStaff),
		"tenant khác":      e.addTag(e.other, a.ID, vip.ID, customer.TagSourceStaff),
		"gỡ nhãn không có": e.removeTag(a.ID, uuid.New()),
	} {
		if !errors.Is(err, customer.ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := e.addTag(e.tenant, a.ID, other.ID, "auto"); !errors.Is(err, customer.ErrInvalidInput) {
		t.Fatalf("nguồn lạ: %v", err)
	}
}

// Gắn cùng nhãn song song 20 lần → một dòng, không lỗi.
func TestTagAddConcurrent(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	tag := e.mustTag("VIP", "red")
	a := e.create(customer.CreateInput{FullName: "A"})
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() { errs <- e.addTag(e.tenant, a.ID, tag.ID, customer.TagSourceStaff) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := e.links(tag.ID); n != 1 {
		t.Fatalf("số dòng: %d", n)
	}
}
