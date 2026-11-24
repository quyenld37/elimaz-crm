# Task 207 — Màn Yêu cầu về đơn hàng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 207, M5-24, làn L4). Loại việc (quy trình mục 3): A (màn hình). Mockup: `docs/mockups/m5/order-requests.html`, hành vi HV01–HV25 chủ shop OK 21/11 (chat).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ sửa `console/` và tài liệu, không chạm mã Go.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn gọi `listOrderRequests`, `getOrderRequest`,
  `replyOrderRequest`, `setOrderRequestStatus` (Task 195) và đọc thêm `getOrder` (M2, `order.read`) để hiện khách,
  tổng tiền, vận đơn hiện hành. oasdiff: không chênh.
- Chỗ dùng trên màn hình: thư mục mới `console/src/screens/order-requests/` (route.tsx tự đăng ký theo
  `src/app/loadScreens.ts`, không sửa `main.tsx`); mục menu "Yêu cầu về đơn hàng" ở nhóm "Đơn hàng" có sẵn
  (`/don/yeu-cau`, trang ẩn `/don/yeu-cau/:id`). Mã chạy của màn khác không đổi; mã test dùng chung có đổi: `app/screenTestkit.tsx` thêm `stubApi`, `page`, `emptyWrites`, `meReply`, `ORDER_PAGE`, `WriteQueue` (dời từ `ma-giam-gia/couponTestkit.tsx`, tệp này đổi sang dùng lại). Nhập lại
  `don/don.css`, `don/orderData.ts` (`usePerms`, `entityMeta`), `staff/staffData.tsx` (`Dialog`),
  `inventory/Balances.tsx` (`useDebounced`), `ma-giam-gia/MoreBar.tsx`, `app/idempotency.ts`, `app/time.ts`.
  `/don/yeu-cau` không bị `/don/:id` (order 900) giành: React Router xếp đoạn tĩnh trước, `currentScreen` theo order.
- Câu sqlc, bảng, pha migration: không có.
- Tài liệu: `docs/mockups/m5/README.md` cột "Chủ shop OK" HV01–HV25 → "OK chủ shop 21/11 (chat)" (theo việc treo).
- Test sẽ chạy: `cd console && npx vitest run src/screens/order-requests` (test mới), `src/app/screens.test.tsx`
  (đăng ký màn), `npx tsc --noEmit`, `npx eslint`, `make jscpd`; ci-task theo phạm vi (không gói Go nào đổi).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục console
  (`useWriteAction`, `unwrap`/`errorMessage`, `queryKey`/`onRealtimeChanged`, `shortTime`, `NavCount`); tìm
  "lblbar", "MoreBar", "Dialog", "useDebounced", "usePerms", "img-ph", ".msg" trong `console/src`.
- Dùng lại: `useWriteAction` (khóa chống trùng, tự gửi lại khi bận), `Dialog` của staffData (hộp `<dialog>` như mọi
  màn), `MoreBar` (Tải thêm theo `nextCursor`), `useDebounced`/`SEARCH_DELAY_MS`, `usePerms`, `entityMeta`
  (báo hiệu chỉ tải lại đúng yêu cầu đang xem), lớp `.lblbar/.lbl/.scroll-x/.kv` của `don.css`, `shortTime`.
- Viết mới vì: dòng tin hai phía (`.msg*`) và ô ảnh riêng tư là thành phần mới của mockup M5 (`m5.css`), chưa có ở
  console; chép đúng các luật của `m5.css` / `m1.css` (`.imgs`, `.img-ph` — `products.css` có bản riêng gắn với kéo
  thả ảnh sản phẩm, không nhập cả tệp đó) vào `order-requests.css`. Luật chuyển trạng thái phía màn hình
  (`requestRules.ts`) chỉ để chọn nút; máy chủ vẫn kiểm (422 `invalid_transition`).
