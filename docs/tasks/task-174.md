# Task 174 — Màn Inbox và khung điều hướng của M4

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 174 = M4-34). Loại việc (quy trình mục 3): A (màn hình)

Mockup: `docs/mockups/m4/inbox.html`, `png/inbox-{1512,390,360}.png`, khung `khung.js`; hành vi HV01–HV08 của
`docs/mockups/m4/README.md` cộng các dòng sửa HV03, HV04, HV06 (mục "Kết luận duyệt").

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task chỉ đổi `console/`).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi mã Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn dùng (chỉ đọc) `listConversations` (lọc `pageId`,
  `status`, `tagId`, `assignee`, `kind`, `unread`, `q`, `updatedSince`, `cursor`, `limit`), `countConversations`,
  `listChannelAccounts`, `listConversationTags` (cả bốn `x-permission: conversation.read`), `listStaff`
  (`identity.users.manage`, chỉ gọi khi có quyền), `getMe` (khóa chung với khung).
- Chỗ dùng trên màn hình:
  - `console/src/app/nav.ts`: thêm nhóm "Hộp chat" đứng đầu `NAV_GROUPS` (HV01) → test ghim thứ tự trong
    `src/app/screens.test.tsx` sửa theo; mọi màn khác giữ nhóm cũ.
  - `console/src/shell/App.tsx`: đưa trạng thái lớp realtime xuống màn qua context (băng "Mất kết nối" có nút "Tải lại
    ngay" của Inbox, HV07); băng của khung (`Banner`, HT05) không đổi. `src/shared/index.ts` xuất hook mới →
    `docs/danh-muc-dung-chung.md` sinh lại (`make catalog`, thêm một dòng).
  - Màn mới: `src/screens/inbox/` (`/inbox`, `/inbox/:conversationId`), khung màn `src/screens/live/`
    (`/live`, `/live/thieu-dia-chi`, `/live/:sessionId`, `/live/:sessionId/thieu-dia-chi`), `src/screens/kenh/`
    (`/settings/channels`, `/settings/channels/facebook/callback`, `/settings/inbox`, `/settings/pages/:pageId`,
    `/settings/tags`, `/settings/templates`); khung thành phần `src/screens/inbox/conversation/index.tsx`,
    `src/screens/inbox/side-panel/index.tsx` cho Task 175, 176.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: vitest các tệp mới `src/screens/inbox/*.test.ts(x)`, `src/screens/live/route.test.tsx`,
  `src/screens/kenh/route.test.tsx`, cùng `src/app/screens.test.tsx`, `src/shell/*.test.tsx` (chạm App, nav);
  `tsc --noEmit`, eslint các tệp sửa; console-check trong ci-task. Playwright `e2e/inbox.spec.ts`: viết sẵn, không chạy.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` phần `console/src/shared`
  (realtime, `subscribeChanged`, `queryKey`, `unwrap`, `errorMessage`, `shortTime`, `loadScreens`), màn mẫu cùng việc:
  `screens/don/OrderList.tsx` (báo hiệu đọc lại dòng, nền 5 giây, tự tải 20 s), `screens/products/ProductList.tsx`
  (danh sách ảo `useWindowVirtualizer`, bộ lọc trên URL), `screens/inventory/Balances.tsx` (`useDebounced`),
  `app/screenTestkit.tsx` (API giả `stubApi`, `renderScreens`, `page`).
- Dùng lại / viết mới vì:
  - Dùng lại: `subscribeChanged` + `meta.realtimeManual` (danh sách tự xử lý báo hiệu), `useWindowVirtualizer` như
    ProductList (đã có trong ADR-44 / package.json, không thêm thư viện), `SEARCH_DELAY_MS`, `unwrap`/`errorMessage`,
    `screenTestkit`, lớp CSS chung (`.chip`, `.toggle`, `.select-wrap`, `.input.is-search`, `.empty`, `.more`,
    `.avatar` của len-don.css được chép vào inbox.css cùng giá trị).
  - Không dùng `useDebounced` cho ô tìm: Filters hẹn giờ trong lúc gõ và gửi với bộ lọc mới nhất (ref), vì bộ lọc
    nằm trên URL và có thể đổi (Bỏ lọc, chọn Page) trong 300 ms chờ; `useDebounced` + effect sẽ gửi lại chữ cũ.
  - jscpd: 4 đoạn trùng mới đã gỡ (writeFilters viết theo bảng trường thay vì giống productRules; test gom helper
    `edit`, `calls`, `newMessage`; spec e2e dùng hàm ca theo khổ màn).
  - Viết mới `useConversationList`: khác OrderList ở chỗ máy chủ có `updatedSince` (lấy phần đổi rồi gộp theo `id`,
    sắp lại theo `(lastMessageAt, id)`), không đọc lại từng dòng; logic gộp tách thành hàm thuần `mergeRows` để test.
  - `inbox.css` chép đúng lớp của `docs/mockups/m4/m4.css` (`.chat`, `.pg`, `.unr`, `.bnr`, `.cv`, `.lb`, `.ch-ic`)
    chỉ bằng biến có sẵn, như các màn trước chép từ m2/m3.css.

## Soát thiết kế

- 5.1–5.8, 5.10, 6.x về database, khóa, transaction, tiền, hàng đợi: không áp dụng (task chỉ có màn hình, không đổi
  máy chủ).
- 5.9 đường ghi: màn không có yêu cầu ghi nào (chỉ đọc). Gửi mẫu "Khách chưa trả lời" (CH16, trạng thái D) không làm ở
  đây (xem "Quyết định thay chủ shop").
- 5.11 thư viện: không thêm (`@tanstack/react-virtual` đã có).
- 5.12 mẫu: màn tự đăng ký bằng `route.tsx` (quy ước `src/app/loadScreens.ts`), API chỉ qua client sinh, không `as`
  trên dữ liệu API (luật console 2, 3).
- Quyền (PQ04): menu và route đòi `conversation.read` (Inbox), `live.operate` (Live), `channel.manage` (Kênh kết nối),
  `conversation.settings` (Cài đặt). Máy chủ vẫn lọc hội thoại theo Page được giao; màn chỉ hiện Page mà
  `listChannelAccounts` trả (đã lọc theo phạm vi ở máy chủ). Nút "Mở Kênh kết nối" chỉ hiện khi có `channel.manage`.
- Dữ liệu từ khách (04 mục 13): tên, đoạn tin cuối, tên bài hiện bằng văn bản React (không `dangerouslySetInnerHTML`);
  ảnh đại diện Facebook (`avatarUrl`, máy chủ ngoài) không nạp, dùng chữ cái đầu như mockup.
- Đồng thời phía màn hình: báo hiệu dồn dập → tối đa một lần tải phần đổi đang chạy, báo hiệu tới trong lúc đó gộp
  thành một lần tải kế tiếp; kết quả tải phần đổi cũ hơn lần tải lại cả trang thì bỏ (so mốc lúc gửi yêu cầu).

## Quyết định thay chủ shop

1. **Vị trí tệp theo quy ước thật của console**, không theo đường kế hoạch ghi (`console/src/features/...`,
   `routes.tsx`): console tự nạp màn từ `src/screens/<tên>/route.tsx` (Task 393), không có `routes.tsx`. Inbox ở
   `src/screens/inbox/`, khung hội thoại và khung bên phải là thư mục con của nó, Live ở `src/screens/live/`, cài
   đặt kênh ở `src/screens/kenh/`. Phương án khác: tạo `features/` song song — hai quy ước cho một việc.
2. **Đường dẫn:** giữ mọi đường kế hoạch ghi, thêm hai đường mockup cần cho mục menu không có tham số:
   `/live/thieu-dia-chi` ("Đã chốt, thiếu địa chỉ") và `/settings/inbox` ("Cài đặt hộp chat"). Màn khung (Live,
   Kênh, Cài đặt) hiện câu "Màn này đang được làm" cho tới Task 177, 178; chỉ có trên nhánh m4.
3. **Nạp lười:** mỗi màn khung (Live, Kênh kết nối, Cài đặt, khung hội thoại, khung bên phải) nạp bằng `React.lazy`
   trong `Suspense`; Inbox nạp ngay vì là màn đầu của nhóm.
4. **Băng Page mất kết nối:** câu đậm đúng kế hoạch "Page {tên} mất kết nối, cần quản trị kết nối lại." Câu giải
   thích: lý do theo `statusReason` (Task 149, 150: `token_invalid`/`auth_failed` → mã truy cập, `missing_permissions`,
   `meta_policy:*`) + "CRM chưa gửi được gì ra Facebook cho tới khi kết nối lại." Bỏ câu mockup "Tin mới không về CRM":
   máy chủ giữ tuyến webhook khi `degraded` (Task 150) nên câu đó có thể sai; cũng không khẳng định tin vẫn về (tùy
   Meta). Nút "Mở Kênh kết nối" (tên mục menu, HV32) thay "Mở Kết nối Page", chỉ khi có `channel.manage`. Tài khoản
   `disconnected` (quản trị đã ngắt) không có băng, chỉ chip "Đã ngắt".
5. **HV03 (sửa):** danh sách tài khoản kênh, biểu tượng chữ "FB" (lớp `.ch-ic` của mockup Kênh kết nối, cỡ nhỏ 24 px)
   + tên; dòng đầu "Tất cả tài khoản"; số chưa đọc = `countConversations.byPage[].unread` (số hội thoại còn tin chưa
   đọc, quyết định 3 của Task 159), trên 99 hiện "99+".
6. **HV04 (sửa) âm báo:** nút "Âm báo" (bật/tắt, `aria-pressed`) ở đầu cột danh sách, tắt mặc định, nhớ trên máy
   (`localStorage`). Kêu một tiếng ngắn (Web Audio, không tệp âm thanh) khi lần tải phần đổi đưa về một hội thoại có
   tin mới, còn chưa đọc, của tôi hoặc chưa ai nhận. Trình duyệt chặn âm thanh khi chưa bấm gì thì im lặng, không báo lỗi.
7. **HV06 (sửa) nhớ bộ lọc:** URL là nguồn của bộ lọc (kế hoạch); mỗi lần đổi lưu bộ lọc vào `localStorage` của máy;
   mở `/inbox` không có tham số lọc → lấy bộ lọc đã lưu. "Bỏ lọc" hiện khi bộ lọc khác mặc định (Đang mở, Mọi người,
   Mọi nhãn, Tin và comment, mọi tài khoản, không chỉ chưa đọc, không tìm).
8. **CH16 "Khách chưa trả lời" (trạng thái D) chưa làm ở task này:** không nằm trong tiêu chí của Task 174, và nút
   "Gửi mẫu" cần đường gửi tin theo mẫu (render mẫu + gửi) mà màn hội thoại Task 175 dựng. Không vẽ nút bật/tắt
   "Khách chưa trả lời" (không vẽ hành vi chưa có). Ghi ở "Việc phát hiện thêm".
9. **Cập nhật khi có báo hiệu:** máy chủ lọc `updatedSince` theo `last_message_at` (đã tự lùi 30 s,
   `query.SinceSlack`); màn vẫn gửi `lần tải trước − 30 s` như kế hoạch (thêm lề cho đồng hồ máy lệch). Báo hiệu của
   hội thoại đang hiện mà lần tải phần đổi không trả nó (đổi không có tin mới: giao, nhãn, đã đọc) → tải lại cả danh
   sách một lần, để dòng không cũ tới 20 giây.
10. **Chip "Khách tiềm năng mới" (CH08) không vẽ:** `ConversationSummary` không có trường cho biết khách vừa tạo; thêm
    trường là việc của hợp đồng (ghi ở "Việc phát hiện thêm"). Ảnh đại diện dùng chữ cái đầu (quyết định soát 04 mục 13).

11. **HV07 băng mất kết nối của Inbox:** khung đưa trạng thái lớp realtime xuống màn bằng context
    (`src/shell/realtimeState.ts`); khi `reconnecting` danh sách có băng mockup B kèm "Tải lại ngay". Băng chung của
    khung (HT05, câu spec 04 mục 6) vẫn hiện như cũ, nên lúc mất kết nối có cả hai (băng khung ngắn, băng danh sách có
    nút).
12. **Màu nhãn:** `ConversationTag.color` là tên token; màn đổi `soft`, `surface`, `success`, `edge` sang bốn sắc
    `.lb-1..4` của mockup, tên khác → sắc trung tính `.lb-2`. Task 178 (màn nhãn) chọn đúng bốn tên này.

## Bằng chứng

Lệnh vitest chạy trong `console/`. Ảnh chụp so mockup: chưa chụp (Playwright không chạy trong task, chủ shop 24/11);
spec chụp `e2e/inbox.spec.ts` viết sẵn.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Step 1: 200 hội thoại → dưới 60 dòng trong DOM (danh sách ảo) | `ConversationList.test.tsx` "200 hội thoại → chỉ dựng phần đang thấy" | `npx vitest run src/screens/inbox` | đỏ trước (không có mô-đun `./ConversationList`, 8 tệp đỏ); sau `Tests 32 passed (32)` |
| CH01 phía màn hình: báo hiệu X có `lastMessageAt` mới → đúng một lần tải phần đổi (`updatedSince` ≤ lần tải trước − 30 s), X dòng đầu, số chưa đọc của X tăng, X sáng nền, gọi âm báo một lần | `useConversationList.test.tsx` "báo hiệu cho X có tin mới…" | như trên | PASS |
| CH01: số chưa đọc của X và của Page (Tất cả tài khoản 129 → 130) tăng, đúng 1 lần tải có `updatedSince` | `InboxPage.test.tsx` "CH01: báo hiệu tin mới của X…" | như trên | PASS |
| Cùng id ở hai lần tải → một dòng; bản tải sau thắng, bản cũ không đè | `useConversationList.test.tsx` "cùng id trả về ở hai lần tải"; `inboxRules.test.ts` `mergeRows` (2 ca) | như trên | PASS |
| `resync` → tải lại từ đầu (không `updatedSince`, không con trỏ) | `useConversationList.test.tsx` "resync → tải lại từ đầu" | như trên | PASS |
| Đổi không có tin mới → tải lại cả danh sách một lần; báo hiệu chủ đề khác không tải | `useConversationList.test.tsx` 2 ca cuối | như trên | PASS |
| CH06: chọn "Của tôi" → yêu cầu `assignee=me`, URL `assignee=me`, Bỏ lọc hiện; CH07: chọn nhãn → `tagId` | `InboxPage.test.tsx` "chọn Của tôi…"; `inboxRules.test.ts` "listQuery…" | như trên | PASS |
| Tên khách 60 ký tự ở 360 px không tràn | vitest: `ConversationList.test.tsx` "tên khách 60 ký tự liền" (giữ chữ, ô tên `ellip`); bố cục thật: `e2e/inbox.spec.ts` `rowsFit` ở 360/390/1512 | vitest như trên; Playwright viết sẵn | vitest PASS; e2e chưa chạy (jsdom không đo bố cục) |
| HT15 phía màn hình: Page `read_only` và Page `degraded` → đúng hai câu băng; chip cạnh tên Page; nút Kênh kết nối theo quyền | `ChannelBanners.test.tsx` (3 ca); `InboxPage.test.tsx` "Page chỉ xem, Page mất kết nối…" | như trên | PASS |
| HT05: băng mất kết nối của M0 vẫn hiện | `src/shell/Banner.test.tsx`, `Layout.test.tsx` (không đổi; vẫn qua sau khi App bọc context) | `npx vitest run` (cả console) | `Test Files 129 passed (129)`, `Tests 1453 passed (1453)` |
| HV07: realtime `reconnecting` → băng "Mất kết nối, đang nối lại." + "Tải lại ngay" tải lại | `InboxPage.test.tsx` "HV07…" | `npx vitest run src/screens/inbox` | PASS |
| HV06: bộ lọc nhớ trên máy; mở `/inbox` không tham số → mọi lần gọi có `unread=true`; Bỏ lọc về mặc định | `InboxPage.test.tsx` "HV06…" | như trên | đỏ lần đầu (lần gọi thứ hai bỏ lọc vì URL chưa kịp đổi), sửa `useInboxFilters`; PASS |
| HV03, HV04: tài khoản kênh (FB + tên, 99+, 9, bấm → `pageId`); âm báo tắt mặc định, bật → nhớ | `InboxPage.test.tsx` 2 ca | như trên | PASS |
| HV01: nhóm "Hộp chat" đầu menu với 5 mục; đường kế hoạch đủ | `src/app/screens.test.tsx`; `screens/{inbox,live,kenh}/route.test.tsx` | như trên | PASS |
| 04 mục 13: tên, đoạn tin là chữ thuần; lỗi API hiện câu lỗi, không vẽ như rỗng | `ConversationList.test.tsx` "chữ thuần"; `InboxPage.test.tsx` "lỗi API danh sách" | như trên | PASS |
| Reviewer vòng 1: phần đổi không giữ mãi trong bộ nhớ (sau lần tải lại cả danh sách chỉ còn phần đổi gửi sau đó; thứ tự dòng vẫn đúng) | `useConversationList.test.tsx` "phần đổi không giữ mãi…" | `npx vitest run src/screens/inbox` | viết cùng bản sửa (không chạy đỏ trước); `Tests 33 passed (33)` |
| Tìm chỉ chữ số dưới 4 số không gửi (tránh 422 `query_too_short`), có câu nhắc | `inboxRules.test.ts` "listQuery…" (thêm 3 dòng) | như trên | PASS |
| tsc, eslint, prettier, jscpd | — | `npx tsc --noEmit`; `npx eslint` các thư mục sửa và `e2e/inbox.spec.ts`; `npx prettier --check src e2e`; `make jscpd` | sạch; jscpd 0 đoạn trùng mới (lần đầu 4, đã gỡ) |
| ci-task | — | `scripts/agent/ci-task.sh` | `static: success, 4 phút 24 giây` (gồm console-check: tsc, eslint, vitest của console); `test từ xa: không gói nào` (không đổi Go); `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh ebf50c54)`, run 45860086522; sau sửa vòng 1: `static: success, 2 phút 14 giây`, `CI-TASK XANH`, run 45861622291 |

## E2E viết sẵn, chưa chạy

- `console/e2e/inbox.spec.ts`: "Hộp chat {360,390,1512} px › danh sách: tên 60 ký tự không tràn, không cuộn ngang,
  băng Page, 99+" và "› mở hội thoại: máy tính ba cột, điện thoại chỉ hội thoại có nút quay lại"; chụp
  `test-results/inbox-{360,390,1512}.png`, `inbox-hoi-thoai-*.png` để so với `docs/mockups/m4/png/inbox-*.png`.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Đường tệp theo quy ước `src/screens/<tên>/route.tsx` thay `features/` + `routes.tsx` (Quyết định 1).

## Việc phát hiện thêm

- CH16 trạng thái D của mockup Inbox ("Khách chưa trả lời", gửi mẫu một chạm, `listFollowUps`) chưa có task màn hình
  nào nhận; nên làm sau Task 175 (dùng đường gửi tin có mẫu của nó).
- `ConversationSummary` chưa có cờ "khách mới tạo từ kênh" để vẽ chip "Khách tiềm năng mới" (CH08) ở danh sách.

## Reviewer

Vòng 1: CẦN SỬA — một phát hiện: `deltas.loads` của `useConversationList` chỉ thêm, không bỏ (tab mở cả ngày, live
nhiều comment → bộ nhớ tăng mãi, mỗi lần vẽ duyệt cả mảng). Đã sửa: khi thêm lần tải phần đổi mới, bỏ các lần gửi
trước lần tải lại cả danh sách gần nhất (`baseAt`); test "phần đổi không giữ mãi…". Ghi chú không bắt buộc đã làm: tìm
chỉ chữ số dưới 4 số không gửi (`searchable`), âm báo kêu một lần mỗi lượt tải phần đổi (`onFresh` nhận mảng). Ghi chú
chưa làm: chưa có test riêng cho "báo hiệu dồn dập gộp một lần tải" và "phần đổi về sau lần tải lại thì bỏ".

Vòng 2: ĐẠT — bản sửa đúng (`baseAt` là mốc gửi trang đầu của lần tải lại cả danh sách; "Tải thêm 50" không đổi mốc nên
không bỏ nhầm; số lần giữ có trần nhờ tự tải lại 20 s); không phát hiện phải sửa. Ghi chú không bắt buộc (không làm):
`SEARCH_DELAY_MS` nhập từ màn Kho, nên đưa về `src/shared` khi có dịp; câu trống "khớp "a"" khi chữ tìm quá ngắn; chưa
có test riêng cho gộp báo hiệu dồn dập và bỏ phần đổi về sau lần tải lại.

Kết luận: ĐẠT
