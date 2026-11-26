# Task 175 — Màn hội thoại

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 175 = M4-35). Loại việc (quy trình mục 3): A (màn hình)

Mockup: `docs/mockups/m4/hoi-thoai.html` trạng thái A–D, `png/hoi-thoai-{1512,390,360}.png`; Inbox trạng thái D
(`inbox.html`, CH16, giao thêm theo ghi chú điều phối); hành vi HV10–HV23 của `docs/mockups/m4/README.md` cộng các dòng
sửa HV14, HV17, HV21 (mục "Kết luận duyệt").

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task chỉ đổi `console/`).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi mã Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn dùng: đọc `getConversation`, `listMessages`
  (`before`), `getPresence`, `listReplyTemplates` (`q`, `pageId`), `getAttachmentUrl`, `listMessageRevisions`,
  `listFollowUps`, `getFile` (chờ ảnh `ready`), `getMe`; ghi (đều qua `useWriteAction`, có `Idempotency-Key`):
  `sendMessage` (`text` | `attachmentFileId`, `humanAgent`), `addConversationNote`, `renderReplyTemplate`,
  `replyToComment`, `sendPrivateReply`, `hideComment`, `unhideComment`, `resendMessage`, `discardMessage`,
  `markConversationRead`, `setConversationStatus` (`expectedVersion`), `claimConversation`, `setPresence` (khóa mới mỗi
  nhịp, Q9), `createUpload` + `completeUpload` (mục đích `chat_outgoing`).
