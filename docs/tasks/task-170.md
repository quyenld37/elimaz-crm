# Task 170 — Phiên live — nhận biết, khóa thuê, bộ dò, mở lại luồng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 170 = M4-30). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào đã có. `live.Parts` (khung Task 143, 1 chỗ
  dùng: `conversation/wiring.go`) giữ chữ ký, nay trả lệnh `command.fb.live_detect` + lịch `live.detect` khi bộ adapter
  có kênh nhận biết được live, và luồng nền `Manager.Run` khi có nguồn comment (Task 171 cắm nguồn Facebook). Thêm mới:
  gói gốc `conversation`: kiểu `LiveSession`, `ListLiveSessions`, `StartLiveManually`, `EndLiveManually`, sentinel
  `ErrLiveVideoInvalid`, `ErrLiveStatusInvalid`, hằng `LiveStatusLive`, `LiveStatusEnded`; phần `internal/live`: `Session`,
  `Observation`, `StateOf`, `UpsertSession` (nhánh ended gọi câu sqlc `EndLiveSessionByVideo`), `IsLiveComment`,
  `OnComment`, `ParseVideoRef`, `StartManual`, `EndManual`, `List`, `Manager`, `Options`, `CommentSource`,
  `ErrLeaseLost`, `MaxReopenDelay`, `DetectCommand`, `NewDetectCommand`, `DetectScheduleFor`, hằng `LeaseTTL`,
  `RenewEvery`, `ScanEvery`, `Overlap`;
  `inbound.LiveStatus` (tệp `inbound/live.go`, Task 153 nối một nhánh `case channel.EventLiveStatus` trong bộ phân
  phát); adapter Facebook: `(*Channel).DetectLive`, `(*Channel).LiveStatus` (tệp `live_detect.go`). Lỗi HTTP: thêm
  dòng vào `errors_conversation.go` (`ErrLiveVideoInvalid` → 400 `invalid_input`), không mã lỗi mới.
- Gói phụ thuộc ngược (go list): `internal/modules/conversation` ← `internal/httpapi`, `cmd/crm`;
  `internal/adapters/facebook` ← `cmd/crm`. Phần `internal/live`, `internal/inbound` chỉ được gói gốc conversation nhập.
- Operation OpenAPI bị chạm, oasdiff: hiện thực 3 operation đã khai ở Task 143 (`startLiveSessionManually`,
  `endLiveSessionManually`, `listLiveSessions`), xóa 3 dòng khỏi `tests/arch/testdata/m4_pending.txt`. Không sửa
  `openapi.yaml` → oasdiff không đổi.
- Chỗ dùng trên màn hình: chưa có (màn Live là Task 177).
- Câu sqlc, bảng, pha migration: migration mới `0409_live.sql` (pha mở rộng: CREATE TABLE `conversation.live_sessions`,
  index, RLS, GRANT; khóa ngoại ghép tới `channel.channel_accounts`, `conversation.posts`). Câu sqlc mới
  `queries/conversation/live.sql` (sinh `conversation/internal/store/live.sql.go`). Đọc `channel.Account`,
  `channel.CredentialsFor`, `channel.ListAccounts` (public API của channel); ghi `platform.jobs`/bản nhận qua
  `webhooks.EnqueueInbound`.
- Test sẽ chạy: `conversation/internal/live/{sessions_test,manager_test,lease_test,detect_test}.go`,
  `adapters/facebook/live_detect_test.go`, `httpapi/handlers_live_sessions_test.go` (ba hàm gốc `StartLiveManually`, `EndLiveManually`,
  `ListLiveSessions` là lớp bọc một dòng, kiểm qua test HTTP này),
  `TestM4Contract_StatusCodes`, `TestDomainErrorSnapshot*`, `TestWiringEmpty`, `TestWireM4_Frame`, `tests/arch`
  (`TestSchemaRules`, ranh giới).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (queue `SKIP LOCKED`,
  schedules, commands, webhooks.EnqueueInbound, realtime, metrics, tenancy.ListTenantIDs/WithTenant);
  `access.Require`, `access.VisiblePages`, `access.PageScope`; `channel.Account`, `channel.ListAccounts`,
  `channel.CredentialsFor`, `channel.LiveSource`, `channel.LiveVideo`, `channel.LiveStatusPayload`; adapter Facebook
  `client.Do`, `flexTime`; `httpapi.refusableWrite`, `catalogRead`; `testkit.ChannelAccount`, `fakegraph.AddLiveVideo`,
  `SetLiveStatus`.
