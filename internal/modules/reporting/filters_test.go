package reporting

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

func runFilters(t *testing.T, e setEnv, s DataScope) FilterOptions {
	t.Helper()
	var out FilterOptions
	if err := inReportTx(t, e.st.App, s, func(ctx context.Context, tx pgx.Tx) error {
		f, err := Filters(ctx, tx, s)
		out = f
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func optionIDs(opts []FilterOption) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.ID)
	}
	return out
}

// Lựa chọn của bộ lọc (kế hoạch M6, Task 230): người toàn shop thấy mọi Page, nguồn của đơn và nguồn đang bật, nhân
// viên, khóa hãng; người chỉ có P1 không thấy Page P2 và nguồn "Mes P2"; không phạm vi → rỗng; tenant khác không lọt.
func TestFilters_Scope(t *testing.T) {
	t.Parallel()
	e := newSetEnv(t)
	sc := reportseed.NineOrders(e.seed, Today(time.Now()))
	idle := e.seed.Source("Nguồn chưa dùng") // đang bật, chưa gắn đơn nào
	clerk := e.seed.Staff("Chưa có đơn")     // đang làm, chưa phụ trách đơn nào
	tb := e.st.Tenant(t, "b")
	sb := reportseed.New(t, e.st.Owner, tb)
	otherPage, otherSource := sb.Page("Page B"), sb.Source("Nguồn B")

	all := runFilters(t, e, e.all)
	if got := sorted(optionIDs(all.Pages)); !slices.Equal(got, sorted([]uuid.UUID{sc.P1, sc.P2})) {
		t.Errorf("Page toàn shop: %+v", all.Pages)
	}
	if got := sorted(optionIDs(all.Sources)); !slices.Equal(got, sorted([]uuid.UUID{sc.LiveP1, sc.MesP2, sc.Website, idle})) {
		t.Errorf("nguồn toàn shop: %+v", all.Sources)
	}
	for _, id := range []uuid.UUID{sc.An, sc.Binh, clerk} {
		if !slices.Contains(optionIDs(all.Staff), id) {
			t.Errorf("nhân viên toàn shop thiếu %s: %+v", id, all.Staff)
		}
	}
	if !slices.Equal(all.CarrierKeys, []string{"best", "spx", "vtp"}) {
		t.Errorf("khóa hãng: %v", all.CarrierKeys)
	}
	for _, o := range append(append([]FilterOption(nil), all.Pages...), all.Sources...) {
		if o.ID == otherPage || o.ID == otherSource || o.Name == "" {
			t.Errorf("lựa chọn lạ: %+v", o)
		}
	}

	p1 := runFilters(t, e, DataScope{TenantID: e.tenant, SellerID: e.all.SellerID, PageIDs: []uuid.UUID{sc.P1}})
	if got := optionIDs(p1.Pages); !slices.Equal(got, []uuid.UUID{sc.P1}) || p1.Pages[0].Name != "P1" {
		t.Errorf("Page của người P1: %+v", p1.Pages)
	}
	if got := optionIDs(p1.Sources); !slices.Equal(got, []uuid.UUID{sc.LiveP1}) {
		t.Errorf("nguồn của người P1: %+v (không được có Mes P2, Website, nguồn chưa dùng)", p1.Sources)
	}
	if !slices.Contains(optionIDs(p1.Staff), sc.An) {
		t.Errorf("nhân viên của người P1: %+v", p1.Staff)
	}

	none := runFilters(t, e, DataScope{TenantID: e.tenant, SellerID: e.all.SellerID})
	if len(none.Pages)+len(none.Sources)+len(none.Staff)+len(none.CarrierKeys) != 0 {
		t.Errorf("không phạm vi: %+v", none)
	}
}