- Chỗ dùng trên màn hình:
  - Thay khung tạm `console/src/screens/inbox/conversation/index.tsx` (Task 174) bằng màn thật; tệp mới cùng thư mục:
    `ConversationView.tsx`, `MessageList.tsx`, `MessageBubble.tsx`, `SendStatus.tsx`, `Composer.tsx`,
    `TemplateMenu.tsx`, `AttachmentView.tsx`, `CommentActions.tsx`, `RevisionsPopover.tsx`, `PresenceBar.tsx`,
    `usePresenceHeartbeat.ts`, luật thuần `conversationRules.ts`, đường ghi chung `messageWrites.ts`, CSS
    `conversation.css`, API giả `conversationTestkit.tsx`.
  - Inbox trạng thái D (CH16): `inboxRules.ts` thêm trường lọc `followUp` (tham số URL `followup=1`; `readFilters`,
    `writeFilters`, `FILTER_PARAMS`, `DEFAULT_FILTERS` — mọi chỗ dùng trong `screens/inbox`), `Filters.tsx` thêm nút
    bật/tắt "Khách chưa trả lời", `InboxPage.tsx` hiện danh sách `FollowUps.tsx` (mới) thay danh sách hội thoại khi bật,
    và gắn `id="khach"` cho khung bên phải (nút "Khách" trên điện thoại); `inbox.css` thêm lớp `.fu`, `.fu-row`,
    `.fu-list`; `inboxTestkit.tsx` (chỉ test) thêm `followUps`, `templates`, `rendered` và đường ghi render/gửi tin.
  - `screens/products/useUpload.ts` dùng lại nguyên (tham số `purpose`), không sửa.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: vitest `src/screens/inbox/conversation/*.test.tsx` (mới: `Composer`, `SendStatus`, `MessageBubble`,
  `CommentActions`, `PresenceBar`, `ConversationView`), `src/screens/inbox/*.test.ts(x)` (chạm inboxRules, Filters,
  InboxPage; thêm `FollowUps.test.tsx`); `tsc --noEmit`, eslint các tệp sửa; console-check trong ci-task.
  Playwright `e2e/hoi-thoai.spec.ts`: viết sẵn, không chạy.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` phần `console/src/shared`
  (`useWriteAction`, `unwrap`, `errorMessage`, `ApiError`, `queryKey`, `RealtimeMeta`, `shortTime`, `hasPermission`),
  màn cùng việc: `screens/products/useUpload.ts` (tải ảnh lên kho tệp, chờ `ready`), `screens/staff/staffData.tsx`
  `Dialog` (hộp thoại `<dialog>`), `screens/len-don` lớp `.sugg`/`.sugg-i` (menu gợi ý), `screens/inbox`
  (`inboxTestkit`, `inbox.css`, `CONVERSATION_TOPIC`), `app/screenTestkit.tsx` (`stubApi`, `reply`).
- Dùng lại / viết mới vì:
  - Dùng lại: `useWriteAction` cho mọi yêu cầu ghi (khóa sinh khi bấm, giữ khi tự gửi lại 503/409 — tiêu chí kế hoạch),
    `useUploads({ purpose: "chat_outgoing" })` cho ảnh (createUpload → PUT → completeUpload → chờ getFile `ready`),
    `Dialog` cho hộp xác nhận gửi lại, `useNow` của `screens/live/LivePage.tsx` (Task 177, gộp m4; jscpd báo trùng
    khi viết riêng), `meta.realtimeEntity` để báo hiệu `conversation.changed`/`conversation.presence`
    chỉ tải lại hội thoại đang mở, `shortTime`.
  - Viết mới: hiển thị tin (không có màn nào vẽ bong bóng chat), luật chính sách gửi → câu khóa ô gõ, trạng thái gửi →
    chữ và nút (`conversationRules.ts`, hàm thuần để test), nhịp tim có mặt (`usePresenceHeartbeat`: khóa mới mỗi nhịp
    theo Q9 nên không qua `useWriteAction` vốn nhớ khóa theo nội dung khi lỗi chưa rõ kết quả).
  - Bảng câu lý do lỗi gửi (`failReason`) chép từ `send.FailReasonText` của máy chủ (Task 156/157 quyết định để màn
    hình tự đổi mã sang câu); không gọi API cho câu.
  - CSS: lớp của `docs/mockups/m4/m4.css` cho hội thoại (`.conv-h`, `.presence`, `.thread`, `.msg`, `.bub`, `.meta`,
    `.sendfix`, `.sys`, `.post`, `.cacts`, `.revs`, `.reply-in`, `.att-*`, `.cmp*`, `.clock`) chép vào
    `conversation.css` chỉ bằng biến có sẵn; `.sugg`, `.tab`, `.dot` cùng giá trị với len-don.css / m1.css.

## Soát thiết kế

- 5.1–5.8, 5.10, 6.x về database, khóa, transaction, tiền, hàng đợi: không áp dụng (task chỉ có màn hình, không đổi
  máy chủ).
- 5.9 đường ghi: mọi yêu cầu ghi qua `useWriteAction` (khóa sinh khi người dùng bấm, tự gửi lại cùng khóa khi 503
  `retry_later`, 409 `request_in_progress`, lỗi mạng); mã hội thoại/tin nằm trong thân của `run` để khóa không dùng lại
  cho đối tượng khác. Nhịp tim `setPresence` khóa mới mỗi nhịp (Q9, Task 163), không tự gửi lại (nhịp sau 10 s).
- 5.11 thư viện: không thêm.
- 5.12 mẫu: màn trong `src/screens/inbox/conversation/` (khung Task 174), API chỉ qua client sinh, không `as` trên dữ
  liệu API, không `fetch` tay (PUT ảnh dùng `xhrPut` có sẵn).
- Quyền: gửi, ghi chú, trả lời comment, ẩn/hiện, gửi lại/bỏ, đổi trạng thái, "Nhận" chỉ hiện khi có
  `conversation.reply` ở phạm vi all hoặc đúng Page của hội thoại (máy chủ vẫn kiểm; thiếu quyền ở Page → 404). Không
  có quyền: ô gõ khóa kèm câu "Bạn chưa được cấp quyền trả lời hội thoại ở Page này".
- Chỉ vẽ hành vi máy chủ làm: lý do khóa ô gõ lấy `sendPolicy` (`allowed`, `mode`, `until`, `reason`) của
  `getConversation`; gửi lỗi 409 `send_not_allowed` hiện câu theo `denyReason` và tải lại chính sách; gửi lại chỉ có ở
  `in_doubt`, `expired`, `failed` lý do tạm (`rate_limited`, `not_sent`) đúng Task 157; tin tự động `discarded` không có
  nút. Nhắn riêng khóa khi `privateReply.state` là tin riêng đang sống (`queued|sent|checking|in_doubt`, Task 159) hoặc
  comment quá 7 ngày (máy chủ cũng chặn: `private_reply_expired`).
- Dữ liệu từ khách (04 mục 13): nội dung tin, tên, tên tệp, đoạn bài hiện bằng văn bản React; không
  `dangerouslySetInnerHTML`; ảnh chỉ nạp từ đường có hạn của `getAttachmentUrl` khi bấm (TP04), tệp khác chỉ liên kết
  `download`; ảnh đại diện Facebook không nạp.
- Đồng thời phía màn hình: báo hiệu `conversation.changed` của đúng hội thoại → tải lại chi tiết và trang tin mới nhất,
  gộp theo `id` với các trang cũ đã tải; đổi trạng thái gửi `version` (409 `version_conflict` → câu tải lại);
  "Gửi lại" bấm hai lần → `useWriteAction` trả lại lời hứa đang chạy (không gửi thêm).

## Quyết định thay chủ shop

1. **Vị trí tệp theo quy ước thật của console** (như Task 174 quyết định 1): `src/screens/inbox/conversation/` thay
   `console/src/features/conversation/` của kế hoạch; khung `index.tsx` của 174 giờ là màn thật.
2. **Hội thoại comment không có tab "Trả lời"**: máy chủ chỉ nhận `sendMessage` ở hội thoại hộp thư
   (`send/message.go`: kind ≠ inbox → từ chối); trả lời comment bằng "Trả lời công khai"/"Nhắn riêng" dưới từng comment
   (mockup trạng thái C không có ô gõ cho comment), ô gõ chỉ còn "Ghi chú nội bộ". Phương án khác: để tab và báo lỗi khi
   gửi (vẽ hành vi máy chủ không làm).
3. **Đánh dấu đã đọc làm ở màn hội thoại** (`markConversationRead` tới tin mới nhất của khách khi `unreadCount > 0`,
   mỗi tin một lần): kế hoạch không gán bước này cho task màn hình nào, nhưng không có thì số chưa đọc của Inbox
   (CH01, Task 174) không bao giờ giảm. Không gửi "đã xem" cho khách (Q10, máy chủ lo).
4. **HV14 (sửa):** nút Bỏ / Gửi lại ở `in_doubt`, `expired`, và `failed` lý do tạm `rate_limited`, `not_sent` (Task 157
   quyết định 1); `failed` lý do khác chỉ hiện câu lý do (bảng câu chép từ `send.FailReasonText`, Task 156 quyết định 3
   để màn hình tự đổi mã sang câu); `discarded` hiện "Đã bỏ, không gửi" (tin tự động bỏ kèm lý do), không nút. Chỉ
   "Gửi lại" ở `in_doubt` mở hộp xác nhận ("khách có thể nhận tin này hai lần"), đúng kế hoạch.
5. **HV17 (sửa):** máy tính Enter gửi, Shift + Enter xuống dòng; menu "/" đang mở thì Enter chèn mẫu; máy con trỏ thô
   (`pointer: coarse`, điện thoại) Enter xuống dòng, gửi bằng nút. Câu gợi ý dưới ô gõ đổi theo.
6. **HV21 (sửa):** comment khách sửa: chip "Khách đã sửa" + nút "Xem bản trước" (thu gọn, bấm mới gọi
   `listMessageRevisions`); khách xóa: bong bóng "Khách đã xóa comment này" + nội dung trước khi xóa mở sẵn (lấy từ
   `body` của tin, Task 154 giữ nội dung khi xóa) — không cần gọi API.
7. **Nhắn riêng khóa** khi `privateReply.state` là tin riêng đang sống (`queued|sent|checking|in_doubt`; máy chủ trả
   `none` hay send_status, Task 159 quyết định 8 — không có giá trị `used` như kế hoạch viết) với chữ "Đã nhắn riêng…";
   comment quá 7 ngày (tính từ `sentAt`) với chữ "Quá 7 ngày…". Nút không có khi kênh không hỗ trợ
   (`features.privateReply`, `features.hideComment`).
8. **Mẫu trả lời có ảnh hay nút trả lời nhanh**: chỉ chèn chữ đã điền biến vào ô (hợp đồng `SendMessageRequest` không
   mang nút; một tin là chữ hoặc một ảnh). Giống Task 168 quyết định 3.
9. **Inbox trạng thái D "Khách chưa trả lời" (CH16) làm ở đây** (ghi chú điều phối): nút bật/tắt cạnh "Chỉ chưa đọc",
   nằm trên URL (`followup=1`) và trong bộ lọc nhớ trên máy như các lọc khác (HV06); chọn "Im lặng quá" 1/3/6/12 giờ
   (mặc định 3, không nhớ) và mẫu; "Gửi mẫu /x" = `renderReplyTemplate` cho hội thoại đó rồi `sendMessage` chữ; dòng
   thành "Đã gửi mẫu /x lúc …". Mẫu riêng Page khác với Page của dòng → không có nút (máy chủ sẽ từ chối). Không có
   đoạn "Bạn: …" của mockup vì `FollowUpItem` không có tin cuối (thêm trường là việc hợp đồng).
10. **"Giao cho…", "Gọi khách", "Nhắc tôi lúc…"** của mockup không vẽ ở đây: thuộc Task 176 (AssigneeControl,
    CallButton, ReminderForm ở khung bên phải). Đầu hội thoại có "Nhận" (CH06) và chọn trạng thái; nút "Khách" trên
    điện thoại cuộn xuống khung bên phải (`#khach`).
