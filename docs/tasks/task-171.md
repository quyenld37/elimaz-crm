# Task 171 — Nguồn comment live — luồng SSE, hỏi định kỳ dự phòng, đối chiếu cuối phiên

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 171 = M4-31). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào đã có. Thêm mới: adapter Facebook
  `(*Channel).StreamComments`, `(*Channel).PollComments`, `(*Channel).CountComments` (tệp `live_stream.go`,
  `live_poll.go`; cùng `DetectLive`, `LiveStatus` của Task 170 → `*Channel` hiện thực đủ `channel.LiveSource`, có dòng
  `var _ channel.LiveSource`). Phần `internal/live` (chỉ gói gốc conversation nhập): `FBSource`/`NewSource`
  (`CommentSource` cho kênh có `channel.LiveSource`), `SourceOptions`, hằng `PollEvery`, `SilenceAfter`,
  `RetrySSEEvery`, `FailWindow`, `SourceSSE`, `SourcePoll`, hàm `ReconnectDelays()`; `ReconcileCommand`,
  `NewReconcileCommand`, `ReconcileCommandName`; `AlertCommand`, `NewAlertCommand`, `AlertCommandName`. `live.Parts`
  (1 chỗ dùng: `conversation/wiring.go`, chữ ký giữ nguyên) nay luôn trả lệnh `command.fb.live_reconcile`, và luồng
  nền `Manager.Run` với `FBSource` khi bộ adapter có kênh `channel.LiveSource`; `commentSource` (riêng gói, trả nil)
  bị xóa; `detectorOf`/`canDetect` (riêng gói) thay bằng `adapterOf[T]`/`anyAdapter[T]` dùng chung cho nhận biết và
  đọc comment. `UpsertSession` (nhánh ended) và `EndManual` thêm ghi lệnh đối chiếu (chữ ký giữ nguyên; chỗ gọi:
  `inbound/live.go`, `detect.go` OnDone, `conversation/live.go`). Gói gốc conversation thêm
  `NewLiveAlertCommand(a, logger)` (1 chỗ dùng: `cmd/crm/main.go`, danh sách lệnh cảnh báo dùng chung Alerter).
  Đường vào comment (`inbound/comments.go`): phần live của `afterCommentInserted` tách thành `(*handler).liveComment`
  (riêng gói), thêm chỉ số `live_comment_to_signal_ms`.
  fakegraph (test): `AddLiveComment`, `SetLiveCommentTotal`, trang `limit/after` và `parent` cho `{live}/comments`,
  `Call.At`.
- Gói phụ thuộc ngược (go list): `internal/modules/conversation` ← `internal/httpapi`, `cmd/crm`;
  `internal/adapters/facebook` ← `cmd/crm`; `internal/testkit/fakegraph` ← chỉ test (facebook, conversation/…,
  channel, httpapi, cmd/crm).
- Operation OpenAPI bị chạm, oasdiff: không (không route mới).
- Chỗ dùng trên màn hình: chưa có (màn Live là Task 177; `active_source`, `meta_comment_count`, `reconciled_at` đã có
  trong bảng của Task 170).
- Câu sqlc, bảng, pha migration: không migration (mọi cột đã có ở `0409_live.sql`). Câu sqlc mới trong
  `queries/conversation/live.sql`: `SetLiveActiveSource`, `SetLiveReconciled`, `LiveCommentIDsOfSession` (mã comment
  `is_live` của phiên qua `conversation.posts`). Đọc `channel.Account`, `channel.CredentialsFor` (public API); ghi việc
  vào qua `webhooks.EnqueueInbound`, lệnh qua `queue.JobQueue`.
