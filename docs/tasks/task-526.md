# Task 526 — Test EXPLAIN tổng quan chập chờn, demo thiếu địa giới

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi: không có (`(*Seed).analyzeFully` và `seedDevAdminUnits` là hàm trong gói, chưa xuất; `Seed.Bulk` giữ chữ ký).
- Gói phụ thuộc ngược: `internal/testkit/reportseed` được các test của `reporting` (và test gói khác gọi `Bulk`) nhập; `cmd/crm` (seed-dev).
- OpenAPI: không đổi (script demo chỉ gửi thêm `provinceCode`, `wardCode` đã bắt buộc ở `ShipAddress`).
- Màn hình: không.
- sqlc, migration: không. không câu SQL mới: `seed-dev` gọi `customer.EnsureFixtureAdminUnits` (câu sqlc có sẵn) trong `owner.RetryTx`.
- Test: `TestDashboard_ExplainNoSeqScan` (-count=14 cục bộ), `TestSeedDevAdminUnits`; ci-task chạy gói đổi và gói phụ thuộc (gồm `TestOrderSet_ExplainNoSeqScan`, `TestBestSellers_ExplainNoSeqScan`, dùng chung `Bulk`).

## Tái dùng

- Đã kiểm `docs/danh-muc-dung-chung.md`, `internal/testkit` (explain.go, adminunits.go): `testkit.SeedAdminUnits` chèn mã T01 cho test, không dùng được cho dev (cần mã 79/26734 của hãng giả).
- Dùng lại `Bulk` và `ANALYZE` sẵn có (chỉ đổi cách ANALYZE); dùng lại `customer.EnsureFixtureAdminUnits` (lần đầu viết SQL riêng bị `TestDynSQLProductCode`, `TestSQLUsageProductCode`, `TestTxUsageProductCode` chặn đúng); `seedDevAdminUnits` chỉ là lời gọi mỏng.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

5.x/6.x chỉ phần liên quan: không áp dụng: không đổi hợp đồng, quyền, hàng đợi, khóa, tiền. Database: `EnsureFixtureAdminUnits` không đè mã đã có khác nội dung (trả conflicts, seed-dev bỏ qua); bảng không bị khóa (ADR-46). Test: ANALYZE đặt `default_statistics_target` trên đúng một kết nối lấy từ pool và RESET khi trả (không rò sang kết nối khác).

**Nguyên nhân chập chờn (đo):** ANALYZE mặc định lấy mẫu ngẫu nhiên 30.000 dòng, nên ước lượng số sự kiện "giao thành công" trong 31 ngày đổi theo lần chạy; khi ước cao, nhánh giao thành công của `orderSet` ra Hash Join + Seq Scan trên `ord.orders` (100.000 dòng; đo 2/12 lần đỏ, bảng bị quét là `orders`). Đó là Seq Scan thật trên bảng lớn nên không nới điều kiện; sửa là làm thống kê tất định (mẫu 600.000 ≥ cỡ bảng, tức thống kê của cả bảng).

## Quyết định thay chủ shop

- QĐ-526-a: sửa ở `reportseed.Bulk` (ANALYZE đủ mẫu), không đổi test hay câu SQL: giữ nguyên điều khẳng định "không Seq Scan", đồng thời ổn định luôn các test EXPLAIN cùng dùng Bulk. Khác: chỉ cấm Seq Scan trên `orders` (vẫn phải ổn định thống kê nên bỏ), hoặc `enable_seqscan=off` (che lỗi thật).
- QĐ-526-b: địa giới demo nạp trong `seed-dev` (chỉ dev), mã 79 / 26734 trùng hãng giả; script demo gửi mã đó và tự đồng bộ địa giới với hãng giả trước bước vận đơn. Khác: migration dữ liệu (sẽ lẫn dev vào production).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test EXPLAIN tổng quan không còn chập chờn | `TestDashboard_ExplainNoSeqScan` | trước sửa: `go test -v -count=12 -run '^TestDashboard_ExplainNoSeqScan$' ./internal/modules/reporting/`; sau sửa: `-count=14` cùng lệnh | trước: 2/12 đỏ, Seq Scan trên `orders`; sau: 14/14 `ok` (máy cục bộ, PostgreSQL chung 5600); ci-task từ xa: các shard test xanh (xem bên dưới) |
| Test EXPLAIN còn khẳng định đúng | cùng test, không đổi mã test | `git diff -- internal/modules/reporting` | rỗng: vẫn `RequireNoSeqScan` trên hai câu, phạm vi all |
| seed-dev nạp địa giới 79 / 26734, chạy lại không nhân đôi, không đè dữ liệu thật | `TestSeedDevAdminUnits`, `TestSeedDevAdminUnits_KeepsExisting` | `go test -race=false -count=1 -run '^TestSeedDevAdminUnits' ./cmd/crm/` | `ok` |
| Luật kiến trúc SQL/transaction | `TestDynSQLProductCode`, `TestSQLUsageProductCode`, `TestTxUsageProductCode` | `go test -count=1 -run 'ProductCode' ./tests/arch/` | `ok` (hai lượt ci-task đầu đỏ ở đây vì SQL riêng trong cmd/crm; đã đổi sang hàm của customer) |
| Script demo gửi mã địa giới và đồng bộ với hãng giả | (chưa chạy được: cần docker compose + image) | `bash -n scripts/demo/mot-don-di-tron.sh`; thử câu jq lọc `unmapped` với dữ liệu mẫu | cú pháp đúng; hàm `unmapped_ours` thử với `api` giả hai trang (mã nằm ở trang 2 → trả 1, đi theo `nextCursor`). Đường dẫn đối chiếu `openapi.yaml` (`carrier` bắt buộc, `limit` ≤ 100) và `carrier_locations_test.go`. Chạy `make demo-mot-don` thật: chờ phiên có docker |

## Việc phát hiện thêm

- `customer.EnsureFixtureAdminUnits` không chèn mã nào khi một mã xung đột: nếu dev có sẵn tỉnh 79 tên khác mà thiếu phường 26734, seed-dev không nạp phường (giữ danh mục thật; chưa gặp vì chưa có migration dữ liệu địa giới).
- Khi migration dữ liệu địa giới thật (tools/adminunits) ra đời, nó dùng INSERT thường: dev đã seed 79/26734 sẽ trùng khóa; cần `ON CONFLICT (code) DO NOTHING` hoặc xóa hai dòng seed-dev trước.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 2). Lượt 1 CẦN SỬA: script demo gọi `GET /api/v1/carrier-locations/unmapped` thiếu `carrier` (bắt buộc, sẽ 400) và không đi hết các trang; đã sửa bằng hàm `unmapped_ours` (`?carrier=fake&limit=100`, theo `nextCursor`). Chưa chạy `make demo-mot-don` thật (cần docker): chờ phiên có docker.
