# Task 148 — Facebook — đường ra, SendPolicy, tra kết quả, kiểm sức khỏe

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 148, M4-08). Nhánh gốc: `m4`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences / grep cả test):
  - `facebook.New(appSecret, verifyToken logging.Secret) *Channel` → `facebook.New(o Options) *Channel` (Channel giữ
    Graph client; hợp nhất hàm tạo của Task 146 `NewClient(Options)` và Task 147). Chỗ gọi: 4, đều trong test của gói
    (`signature_test.go` ×3, `source_test.go` ×1) — sửa cả 4. Không gói nào ngoài `facebook` gọi `facebook.New`.
  - `facebook.Options` thêm trường `OAuthRedirectURL` (cho `ExchangeCode`); chỗ dựng: `client_test.go`, `wiring.go`.
  - `*facebook.Channel` thêm phương thức: `SendPolicy`, `Send`, `Lookup`, `HealthCheck`, `Backfill`, `ClassifyError`
    (đủ `channel.MessagingAdapter`), `FetchProfile`, `FetchPost`, `FetchHistory`, `Download`, `ExchangeCode`,
    `ListPages`, `PageToken`, `Subscribe`, `Unsubscribe` (các interface tùy chọn của `channel`). Hằng `SubscribedFields`,
    `RequiredPermissions`, sentinel `ErrNoToken` (Meta không trả token khi đổi mã, lấy token Page).
  - `facebook.Classify` đổi hành vi hai ca (ghi chú 2, 3 của reviewer Task 146): lỗi dựng yêu cầu → `permanent` (trước
    `not_sent_retry`); HTTP 408 không có JSON lỗi → `unknown` (trước `permanent`). Chỗ dùng: `client.go` (nhãn log;
    nhãn chỉ số `graph_errors` thêm giá trị `build`), `errors_test.go`.
  - `facebook.Source(cfg)` giữ chữ ký, dựng bằng `New(Options)` từ `cfg.Meta`; `facebook.Adapter(cfg)` giữ chữ ký và
    **vẫn trả nil** (Quyết định 1).
  - Hằng test `webhookAppSecret` (147) gộp vào `testAppSecret` (146), cùng giá trị.
- Gói phụ thuộc ngược (`go list`): chỉ `cmd/crm` nhập `internal/adapters/facebook` (gọi `facebook.Adapter(cfg)`, không
  đổi). Không sửa `internal/modules/channel` (để không kéo test của `httpapi`, `conversation`), không sửa
  `internal/testkit/fakegraph`.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có (màn M4 là Task 174–178).
- Câu sqlc, bảng, pha migration: không có (adapter không chạm database).
- Test sẽ chạy: `go test -race ./internal/adapters/facebook/` (test mới `policy_test.go`, `send_test.go`,
  `lookup_test.go`, `health_test.go`, `fetch_test.go`, `contract_test.go` và test cũ đã sửa), `go test ./cmd/crm/`
  (phụ thuộc ngược, qua ci-task); `tests/arch`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (logging.Secret,
  observability.ScrubMessage, urlerr, webhooks.InboundEvent/Via*), gói `facebook` (Client.Do, Classify, Reason,
  RetryAfter, policyReason, split.go: `ms`, `textKey`, `flexTime`, `attachments`), `channel` (kiểu chuẩn hóa, payload
  sự kiện), `adapters/fake/send.go` (SendPolicy của kênh giả), `channeltest.RunContract`, `testkit/fakegraph`.
