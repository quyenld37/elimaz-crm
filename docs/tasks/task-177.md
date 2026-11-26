# Task 177 — Màn Live và danh sách thiếu địa chỉ

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 177 = M4-37). Loại việc (quy trình mục 3): A (màn hình).
Mockup: `docs/mockups/m4/live.html`, `docs/mockups/m4/thieu-dia-chi.html` (HV40–HV51, sửa HV49 ở "Kết luận duyệt").

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ sửa `console/` (không chạm Go).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn dùng (client sinh, không sửa): `listLiveSessions`,
  `startLiveSessionManually`, `listLiveComments`, `getLiveCommentContext`, `claimLiveComment`, `listPendingDrafts`,
  `remindPendingDrafts`, `listChannelAccounts`, `getPageSettings`, `listReplyTemplates`, `setPresence`, `getPresence`,
  `listCustomerConversations`, `searchVariants` (qua `VariantPicker` của M2), `listWarehouses`/`listBalances` (qua
  `VariantPicker`), `getMe`.
- Chỗ dùng trên màn hình: thư mục màn mới `console/src/screens/live/` (route `/live`, `/live/:sessionId`,
  `/live/thieu-dia-chi`, `/live/:sessionId/thieu-dia-chi`). **Chạm khung chung:** thêm nhóm menu `"Hộp chat"` đứng đầu
  `NAV_GROUPS` (`console/src/app/nav.ts`, theo `docs/mockups/m4/khung.js`) và sửa ca ghim thứ tự nhóm ở
  `console/src/app/screens.test.tsx` — Task 174 cũng thêm nhóm này; lượt gộp giữ một dòng `"Hộp chat"`.
  Dùng lại `VariantPicker` (`screens/len-don/VariantPicker.tsx`) không sửa. `screens/len-don/orderTestkit.tsx`: hàm
  test `balance` đổi thành `export` để testkit Live dùng lại (chỉ test dùng, không đổi chữ ký).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: vitest các tệp mới `screens/live/*.test.ts(x)`, `app/screens.test.tsx`, `app/errors.test.ts`;
  `npx tsc --noEmit`; `npx eslint` các tệp sửa; ci-task (console-check ở bể).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `console/src/shared/index.ts` (`useWriteAction`,
  `subscribeChanged`, `queryKey`, `unwrap`, `ApiError`, `errorMessage`, `shortTime`, `hasPermission`), màn mẫu
  `screens/products/ProductList.tsx` (danh sách ảo `@tanstack/react-virtual`), `screens/len-don/VariantPicker.tsx`
  (tìm biến thể, có thể bán), `screens/products/productRules.ts` (`formatMoney`), `screens/len-don/orderRules.ts`
  (`displayPhone`), `app/screenTestkit.tsx` (API giả, `renderScreens`).
- Dùng lại / viết mới vì: dùng lại tất cả các mục trên; `VariantPicker` dùng nguyên (kế hoạch cấm bản thứ hai).
  Viết mới: hook `useLiveComments` (gộp theo `messageId`, mốc `after`, gom báo hiệu) và hàm `unseenCount` — chưa có
  danh sách nào nạp thêm theo mốc thời gian (Inbox của Task 174 chạy song song, chưa có trên m4); danh sách ảo dùng
  `useVirtualizer` theo khung cuộn riêng (ProductList cuộn theo cửa sổ, không hợp khung comment cao cố định).

## Soát thiết kế

Mục 5:
- 5.1–5.3, 5.7, 5.10: không áp dụng: màn hình, không có dữ liệu, bảng, truy vấn mới.
- 5.4–5.6: không áp dụng ở màn hình; chốt comment là một lệnh `claimLiveComment` (một transaction ở máy chủ, Task 172).
- 5.8: màn chỉ gọi client sinh `console/src/api/` (luật console 2), không `fetch` tay.
- 5.9: mọi lần ghi (`claimLiveComment`, `remindPendingDrafts`, `startLiveSessionManually`) đi qua `useWriteAction`
  (khóa sinh khi bấm, giữ khi tự gửi lại); `setPresence` khóa mới mỗi nhịp (Q9 của hợp đồng).
- 5.11: không thêm thư viện (`@tanstack/react-virtual` đã có trong package.json).
- 5.12: theo quy ước màn tự đăng ký (`src/app/loadScreens.ts`), testkit API giả như các màn M2/M5.

