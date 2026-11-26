# Task 176 — Khung bên phải — thẻ khách, lên đơn, nhãn, người phụ trách, nhắc việc, gọi điện

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 176, M4-36). Loại việc (quy trình
mục 3): A (màn hình) + một trường hợp đồng nhỏ ở `createOrder` (xem Quyết định 1).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có ký hiệu xuất ra ngoài nào đổi. Trong gói `httpapi`
  (không xuất): `CreateOrder` (handler) thêm bước đọc bối cảnh hội thoại; hàm mới `orderConversationAt` (một chỗ gọi);
  `isOrderRefusal` thêm `conversation.ErrNotFound` (1 chỗ gọi: `CreateOrder`). Mã sinh `gen.CreateOrderRequest` thêm
  trường `ConversationId` (OptUUID; ogen sinh, không sửa tay).
- Gói phụ thuộc ngược (go list): `internal/httpapi` được nhập bởi `cmd/crm`, `internal/httpapi/web` (chỉ build).
  Không đổi module nghiệp vụ nào (`order`, `conversation`, `customer` chỉ được gọi qua public API có sẵn:
  `conversation.OrderContext`, `order.CreateInput{SellerID, Channel, ConversationID, ChannelAccountID}`).
- Operation OpenAPI bị chạm, oasdiff: `createOrder` — thêm trường tùy chọn `conversationId` vào `CreateOrderRequest`
  (mở rộng, không phá vỡ; oasdiff không chặn). Không thêm mã lỗi mới (`not_found`, `mixed_sellers` đã có).
- Chỗ dùng trên màn hình: `console/src/screens/inbox/side-panel/*` (mới, thay màn khung tạm của Task 174);
  `console/src/screens/len-don/OrderForm.tsx` (chỉ thêm prop `conversationId?`, `initialTrafficSourceId?`,
  `edit.confirmAfterSave?`; màn Lên đơn, Sửa đơn của M2 không truyền → hành vi cũ giữ nguyên);
  `console/src/screens/khach/NotesPanel.tsx` (dòng thời gian khách: thêm hai loại mục `conversation`, `call`, C7) —
  dùng bởi `CustomerProfile.tsx`. Client sinh `console/src/api/types.gen.ts` (`make gen`).
- Câu sqlc, bảng, pha migration: không có (không migration, không câu SQL mới).
- Test sẽ chạy: Go `-run '^TestCreateOrder_FromConversation$' ./internal/httpapi/` (+ gói `httpapi` đầy đủ ở ci-task);
  console vitest `src/screens/inbox/side-panel/`, `src/screens/len-don/OrderForm.test.tsx`,
  `src/screens/khach/CustomerProfile.test.tsx`, `src/app/errors.test.ts`; `tsc --noEmit`, eslint các tệp sửa;
  `go test ./tests/arch/`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: mục `console/src/shared` của
  `docs/danh-muc-dung-chung.md` ("Không viết form lên đơn thứ hai… dùng OrderForm"; "Không gọi hàm ghi thẳng: qua
  useWriteAction"; `errorMessage`, `queryKey`, `unwrap`); `screens/inbox/inboxRules.ts` (màu nhãn `.lb-1..4`, tên người
  phụ trách); `screens/khach/customerRules.ts` (`percentText`, `tagChip`); `screens/don/labels.ts` (`STATUS_TEXT`);
  `screens/products/productRules.ts` (`formatMoney`); `app/time.ts` (`shortTime`); `len-don/orderRules.ts`
  (`displayPhone`). Phía Go: `conversation.OrderContext` (Task 159) đã kiểm phạm vi Page và trả người bán, Page.
- Dùng lại / viết mới vì: dùng lại nguyên `OrderForm` (cả chế độ sửa `edit` của Task 85 cho "Hoàn tất" đơn nháp —
  `updateOrder` với `version`, câu 409 `version_conflict` có sẵn) thay vì viết form thứ hai; `conversation.OrderContext`
  cho phía máy chủ thay vì đọc hội thoại lần nữa. Viết mới: các thành phần khung bên phải (chưa có ở đâu), bảng chữ
  kết quả cuộc gọi `side-panel/labels.ts` (dùng chung giữa khung bên phải và dòng thời gian khách — một nguồn).