11. **Nhịp tim** gửi ngay khi mở và khi đổi trạng thái (đang xem ↔ đang trả lời; thôi gõ 5 giây về đang xem), rồi mỗi
    10 giây; khóa mới mỗi nhịp (Q9), lỗi bỏ qua. Dải ai đang xem tải lại khi có báo hiệu `conversation.presence` của hội
    thoại và tự tải mỗi 15 giây (máy chủ không báo khi một người hết hạn 30 giây).
12. **Ảnh**: dùng lại `useUploads` của M1 (tham số `purpose: "chat_outgoing"`), tải lần lượt, mỗi ảnh xong thì gửi một
    tin; dòng ảnh lỗi có câu lý do và nút "Bỏ". Ảnh khách gửi chỉ nạp khi bấm (đường 5 phút), hiện trong trang, cao tối
    đa 360 px, kèm "Xem cỡ thật".

## Bằng chứng

Lệnh vitest chạy trong `console/`. Ảnh chụp so mockup: chưa chụp (Playwright không chạy trong task, chủ shop 24/11);
spec chụp `e2e/hoi-thoai.spec.ts` viết sẵn.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Step 2: test viết trước, đỏ | mọi tệp test mới | `npx vitest run src/screens/inbox` | đỏ: `Failed Tests 40` (không có `./usePresenceHeartbeat`, không có nút "Khách chưa trả lời", không thấy ô gõ…) |
| CH09: `window_closed` → ô gõ khóa, câu "Đã quá 24 giờ từ tin cuối của khách, Facebook không cho gửi tin thường", vẫn viết ghi chú | `Composer.test.tsx` "CH09: hết 24 giờ…"; `conversationRules.test.ts` "composerPolicy…" | `npx vitest run src/screens/inbox/conversation` | PASS |
| `until` +30 phút → "Còn 30 phút để trả lời" | `Composer.test.tsx` "còn cửa sổ…"; `conversationRules.test.ts` "remaining…" | như trên | PASS |
| Human Agent → "Trả lời (nhân viên)", gửi `humanAgent: true` | `Composer.test.tsx` "Human Agent…" | như trên | PASS |
| `read_only` → câu chế độ chỉ xem; không có `conversation.reply` → khóa kèm lý do quyền | `Composer.test.tsx` "Page chỉ xem…", "không có conversation.reply…" | như trên | PASS |
| CH03, HV17 sửa: Enter gửi, Shift+Enter không gửi, ô gõ trống lại | `Composer.test.tsx` "CH03: Enter gửi chữ…" | như trên | PASS |
| 503 `retry_later` rồi 201 → hai yêu cầu cùng `Idempotency-Key` | `Composer.test.tsx` "503 retry_later rồi 201…" | như trên | PASS |
| 409 `send_not_allowed` → câu theo `denyReason`, chữ đã gõ giữ | `Composer.test.tsx` "gửi bị 409 send_not_allowed…" | như trên | PASS |
| CH07: gõ `/xin` → menu có `/xindiachi` (gọi `q=xin`, `pageId`); Enter → ô gõ có chữ đã điền biến, không gửi; Esc đóng | `Composer.test.tsx` "CH07: gõ /xin…", "menu mẫu: Esc đóng…" | như trên | PASS |
| CH07: ghi chú nội bộ → `addConversationNote`, không gửi khách; nhãn "Chỉ nhân viên thấy" | `Composer.test.tsx` "CH07: tab Ghi chú nội bộ…"; `MessageBubble.test.tsx` "đủ loại tin…" | như trên | PASS |
| CH04: ảnh → `createUpload` mục đích `chat_outgoing` → chờ ready → `sendMessage` `attachmentFileId`; ảnh bị từ chối → câu lỗi, không gửi | `Composer.test.tsx` "CH04…", "tải ảnh bị từ chối…" | như trên | PASS |
| Sáu trạng thái gửi; Bỏ/Gửi lại chỉ ở `in_doubt`, `expired` | `SendStatus.test.tsx` "sáu trạng thái…"; `conversationRules.test.ts` "statusView…" | như trên | PASS |
| CH18: "Gửi lại" ở `in_doubt` → hộp xác nhận "có thể nhận tin này hai lần" → `resendMessage` | `SendStatus.test.tsx` "CH18…" | như trên | PASS |
| CH19: `expired` Gửi lại → `resendMessage`, Bỏ → `discardMessage`; HV14 sửa: `failed` `rate_limited` có Gửi lại, tin tự động đã bỏ không nút | `SendStatus.test.tsx` "CH19…", "HV14 sửa…" | như trên | PASS |
| 04 mục 13: `<img src=x onerror=alert(1)>` hiện nguyên văn, DOM không có `img` | `MessageBubble.test.tsx` "04 mục 13…" | như trên | PASS |
| TP04: bấm ảnh mới gọi `getAttachmentUrl` rồi hiện ảnh; PDF → liên kết có `download`, không xem trước | `MessageBubble.test.tsx` "TP04: bấm ảnh…", "TP04: tệp PDF…" | như trên | PASS |
| HV10–HV12: lịch sử, hệ thống, tự động, ngoài CRM, ghi chú, tên nhân viên; HV22: "Xem hết (2.000 ký tự)" | `MessageBubble.test.tsx` "đủ loại tin…", "HV22…" | như trên | PASS |
| CH13, HV21 sửa: "Khách đã sửa", lịch sử thu gọn, bấm mới tải; khách xóa giữ nội dung trước khi xóa | `MessageBubble.test.tsx` "CH13, HV21 sửa…", "CH13: khách xóa…" | như trên | PASS |
| CH05: `privateReply.state = sent` → nút khóa, chữ "Đã nhắn riêng"; quá 7 ngày → "Quá 7 ngày"; trả lời công khai/nhắn riêng gửi đúng `commentMessageId` | `CommentActions.test.tsx` (5 ca đầu); `conversationRules.test.ts` "privateReplyLock…" | như trên | PASS |
| CH11: "Đã ẩn trên Page", "Hiện lại trên Page" → `unhideComment`, "Ẩn trên Page" → `hideComment`; kênh không hỗ trợ → không nút; hội thoại comment ô gõ chỉ ghi chú | `CommentActions.test.tsx` "CH11…", "Page không hỗ trợ…", "hội thoại comment…" | như trên | PASS |
| CH10: nhịp tim mỗi 10 s (3 lần trong 30 s, đồng hồ giả), đổi trạng thái gửi ngay; người khác `typing` → "An đang trả lời…", "đang lên đơn", không hiện mình; mỗi nhịp khóa mới | `PresenceBar.test.tsx` (4 ca) | như trên | PASS |
| CH01 phía hội thoại: báo hiệu `conversation.changed` → tin mới hiện; đánh dấu đã đọc tới tin khách mới nhất; "Xem tin cũ hơn" dùng `before`; CH06 "Nhận" → `claimConversation`; trạng thái → `expectedVersion` | `ConversationView.test.tsx` (6 ca) | như trên | PASS |
| Reviewer vòng 1: chỉ có quyền xem → không có nút "Gửi mẫu", câu lý do quyền (gửi cần `conversation.reply` ở Page của dòng) | `FollowUps.test.tsx` "chỉ có quyền xem…" | `npx vitest run src/screens/inbox/FollowUps.test.tsx` | viết cùng bản sửa; `Tests 4 passed (4)` |
| CH16 (Inbox D): bật "Khách chưa trả lời" → `listFollowUps` `afterHours=3`, URL `followup=1`, đổi 6 giờ; "Gửi mẫu /nhac" → render rồi gửi chữ (hai khóa), "Đã gửi mẫu"; chưa có mẫu → không nút | `FollowUps.test.tsx` (3 ca); `inboxRules.test.ts` (thêm `followUp`) | `npx vitest run src/screens/inbox` | PASS |
| Không làm hỏng Inbox, menu (sau gộp m4) | `src/screens/inbox/*.test.tsx`, `src/app/screens.test.tsx` | `npx vitest run src/screens/inbox src/app/screens.test.tsx` | `Test Files 15 passed (15)`, `Tests 117 passed (117)` |
| tsc, eslint, prettier, jscpd | — | `npx tsc --noEmit`; `npx eslint src/screens/inbox e2e/hoi-thoai.spec.ts`; `npx prettier --check …`; `make jscpd` | sạch; jscpd lần đầu 3 đoạn trùng mới (useNow giống LivePage, vòng khổ màn giống inbox.spec), đã gỡ → `Found 0 clones` |
| ci-task | — | `ci-task.sh` | `static: success, 4 phút 15 giây` (gồm console-check); `test từ xa: không gói nào` (không đổi Go); `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh 4370f657)`, run 45866551391; sau sửa vòng 1: `static: success, 2 phút 15 giây`, `CI-TASK XANH`, run 45868035658 |

