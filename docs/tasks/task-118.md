# Task 118 — Đảo Thành công — hãng sửa lại và nhân viên sửa nhầm (M3-27)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md`, mục "### Task 118 (M3-27)". Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ THÊM ở `fulfillment` — `CorrectionInput`, `CorrectionPreview`, `Service.PreviewCorrection`, `Service.ApplyCorrection`, `ErrCorrectionInvalid`, `ErrCorrectionNotAllowed`, `ErrCorrectionBlocked`, `ErrCorrectionStale`, `CorrectedAlertKind`, `NewCorrectedAlertCommand`. Không đổi chữ ký nào đã có (0 chỗ tham chiếu bị ảnh hưởng). Hành vi đổi: `SetManualStatus` (6 chỗ gọi: handler `handlers_shipment_manual.go` + test của fulfillment) chặn delivered → returning khi tiền đã về shop (QĐ-118-2); tác động `EffDeliveryReversed` (`apply_effects.go`) ghi lý do `staff_corrected` khi nguồn là nhân viên (QĐ-118-3) và ghi lệnh cảnh báo khi nguồn là hãng. Dọn mã lặp (jscpd): `notifyNormal` mới trong `accounts_check.go` dùng chung cho `authAlert` và `correctedAlert` (hành vi giữ nguyên); helper test `manualShipping` dùng ở `partial_flow_test.go`; helper e2e `awaitingTracking` dùng ở `mot_don_di_tron_test.go`. `tests/arch/sqlusage_test.go`: ngoại lệ đọc view (QĐ-118-6). `httpapi`: const `codePreviewStale` dời từ `errors_cash.go` sang `errors.go` (2 chỗ dùng: cash, fulfillment).
- Gói phụ thuộc ngược (go list): nhập `fulfillment`: `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`, `internal/modules/cash`, các adapter hãng `internal/adapters/carrier/*`, `internal/testkit/{codtest,shiptest}`, `internal/wiring/webcache`; chỉ thêm ký hiệu nên chỉ build lại.
- Operation OpenAPI bị chạm, oasdiff: `previewShipmentCorrection`, `applyShipmentCorrection` (hợp đồng đã có từ Task M3-04, không sửa `openapi.yaml`; oasdiff không chênh); `tests/arch/testdata/m3_pending.txt` bỏ 2 dòng.
- Chỗ dùng trên màn hình: chưa có màn gọi hai operation (màn ở task sau); `console/src/screens/don/labels.ts` thêm 2 lý do `carrier_corrected`, `staff_corrected` cho dòng thời gian đơn ("Hãng sửa trạng thái từ Thành công sang Đang chuyển hoàn"), test `labels.test.ts`.
- Câu sqlc, bảng, pha migration: thêm `queries/fulfillment/correction.sql` (`CorrectionFacts :one` — đọc `fulfillment.shipments` của mình + `cash.shipment_cod_v` LEFT JOIN + `cash.cod_transfers_v`, chỉ đọc view của cash); không migration, không bảng mới; `docs/views.md` thêm `fulfillment` vào cột "ai đọc" của `cash.shipment_cod_v`, `cash.cod_transfers_v`.
- Test sẽ chạy: `go test -race -run '^TestCorrection' ./internal/modules/fulfillment/`, test thuần `./internal/modules/fulfillment/internal/correction/`, `TestManual_*` liên quan, `TestApply_DeliveryReversed*`, httpapi `TestErrorSnapshot*`, `TestShipmentCorrection*`; console vitest `labels.test.ts`; cả gói (vùng rủi ro cao fulfillment/order/inventory/cash) do ci-task. E2E `tests/e2e/m3_reverse_delivered_test.go` viết sẵn, chưa chạy.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (xem trước/token, định dạng tiền: không có hàm Go dùng chung định dạng "630.000 đ"); `cash` có xem trước điều chỉnh COD (token riêng của cash, nghiệp vụ khác, không nhập được vì fulfillment không nhập cash); `SetManualStatus` (khóa `staff:<uuid>`, sự kiện tổng hợp); `lockRun`/`ApplyTracking`; mẫu lệnh cảnh báo `authAlert`/`trackAlert` (`Notifier`); `shipstate.Decide`.
- Dùng lại / viết mới vì: dùng lại `ApplyTracking` (cùng máy trạng thái, tác động ReverseDelivery → OnShipmentReturning, sự kiện, báo hiệu), `lockRun` (khóa đơn → vận đơn ADR-46), `shipstate.Decide` để xem trước có áp được không (cùng luật với lúc áp: giao một phần, không phải vận đơn hiện hành, tin cũ), `Notifier` + mẫu lệnh cảnh báo. Viết mới: `internal/correction` (luật thuần: câu tác động, câu chặn, token sha256, định dạng tiền nhỏ — riêng của vùng, không đủ lý do làm gói platform), câu SQL `CorrectionFacts`.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 chủ dữ liệu: trạng thái vận đơn thuộc fulfillment; đơn, tồn đổi qua public API order (ReverseDelivery, OnShipmentReturning) như đã có; tiền thuộc cash (nghe shipment.status_changed).
- 5.2 không ghi bảng module khác: đúng, chỉ gọi order.Writer qua ApplyTracking.
- 5.3 đọc qua view: tiền đọc `cash.shipment_cod_v`, `cash.cod_transfers_v` (security_invoker, có GRANT SELECT crm_app); ghi `docs/views.md`.
- 5.4 nguyên tử: xem trước lại + áp + audit trong một WriteTx.
- 5.5 sau commit: tiền do consumer cash (sự kiện); cảnh báo `carrier-corrected:<mã>` là lệnh `commands` ghi trong transaction, gửi sau commit.
- 5.6 gọi ra ngoài: chỉ Alerter qua lệnh (AutoRetryNotSent như mẫu cảnh báo khác); không gọi hãng.
- 5.7 tenant: mọi câu trong WriteTx tenant; câu mới chạy dưới RLS (view security_invoker).
- 5.8 không HTTP trong module: handler mỏng ở `handlers_corrections.go`; lỗi đổi ở `errors_fulfillment.go`.
- 5.9 đường ghi: cả hai operation có `x-permission: fulfillment.correct` và `IdempotencyKey` (hợp đồng có sẵn).
- 5.10 Valkey: không áp dụng.
- 5.11 thư viện: không thêm (crypto/sha256 thư viện chuẩn).
- 5.12 module mẫu: theo area_partial/area_manual (khóa đơn → vận đơn, sentinel, audit ở lớp HTTP).
- 6.1 đồng thời: áp khóa đơn rồi vận đơn FOR UPDATE, tính lại xem trước dưới khóa; token chứa version vận đơn → hai lần áp cùng token: lần sau thấy version mới → 409 preview_stale. Test hai yêu cầu song song cùng token: đúng một lần qua.
- 6.2 gửi lại: Idempotency-Key ở middleware; lỗi nghiệp vụ không lưu (refusableWrite); khóa sự kiện `staff:<uuid>` mới mỗi lần.
- 6.3 thứ tự khóa: đơn (nhóm 7) → vận đơn (nhóm 8) → tồn (nhóm 11) như ApplyTracking; câu đọc view cash không khóa.
- 6.4 lỗi tạm: WriteTx (RetryTx) có sẵn; hàm trong tx gán lại mọi biến kết quả.
- 6.5 cách ly: READ COMMITTED + khóa dòng. Tiền về shop giữa xem trước và áp: cash không khóa vận đơn → chặn đọc lại dưới khóa lúc áp; cửa sổ còn lại (bảng kê chốt sau khi đọc trong tx áp) được cash xử lý: consumer gặp shop_received ≠ 0 → finding remitted_then_returned, không ghi tiền (đường an toàn có sẵn).
- 6.6 việc nền lỗi: lệnh cảnh báo theo mẫu (not_sent tự gửi lại, alerter_missing rejected).
- 6.7 429: Telegram 429 theo mẫu của Alerter (mẫu fulfillment không đọc RetryAfter; giữ như authAlert).
- 6.8 chết giữa chừng: transaction lùi; lệnh cảnh báo cùng transaction.
- 6.9 treo: giới hạn sẵn của WriteTx; lệnh cảnh báo timeout 10 s.
- 6.10 tiền: không ghi tiền ở fulfillment; sổ chỉ ghi thêm (test so số dòng, xmin trước/sau của stock_ledger, cod_ledger_entries).
- 6.11 quy luật: test sinh ngẫu nhiên (rapid) cho luật thuần token/câu (token đổi khi version/tác động đổi, ổn định khi không).
- 6.12 dữ liệu cá nhân: lý do qua `logging.ScrubText` trước khi vào audit, raw sự kiện; cảnh báo chỉ có mã vận đơn nội bộ; câu tác động chỉ SKU, số lượng, tiền.
- 6.13 quyền: `fulfillment.correct` (không theo kho, như giao một phần) do guard kiểm theo x-permission; test 403 khi thiếu quyền.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test viết trước, thất bại trước khi có mã | mọi test mới | `go test ./internal/modules/fulfillment/...` trước khi viết `correction.go` | FAIL biên dịch: `undefined: fulfillment.CorrectionInput`…; vitest `labels.test.ts` 1 failed |
| Câu tác động (3 câu cho D1 một dòng), chặn, token, định dạng tiền, quy luật token (rapid) | `TestDecide_TI08`, `TestDecide_NoMoneyManyLines`, `TestBlocking`, `TestVND`, `TestToken_Property` | `go test -count=1 ./internal/modules/fulfillment/internal/correction/` | `ok` |
| TI08: xem trước không đổi dữ liệu; áp với token → returning, đơn confirmed, tồn {5,0,1}/{10,0,2}, phiếu hoàn, lý do staff_corrected, sự kiện staff có lý do đã che SĐT, sổ chỉ thêm (xmin), không cảnh báo; áp lần hai → NotAllowed | `TestCorrection_TI08` | `go test -race -count=1 -run '^(TestCorrection_.*\|TestCorrectedAlertCommand)$' ./internal/modules/fulfillment/` | `ok … 3.325s` |
| Token cũ sau khi vận đơn đổi → ErrCorrectionStale, không đổi gì | `TestCorrection_StaleToken` | như trên | `ok` |
| Hai lần áp song song cùng token → đúng một lần qua, một phiếu hoàn | `TestCorrection_ConcurrentApply` | như trên | `ok` |
| Bảng kê chốt (shop_received 598.000) → Blocking, áp → ErrCorrectionBlocked; hãng sửa lại vẫn đảo, sổ chỉ thêm, lý do carrier_corrected, cảnh báo carrier-corrected:<mã>; remitted đã đảo → không chặn | `TestCorrection_BlockedByRemittance` | như trên | `ok` |
| Từ chối: chưa giao, giao một phần, lý do trống/dài, đích sai, thiếu token, vận đơn lạ | `TestCorrection_Refusals` | như trên | `ok` |
| QĐ-118-2: ghi tay returning khi tiền đã về → ErrCorrectionBlocked; chưa có tiền → được | `TestCorrection_ManualStatusBlocked`, `TestPartialDelivery_ManualReversalRefused`, `TestManual_*` | như trên + `-run '^(TestPartialDelivery_ManualReversalRefused\|TestManual_.*)$'` | `ok` |
| Lệnh cảnh báo: key, mức, payload hỏng, không Notifier, Notify lỗi | `TestCorrectedAlertCommand` | như trên | `ok` |
| HTTP: 403 thiếu quyền, 422, cùng khóa cùng thân, 409 preview_stale, 200 returning + audit đã che SĐT, 409 invalid_transition, 409 correction_blocked, 404 | `TestShipmentCorrectionAPI`, `TestShipmentCorrectionAPI_Blocked` | `go test -race -count=1 -run '^(TestShipmentCorrectionAPI.*)$' ./internal/httpapi/` | `ok … 2.582s` |
| Bảng lỗi (mã, thứ tự) | `TestDomainErrorSnapshot*` | `go test -race -count=1 -run '^TestDomainErrorSnapshot.*$' -v ./internal/httpapi/` | `--- PASS` ×3 |
| Dòng thời gian đơn hiện 2 lý do | `labels.test.ts` | `cd console && npx vitest run src/screens/don/labels.test.ts && npx tsc --noEmit && npx eslint <2 tệp>` | `Tests 7 passed`, tsc/eslint sạch |
| Cổng tĩnh (lint, jscpd, gen, test kiến trúc, gitleaks…) + test gói đổi và gói phụ thuộc | ci-task | `ci-task.sh` (bản đã gộp main b07c801d) | `REMOTE-TEST XANH https://github.com/quyenld37/elimaz-crm/actions/runs/46057089957`, `CI-TASK XANH (từ xa trên github…)`; lượt trước đỏ (gocognit, 4 mã lặp, TestSQLUsageProductCode) đã sửa |
| E2E TD10/TI08 | `TestM3ReverseDelivered_*` | `go vet ./tests/e2e/` | biên dịch, vet qua; **chưa chạy** (luật chủ shop) |

