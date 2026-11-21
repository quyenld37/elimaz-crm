# Task 124 — M3-33: Điều chỉnh COD có xem trước; đối chiếu sổ hằng đêm

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 124, M3-33). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không sửa chữ ký nào đã có. Thêm mới ở `cash`:
  `Service.PreviewCODAdjustment`, `Service.CreateCODAdjustment`, `Service.ListReconcileFindings`, kiểu
  `CODAdjustment`, `CODAdjustmentPreview`, `ReconcileFindingPage`, `ReconcileFinding`, sentinel `ErrPreviewStale`,
  `ErrAdjustmentInvalid`, `ErrAdjustReasonRequired`, `ErrAdjustmentBlocked`, `ErrAdjustTargetNotFound`,
  `ErrReconcileRunNotFound`, hằng `ReconcileScheduleName`, `ReconcileAlertKind`, `AdjustReverse`, `AdjustMove`,
  `FindingLedgerNonzero`, `FindingTransferUnbalanced`, `FindingBalanceMismatch`, `FindingSkipped`, hàm
  `NewReconcileAlertCommand`; kiểu `CODAdjustmentInput`. Hàm riêng mới: `notifyOutcome` (commit 1), `withDB`,
  `planAdjustment`. Gói
  mới `cash/internal/adjust`, `cash/internal/reconcile`, `internal/testkit/codtest` (chỉ test dùng). Sửa thân
  `Transfer.validate` (ledger: độ dài lý do đếm theo ký tự như CHECK của bảng, trước đếm byte) — chỗ gọi: `PostWithID`
  (qua `Post`, `Reverse`), cùng gói. `area_adjustments.go`, `area_reconcile.go`: thay hàm khung trả nil.
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi` nhập `cash`; gói con của cash (`formats`, `lists`,
  `postings`, `refunds`, `statement`, `store`) là phụ thuộc xuôi. Gói con mới chỉ cash (và test) nhập.
- Operation OpenAPI bị chạm, oasdiff: `previewCodAdjustment`, `createCodAdjustment`, `listReconcileFindings` — đã có
  trong hợp đồng từ Task M3-04 (khung 501); chỉ thay thân handler, không sửa `openapi.yaml`, oasdiff không đổi. Bỏ ba
  dòng khỏi `tests/arch/testdata/m3_pending.txt`. Mã lỗi `preview_stale` đã khai trong `x-error-codes`.
- Chỗ dùng trên màn hình: chưa có màn nào gọi ba operation (console chỉ có client sinh); không sửa console.
- Câu sqlc, bảng, pha migration: migration mới `0321_cash_reconcile.sql` (pha mở rộng): thêm cột `slot` cho
  `cash.reconcile_runs` cùng unique `(tenant_id, slot)` (một lần chạy mỗi kỳ), index `(tenant_id, run_id, id)` của
  `reconcile_findings` (đọc theo lần chạy), dòng lịch `cash.reconcile` trong `platform.schedules` (86.400 s, theo
  tenant, kỳ đầu 19:30 UTC = 02:30 giờ Việt Nam, như `inventory.reconcile`). Câu mới `queries/cash/reconcile.sql`
  (tổng sổ, bút toán lệch theo lô, số dư từng vận đơn theo lô kèm cờ bỏ qua, ghi lần chạy, ghi phát hiện, đọc phát
  hiện) và `queries/cash/adjust.sql` (trạng thái vận đơn, bút toán của vận đơn kèm ngày chốt bảng kê). Bảng đọc:
  `cash.cod_ledger_entries`, `cod_transfers`, `cod_statement_lines`, `cod_statements`, `reconcile_findings`,
  `fulfillment.shipments_v` (view công bố). Bảng ghi: `cod_transfers`, `cod_ledger_entries` (qua `Reverse`/`PostWithID`),
  `reconcile_runs`, `reconcile_findings` (chỉ chèn), `platform.jobs` (lệnh cảnh báo), `identity.audit_logs`.
- Test sẽ chạy: `go test -race ./internal/modules/cash/...` (vùng rủi ro cao: cả bộ cash),
  `go test -race -run 'CodAdjust|ReconcileFindings|ErrorCodes|M3' ./internal/httpapi/`, `./cmd/crm/...`,
  `./tests/arch/...` (qua cổng kiểm của task; có migration mới nên cổng chạy toàn bộ).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`schedules.Schedule`,
  `commands.CommandKind`, `commands.Handler`/`Looker`, `tenancy.WithTenant`, `db.TxWorker`, `queue.JobQueue.Enqueue`);
  trong cash: `Reverse`, `PostWithID`, `BalancesFor`, `LockShipmentCOD` (khóa tư vấn sổ của vận đơn),
  `ShipmentTransfers`/`ShipmentEntries`, `ShipmentExists`, `postings.Balances`, lệnh cảnh báo
  `remittedReturnedAlert`; mẫu đối chiếu `inventory.Reconcile` (lô theo khóa, ghi một dòng lần chạy, lệnh cảnh báo
  trong cùng tx); mẫu lịch bằng lệnh `fulfillment` `pollCommand` (payload kỳ, `WithTenant` + `db.TxWorker`); httpapi
  `refusableWrite`, `catalogRead`, `h.audit`, `staffNames`, `shipmentCodOut` (thân `CodTransfer`, số dư), `optPtr`.
- Dùng lại / viết mới vì: `Reverse` cho chế độ đảo, `PostWithID` (kind `adjustment`) cho chuyển ngăn — không viết đường
  ghi sổ mới. Viết mới `reconcile.Target` (bảng đích theo trạng thái, kế hoạch yêu cầu) và lần đối chiếu (khác
  `inventory.Reconcile`: khác bảng, khác phép so, ghi phát hiện từng dòng). Lịch làm bằng lệnh (`commands.Handler`)
  như `fulfillment.poll` vì `Module` của cash chỉ gom lệnh, không gom `queue.Handler` (không sửa `wire.go`). Gói
  `testkit/codtest` gom bộ dựng dữ liệu thử của hai gói test mới (tránh chép hai lần, jscpd); không gộp với bộ dựng của
  `statement` (test của task khác, ghi sổ qua consumer; ở đây ghi sổ bằng `cash.Post` trực tiếp để dựng đủ trạng thái).

## Soát thiết kế

5.1 Chủ dữ liệu: cash (sổ COD, lần đối chiếu, phát hiện) — bảng có sẵn ở schema `cash`. 5.2 Không ghi bảng module
khác (chỉ đọc vận đơn). 5.3 Đọc vận đơn qua `fulfillment.shipments_v`. 5.4 Điều chỉnh: khóa, đọc lại, kiểm token,
ghi bút toán, audit trong một `WriteTx`. 5.5 Cảnh báo đối chiếu là lệnh `commands` ghi trong tx của lần chạy, gửi sau
commit. 5.6 Gửi Telegram chỉ qua lệnh (Alerter chống lặp theo key, not_sent tự gửi lại). 5.7 Mọi câu trong
`WithTenant`; cột, unique mới ghép `tenant_id`. 5.8 Module không nhập HTTP; handler mỏng. 5.9 Hai POST có
`IdempotencyKey`, `x-permission: cash.adjust`; GET `cash.cod.read`. 5.10 không áp dụng: không dùng Valkey. 5.11 Không
thêm thư viện. 5.12 Theo module mẫu (sentinel trong module, đổi HTTP ở errors.go, test PostgreSQL thật).

6.1 Hai điều chỉnh cùng lúc cho một vận đơn: `CreateCODAdjustment` lấy `LockShipmentCOD` (khóa tư vấn mà consumer sổ
và lô ghi bảng kê cũng lấy) trước khi đọc số dư; token tính lại sau khóa nên người thứ hai nhận 409 `preview_stale`.
Khóa chống trùng của bút toán `adjustment:<token>` (token đã băm mã vận đơn) chặn ghi đôi. Lần đối chiếu: unique `(tenant_id, slot)`,
chèn `ON CONFLICT DO NOTHING`; trùng kỳ thì không ghi phát hiện. 6.2 Middleware chống trùng; lệnh lịch có `Lookup` theo
kỳ (đã có lần chạy → done). 6.3 Một khóa tư vấn mỗi transaction điều chỉnh (ADR-46 mục advisory, trước mọi khóa dòng
của nhóm 9). Đối chiếu không khóa (chỉ đọc + chèn). 6.4 `WriteTx`/`WithTenant` chạy lại khi lỗi tạm; thân hàm gán lại
mọi biến. 6.5 READ COMMITTED: tổng sổ, số dư và trạng thái mỗi lô đọc trong một câu; vận đơn đổi trạng thái trong 15
phút trước lần chạy bỏ qua (`skipped`, sự kiện có thể chưa vào sổ). 6.6 Lệnh lịch lỗi database → not_sent, chạy lại
trong kỳ (hết hạn sau 24 giờ). 6.7 Telegram 429 chờ theo Retry-After như lệnh cảnh báo có sẵn. 6.8 Tiến trình chết giữa
chừng: transaction lùi, lệnh in_doubt → Lookup theo kỳ. 6.9 Câu đối chiếu theo lô 2.000 dòng (dưới 30 s). 6.10 `int64`
đồng; chỉ ghi thêm (đảo bằng bút toán mới, không xóa); database kiểm cân hai vế. 6.11 Test rapid: `Target` khớp số dư
mà luật ghi sổ `postings.Rules` sinh ra cho các chuỗi sự kiện; điều chỉnh chuyển ngăn giữ tổng = 0. 6.12 Cảnh báo chỉ
mang số lượng và ngày; phát hiện chỉ mang mã, số tiền, trạng thái; audit không ghi lý do (chữ tự do). 6.13
`cash.adjust` (owner, accountant), `cash.cod.read`; kiểm ở API.

## Quyết định thay chủ shop

1. **Lịch đối chiếu là lệnh (`commands.Handler`), không phải `queue.Handler`.** Vì `Module` của cash chỉ gom lệnh và
   không được sửa `wire.go`; làm như `fulfillment.poll`. Lệnh có `Lookup` theo kỳ; một lần chạy mỗi (tenant, kỳ) nhờ
   unique `(tenant_id, slot)` thêm ở 0321. Phương án khác: thêm gom `queue.Handler` vào wire (sửa tệp khung).
2. **Bỏ qua vận đơn vừa đổi trạng thái trong 15 phút** (`skipped`, lý do `recent_status`) — thêm ngoài danh sách bỏ qua
   của kế hoạch. Vì sự kiện đổi trạng thái vào sổ bất đồng bộ (consumer), lần chạy 02:30 có thể đọc vận đơn đã đổi mà
   sổ chưa kịp ghi → báo lệch oan. Đảo ngược: bỏ một nhánh trong `shipmentFinding`.
3. **"remitted_then_returned còn mở"** = có phát hiện đó mà chưa có điều chỉnh của nhân viên (`adjustment`/`reversal`,
   tác nhân staff) ghi sau phát hiện (bảng phát hiện không có trạng thái đóng). **"adjustment sau lần chốt gần nhất"** =
   có điều chỉnh của nhân viên ghi sau lúc chốt muộn nhất của bảng kê có dòng của vận đơn; chưa có bảng kê nào → mọi
   điều chỉnh. Phương án khác: thêm cột trạng thái cho phát hiện (đổi bảng, ngoài phạm vi).
4. **Đảo `expected` chỉ khi vận đơn đã kết thúc** (`delivered`, `returned`, `cancelled`); trạng thái khác → 409
   `invalid_transition` (ghi chú Task 120: khóa `expected:<vận đơn>` cố định, đảo xong sự kiện hãng không ghi lại được).
5. **Lý do tối đa 500 ký tự** (CHECK của bảng) dù hợp đồng cho `maxLength: 1000`: quá 500 → 422 `invalid_input`. Sửa
   kèm `Transfer.validate` đếm ký tự thay vì byte (trước đó lý do tiếng Việt 200 ký tự có thể bị từ chối thành lỗi 500).
   Không sửa hợp đồng (thu hẹp là thay đổi phá vỡ, cần chủ shop).
6a. **Lý do rỗng hẳn** (`""`) bị ogen chặn theo `minLength: 1` của hợp đồng → 400 `invalid_input`; lý do chỉ khoảng
   trắng → 422 (kế hoạch ghi "thiếu lý do → 422").
6. **Ghi lại sau khi sổ đã đổi:** lúc ghi, bút toán cần đảo đã bị đảo / không còn → 409 `preview_stale` (bọc cả lỗi gốc),
   vì cùng yêu cầu trên cùng sổ thì xem trước đã từ chối. Xem trước vẫn trả lỗi gốc.
7. **`skipped` không tính là lệch:** `reconcile_runs.findings` và `ok` chỉ tính ba loại lệch; cảnh báo chỉ khi có lệch.
   Khóa cảnh báo `cod-reconcile:<ngày giờ Việt Nam của kỳ>` đúng như kế hoạch (không gồm tenant; một shop).
8. **`Target` với `partial`:** vận đơn giao một phần rồi chuyển `returning`/`returned` giữ phần đã thu như `delivered`
   (luật ghi sổ chỉ trả phần còn ở `to_collect`); không giao một phần thì mọi ngăn 0 như kế hoạch. Test rapid đối chiếu
   `Target` với `postings.Rules`.
9. **Test TI03 chèn một bút toán một dòng** `to_collect +1000` (unique `(transfer, account)` không cho thêm dòng
   `to_collect` vào bút toán expected có sẵn); lần chạy vì vậy có thêm `transfer_unbalanced` (đúng: bút toán lệch).
11. **Vận đơn không COD** (`cod = 0`) ở `delivered`/giao một phần: đích là mọi ngăn 0 dù hãng báo đã thu (luật ghi sổ bỏ
    qua vận đơn không COD) — test rapid `TestTarget_MatchesPostingRules` tìm ra ca này.
10. **Audit** `cash.cod.adjust` trên `cash.cod_transfer` (bút toán vừa ghi, như quy ước `cash.<thực thể>` của các
    audit cash), ghi chế độ, vận đơn, bút toán bị đảo, các vế; không ghi lý do (chữ tự do
    có thể chứa dữ liệu cá nhân).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test điều chỉnh đỏ trước khi có mã | `TestAdjust_*` (thân hàm trả "chưa làm") | `go test -race -count=1 ./internal/modules/cash/internal/adjust/` | `FAIL … xem trước chưa làm, mong cash: …` |
| TI08: xem trước đảo remitted → after `{0, 630000, 0, 0, −630000}`, cảnh báo "thuộc bảng kê đã chốt ngày 19/11/2026"; ghi với token → reversal, bút toán cũ còn 3 dòng; ghi lại token cũ → stale; đảo lần hai, đảo bút toán đảo → blocked | `TestAdjust_TI08ReverseRemitted` | `go test -race -count=1 ./internal/modules/cash/internal/adjust/` | `ok … adjust 2.156s` |
| Token cũ sau khi có bút toán khác → `ErrPreviewStale`, không ghi; chuyển ngăn hai vế; cảnh báo ngăn âm; tổng sổ 0 | `TestAdjust_StaleTokenAndMove` | như trên | `ok` |
| Thiếu lý do → `ErrAdjustReasonRequired`; 8 ca yêu cầu sai → `ErrAdjustmentInvalid`; bút toán vận đơn khác → 404; vận đơn không có; expected chưa kết thúc → blocked; lý do 500 ký tự tiếng Việt ghi được | `TestAdjust_Refusals` | như trên | `ok` |
| 4 lần ghi song song cùng token → 1 ghi, 3 stale | `TestAdjust_ConcurrentSameToken` | như trên | `ok`; đột biến bỏ `LockShipmentCOD` → `FAIL` |
| Quy luật bản xem trước (cân, tổng giữ nguyên, vế đảo đúng dấu, token theo sổ) | `TestPreview_Properties` (rapid) | như trên | `ok` |
| TI03: 10 vận đơn đủ trạng thái, sổ đúng → ok, 0 phát hiện, 0 lệnh cảnh báo; chèn bút toán một dòng `to_collect +1000` cho V3 → `balance_mismatch` V3, `ledger_nonzero` 1000, `transfer_unbalanced`; một cảnh báo `cod-reconcile:2026-11-22` "3 phát hiện", không mã đơn, mã vận đơn | `TestNightly_TI03` | `go test -race -count=1 ./internal/modules/cash/internal/reconcile/` | `ok … reconcile 3.269s`; đột biến `total > 1000` → `FAIL` |
| Vận đơn có `remitted_then_returned` mở → không `balance_mismatch`, `skipped`; vừa đổi trạng thái → `skipped recent_status`; run ok | `TestNightly_SkipsOpenReturnedFinding` | như trên | `ok`; đột biến bỏ cờ → `FAIL` |
| Điều chỉnh sau lần chốt → `skipped adjusted` | `TestNightly_SkipsAdjustedSinceConfirm` | như trên | `ok` |
| Lịch chạy hai lần (4 lần song song) cùng kỳ → một `reconcile_runs`; Lookup done / not_sent; payload hỏng → rejected | `TestNightly_OneRunPerSlot` | như trên | `ok` |
| Lịch 86.400 s theo tenant, dòng lịch 19:30 UTC; chưa chạy → trang rỗng; run lạ → `ErrReconcileRunNotFound` | `TestNightly_ScheduleAndEmptyList` | như trên | `ok` |
| Bảng đích khớp luật ghi sổ cho mọi chuỗi sự kiện; 11 ca biên | `TestTarget_MatchesPostingRules` (rapid), `TestTarget_Table` | như trên | `ok` |
| HTTP: xem trước 200, ghi 201 + audit `cash.cod_transfer` không lý do, token cũ 409 `preview_stale`, lý do trống 422, bút toán vận đơn khác 404, expected đang giao 409, Kho 403 | `TestCodAdjustAPI` | `go test -race -count=1 -run 'TestCodAdjustAPI\|TestReconcileFindingsAPI\|ErrorCodes\|M3' ./internal/httpapi/` | `ok … httpapi 16.409s` |
| HTTP: chưa chạy → rỗng; sau chạy runId, phát hiện detail chuỗi; runId lạ 404; con trỏ sai 400; Kho 403; mã lỗi khai trong hợp đồng; danh sách 501 | `TestReconcileFindingsAPI`, `TestErrorCodes_*`, `TestM3Routes_*` | như trên | `ok` |
| Cả bộ cash (vùng rủi ro cao) | mọi test `./internal/modules/cash/...` | `go test -race -count=1 ./internal/modules/cash/...` | `ok` 6 gói có test (cash 39.6s, statement 83.7s) |
| Nối lệnh cảnh báo, lịch trong cmd/crm | `./cmd/crm` | `go test -race -count=1 ./cmd/crm/` | `ok … 1.607s` |
| Lệnh cảnh báo: payload hỏng → rejected bad_payload; không Alerter → rejected alerter_missing; gửi lỗi → not_sent alert_failed; Telegram 429 → RetryAfter 7 s; gửi được → done, key `cod-remitted-returned:<mã>` / `cod-reconcile:<ngày>` | `TestAlertCommands_Outcomes` | `go test -count=1 -run TestAlertCommands ./internal/modules/cash/` | `ok` trên bản cũ (HEAD) của `area_postings.go` và trên bản tách |
| Đích khi COD = 0 mà hãng báo đã thu (rapid tìm ra, đã sửa) | `TestTarget_MatchesPostingRules` | `go test -race -count=1 ./internal/modules/cash/internal/reconcile/ -rapid.checks=3000` | `ok … 2.520s` |
| Không đoạn trùng mới | — | `jscpd --config .jscpd.json --similarity --baseline jscpd-baseline.json --fail-on-new-clones=0 .` | không còn `NEW` (lần đầu cổng báo 6 đoạn, đã gỡ) |
| Lint | — | `golangci-lint run ./internal/modules/cash/... ./internal/httpapi/ ./cmd/crm/` | `0 issues.` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `queries/cash/adjust.sql` (hai câu đọc của điều chỉnh) ngoài `reconcile.sql` của kế hoạch.
- Commit cấu trúc riêng (commit 1): tách `notifyOutcome` khỏi `remittedReturnedAlert.Execute` (`area_postings.go`, Task
  M3-29) để lệnh cảnh báo đối chiếu dùng chung (jscpd báo trùng 15 dòng), kèm test `TestAlertCommands_Outcomes` (các nhánh
  rejected/not_sent/429/done chưa có test) — test qua trên cả bản cũ lẫn bản đã tách.
- Thêm `internal/testkit/codtest` (bộ dựng dữ liệu thử dùng chung cho hai gói test; nằm trong testkit vì test kiến trúc
  dynsql miễn kiểm testkit, như `shiptest`), `adjust/preview_test.go`,
  `reconcile/targets_test.go` (rapid), `internal/httpapi/handlers_cod_adjust_test.go`.
- `adjust` là luật thuần (không database); đọc sổ, khóa, ghi nằm ở `cash/area_adjustments.go` (gói con không nhập được
  `cash.Reverse`/`PostWithID`). `apply.go` giữ phần so token và khóa chống trùng.
- Sửa `cash/cod.go` (`Transfer.validate`: độ dài lý do theo ký tự), `cmd/crm/main.go` (một dòng đăng ký lệnh cảnh báo),
  `internal/httpapi/errors.go` (ánh xạ lỗi mới), `tests/arch/testdata/m3_pending.txt` (bỏ ba operation).
- Mã đối chiếu (`reconcile/*.go`) viết trước test của nó (không đúng thứ tự test trước); bù bằng đột biến tay: bỏ cờ
  bỏ qua remitted_then_returned, đổi điều kiện tổng sổ, bỏ khóa sổ khi ghi điều chỉnh — cả ba làm test đỏ (bảng bằng
  chứng). Điều chỉnh (`adjust`, `area_adjustments.go`) đi đúng thứ tự: test đỏ trước (thân hàm trả "chưa làm").

## Việc phát hiện thêm

- Test chập chờn có sẵn, ngoài phạm vi: `customer.TestStatsConcurrentWithMerge` panic `index out of range [0] with
  length 0` ở `internal/modules/customer/stats.go:108` (`locked[0].TenantID` khi không khóa được dòng nào) trong lần
  chạy cổng kiểm thứ hai (máy đang tải nặng); chạy lại gói `customer` hai lần (`go test -race -count=2`) đều `ok`.
- Commit 1 (cấu trúc) để lọt một lỗi lint `gocognit` ở `TestAlertCommands_Outcomes` (độ phức tạp 21 > 20; tôi chưa chạy
  lint trước khi commit cấu trúc); sửa ở commit 2 (tách `checkAlertCase`, mỗi lệnh một subtest).
- Hợp đồng `CodAdjustmentRequest.reason` khai `maxLength: 1000` nhưng bảng chỉ nhận 500 ký tự (quyết định 5): cần một
  task thu hẹp hợp đồng có OK của chủ shop.

## Reviewer

Kết luận: ĐẠT (cả hai commit).

- Commit 1 (97e2e40, cấu trúc: tách `notifyOutcome`, test `TestAlertCommands_Outcomes`) — ĐẠT, không phát hiện phải
  sửa: hành vi từng nhánh giữ nguyên; build được trên HEAD. Ghi chú không bắt buộc (test chưa kiểm severity, Reason ở
  ca 429) đã làm ở commit 2.
- Commit 2 (tính năng) — ĐẠT, không phát hiện phải sửa. Reviewer xác nhận: khóa sổ trước khi đọc, token tính lại sau
  khóa, `Reverse` cùng transaction; từ chối thì transaction lùi; một lần chạy mỗi kỳ đúng dưới READ COMMITTED, `res` gán
  lại khi chạy lại; chỉ ghi thêm; RLS; không dữ liệu cá nhân trong cảnh báo, phát hiện, audit; expected chỉ đảo khi vận
  đơn kết thúc; mọi dòng bằng chứng khớp test thật. Ghi chú không bắt buộc: (1) câu chữ khóa chống trùng ở 6.1 — đã sửa;
  (2) lệnh đối chiếu lỗi trả not_sent không log nguyên nhân (giống `fulfillment.poll`; kết quả lệnh ghi Reason
  `reconcile_failed`) — giữ; (3) test TI03 `ALTER TABLE … DISABLE TRIGGER` trên database riêng của test — giữ; (4) vận
  đơn có điều chỉnh mà chưa có bảng kê chốt bị bỏ qua tới khi có bảng kê chốt (quyết định 3) — chủ shop nên biết.
