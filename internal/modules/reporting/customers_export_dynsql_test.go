package reporting

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Câu danh sách khách trên PostgreSQL thật: đọc theo trang 2 dòng (keyset created_at, id giảm dần; hai khách cùng giờ
// tạo) ra đúng tập và thứ tự của một lần đọc cả bảng, mới tạo trước, chỉ hồ sơ gốc; SĐT chính là số xác minh mới nhất
// của nhóm gộp, không có số xác minh thì số mới tạo nhất; nhãn gom cả nhóm, không trùng.
func TestCustomerRows_Keyset(t *testing.T) {
	t.Parallel()
	e := newSetEnv(t)
	ids := seedKeysetCustomers(t, e)
	all, paged := readCustomers(t, e, 1000), readCustomers(t, e, 2)
	if len(all) != 5 || !slices.EqualFunc(all, paged, func(a, b customerRow) bool { return a.ID == b.ID }) {
		t.Fatalf("một lần %d dòng, theo trang %d dòng", len(all), len(paged))
	}
	if !slices.IsSortedFunc(all, func(a, b customerRow) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return slices.Compare(b.ID[:], a.ID[:])
	}) {
		t.Errorf("không theo (created_at, id) giảm dần")
	}
	checkKeysetRows(t, all, ids)
}

// checkKeysetRows: SĐT nhóm gộp là số xác minh; không có số xác minh thì số mới tạo nhất; nhãn gom cả nhóm.
func checkKeysetRows(t *testing.T, all []customerRow, ids []uuid.UUID) {
	t.Helper()
	byID := map[uuid.UUID]customerRow{}
	for _, r := range all {
		byID[r.ID] = r
	}
	if p := byID[ids[0]].Phone; p == nil || *p != "+84911111111" {
		t.Errorf("SĐT nhóm gộp = %v", p)
	}
	if p := byID[ids[1]].Phone; p == nil || *p != "+84944444444" {
		t.Errorf("SĐT chưa xác minh = %v", p)
	}
	if tags := byID[ids[0]].Tags; !slices.Equal(tags, []string{"VIP"}) {
		t.Errorf("nhãn = %v", tags)
	}
	if r := byID[ids[2]]; r.Phone != nil || len(r.Tags) != 0 || r.ReturnRateBP != nil {
		t.Errorf("khách trống = %+v", r)
	}
}

// seedKeysetCustomers chèn 5 khách gốc (giờ tạo 0, 0, 1, 1, 2 phút), một hồ sơ con của khách đầu, các SĐT và nhãn VIP.
func seedKeysetCustomers(t *testing.T, e setEnv) []uuid.UUID {
	t.Helper()
	at := time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC)
	ids := make([]uuid.UUID, 5)
	for i := range ids {
		ids[i] = uuid.New()
	}
	verified := at.Add(time.Hour)
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		for i, id := range ids {
			created := at.Add(time.Duration(i/2) * time.Minute) // 0, 0, 1, 1, 2 phút: trùng giờ tạo
			if _, err := tx.Exec(t.Context(), `INSERT INTO customer.customers (id, tenant_id, root_id, full_name, lifecycle,
					created_at) VALUES ($1, $2, $1, 'K', 'customer', $3)`, id, e.tenant, created); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		// Hồ sơ con của ids[0]: hai số, một xác minh (cũ hơn) và một chưa xác minh (mới hơn) → chọn số xác minh.
		child := uuid.New()
		if _, err := tx.Exec(t.Context(), `INSERT INTO customer.customers (id, tenant_id, root_id, full_name, lifecycle)
			VALUES ($1, $2, $3, 'Con', 'customer')`, child, e.tenant, ids[0]); err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, ph := range []struct {
			id      uuid.UUID
			v       string
			ver     *time.Time
			created time.Time
		}{
			{child, "+84911111111", &verified, at},
			{ids[0], "+84922222222", nil, at.Add(time.Hour)},
			{ids[1], "+84933333333", nil, at},
			{ids[1], "+84944444444", nil, at.Add(time.Minute)},
		} {
			if _, err := tx.Exec(t.Context(), `INSERT INTO customer.customer_identities (tenant_id, customer_id, kind, value,
					verified_at, source, created_at) VALUES ($1, $2, 'phone', $3, $4, 'staff', $5)`,
				e.tenant, ph.id, ph.v, ph.ver, ph.created); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		var tag uuid.UUID
		if err := tx.QueryRow(t.Context(), `INSERT INTO customer.customer_tags (tenant_id, name, color)
			VALUES ($1, 'VIP', 'red') RETURNING id`, e.tenant).Scan(&tag); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `INSERT INTO customer.customer_tag_links (tenant_id, customer_id, tag_id, source)
			VALUES ($1, $2, $4, 'staff'), ($1, $3, $4, 'staff')`, e.tenant, ids[0], child, tag)
		return err //nolint:wrapcheck // test
	})
	return ids
}

func readCustomers(t *testing.T, e setEnv, limit int) []customerRow {
	t.Helper()
	var out []customerRow
	err := inReportTx(t, e.st.App, e.all, func(ctx context.Context, tx pgx.Tx) error {
		out = nil
		var after *customerKey
		for range 100 {
			rows, err := customerRows(ctx, tx, &CustomerExportParams{}, after, limit)
			if err != nil {
				return err
			}
			for _, r := range rows {
				if r.Name == "K" { // bỏ khách mặc định của Seed nếu có
					out = append(out, r)
				}
			}
			if len(rows) < limit {
				return nil
			}
			last := rows[len(rows)-1]
			after = &customerKey{created: last.CreatedAt, id: last.ID}
		}
		t.Fatal("không dừng")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
