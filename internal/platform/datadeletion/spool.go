package datadeletion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

const (
	// spoolMode: chỉ tiến trình ingest đọc được.
	spoolMode = 0o600
	// SpoolSubdir là thư mục con của SPOOL_DIR giữ tệp tạm của yêu cầu xóa (tách khỏi tệp tạm webhook).
	SpoolSubdir = "data-deletion"
	// drainEvery: ingest nạp lại tệp tạm mỗi 5 s (như webhook).
	drainEvery = 5 * time.Second
)

// spoolFile là mẫu tên tệp tạm: <mã xác nhận>.json (mã đã ngẫu nhiên, duy nhất).
var spoolFile = regexp.MustCompile(`^[0-9a-f]{32}\.json$`)

// request là một yêu cầu xóa đã nhận (cũng là nội dung tệp tạm): chỉ có băm, không có mã người dùng gốc.
type request struct {
	Code        string    `json:"code"`
	Source      string    `json:"source"`
	UserRefHash string    `json:"userRefHash"`
	ReceivedAt  time.Time `json:"receivedAt"`
}

func (r request) valid() bool {
	return codePattern.MatchString(r.Code) && r.Source != "" && hashPattern.MatchString(r.UserRefHash) && !r.ReceivedAt.IsZero()
}

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// insert ghi r (trùng mã thì bỏ qua) trong tx.
func insert(ctx context.Context, tx pgx.Tx, r request) error {
	if err := store.New(tx).InsertDataDeletionRequest(ctx, store.InsertDataDeletionRequestParams{
		Code: r.Code, Source: r.Source, UserRefHash: r.UserRefHash, ReceivedAt: r.ReceivedAt,
	}); err != nil {
		return fmt.Errorf("ghi yêu cầu xóa: %w", err)
	}
	return nil
}

// Spool là thư mục tệp tạm của yêu cầu xóa khi database không ghi được: mỗi yêu cầu một tệp <mã>.json, ghi tên tạm →
// fsync → đổi tên (không bao giờ để tệp dở dưới tên thật). Một tiến trình ingest trên một thư mục.
type Spool struct {
	Dir string
}

// Write ghi r thành tệp <mã>.json.
func (s *Spool) Write(r request) error {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("mã hóa tệp tạm yêu cầu xóa: %w", err)
	}
	f, err := os.CreateTemp(s.Dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("tạo tệp tạm yêu cầu xóa: %w", err)
	}
	tmp := f.Name()
	werr := errors.Join(f.Chmod(spoolMode), writeSync(f, data), f.Close())
	if werr == nil {
		werr = os.Rename(tmp, filepath.Join(s.Dir, r.Code+".json"))
	}
	if werr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("ghi tệp tạm yêu cầu xóa: %w", werr)
	}
	d, err := os.Open(s.Dir)
	if err != nil {
		return fmt.Errorf("mở thư mục tệp tạm yêu cầu xóa: %w", err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("fsync thư mục tệp tạm yêu cầu xóa: %w", err)
	}
	return nil
}

func writeSync(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		return err //nolint:wrapcheck // bọc ở Write
	}
	return f.Sync() //nolint:wrapcheck // bọc ở Write
}

// Drain nạp mọi tệp tạm vào database (mỗi tệp một transaction, trùng mã thì bỏ qua), commit xong mới xóa tệp.
// Database lỗi thì dừng, giữ tệp. Tệp không đọc được đổi tên thêm đuôi .corrupt (giữ cho người vận hành), nạp tiếp.
// n là số tệp đã nạp và xóa.
func (s *Spool) Drain(ctx context.Context, d *db.DB) (n int, err error) {
	es, err := os.ReadDir(s.Dir)
	if err != nil {
		return 0, fmt.Errorf("đọc thư mục tệp tạm yêu cầu xóa: %w", err)
	}
	var bad []error
	for _, e := range es {
		if !e.Type().IsRegular() || !spoolFile.MatchString(e.Name()) {
			continue
		}
		path := filepath.Join(s.Dir, e.Name())
		var r request
		raw, rerr := os.ReadFile(path) //nolint:gosec // tên đã khớp spoolFile, trong thư mục tệp tạm
		if rerr == nil {
			rerr = json.Unmarshal(raw, &r)
		}
		if rerr != nil || !r.valid() {
			if err := os.Rename(path, path+".corrupt"); err != nil {
				return n, fmt.Errorf("giữ lại tệp tạm hỏng %s: %w", e.Name(), err)
			}
			bad = append(bad, fmt.Errorf("tệp tạm yêu cầu xóa hỏng: %s", e.Name()))
			continue
		}
		if err := d.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error { return insert(ctx, tx, r) }); err != nil {
			return n, errors.Join(append(bad, fmt.Errorf("nạp tệp tạm %s: %w", e.Name(), err))...)
		}
		if err := os.Remove(path); err != nil {
			return n, fmt.Errorf("xóa tệp tạm đã nạp %s: %w", e.Name(), err)
		}
		n++
	}
	return n, errors.Join(bad...)
}

// Drainer chạy trong ingest: ngay khi chạy rồi mỗi 5 s nạp lại tệp tạm (Spool.Drain). Lỗi ghi log một lần khi bắt
// đầu lỗi, một lần khi hết lỗi.
type Drainer struct {
	Spool  *Spool
	DB     *db.DB
	Logger *slog.Logger
}

// Run chạy tới khi ctx bị hủy.
func (d *Drainer) Run(ctx context.Context) {
	t := time.NewTicker(drainEvery)
	defer t.Stop()
	failing := false
	for {
		n, err := d.Spool.Drain(ctx, d.DB)
		switch {
		case err != nil && !failing:
			d.Logger.Warn("nạp tệp tạm yêu cầu xóa dữ liệu", slog.Int("loaded", n), logging.Err(err))
		case err == nil && (failing || n > 0):
			d.Logger.Info("đã nạp tệp tạm yêu cầu xóa dữ liệu", slog.Int("loaded", n))
		}
		failing = err != nil
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
