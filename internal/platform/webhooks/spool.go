package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/db"
)

// Quyền của tệp tạm: chỉ tiến trình ingest đọc được (thân webhook có tin nhắn của khách).
const spoolFileMode = 0o600

// corruptSuffix: tệp tạm không đọc được được đổi tên thêm đuôi này, giữ lại để người vận hành xem, không chặn hàng nạp.
const corruptSuffix = ".corrupt"

// spoolName là mẫu tên tệp tạm: <epochMs>-<uuid>.json.
var spoolName = regexp.MustCompile(`^(\d{13,})-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.json$`)

// ErrCorruptSpoolFile báo một tệp tạm không đọc được; tệp đã được đổi tên thêm đuôi .corrupt.
var ErrCorruptSpoolFile = errors.New("tệp tạm webhook hỏng")

// Spool là thư mục tệp tạm của ingest (volume riêng): mỗi lần nhận mà database không ghi được thành một tệp
// <epochMs>-<uuid>.json, nạp lại theo tên tăng dần. Một tiến trình ingest trên một thư mục.
type Spool struct {
	Dir string
}

// Write ghi e thành một tệp mới: ghi vào tên tạm, fsync, đổi tên, fsync thư mục; không bao giờ để lại tệp dở dưới
// tên thật. Trả tên tệp.
func (s *Spool) Write(e Entry) (string, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("mã hóa tệp tạm: %w", err)
	}
	id, err := uuid.NewV7() // tăng dần trong cùng mili giây
	if err != nil {
		return "", fmt.Errorf("tạo tên tệp tạm: %w", err)
	}
	name := strconv.FormatInt(e.ReceivedAt.UnixMilli(), 10) + "-" + id.String() + ".json"
	f, err := os.CreateTemp(s.Dir, ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("tạo tệp tạm: %w", err)
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(spoolFileMode); err != nil {
		return "", fmt.Errorf("đặt quyền tệp tạm: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return "", fmt.Errorf("ghi tệp tạm: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("fsync tệp tạm: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("đóng tệp tạm: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(s.Dir, name)); err != nil {
		return "", fmt.Errorf("đổi tên tệp tạm: %w", err)
	}
	ok = true
	if err := s.syncDir(); err != nil {
		return "", err
	}
	return name, nil
}

func (s *Spool) syncDir() error {
	d, err := os.Open(s.Dir)
	if err != nil {
		return fmt.Errorf("mở thư mục tệp tạm: %w", err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("fsync thư mục tệp tạm: %w", err)
	}
	return nil
}

// List trả tên các tệp tạm chờ nạp (đúng mẫu tên), tăng dần. Tệp khác trong thư mục bị bỏ qua.
func (s *Spool) List() ([]string, error) {
	es, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, fmt.Errorf("đọc thư mục tệp tạm: %w", err)
	}
	var out []string
	for _, e := range es {
		if e.Type().IsRegular() && spoolName.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	slices.SortFunc(out, compareSpoolNames)
	return out, nil
}

// compareSpoolNames so theo epochMs (số) rồi theo uuid v7 (tăng dần trong một mili giây).
func compareSpoolNames(a, b string) int {
	ma, _ := strconv.ParseInt(spoolName.FindStringSubmatch(a)[1], 10, 64)
	mb, _ := strconv.ParseInt(spoolName.FindStringSubmatch(b)[1], 10, 64)
	if ma != mb {
		if ma < mb {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// Drain nạp các tệp theo tên tăng dần, mỗi tệp một transaction như đường thường (Save, giữ received_at gốc; trùng
// thân với dòng đã có thì không thêm), commit xong mới xóa tệp. Database lỗi thì dừng ngay, giữ tệp đó và các tệp
// sau. Tệp không đọc được đổi tên thêm đuôi .corrupt rồi nạp tiếp; lỗi trả về bọc ErrCorruptSpoolFile. n là số tệp
// đã nạp xong và xóa.
func (s *Spool) Drain(ctx context.Context, d *db.DB) (n int, err error) {
	names, err := s.List()
	if err != nil {
		return 0, err
	}
	var corrupt []error
	for _, name := range names {
		path := filepath.Join(s.Dir, name)
		e, err := readEntry(path)
		if err != nil {
			if rerr := os.Rename(path, path+corruptSuffix); rerr != nil {
				return n, fmt.Errorf("giữ lại tệp tạm hỏng %s: %w", name, rerr)
			}
			corrupt = append(corrupt, fmt.Errorf("%w: %s: %w", ErrCorruptSpoolFile, name, err))
			continue
		}
		if _, err := Save(ctx, d, e); err != nil {
			return n, errors.Join(append(corrupt, fmt.Errorf("nạp tệp tạm %s: %w", name, err))...)
		}
		if err := os.Remove(path); err != nil {
			return n, fmt.Errorf("xóa tệp tạm đã nạp %s: %w", name, err)
		}
		n++
	}
	return n, errors.Join(corrupt...)
}

func readEntry(path string) (Entry, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path là tệp trong thư mục tệp tạm, tên đã khớp spoolName
	if err != nil {
		return Entry{}, fmt.Errorf("đọc: %w", err)
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, fmt.Errorf("giải mã: %w", err)
	}
	if e.Source == "" || e.ReceivedAt.IsZero() {
		return Entry{}, errors.New("thiếu source hoặc receivedAt")
	}
	if e.RouteKey != "" && !routeKeyPattern.MatchString(e.RouteKey) {
		return Entry{}, errors.New("routeKey sai mẫu")
	}
	return e, nil
}

// Stats trả số tệp chờ nạp và tuổi của tệp cũ nhất (theo giờ nhận ghi trong tên).
func (s *Spool) Stats() (files int, oldestAge time.Duration, err error) {
	names, err := s.List()
	if err != nil || len(names) == 0 {
		return 0, 0, err
	}
	ms, _ := strconv.ParseInt(spoolName.FindStringSubmatch(names[0])[1], 10, 64)
	return len(names), max(time.Since(time.UnixMilli(ms)), 0), nil
}
