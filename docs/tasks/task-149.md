# Task 149 — Kết nối Page (đăng nhập Facebook trong tab, mã system user), ngắt kết nối

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 149 = M4-09). Loại việc (quy trình mục 3): A.
Đọc: Interfaces của Task 143 (hợp đồng bốn operation đã có), 145 (`channel` tài khoản, token, Writer), 146, 147; spec 02
mục 2, 9; 04 mục 4, 5.3; ADR-46 nhóm 1, 3, 13; `docs/tasks/task-143.md`, `task-145.md`. Nhánh gốc `m4`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (grep/LSP): chỉ **thêm**, không đổi chữ ký có sẵn.
  - Mới trong `channel`: `StartOAuth`, `AuthorizeURL`, `CheckOAuthState`, `SaveOAuthPages`, `OAuthUserToken`,
    `ExchangeCode`, `FetchPageToken`, `CanConnect`, `ConnectInput`, `Writer.Connect`, `Writer.Disconnect`, `WebhookFields`,
    hằng `SubscribePageKind`, `UnsubscribePageKind`, `SubscribeAlertKind`, `NewSubscribeAlertCommand`; sentinel
    `ErrOAuthStateInvalid`, `ErrPageNotAccessible`, `ErrPageConnectedElsewhere`, `ErrConnectUnavailable`,
    `ErrNoConnector`. `subscribeParts(d)` (tệp khung) trả hai lệnh thay `parts{}` → `channel.CommandHandlers` có 2
    phần tử (cmd/crm `checkM4Unique`, `TestWireM4_*` dùng).
  - `alert.go`: `sendModeAlert` thành một trường hợp của kiểu lệnh cảnh báo chung theo Page (tách cấu trúc, cùng
    hành vi; `NewSendModeAlertCommand` giữ chữ ký, 1 chỗ gọi ở `cmd/crm/main.go`).
  - `channeltest`: thêm `Connector` giả (adapter giả có `Connector`, khóa tùy chọn) cho test của `channel` và `httpapi`.
  - `httpapi.Deps` thêm `Channels *channel.Registry`, `ChannelKeys *fieldcrypt.Keyring` (nil → ba route kết nối trả 503
    `retry_later`); `cmd/crm` nối từ `m4Parts` (thêm trường `Keys` vào `m4Parts`).
- Gói phụ thuộc ngược (`go list`): `internal/modules/channel` ← `cmd/crm`, `internal/adapters/facebook`,
  `internal/adapters/fake`, `internal/httpapi`, `internal/modules/conversation` (+ `internal/deps`), `channeltest`.
- Operation OpenAPI bị chạm, oasdiff: hiện thực `startFacebookOAuth`, `exchangeFacebookOAuth`, `connectFacebookPage`,
  `disconnectChannel` (hợp đồng đã có từ Task 143, **không sửa `openapi.yaml`**, oasdiff không đổi); xóa 4 dòng khỏi
  `tests/arch/testdata/m4_pending.txt`. Mã lỗi đều đã khai ở `x-error-codes`: `oauth_state_invalid`,
  `page_not_accessible`, `page_connected_elsewhere`, `retry_later`, `invalid_input`, `not_found`.
- Chỗ dùng trên màn hình: chưa có màn M4 (Task 178); `console/src/api/**` không đổi.
- Câu sqlc, bảng, pha migration: tệp mới `queries/channel/oauth.sql` (chèn, đọc theo băm, đánh dấu dùng, đọc phiên Page,
  dọn phiên hết hạn trên `channel.oauth_sessions`); thêm vào `queries/channel/accounts.sql`: `UpsertAccount`
  (`INSERT … ON CONFLICT (tenant_id, channel, external_id) DO UPDATE` tên, trạng thái), `SetSubscribedFields`.
  **Không có migration** (bảng, quyền cột đã có ở 0401; xem Quyết định 1). Gọi `tenancy.PutRoute` (hàm SQL
  `platform.put_tenant_route`), `catalog.DefaultSeller` (ở handler).
- Test sẽ chạy: `go test -race` gói `internal/modules/channel` (cả gói, nhỏ), `-run` các test mới/sửa của
  `internal/httpapi` (`TestChannelConnect*`, `TestM4Contract_StatusCodes`, `TestDomainErrorSnapshot*`), `cmd/crm`
  (`TestWireM4*`), `tests/arch`; cả bộ theo phạm vi ở ci-task.

