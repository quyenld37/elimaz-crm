# Task 150 — Sức khỏe kênh, im lặng bất thường, hỏi bù, đọc comment định kỳ, nhắc phiên bản Graph

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 150, M4-10). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): **chỉ thêm, không đổi chữ ký cũ.** Thêm trong `channel`:
  `HealthCheckKind`, `BackfillKind`, `CommentPollKind`, các hằng lệnh cảnh báo, `Writer.ReportAuthFailure`,
  `Writer.ReportPolicyEnforcement`, `Writer.RequestHealthCheck`, `EnqueueCommentPoll`, `InSalesHours`, `JobHandlers`,
  `HealthAlertCommands`, interface tùy chọn `CommentPoller`; `channeltest.ConnectorAdapter` thêm `SetBackfill`,
  `Backfill`, `SetComments`, `PollPageComments`, `Features` (adapter giả chỉ dùng trong test: `connect_test`, `oauth_test`,
  `handlers_channel_connect_test` — 3 chỗ dựng, không đổi hành vi cũ). `facebook.Channel` thêm `PollPageComments`
  (dùng lại `backfill.feed`). `channelAccountOut` (httpapi, 2 chỗ gọi) không đổi; `ListChannelAccounts` thêm `features`.
  `parts` của channel thêm trường `Jobs` (gói riêng, 3 hàm gom trong `wiring.go`).
- Gói phụ thuộc ngược (go list): `channel` được nhập bởi `cmd/crm`, `adapters/facebook`, `adapters/fake`,
  `httpapi`, `channel/channeltest`, `conversation`, `conversation/internal/deps` (và các phần của conversation qua deps).
  `conversation/internal/history`, `conversation/internal/inbound` chỉ được nhập bởi gói gốc `conversation`.
- Operation OpenAPI bị chạm, oasdiff: `runChannelHealthCheck` (đã có hợp đồng từ Task 143, bỏ khỏi `m4_pending.txt`);
  `listChannelAccounts`: schema `ChannelAccount` thêm thuộc tính **không bắt buộc** `features` (`ChannelFeatures`) —
  chỉ thêm, oasdiff không phá vỡ. Không thêm mã lỗi.
- Chỗ dùng trên màn hình: chưa có màn kênh (Task 174–178 chưa làm); `console/src/api` sinh lại có thêm kiểu
  `ChannelFeatures`, chạy `tsc`.
- Câu sqlc, bảng, pha migration: **không migration** (cột `token_checked_at`, `last_webhook_at`, `last_feed_at`,
  `status_reason` và quyền UPDATE đã có ở 0401; `page_settings.comment_poll_mode` đã có ở 0402). Câu mới:
  `queries/channel/accounts.sql` `SetTokenChecked`; `queries/conversation/comment_poll.sql` `CommentPollModes`
  (đọc `conversation.page_settings` của đúng module conversation). Bảng chạm: `channel.channel_accounts` (UPDATE
  status, token_checked_at), `platform.jobs` (Enqueue), `platform.webhook_deliveries` (qua `webhooks.EnqueueInbound`).
- Test sẽ chạy: `go test -race -run '…' ./internal/modules/channel/` (health_test mới), `./internal/modules/conversation/internal/history/`
  (backfill_test mới), `./internal/adapters/facebook/` (`TestPollPageComments`), `./internal/httpapi/` (health-check,
  danh sách có features, `TestM4Contract_StatusCodes`, `TestM3Routes_PendingList`), `./cmd/crm/` (wiring);
  `tests/e2e/channel_silence_test.go` viết sẵn, không chạy; `tests/arch`; ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `webhooks`
  (`EnqueueInbound`: "sự kiện đọc chủ động, hỏi bù ghi bằng EnqueueInbound"), `observability`/`alertjobs`
  (`Notify` chỉ gọi từ lệnh), `schedules.Schedule` (PerTenant), `commands.CommandKind`, `queue.Enqueue` DedupeKey;
  trong module: `channel.enqueueAlert`/`accountAlert` (Task 145, 149), `pageCommand.load` (đọc tài khoản + token
  trong tx ngắn), `enqueueAccountCommand`, `MarkStatus`, `lockFor`, `Writer.signal`; adapter: `Channel.Backfill`,
  `HealthCheck` (Task 148); mẫu lịch theo tenant `reminders.fireHandler` (Task 169).
