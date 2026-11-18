package testkit

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"sort"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/migrations"
)

// fakeMigrations trả một bộ migration thử có nội dung riêng của lần chạy (mã băm không trùng mẫu của lần chạy khác) và
// hàm migrate đếm số lần chạy: mỗi tệp *.sql chạy nguyên văn bằng chủ bảng.
func fakeMigrations(t *testing.T) (fstest.MapFS, migrateFunc, *atomic.Int32) {
	t.Helper()
	fsys := fstest.MapFS{
		"0001_a.sql": {Data: []byte("-- lần chạy " + randomSuffix(t) + "\nCREATE TABLE public.t1 (x int);\n")},
	}
	var runs atomic.Int32
	migrate := func(ctx context.Context, ownerURL string, f fs.FS) error {
		runs.Add(1)
		c, err := pgx.Connect(ctx, ownerURL)
		if err != nil {
			return fmt.Errorf("nối: %w", err)
		}
		defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
		names, err := fs.Glob(f, "*.sql")
		if err != nil {
			return fmt.Errorf("liệt kê: %w", err)
		}
		sort.Strings(names)
		for _, n := range names {
			b, err := fs.ReadFile(f, n)
			if err != nil {
				return fmt.Errorf("đọc %s: %w", n, err)
			}
			if _, err := c.Exec(ctx, string(b)); err != nil {
				return fmt.Errorf("chạy %s: %w", n, err)
			}
		}
		return nil
	}
	return fsys, migrate, &runs
}

// dropTemplateAtCleanup xóa database mẫu của bộ migration fsys ở thời điểm at khi test xong (mẫu thử không để lại
// trên máy chủ chung).
func dropTemplateAtCleanup(t *testing.T, fsys fs.FS, at time.Time) {
	t.Helper()
	name := templateName(t, fsys, at)
	base := serverURL(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		withAdmin(ctx, t, base, func(c *pgx.Conn) {
			_, _ = c.Exec(ctx, "ALTER DATABASE "+name+" IS_TEMPLATE false")
			exec(ctx, t, c, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		})
	})
}

func tableExists(t *testing.T, ownerURL, table string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := connect(ctx, t, ownerURL)
	defer func() { _ = c.Close(ctx) }()
	var ok bool
	if err := c.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+table).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

// Lần hai với cùng bộ migration (cùng giờ) không migrate lại; thêm một tệp migration thì mẫu mới được dựng (một lần);
// sang giờ sau cũng dựng mẫu mới (dữ liệu now() của migration không cũ quá một giờ).
func TestTemplateMigratesOncePerMigrationSet(t *testing.T) {
	fsys, migrate, runs := fakeMigrations(t)
	at := time.Now()
	dropTemplateAtCleanup(t, fsys, at)

	_, owner1, _ := cloneFromTemplate(t, fsys, migrate, at)
	_, owner2, _ := cloneFromTemplate(t, fsys, migrate, at)
	if got := runs.Load(); got != 1 {
		t.Fatalf("hai database cùng bộ migration: migrate chạy %d lần, muốn 1", got)
	}
	if owner1 == owner2 {
		t.Fatal("hai lần gọi trả cùng một database")
	}
	for _, u := range []string{owner1, owner2} {
		if !tableExists(t, u, "t1") {
			t.Fatalf("database nhân bản thiếu bảng của migration: %s", u)
		}
	}

	more := maps.Clone(fsys)
	more["0002_b.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE public.t2 (y int);\n")}
	dropTemplateAtCleanup(t, more, at)
	_, owner3, _ := cloneFromTemplate(t, more, migrate, at)
	if got := runs.Load(); got != 2 {
		t.Fatalf("thêm migration: migrate chạy tổng %d lần, muốn 2 (dựng lại mẫu một lần)", got)
	}
	if !tableExists(t, owner3, "t2") {
		t.Fatal("mẫu dựng lại thiếu bảng của migration mới")
	}

	next := at.Add(time.Hour)
	if templateName(t, fsys, next) == templateName(t, fsys, at) {
		t.Fatal("giờ sau phải dùng tên mẫu khác")
	}
	dropTemplateAtCleanup(t, fsys, next)
	cloneFromTemplate(t, fsys, migrate, next)
	cloneFromTemplate(t, fsys, migrate, next.Add(59*time.Minute-time.Duration(next.Minute())*time.Minute))
	if got := runs.Load(); got != 3 {
		t.Fatalf("sang giờ sau: migrate chạy tổng %d lần, muốn 3 (một mẫu mới cho cả giờ đó)", got)
	}
}

