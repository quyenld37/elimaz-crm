package testkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Database mẫu (Task 391): mỗi bộ migration được migrate một lần mỗi giờ vào crm_tpl_<băm>_<giờ UTC>, rồi mỗi test
// nhân bản bằng CREATE DATABASE … TEMPLATE (nhanh hơn chạy lại mọi migration, nhẹ cho máy chủ PostgreSQL dùng chung
// của nhiều phiên). Giờ trong tên: dữ liệu mà migration tính theo now() (ví dụ next_run_at của lịch ở 0122) cũ tối đa
// một giờ so với lúc test chạy, như khi migrate thẳng ở đầu một gói test dài. Mẫu của giờ trước không bao giờ được
// dùng lại: lần đầu một tiến trình dùng mẫu giờ h của một băm, mọi mẫu giờ < h cùng băm (kể cả mẫu có dữ liệu dựng từ
// chúng) bị xóa (pruneOlderHours, Task 447), nên mỗi bộ migration chỉ còn mẫu của giờ mới nhất.
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
	// database mặc định nên cùng khóa). Khác khóa 727001 của db.Migrate. templateLockKey: khóa chung, người dựng mẫu
	// migration chờ (như trước Task 447, để không dựng trùng với phiên còn chạy mã cũ); đường xóa mẫu không dùng nó.
	templateLockKey = 727391
	cleanupLockKey  = 727392
	// nameLockKey: khóa advisory hai số (nameLockKey, hashtext(tên mẫu)) — mỗi mẫu (mẫu migration và mẫu có dữ liệu)
	// một khóa. Người dựng giữ khóa của tên mình dựng; người xóa chỉ thử khóa của đúng tên sẽ xóa (Task 447), nên dựng
	// một mẫu không chặn việc dọn mẫu khác. Va chạm hashtext chỉ làm hai việc chờ nhau, không sai.
	nameLockKey = 727393
	// staleAfter: database test, mẫu cũ hơn ngưỡng này và không ai nối là đồ bỏ sót (không test nào chạy 6 giờ).
	staleAfter = 6 * time.Hour
	// hourLayout: giờ UTC ở cuối tên mẫu migration (YYYYMMDDHH).
	hourLayout = "2006010215"
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
	return templatePrefix + hex.EncodeToString(h.Sum(nil))[:16] + "_" + at.UTC().Format(hourLayout)
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
		cleanupStale(ctx, t, admin, cleanupLockKey, []string{testDBPrefix, templatePrefix}, tpl, staleAfter)
		// Mẫu có thể vừa bị phiên khác dọn (3D000), hay một kết nối cũ vào mẫu chưa thoát hẳn (55006): dựng/kiểm lại
		// mẫu rồi thử lại.
		const attempts = 5
		for i := 1; ; i++ {
			ensureTemplate(ctx, t, admin, base, tpl, fsys, migrate)
			_, err := admin.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE "+tpl+" OWNER crm_owner")
			if err == nil {
				break
			}
			if i >= attempts || (!isCode(err, "3D000") && !isCode(err, "55006")) {
				t.Fatalf("testkit: nhân bản database thử từ %s: %v", tpl, err)
			}
			time.Sleep(time.Duration(i) * 200 * time.Millisecond)
		}
		pruneOlderHours(ctx, t, admin, tpl)
	})
	return adminURL, ownerURL, appURL
}

// ensureTemplate bảo đảm database mẫu tpl đã dựng xong (datistemplate = true là dấu "xong", đặt ở bước cuối). Chưa có
// thì dựng dưới khóa chung templateLockKey rồi khóa theo tên (nhiều tiến trình, nhiều phiên không dựng trùng; người
// xóa không chen vào): tạo database trống, migrate, chặn mọi kết nối vào mẫu, đánh dấu mẫu. Mẫu dở dang (tiến trình
// dựng chết giữa chừng) bị xóa và dựng lại.
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
	lockName(ctx, t, admin, tpl)
	defer unlockName(ctx, admin, tpl)
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

// lockName chờ khóa theo tên của mẫu name (cấp phiên, lấy lồng được trong cùng phiên).
func lockName(ctx context.Context, t testing.TB, admin *pgx.Conn, name string) {
	t.Helper()
	if _, err := admin.Exec(ctx, "SELECT pg_advisory_lock($1, hashtext($2))", int32(nameLockKey), name); err != nil {
		t.Fatalf("testkit: chờ khóa dựng database mẫu %s: %v", name, err)
	}
}

// tryLockName thử khóa theo tên của mẫu name, không chờ; lỗi coi như không giành được.
func tryLockName(ctx context.Context, admin *pgx.Conn, name string) bool {
	var got bool
	err := admin.QueryRow(ctx, "SELECT pg_try_advisory_lock($1, hashtext($2))", int32(nameLockKey), name).Scan(&got)
	return err == nil && got
}

func unlockName(ctx context.Context, admin *pgx.Conn, name string) {
	_, _ = admin.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1, hashtext($2))", int32(nameLockKey), name)
}

