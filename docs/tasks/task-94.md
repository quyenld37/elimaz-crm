# Task 94 — M3-03: Mở rộng CarrierAdapter của M1, dời hãng giả, bộ contract test

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 3 = M3-03). Loại việc (quy trình mục 3): A. Hai commit: cấu trúc (dời hãng giả, tách `tools/fakecarrier/server`, không đổi hành vi) rồi hành vi (mở rộng interface, khung bốn hãng, registry, contract test).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences / grep):
  - Gói `internal/adapters/fakecarrier` dời thành `internal/adapters/carrier/fake` (tên gói `fake`): `NewClient`, `NewSource`, `Sign`, `Key`, `CarrierKey`, `Packet`, `Event`, `SignatureHeader` đổi đường nhập ở 6 chỗ: `cmd/crm/main.go` (5 tham chiếu), `cmd/crm/seed.go` (1), `tools/fakecarrier/admin.go` (3), `tools/fakecarrier/main_test.go` (dời sang `tools/fakecarrier/server/server_test.go`), `tests/e2e/stack_test.go` (4), `tests/arch/boundaries_test.go` (1 dòng ca luật).
  - `fulfillment.CarrierAdapter` thêm 12 phương thức: phải hiện thực ở mọi bản cài: `carrier/fake.Client`, gói trong bộ nhớ `fulfillment/internal/carriers/fake.Carrier` (đồ test M1), và bốn khung mới. Không có bản cài nào khác (`grep "func (.*) MapStatus("` → 2 chỗ). Thực tế: phần "không hỗ trợ" là kiểu nhúng mới `fulfillment.Unsupported` (Q-T94-9); `carriers/fake.Carrier` nhúng nó, khai `Create`, `LookupByRef`, thêm `MapCanonical`, `MapStatus` đi qua `StatusFromCanonical` (cùng kết quả M1 cho 3 mã thô).
  - `create_shipment.go:192` đổi phép chuyển kiểu `createResult(res)` thành dựng từng trường (hai struct không còn cùng trường vì `CreateResult` thêm `FeeKnown`); JSON kết quả lệnh giữ nguyên `{trackingCode, fee}`.
  - Kiểu mới của `fulfillment`: `types.go` (Capabilities, Address, LocationMap, Parcel, Options, QuoteInput/Result, CarrierEvent, Tracking, CancelResult, UpdateResult, Label, Location, Health, WebhookSignal, ErrorClass, CarrierError, ErrNotSupported, Header, StatementFormat, StatementField, StatementFormatProvider), `canonical.go`, `credentials.go` (Credentials, InputHash), `registry.go` (Registry, NewRegistry, ErrDuplicateCarrier), `unsupported.go`; `Account.LogValue` (adapter.go).
  - `cmd/crm/main.go`: bỏ hàm `fakeCarrierOn` (3 chỗ gọi) thay bằng `carrier.FakeOn` (một nguồn điều kiện cho cả `carrier.All`); `CommandOptions.Adapters` lấy từ `carrier.All(cfg)` (vẫn chỉ khi hãng giả bật, như M1).
  - `fake.Event` thêm trường `Code` (`json:"code,omitempty"`), máy chủ gửi kèm; `fake.Source.Split` của M1 bỏ qua trường này (không đổi tín hiệu).
  - Adapter `fake`: `NewClientWithKey`; lỗi gọi hãng thành `*fulfillment.CarrierError`. `CreateShipment` bọc `ErrNotSent`/`ErrRejected` như M1 (`errors.Is` của lệnh tạo vẫn đúng), riêng 429 nay bọc `ErrNotSent` thay `ErrRejected` (Q-T94-5). `LookupByCode`, `GetStatus` (lệnh `refresh_status` đọc `ErrRejected` = dừng hẳn) giữ đúng nghĩa M1: chỉ `GetStatus` 404 bọc `ErrRejected`, mọi mã khác bọc `errStatus` (đọc thử lại) dù mang lớp `ClassAuth`, `ClassRateLimited`… (Q-T94-16; reviewer vòng 1 bắt).
  - Hằng trạng thái thô mới của `fake`: `RawInTransit`, `RawOutForDelivery`, `RawDeliveryFailed`, `RawReturning`, `RawReturned`, `RawCancelled`, `RawLost` (chỉ thêm).
  - `fulfillment.Account`, `CreateRequest`, `CreateResult`, `LookupResult` chỉ thêm trường. `Account` có trường map và interface nên không còn so được bằng `==`: grep không có chỗ nào so `Account`. `CreateResult`, `LookupResult` vẫn so được; test M1 `fakecarrier_test.go:52` so `CreateResult` nên đổi giá trị mong đợi thêm `FeeKnown: true` (siết, không nới).
  - Chương trình `tools/fakecarrier`: `main.go` còn đọc cấu hình và chạy; máy chủ (route, kho trạng thái, admin) dời vào gói `tools/fakecarrier/server`.