## Quyết định thay chủ shop

- **QĐ-118-1 (lỗi, mã HTTP):** chỉ dùng mã đã có trong `x-error-codes`: token khác → 409 `preview_stale`; tiền đã về shop → 409 `correction_blocked`; vận đơn không đảo được (chưa giao, giao một phần, không phải vận đơn hiện hành, đã đảo) → 409 `invalid_transition`; lý do trống/đích sai/thiếu token → 422 `invalid_input` (như `ErrManualReasonRequired`). Thứ tự kiểm lúc áp: không đảo được → token → chặn (theo kế hoạch). Khác: kiểm chặn trước token — bỏ vì kế hoạch ghi token trước.
- **QĐ-118-2 (chồng Task 106):** `SetManualStatus` delivered → returning **áp cùng luật chặn tiền** (`ErrCorrectionBlocked` → 409 `correction_blocked`), không đòi xem trước (giữ hợp đồng manual-status). Lý do: không để nhân viên đi đường vòng qua ghi tay mà đảo vận đơn đã có tiền về shop (AT40). Dễ đảo ngược: bỏ 5 dòng ở `area_manual.go`. Phương án khác: cấm hẳn delivered → returning ở ghi tay (đẩy sang màn đảo) — bỏ vì đổi hành vi Task 106 nhiều hơn cần.
- **QĐ-118-3 (lý do lịch sử đơn):** nhân viên đảo (nguồn staff, cả ghi tay lẫn sửa nhầm) ghi lý do `staff_corrected` thay vì `carrier_corrected`; màn hình hiện "Nhân viên đổi Thành công sang Đang chuyển hoàn" / "Hãng sửa trạng thái từ Thành công sang Đang chuyển hoàn" (dòng thời gian đơn, `labels.ts`). Lý do nhân viên gõ (≤ 1000) đi theo `CarrierEvent.Reason` (lịch sử vận đơn, đã che) và audit `fulfillment.shipment.correction`.
- **QĐ-118-4 (cảnh báo):** chỉ hãng sửa lại (nguồn carrier_api) mà đơn thật sự được đảo mới ghi lệnh `fulfillment.carrier_corrected_alert` (key `carrier-corrected:<mã vận đơn nội bộ>`, mức thường); nhân viên tự đảo không cảnh báo (đã có audit). Mẫu theo `authAlert` (Notifier của fulfillment).
- **QĐ-118-5 (câu tác động, token):** câu: một câu trạng thái + mỗi dòng đơn một câu đảo phiếu xuất + một câu đảo tiền hãng đã thu (khi COD > 0; số = tiền hãng báo, không báo thì COD — cùng luật cash). D1 một dòng → 3 câu như kế hoạch. Token = sha256(mã vận đơn, version, từ, tới, các câu) — không gồm câu chặn (chặn kiểm riêng). Xem trước khóa đơn → vận đơn trong transaction ngắn của nó (cùng hàm với lúc áp, không đổi dữ liệu).

