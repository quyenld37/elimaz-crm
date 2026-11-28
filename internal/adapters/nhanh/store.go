package nhanh

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SnapshotRetention là hạn giữ bản chụp (dữ liệu khách thật chỉ nằm trên server, xóa sau 30 ngày).
const SnapshotRetention = 30 * 24 * time.Hour

const (
	snapshotPrefix = "nhanh-snapshot-"
	snapshotSuffix = ".json"
	snapshotTime   = "20060102T150405Z"
)

// WriteSnapshot ghi bản chụp vào dir (tạo 0700) thành tệp nhanh-snapshot-<giờ UTC>.json mode 0600: ghi tệp tạm cùng
// thư mục rồi đổi tên, nên không bao giờ có tệp dở. Trả đường dẫn tệp.
func WriteSnapshot(dir string, s *Snapshot) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("tạo thư mục bản chụp: %w", err)
	}
	final := filepath.Join(dir, snapshotPrefix+s.TakenAt.UTC().Format(snapshotTime)+snapshotSuffix)
	tmp, err := os.CreateTemp(dir, ".tmp-snapshot-*") // CreateTemp tạo mode 0600
	if err != nil {
		return "", fmt.Errorf("tạo tệp tạm: %w", err)
	}
	enc := json.NewEncoder(tmp)
	werr := enc.Encode(s)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("ghi bản chụp: %w", err)
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("đặt tên bản chụp: %w", err)
	}
	return final, nil
}

// PruneSnapshots xóa tệp bản chụp trong dir có giờ trong tên cũ hơn SnapshotRetention so với now; trả số tệp đã xóa.
// Chỉ đụng tệp đúng tên bản chụp; thư mục chưa có thì không việc gì.
func PruneSnapshots(dir string, now time.Time) (int, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("đọc thư mục bản chụp: %w", err)
	}
	removed := 0
	for _, e := range ents {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasPrefix(name, snapshotPrefix) || !strings.HasSuffix(name, snapshotSuffix) {
			continue
		}
		at, err := time.Parse(snapshotTime, strings.TrimSuffix(strings.TrimPrefix(name, snapshotPrefix), snapshotSuffix))
		if err != nil || now.Sub(at) <= SnapshotRetention {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, fmt.Errorf("xóa bản chụp cũ: %w", err)
		}
		removed++
	}
	return removed, nil
}