- Gói phụ thuộc ngược (go list): `fulfillment` được nhập bởi `cmd/crm`, `internal/adapters/fakecarrier`, `internal/httpapi`, `internal/modules/cash`, `fulfillment/internal/carriers/fake`, `internal/testkit/shiptest`, `tools/fakecarrier`; thêm `tests/e2e`, `tests/arch` qua test. `config` được nhập bởi gần như mọi gói (chỉ thêm trường, không đổi chữ ký).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có (task không chạm database).
- Cấu hình: `config.go` thêm `CarrierWebhookSecrets map[string]logging.Secret` đọc `CARRIER_<KEY>_WEBHOOK_SECRET` tùy chọn (chỉ thêm, không đổi biến cũ).
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/... ./internal/adapters/... ./tools/fakecarrier/... ./internal/platform/config/... ./tests/arch/... ./cmd/crm/...` khi viết; `scripts/agent/ci-task.sh` một lần trước review (gói `fulfillment`, `fulfillment/status.go` là vùng rủi ro cao → cả bộ của vùng). `tests/e2e` (ingest, một đơn đi trọn) chạy qua ci-task vì `cmd/crm` đổi.

## Tái dùng

- Đã kiểm trong `docs/danh-muc-dung-chung.md` và tìm theo tên, theo việc: `logging.Phone`, `logging.Secret` (che khi log), `fieldcrypt` (không cần ở đây: task chỉ khai kiểu `Credentials`, mã hóa ở Task 9 của M3), HMAC chữ ký của nguồn `fakecarrier` M1 (`Sign`, `VerifySignature`).
- Dùng lại: interface và hai hãng giả của M1 Task 31 (mở rộng, không tạo hãng giả thứ hai: A10, C1); `Sign` của M1 cho `VerifyWebhook` của adapter `fake`; `logging.Phone` cho `Address.Phone`.
- Viết mới vì: `Credentials` là map nhiều khóa (token, mã khách hàng…) nên không dùng một `logging.Secret` đơn; `InputHash` không có hàm băm chuẩn hóa địa chỉ nào sẵn; `carriertest` là bộ contract mới theo kế hoạch.
- Không để trùng: phần "mọi hàm trả `ErrNotSupported`" của bốn khung và hãng giả trong bộ nhớ dùng chung kiểu nhúng `fulfillment.Unsupported` (Q-T94-9); mỗi khung chỉ còn `Key`, `New`. Khi Task 35–39 làm hãng thật, gói hãng cài đè phương thức mình làm được (hoặc bỏ nhúng).
- `VerifyWebhook` của `fake` dùng lại `NewSource(secret).VerifySignature` của M1 (không viết HMAC thứ hai); `MapStatus` của `fake` dùng `StatusFromCanonical` chung.

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
| Q-T94-9 | Thêm kiểu nhúng `fulfillment.Unsupported` (mọi phương thức trả `ErrNotSupported`, Capabilities rỗng, `MapCanonical` → unknown, `VerifyWebhook` false) | Năm bản cài (bốn khung + hãng giả trong bộ nhớ) sẽ chép y nguyên 16 phương thức; jscpd chặn đoạn trùng mới; một chỗ khai "không hỗ trợ" | Chép 16 phương thức vào từng gói |
| Q-T94-10 | `StatusFromCanonical`: picked_up, in_transit, out_for_delivery → `shipping`; awaiting_pickup, delivered, cancelled → cùng tên; delivery_failed, partially_delivered, returning, returned_to_shop, lost, damaged, unknown → `("", false)` | M1 chỉ có 5 trạng thái; Task 7 (M3-07) thêm delivery_failed, returning… thì mở rộng hàm này. Trả false = M1 không đổi gì (an toàn) | Đổi delivery_failed → shipping (sai nghĩa khi Task 7 thêm trạng thái riêng) |
| Q-T94-11 | `Credentials` che khi `slog`, `%v`, `%+v`, `%#v`, `%s` nhưng **không** che `MarshalJSON`; `Account` cài `LogValue` chỉ ghi id, khóa hãng, mã tài khoản, môi trường | slog JSON in struct lồng bằng encoding/json (không gọi String) nên `Account` phải tự che, kèm địa chỉ lấy hàng có SĐT; Task 9 cần mã hóa `Credentials` dạng JSON trước khi `fieldcrypt` | Che cả `MarshalJSON` (Task 9 phải tự dựng JSON) |
| Q-T94-12 | `Registry` đặt ở `fulfillment/registry.go`; `carrier.All(cfg)` và `carrier.FakeOn(cfg)` ở `internal/adapters/carrier/registry.go`; khóa giao tay `"self"` | Module tra adapter theo khóa mà không nhập gói adapter; điều kiện bật hãng giả một nguồn (bỏ `fakeCarrierOn` của `cmd/crm`) | Registry ở gói adapter (module phải nhập adapter: ngược chiều phụ thuộc) |
| Q-T94-13 | `InputHash` gồm dòng địa chỉ, tỉnh, phường (chuẩn hóa), cả 6 số của kiện, COD, dịch vụ, `Inspection`, `Payer`, `FeeOnRefuse`, `Pickup`; bỏ tên, SĐT, ghi chú | Phí không phụ thuộc tên, SĐT, ghi chú: sửa chúng không được làm mất báo phí; trường nào đổi phí thì vào băm. Chuỗi `%q` có tiền tố `v1` để đổi công thức sau này | Băm cả địa chỉ đầy đủ |
| Q-T94-14 | Lỗi khi không gửi được của adapter `fake`: không dựng được yêu cầu, lỗi dial → `ClassNotSentRetry` (bọc `ErrNotSent`); quá giờ, đứt sau khi gửi, thân 200 hỏng → `ClassUnknownOutcome` | Đúng nghĩa M1 (dial → `ErrNotSent`, còn lại không rõ), thêm lớp để lệnh phân biệt | — |
| Q-T94-15 | `Harness.SetMode` với chế độ sai chỉ ghi log (không panic); `POST /admin/mode` sai vẫn 422 | Interface của kế hoạch không trả lỗi; contract chỉ dùng hằng đúng | Đổi interface trả lỗi |
| Q-T94-16 | Hai hàm đọc của M1 (`LookupByCode`, `GetStatus`) phân lớp lỗi nhưng chỉ bọc `ErrRejected` khi `GetStatus` 404 | `refresh_status` coi `ErrRejected` là dừng hẳn; khóa bị thu hồi một lúc không được làm vận đơn ngừng cập nhật vĩnh viễn (giữ hành vi M1) | Cho 401, 4xx bọc `ErrRejected` như tạo kiện (đổi hành vi M1) |
| Q-T94-8 | Webhook hãng giả gửi tới `FAKECARRIER_WEBHOOK_URL + "/" + routeKey` chỉ khi kiện có `routeKey`; kiện không có (M1) gửi tới URL gốc như cũ | Giữ đường `/hooks/fakecarrier` của M1 và e2e một đơn đi trọn | Luôn thêm `/routeKey` (vỡ M1) |

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit cấu trúc: dời `adapters/fakecarrier` → `adapters/carrier/fake`, tách `tools/fakecarrier/server`, không đổi hành vi; test M1 xanh không sửa ca | `TestClientCreateShipment`, `TestClientLookupAndStatus`, `TestFailureModes`, `TestSourceSignatureAndSplit`, `TestIngestPipeline` (fake_test.go); mọi test của `server_test.go`, `TestLoadConfig`; test M1 của `fulfillment` | `go test -race ./internal/adapters/carrier/... ./tools/fakecarrier/... ./internal/modules/fulfillment/...`; `diff` từng tệp dời với bản ở HEAD (chỉ khác tên gói, tên kiểu xuất) | `ok` cả 4 gói; diff chỉ đổi tên gói/đường nhập |
| Commit cấu trúc: không gói nào khác vỡ (cmd/crm, e2e một đơn đi trọn, kiến trúc) | toàn bộ test (ci-task chạy toàn bộ vì gộp main có migration) | ci-task (sau gộp main b497aa5) | mọi gói `ok` gồm `tests/e2e 47.1s`; đỏ duy nhất: wrapcheck `tools/fakecarrier/main.go:99` (đã bọc lỗi, `golangci-lint run ./tools/... ./internal/adapters/... ./cmd/...` → `0 issues`) và test-guard (đã ghi mục Test bị xóa) |
| `Credentials{"token":"abc123secret"}` qua slog JSON, `%v`, `%+v`, `%#v`, `%s` (cả lồng trong `Account`) không lộ; `Account` ghi log không lộ SĐT, địa chỉ lấy hàng | `TestCredentialsRedacted` (fulfillment/credentials_test.go) | `go test -race ./internal/modules/fulfillment/` | `ok` (lần đầu đỏ: slog JSON in `Creds` của `Account` → thêm `Account.LogValue`) |
| `InputHash` đổi khi COD +1 đồng, cân nặng +1 g, dịch vụ, tùy chọn, phường; không đổi khi địa chỉ khác khoảng trắng, chữ hoa, khi tên, SĐT, ghi chú khác | `TestInputHash` | như trên | `ok` |
| `StatusFromCanonical` đúng bảng Q-T94-10; `CarrierError` `errors.Is` tới `ErrNotSent`, `errors.As` lấy lớp, `RetryAfter` | `TestStatusFromCanonical`, `TestCarrierErrorUnwrap` | như trên | `ok` |
| `NewRegistry` trùng khóa → `ErrDuplicateCarrier` (cả `fake.NewClient` × 2), khóa rỗng lỗi, `fake` + `fake_b` dựng được; `Get`, `Keys` sắp xếp, `StatementFormats` gom từ provider | `TestRegistry` (fulfillment), `TestRegistryDuplicateFake` (internal/adapters/carrier/registry_test.go) | `go test -race ./internal/modules/fulfillment/ ./internal/adapters/carrier/` | `ok` |
| `carrier.All`: self, vtp, best, spx luôn có; fake chỉ khi khác production và có `FAKECARRIER_URL` | `TestAll` | `go test -race ./internal/adapters/carrier/` | `ok` |
| `carriertest.Run` trên `fake` (máy chủ `tools/fakecarrier/server` trong httptest) qua mọi ca: Create (ok, timeout_after_create rồi ok cùng mã → `LookupByCode` thấy, `Has`; fail_before_create, reject, auth_revoked → `ClassAuth`, rate_limited → `RetryAfter`>0), LookupByRef, Quote (+429, 401), Inspection, DeclaredValue, FeeOnRefuse, Cancel (+idempotent, đã lấy → rejected_picked_up, 429), Track (Push → sự kiện in_transit, 401), Webhook (`webhook.json`: chữ ký đúng/sai/thân sửa/thiếu, Split đúng tín hiệu), Labels (+batch), UpdateAddress, Redeliver, Locations, Statement; quét SĐT `0[0-9]{9}` mọi kết quả và lỗi | `TestContractFake` | `go test -race -count=1 -v -run TestContractFake ./internal/adapters/carrier/carriertest/` | 23 subtest `--- PASS` |
| `carriertest.Run` trên khung `self`, `vtp`, `best`, `spx` (mọi khả năng false) → đạt, chỉ kiểm `ErrNotSupported` | `TestContractFrames` | `go test -race ./internal/adapters/carrier/carriertest/` | `ok` |
| Contract cài lỗi: adapter khai `Cancel: true` nhưng `Cancel` trả `ErrNotSupported` → đỏ đúng ca Cancel, nêu "khả năng Cancel khai true nhưng Cancel trả ErrNotSupported", ca khác không đỏ | `TestContractCatchesLie` (chạy contract ở tiến trình con) | như trên | `--- PASS: TestContractCatchesLie` |
| `/admin/mode {mode: timeout_after_create}` không `op` vẫn áp cho tạo kiện; `{op: cancel, mode: rate_limited}` → hủy 429 `Retry-After: 7`, client ra `ClassRateLimited` 7 s, tạo kiện vẫn ok; op lạ, chế độ không hợp op → 422 | `TestModeByOp` (tools/fakecarrier/server/harness_test.go) | `go test -race ./tools/fakecarrier/...` | `ok` |
| Webhook tới `FAKECARRIER_WEBHOOK_URL/<routeKey>` khi kiện có routeKey, URL gốc khi không (M1), chữ ký đúng; Harness `Calls`, `Has`, `Push`, `Reset` | `TestWebhookRouteKey`, `TestHarness` | như trên | `ok` |
| Test M1 xanh: client, nguồn webhook, máy chủ, tạo/đọc vận đơn của fulfillment, e2e một đơn đi trọn | `fake_test.go` (siết `FeeKnown`), `server_test.go`, test `fulfillment`, `tests/e2e` | ci-task (toàn bộ test, 183 s) | mọi gói `ok`; đỏ: jscpd 2 đoạn trùng (đã gộp: `pathParcel`, `MapStatus` hai dòng → `Found 0 clones`), catalog-check (danh mục sinh lại chưa stage) |
| Hai hàm đọc giữ nghĩa M1: 401, 403, 422, 429, 500 của `LookupByCode`, `GetStatus` không bọc `ErrRejected` nhưng mang đúng lớp; `GetStatus` 404 → `ErrRejected` | `TestReadErrorsKeepM1Meaning` (fake_test.go) | `go test -race ./internal/adapters/carrier/fake/` | lần đầu đỏ (`GetStatus 422 … bọc ErrRejected`), sau sửa `ok` |
| Tạo lại cùng mã: đúng 2 lời gọi tạo (`Calls(create)`), lần ba cùng mã trả cùng kiện (không có kiện thứ hai) | `TestContractFake/Create/timeout_after_create_rồi_ok_cùng_mã` | `go test -race ./internal/adapters/carrier/carriertest/` | `ok` |
| `CARRIER_<KEY>_WEBHOOK_SECRET` tùy chọn cho ingest, ≥ 32 ký tự, không lộ qua `LogValue`, worker không đọc | `TestLoadCarrierWebhookSecrets` | `go test -race ./internal/platform/config/` | `ok` |
| Cổng kiểm sau sửa vòng 1 | toàn bộ test + build, vet, lint, catalog, jscpd, gitleaks, oasdiff, test-guard | ci-task | `CI-TASK XANH (so với điểm tách nhánh b497aa5)`, test toàn bộ 191 s |
| Lint | — | `golangci-lint run ./internal/adapters/... ./internal/modules/fulfillment/... ./tools/fakecarrier/... ./cmd/... ./internal/platform/config/...` | `0 issues.` |