- Test sẽ chạy: `adapters/facebook/{live_stream_test,live_poll_test}.go`,
  `conversation/internal/live/{source_fb_test,reconcile_test}.go` cùng test cũ của gói live,
  `inbound/via_test.go` (`TestInbound_LiveStatusAndLiveComment`), `conversation/wiring_test.go` (`TestWiring`),
  `testkit/fakegraph`, `cmd/crm` `TestWireM4*`, `tests/arch`; cả gói ở ci-task (live, inbound, conversation, facebook,
  cmd/crm, httpapi theo phụ thuộc ngược).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (webhooks.EnqueueInbound,
  commands.CommandKind, alertjobs.Notify, queue.NewQueue, tenancy.WithTenant, metrics); adapter Facebook `Client.Do`,
  `newRequest`/`proof` (appsecret_proof), `decodeError`, `flexTime`, `graphComment`, `idRef`, `splitter.add`, `textKey`,
  `backfill.feed` (đọc comment thành `comment.changed`); `live.Manager`, `CommentSource`, `Session`, `UpsertSession`;
  mẫu lệnh cảnh báo của module (`channel/alert.go`, `fulfillment/track_poll.go`); fakegraph `PushLiveComment`,
  `DropStream`, `FailNext`, `liveCommentsPage`.
- Dùng lại / viết mới vì: sự kiện comment live dựng bằng `splitter.add` với đúng khóa chống trùng của webhook feed
  (`c:<id>:add:<textKey>`) để ba đường đổ về một chỗ (test so khóa với `Split` của cùng comment); lời gọi Graph đi qua
  `Client.Do`; riêng luồng SSE viết mới (`Client.Do` đọc trọn thân trong hạn 10 s, không dùng được cho luồng dài) nhưng
  dựng yêu cầu bằng `newRequest` (cùng token header, appsecret_proof) và đọc lỗi bằng `decodeError`. Lệnh cảnh báo viết
  trong `live` theo mẫu module (mỗi module một lệnh cảnh báo, không có lệnh chung). Không thêm thư viện: SSE đọc bằng
  `bufio.Scanner` (thư viện chuẩn).
- Tìm phần tùy chọn của adapter: `detectorOf`/`canDetect` (Task 170) và nhu cầu mới (đọc comment live) gộp thành một
  hàm generic riêng gói `adapterOf[T]`/`anyAdapter[T]` (jscpd báo trùng); `inbound.canFetchPosts` giống dạng nhưng ở
  phần khác của module (inbound), giữ riêng theo luật "phần của module không nhập nhau". Đọc Page + token + adapter của
  phiên là một hàm `readTarget` dùng chung cho nguồn và lệnh đối chiếu.

## Soát thiết kế

- 5.1 chủ dữ liệu: phiên live, mốc nguồn, số đối chiếu thuộc `conversation.live_sessions` (Task 170).
- 5.2 không ghi bảng module khác: comment vào qua `webhooks.EnqueueInbound` (API nền tảng), lệnh qua `queue`.
- 5.3 đọc qua public API: `channel.Account`, `channel.CredentialsFor`; tin `is_live` đọc bảng của chính module.
- 5.4 nguyên tử: lô comment ghi như Task 170 (`emit`); chuyển nguồn ghi `active_source` + lệnh cảnh báo trong một tx
  ngắn; đối chiếu ghi `meta_comment_count`, `reconciled_at`, lệnh cảnh báo trong tx `OnDone` của Runner.
- 5.5/5.6 gọi ra ngoài: luồng đọc là goroutine có chủ (khóa thuê, ngoại lệ có chủ đích của Global Constraints), không
  giữ tx lúc chờ mạng; đối chiếu là lệnh `command.fb.live_reconcile` chỉ đọc (`AutoRetryNotSent`, `Lookup` = not_sent):
  `Execute` đọc token trong tx riêng, gọi Meta ngoài tx, ghi việc vào trong tx ngắn riêng (trùng thì bỏ, chạy lại không
  hại), `OnDone` ghi số; cảnh báo là lệnh `conversation.live_alert` (Telegram gọi trong `Execute`, ngoài tx).
- 5.7 tenant: mọi câu trong `tenancy.WithTenant` của tenant phiên; không bảng mới.
- 5.8 không HTTP trong module: `live` không nhập `net/http`; SSE nằm trong adapter.
- 5.9 đường ghi: không route mới. 5.10 Valkey: không áp dụng. 5.11 thư viện: không thêm. 5.12 module mẫu: lệnh theo
  mẫu `commands`, test PostgreSQL thật + fakegraph.
