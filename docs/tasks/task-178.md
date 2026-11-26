# Task 178 — Màn cài đặt — kết nối Page, chế độ chỉ xem, quy tắc tự động, mẫu, nhãn, cài đặt Page

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 178 = M4-38). Loại việc (quy
trình mục 3): A (màn hình). Mockup đã chốt: `docs/mockups/m4/ket-noi-page.html`, `docs/mockups/m4/cai-dat.html`
(HV32–HV38, HV52 sửa, HV53–HV55; `docs/mockups/m4/README.md`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task chỉ đổi console).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn gọi (client sinh `console/src/api`): `listChannelAccounts`,
  `setChannelSendMode`, `setChannelHumanAgent`, `startFacebookOAuth`, `exchangeFacebookOAuth`, `connectFacebookPage`,
  `disconnectChannel`, `runChannelHealthCheck`, `getPageSettings`, `updatePageSettings`, `listAssignableUsers`,
  `listAutomationRules`, `updateAutomationRule`, `listConversationTags`, `createConversationTag`,
  `updateConversationTag`, `deleteConversationTag`, `listReplyTemplates`, `createReplyTemplate`, `updateReplyTemplate`,
  `deleteReplyTemplate`, cùng `listTrafficSources`, `getOrderSettings` của M2 (đọc). Mọi operation đã làm trên m4
  (`tests/arch/testdata/m4_pending.txt` rỗng).