- Dùng lại / viết mới vì: dùng lại `accountAlert` cho 4 cảnh báo mới (auth, fields, silence, policy), `enqueueAlert`,
  `MarkStatus`, `lockFor`, `enqueueAccountCommand` (thêm biến thể có khóa chống trùng), `EnqueueInbound`, `backfill.feed`
  của adapter (đọc comment định kỳ gọi lại đúng hàm này). Tách phần đọc tài khoản + token của `pageCommand.load` thành
  hàm chung `loadTarget` (lệnh subscribe và các lệnh đọc mới cùng dùng, cùng lý do thay đổi). Viết mới: lệnh đọc
  `readCommand` (hỏi bù và đọc comment chung một kiểu, chỉ khác hàm đọc và `via`), cảnh báo phiên bản Graph (payload
  không theo tài khoản nên không dùng `accountAlert`).

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: trạng thái, mốc kiểm của Page thuộc `channel`; chế độ đọc comment (`page_settings`) thuộc
  `conversation`. Lịch đọc comment định kỳ đặt ở `conversation` (đọc bảng của mình), gọi public API `channel.EnqueueCommentPoll`.
- 5.2 Không ghi bảng module khác: `conversation` chỉ gọi `channel.Writer.ReportPolicyEnforcement`, `channel.EnqueueCommentPoll`.
- 5.3 Đọc module khác: `conversation` đọc tài khoản qua `channel.ListAccounts` (public API), không SQL chéo schema.
- 5.4 Nguyên tử: đổi trạng thái + ghi lệnh cảnh báo + lệnh đăng ký lại + báo hiệu cùng một tx (hook OnDone/OnTerminal hay việc của lịch).
- 5.5 Không nguyên tử: cảnh báo Telegram, gọi Graph đi bằng lệnh `commands` sau commit.
- 5.6 Gọi ra ngoài: kiểm sức khỏe, hỏi bù, đọc comment là lệnh chỉ đọc (`command.fb.*`), Execute ngoài tx; `in_doubt`
  của lệnh chỉ đọc tra lại bằng `Lookup` → `not_sent` (đọc lại vô hại). Lệnh đăng ký lại là `subscribe_page` của Task 149.
- 5.7 Ngữ cảnh shop: lịch theo tenant chạy trong WithTenant của việc; lệnh đọc ghi sự kiện trong `tenancy.WithTenant`.
  Không bảng mới.
- 5.8 Không HTTP trong module: handler `RunChannelHealthCheck` mỏng ở httpapi.
- 5.9 Đường ghi: `POST …/health-check` có `IdempotencyKey`, `x-permission: channel.manage` (hợp đồng Task 143).
- 5.10 Không dùng Valkey.
- 5.11 Không thư viện mới.
- 5.12 Theo mẫu: lệnh như `subscribe.go` (Task 149), lịch như `reminders` (Task 169), cảnh báo như `alert.go`.

Mục 6:
- 6.1 Đồng thời: đổi trạng thái bằng `MarkStatus` (UPDATE có điều kiện, `changed` chỉ đúng một lần → một cảnh báo);
  hook khóa dòng tài khoản (`lockFor`) trước khi đổi; hai lịch chạy chồng: khóa chống trùng của lệnh (`<id>:<kỳ>`).
- 6.2 Gửi lại: POST có Idempotency-Key; lệnh của lịch chống trùng theo kỳ; sự kiện hỏi bù chống trùng theo khóa của
  webhook (`EnqueueInbound` → `created = false`).
- 6.3 Thứ tự khóa: tài khoản kênh (nhóm 3) rồi `platform.jobs` (nhóm 13) — đúng ADR-46; lệnh đọc không khóa dòng nghiệp vụ.
- 6.4 Lỗi tạm: `tenancy.WithTenant` dùng `RetryTx`; hàm trong tx gán lại biến kết quả mỗi lần chạy.
- 6.5 Cách ly: READ COMMITTED + khóa dòng.
- 6.6 Việc nền lỗi: việc của lịch `MaxAttempts 1` (kỳ sau có việc mới); lệnh đọc 8 lượt, lỗi chưa gửi/quá tải → not_sent tự thử lại.
- 6.7 429: lớp `rate_limited` → not_sent (lịch thử lại của hàng đợi).
- 6.8 Chết giữa chừng: lệnh đọc `in_doubt` → `Lookup` trả not_sent → chạy lại; sự kiện đã ghi không ghi trùng.
- 6.9 Treo: hạn Execute 30 s; hỏi bù ghi theo lô 100 sự kiện mỗi tx ngắn.
- 6.10 Tiền: không áp dụng: không chạm tiền.
- 6.11 Quy luật: không áp dụng rapid (không vùng rủi ro cao nào bị chạm); có test bảng cho khung giờ, ngưỡng.
- 6.12 Dữ liệu cá nhân: cảnh báo chỉ có tên Page (công khai), mã tài khoản, hành động chính sách; không token, nội dung.
  Payload lệnh chỉ mã tài khoản, mốc thời gian.
