package customer

import "elimaz.com.vn/crm/internal/platform/textnorm"

// NormalizeName trả tên dạng tìm kiếm cho cột name_search: đúng textnorm.Fold (bỏ dấu, chữ thường, gộp khoảng trắng;
// C22: không bảng dấu thứ hai trong Go).
func NormalizeName(s string) string { return textnorm.Fold(s) }
