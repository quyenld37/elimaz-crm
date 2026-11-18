// Package article là bài viết dạng khối của mẫu sản phẩm (kế hoạch M1, Q34, C8): kiểu khối, giới hạn và bộ kiểm lược
// đồ. Không lưu, không nhận HTML; chuỗi giữ nguyên văn (thoát ký tự là việc của bộ dựng HTML catalog.RenderArticleHTML).
package article

import (
	"errors"

	"github.com/google/uuid"
)

// ErrInvalid báo bài viết sai lược đồ hoặc vượt giới hạn. Lỗi không mang nội dung bài viết.
var ErrInvalid = errors.New("catalog: bài viết không hợp lệ")

// Loại khối.
const (
	TypeParagraph = "paragraph"
	TypeHeading   = "heading"
	TypeList      = "list"
	TypeImage     = "image"
)

// Giới hạn (kế hoạch M1-27).
const (
	MaxBlocks   = 200
	MaxText     = 5000 // ký tự (rune) của text, caption, mỗi mục list
	MaxItems    = 50
	MaxRawBytes = 200 * 1024
)

// Block là một khối. Trường nào dùng cho loại nào: paragraph {text}; heading {text, level 2|3}; list {items};
// image {fileId, caption?}. Image không lưu, không nhận từ JSON: chỉ điền lúc dựng HTML (Task M1-44).
type Block struct {
	Type    string     `json:"type"`
	Text    string     `json:"text,omitempty"`
	Level   int        `json:"level,omitempty"`
	Items   []string   `json:"items,omitempty"`
	FileID  *uuid.UUID `json:"fileId,omitempty"`
	Caption string     `json:"caption,omitempty"`
	Image   *Image     `json:"-"`
}

// Image là các biến thể đã xử lý của ảnh trong khối image.
type Image struct {
	Variants []ImageVariant
}

// ImageVariant là một biến thể ảnh: chiều rộng (px), định dạng (avif, webp, jpeg), đường dẫn công khai.
type ImageVariant struct {
	Width  int
	Format string
	URL    string
}