- **QĐ-118-6 (luật kiến trúc):** kế hoạch chốt "fulfillment không nhập cash → chỉ đọc view `cash.shipment_cod_v` bằng SQL", nhưng `tests/arch/sqlusage_test.go` chặn (fulfillment không được gọi cash ở cột "Được gọi" của 04 mục 3). Thêm đúng hai view vào `viewReadExceptions()["fulfillment"]` (cơ chế ngoại lệ "chỉ đọc view, không nhập Go" đã có, như `inventory.warehouses_v`), lý do ghi kèm và ở cột đọc của `docs/views.md`. Không nới luật nào khác. Phương án khác: thêm cash vào moduleCalls của fulfillment — bỏ vì rộng hơn cần và sẽ thành vòng nhập.

## E2E viết sẵn, chưa chạy

- `tests/e2e/m3_reverse_delivered_test.go`: `TestM3ReverseDelivered_CarrierCorrected` (TD10: hãng giả đổi delivered → returning, webhook là tín hiệu, tra bù; vận đơn returning, đơn confirmed, tồn on_hand 5 in_transit 1, sổ COD 5 ngăn = 0, `order.delivery_reversed`, phiếu carrier_return requested, lệnh cảnh báo; kho nhận → đơn returned), `TestM3ReverseDelivered_StaffCorrection` (TI08 qua API: xem trước 3 câu, áp với token, cùng kết quả, áp lại → 409, audit). Biên dịch, `go vet` qua.

