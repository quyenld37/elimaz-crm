// Package keyspec đọc chuỗi khóa của biến FIELD_ENC_KEYS: `<key_id>:<base64 32 byte>` cách nhau dấu phẩy, khóa đầu là
// khóa đang dùng để mã hóa. Tách khỏi fieldcrypt để config kiểm định dạng lúc khởi động mà không nhập logging (config
// ← logging ← fieldcrypt tạo vòng). Lỗi chỉ nêu vị trí mục, không bao giờ chứa khóa hay key_id.
package keyspec

import (
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// KeySize là độ dài khóa AES-256 (byte).
const KeySize = 32

// ErrInvalid báo chuỗi khóa sai định dạng.
var ErrInvalid = errors.New("chuỗi khóa không hợp lệ")

var keyID = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

// Key là một khóa: ID khớp [a-z0-9]{1,16}, Secret đúng KeySize byte.
type Key struct {
	ID     string
	Secret []byte
}

// Parse đọc spec theo thứ tự (khóa đầu là khóa mã hóa). Rỗng, mục rỗng, thiếu dấu hai chấm, key_id sai mẫu hay trùng,
// base64 hỏng, khóa không đúng 32 byte → lỗi bọc ErrInvalid nêu vị trí mục (đếm từ 1).
func Parse(spec string) ([]Key, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, fmt.Errorf("%w: rỗng", ErrInvalid)
	}
	parts := strings.Split(spec, ",")
	out := make([]Key, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for i, p := range parts {
		id, b64, ok := strings.Cut(strings.TrimSpace(p), ":")
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: mục %d thiếu dạng <key_id>:<base64>", ErrInvalid, i+1)
		case !keyID.MatchString(id):
			return nil, fmt.Errorf("%w: mục %d có key_id không khớp [a-z0-9]{1,16}", ErrInvalid, i+1)
		case seen[id]:
			return nil, fmt.Errorf("%w: mục %d trùng key_id với mục trước", ErrInvalid, i+1)
		}
		secret, err := base64.StdEncoding.Strict().DecodeString(b64)
		if err != nil { // không bọc err: thông điệp của base64 có thể trích vị trí, ký tự của khóa
			return nil, fmt.Errorf("%w: mục %d không phải base64 chuẩn", ErrInvalid, i+1)
		}
		if len(secret) != KeySize {
			return nil, fmt.Errorf("%w: mục %d dài %d byte, phải đúng %d", ErrInvalid, i+1, len(secret), KeySize)
		}
		seen[id] = true
		out = append(out, Key{ID: id, Secret: secret})
	}
	return out, nil
}