## E2E viết sẵn, chưa chạy

- `console/e2e/hoi-thoai.spec.ts`: "Hội thoại {360,390,1512} px › tin nhắn đủ loại, trạng thái gửi: không tràn, không
  cuộn ngang" và "› comment: ba nút luôn hiện, 2.000 ký tự gọn 8 dòng, không tràn"; chụp
  `test-results/hoi-thoai-{360,390,1512}.png`, `hoi-thoai-comment-*.png` để so với `docs/mockups/m4/png/hoi-thoai-*.png`.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Đường tệp `src/screens/inbox/conversation/` thay `features/conversation/` (Quyết định 1).
- Thêm `markConversationRead` (Quyết định 3) và Inbox trạng thái D (Quyết định 9, ghi chú điều phối).
- `privateReply.state` thật là `none`/send_status, không có `used` (Quyết định 7).

## Việc phát hiện thêm

- `FollowUpItem` không có đoạn tin cuối của shop ("Bạn: …" trong mockup Inbox D): thêm trường là việc của hợp đồng.
- `ChatPostRef` chỉ có `snippet`, không có giờ đăng, ảnh bài, liên kết "Mở bài" như mockup trạng thái C: thẻ bài hiện
  đoạn bài; thêm trường là việc hợp đồng.
- Lý do tự ẩn comment (vd "tự ẩn vì có SĐT") không có trong `ChatMessage`: chip "Đã ẩn trên Page" hiện câu chung.
- `useUploads` nằm ở `screens/products` mà Banner, Nhập Excel, Hội thoại cùng dùng: nên đưa về `src/shared` khi có dịp
  (như `SEARCH_DELAY_MS`, ghi chú reviewer Task 174).

