# Task 179 — Callback xóa dữ liệu của Meta, lịch nhắc Data Use Checkup, hồ sơ App Review

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 179 = M4-39). Loại việc (quy trình mục 3): A.
Đọc: spec 02 mục 4, 5; Q30 của kế hoạch M4; ADR-46 nhóm 3, 13; `docs/tasks/task-143.md`, `task-149.md`. Nhánh gốc `m4`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (grep/LSP):
  - **Đổi chữ ký** `channel.ExchangeCode` (trả `OAuthLogin{Token, Pages, AppUserID}` thay cho hai giá trị) và
    `channel.SaveOAuthPages` (nhận `OAuthLogin` thay cho `userToken, pages`): chỗ gọi `internal/httpapi/handlers_channel_connect.go`
    (2), `internal/modules/channel/oauth_test.go` (4), không chỗ nào khác.
  - **Thêm phương thức** `AppUserID(ctx, userToken)` vào interface `channel.Connector`: hiện thực `facebook.Channel`
    (`GET me?fields=id`), `channeltest.ConnectorAdapter`; `contract_test.go` của facebook kiểm khớp interface.
  - Mới: `channel.DeleteOAuthDataFor`, `channel.DataUseCheckupSchedule`, `channel.NewDataUseCheckupCommand`;
    `facebook.ParseSignedRequest`, `facebook.ErrSignedRequest`, `facebook.ErrNoUserID`; gói mới `internal/platform/datadeletion`
    (`UserRefHash`, `Processor`, `Endpoint`, `Spool`, `Drainer`); `observability.AlertSentWithin`;
    `tenancy.SetTenant` (đổi `app.tenant_id` trong transaction đang mở, cho việc nền toàn hệ thống);
    `config.MetaConfig.DataUseCheckupDue` (`META_DATA_USE_CHECKUP_DUE`, worker, tùy chọn).
  - `deletionParts(d)` (tệp khung) vẫn trả `parts{}`: lịch `channel.data_use_checkup` và lệnh của nó đăng ký ở
    `cmd/crm` (`alertSchedules`); lịch `platform.data_deletion` ở `runWorker`. `alertjobs.NewScheduledCommand` mới (xuất
    hàm dựng `scheduled` có sẵn).
- Gói phụ thuộc ngược (`go list`): `internal/modules/channel` ← `cmd/crm`, `internal/adapters/facebook`,
  `internal/adapters/fake`, `internal/httpapi`, `internal/modules/conversation` (+ `internal/deps`), `channeltest`.
  `tenancy`, `observability`, `config` được gần như mọi gói nhập (chỉ thêm, không đổi gì có sẵn).
- Operation OpenAPI bị chạm, oasdiff: không (đường mới ở vai trò ingest `/hooks/facebook/data-deletion`, ngoài hợp đồng
  `openapi.yaml`, như `/hooks/*`).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: migration mới `0411_data_deletion.sql`, pha mở rộng: `CREATE TABLE
  platform.data_deletion_requests` (không RLS, không `tenant_id`), `ALTER TABLE channel.oauth_sessions ADD COLUMN
  fb_user_ref_hash text` (thêm cột, có thể null), chỉ mục một phần, GRANT. Câu mới `queries/platform/data_deletion.sql`
  (chèn bỏ trùng, đọc theo mã, đọc yêu cầu chưa xong, đánh dấu xong có điều kiện); `queries/channel/oauth.sql`:
  `InsertOAuthSession` thêm cột băm, câu mới `DeleteOAuthSessionsByUserRef`.
- Test sẽ chạy: `go test -race` của `internal/platform/datadeletion` (mới), `internal/modules/channel` (gói nhỏ),
  `internal/adapters/facebook` (`-run` test mới + contract), `-run` test của `internal/httpapi` cho kết nối Page
  (`TestChannelConnect*`), `internal/platform/config` (`-run` test mới), `internal/platform/tenancy` (`-run` test mới),
  `cmd/crm` (`TestWireM4*`, test ingest), `tests/arch` (`lockorder` với bảng mới); cả bộ theo phạm vi ở ci-task.

