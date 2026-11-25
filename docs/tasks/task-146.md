# Task 146 — Graph client, phân loại lỗi, tải tệp an toàn, máy chủ Meta giả

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 146, M4-06). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**.
  - Gói `internal/adapters/facebook` (khung Task 143, chỉ có `Key`, `Adapter`): thêm `Options`, `NewClient`,
    `Client.Do`, `Client.Download`, `Meta`, `Usage`, `ParseUsage`, `Usage.ShouldSlowDown`, `GraphError`, `SentState`,
    `Classify`, `Reason`, `RetryAfter`, `ErrHostNotAllowed`, `ErrPrivateAddress`, `ErrTooLarge`. `Adapter()` giữ nguyên
    (Task 147 trả adapter thật). Chưa gói nào ngoài `cmd/crm` nhập `facebook`; `cmd/crm` chỉ dùng `Key`, `Adapter`.
  - `channel.DownloadInfo` (dùng ở 1 chỗ: interface `AttachmentDownloader`, chưa có bản hiện thực) thêm trường
    `SHA256 string` (kế hoạch ghi `DownloadInfo{ContentType, Size, SHA256}`; `Download` của Graph client trả đúng kiểu
    của channel để adapter Task 147 hiện thực `AttachmentDownloader` không phải đổi kiểu).
  - Gói mới `internal/testkit/fakegraph` (chỉ thư viện chuẩn), lệnh mới `tools/fakegraph`.
- Gói phụ thuộc ngược (go list): `internal/modules/channel` ← `cmd/crm`, `internal/adapters/facebook`,
  `internal/adapters/fake`, `internal/modules/conversation/internal/deps`; `internal/adapters/facebook` ← `cmd/crm`.
  Gói mới chưa ai nhập (fakegraph: test của facebook và `tools/fakegraph`).
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không.
- Test sẽ chạy: `go test -race ./internal/adapters/facebook/ ./internal/testkit/fakegraph/ ./tools/fakegraph/`,
  `./internal/modules/channel/`, `./cmd/crm/` (nhập channel), `./tests/arch/` (luật (n), testkit không nhập module).
  Không chạm vùng rủi ro cao của `HIGH_RISK_DIRS` (kế hoạch coi `facebook/download.go` là vùng rủi ro M4: có test
  bảng đủ lớp địa chỉ cấm).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (logging, observability,
  metrics, urlerr, config, webhooks, files, testkit); tìm `net.Dialer`, `IsPrivate`, `httptrace`, `DetectContentType`
  trong mã: chưa có bộ chặn địa chỉ nội bộ hay theo dõi "đã gửi trọn" nào. Adapter hãng (`carrier/vtp/client.go`)
  phân "chưa gửi" theo lỗi dial (`*net.OpError` Op `dial`).
- Dùng lại: `logging.Secret` (token, app secret), `observability.ScrubMessage` (message lỗi của Meta),
  `urlerr.Strip` (bỏ URL khỏi lỗi net/http), `metrics.Registry` (`graph_ms`, `graph_errors`), `config.MetaConfig`
  (cùng tên trường cho `Options`), `channel.ErrorClass`, `channel.DenyReason`, `channel.DownloadInfo`.
- Viết mới vì: (1) "đã gửi trọn" theo `httptrace.WroteRequest` chính xác hơn cách đoán theo lỗi dial của vtp (TLS
  lỗi, kết nối bị đóng trước khi gửi đều đúng là chưa gửi; kế hoạch đòi rõ); bảng mã lỗi của Meta là riêng của
  adapter, không chung với hãng. (2) Bộ chặn địa chỉ nội bộ (`Dialer.Control`) chỉ adapter facebook cần ở M4; đặt
  trong adapter, khi mốc sau có nơi thứ hai tải URL ngoài thì dời vào `internal/platform/<việc>` (ghi ở Việc phát hiện
  thêm). (3) `fakegraph` khác `tools/fakecarrier` (khác giao thức hoàn toàn), không trùng mã.

## Soát thiết kế

