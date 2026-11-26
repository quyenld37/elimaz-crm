# Task 154 — Comment vào — bài viết, hội thoại comment, sửa, xóa, ẩn có lịch sử

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 154 = M4-14). Nhánh gốc: `m4`.
Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Mới (gói gốc conversation): `conversation.MessageRevision`, `conversation.ListMessageRevisions(ctx, tx, u, messageID)`
    (1 chỗ dùng: `internal/httpapi/handlers_message_revisions.go`). Lỗi dùng lại `conversation.ErrNotFound` (đã map 404).
  - Mới (gói internal inbound): `inbound.FetchPostKind` (hằng tên lệnh `command.fb.fetch_post`), handler lệnh
    `fetchPost`; `inbound.Parts` thêm `Commands` (chỉ khi Registry có adapter hiện thực `channel.PostFetcher`).
    `handler.Handle` thêm nhánh `channel.EventCommentChanged`. Không đổi chữ ký nào đã có.
  - httpapi: `(*handler).ListMessageRevisions` thay bản `gen.UnimplementedHandler` (operation đã có trong hợp đồng Task 143).
- Gói phụ thuộc ngược (go list): `conversation/internal/inbound` ← `conversation` ← `cmd/crm`, `internal/httpapi`.
  `conversation` ← `internal/httpapi` ← `cmd/crm`, `tests/e2e`.
- Operation OpenAPI bị chạm, oasdiff: `listMessageRevisions` (GET `/api/v1/messages/{id}/revisions`) chỉ hiện thực, hợp
  đồng không đổi (oasdiff không chênh); xóa dòng `listMessageRevisions` khỏi `tests/arch/testdata/m4_pending.txt`.
- Chỗ dùng trên màn hình: không (màn hình M4 là task 174–178; `console/src/api` không đổi vì hợp đồng không đổi).
- Câu sqlc, bảng, pha migration:
  - Mới `queries/conversation/comments.sql` → `internal/modules/conversation/internal/store/comments.sql.go` (make gen):
    chèn bài chưa có (`ON CONFLICT DO NOTHING`), tìm bài, upsert hội thoại comment, tìm hội thoại của comment cha,
    khóa hội thoại, chèn comment với trạng thái, khóa comment theo mã ngoài, đổi nội dung/xóa/ẩn, ghi lịch sử có cờ,
    đọc Page của tin, đọc lịch sử của tin.
  - Dùng lại câu của Task 151/153: `UpsertPost` (OnDone của `fetch_post`), `BumpConversationInbound`,
    `BumpConversationOutbound`.
  - Bảng chạm: `conversation.posts`, `conversation.conversations`, `conversation.messages`,
    `conversation.message_revisions` (ghi); `customer.*` qua `customer.Writer.FindOrCreateByIdentity`; `channel` qua
    `AccountByExternal`, `NoteInbound`, `Account`, `CredentialsFor`; `platform.jobs` qua `queue.Enqueue` (lệnh fetch_post).
  - Không có migration (bảng, cột, ràng buộc của 0402, 0412 đã đủ).
- Tệp khác: `inbound/export_test.go` (đưa `postKind` ra test), `inbound/hooks.go` (hook khung `isLiveComment`, `onCommentCreated`, `onLiveComment` cho Task 165/166, 170),
  `cmd/crm/wire_m4_test.go` (danh sách lệnh M4 thêm `command.fb.fetch_post`), `internal/modules/conversation/wiring_test.go`
  (có adapter đọc bài → có lệnh fetch_post).
- Test sẽ chạy: test mới trong `internal/modules/conversation/internal/inbound/` (comments, posts, same_person) và các
  test cũ của gói (cả gói, ~10 s); `internal/modules/conversation/` (wiring_test, revisions_test); `internal/httpapi`
  chỉ `-run TestListMessageRevisions` khi viết, cả gói ở ci-task; `cmd/crm` (`TestWireM4_Frame`); `tests/arch`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (commands, queue,
  webhooks, realtime, testkit); `customer.Writer.FindOrCreateByIdentity` (Task 152), `channel.AccountByExternal`,
  `channel.NoteInbound`, `channel.Account`, `channel.CredentialsFor`, `channel.PostFetcher`, `channel.Registry`
  (Task 145/146), `commands.CommandKind`, `commands.DoneHook`, `commands.Looker`; trong inbound của Task 153: `account`,
  `publish`, `snippet`, `text`, `pendingAttachments`, `OnInboundAttachments`, `OnNewCustomer`, `onNewConversation`,
  `with`, `ignore`; câu `UpsertPost`, `BumpConversationInbound/Outbound`; `access.Require`, `access.PageScope`;
  test: env của `inbound/env_test.go`, `fakegraph` (bài, Page, token), `testkit.ChannelAccount`, `testkit.DrainDue`.