## Tái dùng

- Đã kiểm trong `docs/danh-muc-dung-chung.md` và tìm theo tên, theo việc: `webhooks.Spool`/`Drainer` (tệp tạm của
  webhook), `observability.Alerter` + `alertjobs.Notify` (gửi cảnh báo từ lệnh), `alertjobs` (lịch chạy bằng lệnh),
  `tenancy.ListTenantIDs`, `tenancy.WithTenant`, `schedules.Schedule`, `facebook` `VerifySignature` (HMAC của webhook),
  `logging.Secret`, `platform/internal/store.SetTxConfig`.
- Dùng lại / viết mới vì:
  - Dùng lại `alertjobs.Notify` (cảnh báo), Alerter thật (chống lặp 10 phút) + `AlertSentWithin` mới trên câu sqlc có sẵn
    `AlertSentWithin` (chống lặp 7 ngày dựa trên `platform.alert_events` — không thêm bảng).
  - Dùng lại `store.SetTxConfig` cho `tenancy.SetTenant` (cùng câu `set_config(…, true)` mà `WithTenant` dùng).
  - Tệp tạm: **viết mới** nhỏ trong `datadeletion` thay vì dùng `webhooks.Spool`: `Spool.Drain` của webhook nạp thẳng
    vào `platform.webhook_deliveries` (rồi việc tách gói theo nguồn), không hợp với một yêu cầu xóa; tổng quát hóa
    `webhooks.Spool` là đổi cấu trúc vùng rủi ro cao ngoài phạm vi. Bản mới chỉ ghi tên tạm → fsync → đổi tên.
  - Kiểm `signed_request`: viết mới trong `facebook/deletion.go` (định dạng khác chữ ký webhook: `sig.payload` base64url,
    HMAC trên phần payload đã mã hóa); dùng `hmac.Equal` như `signature.go`.

## Soát thiết kế

- 5.1 Chủ dữ liệu: `data_deletion_requests` là bảng nền tảng (`platform`, kế hoạch Interfaces; không thuộc shop nào vì
  Meta không cho biết shop). Băm mã người dùng app nằm ở `channel.oauth_sessions` (module `channel`).
- 5.2 Không ghi bảng module khác: việc nền của `datadeletion` không xóa `channel.oauth_sessions` trực tiếp; nó gọi
  `channel.DeleteOAuthDataFor` được `cmd/crm` đăng ký (platform không nhập module).
- 5.3 Không đọc bảng module khác: không áp dụng (không đọc chéo).
- 5.4 Nguyên tử: một lượt (tới 100 yêu cầu) = một transaction: xóa phiên của mọi yêu cầu ở mọi tenant trước, rồi mới
  đánh dấu `completed_at` cả lô (ADR-46: nhóm 3 trước, nhóm 13 luôn cuối).
- 5.5 Sau commit: cảnh báo Data Use Checkup gửi trong lệnh (ngoài transaction), không gửi trong tx.
- 5.6 Gọi ra ngoài: `GET me?fields=id` chạy cùng lúc đổi mã (ngoài transaction, như `ExchangeCode` có sẵn, chỉ đọc,
  không cần `in_doubt`). Telegram qua `alertjobs.Notify` (lệnh, `not_sent` thì tự gửi lại; Alerter chống lặp).
- 5.7 Ngữ cảnh shop: xóa phiên chạy với `app.tenant_id` của từng tenant (`tenancy.SetTenant` trong transaction của việc
  không tenant), sau cùng trả về rỗng trước câu đánh dấu xong. Bảng mới không có `tenant_id` nên không RLS.
- 5.8 Nghiệp vụ không biết HTTP: `channel` không nhập `net/http`; phần HTTP ở `platform/datadeletion` (như
  `platform/webhooks`) và `adapters/facebook`.