- 6.1 đồng thời: một phiên chỉ một luồng (khóa thuê Task 170); ba đường (SSE, hỏi định kỳ, webhook) cùng khóa chống trùng
  `c:<comment_id>:add:<textKey>` (tầng 1 `jobs.dedupe_key`; tầng 2 khóa tự nhiên tin). Ghi `active_source` không cần
  khóa chủ (chỉ hiển thị); lô comment vẫn chốt `lease_owner = $me`.
- 6.2 gửi lại: lệnh đối chiếu chống trùng một lệnh mỗi phiên (`live-reconcile:<id>`); cảnh báo `live-source` một lần mỗi
  phiên, `live-missing` một lần mỗi lần đối chiếu.
- 6.3 thứ tự khóa (ADR-46 nhóm 6, 13): chỉ dòng `live_sessions` (một dòng) rồi `jobs`; không khóa `messages`.
- 6.4 lỗi tạm: `WithTenant` qua `db.RetryTx`, thân hàm gán lại kết quả; lệnh đối chiếu lỗi mạng → not_sent, tự chạy lại.
- 6.5 READ COMMITTED + câu có điều kiện (`status = 'live'`). 6.6 việc nền lỗi: nguồn lỗi → nối lại 1 s, 2 s, 5 s; 3 lỗi
  trong 60 s → hỏi định kỳ 2 s, thử SSE lại mỗi 5 phút; lỗi hỏi định kỳ → thử lại kỳ sau; emit lỗi (mất khóa, database)
  → nguồn dừng, bộ quản lý nhả khóa và quét lại.
- 6.7 429: hỏi định kỳ giãn ≥ 2 s mỗi phiên (cả lượt lấp chỗ hở lúc nối lại); điều tiết Graph là việc của client.
- 6.8 tiến trình chết: Task 170 (khóa hết hạn, mở lại ≤ 15 s, hỏi bù từ `last_comment_at − 5 s`); mỗi lần (nối lại)
  nguồn hỏi định kỳ một lượt từ mốc − 5 s.
- 6.9 treo lâu: mở luồng có hạn 10 s tới khi có header; 30 s không dữ liệu → hỏi định kỳ kiểm; lời gọi Graph hạn 10 s;
  lệnh đối chiếu hạn 2 phút, tối đa 1000 trang.
- 6.10 tiền: không áp dụng. 6.11 quy luật: không thêm rapid (đường đi là máy trạng thái thời gian, kiểm bằng ca có
  tên); bộ tách SSE kiểm thêm dòng dài, nhiều dòng data, dòng chú thích.
- 6.12 dữ liệu cá nhân: nội dung comment, tên, mã người chỉ đi qua `EnqueueInbound` (bản nhận), không log, không vào kết
  quả lệnh (kết quả đối chiếu chỉ có hai con số), cảnh báo chỉ mang mã phiên và con số; token gửi bằng header,
  lỗi bỏ URL.
- 6.13 quyền: không route mới; luồng nền không có người dùng.

## Bằng chứng

