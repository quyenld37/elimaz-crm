# Task 527 — Sửa e2e M3 đỏ sau 139 (đường trọn, hoàn hãng, hai người bán, bảng kê)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (task điều phối giao, không có trong tệp PLAN; số toàn cục 527); nền: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` Task 139 (M3-48). Loại việc (quy trình mục 3): B (lỗi tìm thấy: test đỏ trước, rồi sửa)

Nguồn: job e2e Go của GitHub run 46129195839 / 46130526877 trên main sau khi gộp Task 139 (1086279f): đỏ TestM3FullFlow, TestM3FullFlow_CarrierReturn, TestM3TwoSellers, TestM3Chaos_StatementBatchRestart.

## Chẩn đoán gốc rễ

1. **TestM3TwoSellers — lỗi MÃ (lỗ hổng phân quyền người bán, ADR-29).** `GET /api/v1/shipments/{id}` và `/events` của API (chạy với người bán mặc định) trả 200 vận đơn của người bán PARTNER1. Gốc: các handler vận đơn chỉ kiểm phạm vi kho (`canAt`, `shipAuthorize`), không handler nào so `shipments.seller_id` với người bán mặc định, trong khi đường đơn (`order.Get(seller, id)`, `orderAt`) có. Threat model M3 dòng S ghi "mọi câu đọc có seller_id" — chưa đúng với mã. Rà mọi route: cùng lỗ ở mọi route theo mã vận đơn (sync, cancel, resend, resolve-create, manual-status, address-changes, partial-delivery, corrections), theo mã việc gọi lại / yêu cầu đổi địa chỉ / việc cần xem / lô nhãn, `POST /orders/{id}/shipments` (gửi đơn của B bằng tài khoản của B), `GET /orders/{id}/shipments`, `GET /shipments/{id}/cod`, điều chỉnh COD, và ba danh sách vận hành (đơn kẹt, cần xem + số đếm, việc gọi lại + số đếm).
2. **TestM3FullFlow — lỗi TEST.** `ConfirmStatement` chỉ chốt và phát `cod_statement.confirmed`; lô ghi tiền chạy ở consumer sau commit (chú thích của hàm, spec ADR-24/ lô ghi theo lô). Test đọc sổ COD ngay sau 200 của lệnh chốt → còn `carrier_holding 280000`. Sửa: chờ sổ COD về đúng số (theo sự kiện, có trần thời gian).
3. **TestM3FullFlow_CarrierReturn — lỗi TEST.** Theo Q-9 (`shipstate/graph.go`: `CanonReturnedToShop` → giữ `returning`, chỉ ghi mốc `carrier_reported_returned_at`), vận đơn chỉ sang `returned` khi kho bấm nhận (TD02; `ReturnReceivedConsumer`). Test chờ `returned` trước khi kho nhận. Sửa: chờ mốc hãng báo đã hoàn (vẫn `returning`), kho nhận, rồi chờ vận đơn `returned`.
4. **TestM3Chaos_StatementBatchRestart — lỗi TEST.** Giới hạn mỗi nhân viên 300 yêu cầu / 60 s (spec 04 mục 11, ADR-30), khóa theo UserID (đúng: mỗi nhân viên một khóa). `waitFor` hỏi API mỗi 20 ms (50 lượt/s) trong tới 60 s → 429 sau ~6 s. Màn hình thật không hỏi 50 lần/giây. Sửa (QĐ-525-1, hướng chung của điều phối): các vòng chờ hỏi qua API hỏi theo nhịp `apiPoll` 300 ms (≤ 200 lượt/phút), không đổi ngưỡng. Lượt e2e từ xa thứ nhất (run 46140692456) còn đỏ: hết 429 nhưng bảng kê không tới `matched` trong 60 s — lần ghi kết quả của lệnh đọc tệp rơi đúng lúc ngắt database, lệnh còn `running` tới hết giữ chỗ (90 s) rồi `in_doubt`, và việc tra lại `in_doubt` (`commands.ResolveInDoubt`, lịch 30 s của cmd/crm) không chạy trong stack e2e. Đây là đường phục hồi đúng thiết kế (Bẫy: không chắc thì in_doubt, không tự gửi lại), không phải lỗi mã: test tua nhanh hai bước đó như `workercrash_test` (`statementAfterOutage`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): thêm `fulfillment.CheckSeller`, kiểu `fulfillment.ScopeKind` và 5 hằng `Scope*` (mới, 0 chỗ dùng cũ). Thêm trường bắt buộc `SellerID` vào `fulfillment.StuckFilter` (chỗ dùng: `handlers_shipment_lists.go` + 9 `StuckFilter{` trong `lists_stuck_test.go`), `fulfillment.ReviewFilter` (`handlers_shipment_lists.go:76` + 5 trong `lists_review_test.go`), `fulfillment.CallbackFilter` (`handlers_callbacks.go:36` (trước khi đổi; nay :37) + 12 trong `callbacks_test.go`) — sửa hết trong task này. Không đổi chữ ký hàm nào. Hàm riêng mới `cursorAfter` (fulfillment/area_lists.go) thay ba khối `if after != nil {…}` giống nhau (jscpd). Helper test mới `shiptest.Children`, `shiptest.ChildrenOf` (internal/testkit/shiptest, chỉ test dùng: `seller_scope_test.go` của fulfillment và httpapi). httpapi: helper riêng của gói `sellerOwns`, `sellerShipment`, `sellerShipmentCOD`, `sellerOrder`, `sellerShipmentWrite` (refusableWrite + kiểm người bán), `readableShipment` (người bán + đọc + phạm vi kho đọc), `listScope` (người bán + kho đọc được của danh sách) (`internal/httpapi/seller_scope.go`); test: `shipDeps` tách từ `shipServer` (`create_shipment_test.go`), gọi trong 26 handler ở 14 tệp `handlers_{shipment,shipment_events,shipment_sync,shipment_cancel,shipment_manual,shipments_m3,address_changes,partial_delivery,corrections,callbacks,shipment_lists,labels,cod_balances,cod_adjust}.go`. e2e: `waitForAPI`, `waitEvery`, `apiPoll` mới ở `tests/e2e/stack_test.go` (`waitFor` giữ nguyên nhịp 20 ms); vòng chờ gọi API ở `m3_full_flow_test.go`, `m3_reverse_delivered_test.go` dùng `waitForAPI`. Tài liệu: `docs/threat-model-m3.md` thêm dòng I/T người bán.
- Gói phụ thuộc ngược (go list): nhập `internal/modules/fulfillment`: cmd/crm, internal/adapters/carrier/{,best,carriertest,fake,manual,spx,vtp,vtp/vtptest}, internal/httpapi, internal/httpapi/web, internal/modules/cash (+ internal/formats, internal/statement), internal/testkit/{codtest,shiptest}, internal/wiring/webcache, tools/fakecarrier/server, tools/loadkit — chỉ thêm ký hiệu và trường, chỉ 3 tệp httpapi dựng ba bộ lọc.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng (route, thân, mã lỗi giữ nguyên; người bán khác trả 404 đã có trong mô tả "vận đơn lạ → 404"). Hành vi đổi ở: getShipment, listShipmentEvents, syncShipment, cancelShipment, resendShipment, resolveShipmentCreate, setShipmentManualStatus, listShipmentAddressChanges, requestAddressChange, resolveAddressChange, recordPartialDelivery, previewShipmentCorrection, applyShipmentCorrection, createShipment, listOrderShipments, createLabelBatch, getLabelBatch, recordCallbackOutcome, assignCallbackTask, resolveShipmentAnomaly, listCallbackTasks, listStuckShipments, listShipmentsForReview, getShipmentCod, previewCodAdjustment, createCodAdjustment.
- Chỗ dùng trên màn hình: không đổi mã console (cùng hợp đồng); màn hình chỉ thấy vận đơn của người bán mặc định — hôm nay chỉ có một người bán thật (ELIMAZ) nên dữ liệu thật không đổi.
- Câu sqlc, bảng, pha migration: tệp mới `queries/fulfillment/seller_scope.sql` (5 câu đọc người bán của vận đơn / việc gọi lại / yêu cầu đổi địa chỉ / việc cần xem / lô nhãn); thêm điều kiện `s.seller_id = @seller_id` vào `ListStuckShipments`, `ListOpenAnomalies`, `ReviewCounts`, `ListCallbackTasks`, `CountOpenCallbacks`. Không migration, không bảng mới (đọc theo khóa chính / khóa ngoại sẵn có).
- Test kế hoạch truy vấn đổi tham số theo câu: `TestCallbacks_PlanUsesIndexes`, `TestStuck_PlanUsesIndexes` (fulfillment), `TestM3ListPlans_Large` (cash, `seed.Own()` làm người bán).
- Test sẽ chạy: mới `TestShipmentRoutes_OtherSellerNotFound`, `TestShipmentLists_OtherSellerHidden` (internal/httpapi), `TestCheckSeller` (internal/modules/fulfillment); sửa và chạy lại `TestListStuck*`, `TestListForReview*`, `TestCallback*` liên quan (fulfillment); e2e từ xa: TestM3FullFlow, TestM3FullFlow_CarrierReturn, TestM3TwoSellers, TestM3Chaos_StatementBatchRestart (remote-test.sh, job e2e Go); ci-task theo gói đổi (fulfillment, httpapi vùng rủi ro cao không chạm; fulfillment/status.go không chạm).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm "đối tượng thuộc người bán"); tìm `DefaultSeller`, `readAsDefaultSeller`, `orderAt`, `shipAuthorize`, `CallbackTaskShipment`, `AddressChangeShipment`, `AnomalyForResolve`.
- Dùng lại / viết mới vì: dùng lại `catalog.DefaultSeller` (như `orderAt`, `readAsDefaultSeller`) và `orderAt`-kiểu `order.Get(seller, id)` cho đường theo mã đơn; viết mới `fulfillment.CheckSeller` vì chưa có hàm công khai nào trả người bán của vận đơn / việc con (câu `CallbackTaskShipment` v.v. là nội bộ, trả mã vận đơn, không trả người bán). Không gộp với `shipAuthorize` (phạm vi kho, ADR-18) — hai trục khác nhau, khác lý do thay đổi. Gộp mẫu lặp mà jscpd báo (kiểm người bán + đọc vận đơn + phạm vi kho; đầu hai danh sách; con trỏ trang) thành `sellerShipmentWrite`, `readableShipment`, `listScope`, `cursorAfter`: cùng mục đích, cùng lý do thay đổi. `customer.ListCursor.params` làm cùng phép đổi con trỏ → hai tham số keyset trên kiểu con trỏ riêng của customer: giữ riêng (khác module, khác kiểu con trỏ, khác lý do thay đổi; luật "logic của module ở lại trong module"). e2e: thêm `waitForAPI` (nhịp 300 ms) cạnh `waitFor`, dùng lại `receiveReturn`, `cod`, `shipmentStatusOwner`.

