package logging

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Mặc định của tệp log theo ngày (Global Constraints của kế hoạch M0).
const (
	DefaultKeep     = 30
	DefaultMaxBytes = 200 << 20
)

// DailyFile là io.Writer ghi vào <Dir>/<Role>-YYYY-MM-DD.log (ngày theo UTC). Sang ngày thì mở tệp mới và xóa tệp
// cũ của cùng vai trò, chỉ giữ Keep tệp. Vượt MaxBytes trong một ngày thì bỏ dòng và đếm (Dropped). An toàn khi gọi
// đồng thời.
type DailyFile struct {
	Dir      string
	Role     string
	Keep     int
	MaxBytes int64
	Now      func() time.Time

	mu      sync.Mutex
	f       *os.File
	day     string
	written int64
	dropped int64
}

// NewDailyFile tạo DailyFile với Keep, MaxBytes mặc định và đồng hồ thật. Tệp chỉ được mở ở lần Write đầu.
func NewDailyFile(dir, role string) *DailyFile {
	return &DailyFile{Dir: dir, Role: role, Keep: DefaultKeep, MaxBytes: DefaultMaxBytes, Now: time.Now}
}

// Check tạo thư mục log nếu chưa có; dùng khi khởi động để báo lỗi sớm.
func (d *DailyFile) Check() error {
	if err := os.MkdirAll(d.Dir, 0o750); err != nil {
		return fmt.Errorf("tạo thư mục log %s: %w", d.Dir, err)
	}
	return nil
}

// Write ghi p vào tệp của ngày hiện tại. Dòng bị bỏ vì vượt MaxBytes vẫn trả len(p), nil để không chặn stdout.
func (d *DailyFile) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	day := d.Now().UTC().Format(time.DateOnly)
	if d.f == nil || day != d.day {
		if err := d.rotate(day); err != nil {
			return 0, err
		}
	}
	if d.written+int64(len(p)) > d.MaxBytes {
		d.dropped++
		return len(p), nil
	}
	n, err := d.f.Write(p)
	d.written += int64(n)
	if err != nil {
		return n, fmt.Errorf("ghi tệp log: %w", err)
	}
	return n, nil
}

// Dropped trả số dòng đã bỏ vì vượt MaxBytes.
func (d *DailyFile) Dropped() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dropped
}

// Close đóng tệp đang mở.
func (d *DailyFile) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.f == nil {
		return nil
	}
	err := d.f.Close()
	d.f = nil
	if err != nil {
		return fmt.Errorf("đóng tệp log: %w", err)
	}
	return nil
}

func (d *DailyFile) rotate(day string) error {
	if d.f != nil {
		if err := d.f.Close(); err != nil {
			return fmt.Errorf("đóng tệp log cũ: %w", err)
		}
		d.f = nil
	}
	if err := d.Check(); err != nil {
		return err
	}
	root, err := os.OpenRoot(d.Dir)
	if err != nil {
		return fmt.Errorf("mở thư mục log: %w", err)
	}
	defer func() { _ = root.Close() }()
	f, err := root.OpenFile(d.Role+"-"+day+".log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("mở tệp log: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("đọc cỡ tệp log: %w", err)
	}
	d.f, d.day, d.written = f, day, st.Size()
	return d.prune(root)
}

// prune xóa tệp cũ của cùng vai trò, giữ Keep tệp mới nhất (tên chứa ngày nên thứ tự chữ là thứ tự ngày).
func (d *DailyFile) prune(root *os.Root) error {
	matches, err := filepath.Glob(filepath.Join(d.Dir, d.Role+"-????-??-??.log"))
	if err != nil {
		return fmt.Errorf("liệt kê tệp log: %w", err)
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, filepath.Base(m))
	}
	slices.Sort(names)
	for len(names) > d.Keep {
		if err := root.Remove(names[0]); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("xóa tệp log cũ: %w", err)
		}
		names = names[1:]
	}
	return nil
}
