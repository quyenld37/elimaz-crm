# Task 172 — Chốt từ comment live — đơn nháp, giữ hàng, xin địa chỉ, một comment một đơn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 172 = M4-32). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào đã có. Thêm mới: gói gốc `conversation`
  (tệp `liveclaims.go`): `ClaimLiveComment`, `LiveClaimInput`, `LiveClaimItem`, `LiveClaimResult`, `LiveClaimRef`,
  `GetLiveCommentContext`, `LiveCommentContext`, `LiveDraft`, `ListLiveComments`, `LiveCommentQuery`, `LiveCommentRow`,
  kiểu lỗi `CommentClaimedError`, `OpenDraftExistsError`, sentinel `ErrCommentClaimed`, `ErrOpenDraftExists`,
  `ErrSellerMismatch`, `ErrClaimInvalid`, `ErrDraftNotOpen`; phần `internal/live` (tệp `claim.go`, `context.go`,
  `comments.go`; bước xin địa chỉ ở `send/ask.go`): `Claim`, `ClaimInput`, `ClaimEnv`, `PageDefaults`, `Context`, `ListComments`…; phần
  `internal/send`: `AskAddress`, `AskResult`, `InboxFor` (tách `findInbox` khỏi `inboxOf`, cùng hành vi; dùng lại khuôn nhắn riêng, gửi tin của Task 156/158, không kiểm
  `conversation.reply`: route chốt đã đòi `live.operate` trên Page). `conversation.autoSource` (Task 159) dùng thêm ở
  đường chốt (giờ bắt đầu = giờ phiên live), chữ ký không đổi. httpapi: `errorBody` thêm `ClaimedBy`, `OrderID`,
  `OrderCode`, `Drafts` (thân lỗi đã khai ở openapi); `errors_conversation.go` thêm dòng lỗi và nhánh chi tiết;
  `errors.go` thêm `variantIds` vào thân out_of_stock; `handlers_conversation_read.go` tách `cardInfoOut` (thẻ khách
  dùng chung với bối cảnh chốt). `conversation.activeSource` tách từ `OrderContext` (cùng hành vi). Gói thử mới
  `internal/testkit/livetest` (khách, phiên live, comment live, bật gửi thật, câu sửa dữ liệu thử) — chỉ test của
  `live` và `httpapi` nhập.
- Module customer (vùng rủi ro cao): câu `LockCustomerRow` (`queries/customer/lock.sql`, dùng bởi `LockRoot` và
  `SetDoNotContact` qua `lockRoot`) đổi `FOR UPDATE` → `FOR NO KEY UPDATE` (Quyết định 11); `lockRootOnce` khóa thêm
  hồ sơ con khác gốc, theo id tăng dần cùng gốc (Quyết định 12, sau review lần 1); chữ ký không đổi. `LockRoot` chỉ có
  một chỗ gọi (`live.Claim`); `lockRoot` thêm `SetDoNotContact` (`contact.go`).
- Gói phụ thuộc ngược (go list): `internal/modules/conversation` ← `internal/httpapi`, `cmd/crm`, `tests/e2e`;
  `internal/live`, `internal/send` chỉ được gói gốc conversation (và `inbound` nhập `live`) dùng.
- Operation OpenAPI bị chạm, oasdiff: hiện thực 3 operation đã khai ở Task 143 (`listLiveComments`,
  `getLiveCommentContext`, `claimLiveComment`), xóa 3 dòng khỏi `tests/arch/testdata/m4_pending.txt`. `openapi.yaml`
  chỉ sửa mô tả trường `variantIds` của Error (Quyết định 5; mã sinh `gen/oas_schemas_gen.go`, `console/src/api`
  đổi chú thích) → oasdiff không đổi. Không thêm mã lỗi, trường hay route (mã lỗi `comment_claimed`, `open_draft_exists`, `seller_mismatch` và trường chi tiết đã có, câu tiếng
  Việt đã có ở `console/src/app/errors.ts`).
- Chỗ dùng trên màn hình: chưa có (màn Live là Task 177).
- Câu sqlc, bảng, pha migration: migration mới `0410_live_claims.sql` (pha mở rộng: CREATE TABLE
  `conversation.live_comment_claims`, index, RLS, GRANT; khóa ngoại ghép tới `messages`, `live_sessions`,
  `conversations`, `identity.users`). Câu sqlc mới `queries/conversation/claims.sql`. Đọc view `ord.orders_v`,
  `ord.order_items_v`, `customer.customers_v`, `identity.users_v` (được phép, Q1; `docs/views.md` thêm M4 vào cột
  người đọc của `ord.order_items_v`). Ghi đơn chỉ qua `order.Writer.Create`,
  `order.Writer.Edit`; khóa khách qua `customer.LockRoot`.