## Reviewer

Vòng 1: CẦN SỬA — hai phát hiện: (1) nút "Gửi mẫu /x" của "Khách chưa trả lời" hiện cả với người chỉ có
`conversation.read` (máy chủ từ chối `sendMessage`): đã sửa, `FollowUps` nhận `grants`, kiểm `canOnPage(…,
"conversation.reply", pageId của dòng)`, không quyền → câu "Bạn chưa được cấp quyền trả lời ở Page này", thêm test;
(2) khóa cache danh sách mẫu của FollowUps (`[conversation.templates, pageId, "all"]`) trùng được khóa của menu "/" khi
gõ "/all": đổi thành `[conversation.templates, "follow-ups", pageId]`. Ghi chú không bắt buộc: Task 176 cần biết nút
"Nhận" đã ở đầu hội thoại (Quyết định 10) để không vẽ hai lần; `useNow`, `useUploads` nên đưa về `src/shared`.

Vòng 2: ĐẠT — hai bản sửa đúng (kiểm `conversation.reply` theo Page của từng dòng, test mới đỏ trước bản sửa; hai khóa
cache không thể trùng vì mã Page là UUID); soát lại 33 tệp theo 7 mục, không phát hiện phải sửa. Ghi chú không bắt
buộc (không làm): lúc `getMe` đang tải, `grants` tạm rỗng nên có thể thoáng hiện câu thiếu quyền / ô gõ khóa (khung đã
nạp sẵn `identity.me` cùng khóa nên thường không xảy ra).

Kết luận: ĐẠT
