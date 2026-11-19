package testkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Database mẫu (Task 391): mỗi bộ migration được migrate một lần mỗi giờ vào crm_tpl_<băm>_<giờ UTC>, rồi mỗi test
// nhân bản bằng CREATE DATABASE … TEMPLATE (nhanh hơn chạy lại mọi migration, nhẹ cho máy chủ PostgreSQL dùng chung
// của nhiều phiên). Giờ trong tên: dữ liệu mà migration tính theo now() (ví dụ next_run_at của lịch ở 0122) cũ tối đa
// một giờ so với lúc test chạy, như khi migrate thẳng ở đầu một gói test dài.
//
// Khóa advisory tính theo database: mọi phiên phải nối cùng database mặc định của máy chủ thử (đường dẫn trong
// CRM_TEST_DATABASE_URL / CRM_TEST_ADMIN_URL) mới loại trừ nhau; phiên trỏ database mặc định khác có thể làm test của
// phiên kia đỏ (không mất dữ liệu).

const (
	templatePrefix = "crm_tpl_"
	testDBPrefix   = "crm_test_"
	// templateRecipe phải đổi khi cách dựng mẫu đổi (prepareEmpty, extension, ensurePlatformSchema của db.Migrate…):
	// mẫu cũ không còn được dùng.
	templateRecipe = "testkit-template-v2"
	// Khóa advisory trên database mặc định của máy chủ thử (khóa advisory tính theo database: mọi phiên nối cùng
	// database mặc định nên cùng khóa). Khác khóa 727001 của db.Migrate.
	templateLockKey = 727391
	cleanupLockKey  = 727392
	// staleAfter: database test, mẫu cũ hơn ngưỡng này và không ai nối là đồ bỏ sót (không test nào chạy 6 giờ).
	staleAfter = 6 * time.Hour
)

// migrateFunc migrate database trống của chủ bảng ownerURL theo bộ migration fsys.
type migrateFunc func(ctx context.Context, ownerURL string, fsys fs.FS) error

// templateName là tên database mẫu của bộ migration fsys dùng ở thời điểm at: sha256 của templateRecipe, tên và nội
// dung mọi tệp (16 ký tự hex), rồi giờ UTC của at (YYYYMMDDHH).
func templateName(t testing.TB, fsys fs.FS, at time.Time) string {
	t.Helper()
	h := sha256.New()
	h.Write([]byte(templateRecipe + "\x00"))
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("đọc %s: %w", p, err)
		}
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", p, len(b))
		h.Write(b)
		return nil
	})
	if err != nil {
		t.Fatalf("testkit: băm bộ migration: %v", err)
	}
	return templatePrefix + hex.EncodeToString(h.Sum(nil))[:16] + "_" + at.UTC().Format("2006010215")
}

// cloneFromTemplate tạo database crm_test_<ngẫu nhiên> (chủ crm_owner) nhân bản từ mẫu của bộ migration fsys ở thời
// điểm at (dựng mẫu bằng migrate nếu chưa có), xóa khi test xong. Trước đó dọn database test và mẫu bị bỏ sót.
func cloneFromTemplate(t testing.TB, fsys fs.FS, migrate migrateFunc, at time.Time) (adminURL, ownerURL, appURL string) {
	t.Helper()
	base := serverURL(t)
	tpl := templateName(t, fsys, at)
	name, adminURL, ownerURL, appURL := newTestDB(t, base)
	dropAtCleanup(t, base, name)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	withAdmin(ctx, t, base, func(admin *pgx.Conn) {
		cleanupStale(ctx, t, admin, serverLocks(), []string{testDBPrefix, templatePrefix}, tpl, staleAfter)
		// Mẫu có thể vừa bị phiên khác dọn (3D000), hay một kết nối cũ vào mẫu chưa thoát hẳn (55006): dựng/kiểm lại
		// mẫu rồi thử lại.
		const attempts = 5
		for i := 1; ; i++ {
			ensureTemplate(ctx, t, admin, base, tpl, fsys, migrate)
			_, err := admin.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE "+tpl+" OWNER crm_owner")
			if err == nil {
				return
			}
			if i >= attempts || (!isCode(err, "3D000") && !isCode(err, "55006")) {
				t.Fatalf("testkit: nhân bản database thử từ %s: %v", tpl, err)
			}
			time.Sleep(time.Duration(i) * 200 * time.Millisecond)
		}
	})
	return adminURL, ownerURL, appURL
}

