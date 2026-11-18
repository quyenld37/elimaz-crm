# ADR-29 — Sẵn chỗ cho sàn nhiều người bán

Trạng thái: đã duyệt (chủ shop chốt 12/11/2026)
Ngày: 12/11/2026
Kiểm bằng: `TestOrderItems_SellerFK` (internal/modules/order/create_test.go); `TestCheckSchemaRulesViewsAndSeller` (internal/platform/tenancy/schemarules_test.go); hai người bán trọn bộ test đơn, kho, vận chuyển: chưa có, làm ở mốc M2 (`testkit.SeedCatalog`) và M3 (Task 139)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-29. Tệp này không chép lại quyết định.

Bảng `sellers` và cột `seller_id` ở sản phẩm, biến thể, đơn, tài khoản hãng, tài khoản kênh. Một đơn thuộc đúng một
người bán, bảo đảm bằng khóa ghép. Mã hàng và barcode duy nhất theo người bán. Không xây tính năng sàn nào bây giờ.

## Kiểm ở đâu

- Dòng đơn trỏ tới biến thể của người bán khác bị database từ chối: `TestOrderItems_SellerFK` (internal/modules/order/create_test.go).
- Unique của bảng có `seller_id` bắt đầu bằng `(tenant_id, seller_id)`: `TestCheckSchemaRulesViewsAndSeller` (schemarules_test.go).
- Mã hàng theo người bán: `TestCreateProduct_DuplicateSKU` (internal/modules/catalog/products_test.go).
- Đọc danh sách có điều kiện người bán: `TestProducts` (internal/queries/products_dynsql_test.go).
- Chạy trọn bộ test đơn, kho, vận chuyển với hai người bán: chưa có, làm ở mốc M2 và M3.