- Dùng lại / viết mới vì: dùng lại toàn bộ các hàm trên. Khóa thuê viết mới trong `live` (không có khóa thuê dùng
  chung: giữ chỗ của queue gắn với bảng `platform.jobs`, không dùng cho dòng nghiệp vụ). Bộ dò không tự chạy
  `time.Ticker` cho việc định kỳ trong database: nhận biết live là lịch + lệnh; riêng vòng quét khóa thuê là luồng nền
  chủ ý (Global Constraints M4 "Luồng đọc comment live là ngoại lệ có chủ đích"). Lệnh nhận biết chỉ cần hai hàm của
  `channel.LiveSource` nên kiểm bằng interface nhỏ trong `live` (adapter chưa có đủ năm hàm tới Task 171).

## Soát thiết kế

- 5.1 chủ dữ liệu: phiên live thuộc `conversation` (spec 04 mục 3), bảng `conversation.live_sessions`.
- 5.2 không ghi bảng module khác: chỉ ghi `live_sessions`; việc vào ghi qua `webhooks.EnqueueInbound` (API nền tảng).
- 5.3 đọc qua public API của channel (`Account`, `ListAccounts`, `CredentialsFor`), không đọc bảng `channel`.
- 5.4 nguyên tử: mỗi lô comment: các việc vào + cập nhật `last_comment_at`, `received_count` trong một transaction ngắn,
  có điều kiện `lease_owner = $me` (mất khóa → lùi cả lô).
- 5.5/5.6 gọi ra ngoài: nhận biết live là lệnh `command.fb.live_detect` (chỉ đọc, `AutoRetryNotSent`, `Lookup` trả
  `not_sent`): `Execute` đọc token trong tx riêng rồi gọi Meta ngoài transaction, kết quả ghi ở `OnDone` (tx của Runner).
  Luồng đọc comment là goroutine có chủ (khóa thuê), không giữ transaction lúc chờ mạng.
- 5.7 tenant: bảng mới có `tenant_id`, RLS ép, UNIQUE (tenant_id, id), khóa ngoại ghép; bộ quét chạy từng tenant trong
  `tenancy.WithTenant` (`tenancy.ListTenantIDs`).
- 5.8 không HTTP trong module: đúng; handler mỏng `handlers_live_sessions.go`; tách mã video từ URL bằng chuỗi (không
  `net/url`).
- 5.9 đường ghi: 2 route ghi đã có `Idempotency-Key`, `x-permission: live.operate` từ Task 143.
- 5.10 Valkey: không áp dụng. 5.11 thư viện: không thêm. 5.12 module mẫu: public API ở gói gốc, sqlc, test PostgreSQL thật.
- 6.1 đồng thời: nhận phiên bằng `UPDATE … WHERE id IN (SELECT … FOR UPDATE SKIP LOCKED)` chỉ lấy phiên `live` hết
  khóa; gia hạn, ghi lô, nhả đều `WHERE lease_owner = $me`. `UpsertSession` là `INSERT … ON CONFLICT DO UPDATE` chỉ đi
  `live → ended`. Kết thúc tay: `UPDATE … WHERE status = 'live'`.
- 6.2 gửi lại: Idempotency-Key của M0; bấm theo dõi tay hai lần cùng mã → cùng một phiên (khóa tự nhiên UNIQUE);
  comment trùng → `EnqueueInbound` không tạo việc, không đếm.
- 6.3 thứ tự khóa (ADR-46 nhóm 6): mỗi transaction của phần live chỉ khóa dòng `live_sessions` (một dòng, hoặc nhiều
  dòng theo `ORDER BY id` khi nhận phiên) rồi `jobs` (nhóm 13); không khóa `conversations`, `messages`.
- 6.4 lỗi tạm: `tenancy.WithTenant` chạy qua `db.RetryTx`; thân hàm gán lại biến kết quả mỗi lần.
- 6.5 READ COMMITTED + câu có điều kiện. 6.6 việc nền lỗi: lệnh nhận biết lỗi theo Page → bỏ Page đó lượt này (kỳ
  sau 30 s hỏi lại), không `in_doubt`; lệnh hết hạn sau 60 s.
- 6.7 429: lỗi của Meta ở một Page chỉ bỏ Page đó lượt này; điều tiết Graph là việc của client adapter (Task 146).
- 6.8 tiến trình chết: khóa thuê hết hạn sau `LeaseTTL` (10 s), bộ quét 5 s nhận lại, hỏi bù từ
  `last_comment_at − 5 s` (LV10, ≤ 15 s); tắt êm nhả khóa ngay.