## Tái dùng

- Đã kiểm trong `docs/danh-muc-dung-chung.md` và tìm theo tên, theo việc: `tenancy.PutRoute` (tuyến Page),
  `fieldcrypt.Keyring.Seal/Open` (token người dùng trong phiên), `channel.StoreCredentials`, `CredentialsFor`,
  `ClearCredentials`, `MarkStatus`, `Writer.signal` (Task 145), `commands.CommandKind`, `commands.Looker`, `DoneHook`,
  `queue.TerminalHook`, `alertjobs.Notify`, `identity.RecordAudit`, `catalog.DefaultSeller`, `refusableWrite`,
  `inventoryRead`, `idempotency` (middleware), `logging.Secret`, `testkit.(*Staff).ChannelAccount`, `DrainJobs`.
- Dùng lại / viết mới vì: dùng lại mọi thứ trên. Viết mới: câu SQL của phiên đăng nhập (chưa có), lệnh đăng ký/hủy
  đăng ký (tệp khung của task này). Lệnh cảnh báo đăng ký webhook dùng chung khung với lệnh cảnh báo chế độ gửi
  (tách phần chung trong `alert.go` thay vì chép, tránh jscpd). Không có hàm sinh chuỗi ngẫu nhiên base64url dùng chung
  trong danh mục: `state` cần đúng 32 byte →
  `rand.Read` + `base64.RawURLEncoding` (3 dòng, thư viện chuẩn).

## Soát thiết kế

- 5.1 Chủ dữ liệu: `channel.oauth_sessions`, `channel.channel_accounts` thuộc `channel`; tuyến `platform.tenant_routes`
  ghi qua `tenancy.PutRoute` (hàm `SECURITY DEFINER`, `crm_app` không `INSERT` thẳng).
- 5.2 Không ghi bảng module khác: audit qua `identity.RecordAudit`, lệnh qua `queue`; người bán đọc bằng
  `catalog.DefaultSeller` (public API) ở handler, truyền vào `ConnectInput`.
- 5.3 Đọc module khác qua view/API: chỉ `catalog.DefaultSeller`.
- 5.4 Nguyên tử: tuyến + tài khoản + token + audit + lệnh đăng ký trong một `WriteTx`; ngắt kết nối: trạng thái +
  audit + lệnh hủy trong một tx.
- 5.5 Không gọi mạng trong tx: đổi mã, liệt kê Page, lấy token Page gọi **ngoài** transaction (trước `WriteTx`); đăng
  ký/hủy webhook là lệnh `commands` ghi trong tx, worker gọi sau commit.
- 5.6 Gọi ra ngoài: `command.fb.subscribe_page` / `unsubscribe_page` mode command, `in_doubt` khi không rõ; `Lookup`
  đọc lại trạng thái đăng ký bằng `HealthCheck` của adapter (POST/DELETE `subscribed_apps` vốn lũy đẳng nên tra thấy
  "chưa" thì chạy lại an toàn). Không chắc đã gửi → không tự gửi lại ngoài đường `Lookup`.
- 5.7 Tenant: mọi câu trong `tenancy.WithTenant`/`WriteTx`; Execute đọc tài khoản trong tx tenant của lệnh.
- 5.8 Module không biết HTTP: `channel` chỉ dựng chuỗi URL đăng nhập bằng `net/url` (không `net/http`).
- 5.9 Đường ghi: bốn operation có `IdempotencyKey`, `x-permission: channel.manage` (Task 143).
- 5.10 Valkey: không dùng.
- 5.11 Thư viện: không thêm.
- 5.12 Module mẫu: `Writer` như `SetSendMode`; lệnh như `fulfillment` `carrier.check`; lỗi sentinel trong `channel`,
  đổi sang HTTP ở `errors_channel.go`.