Lệnh chung (máy, PostgreSQL chung crm-shared 5600): `go test -race -count=1 -run '^(…)$' <gói>`; sau gộp m4 chạy lại
cả gói live (`go test -race -count=1 ./internal/modules/conversation/internal/live/` → `ok … 14.563s`). Ổn định: bốn
test thời gian chạy `-count=3`/`-count=4` đều qua sau khi sửa hai chỗ test tự đua (ghi ở "Khác kế hoạch").

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| LV02: 600 comment qua SSE, `DropStream` hai lần giữa chừng, webhook feed đưa 100 comment trong số đó → đúng 600 việc comment khác nhau, 600 việc tổng (không trùng); `active_source = sse`, 0 cảnh báo; đối chiếu `meta_comment_count = 600`, 0 `live-missing` | `live.TestLV02_StreamDropsAndWebhookStillExact` | `go test -race -count=3 -run '^(TestLV02_…)$' ./internal/modules/conversation/internal/live/` | `--- PASS` ×3 (5,2–6,6 s) |
| Comment phát lúc SSE đang rớt → lượt hỏi định kỳ lúc nối lại lấp, không trùng | cùng test (lô 4 và 8 phát trong lúc luồng rớt; chờ ghi đủ lô trước khi phát lô sau) | như trên | PASS |
| SSE 404 → sau đúng 3 lần chuyển hỏi định kỳ (lần mở thứ 4 cách lần 3 ≥ RetrySSEEvery), `active_source = poll`, đúng 1 cảnh báo `live-source` kể cả khi thử lại vẫn lỗi; vẫn đủ 600 | `live.TestStream404FallsBackToPoll` | `go test -race -count=4 -run '^(TestStream404FallsBackToPoll\|TestSilentStreamFallsBackToPoll)$' …/live/` | `ok … 21.417s` |
| Luồng im `SilenceAfter` mà hỏi định kỳ thấy comment mới → chuyển poll, 1 cảnh báo; sau `RetrySSEEvery` mở lại luồng → `active_source = sse`, không cảnh báo thêm | `live.TestSilentStreamFallsBackToPoll` | như trên | PASS ×4 |
| Yêu cầu SSE có `comment_rate`, `appsecret_proof`; hỏi định kỳ cách nhau ≥ `PollEvery` (mỗi lượt, trang sau cùng lượt không tính) | `streamAsked`, `pollGaps` trong LV02 và 404 | như trên | PASS |
| `source_mode = poll` ép: không mở luồng, không cảnh báo | `live.TestSourceModePollNeverStreams` | `go test -race -count=1 -run '^(…TestSourceModePollNeverStreams)$' …/live/` | `ok … 3.897s` |
| LV10 nguồn thật: worker 1 đọc 100 comment rồi chết (không nhả khóa) → worker 2 hỏi bù `since = last_comment_at − 5 s`, tổng đủ 200, không trùng | `live.TestLV10_RealSourceReopensAndBackfills` | `-count=3` như dòng LV02 | PASS ×3 |
| Fakegraph giữ 5 comment không qua SSE lẫn webhook → đối chiếu lấy đủ (5 việc mới); `total_count = 17`, CRM có 15 → `meta_comment_count = 17`, 1 `live-missing` 15/17; kết quả lệnh không có nội dung, mã comment; một lệnh mỗi phiên; đường vào thật biến thành 15 tin `is_live` | `live.TestReconcileFillsMissingAndAlerts` | `go test -race -count=1 -run '^(TestReconcile…\|TestEndManual…\|TestAlertCommand\|…)$' …/live/` | `ok … 3.897s` |
| Bấm tay kết thúc ghi 1 lệnh đối chiếu (bấm lại không thêm); phiên không có → rejected; Meta lỗi → not_sent; kênh không đọc được live → rejected `no_live_source` | `live.TestEndManualEnqueuesReconcileOnce`, `live.TestReconcileRejectsChannelWithoutLiveSource` | như trên; `-run '^TestReconcileRejectsChannelWithoutLiveSource$'` | `ok … 3.622s` |
| Cảnh báo `live-source`/`live-missing` mức thường, văn bản chỉ mã phiên và con số (600/605) | `live.TestAlertCommand` | như trên | PASS |
| Hằng thật (2 s, 30 s, 5 phút, 60 s, nối lại 1/2/5 s); Parts có lệnh nhận biết + đối chiếu + 1 luồng nền khi có Facebook; chỉ lệnh đối chiếu khi không có kênh live | `live.TestSourceConstantsAndParts`, `live.TestDetectCommand_PollFindsAndEndsSessions` | `go test -race -count=1 ./internal/modules/conversation/internal/live/` | `ok … 14.563s` |
| StreamComments/PollComments/CountComments (yêu cầu, trang, parent, khóa chống trùng như webhook, lỗi auth, bộ đọc SSE) | `facebook.TestStreamComments`, `TestStreamComments_OpenErrors`, `TestReadSSE`, `TestPollAndCountComments`, `TestCountComments`, `TestPollComments_SkipsInvalidAndMarksPage` | `go test -race -count=1 -run 'Live\|ReadSSE\|Stream\|Poll\|Count' ./internal/adapters/facebook/ ./internal/testkit/fakegraph/` | `ok facebook 2.386s`, `ok fakegraph 1.789s` |
| Chỉ số `live_comment_to_signal_ms` có đúng 1 mẫu cho comment live | `inbound.TestInbound_LiveStatusAndLiveComment` | `go test -race -count=1 -run '^TestInbound_LiveStatusAndLiveComment$' …/inbound/` | `ok … 3.560s` |
| Nối dây: lệnh `command.fb.live_reconcile` mọi cấu hình, 1 luồng nền khi có Facebook | `conversation.TestWiring`, `cmd/crm TestWireM4*` | `go test -count=1 -run '^TestWiring$' ./internal/modules/conversation/`; `-run TestWireM4 ./cmd/crm/` | `ok 0.839s`, `ok 1.036s` |
| Cổng kiểm của task (mọi cổng tĩnh như `make ci`, gồm `tests/arch`; test toàn bộ vì đổi testkit) | ci-task | `ci-task.sh` (từ xa, run 45790705810, điểm tách 6fcce01e) | `static: success`, `test (1)…(6): success`, `CI-TASK XANH (từ xa, …)` |
| Cổng tĩnh tự soát | golangci-lint (5 vùng gói đổi), `make jscpd` | `golangci-lint run ./internal/modules/conversation/... ./internal/adapters/facebook/... ./internal/testkit/fakegraph/... ./cmd/crm/...`; `make jscpd` | `0 issues.`; `Found 0 clones.` |