## Soát thiết kế

- 5.1–5.3: không thêm dữ liệu, không đọc bảng module khác: handler gọi `conversation.OrderContext` (public API) rồi
  `order.Writer.Create` (public API) với `ConversationID`, `ChannelAccountID` có sẵn ở `CreateInput` (M2 A14).
- 5.4: đọc bối cảnh và tạo đơn trong cùng một transaction `WriteTx` (đọc không khóa, rồi luồng tạo đơn có sẵn; không
  thêm khóa nào → thứ tự khóa ADR-46 không đổi).
- 5.5, 5.6: không áp dụng: không có việc sau commit hay gọi ra ngoài mới.
- 5.7: `WriteTx` chạy trong tenant; `OrderContext` đọc view của hội thoại theo RLS.
- 5.8: handler mỏng, module không biết HTTP.
- 5.9: `createOrder` đã có `Idempotency-Key`, `x-permission: order.create`; mọi thao tác ghi ở màn (nhận, giao, nhãn,
  nhắc việc, cuộc gọi, không liên hệ, hoàn tất đơn) đi qua `useWriteAction`.
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện mới).
- 5.12: màn theo quy ước `src/screens/inbox/side-panel` của Task 174; API chỉ qua client sinh, không `as` trên dữ liệu API.
- 6.1: hai người cùng hoàn tất một đơn nháp: `updateOrder`, `confirmOrder` kiểm `version` → 409 `version_conflict` →
  "Có người vừa sửa, tải lại" (màn không ghi đè); giao hội thoại gửi `expectedVersion` của chi tiết đã đọc.
- 6.2: khóa chống trùng sinh khi bấm (useWriteAction), gửi lại cùng khóa; "Gắn nhãn Đã lên đơn" là PUT gắn lại không lỗi.
- 6.3–6.11: không áp dụng (không khóa mới, không việc nền, không tiền mới — tiền chỉ hiện số máy chủ trả).
- 6.12 dữ liệu cá nhân: SĐT chỉ hiện khi máy chủ trả (cần `customer.read`); dòng thời gian không có nội dung tin, ghi
  chú cuộc gọi (máy chủ không trả); tên khách hiện bằng văn bản React.
- 6.13 quyền: máy chủ kiểm (`conversation.read` trên Page của hội thoại khi tạo đơn có `conversationId` — ngoài phạm vi
  → 404, không lộ hội thoại; người bán của Page áp cho đơn → hàng người bán khác bị từ chối như M2). Màn chỉ ẩn/khóa
  nút theo quyền mà máy chủ thật sự kiểm: "Giao cho…", "Trả lại" cần `conversation.assign`; "Nhận", nhãn, nhắc việc,
  ghi cuộc gọi cần `conversation.reply`; "Không liên hệ" cần `customer.write`; lên đơn cần `order.create` (khóa sẵn
  kèm lý do khi không có ở kho mặc định, HV28); "Hoàn tất" cần `order.update` + `order.confirm`.

## Quyết định thay chủ shop

1. **`createOrder` nhận `conversationId` (tùy chọn).** Kế hoạch (A14) giả định `OrderForm` của M2 đã có
   `conversationId`, `channelAccountId` và hợp đồng chở chúng; thực tế `CreateOrderRequest` không có hai trường này
   (module `order` thì có). Không có thì tiêu chí "đơn lưu hội thoại" (01 mục 6) không làm được. Chọn: thêm **một**
   trường `conversationId`; máy chủ tự suy ra Page (`channelAccountId`), người bán của Page (áp làm người bán của đơn,
   ADR-29) và kênh `facebook` bằng `conversation.OrderContext` (kiểm `conversation.read` trên Page, ngoài phạm vi → 404).
   Phương án khác: cho màn gửi cả `channelAccountId` (phải kiểm khớp hai trường, thêm một đường sai); bỏ qua liên kết
   (trái spec). Thêm trường tùy chọn là mở rộng, dễ đảo ngược.