- 6.13 Quyền: `channel.manage` toàn shop do guard kiểm; tài khoản shop khác → 404 (RLS).

## Quyết định thay chủ shop

1. **Lệnh hỏi bù `command.fb.backfill` và đọc comment `command.fb.comment_poll` nằm ở module `channel`** (kế hoạch ghi
   `conversation/internal/history/backfill.go`). Lý do: lịch im lặng của `channel` phải ghi lệnh hỏi bù, mà `channel`
   không được nhập `conversation` (cạnh ngược). Hai lệnh chỉ cần adapter + `webhooks.EnqueueInbound`, không đụng bảng
   của `conversation`. `history/backfill.go` giữ phần cần dữ liệu của `conversation`: lịch `channel.comment_poll` đọc
   `page_settings.comment_poll_mode` rồi gọi `channel.EnqueueCommentPoll`. Phương án khác: lệnh ở `conversation`, lịch
   im lặng ghi lệnh bằng tên Kind chép tay (hai nơi khai cùng một Kind). Đảo ngược: dời tệp, không đổi hành vi.
2. **Mốc đọc của đọc comment định kỳ không lưu trạng thái**: mỗi lần đọc từ `lúc chạy − 2 phút` (gối bốn kỳ 30 s),
   comment trùng bị khóa chống trùng của webhook loại; một kỳ lỡ quá 2 phút vẫn được lịch hỏi bù 10 phút (đọc 30 phút)
   bù lại. Kế hoạch ghi "sau mốc lần đọc trước" nhưng không có cột lưu mốc; thêm cột cần migration ngoài dải đã chia.
   Đảo ngược: thêm cột `page_settings.comment_polled_at` và đọc từ đó.
3. **Adapter Facebook thêm interface tùy chọn `channel.CommentPoller` (`PollPageComments`)**, dùng lại đúng hàm đọc feed
   của `Backfill` (một lời gọi feed, không đọc hội thoại mỗi 30 s). Adapter không có → lệnh `rejected`
   `no_comment_poller`.
4. **Chống lặp 7 ngày của cảnh báo phiên bản Graph** bằng khóa chống trùng của lệnh theo đợt 7 ngày tính từ lúc vào
   khoảng 180 ngày (lịch chạy mỗi ngày nên mỗi đợt gửi một lần, cách nhau 7 ngày). Không cần bảng trạng thái. Phương án
   khác: đọc `platform.alert_events` (bảng nền tảng, module không được đọc).
5. **Lệnh đọc gặp lỗi token (lớp `auth`) gọi `ReportAuthFailure` ngay trong `Execute` (tx ngắn riêng) rồi trả
   `rejected`**, vì `OnTerminal` không nhận lý do lỗi (kế hoạch: gọi từ `OnTerminal` của lệnh gửi — Task 156/158 làm
   như kế hoạch). Lệnh kiểm sức khỏe không cần: token hỏng là kết quả kiểm (`TokenValid = false`).
6. **`ReportAuthFailure`, `ReportPolicyEnforcement`, `RequestHealthCheck` là phương thức của `channel.Writer`**
   (kế hoạch: hàm `channel.ReportAuthFailure(ctx, tx, id)`) để báo hiệu `channel.status` qua `Writer.Realtime` như mọi
   đường ghi khác của `channel`.
7. **Lý do `degraded` và ai gỡ**: `token_invalid`, `missing_permissions` (lịch sức khỏe), `auth_failed` (lệnh gặp lỗi
   token), `subscribe_failed` (Task 149) do lịch sức khỏe gỡ khi token, quyền tốt lại; `meta_policy:<hành động>` chỉ gỡ
   khi Meta gửi `unblock` (lịch sức khỏe không biết chế tài đã hết). Đã `degraded` vì mất quyền thì báo mất quyền lần
   nữa không cảnh báo thêm (một cảnh báo mỗi lần mất quyền; cảnh báo gấp nhắc lại do Alerter của M0 lo). Thiếu quyền
   (`PermissionsOK = false`) xử lý như token hỏng (cảnh báo `channel-auth`, HT15 "mất quyền").
8. **Giờ bán 08:00–23:00 giờ Việt Nam là `[08:00, 23:00)`**; im lặng tính khi webhook cuối cách `[30 phút, 3 giờ]`.
   Lịch chỉ chạy cho Page mà kênh có adapter ở tiến trình (không ghi lệnh chắc chắn bị từ chối).
9. **`features` trong danh sách kênh**: thuộc tính không bắt buộc `ChannelFeatures` (7 cờ; `messages` luôn đúng vì
   mọi adapter nhắn tin đều có hộp thư). Page của kênh chưa bật ở tiến trình → không có `features`.

