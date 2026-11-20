# Task 94 — M3-03: Mở rộng CarrierAdapter của M1, dời hãng giả, bộ contract test

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 3 = M3-03). Loại việc (quy trình mục 3): A. Hai commit: cấu trúc (dời hãng giả, tách `tools/fakecarrier/server`, không đổi hành vi) rồi hành vi (mở rộng interface, khung bốn hãng, registry, contract test).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences / grep):
  - Gói `internal/adapters/fakecarrier` dời thành `internal/adapters/carrier/fake` (tên gói `fake`): `NewClient`, `NewSource`, `Sign`, `Key`, `CarrierKey`, `Packet`, `Event`, `SignatureHeader` đổi đường nhập ở 6 chỗ: `cmd/crm/main.go` (5 tham chiếu), `cmd/crm/seed.go` (1), `tools/fakecarrier/admin.go` (3), `tools/fakecarrier/main_test.go` (dời sang `tools/fakecarrier/server/server_test.go`), `tests/e2e/stack_test.go` (4), `tests/arch/boundaries_test.go` (1 dòng ca luật).
  - `fulfillment.CarrierAdapter` thêm 12 phương thức: phải hiện thực ở mọi bản cài: `carrier/fake.Client`, gói trong bộ nhớ `fulfillment/internal/carriers/fake.Carrier` (đồ test M1), và bốn khung mới. Không có bản cài nào khác (`grep "func (.*) MapStatus("` → 2 chỗ).
  - `fulfillment.Account`, `CreateRequest`, `CreateResult`, `LookupResult` chỉ thêm trường. `Account` có trường map và interface nên không còn so được bằng `==`: grep không có chỗ nào so `Account`. `CreateResult`, `LookupResult` vẫn so được; test M1 `fakecarrier_test.go:52` so `CreateResult` nên đổi giá trị mong đợi thêm `FeeKnown: true` (siết, không nới).
  - Chương trình `tools/fakecarrier`: `main.go` còn đọc cấu hình và chạy; máy chủ (route, kho trạng thái, admin) dời vào gói `tools/fakecarrier/server`.
