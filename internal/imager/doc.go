// Package imager là tiến trình giải mã ảnh của CRM (ADR-45): tải ảnh bằng đường dẫn ký sẵn, kiểm loại bằng nội
// dung, đọc kích thước trước khi giải mã, tạo biến thể bằng chương trình dòng lệnh của libvips trong tiến trình con
// có hạn giờ và giới hạn bộ nhớ, rồi ghi biến thể bằng đường dẫn ký sẵn. Không có khóa bí mật, không nối database.
package imager
