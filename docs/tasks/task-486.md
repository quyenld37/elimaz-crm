# Task 486 — Bổ sung dữ liệu theo mockup: yêu cầu về đơn + góp ý (API + màn)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm** trường/hàm, không đổi chữ ký có sẵn.
  `order.RequestFilter` thêm `Statuses []string` (dùng: `handlers_order_requests.go`, `requests_test.go`);
  `order.RequestSummary` thêm `CustomerName`, `PhotoCount`, `ShipBackDeadline` (dùng: `handlers_order_requests.go`);
  `order.RequestLineDetail` thêm `Want *WantVariant` (dùng: `handlers_order_requests.go`, `handlers_web_requests.go` —
  website không trả trường mới); `order.RequestDetail` thêm `History []RequestEvent` (2 chỗ như trên); mới
  `order.CountRequests`, `order.RequestCounts`, `order.WantVariant`, `order.RequestEvent`. `customer.Feedback` thêm
  `CustomerCode` (dùng: `handlers_feedback.go`); mới `customer.CountFeedback`. Hàm nội bộ `moveRequest` thêm tham số
  người làm (3 chỗ gọi trong `order`).
- Gói phụ thuộc ngược (go list): nhập `order` hoặc `customer`: `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`,
  `cash`, `cash/internal/refunds`, `fulfillment`, `promotion`, `testkit/codtest`, `testkit/shiptest`,
  `wiring/webcache` — không gói nào ngoài `httpapi` dùng ký hiệu bị chạm (chỉ thêm trường).
- Operation OpenAPI bị chạm, oasdiff: `listOrderRequests` (thêm tham số `statuses`, trường `customerName`,
  `photoCount`, `shipBackDeadline` trong `OrderRequestSummary`); `getOrderRequest`/`setOrderRequestStatus` (thêm
  `history` trong `OrderRequest`, `want` trong `OrderRequestLine` — `OrderRequestLine` cũng nằm trong câu trả lời
  website `webListRequests`: trường tùy chọn, handler web không điền); mới `countOrderRequests`
  (`GET /api/v1/order-requests/counts`, order.requests.read), `countFeedback` (`GET /api/v1/feedback/counts`,
  customer.feedback.read); `Feedback.customerCode`. Chỉ thêm → oasdiff không báo phá vỡ.
- Chỗ dùng trên màn hình: `console/src/screens/order-requests/{RequestList,RequestDetail,route,requestData,
  requestRules,requestTestkit}`, `console/src/screens/feedback/{FeedbackList,FeedbackPanel,feedbackTestkit,feedbackData}`.
  Không sửa `app/nav.ts` (số cạnh menu dùng `badge` có sẵn của `Screen`); không chạm tệp của Task 462.
- Câu sqlc, bảng, pha migration: `queries/ord/requests.sql` (`ListOrderRequests` thêm tên khách, số ảnh, hạn gửi trả,
  lọc mảng trạng thái; mới `CountOrderRequests`, `RequestWantVariants`, `InsertOrderRequestEvent`,
  `ListOrderRequestEvents`); `queries/customer/feedback.sql` (`ListFeedback` thêm `code` của gốc; mới
  `CountFeedback`). Migration mới `0528_order_request_events.sql`: pha mở rộng (CREATE TABLE chỉ ghi thêm, RLS, index,
  GRANT SELECT, INSERT). Đọc khác module qua view: `customer.customers_v`, `catalog.variants_v`,
  `inventory.stock_available_v`, `inventory.warehouses_v`.