// cleanupStale xóa các database có tên bắt đầu bằng một trong prefixes (trừ keep) cũ hơn olderThan và không có kết nối.
// Tuổi đọc từ thời điểm tạo tệp PG_VERSION của database (database bỏ sót trước Task 391 không có mốc thời gian trong
// tên). Xóa bằng DROP DATABASE không FORCE: PostgreSQL từ chối nếu có ai vừa nối vào, nên không bao giờ xóa database
// đang dùng. Chỉ một tiến trình dọn mỗi lúc (khóa advisory cleanupLock thử, không chờ); lỗi khi xóa bỏ qua (lần sau
// dọn tiếp).
func cleanupStale(ctx context.Context, t testing.TB, admin *pgx.Conn, cleanupLock int64, prefixes []string, keep string,
	olderThan time.Duration,
) {
	t.Helper()
	var got bool
	if err := admin.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", cleanupLock).Scan(&got); err != nil {
		t.Fatalf("testkit: khóa dọn database thử: %v", err)
	}
	if !got {
		return
	}
	defer func() {
		_, _ = admin.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", cleanupLock)
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
		Name       string
		IsTemplate bool
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[stale])
	if err != nil {
		t.Fatalf("testkit: đọc database thử bị bỏ sót: %v", err)
	}
	for _, s := range list {
		dropStale(ctx, t, admin, s.Name, s.IsTemplate, olderThan)
	}
}

// dropStale xóa một database bỏ sót (không FORCE). Mẫu (IS_TEMPLATE, hay tên crm_tpl_… kể cả mẫu dở) đi qua
// dropTemplateDB: chỉ cần khóa theo tên của chính nó, kiểm lại tuổi dưới khóa (mẫu cùng tên có thể vừa được dựng lại
// sau lúc liệt kê). Lỗi bỏ qua, lần sau dọn tiếp.
func dropStale(ctx context.Context, t testing.TB, admin *pgx.Conn, name string, isTemplate bool,
	olderThan time.Duration,
) {
	t.Helper()
	if isTemplate || strings.HasPrefix(name, templatePrefix) {
		dropTemplateDB(ctx, t, admin, name, `SELECT EXISTS (SELECT 1 FROM pg_database d WHERE d.datname = $1
  AND (pg_stat_file('base/' || d.oid || '/PG_VERSION', true)).modification < now() - make_interval(secs => $2))`,
			olderThan.Seconds())
		return
	}
	// 55006: có ai vừa nối vào giữa lúc kiểm và lúc xóa; database đó đang dùng, để nguyên.
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()); err != nil &&
		!isCode(err, "55006") {
		t.Logf("testkit: bỏ qua dọn %s: %v", name, err)
	}
}

// dropTemplateDB xóa database mẫu name khi giữ được khóa theo tên của nó (thử, không chờ: ai đang dựng lại đúng tên
// đó thì để nguyên) và câu kiểm still (tham số $1 = name, rồi args) dưới khóa còn trả true. Bỏ dấu mẫu trước (DROP một
// mẫu lỗi 42809), rồi DROP không FORCE: bản sao đang chạy CREATE DATABASE … TEMPLATE name giữ khóa trên name nên DROP
// chờ bản sao xong; ai đang nối vào thì DROP lỗi 55006 và mẫu (đã bỏ dấu) được người dựng sau dựng lại. Lỗi bỏ qua.
func dropTemplateDB(ctx context.Context, t testing.TB, admin *pgx.Conn, name, still string, args ...any) {
	t.Helper()
	if !tryLockName(ctx, admin, name) {
		return
	}
	defer unlockName(ctx, admin, name)
	var ok bool
	if err := admin.QueryRow(ctx, still, append([]any{name}, args...)...).Scan(&ok); err != nil || !ok {
		return
	}
	ident := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "ALTER DATABASE "+ident+" WITH IS_TEMPLATE false"); err != nil {
		if !isCode(err, "3D000") {
			t.Logf("testkit: bỏ qua dọn %s: %v", name, err)
		}
		return
	}
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+ident); err != nil && !isCode(err, "55006") {
		t.Logf("testkit: bỏ qua dọn %s: %v", name, err)
	}
}

// templateNamePattern khớp tên mẫu migration crm_tpl_<băm 16 hex>_<giờ> và mẫu có dữ liệu …_s<băm 12 hex>.
const templateNamePattern = `^crm_tpl_[0-9a-f]{16}_[0-9]{10}(_s[0-9a-f]{12})?$`

// pruneOlderHours xóa mọi mẫu cùng băm migration với mẫu tpl (crm_tpl_<băm>_<giờ>) nhưng của giờ trước giờ của tpl, kể
// cả mẫu có dữ liệu dựng từ chúng: không ai dùng lại mẫu giờ cũ (tên mẫu tính theo giờ hiện tại). Mỗi mẫu xóa qua
// dropTemplateDB (chỉ khóa theo tên của chính nó). Tiến trình còn ở giờ cũ mà mất mẫu thì nhân bản gặp 3D000, dựng lại.
func pruneOlderHours(ctx context.Context, t testing.TB, admin *pgx.Conn, tpl string) {
	t.Helper()
	family, hour := tpl[:len(tpl)-len(hourLayout)], tpl[len(tpl)-len(hourLayout):]
	rows, err := admin.Query(ctx, `SELECT datname FROM pg_database
WHERE left(datname, length($1)) = $1 AND datname ~ $2 AND substr(datname, length($1) + 1, $3) < $4
ORDER BY datname`, family, templateNamePattern, len(hourLayout), hour)
	if err != nil {
		t.Fatalf("testkit: tìm mẫu giờ cũ của %s: %v", tpl, err)
	}
	old, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("testkit: đọc mẫu giờ cũ của %s: %v", tpl, err)
	}
	for _, name := range old {
		dropTemplateDB(ctx, t, admin, name, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)")
	}
}

func isCode(err error, code string) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == code
}