- 6.9 treo lâu: lệnh nhận biết timeout 20 s; transaction ghi lô ngắn; luồng tự dừng khi quá hạn khóa mà chưa gia hạn
  được (tránh hai worker cùng đọc).
- 6.10 tiền: không áp dụng. 6.11 quy luật: rapid — chuỗi sự kiện trạng thái ngẫu nhiên → phiên `ended` khi và chỉ khi
  có ít nhất một sự kiện kết thúc, luôn đúng 1 dòng mỗi video.
- 6.12 dữ liệu cá nhân: phiên live không có dữ liệu cá nhân; nội dung comment chỉ đi qua `EnqueueInbound`, không log.
- 6.13 quyền: theo dõi/kết thúc tay cần `live.operate` trên Page (không có → 404); danh sách lọc theo Page được đọc
  (`conversation.read`).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| LV01 webhook: `live.status = LIVE` → 1 phiên `live`, `detected_by webhook`; trong một chu kỳ quét nguồn nhận `Run` (since = started_at); hỏi định kỳ cùng thấy → vẫn 1 phiên; `LIVE_STOPPED` → `ended`, luồng dừng trong một chu kỳ gia hạn, khóa nhả; `LIVE` sau `ended` → vẫn `ended`; Page lạ, trạng thái lên lịch, dữ liệu hỏng | `live.TestLV01_WebhookDetectsAndStreamOpens` | `go test -race -count=1 -run '^(TestLV01_WebhookDetectsAndStreamOpens\|TestStartAndEndManual\|TestParseVideoRef\|TestIsLiveCommentAndSignal\|TestUpsertSession_StatusOnlyMovesForward)$' ./internal/modules/conversation/internal/live/` | `ok … live 6.197s` |
