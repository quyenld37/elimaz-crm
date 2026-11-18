package logging

import (
	"encoding"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
	"unicode"
)

// Giá trị thay cho dữ liệu bị che.
const (
	redacted  = "[Redacted]"
	truncated = "[Truncated]"
)

// DefaultDepth là độ sâu mặc định khi che một giá trị lồng nhau.
const DefaultDepth = 8

// sensitiveKey là mẫu khóa phải che ở mọi cấp (Global Constraints của kế hoạch M0; full_name, recipient, birthday,
// email, note thêm ở kế hoạch M2 Task M2-03, Q21).
var sensitiveKey = regexp.MustCompile(`(?i)phone|address|token|secret|password|authorization|cookie|body|text|id_number|bank_account|full_name|recipient|birthday|email|note`)

// SensitiveKey báo khóa key có phải bị che giá trị không. Khóa được so cả nguyên dạng lẫn dạng snake_case, để tên
// trường Go như BankAccount, IDNumber cũng khớp bank_account, id_number.
func SensitiveKey(key string) bool {
	return sensitiveKey.MatchString(key) || sensitiveKey.MatchString(snake(key))
}

// snake đổi CamelCase thành snake_case: chèn "_" trước chữ hoa đứng sau chữ thường hoặc chữ số, hay trước chữ hoa
// đầu của một từ sau cụm viết tắt (IDNumber → id_number).
func snake(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i, c := range r {
		if unicode.IsUpper(c) && i > 0 {
			prev := r[i-1]
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}

// Phone là số điện thoại; ghi log luôn ra "[Redacted]".
type Phone string

// LogValue che số điện thoại.
func (Phone) LogValue() slog.Value { return slog.StringValue(redacted) }

// Address là địa chỉ; ghi log luôn ra "[Redacted]".
type Address string

// LogValue che địa chỉ.
func (Address) LogValue() slog.Value { return slog.StringValue(redacted) }

// Secret là bí mật (token, mật khẩu, khóa); ghi log luôn ra "[Redacted]".
type Secret string

// LogValue che bí mật.
func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// Redact trả bản sao của v đã che: đi vào map, slice, mảng, struct, con trỏ, slog.LogValuer và json.Marshaler;
// giá trị error thành nội dung đã qua ErrorText; giá trị của mọi khóa (khóa map, tên trường, thẻ json) khớp SensitiveKey thành "[Redacted]". Phần sâu hơn maxDepth
// thành "[Truncated]", không bao giờ in thô. maxDepth <= 0 dùng DefaultDepth.
func Redact(v any, maxDepth int) any {
	if maxDepth <= 0 {
		maxDepth = DefaultDepth
	}
	return redactValue(v, maxDepth)
}

func redactValue(v any, depth int) any {
	if v == nil {
		return nil
	}
	if lv, ok := v.(slog.LogValuer); ok {
		return redactSlogValue(slog.AnyValue(lv).Resolve(), depth)
	}
	switch x := v.(type) {
	case error:
		return ErrorText(x)
	case json.Marshaler:
		return redactMarshaler(x, depth)
	case encoding.TextMarshaler:
		b, err := x.MarshalText()
		if err != nil {
			return truncated
		}
		return string(b)
	case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, []byte:
		return x
	}
	return redactReflect(reflect.ValueOf(v), depth)
}

func redactMarshaler(m json.Marshaler, depth int) any {
	b, err := m.MarshalJSON()
	if err != nil {
		return truncated
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return truncated
	}
	return redactReflect(reflect.ValueOf(decoded), depth)
}

func redactReflect(rv reflect.Value, depth int) any {
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return redactValue(rv.Elem().Interface(), depth)
	case reflect.Map:
		if depth == 0 {
			return truncated
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			k := fmt.Sprint(iter.Key().Interface())
			if SensitiveKey(k) {
				out[k] = redacted
				continue
			}
			out[k] = redactValue(iter.Value().Interface(), depth-1)
		}
		return out
	case reflect.Slice, reflect.Array:
		if depth == 0 {
			return truncated
		}
		out := make([]any, rv.Len())
		for i := range rv.Len() {
			out[i] = redactValue(rv.Index(i).Interface(), depth-1)
		}
		return out
	case reflect.Struct:
		if depth == 0 {
			return truncated
		}
		return redactStruct(rv, depth)
	case reflect.String:
		return rv.String()
	case reflect.Bool:
		return rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint()
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	default:
		// Hàm, kênh, số phức, unsafe.Pointer: không có gì đáng ghi.
		return truncated
	}
}

func redactStruct(rv reflect.Value, depth int) map[string]any {
	t := rv.Type()
	out := make(map[string]any, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if tag == "-" {
			continue
		}
		if tag != "" {
			name = tag
		}
		if SensitiveKey(f.Name) || SensitiveKey(name) {
			out[name] = redacted
			continue
		}
		out[name] = redactValue(rv.Field(i).Interface(), depth-1)
	}
	return out
}

// redactSlogValue che một slog.Value đã Resolve.
func redactSlogValue(v slog.Value, depth int) any {
	switch v.Kind() {
	case slog.KindGroup:
		out := map[string]any{}
		for _, a := range v.Group() {
			if SensitiveKey(a.Key) {
				out[a.Key] = redacted
				continue
			}
			out[a.Key] = redactSlogValue(a.Value.Resolve(), depth-1)
		}
		return out
	case slog.KindAny:
		return redactValue(v.Any(), depth)
	default:
		return v.Any()
	}
}