- 5.1–5.3 (chủ dữ liệu, bảng module khác, view): không áp dụng — không bảng, không truy vấn.
- 5.4, 5.5: không áp dụng — không transaction.
- 5.6 Gọi ra ngoài: client chỉ là lớp gọi; lệnh `commands` và `in_doubt` ở Task 148, 156. Client cung cấp đúng điều
  đó cần: `Sent = not_sent` chỉ khi yêu cầu **chưa ghi trọn** lên kết nối (httptrace), mọi lỗi sau đó là `maybe_sent`
  → lớp `unknown` (không tự gửi lại). Không tự thử lại trong client.
- 5.7 tenancy: không áp dụng. 5.8: adapter được nhập `net/http` (không phải module). 5.9: không đường ghi API.
- 5.10: không Valkey. 5.11: chỉ thư viện chuẩn và gói nội bộ; không thư viện mới.
- 5.12 Theo mẫu: adapter nhận `*http.Client` qua tham số (không `http.DefaultClient`), luật (n) (chỉ nhập platform và
  channel); lỗi là sentinel/kiểu lỗi của gói, kiểm bằng `errors.Is/As`.
- 6.1–6.5, 6.10: không áp dụng (không database, không tiền).
- 6.6, 6.8: việc nền thử lại theo lớp lỗi do Task 148, 156 dùng `Classify`; `not_sent_retry` an toàn để gửi lại,
  `unknown` không.
- 6.7 Quá tải: 429 và mã `4, 17, 32, 613, 80001, 80006` → `rate_limited`, `RetryAfter` từ
  `estimated_time_to_regain_access` (phút) của header mức dùng, tối thiểu 60 s; `Usage.ShouldSlowDown` ≥ 80%.
- 6.9 Treo: hạn 10 s mỗi lời gọi Graph, 30 s mỗi lần tải tệp.
- 6.11: không quy luật database; phần quy luật là bảng phân loại (test bảng 14 ca) và bộ chặn địa chỉ (test bảng).
- 6.12 Dữ liệu cá nhân: token không bao giờ vào URL (gửi bằng header `Authorization`), lỗi bỏ URL (`urlerr.Strip`),
  message của Meta qua `ScrubMessage`; log debug chỉ có phương thức, đường dẫn đã bỏ mã, mã trạng thái; test đọc log.
- 6.13 Quyền: không áp dụng. An toàn tải tệp (04 mục 13): chỉ `https`, tên máy theo hậu tố cho phép, chặn địa chỉ
  nội bộ ở lúc nối (sau DNS) và lúc chuyển hướng, tối đa 3 lần chuyển, tối đa 25 MiB.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mọi yêu cầu có tiền tố `/v26.0/` và `appsecret_proof` đúng; token không nằm trong URL | `TestDo_VersionPrefixAndProof` | `go test -race -count=1 ./internal/adapters/facebook/` | đỏ trước (không biên dịch: `undefined: facebook.Client`), sau `ok 11.970s` |
