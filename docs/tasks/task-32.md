# Task 32 — Sổ COD tối thiểu (M1-32)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 32 (M1-32)). Loại việc (quy trình mục
3): A. Vùng rủi ro cao: tiền (sổ COD hai vế, ADR-24). Lát mỏng (Q-X1): bảng `cash` đúng tên ERD (05 mục 2 "Sổ COD"); M3
mở rộng bằng ALTER, không tạo lại.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu có sẵn nào đổi. Gói mới `internal/modules/cash`
  (`Post`, `Transfer`, `Entry`, `Actor`, hằng ngăn/loại bút toán, `Consumer`, `ConsumerName`, sentinel lỗi). Trong
  `cmd/crm` (gói main, không ai nhập): `newEvents()` đổi thành `newEvents() (*events.Registry, error)` để đăng ký
  consumer `cash`; 3 chỗ gọi (`newCommands`, `runWorker`, `serveHTTP`) sửa cùng task.
- Gói phụ thuộc ngược (go list): `cash` mới, chỉ `cmd/crm` nhập. `cash` nhập `fulfillment` (hằng topic, kiểu payload
  `StatusChanged`, `Status`) — được phép theo `tests/arch/boundaries_test.go` (`"cash": {"order", "fulfillment"}`).
- Operation OpenAPI bị chạm, oasdiff: không (không có đường HTTP; M3 làm màn và API sổ COD).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: migration mới `0119_cash_cod.sql` (pha mở rộng: CREATE bảng `cash.cod_transfers`,
  `cash.cod_ledger_entries`, hai constraint trigger kiểm cân lúc commit, trigger chỉ ghi thêm, view
  `cash.cod_balances_v`, GRANT). Câu mới `queries/cash/cod.sql` (chèn bút toán, chèn dòng, đọc loại bút toán của vận
  đơn, đọc vận đơn qua `fulfillment.shipments_v`, đọc số dư qua `cash.cod_balances_v`); khối sqlc mới →
  `internal/modules/cash/internal/store`. Dòng `docs/views.md` cho `cash.cod_balances_v` đã có từ Task M1-25; ADR-46
  nhóm 9 đã có.
