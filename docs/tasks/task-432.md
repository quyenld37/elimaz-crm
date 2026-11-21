# Task 432 — M2-r3: Đơn trùng: hủy đơn đã gửi hãng, cờ nghi trùng, version

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (bù review M2: `docs/review-doc-lap-m2.md`
B-03, B-04, D-01, E-08). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `order.Writer.ResolveDuplicate` thêm tham số `version *int64`
  (2 chỗ gọi: `internal/httpapi/handlers_order_duplicates.go`, helper `resolve` của
  `internal/modules/order/duplicates_test.go`); `order.DuplicatesConsumer.Topics` thêm `order.cancelled` (1 chỗ kiểm:
  `TestDuplicates_ConsumerReplay`; đăng ký ở `module.go` không đổi). Không thêm ký hiệu xuất ra ngoài.
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/modules/cash`, `cash/internal/refunds`,
  `internal/modules/fulfillment`, `internal/testkit/codtest`, `internal/testkit/shiptest` (chỉ build lại; không gói nào
  khác gọi `ResolveDuplicate`).
- Operation OpenAPI bị chạm, oasdiff: `resolveOrderDuplicate` — `ResolveOrderDuplicateRequest` thêm thuộc tính tùy chọn
  `version` (int64), không bắt buộc ở lược đồ (oasdiff không phá vỡ); mã lỗi dùng mã có sẵn (`version_conflict` 409,
  `invalid_input` 400).
- Chỗ dùng trên màn hình: `console/src/screens/don/DuplicatePairs.tsx` (gửi `version` của đơn bị hủy; câu hộp xác nhận
  và thông báo theo đơn đã gửi hãng hay chưa), test `DuplicatePairs.test.tsx`; client sinh `console/src/api/**` (make gen).
- Câu sqlc, bảng, pha migration: thêm câu `SuspectPairedOrders` ở `queries/ord/duplicates.sql` (đọc `ord.orders`,
  `ord.order_duplicate_links`); không bảng, không migration.
- Test sẽ chạy: `go test -race ./internal/modules/order/...` (vùng rủi ro cao: cả gói), `./internal/httpapi/...`,
  `./tests/arch/...`; console `npx vitest run src/screens/don/DuplicatePairs.test.tsx`, tsc, eslint; ci-task một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có gì về đơn
  trùng); trong `order`: `clearSuspectIfClosed`, `lockOrder`, `orderedPair`, `changeLocked` (kiểm version), `Cancel`;
  console: `CancelDialog.tsx` (câu "Đơn đã gửi hãng…"), `useOrder`, `useWriteAction`.
- Dùng lại / viết mới vì: gỡ cờ dùng lại `clearSuspectIfClosed` và `lockOrder` theo id tăng dần như `linkAll`; kiểm
  version dùng lại kiểm của `Cancel`/`changeLocked` (truyền version người dùng gửi thay version đọc dưới khóa), không
  viết so sánh mới. Câu SQL `SuspectPairedOrders` mới vì chưa có câu nào lấy các đơn còn cờ ghép cặp với một đơn. Câu
  trên màn giữ ý của `CancelDialog` nhưng viết tại chỗ (hộp đơn trùng không chọn lý do, ngữ cảnh khác).

## Soát thiết kế

Mục 5:
- 5.1–5.3: dữ liệu của `order` (`ord.orders.duplicate_suspect`, `ord.order_duplicate_links`); không ghi bảng module khác.
- 5.4: hủy qua xử lý đơn trùng vẫn một transaction (khóa cặp → `Cancel`); không đổi.
- 5.5: gỡ cờ của đơn kia khi đơn bị hủy theo đường khác (hủy tay, nháp hết hạn, hãng xác nhận hủy) đi sau commit:
  consumer `order_duplicates` nghe thêm `order.cancelled` (đề xuất của review B-04); transaction hủy không khóa thêm
  đơn (tránh khóa ngược chiều nhóm 7).
- 5.6: không gọi ra ngoài; đơn đã gửi hãng vẫn qua `order.cancel_requested` → M3 gửi lệnh hủy, chỉ nhả hàng khi hãng
  xác nhận (`OnShipmentCancelConfirmed`).
- 5.7: consumer chạy trong tx tenant của worker; handler trong `WriteTx`.
- 5.8: module không nhập HTTP; handler chỉ truyền `version`.
- 5.9: đường ghi có sẵn `Idempotency-Key` và `x-permission`; không đường mới.
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện).
- 5.12: theo mẫu Task 73/80 (`Cancel` là đường hủy duy nhất; consumer như `DuplicatesConsumer` có sẵn).

Mục 6:
- 6.1: `version` của đơn bị hủy so dưới khóa (`changeLocked`) → `ErrVersionConflict` 409, cặp không đổi. Cờ đổi bằng
  câu có điều kiện `IS DISTINCT FROM` dưới khóa đơn.
- 6.2: gửi lại cùng khóa trả kết quả đã lưu (có sẵn); consumer chạy lại được (gỡ cờ lũy đẳng).
- 6.3: consumer khóa đơn bị hủy và mọi đơn còn cờ ghép với nó theo id tăng dần (nhóm 7, như `linkAll`), không khóa gì
  khác; transaction hủy không khóa thêm đơn. Test hai consumer song song trên một cặp không deadlock.
- 6.4–6.9: không đổi (lịch thử lại của hàng đợi; không treo mới).
- 6.10: không chạm tiền.
- 6.11: quy luật "cờ nghi trùng bật ⇔ còn cặp mở" kiểm bằng ca cụ thể (ba đơn, hủy lần lượt; hãng từ chối rồi xác
  nhận); không thêm rapid (không có không gian trạng thái mới đáng sinh ngẫu nhiên).
- 6.12: audit vẫn chỉ ghi mã đơn, hành động.
- 6.13: quyền `order.duplicates.resolve` không đổi.

## Quyết định thay chủ shop

1. **B-03: đơn đã gửi hãng bị "hủy" qua màn Đơn trùng → cặp vẫn mở.** `ResolveDuplicate` chỉ ghi kết luận `cancelled`
   và gỡ cờ khi đơn thật sự `cancelled`; đơn chỉ sang `cancel_requested` thì cặp giữ nguyên (còn ở danh sách, còn cờ).
   Hãng xác nhận hủy → đơn `cancelled` → cặp rời danh sách (câu danh sách đã bỏ cặp có đơn hủy) và consumer gỡ cờ;
   hãng từ chối → đơn về `confirmed`, cặp vẫn hiện để nhân viên xử lý lại. Vì sao: đúng DH09 "không tự hủy, nhân viên
   quyết", không mất cặp khi hai đơn cùng đi giao. Phương án khác: trả lỗi bắt hủy qua màn đơn (thêm bước cho nhân
   viên), hay ghi `not_duplicate` (sai nghĩa). Đảo ngược: bỏ điều kiện "đơn đã cancelled" ở `ResolveDuplicate`.
2. **E-08: `version` thêm vào thân, tùy chọn ở lược đồ nhưng bắt buộc với `cancel_other`/`cancel_this`** (thiếu → 400
   `invalid_input`; khác version hiện tại của đơn bị hủy → 409 `version_conflict`). `not_duplicate` không cần (không đổi
   trạng thái đơn). Vì sao: luật hợp đồng "mọi chuyển trạng thái có `version`"; màn là bên gọi duy nhất và được sửa
   cùng task; lược đồ để tùy chọn vì `not_duplicate` không dùng và oasdiff không phá. Phương án khác: chỉ kiểm khi có
   (bên gọi bỏ trống vẫn hủy nhầm được). Đảo ngược: bỏ nhánh "thiếu version" ở `ResolveDuplicate`.
3. **B-04: gỡ cờ ở consumer `order_duplicates` nghe `order.cancelled`**, không trong transaction hủy. Vì sao: mọi đường
   hủy (hủy tay, nháp hết hạn, hãng xác nhận, hủy qua đơn trùng) đều phát `order.cancelled`; không phải khóa đơn kia
   ngược thứ tự id. Cờ gỡ chậm tới khi worker chạy (vài giây). Phương án khác: khóa đơn kia trong `cancelAll` (phải
   khóa lại theo thứ tự id trước nhóm 10/11, chạm mọi đường hủy).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| B-03: hủy qua đơn trùng đơn đã gửi hãng → `cancel_requested`, không nhả hàng, cặp mở, còn cờ, còn ở danh sách; hãng từ chối → `confirmed`, cặp còn; hãng xác nhận → `cancelled`, nhả hàng, sau worker hết cờ, rời danh sách, không tự ghi kết luận | `TestDuplicates_CancelShipped` | `go test -race -count=1 -run TestDuplicates ./internal/modules/order/` | `ok … 10.8s`; bỏ nhánh "chưa cancelled thì giữ cặp" → FAIL "sau yêu cầu hủy: cặp mở 0, danh sách 0, cờ false false" |
| E-08: thiếu version → `ErrInvalidInput`; version cũ → `ErrVersionConflict` (cả hai chiều cặp), không đổi đơn/cặp/cờ; `not_duplicate` không cần version | `TestDuplicates_CancelVersion` | như trên | ok |
| E-08 qua API: thiếu version 400 `invalid_input`, version cũ 409 `version_conflict`, đúng version 200, audit đúng 1 dòng | `TestOrderAPI_Duplicates` | `go test -race -count=1 -run 'TestOrderAPI_Duplicates\|Snapshot\|Contract' ./internal/httpapi/` | `ok … 4.3s` |
| B-04: hủy tay A (ba đơn, ba cặp) → A hết cờ, B, C giữ (cặp B–C mở); hủy C → B, C hết cờ; không cặp nào bị ghi kết luận | `TestDuplicates_OtherCancelClearsFlag` | như dòng 1 | ok; bỏ `clearCancelledPairs` (trả nil) → FAIL |
| 6.3: hai đơn của cặp hủy, 8 lần `Handle(order.cancelled)` song song → không lỗi/deadlock, hết cờ | `TestDuplicates_CancelledConsumerParallel` | như dòng 1 | ok; bỏ `clearCancelledPairs` → FAIL |
| Consumer nghe đúng `order.created`, `order.updated`, `order.cancelled` | `TestDuplicates_ConsumerReplay` | như dòng 1 | ok |
| D-01: hộp xác nhận theo trạng thái gửi hãng (câu "Đang yêu cầu hủy… hàng chưa trả về kho", nút "Gửi yêu cầu hủy …"), thông báo "Đã gửi yêu cầu hủy …", cặp còn; đơn chưa gửi: câu cũ, "Đã hủy …"; thân gửi `version` của đơn bị hủy; 409 → câu báo, đọc lại đơn | `DuplicatePairs.test.tsx` (6 ca: "Hủy đơn này cần xác nhận…", "đơn đã gửi hãng…", "409 người khác vừa sửa đơn…") | `cd console && npx vitest run src/screens/don/DuplicatePairs.test.tsx` | `Tests 6 passed (6)` |
| Cổng tĩnh + test gói đổi, phụ thuộc, vùng cao | — | ci-task (một lần) | mọi gói test `ok` (order 264.8s, httpapi 139.0s, fulfillment, cash, customer, arch, e2e…); lint, oasdiff, console-check qua; gen-check đỏ chỉ vì mã sinh chưa stage (sẽ stage), jscpd 1 bản lặp trong test → tách `attachShipment`, chạy lại jscpd "Found 0 clones", `golangci-lint run ./internal/modules/order/...` "0 issues", `TestDuplicates` ok |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Cờ `duplicate_suspect` của các đơn bị hủy theo đường khác **trước** task này (sự kiện `order.cancelled` cũ không
  được giao cho consumer theo topic mới) không tự gỡ. CRM chưa chạy thật nên không có dữ liệu cần sửa; nếu có thì cần
  một lệnh vận hành chạy `clearCancelledPairs` cho các đơn cancelled còn cờ (không làm ở đây).

## Reviewer

Kết luận: ĐẠT (lần 1, agent reviewer trên phần đã stage sau khi gộp main — main không có commit mới).
- Phát hiện phải sửa: không có. Đã kiểm thứ tự khóa ADR-46 của `clearCancelledPairs` (chỉ `ord.orders`, id tăng dần
  như `linkAll`), `ResolveDuplicate` (hai đơn → dòng cặp → `Cancel`), màn hình chỉ vẽ hành vi máy chủ (thông báo theo
  trạng thái máy chủ trả), lỗi thiếu/sai version không lưu theo khóa chống trùng, cách cũ (`versions` của
  `lockOpenPair`) đã xóa.
- Ghi chú không bắt buộc (không sửa): cặp có đơn hủy sau khi hãng xác nhận giữ `resolution` NULL (Quyết định 1; báo
  cáo đếm thẳng trên bảng phải lọc đơn cancelled); `version ?? 0` khi chi tiết thiếu version → 409 an toàn; mục Ảnh
  hưởng đã nêu `docs/views.md`, `docs/nghiem-thu-m2.md`; gen-check: `make gen` sau khi stage không chênh (GEN-OK).
