# Task 28 — Inventory lõi

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 28 = M1-28: kho, số dư, sổ chỉ ghi
thêm, giữ hàng chống bán vượt). Loại việc (quy trình mục 3): A. **Vùng rủi ro cao** (tồn kho, chạy đồng thời).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn. Thêm gói mới
  `internal/modules/inventory` (chưa ai tham chiếu: 0 chỗ) và `internal/modules/inventory/invtest` (helper test cho
  module khác). `cmd/crm/seed.go` gọi thêm `inventory.EnsureDefaultWarehouse` (hàm `seedDev` không đổi chữ ký).
  Thêm (không đổi cái cũ): `realtime.WarehouseScope` (Q-T28-8), `testkit.RecordingBus`, `(*testkit.Staff).CountIn`.
  Test catalog đổi sang dùng hai helper testkit (bỏ `fakeBus` và thân `count` riêng; 1 tệp `products_test.go`).
- Gói phụ thuộc ngược (go list): gói mới chưa ai nhập. `cmd/crm` nhập thêm `inventory`. `inventory` không nhập
  `catalog` hay `identity` (test `TestBoundariesRealCode`: `inventory` chỉ được gọi `catalog`); chỉ đọc view
  `catalog.variants_v` bằng SQL. `realtime`, `testkit` chỉ thêm ký hiệu: gói nhập chúng không đổi hành vi.
- Mã sinh: sqlc dùng chung `schema: migrations` nên `models.go` của `catalog`, `identity`, `platform` sinh lại thêm kiểu
  `Inventory*` (chỉ thêm, không sửa tay).
- Operation OpenAPI bị chạm, oasdiff: không (Task 28 chưa có API; `listBalances` ở Task M1-33).
- Chỗ dùng trên màn hình: không (màn hình tồn ở Task M1-50 trở đi).
- Câu sqlc, bảng, pha migration: migration mới `0115_inventory.sql` (pha mở rộng: chỉ `CREATE`, `GRANT`): bảng
  `inventory.warehouses`, `stock_balances`, `stock_ledger`, `stock_reservations`; view `warehouses_v`,
  `stock_available_v`, `stock_ledger_v`; trigger chỉ ghi thêm của sổ. Khối sqlc mới `queries/inventory` →
  `internal/modules/inventory/internal/store`. Số `0115` nhỏ hơn `0124` đã có: chạy được nhờ Task 318 (lệch thứ tự).
- ADR-46 (`docs/adr/0046-thu-tu-khoa.md`): nhóm 11 đã có `warehouses → … → stock_balances → stock_reservations`,
  `stock_ledger` ở mục (d) — chỉ kiểm khớp, không sửa. `docs/views.md`: ba view và sự kiện `stock.changed` đã có —
  chỉ kiểm khớp (`viewsdoc_test`).
- Test sẽ chạy: `go test -race ./internal/modules/inventory/...` (mới), `./cmd/crm/...` (seed-dev), `./tests/arch/...`
  (luật schema, view, SQL, boundaries), `./internal/platform/tenancy/...` (`CheckSchemaRules` trên schema đã migrate),
  `./internal/platform/db/...` (migration chạy được), `./tests/e2e/...` (seed-dev). Vùng cao: cả bộ inventory.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (db, events, realtime,
  tenancy, queue, logging); tìm `sort.*uuid`, `bytes.Compare`, `append-only`, `FOR UPDATE`, `ON CONFLICT` trong
  `internal/`; mẫu module có chiều người bán `internal/modules/catalog` (Writer, events.go, errors.go, test env).
- Dùng lại: `db.RetryTx`/`tenancy.WithTenant` (bên gọi), `events.Registry.Emit`, `realtime.Bus.Publish`,
  `identity.Scope`, `testkit.OpenStaff`, `testkit.SetRapidChecks`; trigger chỉ ghi thêm theo câu lệnh làm như
  `identity.audit_logs` (0004); mẫu `Writer` của catalog (Q-T27-2) cho đường ghi có sự kiện; `catalog.EnsureDefaultSeller`
  trong seed-dev và test.