| Đổi app secret ở client → fakegraph trả lỗi 100, lớp `permanent` | `TestDo_WrongAppSecretRejected` | như trên | `ok` |
| Log debug mỗi lời gọi không chứa token `EAAB-test-1`, app secret, SĐT; `GraphError.Message`, `Error()`, lỗi mạng đã lọc `0912345678` và token | `TestDo_NoSecretsInLogsAndMessages`, `TestGraphError_MessageScrubbed` | như trên | lần đầu đỏ (`message chứa "EAAB-test-1"`: Meta chép token vào message), thêm che token rồi `ok` |
| Chỉ số `graph_ms`, `graph_errors` có nhãn đã bỏ mã đối tượng | `TestDo_MetricsLabelWithoutIDs` | như trên | `ok` |
| Bảng phân loại: 14 ca của kế hoạch + từng mã giới hạn, mã token; lý do và câu tiếng Việt | `TestClassify`, `TestClassify_NotGraphError` | như trên | `ok` |
| `{code:613}` + `estimated_time_to_regain_access: 5` → `rate_limited`, `RetryAfter = 5 phút`; tối thiểu 60 s | `TestDo_RateLimitRetryAfterFromUsage`, `TestRetryAfter` | như trên | `ok` |
| Đóng kết nối trước khi gửi → `not_sent`/`not_sent_retry`; đọc thân rồi đứt → `maybe_sent`/`unknown`; 502 HTML → `unknown` | `TestDo_SentState` (3 ca) | như trên; thêm `-count=5` | `ok` cả 5 lượt |
| Treo 11 s sau khi đọc → `unknown` sau khoảng 10 s, khớp `context.DeadlineExceeded`, không tự gửi lại | `TestDo_HangAfterReadTimesOut` | như trên; sau sửa trần (lượt review 1): `go test -race -count=1 -run '^TestDo_HangAfterReadTimesOut$' ./internal/adapters/facebook/` | `ok` (gói 11.97 s); sau sửa `ok 11.462s` |
| Header mức dùng 85 → `ShouldSlowDown` đúng, 79 sai, 80 đúng; X-Business-Use-Case-Usage lấy lớn nhất | `TestDo_UsageHeader`, `TestParseUsage` | như trên | `ok` |
| Tải tệp từ `scontent.fsgn5-1.fbcdn.net` (DNS giả) → đọc được, `image/jpeg` dù header `text/html`, SHA256, cỡ đúng | `TestDownload_FromMetaHostSniffsType` | như trên | `ok` |
| `evil.example`, `http://`, DNS trả `10.0.0.5`/`169.254.169.254`, IP viết thẳng, hậu tố giả, chuyển hướng tới `169.254.169.254`, máy lạ, http, tên trỏ nội bộ, quá 3 lần chuyển → đúng lỗi | `TestDownload_Rejects` (12 ca + 1 ca chuyển hướng hợp lệ) | như trên | `ok` |
| 25 MiB + 1 byte → `ErrTooLarge`; trần nhỏ; trần > 25 MiB bị hạ | `TestDownload_SizeLimit` | như trên | `ok` |
| Bộ chặn địa chỉ: loopback, riêng tư, ULA, link-local, multicast, `0.0.0.0/8`, `100.64.0.0/10`, IPv4 dạng IPv6; địa chỉ Meta không bị chặn | `TestBlockedIP` | như trên | `ok` |
| fakegraph: proof sai → 100, thiếu token → 104, `RevokeToken` → 190 | `TestServer_ProofAndToken` | `go test -race -count=1 ./internal/testkit/fakegraph/` | `ok 1.781s` |
| fakegraph: gửi tin, nhắn riêng một lần (lần hai 10900), trả lời comment, ẩn comment, đăng ký/hủy ứng dụng | `TestServer_SendCommentSubscribe` | như trên | `ok` |
| fakegraph: đổi mã đăng nhập, `me/accounts`, `debug_token`; hồ sơ, bài, hội thoại, feed, live, comment live có `since`, `summary` | `TestServer_OAuthAccountsDebugToken`, `TestServer_ReadObjects` | như trên | lần đầu đỏ (giá trị `since` của test sai), sửa test rồi `ok` |
| fakegraph: `FailNext` một lần, header mức dùng; luồng SSE nhận comment, `DropStream` đóng; webhook ký `X-Hub-Signature-256` | `TestServer_FaultsAndUsage`, `TestServer_LiveStream`, `TestServer_EmitWebhookSigned` | như trên | `ok` |
| `tools/fakegraph`: từ chối `CRM_ENV=production`, secret sai dạng; Page dev có sẵn; `/_fake/fail-next`, `/_fake/revoke`, `/_fake/calls` | `TestLoadConfig`, `TestServer_DevSeedAndAdmin` | `go test -race -count=1 ./tools/fakegraph/` | `ok 2.169s` |
| Luật ranh giới (n), testkit không nhập module | `TestBoundaries*`, `TestTestkitNoModules` (có sẵn) | `go test -count=1 ./tests/arch/` | `ok 220.468s` |
| Cổng tĩnh | — | `make jscpd`, `golangci-lint run` 4 gói, `go vet`, `make catalog-check` | jscpd lần đầu 4 bản lặp mới → sửa → `Found 0 clones`; lint `0 issues` |
| Toàn bộ cổng của task | — | ci-task | `CI-TASK XANH (so với điểm tách nhánh aafa087…)`; toàn bộ test trừ e2e (do đổi testkit) 360 s gồm `ok …/adapters/facebook`, `ok …/testkit/fakegraph`, `ok …/tools/fakegraph`, `ok …/tests/arch` |
| Gộp `m4` trước khi stage | — | `git merge m4` | `Already up to date.` |

