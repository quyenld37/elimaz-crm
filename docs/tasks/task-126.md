# Task 126 — M3-35: Viettel Post — hủy, tra trạng thái, webhook, nhãn, định dạng bảng kê

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 126, M3-35). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `vtp.Adapter` thêm phương thức (cài interface `fulfillment.CarrierAdapter` đã có, không đổi chữ ký): `Cancel`,
    `Redeliver`, `Labels`, `VerifyWebhook`, `SplitWebhook`, `Track`/`GetStatus` (viết rõ `ErrNotSupported`).
    `Capabilities()` đổi giá trị: thêm `Cancel`, `Webhook`, `Labels`, `LabelsBatch`, `Redeliver` (Track, LookupByRef,
    UpdateAddress, Statement, CancelIdempotent vẫn false). Chỗ dùng `vtp.`: `contract_test.go`, `adapter_test.go` (gói
    vtp); không có chỗ dùng ngoài gói (vtp chưa nối vào `carrier.All`, `registry.go` dùng `manual.New(vtp.Key)`).
  - `vtp.Config` thêm trường `PrintURL` (máy in nhãn; rỗng = theo môi trường). Chỗ dùng `vtp.Config{`: chỉ test gói vtp.
  - `vtptest`: thêm `OpUpdate`, `ModeSystemError`, `Print`, `Status`, `Prints`, `Options.UpdateOrder`,
    `Options.PrintingCode`; chế độ `timeout_after_send` cho mọi thao tác; `Push` ghi trạng thái kiện (trước là không làm
    gì); đường mới `/v2/order/UpdateOrder` (TYPE 4 là thao tác `cancel`, loại khác `update`), `/v2/order/printing-code`
    (thao tác `labels`), `/DigitalizePrint/report.do`. Chỗ dùng: chỉ test gói vtp.
  - Nội bộ gói vtp (không xuất): `decode` trả thêm `message` của phong bì (chỗ gọi: `client.go`, `auth.go`), `call` tách thành `call` + `exchange` (mã in
    nằm ở `message`); loại việc mới `kindCancel`, `kindUpdate` (lỗi sau khi gửi là không rõ), `kindLabel` (việc đọc).
  - `carriertest` (nội bộ test, không đổi ký hiệu xuất): `webhookSample` thêm trường `tokenOnly` — nhánh mới cho hãng
    xác thực webhook bằng token chung không ký thân (như Task 125 thêm nhánh `LookupByRef = false`); hãng khác (fake)
    không khai trường này nên giữ nguyên ca "thân bị sửa". Thêm `testdata/vtp/webhook.json`.
- Gói phụ thuộc ngược (go list): `vtp` ← `internal/adapters/carrier` (`registry.go`, chỉ dùng `vtp.Key`, không đổi) và `tests/arch`; `carriertest` ← test của
  `carriertest` (fake, khung), `vtp` — chạy lại cả hai.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có (VTP chưa nối vào registry, màn tài khoản hãng vẫn thấy khung giao tay).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `go test -race ./internal/adapters/carrier/...`, `go test ./tests/arch/...`, rồi cổng kiểm của task
  (ci-task).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`webhooks`, `urlerr`,
  `selftest` — mẫu kiểm chữ ký HMAC), `internal/adapters/carrier/webhook.go` (`WebhookSource`),
  `fulfillment.SplitSignals`, `config.CarrierWebhookSecrets` (bí mật webhook theo khóa hãng), client của Task 125
  (`call`, `send`, `decode`, `classifyBody`, `afterSend`), `fake` (Labels, Cancel của hãng giả).