- Viết mới: sắp xếp theo `(variant_id, warehouse_id)` bằng `bytes.Compare` trên 16 byte của uuid (đúng thứ tự so sánh
  `uuid` của PostgreSQL) — chưa có chỗ nào cần; `applyDelta` một câu `UPDATE` có điều kiện (kế hoạch giao riêng cho
  inventory, Task M1-29 dùng lại). `fakeBus` của test: trùng với test catalog; giữ riêng vì là mã test của từng gói
  (testkit chưa có Bus giả) — jscpd báo trùng nên đã gộp vào `testkit.RecordingBus` (Q-T28-9), cùng hàm đếm
  `Staff.CountIn`. Hai đoạn trùng trong chính test inventory (chạy song song hai hàm, đếm kết quả đua) tách thành
  `both`, `raceReserve`. `Writer.check` của catalog: inventory kiểm `Events/Queue` ở chỗ phát sự kiện (`changed`), không
  chép hàm.

## Soát thiết kế

Mục 5 (khớp kiến trúc):

- 5.1 Chủ dữ liệu: kho, số dư, sổ, giữ hàng thuộc `inventory` (04 mục 3) → schema `inventory`.
- 5.2 Không ghi bảng module khác: chỉ ghi `inventory.*`; sự kiện qua `events`, báo hiệu qua `realtime`.
- 5.3 Đọc module khác qua view: `seller_id` của số dư lấy từ `catalog.variants_v` (view công bố); khóa ngoại ghép
  `(tenant_id, seller_id, variant_id) → catalog.variants` (khóa ngoại xuyên module được phép như ADR-27 mục 4).
- 5.4 Nguyên tử: `Reserve`/`Release`/`StartTransit`/`Consume` chạy trong `tx` của bên gọi (đơn ở Task M1-30 gọi trong
  cùng transaction tạo đơn); không tự mở transaction.
- 5.5 Sau commit: `stock.changed` ghi bằng `events.Emit` trong tx (tới consumer sau commit); báo hiệu đi khi commit.
- 5.6 Gọi ra ngoài: không áp dụng (không gọi bên ngoài).
- 5.7 Ngữ cảnh shop: mọi bảng có `tenant_id`, RLS (`enable_tenant_rls`), `UNIQUE (tenant_id, id)`, khóa ngoại ghép
  `tenant_id`; `CheckSchemaRules` kiểm.
- 5.8 Không biết HTTP: module không nhập `net/http`, ogen.
- 5.9 Đường ghi có khóa chống trùng: không áp dụng ở task này (không có route; bên gọi qua API ở Task M1-29/30 có
  `Idempotency-Key`). Ở mức dữ liệu: unique sổ `(doc_type, doc_id, variant, warehouse, bucket, step)`, unique giữ hàng
  `(order_item_id, variant, warehouse)`.
- 5.10 Valkey: không dùng.
- 5.11 Thư viện: không thêm (pgx, uuid, rapid đã có).
- 5.12 Module mẫu: theo `catalog` (gói gốc công khai, `internal/store` sqlc, test `_test` trên PostgreSQL thật, rapid).

Mục 6 (đồng thời):

- 6.1 Hai yêu cầu cùng sửa: mỗi thay đổi số dư là một `UPDATE … SET <ngăn> = <ngăn> + d WHERE … AND <điều kiện cố
  định>` (`available >= q`, `held >= q`, `in_transit >= q`, `damaged >= q`); 0 dòng → thiếu. Không đọc số dư ra Go rồi
  ghi lại (ADR-46 điểm 5). Đổi trạng thái giữ hàng `UPDATE … WHERE state = <cũ>` (0 dòng → `ErrStateConflict`, B2).
- 6.2 Gửi lại: unique sổ chặn áp một bước hai lần; giữ hàng nhả hai lần → `ErrStateConflict`.
- 6.3 Thứ tự khóa (ADR-46 điểm 4, nhóm 11): `stock_balances` trước `stock_reservations`. `Reserve`: sắp dòng theo
  `(variant_id, warehouse_id)` tăng dần, cập nhật từng dòng số dư theo thứ tự đó, rồi chèn giữ hàng (dòng mới không
  tính). `Release`/`StartTransit`/`Consume`: đọc giữ hàng không khóa (variant, kho của một dòng giữ không bao giờ đổi)
  → khóa số dư `ORDER BY variant_id, warehouse_id FOR UPDATE` → khóa giữ hàng `ORDER BY id FOR UPDATE` → đổi trạng
  thái → đổi số dư theo thứ tự. Thứ tự Go = thứ tự `uuid` của PostgreSQL (so 16 byte). Test `deadlock_test`.
