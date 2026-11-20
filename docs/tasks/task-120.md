# Task 120 — M3-29: Ghi sổ COD theo sự kiện vận đơn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 120 = M3-29). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `cash.Consumer.Handle` giữ chữ ký, đổi thân (luật theo `effects`, gói mới `cash/internal/postings`); chỗ dùng `cash.Consumer{}`: `wire.go` (Consumers), `cmd/crm/wire_m3.go` (qua `Module.Consumers`), `tests/e2e/stack_test.go:116`, `cash/env_test.go`, `cash/cod_test.go` — không đổi chỗ gọi. Hằng `cash.Account*`, `cash.Kind*` giữ tên và giá trị. Thêm mới: `cash.ShipmentCOD`, `cash.CODTransfer`, `(*Service).ShipmentCOD`, `cash.ErrShipmentNotFound`, `cash.RemittedReturnedAlertKind`, `cash.NewRemittedReturnedAlertCommand`, `cash.FindingRemittedThenReturned`, `cash.FindingUnexpectedCancelAfterPickup`; hằng `cash.Account*`, `Kind{Expected,Collected,Remitted,Returned}` thành bí danh của hằng trong `postings` (cùng giá trị). Vùng postings (`area_postings.go`) chứa lệnh cảnh báo và `Service.ShipmentCOD`; các hàm khung vẫn trả nil. Bỏ hàm không xuất `entries` (consumer.go) — thay bằng `postings.Rules`.
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `tests/e2e` (test); các gói con `cash/internal/*` không đổi.
- Operation OpenAPI bị chạm, oasdiff: `getShipmentCod` (GET /api/v1/shipments/{id}/cod, `cash.cod.read`) — hợp đồng đã có từ Task 95, không sửa `openapi.yaml`; thay thân 501 trong `handlers_cod_balances.go`, bỏ dòng `getShipmentCod` khỏi `tests/arch/testdata/m3_pending.txt`. oasdiff: không đổi.
- Chỗ dùng trên màn hình: chưa có (Task 134 dùng sau); `console/src/api` không đổi.
- Câu sqlc, bảng, pha migration: `queries/cash/cod.sql` — `ShipmentCOD` trả thêm `code` (mã vận đơn nội bộ cho cảnh báo); `TransferKinds` (M1, chỉ consumer dùng) bỏ, thay bằng `ShipmentTransfers` (id, loại, khóa, đã bị đảo chưa); thêm `LockShipmentCOD` (khóa advisory theo vận đơn), `ShipmentEntries`, `InsertEventFinding`; `queries/cash/ledger.sql` thêm `ShipmentTransferList`, `ShipmentExists` cho đường đọc. `make gen` (sqlc) sinh lại `cash/internal/store/{cod,ledger}.sql.go` và `models.go` của mọi gói store (kiểu `CashReconcileFinding` đổi theo migration: `RunID` thành `pgtype.UUID`, thêm `SourceKey`; không chỗ nào dùng). Migration mới `0314_cash_event_findings.sql` (pha mở rộng): `reconcile_findings.run_id` DROP NOT NULL (nới), thêm cột `source_key text` cho phép rỗng + unique `(tenant_id, source_key)`. Bảng chạm: `cash.cod_transfers`, `cash.cod_ledger_entries`, `cash.reconcile_findings` (chèn), `platform.jobs` (lệnh cảnh báo); đọc `fulfillment.shipments_v`.
- Tệp khác: `cmd/crm/main.go` (thêm một dòng `cash.NewRemittedReturnedAlertCommand(alerter)` vào danh sách lệnh cảnh báo dùng chung Alerter của worker — Alerter không có trong `cash.Deps` của wireM3), `internal/httpapi/errors.go` (một dòng `cash.ErrShipmentNotFound` → 404), `docs/adr/0046-thu-tu-khoa.md` (khóa advisory mới), `cash/cod_test.go` (khóa expected mới, xem mục Test bị xóa hoặc tắt).
- Test sẽ chạy: `go test -race ./internal/modules/cash/...` (vùng rủi ro cao: cả bộ), `./internal/modules/fulfillment/...` (Step 4 của kế hoạch), `./internal/httpapi/ -run 'M3|Cod'`, `./tests/arch/...`; cuối cùng cổng kiểm của task (ci-task.sh).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục cash, commands, queue, observability; `cash.PostWithID`, `cash.Reverse`, `cash.BalancesFor` (Task 93); mẫu lệnh cảnh báo `inventory/shortagealert.go`, `fulfillment/accounts_check.go` (`commands.CommandKind` + `Enqueue` trong tx, `Notify` ngoài tx); khóa advisory theo mẫu `queries/identity/roles.sql`; bộ sinh hành trình `trackJourney` của `fulfillment/apply_property_test.go` (gói test khác, không nhập được).
- Dùng lại / viết mới vì: dùng lại `PostWithID`, `Reverse`, `BalancesFor`, `commands.CommandKind`, `queue.NewQueue().Enqueue`, `shiptest`, `codEnv` của `cash_test`. Viết mới `postings.Rules` (hàm thuần: chọn bút toán theo tác động và số dư — riêng của sổ COD). Hàm `notify` của inventory không xuất và thuộc module đó, nên lệnh cảnh báo của cash viết riêng (~30 dòng; cùng dạng nhưng khác module, khác lý do thay đổi). Bộ sinh hành trình chép lại dạng rút gọn trong test của cash, thêm đảo "Thành công" và giao một phần.
- jscpd (lần chạy cổng kiểm đầu báo 7 đoạn trùng mới): đã sửa hết bằng cách tách hàm, không đổi mốc nền — `v1()` trong `postings_test.go` (bước đầu lặp), `carrierEvent`/`movingCanonicals` (bộ sinh hành trình giống `fulfillment` trackJourney: gói test khác, không nhập được nên giữ bản riêng, viết lại dạng khác), `statusEvents` dùng `pgx.RowToStructByPos`, `decodeRemittedReturned` (lệnh cảnh báo cùng dạng với `fulfillment` authAlert nhưng khác module), tạo vận đơn trong test HTTP gộp trong một tx chủ bảng.