// ensureTemplate bảo đảm database mẫu tpl đã dựng xong (datistemplate = true là dấu "xong", đặt ở bước cuối). Chưa có
// thì dựng dưới khóa advisory (nhiều tiến trình, nhiều phiên không dựng trùng): tạo database trống, migrate, chặn mọi
// kết nối vào mẫu, đánh dấu mẫu. Mẫu dở dang (tiến trình dựng chết giữa chừng) bị xóa và dựng lại.
func ensureTemplate(ctx context.Context, t testing.TB, admin *pgx.Conn, base *url.URL, tpl string, fsys fs.FS,
	migrate migrateFunc,
) {
	t.Helper()
	if exists, done := templateState(ctx, t, admin, tpl); exists && done {
		return
	}
	if _, err := admin.Exec(ctx, "SELECT pg_advisory_lock($1)", int64(templateLockKey)); err != nil {
		t.Fatalf("testkit: chờ khóa dựng database mẫu: %v", err)
	}
	defer func() {
		_, _ = admin.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", int64(templateLockKey))
	}()
	exists, done := templateState(ctx, t, admin, tpl)
	if done {
		return
	}
	if exists {
		exec(ctx, t, admin, "DROP DATABASE "+tpl+" WITH (FORCE)")
	}
	exec(ctx, t, admin, "CREATE DATABASE "+tpl+" OWNER crm_owner")
	prepareEmpty(ctx, t, admin, withDB(base, nil, tpl))
	if err := migrate(ctx, withDB(base, url.UserPassword("crm_owner", devOwnerPass), tpl), fsys); err != nil {
		_, _ = admin.Exec(context.WithoutCancel(ctx), "DROP DATABASE IF EXISTS "+tpl+" WITH (FORCE)")
		t.Fatalf("testkit: migrate database mẫu %s: %v", tpl, err)
	}
	exec(ctx, t, admin, "ALTER DATABASE "+tpl+" WITH ALLOW_CONNECTIONS false")
	exec(ctx, t, admin, "ALTER DATABASE "+tpl+" WITH IS_TEMPLATE true")
}

func templateState(ctx context.Context, t testing.TB, admin *pgx.Conn, tpl string) (exists, done bool) {
	t.Helper()
	err := admin.QueryRow(ctx, "SELECT datistemplate FROM pg_database WHERE datname = $1", tpl).Scan(&done)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false
	}
	if err != nil {
		t.Fatalf("testkit: đọc trạng thái database mẫu: %v", err)
	}
	return true, done
}

// cleanupLocks là cặp khóa advisory của việc dọn: khóa dọn (một tiến trình dọn mỗi lúc) và khóa dựng mẫu (không xóa mẫu
// đang được dựng lại). Đường thật dùng serverLocks(); test của việc dọn dùng cặp khóa riêng của lần chạy để không chờ
// khóa chung của các phiên khác trên máy chủ thử.
type cleanupLocks struct{ cleanup, template int64 }

// serverLocks là khóa dọn và khóa dựng mẫu chung của mọi tiến trình test trên máy chủ thử.
func serverLocks() cleanupLocks {
	return cleanupLocks{cleanup: cleanupLockKey, template: templateLockKey}
}