- Dùng lại / viết mới vì: dùng lại toàn bộ phần trên. Viết mới: phần nghiệp vụ comment (`inbound/comments.go`), lệnh
  đọc bài (`inbound/posts.go`, theo khuôn `channel.pageCommand` nhưng là lệnh chỉ đọc của conversation: đọc tài khoản +
  token trong tx ngắn rồi gọi `PostFetcher` — không gộp với `pageCommand` vì khác module, khác lý do thay đổi),
  câu SQL comment riêng (`comments.sql`, câu chèn tin của 153 không có cột `parent_external_id`, `post_id`, trạng thái).
  `thread` của 153 chỉ cho inbox: tách phần "tìm hoặc tạo khách" thành `customerFor` dùng chung cho inbox và comment
  (thay đổi cấu trúc, cùng commit vì nhỏ; hành vi inbox giữ nguyên).

## Soát thiết kế

Khớp kiến trúc (mục 5):
- 5.1 Bài, hội thoại comment, tin comment, lịch sử thuộc conversation; khách thuộc customer; Page, token thuộc channel.
- 5.2 Không ghi bảng module khác: khách qua `customer.Writer`, mốc Page qua `channel.NoteInbound`, lệnh qua `queue`.
- 5.3 Đọc Page, token qua public API của channel (`AccountByExternal`, `Account`, `CredentialsFor`).
- 5.4 Một sự kiện comment = một transaction (việc `inbound.<nguồn>`): khách + bài + hội thoại + tin + lịch sử + lệnh
  đọc bài + báo hiệu nguyên tử. OnDone của fetch_post ghi bài trong tx đổi lệnh sang done.
- 5.5 Báo hiệu `conversation.changed` qua `Bus.Publish` trong tx. Hook task sau (tự động 165/166, live 170) là hàm khung.
- 5.6 Gọi ra ngoài duy nhất là đọc bài: lệnh `command.fb.fetch_post` (mode command), Execute ngoài transaction; token
  chỉ giải mã trong Execute, không vào payload, log.
- 5.7 Mọi câu trong tx tenant (việc vào; lệnh: `tenancy.WithTenant` theo `j.TenantID`; API: `db.TxAPI`).
- 5.8 inbound, conversation không nhập `net/http`; handler HTTP mỏng.
- 5.9 Route đọc `GET /api/v1/messages/{id}/revisions` (không ghi, không khóa chống trùng). Chống trùng đường vào: tầng 1
  `jobs.dedupe_key` (adapter: `c:<comment>:<verb>:<text>`), tầng 2 `messages_external_key`, `posts_external_key`,
  `conversations_thread_key`; lệnh đọc bài khóa chống trùng `fetch_post:<post id>`.
- 5.10 Không Valkey. 5.11 Không thư viện mới. 5.12 Theo mẫu module (sqlc, test PostgreSQL thật, handler mỏng).

Đúng khi chạy đồng thời (mục 6):
- 6.1 / 6.3 **Thứ tự khóa (ADR-46):** Page (nhóm 3, `NoteInbound`) → khách (nhóm 5, khóa tư vấn danh tính) → `posts`
  (chèn DO NOTHING) → `conversations` (upsert DO UPDATE, khóa dòng) → `messages` (chèn, hay khóa FOR UPDATE) →
  `message_revisions` (chèn) → cập nhật lại hội thoại đã khóa. Sửa/xóa/ẩn tin đã có: chỉ khóa dòng tin rồi ghi lịch
  sử, không chạm hội thoại (không đảo thứ tự). Sửa/xóa/ẩn tới trước khi có tin: khóa tin không ra dòng (không giữ khóa
  nào) → đi đường tạo như `add`; chèn đụng (add song song thắng trước) → khóa tin rồi áp đổi (hội thoại đứng trước tin,
  đúng thứ tự). Comment của Page: tìm hội thoại của comment cha không khóa → bài → khóa hội thoại → chèn tin. CH15: tin
  nhắn và comment cùng PSID khác OrderKey chạy song song; khóa tư vấn danh tính của customer tuần tự hóa phần tạo khách
  → 1 khách (test 50 vòng hai worker).