2. **Hoàn tất đơn nháp dùng chế độ sửa sẵn có của `OrderForm`** (Task 85) thay vì prop `draftOrderId` của kế hoạch:
   khung đọc `getOrder` rồi mở form với `edit={order, confirmAfterSave: true}` — nút "Lưu và xác nhận", kiểm địa chỉ như
   ý định xác nhận, gửi `updateOrder` (chỉ trường đổi, `version`) rồi `confirmOrder` với `version` mới nhất do
   `updateOrder` trả; không đổi gì thì chỉ `confirmOrder`. Phương án khác: thêm đường nạp đơn thứ hai trong form (hai
   cách cho một việc).
3. **Mã khách, nhãn khách ("Hay hoàn") đọc bằng `getCustomer`** khi người xem có `customer.read` (thẻ hội thoại không có
   hai thứ này); không có quyền thì không hiện mã, nhãn, SĐT (đúng như máy chủ che SĐT). Ô "Hoàn" tô đỏ khi khách có
   nhãn tên "Hay hoàn" (HV24). Không thêm trường vào hợp đồng thẻ khách.
4. **"Giao cho…" không hiện số hội thoại đang phụ trách** (mockup "đang phụ trách 14"): `AssignableUser` chỉ có id, tên
   — không vẽ số máy chủ không trả. Dòng người phụ trách không có "nhận lúc 09:50" vì lý do tương tự (chi tiết hội thoại
   không có giờ nhận). Ghi ở Việc phát hiện thêm.
5. **Chip "Khách tiềm năng mới" (CH08) không vẽ**, cùng lý do Task 174 (không có trường). Khách 0 đơn: câu
   "Khách mới, chưa có đơn" (kế hoạch).
6. **"Lấy địa chỉ từ tin 09:14 của khách" (HV26) chưa làm**: cần đọc tin của hội thoại và nhận ra tin nào chứa địa chỉ;
   phần tin là của màn hội thoại (Task 175) chưa có trên m4. Ghi ở Việc phát hiện thêm.
7. **Nhắc việc trong khung** đọc `listMyReminders` (chỉ nhắc của chính mình, máy chủ không có danh sách theo hội
   thoại) lọc theo hội thoại; "Xong" → `completeReminder`; "Nhắc tôi lúc…" mở ô giờ + ghi chú → `createReminder`.
8. **Nhịp tim `drafting_order`** gửi khi khung lên đơn đang mở, mỗi 20 giây (hạn máy chủ 30 giây), dừng khi đóng.
9. **Quyền lên đơn khóa sẵn (HV28)**: khóa khi người xem không có grant `order.create` toàn shop hay theo kho (gán theo
   Page không đủ, Task 411). Có grant theo một kho khác kho chính thì vẫn mở form; máy chủ trả 403 và form M2 đã có câu
   báo (không đọc danh sách kho để đoán kho chính — cần `inventory.read`).
10. **Ô số "Nhận hàng · %"** = phần bù của `returnRateBp` trên cùng mẫu số của M2 (giao / (giao + hoàn)), làm tròn
    xuống; chưa có đơn giao xong hay hoàn thì chỉ hiện số. Ô "Hoàn" là "số · N%" theo mockup ("2 · 17%") thay chữ
    "Tỷ lệ hoàn N%" của kế hoạch (cùng số N).
11. **Nút "Đặt không liên hệ"** (khi khách chưa có cờ, có `customer.write`): kế hoạch ghi "bật/tắt"; mockup chỉ vẽ nút
    bỏ cờ. Thêm nút đặt cờ dạng nút phụ ở thẻ khách.
12. **Form hoàn tất nháp dựng từ bản chụp đơn lúc mở**: báo hiệu realtime của đơn không dựng lại form (không mất phần
    đang gõ); "Tải lại đơn nháp" lấy bản mới. Lưu thay đổi xong mà xác nhận lỗi (vd. 422 chờ duyệt giảm giá) → bản chụp
    là đơn vừa lưu (version mới).
