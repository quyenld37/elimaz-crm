# Task 112 — M3-21: Mức khách hay hoàn khi lên đơn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 112, M3-21). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ THÊM ở `customer`: `ReturnRisk`, `DeliveryStats`, `DeliveryStatsFor`, hằng `RiskNone/RiskWatch/RiskHigh`, `HintWatch/HintHigh` — chưa có chỗ tham chiếu (0). Không đổi ký hiệu cũ. Sửa lỗi trong `lockGroupsOnce` (không xuất; 1 chỗ gọi: `lockGroups`, dùng bởi `RecomputeStats`, `StatsConsumer.Handle`). `StatsFor` dùng lại, không đổi chữ ký.
- Gói phụ thuộc ngược (go list): `customer` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/modules/order`, `internal/modules/promotion`, `internal/testkit/shiptest` — chỉ thêm ký hiệu nên không gãy; chạy test `customer`, `httpapi -run` các test khách/M3, `tests/arch`.
- Operation OpenAPI bị chạm, oasdiff: `getCustomerDeliveryStats` (đã có trong hợp đồng từ Task M3-04, không đổi `openapi.yaml`, oasdiff không chênh); bỏ dòng `getCustomerDeliveryStats` khỏi `tests/arch/testdata/m3_pending.txt` (operation hết trả 501).
- Tệp đổi: mới `internal/modules/customer/delivery_stats.go` (+ `delivery_stats_test.go`, `delivery_stats_rules_test.go`, `stats_merge_race_test.go`), `internal/httpapi/handlers_customer_stats_test.go`, `tests/arch/customer_writer_test.go`; sửa `internal/modules/customer/stats.go` (3 dòng), `internal/httpapi/handlers_customer_stats.go` (thân handler), `tests/arch/testdata/m3_pending.txt`. Không chạm `cmd/crm`, `server.go`, `module.go`.
- Chỗ dùng trên màn hình: chưa có (CustomerPicker, khối gửi hãng dùng ở task màn hình sau); không đổi `console/`.
- Câu sqlc, bảng, pha migration: không câu sqlc mới, không migration (kế hoạch: "không có migration"); đọc qua câu có sẵn `GetStats`/`GetRootOf` (bảng `customer.customers`). Cài đặt đọc qua `fulfillment.LoadSettings` (platform.settings) ở handler.
- Test sẽ chạy: `go test -race ./internal/modules/customer/...` (gồm test M2 Task 25 và `TestStatsConcurrentWithMerge` lặp ≥100 lần), `go test -race ./internal/httpapi/ -run 'CustomerDeliveryStats|M3Routes'`, `go test ./tests/arch/ -run 'M3|CustomerWriter'`; rồi ci-task một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có gì về ngưỡng rủi ro); `customer.StatsFor` (stats.go) đã đọc chỉ số gốc hiện tại kèm `ReturnRateBP` theo công thức M2 (`statsOf`); consumer `customer_stats` đã nghe `order.delivery_reversed`; `fulfillment.ReturnRisk` + `LoadSettings` (Task M3-04) giữ ngưỡng.
- Dùng lại / viết mới vì: dùng lại `StatsFor` (không viết câu SQL mới — `queries/customer/delivery.sql` của kế hoạch là thừa, xem Khác kế hoạch), dùng lại `RecomputeStats`/consumer M2 (không tính lại, không consumer thứ hai). Viết mới `customer.ReturnRisk` (bản của customer, vì `customer` không được nhập `fulfillment`; handler chép 3 số) và hàm phân mức `DeliveryStatsFor`.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 chủ dữ liệu: số nhận/hoàn là của `customer` (M2); ngưỡng là của `fulfillment` (platform.settings khóa fulfillment). Không dữ liệu mới.
- 5.2 không ghi bảng module khác: đường mới chỉ đọc; test kiến trúc mới đòi chỉ `customer` (Go + `queries/customer`) có câu ghi `customer.customers`, `delivery_stats.go` không có UPDATE/INSERT/DELETE.
- 5.3 đọc qua public API: handler gọi `customer.DeliveryStatsFor` và `fulfillment.LoadSettings`, không đọc bảng.
- 5.4, 5.5, 5.6: không áp dụng: chỉ đọc, không việc sau commit, không gọi ra ngoài.
- 5.7 tenant: handler dùng `catalogRead` (tenancy.WithTenant); khách tenant khác → 404 (có test).
- 5.8 không HTTP trong module: `customer` không nhập fulfillment/httpapi; handler mỏng chép ngưỡng.
- 5.9 đường ghi: không áp dụng (GET); quyền `customer.read` đã khai (x-permission), có test 403.
- 5.10, 5.11: không áp dụng: không Valkey, không thư viện mới.
- 5.12 theo module mẫu: hàm đọc nhận ctx, tx; test PostgreSQL thật, đơn tạo qua `order`.
- 6.1 đồng thời: đường đọc không khóa (như `StatsFor` của M2). Sửa lỗi có sẵn ở `lockGroupsOnce`: gốc vừa bị gộp giữa `RootOf` và `GroupMembers` → nhóm rỗng → `locked[0]` panic (gốc của test chập chờn `TestStatsConcurrentWithMerge`); sửa: nhóm rỗng → `errGroupRetry` (lùi savepoint, đọc lại gốc). Test tái hiện tất định bằng tx bọc chen lần gộp giữa hai câu, và `TestStatsConcurrentWithMerge` lặp ≥100 lần.
- 6.2, 6.3: không áp dụng (đọc; thứ tự khóa M2 không đổi, chỉ thêm nhánh thử lại trước khi khóa).
- 6.4–6.8: không áp dụng: đọc, không việc nền mới (consumer M2 có sẵn: panic → thử lại trong savepoint).
- 6.9: mặc định của pool. 6.10: không tiền. 6.11: không vùng rủi ro cao; ngưỡng là hàm thuần — test bảng biên (=, −1).
- 6.12 dữ liệu cá nhân: kết quả chỉ có số và câu gợi ý cố định; không log mã/SĐT khách.
- 6.13 quyền: `customer.read` (route sinh từ openapi), test 403 khi thiếu quyền.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| K1 3 giao, 2 hoàn → {3, 2, 4000, high}, hint "Khách hay hoàn…" | `customer.TestDeliveryStatsFor` (đơn qua `order`, số do consumer M2 tính) | `go test -race -count=1 -run 'TestDeliveryStatsFor\|TestClassify\|TestStatsRootMerged' ./internal/modules/customer/` | `ok elimaz.com.vn/crm/internal/modules/customer 2.614s` |
| K2 9 giao, 1 hoàn → {9, 1, 1000, watch}; K3 chưa kết thúc → {0, 0, 0, none} | `TestDeliveryStatsFor` | như trên | ok |
| `order.delivery_reversed` (K2 delivered → confirmed qua `ReverseDelivery`) → consumer M2 tính lại {8, 1}; đơn đó hoàn → {8, 2, 2000} | `TestDeliveryStatsFor` ("K2 sau đảo giao", "K2 sau hoàn…") | như trên | ok — 2000 < 3000 nên watch với ngưỡng mặc định; high với HighRateBP 2000 (xem Khác kế hoạch) |
| Khách trả hàng sau khi nhận không đổi `returned` (Q-13) | `TestDeliveryStatsFor` ("K4 sau khi khách trả" → {1, 0, 0, none}) | như trên | ok |
| HighRateBP = 5000 → K1 watch | `TestDeliveryStatsFor` (ngưỡng truyền vào) và `httpapi.TestCustomerDeliveryStatsAPI` (lưu cài đặt fulfillment thật rồi GET) | `go test -race -count=1 -run TestCustomerDeliveryStatsAPI ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.070s` |
| Biên ngưỡng (=, −1, high trước watch, ngưỡng rỗng → none) | `customer.TestClassifyReturnRisk` (7 ca bảng + 2 ca) | `go test -count=1 -run TestClassifyReturnRisk ./internal/modules/customer/` | `ok … 0.505s` |
| Operation `getCustomerDeliveryStats`: trả `{delivered, returned, returnRateBp, risk, hint}`, hồ sơ con đọc số gốc, 403 thiếu `customer.read`, 404 khách không có / tenant khác | `httpapi.TestCustomerDeliveryStatsAPI`; dòng bỏ khỏi `m3_pending.txt` (`TestM3Routes_*`, `tests/arch` M3) | như trên; `go test -count=1 -run 'M3\|CustomerWriter\|Routes' ./tests/arch/` | ok; `ok elimaz.com.vn/crm/tests/arch 0.940s` |
| Không có câu ghi `customer.customers` ngoài module `customer`; `delivery_stats.go` không có UPDATE | `tests/arch.TestCustomerWriter_OnlyCustomerModule` | như trên; thử thêm dòng `// UPDATE customer.customers SET x` vào delivery_stats.go | ok trên mã thật; với dòng thêm: `câu ghi customer.customers … [internal/modules/customer/delivery_stats.go]` (đỏ đúng), đã gỡ |
| Lỗi có sẵn: panic `locked[0]` khi nhóm rỗng (gốc bị gộp giữa `RootOf` và `GroupMembers`) | `customer.TestStatsRootMergedBetweenReads` (tx bọc chen lần gộp sau câu GetRootOf, tất định) | trước khi sửa: `go test -race -run TestStatsRootMergedBetweenReads ./internal/modules/customer/` | trước: `panic … lockGroups stats.go:51 … FAIL`; sau: ok, A = {2, 2, 0, 360000}, B về 0 |
| `TestStatsConcurrentWithMerge` lặp ≥ 100 lần | `TestStatsConcurrentWithMerge` | `go test -race -count=100 -run 'TestStatsConcurrentWithMerge$' ./internal/modules/customer/` | `ok … 57.018s` (100/100). Ghi thật: mã cũ chạy 100 lần trên máy này cũng không panic (`ok … 59.216s`) — lỗi hiếm theo thời điểm, nên bằng chứng tái hiện là test tất định ở dòng trên |
| Cả bộ `customer` (gồm M2 Task 25) | toàn gói | `go test -race -count=1 ./internal/modules/customer/...` | `ok elimaz.com.vn/crm/internal/modules/customer 53.540s` |
| Cổng của task | ci-task | ci-task (lần 1) | lần 1 ĐỎ chỉ vì lint ở 4 tệp test (gochecknoglobals, gocognit, G115, ireturn) — đã sửa; `golangci-lint run` 3 gói → `0 issues` |
| Cổng của task (lần 2, sau sửa lint) | ci-task | ci-task (lần 2) | build, vet, lint, catalog, jscpd, gitleaks, oasdiff, test-guard qua; test: mọi gói `ok` (customer 229.7s, order 525.2s, fulfillment, promotion, cash, tests/arch 36.1s, e2e) trừ `httpapi.TestOrderAPI_CreateRetryLater` (đơn M2, khóa số dư 50 ms — không thuộc phần đổi; lần 1 qua). Chạy lẻ `go test -race -count=5 -run 'TestOrderAPI_CreateRetryLater$' ./internal/httpapi/` → `ok … 5.118s` (5/5): chập chờn do máy chạy nhiều phiên. Không chạy ci-task lần 3 (giới hạn của lời giao) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Không tạo `queries/customer/delivery.sql`: `customer.StatsFor` (M2) đã đọc đúng `delivered_count`, `returned_count` của gốc hiện tại và tính `ReturnRateBP` theo công thức M2; viết câu thứ hai là trùng (kế hoạch cũng ghi "không tính lại").
- Ca "đơn đó sau đó returned → {8, 2, 2000, high}": với ngưỡng mặc định (HighRateBP 3000, Task M3-04) thì 2000 < 3000 → `watch`. Test ghi đúng theo luật của Interface (watch với mặc định) và thêm ca HighRateBP 2000 → high để kiểm biên ≥. Xem Quyết định thay chủ shop.
- Thêm `customer.ReturnRisk` (kế hoạch dùng tên này trong chữ ký `DeliveryStatsFor`): kiểu riêng của customer, handler chép từ `fulfillment.ReturnRisk`.
- Không có migration (kế hoạch: "không có migration"); số 0324 của lời giao không dùng.