- Dùng lại / viết mới vì:
  - Mọi lời gọi Graph đi qua `Client.Do` (appsecret_proof, hạn 10 s, phân "chưa gửi/có thể đã gửi"); phân loại lỗi
    dùng `Classify`/`policyReason`/`RetryAfter` của Task 146, không viết bảng thứ hai.
  - Sự kiện lịch sử, hỏi bù dùng lại kiểu `channel.MessagePayload`, `CommentPayload`, khóa chống trùng `m:<mid>`,
    `c:<id>:add:<textKey>` và `OrderKey` của `split.go` để sự kiện đọc lại trùng khóa với webhook (không tạo việc thứ
    hai); dùng `textKey`, `flexTime`, `RouteKind`.
  - `SendPolicy` của Facebook viết riêng, không gộp với `fake.SendPolicy`: cùng hình nhưng khác lý do thay đổi (luật
    của Meta theo payload thật E3/E8 so với kênh giả cho test); kế hoạch khóa "mọi giới hạn của 02 mục 3" ở adapter
    Facebook. Nếu jscpd báo trùng sẽ trả lời ở đây.
  - Contract test dùng `channeltest.RunContract` đủ (1)–(7), fixture là `testdata/*.json` của Task 147, máy Meta giả là
    `fakegraph` (FailNext, DropAfterRead, RefuseNextConn) — không thêm máy giả mới. Riêng bước đổi token dài hạn
    (`fb_exchange_token`) fakegraph chưa phục vụ: test dựng một `httptest.Server` nhỏ trong test thay vì sửa testkit.

## Soát thiết kế

- 5.1 Chủ dữ liệu: không bảng mới; kiểu Meta ở lại trong adapter, ra ngoài chỉ kiểu chuẩn hóa của `channel`.
- 5.2, 5.3: adapter không đọc, không ghi bảng nào.
- 5.4, 5.5: không áp dụng: adapter không có việc nghiệp vụ; bên gọi (lệnh `command.fb.*`, Task 156–158) gọi
  `Send` ngoài transaction.
- 5.6 Gọi ra ngoài: `Send` trả đủ bốn kết quả `sent | rejected | not_sent | unknown`; `not_sent` chỉ khi chắc chắn
  chưa tới Meta (yêu cầu chưa ghi trọn lên kết nối) hoặc Meta báo giới hạn (`rate_limited`, kèm `RetryAfter` ≥ 60 s);
  rớt kết nối/hết giờ sau khi gửi trọn, 5xx không JSON, mã 1/2 → `unknown` (bên gọi để `in_doubt`, không tự gửi lại).
  `Lookup` không bao giờ kết luận "chưa gửi" cho trả lời comment chỉ vì chưa thấy (Meta có thể trễ) → `unknown`.
  Adapter không tự gửi lại.
- 5.7: không truy vấn bảng tenant.
- 5.8: adapter nhập `net/http` (là bộ nối ngoài, đúng chỗ); module `channel` không nhập `net/http`.
- 5.9: không có đường ghi API mới.
- 5.10, 5.11: không Valkey, không thư viện mới (thư viện chuẩn).
- 5.12: theo mẫu adapter kênh giả (`adapters/fake`) và bộ nối ngoài `internal/adapters/<tên>`.
- 6.1–6.6, 6.10, 6.11: không áp dụng: không ghi database, không tiền, không tồn.
- 6.7 Bên ngoài báo quá tải: mã 4, 17, 32, 613, 80001, 80006 hay HTTP 429 → `not_sent` kèm `RetryAfter` theo
  header của Meta, tối thiểu 60 s. Hỏi bù (`Backfill`) giãn tối đa 2 lời gọi mỗi giây mỗi Page.
- 6.8, 6.9: mỗi lời gọi Graph có hạn 10 s (Client); hết hạn sau khi gửi trọn → `unknown`.
- 6.12 Dữ liệu cá nhân: nội dung tin, PSID, token không vào log, lỗi hay `Detail`: `Detail` là câu tiếng Việt cố định
  của `Reason()` (không có message gốc của Meta); token là `logging.Secret`; log debug của Client chỉ có đường dẫn đã
  bỏ mã. App token (`<app_id>|<app_secret>`) chỉ dựng lúc gọi `debug_token`, kiểu `logging.Secret`.
- 6.13 Quyền: `SendPolicy` mặc định từ chối (mục đích lạ → từ chối); `ReadOnly` thắng mọi luật; không có token →
  `rejected`/`no_permission` không gọi Meta.

## Quyết định thay chủ shop

