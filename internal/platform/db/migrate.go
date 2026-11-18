package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/migrations"
)

// migrateLockKey là khóa advisory của mọi lần chạy migrate (hai tiến trình migrate cùng lúc chạy lần lượt).
const migrateLockKey = 727001

// gooseTable: bảng phiên bản của goose nằm trong schema platform (không bảng nào trong public).
const gooseTable = "platform.goose_db_version"

// ErrMigration báo migration bị từ chối trước khi chạy (checksum lệch, thiếu tệp đã áp dụng, lệnh thu hẹp thiếu
// "-- contract:"). Thông báo lỗi nêu tên tệp.
var ErrMigration = errors.New("migration bị từ chối")

// Migrate áp dụng các tệp của migrations.FS bằng goose, với chuỗi kết nối của chủ bảng (chỉ vai trò migrate có).
// Cả lần chạy giữ pg_advisory_lock(727001) trên một kết nối riêng. Trước khi chạy: so sha256 của từng tệp đã áp dụng
// với platform.migration_checksums (lệch hay mất tệp thì từ chối, nêu tên tệp) và kiểm LintContract trên các tệp sắp
// chạy. Tệp có số nhỏ hơn số lớn nhất đã áp dụng (gộp lệch thứ tự) vẫn chạy, trước các tệp mới. Mỗi tệp chạy trong
// transaction riêng của goose; tệp lỗi không để lại gì. Trả tên các tệp vừa áp dụng, theo thứ tự chạy. Câu của goose chạy với ba giới hạn của ADR-26 như mọi kết nối ứng dụng.
func Migrate(ctx context.Context, ownerURL string) (applied []string, err error) {
	return migrate(ctx, ownerURL, migrations.FS)
}

func migrate(ctx context.Context, ownerURL string, fsys fs.FS) (applied []string, err error) {
	cc, err := pgx.ParseConfig(ownerURL)
	if err != nil {
		return nil, fmt.Errorf("đọc chuỗi kết nối của chủ bảng: %w", err)
	}
	cc.DefaultQueryExecMode = pgx.QueryExecModeExec
	lockConn, err := pgx.ConnectConfig(ctx, cc)
	if err != nil {
		return nil, fmt.Errorf("nối database để migrate: %w", err)
	}
	defer func() { _ = lockConn.Close(context.WithoutCancel(ctx)) }()

	q := store.New(lockConn)
	if err := q.AdvisoryLock(ctx, migrateLockKey); err != nil {
		return nil, fmt.Errorf("chờ khóa migrate: %w", err)
	}
	defer func() { _ = q.AdvisoryUnlock(context.WithoutCancel(ctx), migrateLockKey) }()

	if err := ensurePlatformSchema(ctx, lockConn); err != nil {
		return nil, err
	}
	if err := verifyChecksums(ctx, q, fsys); err != nil {
		return nil, err
	}

	gooseCfg := cc.Copy()
	setSessionLimits(gooseCfg, 0, 0, 0)
	sqlDB := stdlib.OpenDB(*gooseCfg)
	defer func() { _ = sqlDB.Close() }()
	// Ngân sách kết nối của vai trò migrate là 2 (ADR-42): kết nối giữ khóa cộng một kết nối cho goose.
	sqlDB.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, fsys,
		goose.WithTableName(gooseTable), goose.WithDisableGlobalRegistry(true),
		// Các làn song song gộp migration lệch số (Task 318): tệp số nhỏ hơn số lớn nhất đã áp dụng vẫn chạy, trước
		// các tệp mới. Checksum và luật "-- contract:" ở trên vẫn kiểm mọi tệp, kể cả tệp lệch số.
		goose.WithAllowOutofOrder(true))
	if err != nil {
		return nil, fmt.Errorf("dựng goose: %w", err)
	}
	if err := lintPending(ctx, provider, fsys); err != nil {
		return nil, err
	}
	results, upErr := provider.Up(ctx)
	if pe := (*goose.PartialError)(nil); errors.As(upErr, &pe) {
		results = pe.Applied
		upErr = fmt.Errorf("%s: %w", path.Base(pe.Failed.Source.Path), upErr)
	}
	for _, r := range results {
		if r.Error == nil {
			applied = append(applied, path.Base(r.Source.Path))
		}
	}
	// Ghi checksum cả khi có tệp lỗi: các tệp trước nó đã áp dụng.
	if err := recordChecksums(ctx, q, provider, fsys); err != nil {
		return applied, errors.Join(upErr, err)
	}
	if upErr != nil {
		return applied, fmt.Errorf("chạy migration: %w", upErr)
	}
	return applied, nil
}

