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
  hàm cân của M1 (giữ tên, giữ hai constraint trigger). Viết mới `Reverse`, `BalancesFor` (chưa có gì tương tự).

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

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Quyết định thay chủ shop

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (commit cấu trúc, lượt 1): `Post` giữ chữ ký và mọi đường trả lỗi; `PostWithID` trả `old.ID` khi trùng
khóa, `id` khi ghi mới; hằng dời nguyên văn. Ghi chú: `id` trả về chưa có test (đưa vào commit hành vi); mục Khác kế
hoạch phải khớp mục Tái dùng.
