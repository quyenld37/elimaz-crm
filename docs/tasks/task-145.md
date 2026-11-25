# Task 145 — Module channel: tài khoản Page, token, chỉ xem, giới hạn gửi, adapter giả

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 145, M4-05). Loại việc (quy trình mục 3): A

Căn cứ: Interfaces và Step 1 của Task 145; Interfaces của Task 143 (kiểu công khai `channel`, `Registry`, khung
`fake`) và `docs/tasks/task-143.md` (Quyết định 1: `channel.manage` toàn shop; 6: khóa kênh giả `fakechat`); spec 04
mục 3, 4, 5.3, 11; ADR-29 (người bán), ADR-41, ADR-46 nhóm 3; Global Constraints M4 (báo hiệu realtime, không gọi
mạng trong transaction). Nhánh gốc `m4`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences / grep):
  - `channel.ChannelAccount` **thêm** trường `StatusReason`, `TokenKind`, `SubscribedFields`, `TokenCheckedAt`,
    `LastWebhookAt`, `LastFeedAt` (đã có 0 chỗ dựng giá trị `ChannelAccount{…}` ngoài gói: không gãy chỗ nào).
  - Mới trong `channel`: `Account`, `AccountByExternal`, `ListAccounts`, `StoreCredentials`, `CredentialsFor`,
    `ClearCredentials`, `NoteInbound`, `MarkStatus`, `NoteBlockedUntil`, `AcquireSendToken`, kiểu `Writer`
    (`SetSendMode`, `SetHumanAgent`), sentinel `ErrAccountNotFound`, `ErrVersionConflict`, `ErrInvalidSendMode`,
    `ErrInvalidTokenKind`, `ErrNoCredentials`, `ErrNoKeyring`, hằng `SendModeReadOnly`, `SendModeEnabled`,
    `TopicStatus`, `SendModeAlertKind`, `NewSendModeAlertCommand`; gói `channel/channeltest` (`Harness`,
    `RunContract`, `ExpectedEvent`, `SeedAccount`).
  - `conversation.TopicChannelStatus` đổi thành bí danh của `channel.TopicStatus` (cùng giá trị `channel.status`;
    0 chỗ dùng ngoài khai báo).
  - `fake.Adapter(cfg)` trả adapter thật ngoài production (trước luôn nil); mới `fake.New`, `fake.Sign`,
    `fake.SignatureHeader`, `fake.RouteKind`.
  - `config.Config` thêm `FakeChatWebhookSecret` (đọc `FAKECHAT_WEBHOOK_SECRET`, cấm ở production).
  - Mới ở platform: `alertjobs.Notify`, `alertjobs.ReasonAlerterMissing` (gửi cảnh báo từ Execute của lệnh cảnh báo;
    jscpd báo đoạn trùng với `fulfillment.NewAuthAlertCommand` nên tách phần dùng chung, xem Tái dùng).
  - Mới ở testkit: `(*Staff).ChannelAccount` (chèn Page thử, người bán own) cho test của mọi task M4.
  - Sinh lại `models.go` của mọi khối sqlc (sqlc đưa ba bảng mới vào models của mọi khối; không đổi kiểu có sẵn).
- Gói phụ thuộc ngược (go list): `internal/modules/channel` ← `cmd/crm`, `internal/modules/conversation` (+ `internal/deps`),
  `internal/adapters/facebook`, `internal/adapters/fake`, và mới `internal/httpapi`; `internal/adapters/fake` ← chỉ
  `cmd/crm`; `internal/platform/config` ← 10 gói (chỉ thêm trường).
- Operation OpenAPI bị chạm, oasdiff: hiện thực `listChannelAccounts`, `setChannelSendMode`, `setChannelHumanAgent` (hợp
  đồng đã có từ Task 143, **không sửa `openapi.yaml`**: oasdiff không đổi); xóa ba dòng khỏi
  `tests/arch/testdata/m4_pending.txt`. Mã lỗi dùng: `not_found`, `version_conflict`, `invalid_input` (đã có).
