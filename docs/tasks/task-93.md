# Task 93 — M3-02: Mở rộng schema cash và lõi ghi sổ COD hai vế

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 93, M3-02; ca TI10, nền TI01–TI09). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `cash.Post` giữ chữ ký (gọi `PostWithID`); dùng ở
  `consumer.go` (1) và test của gói (`cod_test.go`). `cash.Transfer` thêm 3 trường (`StatementLineID`,
  `ReversesTransferID`, `OccurredAt`) — không phá chỗ dựng bằng tên trường (consumer.go, test). Mới: `PostWithID`,
  `Reverse`, `BalancesFor`, `Balances`, `KindFee`, `KindReversal`, `ErrAlreadyReversed`, `ErrTransferNotFound`.
  Ngoài gói: `cmd/crm/modules.go` chỉ dùng `cash.Consumer{}`; `tests/e2e/stack_test.go` dùng `cash.Consumer`. Không
  ai ngoài gói gọi `Post`.
- Gói phụ thuộc ngược (go list): `cmd/crm`, `tests/e2e`. `internal/testkit/shiptest` đọc view `cash.cod_balances_v`
  (giữ nguyên). `tests/arch` (lockorder_frame, viewsdoc) đọc ADR-46 và `docs/views.md`.
- Operation OpenAPI bị chạm, oasdiff: không có (task chỉ có module và schema; API của sổ ở task sau).
- Chỗ dùng trên màn hình: không có.
- Tệp khác đổi theo: mã sinh sqlc `internal/**/internal/store/models.go` của mọi module (sqlc sinh model cho mọi bảng
  của migration, chỉ thêm kiểu `CashCodStatement`… và cột mới); `docs/views.md` (5 view, 3 khóa ngoại xuyên module);
  `tests/arch/viewsdoc_test.go` (số view ghim 33 → 38); `cod.go` nhập `internal/platform/db` (`db.NullUUID`).
- Câu sqlc, bảng, pha migration: `queries/cash/cod.sql` (M1: `InsertTransfer` thêm `statement_line_id`,
  `reverses_transfer_id`, `occurred_at`; `InsertEntry` thêm `shipment_id`); mới `queries/cash/ledger.sql`
  (`GetTransfer`, `TransferEntries`, `ShipmentBalances`). Migration `0303_cash_m3.sql` pha mở rộng: ALTER
  `cash.cod_transfers` (CHECK kind 7 giá trị, `occurred_at`, CHECK lý do, unique đảo một lần, khóa ngoại
  `statement_line_id`, index), ALTER `cash.cod_ledger_entries` (`shipment_id` điền rồi `SET NOT NULL`, index), thay
  thân hàm `cash.cod_transfer_balanced`, bảng mới `cod_statements`, `cod_statement_lines`, `payments`,
  `refund_obligations`, `reconcile_runs`, `reconcile_findings`, view mới `shipment_cod_v`, `cod_transfers_v`,
  `payments_v`, `refund_obligations_v`, `reconcile_runs_v`. Hai `SET NOT NULL` có dòng `-- contract:`.
- Test sẽ chạy: `go test -race ./internal/modules/cash/...` (vùng rủi ro cao: cả bộ cash), `./tests/arch/...`
  (views.md, thứ tự khóa), `./cmd/crm/...`, `tests/e2e` (đọc sổ COD) qua cổng kiểm của task (ci-task).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (tenancy, db, testkit),
  `cash.Post`, trigger `cash.cod_transfer_balanced`, `cash.cod_append_only` của M1, `testkit.MigrateBefore`,
  `testkit.CheckSchemaRulesClean`, `shiptest.Shop`, khối điền dữ liệu từng tenant của `0300`.
- Dùng lại / viết mới vì: dùng lại `Post` (thân chuyển sang `PostWithID`, `Post` gọi lại), `validate`, hằng ngăn và
  loại của M1 (`AccountToCollect`…; không thêm hằng trùng nghĩa `ToCollect`…, xem Khác kế hoạch); dùng lại hàm
  trigger `cash.cod_append_only()` cho `payments`, `reconcile_findings` (hàm chung theo `TG_TABLE_NAME`); thay thân
  hàm cân của M1 (giữ tên, giữ hai constraint trigger). Viết mới `Reverse`, `BalancesFor` (chưa có gì tương tự). Tham số uuid có thể rỗng dùng `db.NullUUID` (jscpd
  báo trùng với hàm tự viết ban đầu, đã bỏ hàm đó).

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: mọi bảng mới thuộc `cash` (04 mục 3: sổ COD, bảng kê, phiếu thu, nghĩa vụ hoàn, đối chiếu).
- 5.2 Không ghi bảng module khác: chỉ ghi bảng `cash`. Khóa ngoại xuyên module mới (`cod_statements` → tài khoản hãng,
  `payments` → `ord.orders`) ghi vào `docs/views.md`.
