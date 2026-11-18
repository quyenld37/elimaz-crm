package imager

import (
	"net/http"
	"slices"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

// sniffLen: số byte đầu dùng để nhận dạng loại (ADR-45: 3.072 byte, đủ cho mimetype; DetectContentType đọc 512).
const sniffLen = 3072

// loaderFor: bộ đọc libvips phải khớp với loại đã nhận (vipsheader -f vips-loader). Danh sách trắng của ADR-45:
// JPEG, PNG, WebP; loại khác (GIF, HEIC, AVIF, SVG…) bị từ chối dù bên gọi cho phép.
func loaderFor(detected string) (string, bool) {
	switch detected {
	case "image/jpeg":
		return "jpegload", true
	case "image/png":
		return "pngload", true
	case "image/webp":
		return "webpload", true
	}
	return "", false
}

// detect trả loại MIME của nội dung (không tham số), chỉ dựa vào byte đầu, không tin tên tệp hay loại khai.
// Thư viện chuẩn trước; nó không nhận ra (octet-stream) thì hỏi mimetype (HEIC, AVIF, TIFF…).
func detect(head []byte) string {
	t := http.DetectContentType(head)
	if t == "application/octet-stream" {
		t = mimetype.Detect(head).String()
	}
	t, _, _ = strings.Cut(t, ";")
	return strings.TrimSpace(t)
}

// classify: "" nếu xử lý được; ngược lại lý do từ chối.
func classify(detected string, allowed []string) string {
	if !strings.HasPrefix(detected, "image/") || detected == "image/svg+xml" {
		return ReasonNotImage
	}
	if _, ok := loaderFor(detected); !ok {
		return ReasonTypeNotAllowed
	}
	if slices.Contains(allowed, detected) {
		return ""
	}
	return ReasonTypeNotAllowed
}