Mục 6:
- 6.1: hai người cùng chốt một comment — máy chủ trả 409 `comment_claimed`, màn hiện "{tên} đang lên đơn cho comment
  này" và nút mở đơn; thêm vào nháp gửi `expectedVersion`, `version_conflict` → "Có người vừa sửa, tải lại".
- 6.2: bấm hai lần → `useWriteAction` trả lời hứa đang chạy, nút khóa khi đang gửi; 503/409 in-progress gửi lại cùng khóa.
- 6.3–6.11: không áp dụng: không chạm database, hàng đợi, tiền ở màn hình (Tạm tính chỉ hiện, tiền `number` đồng
  từ API, máy chủ tính lại).
- 6.12: không ghi log; SĐT chỉ hiện trên màn cho người có quyền (máy chủ đã lọc `phones` theo quyền).
- 6.13: menu "Live" cần `live.operate` và `conversation.read`; nút chốt khóa khi thiếu `order.create` (mode new) /
  `order.update` (thêm vào nháp), máy chủ vẫn kiểm từng lần (403 → câu chưa có quyền). Nội dung comment hiện dạng chữ
  thuần (không HTML).

## Quyết định thay chủ shop

1. **Thư mục màn là `console/src/screens/live/`** (kế hoạch ghi `console/src/features/live/`): console thật dùng quy
   ước `screens/<tên>/route.tsx` tự đăng ký (Task 393), không có `features/`. Đổi lại: dời thư mục.
2. **Nhóm menu "Hộp chat"** thêm tối thiểu vào `NAV_GROUPS` (đứng đầu, như khung.js của mockup M4) để hai mục Live và
   "Đã chốt, thiếu địa chỉ" có chỗ; Task 174 làm khung chung, lượt gộp giữ một dòng.
3. **"Dùng số này"**: `claimLiveComment` không nhận SĐT và đơn nháp hoàn tất ở khung bên phải (Task 176). Nút chép số vào
   bộ nhớ tạm và hiện "Đã chép số, dán vào đơn khi hoàn tất" — không giả vờ máy chủ đã lưu số. Khác: thêm trường vào
   hợp đồng (việc backend). Ghi ở Việc phát hiện thêm.
4. **Chỉ vẽ số máy chủ có**: dải số của phiên hiện "Bắt đầu, đã live, Nhận được, Nguồn"; bỏ "Có SĐT", "Đã lên đơn"
   (API không trả); phiên đã kết thúc hiện "Nhận được" và "Trên Facebook" (`metaCommentCount`), lệch thì hộp đỏ.
   Băng "Đang mở lại luồng comment…" (LV10) không vẽ: API không báo trạng thái mở lại; băng hỏi định kỳ hiện khi
   `activeSource = poll`. Chip "Đã nhắn riêng" không kèm tên người (API không trả).
5. **Trang thiếu địa chỉ theo một phiên** (API `listPendingDrafts` theo phiên): ô chọn phiên liệt kê phiên hôm nay, bỏ
   mục "Mọi phiên hôm nay" của mockup; `/live/thieu-dia-chi` chọn phiên mới nhất. Không phân trang ("Tải thêm"): API trả
   cả danh sách. Cột "Xin địa chỉ" theo `ask` (không có giờ gửi, không biết nhắn riêng hay hộp thư): `queued` không lý do
   → "Đã xin địa chỉ"; có lý do hay `skipped_policy` → "Chưa xin được" + lý do; `not_requested` → "Chưa xin" + lý do.
   "Khách trả lời" không có giờ.
6. **"Mở hội thoại để hoàn tất"**: `PendingDraft` không có mã hội thoại; bấm thì đọc `listCustomerConversations` của
   khách, mở hội thoại hộp thư trên Page của phiên (không có thì hội thoại bất kỳ trên Page đó), không có thì báo
   "Khách chưa có hội thoại trên Page này".
7. **Mẫu tin nhắc**: `remindPendingDrafts` đòi `templateId`; dùng `askAddressTemplateId` của cài đặt Page của phiên.
   Page chưa chọn mẫu → nút Nhắc khóa, lý do "Page chưa chọn mẫu xin địa chỉ (Cài đặt hộp chat)".
8. **Lưới an toàn 20 s và resync tải lại cả danh sách comment** (đúng trạng thái chốt của comment cũ hơn 30 s mà mốc
   `after` không lấy lại); báo hiệu `live.comments` của phiên chỉ tải phần sau mốc, gom: đang tải thì chờ xong rồi tải
   thêm một lần.
