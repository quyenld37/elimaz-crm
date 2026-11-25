# Task 147 — Facebook — đường vào (xác minh, chữ ký, tách gói)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 147, M4-07). Nhánh gốc: `m4`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm** trong gói `internal/adapters/facebook`: kiểu
  `Channel`, hàm `New(appSecret, verifyToken logging.Secret) *Channel`, phương thức `Key`, `Challenge`,
  `VerifySignature`, `Split`, `Features` của `*Channel`; hằng `SignatureHeader`, `RouteKind`; lỗi `ErrBadPacket`; hàm `Source(cfg) webhooks.Source`
  trong `wiring.go`. `facebook.Adapter(cfg)` (khung Task 143, 1 chỗ gọi: `cmd/crm/wire_m4.go:52`) **giữ nguyên chữ ký
  và vẫn trả nil** (xem Quyết định 2). Không đổi ký hiệu nào của gói khác. Tên tránh trùng với Task 146 chạy song
  song trong cùng gói (146 dùng `Options`, `Client`, `NewClient`, `GraphError`, `Classify`, `Reason`, `Usage`,
  `export_test.go`): task này không khai các tên đó, không tạo `export_test.go`.
- Gói phụ thuộc ngược (go list): `internal/adapters/facebook` được nhập bởi `cmd/crm` (wire_m4.go) và dữ liệu thử
  `tests/arch/testdata` (không biên dịch). Test chạy: gói facebook + `cmd/crm` (ci-task tự chọn).
- Operation OpenAPI bị chạm, oasdiff: không (đường `/hooks/facebook` là của ingest M0, không thuộc openapi.yaml).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không có migration, không câu sqlc mới. Test e2e trong gói đọc
  `platform.webhook_deliveries`, `platform.jobs` bằng owner (như `carrier/webhook_test.go`), thêm route bằng
  `tenancy.AddRoute(fb_page)`.
- Test sẽ chạy: `go test -race ./internal/adapters/facebook/` (signature_test, split_test, split_fuzz_test có seed,
  ingest_e2e_test trên PostgreSQL thật); `FuzzSplit` chạy tay 30 s (`-fuzz=FuzzSplit -fuzztime=30s`); `cmd/crm`
  (TestWireM4_Frame, không đổi) và `tests/arch` (luật (n)).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `webhooks`
  (Source, SplitResult, InboundEvent, Route, NewIngest, NewSplitHandler, InboundJobHandlers), `selftest` (nguồn mẫu:
  Challenge so thời gian hằng, chữ ký HMAC-SHA256), `tenancy.AddRoute`; `testkit` (OpenStaff, DrainDue, Do);
  `carrier/webhook_test.go` (mẫu dựng ingest + worker tách trong test); tìm `hmac.New` (selftest, carrier/fake,
  cache, files).
- Dùng lại / viết mới vì: dùng lại nguyên `webhooks.Source` và cả đường ingest → tách → việc vào của M0 (không
  handler HTTP riêng); `channel.EventX`, `channel.*Payload` của Task 143 làm payload chuẩn hóa. Viết mới phần kiểm
  chữ ký vì khác header (`X-Hub-Signature-256`), khác khóa (app secret là `logging.Secret`), chỉ 6 dòng thư viện
  chuẩn (`crypto/hmac`); gộp với `selftest` sẽ buộc nền tảng biết định dạng header của Meta (khác lý do thay đổi).
  Test e2e theo đúng mẫu `carrier/webhook_test.go` (ingest thật + `NewSplitHandler` + `InboundJobHandlers` +
  `testkit.DrainDue`).

## Soát thiết kế

- 5.1 Chủ dữ liệu: không bảng mới; kiểu Meta ở lại trong adapter, ra ngoài chỉ kiểu chuẩn hóa của `channel`.
- 5.2, 5.3: adapter không đọc, không ghi bảng nào; việc ghi `webhook_deliveries`, `jobs` là của nền tảng webhooks.
- 5.4, 5.5: không áp dụng (không có việc nghiệp vụ nguyên tử; tách gói chạy trong việc `webhook.split` của M0).
- 5.6: không gọi ra ngoài (chỉ phần vào).
- 5.7: không truy vấn bảng tenant; định tuyến tenant qua `platform.tenant_routes` (kind `fb_page`) do M0 làm.
- 5.8: adapter chỉ nhập thư viện chuẩn, `internal/platform/{webhooks,logging,config}` và `internal/modules/channel`
  (luật (n)); không nhập `testkit` ở mã chạy (chỉ test).