13. **Nút "‹ Hội thoại" trên điện thoại** (mockup) chưa vẽ: cách chuyển giữa hội thoại và khung khách trên điện thoại
    nằm ở đầu hội thoại (nút "Khách") của Task 175. Khung bên phải vẫn xuống dưới hội thoại dưới 1200 px (Task 174).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| 01 mục 6: đơn lên trong khung chat lưu hội thoại, Page, kênh facebook (cả khi form gửi manual), người bán của Page; Page khác → 404, không tạo đơn; Page đổi người bán → 422 mixed_sellers, không tạo đơn | `TestCreateOrder_FromConversation` (lần đầu ĐỎ: 400 bad_request trước khi có trường) | `go test -count=1 -run '^TestCreateOrder_FromConversation$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 4.383s` |
| DH03 phía màn hình: returnRateBP 2000 → "Hoàn 2 · 20%", "Nhận hàng 8 · 80%"; 1250 → "12%"; "Hay hoàn" tô đỏ ô Hoàn; 5 đơn gần đây có liên kết, "Xem cả 12 đơn" | `CustomerCard.test.tsx` › chỉ số của khách cũ (4 ca) | `npx vitest run src/screens/inbox/side-panel/` | 5 tệp, 41 passed |
| Khách mới 0 đơn → "Khách mới, chưa có đơn"; tên 60 ký tự cắt chữ; gợi ý cùng một khách; không có `customer.write` → không có công tắc; bỏ/đặt cờ gửi `on` đúng | `CustomerCard.test.tsx` › khách mới, gợi ý cùng một khách, không liên hệ (HV29) | như trên | passed |
| Mở lên đơn → OrderForm nhận `conversationId`, `initialTrafficSourceId`, khách của bối cảnh; nhịp tim `drafting_order`; tạo xong → chip "Gắn nhãn Đã lên đơn", bấm → đúng 1 PUT `addConversationTag`; "Bỏ qua" không gắn; không `order.create` → khóa sẵn (HV28) | `OrderPanel.test.tsx` › lên đơn trong khung chat (4 ca) | như trên | passed |
| LV04: đơn nháp trong "Đơn nháp đang mở" → Hoàn tất mở form sửa đơn đó (`confirmAfterSave`), lưu xong `confirmOrder` với version mới nhất (4); 409 `version_conflict` → "Có người vừa sửa, tải lại", không tự gửi lại; 422 → form dựng từ đơn vừa lưu; thiếu `order.confirm` → không có Hoàn tất | `OrderPanel.test.tsx` › hoàn tất đơn nháp từ Live (4 ca) | như trên | passed |
| OrderForm: `conversationId` vào thân createOrder, nguồn đơn tự điền; màn Lên đơn M2 không gửi `conversationId`; hoàn tất nháp không đổi → onSaved không gửi updateOrder; thiếu địa chỉ → nút khóa | `OrderForm.test.tsx` › lên đơn cạnh khung chat (4 ca) | `npx vitest run src/screens/len-don/OrderForm.test.tsx` | passed |
| CH06: chưa ai nhận → "Nhận" (claim, có khóa); 409 already_assigned → câu tiếng Việt; không `conversation.assign` (hay chỉ ở Page khác) → không "Giao cho…"; Giao gửi userId + expectedVersion; tìm nhân viên; Trả lại gửi null | `AssigneeControl.test.tsx` (9 ca) | `npx vitest run src/screens/inbox/side-panel/` | passed |
| CH17: sáu kết quả; "Hẹn gọi lại" thiếu giờ → lỗi tại ô (aria-invalid), không gửi; đủ → createCallLog đúng thân; kết quả khác không có callbackAt; khách không SĐT → không nút gọi; tel: đúng số; lần gọi trước | `CallResultDialog.test.tsx` (8 ca) | như trên | passed |
| CH07: gỡ nhãn → DELETE; "+ Nhãn" chỉ nhãn chưa gắn, chọn → PUT; chỉ xem → không nút; nhắc việc của hội thoại này, Xong → completeReminder; "Nhắc tôi lúc…" thiếu giờ báo tại ô, đủ → createReminder | `SidePanel.test.tsx` (6 ca) | như trên | passed |
| C7 phía màn hình: mục conversation hiện tên Page + liên kết `/inbox/:id`; comment; mục call hiện kết quả + người gọi; không nội dung tin; đọc tên Page lỗi vẫn hiện mục | `CustomerProfile.test.tsx` › hội thoại và cuộc gọi trong dòng thời gian (2 ca; lần đầu ĐỎ 2 ca trước khi sửa NotesPanel) | `npx vitest run src/screens/khach/CustomerProfile.test.tsx` | 14 passed |
| Không vỡ màn khác (Inbox, Lên đơn, Khách, Đơn) | toàn bộ vitest các thư mục đó | `npx vitest run src/screens/inbox src/screens/len-don src/screens/khach src/screens/don src/app/errors.test.ts` | 32 files, 507 passed |
| Cổng kiểm của task (cổng tĩnh gồm console-check, test `httpapi` -race và gói phụ thuộc) | ci-task (lượt 1 ĐỎ jscpd: đoạn test 422/409 lặp → gộp `openDraft`; lượt 2 xanh) | ci-task của quy trình | `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh 4370f657…)`, run 45868235440: static, test (1), test (2) success |
| Cổng tĩnh tự soát | tsc, eslint, jscpd, golangci-lint, arch | `npx tsc --noEmit`; `npx eslint …`; `make jscpd`; `golangci-lint run ./internal/httpapi/`; `go test ./tests/arch/` | sạch; `Found 0 clones`; `0 issues`; `ok tests/arch 163s` |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 176 không có spec Playwright; đủ ca nằm ở vitest và test `httpapi`.)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Đường tệp theo quy ước thật của console (`src/screens/inbox/side-panel/`, `src/screens/len-don/OrderForm.tsx`,
  `src/screens/khach/NotesPanel.tsx`) thay `console/src/features/...` (cùng quyết định 1 của Task 174).