- 5.3 Đọc module khác qua view: COD đọc qua `fulfillment.shipments_v` (M1 giữ). `Reverse`, `BalancesFor` chỉ đọc bảng
  `cash`.
- 5.4 Nguyên tử: `PostWithID`, `Reverse` chạy trong tx của bên gọi; database kiểm cân lúc commit của chính tx đó.
- 5.5, 5.6 không áp dụng: không sự kiện, không gọi ra ngoài.
- 5.7 Ngữ cảnh shop: bảng mới qua `platform.enable_tenant_rls`, `UNIQUE (tenant_id, id)`, unique/khóa ngoại ghép
  `tenant_id`; điền dữ liệu cũ từng tenant với `set_config('app.tenant_id')` (như 0300). `CheckSchemaRules` → `[]`.
- 5.8 Không HTTP. 5.9 Đường ghi HTTP: chưa có ở task này (task sau); hàm Go có khóa chống trùng (`idempotency_key`
  của bút toán, `reversal:<id>`).
- 5.10 Không Valkey. 5.11 Không thư viện mới. 5.12 Theo module mẫu identity/catalog: sqlc trong `internal/store`, test
  `_test` trên PostgreSQL thật, rapid.

Mục 6:
- 6.1 Đồng thời: hai `Reverse` cùng bút toán → cùng khóa `reversal:<id>` (`ON CONFLICT DO NOTHING`) và unique một phần
  `reverses_transfer_id`; bên sau nhận `ErrAlreadyReversed` (đổi cả 23505 của unique đó sang `ErrAlreadyReversed`).
  Bảng kê, nghĩa vụ hoàn có `version` cho task sau.
- 6.2 Gửi lại: `PostWithID` trùng khóa trả id cũ, `created = false`; khóa dùng lại cho vận đơn/loại khác → lỗi.
- 6.3 Thứ tự khóa: chỉ chèn `cod_transfers` rồi `cod_ledger_entries` (ADR-46 nhóm 9); không khóa dòng.
- 6.4–6.9: không đổi (tx của bên gọi; `db.RetryTx` của bên gọi chạy lại cả thân — `PostWithID` không giữ biến ngoài).
- 6.10 Tiền: `int64` đồng; sổ chỉ ghi thêm (trigger M1 + REVOKE); `payments`, `reconcile_findings` chỉ ghi thêm;
  database kiểm ≥ 2 dòng, tổng 0 **và** mọi dòng cùng vận đơn với bút toán (TI10).
- 6.11 Quy luật: rapid 300 lượt — tổng tenant 0, tổng mỗi bút toán 0, không bút toán bị đảo hai lần, bút toán lệch bị
  từ chối.
