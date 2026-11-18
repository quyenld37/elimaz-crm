package customer

import (
	"log/slog"
	"time"
)

// Giá trị thay cho dữ liệu cá nhân trong log, cùng chữ với logging.Phone.
const redacted = "[Redacted]"

// Name là họ tên khách (dữ liệu cá nhân); ghi log luôn ra "[Redacted]". SĐT dùng logging.Phone, địa chỉ
// logging.Address.
type Name string

// LogValue che họ tên.
func (Name) LogValue() slog.Value { return slog.StringValue(redacted) }

// Birthday là ngày sinh khách (dữ liệu cá nhân); ghi log luôn ra "[Redacted]".
type Birthday time.Time

// LogValue che ngày sinh.
func (Birthday) LogValue() slog.Value { return slog.StringValue(redacted) }