- 5.9: không có đường ghi API mới; chống trùng tầng 1 bằng `DedupeKey` của từng sự kiện (bảng khóa của kế hoạch).
- 5.10, 5.11: không Valkey, không thư viện mới (`crypto/hmac`, `crypto/sha256`, `crypto/subtle`, `encoding/json`).
- 5.12: theo mẫu nguồn `selftest` và `carrier/fake` (Source), test e2e theo `carrier/webhook_test.go`.
- 6.1–6.11: không áp dụng: task không ghi bảng, không tiền, không tồn; tính chạy lại: `Split` thuần (không đồng hồ,
  không ngẫu nhiên) nên tách lại một bản nhận ra đúng cùng `DedupeKey`, `OrderKey` (test) — chống trùng tầng 1 của
  `jobs.dedupe_key` dựa vào điều này.
- 6.12 Dữ liệu cá nhân: thân gói (tên, nội dung tin, PSID) chỉ vào payload sự kiện (bảng nhận nguyên văn của M0),
  không vào log, nhãn chỉ số hay lỗi: lỗi của `Split` không trích thân; nhãn `Skipped` chỉ là tên loại đã lọc theo
  mẫu `[a-z0-9_]{1,30}` (giá trị lạ → `other`), không bao giờ là nội dung. App secret, verify token là
  `logging.Secret`. `DedupeKey` của comment dùng 8 ký tự đầu sha256 của nội dung, không chứa nội dung.
- 6.13 Quyền: đường `/hooks/facebook` công khai theo thiết kế M0, bảo vệ bằng chữ ký HMAC trên thân thô (so bằng
  `hmac.Equal`) và Lockout của ingest; bước GET chỉ trả `hub.challenge` khi verify token khớp (so thời gian hằng,
  token rỗng luôn sai) và challenge đúng mẫu `[A-Za-z0-9_-]{1,128}` (không phản chiếu chuỗi tùy ý).

## Quyết định thay chủ shop

1. **`New` nhận hai bí mật thay vì `Options` của Task 146.** Kế hoạch ghi `New(o Options)`, nhưng `Options` do Task
   146 (chạy song song, cùng gói) khai trong `client.go`; khai lại ở đây sẽ trùng tên khi gộp. Chọn
   `New(appSecret, verifyToken logging.Secret) *Channel`. Task 148 (đường ra, cần Graph client) đổi thành
   `New(o Options)` (Options của 146 đã có `AppSecret`, `VerifyToken`) và sửa 3 chỗ gọi. Phương án khác: khai
   `InboundOptions` riêng (thêm một kiểu sẽ bỏ ở 148), hay chờ 146 gộp (chặn việc song song). Đảo ngược: đổi chữ ký.
2. **Chưa nối adapter vào `cmd/crm` (`facebook.Adapter(cfg)` vẫn trả nil).** `channel.Registry` cần đủ
   `MessagingAdapter` (Send, SendPolicy, Lookup, HealthCheck, Backfill, ClassifyError) — phần ra là Task 148; khai
   hàm giả ở đây sẽ trùng tên với tệp của 148. Nối riêng nguồn vào ingest bây giờ cũng không an toàn: worker sẽ tách
   ra việc `inbound.facebook` chưa có handler (Task 153) và dồn ở hàng chờ. Vì vậy `wiring.go` thêm
   `Source(cfg) webhooks.Source` (adapter phần vào khi `cfg.Meta.Enabled` và đủ app secret; không thì nil), có test,
   để Task 148 đổi `Adapter(cfg)` trả nó khi adapter đủ. Phương án khác: sửa `wire_m4.go` nối nguồn riêng (trái ghi
   chú "task sau không sửa tệp này" và gây việc mồ côi). Đảo ngược: một dòng ở `Adapter`.
3. **`RunContract` phần (1)–(4) kiểm tại chỗ.** `channeltest.RunContract` là của Task 145 (chạy song song, chưa có
   trên `m4`). Bốn ý (1) fixture tách đúng sự kiện, (2) tách hai lần cùng khóa, (3) loại lạ vào `Skipped`, (4) đổi
   một byte → chữ ký sai được viết thành test trong `split_test.go`/`signature_test.go`; Task 148 nối
   `RunContract(t, facebookHarness)` đủ (1)–(7) như kế hoạch đã ghi. Đảo ngược: thay test tại chỗ bằng RunContract.
