package testkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/migrations"
)

// Database mẫu có dữ liệu thử (Task 423): test cần bộ dữ liệu lớn cố định (hàng chục nghìn dòng sinh bằng
// generate_series, kèm ANALYZE) dựng bộ đó một lần vào crm_tpl_<băm migration>_<giờ>_s<băm dữ liệu>, nhân bản từ mẫu
// migration của Task 391; các lần sau CREATE DATABASE … TEMPLATE chép tệp, không chạy lại INSERT. Thống kê của
// ANALYZE (pg_statistic, reltuples) và bản đồ hiển thị của VACUUM nằm trong tệp database nên đi theo bản sao. Thiết
// lập theo role trong database (ALTER ROLE … IN DATABASE, ví dụ EnableAutoExplain) không đi theo: gọi sau khi nhân bản.

const (
	// seedRecipe phải đổi khi cách dựng mẫu có dữ liệu đổi (cách chạy câu, vai trò chạy câu…).
	seedRecipe = "testkit-seed-v1"
	// seedTimeout: dựng mẫu có dữ liệu lúc máy chủ chung bận có thể mất vài phút (một câu INSERT … generate_series lớn
	// từng chạy 2 phút khi 6–7 phiên song song).
	seedTimeout = 8 * time.Minute
)

// Seed là bộ dữ liệu thử cố định dựng vào database mẫu. Mã băm phủ Name và nguyên văn mọi câu của SQL: đổi một ký tự
// là mẫu mới. Vì vậy câu phải tự đủ (giá trị viết thẳng trong câu, mã cố định từ FixedUUID), không phụ thuộc biến của
// test; giá trị ngẫu nhiên trong câu (random(), uuidv7() mặc định, now()) bị đóng băng ở lúc dựng mẫu, cũ tối đa một
// giờ như mẫu migration.
type Seed struct {
	// Name: tên ngắn đọc được của bộ dữ liệu (ví dụ "queries-fixture"), phân biệt hai bộ có câu trùng nhau.
	Name string
	// SQL: các câu chạy lần lượt bằng người quản trị (bỏ qua RLS) trên database mẫu, mỗi câu tự commit, trên cùng một
	// kết nối.
	SQL []string
}

// StartPostgresSeeded như StartPostgres nhưng database đã có dữ liệu của s: lần đầu (mỗi giờ, mỗi bộ migration, mỗi
// nội dung s) dựng mẫu có dữ liệu dưới khóa (nhiều tiến trình, nhiều phiên không dựng trùng), các lần sau chỉ nhân bản.
// Mẫu cũ dọn theo cùng luật với mẫu migration (cleanupStale, pruneOlderHours).
func StartPostgresSeeded(t testing.TB, s Seed) (adminURL, ownerURL, appURL string) {
	t.Helper()
	return cloneSeeded(t, migrations.FS, migrateDB, time.Now(), s)
}

// OpenStaffSeeded như OpenStaff trên database của StartPostgresSeeded(t, s).
func OpenStaffSeeded(t testing.TB, s Seed) *Staff {
	t.Helper()
	_, ownerURL, appURL := StartPostgresSeeded(t, s)
	return openStaff(t, ownerURL, appURL)
}

// fixedUUIDSpace là không gian tên của FixedUUID (chọn một lần, không đổi: đổi là mọi mẫu có dữ liệu dựng lại).
const fixedUUIDSpace = "6f3c0c1e-4b23-5d0a-9a51-423000000423"

// FixedUUID trả mã UUID (phiên bản 5) cố định theo name: cùng name luôn cùng mã, ở mọi lần chạy. Dùng cho mã viết
// thẳng trong câu của Seed (mã ngẫu nhiên đổi mỗi lần chạy, làm mẫu không bao giờ dùng lại được). Mã không tăng theo
// thời gian như uuidv7.
func FixedUUID(name string) uuid.UUID {
	return uuid.NewSHA1(uuid.MustParse(fixedUUIDSpace), []byte(name))
}

// seededName là tên mẫu có dữ liệu s dựng từ mẫu migration tpl: tpl + "_s" + 12 ký tự hex của sha256(seedRecipe,
// Name, mọi câu). Dài tối đa 49 ký tự (giới hạn tên PostgreSQL 63); tiền tố crm_tpl_ của tpl để cleanupStale dọn.
func seededName(tpl string, s Seed) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%d\x00%s\x00", seedRecipe, len(s.Name), s.Name)
	for _, q := range s.SQL {
		_, _ = fmt.Fprintf(h, "%d\x00", len(q))
		h.Write([]byte(q))
	}
	return tpl + "_s" + hex.EncodeToString(h.Sum(nil))[:12]
}