## Dừng để hỏi

(không có)

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| HT15: token thu hồi → lịch `channel.health` ghi đúng 1 lệnh kiểm khóa `<id>:<giờ>` (chạy 2 lần) → `degraded` `token_invalid`, `token_checked_at` = giờ chạy, đúng 1 cảnh báo gấp `channel-auth:<id>` có "Shop Một", "kết nối lại", không `EAAB`; báo hiệu `channel.status`; 1 giờ sau vẫn hỏng → vẫn 1 cảnh báo; khi degraded tuyến `fb_page:1001` còn, 3 sự kiện → 3 việc `inbound.facebook`; nối lại → `active`; 31 phút sau kiểm lại → `active`, vẫn 1 cảnh báo | `TestHealth_TokenRevoked` | `go test -race -count=1 ./internal/modules/channel/...` | đỏ trước (`undefined: channel.HealthSchedule`…); sau `ok …/modules/channel 6.325s` |
| Page mất đăng ký webhook (khe Task 149) → tự ghi `subscribe_page`, cảnh báo thường `channel-fields:<id>`, đăng ký lại đủ 11 trường; degraded `auth_failed` mà token tốt → `active` + báo hiệu; chế tài Meta (`meta_policy:block`) không bị lịch gỡ; Page đã ngắt không được kiểm; `ReportAuthFailure` hai lần → thêm đúng 1 cảnh báo | `TestHealth_FieldsAndRecovery` | như trên | `PASS` |
| 02 mục 8: `ReportPolicyEnforcement` block (2 lần) → `degraded` `meta_policy:block`, 1 cảnh báo gấp `meta-policy:<id>` có tên Page và hành động; unblock → `active`; hành động lạ → `other`; Page đã ngắt không đổi | `TestHealth_PolicyEnforcement` | như trên | `PASS` |
| Sự kiện `policy.enforcement` (fixture) → `degraded`, đúng 1 lệnh cảnh báo `meta-policy:<id>` (payload không có lý do văn bản của Meta), 1 báo hiệu; lặp lại không thêm; Page lạ bỏ qua; dữ liệu hỏng → `ErrPermanent` | `TestHandlePolicy` | `go test -race -count=1 -run '^(TestCommentPoll\|TestHandlePolicy)$' ./internal/modules/conversation/internal/history/ ./internal/modules/conversation/internal/inbound/` | đỏ trước (`undefined: inbound.HandlePolicy`); sau `ok …/inbound 2.600s` |
| HT16: 20:00 giờ VN, webhook cuối cách 40 phút → 1 cảnh báo gấp `webhook-silence:<id>` ("Im lặng bất thường", tên Page), 1 lệnh đăng ký lại, 1 lệnh hỏi bù (chạy 2 lần vẫn 1), hỏi bù đọc từ `last − 5 phút`; 08:00/30 phút → có; 03:00, 23:00, cách 5 giờ, cách 10 phút → không gì; Page degraded, chưa từng có webhook → không | `TestSilence` (7 ca con) | `go test -race -count=1 ./internal/modules/channel/...` | `PASS` |
| Hỏi bù: kênh trả 3 sự kiện, 1 đã có việc từ webhook → kết quả lệnh `read 3, created 2`, tổng 3 việc `inbound.facebook`, 2 việc `via backfill`; lịch `channel.backfill` ghi 1 lệnh cho Page active (Page đã ngắt không) từ `now − 30 phút`, chạy 2 lần trong kỳ vẫn 1 | `TestBackfill_Dedupe` | như trên | `PASS` |
| Lệnh đọc: lỗi chưa gửi → tự thử lại rồi `done`; đọc comment ghi việc `via comment_poll`, mốc đọc đúng; `Lookup` của 3 lệnh đọc → `not_sent`; hỏi bù gặp token hỏng → `dead`, `degraded` `auth_failed`, 1 cảnh báo | `TestReadCommands_Errors` | như trên | `PASS` |
| 02 mục 7: hạn còn 181 ngày → 0 cảnh báo; 179 ngày → 1 (`graph-version`, thường, có `v26.0`, `2028-07-29`, `179`); +1 ngày, +5 ngày → vẫn 1; +7 ngày → 2; không có hạn → không | `TestGraphVersion` | như trên | `PASS` (lần đầu đỏ ở ca +6 ngày 23 giờ: đợt 7 ngày tính từ lúc vào khoảng 180 ngày — Quyết định 4, sửa ca test) |
| Lệnh, lịch, việc nền của channel đúng tên, chu kỳ, theo tenant | `TestWiringCommands` | như trên | `PASS` |
| Q4: `auto` + feed 11 phút lúc 12:30 → 1 lệnh (mốc = lúc chạy − 2 phút, chạy 2 lần vẫn 1); feed 2 phút → 0; `auto` chưa có feed → 1; `auto` 03:00 → 0; không có dòng cài đặt = `auto`; `off` → 0; `on` → luôn có (cả 03:00); Page degraded, kênh không có adapter → 0 | `TestCommentPoll` (10 ca con) | lệnh `history/inbound` ở trên | đỏ trước (`undefined: history.CommentPollSchedule`); sau `ok …/history 2.243s` |
| Adapter: `PollPageComments` một lời gọi feed (0 lời gọi hội thoại), comment sau `since` thành `comment.changed` khóa như webhook, comment cũ bỏ; token thu hồi → lớp `auth`; `*Channel` hiện thực `CommentPoller` | `TestPollPageComments`, `TestContract` | `go test -race -count=1 -run '^(TestPollPageComments\|TestBackfill\|TestContract)$' ./internal/adapters/facebook/` | `ok …/adapters/facebook 2.625s` |
| `POST /channels/{id}/health-check`: sale → 403; owner → 202 trả tài khoản, 1 lệnh `command.fb.health_check` (payload không token); cùng khóa → `Idempotent-Replayed`, không thêm; khóa mới → thêm 1; Page lạ → 404 | `TestChannelAPI_HealthCheck` | `go test -race -count=1 -run '^(TestChannelAPI_.*\|TestM4Contract_StatusCodes\|TestM3Routes_PendingList\|TestChannelConnect_Page)$' ./internal/httpapi/` | `ok …/httpapi 9.200s` |
| Chủ shop 26/11: `GET /channels` có `features` (7 cờ, `messages` đúng) cho Page kênh có adapter; Page kênh không có adapter → không có | `TestChannelAPI_ListFeatures` | như trên | `PASS` |
| `runChannelHealthCheck` hết 501 (bỏ khỏi `m4_pending.txt`); operation M4 khác vẫn 501 | `TestM4Contract_StatusCodes`, `TestM3Routes_PendingList` | như trên | `PASS` |
| Nối M4: lệnh 5 Kind, 7 lịch (4 của channel, presence_cleanup, reminders, comment_poll) mỗi lịch một việc nền cùng Kind; conversation có 3 lịch | `TestWireM4_Frame`, `TestWiring` | `go test -count=1 -run 'TestWireM4\|TestWiring$' ./cmd/crm/ ./internal/modules/conversation/` | `ok …/cmd/crm 1.473s`, `ok …/conversation 0.571s` (sau gộp m4) |
| Tĩnh: lint, mã lặp, kiến trúc, tsc | — | `golangci-lint run` 6 thư mục; `make jscpd`; `go test -count=1 ./tests/arch/`; `cd console && npx tsc --noEmit` | `0 issues.`; `Found 0 clones.` (trước sửa 6 clone); `ok …/tests/arch 249.044s`; tsc không lỗi |
| Cổng kiểm của task | ci-task | ci-task của task (sau khi stage) | lần 1 đỏ `gen-check` (chưa stage: cây làm việc khác index); lần 2 bị dừng ở giới hạn 1 giờ của lệnh nền khi đang xếp hàng sau phiên khác (57 gói `ok`, 0 hỏng); lần 3: 94 gói `ok`, gồm `ok …/modules/channel 3.888s`, `ok …/history 2.498s`, `ok …/inbound 2.109s`, `ok …/httpapi 55.826s`, `ok …/adapters/facebook 11.966s`, `ok …/cmd/crm 4.681s`, `CI-TASK XANH (so với điểm tách nhánh aafa0878…)` |

