// Package files là kho tệp dùng chung của CRM (kế hoạch M1, Task M1-41; spec 05 mục 7.3, ADR-36, ADR-45, ADR-46;
// vùng rủi ro cao): FileStore (S3 qua aws-sdk-go-v2, MemStore cho test), bảng platform.files có RLS, mục đích tệp
// (Purpose), tải thẳng từ trình duyệt bằng đường dẫn ký sẵn (CreateUpload, CompleteUpload), ghi tệp từ máy chủ
// (FileStore.Put rồi Register) và đường đọc có kiểm (DownloadURL).
//
// Luật: ứng dụng chỉ nói chuyện với kho qua FileStore; khóa trên kho do hệ thống sinh (keys.go), không bao giờ có tên
// tệp của người dùng; không ghi đè khóa đã có; mọi lời gọi kho có mạng (Head, Put, Open, CopyIfAbsent, Delete) nằm
// ngoài transaction (ADR-46), ký đường dẫn là tính tại chỗ nên được làm trong transaction.
package files

import (
	"context"
	"errors"
	"io"
	"time"
)

// Bucket là bucket logic; tên bucket thật trên kho là tiền tố cấu hình (S3_BUCKET_PREFIX) + giá trị này.
type Bucket string

// Bốn bucket của ADR-36 (Task M1-40 dựng): tạm (tự xóa sau 1 ngày), ảnh công khai, ảnh gốc, riêng tư.
const (
	BucketTmp       Bucket = "tmp"
	BucketPublic    Bucket = "public"
	BucketOriginals Bucket = "originals"
	BucketPrivate   Bucket = "private"
)

func (b Bucket) valid() bool {
	switch b {
	case BucketTmp, BucketPublic, BucketOriginals, BucketPrivate:
		return true
	}
	return false
}

// PresignedPut là đường dẫn PUT ký sẵn: trình duyệt gửi đúng URL với đúng Headers (chữ ký gồm cả content-length,
// content-type và If-None-Match: *, nên khác kích thước, khác loại hay ghi đè đều bị kho từ chối).
type PresignedPut struct {
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

// ObjectInfo là thông tin của một đối tượng trên kho.
type ObjectInfo struct {
	Size        int64
	ContentType string
	ETag        string
}

// Lỗi của FileStore.
var (
	// ErrObjectNotFound: khóa không có (hoặc chỉ còn dấu xóa).
	ErrObjectNotFound = errors.New("files: đối tượng không có trên kho")
	// ErrObjectExists: Put lên khóa đã có; không ghi đè.
	ErrObjectExists = errors.New("files: khóa đã có trên kho, không ghi đè")
	// ErrTooLarge: vượt giới hạn dung lượng (Open quá maxBytes, hay tải lên quá MaxBytes của mục đích).
	ErrTooLarge = errors.New("files: tệp quá lớn")
)

// FileStore là phần S3 chung mà CRM dùng (ADR-36). Interface vì có hai bản hiện thực thật (S3Store, MemStore) và
// chỗ cắm bản giả trong test (luật interface của M0).
type FileStore interface {
	// PresignPut ký PUT cho đúng size byte, đúng contentType, chỉ khi khóa chưa có; không gọi mạng.
	PresignPut(ctx context.Context, b Bucket, key string, size int64, contentType string, ttl time.Duration) (PresignedPut, error)
	// PresignPutAnySize ký PUT cho đúng contentType, chỉ khi khóa chưa có, không ký kích thước: chỉ cho đầu ra mà
	// máy chủ của CRM tạo (biến thể của imager, kích thước chỉ biết sau khi mã hóa), không bao giờ cho trình duyệt.
	PresignPutAnySize(ctx context.Context, b Bucket, key string, contentType string, ttl time.Duration) (PresignedPut, error)
	// PresignGet ký GET hạn ttl; attachmentName khác rỗng thì trình duyệt tải về với tên đó. Không gọi mạng.
	PresignGet(ctx context.Context, b Bucket, key string, ttl time.Duration, attachmentName string) (string, error)
	// Head đọc thông tin; không có → ErrObjectNotFound.
	Head(ctx context.Context, b Bucket, key string) (ObjectInfo, error)
	// Open mở để đọc; đối tượng hay phần đọc vượt maxBytes → ErrTooLarge.
	Open(ctx context.Context, b Bucket, key string, maxBytes int64) (io.ReadCloser, error)
	// CopyIfAbsent chép src sang dst nếu dst chưa có; dst đã có → nil, không ghi đè.
	CopyIfAbsent(ctx context.Context, src Bucket, srcKey string, dst Bucket, dstKey string) error
	// Put ghi từ máy chủ đúng size byte; khóa đã có → ErrObjectExists, không ghi đè.
	Put(ctx context.Context, b Bucket, key string, r io.Reader, size int64, contentType string) error
	// Delete chỉ tạo dấu xóa (bucket giữ phiên bản); bản cũ còn cho crm-admin.
	Delete(ctx context.Context, b Bucket, key string) error
}

// contentDisposition là giá trị Content-Disposition cho tệp tải về: tên chỉ nằm trong filename* (RFC 5987, đã mã hóa)
// và một tên ASCII thay thế, nên tên của người dùng không chèn được header hay tham số khác.
func contentDisposition(name string) string {
	if name == "" {
		return ""
	}
	return `attachment; filename="download"; filename*=UTF-8''` + rfc5987Escape(name)
}

// rfc5987Escape mã hóa mọi byte ngoài tập ký tự an toàn của RFC 5987 (attr-char).
func rfc5987Escape(s string) string {
	const hex = "0123456789ABCDEF"
	out := make([]byte, 0, len(s)*3)
	for i := range len(s) {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			c == '!', c == '#', c == '$', c == '&', c == '+', c == '-', c == '.', c == '^', c == '_', c == '`', c == '|', c == '~':
			out = append(out, c)
		default:
			out = append(out, '%', hex[c>>4], hex[c&15])
		}
	}
	return string(out)
}