## Test bị xóa hoặc tắt

- `internal/adapters/fakecarrier/fakecarrier_test.go`: không xóa ca nào, tệp dời nguyên văn sang `internal/adapters/carrier/fake/fake_test.go` (chỉ đổi tên gói `fakecarrier` → `fake`), cùng gói nó kiểm (commit cấu trúc). Commit hành vi siết một câu kiểm của `TestClientCreateShipment`: giá trị mong đợi thêm `FeeKnown: true` (không nới).
- `tools/fakecarrier/main_test.go`: không xóa ca nào; mọi ca trừ `TestLoadConfig` dời sang `tools/fakecarrier/server/server_test.go` (Q-T94-4).

## Khác kế hoạch

- Tên: `fake.NewClient`/`NewClientWithKey` thay `fake.New`/`NewWithKey` (Q-T94-3); hằng lớp lỗi `Class*` thay `Err*` (Q-T94-2); `fulfillment.Header` thay `http.Header` (Q-T94-1).
- Thêm tệp ngoài danh sách: `fulfillment/{registry.go,unsupported.go}`, `internal/adapters/carrier/fake/ops.go`, `tools/fakecarrier/server/ops.go`, test `tools/fakecarrier/server/harness_test.go`, `internal/adapters/carrier/registry_test.go`, `internal/platform/config/carrier_secrets_test.go`; testdata `carriertest/testdata/<hãng>/` (Q-T94-7).
- Lệnh tạo vận đơn của M1 (`CreateCommand.request`) chưa điền `From`, `To`, `Parcel`: dựng yêu cầu đủ trường là việc của task tạo vận đơn M3 (Task 13); task này chỉ thêm trường.