## Test bị xóa hoặc tắt

(không có)

## E2E viết sẵn, chưa chạy

- `tests/e2e/channel_silence_test.go`: `TestE2E_ChannelTokenRevoked` (HT15 trên máy Meta giả + adapter Facebook thật:
  token thu hồi → degraded, 1 cảnh báo; 3 webhook có chữ ký qua ingest → 3 việc `inbound.facebook`; nối lại → active; 31
  phút sau không nhắc lại), `TestE2E_ChannelSilence` (HT16: 20:00, im 40 phút → 1 cảnh báo, Meta giả nhận POST
  `subscribed_apps`, hỏi bù hội thoại 3 tin/1 đã có → 3 việc, 2 qua hỏi bù). Biên dịch, qua vet, lint; chưa chạy (luật
  chủ shop 24/11). Đủ ca đã nằm ở test gói `channel`, `history`, `inbound`, `httpapi`.

## Khác kế hoạch

- Quyết định 1–9 ở trên.
- Handler `RunChannelHealthCheck` nằm trong `internal/httpapi/handlers_channel.go` cạnh các handler kênh khác (kế hoạch
  ghi tệp mới `handlers_channel_health.go`; một hàm 15 dòng không cần tệp riêng).
- `inbound/policy.go` là hàm `HandlePolicy` (Task 153 chưa gộp vào m4 nên chưa có handler vào `inbound.facebook` để
  thêm nhánh); xem Việc phát hiện thêm 1.