- Chỗ dùng trên màn hình: thư mục màn mới `console/src/screens/kenh/` (route.tsx tự đăng ký 6 đường: `/settings/channels`,
  `/settings/inbox`, `/settings/channels/facebook/callback`, `/settings/pages/:pageId`, `/settings/tags`,
  `/settings/templates`). Chạm khung chung: `console/src/app/nav.ts` thêm nhóm "Hộp chat" đứng đầu (đúng từng chữ như
  bản của Task 174 đang làm song song) và câu kiểm NAV_GROUPS trong `console/src/app/screens.test.tsx`.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run src/screens/kenh src/app/screens.test.tsx src/app/errors.test.ts
  src/shell/Layout.test.tsx`, `npx tsc --noEmit`, `npx eslint src/screens/kenh src/app`; console-check trong ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (chỉ mã Go); console:
  `src/shared/index.ts`, `src/app/{errors,idempotency,queryClient,screenTestkit,routeTestkit,time}.ts`,
  `src/screens/staff/staffData.tsx` (`Dialog`, `RefusalBox`), `src/screens/don/orderData.ts` (`useOrderSettings`,
  `ORDER_TOPIC`), `src/screens/web-banners/listEditor.tsx` (`ConflictDialog`), `src/screens/don/donTestkit.tsx`
  (`stubDialog`); tìm "Dialog", "Confirm", "beforeunload", "useBlocker", "charCount".
- Dùng lại: `unwrap`, `errorMessage`, `ApiError` (lỗi); `useWriteAction` (mọi đường ghi, trừ một chỗ ở dưới);
  `queryKey` + chủ đề realtime `channel.status`, `conversation.changed` (máy chủ Task 145, 160); `Dialog` và `RefusalBox`
  của staffData (hộp thoại `<dialog>` showModal, hộp báo chặn); `useOrderSettings` (hạn đơn nháp, Q2);
  `renderScreens`, `stubApi`, `emptyWrites`, `meReply`, `reply`, `routeRows` trong test; lớp CSS có sẵn của console
  (`card`, `btn`, `toggle`, `chip`, `box`, `dlg`, `field`, `field-err`, `tbl`, `kv`, `radios`, `act`).
- Viết mới vì: `SystemTokenForm` không dùng `useWriteAction` — hook đó nhớ thân yêu cầu (`JSON.stringify(body)`) trong
  ref để dùng lại khóa, tức mã system user sẽ nằm lại trong bộ nhớ của ứng dụng sau khi gửi (kế hoạch cấm). Form gọi
  thẳng `connectFacebookPage` một lần với khóa mới mỗi lần bấm (kết nối lại cùng Page là lũy đẳng ở máy chủ, Task 149
  quyết định 6). Bảo vệ rời trang khi còn khối chưa lưu (HV52 sửa) viết mới (`useLeaveGuard`): console dùng React
  Router chế độ khai báo nên không có `useBlocker`; chặn bấm liên kết nội bộ ở pha capture + `beforeunload`. Lớp CSS
  của mockup M4 chưa có trong console (`chrow`, `ch-ic`, `feats`, `seg`, `qrs`, `lines-f`, `slots`, `lbs`, `lb-*`,
  `olines`, `cnt`, `tabs`) chép từ `m4.css`/`m1.css` vào `kenh.css`, bọc trong `.chat-set` để không đè lớp cùng tên Task
  174 có thể thêm.

## Soát thiết kế

- 5.1–5.8, 5.10; 6.1, 6.3–6.11: không áp dụng (task chỉ có màn hình, không đổi máy chủ, không database, tiền, hàng đợi).
- 5.9 đường ghi: mọi yêu cầu ghi gửi `Idempotency-Key` (hợp đồng đòi); `useWriteAction` tự gửi lại cùng khóa khi bận.
  Mọi thứ đổi theo thao tác (mã Page, version, kind) nằm trong `body` của `run` (luật của hook).
- 6.1 phía màn hình (hai người cùng sửa): mỗi khối giữ `version` đọc lúc mở khối và gửi `expectedVersion`; 409
  `version_conflict` → "Có người vừa sửa, tải lại trang." kèm nút "Tải lại" (tải lại dữ liệu và dựng lại khối). Báo
  hiệu realtime không đè ô đang sửa (khối giữ giá trị nhập và version gốc).
- 6.2 bấm hai lần: nút khóa khi đang gửi; `useWriteAction` trả lại đúng lời hứa đang chạy.
- 5.11 thư viện: không thêm.
- 5.12 mẫu: màn tự đăng ký bằng `route.tsx`; API chỉ qua client sinh; câu lỗi qua `errorMessage`.
- 6.12 dữ liệu bí mật: mã system user là ô `type=password`, `autocomplete=off`, ô không điều khiển bằng state (đọc bằng
  ref lúc gửi), xóa ô ngay khi gửi, không vào `localStorage`, `sessionStorage`, bộ đệm TanStack Query hay ref của hook ghi;
  `code`, `state` của Facebook bị xóa khỏi thanh địa chỉ (`history.replaceState`) trước khi gọi `exchangeFacebookOAuth`,
  và không đưa vào bộ đệm truy vấn. `code`, `state` đi qua `useWriteAction` (nhớ thân trong ref tới khi xong để gửi lại
  cùng khóa): chấp nhận vì mã đăng nhập chỉ dùng được một lần, hết hạn nhanh; kế hoạch chỉ cấm việc này với mã system user.
- 6.13 quyền: menu và route theo quyền thật (`channel.manage` toàn shop; `conversation.settings` theo Page, nhãn và mẫu
  chung cần phạm vi all — Task 161 quyết định 1). Không có quyền sửa trên Page → khối hiện chỉ xem, nút Lưu khóa kèm lý
  do (mockup B "khong-quyen-sua"); máy chủ vẫn kiểm (404/403 hiện bằng `RefusalBox`).
- `authorizeUrl` từ máy chủ chỉ được mở khi là `https://www.facebook.com/…` (không chuyển trang tới đường lạ).

## Quyết định thay chủ shop

1. **Thư mục `console/src/screens/kenh/`** thay `console/src/features/settings/` của kế hoạch: console tự nạp màn từ
   `src/screens/<tên>/route.tsx` (Task 393); Task 174 (song song) đã chọn đúng thư mục và sáu đường này cho khung (test
   `kenh/route.test.tsx` của 174), nên dùng chung để lượt gộp chỉ thay khung chữ bằng màn thật. Phương án khác: thư mục
   riêng — hai `route.tsx` khai trùng đường làm hỏng lần nạp màn.