- Test sẽ chạy: `conversation/internal/live/{claim_test,claim_concurrency_test,ask_test,property_test}.go`,
  `httpapi/handlers_live_claims_test.go`, `TestM4Contract_StatusCodes`, `TestDomainErrorSnapshot*`, `tests/arch`
  (`TestSchemaRules`, ranh giới, thứ tự khóa), các test cũ của `live`, `send` (chạy ở ci-task).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (realtime, queue,
  commands, tenancy); `customer.LockRoot`, `customer.OrderCustomerInput`; `order.Writer.Create/Edit`, `order.GetDetail`,
  `order.OutOfStockError`, `order.ActiveSource`; `channel.Account`; `access.Require`; `live.IsLiveComment`,
  `live.OnComment`; `send.PrivateReply` (inboxOf, ensureInbox, insertAction, privateState, latestPrivate, policyFor),
  `send.Send` (state, insert), `send.FailReasonText`; `templates.Render`; `automation.ExtractPhones`,
  `automation.ContainsPhone`; `conversation.CustomerCard` (thẻ khách cho bối cảnh), `autoSource`, `readSettings`;
  httpapi `refusableWrite`, `orderWarehouseAt`, `orderActor`, `createOrder` (audit), `isOrderRefusal`, `stockShortage`.
- Dùng lại / viết mới vì: dùng lại toàn bộ các hàm trên. Viết mới `send.AskAddress` vì cần ba nhánh (nhắn riêng → tin
  thường → bỏ kèm lý do) trả kết quả thay vì lỗi, không kiểm `conversation.reply`; dùng lại đúng các hàm nội bộ của
  nhắn riêng/gửi tin nên không lặp mã. Không viết hàm thêm dòng vào đơn nháp (M4 không có `AddDraftItems`): dựng
  danh sách dòng rồi `order.Edit`.

## Soát thiết kế

- 5.1 chủ dữ liệu: dòng ghi nhận thuộc `conversation` (bảng `conversation.live_comment_claims`); đơn và giữ hàng thuộc
  `order`/`inventory`, chỉ ghi qua public API của `order` (Q1 của kế hoạch: `conversation` điều phối, cạnh
  conversation → order đã có trong test ranh giới).
- 5.2 không ghi bảng module khác: đúng; `order.Create/Edit` tự ghi đơn, giữ hàng, khách.
- 5.3 đọc module khác qua view đã công bố (`ord.orders_v`, `ord.order_items_v`, `customer.customers_v`,
  `identity.users_v`) và public API (`channel.Account`, `order.GetDetail`).
- 5.4 nguyên tử: một `WriteTx` cho cả lần chốt: khóa → ghi nhận → đơn + giữ hàng → tin xin địa chỉ + lệnh. Nhánh lùi
  cả transaction (không đơn, không ghi nhận, không tin, không việc): comment đã chốt (409), khách có nháp của phiên mà
  không `forceNew` (409, kiểm trước khi tạo đơn), hết hàng (409 `out_of_stock`, lỗi của `order`), biến thể ngừng bán,
  không có, khác người bán (422/404), version cũ (409), mẫu do người bấm chỉ định không có (404). Xin địa chỉ không gửi
  được vì chính sách (chỉ xem, quá 7 ngày và hết cửa sổ, Page chưa chọn mẫu) không lùi: đơn nháp vẫn tạo, `ask` ghi lý do.
- 5.5/5.6 gọi ra ngoài: tin xin địa chỉ là lệnh `command.fb.private_reply` hay `command.fb.send_message` của Task
  156/158 ghi trong cùng transaction, chạy sau commit; không gọi Meta trong transaction.
- 5.7 tenant: bảng mới có `tenant_id`, RLS ép, `UNIQUE (tenant_id, …)`, khóa ngoại ghép; mọi câu trong `WriteTx` /
  `tenancy.WithTenant`.
- 5.8 không HTTP trong module: handler mỏng `handlers_live_claims.go` (đọc yêu cầu, kiểm `order.create`/`order.update`,
  audit đơn, đổi lỗi).
- 5.9 đường ghi: route chốt có `Idempotency-Key`, `x-permission: live.operate` (Task 143); lỗi nghiệp vụ không lưu
  theo khóa (`refusableWrite`).
