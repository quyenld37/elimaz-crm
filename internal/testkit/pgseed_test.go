package testkit

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// randomSeed trả bộ dữ liệu thử có giá trị ngẫu nhiên đóng băng lúc dựng (bảng public.s một dòng random()) và bảng lớn
// public.big 1.000 dòng đã ANALYZE: hai database cùng giá trị r nghĩa là cùng một lần dựng.
func randomSeed(name string) Seed {
	return Seed{Name: name, SQL: []string{
		"CREATE TABLE public.s AS SELECT random() AS r",
		"CREATE TABLE public.big AS SELECT g FROM generate_series(1, 1000) AS g",
		"ANALYZE public.big",
	}}
}

// dropSeededAtCleanup xóa mẫu có dữ liệu s (và mẫu migration của fsys) ở thời điểm at khi test xong.
func dropSeededAtCleanup(t *testing.T, fsys fs.FS, at time.Time, s Seed) {
	t.Helper()
	dropTemplateAtCleanup(t, fsys, at)
	dropNamedTemplateAtCleanup(t, seededName(templateName(t, fsys, at), s))
}

// adminExec chạy sql bằng người quản trị trên database mặc định của máy chủ thử.
func adminExec(t *testing.T, sql string) {
	t.Helper()
	withAdmin(t.Context(), t, serverURL(t), func(c *pgx.Conn) { exec(t.Context(), t, c, sql) })
}

// seedValue đọc giá trị ngẫu nhiên của randomSeed bằng người quản trị (bảng do người quản trị tạo lúc dựng).
func seedValue(t *testing.T, adminURL string) float64 {
	t.Helper()
	var r float64
	withAdmin(t.Context(), t, mustParse(t, adminURL), func(c *pgx.Conn) {
		if err := c.QueryRow(t.Context(), "SELECT r FROM public.s").Scan(&r); err != nil {
			t.Fatalf("đọc dữ liệu thử: %v", err)
		}
	})
	return r
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// Hai database cùng bộ dữ liệu: dữ liệu chỉ dựng một lần (cùng giá trị ngẫu nhiên), mẫu migration chỉ migrate một
// lần; database test không phải mẫu. Đổi một ký tự trong câu, hay đổi tên bộ, là mẫu mới (dựng lại, giá trị khác).
// Thống kê ANALYZE đi theo bản sao.
func TestSeededBuildsOncePerContent(t *testing.T) {
	fsys, migrate, runs := fakeMigrations(t)
	at := time.Now()
	s := randomSeed("thu")
	dropSeededAtCleanup(t, fsys, at, s)

	admin1, _, _ := cloneSeeded(t, fsys, migrate, at, s)
	admin2, owner2, _ := cloneSeeded(t, fsys, migrate, at, s)
	if admin1 == admin2 {
		t.Fatal("hai lần gọi trả cùng một database")
	}
	r1, r2 := seedValue(t, admin1), seedValue(t, admin2)
	if r1 != r2 {
		t.Fatalf("hai database cùng bộ dữ liệu có giá trị khác nhau (%v, %v): dữ liệu bị dựng lại", r1, r2)
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("migrate chạy %d lần, muốn 1", got)
	}
	if !tableExists(t, owner2, "t1") {
		t.Fatal("database có dữ liệu thiếu bảng của migration")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := connect(ctx, t, admin2)
	defer func() { _ = c.Close(ctx) }()
	var (
		tuples float32
		stats  int
		isTpl  bool
	)
	if err := c.QueryRow(ctx, `SELECT (SELECT reltuples FROM pg_class WHERE oid = 'public.big'::regclass),
		(SELECT count(*) FROM pg_stats WHERE schemaname = 'public' AND tablename = 'big'),
		(SELECT datistemplate FROM pg_database WHERE datname = current_database())`).Scan(&tuples, &stats, &isTpl); err != nil {
		t.Fatal(err)
	}
	if tuples != 1000 || stats == 0 || isTpl {
		t.Fatalf("bản sao: reltuples=%v, pg_stats=%d dòng, datistemplate=%v; muốn 1000, >0, false", tuples, stats, isTpl)
	}

	changed := s
	changed.SQL = append([]string{}, s.SQL...)
	changed.SQL[1] = strings.Replace(changed.SQL[1], "1000", "1001", 1)
	renamed := s
	renamed.Name = "thu2"
	for _, other := range []Seed{changed, renamed} {
		if seededName(templateName(t, fsys, at), other) == seededName(templateName(t, fsys, at), s) {
			t.Fatalf("bộ %+v trùng tên mẫu với bộ gốc", other)
		}
		dropSeededAtCleanup(t, fsys, at, other)
		a, _, _ := cloneSeeded(t, fsys, migrate, at, other)
		if seedValue(t, a) == r1 {
			t.Fatalf("bộ %+v dùng lại mẫu của bộ gốc", other)
		}
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("đổi dữ liệu không đổi migration: migrate chạy %d lần, muốn 1", got)
	}
}

// Nhiều test song song cần cùng một mẫu có dữ liệu chưa có: dữ liệu dựng một lần (khóa), mọi test có database.
func TestSeededConcurrentBuildOnce(t *testing.T) {
	fsys, migrate, runs := fakeMigrations(t)
	at := time.Now()
	s := randomSeed("song-song")
	dropSeededAtCleanup(t, fsys, at, s)
	vals := make([]float64, 6)
	t.Run("group", func(t *testing.T) {
		for i := range vals {
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				t.Parallel()
				admin, _, _ := cloneSeeded(t, fsys, migrate, at, s)
				vals[i] = seedValue(t, admin)
			})
		}
	})
	for i, v := range vals {
		if v != vals[0] {
			t.Fatalf("database %d có giá trị %v khác database 0 (%v): dữ liệu bị dựng nhiều lần", i, v, vals[0])
		}
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("6 test song song: migrate chạy %d lần, muốn 1", got)
	}
}