- 6.2 Cùng sự kiện hai lần: `ON CONFLICT DO NOTHING` (tin), sửa có nội dung bằng nội dung hiện tại hay `edited_at` ≥
  mốc → bỏ qua; xóa khi đã xóa, ẩn khi đã ẩn → bỏ qua (không thêm dòng lịch sử). `add` đến sau sự kiện đổi trạng thái →
  đụng khóa tự nhiên, không ghi đè. Lệnh đọc bài chạy lại/giao lại: `UpsertPost` ghi đè cùng giá trị.
- 6.4 Lỗi tạm → lịch thử lại của hàng đợi; payload hỏng → `queue.ErrPermanent`. Lệnh đọc bài: chưa gửi/quá tải →
  not_sent (tự thử lại); quyền/bài không có → rejected (dead, bài giữ `unknown`); không rõ → unknown → in_doubt, Lookup
  trả not_sent (lệnh chỉ đọc, chạy lại không gây tác dụng phụ ngoài).
- 6.5 READ COMMITTED + câu có điều kiện (`GREATEST`, `coalesce(removed_at, …)`, `ON CONFLICT`).
- 6.6 Như 6.4. 6.7 Lệnh đọc bài có hạn 30 s một lần, hết hạn sau 1 giờ còn chờ. 6.8 Tiến trình chết → tx lùi, việc
  chạy lại an toàn nhờ khóa tự nhiên; lệnh giữ chỗ hết hạn → in_doubt → Lookup not_sent → chạy lại.
- 6.9 Handler chỉ vài câu SQL; không gọi mạng trong tx. 6.10 Không áp dụng: không có tiền.
- 6.11 "Thứ tự đến không đổi trạng thái cuối" (xóa trước add) kiểm bằng test đảo thứ tự; không phải vùng rủi ro cao.
- 6.12 Nội dung comment, tên, PSID không vào log, lỗi, payload việc (payload lệnh chỉ có mã tài khoản, mã bài, mã ngoài
  của bài); lỗi bọc chỉ có loại sự kiện.
- 6.13 Báo hiệu đòi `conversation.read` phạm vi `page:<id>`; API lịch sử kiểm `conversation.read` trên Page của tin,
  thiếu → 404 (không lộ sự tồn tại).

## Quyết định thay chủ shop

1. **Comment của Page vào hội thoại của comment cha.** Kế hoạch chỉ nói "tin out, page_external, không tạo khách". Hội
   thoại comment thuộc (bài, khách); comment của Page trả lời comment khách → hội thoại chứa comment cha. Comment gốc
   của Page (cha là bài) hay cha chưa có trong CRM → bỏ qua, đếm `inbound_ignored{page_comment_no_thread}`. Phương án
   khác: hội thoại riêng cho Page (không có khách, trái `customer_id NOT NULL`).
2. **Ý nghĩa một dòng `message_revisions`:** `body` = nội dung *trước* lần đổi; `is_hidden`, `removed` = trạng thái
   *sau* lần đổi (kế hoạch: xóa ghi "nội dung cũ, removed = true"). Dòng của đường tin nhắn (Task 153) không ghi cờ →
   API đọc là false. Sự kiện đổi trạng thái tới trước add → một dòng lịch sử `body` NULL (không biết nội dung trước).
3. **Nguồn của lịch sử:** sửa, xóa comment của khách → `customer`; ẩn/hiện (Page kiểm duyệt, kể cả ngoài CRM) và mọi
   đổi trên comment của Page → `page_external`. Webhook không cho biết Page hay khách xóa comment của khách: ghi
   `customer`. Task 158 (ẩn từ CRM) tự ghi `staff`; webhook ẩn lặp lại sau đó thấy trạng thái đã đúng nên không ghi thêm.
4. **Chống trùng, sai thứ tự:** sửa bỏ qua khi nội dung bằng nội dung hiện tại hoặc `edited_at` ≥ mốc sự kiện; xóa bỏ
   qua khi đã xóa; ẩn/hiện bỏ qua khi trạng thái đã đúng (không có cột mốc ẩn nên ẩn/hiện đảo thứ tự giữa nhau theo
   thứ tự xử lý; cùng bài cùng người cùng OrderKey nên đến lần lượt). Comment chèn bởi sự kiện xóa tới trước add không
   đổi hội thoại (không tăng chưa đọc) và không gọi hook tự động.