// Nhiều test song song cần cùng một mẫu chưa có: chỉ một lần migrate (khóa advisory), mọi test đều có database.
func TestTemplateConcurrentBuildMigratesOnce(t *testing.T) {
	fsys, migrate, runs := fakeMigrations(t)
	at := time.Now()
	dropTemplateAtCleanup(t, fsys, at)
	t.Run("group", func(t *testing.T) {
		for i := range 6 {
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				t.Parallel()
				_, owner, _ := cloneFromTemplate(t, fsys, migrate, at)
				if !tableExists(t, owner, "t1") {
					t.Fatal("database nhân bản thiếu bảng")
				}
			})
		}
	})
	if got := runs.Load(); got != 1 {
		t.Fatalf("6 test song song: migrate chạy %d lần, muốn 1", got)
	}
}

// Mẫu dở dang (tiến trình dựng chết trước khi đánh dấu IS_TEMPLATE) bị xóa và dựng lại.
func TestTemplateIncompleteIsRebuilt(t *testing.T) {
	fsys, migrate, runs := fakeMigrations(t)
	at := time.Now()
	dropTemplateAtCleanup(t, fsys, at)
	base := serverURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	withAdmin(ctx, t, base, func(admin *pgx.Conn) {
		exec(ctx, t, admin, "CREATE DATABASE "+templateName(t, fsys, at)+" OWNER crm_owner")
	})

	_, owner, _ := cloneFromTemplate(t, fsys, migrate, at)
	if got := runs.Load(); got != 1 {
		t.Fatalf("mẫu dở dang: migrate chạy %d lần, muốn 1", got)
	}
	if !tableExists(t, owner, "t1") {
		t.Fatal("database nhân bản từ mẫu dựng lại thiếu bảng")
	}
}

// Mẫu thật (migrations.FS + db.Migrate): database nhân bản đã migrate đủ, không phải mẫu, crm_app nối được, chạy lại
// Migrate không áp dụng gì thêm.
func TestStartPostgresClonesMigratedTemplate(t *testing.T) {
	before := time.Now()
	_, owner, app := StartPostgres(t)
	after := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	applied, err := db.Migrate(ctx, owner)
	if err != nil || len(applied) != 0 {
		t.Fatalf("migrate lại database nhân bản: applied=%v err=%v, muốn rỗng", applied, err)
	}
	c := connect(ctx, t, app)
	defer func() { _ = c.Close(ctx) }()
	var tpl, conn bool
	if err := c.QueryRow(ctx, "SELECT datistemplate, datallowconn FROM pg_database WHERE datname = current_database()").
		Scan(&tpl, &conn); err != nil || tpl || !conn {
		t.Fatalf("database test: datistemplate=%v datallowconn=%v err=%v, muốn false/true", tpl, conn, err)
	}
	base := serverURL(t)
	admin := connect(ctx, t, withDB(base, nil, base.Path))
	defer func() { _ = admin.Close(ctx) }()
	// StartPostgres dùng mẫu của giờ hiện tại (before và after khác nhau chỉ khi vừa sang giờ mới giữa chừng).
	used := ""
	for _, at := range []time.Time{before, after} {
		if exists, done := templateState(ctx, t, admin, templateName(t, migrations.FS, at)); exists && done {
			used = templateName(t, migrations.FS, at)
		}
	}
	if used == "" {
		t.Fatal("StartPostgres không dùng database mẫu của migrations.FS")
	}
	var allow bool
	if err := admin.QueryRow(ctx, "SELECT datallowconn FROM pg_database WHERE datname = $1", used).Scan(&allow); err != nil ||
		allow {
		t.Fatalf("database mẫu phải chặn kết nối: datallowconn=%v err=%v", allow, err)
	}
}

