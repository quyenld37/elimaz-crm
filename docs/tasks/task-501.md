# Task 501 — Sửa sau gộp M4 đợt 4: nối các màn và API còn thiếu

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (task sửa sau gộp M4 đợt 4, không có
hàng trong bảng kế hoạch; nguồn: Việc phát hiện thêm của Task 174–178, `docs/mockups/m4/README.md`). Nhánh gốc `m4`.
Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): thêm mới, không đổi chữ ký cũ —
  `conversation.ErrOrderCustomerMismatch`, `conversation.CheckOrderCustomer` (gọi ở `httpapi/handlers_order.go`, 1 chỗ);
  `conversation.ListPagePosts` + kiểu `PagePost` (1 chỗ gọi, handler mới); thêm trường vào kiểu xuất có sẵn
  `CustomerConversation` (`PostCreatedTime`, `PostPermalink`, `PostImageFileID`, `NewLead`), `Message.HiddenReason`,
  `PendingRow.ConversationID/InboxConversationID`, `FollowUpItem.LastStaffSnippet`, `PurposeChatPostImage` (thêm trường, mọi chỗ dựng kiểu bằng tên
  trường — không gãy). `channel.PostInfo.PictureURL` (thêm trường; 1 adapter thật `facebook`, `fakegraph` trong test).
- Hành vi đổi trong hàm có sẵn: `hideCommand.OnDone` ghi dòng lịch sử automation kèm lý do cả khi comment đã ẩn sẵn
  (Quyết định 3); `pendingRow` gọi `send.InboxFor` cho mọi dòng (trước đây chỉ khi chưa nhắc gần đây) để trả
  `inboxConversationId` — thêm một câu đọc mỗi dòng, kết quả xét nhắc không đổi; `inbound.OnNewCustomer` đánh dấu
  `created_customer`; `conversation.AttachmentURL` ký ảnh bài, ảnh bài mở trong trình duyệt.
- Gói phụ thuộc ngược (go list): `modules/conversation` ← `httpapi`, `cmd/crm`; `modules/channel` ← conversation,
  adapters/facebook, httpapi, cmd/crm, testkit; gói nội bộ chạm: `conversation/internal/{query,live,inbound,automation,
  calls}`, `adapters/facebook`, `testkit/fakegraph`.
- Operation OpenAPI bị chạm, oasdiff: chỉ thêm (không phá): `ChatPostRef` + `postedAt`, `permalink`, `imageFileId`;
  `ConversationSummary` + `newLead`; `ChatMessage` + `hiddenReason`; `FollowUpItem` + `lastStaffSnippet`; `PendingDraft`
  + `conversationId`, `inboxConversationId`; operation mới `listPagePosts` (`GET /api/v1/channels/{id}/posts`,
  `conversation.read`); `createOrder` thêm mã lỗi `conversation_customer_mismatch` (422) vào `x-error-codes`;
  `getAttachmentUrl` nhận thêm ảnh bài viết của hội thoại comment (cùng đường, cùng quyền).
- Chỗ dùng trên màn hình: `screens/inbox/side-panel` (HV26 nút lấy địa chỉ, HV02 nút "‹ Hội thoại"),
  `screens/len-don/OrderForm.tsx` (prop gợi ý địa chỉ), `screens/inbox/conversation/MessageList.tsx` + `MessageBubble`
  (thẻ bài: giờ đăng, ảnh, Mở bài; chip lý do ẩn), `screens/inbox/FollowUps.tsx` ("Bạn: …"), `ConversationRow.tsx` (chip
  CH08), `screens/live` (mở hội thoại theo `inboxConversationId`), `screens/kenh` (nút "+ Chọn bài" ngoại lệ tự ẩn, chỉ
  bài chọn của tự trả lời); `app/errors.ts` (câu cho mã lỗi mới).