5. **Lệnh đọc bài:** chỉ xếp khi adapter của nguồn hiện thực `channel.PostFetcher` (facebook; kênh giả fakechat không có
   → không xếp, không dead). Bài không còn trên kênh (lỗi vĩnh viễn) → lệnh `done` kèm `missing`, bài giữ `unknown`,
   có `fetched_at` (không dead, không cảnh báo vô ích); token/quyền/chính sách → rejected; không rõ → in_doubt, Lookup
   trả `not_sent` (lệnh chỉ đọc). Hạn 1 giờ, 30 s một lần.
6. **Loại bài:** `status_type` chứa "live" → `live`; rỗng → `unknown` (UpsertPost giữ loại đã biết); còn lại → `post`.
   Bài quảng cáo (`ad`) chưa nhận biết từ `status_type` (Task 170/181 đối chiếu E-thí nghiệm). Đoạn trích bài 120 ký tự.
7. **Hook khung** `isLiveComment`, `onCommentCreated`, `onLiveComment` đặt trong `inbound/hooks.go` (như Task 153), không
   tạo hàm trong gói `live`, `automation` (của Task 165/166/170/172): tránh xung đột tệp; task sau nối hook vào phần của mình.
8. **Tên người comment** dùng làm tên khách khi tạo mới (`name_source = channel`); khách đã có giữ tên. `parent_external_id`
   chỉ ghi khi cha là comment khác (cha = bài → NULL).
9. **Comment không có `from`** (Meta có thể bỏ khi app thiếu quyền đọc nội dung người dùng): bỏ qua, đếm
   `inbound_ignored{comment_without_from}`, việc xong (không dead); thiếu mã comment, bài, mốc → lỗi vĩnh viễn (dead).