## Soát thiết kế

- 5.1–5.3: bút toán, phát hiện thuộc `cash`; đọc COD, mã vận đơn qua view `fulfillment.shipments_v`; không ghi bảng module khác.
- 5.4: bút toán, phát hiện, lệnh cảnh báo ghi trong cùng transaction của việc `event.cash`.
- 5.5–5.6: cảnh báo là lệnh `commands` ghi trong tx, gửi ngoài tx; not_sent → tự gửi lại (Alerter chống lặp theo key).
- 5.7: mọi câu trong tx tenant của worker; không bảng mới, cột mới trên bảng đã bật RLS.
- 5.8: module không nhập net/http; handler mỏng gọi `Service.ShipmentCOD`.
- 5.9: không có đường ghi HTTP mới; ghi chỉ ở consumer — chống trùng bằng khóa bút toán (`expected:<s>`, `collected:<s>:<seq>`, `returned:<s>:<seq>`, `reversal:<t>`), `source_key` của phát hiện, `DedupeKey` của lệnh cảnh báo.
- 5.10–5.11: không Valkey, không thư viện mới. 5.12: theo mẫu Task 28/93 (sổ chỉ ghi thêm, test rapid).
- 6.1/6.3: consumer chạy thứ tự nghiêm theo vận đơn; thêm khóa advisory `cash.cod:<shipment>` trong tx trước khi đọc số dư, để đường ghi khác của cash chọn bút toán theo số dư (chốt bảng kê Task 123, điều chỉnh Task 124) không xen giữa đọc và ghi; ghi vào ADR-46 mục advisory. Bút toán chỉ chèn, không khóa dòng.
- 6.2: giao lại sự kiện (VC08) → cùng khóa bút toán → không ghi thêm; sự kiện khác cùng loại (kể cả bút toán do M1 ghi với khóa `evt:`) → kiểm "đã có bút toán cùng loại chưa bị đảo".
- 6.6/6.8: payload hỏng, vận đơn không có, `codCollected` âm → `queue.ErrPermanent` (dead, chặn hàng của vận đơn nhờ BlockOnDead — không đoán tiền); lỗi khác → thử lại; worker chết giữa chừng → tx lùi, chạy lại cho cùng kết quả (test rapid).
- 6.10: `int64`; sổ chỉ ghi thêm, hai vế; mọi bút toán qua `PostWithID`/`Reverse`.
- 6.11: rapid 200 lượt: hành trình ngẫu nhiên qua `ApplyTracking`, lặp và chết giữa chừng → tổng 0, số dư khớp bảng đích.
- 6.12: phát hiện, cảnh báo chỉ mang mã vận đơn nội bộ, số tiền; không mã đơn, SĐT.
- 6.13: `getShipmentCod` quyền `cash.cod.read` (ScopeNone); vận đơn không có / tenant khác → 404.
- 6.4, 6.5, 6.7, 6.9: không áp dụng riêng (mặc định của worker, không gọi ngoài trong tx).

