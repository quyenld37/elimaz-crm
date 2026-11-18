package customer_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

func (e *writeEnv) addNote(customerID uuid.UUID, body string, at time.Time) (customer.Note, error) {
	var n customer.Note
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		var err error
		n, err = e.w.AddNote(e.t.Context(), tx, customerID, body, e.actor, at)
		return err //nolint:wrapcheck // test
	})
	return n, err
}

func (e *writeEnv) mustNote(customerID uuid.UUID, body string, at time.Time) customer.Note {
	e.t.Helper()
	n, err := e.addNote(customerID, body, at)
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

// Ghi chú: lưu vào đúng hồ sơ được mở (hồ sơ con B, không dời về gốc — 05 mục 1, B5), người viết, lúc viết; audit customer.note_add chỉ changed_fields ["note"],
// không có nội dung; rỗng, quá 4000 ký tự → ErrInvalidInput; khách không có → ErrNotFound; UPDATE,
// DELETE ghi chú (kể cả chủ bảng) → lỗi, nội dung không đổi.
func TestAddNoteAppendOnly(t *testing.T) {
	e := newWriteEnv(t)
	a := e.create(customer.CreateInput{FullName: "A"})
	b := e.create(customer.CreateInput{FullName: "B"})
	e.mustMerge(a.ID, b.ID)
	const secret = "Khách dặn gọi 0909123456 sau 18h"
	n := e.mustNote(b.ID, "  "+secret+"\n", e.now)
	if n.CustomerID != b.ID || string(n.Body) != secret || n.AuthorID != e.actor.ID || !n.CreatedAt.Equal(e.now) {
		t.Fatalf("ghi chú: %+v", n)
	}
	if c := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.note_add' AND entity_id = $1
		AND changed_fields = '{note}' AND actor_id = $2`, a.ID.String(), e.actor.ID.String()); c != 1 {
		t.Fatalf("audit ghi chú: %d", c)
	}
	if c := e.count(`SELECT count(*) FROM identity.audit_logs WHERE before::text LIKE '%0909%' OR after::text LIKE '%0909%'
		OR after::text LIKE '%18h%'`); c != 0 {
		t.Fatalf("audit chứa nội dung ghi chú: %d", c)
	}
	for _, body := range []string{"", " \n ", strings.Repeat("a", 4001)} {
		if _, err := e.addNote(a.ID, body, e.now); !errors.Is(err, customer.ErrInvalidInput) {
			t.Errorf("thân %d ký tự: %v", len(body), err)
		}
	}
	if _, err := e.addNote(a.ID, strings.Repeat("ữ", 4000), e.now); err != nil {
		t.Fatalf("đúng 4000 ký tự: %v", err)
	}
	if _, err := e.addNote(uuid.New(), "x", e.now); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("khách không có: %v", err)
	}
	for _, q := range []string{
		`UPDATE customer.customer_notes SET body = 'đã sửa' WHERE id = $1`,
		`DELETE FROM customer.customer_notes WHERE id = $1`,
	} {
		if err := e.try(e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), q, n.ID)
			return err //nolint:wrapcheck // test
		}); err == nil {
			t.Errorf("crm_app chạy được %q", q)
		}
		e.staff.CheckOwnerAppendOnly(t, e.tenant, strings.ReplaceAll(q, "$1", "'"+n.ID.String()+"'"))
	}
	if c := e.count(`SELECT count(*) FROM customer.customer_notes WHERE id = $1 AND body = $2`, n.ID, secret); c != 1 {
		t.Fatal("ghi chú bị đổi")
	}
}
