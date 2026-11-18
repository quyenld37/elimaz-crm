package article

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// maxInputBytes chặn đầu vào quá lớn trước khi giải mã. Trần thật (MaxRawBytes) do Check kiểm trên dạng JSON gọn,
// vì cột jsonb đọc ra có thêm dấu cách sau mỗi ':' và ',' (dài hơn bản đã lưu, không quá gấp đôi): kiểm MaxRawBytes
// trên byte thô sẽ khóa mẫu có bài viết sát trần sau khi lưu (B-02, review độc lập M1).
const maxInputBytes = 2 * MaxRawBytes

// Validate đọc raw (mảng JSON các khối) và kiểm lược đồ: rỗng → không khối; tối đa MaxRawBytes byte ở dạng JSON gọn
// (Check), MaxBlocks khối;
// loại lạ, trường lạ, trường của loại khác, khóa trùng, giá trị null, chuỗi có ký tự điều khiển (trừ xuống dòng trong
// text của paragraph), UTF-8 hỏng, vượt giới hạn → ErrInvalid. fileId chỉ kiểm dạng uuid; tệp được kiểm khi lưu mẫu (catalog, Task M1-44b).
func Validate(raw json.RawMessage) ([]Block, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return []Block{}, nil
	}
	if len(trimmed) > maxInputBytes || !utf8.Valid(trimmed) {
		return nil, ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return nil, ErrInvalid
	}
	blocks := []Block{}
	for dec.More() {
		if len(blocks) == MaxBlocks {
			return nil, ErrInvalid
		}
		b, err := decodeBlock(dec)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, b)
	}
	if _, err := dec.Token(); err != nil { // ']'
		return nil, ErrInvalid
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) { // không có gì sau mảng
		return nil, ErrInvalid
	}
	if err := Check(blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

// decodeBlock đọc một đối tượng khối theo từng khóa (để bắt khóa lạ, khóa trùng, null).
func decodeBlock(dec *json.Decoder) (Block, error) {
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return Block{}, ErrInvalid
	}
	var b Block
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return Block{}, ErrInvalid
		}
		key, _ := tok.(string)
		if seen[key] {
			return Block{}, ErrInvalid
		}
		seen[key] = true
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil || string(val) == "null" {
			return Block{}, ErrInvalid
		}
		if err := setField(&b, key, val); err != nil {
			return Block{}, err
		}
	}
	if _, err := dec.Token(); err != nil { // '}'
		return Block{}, ErrInvalid
	}
	if !seen["type"] || !allowedKeys(b.Type, seen) {
		return Block{}, ErrInvalid
	}
	return b, nil
}

func setField(b *Block, key string, val json.RawMessage) error {
	var err error
	switch key {
	case "type":
		err = json.Unmarshal(val, &b.Type)
	case "text":
		err = json.Unmarshal(val, &b.Text)
	case "level":
		err = json.Unmarshal(val, &b.Level)
	case "items":
		err = json.Unmarshal(val, &b.Items)
		if err == nil && b.Items == nil {
			b.Items = []string{}
		}
	case "caption":
		err = json.Unmarshal(val, &b.Caption)
	case "fileId":
		var s string
		if err = json.Unmarshal(val, &s); err == nil {
			id, perr := uuid.Parse(s)
			if perr != nil || id.String() != s {
				return ErrInvalid
			}
			b.FileID = &id
		}
	default:
		return ErrInvalid
	}
	if err != nil {
		return fmt.Errorf("%w: trường %s sai kiểu", ErrInvalid, key)
	}
	return nil
}

// allowedKeys: khóa có mặt phải thuộc đúng loại khối.
func allowedKeys(typ string, seen map[string]bool) bool {
	var allow []string
	switch typ {
	case TypeParagraph:
		allow = []string{"type", "text"}
	case TypeHeading:
		allow = []string{"type", "text", "level"}
	case TypeList:
		allow = []string{"type", "items"}
	case TypeImage:
		allow = []string{"type", "fileId", "caption"}
	default:
		return false
	}
	for k := range seen {
		if !slices.Contains(allow, k) {
			return false
		}
	}
	return true
}

// Check kiểm các khối đã có trong bộ nhớ theo đúng luật của Validate (trừ khóa lạ, khóa trùng chỉ thấy ở JSON): số
// khối, loại, trường bắt buộc và trường của loại khác, cấp tiêu đề, độ dài, ký tự điều khiển, tổng cỡ JSON. Bộ dựng
// HTML gọi lại để phòng dữ liệu cũ.
func Check(blocks []Block) error {
	if len(blocks) > MaxBlocks {
		return ErrInvalid
	}
	for _, b := range blocks {
		if !checkBlock(b) {
			return ErrInvalid
		}
	}
	enc, err := json.Marshal(blocks)
	if err != nil || len(enc) > MaxRawBytes {
		return ErrInvalid
	}
	return nil
}

func checkBlock(b Block) bool {
	switch b.Type {
	case TypeParagraph:
		return validText(b.Text, true, true) && b.Level == 0 && b.Items == nil && b.FileID == nil && b.Caption == ""
	case TypeHeading:
		return validText(b.Text, true, false) && (b.Level == 2 || b.Level == 3) && b.Items == nil && b.FileID == nil &&
			b.Caption == ""
	case TypeList:
		if len(b.Items) == 0 || len(b.Items) > MaxItems || b.Text != "" || b.Level != 0 || b.FileID != nil || b.Caption != "" {
			return false
		}
		for _, it := range b.Items {
			if !validText(it, true, false) {
				return false
			}
		}
		return true
	case TypeImage:
		return b.FileID != nil && validText(b.Caption, false, false) && b.Text == "" && b.Level == 0 && b.Items == nil
	}
	return false
}

// validText: UTF-8 đúng, tối đa MaxText ký tự, không ký tự điều khiển (C0, DEL, C1) trừ '\n' khi newline; required
// thì phải có chữ khác khoảng trắng.
func validText(s string, required, newline bool) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxText {
		return false
	}
	if required && strings.TrimSpace(s) == "" {
		return false
	}
	for _, r := range s {
		if r == '\n' && newline {
			continue
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return false
		}
	}
	return true
}