1. **Chưa nối adapter Facebook vào chương trình (`facebook.Adapter(cfg)` vẫn trả nil).** Adapter đã đủ
   `channel.MessagingAdapter` (test `var _ channel.MessagingAdapter = (*Channel)(nil)` và RunContract), nhưng nối vào
   `channel.Registry` thì ingest nhận webhook Facebook và worker tách ra việc `inbound.facebook` khi chưa có handler
   (Task 153 chạy song song) — việc mồ côi dồn ở hàng chờ. Chọn: `Source(cfg)` dựng adapter đầy đủ từ `cfg.Meta`
   (cùng hàm `fromConfig`), `Adapter(cfg)` trả nil kèm chú thích; Task 153 (hoặc lượt gộp sau khi 153 vào `m4`) đổi
   một dòng `return nil` → `return fromConfig(cfg)` và sửa `TestSource`, `TestWireM4_Frame`. Phương án khác: nối ngay
   (sinh việc mồ côi khi Meta bật). Đảo ngược: một dòng.
2. **Lý do từ chối khi không phải chính sách.** `channel.DenyReason` (và enum `denyReason` của OpenAPI) chỉ có 7 lý do
   chính sách. `Send` từ chối vì nội dung sai (cả chữ lẫn ảnh, 14 nút, tiêu đề 21 ký tự, quá 2 000 ký tự, thiếu người
   nhận) hay Meta báo lỗi vĩnh viễn (mã 100…) trả `rejected` **không có `Reason`**, kèm `Detail` tiếng Việt và `Code`.
   Không thêm lý do mới để khỏi đổi module `channel` và hợp đồng API. Lỗi chính sách của Meta (551, 10900, 10/2018278,
   200…) → `rejected` có `Reason`. Phương án khác: thêm `invalid_content` vào `channel` và OpenAPI (đổi hợp đồng của
   Task 143). Đảo ngược: thêm hằng và một nhánh.
3. **Token hỏng (lớp `auth`) trả `rejected`/`no_permission` cùng lỗi gốc (`err` khác nil).** Kế hoạch: "trả `rejected`
   lý do `no_permission` và để bên gọi báo kênh hỏng". `SendOutcome` không có trường lớp lỗi, nên `Send`, `Lookup`
   trả kèm `*GraphError` để bên gọi dùng `ClassifyError(err) == auth` mà đánh dấu kênh `degraded`. Mọi kết quả khác
   trả `err = nil` (contract (6) đòi `rejected`, `not_sent` không lỗi). Phương án khác: bên gọi so `Code` với bảng mã
   token (lặp bảng của adapter ra ngoài). Đảo ngược: bỏ trả lỗi ở một nhánh.
4. **Hai ghi chú reviewer của Task 146 sửa luôn:** lỗi dựng yêu cầu (vd thân không mã hóa được) → `permanent` (lỗi của
   mã, thử lại chỉ tới `dead`); HTTP 408 không có JSON → `unknown` (tầng trung gian hết giờ sau khi yêu cầu có thể đã
   tới Meta). 4xx khác không có JSON vẫn `permanent`.
5. **Echo của tin nhắn riêng mang `recipient.comment_id`: chưa xử lý.** Chưa có payload thật (E3/E8); xử lý cần thêm
   trường vào `channel.MessagePayload` (đổi module `channel`, kéo test `httpapi`, `conversation`) và quyết định
   `OrderKey` không có PSID. Ghi ở Việc phát hiện thêm cho Task 157 sau E3/E8.
