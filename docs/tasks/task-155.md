# Task 155 — Tệp đính kèm vào, đường dẫn có hạn, hồ sơ khách từ PSID

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 155 = M4-15). Loại việc (quy
trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `internal/platform/files` (vùng rủi ro cao): **thêm** trường `Purpose.AnyType` (mục đích nhận mọi loại tệp, chỉ cho
    tệp máy chủ ghi: `!Upload && !Image && AllowedTypes rỗng`, Quyết định 1); `validate`, `samePurpose`, `allows`,
    `Registry.Check` (không đổi chữ ký). 0 chỗ gọi phải sửa; mục đích có sẵn không đặt `AnyType` nên hành vi cũ giữ.
  - `internal/modules/channel`: **thêm** sentinel `ErrDownloadBlocked`, `ErrDownloadUnavailable`, `ErrDownloadTooLarge`
    (lỗi chung của `AttachmentDownloader`, module không nhập adapter). Không đổi ký hiệu có sẵn.
  - `internal/adapters/facebook`: lỗi của `Download` bọc thêm sentinel chung (`ErrHostNotAllowed`, `ErrPrivateAddress`,
    `ErrTooManyRedirects` → `ErrDownloadBlocked`; HTTP 403/404/410 → `ErrDownloadUnavailable`; `ErrTooLarge` →
    `ErrDownloadTooLarge`). Sentinel cũ giữ nguyên (test của 146 dùng `errors.Is` vẫn đúng).
  - `internal/adapters/fake`: **thêm** `(*Chat).Download`, `(*Chat).FetchProfile` (kênh giả hiện thực
    `AttachmentDownloader`, `ProfileFetcher`), `AddFile`, `SetProfile`, `FailProfile`, `DownloadCalls`; hằng `CDNHost`.
    Tệp mới `fetch.go` (không chạm `split.go`, `send.go` mà Task 156 sửa).
  - `internal/modules/conversation`: **thêm** `InboundFilePurposes`, `AttachmentURL`, `AttachmentLink`,
    `AttachmentURLTTL`, hằng mục đích `PurposeChatImage`, `PurposeChatAttachment`, `PurposeChatAvatar`;
    `RegisterFilePurposes` (của 156) dời sang `filepurposes.go`, đăng ký thêm ba mục đích; dùng lại
    `ErrAttachmentNotReady`, `ErrAttachmentRejected` của 156 (Quyết định 6). 1 chỗ gọi (`cmd/crm/main.go`), không đổi.
  - `internal/modules/conversation/internal/inbound` (gói nội bộ): `OnInboundAttachments`, `OnNewCustomer` đổi chữ ký
    (thêm `queue.JobQueue`, mục đính kèm mang thêm URL), 3 chỗ gọi (`messages.go`, `echo.go`, `handler.go`) sửa cùng
    task; `Parts` thêm `Commands` (hai lệnh mới).
  - `internal/httpapi`: thêm `(*handler).GetAttachmentUrl` (thay 501 mặc định của ogen); hai dòng `errorMapping` lỗi
    422 của 156 dùng chung (chỉ sửa chú thích ở `errors_conversation.go`).
- Gói phụ thuộc ngược (go list): `channel` ← `cmd/crm`, `adapters/facebook`, `adapters/fake`, `httpapi`,
  `channel/channeltest`, `conversation` (+ `deps`, `inbound`, `templates`); `files` ← `cmd/crm`, `httpapi`, `cash`
  (+`statement`), `catalog`, `content`, `conversation/internal/deps`, `customer`, `fulfillment`, `order`,
  `files/filestest`, `queries/webcatalog`; `adapters/fake`, `adapters/facebook` ← `cmd/crm`; `conversation` ←
  `cmd/crm`, `httpapi`. Không chỗ nào phải sửa ngoài các tệp nêu.
- Operation OpenAPI bị chạm, oasdiff: `getAttachmentUrl` (GET `/api/v1/conversations/{id}/attachments/{fileId}/url`,
  `conversation.read`) đã khai ở Task 143, mã `attachment_not_ready`, `attachment_rejected` đã có trong
  `x-error-codes` → **không sửa** `openapi.yaml`, oasdiff không đổi. Xóa dòng `getAttachmentUrl` khỏi
  `tests/arch/testdata/m4_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn M4 ở Task 174–178); `console/src/api` không đổi.
- Câu sqlc, bảng, pha migration: `migrations/0404_files_chat_purposes.sql` — pha **mở rộng**, một câu `INSERT … ON
  CONFLICT DO NOTHING` ba dòng `platform.file_purposes` (`chat_image` private/private, `chat_attachment`
  private/standard, `chat_avatar` private/standard; `chat_outgoing` Task 156 đã thêm ở 0413, không khai lại). Câu sqlc
  mới `queries/conversation/attachments.sql`: đọc tin cho lệnh tải, đặt một mục đính kèm (`jsonb_set` theo chỉ số, một
  câu UPDATE trên dòng tin), đọc hội thoại cho lệnh hồ sơ, đặt `avatar_file_id`, tìm mục đính kèm theo `fileId` trong
  hội thoại. Bảng chạm: `conversation.messages` (cột `attachments`), `conversation.conversations` (`avatar_file_id`),
  `platform.files` (qua `files.Register`, `files.RegisterPrivateImage`), `customer.customers` (qua
  `customer.Writer.SetNameFromChannel`), `platform.jobs` (Enqueue).
- Tệp khác đổi theo: `internal/modules/conversation/internal/inbound/comments.go` (Task 154: hai chỗ gọi hook đổi chữ
  ký), `internal/modules/conversation/send.go` (bỏ `RegisterFilePurposes` của 156), `cmd/crm/main.go` (chú thích dòng
  `conversation.RegisterFilePurposes()`), `cmd/crm/wire_m4_test.go`
  (danh sách lệnh M4 thêm hai lệnh), `internal/modules/conversation/wiring_test.go`, `internal/platform/files/purpose_test.go`
  (`allPurposes` thêm mục đích của conversation).
- Test sẽ chạy: test mới `inbound/attachments_test.go`, `inbound/profile_test.go`, `conversation/filepurposes_test.go`,
  `httpapi/handlers_attachments_test.go`, `files` (`TestRegisterPurpose_*`, `TestPurposes_GoMatchesTable`, ca
  `AnyType`), `facebook` (`TestDownload_*` ca sentinel chung), `fake` (ca tải, hồ sơ); `tests/arch`; toàn bộ gói
  đổi và gói phụ thuộc qua `ci-task` (files, conversation là vùng có test lớn).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (mục files:
  `TmpKey`, `ServerKey`, `RegisterPrivateImage`, `Register`, `DownloadURL`, `Status`, `MemStore`, `filestest`;
  commands: `CommandKind`, `Outcome`, `DoneHook`; channel: `Account`, `CredentialsFor`, `AttachmentDownloader`,
  `ProfileFetcher`; customer: `Writer.SetNameFromChannel`; testkit: `StubImager`, `OpenStaff`, `ChannelAccount`,
  `DrainDue`; conversation: `access.Require`, `access.Signal`).
- Dùng lại / viết mới vì:
  - Dùng lại nguyên: xử lý ảnh riêng tư của M1 (`RegisterPrivateImage` → `file.process_private`, chống trùng cùng
    tenant, biến thể ở `BucketPrivate`), `DownloadURL` (ký 5 phút, biến thể JPEG lớn nhất), `files.Register` cho tệp
    không phải ảnh, bộ tải tệp an toàn của adapter Facebook (Task 146: chỉ máy chủ Meta, chặn địa chỉ nội bộ, nhận
    dạng loại theo nội dung), `channel.CredentialsFor`, `customer.Writer.SetNameFromChannel` (Task 152),
    `access.Require`/`access.Signal`, `testkit.StubImager` (imager giả của test M5 Task 195).
  - Viết mới: hai lệnh `command.fb.fetch_attachment`, `command.fb.fetch_profile` (việc riêng của conversation); sentinel
    lỗi tải chung ở `channel` (module không được nhập adapter để `errors.Is` lỗi của Facebook); `Purpose.AnyType`
    (sổ mục đích M1 không có cách nói "mọi loại"); bản giả tải tệp và hồ sơ cho kênh `fakechat` (Quyết định 3).
  - Không gộp: đọc biến thể của 156 (`send.imageURL`, ảnh công khai `chat_outgoing`) khác việc ở đây (ảnh riêng tư,
    link ký) — dùng thẳng `files.DownloadURL`.

## Soát thiết kế

Mục 5 (khớp kiến trúc):

- 5.1 Chủ dữ liệu: mục đính kèm là cột `attachments` của `conversation.messages` (module conversation); dòng tệp là
  của `platform.files` (ghi qua public API `files`); tên khách của `customer` (qua `Writer.SetNameFromChannel`).
- 5.2 Không ghi bảng module khác: `platform.files` chỉ qua `files.Register`/`RegisterPrivateImage`; khách qua
  `customer.Writer`. Không SQL nào của conversation chạm bảng khác schema.
- 5.3 Đọc module khác: `channel.Account`, `channel.CredentialsFor`, `files.Status`, `files.DownloadURL` (public API).
- 5.4 Nguyên tử: OnDone ghi dòng tệp (và xếp `file.process_private`) cùng transaction với việc đổi mục đính kèm.
- 5.5 Không nguyên tử: tải tệp, đọc hồ sơ là lệnh (`commands`) xếp trong tx của tin vào, chạy sau commit.
- 5.6 Gọi ra ngoài: hai lệnh **chỉ đọc** (GET tới Meta/CDN): không có gì để "gửi hai lần", nên mọi lỗi tạm là
  `not_sent` (tự thử lại, tối đa 8 lượt, hạn 1 giờ); lỗi vĩnh viễn là `rejected`. Không bao giờ trả `unknown`.
- 5.7 Tenant: mọi câu trong `tenancy.WithTenant`; không bảng mới.
- 5.8 Module không nhập `net/http`: phân loại lỗi tải bằng sentinel của `channel`; HTTP ở `handlers_attachments.go`.
- 5.9 Đường ghi: không có đường ghi HTTP mới (GET chỉ đọc, `x-permission: conversation.read` đã khai).
- 5.10 không áp dụng: không dùng Valkey.
- 5.11 Không thêm thư viện.
- 5.12 Theo mẫu lệnh của Task 73/156 (Execute ngoài tx, OnDone/OnTerminal trong tx của Runner), handler mỏng.

Mục 6 (đồng thời):

- 6.1 Nhiều lệnh tải cùng một tin (nhiều tệp) cùng ghi cột `attachments`: mỗi OnDone là **một câu UPDATE** `jsonb_set`
  theo chỉ số trên dòng tin; READ COMMITTED đánh giá lại SET trên bản mới sau khi chờ khóa dòng → không mất mục của
  lệnh khác (test hai tệp một tin). OnDone chạy lại (Runner) → dòng tệp `Register`/`RegisterPrivateImage` trả id cũ
  khi cùng ID; ID sinh ở Execute nằm trong `result` nên OnDone lần hai dùng cùng ID.
- 6.2 Webhook giao lại: lệnh có `DedupeKey = <messageId>:<index>`; hồ sơ `DedupeKey = <customerId>`.
- 6.3 Thứ tự khóa (ADR-46): OnDone tải tệp làm nhóm 6 trước — đổi (khóa) dòng `conversation.messages` — rồi mới
  chèn `platform.files` (12b) và `platform.jobs` (13). Lệnh hồ sơ: tên ghi trong transaction riêng của Execute (chỉ
  `customer.customers`, nhóm 5, `SetNameFromChannel`); OnDone chỉ `conversation.conversations` (6) → `platform.files` (12b). Chèn dòng mới không chờ khóa nên không tạo vòng.
- 6.4 Lỗi tạm của database trong OnDone: Runner chạy trong `tenancy.WithTenant` (RetryTx); thân hàm gán lại mọi biến.
- 6.5 Mặc định READ COMMITTED + câu có điều kiện.
- 6.6 Lệnh lỗi: tạm → `not_sent` theo lịch thử lại; hết 8 lượt hay quá 1 giờ → `dead`/`expired` + OnTerminal đặt
  mục `failed`/`expired`. Từ chối (máy chủ lạ, 403/404, quá cỡ, Meta báo thiếu quyền) → `rejected` → `dead`.
- 6.7 429 của Meta khi đọc hồ sơ: `not_sent` (lịch thử lại).
- 6.8 Worker chết giữa chừng: lệnh `in_doubt` (giữ chỗ hết hạn); lệnh chỉ đọc nên chạy lại bằng tay an toàn.
- 6.9 Tải 30 s (bộ tải của 146), lệnh 30 s.
- 6.10 không áp dụng: không có tiền.
- 6.11 Quy luật: tệp của khách chỉ tới kho từ máy chủ được phép; ảnh thô chỉ ở `crm-tmp`; không biến thể nào ngoài
  `priv/<tenant>/` — có test cụ thể (không rapid: không có không gian trạng thái ngẫu nhiên đáng kể).
- 6.12 Dữ liệu cá nhân: tên khách không vào `jobs.result` (Execute ghi tên trong transaction riêng; test kiểm `result`
  không chứa tên, `payload` không chứa PSID). URL của Meta (có chữ ký) nằm trong cột `attachments` (dữ liệu cá nhân, không log) tới khi lệnh
  xong thì xóa khỏi mục; payload lệnh chỉ mang mã; `Reason` chỉ là mã; lỗi tải không chứa URL (146).
- 6.13 Quyền: `GET …/url` kiểm `conversation.read` trên Page của hội thoại, thiếu → 404; `fileId` phải thuộc một tin
  của hội thoại.

## Quyết định thay chủ shop

1. **`Purpose.AnyType` thay cho `AllowedTypes: nil`** cho `chat_attachment`: sổ mục đích của M1 coi danh sách loại rỗng
   là lỗi lập trình (có test "không có loại" panic). Thêm cờ rõ ràng `AnyType` (chỉ hợp lệ khi `!Upload && !Image` và
   danh sách rỗng) để giữ test cũ và không vô tình mở mọi loại cho mục đích khác. Phương án khác: liệt kê mọi loại mà
   `http.DetectContentType` trả (dễ thiếu, tệp lạ bị từ chối).
2. **Tệp không phải ảnh ghi lên kho với `Content-Type: application/octet-stream`**, dòng `platform.files` giữ loại đã
   nhận dạng (`declared_type`). `FileStore.PresignGet` của M1 chỉ đặt `Content-Disposition`, không đặt loại trả về;
   ghi đối tượng bằng octet-stream làm link luôn trả `application/octet-stream` + `attachment` (TP04) mà không đổi
   chữ ký của kho tệp (vùng rủi ro cao). Phương án khác: thêm tham số loại vào `PresignGet` (đổi interface, 3 bản hiện
   thực).
3. **Test của module dùng kênh giả `fakechat` làm CDN** (tải tệp, hồ sơ trong bộ nhớ), không dùng fakegraph: bộ tải của
   adapter Facebook không bao giờ nối 127.0.0.1 ngoài test của chính gói `facebook` (hook `UseTestResolver` chỉ có
   trong `export_test.go`, cố ý từ Task 146). Phần riêng của Facebook (fakegraph làm CDN: 404 → `unavailable`,
   `evil.example` bị chặn không yêu cầu nào tới máy giả, 26 MiB → `too_large`) test ở gói `facebook` với fakegraph.
   Phương án khác: mở hook cho phép loopback ở mã thật (trái 146, nguy cơ SSRF).
4. **URL của tệp lưu tạm trong mục đính kèm** (`messages.attachments[i].url`) tới khi lệnh xong (stored hay lỗi thì
   xóa): kế hoạch giữ payload lệnh chỉ `{messageId, index}` mà `Execute` phải "đọc URL trong transaction". Cột đã là
   dữ liệu cá nhân (0402). API danh sách tin (Task 159) không được trả trường `url` (ghi ở Việc phát hiện thêm).
5. **Echo của chính CRM chưa khớp (`own_echo_unmatched`) không tải đính kèm**: ảnh đó là ảnh CRM vừa gửi (đã có tệp
   `chat_outgoing`); Task 156 xóa dòng này khi gộp. Lệnh tải gặp tin đã bị xóa → xong, không ghi gì (tệp đã ghi lên
   kho thành mồ côi, việc dọn Q16 xử lý). Echo của ứng dụng khác (Business Suite) vẫn tải.
6. **Trùng tên với Task 156** (`RegisterFilePurposes`, `ErrAttachmentNotReady`, `ErrAttachmentRejected`): m4 đã có 156
   lúc gộp lần hai, nên giữ hai sentinel của 156 (`AttachmentURL` trả đúng hai lỗi đó, cùng mã 422) và một
   `RegisterFilePurposes` (ở `filepurposes.go`) đăng ký cả bốn mục đích (`chat_outgoing` của 156 + ba mục đích của task
   này); bỏ bản của 156 ở `send.go`. Dòng snapshot lỗi giữ của 156.
8. **Gộp m4 hai lần trước reviewer**: lần một (fast-forward, có 154, 179) trước ci-task đầu; trong lúc ci chạy, m4 nhận
   156, 150, 170, 493 (156 trùng ký hiệu với task này), nên gộp lần hai (fast-forward) rồi mới stage và gọi reviewer,
   để reviewer duyệt đúng bản đã gộp. Không gộp thêm sau khi reviewer ĐẠT.
7. **Ảnh đại diện lỗi không làm hỏng lệnh hồ sơ**: tên là chính (CH08); ảnh đại diện không tải được, quá 2 MiB hay
   không phải JPEG/PNG/WebP thì bỏ qua ảnh, vẫn đặt tên. Meta báo thiếu quyền đọc hồ sơ → `rejected` (dead, cảnh báo
   mức thường như mọi việc dead; không cảnh báo gấp), tên tạm giữ nguyên.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TP02: ảnh JPEG có GPS → 1 lệnh tải, ảnh thô chỉ ở crm-tmp (TmpKey), 1 dòng `chat_image`, 1 `file.process_private`, 0 `file.process`; sau xử lý ready, mọi biến thể `bucket = private`, khóa `priv/<tenant>/<tệp>/`, không đối tượng public/originals, biến thể không còn khối GPS; mục `stored`, không còn `url`; đường dẫn trên gốc kho tệp tới `/private/priv/…`, không attachment, hạn ≤ 5 phút, đọc được, chữ ký sai không đọc được; nhân viên Page khác → ErrNotFound; fileId hội thoại khác → ErrNotFound; ảnh chưa xử lý → ErrAttachmentNotReady | `TestInboundImage_TP02` | `go test -race -count=1 -run '^(TestInboundImage_TP02|TestInboundImage_Truncated|TestInboundFile_TP04|TestInboundAttachment_Refusals|TestInboundImage_Dedupe|TestFetchProfile_CH08)$' ./internal/modules/conversation/internal/inbound/` | `ok …/conversation/internal/inbound 7.8s` |
| R10 ảnh hỏng: JPEG cụt → imager giả `decode_failed` → tệp rejected, 0 biến thể, đường dẫn → ErrAttachmentRejected | `TestInboundImage_Truncated` | như dòng 1 | PASS |
| TP04: PDF, HTML khai là ảnh (nhận dạng `text/html`), SVG (`text/xml`) → `chat_attachment` ready, `downloadOnly`, tên `tep-<8>.<đuôi>`; GET đường dẫn trả `Content-Type: application/octet-stream`, `Content-Disposition: attachment` có tên; 0 lệnh xử lý ảnh, crm-tmp rỗng | `TestInboundFile_TP04` | như dòng 1 | PASS |
| `evil.example` → lệnh dead, mục `blocked`, CDN 0 yêu cầu; 26 MiB → `too_large`; CDN không có → `unavailable`; ba tệp một tin → ba mục đúng trạng thái, 1 dòng tệp | `TestInboundAttachment_Refusals` | như dòng 1 | PASS |
| Facebook (fakegraph làm CDN): máy lạ, địa chỉ nội bộ → `ErrDownloadBlocked`, máy giả 0 yêu cầu; 403/404/410 → `ErrDownloadUnavailable`; 26 MiB → `ErrDownloadTooLarge`; 500 → lỗi tạm | `TestDownload_ChannelSentinels` | `go test -race -count=1 -run '^TestDownload_' ./internal/adapters/facebook/` | `ok …/adapters/facebook 1.6s` |
| Hai tin cùng ảnh → imager 1 lần, hai tệp cùng khóa biến thể `priv/<A>/…`; tenant B → imager lần 2, biến thể `priv/<B>/…` | `TestInboundImage_Dedupe` | như dòng 1 | PASS |
| R5: sổ Go khớp bảng sau 0404 (chat_image private); 0404 chạy 2 lần → 3 dòng; đổi mode chat_image / xóa dòng chat_attachment → `ErrPurposeMismatch` nêu đúng khóa; `chat_attachmentx` → ErrUnknownPurpose, chèn thẳng → 23503; Register chat_image → ErrPurposeMode; chat_attachment nhận PDF/HTML/GIF | `TestFilePurposes_MatchTable`, `TestPurposes_GoMatchesTable` | `go test -race -count=1 -run '^TestFilePurposes_MatchTable$' ./internal/modules/conversation/`; `go test -race -count=1 -run '^TestPurposes_GoMatchesTable$' ./internal/platform/files/` | PASS |
| `AnyType`: nhận mọi loại trong giới hạn cỡ, quá cỡ ErrTooLarge, mục đích thường vẫn ErrTypeNotAllowed; AnyType kèm loại/tải lên/ảnh → panic; khai lại khác AnyType → panic | `TestRegister_AnyType`, `TestRegisterPurpose_PanicsOnInvalid`, `TestRegisterPurpose_PanicsOnEachChangedField` | `go test -race -count=1 -run '^(TestRegister_AnyType|TestRegisterPurpose_PanicsOnInvalid|TestRegisterPurpose_PanicsOnEachChangedField)$' ./internal/platform/files/` | `ok …/platform/files 3.8s` |
| CH08: khách mới → tên "Lan Nguyễn" (`name_source = channel`), ảnh đại diện → `avatar_file_id`, tệp `chat_avatar` ready private; payload không có PSID, result không có tên; tin sau không thêm lệnh; ảnh đại diện lỗi vẫn đặt tên; kênh từ chối → tên tạm giữ, lệnh dead, chỉ 2 việc mới (không cảnh báo) | `TestFetchProfile_CH08` | như dòng 1 | PASS |
| API: 200 ảnh (private, không attachment, ≤ 5 phút), 200 PDF (attachment, tên), 422 `attachment_not_ready`, 422 `attachment_rejected`, 404 Page khác / tệp ngoài hội thoại / hội thoại không có | `TestGetAttachmentURL`, `TestDomainErrorSnapshot` | `go test -race -count=1 -run '^(TestGetAttachmentURL|TestDomainError.*)$' ./internal/httpapi/` | `ok …/httpapi 2.4s` |
| Nối dây: conversation có lệnh fetch_attachment, fetch_profile (+ fetch_post của 154 khi có Facebook); `cmd/crm` đăng ký mục đích | `TestWiring`, `TestWireM4_Frame`, `TestM4Contract_*` (tests/arch) | `go test -count=1 -run '^TestWireM4_Frame$' ./cmd/crm/`; `go test -count=1 ./tests/arch/` | `ok` |
| Cổng kiểm của task | cổng tĩnh + test race các gói đổi (cmd/crm, facebook, fake, httpapi, channel, conversation, inbound, files) + test thường các gói phụ thuộc | `scripts/agent/ci-task.sh` | `CI-TASK XANH (từ xa, so với điểm tách 2f7d2932)`, run 45781993404: static, test 1–6 success |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch không có test e2e cho task này)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Chữ ký `OnInboundAttachments(ctx, tx, q, tenantID, msgID, refs)`, `OnNewCustomer(ctx, tx, q, tenantID, customerID,
  conversationID)`: thêm hàng đợi và tenant (cần để `Enqueue`). Chỗ gọi của Task 154 (`comments.go`) sửa theo; hội
  thoại comment không có PSID nên lệnh hồ sơ của nó xong ngay, không gọi kênh.
- Mục đính kèm thêm trường `url` khi còn pending (Quyết định 4); trạng thái lỗi thêm `failed`, `expired` (OnTerminal).
- `chat_attachment` dùng `AnyType` thay `AllowedTypes: nil` (Quyết định 1); `chat_outgoing` không có ở 0404 (đã ở 0413
  của Task 156).
- Tệp không phải ảnh ghi lên kho bằng `application/octet-stream` (Quyết định 2).
- Test của module dùng kênh giả làm CDN; fakegraph làm CDN ở test gói `facebook` (Quyết định 3). Ca "lỗi quyền" của
  hồ sơ dùng `fake.ErrPermanent` (cùng lớp `ErrorPermanent` mà `facebook.Classify` trả cho lỗi quyền của Graph).
- "Đọc lại sau hạn → 403": `MemStore` không cho chỉnh đồng hồ từ ngoài gói files; test kiểm `exp` ≤ 5 phút và chữ ký
  sai bị từ chối (hết hạn trả 403 có test của M1 ở gói files).
- "Biến thể JPEG không còn GPS": imager giả không giải mã; test kiểm biến thể không chứa khối GPS của ảnh thô; bỏ EXIF
  là việc của imager thật (M1 Task 43).
- Tên khách ghi trong transaction riêng của `Execute` (không ở `OnDone`) để tên không nằm trong `jobs.result` (6.12).

## Việc phát hiện thêm

- Tệp đính kèm hay ảnh đại diện 0 byte (reviewer): Execute ghi lên kho rồi done, `files.Register` trong OnDone trả
  `ErrInvalidFile` (size ≤ 0) → lệnh in_doubt, mục giữ pending. Gần như không gặp với Meta; sửa sau bằng cách coi
  `info.Size == 0` là `unavailable` trong Execute.
- fakegraph (`internal/testkit/fakegraph/graph.go`, hàm `profile`): PSID chưa `AddProfile` làm máy giả deref nil.
  Lệnh hồ sơ mới gọi GET {psid} trong test của 154 có fakegraph; test vẫn xanh (lỗi mạng → not_sent) nhưng nên trả
  404 Graph. Không sửa ở đây (ngoài phạm vi).
- Task 159 (danh sách tin): không trả trường `url` của mục đính kèm (Quyết định 4); trả `status`, `fileId`,
  `downloadOnly`, `type`, `stickerId`; link xem qua `getAttachmentUrl`.

## Reviewer

Kết luận: ĐẠT (agent reviewer, phần đã stage sau khi gộp m4 2f7d2932; ci-task XANH từ xa, run 45781993404). Phát
hiện phải sửa: không có. Reviewer đã đối chiếu từng dòng Ảnh hưởng (đủ 5 chỗ gọi hook đổi chữ ký, `RegisterFilePurposes`
cũ của 156 đã xóa), từng dòng bằng chứng (test kiểm đúng tiêu chí), thứ tự khóa ADR-46, chạy đồng thời của
`PatchPendingAttachment`, dữ liệu cá nhân (payload chỉ mã, URL bị xóa khỏi mục, tên không vào `jobs.result`), luật
`AnyType` không nới mục đích có sẵn. Ghi chú không bắt buộc: tệp 0 byte giữ mục pending (ghi ở Việc phát hiện thêm);
Page bị xóa thì lệnh thử tới hết hạn 1 giờ rồi expired (kết quả cuối đúng).