- 5.10 Valkey: không áp dụng. 5.11 thư viện: không thêm. 5.12 module mẫu: public API ở gói gốc, sqlc, test PostgreSQL thật.
- 6.1 đồng thời — **thứ tự khóa** (ADR-46, C11 khách trước hội thoại): (0) `customer.LockRoot(khách của comment)`
  (nhóm 5) → (1) `conversations` của comment và hộp thư của cùng khách trên Page, `ORDER BY id FOR UPDATE` (nhóm 6;
  hộp thư chưa có thì tạo lúc xin địa chỉ, dòng mới) → (2) đọc `live_comment_claims` (dòng mới chèn ở bước 5) → (4)
  `order.Create`/`order.Edit`: `orders` (7), khách bên trong chạm lại đúng dòng đã khóa, `stock_balances` (11) →
  (6) `messages` dòng mới + `jobs` (13). Hai nhân viên chốt cùng comment xếp hàng ở khóa khách gốc; người sau đọc
  (READ COMMITTED, câu mới sau khi có khóa) thấy dòng ghi nhận → 409 `comment_claimed`. Chèn ghi nhận dùng
  `ON CONFLICT DO NOTHING` (0 dòng → 409, lùi cả đơn vừa tạo) làm chốt thứ hai. Tranh chiếc cuối: câu UPDATE có điều
  kiện của inventory, thiếu → lỗi, lùi.
- 6.2 gửi lại: `Idempotency-Key` của M0 (cùng khóa → cùng thân 201 hay 409 `request_in_progress`); khóa nghiệp vụ của
  đơn `uuidv5(tenant, messageId)` (đơn mới) để `order.Create` tự trả đơn cũ nếu cùng comment lọt qua; `ExternalRef =
  fb_comment:<mã comment>`.
- 6.3 khóa: như 6.1; có test 200 vòng song song với đường tin vào cùng khách (worker thật) và đếm
  `pg_stat_database.deadlocks`.
- 6.4 lỗi tạm: `WriteTx` chạy `db.RetryTx`; thân hàm gán lại mọi biến kết quả mỗi lần.
- 6.5 READ COMMITTED + khóa dòng. 6.6 việc nền: lệnh gửi của Task 156/158 (hết hạn, `in_doubt`) giữ nguyên.
- 6.7 429: không gọi ngoài trong transaction. 6.8 tiến trình chết: transaction lùi, không gì nửa chừng; lệnh gửi chạy lại
  theo khuôn commands.
- 6.9 treo lâu: một transaction ngắn, không chờ mạng.
- 6.10 tiền: tổng đơn do `order` tính (int64 đồng); M4 không tính tiền.
- 6.11 quy luật (rapid, 100 lượt): chuỗi ngẫu nhiên chốt / thêm vào nháp / gửi lại, song song trên 1–3 biến thể có tồn
  ngẫu nhiên → tổng giữ của các đơn nháp ≤ tồn ban đầu; mỗi comment ≤ 1 dòng ghi nhận, ≤ 1 tin riêng.
- 6.12 dữ liệu cá nhân: không log nội dung comment, SĐT; SĐT tách từ comment chỉ trả trong API bối cảnh cho người có
  `conversation.read` trên Page; thân lỗi `comment_claimed` chỉ có tên hiển thị nhân viên, mã đơn.
- 6.13 quyền: `live.operate` trên Page của comment (không có ở Page nào → 403 guard; Page khác → 404); thêm
  `order.create` ở kho chính (mode new, `orderWarehouseAt`) hay `order.update` (add_to_draft) → thiếu 403; đọc comment,
  bối cảnh cần `conversation.read` trên Page (404).

## Bằng chứng