- Dùng lại / viết mới vì:
  - Dùng lại `Adapter.call` (đăng nhập, lỗi token, phong bì) cho UpdateOrder, printing-code; `classify*`/`afterSend`
    mở rộng bằng loại việc mới (`kindCancel`, `kindUpdate`, `kindLabel`) thay vì viết phân lớp lỗi thứ hai.
  - `crypto/subtle.ConstantTimeCompare` (thư viện chuẩn) cho so token webhook; không dùng HMAC của `selftest`/`fake`
    vì VTP không ký thân (tài liệu: header `Authorization` là "token xác thực").
  - Tải PDF nhãn viết mới trong `labels.go` (máy đích cố định theo môi trường, không theo URL hãng trả; không theo
    chuyển hướng; không gửi header `Token` sang máy in) — `fake` tải nhãn từ chính máy chủ API nên không có phần này.
  - Không có gói chung đọc giờ `dd/MM/yyyy HH:mm:ss` giờ Việt Nam; viết hàm nhỏ trong gói vtp (`time.FixedZone`
    +07:00 thay `time.LoadLocation`: máy chủ có thể thiếu tzdata; Việt Nam không có giờ mùa hè).
  - jscpd lần chạy đầu báo 2 đoạn trùng, sửa cả hai, không thêm baseline: `printURL` trùng `baseURL` → gộp thành
    `byEnvironment` (cùng mục đích: chọn máy đích theo môi trường); `truncateRunes` trùng `truncate` của
    `internal/platform/observability/alerter.go` → không dùng chung được (luật kiến trúc (j): adapter không nhập
    `observability`; hai chỗ đổi vì lý do khác nhau), viết lại bằng `[]rune`.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1–5.4, 5.7, 5.9, 5.10: không áp dụng — adapter không có bảng, database, transaction hay đường ghi API.
- 5.5, 5.6: `Cancel`, `Redeliver` chỉ chạy trong `Execute` của lệnh (ngoài transaction); lỗi sau khi gửi (quá giờ,
  5xx, 205, thân không đọc được) là `ClassUnknownOutcome` → `in_doubt`, không tự gửi lại; `CancelIdempotent = false`
  (tài liệu không nói hủy lại đơn đã hủy trả gì — Q-24). Nhãn là việc đọc: lỗi sau khi gửi là chưa gửi, gửi lại được.
- 5.8: module không nhập adapter; adapter nhập `net/http` (luật (j) cho phép).
- 5.11: chỉ thư viện chuẩn.
- 5.12: theo mẫu adapter của Task 125 và luật B10 ("webhook chỉ là tín hiệu"): `SplitWebhook` chỉ đưa mã vận đơn,
  mã tham chiếu, khóa sự kiện; không đưa trạng thái, tiền ra lõi. Không đổi luật tin webhook (điểm chờ chủ shop).

Mục 6 (đúng khi chạy đồng thời):
- 6.1: không có trạng thái mới dùng chung trong adapter (bộ nhớ mã truy cập của Task 125 giữ nguyên); máy chủ giả có
  khóa; test chạy `-race`.
- 6.2: hủy gửi lại không chắc an toàn → không tự gửi lại (CancelIdempotent false); webhook trùng (VTP gửi lại tối đa
  5 lần, "có thể trùng hoặc thừa") → khóa sự kiện ổn định `vtp:<mã vận đơn>:<mã trạng thái>:<giờ UTC>` để ingest
  chống trùng.
- 6.3–6.5, 6.10, 6.11: không áp dụng (không database, không tiền: COD trong webhook không đọc).
- 6.6–6.8: lớp lỗi theo bảng của Task 125 (429 → chờ `Retry-After`; 503/dial → chưa gửi; còn lại theo loại việc).
- 6.9: hạn giờ client hủy, phát tiếp, nhãn 15 s (Global Constraints); thân PDF tối đa 20 MB (Task 107).
- 6.12: SĐT, tên người nhận, tên/SĐT bưu tá trong webhook không ra khỏi `SplitWebhook`; lỗi không chép thông báo hãng;
  ghi chú phát tiếp không vào lỗi. Test quét SĐT trên kết quả và lỗi.
- 6.13: kiểm webhook mặc định từ chối: bí mật rỗng, thiếu header, sai token → false (so thời gian hằng).
- SSRF (threat model, Task 107): tải nhãn chỉ từ máy in VTP khai cứng theo môi trường; mã in đi qua `url.QueryEscape`;
  không theo chuyển hướng.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test viết trước, thất bại trước khi có mã | 14 test mới/đổi của gói vtp | `go test ./internal/adapters/carrier/vtp/` (trước khi viết cancel/labels/webhook/track.go) | `--- FAIL: TestCapabilities`, `TestCancel*` (4), `TestRedeliver`, `TestLabels*` (4), `TestPrintHostByEnvironment`, `TestSplitWebhook*` (2), `TestVerifyWebhook`; `FAIL …/vtp` |
