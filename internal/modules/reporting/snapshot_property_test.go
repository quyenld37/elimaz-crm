package reporting_test

import (
	"cmp"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// propBuckets là bốn ngăn của sổ tồn.
var propBuckets = []string{"on_hand", "held", "in_transit", "damaged"} //nolint:gochecknoglobals // bảng hằng của test

// propRow là một dòng sổ ngẫu nhiên.
type propRow struct {
	wh, variant int
	bucket      int
	delta       int
	at          time.Time
}

// Quy luật chụp tồn (kế hoạch M6 Task 233): chuỗi ngẫu nhiên 1–80 dòng sổ trên 2 kho, 3 biến thể, 4 ngăn, rải trên 5
// ngày, có dòng đúng 00:00:00 và 23:59:59 giờ VN, tổng chạy theo thời gian của mỗi ngăn không bao giờ âm → sau khi
// chụp, với mọi ngày d và mọi (kho, biến thể, ngăn): giá trị chụp = tổng delta có created_at < DayCutoff(d), tính
// trong Go độc lập với SQL; ngày chụp là mọi ngày từ ngày VN của dòng sổ đầu tới ngày cuối. Mỗi lượt một tenant mới.
// Số lượt theo TestMain của gói (200, nhiều hơn 100 của kế hoạch).
func TestSnapshotProperty(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	const firstDay, nDays = 10, 5
	h := snapHandler(vnAt(firstDay+nDays, 0, 10, 0), nil) // mốc của ngày cuối + 10 phút
	rapid.Check(t, func(rt *rapid.T) {
		rows := drawLedger(rt, firstDay, nDays)
		e := &snapEnv{s: s, tenant: s.Tenant(t, "prop-"+uuid.NewString())}
		e.seed = reportseed.New(t, s.Owner, e.tenant)
		whs := []uuid.UUID{e.seed.Warehouse("W1"), e.seed.Warehouse("W2")}
		vars := []uuid.UUID{
			e.seed.Variant(e.seed.Own(), "Áo", "P-1"), e.seed.Variant(e.seed.Own(), "Áo", "P-2"),
			e.seed.Variant(e.seed.Own(), "Quần", "P-3"),
		}
		insertLedger(t, e, whs, vars, rows)
		r := e.run(t, h)
		start := slices.MinFunc(rows, func(a, b propRow) int { return a.at.Compare(b.at) }).at.In(reporting.VN).Day()
		var wantDays []string
		for d := start; d < firstDay+nDays; d++ {
			wantDays = append(wantDays, vnAt(d, 0, 0, 0).Format(time.DateOnly))
		}
		if !slices.Equal(r.Days, wantDays) || !slices.Equal(e.days(t), wantDays) {
			rt.Fatalf("ngày chụp %v (bảng %v), muốn %v", r.Days, e.days(t), wantDays)
		}
		for _, day := range wantDays {
			checkPropDay(t, rt, e, day, whs, vars, rows)
		}
	})
}

// checkPropDay so bản chụp ngày day với tổng delta có created_at < DayCutoff(day), tính trong Go.
func checkPropDay(t *testing.T, rt *rapid.T, e *snapEnv, day string, whs, vars []uuid.UUID, rows []propRow) {
	t.Helper()
	cutoff, err := reporting.DayCutoff(day)
	if err != nil {
		rt.Fatal(err)
	}
	got := e.snapshot(t, day)
	for wi, wh := range whs {
		for vi, v := range vars {
			var want snapVal
			for _, row := range rows {
				if row.wh == wi && row.variant == vi && row.at.Before(cutoff) {
					*bucketOf(&want, row.bucket) += row.delta
				}
			}
			if got[snapKey{wh, v}] != want {
				rt.Fatalf("ngày %s kho %d biến thể %d: %+v, muốn %+v", day, wi, vi, got[snapKey{wh, v}], want)
			}
		}
	}
}

func bucketOf(v *snapVal, b int) *int {
	return [...]*int{&v.onHand, &v.held, &v.inTransit, &v.damaged}[b]
}

// drawLedger sinh 1–80 dòng sổ theo thứ tự thời gian; delta khác 0, tổng chạy của mỗi (kho, biến thể, ngăn) không âm.
func drawLedger(rt *rapid.T, firstDay, nDays int) []propRow {
	n := rapid.IntRange(1, 80).Draw(rt, "n")
	rows := make([]propRow, n)
	for i := range rows {
		day := firstDay + rapid.IntRange(0, nDays-1).Draw(rt, "day")
		var at time.Time
		switch rapid.IntRange(0, 2).Draw(rt, "edge") {
		case 0:
			at = vnAt(day, 0, 0, 0)
		case 1:
			at = vnAt(day, 23, 59, 59)
		default:
			at = vnAt(day, 0, 0, 0).Add(time.Duration(rapid.IntRange(0, 86399).Draw(rt, "sec")) * time.Second)
		}
		rows[i] = propRow{
			wh: rapid.IntRange(0, 1).Draw(rt, "wh"), variant: rapid.IntRange(0, 2).Draw(rt, "variant"),
			bucket: rapid.IntRange(0, 3).Draw(rt, "bucket"), at: at,
		}
	}
	slices.SortStableFunc(rows, func(a, b propRow) int { return a.at.Compare(b.at) })
	running := map[[3]int]int{}
	for i := range rows {
		k := [3]int{rows[i].wh, rows[i].variant, rows[i].bucket}
		d := rapid.IntRange(-5, 5).Filter(func(x int) bool { return x != 0 }).Draw(rt, "delta")
		switch {
		case running[k]+d >= 0:
		case running[k] == 0:
			d = -d
		default:
			d = -running[k]
		}
		running[k] += d
		rows[i].delta = d
	}
	return rows
}

// insertLedger ghi số dư cuối (bằng tổng sổ) và mọi dòng sổ trong một transaction của chủ bảng (cùng luật với
// reportseed.Ledger, nhanh hơn 80 transaction mỗi lượt).
func insertLedger(t *testing.T, e *snapEnv, whs, vars []uuid.UUID, rows []propRow) {
	t.Helper()
	type key struct{ wh, variant int }
	final := map[key]*snapVal{}
	for _, r := range rows {
		k := key{r.wh, r.variant}
		if final[k] == nil {
			final[k] = &snapVal{}
		}
		*bucketOf(final[k], r.bucket) += r.delta
	}
	keys := slices.SortedFunc(maps.Keys(final), func(a, b key) int { return cmp.Or(cmp.Compare(a.wh, b.wh), cmp.Compare(a.variant, b.variant)) })
	ctx := t.Context()
	if err := tenancy.WithTenant(ctx, e.s.Owner, e.tenant, func(tx pgx.Tx) error {
		for _, k := range keys {
			v := final[k]
			if _, err := tx.Exec(ctx, `INSERT INTO inventory.stock_balances (tenant_id, variant_id, warehouse_id, seller_id,
				on_hand, held, in_transit, damaged) SELECT $1, $2, $3, seller_id, $4, $5, $6, $7 FROM catalog.variants WHERE id = $2`,
				e.tenant, vars[k.variant], whs[k.wh], v.onHand, v.held, v.inTransit, v.damaged); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		for _, r := range rows {
			if _, err := tx.Exec(ctx, `INSERT INTO inventory.stock_ledger (tenant_id, variant_id, warehouse_id, bucket, delta,
				doc_type, doc_id, step, actor_type, created_at) VALUES ($1, $2, $3, $4, $5, 'adjust', $6, 'prop', 'system', $7)`,
				e.tenant, vars[r.variant], whs[r.wh], propBuckets[r.bucket], r.delta, uuid.New(), r.at); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("ghi sổ thử: %v", err)
	}
}