Lệnh A: `go test -race -count=1 -run '^(TestClaim.*)$' ./internal/modules/conversation/internal/live/` → `ok … live 13.531s` (sau gộp m4; trước gộp 12.813s).
Lệnh B: `go test -race -count=1 -run '^(TestLiveClaimsAPI_.*|TestDomainErrorSnapshot.*|TestLiveSessionsAPI)$' ./internal/httpapi/` → `ok … httpapi 3.336s` (sau gộp m4).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| LV03: tồn 5 chốt 2 → 1 đơn draft của khách, facebook, hội thoại comment, Page, nguồn "Live ca trưa" (phiên 12:15 ICT, khung 11:00–14:00), hạn nháp now + 24 giờ, có thể bán 3, 1 ghi nhận (An, ask queued, ask_message_id), 1 tin riêng queued "Chào Lan, …" có mã đơn; worker → Meta nhận đúng 1 nhắn riêng `recipient.comment_id`; báo hiệu live.comments, conversation.changed | `live.TestClaim_LV03` | A | ok |
| LV07: An và Bình bấm cùng comment song song → 1 thành công, 1 `CommentClaimedError` tên người thắng; 1 đơn; có thể bán giảm đúng 2 | `live.TestClaim_LV07_TwoStaffOneComment` | A | ok |
| LV08: tồn 0 → `OutOfStockError` (biến thể, 0); hai dòng một dòng hết → cả yêu cầu bị từ chối, dòng kia không bị giữ; 0 đơn, 0 ghi nhận, 0 tin ra, số việc không đổi; ngừng bán → `ErrNotForSale` | `live.TestClaim_LV08` | A | ok |
| LV09: khách có nháp của phiên → `OpenDraftExistsError` kèm đơn (dòng, version); add_to_draft version đúng → cùng đơn, 2 dòng (trùng biến thể cộng số lượng), version + 1, có thể bán giảm thêm; version cũ → `ErrVersionConflict`; forceNew → nháp thứ hai; comment đã chốt → `CommentClaimedError` (An, đơn, mã) | `live.TestClaim_LV09` | A | ok |
| DH08 đường Live: 30 khách tranh 10 chiếc song song → 10 đơn nháp, 20 out_of_stock, có thể bán 0, `pg_stat_database.deadlocks` không tăng | `live.TestClaim_LastUnits` | A | ok |
| C11 (ADR-46): 200 vòng chốt comment của K song song với tin vào hộp thư và comment live mới của K (worker thật, kênh giả) → không deadlock, 0 việc dead, không yêu cầu lỗi, 200 ghi nhận, một khách | `live.TestClaim_C11_CustomerBeforeConversation` | A | ok |
| ADR-29: Page người bán partner, biến thể của ELIMAZ → `ErrSellerMismatch`, 0 đơn; Page khác, comment lạ, không phải comment live → `ErrNotFound`; yêu cầu sai (0 dòng, qty 21/0, mode lạ, add_to_draft thiếu đơn) → `ErrClaimInvalid` | `live.TestClaim_SellerAndAccess` | A | ok |
| Xin địa chỉ: Meta từ chối nhắn riêng → tin riêng failed có lý do; comment quá 7 ngày + hộp thư còn cửa sổ → tin thường queued trong hộp thư; cả hai không được → skipped_policy câu có "7 ngày", "24 giờ", ghi vào ghi nhận; chưa có hộp thư → lý do "hộp thư"; không bấm → not_requested; Page chưa có mẫu → not_requested kèm lý do; mẫu chọn tay → queued; Page chỉ xem → đơn draft vẫn tạo, skipped_policy "chỉ xem" | `live.TestClaim_AskAddress` | A | ok |
| Quy luật (rapid 100 lượt): chuỗi chốt / thêm vào nháp / gửi lại song song trên 1–3 biến thể tồn 0–5 → giữ của nháp = tồn − có thể bán ≤ tồn; mỗi comment ≤ 1 ghi nhận, ≤ 1 tin riêng; chỉ lỗi nghiệp vụ dự kiến | `live.TestClaimProperty_NeverOversellOneClaimPerComment` | A | ok |
| HTTP: 201 (orderCode, claim.by.name An, ask not_requested kèm lý do); 409 comment_claimed (claimedBy, orderId, orderCode); không live.operate → 403; không order.create → 403; Page khác, comment lạ → 404; add_to_draft thiếu đơn → 400; hết hàng → 409 out_of_stock (variantId, variantIds, available 0); 409 open_draft_exists (drafts: đơn, dòng, version); thêm vào nháp → 201 cùng đơn; Page người bán khác → 422 seller_mismatch | `httpapi.TestLiveClaimsAPI_Claim` | B | ok |
| Cùng Idempotency-Key 50 lần song song → 1 đơn; câu trả lời 201 cùng thân hay 409 request_in_progress | `httpapi.TestLiveClaimsAPI_SameKey` | B | ok |
| Bối cảnh (thẻ khách, nháp của phiên, SĐT 0901234567 tách từ "0901 234 567", người bán; Page khác 404) và danh sách comment (thứ tự đến, ghi nhận, nhắn riêng none, khách có nháp, lọc có SĐT, chưa chốt, trang tiếp theo đúng vị trí, mốc màn hình lùi 30 s, mốc hỏng 400, Page khác 404) | `httpapi.TestLiveClaimsAPI_ContextAndComments` | B | ok |
| Bảng đổi lỗi mới (9 dòng chụp, 6 dòng thuộc vùng) | `httpapi.TestDomainErrorSnapshot`, `TestDomainErrorSnapshot_TableMembership` | B | ok |
| Nhắn riêng của Task 158 sau khi tách `findInbox` | `send.TestPrivateReply*` | `go test -race -count=1 -run '^TestPrivateReply' ./internal/modules/conversation/internal/send/` | `ok … send 2.193s` |
| Kiến trúc (SQL ngoài sqlc, ranh giới, thứ tự khóa, hợp đồng M4 với 3 dòng m4_pending đã xóa) | `tests/arch` | `go test -count=1 ./tests/arch/` | ok (exit 0, trước gộp m4); lần đầu đỏ `TestDynSQLProductCode` vì câu SQL dựng dữ liệu thử nằm trong `convtest` → dời sang `internal/testkit/livetest` |
| C11 ca chắc chắn: tin vào đã khóa hộp thư rồi sửa lại chính dòng đó (kiểm lại khóa ngoại khách) trong lúc lệnh chốt giữ khóa khách và chờ hộp thư → không deadlock, cả hai xong | `live.TestClaim_C11_InboundFKRecheckWhileClaimWaits` | `go test -race -count=1 -run '^(TestClaim_C11_InboundFKRecheckWhileClaimWaits\|TestClaim_C11_CustomerBeforeConversation\|TestClaim_LV07_TwoStaffOneComment)$' ./internal/modules/conversation/internal/live/` | trước khi sửa: `FAIL … deadlock tăng 0 → 1`; sau: `ok … live 8.088s` |
| ADR-46 trong bảng khách (review lần 1): khách của comment là hồ sơ con K < gốc R; giao dịch khác khóa K rồi R (như `lockPair` của đơn, `LockCustomers` của gộp) chen giữa → lệnh chốt chờ ở K, không deadlock, đơn gắn K | `live.TestClaim_MergedCustomerLockOrder` | `go test -race -count=1 -run '^TestClaim_MergedCustomerLockOrder$' ./internal/modules/conversation/internal/live/` | tạm tắt khóa hồ sơ con: `FAIL … deadlock tăng 0 → 1`; mã thật: `ok … live 2.306s`; cả bộ chốt `go test -race -count=1 -run '^(TestClaim.*)$' …/live/` → `ok … live 15.580s` |
| Khóa gốc của customer sau khi đổi FOR NO KEY UPDATE (gộp, không liên hệ, tên từ kênh) | `customer.TestLockRoot`, `TestDoNotContactFollowsRoot`, `TestSetNameFromChannel` | `go test -race -count=1 -run '^(TestLockRoot\|TestDoNotContactFollowsRoot\|TestSetNameFromChannel)' ./internal/modules/customer/` | `ok … customer 2.104s`; sau Quyết định 12: `ok … customer 2.198s` |
| Cổng tĩnh rẻ | lint, jscpd | `golangci-lint run ./internal/modules/conversation/... ./internal/httpapi/ ./internal/testkit/livetest/`; `make jscpd` | `0 issues`; `Found 0 clones` |
| Cổng của task | ci-task | `scripts/agent/ci-task.sh` | Lần 1: `CI-TASK ĐỎ` — cổng tĩnh xanh; `TestClaim_C11_CustomerBeforeConversation` `deadlock tăng 0 → 4` trên máy từ xa (máy Mac không tái hiện được; tái hiện được khi giữ khóa lâu hơn, chẩn đoán bằng `pg_blocking_pids`: `LockConversationsByID` của lệnh chốt ⇄ `BumpConversationInbound` của tin vào) → sửa (Quyết định 11), thêm test ca chắc chắn. Lần 2 (sau sửa, run 45812576120, 12 phút blacksmith): cổng tĩnh xanh, test (2) xanh; test (1) chỉ đỏ `adapters/facebook` `TestDo_HangAfterReadTimesOut` (`client_test.go:199: mong maybe_sent/unknown, có <nil>`) — gói task không đổi (chạy vì đổi testkit → toàn bộ), chạy lại trên máy `go test -race -count=3 -run '^TestDo_HangAfterReadTimesOut$' ./internal/adapters/facebook/` → `ok … facebook 31.575s`: lỗi chập chờn của máy từ xa (ghi ở Việc phát hiện thêm). Mọi gói khác, kể cả `live`, `customer`, `httpapi`, `tests/arch`, xanh. Lần 3 (sau review lần 1, run 45814730189, 13 phút blacksmith): `REMOTE-TEST XANH`, `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh 7a7567954432…)` — static, test (1), test (2) đều success |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 172 không có test e2e; đường trọn `tests/e2e/live_flow_test.go` thuộc Task 173)