- Thêm một trường hợp đồng và một test `httpapi` (Quyết định 1).
- Thứ tự TDD: phần thêm của `OrderForm.tsx` (3 prop nhỏ) viết trước test của nó trong cùng lượt; các thành phần khung
  bên phải và dòng thời gian C7 có lần chạy đỏ trước khi viết mã (thiếu mô-đun / 2 ca C7 đỏ).
- Thêm test, tệp ngoài danh sách kế hoạch: `SidePanel.test.tsx`, `sidePanelTestkit.tsx`, `TimeField.tsx`,
  `sidePanelData.ts`, `labels.ts`, `side-panel.css`; `khachTestkit.tsx` thêm đường `/api/v1/channels`.

## Việc phát hiện thêm

- `AssignableUser` chưa có số hội thoại đang phụ trách; `ConversationDetail` chưa có giờ nhận (mockup HV31, trạng thái A).
- "Lấy địa chỉ từ tin của khách" (HV26) cần làm khi màn hội thoại (Task 175) có trên m4.
- Thẻ khách hội thoại chưa có mã khách, nhãn khách (đang đọc thêm `getCustomer` khi có `customer.read`).
- (Reviewer) `createOrder` có `conversationId` chưa đối chiếu `customerId` của thân với khách của hội thoại (màn luôn
  gửi khách của hội thoại; spec không đòi). Nếu muốn chặt: thêm kiểm cùng nhóm khách ở `orderConversationAt`.
- (Reviewer) Lúc đang `confirmOrder` sau khi lưu nháp, nút chưa hiện "đang xác nhận" (không gửi trùng được nhờ
  useWriteAction).

## Reviewer

Kết luận: ĐẠT (vòng 1, reviewer trên `git diff --cached` sau khi đã gộp m4 tới 4370f657 và ci-task xanh).
Không có phát hiện phải sửa. Đã đối chiếu: Ảnh hưởng khớp thực tế (CreateOrder, orderConversationAt 1 chỗ gọi,
isOrderRefusal + conversation.ErrNotFound → 404, mã sinh không sửa tay, 3 prop OrderForm giữ hành vi cũ khi không
truyền, khóa summary của NotesPanel khớp chat_timeline_dynsql.go); bảng bằng chứng khớp (41 ca vitest khung bên phải,
4 ca OrderForm, 2 ca C7, TestCreateOrder_FromConversation đọc ord.orders thật); không test bị tắt; quyền trên màn khớp
x-permission. Ghi chú không bắt buộc (khách của thân so với khách hội thoại; chưa có dấu "đang xác nhận"; `fold` gần
giống bản ở web-looks, khác màn nên giữ) — hai ý đầu ghi ở Việc phát hiện thêm.

