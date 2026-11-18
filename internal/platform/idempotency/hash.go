package idempotency

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
)

// RequestHash là sha256 của method, route và thân đã chuẩn hóa JSON (ADR-41: so nội dung bằng mã băm). route là
// đường dẫn thật của yêu cầu (kèm query nếu có), không phải mẫu, để cùng khóa cho hai đích khác nhau là "khóa đã
// dùng". Chuẩn hóa: bỏ khoảng trắng, xếp khóa của object; số giữ nguyên chữ (1 và 1.0 khác nhau). Thân rỗng và thân
// không phải JSON hợp lệ được băm nguyên byte. Mỗi phần mang độ dài phía trước nên không ghép nhầm hai phần.
func RequestHash(method, route string, body []byte) []byte {
	h := sha256.New()
	for _, part := range [][]byte{[]byte(method), []byte(route), canonicalJSON(body)} {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(part)))
		h.Write(n[:])
		h.Write(part)
	}
	return h.Sum(nil)
}

// canonicalJSON trả bản mã hóa lại của body (encoding/json xếp khóa map khi mã hóa); body không phải đúng một giá trị
// JSON thì trả nguyên.
func canonicalJSON(body []byte) []byte {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return body
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return body // còn dữ liệu sau giá trị đầu
	}
	out, err := json.Marshal(v)
	if err != nil {
		return body
	}
	return out
}