## Test bị xóa hoặc tắt

(không có)

## Quyết định thay chủ shop

1. **Cho chốt comment của phiên live đã kết thúc** (ghi chú điều phối 1): không kiểm trạng thái phiên. Vì: nhân viên
   thường lên đơn nốt sau khi tắt live; ghi nhận vẫn gắn đúng phiên; chính sách gửi (7 ngày, 24 giờ) vẫn chặn tin xin
   địa chỉ. Phương án khác: 409 khi phiên ended. Đảo ngược: một điều kiện trong `claimRun.load`.
2. **Đơn tạo theo người bán của Page** (`SellerID = channel_accounts.seller_id`), `order.ErrMixedSellers` đổi thành
   `ErrSellerMismatch` (422 seller_mismatch), vẫn so `res.Order.SellerID` sau khi tạo. Vì: kế hoạch để `SellerID` rỗng
   (người bán mặc định) rồi so — Page của người bán partner sẽ không bao giờ chốt được hàng của chính họ. ADR-29 giữ.
3. **Xin địa chỉ chạy trước khi chèn ghi nhận** (cần `ask_message_id`), trong cùng transaction; tin, lệnh là dòng mới.
   Bị chính sách chặn (chỉ xem; quá 7 ngày và hết cửa sổ; chưa có hộp thư) → đơn nháp vẫn tạo, `ask = skipped_policy`
   kèm câu tiếng Việt của cả hai lý do. Page chưa chọn mẫu và người bấm không chọn mẫu → `not_requested` kèm lý do
   "Page chưa chọn mẫu xin địa chỉ" (không chặn chốt). Mẫu người bấm chỉ định không có → 404, biến của mẫu thiếu → 422
   (lùi cả lần chốt, như gửi tin thường).