- 5.9 Đường ghi có khóa chống trùng, quyền: `POST /hooks/facebook/data-deletion` là điểm công khai của Meta (như
  `/hooks/*`), xác thực bằng chữ ký HMAC; mã xác nhận ngẫu nhiên 16 byte là khóa chính (`ON CONFLICT DO NOTHING` khi
  nạp tệp tạm). Không qua `openapi.yaml`.
- 5.10 PostgreSQL là nguồn sự thật: có.
- 5.11 Thư viện: không thêm (chỉ thư viện chuẩn `crypto/hmac`, `crypto/sha256`, `encoding/base64`).
- 5.12 Module mẫu: lịch bằng lệnh theo `alertjobs`; tệp khung `deletion.go` của Task 143; câu SQL qua sqlc.
- 6.1 Đồng thời: hai việc `platform.data_deletion` chồng nhau cùng xóa một phiên: câu xóa thứ hai chờ khóa dòng rồi xóa
  0 dòng; câu đánh dấu `WHERE completed_at IS NULL` chỉ một lần thắng. Đọc danh sách yêu cầu không khóa (khóa nhóm 13
  trước nhóm 3 sẽ ngược ADR-46).
- 6.2 Gửi lại: Meta gửi lại callback → yêu cầu mới, mã mới (Meta lưu mã mới nhất); xóa chạy lại được.
- 6.3 Thứ tự khóa: `channel.oauth_sessions` (nhóm 3) → `platform.data_deletion_requests` (đầu nhóm 13), khớp dòng
  ADR-46 có sẵn; `lockorder_test` kiểm bảng mới có tên trong ADR.
- 6.4 Lỗi tạm: handler việc nền chạy trong tx của hàng đợi (lỗi → hẹn chạy lại); POST: database lỗi → tệp tạm, vẫn trả
  mã cho Meta; ghi tệp tạm cũng lỗi → 503 (Meta gọi lại).
- 6.5 Cách ly: READ COMMITTED mặc định.
- 6.6 Việc nền lỗi: lịch thử lại có sẵn của hàng đợi; yêu cầu chưa xong ở kỳ sau vẫn được lấy lại.
- 6.7 429: Telegram 429 qua `alertjobs.Notify` (RetryAfter).
- 6.8 Chết giữa chừng: transaction lùi, kỳ sau làm lại (xóa chạy lại được).
- 6.9 Treo: mỗi lượt tối đa 100 yêu cầu; hạn 30 s của handler.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: không áp dụng vùng rapid (không tiền, tồn); ca biên bằng test bảng (chữ ký sai từng kiểu).
- 6.12 Dữ liệu cá nhân: mã người dùng app chỉ lưu dạng băm SHA-256 (hex); `signed_request`, mã người dùng không vào log,
  dấu vết, cảnh báo; tệp tạm chỉ có băm. Log ở mép chỉ có mã xác nhận/lỗi.
- 6.13 Quyền: không áp dụng quyền nhân viên (điểm của Meta, xác thực bằng chữ ký app secret); trang trạng thái chỉ trả
  "đang/đã xử lý" theo mã 128 bit ngẫu nhiên, không lộ dữ liệu.

## Quyết định thay chủ shop

1. **Chống lặp 7 ngày của nhắc Data Use Checkup** dựa trên `platform.alert_events` (hàm mới
   `observability.AlertSentWithin`, câu sqlc có sẵn): lệnh hằng ngày chỉ gửi khi key `data-use-checkup:<hạn>` chưa
   gửi trong 7 ngày. Vì sao: Alerter chỉ chống lặp 10 phút; chống trùng của hàng đợi mất khi dọn việc xong (có thể chỉ
   giữ 1 ngày). Phương án khác: lịch mỗi 7 ngày (nhắc trễ tới 6 ngày), bảng riêng cho mốc đã nhắc (thêm bảng cho một
   dòng). Còn ≤ 30 ngày, kể cả đã quá hạn, đều nhắc (quá hạn ghi rõ "đã quá hạn").