- 6.4 Lỗi tạm: hàm chạy trong tx của bên gọi, bên gọi dùng `db.RetryTx` (qua `WithTenant`); hàm không giữ trạng thái
  giữa các lần chạy lại (Writer dựng mới, không biến toàn cục).
- 6.5 READ COMMITTED: điều kiện nằm trong câu `UPDATE` (PostgreSQL đánh giá lại `WHERE` trên bản mới nhất của dòng
  sau khi chờ khóa), nên đúng ở mức mặc định.
- 6.6–6.8 Việc nền, 429, tiến trình chết: không áp dụng (không có việc nền); tx lùi thì không còn gì.
- 6.9 Treo: dùng giới hạn của `db.RetryTx` (bên gọi).
- 6.10 Tiền: không áp dụng (số lượng `int`); sổ tồn chỉ ghi thêm (trigger theo câu lệnh chặn `UPDATE/DELETE/TRUNCATE`
  mọi role; `crm_app` chỉ `SELECT, INSERT`).
- 6.11 Quy luật: `TestInventoryInvariants` (rapid 300 lượt, 1–4 goroutine): mọi cột ≥ 0, tổng sổ từng ngăn = số dư,
  `held`/`in_transit` = tổng giữ hàng theo trạng thái, `available ≥ 0`; CHECK database chặn số âm.
- 6.12 Dữ liệu cá nhân: không có (chỉ mã, số lượng); lý do nhả là mã ngắn.
- 6.13 Quyền: không có route ở task này; báo hiệu `inventory.balance` đòi `inventory.read` phạm vi `warehouse:<id>`.

## Quyết định thay chủ shop

- **Q-T28-1 — Đường ghi là phương thức của `inventory.Writer`.** Kế hoạch ghi `Reserve`, `Release`, `StartTransit`,
  `Consume`, `*ByItems` là hàm gói, nhưng chúng phải phát `stock.changed` (`events.Registry.Emit` cần `*Registry` và
  `queue.JobQueue`) và báo hiệu (`realtime.Bus`). Chọn giống Q-T27-2 của catalog: các hàm này là phương thức của
  `inventory.Writer{Events, Queue, Realtime}` với đúng tên, tham số, kiểu (`int32`, `Actor`, `*InsufficientError`) của
  kế hoạch; hàm đọc và hàm tạo kho (`Availability`, `DefaultWarehouse`, `EnsureDefaultWarehouse`) giữ là hàm gói. Vì:
  mọi đường ghi tự có sự kiện, không biến toàn cục (lint cấm), cùng cách với catalog. Phương án khác: biến toàn cục
  (cấm); bên gọi tự phát sự kiện (dễ quên). Đảo ngược: đổi chữ ký trước Task M1-30.
- **Q-T28-2 — Lưu lý do nhả.** `Release(…, reason, …)` có `reason` nhưng bảng không có cột. Thêm cột
  `stock_reservations.release_reason text NULL` (CHECK `^[a-z][a-z0-9_]{0,63}$`, chỉ có khi `released`), để danh sách
  "Giữ hàng quá hạn" và đối chiếu biết vì sao nhả. Lý do rỗng hay sai mẫu → `ErrInvalidInput` (lỗi thêm ngoài ba lỗi
  của kế hoạch, dùng chung cho số lượng ≤ 0, thiếu mã, tác nhân lạ). Phương án khác: bỏ qua
  `reason` (mất thông tin bên gọi đưa); ghi vào `step` của sổ (đổi khóa unique).
- **Q-T28-3 — Khóa ngoại giữ hàng và sổ tới số dư.** Thêm khóa ngoại `(tenant_id, variant_id, warehouse_id) →
  stock_balances` cho `stock_reservations` và `stock_ledger` (kế hoạch không ghi): không thể có dòng sổ hay giữ hàng
  của số dư không tồn tại. Khóa `FOR KEY SHARE` của kiểm khóa ngoại rơi vào dòng số dư đã khóa trước trong cùng tx,
  không đổi thứ tự khóa.