- Test sẽ chạy: `go test -race -run` các test mới/sửa của `internal/modules/order` (requests_*), `customer`
  (feedback_*), `internal/httpapi` (`TestOrderRequestAPI_*`, `TestFeedbackAPI_*`), `tests/arch`; console vitest của hai
  thư mục màn + `src/app/errors.test.ts`; ci-task cho gói đổi + phụ thuộc (order, customer là vùng rủi ro cao → cả bộ
  của vùng do ci-task chạy).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm đếm theo
  trạng thái chung — mỗi module đếm bảng của mình); `identity.ListEntityAudit` (ghi rõ "không thay cho lịch sử nghiệp
  vụ của module") → không dùng làm lịch sử yêu cầu; mẫu bảng lịch sử chỉ ghi thêm `ord.order_status_history` (0117) và
  trigger chặn sửa/xóa của `order_request_messages` (0504); `NavCount` (`shell/NavCount.tsx`, khai qua `badge` của
  `Screen`); `MoreBar`, `statusChip`, `shortTime`, `requestTestkit`/`feedbackTestkit` có sẵn.
- Dùng lại / viết mới vì: dùng lại `NavCount`, `badge`, testkit, `staffNames` (tên nhân viên của tin) cho người làm
  của lịch sử; bảng sự kiện mới theo đúng mẫu chỉ ghi thêm của 0504 (trigger riêng cho bảng mới vì hàm trigger cũ có
  tên bảng trong câu báo lỗi). Câu đếm viết riêng ở mỗi module (order đếm yêu cầu, customer đếm góp ý): khác bảng, khác
  lý do thay đổi.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Chủ dữ liệu: lịch sử yêu cầu thuộc `order` → bảng `ord.order_request_events`; đếm góp ý thuộc `customer`.
- 5.2 Không ghi bảng module khác: chỉ ghi `ord.order_request_events` trong `order`.
- 5.3 Đọc module khác qua view: tên khách `customer.customers_v`, biến thể muốn đổi `catalog.variants_v`, số còn
  `inventory.stock_available_v`, tên kho `inventory.warehouses_v` (đều có trong `docs/views.md`).
- 5.4 Nguyên tử: dòng sự kiện ghi trong `moveRequest`, cùng transaction với lần đổi trạng thái.
- 5.5, 5.6 không áp dụng: không có việc sau commit, không gọi ra ngoài.
- 5.7 Bảng mới có `tenant_id`, RLS (`platform.enable_tenant_rls`), khóa ngoại ghép `(tenant_id, request_id)`; mọi câu
  chạy trong `tenancy.WithTenant` (handler dùng `catalogRead` có sẵn).
- 5.8 `order`, `customer` không nhập HTTP; handler mỏng.
- 5.9 Không thêm đường ghi; hai đường đọc mới có `x-permission` (order.requests.read, customer.feedback.read).
- 5.10 không áp dụng: không dùng Valkey. 5.11 Không thêm thư viện. 5.12 Theo mẫu module order/customer có sẵn.
- 6.1 Đường đọc không sửa dòng. Sự kiện chỉ chèn sau khi đã khóa dòng yêu cầu và cập nhật có điều kiện version.
- 6.2 không áp dụng: không thêm đường ghi; đường ghi cũ vẫn chống trùng như trước (dòng sự kiện cùng tx với kết quả).
- 6.3 Thứ tự khóa: ADR-46 nhóm 7 thêm `order_request_events` cạnh messages/files (chỉ chèn, không khóa) — cập nhật chú
  thích. Không khóa thêm dòng nào.
- 6.4 Dùng `refusableWrite`/`catalogRead` có sẵn (đã có thử lại). 6.5 READ COMMITTED; số đếm là ảnh chụp, không quyết
  định nghiệp vụ.
- 6.6–6.8 không áp dụng: không việc nền, không gọi ngoài. 6.9 Câu đếm dùng index có sẵn (`order_requests_status`,
  `feedback_state_created`): kiểm bằng EXPLAIN trong test. 6.10 không áp dụng: không tiền. 6.11 không áp dụng: không
  thêm quy luật vùng rủi ro (chỉ đọc + một dòng lịch sử).
- 6.12 Dữ liệu cá nhân: tên khách, mã KH chỉ trả cho người có order.requests.read / customer.feedback.read; không vào
  log; lịch sử chỉ trạng thái, loại người làm, mã người làm (không nội dung tin); audit không đổi.
- 6.13 Quyền: route mới khai `x-permission`, test 403 khi thiếu quyền.

## Quyết định thay chủ shop

1. **Lịch sử yêu cầu = bảng mới `ord.order_request_events`** (chỉ ghi thêm, một dòng mỗi lần đổi trạng thái: từ, sang,
   loại người làm, mã người làm, lúc). Màn ghép thêm tin (trả lời, ghi chú nội bộ, tin khách) và dòng "Khách gửi yêu
   cầu" từ `createdAt`. Không đọc từ audit (`identity.ListEntityAudit` ghi rõ không thay lịch sử nghiệp vụ; audit
   không có lần khách rút/trả lời qua website). Yêu cầu tạo trước task này không có dòng đổi trạng thái cũ (CRM chưa có
   dữ liệu thật). Phương án khác: đọc audit — sai luật của hàm, thiếu thao tác của khách.
2. **Số đếm theo trạng thái là API riêng, đếm toàn bộ** (`countOrderRequests`, `countFeedback`), không theo bộ lọc
   loại/chủ đề/ô tìm: như mockup (số trên nhãn cố định khi đổi loại). Màn yêu cầu dùng số "Đang mở" cho số cạnh menu
   (HV02). Phương án khác: đếm theo bộ lọc — mỗi lần gõ tìm thêm một câu đếm.
