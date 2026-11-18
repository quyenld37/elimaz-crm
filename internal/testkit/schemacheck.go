package testkit

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Column là tên và kiểu (information_schema.columns.data_type) của một cột.
type Column struct{ Name, Type string }

// Columns trả các cột của bảng hay view schema.rel theo ordinal_position, đọc bằng owner; lỗi làm test dừng.
func Columns(t testing.TB, owner *db.DB, schema, rel string) []Column {
	t.Helper()
	rows, err := owner.Pool.Query(t.Context(), `
		SELECT column_name::text, data_type::text FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position`, schema, rel)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Column, error) {
		var c Column
		return c, r.Scan(&c.Name, &c.Type)
	})
	if err != nil {
		t.Fatal(err)
	}
	return cols
}

// CheckViews kiểm mỗi view schema.<tên> của want có đúng cột (chuỗi "a, b, c" theo thứ tự, như docs/views.md) và có
// security_invoker (ADR-27); sai thì test đỏ.
func CheckViews(t testing.TB, owner *db.DB, schema string, want map[string]string) {
	t.Helper()
	for view, cols := range want {
		var names []string
		for _, c := range Columns(t, owner, schema, view) {
			names = append(names, c.Name)
		}
		if got := strings.Join(names, ", "); got != cols {
			t.Errorf("%s.%s: cột %q, mong %q", schema, view, got, cols)
		}
		var invoker bool
		if err := owner.Pool.QueryRow(t.Context(), `
			SELECT coalesce('security_invoker=true' = ANY (c.reloptions), false) FROM pg_class c
			WHERE c.oid = ($1 || '.' || $2)::regclass`, schema, view).Scan(&invoker); err != nil {
			t.Fatal(err)
		}
		if !invoker {
			t.Errorf("%s.%s thiếu security_invoker", schema, view)
		}
	}
}

// CheckSchemaRulesClean đòi tenancy.CheckSchemaRules trên database của owner trả rỗng (bảng platform không theo tenant:
// tenant_routes, jobs).
func CheckSchemaRulesClean(t testing.TB, owner *db.DB) {
	t.Helper()
	got, err := tenancy.CheckSchemaRules(t.Context(), owner.Pool, []string{"platform.tenant_routes", "platform.jobs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("CheckSchemaRules phải rỗng, có:\n%s", strings.Join(got, "\n"))
	}
}

// CheckOwnerAppendOnly chạy từng câu sửa, xóa bằng chủ bảng trong ngữ cảnh tenant và đòi lỗi "append-only" (trigger
// theo câu lệnh của bảng chỉ ghi thêm chặn cả chủ bảng, mã 42501, thông báo có "append-only").
func (s *Staff) CheckOwnerAppendOnly(t testing.TB, tenant uuid.UUID, queries ...string) {
	t.Helper()
	ctx := t.Context()
	for _, q := range queries {
		err := s.TryExec(ctx, tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, q)
			return err //nolint:wrapcheck // test
		})
		pe, ok := errors.AsType[*pgconn.PgError](err)
		if !ok || pe.Code != "42501" || !strings.Contains(pe.Message, "append-only") {
			t.Errorf("chủ bảng %q: %v, mong lỗi append-only", q, err)
		}
	}
}