| `carriertest.Run` đủ khả năng mới (Cancel, Webhook, Labels+LabelsBatch, Redeliver; Track, UpdateAddress, Statement = ErrNotSupported) | `TestContractVTP` (17 phần, Create có 6 ca con) | `go test -race -v -run TestContractVTP$ ./internal/adapters/carrier/vtp/` | `--- PASS: TestContractVTP`, `…/Cancel`, `…/Webhook`, `…/Labels`, `…/Redeliver`, `…/Track`, `…/Statement` PASS |
| Nhánh `tokenOnly` không nới cho hãng khác | `TestContractFake` | `go test -race -run TestContractFake ./internal/adapters/carrier/carriertest/` | `ok` (fake vẫn qua ca "thân bị sửa") |
| VC05: hủy vận đơn chưa lấy → CancelDone, TYPE 4 theo mã VTP | `TestCancelNotPickedUp` | `go test -race -v -run 'TestCancel' ./internal/adapters/carrier/vtp/` | `--- PASS: TestCancelNotPickedUp` |
| Hủy đã lấy → từ chối (Q2), không chép thông báo hãng | `TestCancelPickedUpIsRejected` | như trên | `--- PASS` |
| Lớp lỗi hủy: quá giờ, 205 → không rõ; 429; 503; auth; token hết hạn đăng nhập lại một lần; mã rỗng không gọi | `TestCancelErrorClasses` (7 ca) | như trên | `--- PASS: TestCancelErrorClasses` |
| CancelIdempotent false (Q-24) | `TestCancelTwiceNotIdempotent` | như trên | `--- PASS` |
| VC17: phát tiếp chỉ khi 505, NOTE ≤ 150 ký tự, quá giờ → không rõ | `TestRedeliver` | `go test -race -v -run TestRedeliver ./internal/adapters/carrier/vtp/` | `--- PASS: TestRedeliver` |
| Sửa địa chỉ không hỗ trợ | `TestUpdateAddressNotSupported` | `-run TestUpdateAddress` | `--- PASS` |
| VC11: không có API hành trình → Track/GetStatus ErrNotSupported | `TestTrackNotSupported` | `-run TestTrack` | `--- PASS` |
| Nhãn: `%PDF-`, nhiều mã một lần, mã in escape, không gửi Token sang máy in | `TestLabelsBatch` | `-run 'TestLabels\|TestPrintHost'` | `--- PASS: TestLabelsBatch` |
| Nhãn: 0/101 mã/mã rỗng từ chối không gọi; mã lạ, 429, quá giờ | `TestLabelsBadInput`, `TestLabelsErrors` | như trên | `--- PASS` (2) |
| SSRF: không theo chuyển hướng (máy đích 0 lời gọi), không PDF, quá 20 MB, 404 → permanent | `TestLabelsDownloadGuards` (4 ca) | như trên | `--- PASS: TestLabelsDownloadGuards` |
| Máy in khai cứng theo môi trường; test không chạm máy thật | `TestPrintHostByEnvironment` | như trên | `--- PASS` |
| TI02/VC12: 5 webhook mẫu → một WebhookSignal mỗi thân, giờ VN → UTC, không SĐT | `TestSplitWebhookSamples` (5 ca) | `-run 'TestSplitWebhook\|TestVerifyWebhook'` | `--- PASS: TestSplitWebhookSamples` |
| Thân sai → lỗi; giờ hỏng → khóa rỗng | `TestSplitWebhookBadBodies` (6 + 3 ca) | như trên | `--- PASS` |
| Token sai/thiếu/bí mật rỗng → VerifyWebhook false | `TestVerifyWebhook` (11 ca) | như trên | `--- PASS` |
| Khả năng khai đúng | `TestCapabilities` | `-run TestCapabilities` | `--- PASS` |
| Bảng kê `vtp_v1` 20 dòng | — | — | **không làm: chờ tệp mẫu của VTP** (Khác kế hoạch) |
| Cổng kiểm của task (build, vet, lint, jscpd, gitleaks, oasdiff, test-guard, test gói đổi + phụ thuộc + e2e/arch) | — | ci-task (log ngoài worktree) | `CI-TASK XANH (so với điểm tách nhánh 03306ea…)`; lần đầu ĐỎ vì jscpd 2 đoạn trùng (đã sửa) và Docker của máy tắt giữa chừng (PostgreSQL 5600 từ chối; tự lên lại, không đụng tới) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- **Bảng kê `vtp_v1` (`statement.go`, `statement_test.go`, `testdata/statement_vtp_v1.xlsx`): không làm — chờ tệp mẫu
  của VTP** (Step 0 của kế hoạch, giao việc của điều phối viên). `Capabilities.Statement` false, adapter không có
  `StatementFormats()`; kế toán dùng `elimaz_v1`. Ghi trong `docs/carriers/vtp.md` mục "Bảng kê COD".
