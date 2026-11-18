# ADR-08 — Tìm kiếm bằng PostgreSQL

Trạng thái: đã duyệt (spec 07-adr, 11/11/2026); cách viết truy vấn dưới RLS bổ sung bởi ADR-14
Ngày: 11/11/2026
Kiểm bằng: `TestSearchPlans`, `TestSearchFunctionSeesOnlyOwnTenant` (internal/platform/tenancy/search_test.go); `TestPlansHaveNoSeqScan` (internal/queries/explain_test.go); `scripts/backup/search-plan-probe.sh`

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-08. Tệp này không chép lại quyết định.

Index btree cho khớp chính xác và tiền tố/hậu tố, `pg_trgm` + `unaccent` cho tên gần đúng. Không dùng
Elasticsearch/Meilisearch.

## Kiểm ở đâu

- Kế hoạch chạy của câu tìm kiếm bằng role ứng dụng không quét toàn bảng: `TestSearchPlans`, `TestRequireNoSeqScanFails`
  (internal/platform/tenancy/search_test.go); `TestPlansHaveNoSeqScan` (internal/queries/explain_test.go);
  phép thử ngắn `scripts/backup/search-plan-probe.sh` (kết quả `docs/do-luong/phep-thu-ngan.md`).
- Tìm sản phẩm theo tên không dấu: `TestProducts` (internal/queries/products_dynsql_test.go).
- Tìm khách theo đuôi SĐT dưới 300 ms trên 1 triệu khách: chưa có, làm ở mốc M2.