- **Q-T28-4 — `Availability` trả đủ mỗi biến thể hỏi.** Biến thể chưa có dòng số dư trả `Balance` toàn 0 (theo thứ
  tự `variant_id` tăng dần, bỏ trùng), để bên gọi không phải phân biệt "chưa nhập kho" với "hết hàng".
- **Q-T28-5 — Một `stock.changed` cho mỗi dòng giữ hàng đã xử lý.** `StartTransit`, `Consume` đổi hai ngăn của cùng
  dòng số dư (version tăng hai lần); sự kiện phát một lần sau khi đổi xong, `Seq` = version cuối. Báo hiệu
  `inventory.balance` mang `Entity{Type: "balance", ID: "<variant>:<warehouse>"}`.
- **Q-T28-6 — `invtest.SeedStock` cộng thêm `on_hand`.** Chèn dòng số dư nếu chưa có (người bán lấy từ
  `catalog.variants_v`), rồi `on_hand + onHand` kèm một dòng sổ `import` (doc mới, step `seed`, actor `system`). Không
  phát sự kiện (helper test). Gọi được bằng `crm_app` hay chủ bảng.

- **Q-T28-7 — Bản theo dòng đơn tìm không khóa.** Kế hoạch ghi `*ByItems` "tìm dòng giữ hàng theo order_item_id
  (ORDER BY id FOR UPDATE) rồi gọi phần lõi", nhưng cùng mục kế hoạch và ADR-46 đòi khóa `stock_balances` trước
  `stock_reservations`; khóa giữ hàng trước rồi lõi mới khóa số dư sẽ ngược thứ tự. Chọn: tìm mã giữ hàng (đúng trạng
  thái cũ) không khóa, rồi lõi khóa số dư → khóa giữ hàng → `UPDATE … WHERE state = <cũ>`; dòng vừa bị transaction
  khác đổi → `ErrStateConflict`. Lõi `Release/StartTransit/Consume` cũng vậy: kế hoạch ghi "SELECT … FOR UPDATE giữ
  hàng, rồi UPDATE, rồi đổi số dư" nhưng câu cuối của mục đó chốt "khóa dòng số dư trước rồi mới cập nhật dòng giữ
  hàng" — làm theo câu cuối (khớp ADR-46).
- **Q-T28-8 — `realtime.WarehouseScope`.** Báo hiệu theo kho cần `*identity.Scope`, mà `inventory` không được nhập
  `identity` (04 mục 3, `TestBoundariesRealCode`). Thêm hàm nhỏ `realtime.WarehouseScope(id) *identity.Scope` vào gói
  `realtime` (đã nhập `identity`), kèm test; `Requires` là hằng chuỗi không kiểu. Phương án khác: sửa bảng "Được gọi"
  (đổi spec); mẹo generic dựng `*identity.Scope` không cần tên gói (né luật).
- **Q-T28-9 — Bus giả và hàm đếm vào `testkit`.** jscpd báo `fakeBus` và thân `count` của test inventory trùng test
  catalog. Gộp (cùng mục đích, cùng lý do đổi): `testkit.RecordingBus`, `(*testkit.Staff).CountIn`; test catalog dùng
  chúng (commit cấu trúc riêng, trước commit hành vi).

## Dừng để hỏi

(không có)

## Bằng chứng

