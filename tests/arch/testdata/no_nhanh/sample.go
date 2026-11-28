// Package sample là mẫu cố ý vi phạm cho TestNoNhanh_FindsSample (Task 265): CRM không gọi, không đọc Nhanh (ADR-11).
package sample

// Base là địa chỉ API bị cấm.
const Base = "https://open.nhanh.vn/api/order/index"

// TokenEnv là tên biến môi trường bị cấm.
const TokenEnv = "NHANH_ACCESS_TOKEN"