- Danh sách ảo (TanStack Virtual, ADR-34): không dùng — ngưỡng đổi của `docs/chon-cong-cu-console.md` ("dưới vài trăm
  dòng có phân trang → không cần"); danh sách tải 50 dòng mỗi lần theo con trỏ, test 200 dòng.

## Soát thiết kế

- 5.1–5.3: màn chỉ gọi API có sẵn của module `order`; không thêm module, không đọc bảng.
- 5.4–5.6: không áp dụng: không thêm đường ghi máy chủ; màn dùng `replyOrderRequest`, `setOrderRequestStatus` có sẵn.
- 5.7, 5.8: không áp dụng (không SQL, không handler).
- 5.9: mọi lần ghi qua `useWriteAction` (Idempotency-Key sinh khi bấm, giữ nguyên khi tự gửi lại lúc 503/mạng); quyền
  ghi theo `order.requests.manage`, đọc `order.requests.read`; máy chủ vẫn kiểm.
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện mới; `@tanstack/react-virtual` đã có nhưng không dùng).
- 5.12: theo mẫu màn Đơn (danh sách có thanh nhãn, chi tiết trang riêng, hộp thoại cho thao tác đóng).
- 6.1: đổi trạng thái gửi `version` đã đọc; 409 `version_conflict` → báo, tải lại yêu cầu, giữ chữ đang gõ, không tự
  gửi lại. Trả lời không đổi version (máy chủ), nên không gửi version.
- 6.2: chống bấm hai lần: nút khóa khi đang gửi (`pending`), khóa chống trùng giữ trong `useWriteAction`.
- 6.3–6.9: không áp dụng (không khóa, transaction, hàng đợi phía màn).
- 6.10: tiền chỉ hiển thị (tổng đơn, int64 đồng từ API).
- 6.11: không áp dụng (không chạm vùng rủi ro cao).
- 6.12: nội dung tin, lời nhắn của khách hiện dạng văn bản thuần (React tự thoát; không `dangerouslySetInnerHTML`);
  link ảnh chỉ nhận `http(s):`, mở tab mới `rel="noopener noreferrer"`; màn không ghi log, không lưu gì vào
  `localStorage`.
- 6.13: thiếu `order.requests.manage` → không có ô trả lời, không đổi trạng thái (câu "chỉ có quyền xem"); thiếu
  `order.read` → không gọi `getOrder`, mã đơn không thành liên kết.

## Quyết định thay chủ shop

1. **Chỉ vẽ dữ liệu API thật sự trả** (luật CLAUDE.md "chỉ vẽ hành vi máy chủ thật sự làm"). Mockup có vài thứ API
   của Task 195 chưa trả, màn bỏ thay vì bịa:
   - Danh sách: không có cột "Khách" và "Ảnh", không có số đếm trên nhãn trạng thái, không có "Tin mới"
     (`OrderRequestSummary` không có tên khách, số ảnh, số đếm). Cột "Lý do" thay chỗ (lý do cấp yêu cầu bằng chữ).
   - Không có nhãn "Đang mở": API lọc một `status`; gộp nhiều trạng thái phải đọc hết rồi lọc ở trình duyệt (sai khi
     còn trang sau). Mặc định "Tất cả" (mới nhất trước) để không giấu yêu cầu nào. Phương án khác: mặc định
     "Đã tiếp nhận" — giấu yêu cầu đang kiểm tra/cần bổ sung.
   - Chi tiết: tên khách, tổng tiền, trạng thái đơn đọc từ `getOrder` (người có `order.read`); "Muốn đổi sang" chỉ ghi
     "Biến thể khác (xem ở trang đơn)" vì API chỉ trả `wantVariantId` (không tên, không số còn); không có thẻ
     "Lịch sử" riêng (API không có sự kiện), lịch sử là dòng tin + thời điểm gửi/đóng.
   - Không có số đếm cạnh mục menu (HV02): không có API đếm; thuộc Task 462 (menu).
   Mọi chỗ ghi ở "Việc phát hiện thêm" để task máy chủ bổ sung. Dễ đảo ngược: thêm cột khi API có trường.
2. **Yêu cầu đã đóng không có ô viết tin** (đúng mockup E), dù máy chủ vẫn nhận ghi chú nội bộ. Phương án khác: cho
   ghi chú nội bộ sau khi đóng — thêm sau nếu chủ shop cần.
3. **"Đang kiểm tra", "Đã nhận hàng gửi về", "Đã xác nhận đổi" lưu ngay bằng nút "Lưu xử lý"** (Đã xác nhận đổi có
   khung "Hướng dẫn gửi hàng về" ngay trong thẻ, mockup C); "Cần bổ sung", "Từ chối", "Hoàn tất" mở hộp riêng (HV08,
   mockup D).