- Chỗ dùng trên màn hình: chưa có màn M4 (Task 178); `console/src/api/**` không đổi vì hợp đồng không đổi.
- Câu sqlc, bảng, pha migration: migration mới `migrations/0401_channel.sql` (pha mở rộng: `CREATE SCHEMA channel`, ba
  bảng `channel_accounts`, `send_buckets`, `oauth_sessions`, RLS, view `channel.channel_accounts_v`, GRANT); khối sqlc
  mới `queries/channel` → `internal/modules/channel/internal/store` (`accounts.sql`, `buckets.sql`); `docs/views.md`
  thêm dòng view và dòng ngoại lệ luật (g) `channel.channel_accounts_external_key`; ADR-46 nhóm 3 đã có đủ ba bảng.
- Tệp khác: `internal/httpapi/{handlers_channel.go,errors_channel.go,errors.go (thêm channelError vào
  domainErrorMappers)}`, `errors_snapshot_test.go` (dòng mới); `cmd/crm/main.go` (một dòng: lệnh cảnh báo
  `channel.send_mode_alert` vào danh sách lệnh cảnh báo dùng chung Alerter), `cmd/crm/wire_m4_test.go` (kênh giả có
  ngoài production); `internal/platform/config/{meta.go,config.go,m4_test.go}`; `tests/arch/fakechat_test.go`.
