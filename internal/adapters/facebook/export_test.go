package facebook

import (
	"errors"
	"io"
	"net/netip"
)

// UseTestResolver thay DNS của bộ tải tệp bằng bảng tên máy → địa chỉ, và cho phép nối tới đúng 127.0.0.1 (máy Meta
// giả). Chỉ có trong test: mã thật không có đường nào cho phép địa chỉ loopback.
func (c *Client) UseTestResolver(m map[string]string) {
	c.dl.resolve = func(host string) (string, bool) {
		ip, ok := m[host]
		return ip, ok
	}
	c.dl.allow = netip.MustParseAddr("127.0.0.1")
}

// IsBlockedIP báo địa chỉ có bị bộ tải tệp chặn không.
func IsBlockedIP(s string) bool { return blockedAddr(netip.MustParseAddr(s)) }

// NewGraphErrorForTest dựng GraphError như khi giải lỗi JSON của Meta (message đã lọc).
func NewGraphErrorForTest(status, code, subcode int, typ, trace, msg string) *GraphError {
	return newGraphError(status, code, subcode, typ, trace, msg)
}

// NewTransportErrorForTest dựng lỗi mạng như Client trả: sent false là chưa ghi trọn yêu cầu, true là có thể đã gửi.
func NewTransportErrorForTest(sent bool) *GraphError {
	return transportError(errors.New("connection reset"), sent, "")
}

// ReadSSE là bộ đọc text/event-stream của luồng comment live.
func ReadSSE(r io.Reader, emit func(data []byte) bool) error { return readSSE(r, emit) }