4. **Không dùng danh sách ảo** (xem Tái dùng): 50 dòng mỗi lần, "Tải thêm" khi còn `nextCursor`.
5. **Đường dẫn** `/don/yeu-cau`, `/don/yeu-cau/:id` như README mockup đề xuất.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Lọc theo trạng thái (thanh nhãn), loại, tìm gửi đúng tham số API | `RequestList.test.tsx` › "lọc trạng thái bằng thanh nhãn, lọc loại, tìm gửi status, kind, q lên API" | `cd console && npx vitest run src/screens/order-requests --reporter=verbose` | ✓ (4 tệp, 34 test qua) |
| Trả lời "Khách thấy" / "Ghi chú nội bộ" gửi đúng cờ `visibleToCustomer`, có Idempotency-Key | `RequestDetail.test.tsx` › "Khách thấy gửi visibleToCustomer true, Ghi chú nội bộ gửi false; mỗi lần có Idempotency-Key; dưới 5 ký tự không gửi được" | như trên | ✓ |
| Nội dung có `<script>` hiện nguyên chữ | `RequestDetail.test.tsx` › "lời nhắn có <script> hiện nguyên chữ, không thành thẻ; …" | như trên | ✓ |
| Đổi trạng thái gặp 409 → báo, tải lại, giữ chữ, không tự gửi lại | `RequestDetail.test.tsx` › "409: báo người khác vừa đổi, tải lại yêu cầu, giữ lời nhắn đang gõ, không tự gửi lại" | như trên | ✓ |
| Danh sách 200 yêu cầu: 50 dòng mỗi lần theo con trỏ, còn `nextCursor` thì còn "Tải thêm" (không danh sách ảo, Quyết định 4) | `RequestList.test.tsx` › "200 yêu cầu: tải 50 dòng mỗi lần theo con trỏ; còn nextCursor thì còn nút Tải thêm" | như trên | ✓ |
| Báo hiệu `order_request.changed` làm mới danh sách; chi tiết chỉ tải lại đúng yêu cầu | `RequestList.test.tsx` › "báo hiệu order_request.changed tải lại danh sách…"; `RequestDetail.test.tsx` › "báo hiệu của đúng yêu cầu này tải lại; của yêu cầu khác thì không" | như trên | ✓ |
| Ảnh mở bằng link ký ở tab mới, không nhúng; link không phải http(s) không mở; ảnh đang xử lý/từ chối/mất hiện riêng | `RequestDetail.test.tsx` › "ảnh: sẵn sàng mở tab mới bằng link ký; …" | như trên | ✓ |
| Trạng thái kế tiếp khớp sơ đồ máy chủ (`requestTransitions`, `order/requests.go`); hộp Cần bổ sung/Từ chối/Hoàn tất; Đã xác nhận đổi kiểm hướng dẫn gửi trả | `RequestDetail.test.tsx` nhóm "Đổi trạng thái" (9 ca); `requestRules.test.ts` (5 ca: đếm rune, trạng thái kế tiếp, ngày giờ VN) | như trên | ✓ |
| Quyền: chỉ xem, thiếu `order.read`, đã đóng, khách hủy, 403, rỗng | `RequestDetail.test.tsx` nhóm "Quyền và trạng thái đóng" (7 ca, gồm 404 "yêu cầu không có"); `RequestList.test.tsx` "lỗi 403…", "rỗng…" | như trên | ✓ |
| Màn tự đăng ký đúng path, nhóm "Đơn hàng", quyền | `route.test.tsx`; `src/app/screens.test.tsx` | `npx vitest run src/screens/order-requests src/app/screens.test.tsx src/screens/ma-giam-gia` | ✓ 9 tệp, 121 test (gồm màn Mã giảm giá sau khi dời phần API giả sang `screenTestkit`) |
| Kiểu, lint | — | `cd console && npx tsc --noEmit`; `npx eslint src/screens/order-requests src/app/screenTestkit.tsx src/screens/ma-giam-gia/couponTestkit.tsx e2e/order-requests.spec.ts` | không lỗi |
| Không mã lặp; luật kiến trúc | — | `make jscpd`; `go test -count=1 ./tests/arch/` | "Found 0 clones."; `ok … tests/arch 79.814s` |
| Cổng kiểm của task (tĩnh, console-check, arch) | — | `ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh 0e285c0…)`; console-check 44 giây, gitleaks "no leaks found" |
| 390 không tràn ngang | E2E viết sẵn (xem dưới) | chưa chạy (chủ shop 24/11) | — |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch ghi sửa `console/src/main.tsx` và menu: không cần — màn tự đăng ký qua `route.tsx` (`src/app/loadScreens.ts`), menu nhóm "Đơn hàng" sinh từ `group`.
- Để không lặp mã test (jscpd): phần API giả dùng chung (`stubApi`, `page`, `emptyWrites`, `meReply`, `ORDER_PAGE`, kiểu `WriteQueue`) dời từ `ma-giam-gia/couponTestkit.tsx` sang `app/screenTestkit.tsx`; màn Mã giảm giá đổi sang dùng lại, hành vi test không đổi (test của màn đó vẫn qua). Chỉ là mã test nên để chung commit.
- "So ảnh với mockup" (Step 4) và 390 không tràn ngang: để ở Playwright viết sẵn, chưa chạy (chủ shop 24/11).