3. **Lọc nhiều trạng thái bằng tham số mới `statuses`** (giữ `status` cũ để không phá hợp đồng); gửi cả hai → 400
   `invalid_input`. Màn mặc định nhãn **"Đang mở"** (received, in_review, needs_info, confirmed, items_received) như
   mockup F ("Không có yêu cầu đang mở … Xem tất cả yêu cầu"); đảo Quyết định 1 của Task 207 (lý do cũ: API chưa lọc
   nhiều trạng thái).
4. **"Muốn đổi sang"**: trả mã, tên mẫu, màu, cỡ của biến thể và số còn bán được (`available`) ở kho của đơn, kèm tên
   kho (mockup B "Đen / L còn 4 ở Kho chính"). Đơn chưa có kho → không có số còn. Biến thể không còn → chỉ ghi như cũ.
5. **Không làm cờ "Tin mới"** của mockup A: cần trạng thái "đã đọc" theo từng nhân viên (bảng, đường ghi mới); prompt
   task không liệt kê. Ghi ở Việc phát hiện thêm.
6. **Danh sách yêu cầu thay cột "Lý do" bằng "Khách" và thêm cột "Ảnh"** đúng cột của mockup A; trạng thái "Đã xác nhận
   đổi" kèm "gửi trước dd/mm" (trường `shipBackDeadline` của dòng).
7. **Mã KH trong góp ý** là mã của hồ sơ gốc hiện tại (cùng gốc với `customerId`, `customerName`); liên kết ghi
   "Hồ sơ KH…" như mockup.
8. **Lời nhắn bị bỏ khi Từ chối/Hoàn tất**: không đổi (chờ chủ shop, theo prompt).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Danh sách yêu cầu: tên khách (gốc), số ảnh, hạn gửi trả; lọc nhiều trạng thái; status+statuses, trạng thái lạ → ErrInvalidInput | `TestRequest_ListMockupFields` | `go test -race -count=1 -run '^(TestRequest_ListMockupFields\|TestRequest_Counts\|TestRequest_WantVariant\|TestRequest_History\|TestRequest_ListAndTenant\|TestRequest_MessagesAndCustomer)$' ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 3.168s` |
| Đếm theo trạng thái (đủ 8, đang mở, tổng), tenant B không tính | `TestRequest_Counts` | như trên | ok |
| Biến thể muốn đổi sang (mã, màu, cỡ, còn 4 ở kho của đơn); website không nhận | `TestRequest_WantVariant` | như trên | ok |
| Lịch sử: nhân viên, khách trả lời, khách rút; bảng chỉ ghi thêm | `TestRequest_History` | như trên | ok |
| Câu đếm yêu cầu có index (22.000 dòng, không Seq Scan) | `TestCountRequests_Plan` | `go test -count=1 -v -run '^TestCountRequests_Plan$' ./internal/modules/order/` | `kế hoạch: [Aggregate Bitmap Heap Scan Bitmap Index Scan]` PASS |
| Đếm góp ý theo trạng thái, mã KH của gốc (sau gộp) | `TestFeedback_CountsAndCustomerCode` | `go test -race -count=1 -run '^(TestFeedback_CountsAndCustomerCode\|TestCountFeedback_Plan\|TestListFeedback_FiltersAndPaging)$' ./internal/modules/customer/` | ok |
| Câu đếm góp ý có index (22.000 dòng, không Seq Scan) | `TestCountFeedback_Plan` | `go test -count=1 -v -run '^TestCountFeedback_Plan$' ./internal/modules/customer/` | `kế hoạch: [Aggregate Bitmap Heap Scan Bitmap Index Scan]` PASS |
| API: statuses, customerName, photoCount; 400 khi status+statuses; counts 200/403; history kèm tên nhân viên; want; website không thấy want/history | `TestOrderRequestAPI_MockupData` | `go test -count=1 -race -run '^(TestOrderRequestAPI_MockupData\|TestFeedbackAPI_CountsAndCode\|TestOrderRequestAPI_Status\|TestOrderRequestAPI_WebAndStaffFlow\|TestFeedbackAPI_ListUpdate)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.555s` |
| API góp ý: counts (bán hàng 403, tenant khác 0), customerCode | `TestFeedbackAPI_CountsAndCode` | như trên | ok |
| Màn yêu cầu: mặc định Đang mở (statuses), số trên nhãn, cột Khách/Ảnh, hạn gửi, rỗng Đang mở, số cạnh menu, Muốn đổi sang, Lịch sử | `RequestList.test.tsx`, `RequestDetail.test.tsx` | `npx vitest run src/screens/feedback/ src/screens/order-requests/ src/screens/khach` | `Test Files 15 passed (15) Tests 110 passed (110)` |
| Màn góp ý: số trên nhãn đọc lại sau đổi trạng thái, "Hồ sơ KH000231" | `FeedbackList.test.tsx` "Số trên thanh nhãn (Task 486)" | như trên | như trên |
| Cổng của task (tĩnh + test gói đổi và phụ thuộc, vùng rủi ro order/customer) | ci-task | `ci-task.sh` (lệnh của quy trình) | `CI-TASK XANH (so với điểm tách nhánh aadfabd…)` |
| Kiến trúc (seller filter, SQL, RLS…) | `tests/arch` | `go test -count=1 ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 125.042s` |