## Test bị xóa hoặc tắt

(không có) — `TestPollAndCountComments` (viết ở phiên trước của task này, chưa commit) tách phần đếm thành
`TestCountComments` cho qua gocognit; không bớt ca nào.

## Khác kế hoạch

- Đối chiếu cuối phiên đọc **mọi** comment của video (không chặn `since = started_at`): phiên theo dõi tay bắt đầu lúc
  bấm, Meta vẫn đếm comment trước đó vào `total_count` (chặn theo `started_at` sẽ báo thiếu giả). Trùng thì bỏ nên vô hại.
- Số "CRM có" khi đối chiếu = mã comment của tin `is_live` trên bài của phiên ∪ mã comment vừa đọc lại của Meta (việc
  vừa ghi chưa thành tin khi `OnDone` chạy); tính trong `Execute`, kết quả lệnh chỉ hai con số.
- Lệnh `command.fb.live_reconcile` đăng ký ở mọi cấu hình (kế hoạch không nói): mọi phiên kết thúc đều ghi lệnh; kênh
  không đọc được live (kênh giả) → rejected `no_live_source`, không treo chờ không có handler.
- Lệnh cảnh báo `conversation.live_alert` đăng ký ở `cmd/crm/main.go` cùng các lệnh cảnh báo khác (cần Alerter, `wireM4`
  không có Alerter), qua `conversation.NewLiveAlertCommand`.
- Số lần hỏi định kỳ lỗi liền tối đa 30 → nguồn trả lỗi để bộ quản lý nhả khóa, quét lại và đọc lại token (token đổi
  giữa phiên). Kế hoạch không nêu.
- Thứ tự làm: phần adapter (live_stream, live_poll) có test trước ở phiên trước; phần `internal/live` ở phiên tiếp tục
  viết mã rồi mới viết test cùng lượt (không có bước đỏ ghi lại riêng cho FBSource, đối chiếu, cảnh báo).
- Hai test tự đua đã sửa (không phải lỗi mã): `TestSilentStreamFallsBackToPoll` chờ lượt lấp chỗ hở lúc mở luồng xong
  rồi mới thêm comment; `TestStream404FallsBackToPoll` kiểm "chuyển sau đúng 3 lần" bằng khoảng giữa lần mở 3 và 4.
- `tests/arch` chạy trên máy hết 600 s (kẹt ở `TestLintNotBlockedByOtherRunners`, chờ lint của phiên khác khi máy tải
  ~11–14); ci-task từ xa đã chạy và xanh.