- Câu sqlc, bảng, pha migration: migration mới `0417_chat_screen_fields.sql`, chỉ mở rộng: `conversation.posts` +
  `image_file_id uuid`; `conversation.conversations` + `created_customer boolean NOT NULL DEFAULT false`;
  `conversation.message_revisions` + `reason text` (CHECK phone|all); một dòng `platform.file_purposes`
  (`chat_post_image`). Câu sqlc đổi: `ConversationRowExtras`, `ListChatMessagesBefore/After`, `ListFollowUpCandidates`
  (thêm cột), `ConversationHasFile` (thêm ảnh bài), `GetConversationRow` (chi tiết) và `ListGroupConversations` (hội thoại của khách)
  thêm cột thẻ bài và `new_lead`, `PendingDraftsOfSession` (không đổi), mới `ListPagePosts`,
  `SetPostImage`, `MarkConversationCreatedCustomer`, `InsertRevisionState` thêm `reason`.
- Test sẽ chạy: `-run` từng test mới trong `internal/httpapi` (createOrder lệch khách, listPagePosts, trường mới),
  `internal/modules/conversation/...` (query, live, inbound, automation), `internal/adapters/facebook`; console vitest
  các tệp màn bị chạm + `src/app/errors.test.ts`; cả gói ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `customer.RootOf` (so nhóm khách), `send.InboxFor`
  (hộp thư của khách cho đơn nháp — đã dùng cho nhắc), `fetchProfile.avatar` (mẫu tải ảnh từ kênh lên bucket private),
  `files.Register`, `ConversationHasFile` + `AttachmentURL` (đường ký 5 phút), `automation.ContainsPhone`,
  `customer.ListCursor` (con trỏ trang), console `useWriteAction`, `queryKey`, `ERROR_MESSAGES`.
- Dùng lại / viết mới vì: dùng lại tất cả ở trên; ảnh bài tải theo đúng cách ảnh đại diện (một hàm tải chung trong
  `inbound` cho cả hai thay vì chép — `storeChannelImage`), ảnh bài lấy đường dẫn qua `getAttachmentUrl` có sẵn (không thêm
  đường ký thứ hai). Viết mới: `ListPagePosts` (chưa có đọc bài theo Page), nhận biết tin có địa chỉ ở màn
  (`addressRules.ts`, chưa có hàm tương tự ở console).

## Soát thiết kế

- 5.1 chủ dữ liệu: cột mới đều trong schema `conversation` (bài, hội thoại, lịch sử tin) và dòng mục đích tệp như 0404.
- 5.2 không ghi bảng module khác: kiểm khách đọc `customer.RootOf`; ảnh ghi qua `files.Register` (public API).
- 5.3 đọc qua view: đơn của khách đọc `ord.orders_v` (như `RecentGroupOrders`), khách `customer.customers_v`.
- 5.4 nguyên tử: kiểm khách chạy trong cùng transaction tạo đơn (`WriteTx`), trước `createOrder`.
- 5.5/5.6 gọi ngoài: ảnh bài tải trong `Execute` của lệnh chỉ đọc `command.fb.fetch_post` có sẵn (ngoài transaction),
  lỗi tải chỉ bỏ ảnh, không đổi kết quả lệnh; không có lệnh gửi mới.
- 5.7 tenant: mọi câu mới chạy trong `tenancy.WithTenant`/tx tenant; không thêm bảng (cột mới thừa RLS của bảng).
- 5.8 module không biết HTTP: đúng; handler mới mỏng.
- 5.9 đường ghi: không thêm đường ghi; `listPagePosts` chỉ đọc, khai `x-permission: conversation.read`.
- 5.10 Valkey: không áp dụng (không dùng).
- 5.11 thư viện: không thêm.
- 5.12 theo module mẫu: lỗi là sentinel trong `conversation`, đổi HTTP ở `errors_conversation.go`.
- 6.1 đồng thời: kiểm nhóm khách đọc `RootOf` không khóa trước `FindOrCreateForOrder` (khóa gốc); gộp hồ sơ chạy song
  song chỉ làm hai hồ sơ cùng nhóm hơn, nên tệ nhất là từ chối nhầm một lần (bấm lại được), không tạo đơn sai khách.
  `created_customer` ghi một lần cùng tx tạo khách. `SetPostImage` chỉ đặt khi còn NULL.