## Gộp vào m4

- `git merge --no-ff --no-commit task-176` (điểm tách 4370f657; m4 đã có thêm Task 178, 175, 499): **không xung đột văn
  bản**; 33 tệp của nhánh vào nguyên vẹn. Trên máy: `go build ./...`, `go vet ./internal/httpapi/ ./cmd/crm/` sạch;
  `make gen` không chênh; `npx tsc --noEmit`, `prettier --check` sạch.
- Xung đột ngữ nghĩa (reviewer phiên gộp tìm ra): spec Playwright `console/e2e/hoi-thoai.spec.ts` của Task 175 viết khi
  khung bên phải còn là bản giữ chỗ; sau gộp, `/inbox/:id` nạp `SidePanel` thật và gọi `customer-card`, `me/reminders`,
  `customers/{id}/calls` chưa khai → fixture `apiGuard` đỏ cả 6 ca. Sửa: khai thêm 3 đường đó trong `fakeConversation`
  (thẻ khách không SĐT vì không có `customer.read`), và `getByText(NAME_60).first()` vì tên khách nay hiện ở cả đầu
  hội thoại và thẻ khách. Spec vẫn "viết sẵn, chưa chạy" (không chạy Playwright trong phiên gộp); tsc, eslint, prettier
  sạch trên tệp.
- merge-test lượt 1 (run 45870913486, blacksmith): test Go xanh, cổng tĩnh ĐỎ jscpd — `canOnPage` có hai bản giống
  hệt (`inbox/conversation/conversationRules.ts` của Task 175, `inbox/side-panel/sidePanelData.ts` của Task 176). Sửa:
  xóa bản của khung bên phải, `sidePanelData.ts` nhập và xuất lại bản của `conversationRules.ts` (cùng mục đích, cùng
  lý do thay đổi: quyền hộp chat theo Page). tsc, eslint sạch.
- Việc phát hiện thêm (phiên gộp): `console/e2e/inbox.spec.ts` ca "mở hội thoại" đã thiếu mock chi tiết, tin, presence
  từ khi gộp Task 175 (có sẵn trên m4), nay thiếu thêm `customer-card` — cần sửa trước lượt `CRM_CI_E2E=1`.
  Khóa danh
  sách nhãn của Cài đặt (`CONVERSATION_TOPIC,"tags"`) khác khóa của Inbox/TagPicker (`conversation.tags,list`).
- merge-test lượt 2 (run 45871734736, blacksmith, cây 9b1e5163): `static: success` (build, vet, lint, gen-check, jscpd,
  console-check gồm tsc/eslint/prettier/vitest/vite build), `test (1): success` (`./tests/arch`) → `MERGE-TEST XANH
  (blacksmith)`. Test Go (`./internal/httpapi` -race, `./cmd/crm`, `./tests/arch`) xanh ở lượt 1 (test (1), test (2)
  success); mã Go không đổi giữa hai lượt. Không có test chập chờn.
- Reviewer phiên gộp: vòng 1 CẦN SỬA (spec `hoi-thoai.spec.ts` thiếu mock khung bên phải); vòng 2 **ĐẠT** — khung khách
  không còn gọi đường chưa khai với quyền của spec; gỡ `canOnPage` trùng không đổi hành vi (3 nơi gọi vẫn nhập từ
  `./sidePanelData`). Ghi chú không bắt buộc: tên 60 ký tự ở thẻ khách khổ 360 chỉ chắc khi Playwright chạy thật; hai
  nút "Nhận" (đầu hội thoại và khung khách) cần đối chiếu mockup.