| LV01 hỏi định kỳ: video LIVE của kênh → phiên `detected_by poll` (đủ mã video, tiêu đề, giờ bắt đầu); trùng webhook → 1 phiên, giữ `webhook`; kênh lỗi → bỏ lượt; phiên không còn trong danh sách và kênh báo `LIVE_STOPPED` → `ended`; lệnh chỉ đọc (`AutoRetryNotSent`, `Lookup` = not_sent); lịch `live.detect` 30 s theo tenant | `live.TestDetectCommand_PollFindsAndEndsSessions` | `go test -race -count=1 -run '^TestDetectCommand_PollFindsAndEndsSessions$' ./internal/modules/conversation/internal/live/` | `ok … live 3.103s` |
| LV01 adapter: `GET {page}/live_videos?broadcast_status=["LIVE"]&fields=id,status,video{id},permalink_url,title,creation_time`, chỉ trả LIVE; `LiveStatus` đọc `LIVE_STOPPED`; token hỏng → lớp auth | `facebook.TestDetectLiveAndStatus` | `go test -race -count=1 -run '^TestDetectLiveAndStatus$' ./internal/adapters/facebook/` | `ok … facebook 1.660s` |
| LV01 bấm tay: URL `…/videos/123456789/` → phiên `123456789` `manual`; bấm lại → cùng phiên; không quyền, Page lạ → 404; chuỗi không mã → 400; kết thúc tay hai lần → cùng `ended`, version 2; danh sách theo Page, lọc trạng thái, since | `live.TestStartAndEndManual`, `live.TestParseVideoRef`, `httpapi.TestLiveSessionsAPI` | dòng LV01; `go test -race -count=1 -run '^(TestLiveSessionsAPI\|TestM4Contract_StatusCodes\|TestDomainErrorSnapshot.*)$' ./internal/httpapi/` | `ok … live`; `ok … httpapi 7.323s` (sau gộp m4) |
| LV10: worker 1 đọc 100 comment, chết không nhả khóa → worker 2 nhận trong ≤ `LeaseTTL + ScanEvery` (1,5 s thu nhỏ, +300 ms độ trễ database), `since = last_comment_at − 5 s`; nguồn 200 comment → đúng 200 việc vào khác nhau, `received_count = 200` (comment trùng do hỏi bù không đếm), không hai `Run` chồng nhau | `live.TestLV10_CrashedWorkerReopensWithin15s` | `go test -race -count=3 -run '^(TestTwoWorkersNeverReadSameSession\|TestLV10_CrashedWorkerReopensWithin15s)$' ./internal/modules/conversation/internal/live/` | `ok … live 12.694s` |
| LV10 công thức với hằng số thật: mở lại ≤ 15 s; gia hạn ≥ 3 lần mỗi khóa; hỏi bù 5 s | `live.TestLeaseConstants` | `go test -race -count=1 -run '^(TestLV10_CrashedWorkerReopensWithin15s\|TestGracefulStopReleasesLease\|TestTwoWorkersNeverReadSameSession\|TestLeaseConstants)$' ./internal/modules/conversation/internal/live/` | `ok … live 4.902s` |
| Tắt êm → khóa nhả ngay; worker 2 nhận trong ≤ `ScanEvery` | `live.TestGracefulStopReleasesLease` | như dòng trên | `ok` |
| 04 mục 6, 18: hai worker quét song song ~50 lần (quét 50 ms, khóa 1 s, gia hạn 300 ms) trên 3 phiên → mỗi phiên đúng 1 luồng, 1 chủ, không `Run` chồng nhau; ép mất khóa → luồng dừng (`live_lease_lost`), khóa lạ hết hạn → nhận lại | `live.TestTwoWorkersNeverReadSameSession` | `go test -race -count=6 -run '^(TestTwoWorkersNeverReadSameSession\|TestLV10_CrashedWorkerReopensWithin15s\|TestGracefulStopReleasesLease\|TestLV01_WebhookDetectsAndStreamOpens)$' ./internal/modules/conversation/internal/live/`; `go test -race -count=3 ./internal/modules/conversation/internal/live/` | `ok … live 34.338s`; `ok … live 17.745s` (sau khi sửa bộ hẹn giờ, máy đang tải nặng) |
| Quy luật (rapid): chuỗi ngẫu nhiên lần thấy live/ended của 3 đường → 1 phiên mỗi video, `ended` khi và chỉ khi có lần kết thúc sau lần live, không đi ngược | `live.TestUpsertSession_StatusOnlyMovesForward` | dòng LV01 | `ok` |
| `IsLiveComment` khớp mã bài/live/video đúng Page; `OnComment` phát `live.comments` theo Page | `live.TestIsLiveCommentAndSignal` | dòng LV01 | `ok` |
| Cả gói conversation (gốc + internal) với `-race` | mọi test của gói | `go test -race -count=1 ./internal/modules/conversation/...` | `ok … conversation 49.017s`, `ok … live 12.220s` |
| Nối module, kiến trúc | `TestWireM4_Frame`, `tests/arch` | `go test -count=1 -run TestWireM4 ./cmd/crm/`; `go test -count=1 -v -timeout 15m ./tests/arch/` | `ok … cmd/crm 1.182s` (sau gộp m4); `ok … tests/arch 504.162s` (máy bận) |
| Cổng của task (cổng tĩnh + toàn bộ test trừ e2e, vì phần gộp m4 đổi testkit) | ci-task | ci-task (lần 3, sau khi sửa bộ hẹn giờ khóa thuê) | `CI-TASK XANH (so với điểm tách nhánh aafa08780b8b…)`, test 553 giây. Lần 1 đỏ `gen-check` (chưa stage); lần 2 đỏ `TestTwoWorkersNeverReadSameSession` (lỗi thật, đã sửa, Quyết định 9) và `platform/cache` `TestACL`, `TestReadCache_SingleFlight` (Valkey quá tải, không thuộc task; lần 3 qua). Lần 4 sau review (thêm chỉ số `read_accounts`): `CI-TASK XANH (so với điểm tách nhánh aafa08780b8b…)`, test phần đổi từ lần xanh trước |

## Quyết định thay chủ shop

1. **Đếm "200 tin `is_live`" bằng việc vào `inbound.fakechat` khác nhau** (và `received_count`), không bằng dòng
   `messages`: comment vào thành tin là Task 154, chưa có. Vì: đúng phần Task 170 sở hữu (lô comment → việc vào chống
   trùng); Task 154 có test riêng cho tin. Phương án khác: chờ 154. Đảo ngược: thêm một câu đếm ở test khi 154 gộp.
2. **`inbound.LiveStatus` viết sẵn, chưa nối vào bộ phân phát sự kiện vào**: bộ phân phát (`inbound/handler.go`) là
   tệp của Task 153 chưa có trên m4; không được sửa tệp của 153. Nối là một dòng `case channel.EventLiveStatus` (ghi ở
   Việc phát hiện thêm). Test gọi thẳng hàm.
3. **Bộ quản lý luồng chưa chạy trong worker** tới khi có nguồn comment thật (Task 171): `live.Parts` chỉ thêm
   `Manager.Run` khi `commentSource` khác nil (nay luôn nil). Vì: nhận khóa mà không đọc được thì chỉ giữ phiên vô ích.
   Lệnh, lịch nhận biết chỉ đăng ký khi bộ adapter có kênh nhận biết được live (adapter Facebook vào bộ adapter khi Task
   153 bật `Adapter`). Đảo ngược: Task 171 trả nguồn trong `commentSource`.