## Việc phát hiện thêm

- `CommandOptions.Adapters` của worker nay gồm cả khung `self`, `vtp`, `best`, `spx` (`carrier.All`). Khung trả `ErrNotSupported`, mà `CreateCommand.Execute`/`RefreshCommand.read` của M1 chưa có nhánh cho lỗi này: lệnh tạo sẽ ra `in_doubt` dù chắc chắn chưa gửi. Hiện chưa xảy ra (mọi tài khoản hãng là `fake`, lệnh chỉ nối khi hãng giả bật). Task tạo tài khoản và tạo vận đơn của M3 (Task 9, 12, 13) cần đổi `ErrNotSupported` thành `rejected` (hoặc chặn tài khoản `mode = api` của hãng chưa có hiện thực).

## Reviewer

### Commit 1 (cấu trúc)

Kết luận: ĐẠT (vòng 1). Không có phát hiện phải sửa. Ảnh hưởng khớp (5 tham chiếu cmd/crm/main.go, 1 seed.go, 3 admin.go, 4 stack_test.go, 1 dòng arch); grep không còn nơi gọi tên cũ trong mã; test dời giống hệt thân cũ. Ghi chú: dòng "dời nguyên văn" của fake_test.go sẽ chỉnh khi commit 2 đổi `CreateResult` mong đợi.

### Commit 2 (hành vi)

- Vòng 1: CẦN SỬA — `LookupByCode`, `GetStatus` của adapter `fake` đổi nghĩa M1 (401, 403, 4xx bọc `ErrRejected` → `refresh_status` dừng hẳn thay vì thử lại), mục Ảnh hưởng nói ngược lại. Đã sửa: `failRead` (chỉ `GetStatus` 404 bọc `ErrRejected`), test `TestReadErrorsKeepM1Meaning`, Q-T94-16; ghi "Việc phát hiện thêm" về khung trả `ErrNotSupported` trong `CommandOptions.Adapters`; contract tạo lại cùng mã kiểm thêm `Calls(create) == 2` và lần ba cùng kiện. ci-task chạy lại: XANH.
- Vòng 2: ĐẠT (hai reviewer độc lập). Không có phát hiện phải sửa; Ảnh hưởng khớp; bảng bằng chứng trỏ đúng test có kiểm điều tiêu chí nói.