- 6.1 Đồng thời: hai `exchange` cùng `state` → `UPDATE … SET used_at WHERE used_at IS NULL AND expires_at > now`
  (0 dòng → `ErrOAuthStateInvalid`), đúng một thắng. Hai `connect` cùng Page → `ON CONFLICT DO UPDATE` khóa dòng, cả hai
  thành công tuần tự (lần sau là `reconnected`). Ngắt kết nối khóa dòng (`LockAccount`) rồi mới đổi.
  Đua giữa lệnh hủy đăng ký và kết nối lại: Execute của `unsubscribe` bỏ qua (done, không gọi Meta) khi tài khoản
  không còn `disconnected`; `OnDone`/`OnTerminal` khóa dòng và chỉ xóa token khi vẫn `disconnected` → token mới của
  lần kết nối lại không bị xóa. `subscribe` chạy khi tài khoản đã `disconnected` → bỏ qua; `OnTerminal` chỉ đánh
  `degraded` khi tài khoản đang `active`/`degraded`. Còn một khe: DELETE của lệnh hủy đã gửi đi đúng lúc người dùng
  nối lại → Page mất đăng ký dù `active`; lịch sức khỏe (Task 150) thấy thiếu trường và tự đăng ký lại (ghi ở Việc
  phát hiện thêm, không chặn).
- 6.2 Gửi lại: middleware chống trùng; `exchange` lần hai cùng `state` khác khóa → 400. Kết nối lại cùng Page lũy đẳng.
- 6.3 Thứ tự khóa (ADR-46): `platform.tenant_routes` (nhóm 1, `PutRoute`) → `channel.channel_accounts` (nhóm 3) →
  `channel.oauth_sessions` (nhóm 3, sau) → audit (chỉ chèn) → `platform.jobs` (nhóm 13). `exchange` chỉ chạm
  `oauth_sessions` + jobs/idempotency. Hook của lệnh: `channel_accounts` rồi `platform.jobs` (runner đổi jobs sau hook).
- 6.4 Lỗi tạm: `WriteTx`/`RetryTx` chạy lại; mọi biến kết quả gán lại trong thân hàm. Lỗi mạng của Graph →
  `ErrConnectUnavailable` → 503 `retry_later`, không lưu gì (không vào `WriteTx`).
- 6.5 Cách ly: mặc định + câu có điều kiện, khóa dòng.
- 6.6 Việc nền lỗi: lệnh tự thử lại khi `not_sent` (`AutoRetryNotSent`), hạn 1 giờ, mỗi lần 30 s; `dead`/`expired`
  của `subscribe` → `degraded` + cảnh báo gấp `channel-subscribe:<id>`; của `unsubscribe` → vẫn xóa token.
- 6.7 429: `rate_limited` → `not_sent` (runner dùng lịch thử lại; adapter có `RetryAfter` sẽ được 148 trả qua lỗi —
  ở đây phân lớp bằng `ClassifyError`).
- 6.8 Tiến trình chết: lệnh `in_doubt` (giữ token để `Lookup` dùng được), tra lại bằng `Lookup`.
- 6.9 Treo: mỗi lần gọi ≤ 30 s (Timeout của Kind); gọi Graph ở handler theo ctx của yêu cầu và hạn 10 s của client.
- 6.10 Tiền: không áp dụng (không có tiền).
- 6.11 Quy luật: "token chỉ bị xóa khi tài khoản vẫn đã ngắt", "một `state` chỉ đổi được một lần" — test song song và
  test đua kết nối lại; không dùng rapid (không có miền đầu vào để sinh ngẫu nhiên có nghĩa; vùng `channel` không
  thuộc vùng rủi ro cao).
- 6.12 Dữ liệu cá nhân/bí mật: `state` chỉ lưu băm SHA-256; token người dùng, token Page mã hóa (AAD theo bảng, cột,
  dòng); mã system user chỉ sống trong bộ nhớ của yêu cầu; không token trong payload lệnh, audit, log, cảnh báo, câu
  trả lời (test dò `EAAB`, `access_token`). Kiểu `logging.Secret` tự che.
- 6.13 Quyền: `channel.manage` ở guard; phiên đăng nhập gắn `user_id`, người khác dùng `state`/`sessionId` → 400.

## Quyết định thay chủ shop