6. **`Purpose = hide` luôn được** (đúng kế hoạch), kể cả comment đã xóa (kênh giả từ chối ca này). Mục đích lạ hay rỗng
   → từ chối `no_permission` (mặc định từ chối).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| CH09 tin: 23 giờ 59 phút → `standard`, `Until = 2026-11-20T10:01:00Z`; đúng 24 giờ → `window_closed`; 3 ngày + Human Agent + người gõ → `human_agent`, `Until` = tin cuối + 7 ngày; tự động / tắt Human Agent / 7 ngày 1 giây / đúng 7 ngày / chưa từng nhắn → `window_closed` | `TestSendPolicy_Message` | `go test -race -count=1 -v -run '^(TestSendPolicy_.*\|TestSend_.*\|TestLookup_.*\|TestHealthCheck.*\|TestFetch.*\|TestBackfill.*\|TestConnector\|TestContract\|TestClassify\|TestDo_BuildErrorPermanent\|TestSource)$' ./internal/adapters/facebook/` | `--- PASS: TestSendPolicy_Message` |
| CH05 nhắn riêng: 6 ngày 23 giờ chưa dùng → được, `Remaining = 1`; đã dùng → `private_reply_used`; đúng 7 ngày, không biết giờ → `private_reply_expired` | `TestSendPolicy_PrivateReply` | như trên | `--- PASS: TestSendPolicy_PrivateReply` |
| Trả lời comment (xóa → `comment_removed`), ẩn luôn được; `ReadOnly` thắng mọi luật; khách chặn; mục đích lạ/rỗng → từ chối | `TestSendPolicy_CommentAndOverrides` | như trên | `--- PASS: TestSendPolicy_CommentAndOverrides` |
| Send tin chữ: `metadata = ClientRef`, `RESPONSE`, không `tag`; Human Agent → `MESSAGE_TAG`/`HUMAN_AGENT`; ảnh → `attachment.payload.url`, không `text`, `is_reusable false`; nút trả lời nhanh | `TestSend_MessageBody` | như trên | `--- PASS: TestSend_MessageBody` |
| 14 nút, tiêu đề 21 ký tự, chữ + ảnh, quá 2 000 ký tự, thiếu người nhận… → `rejected` không gọi Meta (0 lời gọi); không token → `no_permission`; 13 nút tiêu đề 20 ký tự → gửi được | `TestSend_RejectBeforeCall` | như trên | `--- PASS: TestSend_RejectBeforeCall` |
| Nhắn riêng qua `recipient.comment_id`, lần hai 10900 → `private_reply_used`; trả lời comment `POST {comment-id}/comments`; ẩn/hiện `is_hidden` | `TestSend_CommentPurposes` | như trên | `--- PASS: TestSend_CommentPurposes` |
| 551 → `rejected` `blocked_by_user`; 10/2018278 → `window_closed`; 613 → `not_sent`, `RetryAfter ≥ 60 s`; đóng kết nối trước khi gửi → `not_sent`; `DropAfterRead` → `unknown`; 502 HTML → `unknown`; 100 → `rejected`; token thu hồi → `rejected` `no_permission` kèm lỗi lớp `auth`, Detail không có token | `TestSend_Outcomes` | như trên | `--- PASS: TestSend_Outcomes` |
| Lookup trả lời comment: có comment con của Page cùng nội dung → `sent` kèm mã; chưa có / chỉ khách / nội dung khác → `unknown`; query `filter=stream`, `since` | `TestLookup_CommentReply` | như trên | `--- PASS: TestLookup_CommentReply` |
| Lookup ẩn: `is_hidden` đúng → `sent`, khác → `not_sent`; tin, nhắn riêng → `unknown` không gọi Meta; token thu hồi → `unknown` kèm lỗi auth | `TestLookup_HideAndMessages` | như trên | `--- PASS: TestLookup_HideAndMessages` |
| HealthCheck: token thu hồi → `TokenValid = false`, không lỗi; thiếu `feed` → `WebhookSubscribed = false`, `MissingFields = [feed]`; thiếu `pages_manage_engagement` → `PermissionsOK = false`; debug_token bằng app token; Subscribe đủ 11 trường | `TestHealthCheck` | như trên | `--- PASS: TestHealthCheck` |
| HealthCheck rớt kết nối → lỗi (không coi là token hỏng) | `TestHealthCheck_TransportError` | như trên | `--- PASS: TestHealthCheck_TransportError` |
| HistoryFetcher: hội thoại 25 tin → đúng 20 sự kiện mới nhất theo thời gian, tin Page là `message.echo` (5), khóa `m:<mid>`, `fb:<page>:<psid>`; query `messages.limit(20)` | `TestFetchHistory` | như trên | `--- PASS: TestFetchHistory` |
| ProfileFetcher, PostFetcher (fields đúng; bài không có → `permanent`) | `TestFetchProfileAndPost` | như trên | `--- PASS: TestFetchProfileAndPost` |
| Backfill: tin sau `since` (bỏ tin cũ, dừng trang khi hội thoại cũ hơn `since`), comment sau `since` từ feed, khóa như webhook; 3 lời gọi ≥ 1 s (≤ 2 lời gọi/giây); lỗi → trả đúng 1 lỗi | `TestBackfill, TestBackfill_Error` | như trên | `--- PASS: TestBackfill (1.01s)`, `--- PASS: TestBackfill_Error` |
| Connector: đổi mã → token dài hạn (`fb_exchange_token`, `redirect_uri`); `ListPages` có quyền nhắn/kiểm duyệt; `PageToken`; mã sai → lỗi | `TestConnector` | như trên | `--- PASS: TestConnector` |
| 04 mục 4: `RunContract(t, facebookHarness)` đủ (1)–(7); `*Channel` hiện thực `MessagingAdapter` và 5 interface tùy chọn (kiểm lúc biên dịch) | `TestContract` | như trên | `--- PASS: TestContract` |
| Ghi chú reviewer 146: 408 không JSON → `unknown`; 413 không JSON → `permanent`; lỗi dựng yêu cầu → `permanent`, không gọi Meta | `TestClassify, TestDo_BuildErrorPermanent` | như trên | `--- PASS: TestClassify`, `--- PASS: TestDo_BuildErrorPermanent` |
| `Source(cfg)` trả adapter đầy đủ; `Adapter(cfg)` vẫn nil (Quyết định 1) | `TestSource` | như trên | `--- PASS: TestSource` |
| Ổn định: test có lỗi mạng chạy 3 lần | `TestSend_.*`, `TestHealthCheck.*`, `TestContract` | `go test -race -count=3 -run '^(TestSend_.*\|TestHealthCheck.*\|TestContract)$' ./internal/adapters/facebook/` | `ok … 1.752s` |
| Lint, mã lặp, kiến trúc | — | `golangci-lint run ./internal/adapters/facebook/...`; `make jscpd`; `go test -count=1 ./tests/arch/` | `0 issues.`; `Found 0 clones.`; `ok elimaz.com.vn/crm/tests/arch 166.448s` |
| Cổng kiểm của task (gen-check, build, vet, lint, jscpd, gitleaks, oasdiff, test-guard, kiến trúc; test toàn bộ trừ e2e vì so với điểm tách nhánh trên `main` có testkit của Task 146) | ci-task | `scripts/agent/ci-task.sh` | `ok elimaz.com.vn/crm/internal/adapters/facebook 11.642s`, `ok elimaz.com.vn/crm/cmd/crm 3.796s`, `CI-TASK XANH (so với điểm tách nhánh aafa087…)` |

