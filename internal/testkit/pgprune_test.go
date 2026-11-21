package testkit

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
)

// familyOf là tiền tố chung của mọi mẫu (mọi giờ, có hay không có dữ liệu) của bộ migration fsys: crm_tpl_<băm>_.
func familyOf(t *testing.T, fsys fs.FS) string {
	t.Helper()
	n := templateName(t, fsys, time.Now())
	return n[:len(n)-len(hourLayout)]
}

// familyDatabases liệt kê các database (sắp theo tên) có tiền tố fam.
func familyDatabases(t *testing.T, fam string) []string {
	t.Helper()
	// Không dùng t.Context(): hàm còn chạy trong t.Cleanup (lúc đó t.Context() đã hủy).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var names []string
	withAdmin(ctx, t, serverURL(t), func(c *pgx.Conn) {
		rows, err := c.Query(ctx, "SELECT datname FROM pg_database WHERE left(datname, length($1)) = $1 ORDER BY 1", fam)
		if err != nil {
			t.Fatal(err)
		}
		names, err = pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
	})
	return names
}

// dropFamilyAtCleanup xóa mọi mẫu còn lại của tiền tố fam khi test xong (mẫu thử không để lại trên máy chủ chung).
func dropFamilyAtCleanup(t *testing.T, fam string) {
	t.Helper()
	t.Cleanup(func() {
		for _, n := range familyDatabases(t, fam) {
			withAdmin(context.Background(), t, serverURL(t), func(c *pgx.Conn) {
				_, _ = c.Exec(context.Background(), "ALTER DATABASE "+n+" IS_TEMPLATE false")
				exec(context.Background(), t, c, "DROP DATABASE IF EXISTS "+n+" WITH (FORCE)")
			})
		}
	})
}

// hourAt là giờ thứ i (giờ giả) tính từ đầu giờ của now: mọi lần gọi trong cùng test cùng mốc.
func hourAt(base time.Time, i int) time.Time {
	return base.Truncate(time.Hour).Add(time.Duration(i) * time.Hour)
}

// Dùng mẫu giờ sau thì mọi mẫu giờ trước của cùng bộ migration (cả mẫu có dữ liệu dựng từ chúng) bị xóa; mẫu của bộ
// migration khác không bị chạm; mẫu đang có người giữ khóa theo tên (đang dựng) không bị xóa, nhả khóa thì lần dùng sau
// xóa. Không chờ khóa dựng mẫu chung 727391.
func TestTemplatePruneKeepsNewestHourPerFamily(t *testing.T) {
	fsys, migrate, _ := fakeMigrations(t)
	other := maps.Clone(fsys)
	other["0002_b.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE public.t2 (y int);\n")}
	fam, otherFam := familyOf(t, fsys), familyOf(t, other)
	dropFamilyAtCleanup(t, fam)
	dropFamilyAtCleanup(t, otherFam)
	s := randomSeed("tia")
	base := time.Now()
	h0, h1, h2 := hourAt(base, 0), hourAt(base, 1), hourAt(base, 2)

	cloneFromTemplate(t, fsys, migrate, h0)
	cloneSeeded(t, fsys, migrate, h0, s)
	cloneFromTemplate(t, other, migrate, h0)
	if got := familyDatabases(t, fam); len(got) != 2 {
		t.Fatalf("giờ 0: mẫu của bộ = %v, muốn 2 (mẫu migration, mẫu có dữ liệu)", got)
	}

	// Khóa dựng mẫu chung đang bị giữ (phiên khác đang dựng mẫu bất kỳ) và khóa tên của mẫu có dữ liệu giờ 0 bị giữ
	// (đang được dựng): mẫu migration giờ 0 vẫn bị xóa, mẫu có dữ liệu giờ 0 thì để nguyên.
	// Mẫu giờ 1 dựng sẵn trước khi giữ khóa chung (đang giữ thì không dựng được).
	withAdmin(t.Context(), t, serverURL(t), func(admin *pgx.Conn) {
		ensureTemplate(t.Context(), t, admin, serverURL(t), templateName(t, fsys, h1), fsys, migrate)
	})
	holder := connect(t.Context(), t, withDB(serverURL(t), nil, serverURL(t).Path))
	defer func() { _ = holder.Close(context.WithoutCancel(t.Context())) }()
	exec(t.Context(), t, holder, fmt.Sprintf("SELECT pg_advisory_lock(%d)", templateLockKey))
	seeded0 := seededName(templateName(t, fsys, h0), s)
	lockName(t.Context(), t, holder, seeded0)

	cloneFromTemplate(t, fsys, migrate, h1)
	want := []string{templateName(t, fsys, h1), seeded0}
	slices.Sort(want)
	if got := familyDatabases(t, fam); !slices.Equal(got, want) {
		t.Fatalf("giờ 1, mẫu có dữ liệu giờ 0 đang bị giữ khóa: mẫu còn lại %v, muốn %v", got, want)
	}
	if got := familyDatabases(t, otherFam); len(got) != 1 {
		t.Fatalf("mẫu của bộ migration khác bị chạm: %v", got)
	}
	if _, err := holder.Exec(t.Context(), "SELECT pg_advisory_unlock_all()"); err != nil {
		t.Fatal(err)
	}

	cloneSeeded(t, fsys, migrate, h2, s)
	want = []string{templateName(t, fsys, h2), seededName(templateName(t, fsys, h2), s)}
	slices.Sort(want)
	if got := familyDatabases(t, fam); !slices.Equal(got, want) {
		t.Fatalf("giờ 2: mẫu còn lại %v, muốn %v", got, want)
	}
}