// cleanupStale xóa các database có tên bắt đầu bằng một trong prefixes (trừ keep) cũ hơn olderThan và không có kết nối.
// Tuổi đọc từ thời điểm tạo tệp PG_VERSION của database (database bỏ sót trước Task 391 không có mốc thời gian trong
// tên). Xóa bằng DROP DATABASE không FORCE: PostgreSQL từ chối nếu có ai vừa nối vào, nên không bao giờ xóa database
// đang dùng. Chỉ một tiến trình dọn mỗi lúc (khóa advisory locks.cleanup thử, không chờ); lỗi khi xóa bỏ qua (lần sau
// dọn tiếp).
func cleanupStale(ctx context.Context, t testing.TB, admin *pgx.Conn, locks cleanupLocks, prefixes []string, keep string,
	olderThan time.Duration,
) {
	t.Helper()
	var got bool
	if err := admin.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", locks.cleanup).Scan(&got); err != nil {
		t.Fatalf("testkit: khóa dọn database thử: %v", err)
	}
	if !got {
		return
	}
	defer func() {
		_, _ = admin.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", locks.cleanup)
	}()
	rows, err := admin.Query(ctx, `
SELECT d.datname, d.datistemplate
FROM pg_database d
WHERE EXISTS (SELECT 1 FROM unnest($1::text[]) p WHERE left(d.datname, length(p)) = p)
  AND d.datname <> $2
  AND (pg_stat_file('base/' || d.oid || '/PG_VERSION', true)).modification < now() - make_interval(secs => $3)
  AND NOT EXISTS (SELECT 1 FROM pg_stat_activity a WHERE a.datid = d.oid)
ORDER BY d.datname`, prefixes, keep, olderThan.Seconds())
	if err != nil {
		t.Fatalf("testkit: tìm database thử bị bỏ sót: %v", err)
	}
	type stale struct {
		name string
		tpl  bool
	}
	var list []stale
	for rows.Next() {
		var s stale
		if err := rows.Scan(&s.name, &s.tpl); err != nil {
			t.Fatalf("testkit: đọc database thử bị bỏ sót: %v", err)
		}
		list = append(list, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("testkit: đọc database thử bị bỏ sót: %v", err)
	}
	for _, s := range list {
		dropStale(ctx, t, admin, locks.template, s.name, s.tpl, olderThan)
	}
}

// dropStale xóa một database bỏ sót (không FORCE). Mẫu chỉ xóa khi giữ được khóa dựng mẫu templateLock và khóa dựng mẫu có dữ liệu
// của tên đó (thử, không chờ): không chen vào giữa lúc một tiến trình khác xóa-rồi-dựng-lại mẫu cùng tên. Lỗi bỏ qua, lần sau dọn tiếp.
func dropStale(ctx context.Context, t testing.TB, admin *pgx.Conn, templateLock int64, name string, isTemplate bool,
	olderThan time.Duration,
) {
	t.Helper()
	ident := pgx.Identifier{name}.Sanitize()
	if isTemplate {
		var got bool
		if err := admin.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", templateLock).Scan(&got); err != nil || !got {
			return
		}
		defer func() {
			_, _ = admin.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", templateLock)
		}()
		// Mẫu có dữ liệu (pgseed.go) dựng lại dưới khóa riêng theo tên.
		if err := admin.QueryRow(ctx, "SELECT pg_try_advisory_lock($1, hashtext($2))", int32(seedLockKey), name).
			Scan(&got); err != nil || !got {
			return
		}
		defer func() {
			_, _ = admin.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1, hashtext($2))", int32(seedLockKey), name)
		}()
		// Kiểm lại dưới khóa: mẫu cùng tên có thể vừa được dựng lại sau lúc liệt kê.
		var still bool
		if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database d WHERE d.datname = $1
  AND (pg_stat_file('base/' || d.oid || '/PG_VERSION', true)).modification < now() - make_interval(secs => $2))`,
			name, olderThan.Seconds()).Scan(&still); err != nil || !still {
			return
		}
		if _, err := admin.Exec(ctx, "ALTER DATABASE "+ident+" WITH IS_TEMPLATE false"); err != nil {
			t.Logf("testkit: bỏ qua dọn %s: %v", name, err)
			return
		}
	}
	// 55006: có ai vừa nối vào giữa lúc kiểm và lúc xóa; database đó đang dùng, để nguyên.
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+ident); err != nil && !isCode(err, "55006") {
		t.Logf("testkit: bỏ qua dọn %s: %v", name, err)
	}
}

func isCode(err error, code string) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == code
}
