package files

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OriginalTTL là hạn của đường dẫn tải ảnh gốc cho nhân viên (TP05): đường dẫn bị chia sẻ ra ngoài cũng hết hạn sau
// 5 phút, lấy đường dẫn mới phải qua kiểm quyền của bên gọi.
const OriginalTTL = 5 * time.Minute

// SignedURL là đường dẫn GET ký sẵn và thời điểm hết hạn của nó.
type SignedURL struct {
	URL       string
	ExpiresAt time.Time
}

// originalExt là đuôi tệp tải về theo loại ảnh phát hiện được ("image/jpeg" → ".jpg", "image/png" → ".png",
// "image/webp" → ".webp"; mục đích ảnh chỉ nhận ba loại này); loại khác → "".
func originalExt(detectedType string) string {
	sub, ok := strings.CutPrefix(detectedType, "image/")
	switch {
	case !ok || sub == "":
		return ""
	case sub == "jpeg":
		sub = "jpg"
	}
	return "." + sub
}

// OriginalURL ký GET hạn ttl tới ảnh gốc orig/<sha> (BucketOriginals) của ảnh fileID trong tenant của tx; trình duyệt
// tải về với tên baseName + đuôi theo loại ảnh (".jpg", ".png", ".webp"). Không gọi mạng (ký được trong transaction).
// Chỉ ảnh của mục đích chế độ standard có ảnh gốc: tệp không phải ảnh hay ảnh riêng tư (R10, chỉ có bản đã bỏ siêu dữ
// liệu) → ErrNotFound; các trạng thái khác như DownloadURL (không có, đã xóa, mất → ErrNotFound; chưa xử lý xong →
// ErrNotReady; rejected → ErrRejected). Bên gọi kiểm quyền của mình trước.
func OriginalURL(ctx context.Context, tx pgx.Tx, fs FileStore, fileID uuid.UUID, ttl time.Duration, baseName string) (SignedURL, error) {
	row, err := getFile(ctx, tx, fileID)
	if err != nil {
		return SignedURL{}, err
	}
	if p, ok := PurposeOf(row.Purpose); !ok || !p.Image || p.Private {
		return SignedURL{}, fmt.Errorf("ảnh gốc của tệp %s: %w", fileID, ErrNotFound)
	}
	// ExpiresAt tính trước khi ký: không bao giờ muộn hơn hạn thật của chữ ký.
	exp := time.Now().Add(ttl)
	url, err := DownloadURL(ctx, tx, fs, fileID, ttl, baseName+originalExt(row.DetectedType.String))
	if err != nil {
		return SignedURL{}, err
	}
	return SignedURL{URL: url, ExpiresAt: exp}, nil
}