## Test bị xóa hoặc tắt

(không có)

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 148 không có test e2e)

## Khác kế hoạch

- Quyết định 1–6 ở trên.
- Phần `Connector` (đổi mã, liệt kê Page, token Page, đăng ký/hủy webhook) nằm ở tệp riêng `connect.go` thay vì
  `fetch.go` (tệp quá dài); test ở `fetch_test.go`.
- `Via = history`/`backfill` không nằm trong sự kiện adapter trả (`webhooks.InboundEvent` không có trường `Via`): bên
  gọi ghi bằng `webhooks.EnqueueInbound(…, via)`; test kiểm khóa chống trùng giống webhook thay cho `Via`.
- Thêm ngoài kế hoạch: `Download` của `Channel` (hiện thực `channel.AttachmentDownloader` bằng `Client.Download` có
  sẵn), giới hạn chữ 2 000 ký tự và payload nút 1–1 000 ký tự (giới hạn của Meta), hằng `SubscribedFields()`,
  `RequiredPermissions()`, lỗi `ErrNoToken`, `Options.OAuthRedirectURL`.
- `export_test.go` thêm `NewTransportErrorForTest` (ca lỗi mạng cho contract (7)). Test dùng `refuseNext` (đóng cả kết
  nối đang giữ của client) để ca "chắc chắn chưa gửi" không phụ thuộc kết nối cũ.
