package fieldcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"log/slog"

	"elimaz.com.vn/crm/internal/platform/fieldcrypt/keyspec"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// redacted là cách Keyring tự in ra (slog, fmt).
const redacted = "[Redacted]"

// Keyring giữ các khóa của FIELD_ENC_KEYS đã dựng sẵn AES-256-GCM. Không trường nào xuất ra ngoài; slog và fmt chỉ
// thấy "[Redacted]". Dùng chung được giữa các goroutine (chỉ đọc sau khi dựng).
type Keyring struct {
	keys    map[string]cipher.AEAD
	current string
}

// ParseKeyring dựng Keyring từ giá trị của FIELD_ENC_KEYS (`<key_id>:<base64 32 byte>`, cách nhau dấu phẩy; khóa đầu
// mã hóa). Lỗi bọc keyspec.ErrInvalid, chỉ nêu vị trí mục, không chứa khóa.
func ParseKeyring(spec logging.Secret) (*Keyring, error) {
	keys, err := keyspec.Parse(string(spec))
	if err != nil {
		return nil, fmt.Errorf("fieldcrypt: FIELD_ENC_KEYS: %w", err)
	}
	k := &Keyring{keys: make(map[string]cipher.AEAD, len(keys)), current: keys[0].ID}
	for _, key := range keys {
		block, err := aes.NewCipher(key.Secret)
		if err != nil { // không xảy ra: keyspec đã kiểm đúng 32 byte
			return nil, fmt.Errorf("fieldcrypt: dựng AES cho khóa %q: %w", key.ID, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("fieldcrypt: dựng GCM cho khóa %q: %w", key.ID, err)
		}
		k.keys[key.ID] = aead
	}
	return k, nil
}

// CurrentKeyID là key_id của khóa đầu (khóa Seal dùng).
func (k *Keyring) CurrentKeyID() string { return k.current }

// LogValue che toàn bộ Keyring khi ghi log.
func (Keyring) LogValue() slog.Value { return slog.StringValue(redacted) }

// String không chứa khóa (fmt %v, %+v, %s).
func (Keyring) String() string { return redacted }

// GoString không chứa khóa (fmt %#v).
func (Keyring) GoString() string { return redacted }