4. **Mục lẻ không đọc được không làm hỏng cả gói.** Kế hoạch: "JSON hỏng → lỗi". Chọn: thân không phải JSON hay
   khung ngoài sai kiểu → lỗi (việc tách `dead` theo M0); một mục `messaging[]`/`changes[]` sai kiểu (vd. `timestamp`
   là chuỗi) → bỏ vào `Skipped["<loại>:invalid"]` và vẫn tách các mục còn lại, vì Meta gói nhiều Page một lần: một
   mục lạ không được làm mất tin của Page khác. Đếm ở chỉ số `inbound_skipped`.
5. **Động từ comment của Meta.** Meta có `add, edit, edited, remove, delete, hide, unhide, update, block, …`. Chuẩn
   hóa: `add`→`add`; `edit|edited|update`→`edited`; `remove|delete`→`remove`; `hide`→`hide`; `unhide`→`unhide`;
   khác → `Skipped["feed:comment_<verb>"]` (theo mẫu tên đã lọc). `referral` không có kiểu payload riêng ở Task 143:
   dùng `MessagePayload{PageID, UserID, SentAt}` (Task 153 chỉ ghi tin hệ thống "Khách đến từ quảng cáo", Q34).
   `policy-enforcement` (tên khóa có gạch ngang trong tài liệu Meta; nhận cả `policy_enforcement`) nằm trong
   `messaging[]`. `live_videos`: `Status` viết hoa (`LIVE`, `LIVE_STOPPED`, như Task 170 dùng),
   `OccurredAt` = `entry.time`. `entry.time` nhận cả giây (gói `changes[]` của Meta) lẫn mili giây (gói messaging):
   số nhỏ hơn 1e11 là giây (sửa theo reviewer lần 1). Mọi chi tiết chờ đối chiếu payload thật ở E3 (README của testdata).
6. **Kiểu adapter tên `Channel`, không `Adapter`.** Kế hoạch ghi `New(o) *Adapter`, nhưng Task 143 đã khai hàm
   `facebook.Adapter(cfg)` trong `wiring.go` (cmd gọi), nên không thể có kiểu cùng tên trong gói. Chọn
   `facebook.Channel` (đọc là "kênh Facebook", hiện thực `channel.MessagingAdapter` khi Task 148 thêm phần ra).
   Phương án khác: đổi tên hàm của 143 (phải sửa `wire_m4.go`, trái ghi chú của tệp đó). Đảo ngược: gopls rename.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| GET xác minh: subscribe + token đúng → challenge; sai token, unsubscribe, token cấu hình rỗng, challenge lạ → false | `TestChallenge` | `go test -count=1 -v -run '^(TestChallenge\|TestVerifySignature\|TestSplit_.*\|TestSource\|FuzzSplit)$' ./internal/adapters/facebook/` | `--- PASS: TestChallenge` |