9. **Ai khác đang xem** trong khung chốt: `getPresence` của hội thoại comment, tải lại theo nhịp 10 s cùng nhịp tim
   `drafting_order` của mình.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| LV02 màn mượt: 600 comment trong 6 lô → dưới 80 dòng DOM; đang xem phía trên vị trí giữ nguyên, "↓ 600 comment mới" rồi giảm khi cuộn | `LiveCommentList.test.tsx` "600 comment tới trong 6 lô khi đang xem phía trên…"; `useLiveComments.test.ts` "unseenCount: 600 mới…" | `cd console && npx vitest run src/screens/live` | 4 tệp, 40 test qua |
| Ở cuối danh sách: comment mới tự hiện, không có nút | `LiveCommentList.test.tsx` "đang ở cuối danh sách…" | như trên | qua |
| Tải theo `after` khi nhận `live.comments`, lùi 30 s bỏ trùng, gom báo hiệu, bỏ báo hiệu phiên khác, resync tải lại cả, resync khi lọc "Chưa xử lý" bỏ comment vừa bị chốt (tới rỗng), lọc gửi tham số, patch tại chỗ | `useLiveComments.test.ts` 8 ca của `describe("useLiveComments")` | như trên | qua |
| LV01/HV41 dòng comment: Có SĐT, Đã ẩn trên Page, Đã lên đơn · người, Khách đã có đơn nháp, Đã nhắn riêng; chữ thuần (không `img` từ `<img onerror>`); tên 60 ký tự có `ellip` | `LiveCommentList.test.tsx` "dòng có nhãn…" | như trên | qua |
| Lọc "Chỉ comment có SĐT", "Chưa xử lý" | `LiveCommentList.test.tsx` "lọc…" | như trên | qua |
| HV46 hỏi định kỳ → băng; phiên đang live chọn mặc định; HV47 phiên kết thúc lệch → "Thiếu 5 comment." | `LiveCommentList.test.tsx` "phiên đọc bằng hỏi định kỳ…", "phiên đã kết thúc…" | như trên | qua |
| HV48 thêm phiên bằng link video (có khóa); không có live.operate → không có nút | `LiveCommentList.test.tsx` "chưa có phiên hôm nay…", "không có live.operate…" | như trên | qua |
| Tìm biến thể gọi `searchVariants` có `q`, `limit`, không `sellerId`; chip "Dùng số" | `ClaimPanel.test.tsx` "tìm biến thể…" | như trên | qua |
| LV03 chốt thành công → thân đúng, có khóa; dòng "Đã lên đơn DH0001241 · Trần Minh An"; khung hiện trạng thái xin địa chỉ; HV42 bị chặn → "Chưa xin được địa chỉ." + lý do | `ClaimPanel.test.tsx` "chốt thành công…", "chốt được nhưng Meta không cho…" | như trên | qua |
| LV07 409 `comment_claimed` → "Trần Minh An đang lên đơn cho comment này." + mở đơn | `ClaimPanel.test.tsx` "409 comment_claimed…" | như trên | qua |
| LV08 409 `out_of_stock {available:0}` → "Hết hàng (còn 0)" | `ClaimPanel.test.tsx` "409 out_of_stock…" | như trên | qua |
| LV09 409 `open_draft_exists` → hai nút; "Thêm vào đơn" gửi `add_to_draft`, `orderId`, `expectedVersion`; "Tạo đơn mới" gửi `forceNew` | `ClaimPanel.test.tsx` hai ca open_draft | như trên | qua |
| 422 `seller_mismatch` → "Sản phẩm không thuộc người bán của Page này." | `ClaimPanel.test.tsx` "422 seller_mismatch…" | như trên | qua |
| 503 `retry_later` rồi 201 → cùng `Idempotency-Key` | `ClaimPanel.test.tsx` "503 retry_later…" | như trên | qua |
| Không có `order.create` → nút khóa kèm lý do; comment đã chốt → không có nút chốt | `ClaimPanel.test.tsx` "không có order.create…", "comment đã chốt…" | như trên | qua |
| Mở khung → nhịp tim `drafting_order`; người khác hiện tên | `ClaimPanel.test.tsx` "mở khung → nhịp tim…" | như trên | qua |
| LV05 bảng xếp hết hạn gần nhất trước; số còn nháp/đã xác nhận/hết hạn; LV06 hộp đơn quá hạn tự hủy; sửa HV49 lý do thật + khóa | `PendingDraftsPage.test.tsx` "bảng xếp hết hạn…" | như trên | qua |
| "Nhắc tất cả" → thân chỉ `templateId`; kết quả 2 đã xếp, 1 bỏ qua kèm lý do; "Nhắc" một dòng | `PendingDraftsPage.test.tsx` hai ca nhắc | như trên | qua |
| Page chưa chọn mẫu → khóa; khách đã trả lời → mở hội thoại hộp thư trên Page của phiên / báo chưa có; lọc đã trả lời; 0 đơn → rỗng | `PendingDraftsPage.test.tsx` năm ca còn lại | như trên | qua |
| Quy ước màn, nhóm "Hộp chat" đầu menu, khung, câu lỗi, form Lên đơn (testkit đổi) | `app/screens.test.tsx`, `shell/*.test.tsx`, `lint-guard.test.ts`, `app/errors.test.ts`, `len-don/OrderForm.test.tsx` | `npx vitest run` các tệp đó cùng `src/screens/live` | 11 tệp, 154 test qua |
| tsc, eslint, prettier, jscpd | — | `npx tsc --noEmit -p .`; `npx eslint src/screens/live src/app`; `make jscpd` | sạch; "Found 0 clones." |
| Cổng của task | — | ci-task (bản m4, chạy từ xa) | "CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh ebf50c54…)"; static success 4 phút 54 giây; lần 2 sau sửa review: "CI-TASK XANH", static success 2 phút 20 giây |