- Test sẽ chạy: `go test -race ./internal/modules/cash/... ./cmd/crm/... ./tests/arch/...`; vì consumer đăng ký vào sổ
  sự kiện dùng chung với fulfillment: `./internal/modules/fulfillment/...`; `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `events`
  (`Consumer`, `Registry.Register`, `ConsumerKind`, `JobHandlers`, `StoredEvent`), `queue` (`ErrPermanent`),
  `tenancy.WithTenant`; mẫu sổ chỉ ghi thêm `inventory.stock_ledger` (0115: trigger theo câu lệnh,
  `insufficient_privilege`); kiểu `Actor` của `inventory`/`order`; tìm "DEFERRABLE", "CONSTRAINT TRIGGER",
  "Consumer" trong mã: chưa có consumer nào và chưa có constraint trigger hoãn nào.
- jscpd (lần `make ci` đầu, 3 đoạn trùng mới): hai đoạn dựng dữ liệu thử giống `fulfillment/env_test.go` (gói test
  khác, không nhập được; gom thành gói chung là việc ngoài phạm vi, ghi "Việc phát hiện thêm") → viết lại phần dựng
  của `cash` gọn theo nhu cầu riêng (một khách dùng chung, `order(tx)` tách riêng); đoạn trùng trong `cod_test.go`
  (dựng bút toán cân, kiểm mã lỗi) → gộp thành `withBalancedTransfer`, `execApp`, `pgCode`.
- Dùng lại / viết mới vì: dùng lại `events.Consumer` (worker, thứ tự nghiêm theo vận đơn, thử lại), `queue.ErrPermanent`,
  `fulfillment.TopicShipmentStatusChanged`/`StatusChanged`, `invtest.Shop` + `fulfillment.CreateForOrder` trong test để
  có vận đơn thật. Viết mới: trigger kiểm cân (chưa có sổ hai vế nào), `Actor` riêng của `cash` (mỗi module tự khai
  Actor theo mẫu `inventory`, `order`, `fulfillment`; cùng hình nhưng khác lý do đổi — M3 thêm tác nhân đối soát).

## Soát thiết kế

- 5.1 Chủ dữ liệu: sổ COD thuộc `cash` (04 mục 3), schema `cash` (0112).
- 5.2 Không ghi bảng module khác: `cash` chỉ ghi `cash.*`.
- 5.3 Đọc qua view: vận đơn đọc qua `fulfillment.shipments_v` (security_invoker, đã GRANT cho crm_app).
- 5.4 Nguyên tử: một sự kiện → các bút toán của nó chèn trong tx của việc consumer; trigger hoãn kiểm cân lúc commit.
- 5.5 Không nguyên tử: sổ COD đi sau commit của vận đơn, qua sự kiện `shipment.status_changed` (đúng kế hoạch).
- 5.6 Gọi ra ngoài: không áp dụng (consumer không gọi mạng).
- 5.7 Ngữ cảnh shop: worker chạy Handle trong WithTenant của sự kiện; hai bảng có `tenant_id`, RLS
  (`platform.enable_tenant_rls`), unique và khóa ngoại ghép `tenant_id` (`(tenant_id, shipment_id)` →
  `fulfillment.shipments`, `(tenant_id, transfer_id)` → `cod_transfers`, `(tenant_id, reverses_transfer_id)`).
- 5.8 Không HTTP: module không nhập `net/http`.
- 5.9 Đường ghi: không có đường HTTP; chống trùng bằng `UNIQUE (tenant_id, idempotency_key)` +
  `ON CONFLICT DO NOTHING`.
- 5.10 PostgreSQL là nguồn sự thật: có; không Valkey.
- 5.11 Không thêm thư viện.
- 5.12 Theo module mẫu: gói gốc công khai, sqlc trong `internal/store`, test `_test` trên PostgreSQL thật, rapid.
- 6.1 Đồng thời: việc `event.cash` chạy thứ tự nghiêm theo vận đơn (OrderKey `cash:shipment:<id>`), nên hai sự kiện
  của một vận đơn không chạy cùng lúc. Chốt thêm ở database: khóa bút toán unique theo `idempotency_key`; consumer
  kiểm "đã có bút toán loại này cho vận đơn" trước khi ghi. Bút toán các vận đơn khác nhau không đụng dòng chung.
- 6.2 Gửi lại: chạy lại cùng sự kiện → cùng `IdempotencyKey` (`evt:<eventID>:<kind>`) → `created=false`, không thêm dòng;
  sự kiện khác cùng trạng thái của cùng vận đơn → đã có bút toán loại đó → bỏ qua.
- 6.3 Thứ tự khóa: ADR-46 nhóm 9 (`cod_transfers` → `cod_ledger_entries`); chỉ chèn, không khóa dòng vận đơn.
- 6.4 Lỗi tạm: việc nền dùng lịch thử lại của hàng đợi.
- 6.5 Cách ly: READ COMMITTED mặc định; không có cập nhật có điều kiện (chỉ chèn).
- 6.6 Việc nền lỗi: lỗi trả về → thử lại theo lịch; payload hỏng, vận đơn không có → `queue.ErrPermanent` (dead ngay,
  chặn sự kiện sau của đúng vận đơn đó — thứ tự nghiêm, để không ghi `collected` khi thiếu `expected`).
- 6.7 429: không áp dụng (không gọi ra ngoài).
- 6.8 Chết giữa chừng: tx lùi, việc về hàng chờ, chạy lại cùng khóa chống trùng.
- 6.9 Treo lâu: dùng giới hạn chung của worker (30 s).
- 6.10 Tiền: `bigint` đồng, `int64` trong Go; `amount <> 0`, `abs(amount) <= 2^53−1`; hai bảng chỉ ghi thêm (thu quyền
  UPDATE/DELETE và trigger chặn với mọi role); constraint trigger `DEFERRABLE INITIALLY DEFERRED` trên cả hai bảng: lúc
  commit mỗi bút toán vừa chạm có ≥ 2 dòng và tổng = 0, ngược lại lỗi `cod transfer unbalanced`. `Post` kiểm trước
  cùng điều đó (lỗi sớm, rõ), database là chốt cuối.
- 6.11 Quy luật: rapid 300 lượt — tổng mọi dòng sổ = 0 và số dư từng vận đơn khớp trạng thái cuối.
- 6.12 Dữ liệu cá nhân: sổ chỉ mang mã vận đơn và số tiền; `reason` không chứa dữ liệu khách; không log trong module.
- 6.13 Quyền: không có đường HTTP; crm_app chỉ SELECT, INSERT trên hai bảng, SELECT trên view.

## Bằng chứng

Lệnh chung: `CRM_PG_PORT=5600 go test -race ./internal/modules/cash/... ./cmd/crm/...` (bộ chung crm-shared, database
thử riêng mỗi test) → `ok elimaz.com.vn/crm/internal/modules/cash 17.2s`, `ok elimaz.com.vn/crm/cmd/crm 1.5s`. Trước khi
viết mã: `go test ./internal/modules/cash/...` → `FAIL … [build failed]` (gói chưa có).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TI10: 0 dòng, 1 dòng `+530000`, `+530000/−529999`, 3 dòng lệch → commit lỗi `23514 cod transfer unbalanced`; `±530000`, 3 dòng cân → được | `TestDatabaseRejectsUnbalancedTransfer` | `go test -race -run TestDatabaseRejects ./internal/modules/cash/` | PASS |
| Dòng thêm sau vào bút toán cũ làm lệch → bị chặn lúc commit | `TestDatabaseRejectsLaterUnbalancedLine` | như trên | PASS |
| `UPDATE`/`DELETE` sổ, bút toán bằng `crm_app` → `42501`; chủ bảng cũng bị trigger chặn | `TestLedgerIsAppendOnly` | `-run TestLedgerIsAppendOnly` | PASS |
| `Post` kiểm trước (lệch → `ErrUnbalanced`; dòng 0, vượt 2^53, ngăn lạ/lặp, loại, tác nhân lạ, thiếu khóa/vận đơn → `ErrInvalidTransfer`), không ghi gì; cùng khóa → `created=false`, không thêm dòng; khóa dùng lại cho loại khác → `ErrIdempotencyKeyReused` | `TestPostValidatesAndIsIdempotent` | `-run TestPost` | PASS |
| `shipping` (COD 530000) → `to_collect 530000`, `sales_clearing −530000`; `delivered` → `to_collect 0`, `carrier_holding 530000`; tổng = 0; `awaiting_pickup` không ghi | `TestConsumerShippingThenDelivered` | `-run TestConsumer` | PASS |
| `delivered` tới thẳng → expected + collected | `TestConsumerDeliveredWithoutShipping` | như trên | PASS |
| Cùng sự kiện 2 lần, sự kiện khác cùng trạng thái → 1 bút toán; khóa `evt:<id>:expected` | `TestConsumerIsIdempotent` | như trên | PASS |
| `cod_amount = 0` → không bút toán | `TestConsumerZeroCOD` | như trên | PASS |
| Payload hỏng, vận đơn không có → `queue.ErrPermanent` | `TestConsumerPermanentErrors` | như trên | PASS |
| Nối dây: `Registry.Register(cash.Consumer{})` + `Emit` + worker `RunOnce` → số dư đúng | `TestConsumerThroughRegistryAndWorker` | như trên | PASS |
| `cmd/crm` đăng ký consumer: worker có handler `event.cash` | `TestNewEventsRegistersCash` | `go test -race ./cmd/crm/` | PASS |
| Thuộc tính (rapid 300 lượt): 1–5 vận đơn, chuỗi trạng thái chỉ tiến, giao lại (cả sau sự kiện mới hơn), đan xen giữa vận đơn → tổng mọi dòng = 0, số dư từng vận đơn khớp trạng thái | `TestCODLedgerProperty` | `go test -race -v -run Property ./internal/modules/cash/` | `[rapid] OK, passed 300 tests` |
| Cổng kiểm | `make ci` (sau `git merge main` lần hai, Task 44) | `CRM_PG_PORT=5600 make ci` | build, vet, lint (0 issues), gen-check, sqlc-vet, test (mọi gói ok, gồm `tests/arch`, `tests/e2e`, `fulfillment`, `order`, `events`), vuln, oasdiff, catalog-check qua; jscpd lần đầu báo 3 đoạn trùng → sửa (mục Tái dùng) → `Found 0 clones`; rồi `make test-guard gitleaks console-check catalog-check` qua (`no leaks found`, vitest 101/101). Lần chạy đầu `internal/imager` `TestImager_HangKilledWithinDeadline` đỏ một lần (máy đang tải, task không chạm imager); chạy lại `-count=3` → ok |

## Quyết định thay chủ shop

1. **Trạng thái lấy từ payload sự kiện, số COD đọc qua `fulfillment.shipments_v`.** Payload cho biết vận đơn vừa tới
   trạng thái nào (đúng lần chuyển); view là nguồn sự thật của số tiền. Phương án khác: đọc cả trạng thái từ view
   (sai khi việc chạy trễ, vận đơn đã đi tiếp).
2. **`delivered` mà chưa có `expected` (hãng bỏ qua tín hiệu lấy hàng, fulfillment cho nhảy `awaiting_pickup →
   delivered`) → ghi `expected` rồi `collected` trong cùng việc.** Giữ quy luật "delivered: carrier_holding = X,
   to_collect = 0". Phương án khác: chỉ ghi `collected` (to_collect âm, sai nghĩa ngăn).
3. **Mỗi loại `expected`/`collected` chỉ một bút toán cho một vận đơn** (consumer đọc loại đã ghi rồi bỏ qua), ngoài
   khóa `evt:<eventID>:<loại>` của kế hoạch: hai sự kiện khác nhau cùng trạng thái không ghi hai lần. Không thêm unique
   ở database (M3 có thể cần nhiều bút toán cùng loại, ví dụ giao lại sau hoàn) — dễ đảo ngược.
4. **`Post` với khóa đã dùng cho vận đơn hay loại khác → `ErrIdempotencyKeyReused`** thay vì lặng lẽ `created=false`
   (kế hoạch chỉ nói ca trùng đúng). Tránh nuốt lỗi lập trình ở sổ tiền.
5. **`actor_type` có CHECK `staff | system | carrier`**; consumer ghi `carrier` (cùng cách `order`/`inventory` ghi bước
   vận đơn). M3 mở rộng CHECK bằng ALTER nếu cần. Thêm CHECK độ dài `idempotency_key` 1–200, `reason` ≤ 500.
6. **Trigger kiểm cân chạy bằng quyền người gọi** (không SECURITY DEFINER): RLS chỉ cho thấy dòng của tenant đang đặt,
   đếm sai tenant ra 0 dòng → từ chối (an toàn).
7. **`newEvents()` trả thêm `error`** (đăng ký consumer có thể lỗi) thay vì panic; sửa 3 chỗ gọi.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `cmd/crm/main_test.go` (`TestNewEventsRegistersCash`) để có bằng chứng phần sửa `cmd/crm/main.go`.
- Không có hàm đọc số dư công khai: test đọc `cash.cod_balances_v` bằng SQL (M3 thêm đường đọc khi có màn).

## Việc phát hiện thêm

- `internal/imager` `TestImager_HangKilledWithinDeadline` chập chờn khi máy tải nặng (`script giả chưa chạy: stat …/pid`);
  qua khi chạy riêng. Không thuộc task này.
- Phần dựng dữ liệu thử "cửa hàng + tài khoản hãng + đơn + vận đơn" lặp ở test của `fulfillment` và `cash` (gói test
  khác nhau, không nhập được). Có thể gom vào một gói `fulfillment/fulfilltest` ở task sau (Task 33 cũng cần).
- Sổ cho phép thêm dòng vào bút toán cũ ở transaction sau nếu vẫn cân (ví dụ hai ngăn mới ±5): trigger chỉ kiểm cân.
  M3 cân nhắc chặn "dòng chỉ chèn cùng transaction với bút toán" khi làm `adjustment`.

## Reviewer

Kết luận: ĐẠT (lần 1, trên `git diff --cached` sau khi gộp main lần hai).

- Phát hiện phải sửa: không có. Reviewer đã kiểm: trigger kiểm cân không vượt được (`SET CONSTRAINTS IMMEDIATE` chỉ
  kiểm sớm hơn; nhiều bút toán cùng tx mỗi dòng xếp một lần kiểm; FORCE RLS, đổi tenant → đếm 0 → từ chối; crm_app
  không tắt được trigger); quyền crm_app chỉ SELECT, INSERT; consumer chống trùng (thứ tự nghiêm `cash:shipment:<id>`,
  khóa `evt:<id>:<loại>`, `TransferKinds`), delivered không qua shipping, COD 0, lỗi vĩnh viễn; đủ 3 chỗ gọi
  `newEvents()`; `models.go` của 7 gói store khác sinh lại do `make gen` (schema là cả `migrations`), không sửa tay.
- Ghi chú không bắt buộc (không sửa): (1) thêm dòng cân vào bút toán cũ ở tx sau — đã ghi "Việc phát hiện thêm" cho
  M3; (2) `TestLedgerIsAppendOnly` không tách riêng lỗi thiếu GRANT với lỗi trigger (cùng mã 42501) — GRANT đúng, chặn
  hai lớp; (3) consumer đọc COD qua view thay vì payload — đúng vì `cod_amount` không bị cập nhật sau khi tạo.