## Bằng chứng

Lệnh chung: `go test -race ./internal/modules/cash/... ./internal/modules/fulfillment/...` → `ok cash 41.158s`, `ok cash/internal/postings 1.474s`, `ok fulfillment 47.260s` (sau khi gộp main). HTTP: `go test -race ./internal/httpapi/ -run TestShipmentCodAPI` → `ok`. Đột biến kiểm test thuộc tính: tắt đảo collected → FAIL "vận đơn returning … muốn {0 0 0 0 0}"; bỏ qua codCollected → FAIL "vận đơn delivered … muốn CarrierHolding:94756"; đã khôi phục (cmp giống bản trước).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TI01: lấy hàng {630000,0,0,0,−630000}; giao xong {0,630000,0,0,−630000}; shop_received 0 | `cash_test.TestPostings_TI01PickupThenDelivered` (hai người bán) | `go test -race ./internal/modules/cash/ -run TestPostings` | ok |
| VC08 (tiền): giao lại bước giao xong (cả sau lần chết trước commit) → một collected | `TestPostings_VC08DeliveredReplayed` | như trên | ok |
| Hoàn trước khi giao → {0,0,0,0,0} (Q-10), kể cả lấy hàng + hoàn cùng bước | `TestPostings_ReturnBeforeDelivery` | như trên | ok |
| Giao một phần thu 430.000 → {0,430000,0,0,−430000} | `TestPostings_PartialDelivery` | như trên | ok |
| Đảo khi chưa có tiền về → {0,0,0,0,0}, một reversal trỏ collected; giao lại không đảo thêm | `TestPostings_ReversalBeforeRemit` | như trên | ok |
| Đảo khi shop_received 598.000 → số dư không đổi, một finding remitted_then_returned (run_id rỗng), một lệnh cảnh báo; văn bản chỉ mã vận đơn nội bộ, không mã đơn, SĐT; không Alerter → rejected | `TestPostings_ReversalAfterRemit` | như trên | ok |
| COD 0: lấy hàng, giao xong (và đảo) → không bút toán | `TestPostings_ZeroCOD` | như trên | ok |
| Hủy sau expected → finding unexpected_cancel_after_pickup, không ghi, không cảnh báo | `TestPostings_CancelAfterPickup` | như trên | ok |
| Bút toán M1 (khóa evt:) không bị ghi lặp; codCollected âm → ErrPermanent, không ghi | `TestPostings_M1KeysAndBadCollected` | như trên | ok |
| Thuộc tính (rapid, số lượt của gói 300 ≥ 200): hành trình ngẫu nhiên qua ApplyTracking (thất bại, giao lại, hoàn, giao một phần, đảo, hủy), chết trước commit, giao lại, giao lại sự kiện cũ → tổng 0, số dư khớp bảng đích theo trạng thái | `TestPostings_PropertyApplyTrackingJourneys` | `go test -race ./internal/modules/cash/ -run TestPostings_Property` | ok 27.0s |
| Luật thuần: 16 ca bảng; đầu vào sai → ErrInvalidInput; thuộc tính cân hai vế, áp lại không sinh việc mới | `postings_test.TestRules_Table`, `TestRules_InvalidInput`, `TestRules_PropertyBalancedAndStable` | `go test -race ./internal/modules/cash/internal/postings/` | ok |
| getShipmentCod: số dư, bút toán theo thứ tự (vế, lý do, tác nhân hãng, reversesTransferId); chưa có sổ → rỗng; không có → 404; Kho → 403 | `cash_test.TestPostings_ShipmentCODRead`, `httpapi.TestShipmentCodAPI` | `go test -race ./internal/httpapi/ -run TestShipmentCodAPI` | ok |
| Consumer M1 giữ tên, topic, chạy qua sổ sự kiện và worker | `TestConsumerThroughRegistryAndWorker`, `TestCODLedgerProperty` (M1, không đổi) | `go test -race ./internal/modules/cash/` | ok |

## Test bị xóa hoặc tắt