// cloneSeeded tạo database crm_test_<ngẫu nhiên> nhân bản từ mẫu có dữ liệu s của bộ migration fsys ở thời điểm at
// (dựng mẫu nếu chưa có), xóa khi test xong. Trước đó dọn database test và mẫu bị bỏ sót.
func cloneSeeded(t testing.TB, fsys fs.FS, migrate migrateFunc, at time.Time, s Seed) (adminURL, ownerURL, appURL string) {
	t.Helper()
	base := serverURL(t)
	tpl := templateName(t, fsys, at)
	stpl := seededName(tpl, s)
	name, adminURL, ownerURL, appURL := newTestDB(t, base)
	dropAtCleanup(t, base, name)
	ctx, cancel := context.WithTimeout(context.Background(), seedTimeout)
	defer cancel()
	withAdmin(ctx, t, base, func(admin *pgx.Conn) {
		cleanupStale(ctx, t, admin, cleanupLockKey, []string{testDBPrefix, templatePrefix}, stpl, staleAfter)
		createFromTemplate(ctx, t, admin, name, stpl, func() {
			ensureSeeded(ctx, t, admin, base, tpl, stpl, fsys, migrate, s)
		})
		pruneOlderHours(ctx, t, admin, tpl)
	})
	return adminURL, ownerURL, appURL
}

// createFromTemplate tạo database name (chủ crm_owner) bằng CREATE DATABASE … TEMPLATE src, gọi ensure trước mỗi lần
// thử. Mẫu có thể vừa bị phiên khác dọn (3D000), hay một kết nối cũ vào mẫu chưa thoát hẳn (55006): ensure dựng/kiểm
// lại mẫu rồi thử lại, tối đa 5 lần.
func createFromTemplate(ctx context.Context, t testing.TB, admin *pgx.Conn, name, src string, ensure func()) {
	t.Helper()
	const attempts = 5
	for i := 1; ; i++ {
		ensure()
		_, err := admin.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE "+src+" OWNER crm_owner")
		if err == nil {
			return
		}
		if i >= attempts || (!isCode(err, "3D000") && !isCode(err, "55006")) {
			t.Fatalf("testkit: nhân bản database từ %s: %v", src, err)
		}
		time.Sleep(time.Duration(i) * 200 * time.Millisecond)
	}
}

// ensureSeeded bảo đảm mẫu có dữ liệu stpl đã dựng xong (datistemplate = true, đặt ở bước cuối). Chưa có thì dựng dưới
// khóa (nameLockKey, hashtext(stpl)): nhân bản từ mẫu migration tpl (dựng tpl nếu thiếu), chạy các câu của s bằng
// người quản trị, chặn mọi kết nối vào mẫu, đánh dấu mẫu. Mẫu dở dang (tiến trình dựng chết giữa chừng, hay câu lỗi)
// bị xóa và dựng lại.
func ensureSeeded(ctx context.Context, t testing.TB, admin *pgx.Conn, base *url.URL, tpl, stpl string, fsys fs.FS,
	migrate migrateFunc, s Seed,
) {
	t.Helper()
	if exists, done := templateState(ctx, t, admin, stpl); exists && done {
		return
	}
	lockName(ctx, t, admin, stpl)
	defer unlockName(ctx, admin, stpl)
	exists, done := templateState(ctx, t, admin, stpl)
	if done {
		return
	}
	if exists {
		exec(ctx, t, admin, "DROP DATABASE "+stpl+" WITH (FORCE)")
	}
	createFromTemplate(ctx, t, admin, stpl, tpl, func() { ensureTemplate(ctx, t, admin, base, tpl, fsys, migrate) })
	if err := runSeed(ctx, withDB(base, nil, stpl), s); err != nil {
		_, _ = admin.Exec(context.WithoutCancel(ctx), "DROP DATABASE IF EXISTS "+stpl+" WITH (FORCE)")
		t.Fatalf("testkit: dựng dữ liệu %q vào database mẫu %s: %v", s.Name, stpl, err)
	}
	exec(ctx, t, admin, "ALTER DATABASE "+stpl+" WITH ALLOW_CONNECTIONS false")
	exec(ctx, t, admin, "ALTER DATABASE "+stpl+" WITH IS_TEMPLATE true")
}

// runSeed chạy lần lượt các câu của s trên một kết nối người quản trị tới adminURL (mỗi câu tự commit).
func runSeed(ctx context.Context, adminURL string, s Seed) error {
	c, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("nối database mẫu: %w", err)
	}
	defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
	for i, q := range s.SQL {
		if _, err := c.Exec(ctx, q); err != nil {
			return fmt.Errorf("câu %d: %w", i+1, err)
		}
	}
	return nil
}