4. **Tin xin địa chỉ không đòi `conversation.reply`**: route chốt đã đòi `live.operate` trên Page (quyền vận hành live).
   Biến `{nhan_vien}` là tên người bấm khi họ có `conversation.read` trên Page, không thì tên Page.
5. **Thân 409 out_of_stock thêm `variantIds: [variantId]`** ở mọi đường (trường thêm, không phá): mô tả của
   `claimLiveComment` đòi `variantIds`, thân chung M1 có `variantId`. Sửa mô tả trường `variantIds` trong openapi
   (chỉ mô tả; oasdiff không đổi).
6. **Biến thể không có → 422 `variant_not_found`** (bảng lỗi chung của order), không phải 404 như kế hoạch ghi — giữ
   một chỗ đổi lỗi. **Đơn chọn để thêm không còn nháp → 409 `version_conflict`** (màn hình cũ, tải lại); **không của
   khách trong phiên → 422 `invalid_input`**.
7. **Mốc `after` của danh sách comment**: chuỗi "sentAt,id" màn hình gửi được lùi 30 s (như Task 159); `nextCursor`
   của trang tiếp theo có tiền tố `p:` và đúng vị trí (không lùi, tránh lặp vô hạn khi > 1 trang trong 30 s). Lọc
   "có SĐT": SQL lọc thô (có chữ số), Go lọc lại bằng `automation.ContainsPhone`; trang có thể ít hơn `limit` nhưng còn
   `nextCursor` khi SQL trả đủ trang.
8. **Hộp thư của khách không tạo ở bước khóa** (kế hoạch bước 1 "tạo nếu chưa có"): chỉ tạo khi nhắn riêng thật sự
   (`ensureInbox` của Task 158, dòng mới). Vì: không tạo hội thoại rỗng khi không xin địa chỉ.
9. **Quyền đơn**: mode new đòi `order.create` ở kho chính (`orderWarehouseAt`, như tạo đơn); add_to_draft đòi
   `order.update` (bất kỳ phạm vi, như guard của `updateOrder`); thiếu → 403. Audit `order.create` / `order.update`
   như hai route của M2; dòng ghi nhận không có audit riêng (thao tác vận hành, như Task 169, 170).
10. **Nguồn đơn theo giờ bắt đầu của phiên live** (ghi chú điều phối 3) ở đường chốt; `OrderContext` của Task 159 vẫn
    dùng giờ đăng bài (không chạm, ghi ở Việc phát hiện thêm).

11. **`customer.LockRoot` khóa `FOR NO KEY UPDATE` thay cho `FOR UPDATE`** (lỗi ci-task lần 1). Đường tin vào khóa hội
    thoại (upsert) rồi sửa lại chính dòng đó (`BumpConversationInbound`, `bumpForComment`): PostgreSQL kiểm lại khóa
    ngoại `customer_id` vì dòng do chính transaction đó vừa ghi → `FOR KEY SHARE` dòng khách, bị `FOR UPDATE` của lệnh
    chốt chặn trong lúc lệnh chốt chờ hội thoại → deadlock. `FOR NO KEY UPDATE` vẫn chặn lệnh chốt khác, gộp khách,
    `lockOwner`/`lockPair` (FOR UPDATE) và UPDATE cột thường, chỉ cho câu kiểm khóa ngoại đi qua; giữ đúng thứ tự
    ADR-46 (khách trước hội thoại). Phương án khác: lệnh chốt khóa hội thoại (FOR NO KEY UPDATE) trước rồi mới khóa
    khách — trái ADR-46 nhóm 5 → 6; hay sửa đường tin vào để không ghi hai lần một dòng hội thoại (tệp của Task
    153/154). Đảo ngược: một dòng SQL ở `queries/customer/lock.sql`.