- Test sẽ chạy: `go test -race -run` đúng test mới của `./internal/modules/channel/...`, `./internal/adapters/fake/`,
  `./internal/httpapi/` (`TestChannelAPI*`, `TestM4Contract_*`, `TestM3Routes_PendingList`, `TestDomainErrorSnapshot*`),
  `./internal/platform/config/` (`TestLoad_FakeChat*`), `./cmd/crm/` (`TestWireM4*`), `./tests/arch/` (`Boundaries`,
  `LockOrder`, `ViewsDoc`, `FakeChat`, `M4`), `./internal/platform/tenancy/` không cần (schema rules chạy trong test
  `channel`); cuối cùng `scripts/agent/ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`fieldcrypt.Keyring`
  `Seal`/`Open`/`AAD`, `logging.Secret`, `tenancy.WithTenant`, `tenancy.CheckSchemaRules`, `realtime.Bus`,
  `realtimetest.RecordingBus`, `commands.CommandKind`, `queue.NewQueue().Enqueue`, `identity.RecordAudit`,
  `identity.ScopesFor`, `testkit.OpenStaff`/`InTenant`/`Columns`); mã cùng việc ở module khác:
  `fulfillment/accounts.go` (khóa API hãng mã hóa bằng `fieldcrypt`, AAD theo dòng), `fulfillment/accounts_check.go`
  (`NewAuthAlertCommand`: cảnh báo qua lệnh ghi trong transaction), `catalog.Writer` (đường ghi có `RequestID`, audit,
  báo hiệu trong tx bên gọi), `httpapi.refusableWrite`, `inventoryRead`, `handlers_servicekeys.go` (báo hiệu trong
  `WriteTx`), adapter hãng giả `adapters/carrier/fake` (chữ ký `x-fake-signature`).
- Dùng lại / viết mới vì: dùng lại `fieldcrypt` (không tự mã hóa), `refusableWrite`, `inventoryRead` (đọc trong tenant),
  `matchError`, `RecordingBus`, `commands.CommandKind` + hàng đợi cho cảnh báo (outbox như M3), `serveAs`/`rolePerms`.
  Viết mới: hàm đọc/ghi tài khoản kênh (dữ liệu của `channel`, logic ở lại trong module theo CLAUDE.md); lệnh cảnh báo
  riêng của `channel` theo mẫu `fulfillment.NewAuthAlertCommand`, phần gửi và đổi kết quả (Alerter nil → rejected,
  lỗi → log + not_sent) tách thành `alertjobs.Notify` ở platform vì jscpd báo trùng (cùng mục đích, cùng lý do đổi:
  chính sách gửi cảnh báo từ lệnh); ba lệnh cảnh báo cũ của fulfillment chưa chuyển sang (ngoài phạm vi, ghi ở Việc
  phát hiện thêm); hàm
  ký/kiểm HMAC của kênh giả viết lại trong `adapters/fake` (luật (n): adapter nhắn tin không được nhập
  `adapters/carrier/fake`; hai bên là hai giả lập độc lập, khác khóa, khác lý do đổi). Bộ đếm giới hạn gửi viết bằng
  một câu `UPDATE` có điều kiện trên PostgreSQL theo 04 mục 5.3 (không dùng `platform/ratelimit` Valkey vì spec chốt
  giới hạn gửi theo Page nằm trong PostgreSQL, Valkey không quyết định nghiệp vụ, mục 5.10).

## Soát thiết kế

- 5.1 Chủ dữ liệu: tài khoản kênh, token, bucket gửi, phiên OAuth thuộc `channel` (04 mục 3), bảng trong schema
  `channel`.
- 5.2 Không ghi bảng module khác: chỉ ghi `channel.*`; audit qua `identity.RecordAudit` (public API, `channel` được
  gọi `identity`); `seller_id` là khóa ngoại tới `catalog.sellers` (ràng buộc, không đọc/ghi bảng catalog từ Go).
- 5.3 Đọc qua view: công bố `channel.channel_accounts_v` (không có `credential_enc`, `credential_kid`) cho mốc sau.
- 5.4 Nguyên tử: đổi chế độ gửi + audit + báo hiệu + lệnh cảnh báo trong một transaction (`WriteTx` của bên gọi).
- 5.5 Sau commit: cảnh báo "Page bắt đầu gửi thật" là lệnh `channel.send_mode_alert` ghi trong tx, worker gửi qua
  Alerter (không gọi Telegram trong tx).
- 5.6 Gọi ra ngoài: adapter giả không gọi mạng; `Send` của giả mô phỏng bốn kết quả (`sent/rejected/not_sent/unknown`).
- 5.7 Ngữ cảnh shop: mọi câu chạy trong `tenancy.WithTenant`/tx tenant; ba bảng có RLS (`enable_tenant_rls`),
  `UNIQUE (tenant_id, id)`, khóa ngoại ghép `tenant_id`; `CheckSchemaRules` = [] là test.
- 5.8 Không HTTP trong module: `channel` không nhập `net/http`; adapter giả nhập `net/http` (được, luật (n)).
- 5.9 Đường ghi: hai PUT có `IdempotencyKey`, `x-permission: channel.manage` (hợp đồng Task 143).
- 5.10 Valkey: không dùng; giới hạn gửi trong PostgreSQL (04 mục 5.3).
- 5.11 Không thêm thư viện.
- 5.12 Theo module mẫu: `Writer` có `RequestID` như `catalog.Writer`; sửa có `version` (khóa dòng `FOR UPDATE` rồi so);
  sentinel trong module, đổi sang HTTP ở `errors_channel.go`.
- 6.1 Đồng thời: đổi chế độ gửi khóa dòng rồi so `version`; `NoteInbound` một câu `UPDATE` có điều kiện (100
  goroutine, test); lấy token gửi một câu `UPDATE … WHERE LEAST(…) >= 1` (READ COMMITTED đánh giá lại điều kiện sau
  khi chờ khóa dòng: 20 goroutine trên 5 token → đúng 5, test).
- 6.2 Gửi lại: `Idempotency-Key` (test `Idempotent-Replayed`); lỗi nghiệp vụ (`version_conflict`, `not_found`) không lưu
  vào khóa chống trùng (`refusableWrite`).
- 6.3 Thứ tự khóa: ADR-46 nhóm 3 `channel_accounts` → `send_buckets` → `oauth_sessions`; `AcquireSendToken` chỉ đọc
  `channel_accounts` (không khóa) rồi mới ghi `send_buckets`; đổi chế độ gửi chỉ khóa một dòng `channel_accounts`,
  ghi `identity.audit_logs` (nhóm sau) và `platform.jobs` (nhóm 13, cuối).
- 6.4 Lỗi tạm: `WriteTx`/`RetryTx` có sẵn; hàm trong tx gán lại biến kết quả mỗi lần chạy.
- 6.5 Mức cách ly: READ COMMITTED + câu có điều kiện.
- 6.6–6.8 Lệnh cảnh báo: `AutoRetryNotSent`, Notify lỗi → `not_sent` tự gửi lại (Alerter chống lặp theo key); Alerter
  nil → `rejected alerter_missing`.
- 6.9 Treo: `TxAPI` cho đường HTTP; `AcquireSendToken` tx ngắn riêng.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: không thuộc vùng rủi ro cao; quy luật bucket (không bao giờ quá `capacity`, không cho quá số token)
  kiểm bằng test đồng thời và test đồng hồ.
- 6.12 Dữ liệu cá nhân, bí mật: token chỉ đi qua `logging.Secret`, lưu mã hóa AES-GCM AAD theo tenant|bảng|cột|dòng
  (chép sang dòng khác không mở được, test); view và API không có cột token (test body không có `credential`,
  `token`, `EAAB`); audit chỉ `send_mode`/`human_agent_enabled`; cảnh báo chỉ mã tài khoản.
- 6.13 Quyền: danh sách lọc theo `ScopesFor(conversation.read)` (Page A → 1 dòng); không grant → rỗng (không phải
  "tất cả"); ghi cần `channel.manage` (guard 403 trước handler, test).

## Quyết định thay chủ shop

1. **Kênh giả là `fakechat`** trong CHECK `channel IN ('facebook','fakechat')` (kế hoạch ghi `fake`; theo Quyết định 6
   của Task 143: `fake` đã là khóa hãng giả M3). Loại tuyến của Page giả `fakechat_page` (Facebook dùng `fb_page`).
2. **Bí mật webhook kênh giả: biến mới `FAKECHAT_WEBHOOK_SECRET`** (kế hoạch ghi `FAKE_WEBHOOK_SECRET`; đặt theo khóa
   `fakechat`, cùng kiểu `FAKECARRIER_WEBHOOK_SECRET`): tùy chọn ngoài production (≥ 32 ký tự), đặt ở production là lỗi
   khởi động. Kênh giả có ở mọi môi trường khác production kể cả khi chưa đặt bí mật (khi đó mọi webhook giả bị từ
   chối 401). Đảo ngược: đổi tên biến trong `meta.go`.
3. **Cảnh báo "Page bắt đầu gửi thật" là lệnh `channel.send_mode_alert` ghi trong transaction** (outbox như cảnh báo
   M3), worker gửi qua Alerter sau commit; đăng ký ở danh sách lệnh cảnh báo dùng chung Alerter trong
   `cmd/crm/main.go` (thêm một dòng; kế hoạch ghi main.go không đổi, nhưng chỉ ở đó có Alerter). Phương án khác: gọi
   Alerter trong handler (gọi mạng trong transaction, sai mục 5.5).
4. **`NoteInbound` giữ điều kiện riêng cho mốc feed**: câu của kế hoạch chỉ xét `last_webhook_at`, nên khi tin nhắn
   tới liên tục dưới 1 phút thì `last_feed_at` không bao giờ được ghi (Task 150 sẽ báo nhầm "feed im lặng"). Mỗi cột
   ghi tối đa mỗi phút một lần, mốc cũ không đè mốc mới. Đúng hai ca của kế hoạch (30 s không đổi, 61 s đổi).
5. **`version` chỉ tăng khi nhân viên đổi cài đặt** (`send_mode`, `human_agent_enabled`); trạng thái, token, mốc
   webhook, mốc chặn do hệ thống ghi không tăng version, để màn cài đặt không báo `version_conflict` giả mỗi khi có
   webhook. Đặt lại đúng giá trị hiện tại (version khớp) → không đổi, không audit, không báo hiệu.
6. **Unique `(tenant_id, channel, external_id)` miễn luật (g)** (`seller-exempt`, dòng ở `docs/views.md`): một Page chỉ
   nối một lần trong shop dù thuộc người bán nào (Task 149 dựa vào `ON CONFLICT` của chính unique này).
7. **`conversation.TopicChannelStatus` thành bí danh của `channel.TopicStatus`**: báo hiệu do `channel` phát (module
   sở hữu dữ liệu), `channel` không được nhập `conversation`; một giá trị, hai tên.
8. **`ListAccounts` phân biệt `nil` (mọi Page) và rỗng (không Page nào)**; handler đổi phạm vi rỗng thành rỗng để
   người không có Page nào không thấy gì (PQ04).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| MO03 nền: Page mới không chỉ định gì → `read_only`, `active`, version 1; `CheckSchemaRules` = []; tenant B không đọc được Page của A | `TestAccounts_DefaultsAndIsolation` | `go test -race -count=1 ./internal/modules/channel/` | đỏ trước (không biên dịch: `undefined: channel.Account`), sau `ok …/modules/channel 2.403s`; ci-task `ok …/modules/channel 2.608s` |
| `ListAccounts`: nil = mọi Page, danh sách = đúng các Page đó, rỗng = không Page nào | `TestAccounts_List` | như trên | `PASS` |
| Token mã hóa: cột `credential_enc` không chứa `EAAB-secret-77`; `CredentialsFor` trả đúng; bản mã chép sang tài khoản khác (chủ bảng) → `ErrDecrypt`; xóa → `ErrNoCredentials`; loại lạ, thiếu khóa, rỗng, không tài khoản → lỗi riêng | `TestCredentials_RoundTripAndBinding`, `TestCredentials_StoreRejects` | như trên | `PASS` |
| `NoteInbound`: cách 30 s không đổi, cách 61 s đổi, mốc cũ không đè; mốc feed ghi riêng (Quyết định 4); 100 goroutine dưới `-race` không lỗi, mốc là một giá trị đã gửi | `TestAccounts_NoteInbound` | như trên (`-race`) | `PASS` |
| `MarkStatus` chỉ báo đổi khi khác, không tăng version; `NoteBlockedUntil` chỉ dời về sau | `TestAccounts_StatusAndBlocked` | như trên | `PASS` |
| Đổi chế độ gửi: version+1, một audit `changed_fields = {send_mode}` có `request_id`, một báo hiệu `channel.status` theo `page:<id>` đòi `conversation.read`, một lệnh cảnh báo khi sang `enabled` (về `read_only` không thêm); version cũ → `ErrVersionConflict`; chế độ lạ, Page không có → lỗi; cùng giá trị → không đổi | `TestWriter_SetSendMode`, `TestWriter_SetHumanAgent` | như trên | `PASS` |
| Cảnh báo `send-mode:<id>` mức thường, văn bản chỉ mã tài khoản và kênh; Alerter lỗi → not_sent; Telegram 429 (đã bọc) → not_sent chờ đúng `Retry-After` (reviewer lượt 1); không Alerter, payload hỏng → rejected | `TestSendModeAlertCommand`, `TestNotify` (alertjobs) | `go test -race -count=1 -run '^(TestSendModeAlertCommand\|TestNotify)$' ./internal/modules/channel/ ./internal/platform/observability/alertjobs/` | `ok` cả hai; ca 429 thêm sau reviewer: `ok …/alertjobs 1.609s` (mã cũ trả `RetryAfter = 0`); ci-task lần 3 bên dưới |
| 04 mục 5.3: capacity 5, nạp 1/s, đồng hồ đứng yên → 5 lần được, lần 6 không, chờ trong [0,9 s; 1,1 s]; tiến 2 s → 2 lần; đồng hồ lùi không nạp; để lâu không vượt capacity; thùng mới 50 lượt; `blocked_until` tương lai → không, chờ đúng tới mốc | `TestBuckets_RefillClock`, `TestBuckets_FirstUseAndMissing`, `TestBuckets_Blocked` | như trên | `PASS` |
| 20 goroutine cùng lúc trên thùng còn 5 lượt → đúng 5 lần được | `TestBuckets_Concurrent` | như trên (`-race`) | `PASS` |
| 04 mục 4: `RunContract(fakeHarness)` — tách đúng 9 sự kiện của 2 fixture, tách hai lần cùng khóa, loại lạ vào `Skipped`, thân sửa một byte → chữ ký sai, quá cửa sổ → từ chối có Reason, `Send` ok/reject_policy/fail_before_send/timeout_after_send → sent/rejected/not_sent/unknown, `ClassifyError` đúng 6 lớp (cả khi bọc) | `TestContract` (+ `TestSplit_Payload`, `TestSendPolicy` 11 ca, `TestSendAndLookup`) | `go test -race -count=1 ./internal/adapters/fake/` | `ok …/adapters/fake 2.172s`; ci-task `ok 1.529s` |
| `Registry.Get("fakechat")` có ngoài production, không có ở production; chỉ `cmd/crm` nhập gói `fake`, mọi tệp nằm trong `internal/adapters/fake` | `TestAdapter_Registry`, `TestWireM4_Frame`, `TestFakeChat_OnlyCmdImports` | `go test -count=1 ./cmd/crm/`; `go test -count=1 -skip 'Lint\|StrayDirs' ./tests/arch/` | `ok …/cmd/crm 5.555s`; `ok …/tests/arch 6.071s`; ci-task `ok …/tests/arch 26.583s` |
| PQ04: `GET /api/v1/channels` — owner, `conversation.read` all → 2 Page; chỉ Page A → 1; Page khác → 0; body không có `credential`, `token`, `EAAB` | `TestChannelAPI_ListByScope` | `go test -race -count=1 -run '^(TestChannelAPI_ListByScope\|TestChannelAPI_SetSendMode\|TestChannelAPI_SetHumanAgent\|TestM4Contract_StatusCodes\|TestM3Routes_PendingList\|TestDomainErrorSnapshot\|TestDomainErrorSnapshot_TableMembership)$' ./internal/httpapi/` | `ok …/httpapi 5.176s`; ci-task `ok …/httpapi 39.954s` |
| `PUT send-mode` không `channel.manage` → 403; owner → 200 version 2; một audit, một lệnh cảnh báo, một báo hiệu; cùng khóa → `Idempotent-Replayed: true`; version cũ → 409 `version_conflict`; Page không có → 404 `not_found`; `PUT human-agent` tương tự, không lệnh cảnh báo | `TestChannelAPI_SetSendMode`, `TestChannelAPI_SetHumanAgent` | như trên | `PASS` |
| Ba operation đã làm không còn trả 501; mọi operation M4 khác vẫn 401/403/501 đúng | `TestM4Contract_StatusCodes`, `TestM3Routes_PendingList` | như trên | `PASS` |
| Lỗi channel đổi sang HTTP ở một chỗ (`errors_channel.go`) | `TestDomainErrorSnapshot`, `TestDomainErrorSnapshot_TableMembership` | như trên | `PASS` |
| `FAKECHAT_WEBHOOK_SECRET` tùy chọn ngoài production (≥ 32 ký tự), cấm ở production | `TestLoad_FakeChatSecret` | `go test -race -count=1 -run 'TestLoad_FakeChatSecret\|TestLoad_M4' ./internal/platform/config/` | `ok 1.491s` |
| View `channel.channel_accounts_v` khớp `docs/views.md`, miễn luật (g) có dòng; bảng `04*.sql` có trong ADR-46; ranh giới (n), (k), (a) | `TestViewsDoc*`, `TestLockOrder_M4TablesListed`, `TestBoundaries*` | ci-task bước "test kiến trúc" | `ok` |
| Cổng tĩnh và test theo phạm vi (đổi testkit → toàn bộ trừ e2e) | — | ci-task (lần 2, sau khi stage) | `CI-TASK XANH (so với điểm tách nhánh aafa0878…)`; lần 1 đỏ ở lint (gci `fake.go`), gen-check, catalog-check (chưa stage) — đã sửa. Sau đó sửa nhỏ `sendmode.go` (bỏ gán thừa), `buckets.go` (`retryAfter` không âm): chạy lại `go test -race ./internal/modules/channel/` `ok 2.403s`, lint 0 lỗi. Lần 3 (sau sửa của reviewer lượt 1, mọi thứ đã stage): `CI-TASK XANH (so với điểm tách nhánh aafa0878…)` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `SetSendMode`, `SetHumanAgent` là phương thức của `channel.Writer{Realtime, RequestID}` (module mẫu
  `catalog.Writer`), không phải hàm tự do: audit cần `RequestID` của yêu cầu và báo hiệu phát trong cùng tx. Chữ ký
  còn lại giữ như kế hoạch (`ctx, tx, id, mode, expectedVersion, actor identity.AuthUser`).
- `channeltest.Harness`: `Account()` và `Credentials()` gộp thành `Target()` (lint `interfacebloat` tối đa 10 phương
  thức); thêm `UnknownFixture()`, `ClosedWindow()`, `ErrorCases()` cho luật (3), (5), (7) mà kế hoạch không nói harness
  cung cấp bằng gì; `Sign` trả `map[string][]string` (gói trong `internal/modules` không nhập `net/http`, luật (b)).
- Kiểu adapter giả tên `fake.Chat` (`fake.Adapter` đã là hàm khởi tạo của khung Task 143 mà `wire_m4.go` gọi).
- Thêm `MarkStatus` lỗi `ErrInvalidStatus` với trạng thái lạ; `StoreCredentials` lỗi `ErrNoKeyring`,
  `ErrInvalidCredentials`, `ErrInvalidTokenKind`; `CredentialsFor` lỗi `ErrNoCredentials`.
- `TestViewsDocParses` đếm 43 view (thêm `channel.channel_accounts_v`).

## Việc phát hiện thêm

- Ba lệnh cảnh báo của fulfillment (`NewAuthAlertCommand`, `NewTrackAlertCommand`, `NewStuckAlertCommand`) và hai
  của cash có cùng đoạn "Alerter nil → rejected, lỗi → log + not_sent" (đang trong mốc nền jscpd); có thể chuyển sang
  `alertjobs.Notify` ở một task dọn riêng.
- `platform.put_tenant_route` (migration 0290) chỉ nhận loại tuyến `carrier_account`, `fb_page`, `service_key`: tuyến
  `fakechat_page` của kênh giả chưa ghi được qua đường này. Task 145 không ghi tuyến; task đầu tiên cần định tuyến sự
  kiện của kênh giả tới tenant (Task 153 hay test hội thoại) phải thêm một migration mở loại này.
- `TestPackagePatternSkipsStrayDirs` (tests/arch) chờ khóa "parallel golangci-lint" dùng chung giữa các phiên trên máy
  và có thể treo tới hết giờ khi phiên khác đang lint; chạy `go test ./tests/arch/` riêng thì nên `-skip` nó.

## Reviewer

Lượt 1: CẦN SỬA — `alertjobs.Notify` không chờ theo Telegram 429 (`Retry-After`) như `alertjobs.scheduled` và cash. Đã sửa: nhánh
`TelegramRetryError` → `RetryAfter`, thêm ca 429 vào `TestNotify`; ci-task chạy lại xanh.

Lượt 2: ĐẠT — điểm lượt 1 sửa đúng (`notify.go` đặt `RetryAfter` từ `TelegramRetryError` như `alertjobs.go` và cash;
`notify_test.go` ca 429 đã bọc); Ảnh hưởng, bảng bằng chứng khớp mã; không test nào bị xóa, tắt, nới. Ghi chú không bắt
buộc: `platform.put_tenant_route` chỉ nhận `carrier_account`, `fb_page`, `service_key` (đã ghi ở Việc phát hiện thêm).

Kết luận: ĐẠT