1. **Không thêm migration; `exchange` ghi một dòng phiên mới** thay vì cập nhật dòng phiên `start`. Migration 0401
   (Task 145) chỉ cấp `UPDATE (used_at, pages)` trên `oauth_sessions`, không cấp `user_token_enc`, `expires_at`; dải
   0401–0411 đã chia hết cho task khác. Cách làm: `UPDATE … SET used_at WHERE used_at IS NULL` trên dòng `start` (đúng
   kế hoạch, 0 dòng → 400), rồi `INSERT` dòng phiên Page (token người dùng mã hóa, danh sách Page, hạn 30 phút,
   `state_hash` là băm của 32 byte ngẫu nhiên không lộ ra ngoài); `sessionId` trả về là id dòng mới. Dòng `start` có
   `user_token_enc` NULL, dòng phiên Page có token: câu đọc phân biệt hai loại. Phương án khác: migration 0412 cấp
   thêm quyền cột (ngoài dải đã chia). Đảo ngược: dễ (đổi câu SQL).
2. **Adapter Facebook chưa có `Connector`** (thuộc `fetch.go` của Task 148, đang chạy song song; không được sửa tệp
   của 148): task này viết toàn bộ luồng trên interface `channel.Connector` (lấy từ `channel.Registry` theo khóa
   `facebook`) và test bằng adapter giả có `Connector` trong `channeltest` (ghi nhận trường đăng ký, lần hủy, token đã
   dùng, lỗi theo lớp). Ca "fakegraph ghi nhận 11 trường, một lần `DELETE subscribed_apps`" của kế hoạch chạy được khi
   148 gộp (ghi ở Việc phát hiện thêm). Khi chưa có adapter Facebook hay chưa có `FIELD_ENC_KEYS`, ba route kết nối trả 503
   `retry_later` (tính năng chưa bật ở tiến trình này), không 500; không dùng 501 vì `TestM3Routes_PendingList` coi
   501 là "operation chưa làm". Phương án khác: 501 kèm dòng giữ trong `m4_pending.txt` (sai nghĩa danh sách chờ).
3. **Lỗi của Graph khi kết nối**: phân lớp bằng `ClassifyError` của adapter. `exchange`: lớp `auth`, `policy`,
   `permanent` → 400 `oauth_state_invalid` (mã đăng nhập sai/hết hạn: người dùng bắt đầu lại); lớp khác → 503
   `retry_later`, không lưu. `connect`: `auth`/`policy`/`permanent` hay `ErrPageNotAccessible` của adapter (Page không
   thuộc tài sản của token) → 422 `page_not_accessible`; lớp khác → 503. Đường phiên đăng nhập còn kiểm `pageId` phải
   có trong danh sách Page của phiên.
4. **Sau `exchange`, phiên Page dùng được nhiều lần trong 30 phút** (cùng người dùng) để nối nhiều Page từ một lần
   đăng nhập; kế hoạch không nói phải dùng một lần. `start` dọn luôn phiên đã hết hạn của shop (token người dùng mã hóa
   không nằm lại lâu hơn cần).
5. **Cảnh báo `channel-subscribe:<id>` là lệnh `channel.subscribe_alert`** (outbox như cảnh báo chế độ gửi của Task
   145), đăng ký ở danh sách lệnh cảnh báo trong `cmd/crm/main.go` (chỉ ở đó có Alerter); văn bản có tên Page, mã tài
   khoản, không token. Mức `urgent` như kế hoạch.
6. **Ngắt kết nối Page đã `disconnected`** → trả tài khoản, không audit, không ghi lệnh mới (lũy đẳng). Kết nối lại xóa
   `status_reason`. Kết nối (mới hay lại) không tăng `version` (luật Task 145: version chỉ cho cài đặt nhân viên).