12. **Khóa cả hồ sơ con khi khách của comment là hồ sơ đã gộp** (phát hiện của reviewer lần 1): `lockRootOnce` khóa
    hồ sơ con và gốc theo id tăng dần (ADR-46 trong một bảng), cùng thứ tự với `lockPair` và gộp; trước đó chỉ khóa
    gốc rồi `order.Create` khóa hồ sơ con sau — ngược thứ tự khi con < gốc. Cũng áp cho `SetDoNotContact` (khóa thêm
    hồ sơ con, vô hại). `send.AskAddress` nhận hộp thư đã khóa ở bước 1 thay vì tìm lại (ghi chú của reviewer: không
    khóa hội thoại nào sau đơn, tồn).

## Khác kế hoạch

- Thứ tự "test thất bại trước": mã viết trước, test viết ngay sau trong cùng phiên; không ghi được lần chạy FAIL của
  Step 2 (lần chạy đầu đỏ vì thiếu cài đặt đơn của tenant thử). Mọi test đã chạy và qua trên mã thật.
- Tệp: bước xin địa chỉ ở `live/claim.go` (hàm `ask`) + `send/ask.go` (`AskAddress`, dùng lại nội bộ nhắn riêng/gửi
  tin) thay cho `live/ask.go`; `live` nhập `send` (không vòng). Hàm dựng dữ liệu thử dùng chung ở
  `internal/testkit/livetest` (SQL dữ liệu thử chỉ được nằm trong testkit — TestDynSQLProductCode).
- Quyết định 2, 5, 6, 8 ở trên.

## Việc phát hiện thêm

- Task 159 / 176: `OrderContext` (bối cảnh lên đơn cạnh khung chat) nên lấy giờ bắt đầu từ phiên live
  (`live_sessions.started_at`) thay cho giờ đăng bài, như đường chốt.
- `inbound.onLiveComment` (hooks.go) vẫn là hàm rỗng "hook chốt của Task 172": chốt là thao tác của nhân viên, không cần
  hook ở đường vào; task sau có thể xóa hay dùng.
- `adapters/facebook` `TestDo_HangAfterReadTimesOut` chập chờn trên bể blacksmith (ci-task lần 2 của Task 172): trả
  `<nil>` thay cho lỗi maybe_sent sau 10 s treo; máy Mac qua 3/3.
- Ghi chú điều phối 2 (lịch đọc comment cho Page fakechat ngoài production): không chạm ở task này.

## Reviewer

Kết luận: ĐẠT (lần 2, sau vòng sửa).

- Lần 1: CẦN SỬA — một phát hiện: khách của comment là hồ sơ con K đã gộp vào gốc R, K < R: `LockRoot` chỉ khóa R rồi
  `order.Create` (`lockPair`) khóa K → ngược thứ tự ADR-46 trong bảng khách với `lockPair`/gộp (deadlock). Ghi chú phụ:
  `AskAddress` tìm lại hộp thư sau khi đã khóa đơn, tồn; mục OpenAPI của Ảnh hưởng ghi chưa khớp diff. Đã sửa
  (Quyết định 12, test `TestClaim_MergedCustomerLockOrder` đỏ với mã cũ, xanh với mã mới; `AskAddress` nhận hộp thư đã
  khóa; sửa mục Ảnh hưởng), ci-task lần 3 xanh.
- Lần 2: ĐẠT, không phát hiện phải sửa. Ghi chú không bắt buộc: (1) bỏ `LiveAsk`, sửa đường dẫn `ask.go` trong Ảnh
  hưởng — đã sửa; (2) lúc `order.Create` nâng khóa khách lên FOR UPDATE, một giao dịch khác đang giữ FOR KEY SHARE dòng
  khách (vừa chèn hội thoại mới cho khách) mà lại chờ hội thoại lệnh chốt đang giữ thì hai bên chờ nhau — reviewer
  không tìm thấy đường mã nào như vậy (đường tin vào chỉ chèn hội thoại mới), test 200 vòng với worker thật xanh; ghi
  lại cho task sau chạm đường tin vào.

## Gộp vào m4

Phiên gộp tuần tự ngày 26/11, nhánh `task-172` (1a696156, tách từ m4 7a756795) vào m4 (80f0a119).

- Xung đột nội dung: chỉ `tests/arch/testdata/m4_pending.txt` — m4 (Task 168) đã bỏ `listFollowUps`, nhánh bỏ
  `listLiveComments`, `getLiveCommentContext`, `claimLiveComment`. Giải: bỏ cả bốn dòng (đều đã có mã thật), còn
  `listPendingDrafts`, `remindPendingDrafts` (chưa có handler).