## Quyết định thay chủ shop

- **Ca K2 {8, 2, 2000}:** kế hoạch ghi `high`, nhưng Interface (Returned ≥ HighMinReturned **và** tỷ lệ ≥ HighRateBP) với ngưỡng mặc định 30% cho `watch`. Chọn giữ luật của Interface và ngưỡng mặc định của Task M3-04 (không đổi mặc định), vì luật là phần hợp đồng, con số trong ca test là lỗi đếm. Phương án khác: hạ mặc định HighRateBP xuống 2000 (đổi cài đặt của Task M3-04, ảnh hưởng mọi shop) — chủ shop đổi được trong màn cài đặt nếu muốn.
- **Chưa có đơn hoàn luôn `none`** kể cả khi ngưỡng truyền vào là 0 (bên gọi quên truyền): an toàn, không đổi hành vi với cài đặt hợp lệ (mọi ngưỡng > 0 do `fulfillment.Validate`).
- **Sửa panic:** nhóm rỗng → `errGroupRetry` (lùi savepoint, đọc lại gốc, tối đa 3 lần như sẵn có) thay vì trả lỗi; giữ đúng cơ chế M2, dễ đảo ngược (3 dòng).

## Việc phát hiện thêm

- `TestStatsConcurrentWithMerge` không tái hiện panic trong 100 lần trên máy này; nếu còn chập chờn ở nơi khác thì nguyên nhân có thể khác (Task 103 ghi nhận) — test tất định mới khóa đúng nhánh panic.
- `httpapi.TestOrderAPI_CreateRetryLater` đỏ một lần khi máy tải nặng (đơn tạo được 201 thay vì 503: khóa số dư 50 ms không giữ kịp); qua 5/5 khi chạy lẻ. Vùng order, ngoài phạm vi — đề nghị điều phối viên theo dõi.

## Reviewer

Kết luận: ĐẠT (vòng 1, agent reviewer trên `git diff --cached`, 10 tệp).

- Phát hiện phải sửa: không có. Sửa panic đúng: nhóm rỗng → `errGroupRetry` chặn trước `locked[0]`, thứ tự khóa không đổi, tx consumer chưa giữ khóa khi lần gộp chen vào nên không deadlock.
- Ảnh hưởng khớp thực tế; `customer.ErrNotFound` → 404 có sẵn ở `errors.go`; không test nào bị xóa, tắt, nới.
- Ghi chú không bắt buộc: (1) ca {8, 2, 2000} kế hoạch ghi high, luật Interface với mặc định 30% cho watch — cần chủ shop chốt (đã ghi ở Quyết định thay chủ shop); (2) test kiến trúc bắt câu ghi `customer.customers`, không bắt mọi UPDATE — `delivery_stats.go` không có SQL nên chưa ảnh hưởng; (3) `TestOrderAPI_CreateRetryLater` chập chờn ngoài phạm vi, điều phối viên theo dõi.