## Soát thiết kế

- 5.1 (đúng module): câu SQL người bán nằm trong `queries/fulfillment`, hàm công khai ở gói fulfillment; httpapi chỉ lấy người bán mặc định và gọi public API (luật Go 7).
- 5.2 (quyền): người bán là trục thứ hai cạnh phạm vi kho; người bán khác → cùng 404 như vận đơn lạ (không lộ tồn tại). Kiểm trong cùng transaction với đường đọc/ghi.
- 5.3 (dữ liệu, tenant): mọi câu chạy trong tx tenant (RLS); `seller_id` có sẵn ở shipments, carrier_accounts (ADR-29).
- 5.4 (lỗi): dùng sentinel có sẵn (`ErrNotFound`, `ErrCallbackNotFound`, `ErrAnomalyNotFound`, `ErrOrderNotFound`, `cash.ErrShipmentNotFound`); không mã lỗi mới, không đổi errors*.go.
- 5.5 (hiệu năng): câu kiểm theo khóa chính; điều kiện seller trong danh sách lọc trên dòng đã nối `shipments` (không thêm quét bảng lớn). Việc gọi lại (`ListCallbackTasks`, `CountOpenCallbacks`): JOIN shipments + lọc seller làm planner chọn Hash Join quét cả `shipments` (`TestCallbacks_PlanUsesIndexes` đỏ) → đổi sang câu con vô hướng theo khóa chính của vận đơn, trang vẫn đi index `callback_tasks_status_due` và dừng ở LIMIT.
- 5.6–5.9, 5.11: không áp dụng: không bảng, migration, thư viện, lệnh nền, cấu hình mới.
- 5.10: không đổi giới hạn tốc độ (QĐ-525-1).
- 5.12: test theo mẫu `handlers_shipment_test.go` (orderAPIEnv, serveAs), module test theo `env_test.go`.
- 6.1 (transaction): kiểm người bán trong cùng tx với thao tác; đọc không khóa, thao tác sau vẫn khóa như cũ — `seller_id` của vận đơn không bao giờ đổi nên không có khe đổi giữa lúc kiểm và lúc khóa.
- 6.2 (gửi lại): 404 người bán khác là lỗi nghiệp vụ → không lưu khóa chống trùng (refusableWrite), như vận đơn lạ.
- 6.3 (RetryTx chạy lại): helper không giữ biến ngoài; chạy lại cho cùng kết quả.
- 6.4–6.11, 6.13: không áp dụng: không thêm khóa, tiền, hàng chờ, gọi ra ngoài, đồng hồ.
- 6.12 (dữ liệu cá nhân): không log gì mới.