2. **Lịch nhắc và lệnh của nó cùng đăng ký ở `cmd/crm`** (`alertSchedules`, cạnh `platform.alerts`, `platform.digest`):
   `channel.Deps` do `wireM4` dựng không có Alerter (Alerter dựng một lần ở `alertSchedules`), và để lịch luôn đi cùng
   handler của nó; `deletionParts` vẫn rỗng (lời giải thích ở đầu tệp). Lệnh dựng bằng `alertjobs.NewScheduledCommand`
   (xuất hàm dựng có sẵn của gói, cùng mẫu với hai lịch cảnh báo). Đảo ngược dễ.
3. **Đổi `app.tenant_id` trong một transaction** (`tenancy.SetTenant`, thêm vào gói tenancy) để giữ đúng "một
   transaction: xóa phiên → đánh dấu xong" của ADR-46 nhóm 13; bỏ đặt (rỗng) trước câu đánh dấu. Phương án khác: mỗi
   tenant một `WithTenant` rồi một tx đánh dấu (mất tính nguyên tử, handler hàng đợi không được mở tx riêng).
4. **Tệp tạm của yêu cầu xóa** ở thư mục con `<SPOOL_DIR>/data-deletion` (tạo khi ingest chạy), nạp lại mỗi 5 s bằng
   `datadeletion.Drainer` trong ingest, `ON CONFLICT (code) DO NOTHING`. Trang trạng thái của mã còn nằm trong tệp tạm
   trả 404 tới khi nạp (vài giây sau khi database trở lại).
5. **Giờ trên trang trạng thái**: giờ Việt Nam (`15:04 02/01/2006`), chữ thuần như kế hoạch.
6. **Đổi mã đăng nhập cũng đọc `me?fields=id`**: lỗi ở bước này xử lý như lỗi liệt kê Page (từ chối → đăng nhập lại,
   tạm → thử lại), vì thiếu băm thì callback xóa không tìm được phiên.
7. **Băm** là hex SHA-256 của mã người dùng app (`datadeletion.UserRefHash`, một chỗ cho cả hai đường).

## Dừng để hỏi

- Không có việc dừng. Việc ngoài máy cần chủ shop (không làm ở task này): khai URL callback
  `<HOOKS_PUBLIC_BASE_URL>/hooks/facebook/data-deletion` trong App Dashboard của Meta, đặt
  `META_DATA_USE_CHECKUP_DUE` trên worker, nộp App Review theo `docs/meta/app-review.md`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `signed_request` ký đúng → 200, `url` thuộc `HOOKS_PUBLIC_BASE_URL`, `confirmation_code` 32 hex, 1 dòng yêu cầu (băm đúng), trang trạng thái "đang xử lý" | `TestEndpoint_SignedOK` | `go test -race -count=1 ./internal/platform/datadeletion/` | `ok … datadeletion 2.952s` |
