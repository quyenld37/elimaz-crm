package cache

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// ErrBadKey báo một phần của khóa Valkey ngoài ^[A-Za-z0-9_-]{1,100}$ (EntityKey không bao giờ ghép chuỗi tùy ý vào khóa).
var ErrBadKey = errors.New("cache: phần khóa không hợp lệ")

// keyPart là dạng của tiền tố và mỗi phần của EntityKey.
var keyPart = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// subjectHashLen: số ký tự hex của phần băm trong SubjectKey (128 bit).
const subjectHashLen = 32

// SubjectKey trả kind + ":" + 32 ký tự hex đầu của HMAC-SHA256(secret, kind + "\x00" + value). Mọi khóa chứa SĐT, IP,
// email đi qua hàm này (QĐ22): khóa không mang dữ liệu cá nhân, cùng secret thì cùng khóa, khác secret thì khác.
// secret là CACHE_KEY_SECRET (≥ 32 ký tự, config kiểm). kind là hằng của mã gọi ("phone", "ip"), không phải dữ liệu.
func SubjectKey(secret []byte, kind, value string) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(kind))
	m.Write([]byte{0})
	m.Write([]byte(value))
	return kind + ":" + hex.EncodeToString(m.Sum(nil))[:subjectHashLen]
}

// EntityKey trả prefix + ":" + tenant + ":" + parts nối bằng ":"; prefix và mỗi phần phải khớp
// ^[A-Za-z0-9_-]{1,100}$, sai thì ErrBadKey. Dữ liệu cá nhân không đi vào đây (dùng SubjectKey).
func EntityKey(prefix string, tenant uuid.UUID, parts ...string) (string, error) {
	if !keyPart.MatchString(prefix) {
		return "", fmt.Errorf("%w: tiền tố", ErrBadKey)
	}
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteByte(':')
	b.WriteString(tenant.String())
	for i, p := range parts {
		if !keyPart.MatchString(p) {
			return "", fmt.Errorf("%w: phần thứ %d", ErrBadKey, i+1)
		}
		b.WriteByte(':')
		b.WriteString(p)
	}
	return b.String(), nil
}