## Test bị xóa hoặc tắt

(không có)

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 146 không có test e2e)

## Quyết định thay chủ shop

1. **Token gửi bằng header `Authorization: Bearer`, không đặt `access_token` trong URL** (kế hoạch ghi "thêm
   `access_token`"). Vì: URL hay lọt vào lỗi của net/http, log của proxy; Graph nhận token ở header, `appsecret_proof`
   vẫn ở query. Phương án khác: đúng chữ kế hoạch (query). Đảo ngược: một dòng ở `client.go/newRequest`; fakegraph
   nhận cả hai cách. Thí nghiệm E3/E8 (Task 181) xác nhận trên Meta thật.
2. **"Đóng kết nối trước khi đọc thân" mô phỏng bằng `RefuseNextConn`** (máy giả đóng kết nối mới trước bắt tay TLS).
   Vì: khi máy giả đã đọc được dòng đầu yêu cầu thì client đã ghi trọn một yêu cầu nhỏ, không còn là "chắc chắn chưa
   gửi"; đóng trước bắt tay là ca "chưa gửi" đo được ổn định. Phương án khác: đóng sau khi đọc header (ra
   `maybe_sent`, không kiểm được nhánh `not_sent`).
3. **Bảng phân loại thêm hai dòng an toàn:** mã 1, 2 (Meta báo lỗi tạm) → `unknown` (không tự gửi lại; bảng gốc xếp
   vào "mã lạ → permanent" và đánh dấu tin hỏng dù có thể đã tới); mã 200–299 (cả nhóm lỗi quyền của Meta) →
   `policy`/`no_permission` (bảng ghi 200). HTTP 4xx không có JSON → `permanent` ("Facebook từ chối (HTTP N)").
   Đảo ngược: sửa `Classify`/`policyReason` và bảng `TestClassify`.
4. **`Download` đọc trọn tệp (≤ 25 MiB) vào bộ nhớ** rồi trả `io.ReadCloser`: SHA256 và kiểu nội dung phải biết trước
   khi trả. Kiểu trả là `channel.DownloadInfo` (thêm trường `SHA256`) để adapter Task 147 hiện thực
   `channel.AttachmentDownloader` không đổi kiểu. Phương án khác: ghi ra tệp tạm (cần thư mục ghi được ở worker).
5. **Dải bị chặn rộng hơn danh sách kế hoạch**: thêm `192.0.0.0/24`, `198.18.0.0/15`, `240.0.0.0/4` (gồm
   `255.255.255.255`), NAT64 `64:ff9b::/96`, 6to4 `2002::/16`, `fec0::/10`; địa chỉ không đọc được → chặn. Tải tệp
   không qua proxy (kiểm địa chỉ lúc nối phải là địa chỉ thật).

## Khác kế hoạch

- Các Quyết định 1–5 ở trên.
- `Options` thêm `Logger *slog.Logger` (test "log debug không chứa token" cần chỗ ghi log); `EmitWebhook` nhận `ctx`
  đầu tiên (luật Go 3) và trả `(int, error)`; thêm `RefuseNextConn`, `Sign`, `StreamCount`, hàm nạp dữ liệu (`AddPage`,
  `AddUser`, `AddProfile`, `AddPost`, `AddComment`, `AddLiveVideo`, `SetLiveStatus`, `AddInboundMessage`, `AddFile`,
  `AddRedirect`) và đọc lại (`Messages`, `CommentByID`, `Subscribed`); `NewHandler` cho `tools/fakegraph`.
- Lỗi thêm: `ErrTooManyRedirects`, `ErrDownloadStatus`; hàm `RetryAfter(err)`, `ParseUsage(h)`; `GraphError` có thêm
  `RetryAfter` (giá trị Meta báo; `RetryAfter(err)` áp tối thiểu 60 s).