// Mẫu có dữ liệu dở dang (tiến trình dựng chết trước khi đánh dấu IS_TEMPLATE) bị xóa và dựng lại.
func TestSeededIncompleteIsRebuilt(t *testing.T) {
	fsys, migrate, _ := fakeMigrations(t)
	at := time.Now()
	s := randomSeed("do-dang")
	dropSeededAtCleanup(t, fsys, at, s)
	adminExec(t, "CREATE DATABASE "+seededName(templateName(t, fsys, at), s)+" OWNER crm_owner")
	admin, owner, _ := cloneSeeded(t, fsys, migrate, at, s)
	seedValue(t, admin)
	if !tableExists(t, owner, "t1") {
		t.Fatal("database nhân bản từ mẫu dựng lại thiếu bảng của migration")
	}
}

// Tên mẫu có dữ liệu nằm trong giới hạn 63 ký tự của PostgreSQL và mang tiền tố mẫu (cleanupStale dọn theo cùng luật).
func TestSeededNameFitsAndIsCleanedUp(t *testing.T) {
	fsys, _, _ := fakeMigrations(t)
	n := seededName(templateName(t, fsys, time.Now()), randomSeed("ten"))
	if len(n) > 63 || !strings.HasPrefix(n, templatePrefix) {
		t.Fatalf("tên mẫu %q (%d ký tự): muốn ≤ 63 ký tự, tiền tố %s", n, len(n), templatePrefix)
	}
}

// Dọn không xóa mẫu cũ khi có người đang giữ khóa dựng mẫu có dữ liệu của đúng tên đó; nhả khóa thì xóa được.
func TestCleanupStaleSkipsSeededTemplateBeingBuilt(t *testing.T) {
	tpl := "crm_test_zz" + randomSuffix(t) + "_tpl"
	prefix := strings.TrimSuffix(tpl, "tpl")
	adminExec(t, "CREATE DATABASE "+tpl+" OWNER crm_owner")
	base := serverURL(t)
	dropAtCleanup(t, base, tpl)
	adminExec(t, "ALTER DATABASE "+tpl+" IS_TEMPLATE true")
	t.Cleanup(func() {
		withAdmin(context.Background(), t, base, func(c *pgx.Conn) { _, _ = c.Exec(context.Background(), "ALTER DATABASE "+tpl+" IS_TEMPLATE false") })
	})
	ctx := t.Context()
	admin := connect(ctx, t, withDB(base, nil, base.Path))
	defer func() { _ = admin.Close(context.WithoutCancel(ctx)) }()

	builder := connect(ctx, t, withDB(base, nil, base.Path))
	defer func() { _ = builder.Close(ctx) }()
	if _, err := builder.Exec(ctx, "SELECT pg_advisory_lock($1, hashtext($2))", int32(seedLockKey), tpl); err != nil {
		t.Fatal(err)
	}
	// Như TestCleanupStaleDropsOnlyIdleOldDatabases: khóa dọn, khóa dựng mẫu riêng của lần chạy (không chờ khóa chung).
	locks := privateLocks(t)
	exists := func() bool {
		var ok bool
		if err := admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", tpl).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	cleanupStale(ctx, t, admin, locks, []string{prefix}, "", 0)
	if !exists() {
		t.Fatal("mẫu bị dọn khi khóa dựng mẫu có dữ liệu của nó đang bị giữ")
	}
	if _, err := builder.Exec(ctx, "SELECT pg_advisory_unlock($1, hashtext($2))", int32(seedLockKey), tpl); err != nil {
		t.Fatal(err)
	}
	cleanupStale(ctx, t, admin, locks, []string{prefix}, "", 0)
	if exists() {
		t.Fatal("mẫu cũ không ai giữ khóa phải bị dọn")
	}
}

// FixedUUID: cùng tên cùng mã ở mọi lần gọi, tên khác mã khác.
func TestFixedUUID(t *testing.T) {
	a1, a2, b := FixedUUID("a"), FixedUUID("a"), FixedUUID("b")
	if a1 != a2 || a1 == b {
		t.Fatal("FixedUUID phải cố định theo tên và khác nhau giữa các tên")
	}
	if got := a1.Version(); got != 5 {
		t.Fatalf("phiên bản %d, muốn 5", got)
	}
}