| Chữ ký sai → 400 `bad_signature`, 0 dòng | `TestEndpoint_BadSignature`; kiểm chữ ký thật: `TestParseSignedRequest_Rejects` (10 ca), `TestParseSignedRequest_Valid` | như trên; `go test -race -count=1 -run '^(TestParseSignedRequest_Valid\|TestParseSignedRequest_Rejects\|TestAppUserID)$' ./internal/adapters/facebook/` | `ok`; `ok … facebook 1.912s` |
| Database lỗi → vẫn 200 kèm mã, 1 tệp tạm (không chứa mã gốc); nạp lại → 1 dòng, tệp bị xóa | `TestEndpoint_DatabaseDownSpools` | `go test -race -count=1 ./internal/platform/datadeletion/` | `ok` |
| Hai tenant có phiên cùng băm → việc chạy xong cả hai bị xóa, phiên khác giữ, `completed_at` có giá trị | `TestDeleteOAuthDataFor_AllTenants` (channel thật), `TestProcessor_AllTenantsThenCompleted` (mọi tenant, tenant bỏ đặt sau cùng, chạy lại không làm gì), `TestProcessor_DeleterErrorRollsBack` | `go test -race -count=1 ./internal/modules/channel/`; `… ./internal/platform/datadeletion/` | `ok … channel 8.386s`; `ok` |
| Trang trạng thái → "đã xử lý lúc …"; mã lạ, sai mẫu → 404 | `TestProcessor_AllTenantsThenCompleted`, `TestEndpoint_StatusUnknown` | như trên | `ok` |
| Lúc đổi mã ghi băm mã người dùng app (`me?fields=id`) | `TestSaveOAuthPages_StoresUserRefHash`, `TestConnector_Classify` (2 ca lỗi mới), `TestAppUserID`, `TestChannelConnect_*` (httpapi) | `go test -race -count=1 -run 'DataUseCheckup\|DeleteOAuth\|StoresUserRef\|Connector_Classify' ./internal/modules/channel/`; `go test -race -count=1 -run '^TestChannelConnect' ./internal/httpapi/` | `ok … channel 10.334s`; `ok … httpapi 5.357s` |
| Hạn còn 29 ngày → 1 cảnh báo; 31 ngày → 0; đúng 30, quá hạn → 1; chưa đặt → 0; thiếu Alerter → not_sent | `TestDataUseCheckup_Window`, `TestDataUseCheckup_NoAlerter` | `go test -race -count=1 -run 'DataUseCheckup\|DeleteOAuth' ./internal/modules/channel/` | `ok … channel 3.882s` |
| Chống lặp 7 ngày theo đồng hồ database (ngoài cửa sổ 10 phút của Alerter): đã gửi 1 ngày, 6 ngày 23 giờ trước → không gửi; 7 ngày 1 giờ trước → gửi lại | `TestDataUseCheckup_RepeatAfterSevenDays` (dòng `alert_events` chèn sẵn bằng `now() - interval`) | như trên | `ok` |
| `META_DATA_USE_CHECKUP_DUE` đọc được, sai dạng lỗi nêu tên | `TestLoad_DataUseCheckupDue` | `go test -count=1 -run '^TestLoad_DataUseCheckupDue$' ./internal/platform/config/` | `ok … config 0.400s` |
| Đổi tenant trong một transaction, bỏ đặt, không rò | `TestSetTenantSwitchesWithinTx` | `go test -race -count=1 -run '^TestSetTenantSwitchesWithinTx$' ./internal/platform/tenancy/` | `ok … tenancy 17.422s` |
| Ingest có đường mới khi bật Facebook (chữ ký thật), không bật thì không; lịch nhắc có lệnh cùng Kind; lịch xóa có handler | `TestRegisterDataDeletion`, `TestAlertSchedulesDataUseCheckup`, `TestWireM4_Frame` | `go test -race -count=1 -run '^(TestRegisterDataDeletion\|TestAlertSchedulesDataUseCheckup\|TestWireM4_Frame)$' ./cmd/crm/` | `ok … cmd/crm 2.438s` |
| `lockorder_test` đạt với bảng mới | `TestLockOrder_M4TablesListed` (cả gói `tests/arch`) | `go test -count=1 ./tests/arch/` | `ok … tests/arch 543.500s` |
| `docs/meta/app-review.md`: 7 quyền + 2 tính năng, lời gọi API, kịch bản quay, hướng dẫn người duyệt, URL chính sách, URL xóa dữ liệu, biểu tượng, Business Verification, không xin `pages_manage_posts` | tài liệu (đối chiếu tên hàm với mã: `ListPages`, `AppUserID`, `Subscribe`, `Unsubscribe`, `Send`, `FetchHistory`, `Backfill`, `FetchPost`, `FetchProfile`, `Lookup`, `PageToken`) | đọc | có đủ mục |
| ci-task (cổng tĩnh + test toàn bộ trừ e2e) | mọi gói | `ci-task.sh` (một lần sau khi stage; lần trước đó đỏ gen-check/catalog-check chỉ vì chưa stage) | cổng tĩnh xanh; test: mọi gói `ok` trừ `internal/httpapi/web` `TestWebShippingQuote` ("hãng được gọi 4 lần … muốn 2", bộ đệm báo phí, máy đang chạy ci của nhiều phiên); chạy riêng `go test -race -count=1 -run '^TestWebShippingQuote$' ./internal/httpapi/web/` → `ok … 3.629s`. Gói này task không chạm. **Lần chạy lại sau sửa review: `CI-TASK XANH (so với điểm tách nhánh aafa087…)`**, test toàn bộ trừ e2e 650 giây, mọi gói `ok` |
| Cổng tĩnh của các gói sửa | — | `golangci-lint run` 7 gói; `make jscpd`; `make catalog` | `0 issues.`; không clone mới; danh mục sinh lại |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch không có test e2e cho task này)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Lịch `channel.data_use_checkup` không nằm trong `deletionParts` (tệp khung) mà ở `cmd/crm/main.go` `alertSchedules`
  (Quyết định 2). `deletion.go` chứa `DeleteOAuthDataFor` và lệnh nhắc như kế hoạch.