2. **Cài đặt hộp chat là một trang có thẻ** (mockup: thẻ theo từng Page, "Nhãn", "Mẫu trả lời"). Mỗi thẻ là một đường
   (`/settings/pages/:pageId`, `/settings/tags`, `/settings/templates`); `/settings/inbox` chuyển sang thẻ Page đầu tiên
   (không có Page → thẻ Nhãn). Mục menu "Cài đặt hộp chat" sáng ở cả ba đường (`also`).
3. **Màu nhãn** là tên token của hệ thiết kế (hợp đồng: "Tên token màu"): `soft` (Hồng, `lb-1`), `surface` (Xám,
   `lb-2`), `success` (Xanh, `lb-3`), `edge` (Viền, `lb-4`); màu lạ vẽ như `surface`. Task 174 dùng `soft` trong test.
   Phương án khác: tên tiếng Việt. Đảo ngược: đổi bảng `TAG_COLORS`.
4. **Lỗi 422 `invalid_input` không nói ô nào** (thân lỗi chỉ có mã): màn tự xác định ô theo dữ liệu đã gửi — mẫu có biến
   ngoài bảng biến → lỗi dưới ô Nội dung "Biến {x} không có. Dùng các biến ở bảng dưới."; còn lại → hộp lỗi chung của
   khối. Phím tắt, độ dài, giới hạn số câu, số nút, 20 ký tự, khoảng giờ kiểm tại chỗ trước khi gửi. Biến lạ không chặn
   trước khi gửi (máy chủ là nơi quyết định danh sách biến; đúng ca kế hoạch "API 422 → lỗi dưới ô").
5. **Tên trùng (409 `already_exists`)** hiện tại ô Tên nhãn / Phím tắt ("Nhãn "…" đã có.", "Phím tắt /… đã có.").
6. **"Kiểm tra kết nối"** (202, kết quả về qua báo hiệu `channel.status`): nút khóa "Đang kiểm tra…" tới khi danh sách
   tải lại có `tokenCheckedAt` khác giá trị lúc bấm (chỉ so giờ máy chủ với giờ máy chủ, sửa sau reviewer vòng 1), hoặc 60 giây; kết quả hiện ở dòng "Lần kiểm gần nhất". Hợp đồng không
   trả số trường webhook thiếu cụ thể, nên hộp "thiếu 2 trường" của mockup hiện là "Đã đăng ký n/11 trường" từ
   `subscribedFields`.
7. **Trạng thái kênh:** `active` → "Đang hoạt động"; `degraded` → chip "Mất quyền" + hộp đỏ + nút chính "Kết nối lại"
   (HV36; câu "CRM vẫn nhận và lưu tin" đúng máy chủ Task 149/150); `disconnected` → chip "Đã ngắt", nút "Kết nối lại",
   không có công tắc. "Kết nối lại" chạy lại luồng đăng nhập Facebook (máy chủ nối lại cùng dòng, Task 149).
8. **Khối hiện theo `features` của kênh (HV33):** tự ẩn cần `hideComment`, tự trả lời comment cần `comments`, Live và đọc
   comment định kỳ cần `liveComments`/`comments`, nút trả lời nhanh của Tin chào cần `quickReplies`. Tài khoản không có
   `features` (kênh chưa bật ở tiến trình, Task 150 quyết định 9) hiện mọi khối của Facebook khi `channel = facebook`.
9. **Nhắn lại khách chưa trả lời** chưa cấu hình (`version = 0`, `templateId` = uuid rỗng, Task 165 quyết định 5): ô mẫu
   để trống "Chọn mẫu", bật mà chưa chọn mẫu → báo tại ô, không gửi.