Lệnh chung: `go test -race -count=1 ./internal/modules/inventory/...` (PostgreSQL thật `CRM_TEST_DATABASE_URL`, cổng
5528) trên bản đã gộp `main` `509f61e` → `ok elimaz.com.vn/crm/internal/modules/inventory 29.697s`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| KH04: available = 10 − 1 − 2 − 3 = 4 ở `Availability` và view | `TestAvailability_Formula` | lệnh chung | PASS |
| Biến thể chưa có số dư → 0, bỏ trùng, theo thứ tự (Q-T28-4) | `TestAvailability_MissingRowIsZero` | lệnh chung | PASS |
| Giữ hàng: held +, sổ `order/reserve` của nhân viên, `stock.changed` seq = version đúng payload, báo hiệu `inventory.balance` `inventory.read` phạm vi kho | `TestReserve_HoldsAndRecords` | lệnh chung | PASS |
| Chiếc cuối: 2 tx song song → 1 được, 1 `InsufficientError{Requested 1, Available 0}`; held 1; sổ 1 dòng | `TestReserve_LastItem` | lệnh chung | PASS |
| 50 goroutine trên available 10 → 10 được, 40 thiếu, held 10, 10 dòng giữ | `TestReserve_FiftyOnTen` | lệnh chung | PASS |
| 2 dòng, B thiếu → lỗi của B, A không đổi, 0 giữ, 0 sổ | `TestReserve_PartialShortageRollsBack` | lệnh chung | PASS |
| Chưa có dòng số dư → thiếu; qty ≤ 0, tác nhân lạ, thiếu dòng đơn → `ErrInvalidInput` | `TestReserve_NoRowAndInvalidInput` | lệnh chung | PASS |
| Khóa chéo 200 lượt `{A,B}`/`{B,A}`, tx mở thẳng (không thử lại) → không 40P01, `pg_stat_database.deadlocks` không tăng | `TestReserve_NoDeadlockOnCrossOrder` | lệnh chung | PASS |
| Nhả và đi giao chéo thứ tự 50 lượt → xong, số dư đúng | `TestCore_NoDeadlockOnCrossOrder` | lệnh chung | PASS |
| Release hai lần → lần hai `ErrStateConflict`, held giảm một lần, lý do lưu; lý do sai mẫu → `ErrInvalidInput` | `TestRelease_Twice` | lệnh chung | PASS |
| StartTransit rồi Consume → held 0, in_transit 0, on_hand −3; Consume dòng held → `ErrStateConflict`; 4 dòng sổ đúng bước, actor carrier NULL | `TestTransitThenConsume` | lệnh chung | PASS |
| Mã lạ trong danh sách → `ErrStateConflict` không đổi gì; mã trùng tính một lần; trạng thái trộn → lỗi và chưa ghi gì kể cả khi bên gọi vẫn commit | `TestCore_UnknownIDAndMixed` | lệnh chung | PASS (lượt 2: `ok … 22.934s`) |
| Sổ hệ thống actor_id NULL, nhân viên đúng uuid; đọc được qua `stock_ledger_v` | `TestLedger_Actor` | lệnh chung | PASS |
| Ghi sổ trùng bước → 23505; UPDATE sổ bằng crm_app → 42501; bằng chủ bảng UPDATE/DELETE → "append-only"; held âm → 23514 | `TestLedger_AppendOnly` | lệnh chung | PASS |
| `ByItems` ra cùng số dư, sổ, trạng thái như bản lõi; gọi lại → `ErrStateConflict`; dòng đơn lạ → `ErrStateConflict` | `TestByItems_SameAsCore` | lệnh chung | PASS |
| `DefaultWarehouse` = MAIN "Kho chính" ELIMAZ; tenant chưa có kho → `ErrNoWarehouse`; Ensure chạy lại được | `TestDefaultWarehouse` | lệnh chung | PASS |
| Tenant B không thấy số dư A, không giữ được hàng A; 3 view ngoài ngữ cảnh → 0 dòng | `TestTenantIsolation` | lệnh chung | PASS |
| Bất biến tồn (rapid 300 lượt, 1–4 biến thể, 1–4 goroutine) | `TestInventoryInvariants` | `go test -count=1 -v -run TestInventoryInvariants ./internal/modules/inventory/` | `[rapid] OK, passed 300 tests` |
| seed-dev tạo đúng 1 kho MAIN của ELIMAZ sau hai lần chạy | `TestSeedDev` | `go test -race -count=1 -run TestSeedDev ./tests/e2e/` | `ok` |
| Bảng mới đúng luật schema (RLS, unique, khóa ngoại ghép), view đúng `docs/views.md`, SQL đúng schema, ranh giới module, `sqlc vet` | `tests/arch` (`TestBoundariesRealCode`, `TestSQLUsage*`, `TestViewsDoc*`, `TestSqlcVet`), `internal/platform/tenancy` | `go test -race -count=1 ./tests/arch/... ./internal/platform/tenancy/... ./internal/platform/db/...` | `ok` cả ba |
| `realtime.WarehouseScope` khớp `identity.Can` | `TestWarehouseScope` | `go test -race -count=1 ./internal/platform/realtime/...` | `ok` |
| Test catalog vẫn qua sau khi dùng helper testkit | cả gói | `go test -race -count=1 ./internal/modules/catalog/...` | `ok` |
| Lint, sinh mã | — | `golangci-lint run ./...`; `make gen` rồi `git status`; `make catalog` | `0 issues`; không chênh ngoài tệp của task; danh mục thêm 1 dòng `WarehouseScope` |
| Đột biến 1: bỏ sắp xếp dòng trong `Reserve` → test khóa chéo phải đỏ | `TestReserve_NoDeadlockOnCrossOrder` | sửa tạm `reserve.go`, chạy, khôi phục | `lượt 1: … deadlock detected (SQLSTATE 40P01)` FAIL |
| Đột biến 3: bỏ kiểm trạng thái trước UPDATE (lượt 2) → ca trạng thái trộn phải đỏ | `TestCore_UnknownIDAndMixed` | sửa tạm, chạy, khôi phục | `dòng held bị đổi thành "released" dù lỗi` FAIL |
| Đột biến 2: giữ hàng với điều kiện `none` thay `available` → bất biến và chiếc cuối phải đỏ | `TestInventoryInvariants`, `TestReserve_LastItem` | sửa tạm, chạy, khôi phục | rapid `failed after 5 tests … on_hand_check (23514)`; `mong 1 được 1 thiếu, có 2 được 0 thiếu` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Xem Q-T28-1 … Q-T28-6.