- Xung đột ngầm (gộp tự động nhưng không biên dịch): hằng test `fbPageExt`, `fbToken` khai ở cả
  `live/source_fb_test.go` (Task 171, đã ở m4) và `live/claimenv_test.go` (nhánh) trong cùng gói `live_test`. Giải:
  đổi tên phía nhánh thành `claimPageExt`, `claimToken` (sửa mọi chỗ dùng: `claimenv_test.go`, `ask_test.go`).
- Tệp gộp tự động còn lại (`errors.go`, `errors_conversation.go`, `errors_snapshot_test.go`,
  `handlers_conversation_read.go`, `send/private_reply.go`, `ordercontext.go`, `models.go` của các store,
  `openapi.yaml`): giữ đủ phần hai bên. Migration `0410_live_claims.sql` dùng số đã dành trong kế hoạch (0409, 0411
  đã có ở m4), chỉ thêm bảng.
- Kiểm trên máy: `make gen` không đổi tệp nào; `go build ./...`; `go vet` cmd/crm, httpapi, conversation/...,
  customer/..., testkit/livetest, tests/arch: qua; `go test -c` gói `live` biên dịch được.
- Merge-test lần 1 (tree 4e01e73d, chưa sửa gì thêm): `MERGE-TEST XANH (blacksmith)`,
  https://github.com/quyenld37/elimaz-crm/actions/runs/45816803006 (12 phút máy).

### Reviewer lượt gộp, lần 1: CẦN SỬA

- Phát hiện: `conversation.live_comment_claims` (0410) có khóa ngoại tới `conversation.messages` (`message_id`,
  `ask_message_id`) không ON DELETE, còn việc dọn 24 tháng của Task 164 (`DeleteExpiredMessages`) xóa thẳng tin →
  comment đã chốt quá 24 tháng làm DELETE lỗi 23503, việc dọn của tenant đứng hẳn ở tin cũ nhất đó (Q17). Lỗi có từ
  nhánh (Task 164 đã ở điểm tách) nhưng chỉ lộ khi hai phần chung một bản.
- Sửa (theo mẫu `message_revisions` của chính việc dọn, không đổi thiết kế, không đụng đơn nháp):
  - `migrations/0416_live_claims_retention.sql`: `GRANT DELETE ON conversation.live_comment_claims TO crm_app` (pha mở
    rộng; số 0416 là số kế tiếp còn trống trong dải M4 lúc gộp, theo Global Constraints của kế hoạch).
  - `queries/conversation/retention.sql`: câu `DeleteClaimsOfMessages` (xóa ghi nhận có `message_id` hay
    `ask_message_id` thuộc lô); `make gen` sinh `store/retention.sql.go`.
  - `history/retention.go` `deleteMessages`: xóa lịch sử sửa → ghi nhận chốt → tin (thứ tự nhóm 6 ADR-46:
    live_comment_claims trước messages).
  - Test `TestRetention_LiveClaims` (history): comment đã chốt 26 tháng + tin xin địa chỉ → xóa 2 tin, ghi nhận của
    chúng mất; ghi nhận của comment 1 tháng còn.
- Bằng chứng sửa:
  - Đỏ trước: tree bbbeaf9a (có test, migration, câu SQL; `retention.go` chưa gọi `DeleteClaimsOfMessages`),
    `merge-test.sh` chỉ gói history: `--- FAIL: TestRetention_LiveClaims`, MERGE-TEST ĐỎ,
    https://github.com/quyenld37/elimaz-crm/actions/runs/45817986815.
  - Xanh sau: tree f4d3181d (bản gộp đầy đủ + phần sửa), đủ danh sách gói (cmd/crm, httpapi/..., conversation/...,
    customer/..., channel/..., adapters/facebook/..., order/..., testkit/..., tests/arch; -race: httpapi, conversation,
    live, send, history, customer) và cổng tĩnh: `MERGE-TEST XANH (blacksmith)`,
    https://github.com/quyenld37/elimaz-crm/actions/runs/45817982263 (10 phút máy). Không có test chập chờn.

### Reviewer lượt gộp, lần 2: ĐẠT

Không phát hiện phải sửa, không ghi chú. Đã đối chiếu: 0416 chỉ GRANT DELETE (RLS áp cả DELETE), câu
`DeleteClaimsOfMessages` và mã sinh khớp, thứ tự xóa đúng nhóm 6 ADR-46, hội thoại rỗng không còn bị ghi nhận chặn,
`TestRetention_LiveClaims` kiểm đúng tiêu chí (đỏ trước, xanh sau); các phần gộp của lượt 1 không đổi.