10. **Ngoại lệ bài, "Chỉ bài chọn"** (`exceptPostIds`, `postIds`): API chưa có danh sách bài để chọn; màn giữ nguyên giá
    trị đã lưu khi gửi lại, hiện số bài, chưa vẽ nút "+ Chọn bài" (không vẽ hành vi máy chủ chưa có). Ghi ở Việc phát
    hiện thêm.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Callback `?code=abc&state=xyz` → `exchangeFacebookOAuth({code:"abc",state:"xyz"})`, `location.search` rỗng trước lời gọi | `FacebookCallback.test.tsx` "?code=abc&state=xyz → exchangeFacebookOAuth…" (bọc fetch ghi `window.location.search` lúc gọi exchange) | `cd console && npx vitest run src/screens/kenh` | `Tests 74 passed (74)` |
| Hộp chọn Page: Page thiếu quyền mờ kèm lý do, chọn → `connectFacebookPage{pageId,sessionId}`; lỗi `page_connected_elsewhere`/`page_not_accessible`/`oauth_state_invalid`; thiếu code | `FacebookCallback.test.tsx` (các ca còn lại, `it.each` 409/422) | như trên | qua |
| Mã system user: ô rỗng ngay khi gửi (cả lúc chờ), không có trong localStorage, sessionStorage, bộ đệm truy vấn/mutation, DOM | `SystemTokenForm.test.tsx` "gửi: thân có systemUserToken; ô rỗng…" + ca lỗi 422 | như trên | qua |
| Tắt chỉ xem → hộp xác nhận đúng câu "Bật gửi thật cho Page ELIMAZ?" / "Khách sẽ nhận tin từ CRM."; Hủy → không gọi API | `ChannelsPage.test.tsx` "tắt chỉ xem → hộp xác nhận đúng câu; Hủy → không gọi API", "xác nhận → setChannelSendMode…" | như trên | qua |
| HT15 (Page mất quyền, kết nối lại), Human Agent "Chỉ bật khi Meta đã cấp", Kiểm tra kết nối (chờ kết quả, 60 s, không so giờ máy nhân viên với giờ máy chủ — 2 ca lệch đồng hồ), Ngắt hỏi lại, "Kết nối Page" cùng tab, chặn địa chỉ lạ | `ChannelsPage.test.tsx` (17 ca) | như trên | qua |
| Nút trả lời nhanh 21 ký tự báo tại ô trước khi gửi; "Thêm nút" khóa ở nút 13; "Thêm câu" khóa ở câu 5 | `AutomationForms.test.tsx` "nút 21 ký tự…", "'Thêm nút' khóa ở nút thứ 13…", "'Thêm câu' khóa ở câu thứ 5…" | như trên | qua |
| 409 `version_conflict` → "Có người vừa sửa, tải lại trang" (quy tắc, cài đặt Page, mẫu) | `AutomationForms.test.tsx` "409 version_conflict → …" (2 ca), `TemplatesPage.test.tsx` "sửa chỉ gửi trường đã đổi…; 409 version_conflict" | như trên | qua |
| Mẫu có `{abc}` → API 422 → lỗi dưới ô nội dung | `TemplatesPage.test.tsx` "mẫu có {abc} → API 422 → lỗi hiện dưới ô nội dung" (kiểm `previousElementSibling` là ô nội dung, `aria-invalid`) | như trên | qua |
| CH11, CH12, CH16 cài đặt; HV33 ẩn khối theo tính năng; quyền chỉ xem; Live khung giờ chồng; hai khối cùng bản cài đặt Page không xung đột giả; HV52 "Chưa lưu" + hỏi khi rời trang | `AutomationForms.test.tsx` (22 ca) | như trên | qua |
| Nhãn: tên trùng tại ô, bốn màu, 409 `already_exists` tại ô, PATCH chỉ trường đổi, xóa hỏi lại; chỉ xem khi không có quyền cả shop | `TemplatesPage.test.tsx` describe "Nhãn" (5 ca) | như trên | qua |
| Đường, quyền, menu | `route.test.tsx`; `src/app/screens.test.tsx` (NAV_GROUPS có "Hộp chat" đầu) | `npx vitest run src/app src/shell src/screens/kenh` | `Tests 191 passed (191)` |
| Test đỏ khi bỏ mã (thay bước "chạy → FAIL"): 7 lần đảo tạm (bỏ `clearOAuthParams`, bỏ xóa ô mã, bỏ hộp xác nhận, nới 20→25 ký tự, 5→6 câu, bỏ lỗi biến tại ô, đổi câu xung đột) | các tệp test trên | `npx vitest run src/screens/kenh/<tệp>` | mỗi lần 1–3 ca đỏ; mã trả lại nguyên (diff rỗng với bản lưu) |
| tsc, eslint, prettier, jscpd, test kiến trúc | — | `npx tsc --noEmit`; `npx eslint src/screens/kenh src/app`; `npx prettier --check src/screens/kenh src/app`; `make jscpd`; `go test -count=1 ./tests/arch/` | sạch; `Found 0 clones`; `ok … tests/arch 139.672s` |
| Cổng của task | — | ci-task.sh | `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh ebf50c54)` |