4. **Kênh báo kết thúc một phiên chưa biết → bỏ qua** (không tạo phiên `ended`); trạng thái chưa phát
   (`SCHEDULED_*`, `UNPUBLISHED`) bỏ qua; `LIVE_STOPPED`, `VOD`, `PROCESSING` coi là kết thúc. Phương án khác: ghi phiên
   ended để màn Live thấy lịch sử. Đảo ngược: một nhánh trong `UpsertSession`, hàm `StateOf`.
5. **Theo dõi tay dùng mã nhập cho cả `live_video_id` và `video_id`**; trùng phiên đã có theo một trong hai mã thì trả
   phiên đó. Cách Meta đặt mã (E4) chưa xác minh.
6. **Không ghi audit cho theo dõi/kết thúc tay** (thao tác vận hành như nhắc việc Task 169, không đổi cài đặt). Đảo
   ngược: thêm `identity.RecordAudit` trong hai hàm.
7. **Khóa ngoại `live_sessions → channel.channel_accounts`** có ngay (bảng 0401 đã có trên m4, khác 0402 phải hoãn).
8. **Không có `live.operate` ở Page nào → 403 `forbidden`** (guard M0 theo `x-permission`); có ở Page khác hay Page lạ →
   404 (PQ04). Đúng hành vi chung của route M4.
9. **Luồng tự dừng khi quá `LeaseTTL − RenewEvery/2` kể từ lúc bắt đầu lần gia hạn thành công gần nhất** (đồng hồ máy,
   sớm hơn hạn database ghi), bằng một bộ hẹn giờ độc lập (`time.AfterFunc`) nên vẫn dừng đúng hạn khi lần gia hạn đang
   kẹt chờ database; mốc khóa lấy trước transaction nhận phiên. Ghi lô comment còn chốt thêm `lease_owner = $me` nên
   luồng cũ không ghi được sau khi worker khác nhận. (Lần ci-task đầu bắt được lỗi: bản trước chỉ kiểm hạn giữa hai lần
   gia hạn.)

## Khác kế hoạch

- Thứ tự "test thất bại trước": mã phần live viết trước rồi viết test ngay sau trong cùng phiên; không ghi được lần
  chạy FAIL của Step 2. Mọi test đã chạy và qua trên mã thật.
- Ngưỡng thời gian LV10 và tắt êm cộng 300 ms cho độ trễ database lúc chạy `-race` trên máy dùng chung (công thức với
  hằng số thật kiểm riêng ở `TestLeaseConstants`).
- "200 tin `is_live`" đếm bằng việc vào (Quyết định 1).
- Test hai worker dùng hằng số thu nhỏ 10 lần như các test khác (chỉ quét dày 50 ms); bản đầu dùng hằng số thu nhỏ
  ~30 lần và gãy trên máy dùng chung đang tải nặng (khoảng dư 40 ms nhỏ hơn độ trễ lập lịch).

## Việc phát hiện thêm

- Task 153/154: thêm `case channel.EventLiveStatus: return LiveStatus(ctx, tx, h.source, e)` vào bộ phân phát
  `inbound/handler.go`; comment vào (Task 154) gọi `live.IsLiveComment` để đánh dấu `is_live` và `live.OnComment` để
  báo hiệu.
- Task 171: trả nguồn Facebook trong `live.commentSource`; nguồn ghi `active_source`.
- E4: nếu Meta dùng mã khác nhau cho live video và video, theo dõi tay bằng mã video rồi webhook về với mã live có thể
  ra hai phiên; xem lại khi có dữ liệu thật.
- `fakegraph` bỏ qua `broadcast_status` và không trả `creation_time`; adapter tự lọc LIVE.

## Test bị xóa hoặc tắt

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 2).

- Lượt 1: CẦN SỬA — mã đúng, không lỗi tính đúng/an toàn; mục Ảnh hưởng nêu `EndByVideo` (không có) và
  `conversation/live_test.go` (không có). Đã sửa hai dòng; thêm chỉ số `live_detect_failed{read_accounts}` khi lệnh dò
  không đọc được Page từ database (ghi chú không bắt buộc của reviewer).
- Lượt 2: ĐẠT, không phát hiện phải sửa. Ghi chú không bắt buộc còn lại: phiên theo dõi tay có thể không tự kết thúc
  nếu Meta dùng mã live khác mã video (E4, đã ghi ở Việc phát hiện thêm, xem lại ở Task 171); hai test LV10 và tắt êm
  lấy ngưỡng giờ thật (+300 ms) theo kế hoạch, có thể chập chờn trên máy tải nặng.
