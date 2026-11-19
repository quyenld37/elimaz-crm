# Task 89 — M2-32: Đóng mốc M2 — review độc lập, mutation, nghiệm thu (không thử tải)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 89, M2-32). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ đổi thân hai hàm không xuất: `(*editRun).decideApproval` (internal/modules/order/edit.go; 1 chỗ gọi trong `editRun.run`) — sửa C-02; `(Writer).recompute` (internal/modules/customer/stats.go; gọi từ `RecomputeStats` và consumer `customer_stats`) — sửa A-03: gốc lead có đơn → `w.promote` (hàm sẵn có, actor `system`, audit `customer.update`). Gói `customer` được nhập bởi `order`, `promotion`, `internal/httpapi`, `cmd/crm`: chữ ký không đổi; hành vi đổi chỉ ở cột `lifecycle` của gốc sau gộp/tính lại.
- Gói phụ thuộc ngược (go list): `internal/modules/order` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`, `internal/testkit/shiptest` (và test). Hành vi đổi: `Writer.Edit` thêm một ca trả `ErrDiscountNeedsApproval` (đơn confirmed) hay chờ duyệt (đơn new, draft) khi tiền giảm tay tăng dù tỷ lệ không tăng; lỗi này đã được `internal/httpapi/errors.go` đổi sang 403 `discount_needs_approval` và màn Sửa đơn đã xử lý mã lỗi đó.
- Operation OpenAPI bị chạm, oasdiff: chỉ mô tả — xóa câu "Chưa làm ở Task M2-02 (trả 501 not_implemented)…" ở 33 operation M2 đã có handler (listOrders, updateOrder, listCustomers, createCustomer, getCustomer, updateCustomer, addCustomerIdentity, removeCustomerIdentity, addCustomerAddress, updateCustomerAddress, deleteCustomerAddress, listCustomerMergeCandidates, mergeCustomer, undoCustomerMerge, getCustomerTimeline, addCustomerNote, listCustomerTags, createCustomerTag, updateCustomerTag, addCustomerTag, removeCustomerTag, getCustomerImportTemplate, startCustomerImport, getCustomerImport, confirmOrder, approveOrderDiscount, setOrderPending, cancelOrder, getOrderTimeline, addOrderNote, checkOrderDuplicates, listOrderDuplicates, resolveOrderDuplicate) và câu "trước đó gửi trường M2 → 501" của createOrder. Không đổi schema, đường dẫn, quyền: oasdiff không có thay đổi phá vỡ. `make gen` chỉ đổi chú thích trong `internal/httpapi/gen/*` và `console/src/api/sdk.gen.ts`.
- Chỗ dùng trên màn hình: `console/src/screens/don/OrderList.tsx` — thay `now` cố định lúc mở màn bằng hook mới `useVnToday` (tự đổi ngày lúc qua nửa đêm giờ Việt Nam) cho bộ lọc ngày (sửa D-03); `rangeQuery`, `vnMidnight` giữ nguyên. `sdk.gen.ts` chỉ đổi chú thích (sinh).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `go test -race ./internal/modules/order/ -run 'TestEdit_|TestCreateCoupon_'` (test mới `TestEdit_DiscountApprovalAmountUp`, `TestCreateCoupon_BaseAfterManualDiscounts`, `TestCreateCoupon_BoundPhoneUsesRecipient`, 5 ca mới trong `TestEdit_Errors`), `./internal/modules/customer/ -run TestStats` (`TestStatsPromotesRootAfterMerge`), `./internal/modules/promotion/ -run 'TestCheck|TestRemainingUses'` (`TestCheck_NoPerCustomerLimit`, `TestRemainingUses`), `cd console && npx vitest run src/screens/don/OrderList.test.tsx`, `./internal/httpapi/` (mã sinh đổi chú thích), `./tests/arch/` (views.md, hợp đồng), `cd console && npx tsc --noEmit`; mutation testing (gremlins) trên `internal/modules/order/**`, `internal/modules/promotion/**`, `internal/modules/customer/{merge,fororder,stats}.go`; cuối cùng ci-task.sh.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm so tiền giảm tay); trong gói `order/internal/pricing`: `ManualDiscountBP`, `Totals` (đã dùng sẵn trong `decideApproval`).
- Dùng lại / viết mới vì: sửa C-02 chỉ thêm một phép so tổng `LineDiscountTotal + OrderDiscount` mới/cũ (int64 đồng, các thành phần đã được `pricing.Compute` kiểm không âm) ngay trong `decideApproval`; không đáng một hàm riêng. Test dùng lại `newM2Env`, `mustCreateM2`, `mustEdit`, `editOrder`, `sameOrder` của `edit_test.go`. Tài liệu dùng lại khuôn `docs/review-doc-lap-m1.md` (review độc lập, lời giao chung), `docs/do-luong/cai-dat-agent-m0.md` (đo quy trình).

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

Phần mã duy nhất là sửa C-02 trong `order.Writer.Edit` (đường ghi có sẵn); còn lại là tài liệu và mô tả hợp đồng.

- 5.1–5.3 (chủ dữ liệu, không ghi bảng module khác, đọc qua view): không đổi — điều kiện mới chỉ đọc tiền của đơn đã khóa (`e.cur`) và tiền vừa tính (`t`) trong gói `order`.
- 5.4 (một transaction): không đổi; từ chối `ErrDiscountNeedsApproval` xảy ra trước mọi ghi của lần sửa, transaction của bên gọi lùi trọn (test kiểm `sameOrder`).
- 5.5, 5.6 (việc sau commit, gọi ra ngoài): không áp dụng — không thêm sự kiện, lệnh.
- 5.7 (tenant): không đổi; test chạy trong `newM2Env` (tenant riêng).
- 5.8 (nghiệp vụ không biết HTTP): giữ; đổi sang 403 vẫn ở `internal/httpapi/errors.go`.
- 5.9 (khóa chống trùng, quyền): không đổi; chỉ mô tả operation đổi.
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện mới).
- 5.12 (module mẫu): theo đường ghi `order.Edit` có sẵn.
- 6.1 (hai yêu cầu cùng lúc): không đổi — Edit khóa dòng đơn và so `version` trước khi tính; điều kiện mới so với `e.cur` đọc dưới khóa.
- 6.2–6.13: không áp dụng — không thêm khóa, bảng, việc nền, lệnh ra ngoài, hạn giờ; `decideApproval` không cộng dồn biến nào giữa hai lần chạy của `db.RetryTx`.
- Tiền: so tổng hai số `int64` đồng không âm (mỗi số ≤ tạm tính, đã qua `pricing.Compute`), không tràn.

## Quyết định thay chủ shop

1. **Thư mục mutation của M2 không thêm vào `HIGH_RISK_DIRS`.** Danh sách nằm ở tệp quy trình (chỉ chủ shop sửa; hook
   chặn). Chọn: chạy gremlins trực tiếp trên ba vùng M2 với đúng `.gremlins.yaml` của `make mutation` (ngưỡng 80%), ghi
   lệnh vào `docs/mutation.md`; việc thêm vào danh sách để `make mutation`, lịch CI và ci-task tự gồm M2 ghi ở "Dừng để
   hỏi". Phương án khác: sửa `.gremlins.yaml`/Makefile để có danh sách thứ hai — trái luật một nguồn (Task 392).
   Đảo ngược: chủ shop thêm ba mục vào `HIGH_RISK_DIRS`.
2. **Chế độ chạy mutation cho nhanh trên máy dùng chung:** `RAPID_CHECKS=25` (thay 200), `GOFLAGS='-count=1
   -skip=Deadlock'` (bỏ test chống deadlock — chúng kiểm thứ tự khóa, không bắt đột biến điều kiện, mỗi test 15–30 s),
   `--workers 4` cho order, 2 cho promotion và customer. Lý do: gói order chạy 220 s/lượt, 911 đột biến. Hệ quả: hiệu
   quả đo được có thể thấp hơn thật một chút (ít lượt rapid hơn), không cao hơn. Đảo ngược: chạy lại với cấu hình gốc.
3. **Chỉ sửa ngay các phát hiện không cần chủ shop chọn và không chạm thứ tự khóa hay hợp đồng M3:** C-02 (an ninh,
   sửa nhỏ), A-03, D-03; tài liệu E-01, E-03, E-04, E-07, E-09. Các phát hiện nhỏ khác gom vào task bù 430–439 theo
   tệp chạm chung, vì: cần chủ shop chọn hành vi (A-01, A-05, B-03 cách đóng cặp, C-03, C-04, C-05, B-06), chạm hợp
   đồng mà kế hoạch M3 dựa vào (B-01, B-02), hay đổi thứ tự khóa cần test song song riêng (A-04, B-04). Đảo ngược: làm
   từng task bù.
4. **C-02:** điều kiện bỏ qua duyệt lại khi sửa đơn đổi từ "tỷ lệ giảm tay không tăng" thành "tỷ lệ **và** số tiền
   giảm tay không tăng" (giữ ý Quyết định 9 của Task 77: giảm bớt hay tăng số lượng với cùng số tiền giảm không đòi duyệt
   lại). Phương án khác: luôn xét ngưỡng như câu chữ kế hoạch Task 77 — chặt hơn, làm nhân viên phải xin duyệt lại cả
   khi giảm bớt. Đảo ngược: bỏ `moreMoney`.
5. **A-03:** gốc lead của nhóm có đơn được đưa sang customer khi tính lại chỉ số (consumer `customer_stats`, người làm
   `system`, có audit `customer.update`); hoàn tác gộp không hạ về lead (lead → customer một chiều như ở đơn đầu tiên).
   Đảo ngược: bỏ khối promote trong `recompute`.
6. **A-01 giữ nguyên mã** (đúng kế hoạch Task M2-09: không mã khách + SĐT đã có chủ → dùng hồ sơ đó), ghi task bù 435 để
   chủ shop chọn, vì spec 05 mục 1 có hai dòng có thể hiểu khác nhau (đơn web gắn vào hồ sơ có SĐT; SĐT của hồ sơ khác
   không tự gộp).
7. **Threat model, `checkCoupon`:** chấp nhận lý do từ chối cụ thể cho nhân viên (KM02 đòi), ghi rõ M5 phải gộp
   `not_found` khi mở cho khách web.
8. **Thử tải M2 (Task 88) dời theo chủ shop 19/11:** tiêu chí 8 của nghiệm thu ghi "dời", dòng D của threat model ghi
   "chấp nhận tạm".

## Dừng để hỏi

- **Điểm dừng C** (ngoài máy này): (1) triển khai bản M2 lên staging; (2) cho 1–2 nhân viên chạy
  `docs/m2/kich-ban-nhan-vien.md` (~2 giờ), ghi kết quả vào tệp đó; chủ shop quyết sang M3. Chưa làm.
- **Thêm vùng cao M2 vào `HIGH_RISK_DIRS`** (tệp quy trình, chỉ chủ shop sửa): `internal/modules/order`,
  `internal/modules/promotion`, `internal/modules/customer/merge.go`, `internal/modules/customer/fororder.go`,
  `internal/modules/customer/stats.go` — để `make mutation`, lịch CI chủ nhật và ci-task (chạy cả bộ vùng rủi ro cao khi
  chạm) tự gồm M2. Lưu ý: ci-task sẽ chạy cả gói order (≈ 4 phút) mỗi khi chạm order.
- **Task bù 430–439** cần điều phối viên ghi vào bảng số task toàn cục (tệp ngoài repo CRM); 433 (C-04), 435 (A-01),
  436 (B-06) cần chủ shop chọn hành vi.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Step 1: mutation order ≥ 80% | gremlins trên `internal/modules/order` | `RAPID_CHECKS=25 GOFLAGS='-count=1 -skip=Deadlock' gremlins unleash --config .gremlins.yaml --workers 4 -o .mutation/order.json ./internal/modules/order` | `Killed: 787, Lived: 92, Not covered: 205` … `Test efficacy: 89.53%` (3 giờ 21 phút); sau test thêm 800/879 = 91,01% |
| Step 1: mutation promotion ≥ 80% | gremlins trên `internal/modules/promotion` | như trên, `--workers 2`, `./internal/modules/promotion` | `Killed: 165, Lived: 25, Not covered: 78` … `Test efficacy: 86.84%`; sau test thêm 168/190 = 88,42% |
| Step 1: mutation customer/{merge,fororder,stats}.go ≥ 80% | gremlins, loại mọi tệp khác của gói (`-E`) | như trên, `./internal/modules/customer` | `Killed: 81, Lived: 9, Not covered: 19` … `Test efficacy: 90.00%` |
| Đột biến sống → test thêm, kiểm tay (13 order, 3 promotion) | `TestEdit_DiscountApprovalAmountUp`, `TestEdit_Errors` (5 ca mới), `TestCreateCoupon_BoundPhoneUsesRecipient`, `TestCreateCoupon_BaseAfterManualDiscounts`, `TestCheck_NoPerCustomerLimit`, `TestRemainingUses` | script (scratchpad) cài từng đột biến vào bản sao mã rồi `go test -count=1 -skip=Deadlock -run <test> ./internal/modules/order` | `BỊ GIẾT` cho `edit.go:484` (4), `edit.go:158:114, 159, 160` (4), `create_coupon.go:32` (3), `:38` (2); còn sống `edit.go:158:60`, `:186` — lý do ở `docs/mutation.md` mục "Lần chạy M2" |
| Review C-02: tăng tiền giảm tay cùng tỷ lệ phải xin duyệt lại; giảm bớt / đổi chỗ với tổng không đổi thì không | `TestEdit_DiscountApprovalAmountUp` | ci-task (gói order) | `ok  elimaz.com.vn/crm/internal/modules/order 523.822s` |
| Review A-03: gộp nhóm đã mua vào gốc lead → gốc thành customer; lead không đơn giữ lead | `TestStatsPromotesRootAfterMerge` | ci-task (gói customer) | `ok  elimaz.com.vn/crm/internal/modules/customer 83.457s` |
| Review D-03: "Hôm nay" chuyển ngày khi màn mở qua nửa đêm | vitest "lọc Hôm nay theo ngày hiện tại khi màn mở qua nửa đêm" (`OrderList.test.tsx`) | `cd console && npx vitest run src/screens/don/OrderList.test.tsx` | `Tests  9 passed (9)` |
| Review E-01: bỏ mô tả "Chưa làm…(501)"; không thay đổi phá vỡ | oasdiff, gen-check | ci-task | xem dòng cổng kiểm |
| Review độc lập toàn M2 (Step 3), ánh xạ ca kiểm, "Xong M2 khi" | 5 reviewer mới theo vùng A–E | — (agent) | `docs/review-doc-lap-m2.md`: 0 nghiêm trọng, 7 lớn, 17 nhỏ, 4 nghi ngờ; 19/19 ca M2 có test; `docs/nghiem-thu-m2.md` |
| Step 2, 4: threat model trỏ test, RUNBOOK M2, ADR 0023/0029/0041/0046, đo quy trình | tài liệu | đọc | `docs/threat-model-m2.md`, `docs/RUNBOOK.md` (mục M2), `docs/adr/0046-thu-tu-khoa.md`, `docs/do-luong/cai-dat-agent-m2.md` |
| Cổng kiểm của task | cổng tĩnh + test gói đổi, phụ thuộc | ci-task (lần 2, sau khi stage) rồi `go test -race -count=1 -timeout 40m ./internal/modules/order/...` | Lần 2: mọi cổng tĩnh xanh (build, vet, lint, gen-check, catalog, oasdiff, jscpd `Found 0 clones`, gitleaks `no leaks found`, test-guard, console-check); test: `ok` cho cmd/crm, httpapi, cash, customer, fulfillment, promotion, files, realtime, tests/arch, tests/e2e; riêng order `panic: test timed out after 10m0s` khi đang chạy `TestSchemaOrderMoneyChecks` — máy dùng chung tải 45 (`uptime`), không test nào đỏ. Chạy riêng gói order với `-timeout 40m`: `ok elimaz.com.vn/crm/internal/modules/order 628.368s`, pricing, state `ok`. (Lần 1, trước khi stage, order `ok 523.822s`.) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `.gremlins.yaml` không đổi (kế hoạch ghi "thêm thư mục vùng cao M2"): danh sách thư mục nằm ở `HIGH_RISK_DIRS` (một
  nguồn, Task 392; chỉ chủ shop sửa) — xem Quyết định 1 và Dừng để hỏi.
- Thử tải M2 (Task 88) và đo 1 triệu khách: dời theo chủ shop 19/11 (Quyết định 8).
- Step 5 (Điểm dừng C) chưa làm: cần staging và nhân viên (Dừng để hỏi).
- Ngoài kế hoạch, theo phát hiện mutation: thêm test `TestCreateCoupon_*` (2), `TestCheck_NoPerCustomerLimit`,
  `TestRemainingUses`, 5 ca trong `TestEdit_Errors` (chỉ test, không đổi mã).

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 1, agent reviewer, trên bản đã gộp main `a9a6339`).

- Phát hiện phải sửa: không có.
- Đủ yêu cầu: mọi test mới có thật; C-02 (`TestEdit_DiscountApprovalAmountUp`), A-03 (`TestStatsPromotesRootAfterMerge`),
  D-03 (vitest nửa đêm) đều đỏ với mã cũ; mã sinh chỉ đổi chú thích, khớp 33 mô tả + `createOrder` đã bỏ.
- Khóa: `promote` trong `recompute` đổi dòng gốc `lockGroups` đã khóa, không thêm khóa, không đổi thứ tự ADR-46.
- Ghi chú không bắt buộc (không sửa): báo hiệu `customer.changed` hai lần khi promote trong `recompute` (thừa, không sai);
  promote tăng `version` gốc nên người đang sửa hồ sơ có thể gặp xung đột version một lần (đúng khóa lạc quan); mục Ảnh
  hưởng có thể kể thêm `CLAUDE.md`, `docs/views.md` (chỉ tài liệu).