// verifyChecksums: mọi tệp đã ghi checksum phải còn và không đổi.
func verifyChecksums(ctx context.Context, q *store.Queries, fsys fs.FS) error {
	present, err := q.MigrationChecksumsExist(ctx)
	if err != nil {
		return fmt.Errorf("kiểm bảng checksum: %w", err)
	}
	if !present {
		return nil
	}
	rows, err := q.ListMigrationChecksums(ctx)
	if err != nil {
		return fmt.Errorf("đọc checksum migration: %w", err)
	}
	for _, r := range rows {
		sum, err := fileSHA256(fsys, r.Version)
		if err != nil {
			return fmt.Errorf("%w: %s đã áp dụng nhưng không đọc được (%w)", ErrMigration, r.Version, err)
		}
		if sum != r.Sha256 {
			return fmt.Errorf("%w: %s đã áp dụng rồi bị sửa (checksum lệch); thêm tệp mới thay vì sửa tệp cũ",
				ErrMigration, r.Version)
		}
	}
	return nil
}

// lintPending kiểm LintContract trên các tệp goose sắp chạy.
func lintPending(ctx context.Context, p *goose.Provider, fsys fs.FS) error {
	pending, err := sourcesIn(ctx, p, goose.StatePending)
	if err != nil {
		return err
	}
	for _, src := range pending {
		b, err := fs.ReadFile(fsys, src)
		if err != nil {
			return fmt.Errorf("đọc %s: %w", src, err)
		}
		if v := LintContract(string(b)); len(v) > 0 {
			return fmt.Errorf("%w: %s có lệnh thu hẹp thiếu dòng \"-- contract:\" ngay phía trên: %s",
				ErrMigration, path.Base(src), strings.Join(v, "; "))
		}
	}
	return nil
}

// recordChecksums ghi checksum cho mọi tệp đã áp dụng chưa có dòng (ON CONFLICT DO NOTHING).
func recordChecksums(ctx context.Context, q *store.Queries, p *goose.Provider, fsys fs.FS) error {
	applied, err := sourcesIn(ctx, p, goose.StateApplied)
	if err != nil {
		return err
	}
	for _, src := range applied {
		name := path.Base(src)
		sum, err := fileSHA256(fsys, src)
		if err != nil {
			return fmt.Errorf("đọc %s: %w", name, err)
		}
		if err := q.InsertMigrationChecksum(ctx, store.InsertMigrationChecksumParams{Version: name, Sha256: sum}); err != nil {
			return fmt.Errorf("ghi checksum của %s: %w", name, err)
		}
	}
	return nil
}

// sourcesIn trả đường dẫn (trong fsys) của các tệp goose đang ở trạng thái state, theo thứ tự phiên bản.
func sourcesIn(ctx context.Context, p *goose.Provider, state goose.State) ([]string, error) {
	statuses, err := p.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc trạng thái migration: %w", err)
	}
	var out []string
	for _, s := range statuses {
		if s.State == state {
			out = append(out, s.Source.Path)
		}
	}
	return out, nil
}

func fileSHA256(fsys fs.FS, name string) (string, error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return "", fmt.Errorf("đọc tệp migration: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