- `channel` có thêm trường `Jobs` trong `parts` và hàm `JobHandlers`; `cmd/crm/wire_m4.go` gom `channel.JobHandlers`
  (tệp ghi "task sau không sửa"; cần vì lịch của channel là việc nền trong database), `cmd/crm/main.go` đăng ký
  `channel.HealthAlertCommands` cùng các lệnh cảnh báo khác (chỉ ở đó có Alerter).
- `channel.Registry.Lookup` (nil-safe) mới; `pageCommand.load` tách phần đọc tài khoản + token thành `loadTarget`,
  `accountOf` (dùng chung với lệnh mới; hành vi lệnh đăng ký không đổi, test Task 149 vẫn xanh); `pageCommandKind` đổi
  tên `accountCommandKind` (đọc comment sống 2 phút, lệnh khác 1 giờ).
- Thêm vào adapter giả `channeltest.ConnectorAdapter`: `Features`, `SetBackfill`/`Backfill`, `SetComments`/`PollPageComments`.
- Gộp `m4` (c3e8613, Task 163) trước review: fast-forward; ba tệp chồng lấn (`wire_m4_test.go`, `conversation/wiring_test.go`,
  `m4_pending.txt`) áp lại phần của 150 lên bản của 163 (giữ lịch `presence_cleanup` của 163, thêm lịch của 150).

## Việc phát hiện thêm

1. **Khi Task 153 gộp** (handler `inbound.facebook` ở `inbound/handler.go`): thêm nhánh
   `case channel.EventPolicyEnforcement: err = HandlePolicy(ctx, tx, h.d, h.source, e)` vào `handler.Handle` (hiện
   sự kiện này rơi vào `inbound_ignored`), và chỉ gọi `channel.NoteInbound` khi `e.Via == webhooks.ViaWebhook`: sự
   kiện hỏi bù, đọc comment ghi lại mốc `last_webhook_at`/`last_feed_at` sẽ che mất "im lặng bất thường" và làm chế độ
   `auto` của đọc comment tự tắt rồi bật lại. Hai việc này của người gộp 153 + 150 (hay Task 154 comment vào).
2. Khe ngược của Task 149 (Page đã ngắt, token đã xóa nhưng Meta còn đăng ký webhook): lịch sức khỏe không làm được gì
   khi không còn token (không gọi được `DELETE subscribed_apps`); tin vẫn lưu vì tuyến còn. Chỉ có thể gỡ trên Meta
   Business Suite; ghi vào RUNBOOK khi làm Task 181.
3. Khe hẹp đã biết: kết quả kiểm sức khỏe "token hỏng" của lần gọi Meta bắt đầu **trước** khi quản trị nối lại Page
   nhưng ghi xong **sau** đó sẽ đặt lại `degraded` và gửi một cảnh báo thừa; lần kiểm giờ sau tự gỡ. Muốn bịt hẳn cần
   mốc "token đổi lúc" trên `channel_accounts` (migration) để `OnDone` bỏ kết quả cũ hơn mốc đó.
4. (Reviewer) Page đang `degraded` vì chế tài Meta (`meta_policy:<x>`) mà lịch sức khỏe thấy token hỏng: lý do bị ghi
   đè thành `token_invalid`; nối lại xong lịch đưa về `active`, dấu chế tài mất trên CRM (Meta vẫn chặn, lỗi gửi hiện
   lại khi gửi). Hiếm; muốn giữ cần cột lý do riêng cho chế tài.
5. Màn hình kênh (Task 174–178) dùng `features` để ẩn phần kênh không hỗ trợ và nút "Kiểm ngay" gọi `runChannelHealthCheck`.

## Reviewer

Kết luận: **ĐẠT** (agent reviewer trên phần đã stage, sau khi gộp m4). Phát hiện phải sửa: không có.
Ghi chú không bắt buộc đã xử lý trong tài liệu: mục Ảnh hưởng thêm `SetComments`; Khác kế hoạch ghi handler nằm ở
`handlers_channel.go`; Việc phát hiện thêm 4 (lý do chế tài Meta bị ghi đè khi token hỏng). Ghi chú còn lại: cảnh báo
thường `channel-fields` có thể mỗi giờ một lần nếu đăng ký lại cứ thất bại mà Page vẫn active — Alerter của M0 chống
lặp theo khóa, chấp nhận; hai việc khi gộp Task 153 (Việc phát hiện thêm 1).