- Gộp `m4` trước review: không có commit mới (`Already up to date.`).

## Việc phát hiện thêm

1. **Nối adapter Facebook vào `channel.Registry`** khi Task 153 có handler `inbound.facebook`: `wiring.go`
   `Adapter` đổi `return nil` → `return fromConfig(cfg)`; sửa `TestSource` (dòng "Adapter vẫn nil") và
   `cmd/crm/wire_m4_test.go` ca Meta bật (thêm khóa `facebook`, cần `AppSecret` trong cấu hình thử).
2. Echo của tin nhắn riêng có thể mang `recipient.comment_id` thay `recipient.id` (ghi chú 147): chưa xử lý (Quyết
   định 5); cần payload thật E3/E8 (Task 181) rồi Task 157 thêm trường vào `channel.MessagePayload`.
3. fakegraph chưa phục vụ `oauth/access_token` với `grant_type=fb_exchange_token`, `feed` kèm comment, `limit`/`since`
   của Conversations API: Task 149, 150 cần thì thêm vào `internal/testkit/fakegraph` (test 148 dùng máy giả nhỏ tại
   chỗ để khỏi sửa testkit).
4. `ListPages` đọc tối đa 10 trang × 100 Page; `Backfill` tối đa 10 trang × 25 hội thoại và 25 bài gần nhất (comment
   trên bài cũ hơn không đọc bù), mỗi hội thoại 20 tin và mỗi bài 100 comment, không đọc trang tiếp (hội thoại có hơn 20
   tin mới kể từ `since` sẽ thiếu tin) — đối chiếu khối lượng thật ở E3/E8, Task 150.
5. (Reviewer) Echo đọc qua lịch sử/hỏi bù có cùng khóa `m:<mid>` với echo của webhook nhưng không có `Metadata`
   (ClientRef), `EchoAppID`; nếu hỏi bù vào trước webhook thì bản đầy đủ bị bỏ vì trùng khóa. Task 150/157 khớp echo với
   lần gửi theo `mid` (Send trả `ExternalID` = `message_id`), không chỉ theo `ClientRef`.
6. (Reviewer) `TestBackfill` kiểm khoảng giãn bằng giờ thật (chỉ cận dưới ≥ 1 s, không chập chờn); có thể đổi sang đồng
   hồ tiêm vào nếu cần theo Task 400.

## Reviewer

Kết luận: ĐẠT (lần 1).

- Không có phát hiện phải sửa. Reviewer kiểm riêng: phân loại kết quả gửi (chỉ `not_sent` khi chưa ghi trọn yêu cầu hay
  Meta giới hạn; mọi ca không chắc → `unknown`, kể cả thiếu `message_id`, ẩn/hiện `success=false`); mốc cửa sổ loại trừ
  (đúng 24 giờ/7 ngày là quá); Lookup trả lời comment không bao giờ `not_sent`; `Detail` là câu cố định, token, secret
  không vào lỗi/log; Quyết định 1 (giữ `Adapter` nil) hợp lệ, đảo ngược một dòng.
- Ảnh hưởng khớp; ghi chú: thêm `ErrNoToken` vào mục Ảnh hưởng → đã thêm. Bảng bằng chứng: mọi dòng trỏ test có thật,
  kiểm đúng tiêu chí; không test nào bị xóa, tắt, nới.
- Ghi chú không bắt buộc (echo đọc lại thiếu ClientRef, giới hạn trang của Backfill, `TestBackfill` dùng giờ thật) →
  ghi ở Việc phát hiện thêm 4–6.
