# Task 85 — M2-28: Màn Đơn — danh sách, chi tiết, sửa, hủy, trả, đổi

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 85 = M2-28). Loại việc (quy trình mục 3): A (màn hình). Mockup đã duyệt (Q-X8, Task 83): `docs/mockups/m2/don-danh-sach.html`, `don-chi-tiet.html`, `don-trung.html`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ đổi `console/` (TypeScript).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`, client sinh không đổi. Màn chỉ GỌI operation có sẵn: `listOrders`, `getOrder`, `updateOrder`, `confirmOrder`, `approveOrderDiscount`, `setOrderPending`, `cancelOrder`, `getOrderTimeline`, `addOrderNote`, `listOrderDuplicates`, `resolveOrderDuplicate`, `createReturn`, `getReturn`, `receiveReturn`, `closeReturn`, `cancelReturn`, `listTrafficSources`, `getOrderSettings`, `getMe`, `listStaff` (chỉ khi có `identity.users.manage`).
- Chỗ dùng trên màn hình: thư mục mới `console/src/screens/don/` (route tự đăng ký `/don`, `/don/trung`, `/don/:id`; Q-T57-8, không sửa `router.tsx`). Sửa có chủ đích:
  - `console/src/app/queryClient.ts`: `onRealtimeChanged` thêm hai lựa chọn theo `meta` của truy vấn — `realtimeEntity: <id>` (chỉ tải lại khi báo hiệu đúng đối tượng đó) và `realtimeManual: true` (màn tự xử lý báo hiệu, `onRealtimeResync` vẫn tải lại); thêm `subscribeChanged(qc, fn)` để màn nghe báo hiệu. Truy vấn không có `meta` giữ nguyên hành vi cũ (mọi chỗ dùng hiện có: `shell/App.tsx` và màn M1, M2 — không đổi). Export thêm qua `console/src/shared/index.ts`; danh mục `docs/danh-muc-dung-chung.md` sinh lại bằng `make catalog`.
  - `console/src/screens/len-don/OrderForm.tsx` (+ `orderRules.ts`: `FormLine.itemId`, `addressOf`, `buildUpdate`, tách `shipAddressOf` dùng chung cho `buildRequest`; `len-don.css`: lớp `fieldset.lock`): thêm chế độ sửa (prop `edit`, `onCreated` thành tùy chọn — chỗ dùng: `OrderPage.tsx`, `orderTestkit.tsx`, export `OrderFormProps` ở `shared/index.ts`, đều vẫn truyền `onCreated`) theo luật danh mục "không viết form lên đơn thứ hai (sửa đơn): dùng OrderForm"; chế độ tạo giữ nguyên (276 ca có sẵn của `src/screens/len-don` phải vẫn qua).
  - `console/src/screens/staff/staffData.tsx`: `Dialog` thêm prop tùy chọn `className` (chỗ dùng: staff, roles, inventory, imports — không truyền, hành vi không đổi).
  - `console/src/app/errors.ts`: chỉ thêm dòng nếu cần.
  - Liên kết có sẵn của Task 84 (`/don/:id`, `/don/trung`) nay có màn.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run src/screens/don src/screens/len-don src/app` (mới: `labels.test.ts`, `returnRules.test.ts`, `OrderList.test.tsx`, `OrderDetail.test.tsx`, `ReturnDialog.test.tsx`, `DuplicatePairs.test.tsx`, `route.test.tsx`; sửa: `queryClient.test.ts`, `orderRules.test.ts` thêm 5 ca `buildUpdate`; ca sửa đơn qua form nằm ở `OrderDetail.test.tsx`), `npx tsc --noEmit`, `npx eslint .`, Playwright chỉ `e2e/don.spec.ts` (mới, chụp 1512×767, 390×844, 360); ci-task một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` phần console (useWriteAction, unwrap, errorMessage, ApiError, queryKey, onRealtimeChanged, hasPermission, shortTime, OrderForm và luật "không viết form lên đơn thứ hai… sửa đơn: dùng OrderForm", "tiền hiện trước chỉ tính bằng pricing.ts"); `screens/staff/staffData.tsx` (`Dialog` theo mockup), `screens/products/productRules.ts` (`formatMoney`), `screens/inventory/Balances.tsx` (`useDebounced`), `screens/len-don/{orderRules.ts (displayPhone, PENDING_TEXT, couponReasonText), VariantPicker.tsx, pricing.ts}`; `docs/mockups/m2/m2.css`.
- Dùng lại / viết mới vì: dùng lại `OrderForm` (thêm chế độ sửa, không viết form thứ hai), `useWriteAction` cho mọi đường ghi, `Dialog` của staff (không viết hộp thoại thứ ba), `formatMoney`, `useDebounced`, `displayPhone`, `VariantPicker` (dòng thay thế của phiếu đổi), `shortTime`. Viết mới: `labels.ts` (chữ của 16 nhãn, lý do hủy, lý do chờ, loại và trạng thái phiếu trả; câu lý do mã dùng lại `COUPON_REASON_MESSAGES` qua `couponReasonText`); `refundFor` trong `returnRules.ts` — bản TypeScript của `pricing.RefundFor` + cách tính "đã trả, đã hoàn trước" của `priceReturnLines` (máy chủ là số cuối; kiểm bằng cùng các ca của test Go `returns_test`); cơ chế `realtimeEntity`/`subscribeChanged` trong `queryClient.ts` (chưa có cách tải lại đúng một đối tượng). CSS: chép lớp mockup màn cần (`lblbar`, `lbl`, `toggle`, `acts`, `tl`, `pair-c`, `split`, `side-c`, `kv`, `mini`, `kpis`…) vào `don.css`, chỉ biến có sẵn.

## Soát thiết kế

- 5.1–5.8, 5.10: không áp dụng — không đổi máy chủ, database, module, sự kiện; màn chỉ gọi API có sẵn.
- 5.9 Đường ghi: mọi ghi (sửa, xác nhận, duyệt, chờ, hủy, ghi chú, phiếu trả, nhận, đóng, hủy phiếu, xử lý đơn trùng) qua `useWriteAction` (Idempotency-Key, gửi lại cùng khóa); mã đối tượng và `version` nằm trong `body` của `run` (luật của useWriteAction).
- 5.11 Thư viện: không thêm.
- 5.12 Theo mẫu: màn tự đăng ký như `screens/len-don`; test vitest + testing-library với fetch giả như `len-don/orderTestkit.tsx`.
- 6.1, 6.3–6.9, 6.11: không áp dụng (không chạm database, khóa, việc nền).
- 6.2 Gửi lại / đồng thời: đổi trạng thái và sửa luôn gửi `version` đã đọc; 409 `version_conflict` → giữ phần đang gõ, hiện câu báo và nút "Tải lại đơn" (không tự ghi đè). Báo hiệu `order.changed` cho đơn đang sửa không làm mất phần đang gõ (form chỉ lấy giá trị đầu khi mở).
- 6.10 Tiền: số nguyên đồng; tiền hoàn dự kiến tính bằng `refundFor` (BigInt cho nhân chia như `mulDiv` của Go), số máy chủ trả là số cuối; tổng của đơn sửa tính bằng `pricing.ts` có sẵn.
- 6.12 Dữ liệu cá nhân: màn không ghi log; lịch sử thao tác chỉ hiện tên trường đã đổi (máy chủ không trả giá trị).
- 6.13 Quyền: route cần `order.read`; nút hiện theo `editScope` và quyền trong `/api/v1/me` (`order.update`, `order.confirm`, `order.discount.approve`, `order.cancel`, `order.return`, `order.return.receive`, `order.duplicates.resolve`); máy chủ vẫn chặn (403 hiện câu của `errorMessage`).

## Bằng chứng

Thứ tự: viết `labels.test.ts`, `returnRules.test.ts`, `OrderList.test.tsx`, `OrderDetail.test.tsx`, `ReturnDialog.test.tsx`, `DuplicatePairs.test.tsx`, `route.test.tsx`, ca mới của `queryClient.test.ts` trước mã; chạy `npx vitest run src/screens/don src/app/queryClient.test.ts` → FAIL (7 tệp không nạp được mô-đun, 3 ca queryClient: `subscribeChanged is not a function`…), rồi viết mã. Ca `buildUpdate` của `orderRules.test.ts` cũng viết trước (FAIL `addressOf is not a function`). Ca "sau thao tác: đọc lại đơn" viết khi tự soát, FAIL (30 s, không đọc lại) rồi sửa.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mọi `LabelKey` có chữ, đúng thứ tự `orderview.Labels()`; mọi enum (OrderStatus, PendingReason, CancelReason, ReturnKind, ReturnStatus, CouponRejectReason, tình trạng nhận) có chữ — đọc enum từ `openapi.yaml` | `labels.test.ts` (7 ca: "16 nhãn, đúng thứ tự…", "mọi giá trị của OrderStatus…", "mọi lý do từ chối mã…", "5 lý do nhân viên chọn…", "lý do trong lịch sử…") | `cd console && npx vitest run src/screens/don/labels.test.ts` | `Tests 7 passed (7)` |
| Báo hiệu cho đơn X → chỉ đơn X được tải lại | `OrderList.test.tsx` › "báo hiệu cho đơn X đang có trong danh sách: chỉ tải lại đơn X (tìm theo mã), không tải lại cả trang" (đúng 1 lần listOrders `q=DH0001236`, 0 getOrder, dòng tô nền); "báo hiệu cho đơn không có trong danh sách…"; `OrderDetail.test.tsx` › "báo hiệu cho đơn khác không tải lại đơn này; cho đơn này thì tải lại đơn và lịch sử"; `queryClient.test.ts` › "meta realtimeEntity…", "meta realtimeManual…", "subscribeChanged…" | `npx vitest run src/screens/don/OrderList.test.tsx src/screens/don/OrderDetail.test.tsx src/app/queryClient.test.ts` | `OrderList 8 passed`, `OrderDetail 18 passed`, `queryClient 10 passed` |
| Tự tải lại 20 giây | `OrderList.test.tsx` › "tự tải lại cả trang mỗi 20 giây (khi không có báo hiệu)" | như trên | qua |
| 409 khi sửa → câu báo, không mất dữ liệu đang gõ; "Tải lại đơn" mở bản mới | `OrderDetail.test.tsx` › "409 khi sửa: câu báo, phần đang gõ còn nguyên; Tải lại đơn mở bản mới"; "báo hiệu cho chính đơn này khi đang sửa: không mất phần đang gõ"; "409 khi xác nhận…" | như trên | qua |
| Sửa và chuyển trạng thái gửi `version`; sửa chỉ gửi trường đã đổi trong editScope | `OrderDetail.test.tsx` › "đơn mới: Xác nhận và Chờ khách gửi version…", "sửa chỉ gửi trường đã đổi, cùng version…", "chưa gửi hãng: 5 lý do…"; `orderRules.test.ts` › 5 ca `buildUpdate` | `npx vitest run src/screens/len-don/orderRules.test.ts` | `31 passed` |
| Sau thao tác, màn đọc lại đơn (thân trả về của lệnh ghi thiếu editScope, khách) | `OrderDetail.test.tsx` › "sau thao tác: đọc lại đơn…" | như trên | qua |
| Nút hiện theo `editScope` và quyền; "Nhận hàng trả" ẩn khi không có `order.return.receive` | `OrderDetail.test.tsx` › "chờ duyệt giảm giá, người xem có quyền duyệt…", "chờ duyệt, người xem không có quyền duyệt…", "đơn nháp chờ duyệt…", "thành công, chỉ sửa được phụ trách…", "không có quyền sửa, hủy…", "thiếu order.return.receive: không có nút Nhận hàng trả; có thì có" | như trên | qua |
| Hộp trả hàng không cho chọn quá số còn trả được; tiền hoàn dự kiến khớp máy chủ | `ReturnDialog.test.tsx` (5 ca); `returnRules.test.ts` (9 ca, gồm ca làm tròn 500001 → 250000 rồi 250001 của Task M2-22, dòng "thiếu", số gần 2^53) | `npx vitest run src/screens/don/ReturnDialog.test.tsx src/screens/don/returnRules.test.ts` | `5 passed`, `9 passed` |
| Đơn trùng: cặp, sản phẩm chung tô nền, Hủy cần xác nhận, Không trùng có thông báo, không quyền không nút | `DuplicatePairs.test.tsx` (4 ca) | `npx vitest run src/screens/don/DuplicatePairs.test.tsx` | `4 passed` |
| Màn tự đăng ký `/don`, `/don/trung`, `/don/:id` | `route.test.tsx`; `src/app/screens.test.tsx` (có sẵn) | `npx vitest run src/screens/don src/screens/len-don src/app` | `Test Files 16 passed`, `Tests 403 passed` (gồm 276 ca của Lên đơn có sẵn: chế độ tạo không đổi) |
| Typecheck, lint, định dạng | — | `npx tsc --noEmit`, `npx eslint src e2e`, `npx prettier --check src e2e` | không lỗi |
| Bố cục 1512×767, 390×844, 360 không tràn ngang; ảnh so mockup | `e2e/don.spec.ts` (11 test); `e2e/shell.spec.ts`, `e2e/len-don.spec.ts` (menu, form dùng chung) | `CRM_E2E_PORT=5185 npx playwright test e2e/don.spec.ts e2e/shell.spec.ts e2e/len-don.spec.ts --project=chromium` | `20 passed`; ảnh `console/test-results/don-{danh-sach,chi-tiet,chi-tiet-b,trung,tra,danh-sach-trong}-{1512,390}.png`, `don-*-360.png` |
| Đọc phiếu trả lỗi: câu lỗi riêng, không treo ở "đang tải" (Bẫy đã gặp: ca lỗi API có trạng thái riêng) | `OrderDetail.test.tsx` › "đọc phiếu trả lỗi: câu lỗi riêng, nút tạo phiếu mờ…" (viết trước, FAIL, rồi sửa `useReturns`, `ReturnsCard`) | `npx vitest run src/screens/don` | `Tests 53 passed (53)` |
| Sửa đơn chưa có nguồn: không tự gán nguồn mặc định; chưa đổi gì thì không lưu (phát hiện của reviewer lượt 1) | `OrderDetail.test.tsx` › "đơn chưa có nguồn (đơn đổi hàng do máy chủ tạo)…" (viết trước, FAIL, rồi sửa `OrderForm.tsx`); `orderRules.test.ts` › "đơn chưa có nguồn, ô nguồn để trống → không gửi nguồn" | `npx vitest run src/screens/don src/screens/len-don src/app` | `Test Files 16 passed`, `Tests 406 passed` (OrderDetail 20 ca, orderRules 32 ca) |
| Cổng kiểm của task | — | ci-task sau khi gộp main (8d43c64). Lần 1: đỏ jscpd 2 đoạn trùng e2e/testkit với Lên đơn — đã viết lại phần của màn này; console-check đỏ đúng ca test mới đang đỏ lúc chạy — đã sửa. Lần 2 | "Test Files 57 passed (57) / Tests 769 passed (769)", "CI-TASK XANH (so với điểm tách nhánh 8d43c64…)". Sau đó chỉ thêm ca "đọc phiếu trả lỗi" (+ sửa nhỏ ở `OrderDetail.tsx`, `ReturnsCard.tsx`): chạy lại tsc, eslint, vitest của `src/screens/don` — xanh. Lần 3 (sau sửa của reviewer lượt 1): "Test Files 57 passed (57) / Tests 772 passed (772)", "CI-TASK XANH (so với điểm tách nhánh 8d43c64…)" |

So ảnh với mockup (`docs/mockups/m2/png/don-*.png`): bố cục, thanh nhãn, bảng, thẻ, hộp thoại trùng thứ tự và cỡ. Chỗ lệch có lý do — xem "Cần chủ shop xem".

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Đường tệp: kế hoạch ghi `console/src/features/orders/…` và `console/src/screens/don/route.tsx`; theo Q-T57-8 (như Task 84) mọi tệp ở `console/src/screens/don/`. Thêm `orderData.ts` (khóa truy vấn, quyền), `returnRules.ts` (tiền hoàn), `ReturnsCard.tsx`, `donTestkit.tsx`, `don.css`.
- `EditOrderDialog` không phải form riêng: mở `OrderForm` (chế độ sửa mới thêm) trong hộp thoại — luật danh mục "không viết form lên đơn thứ hai (… sửa đơn): dùng OrderForm".
- "Danh sách nghe báo hiệu và tải lại đúng đơn" cần cách mới ở lớp chung: `onRealtimeChanged` trước đây tải lại mọi truy vấn của chủ đề; thêm `meta` (`realtimeEntity`, `realtimeManual`) và `subscribeChanged` (truy vấn không có meta giữ hành vi cũ).
- `Dialog` của `screens/staff/staffData.tsx` (đã dùng chung cho nhiều màn) thêm prop `className` (hộp rộng cho trả/đổi, sửa đơn).

## Quyết định thay chủ shop

1. Thanh nhãn đúng mockup: 10 nhãn lọc được ở M2 + "Tất cả"; không có nhãn vận chuyển (kể cả Đang/Đã đóng gói dù máy chủ lọc được — mockup ghi "thêm ở M3"). Mặc định nhãn "Mới" và "7 ngày qua" (mockup trạng thái A). Dễ đảo: một hằng trong `labels.ts`, `OrderList.tsx`.
2. Không vẽ số đếm trên nhãn: API không có số đếm theo nhãn (vẽ số sai là trái luật "chỉ vẽ điều máy chủ làm"). Ghi ở Cần chủ shop xem.
3. Lọc "Kênh" bỏ, lọc ngày không có "Chọn khoảng…" (thay bằng "Mọi ngày"), lọc "Người phụ trách" có "Mọi người", "Của tôi" và cả danh sách chỉ khi người xem có `identity.users.manage` (API danh sách nhân viên đòi quyền này); không có "Chưa giao ai" (API `careBy` chỉ nhận một mã người). Ô "Người phụ trách" ở chi tiết: người hiện tại, "Tôi", và cả danh sách khi có quyền trên.
4. Báo hiệu cho đơn chưa có trong danh sách: đọc đơn đó, nhãn khớp nhãn đang lọc (hoặc "Tất cả") thì tải lại trang đầu; không khớp thì để lần tự tải lại 20 giây. Phương án khác: tải lại cả trang mỗi báo hiệu (trái kế hoạch).
5. Tự tải lại 20 giây luôn bật (màn không biết trạng thái kết nối của khung); khi kết nối tốt, lần resync 20 giây của lớp realtime cũng tải lại, nên có thể hai lần mỗi 20 giây. An toàn, dễ đổi.
6. "Tạo phiếu trả/đổi" đặt ở hàng nút đầu trang (cùng các nút khác); thẻ Phiếu trả đặt ở cột trái dưới Sản phẩm (cột phải hẹp cắt cột tiền).
7. Cảnh báo "quá N ngày đổi trả": ngày giao lấy từ mục đổi trạng thái sang "Thành công" trong lịch sử thao tác (Order không có trường ngày giao); N là `returnWarningDays` của Cài đặt đơn.
8. Đổi hàng: chọn hàng đổi bằng ô tìm sản phẩm của Lên đơn (`VariantPicker`), mỗi lần chọn thêm 1; giao tới địa chỉ đơn gốc, không phí ship riêng (máy chủ mặc định như vậy).
9. Sửa đơn giữ mã cũ thì không kiểm lại mã (đã dùng cho chính đơn này, `checkCoupon` sẽ báo hết lượt nhầm); hiện số giảm hiện tại, máy chủ tính lại khi lưu.

## Cần chủ shop xem (khác mockup vì API thiếu; không tự thêm API)

1. Số đếm trên nhãn và "Đơn trùng (n)": không có API đếm; nhãn không có số, "Đơn trùng (n)" đếm trang đầu (tối đa 100, quá thì "100+").
2. Lọc kênh và "Chưa giao ai": thiếu trong hợp đồng `listOrders` (kênh có nhưng chưa có danh sách kênh; careBy không nhận "trống").
3. Thẻ khách ở chi tiết không có SĐT của khách (`OrderCustomer` không có SĐT); SĐT người nhận ở thẻ Giao tới.
4. Dòng sản phẩm không có "Đen · M" và "giá hiện hành" (OrderItem chỉ có SKU, tên, giá lúc đặt); đầu trang không có tên kho.
5. Lịch sử sửa đơn ghi "Sản phẩm: đã đổi" (máy chủ chỉ trả tên trường "lines", không số dòng như mockup "Số lượng: đã đổi (2 dòng)").

## Việc phát hiện thêm

- API đếm đơn theo nhãn (cho thanh nhãn) và danh sách nhân viên được giao đơn cho người không quản lý nhân viên — cần task hợp đồng nếu chủ shop muốn đúng mockup.
- `Order` không có ngày giao thành công: màn suy từ lịch sử thao tác; nên thêm `deliveredAt` vào hợp đồng.
- Liên kết "Hồ sơ" (`/khach/:rootId`) trỏ tới màn của Task 86.

## Reviewer

Lượt 1: CẦN SỬA — `OrderForm.tsx` chế độ sửa: đơn chưa có nguồn (đơn thay thế của phiếu đổi, `createReplacement` không đặt nguồn) thì ô nguồn lấy nguồn mặc định, `buildUpdate` luôn gửi `trafficSourceId` (ghi nguồn không ai yêu cầu; chốt "Chưa có thay đổi" không bao giờ bật). Đã sửa: chế độ sửa không lấy nguồn mặc định, ô hiện "Chưa có" tới khi người dùng chọn; thêm hai test. Ghi chú không bắt buộc đã làm luôn: "Tải lại đơn" lỗi mạng hiện câu báo (`EditOrderDialog`); nút "Hủy đơn này" ở Đơn trùng chỉ hiện cho đơn nháp, mới, đã xác nhận (trạng thái `cancel.go` cho hủy). Giữ nguyên: truy vấn đếm "Đơn trùng (n)" tải lại mỗi báo hiệu (đúng, chỉ tốn tải).

Lượt 2: ĐẠT — lỗi lượt 1 đã sửa đúng, có test thật kiểm (`OrderDetail.test.tsx` ca "đơn chưa có nguồn…": fake có nguồn manual nên lỗi cũ sẽ lộ; `orderRules.test.ts` ca "đơn chưa có nguồn, ô nguồn để trống…"); hai sửa thêm không gây lỗi mới (danh sách trạng thái hủy được khớp `cancel.go`); Ảnh hưởng, số ca trong bảng bằng chứng khớp mã; không test bị tắt; mọi đường ghi qua useWriteAction. Ghi chú không bắt buộc: tải lại một dòng bằng `q=<mã>` — máy chủ khớp đúng mã (`classifyQ`), nếu không thấy đơn thì chờ lần tự tải lại 20 giây.

Kết luận: ĐẠT
