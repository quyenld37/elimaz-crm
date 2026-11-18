// Package queries ghép view chỉ đọc của nhiều module cho màn danh sách (sản phẩm, tồn; kế hoạch M1, Task M1-39).
//
// Gói không ghi gì, không nhập module nào (tests/arch/boundaries_test luật f) và chỉ nhắc view <schema>.<tên>_v có
// trong docs/views.md (tests/arch/sqlusage_test). Câu dựng lúc chạy nằm trong *_dynsql.go, có *_dynsql_test.go chạy
// trên PostgreSQL thật; mọi truy vấn chạy trong tenancy.WithTenant bằng crm_app.
package queries