## Test bị xóa hoặc tắt

Không test nào bị xóa, tắt. Hai chỗ chạm test có sẵn (không nới assertion):
- `tests/arch/sqlusage_test.go` `viewReadExceptions()["fulfillment"]`: thêm `cash.shipment_cod_v`, `cash.cod_transfers_v` (ngoại lệ đọc view kế hoạch đã chốt, QĐ-118-6); luật vẫn chặn mọi view cash khác và mọi bảng cash.
- `TestPartialDelivery_ManualReversalRefused`: dùng helper `manualShipping` (ghi shipping lúc 3 giờ trước thay vì 2 giờ; giờ returning vẫn 1 giờ trước, ca kiểm không đổi); `TestMotDonDiTron` dùng helper `awaitingTracking` (cùng điều kiện chờ).

## Khác kế hoạch

- Chỉ số khách K1 (`delivered −1`, `returned +1`) không kiểm lại ở task này: consumer của M2 nghe `order.delivery_reversed` không đổi; e2e kiểm có sự kiện `order.delivery_reversed`.
- Ca "đã có bảng kê, hãng sửa lại → cash mở finding" thuộc Task 29/120 (`cash/postings_test.go`); ở đây kiểm phần vận đơn vẫn đảo + cảnh báo.
- Thêm gói luật thuần `internal/modules/fulfillment/internal/correction` (kế hoạch cho phép "chỉ khi cần luật thuần").