- 6.2 gửi lại: createOrder bị từ chối không lưu theo khóa (`isOrderRefusal`), gửi lại được xử lý lại.
- 6.3 thứ tự khóa: không khóa thêm; `SetPostImage` (posts, nhóm 6) rồi `files.Register` (12b) như ảnh đại diện.
- 6.4/6.5: dùng `WriteTx`/`RetryTx` sẵn có; biến kết quả gán lại trong thân.
- 6.6–6.9 việc nền: lệnh đọc bài giữ lịch thử lại cũ; tải ảnh có trần 5 MiB, timeout chung của lệnh.
- 6.10 tiền: không áp dụng.
- 6.11 quy luật: không áp dụng (không chạm tồn, sổ).
- 6.12 dữ liệu cá nhân: `lastStaffSnippet` là chữ của shop; nút lấy địa chỉ chỉ chép trên màn (không log). Không log
  thêm gì.
- 6.13 quyền: `listPagePosts` kiểm `conversation.read` trên Page (ngoài phạm vi → 404); kiểm khách lệch chạy sau
  kiểm quyền hội thoại; ảnh bài chỉ qua `getAttachmentUrl` (đòi `conversation.read` trên Page).

## Quyết định thay chủ shop

1. **Đơn lên từ hội thoại chỉ cho khách cùng nhóm hồ sơ** (việc 2, reviewer Task 176): `createOrder` có `conversationId`
   thì khách phải là `customerId` cùng `root_id` với khách của hội thoại (`conversation.CheckOrderCustomer`); gửi `customer`
   (khách mới, tìm theo SĐT) hay `customerId` nhóm khác → 422 `conversation_customer_mismatch` (câu ở màn: "Khách của đơn
   không phải khách của hội thoại này…"). Page và quyền đã kiểm ở `OrderContext` (404). Hoàn tất đơn nháp
   (`updateOrder` + `confirmOrder`) không đổi được khách nên không cần kiểm thêm. Phương án khác: cho tìm theo SĐT rồi so
   kết quả (khó hiểu khi SĐT thuộc người khác); chỉ ghi cảnh báo. Đảo ngược: bỏ một lời gọi trong `orderConversationAt`.
2. **Ảnh bài chép về CRM** (việc 3): lệnh đọc bài có sẵn (`command.fb.fetch_post`) đọc thêm `full_picture`, tải qua bộ
   tải của kênh (chỉ máy chủ Meta, ≤ 5 MiB, JPEG/PNG/WebP nhận theo nội dung) lên bucket private, mục đích tệp mới
   `chat_post_image` (0417), ghi `posts.image_file_id`; màn xin đường 5 phút bằng `getAttachmentUrl` của hội thoại comment
   (cùng quyền). Ảnh bài là nội dung công khai của Page nên nạp ngay (ảnh khách gửi vẫn bấm mới nạp). Phương án khác:
   trả thẳng URL CDN của Facebook (hết hạn sau vài ngày, trình duyệt nhân viên gọi thẳng Meta). Lỗi tải ảnh chỉ bỏ ảnh.
3. **Lý do tự ẩn** (việc 3): thêm cột `message_revisions.reason` (phone | all) ghi lúc lệnh tự ẩn xong; `hiddenReason`
   của tin = nguồn dòng ẩn sau lần hiện cuối (ưu tiên dòng tự ẩn hơn dòng `page_external` do webhook "hide" của chính lệnh
   đó), giá trị `auto_phone`, `auto_all`, `automation` (comment ẩn trước Task 501), `staff`, `page_external`. Lệnh tự ẩn
   ghi dòng lịch sử cả khi comment đã ẩn sẵn (webhook tới trước OnDone), nội dung giữ nguyên nên không thành "Khách đã
   sửa". Phương án khác: đoán từ cấu hình hiện tại (sai khi cấu hình đã đổi).
4. **"Khách tiềm năng mới" (CH08, việc 4)** = cột mới `conversations.created_customer` (đặt trong `OnNewCustomer`, cùng tx
   tạo khách) + hồ sơ chưa gộp với hồ sơ nào + chưa có đơn nào (kể cả nháp, `ord.orders_v`). Trường `newLead` ở
   `ConversationSummary` (danh sách, chi tiết, hội thoại của khách); chip ở dòng Inbox và thẻ khách khung bên phải.
   Hội thoại có trước Task 501 không được đánh dấu (không đoán ngược).
5. **Đơn nháp chờ địa chỉ** trả `conversationId` (hội thoại comment của lần chốt) và `inboxConversationId` (hộp thư của
   khách trên Page, `send.InboxFor`, chưa có → không trả); "Mở hội thoại để hoàn tất" mở hộp thư, không có thì hội thoại
   comment (khung bên phải hai nơi đều thấy đơn nháp của nhóm khách) — bỏ lời gọi `listCustomerConversations` và câu
   "Khách chưa có hội thoại trên Page này" (Task 177 quyết định 6).
6. **`listPagePosts`** (`GET /api/v1/channels/{id}/posts`, `conversation.read` trên Page): bài CRM đã gặp (có comment),
   mới gặp trước theo id, trang 50. Màn cài đặt: ô "Trừ các bài" của Tự ẩn (mockup) và "Mọi bài / Chỉ bài chọn" của Tự trả
   lời (mockup) dùng chung `PostPicker` ("+ Chọn bài", nhãn có "×", "Tải thêm bài", tối đa 200); "Chỉ bài chọn" mà chưa
   chọn bài → báo tại ô. `exceptPostIds` của Tự trả lời không có ô trong mockup: giữ nguyên khi lưu, hiện số bài.
   Bảng `m4Operations` của test kiến trúc thêm dòng (65 operation).
7. **HV02 "‹ Hội thoại"**: khung khách trên điện thoại vẫn nằm dưới hội thoại (Task 174); nút (chỉ điện thoại) là liên
   kết về `#hoi-thoai` (cột hội thoại), đối xứng với nút "Khách" (`#khach`) của Task 175. Phương án khác: đổi bố cục điện
   thoại thành một màn mỗi lúc (đổi thiết kế đã chốt của 174).
8. **HV26 "Lấy địa chỉ từ tin HH:MM của khách"**: tin khách (tin nhắn, comment, chưa xóa) mới nhất trong trang tin đang có
   (bản đệm chung với dòng tin) có chữ số và một từ chỉ địa chỉ (ngõ, đường, phường, xã, quận, huyện, thôn, ấp, tp, P., Q.…);
   bấm chép nguyên văn (tối đa 500) vào ô "Số nhà, đường", nhân viên tự chọn tỉnh, xã. Giờ "HH:MM", khác ngày thì kèm
   ngày. Không có tin như vậy → không có nút. Có ở cả lên đơn mới và hoàn tất nháp.
9. **"Bạn: …" của Inbox D**: `FollowUpItem.lastStaffSnippet` = 120 ký tự đầu tin người thật cuối của Page (chữ của shop);
   tin ảnh → không có dòng.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Việc 2: đơn từ hội thoại cho khách nhóm khác / khách mới / tìm SĐT → 422 `conversation_customer_mismatch`, không tạo đơn; hồ sơ cùng nhóm → 201 | `TestCreateOrder_FromConversationCustomerMismatch`, `TestCreateOrder_FromConversation`, `TestDomainErrorSnapshot*` | `go test -count=1 -run '^(TestCreateOrder_FromConversationCustomerMismatch\|TestCreateOrder_FromConversation\|TestDomainErrorSnapshot.*)$' ./internal/httpapi/` | đỏ trước code (`khách mới: 201 …, 1 đơn`), sau code `ok … 1.367s` |
| Việc 2 màn: mã lỗi mới có câu tiếng Việt | `src/app/errors.test.ts` | `npx vitest run src/app/errors.test.ts` | `Tests 17 passed (17)` |
| 3a: `lastStaffSnippet` 120 ký tự, tin ảnh → không có | `TestFollowUpsAPI_LastStaffSnippet`, `TestFollowUpsAPI_List`; vitest `FollowUps.test.tsx` ("Bạn: …") | `go test -count=1 -run '^(TestFollowUpsAPI_List\|TestFollowUpsAPI_LastStaffSnippet)$' ./internal/httpapi/`; `npx vitest run src/screens/inbox/FollowUps.test.tsx` | đỏ trước (`lastStaffSnippet = map[…:<nil> …]`), sau `ok`; vitest `4 passed` |
| 3b: `PendingDraft.conversationId`, `inboxConversationId` (chưa có hộp thư → không có) | `TestLivePendingAPI_ConversationIDs`, `TestLivePendingAPI_CompleteAndRemind`; vitest `PendingDraftsPage.test.tsx` (mở hộp thư / mở comment) | `go test -count=1 -run '^(TestLivePendingAPI_CompleteAndRemind\|TestLivePendingAPI_ConversationIDs)$' ./internal/httpapi/`; `npx vitest run src/screens/live/` | đỏ trước (`conversationId:00000000-0000…`), sau `ok … 1.235s`; vitest `41 passed` |
| 3c: lý do ẩn — dòng tự ẩn ghi phone/all; `hiddenReason` đủ 5 giá trị + rỗng, ưu tiên automation hơn page_external, tính sau lần hiện cuối | `TestAutoHide_PhoneOnly`, `TestAutoHide_AllExceptAndOff` (chạy cả 7 test `TestAutoHide_*`), `TestMessages_HiddenReason`, `TestMessages_Flags`; vitest `PostCard.test.tsx` "lý do ẩn comment" | `go test -count=1 -run '^(TestAutoHide_.*)$' ./internal/modules/conversation/internal/automation/`; `go test -count=1 -run '^(TestMessages_HiddenReason\|TestMessages_Flags)$' ./internal/modules/conversation/internal/query/` | đỏ trước (biên dịch: chưa có `HiddenReason`, `hideReason`), sau `ok … 1.655s`, `ok … 0.861s`; vitest `3 passed` |
| 3d: ảnh bài — adapter đọc `full_picture`; lệnh đọc bài lưu ảnh (bài không ảnh, tệp không phải ảnh, tải lỗi → NULL, không dead); dòng tệp `chat_post_image` ready | `TestFetchProfileAndPost`, `TestFetchPostStoresPicture`, `TestFetchPostMissingAndLookup`, `TestCommentAddCreatesPostConversationMessage`, `TestFetchProfile.*`, `TestFilePurposes_MatchTable` | `go test -count=1 -run '^TestFetchProfileAndPost$' ./internal/adapters/facebook/`; `go test -count=1 -run '^(TestFetchPostStoresPicture\|TestFetchPostMissingAndLookup\|TestCommentAddCreatesPostConversationMessage\|TestFetchProfile.*)$' ./internal/modules/conversation/internal/inbound/`; `go test -count=1 -run '^TestFilePurposes_MatchTable$' ./internal/modules/conversation/` | đỏ trước (`ảnh bài = map[…:<nil> …]`), sau `ok … 0.531s`, `ok … 1.012s`, `ok … 0.858s` |
| 3d: ảnh bài qua `getAttachmentUrl` — hội thoại comment trên bài, người đọc Page → 200, bucket private, không attachment; cùng tệp qua comment bài khác, hộp thư, người đọc Page khác → 404 (sau reviewer vòng 1) | `TestGetAttachmentURL_PostImage`, `TestGetAttachmentURL` | `go test -count=1 -run '^(TestGetAttachmentURL_PostImage\|TestGetAttachmentURL)$' ./internal/httpapi/` | `ok … 2.003s`; đảo tạm nhánh inline trong `attachments.go` → đỏ (`url …?cd=attachment…`), trả lại → xanh |
| 3d + việc 4: thẻ bài (giờ đăng, đường dẫn, ảnh) và `newLead` ở danh sách, chi tiết, hội thoại của khách; 5 ca newLead | `TestList_PostCardAndNewLead`, `TestList_Filters`, `TestConversationSummary_PostCardAndNewLead` | `go test -count=1 -run '^(TestList_PostCardAndNewLead\|TestList_Filters)$' ./internal/modules/conversation/internal/query/`; `go test -count=1 -run '^(TestPagePostsAPI\|TestConversationSummary_PostCardAndNewLead)$' ./internal/httpapi/` | đỏ trước (vet: `r.NewLead undefined`), sau `ok … 0.939s`, `ok … 1.316s` |
| Việc 4: hội thoại tạo khách có `created_customer`, hội thoại sau của cùng khách không | `TestInboundMarksConversationThatCreatedCustomer`, `TestInboundFirstMessageCreatesLeadAndConversation` | `go test -count=1 -run '^(TestInboundMarksConversationThatCreatedCustomer\|TestInboundFirstMessageCreatesLeadAndConversation)$' ./internal/modules/conversation/internal/inbound/` | đỏ trước (`hộp thư false, … người mới false`), sau `ok … 0.805s` |
| 3e: `listPagePosts` — trang, trường, Page ngoài phạm vi 404, con trỏ sai 400, Page lạ 404; bảng M4 65 operation | `TestPagePostsAPI`, `TestM4Contract_Operations` (tests/arch), `TestM4Contract_*` (httpapi) | cùng lệnh dòng trên; `go test -count=1 -run 'TestM4Contract' ./tests/arch/` và `./internal/httpapi/` | `ok … 1.316s`; `ok … 0.640s`, `ok … 0.998s` |
| Màn: thẻ bài (ảnh qua getAttachmentUrl, Mở bài tab mới; bài chưa đọc → không xin ảnh); chip CH08 ở dòng Inbox và thẻ khách; "‹ Hội thoại" → #hoi-thoai | `PostCard.test.tsx`, `ConversationList.test.tsx` (ca CH08), `SidePanel.test.tsx` (2 ca Task 501) | `npx vitest run src/screens/inbox/` | đỏ trước (2 ca PostCard ×), sau `Tests 146 passed (146)` |
| Màn HV26: nhận ra tin có địa chỉ (6 có, 6 không), tin mới nhất, giờ HH:MM / kèm ngày, bỏ tin shop, ghi chú, tin đã xóa, cắt 500; form chép vào ô; khung truyền gợi ý cho lên đơn và hoàn tất nháp | `addressRules.test.ts`, `OrderForm.test.tsx` (2 ca HV26), `OrderPanel.test.tsx` (ca HV26) | `npx vitest run src/screens/inbox/side-panel/ src/screens/len-don/OrderForm.test.tsx` | đỏ trước (thiếu mô-đun; `expected undefined to deeply equal …`), sau `14 passed`, `56 passed`, `2 passed` |
| Màn cài đặt: "+ Chọn bài" (Tải thêm, chọn, bỏ) → `exceptPostIds`; "Chỉ bài chọn" chưa chọn → báo tại ô; chọn → `postIds` | `AutomationForms.test.tsx` (2 ca Task 501) | `npx vitest run src/screens/kenh/` | đỏ trước (2 ×), sau `Tests 76 passed (76)` |
| Toàn bộ màn chạm | vitest inbox, live, kenh, len-don, errors; tsc; eslint | `npx vitest run src/screens/inbox src/screens/live src/screens/kenh src/screens/len-don src/app/errors.test.ts`; `npx tsc --noEmit`; `npx eslint <tệp đổi>` | `Test Files 38 passed`, `Tests 564 passed`; tsc, eslint 0 lỗi |
| Cổng rẻ trước ci | jscpd, golangci-lint, test kiến trúc | `make jscpd`; `golangci-lint run ./internal/modules/conversation/... ./internal/httpapi/ ./internal/adapters/facebook/... ./internal/testkit/fakegraph/... ./internal/modules/channel/... ./tests/arch/`; `go test -count=1 ./tests/arch/` | `Found 0 clones`; `0 issues`; `ok … 81.509s` |
| Cổng của task (sau gộp m4 ca5dc299) | ci-task (cổng tĩnh + test mọi gói vì đổi testkit) | ci-task.sh của task | `REMOTE-TEST XANH …/runs/45878493769`; `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh ca5dc299…)` |
| Cổng của task, lần 2 (sau sửa reviewer vòng 1: test ảnh bài, helper test) | ci-task | ci-task.sh của task | `REMOTE-TEST XANH …/runs/45880633395`; `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh ca5dc299…)` |

## Test bị xóa hoặc tắt

- `PendingDraftsPage.test.tsx` — ca "khách đã trả lời → 'Mở hội thoại để hoàn tất' mở hội thoại hộp thư của khách trên
  Page của phiên" (đọc `listCustomerConversations` rồi lọc) và ca "khách chưa có hội thoại trên Page → báo tại dòng": bỏ
  vì hành vi tìm hội thoại phía màn bị bỏ (Quyết định 5: máy chủ trả `inboxConversationId`/`conversationId`); thay bằng
  hai ca "mở hộp thư (inboxConversationId)" và "chưa có hộp thư → mở hội thoại comment (conversationId)". Kèm theo,
  `liveTestkit.tsx` bỏ trường `conversations` và đường giả `/customers/{id}/conversations` (không còn ai gọi).

## Khác kế hoạch

- Task sửa sau gộp, không có hàng trong bảng kế hoạch; phạm vi theo giao việc của điều phối (4 việc).
- Sửa test cũ cho đúng hành vi mới (không nới): `TestFilePurposes_MatchTable` đếm 4 dòng `chat_*` (thêm
  `chat_post_image`), `TestM4Contract_Operations` 65 operation, `TestFetchProfileAndPost` đòi thêm `full_picture`,
  `PendingDraftsPage.test.tsx` thay hai ca tìm hội thoại bằng hai ca mở theo mã máy chủ trả, `TestLivePendingAPI_*`
  tách ca mã hội thoại thành test riêng (funlen).
- `TestGetAttachmentURL` dựng môi trường bằng helper chung `newAttEnv`/`getter` với test mới (jscpd), không đổi assertion.
- Tách `storeChannelImage` (dùng chung ảnh đại diện và ảnh bài) khỏi `fetchProfile.avatar`; `latestMessagesQuery` dùng
  chung cho dòng tin và gợi ý địa chỉ.

## E2E viết sẵn, chưa chạy

(không có: kế hoạch không có e2e cho task sửa này; các ca nằm ở test gói `httpapi`, module và vitest)

## Việc phát hiện thêm

- Bài đã đọc trước Task 501 không có ảnh (lệnh đọc bài chạy một lần mỗi bài): cần một lệnh đọc lại bài cũ nếu muốn ảnh.
- `listPagePosts` chỉ có bài đã có comment trong CRM; chọn bài chưa có comment (vd bài vừa đăng) cần đọc danh sách bài
  từ Graph (`/{page}/posts`, việc adapter).
- (Reviewer) Test riêng cho thứ tự "webhook hide tới trước OnDone của lệnh tự ẩn" (lý do vẫn `auto_*`, `editedCount`
  không tăng); index `conversation.posts (tenant_id, channel_account_id, id)` cho `listPagePosts` khi Page có nhiều bài.
- Spec Playwright `hoi-thoai.spec.ts`, `inbox.spec.ts` chưa có dữ liệu giả cho trường mới (ảnh bài, newLead, lý do ẩn,
  gợi ý địa chỉ); nên thêm ca khi chạy e2e của mốc.

## Reviewer

- Commit 1 (fb33c8c0, chỉ cấu trúc: `storeChannelImage`, `latestMessagesQuery`): **ĐẠT**, không có phát hiện phải sửa
  (reviewer xác nhận thân hàm giống hệt, `fs == nil` giữ nghĩa vì `deps.Files` là interface, khóa/queryFn/meta của
  truy vấn tin không đổi).
- Commit 2 (hành vi): vòng 1 **CẦN SỬA**: (1) đường ký ảnh bài qua `getAttachmentUrl` chưa có test máy chủ, (2) mục
  "Test bị xóa hoặc tắt" chưa ghi tên hai ca bị thay. Đã sửa: thêm `TestGetAttachmentURL_PostImage` (200 + 3 ca 404, đảo
  tạm mã thấy đỏ), ghi tên ca và lý do. Vòng 2: **ĐẠT**, không còn phát hiện phải sửa. Ghi chú không bắt buộc: chưa có
  test riêng cho thứ tự "webhook hide tới trước OnDone"; nên có index `(channel_account_id, id)` cho `ListPagePosts` khi
  số bài lớn (ghi ở Việc phát hiện thêm).