| Chữ ký trên thân thô: đúng → true; đổi 1 byte (đầu, giữa, cuối), thiếu header, chỉ sha1, hex sai, secret rỗng → false (contract (4)) | `TestVerifySignature` | như trên | `--- PASS: TestVerifySignature` |
| Mọi fixture (21 tệp, đủ loại) tách đúng loại, `DedupeKey` theo bảng khóa, `OrderKey`, Page, `OccurredAt`; gói 2 Page 7 sự kiện → 7, Skipped rỗng; tách hai lần cùng khóa, cùng payload (contract (1), (2)) | `TestSplit_Fixtures` | như trên | `--- PASS: TestSplit_Fixtures` |
| `feed:status` → `Skipped["feed:status"]=1`; `object:user` → `Skipped["object:user"]=1`; loại lạ đếm, mục hỏng không làm mất mục khác; JSON hỏng → lỗi không trích thân (contract (3)) | `TestSplit_SkippedAndErrors` | như trên | `--- PASS: TestSplit_SkippedAndErrors` |
| Echo CRM `EchoAppID=1626301872631379` + `Metadata`; echo Business Suite `263902037430900`; trả lời nhanh, postback, tệp đính kèm | `TestSplit_MessagePayloads` | như trên | `--- PASS: TestSplit_MessagePayloads` |
| Đã nhận, đã xem, cảm xúc, sửa tin, referral, `policy-enforcement`/`policy_enforcement` | `TestSplit_OtherMessagingPayloads` | như trên | `--- PASS: TestSplit_OtherMessagingPayloads` |
| Comment sửa hai nội dung → hai khóa, cùng nội dung → trùng; comment của Page → `FromPage`; động từ chuẩn hóa; live `LIVE` | `TestSplit_CommentAndLive` | như trên | `--- PASS: TestSplit_CommentAndLive` |
| `entry.time` giây (changes) và mili giây (messaging) → cùng giờ; comment thiếu `created_time` lấy giờ entry | `TestSplit_EntryTimeUnits` | như trên | `--- PASS: TestSplit_EntryTimeUnits` |
| `Source(cfg)` chỉ khi Meta bật và có secret, nil thật khi không; `Adapter(cfg)` vẫn nil; `Features` | `TestSource` | như trên | `--- PASS: TestSource` |
| ingest e2e: GET `abc`; POST không chữ ký 401 không lưu; có chữ ký 200, 1 bản nhận, 1 việc `webhook.split`; worker → đúng 7 việc `inbound.facebook` tenant A, via webhook; giao lại cùng gói → vẫn 7 (HT03); Page không tuyến → `inbound_unrouted`=1 | `TestIngestE2E` | `go test -race -count=1 -v -run '^(TestIngestE2E\|TestIngestE2E_Skipped)$' ./internal/adapters/facebook/` | `--- PASS: TestIngestE2E (0.41s)` |
| Loại bỏ qua qua đường thật → `inbound_skipped{facebook:feed:status}`=1, không việc vào | `TestIngestE2E_Skipped` | như trên | `--- PASS: TestIngestE2E_Skipped (0.28s)` |
| `FuzzSplit` 30 s không panic, bất biến khóa/nhãn | `FuzzSplit` | `go test -count=1 -run '^$' -fuzz=FuzzSplit -fuzztime=30s ./internal/adapters/facebook/` | `fuzz: elapsed: 30s, execs: 343805 … PASS` |
| Cổng kiểm của task (build, vet, lint, jscpd, gitleaks, oasdiff, test-guard, kiến trúc, `-race` gói facebook, `cmd/crm`) | ci-task | `scripts/agent/ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh aafa087…)`; `tests/arch ok`, `internal/adapters/facebook ok`, `cmd/crm ok` |

## Test bị xóa hoặc tắt

(không có)

## E2E viết sẵn, chưa chạy

(không có: kế hoạch không có test trong tests/e2e cho task này; `ingest_e2e_test.go` là test của gói, chạy trong ci-task.)

## Khác kế hoạch

- Kiểu `Channel` thay `Adapter`, `New(appSecret, verifyToken)` thay `New(o Options)`: Quyết định 6, 1.
- `facebook.Adapter(cfg)` chưa trả adapter thật; thêm `Source(cfg)` trong `wiring.go` và test `source_test.go`
  (không có trong danh sách tệp của kế hoạch): Quyết định 2.
- `RunContract(t, facebookInboundHarness)` (1)–(4) viết thành test tại chỗ: Quyết định 3.
- Postback không có `mid` (bản cũ của Meta) dùng khóa `b:<psid>:<ts>` (kế hoạch không ghi).

## Việc phát hiện thêm

- Echo của tin nhắn riêng (private reply) có thể mang `recipient.comment_id` thay `recipient.id`; hiện bị đếm
  `messaging:invalid`. Cần đối chiếu ở E3/E8 (Task 181) và xử lý ở Task 148/157 nếu đúng.
- `channel.MessagePayload` không có trường cho `referral.ref`/`ad_id`; Task 153 hiện chỉ cần tin hệ thống (Q34).

## Reviewer

Kết luận: ĐẠT (lần 2).

- Lần 1: CẦN SỬA. Hàm `live` và nhánh dự phòng của comment đọc `entry.time` luôn theo mili giây. Theo tài liệu
  Meta, gói `changes[]` (feed, live_videos) gửi số này theo giây, nên giờ phiên live sẽ rơi về năm 1970. Fixture
  cũng đặt mili giây, nên test chỉ xác nhận lại giả định của chính nó. Đã sửa: `ms()` nhận cả hai đơn vị (số nhỏ
  hơn 1e11 là giây); `live_videos.json` đổi sang giây; thêm test `TestSplit_EntryTimeUnits`. Ghi chú phụ:
  `{1,40}` → `{1,30}` trong tệp task.
- Lần 2: ĐẠT, không còn phát hiện. Giờ của messaging và khóa chống trùng không đổi. Ghi chú không bắt buộc:
  `split_fuzz_test.go` chấp nhận nhãn tới 40 ký tự, rộng hơn mã (30); để nguyên.