- LV02 kiểm "600 comment khác nhau" ở tầng 1 chống trùng (việc vào `jobs`, khóa `c:<id>:add:<textKey>` mà ba đường
  dùng chung) thay vì chạy đường vào cho 600 tin; việc vào thành tin `is_live` (tầng 2, khóa tự nhiên tin) kiểm bằng
  đường vào thật ở `TestReconcileFillsMissingAndAlerts` (10 rồi 15 tin `is_live`).

## Quyết định thay chủ shop

- Đối chiếu đọc hết comment của video thay vì từ `started_at` (lý do ở trên). Phương án khác: theo đúng chữ kế hoạch
  (`since = started_at − 5 s`) — đổi lại dễ (một tham số trong `reconcile.go`).
- `live-missing` một lần mỗi lệnh đối chiếu (khóa `live-missing:<phiên>:<lệnh>`; một lệnh mỗi phiên nên thực tế một lần
  mỗi phiên); `live-source` một lần mỗi phiên (khóa `live-source:<phiên>`), không báo lại khi thử luồng lại lỗi.
- "Comment mới" khi luồng im = comment hỏi định kỳ thấy có `created_time` mới hơn mọi comment đã thấy qua bất kỳ đường
  nào (tránh chuyển nhầm vì lượt lấp chỗ hở đọc lại comment cũ). Phương án khác: so tập mã comment (tốn bộ nhớ phiên dài).

## Việc phát hiện thêm

- Tham số luồng SSE (`comment_rate=ten_per_second`, quyền) và cách Meta đặt mã bài của video live vẫn chưa xác minh với
  Meta thật (E4, như kế hoạch ghi).
- `queue.Worker` chặn hạn một lần chạy lệnh ở nửa thời gian giữ chỗ (45 s): hạn 2 phút của lệnh đối chiếu thực tế là
  45 s (như `cash.reconcile`). Phiên rất dài (hàng chục nghìn comment) có thể cần tách lệnh theo trang.
- (Reviewer) Chỉ số `live_comment_to_signal_ms` cũng ghi mẫu cho comment cũ được lấp lại (đối chiếu cuối phiên, hỏi bù
  sau khi tiến trình chết), sẽ kéo p95 lên khi đo ở E4: lọc theo `via` hay chỉ đo trong lúc phiên live khi đọc số.
- (Reviewer) `live-missing` lấy "CRM có" = tin đã có ∪ comment vừa đọc lại, nên thực tế chỉ báo khi Meta đếm
  (`total_count`) nhiều hơn số Meta liệt kê được (comment bị ẩn, xóa, chặn) — chủ shop đọc cảnh báo theo nghĩa đó.

## Reviewer

Kết luận: ĐẠT (agent reviewer, 26/11, trên phần đã stage sau khi gộp m4 6fcce01e; 28 tệp).

- Phát hiện phải sửa: không có. Reviewer đã kiểm: goroutine đọc SSE có chủ (`Close` chờ `wg`), token chỉ ở header và
  bị che trong lỗi, lệnh đối chiếu gán lại biến trong `WithTenant`, không giữ tx khi gọi Meta, `OnDone` khóa
  `live_sessions` rồi `jobs` (ADR-46), lệnh đối chiếu chỉ ghi khi phiên thật sự chuyển ended, không SQL ngoài sqlc,
  không sửa tay mã sinh, log/cảnh báo không có dữ liệu cá nhân.
- Ảnh hưởng: khớp; `facebook.ReadSSE` (cửa xuất cho test) không nêu, không ảnh hưởng mã chạy.
- Bảng bằng chứng: mọi dòng trỏ tới test có thật và kiểm đúng tiêu chí; LV02 kiểm ở tầng việc vào (đã ghi ở "Khác kế
  hoạch").
- Ghi chú không bắt buộc (không sửa, ghi vào "Việc phát hiện thêm"): `live_comment_to_signal_ms` có cả mẫu của comment
  lấp lại; `live-missing` thực tế chỉ báo khi Meta đếm nhiều hơn số liệt kê được; `headerTimer` kích đúng lúc header về
  chỉ thành một lần luồng lỗi rồi nối lại.