// Ba "tiến trình" (test song song, mỗi cái kết nối quản trị riêng) cùng dựng và nhân bản qua ba giờ giả, hai bộ
// migration, mỗi bộ cả mẫu migration lẫn mẫu có dữ liệu: mọi lần nhân bản đều được (không 42809, không mất mẫu đang
// nhân bản), và cuối cùng mỗi bộ chỉ còn mẫu của giờ mới nhất (≤ 2 bộ × 2 loại).
func TestTemplateNoPileUpUnderConcurrentHours(t *testing.T) {
	fsysA, migrate, _ := fakeMigrations(t)
	fsysB := maps.Clone(fsysA)
	fsysB["0002_b.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE public.t2 (y int);\n")}
	sets := []fs.FS{fsysA, fsysB}
	for _, f := range sets {
		dropFamilyAtCleanup(t, familyOf(t, f))
	}
	s := randomSeed("ba-tien-trinh")
	base := time.Now()
	const hours = 3
	t.Run("group", func(t *testing.T) {
		for p := range 3 {
			t.Run(fmt.Sprint("p", p), func(t *testing.T) {
				t.Parallel()
				for h := range hours {
					for i := range sets {
						cloneBoth(t, sets[(i+p)%len(sets)], migrate, hourAt(base, h), s)
					}
				}
			})
		}
	})
	last := hourAt(base, hours-1).UTC().Format(hourLayout)
	for _, f := range sets {
		assertOnlyHour(t, familyDatabases(t, familyOf(t, f)), last)
	}
}

// cloneBoth nhân bản một database từ mẫu migration và một từ mẫu có dữ liệu s của fsys ở giờ at, kiểm cả hai dùng được.
func cloneBoth(t *testing.T, fsys fs.FS, migrate migrateFunc, at time.Time, s Seed) {
	t.Helper()
	_, owner, _ := cloneFromTemplate(t, fsys, migrate, at)
	if !tableExists(t, owner, "t1") {
		t.Fatalf("giờ %s: bản sao thiếu bảng", at.UTC().Format(hourLayout))
	}
	admin, _, _ := cloneSeeded(t, fsys, migrate, at, s)
	seedValue(t, admin)
}

// assertOnlyHour: một bộ migration còn ≤ 2 mẫu (mẫu migration, mẫu có dữ liệu), đều của giờ hour.
func assertOnlyHour(t *testing.T, got []string, hour string) {
	t.Helper()
	if len(got) > 2 {
		t.Errorf("bộ migration còn %d mẫu %v, muốn ≤ 2", len(got), got)
	}
	for _, n := range got {
		if !strings.Contains(n, "_"+hour) {
			t.Errorf("mẫu giờ cũ còn sót: %s (giờ mới nhất %s)", n, hour)
		}
	}
}