7. **Lệnh `unsubscribe` khi token đã hỏng** (`auth`) → `rejected` (Meta không cho dùng token nữa thì cũng không còn gửi
   được gì) → `dead` → vẫn xóa token. `in_doubt` giữ token để `Lookup` dùng; chỉ `done`, `dead`, `expired` xóa.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `start`: thiếu `channel.manage` → 403; có → `authorizeUrl` có đúng `client_id`, `config_id`, `redirect_uri`, `/v26.0/dialog/oauth`, `state` 43 ký tự base64url; bảng chỉ lưu băm SHA-256 (không dòng nào chứa state thô); chưa bật kết nối → 503 `retry_later` | `TestChannelConnect_Start` (httpapi), `TestOAuth_StartAndAuthorizeURL` (channel) | `go test -race -count=1 -run '^(TestChannelConnect_Start\|TestChannelConnect_Exchange\|TestChannelConnect_ExchangeConcurrent\|TestChannelConnect_Page\|TestChannelConnect_Disconnect\|TestM4Contract_StatusCodes\|TestM3Routes_PendingList\|TestDomainErrorSnapshot\|TestDomainErrorSnapshot_TableMembership\|TestDomainErrorSnapshot_Order\|TestChannelAPI_ListByScope\|TestChannelAPI_SetSendMode\|TestChannelAPI_SetHumanAgent)$' ./internal/httpapi/`; `go test -race -count=1 ./internal/modules/channel/...` | đỏ trước (channel: `build failed`, `undefined: channel.ErrPageNotAccessible`, `channel.StartOAuth`…); sau `ok …/httpapi 5.804s`, `ok …/modules/channel 3.507s` |
| `exchange`: state sai, của người khác, sau 16 phút, dùng lần hai, mã Meta từ chối → 400 `oauth_state_invalid`; Meta lỗi tạm → 503 `retry_later` không lưu (state vẫn dùng được sau đó); đúng → `sessionId` + 2 Page; body không có `EAAB`, `access_token` | `TestChannelConnect_Exchange`; `TestOAuth_StateAndPagesSession` (thêm: phiên Page hạn 30 phút, token người dùng mã hóa không chứa token thô, người khác/quá hạn/phiên start → lỗi, Page không có trong phiên → `ErrPageNotAccessible`, `start` sau dọn phiên hết hạn) | như trên | `PASS` |
| Hai `exchange` song song cùng `state`, khác khóa chống trùng → đúng một 200, một 400 | `TestChannelConnect_ExchangeConcurrent`, `TestOAuth_ConcurrentSave` (module: đúng một `nil`, một `ErrOAuthStateInvalid`) | như trên (`-race`) | `PASS` |
| Nối Page 1 → `read_only`, `active`, `token_kind page_user`, `credential_enc` không chứa token, tuyến `fb_page:1001` của shop, một audit `channel.connected` không chứa `EAAB`, một lệnh `command.fb.subscribe_page` (payload không token), một báo hiệu; worker chạy → kênh nhận đúng 11 trường bằng token Page, `subscribed_fields` 11 phần tử, lệnh `done` | `TestConnect_NewPage` (adapter giả có Connector, Quyết định 2), `TestChannelConnect_Page` (HTTP: body không token, 1 lệnh) | như trên | `PASS` |
| Page đã nối với shop B → `ErrPageConnectedElsewhere` / 409 `page_connected_elsewhere`, shop B không có tài khoản, việc nào; tuyến vẫn của A; `crm_app` `INSERT` thẳng `platform.tenant_routes` → `permission denied` | `TestConnect_PageConnectedElsewhere`, `TestChannelConnect_Page` | như trên | `PASS` |
| Nối lại Page `enabled`, `degraded` bằng token mới → token mới, `send_mode` vẫn `enabled`, `status active`, lý do xóa, version không đổi, audit `channel.reconnected`, thêm lệnh đăng ký | `TestConnect_Reconnect` | như trên | `PASS` |
| System user → `token_kind = system_user`, `read_only`; Page không thuộc tài sản system user → 422 `page_not_accessible`; Page thiếu MODERATE, Page không có trong phiên → 422; thiếu/thừa nguồn token → 422 `invalid_input`; người khác dùng phiên → 400; Meta lỗi tạm → 503 | `TestChannelConnect_Page`, `TestConnector_Classify` (bảng lớp lỗi → sentinel, cả lỗi bọc) | như trên | `PASS` |
| Ngắt kết nối → `disconnected`, token còn tới khi lệnh hủy chạy; lệnh chạy → kênh nhận đúng một lần hủy bằng token cũ, rồi `credential_enc` rỗng (`ErrNoCredentials`), tuyến còn; ngắt lần hai không thêm lệnh, audit; HTTP: sale 403, owner 200 `disconnected`, Page lạ 404 | `TestDisconnect`, `TestChannelConnect_Disconnect` | như trên | `PASS` |
| Đua: ngắt rồi nối lại trước khi lệnh hủy chạy → lệnh hủy không gọi kênh, token mới giữ nguyên; token đã thu hồi khi hủy → lệnh `dead` nhưng token vẫn bị xóa | `TestDisconnect_ReconnectBeforeUnsubscribe`, `TestDisconnect_RevokedTokenStillCleared` | như trên | `PASS` |
| `subscribe_page` gặp lỗi token (190, lớp `auth`) → `dead`, `degraded` có lý do, đúng một lệnh cảnh báo (payload không token); chạy lệnh cảnh báo → key `channel-subscribe:<id>`, mức `urgent`, văn bản có tên Page, không token; lỗi chưa gửi → tự thử lại rồi xong | `TestSubscribe_AuthDeadAlerts` | như trên | `PASS` |
| `Lookup` (tra lệnh `in_doubt`): subscribe đủ trường → done, thiếu → not_sent, tra lỗi → unknown; unsubscribe: tài khoản đã nối lại → done, đã ngắt còn đăng ký → not_sent, hết → done, không còn token → done | `TestSubscribe_Lookup` | như trên | `PASS` (ca "không còn token" thêm khi tự soát: trước sửa trả `unknown`) |
| Lệnh M4: channel có đúng 2 lệnh; `wireM4` gom đúng 2 lệnh, chưa có phần nền khác | `TestWiringCommands`, `TestWireM4_Frame` | `go test -count=1 ./cmd/crm/` | `ok …/cmd/crm 9.514s` |
| Bốn operation không còn 501; operation M4 khác vẫn đúng 401/403/501; lỗi channel đổi sang HTTP ở một chỗ | `TestM4Contract_StatusCodes`, `TestM3Routes_PendingList`, `TestDomainErrorSnapshot`, `…_TableMembership`, `…_Order` | lệnh httpapi ở trên | `PASS` (lần đầu `TestM3Routes_PendingList` đỏ: 501 khi chưa bật kết nối → đổi sang 503, Quyết định 2) |
| Ranh giới module, luật transaction, SQL, ADR-46 | `tests/arch` | `go test -count=1 ./tests/arch/` | `ok …/tests/arch 246.805s` |
| Cổng tĩnh + test theo phạm vi | — | ci-task của task (sau khi stage) | lần 1 bị dừng ở giới hạn 10 phút của lệnh nền (lỗi của tôi khi gọi, không phải lỗi mã); lần 2: `CI-TASK XANH (so với điểm tách nhánh aafa0878…)`, gồm `ok …/modules/channel 2.971s`, `ok …/httpapi 30.248s`, `ok …/cmd/crm 3.759s`, `ok …/tests/arch 19.943s` |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 149 không có test e2e)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Quyết định 1, 2 ở trên. `cmd/crm/wire_m4.go` thêm trường `Keys` vào `m4Parts` và `cmd/crm/main.go` nối
  `Channels`, `ChannelKeys` vào `httpapi.Deps` và lệnh cảnh báo mới (kế hoạch không ghi; chỉ ở đó có các phụ thuộc này).