## Việc phát hiện thêm

- `cash/consumer.go` ghi lý do bút toán đảo collected cố định "Hãng sửa trạng thái từ Thành công sang Đang chuyển hoàn" cả khi nhân viên đảo (nguồn staff); nên đổi câu theo nguồn (vùng cash, ngoài phạm vi).
- Tra trạng thái dừng 7 ngày sau giao (`closedAfter`, `refresh_status.go`): hãng sửa sau đó không thấy (kế hoạch đã ghi, không mở rộng).

## Reviewer

Kết luận: ĐẠT (agent reviewer, 28/11, trên `git diff --cached` bản đã gộp main b07c801d; không phát hiện phải sửa).
- Đã kiểm: khóa đơn → vận đơn, token có version, áp song song đúng một lần qua; RetryTx không cộng dồn; luật chặn khớp `cash/internal/postings/rules.go`; lý do đã che SĐT ở audit và lịch sử, cảnh báo chỉ có mã; QĐ-118-6 chỉ thêm 2 view; QĐ-118-2 kiểm dưới khóa, có test hai chiều.
- Ghi chú không bắt buộc (không sửa): câu "Đảo bút toán tiền hãng đã thu" dựng từ dữ kiện vận đơn, không từ bút toán collected còn sống (chỉ lệch câu hiển thị nếu cash chưa ghi collected); với vận đơn api có tiền đã về, ghi tay trả `correction_blocked` trước `invalid_transition` (cùng 409); hãng sửa lại khi tiền đã về sinh hai cảnh báo (fulfillment + cash) — theo thiết kế; `ErrCorrectionInvalid` đã thêm vào mục Ảnh hưởng.