- 6.12 Dữ liệu cá nhân: không có (bảng kê chỉ mã vận đơn, số tiền; `reconcile_findings.detail` không dữ liệu cá nhân).
- 6.13 Quyền: crm_app chỉ SELECT, INSERT trên bảng chỉ ghi thêm; UPDATE theo cột trên bảng có trạng thái.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit cấu trúc: `Post` gọi `PostWithID`, hằng dời sang `accounts.go`, không đổi hành vi | cả bộ test M1 của gói (`TestPostValidatesAndIsIdempotent`, `TestConsumer*`, `TestCODLedgerProperty`…) | `go test -race -count=1 ./internal/modules/cash/...`; `golangci-lint run ./internal/modules/cash/...` | `ok elimaz.com.vn/crm/internal/modules/cash 18.675s`; `0 issues.` |
| TI10: database từ chối bút toán lệch 1 đồng, một dòng, không dòng, cân nhưng có dòng khác vận đơn (23514 chứa `unbalanced`); bút toán đúng được | `TestTI10_DatabaseRejectsBadTransfer` (6 ca), `TestDatabaseRejectsUnbalancedTransfer`, `TestDatabaseRejectsLaterUnbalancedLine` (M1) | `go test -race -count=1 -v ./internal/modules/cash/` | `--- PASS: TestTI10_DatabaseRejectsBadTransfer (0.65s)` |
| `PostWithID` lệch, một dòng, lý do sai loại, đảo thiếu đích → lỗi, 0 câu lệnh tới database (tx đếm câu lệnh) | `TestPostWithIDRejectsBeforeDatabase` (6 ca + ca đúng có câu lệnh) | như trên | `--- PASS: TestPostWithIDRejectsBeforeDatabase (0.28s)` |
| `expected:V1` hai lần → created true/false, cùng id, 2 dòng sổ mang `shipment_id` V1; `Post` M1 cùng khóa → false; `occurred_at` rỗng = giờ ghi, có giá trị thì giữ | `TestPostWithIDIdempotent` | như trên | `--- PASS: TestPostWithIDIdempotent (0.28s)` |
| TI01: sau expected + collected `BalancesFor(V1)` = {0, 630000, 0, 0, −630000}, `shipment_cod_v` cùng số; `Reverse(collected)` → {630000, 0, 0, 0, −630000}; đảo lần hai → `ErrAlreadyReversed`; đảo bút toán đảo, thiếu lý do → `ErrInvalidTransfer`; không có → `ErrTransferNotFound` | `TestTI01_BalancesAndReverse` | như trên | `--- PASS: TestTI01_BalancesAndReverse (0.34s)` |
| Đảo hai lần bằng khóa khác → `ErrAlreadyReversed` (unique một phần); 4 lần đảo song song → đúng 1 thành công | `TestPostWithIDSecondReversalOtherKey`, `TestReverseConcurrent` | như trên | `--- PASS: TestReverseConcurrent (0.57s)`, `--- PASS: TestPostWithIDSecondReversalOtherKey (0.22s)` |
| Loại `fee` ghi được; ghi thẳng SQL: adjustment/reversal thiếu lý do, expected có lý do → 23514 `cod_transfers_reason_kind`; reversal thiếu đích, adjustment có đích → 23514 `cod_transfers_reversal_target` (so tên ràng buộc, không lẫn lỗi "unbalanced" lúc commit) | `TestFeeAndAdjustmentReason` (5 ca) | `go test -race -count=1 -run TestFeeAndAdjustmentReason -v ./internal/modules/cash/` | `--- PASS: TestFeeAndAdjustmentReason (0.33s)` |
| `UPDATE` dòng sổ bằng crm_app → 42501; bằng chủ bảng → 42501 `append-only` (dòng sổ, `payments`, `reconcile_findings`) | `TestM3AppendOnly`, `TestLedgerIsAppendOnly` (M1) | như trên | `--- PASS: TestM3AppendOnly (0.57s)` |
| Dữ liệu M1 (expected, collected; hai tenant) chạy 0303: mọi dòng sổ có `shipment_id` của bút toán, `occurred_at` = `created_at`, tổng 0, trigger chỉ ghi thêm bật lại, luật schema sạch | `TestMigrateM1CashData` | như trên | `--- PASS: TestMigrateM1CashData (1.21s)` |
| Ràng buộc bảng mới (sha256, trạng thái, unique tệp, khóa ngoại tài khoản hãng, dòng bảng kê, dấu phiếu hoàn, nghĩa vụ hoàn unique NULLS NOT DISTINCT, đã trả ≤ phải trả, quyền UPDATE theo cột, `statement_line_id` khóa ngoại) | `TestSchemaM3Cash_Constraints` (24 ca + khóa ngoại dòng bảng kê) | như trên | `--- PASS: TestSchemaM3Cash_Constraints (0.46s)` |
| `CheckSchemaRules` → `[]` | `TestSchemaM3Cash_Rules` | như trên | `--- PASS: TestSchemaM3Cash_Rules (0.48s)` |
| Quy luật (rapid 300 lượt, 1–30 thao tác, 1–5 vận đơn, expected/collected/returned/remitted 1–50.000.000, xen Reverse, xen lệch qua Go và SQL): tổng tenant 0, mỗi bút toán ≥ 2 dòng tổng 0, không bút toán bị đảo hai lần, số dư khớp mô hình | `TestCODLedgerM3Property` | như trên | `--- PASS: TestCODLedgerM3Property (20.36s)` |
| Bộ test M1 Task 32 vẫn xanh | `TestConsumer*`, `TestPost*`, `TestCODLedgerProperty` | như trên | `ok elimaz.com.vn/crm/internal/modules/cash 22.085s` |
| `docs/views.md` khớp database (5 view mới) | `TestViewsDocParses`, `TestViewsDocMatchesDatabase` | `go test -count=1 ./tests/arch/...` | `ok elimaz.com.vn/crm/tests/arch 12.940s` |

## Test bị xóa hoặc tắt

Không xóa, không tắt, không nới test nào. Sửa theo CHECK mới của 0303 (giữ nguyên điều kiểm): `rawTransfer` (helper,
thêm lý do và `shipment_id`), `TestDatabaseRejectsLaterUnbalancedLine` (chèn kèm `shipment_id`, vẫn đòi 23514),
`TestPostLimitsAndActorID` (biên lý do 500/501 ký tự đo trên adjustment), `TestViewsDocParses` (33 → 38 view).