## Gộp vào m4

Phiên gộp tuần tự ngày 26/11 (154 → 156 → 150 → 170), `git merge --no-ff --no-commit task-150` trên m4 `e4e8a690`
(đã có 145–149, 151–154, 156, 160, 161, 163, 169, 179). Nhánh tách từ m4 cũ (trước 153), nên nhiều chỗ chồng nhau.

Xung đột chữ và cách giải (giữ đủ hai bên):
- `tests/arch/testdata/m4_pending.txt`: m4 (Task 160) đã bỏ `getPageSettings`, `updatePageSettings`,
  `listAssignableUsers`; nhánh bỏ `runChannelHealthCheck` → bỏ cả bốn.
- `cmd/crm/main.go`: giữ `checkup` (179) và bọc danh sách bằng `append(…, channel.HealthAlertCommands(…)...)` (150).
- `cmd/crm/wire_m4_test.go`: giữ `m4InboundSources`, `m4CommandKinds`, ca Facebook của m4; lệnh mong đợi theo thứ tự
  đăng ký: subscribe, unsubscribe, health_check, backfill, comment_poll (150), `fetch_post` khi có Facebook (154),
  `send_message` (156); `checkM4Schedules` dùng bản của 150 (bốn lịch sức khỏe kênh + presence_cleanup, reminders,
  comment_poll; graph_version không theo tenant).
- `internal/modules/conversation/wiring_test.go`: chỉ chú thích; thân đã gộp tự động (`checkCommands` của 154/156,
  `checkReminders` có `channel.comment_poll` 30 s của 150).
- `internal/modules/channel/channeltest/connector.go`: giữ `appUsers` (179) và `backfill`, `comments` (150).

Xung đột không hiện thành chữ (tên khai trùng):
- `channel.AuthAlertKind = "channel.auth_alert"` khai ở cả `authfail.go` (156, báo token hỏng khi gửi tin) và
  `health.go` (150, kiểm sức khỏe mất quyền), cùng key `channel-auth:<id>`, cùng payload. Giữ một khai báo
  (`authfail.go`), bỏ dòng ở `health.go`. Hai bên cũng có hai handler cùng Kind (`NewAuthAlertCommand` đăng ký ở
  `main.go` và phần tử đầu của `HealthAlertCommands`) — một Kind chỉ được một handler, nên `HealthAlertCommands` dùng
  `NewAuthAlertCommand` và `main.go` bỏ dòng đăng ký riêng. Văn bản cảnh báo đổi thành "mất quyền hoặc hết hiệu lực
  token, tin vẫn nhận nhưng chưa gửi được: quản trị cần kết nối lại Page" để đúng cho cả hai nguyên nhân (test của cả
  hai bên chỉ kiểm tên Page, "kết nối lại", mức gấp, key).
- `reasonTokenInvalid` (150, không xuất) và `ReasonTokenInvalid` (156) cùng giá trị `token_invalid` → `health.go` dùng
  `ReasonTokenInvalid`, bỏ hằng trùng.
- `make gen`: không chênh lệch; `make jscpd`: `Found 0 clones`.

Bằng chứng (chạy trên bản đã gộp):
- `go build ./...` qua; `go vet ./cmd/crm/ ./internal/httpapi/... ./internal/modules/conversation/... ./internal/modules/channel/... ./internal/adapters/... ./tests/...` không lỗi
  (gồm `tests/e2e/channel_silence_test.go`, chỉ biên dịch, không chạy e2e).
- `go test -race -count=1 ./cmd/crm/ ./internal/httpapi/... ./internal/modules/conversation/... ./internal/modules/channel/... ./internal/adapters/facebook/...` → mọi gói `ok` (thoát 0).
- `go test -count=1 ./tests/arch/` → `ok tests/arch 93.228s`.
- `golangci-lint run ./cmd/crm/... ./internal/httpapi/... ./internal/modules/conversation/... ./internal/modules/channel/... ./internal/adapters/facebook/... ./tests/arch/...` → `0 issues.`

Reviewer lần 1: **CẦN SỬA**, 4 phát hiện, đã sửa trong lần gộp này (lần sửa 1):
1. `policy.enforcement` chưa nối (Việc phát hiện thêm 1 giao cho người gộp): `inbound/handler.go` thêm nhánh
   `case channel.EventPolicyEnforcement: err = HandlePolicy(ctx, tx, h.d, h.source, e)`.
   Test `TestInbound_PolicyEnforcementRouted` (`inbound/via_test.go`): sự kiện qua worker → Page `degraded meta_policy:block`.