## E2E viết sẵn, chưa chạy

`console/e2e/order-requests.spec.ts` (390 và 1512 px, không tràn ngang):
- "danh sách: đủ trạng thái và loại, còn trang sau, không tràn ngang"
- "chi tiết đổi hàng: tên dài, 3 ảnh, tin hai phía, thẻ xử lý không tràn ngang"
- "Đã xác nhận đổi: khung hướng dẫn gửi trả không tràn ngang"
- "hộp Từ chối không tràn ngang"
- "danh sách và chi tiết đã đóng không tràn ngang với chữ dài"

## Việc phát hiện thêm

- API `listOrderRequests` (Task 195) chưa trả tên khách, số ảnh, số đếm theo trạng thái, cờ "tin mới" → danh sách thiếu cột Khách, Ảnh, số đếm trên nhãn, "Tin mới" của mockup A (Quyết định 1). Cần task máy chủ bổ sung trường.
- API chưa cho lọc nhiều trạng thái (nhãn "Đang mở" của mockup).
- `OrderRequestLine` chỉ có `wantVariantId`: thiếu tên, số còn của biến thể muốn đổi sang (HV05).
- Không có sự kiện lịch sử của yêu cầu (thẻ "Lịch sử" của mockup).
- Số đếm yêu cầu đang mở cạnh mục menu (HV02) cần API đếm; thuộc Task 462 (menu).
- (Reviewer) `StatusPanel.tsx`: gõ "Lời nhắn cho khách" rồi chọn Từ chối/Hoàn tất thì ô lời nhắn ẩn, lời nhắn không gửi và bị xóa khi lưu xong, không báo. Không sai dữ liệu (máy chủ chỉ nhận lý do/mã vận đơn); có thể gửi kèm tin khi đóng hoặc cảnh báo — chờ chủ shop chọn.

## Reviewer

Kết luận: **ĐẠT** (reviewer 24/11, trên `git diff --cached` sau khi gộp main 0e285c0).

- Không có phát hiện phải sửa. Đã đối chiếu: `nextStatuses` khớp `requestTransitions` (`order/requests.go:80-86`), "Đã xác nhận đổi" chỉ cho đổi hàng; giới hạn ô nhập đếm rune như máy chủ; thân gửi (`shipBack`, `outcome`) khớp `statusParams`/`outcomeOf`; Idempotency-Key qua `useWriteAction`, 409 không tự gửi lại; nội dung khách là văn bản thuần, link ảnh chỉ http(s) với `noopener noreferrer`; thứ tự route `/don/yeu-cau` trước `/don/:id`.
- Ghi chú không bắt buộc đã xử lý: sửa số ca (9 ca "Đổi trạng thái"; phía chi tiết là ca 404), nêu `screenTestkit.tsx`/`couponTestkit.tsx` trong mục Ảnh hưởng; chuyện mất lời nhắn khi chọn Từ chối/Hoàn tất ghi vào "Việc phát hiện thêm".