## Quyết định thay chủ shop

- QĐ-527-1: Cách ly người bán làm ở lớp đường API (httpapi gọi `fulfillment.CheckSeller` với người bán mặc định, danh sách lọc `seller_id`), không thêm RLS theo người bán. Lý do: spec 04 mục 14 bước 0 "chưa xây gì" cho tài khoản người bán; API hôm nay luôn chạy với người bán mặc định giống đường đơn (`order.Get(seller, …)`); RLS theo người bán là việc của bước 2 (cần thiết kế riêng, migration). Dễ đảo ngược: khi có tài khoản người bán thì thay "người bán mặc định" bằng người bán của tài khoản ở một chỗ (helper). Phương án khác: thêm tham số `sellerID` vào mọi hàm Service của fulfillment (đúng mẫu catalog hơn, nhưng đổi chữ ký ~20 hàm và mọi test của gói lớn) — để lại cho bước 2.
- QĐ-527-2: Ba lỗi còn lại sửa ở test, không đổi mã: lô ghi tiền sau chốt chạy nền là thiết kế (ConfirmStatement); Q-9 (hãng báo đã hoàn chỉ ghi mốc, kho nhận mới `returned`); giới hạn 300/60 s mỗi nhân viên đúng khóa (UserID). Phương án khác: cho e2e dùng nhiều nhân viên hay nới ngưỡng trong stack — bị loại (QĐ-525-1: không đổi ngưỡng; một nhân viên thật không hỏi 50 lần/giây).
- QĐ-527-4: Chaos bảng kê tua nhanh hết giữ chỗ và gọi `commands.ResolveInDoubt` trong test thay vì chạy lịch tra lại trong stack e2e hay hạ giữ chỗ của worker. Lý do: không đổi mã chạy thật, giống cách workercrash_test đã làm; phương án khác (thêm lịch vào stack) đổi hạ tầng test chung của mọi e2e.
- QĐ-527-3: Danh sách của cash (`/cod/discrepancies`, `/cod/overdue`, `/cod-statements`) chưa lọc người bán — ghi vào Việc phát hiện thêm, không làm trong task (module cash, câu danh sách riêng; nhập bảng kê đã chặn tài khoản người bán khác).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Người bán khác → 404 `not_found` ở 23 operation theo mã (26 = 23 ca này + 3 danh sách ở dòng dưới), không ghi gì (chụp trạng thái, audit trước/sau); đối chứng 4 route GET của người bán mặc định → 200 | `TestShipmentRoutes_OtherSellerNotFound` (internal/httpapi) | `go test -race -count=1 -run '^(TestShipmentRoutes_OtherSellerNotFound\|TestShipmentLists_OtherSellerHidden)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 3.792s` |
| Danh sách đơn kẹt, cần xem (+ số đếm), việc gọi lại (+ số đếm) không lộ dòng của người bán khác | `TestShipmentLists_OtherSellerHidden` (httpapi) | như trên | ok |
| `CheckSeller`: 5 loại đối tượng, người bán mình nil, người bán khác / mã lạ → sentinel của loại | `TestCheckSeller` (fulfillment) | `go test -race -count=1 -run '^(TestCheckSeller\|TestListStuck.*\|TestListForReview.*\|TestCallback.*\|TestStuck_PlanUsesIndexes)$' ./internal/modules/fulfillment/` | `ok elimaz.com.vn/crm/internal/modules/fulfillment 8.830s` |
| Câu danh sách có lọc người bán không Seq Scan | `TestCallbacks_PlanUsesIndexes`, `TestStuck_PlanUsesIndexes` (fulfillment), `TestM3ListPlans_Large` (cash, ~200.000 vận đơn) | như trên; cash trong ci-task | ok; trước khi đổi sang câu con vô hướng: `kế hoạch có Seq Scan: [... Hash Join Seq Scan ...]` |
| Bốn e2e M3 xanh: TestM3FullFlow, TestM3FullFlow_CarrierReturn, TestM3TwoSellers, TestM3Chaos_StatementBatchRestart | tests/e2e (cả gói) | `CRM_CI_E2E=1 CRM_CI_STATIC=false CRM_CI_RUNNER=github scripts/ci/remote-test.sh <tree> <base> <rỗng>` | run 46140692456: chỉ Chaos đỏ (ba test kia qua); run 46141861880 (tree 57559c6b, sau sửa Chaos): job e2e `ok elimaz.com.vn/crm/tests/e2e 156.527s`; lượt trên bản cuối (tree aefa2561, sau gộp main 9d0be8e9) run 46162253390: bốn test M3 không có `--- FAIL`; dòng đỏ duy nhất `--- FAIL: TestMotDonDiTron_Script (0.58s)` … `POST /api/v1/carrier-accounts/…/locations/sync → 409 (mong 202): invalid_transition` (từ Task 526 trên main, xem Việc phát hiện thêm) |
| Cổng tĩnh + test gói đổi và phụ thuộc (go.mod/testkit đổi → ALL) | ci-task | `ci-task.sh` | `REMOTE-TEST XANH .../runs/46161237245`, `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh 9d0be8e9…)`; lượt trước 46160211937 đỏ ở `TestSendProperty_AtMostOncePerClientRef` (conversation, `commit transaction: timeout`, không liên quan; lượt sau cùng mã xanh) |