2. + 3. Mốc `last_webhook_at` bị sự kiện hỏi bù, đọc comment kéo về hiện tại (che "im lặng bất thường"); không nơi nào
   ghi `last_feed_at` (chế độ `auto` của đọc comment luôn chạy): `handler.account` chỉ gọi `channel.NoteInbound` khi
   `e.Via == webhooks.ViaWebhook`, `feed = e.Type == channel.EventCommentChanged`.
   Test `TestInbound_OnlyWebhookNotesInbound`: comment `ViaCommentPoll` và tin `ViaBackfill` vẫn lưu (2 tin) nhưng không
   ghi mốc; tin qua webhook ghi `last_webhook_at`, chưa ghi feed; comment qua webhook ghi `last_feed_at`.
4. Hai hàm báo token hỏng khác chốt: `channel.ReportAuthFailure(ctx, tx, id, ref)` của 156 gọi thẳng `MarkStatus` (Page
   đã ngắt bị đổi sang degraded rồi lịch sức khỏe của 150 đưa về active; ghi đè lý do mất quyền khác). Bỏ hàm đó; thêm
   `channel.Writer.ReportTokenInvalid(ctx, tx, id)` khóa dòng rồi đi chung `degradeAuth` của 150 (lý do `token_invalid`,
   chốt disconnected và "đã degraded vì mất quyền", báo hiệu `channel.status`). Nơi gọi duy nhất
   `conversation/internal/send/merge.go` (OnTerminal) đổi sang `(channel.Writer{Realtime: c.d.Bus}).ReportTokenInvalid`.
   Test `TestReportTokenInvalid` (đổi tên từ `TestReportAuthFailure`, `channel/authfail_test.go`) thêm ca Page đã ngắt:
   giữ `disconnected`, không thêm cảnh báo. Tên `ReportAuthFailure(ctx, tx, id, ref)` ở các mục trên của tệp
   `docs/tasks/task-156.md` là lịch sử của Task 156, không sửa.
- jscpd sau sửa báo 3 đoạn trùng mới (thân `ReportAuthFailure`/`ReportTokenInvalid`; `enqueueVia` trong test với
  `env.enqueue`) → thêm `Writer.lockAndDegradeAuth` dùng cho cả hai, `env.enqueueFrom(source, via, …)` cho `enqueue`
  và `enqueueVia`. `make jscpd` → `Found 0 clones`.

Bằng chứng sau sửa:
- `go build ./...`; `go vet ./cmd/crm/ ./internal/modules/channel/... ./internal/modules/conversation/... ./tests/...` → không lỗi.
- `go test -count=1 -run 'TestInbound_OnlyWebhookNotesInbound|TestInbound_PolicyEnforcementRouted' ./internal/modules/conversation/internal/inbound/` → `ok`;
  `go test -count=1 -run TestReportTokenInvalid ./internal/modules/channel/` → `ok`.
- `go test -race -count=1 ./cmd/crm/ ./internal/httpapi/... ./internal/modules/conversation/... ./internal/modules/channel/... ./internal/adapters/facebook/...`
  → mọi gói `ok` trừ `internal/httpapi/web` (`TestWebShippingQuote`: "hãng được gọi 4 lần … muốn 2 (lần hai từ bộ
  đệm)"; gói không đổi trong bản gộp, test theo giờ thật của bộ đệm). Chạy lại riêng `-race -run TestWebShippingQuote`
  3 lần: cả 3 `ok` → **chập chờn** do tải máy. Sau lần dọn đoạn trùng chạy lại
  `go test -race -count=1 ./internal/modules/channel/... ./internal/modules/conversation/... ./cmd/crm/` → thoát 0.
- `go test -count=1 ./tests/arch/` → `ok tests/arch 39.546s`.
- `golangci-lint run` trên `cmd/crm`, `internal/httpapi`, `internal/modules/conversation`, `internal/modules/channel`,
  `internal/adapters/facebook`, `tests/arch` → `0 issues.` (chạy lại trên channel, conversation, cmd/crm sau lần dọn: `0 issues.`).
- Reviewer phần gộp, lần 2 (toàn bộ phần đã stage, 43 tệp): **ĐẠT**. Không có phát hiện phải sửa; bốn phát hiện lần 1
  đã sửa đúng (policy qua worker thật; `Via` rỗng của sự kiện cũ được coi là webhook nên không sót; một đường báo mất
  quyền, thứ tự khóa không đổi). Ghi chú: lịch đọc comment vẫn ghi lệnh cho Page fakechat (không đọc được comment) →
  lệnh bị từ chối `no_comment_poller` mỗi 30 giây ngoài production; không ảnh hưởng tính đúng, để task sau.