## Việc phát hiện thêm

- Khi Task 148 gộp (adapter Facebook có `Connector`, `HealthCheck`): thêm test trên fakegraph cho đường kết nối thật
  (11 trường `subscribed_apps`, một `DELETE subscribed_apps` khi ngắt) — giao cho task sau 148 hoặc Task 150.
- Khe nhỏ: lệnh hủy đăng ký đã gửi DELETE đúng lúc người dùng nối lại Page → Page `active` nhưng mất đăng ký; lịch sức
  khỏe (Task 150) thấy thiếu trường và tự ghi `subscribe_page`. Khe ngược (reviewer): `subscribe` đã đọc tài khoản trước
  khi ngắt rồi POST sau khi lệnh hủy chạy → Page `disconnected`, không token nhưng còn đăng ký webhook; Task 150 nên tính
  cả trường hợp này (tin vẫn được lưu vì tuyến còn).
- Quy ước với Task 148 (reviewer): `Lookup` của lệnh hủy coi "đã hủy" khi `HealthCheck` báo thiếu đủ 11 trường (hay
  token hỏng); nếu `HealthCheck` thật trả `WebhookSubscribed=false` mà `MissingFields` rỗng thì Lookup trả `not_sent` và
  DELETE chạy lại (lũy đẳng, an toàn). Khi 148 gộp: đối chiếu và nếu cần đổi điều kiện thành `!WebhookSubscribed || …`.