- `cash/cod_test.go` `TestConsumerIsIdempotent`: assertion khóa chống trùng đổi từ `evt:<id sự kiện>:expected` (M1) sang `expected:<vận đơn>` — kế hoạch Task 120 đặt khóa mới; số bút toán vẫn kiểm đúng như cũ (không nới).

## Khác kế hoạch

- Test trên database đặt ở `internal/modules/cash/{postings_test.go,postings_property_test.go}` (gói `cash_test`, dùng lại `codEnv`) thay vì `internal/postings/{consumer_test.go,property_test.go}`; gói `postings` có test thuần `rules_test.go` (bảng + rapid). Lý do: dựng đơn, vận đơn thật cần `codEnv` của gói `cash_test`, không nhập được từ gói khác.
- `getShipmentCod` không cần sửa `openapi.yaml` (hợp đồng có từ Task 95).

## Quyết định thay chủ shop

1. **Phát hiện theo sự kiện không có lần đối chiếu:** bảng `cash.reconcile_findings` của 0303 bắt buộc `run_id`. Chọn migration 0314 nới `run_id` cho phép rỗng + cột `source_key` unique để chống trùng khi sự kiện giao lại. Phương án khác: tạo một dòng `reconcile_runs` giả cho mỗi phát hiện (làm sai số lần chạy đêm mà Task 124 kiểm "một lần chạy mỗi ngày"). Đảo ngược được: cột thêm, nới ràng buộc.
2. **Cảnh báo `cod-remitted-returned`:** gửi bằng lệnh `commands` ghi trong tx (như cảnh báo kho, khóa API hãng), lệnh đăng ký ở `cmd/crm/main.go` cùng Alerter của worker; khóa và văn bản dùng mã vận đơn nội bộ `shipments.code` (không mã đơn, SĐT).
3. **Hãng báo thu nhiều hơn COD (C > X):** ghi collected to_collect −X, carrier_holding +C, sales_clearing −(C−X) — sổ khớp số hãng báo đang giữ và bảng đích của Task 124 (`sales_clearing = −codCollected`); bảng kê đối chiếu sau. Phương án khác: chặn (dead) hay ghi X (lệch với tiền hãng giữ).
4. **"Đã có tiền về" khi đảo:** coi là đã có khi `shop_received ≠ 0` **hoặc** có bút toán remitted chưa bị đảo (kế hoạch ghi `shop_received > 0`); thận trọng hơn khi phí hãng trừ hết tiền về.
5. **Hãng báo đã thu 0 khi giao xong:** không ghi collected; toàn bộ X ghi returned. `codCollected` âm → lỗi vĩnh viễn (dead, chặn hàng của vận đơn) — không đoán tiền.
6. **Khóa advisory `cash.cod:<vận đơn>`** trước khi đọc số dư (ghi ở ADR-46); chốt bảng kê (Task 123) và điều chỉnh (Task 124) phải lấy cùng khóa.
7. Số migration 0314 (0310–0313 để trống cho các task song song; main đã có 0312).

## Việc phát hiện thêm

- Task 123, 124: lấy `LockShipmentCOD` trước khi đọc số dư để chọn bút toán (ADR-46 đã ghi); Task 124 nên chặn hay cảnh báo khi đảo `expected`/`collected` của vận đơn chưa kết thúc: khóa `expected:<vận đơn>` cố định nên sau khi nhân viên đảo expected, sự kiện hãng sau đó không ghi lại expected.
- Task 124 `targets.go` có thể dùng lại bảng đích của `targetBalances` trong `cash/postings_property_test.go`.

## Reviewer

Kết luận: ĐẠT (agent reviewer, lần 1, trên bản đã gộp main a069d18). Phát hiện phải sửa: không có. Ảnh hưởng dự kiến khớp thực tế (hàm `entries`, câu `TransferKinds` cũ đã xóa, không còn chỗ dùng; mã sinh khớp SQL; `CashReconcileFinding` không có mã tay dùng). Bảng bằng chứng: mọi dòng trỏ tới test có thật và kiểm đúng tiêu chí. Ghi chú không bắt buộc: chưa có test riêng cho vận đơn của tenant khác → 404 (đúng trên mã nhờ view `security_invoker` + RLS); khóa cảnh báo dùng `shipments.code` (đã ghi ở Quyết định 2); `ShipmentTransfers` sắp theo id dựa trên `DEFAULT uuidv7()`.