- Gói phụ thuộc ngược (go list): `fulfillment` được nhập bởi `cmd/crm`, `internal/adapters/fakecarrier`, `internal/httpapi`, `internal/modules/cash`, `fulfillment/internal/carriers/fake`, `internal/testkit/shiptest`, `tools/fakecarrier`; thêm `tests/e2e`, `tests/arch` qua test.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có (task không chạm database).
- Cấu hình: `config.go` thêm `CarrierWebhookSecrets map[string]logging.Secret` đọc `CARRIER_<KEY>_WEBHOOK_SECRET` tùy chọn (chỉ thêm, không đổi biến cũ).
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/... ./internal/adapters/... ./tools/fakecarrier/... ./internal/platform/config/... ./tests/arch/... ./cmd/crm/...` khi viết; `scripts/agent/ci-task.sh` một lần trước review (gói `fulfillment`, `fulfillment/status.go` là vùng rủi ro cao → cả bộ của vùng). `tests/e2e` (ingest, một đơn đi trọn) chạy qua ci-task vì `cmd/crm` đổi.

## Tái dùng

- Đã kiểm trong `docs/danh-muc-dung-chung.md` và tìm theo tên, theo việc: `logging.Phone`, `logging.Secret` (che khi log), `fieldcrypt` (không cần ở đây: task chỉ khai kiểu `Credentials`, mã hóa ở Task 9 của M3), HMAC chữ ký của nguồn `fakecarrier` M1 (`Sign`, `VerifySignature`).
- Dùng lại: interface và hai hãng giả của M1 Task 31 (mở rộng, không tạo hãng giả thứ hai: A10, C1); `Sign` của M1 cho `VerifyWebhook` của adapter `fake`; `logging.Phone` cho `Address.Phone`.
- Viết mới vì: `Credentials` là map nhiều khóa (token, mã khách hàng…) nên không dùng một `logging.Secret` đơn; `InputHash` không có hàm băm chuẩn hóa địa chỉ nào sẵn; `carriertest` là bộ contract mới theo kế hoạch.
- Trùng có chủ ý: bốn khung `manual`, `vtp`, `best`, `spx` giống nhau từng dòng (mọi hàm trả `ErrNotSupported`). Giữ riêng vì mỗi gói sẽ được Task 35–39 thay bằng hiện thực riêng của hãng (khác lý do thay đổi); gộp thành một kiểu chung rồi tách lại là việc thừa.

## Soát thiết kế

- 5.1 Đúng chủ dữ liệu: kiểu và interface thuộc `fulfillment` (04 mục 3); adapter ở `internal/adapters/carrier/<hãng>`.
- 5.2, 5.3, 5.4, 5.5, 5.7, 5.9, 5.10: không áp dụng: không chạm database, không đường ghi.
- 5.6 Gọi ra ngoài: phương thức mới chỉ được lệnh `commands` gọi ở các task sau; `CarrierError.Class` phân biệt chắc chắn chưa gửi (`ClassNotSentRetry`), không rõ (`ClassUnknownOutcome`), để lệnh ra `in_doubt` đúng.
- 5.8 Nghiệp vụ không biết HTTP: `fulfillment` không nhập `net/http` (luật (b)); chữ ký webhook dùng `fulfillment.Header` (= `map[string][]string`, `http.Header` gán vào được) thay `http.Header`. Xem Quyết định thay chủ shop.
- 5.11 Không thêm thư viện.
- 5.12 Theo mẫu bộ nối `internal/adapters/<tên>` (`cfaccess`).
- 6.1–6.5, 6.8–6.11, 6.13: không áp dụng: không database, không tiền ghi sổ, không quyền.
- 6.6, 6.7 Lỗi, 429: `CarrierError{Class: ClassRateLimited, RetryAfter}` mang thời gian chờ hãng yêu cầu (đọc `Retry-After`), có ca contract.
- 6.12 Dữ liệu cá nhân: `Credentials` che khi log, `%v`, `%+v`; `CarrierEvent`, `Tracking` không có trường tên, SĐT, địa chỉ; contract quét SĐT trong kết quả và lỗi.

## Quyết định thay chủ shop

| # | Chọn | Vì sao | Phương án khác |
|---|---|---|---|
| Q-T94-1 | Chữ ký `VerifyWebhook(raw []byte, h fulfillment.Header, secret string)` với `type Header = map[string][]string` | Luật kiến trúc (b) cấm module nhập `net/http`, kể cả để lấy kiểu `http.Header`; `http.Header` gán thẳng vào `map[string][]string` nên bên gọi truyền `r.Header` như cũ | Thêm ngoại lệ vào `tests/arch` (nới test canh) |
| Q-T94-2 | Hằng `ErrorClass` đặt tên `ClassNotSentRetry`, `ClassRateLimited`, `ClassAuth`, `ClassRejected`, `ClassPermanent`, `ClassUnknownOutcome` | Tên `ErrRejected` của kế hoạch trùng sentinel `fulfillment.ErrRejected` của M1 (không biên dịch được); đặt cùng một tiền tố cho cả sáu để task sau đọc `fulfillment.ClassAuth` thay `ErrAuth` | Chỉ đổi tên một hằng bị trùng |
| Q-T94-3 | Giữ tên `fake.NewClient(base, secret, hc)` của M1, thêm `fake.NewClientWithKey(key, base, secret, hc)` | Giữ test M1 không đổi nội dung; kế hoạch viết tắt `fake.New(...)`, `NewWithKey(k)` | Đổi tên `NewClient` → `New` (sửa test M1) |
| Q-T94-4 | Test của `tools/fakecarrier/main_test.go` (trừ `TestLoadConfig`) dời sang `tools/fakecarrier/server/server_test.go`, chỉ đổi tên gói và tên hàm nội bộ bị dời (`openStore` → `OpenStore`…), không đổi ca kiểm | Máy chủ dời vào gói `server`; test đi theo mã nó kiểm | Để hàm bọc trong `main` chỉ để test cũ gọi |
| Q-T94-5 | Lỗi tạo kiện: 401 → `CarrierError{ClassAuth, Err: ErrRejected}`, 429 → `CarrierError{ClassRateLimited, RetryAfter, Err: ErrNotSent}`, 503 → `{ClassNotSentRetry, ErrNotSent}`, 4xx khác → `{ClassRejected, ErrRejected}`, 5xx khác, quá giờ → `{ClassUnknownOutcome}` | Hãng trả 429 nghĩa là chưa nhận yêu cầu, gửi lại an toàn; M1 xếp 429 vào 4xx = từ chối, sẽ bắt người bấm lại vô ích. 401 cần người sửa khóa nên không tự gửi lại | Giữ 429 = `ErrRejected` như M1 |
| Q-T94-6 | Khả năng không có phương thức riêng (`Inspection`, `DeclaredValue`, `FeeOnRefuse`) kiểm qua `Quote`: khai `false` mà nhận tùy chọn đó → `ErrNotSupported`; `Statement` khai `true` ⇔ adapter cài `StatementFormatProvider` và trả ít nhất một định dạng | Kế hoạch đòi "khai false phải trả ErrNotSupported" cho mọi khả năng nhưng ba khả năng này không có hàm | Bỏ qua ba khả năng trong contract |
| Q-T94-7 | `statuses.json`, `webhook.json` của từng hãng đặt ở `internal/adapters/carrier/carriertest/testdata/<hãng>/`; khung `vtp`, `best`, `spx`, `manual` có `statuses.json` rỗng (Task 35–39 điền) và không có `webhook.json` (`Webhook: false`) | Contract chạy trong gói `carriertest` nên đọc testdata của gói đó | Mỗi adapter giữ testdata riêng |
| Q-T94-8 | Webhook hãng giả gửi tới `FAKECARRIER_WEBHOOK_URL + "/" + routeKey` chỉ khi kiện có `routeKey`; kiện không có (M1) gửi tới URL gốc như cũ | Giữ đường `/hooks/fakecarrier` của M1 và e2e một đơn đi trọn | Luôn thêm `/routeKey` (vỡ M1) |

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit cấu trúc: dời `adapters/fakecarrier` → `adapters/carrier/fake`, tách `tools/fakecarrier/server`, không đổi hành vi; test M1 xanh không sửa ca | `TestClientCreateShipment`, `TestClientLookupAndStatus`, `TestFailureModes`, `TestSourceSignatureAndSplit`, `TestIngestPipeline` (fake_test.go); mọi test của `server_test.go`, `TestLoadConfig`; test M1 của `fulfillment` | `go test -race ./internal/adapters/carrier/... ./tools/fakecarrier/... ./internal/modules/fulfillment/...`; `diff` từng tệp dời với bản ở HEAD (chỉ khác tên gói, tên kiểu xuất) | `ok` cả 4 gói; diff chỉ đổi tên gói/đường nhập |
| Commit cấu trúc: không gói nào khác vỡ (cmd/crm, e2e một đơn đi trọn, kiến trúc) | toàn bộ test (ci-task chạy toàn bộ vì gộp main có migration) | ci-task (sau gộp main b497aa5) | mọi gói `ok` gồm `tests/e2e 47.1s`; đỏ duy nhất: wrapcheck `tools/fakecarrier/main.go:99` (đã bọc lỗi, `golangci-lint run ./tools/... ./internal/adapters/... ./cmd/...` → `0 issues`) và test-guard (đã ghi mục Test bị xóa) |

## Test bị xóa hoặc tắt

- `internal/adapters/fakecarrier/fakecarrier_test.go`: không xóa ca nào, tệp dời nguyên văn sang `internal/adapters/carrier/fake/fake_test.go` (chỉ đổi tên gói `fakecarrier` → `fake`), cùng gói nó kiểm (commit cấu trúc).
- `tools/fakecarrier/main_test.go`: không xóa ca nào; mọi ca trừ `TestLoadConfig` dời sang `tools/fakecarrier/server/server_test.go` (Q-T94-4).

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

### Commit 1 (cấu trúc)

Kết luận: ĐẠT (vòng 1). Không có phát hiện phải sửa. Ảnh hưởng khớp (5 tham chiếu cmd/crm/main.go, 1 seed.go, 3 admin.go, 4 stack_test.go, 1 dòng arch); grep không còn nơi gọi tên cũ trong mã; test dời giống hệt thân cũ. Ghi chú: dòng "dời nguyên văn" của fake_test.go sẽ chỉnh khi commit 2 đổi `CreateResult` mong đợi.