## Việc phát hiện thêm

- `make jscpd` trên `main` (`509f61e`) đã đỏ với 2 đoạn trùng không thuộc task này:
  `internal/platform/db/migrate_test.go` (79–84 với 239–244) và `internal/platform/queue/errors_test.go` 117–122 với
  `worker_test.go` 104–109 (từ Task 318, 317). Task này không chạm hai gói đó. Đề xuất task riêng gộp hoặc ghi baseline.
- Bước "Chạy → FAIL" (Step 2): test viết trước mã, lần chạy đầu đỏ vì gói `inventory` chưa có (không biên dịch). Thay
  cho bằng chứng đỏ theo hành vi, đã chạy hai đột biến có chủ ý trên mã thật (bảng Bằng chứng, hai dòng cuối).

## Reviewer

- **Commit 1 (cấu trúc: `testkit.RecordingBus`, `Staff.CountIn`, test catalog dùng chúng):** ĐẠT. Không có phát hiện
  phải sửa: `CountIn` làm đúng như thân `count` cũ (chủ bảng, `WithTenant`, `t.Helper`); câu kiểm báo hiệu của catalog
  giữ đủ điều cũ; `RecordingBus` khóa `mu` ở cả `Publish` và `Signals` (bản sao), chặt hơn bản cũ; không test nào bị
  bỏ hay nới. Ghi chú không bắt buộc: `fakeBus` của `selftest` có chèn lỗi, khác mục đích — giữ riêng.

- **Commit 2 (hành vi: inventory lõi), lượt 1:** ĐẠT, không có phát hiện phải sửa (đã kiểm UPDATE có điều kiện, thứ
  tự khóa ADR-46 trong `Reserve`/`transition`/`byitems`, `bytes.Compare` khớp so uuid của PostgreSQL, sổ chỉ ghi
  thêm, RLS, view, GRANT, `Seq = version`, từng test so tiêu chí). Ghi chú không bắt buộc đã làm: (1) trạng thái trộn
  ghi dở rồi mới báo `ErrStateConflict` → giờ kiểm trạng thái các dòng đã khóa trước khi UPDATE (không ghi gì), thêm ca
  test và đột biến 3; tài liệu `ErrStateConflict` ghi bên gọi phải lùi; (2) thêm dòng mã sinh `models.go` vào Ảnh
  hưởng. Ghi chú giữ nguyên: `pg_stat_database.deadlocks` có thể cập nhật chậm hơn 2 s — lớp bảo vệ chính là lỗi
  40P01 trả thẳng (tx không thử lại), đột biến 1 cho thấy bắt được.
- **Commit 2, lượt 2 (sau sửa):** ĐẠT, không có phát hiện phải sửa. Bước kiểm trạng thái chạy sau `FOR UPDATE` nên
  không đổi được trước UPDATE; UPDATE vẫn giữ `state = <cũ>` làm lớp chặn sau; thứ tự khóa không đổi; ca trạng thái
  trộn trong test đúng dữ liệu. Reviewer không chạy được `go test` (hook của reviewer chặn); test do người làm chạy.

Kết luận: ĐẠT