- **`Track` "hành trình mẫu 5 sự kiện → Canonical, giờ VN → UTC"**: VTP không có API hành trình (Task 125), nên
  `Track`/`GetStatus` trả `ErrNotSupported` (`TestTrackNotSupported`). Phần "5 sự kiện, giờ Việt Nam đổi sang UTC"
  kiểm trên 5 webhook mẫu (`TestSplitWebhookSamples`: khóa sự kiện mang thời điểm UTC).
- **"Hủy đã lấy → `CancelRejectedPickedUp` (theo mã lỗi của tài liệu)"**: tài liệu không có mã lỗi cho trường hợp này
  → trả lỗi `rejected` (Quyết định Q2).
- **`UpdateAddress`**: không làm (Quyết định Q5). `Redeliver` làm (UpdateOrder TYPE 3).
- Plan ghi "chữ ký/token sai (nếu VTP có) → `VerifyWebhook = false`": VTP có token trong header, không có chữ ký thân;
  contract chung thêm nhánh `tokenOnly` (Quyết định Q3).

## Quyết định thay chủ shop

- **Q1 — `Webhook = true` khi `Track = false`.** Chọn: khai `Webhook` true (adapter kiểm và tách được webhook đúng tài
  liệu), giữ `Track`, `LookupByRef` false, **không đổi luật tin webhook**. Hệ quả với lõi hiện tại (đọc mã
  `refresh_status.go`): tín hiệu → lệnh tra → `ErrNotSupported` → `rejected`/`not_supported`, trạng thái không đổi, vận
  đơn VTP cập nhật tay. VTP chưa nối vào `carrier.All` (Task 49) nên chưa có tác dụng thật. Phương án khác: khai
  `Webhook` false tới khi chủ shop chọn (a)/(b) — bỏ vì khi đó contract đòi `SplitWebhook` trả `ErrNotSupported`, mã
  kiểm/tách thành mã chết. Đảo ngược: đổi một dòng trong `Capabilities()`.
- **Q2 — Hủy bị từ chối.** Chọn: mọi từ chối của `UpdateOrder` TYPE 4 → lỗi lớp `rejected` (`vtp_<status>`), lệnh hủy
  `rejected`, việc hủy chờ người. Không trả `CancelRejectedPickedUp`: kết quả đó làm lõi áp sự kiện "đã lấy" (đơn về
  confirmed, hàng in_transit) mà từ chối có thể vì lý do khác (mã sai, đã hủy). Phương án khác: coi mọi 204 khi hủy là
  "đã lấy" — bỏ vì đoán. Task 49 hỏi mã lỗi (câu 8 của `docs/carriers/vtp.md`).
- **Q3 — Xác thực webhook.** Chọn: so header `Authorization` (bỏ `Bearer `) với bí mật webhook của hãng `vtp` trong
  cấu hình CRM, so thời gian hằng trên sha256; `TOKEN` trong thân không dùng (mẫu `0333333333` giống tên tài khoản,
  không phải bí mật). Contract chung: nhánh `tokenOnly` thay ca "thân bị sửa" (không phát hiện được khi hãng không ký
  thân) bằng ca "token khác bị từ chối"; chỉ áp cho mẫu khai `tokenOnly` (fake vẫn kiểm thân bị sửa). Phương án khác:
  chấp nhận khớp `Authorization` hoặc `TOKEN` — bỏ vì nới. Câu 9 gửi VTP.