## Test bị xóa hoặc tắt

- `RequestList.test.tsx`: bỏ các kiểm cột "Lý do" ("Không vừa size", "Đổi ý", lớp `wrap-any` của mã lý do lạ) vì cột này
  thay bằng cột "Khách" theo mockup (Quyết định 6); `wrap-any` nay kiểm trên tên khách 60 ký tự. Lý do vẫn hiện ở trang
  chi tiết (test của `RequestDetail` giữ nguyên). Không test nào bị tắt.

## Khác kế hoạch

- Task không có mục trong kế hoạch M5 (chỉ dòng M5-b1 ở bảng số task); phạm vi theo lời giao của điều phối viên.
- Không sửa `console/src/app/nav.ts`: số cạnh mục menu dùng `badge` có sẵn của `Screen` (`RequestNavCount.tsx`).
- Thêm `console/src/screens/don/CountLabel.tsx` (nút nhãn có số, dùng chung cho hai màn: jscpd báo lặp) và quy tắc
  `.lbl .n` vào `don.css` (nơi có `.lbl`); `storeSQL` của test customer đọc mọi `internal/store/*.sql.go` (trước chỉ
  `search.sql.go`) để test EXPLAIN của góp ý dùng lại.
- ADR-46 nhóm 7 thêm `ord.order_request_events` (chỉ chèn).
- Gộp main trước khi review: main không đổi so với điểm tách nhánh (aadfabd), không có gì để gộp.

## E2E viết sẵn, chưa chạy

- `console/e2e/order-requests.spec.ts` (sửa dữ liệu giả: tên khách 60 ký tự, số ảnh, hạn gửi trả, biến thể muốn đổi,
  lịch sử, route `order-requests/counts`; danh sách bấm "Tất cả" vì mặc định là "Đang mở"): "danh sách: đủ trạng thái
  và loại…", "chi tiết đổi hàng…", "Đã xác nhận đổi…", "hộp Từ chối…", "danh sách và chi tiết đã đóng… 360". Chưa
  chạy (chủ shop 24/11: không chạy Playwright trong lúc làm task). Màn Góp ý chưa có spec Playwright.

## Việc phát hiện thêm

- Cờ "Tin mới" của mockup A chưa làm (Quyết định 5): cần trạng thái đã đọc theo nhân viên.
- Đầu trang chi tiết của mockup B ("đơn giao thành công 18/11, còn 4 ngày trong hạn đổi 7 ngày") và dòng "Giao thành
  công … (Viettel Post)" của thẻ Đơn và khách chưa có API (ngày giao, hạn đổi chỉ có ở phía website).
- Góp ý "Không gắn hồ sơ (lý do)" của mockup vẫn thiếu lý do (Task 461 Quyết định 3) — API không lưu lý do.
- Màn Góp ý chưa có spec Playwright (so ảnh 390/1512) như màn Yêu cầu.

## Reviewer

Kết luận: ĐẠT (24/11, agent reviewer trên `git diff --cached`, sau ci-task XANH).

- Không có phát hiện phải sửa. Reviewer đã soát: mọi lần đổi trạng thái đi qua `moveRequest` (3 chỗ gọi) nên đều ghi
  lịch sử; website không nhận `want`, `history`; đọc module khác qua view công bố (`security_invoker`, RLS lọc tenant);
  migration 0528 đúng luật (RLS, khóa ngoại ghép, chỉ ghi thêm); hai câu đếm dùng index có sẵn (EXPLAIN bằng crm_app);
  route mới có `x-permission` và test 403; không log mới.
- Ghi chú không bắt buộc: mục Ảnh hưởng nêu thừa `FeedbackPanel.tsx` (không đổi), thiếu `models.go` sinh lại và
  `OpenRequestStatuses`/`RequestStatuses` (chỉ thêm); kiểm cột "Lý do" bị bỏ do đổi cột (đã ghi ở "Test bị xóa hoặc
  tắt"); `TestOrderRequestAPI_MockupData` đọc `h[0]`, `h[1]` trước khi kiểm độ dài (vẫn đỏ đúng lúc);
  `CountOrderRequests` không lọc người bán — đúng khi yêu cầu chỉ có trên đơn web của người bán mặc định, phải lọc nếu
  sau này khác.