## E2E viết sẵn, chưa chạy

(không có — bốn test e2e có sẵn, chạy từ xa ở bằng chứng)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Một commit gồm cả thay đổi cấu trúc nhỏ (`cursorAfter` thay ba khối con trỏ giống nhau, `shipDeps` tách từ `shipServer`) và hành vi: chỉ để qua jscpd cho mã mới của task, không dời tệp, không đổi tên.

## Việc phát hiện thêm

- Danh sách của cash (`/cod/discrepancies`, `/cod/overdue`, `/cod-statements`) chưa lọc người bán (QĐ-527-3).
- `TestMotDonDiTron_Script` (tests/e2e) đỏ sau khi gộp Task 526 vào main (848ea4d7 sửa `cmd/crm/seed.go` nạp địa giới và `scripts/demo/mot-don-di-tron.sh`): bước đồng bộ địa giới của tài khoản hãng trả 409 `invalid_transition`. Không thuộc task này (không chạm carrier-accounts/locations), để điều phối giao cho chủ Task 526.
- Job console-e2e (Playwright) của remote-test đỏ 46 ca (staff, van-chuyen…): không thuộc task; chủ shop đã bỏ Playwright (28/11).

## Reviewer

Kết luận: ĐẠT (lượt 4). Lượt 1–3 CẦN SỬA chỉ ở tài liệu (mã không có lỗi tính đúng hay an toàn): số handler/tệp ở Ảnh hưởng (21/13 → 26/14), số chỗ dùng `CallbackFilter{` (13 → 12), dòng bằng chứng e2e bản cuối còn trống, số ca của `TestShipmentRoutes_OtherSellerNotFound` (23 theo mã + 3 danh sách; đối chứng 4 GET) — đã sửa hết. Reviewer xác nhận: không route vận đơn/việc con theo mã nào thiếu kiểm người bán; kiểm trong cùng tx tenant; sentinel 404 đúng loại; câu sqlc lọc đủ cả số đếm; `CountStuckByStatus`/`CountUnsynced`/`CountSyncErrors` chỉ worker dùng nên không lọc là đúng. Ghi chú không bắt buộc (giữ nguyên): đường ghi chưa có đối chứng 200 phía người bán mình; `statementAfterOutage` dựa vào lệnh chạy lại không ghi trùng.