- Worker không có adapter có `Connector` (kênh Facebook chưa bật) mà vẫn có lệnh hủy → `rejected` → xóa token, không gọi
  Meta hủy đăng ký (không mất tin vì tuyến còn).

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, lượt 1, trên phần đã stage sau ci-task xanh). Phát hiện phải sửa: không có. Reviewer đã
kiểm riêng: quyền cột của 0401 đủ cho `UpsertAccount`, `UseOAuthStart`, `DeleteExpiredOAuthSessions` (Quyết định 1 đứng
được); Page của shop khác lùi transaction, không lưu khóa chống trùng; một `state` chỉ đổi một lần (UPDATE có điều kiện,
một đồng hồ); đua ngắt/nối lại khóa dòng và kiểm lại trạng thái; `load` gán lại biến khi `RetryTx` chạy lại; không lộ
token (payload, audit, khóa chống trùng chỉ lưu băm thân); module không nhập `net/http`. Ghi chú không bắt buộc: thêm
`CanConnect` vào Ảnh hưởng (đã thêm); điều kiện `Lookup` của lệnh hủy, worker thiếu Connector, khe đua ngược — đã ghi ở
Việc phát hiện thêm; `systemUserToken` rỗng — hợp đồng đã chặn (`minLength: 1`, ogen trả 400 trước handler), không sửa.

## Lượt sửa gộp m4

`git merge --no-commit m4` (m4 tại 9820e65, đã có Task 148 và Task 169).

**Xung đột và cách giải (giữ đủ hai phía):**
- `cmd/crm/wire_m4_test.go`: 149 kiểm lệnh M4 = [đăng ký, hủy đăng ký Page]; 169 kiểm lịch `conversation.reminders` + 1 handler việc nền. Gộp: Inbound + Background phải rỗng, lệnh đúng hai Kind của 149, lịch và việc nền đúng như 169; chú thích gộp cả hai.
- `internal/httpapi/errors_snapshot_test.go`: giữ cả 7 dòng lỗi channel (149) và 6 dòng lỗi conversation (169).

**Mã trùng (jscpd):** `facebook.SubscribedFields()` (148, `internal/adapters/facebook/health.go`) trùng 6 dòng với `channel.WebhookFields()` (149, `internal/modules/channel/connect.go`), cùng danh sách 11 trường webhook. Module `channel` không được nhập adapter, adapter đã nhập `channel`, nên giữ một nguồn là `channel.WebhookFields()`: xóa `facebook.SubscribedFields`, đổi 2 chỗ dùng trong adapter (`health.go` HealthCheck, `connect.go` Subscribe) và 4 chỗ trong `health_test.go`. Không đổi hành vi (hai danh sách giống hệt, cùng thứ tự).

**Bằng chứng:**
| Kiểm | Lệnh | Kết quả |
|---|---|---|
| Build | `go build ./...` | qua |
| Vet | `go vet ./cmd/crm/ ./internal/httpapi/ ./internal/modules/channel/... ./internal/adapters/facebook/...` | qua |
| Lint | `golangci-lint run` 4 thư mục trên | `0 issues.` |
| Mã trùng | `make jscpd` | trước sửa `Found 1 clones (1 new)`; sau sửa `Found 0 clones.` |
| Danh mục | `make catalog-check` | không chênh |
| Test | `go test -race -count=1 ./cmd/crm/ ./internal/httpapi/ ./internal/modules/channel/... ./internal/adapters/facebook/...` | `ok` cả 4 gói (cmd/crm 18.2s, httpapi 41.1s, channel 5.0s, facebook 15.3s) |
| Kiến trúc | `go test -count=1 ./tests/arch/` | `ok` 108.0s |

**Reviewer (lượt gộp):** ĐẠT, không có phát hiện phải sửa. Giải xung đột giữ đủ hai phía (wire_m4_test: lệnh của 149 + lịch, việc nền của 169; errors_snapshot: 7 dòng channel + 6 dòng conversation); hai danh sách 11 trường giống hệt cùng thứ tự nên thay không đổi hành vi; không còn chỗ dùng `facebook.SubscribedFields` (chỉ còn trong lịch sử `docs/tasks/task-148.md`).