## Khác kế hoạch

- Hằng tên ngăn: giữ tên M1 `AccountToCollect`… (đã có, Q-X1 giữ chữ ký M1), không thêm `ToCollect`… trùng nghĩa
  (CLAUDE.md: chuyển cách mới thì xóa cách cũ; xóa tên M1 lại phá chữ ký). `ErrUnbalanced` đã có từ M1, giữ.
- Thông báo lỗi trigger là `cod transfer unbalanced: <id>` (kế hoạch: `cod transfer <id> unbalanced`) để test M1 kiểm
  chuỗi `cod transfer unbalanced` vẫn đúng; vẫn chứa `unbalanced`, mã 23514.
- `CHECK actor_type` và khóa ngoại `reverses_transfer_id` đã có từ 0119 nên 0303 không thêm lại.
- Thêm (ngoài danh sách kế hoạch, cùng mục đích giữ dữ liệu tiền đúng): CHECK `cod_transfers_reversal_target`
  (`reversal` ⇔ có `reverses_transfer_id`); khóa ngoại `cod_statement_lines.posted_transfer_id → cod_transfers`,
  `refund_obligations.order_id → ord.orders`, `refund_obligations.return_id → ord.returns`; sentinel
  `ErrTransferNotFound` cho `Reverse` khi không có bút toán.
- Test M1 sửa theo CHECK mới (không nới): `rawTransfer` ghi adjustment kèm lý do và dòng sổ có `shipment_id`;
  `TestDatabaseRejectsLaterUnbalancedLine` chèn kèm `shipment_id`; `TestPostLimitsAndActorID` đo biên lý do 500 ký tự
  trên adjustment (từ 0303 expected không được có lý do). Số view ghim trong `TestViewsDocParses` 33 → 38.

## Quyết định thay chủ shop

- `Reverse` một bút toán `reversal` → `ErrInvalidTransfer` (không "đảo của đảo"). Vì: unique "một bút toán chỉ bị đảo
  một lần" làm bút toán gốc không đảo lại được sau đó, dễ rối sổ; muốn ghi lại thì ghi bút toán mới. Phương án khác:
  cho phép đảo bút toán đảo. Dễ đảo ngược (một nhánh trong `Reverse`).
- `reconcile_runs` không có trigger chỉ ghi thêm và crm_app không có quyền UPDATE (kế hoạch chỉ nói `reconcile_findings`
  chỉ ghi thêm): lượt đối chiếu ghi một lần với số phát hiện đã đếm. Phương án khác: cấp UPDATE (ok, findings) khi
  task đối chiếu cần — chỉ thêm GRANT.
- Không thêm CHECK nghiệp vụ chưa có trong kế hoạch cho bảng kê, nghĩa vụ hoàn (ví dụ `paid` ⇒ đã trả đủ): để task
  dùng bảng quyết định cùng hành vi.

## Việc phát hiện thêm

- `PostWithID` nhận `Kind = reversal` với đích bất kỳ (không kiểm đích cùng vận đơn, vế đảo dấu); chú thích `Transfer`
  ghi "chỉ ghi reversal qua `Reverse`". Khi trùng khóa, `PostWithID` chỉ so vận đơn và loại, không so
  `ReversesTransferID`/`StatementLineID` (chưa có đường nào dùng). Task ghi bút toán từ bảng kê (M3-04) nên xem lại.

## Reviewer

Kết luận: ĐẠT (commit cấu trúc, lượt 1): `Post` giữ chữ ký và mọi đường trả lỗi; `PostWithID` trả `old.ID` khi trùng
khóa, `id` khi ghi mới; hằng dời nguyên văn. Ghi chú: `id` trả về chưa có test (đưa vào commit hành vi); mục Khác kế
hoạch phải khớp mục Tái dùng.

Kết luận: ĐẠT (commit hành vi, lượt 2). Lượt 1 CẦN SỬA: `TestFeeAndAdjustmentReason` chỉ so mã 23514 nên vẫn qua khi
bỏ CHECK lý do (lỗi "unbalanced" của trigger lúc commit cũng là 23514) → đã sửa: 5 ca, mỗi ca vi phạm đúng một CHECK,
so cả `pg.ConstraintName` (`cod_transfers_reason_kind`, `cod_transfers_reversal_target`). Lượt 2: không còn phát hiện;
hai ghi chú không bắt buộc (đích của reversal qua `PostWithID`, so khóa trùng) chép vào "Việc phát hiện thêm".