## E2E viết sẵn, chưa chạy

(kế hoạch không có spec Playwright cho task này)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Đường tệp `console/src/screens/kenh/` thay `console/src/features/settings/` (Quyết định 1); thêm tệp
  `kenhData.ts`, `kenhRules.ts`, `settingsKit.tsx`, `connectErrors.tsx`, `kenhTestkit.tsx`, `kenh.css`, `route.tsx`
  và test `route.test.tsx`, `kenhRules.test.ts`; test của Nhãn nằm trong `TemplatesPage.test.tsx`.
- Bước "chạy → FAIL": mã màn viết trước test trong phiên này; thay bằng 7 lần đảo tạm mã (bảng bằng chứng) để chứng
  minh test đỏ khi thiếu hành vi.
- "So ảnh chụp với mockup" (Step 4) chưa làm: chủ shop 24/11 cấm chạy Playwright/so ảnh trong task.
- Gộp m4 trước review: m4 không có commit mới ("Already up to date").
- Chạm khung chung: `console/src/app/nav.ts` (dòng "Hộp chat" đúng như bản Task 174), `console/src/app/screens.test.tsx`
  (câu NAV_GROUPS như Task 174). `kenh/route.tsx`, `kenh/route.test.tsx` trùng tên với khung của Task 174 (chưa gộp):
  lượt gộp lấy bản của Task 178 (cùng sáu đường, quyền, order; thêm `alsoRequires conversation.read` và `also`).

## Việc phát hiện thêm

- Chọn bài cho ngoại lệ tự ẩn / "Chỉ bài chọn" của tự trả lời cần một API liệt kê bài của Page (chưa có trong hợp đồng).

## Reviewer

Vòng 1: CẦN SỬA — "Kiểm tra kết nối" so giờ máy nhân viên (`Date.now()`) với `tokenCheckedAt` của máy chủ (bẫy "một
đồng hồ"). Đã sửa: nhớ `tokenCheckedAt` lúc bấm, xong khi giá trị đó đổi; thêm 2 ca lệch đồng hồ trong `ChannelsPage.test.tsx`.
Ghi chú không bắt buộc (code/state trong ref của hook ghi) ghi ở Soát thiết kế 6.12.

Vòng 2: **ĐẠT** — phát hiện vòng 1 đã sửa đúng (so `tokenCheckedAt` lúc bấm với giá trị mới, `Date.now()` chỉ làm nonce;
ca máy chủ chậm 5 phút chứng minh). 7 mục đều đạt: đủ Interface và 6 ý test của Step 1, bằng chứng khớp số ca, Ảnh
hưởng khớp, không gói util, không cách cũ sót, không test bị tắt/nới, luật console giữ (client sinh, Idempotency-Key,
không `as`, mã system user không vào kho nào, code/state xóa khỏi thanh địa chỉ trước exchange). Ghi chú không bắt buộc:
ca "giờ máy chủ chưa đổi thì chưa báo ổn" yếu (khẳng định lúc lời gọi kiểm còn chạy) — không phải lỗi phải sửa.

Kết luận: ĐẠT