// Dọn chỉ xóa database đúng tiền tố, cũ hơn ngưỡng, không có kết nối; không xóa database được giữ lại (mẫu hiện tại)
// và database đang có kết nối; mẫu (IS_TEMPLATE) cũ cũng xóa được. Tiền tố riêng của lần chạy: không chạm database
// của phiên khác trên máy chủ chung.
func TestCleanupStaleDropsOnlyIdleOldDatabases(t *testing.T) {
	base := serverURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin := connect(ctx, t, withDB(base, nil, base.Path))
	defer func() { _ = admin.Close(ctx) }()
	prefix := "crm_test_zz" + randomSuffix(t) + "_"
	idle, busy, keep, tpl := prefix+"idle", prefix+"busy", prefix+"keep", prefix+"tpl"
	for _, n := range []string{idle, busy, keep, tpl} {
		exec(ctx, t, admin, "CREATE DATABASE "+n+" OWNER crm_owner")
		dropAtCleanup(t, base, n)
	}
	exec(ctx, t, admin, "ALTER DATABASE "+tpl+" IS_TEMPLATE true")
	holder := connect(ctx, t, withDB(base, nil, busy))
	defer func() { _ = holder.Close(ctx) }()

	// Đang có người giữ khóa dựng mẫu: mẫu cũ chưa bị xóa (dọn không chen vào lúc dựng lại mẫu).
	builder := connect(ctx, t, withDB(base, nil, base.Path))
	defer func() { _ = builder.Close(ctx) }()
	exec(ctx, t, builder, fmt.Sprintf("SELECT pg_advisory_lock(%d)", templateLockKey))
	// cleanupStale chỉ thử khóa (không chờ); tiến trình khác trên máy chủ chung có thể đang giữ. admin giữ sẵn (chờ)
	// khóa dọn: khóa advisory cấp phiên lấy lồng được, nên lần thử bên trong luôn được.
	exec(ctx, t, admin, fmt.Sprintf("SELECT pg_advisory_lock(%d)", cleanupLockKey))
	defer exec(context.WithoutCancel(ctx), t, admin, fmt.Sprintf("SELECT pg_advisory_unlock(%d)", cleanupLockKey))
	cleanupStale(ctx, t, admin, []string{prefix}, keep, 0)
	var tplLeft bool
	if err := admin.QueryRow(ctx, "SELECT datistemplate FROM pg_database WHERE datname = $1", tpl).Scan(&tplLeft); err != nil ||
		!tplLeft {
		t.Fatalf("mẫu bị dọn (hay bị bỏ dấu mẫu) khi khóa dựng mẫu đang bị giữ: %v %v", tplLeft, err)
	}
	exec(ctx, t, builder, fmt.Sprintf("SELECT pg_advisory_unlock(%d)", templateLockKey))
	exec(ctx, t, admin, fmt.Sprintf("SELECT pg_advisory_lock(%d)", templateLockKey))
	defer exec(context.WithoutCancel(ctx), t, admin, fmt.Sprintf("SELECT pg_advisory_unlock(%d)", templateLockKey))

	cleanupStale(ctx, t, admin, []string{prefix}, keep, 0)

	exists := func(n string) bool {
		var ok bool
		if err := admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", n).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	for n, want := range map[string]bool{idle: false, tpl: false, busy: true, keep: true} {
		if got := exists(n); got != want {
			t.Errorf("%s còn tồn tại = %v, muốn %v", n, got, want)
		}
	}

	// Ngưỡng tuổi: database vừa tạo chưa đủ cũ thì không bị xóa.
	young := prefix + "young"
	exec(ctx, t, admin, "CREATE DATABASE "+young+" OWNER crm_owner")
	dropAtCleanup(t, base, young)
	cleanupStale(ctx, t, admin, []string{prefix}, keep, time.Hour)
	if !exists(young) {
		t.Error("database mới tạo bị xóa dù chưa cũ hơn ngưỡng")
	}
}