- Thêm tệp `internal/platform/datadeletion/spool.go` (tệp tạm, Drainer), `cmd/crm/datadeletion.go` (nối ingest, worker)
  ngoài danh sách Files; `registry.go` chứa `Processor`, `status.go` chứa `Endpoint`.
- `channel.ExchangeCode` trả `OAuthLogin`, `SaveOAuthPages` nhận `OAuthLogin` (thay vì chỉ thêm tham số) để không
  thêm tham số thứ chín.

## Việc phát hiện thêm

- Chính sách quyền riêng tư của website cần phần "Dữ liệu từ Facebook" (bản nháp ở `docs/meta/app-review.md` mục 6):
  việc của website, ngoài CRM.
- `internal/httpapi/web` `TestWebShippingQuote` chập chờn khi máy tải nặng (đếm lượt gọi hãng sau bộ đệm báo phí):
  đỏ một lần trong ci-task của task này, chạy riêng thì qua; không sửa (ngoài phạm vi).
- Tệp tạm của yêu cầu xóa chưa được tính vào `/health` của ingest (chỉ log khi nạp lỗi); có thể thêm vào khi cần.

## Reviewer

Lượt 1: **CẦN SỬA** — `TestDataUseCheckup_Window` không chứng minh chống lặp 7 ngày (lần chạy hai chỉ lùi đồng hồ Go,
chống lặp 10 phút của Alerter đã đủ làm test qua; trộn hai đồng hồ). Đã sửa: test mới
`TestDataUseCheckup_RepeatAfterSevenDays` chèn sẵn mốc gửi bằng `now()` của database (1 ngày, 6 ngày 23 giờ → không
gửi; 7 ngày 1 giờ → gửi). Ghi chú không bắt buộc đã làm: `Processor.Handle` xóa cả lô trước rồi mới đánh dấu xong
(nhóm 13 luôn cuối); thêm `facebook.ErrNoUserID` vào mục Ảnh hưởng. Ghi chú không làm: `channel` nhập
`platform/datadeletion` (gián tiếp kéo `net/http`) chỉ để dùng `UserRefHash` — luật chỉ cấm nhập trực tiếp, test kiến
trúc qua; giữ một chỗ băm.

Lượt 2: **Kết luận: ĐẠT.** Phát hiện phải sửa: không có. Reviewer kiểm lại `TestDataUseCheckup_RepeatAfterSevenDays`
(bỏ nhánh kiểm 7 ngày thì ca "1 day" đỏ; một đồng hồ; kiểm cả chiều gửi lại) và thứ tự mới của `Processor.Handle`
(nhóm 3 cả lô trước, nhóm 13 cuối). Ảnh hưởng khớp; không còn chữ ký cũ; không test nào bị nới. Ghi chú không bắt buộc
còn lại: `channel` nhập `platform/datadeletion` chỉ để dùng `UserRefHash` (đã ghi lý do giữ).