10. **Lịch sử của ghi chú nội bộ** → 404 (ghi chú không có lịch sử từ kênh); API đọc lịch sử cho mọi loại tin khác.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| CH02: comment add → 1 bài unknown chưa đọc, 1 hội thoại comment có post_id (`<post>:<from>`), 1 tin comment in (tên người comment, không cha), chưa đọc 1, 1 báo hiệu, khách tên thật; 1 lệnh fetch_post; chạy lệnh trên fakegraph → permalink, snippet 120 ký tự, created_time; comment 2 cùng người cùng bài → cùng hội thoại, chưa đọc 2; trả lời comment → `parent_external_id = c2`; bài khác → hội thoại mới cùng khách; 2 lệnh (một mỗi bài) | `TestCommentAddCreatesPostConversationMessage` | `go test -race -count=1 ./internal/modules/conversation/internal/inbound/` | `ok …/inbound 9.092s` |
| CH13: sửa → body mới, edited_at, 1 dòng lịch sử nội dung cũ (customer); giao lại, bản sửa cũ hơn → không thêm; xóa → removed_at, giữ body, 2 dòng đúng thứ tự (dòng 2 removed); xóa lại → không thêm; chưa đọc không tăng | `TestCommentEditAndRemoveKeepHistory` | như trên | PASS |
| CH13 sai thứ tự: xóa trước add → removed_at, 1 dòng lịch sử; add sau không xóa removed_at, không thêm tin; sửa trước add → giữ bản sửa; ẩn trước add → giữ ẩn | `TestCommentStateEventsBeforeAdd` | như trên | PASS |
| Ẩn → is_hidden, tin vẫn trong danh sách tin của hội thoại, 1 dòng lịch sử page_external; ẩn lại → không thêm; hiện → 2 dòng | `TestCommentHideUnhide` | như trên | PASS |
| Comment của Page → tin out page_external trong hội thoại của comment cha, không tạo khách, chưa đọc giữ, last_staff_message_at; comment gốc của Page → bỏ qua + `inbound_ignored{page_comment_no_thread}`; không có người comment → bỏ qua + `inbound_ignored{comment_without_from}`, không dead; ảnh trong comment → mục đính kèm | `TestCommentFromPage` | như trên | PASS |
| CH15: 50 vòng, mỗi vòng PSID mới, tin nhắn + comment cùng PSID, hai worker đồng thời (khác OrderKey) → mỗi vòng 1 danh tính, 2 hội thoại cùng customer_id; 50 khách; không dead | `TestSamePersonMessageAndCommentOneCustomer` | như trên | PASS (2,2 s) |
| Lệnh đọc bài: bài không có → done (không dead), bài unknown có fetched_at; payload chỉ 3 mã; Lookup → not_sent | `TestFetchPostMissingAndLookup` | như trên | PASS |
| Kênh giả không đọc được bài → comment lưu, không xếp lệnh | `TestCommentFakechatNoFetch` | như trên | PASS |
| Loại bài từ status_type | `TestPostKind` | như trên | PASS |
| Đọc lịch sử (module): 2 dòng đúng thứ tự, cờ, nguồn; không lịch sử → rỗng; Page khác, tin không có, ghi chú → ErrNotFound | `TestListMessageRevisions` (conversation) | `go test -race -count=1 -run '^(TestListMessageRevisions\|TestWiring)$' ./internal/modules/conversation/ ./internal/httpapi/` | `ok …/conversation 3.241s` |
| API `GET /api/v1/messages/{id}/revisions`: 200 hai dòng; Page khác, tin không có → 404; không conversation.read → 403; dòng `listMessageRevisions` xóa khỏi m4_pending | `TestListMessageRevisions` (httpapi), `TestM4Contract_StatusCodes` | như trên; `go test -race -count=1 -run '^(TestListMessageRevisions\|TestM4Contract_StatusCodes)$' ./internal/httpapi/` | `ok …/httpapi 3.224s`, `ok …/httpapi 5.120s` |
| Nối: có adapter đọc bài → thêm đúng lệnh fetch_post; production/dev có Facebook → danh sách lệnh M4 có fetch_post | `TestWiring`, `TestWireM4_Frame` | như trên; `go test -count=1 -run '^TestWireM4_Frame$' ./cmd/crm/` | `ok …/cmd/crm 0.738s` |
| Tin nhắn vào (Task 153) không đổi sau khi tách `customerFor` | mọi test cũ của `inbound` | như dòng 1 | PASS |
| ci-task (cổng tĩnh + test theo gói bị ảnh hưởng, trước khi gộp m4) | các gói đổi và phụ thuộc | `scripts/agent/ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh aafa0878…)`; `ok …/conversation 16.503s`, `ok …/inbound 4.585s`, `ok …/httpapi 70.059s`, `ok …/cmd/crm 5.387s` |
| Sau khi gộp m4 (Task 160, fast-forward; chỉ `m4_pending.txt` vướng: giữ cả hai bên xóa dòng) | conversation/..., httpapi (revisions, hợp đồng M4), cmd/crm, tests/arch, lint, jscpd | `go test -race -count=1 ./internal/modules/conversation/...`; `-run '^(TestListMessageRevisions\|TestM4Contract_StatusCodes)$' ./internal/httpapi/`; `-run '^TestWireM4_Frame$' ./cmd/crm/`; `go test -count=1 ./tests/arch/`; golangci-lint; `make jscpd`; `make gen` không chênh | `ok …/conversation 3.806s`, `ok …/inbound 4.263s`, `ok …/httpapi 2.527s`, `ok …/cmd/crm 0.763s`, `ok …/tests/arch 74.732s`, `0 issues.`, `Found 0 clones.` |
| Cổng tĩnh tự soát | golangci-lint, jscpd, tests/arch | `golangci-lint run ./internal/modules/conversation/... ./internal/httpapi/ ./cmd/crm/`; `make jscpd`; `go test -count=1 ./tests/arch/` | `0 issues.`; 0 clone mới; `ok …/tests/arch 248.970s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm tệp ngoài danh sách của kế hoạch: `queries/conversation/comments.sql` (+ mã sinh), `internal/modules/conversation/revisions.go`
  (public API cho handler), `revisions_test.go`, `internal/httpapi/handlers_message_revisions_test.go`, `inbound/export_test.go`.
- Kế hoạch ghi `comments.go` là "tệp khung" nhưng trên m4 chưa có tệp đó: tạo mới.
- Bài không còn trên kênh: lệnh `done` (missing) thay vì dead (Quyết định 5).
- Bước "chạy → FAIL": test mới đỏ ở bước biên dịch (ký hiệu `FetchPostKind`, `PostKind`, `ListMessageRevisions` chưa có).

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 154 không có test e2e)

## Việc phát hiện thêm

- (Reviewer, ghi chú) Mốc sửa comment lấy từ `created_time` của webhook feed: nếu Meta gửi giờ tạo comment chứ không
  phải giờ sửa, từ lần sửa thứ hai các bản sửa bị bỏ qua (`edited_at` ≥ mốc); hai lần sửa trong cùng một giây cũng vậy.
  Fixture `comment_edited.json` cho thấy `created_time` sát `entry.time` (giờ sửa), nhưng cần đối chiếu payload thật ở
  thí nghiệm Meta (Task 181); nếu sai thì đổi so sánh sang `entry.time` hoặc chỉ so nội dung.
- (Reviewer, ghi chú) `coalesce(is_hidden/removed, false)` của `ListMessageRevisions` chưa có test với dòng cờ NULL trên
  tin thường (dòng NULL trong test gắn với ghi chú → 404). Task 159/175 (màn lịch sử) nên thêm ca này.

## Reviewer

Kết luận: ĐẠT (agent reviewer, lần 1, trên phần đã stage sau khi gộp m4 có Task 160).

- Phát hiện phải sửa: không có. Đã soát thứ tự khóa ADR-46 (khách → bài → hội thoại → tin → lịch sử; sửa/xóa/ẩn tin đã
  có chỉ khóa tin), chạy lại trong RetryTx (`load` gán lại biến), quyền (404 không lộ, 403 do `x-permission`), nối
  `Keys` ở `cmd/crm/wire_m4.go`, không dữ liệu cá nhân trong lỗi/metric, SQL qua sqlc, không thư viện, migration mới.
- Ảnh hưởng: khớp; thiếu sót tài liệu nhỏ: `inbound/env_test.go` (thêm `newEnvWith`, `runner`, `deps`) chỉ nêu ở mục
  Tái dùng.
- Bảng bằng chứng: mọi dòng trỏ test có thật và kiểm đúng tiêu chí; không test nào bị xóa, tắt, nới.
- Ghi chú không bắt buộc: mốc sửa comment (chép vào "Việc phát hiện thêm"); `coalesce` cờ NULL chưa có test; handler
  coi `body` rỗng như NULL (không ảnh hưởng vì `text("")` ghi NULL).

## Gộp vào m4

Phiên gộp tuần tự ngày 26/11 (thứ tự 154 → 156 → 150 → 170), `git merge --no-ff --no-commit task-154` trên m4 `7452724d`.

- Xung đột: chỉ `tests/arch/testdata/m4_pending.txt`. m4 (Task 161) đã bỏ `addConversationNote`, nhánh bỏ
  `listMessageRevisions`; cả hai đã có mã thật nên bỏ cả hai dòng.
- Các tệp dùng chung khác (`cmd/crm/wire_m4_test.go`, `internal/modules/conversation/wiring_test.go`) git tự gộp, giữ
  đủ phần của hai bên. `make gen` không sinh chênh lệch (mã store `comments.sql.go` của nhánh khớp nguồn).
- Bằng chứng (chạy trên bản đã gộp):
  - `go build ./...` qua; `go vet ./cmd/crm/ ./internal/httpapi/... ./internal/modules/conversation/... ./tests/arch/` không lỗi.
  - `go test -race -count=1 ./cmd/crm/ ./internal/httpapi/... ./internal/modules/conversation/... ./internal/modules/channel/... ./internal/adapters/facebook/...` → mọi gói `ok`.
  - `make jscpd` → `Found 0 clones` (trước khi gộp cũng 0).
  - `go test -count=1 ./tests/arch/`: lần đầu hết giờ mặc định 10 phút (máy đang chạy nhiều phiên và golangci-lint
    `./...` của phiên khác; các test arch tự gọi golangci-lint); chạy lại `-timeout 40m -v` → `ok tests/arch 248.881s`.
    Ghi là chập chờn do tải máy, không phải lỗi mã.
  - `golangci-lint run ./cmd/crm/... ./internal/httpapi/... ./internal/modules/conversation/... ./tests/arch/...` → `0 issues.`
- Reviewer phần gộp: **ĐẠT** (agent reviewer trên phần đã stage). Không có phát hiện phải sửa. Đã soát: 28 dòng còn lại
  của `m4_pending.txt` không có handler; `wire_m4_test.go`, `wiring_test.go`, `inbound/*.go` không mất phần của m4
  (lịch Task 163/169, `checkReminders`), `wire_m4.go` đã truyền `Registry`, `Keys`; `comments.sql.go` giống bản đã duyệt.
  Ghi chú: hook `onNewConversation` giờ cũng chạy cho hội thoại comment — task nối round robin (160) và lấy lịch sử
  tin (164) cần quyết định có áp cho comment không.