- Tệp thêm ngoài danh sách: `internal/testkit/fakegraph/{graph.go,tls.go}` (các đường Graph, chứng chỉ tự ký có tên
  máy Meta), `tools/fakegraph/{admin.go,main_test.go}`.
- `tools/fakegraph` nghe `127.0.0.1:4193` bằng HTTP thường (không TLS): đủ cho console dev và k6 gọi Graph; tải tệp
  đính kèm qua bản chạy độc lập không thử được (bộ tải chỉ nhận https và chặn loopback — đúng ý).
- Test treo dùng hạn thật 10 s (một test song song, gói ~12 s), không thêm nút chỉnh hạn chỉ cho test.

## Việc phát hiện thêm

1. Bộ chặn địa chỉ nội bộ (`download.go`) hiện chỉ adapter facebook dùng. Khi mốc sau có nơi thứ hai tải URL từ bên
   ngoài, dời vào `internal/platform/<việc>` thay vì chép.
2. Bảng mã lỗi, dạng header mức dùng, cách nhận token ở header theo tài liệu Meta; đối chiếu bằng payload thật ở thí
   nghiệm E3, E8 (Task 181, cần OK của chủ shop).
3. Thêm gói con dưới `internal/testkit/` làm ci-task chạy toàn bộ test (luật "đổi testkit"), dù gói chỉ dùng thư viện
   chuẩn và không gói nào ngoài test, `tools/` nhập. Có thể cân nhắc cho bộ chọn test bỏ qua gói con mới của testkit.
4. (Reviewer, ghi chú 2) Lỗi dựng yêu cầu (vd `json.Marshal` thân hỏng) hiện là `not_sent` → `not_sent_retry`: lỗi
   của mã sẽ thử lại tới `dead` (không gửi trùng). Task 148 cân nhắc xếp ca này vào `permanent`.
5. (Reviewer, ghi chú 3) HTTP 4xx không có JSON (vd 408 từ tầng trung gian sau khi yêu cầu đã tới) xếp `permanent`;
   đối chiếu ở E3/E8, nếu gặp thì chuyển 408 sang `unknown`.

## Reviewer

Kết luận: ĐẠT (lượt 1 và lượt 2, 25/11).

- Lượt 2 (sau khi sửa ghi chú 1): ĐẠT, không có phát hiện phải sửa. Trần 20 s chỉ còn là chốt chống treo: hạn 10 s
  không có tác dụng thì máy giả trả lời sau 11 s và test hỏng ở kiểm lỗi; cận dưới 9,5 s vẫn giữ. Ghi chú không bắt
  buộc: vòng chờ `StreamCount` trong `TestServer_LiveStream` không có chốt thời gian; `tools/fakegraph` khi tắt không
  đóng luồng SSE đang mở (chờ hết 5 s) — công cụ dev, không ảnh hưởng tính đúng của CRM.

- Lượt 1: ĐẠT, không có phát hiện phải sửa. Reviewer đã đọc kỹ ba vùng rủi ro: phân "đã gửi/chưa gửi" theo
  `httptrace.WroteRequest` (POST không bị net/http gửi lại ngầm; lỗi sau khi ghi trọn luôn `maybe_sent`); bộ tải tệp
  (chỉ https, hậu tố tên máy đúng ranh giới nhãn, IP nội bộ viết thẳng, `Dialer.Control` sau DNS chống rebinding,
  `Proxy: nil`, kiểm lại mỗi lần chuyển hướng, trần 25 MiB trên dữ liệu đã giải nén, lối 127.0.0.1 chỉ trong
  `export_test.go`); dữ liệu cá nhân (token ở header, `urlerr.Strip`, `ScrubMessage` + che token, log chỉ nhãn đã bỏ
  mã). Năm Quyết định thay chủ shop an toàn, không trái spec 04 mục 5.3, 13. Ảnh hưởng khớp; bảng bằng chứng kiểm đúng.
- Ghi chú không bắt buộc: (1) trần 10,9 s của `TestDo_HangAfterReadTimesOut` là ngưỡng giờ thật làm tiêu chí (bẫy
  Task 400) → **đã sửa**: trần thành chốt chống treo 20 s; (2), (3) ghi ở Việc phát hiện thêm 4, 5.