- **Q4 — Nhãn.** Chọn khổ `type=2` (mục "Nhãn A6" của tài liệu), `showPostage=0` (không in cước), mã in hạn 10 phút,
  `Pages` = số mã. Máy in khai cứng theo môi trường; có `BaseURL` mà không có `PrintURL` thì dùng `BaseURL` (test không
  chạm máy thật). Phương án khác: `a6_1`, `showPostage=1` (như ví dụ của tài liệu). Là hằng trong `labels.go`.
- **Q5 — `UpdateAddress` false.** API `edit` đòi gửi lại cả đơn (người gửi, dịch vụ, `ORDER_PAYMENT`…), interface chỉ
  có địa chỉ mới; gửi thiếu có thể ghi đè dữ liệu của đơn. Phương án khác: đọc lại đơn từ CRM để gửi đủ — cần đổi
  interface hay module fulfillment (ngoài phạm vi).
- **Q6 — `CancelIdempotent` false** (Q-24): tài liệu không nói hủy lại đơn đã hủy trả gì; máy chủ giả trả 204.

## Việc phát hiện thêm

- **ĐIỂM CHỜ CHỦ SHOP — luật tin webhook của VTP** (spec B10: "webhook chỉ là tín hiệu, tra lại bằng API có xác
  thực"; VTP không có API đọc trạng thái/hành trình). Task này **không đổi luật**; hai phương án để chủ shop chọn:
  - **(a) Tin webhook VTP có điều kiện:** chỉ khi khớp bí mật (`VerifyWebhook`); chỉ nhận bước đi tới theo máy trạng
    thái (không lùi, không nhảy qua trạng thái cuối khác); mã lạ/mâu thuẫn (104, 202, 503, mã ngoài bảng) và bước lùi
    vào "Cần xem". Cần task riêng: tín hiệu mang thêm mã trạng thái + thời điểm (đổi `WebhookSignal` hay một đường
    `Track` từ hộp thư webhook đã lưu), sửa lệnh tra của lõi; rủi ro: token lộ là giả được trạng thái (VTP không ký thân,
    câu 9).
  - **(b) Giữ luật:** vận đơn VTP cập nhật tay (ghi tay trạng thái của Task 117/area_manual), webhook chỉ để thấy "có
    thay đổi"; nhân viên đối chiếu trên bảng điều khiển VTP ("Tìm đơn", "Đồng bộ hành trình").
- Lệnh nhãn của Task 107 có thể gửi tới 200 mã một tài khoản; VTP nhận tối đa 100 mã một mã in → adapter trả `rejected`
  `vtp_label_count` khi quá 100. Cần thêm giới hạn lô theo hãng (ví dụ `Capabilities` hay hằng của adapter) ở lõi.
- `carriertest/testdata/vtp/statuses.json` vẫn `{}`: contract `Cancel` bỏ qua ca "kiện đã lấy" (cần mã ra
  `picked_up`); khi có mã lỗi "đã lấy" thật (câu 8) thì thêm `"200": "picked_up"` và trả `CancelRejectedPickedUp`.
- Bảng kê `vtp_v1`: chờ tệp mẫu của VTP (chủ shop gửi).
- Gửi VTP câu 8–11 mới ở `docs/carriers/vtp.md`; Task 49 xác minh: link in trả PDF, khổ, token webhook, mã lỗi hủy.

## Reviewer

Kết luận: ĐẠT (lần 1, 21/11/2026). Không có phát hiện phải sửa. Ghi chú không bắt buộc của reviewer:
- Câu "gói phụ thuộc ngược" của mục Ảnh hưởng thiếu `internal/adapters/carrier` (`registry.go`, `vtp.Key`) và
  `tests/arch` — đã sửa câu trong tệp task (không đổi mã).
- Khi nối VTP vào `carrier.All`, mỗi webhook sinh một lệnh tra `rejected`/`not_supported`; Lookup của lệnh hủy
  `in_doubt` trả `unknown` — đã ghi ở Q1 và điểm chờ chủ shop, cần chốt trước Task 49.
- `classifyHTTP` (Task 125) xếp 408 vào `rejected`: `Redeliver` gặp 408 sẽ ra `UpdateRejected` thay vì "không rõ";
  hiếm, xem lại khi nối thật (Task 49).