## E2E viết sẵn, chưa chạy

(kế hoạch không có spec Playwright cho task này; so ảnh chụp với mockup chưa làm — chủ shop 24/11: không chạy Playwright trong lúc làm task)

## Test bị xóa hoặc tắt

(không có)

## Gộp m4

`git merge m4` trước review: "Already up to date" (m4 vẫn ở ebf50c54, chưa có khung của Task 174).

## Khác kế hoạch

- Thư mục `screens/live/` thay `features/live/` (Quyết định 1); thêm `live.css`, `liveTestkit.tsx`, `liveRules.ts`.

## Việc phát hiện thêm

- `claimLiveComment` không nhận SĐT chọn từ comment; muốn "Dùng số này" điền sẵn vào đơn nháp cần thêm trường vào
  hợp đồng và lưu vào địa chỉ nháp (backend).
- `PendingDraft` không có `conversationId`; thêm trường sẽ bớt một lần gọi `listCustomerConversations` (nay đọc một
  trang 100 hội thoại của khách).
- Khung chốt chưa chặn quá 20 món ở màn (hợp đồng `maxItems: 20`; máy chủ trả 400 `invalid_input`).
- `LiveSession` không có số comment có SĐT, số đơn đã lên (dải số của mockup).

## Reviewer

Lần 1: CẦN SỬA — (1) `useLiveComments.ts` `loadAll`: tải lại cả vẫn giữ comment cũ không còn khớp lọc "Chưa xử lý"
(máy chủ trả rỗng thì giữ cả bản cũ); (2) `PendingDraftsPage.test.tsx` ca mở hội thoại không phân biệt hội thoại hộp
thư với hội thoại comment. Đã sửa: `loadAll` thay hẳn bằng bản mới, chỉ giữ comment tới bằng báo hiệu trong lúc tải
(trừ comment đã chốt khi lọc "Chưa xử lý"), thêm ca test resync khi lọc (đỏ trước khi sửa); trang hội thoại giả hiện mã
hội thoại, test đòi đúng `…c0001`. Ghi chú không bắt buộc đã làm: giờ trang thiếu địa chỉ tự cập nhật (`useNow`), thêm
`orderTestkit.tsx` vào Ảnh hưởng; trần 20 món ghi ở Việc phát hiện thêm.

Lần 2: ĐẠT — không còn phát hiện phải sửa; hai phát hiện lần 1 đã sửa đúng (ca resync khi lọc bắt được lỗi cũ; ca mở
hội thoại đòi đúng `…c0001`). Ghi chú không bắt buộc: cửa sổ chạy đua nhỏ khi một lần tải phần mới ghi sau lần tải
lại cả (lưới an toàn 20 s tự chữa); liên kết `/inbox/:id` chờ route của Task 174.

Kết luận: ĐẠT
