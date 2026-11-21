# Task 125 — M3-34: Viettel Post — xác thực, địa giới, báo phí, tạo vận đơn, tra theo mã tham chiếu

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 125, Phần D "Luật chung cho Task 34–39").
Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `vtp.New`: chữ ký đổi `New(config.Config)` → `New(Config)` (Interface của kế hoạch). Chỗ dùng (grep `vtp\.`): 2 —
    `internal/adapters/carrier/registry.go:25` và `internal/adapters/carrier/carriertest/contract_test.go:51`
    (`TestContractFrames`). Cả hai sửa trong task (xem Quyết định Q1): registry dùng `manual.New(vtp.Key)` (chưa nối
    adapter thật), `TestContractFrames` bỏ `vtp` (vtp có contract test riêng trên `vtptest`).
  - `vtp.Adapter`: từ khung (`fulfillment.Unsupported`) thành hiện thực có `Capabilities`, `CheckConnection`, `Quote`,
    `CreateShipment`, `LookupByCode` (vẫn `ErrNotSupported`), `Locations`, `MapCanonical`, `MapStatus`. Mới: `vtp.Config`,
    các hằng mã trạng thái thô; gói mới `vtp/vtptest` (`Server`, `New`, `Options`).
  - `carriertest` (nội bộ, không đổi ký hiệu xuất): ca `Create/timeout_after_create rồi ok cùng mã` thêm nhánh cho
    adapter `LookupByRef = false` (Quyết định Q2).
- Gói phụ thuộc ngược (go list): `vtp` ← `internal/adapters/carrier` (← `cmd/crm`), `carriertest_test`. `carriertest` ←
  test của `fake`, `carriertest`, `vtp` (mới).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có (màn tài khoản hãng đọc `Capabilities` qua API; registry vẫn trả khung giao tay cho
  `vtp` nên màn không đổi).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `go test -race ./internal/adapters/carrier/...` (vtp, vtptest, carriertest, fake, registry),
  `go test ./tests/arch/...` (luật (j) nhập của adapter), `go build ./cmd/crm/...`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (urlerr, fieldcrypt,
  telemetry, logging); `internal/adapters/carrier/fake` (client HTTP của hãng giả: `fail`, `retryAfter`, `do`);
  `tools/fakecarrier/server` (Harness); `fulfillment.Unsupported`, `fulfillment.StatusFromCanonical`,
  `fulfillment.ErrAddressUnmapped`, `carriertest.Run` + hằng `Op*`, `Mode*`.
- Dùng lại / viết mới vì:
  - Dùng lại: `fulfillment.Unsupported` (nhúng cho các khả năng false), `StatusFromCanonical` (MapStatus),
    `ErrAddressUnmapped`, `CarrierError`/lớp lỗi, `carriertest.Run` và hằng op/mode cho `vtptest`.
  - Viết mới `vtp/client.go` (gọi HTTP, phong bì VTP, đăng nhập lại một lần): `fake.Client.do` là phần riêng của gói
    fake (không xuất) và đọc định dạng dây khác (không phong bì `status/error/message/data`, không token); luật dự án giữ
    mã của từng hãng trong gói của hãng. `retryAfter` (đọc `Retry-After` số giây) viết riêng thay vì xuất từ `fake`
    (gói của hãng giả, không phải gói chung; hai hãng đổi vì lý do khác nhau). jscpd lần chạy đầu báo 3 đoạn trùng
    (`retryAfter` với `fake/client.go`; hai vòng lặp của `locations.go`; `Has` của `vtptest` với
    `fulfillment/internal/carriers/fake`): sửa cả ba (viết lại `retryAfter`, gộp vòng lặp thành `complete`, `Has` qua
    `locked`), không thêm vào baseline.
  - `urlerr.Strip` không dùng được: luật kiến trúc (j) chỉ cho adapter nhập `logging, telemetry, config, webhooks`;
    `client.go` tự bỏ `*url.Error` bằng thư viện chuẩn (URL của VTP không chứa bí mật, chỉ `provinceId`).
  - `vtptest` viết mới: `tools/fakecarrier/server` nói định dạng của hãng giả, không phải định dạng dây của VTP
    (Global Constraints: máy chủ giả `<hãng>test` nói đúng định dạng của hãng).

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1–5.4, 5.7, 5.9, 5.10: không áp dụng — adapter không có bảng, không database, không transaction, không đường ghi API.
- 5.5, 5.6: adapter chỉ chạy trong `Execute` của lệnh (ngoài transaction); lỗi "không rõ" (quá giờ, 5xx sau khi gửi,
  206 đơn đã tồn tại) là `ClassUnknownOutcome` để lệnh thành `in_doubt`, không tự gửi lại. Đăng nhập lại một lần chỉ
  khi hãng báo lỗi token (chắc chắn chưa xử lý yêu cầu).
- 5.8: module không nhập adapter; adapter nhập `net/http` (được phép, luật (j)).
- 5.11: không thêm thư viện (chỉ thư viện chuẩn).
- 5.12: theo mẫu adapter `fake` (lớp lỗi, hạn giờ, không chép thân trả lời vào lỗi) và mẫu "Gọi dịch vụ ngoài".

Mục 6 (đúng khi chạy đồng thời):
- 6.1: bộ nhớ mã truy cập dùng chung giữa các worker trong tiến trình → `sync.Mutex`; test `-race` với nhiều lời gọi
  song song. Hai worker cùng thấy mã hết hạn có thể cùng đăng nhập: chấp nhận (đăng nhập không có tác dụng phụ).
- 6.2: tạo vận đơn gửi `CHECK_UNIQUE: true` với `ORDER_NUMBER` = mã CRM → gửi lại cùng mã không tạo đơn thứ hai (hãng trả
  206, adapter trả "không rõ").
- 6.3–6.5, 6.10, 6.11, 6.13: không áp dụng (không database, không tiền ghi sổ, không quyền). Tiền đọc từ hãng là
  `int64` đồng.
- 6.6, 6.8: lớp lỗi quyết định lệnh gửi lại/in_doubt (bảng mã lỗi trong `docs/carriers/vtp.md`).
- 6.7: 429 → `rate_limited` theo `Retry-After` (mặc định 30 s), bọc `ErrNotSent`.
- 6.9: hạn giờ mỗi lời gọi: báo phí 10 s, tạo vận đơn 20 s, địa giới/đăng nhập 15 s (dưới 30 s của lệnh).
- 6.12: thân yêu cầu có tên, SĐT, địa chỉ: không log, không chép vào lỗi; thông báo lỗi của hãng không chép vào lỗi
  (chỉ mã); test quét log và lỗi của cả bộ không có SĐT, địa chỉ mẫu.

## Quyết định thay chủ shop

- **Q1 — Chưa nối adapter thật vào registry.** Đổi `vtp.New` theo Interface của kế hoạch làm `registry.go` phải sửa;
  chọn `manual.New(vtp.Key)` (khóa `vtp`, mọi khả năng false, như khung cũ) thay vì `vtp.New(vtp.Config{})`, vì nối
  thật thì tài khoản `vtp` mode api sẽ gọi máy chủ thật của Viettel Post — kế hoạch chỉ cho gọi thật ở Task 49 khi chủ
  shop OK, và lời giao việc bảo ghi việc nối vào "Việc phát hiện thêm". Phương án khác: nối luôn (một dòng, đảo được).
  Sửa ngoài `vtp/**`: 1 dòng `registry.go`, 1 dòng `carriertest/contract_test.go` (hệ quả bắt buộc của đổi chữ ký).
- **Q2 — Bộ contract có lỗ khi `LookupByRef = false`.** Ca `timeout_after_create rồi ok cùng mã` của `carriertest`
  luôn đòi `LookupByCode` thấy kiện, trong khi ca `LookupByRef` đòi `ErrNotSupported` khi khả năng false — adapter
  không có tra theo mã (đúng kế hoạch đã dự tính cho VTP) không bao giờ qua được. Sửa `carriertest/contract.go`: khi
  `LookupByRef = false`, gửi lại cùng mã phải ra lỗi "không rõ" (không `ErrNotSent`, không `ErrRejected`), máy chủ vẫn
  chỉ có một kiện, đúng 2 lần gọi tạo; nhánh `LookupByRef = true` giữ nguyên. Phương án khác: khai `Create = false`
  (bỏ mục tiêu của task) hay bỏ `carriertest.Run` (trái kế hoạch). Cùng lý do, ca `Create/ok` luôn đòi `GetStatus`
  trả trạng thái; VTP không có API đọc trạng thái (chỉ webhook): khi `Track = false`, `GetStatus` phải trả
  `ErrNotSupported` (kiểm chặt như khả năng false khác, không nới); `Track = true` giữ nguyên.
- **Q3 — `LookupByCode` trả `ErrNotSupported`, không trả `Inconclusive`.** Kế hoạch ghi "Inconclusive = true và
  LookupByRef = false"; contract đòi khả năng false thì `ErrNotSupported`. Lõi xử lý hai cách như nhau ở đường tạo vận
  đơn (`ResolveInDoubt`: Lookup lỗi → giữ in_doubt chờ người); ở `refresh_status`, `ErrNotSupported` dừng lần tra
  (rejected `not_supported`) thay vì thử lại mãi (`Inconclusive` → not_sent). Chọn theo contract.
- **Q4 — Dịch vụ rỗng khi tạo vận đơn** (`ORDER_SERVICE` bắt buộc ở VTP; `carriertest` tạo kiện với `Service = ""`):
  gọi `getPriceAll` và chọn dịch vụ có `GIA_CUOC` thấp nhất (trùng giá: mã dịch vụ nhỏ hơn). Phí thật lấy từ trả lời
  `createOrder` (`FeeKnown`). Phương án khác: từ chối `service_required` trước khi gọi mạng.
- **Q5 — `Inspection`, `FeeOnRefuse` khai false** (mã dịch vụ cộng thêm theo từng tài khoản, chưa có mẫu thật);
  `DeclaredValue` true (`PRODUCT_PRICE`). Báo phí/tạo vận đơn có `Inspection` khác `""`/`none` hay `FeeOnRefuse > 0` →
  `ErrNotSupported`, không gọi mạng.
- **Q6 — Mã 104, 503, 202 để `unknown`** (104 mâu thuẫn theo I15, 503 nghĩa không rõ, 202 là sự kiện sửa phiếu); câu
  hỏi ghi ở `docs/carriers/vtp.md`.
- **Q7 — Nguồn tài liệu:** trang partner2.viettelpost.vn là ứng dụng JS; nội dung lấy từ các tệp JS và `vi.json` công
  khai của chính trang (đọc 21/11/2026), không đăng nhập, không gọi API của hãng. Chỗ chưa chắc ghi "Cần xác minh ở
  Task 49" trong `docs/carriers/vtp.md`.
- **Q8 — `ClassifyError` không xuất** (hàm `classify` trong `errors.go`): chỉ adapter dùng; luật Go của dự án không
  xuất ký hiệu không ai ngoài gói gọi.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `carriertest.Run`: khả năng true qua ca của nó, false trả `ErrNotSupported` | `TestContractVTP` (Key, Capabilities, MapCanonical, Create/ok, timeout_after_create, fail_before_create, reject, auth_revoked, rate_limited, LookupByRef, Quote 429/401, Inspection/DeclaredValue/FeeOnRefuse, Cancel…Statement) | `go test -race -count=1 -v ./internal/adapters/carrier/vtp/...` | `--- PASS: TestContractVTP (0.32s)` |
| Contract vẫn đúng cho hãng giả và vẫn bắt adapter khai sai sau khi thêm nhánh `LookupByRef/Track = false` | `TestContractFake`, `TestContractCatchesLie`, `TestContractFrames` | `go test -race -count=1 ./internal/adapters/carrier/...` | `ok …/carrier/carriertest 2.340s` |
| Đăng nhập sai → `Health{OK: false}` + lỗi lớp auth; thiếu khóa không gọi mạng | `TestCheckConnection` | `go test -race -count=1 -v ./internal/adapters/carrier/vtp/...` | `--- PASS: TestCheckConnection` |
| Mã hết hạn giữa hai lần gọi → lấy lại đúng một lần, lần gọi thành công; vẫn bị từ chối → auth, không lấy lại lần hai | `TestTokenRefreshOnce` (Calls(login) 1→2→3→4), `TestTokenExpiryFromLogin` (hạn `expired`, đồng hồ giả), `TestTokenKeyedByCredentials`, `TestQuoteConcurrent` (-race) | `go test -race -count=1 -v ./internal/adapters/carrier/vtp/...` | `--- PASS` cả bốn |
| Báo phí Hà Nội → TP. HCM, 900 g, COD 630.000: dịch vụ và `Total` đúng mẫu tài liệu; thân gửi đúng ID, cân, COD | `TestQuoteSample` (PHS 26400 2 ngày; VHT 14700, Breakdown) | `go test -race -count=1 -v ./internal/adapters/carrier/vtp/...` | `--- PASS: TestQuoteSample` |
| Địa chỉ chưa có mã VTP → `address_unmapped` trước khi gọi mạng | `TestQuoteUnmapped` (5 ca, cả CreateShipment; Calls(login+quote+create) = 0) | `go test -race -count=1 -v ./internal/adapters/carrier/vtp/...` | `--- PASS: TestQuoteUnmapped` |
| Tạo vận đơn `E261118ABC123` → máy chủ nhận đúng mã ở `ORDER_NUMBER`, `CHECK_UNIQUE`; trả mã hãng, phí | `TestCreateSendsReference`, `TestCreateOrderPayment` (5 ca ORDER_PAYMENT) | `go test -race -count=1 -v ./internal/adapters/carrier/vtp/...` | `--- PASS` cả hai |
| `timeout_after_create` rồi tra theo mã: VTP không có tra → `ErrNotSupported`; gửi lại cùng mã → 206, không rõ, một đơn | `TestCreateTimeoutThenResend`; contract `Create/timeout_after_create rồi ok cùng mã` | `go test -race -count=1 -v ./internal/adapters/carrier/vtp/...` | `--- PASS: TestCreateTimeoutThenResend (0.30s)` |
| Lớp lỗi HTTP/mã VTP → `CarrierError` | `TestCreateErrorClasses` (204, 503, 429 Retry-After 7 s, 202, đăng nhập 429 khi tạo → chưa gửi), `TestAfterSendErrors` (7 ca: 500, 502, 205, thân hỏng, `{}`, 203, 400), `TestBaseURLByEnvironment` | `go test -race -count=1 -v ./internal/adapters/carrier/vtp/...` | `--- PASS` cả ba |
| Mọi mã trong `testdata/statuses.json` ra đúng `Canonical`; 104 và mã lạ → unknown | `TestMapCanonical` (21 mã; 104, 999, rỗng, `"501 "`) | `go test -race -count=1 -v ./internal/adapters/carrier/vtp/...` | `--- PASS: TestMapCanonical` |
| Địa giới V3 (tỉnh, phường), cấp khác không hỗ trợ | `TestLocations` | `go test -race -count=1 -v ./internal/adapters/carrier/vtp/...` | `--- PASS: TestLocations` |
| Tùy chọn khai false → `ErrNotSupported`, không gọi mạng; `Capabilities` theo tài liệu | `TestUnsupportedOptions`, `TestCapabilities` | `go test -race -count=1 -v ./internal/adapters/carrier/vtp/...` | `--- PASS` cả hai |
| Log và lỗi của cả bộ không chứa SĐT, địa chỉ, tên mẫu, mật khẩu, thông báo của hãng | `TestNoPersonalDataInLogsOrErrors` (+ `noPhone` của contract) | `go test -race -count=1 -v ./internal/adapters/carrier/vtp/...` | `--- PASS: TestNoPersonalDataInLogsOrErrors` |
| Cổng tĩnh + test gói đổi và phụ thuộc | cổng kiểm của task (ci-task) | ci-task.sh | `CI-TASK XANH (so với điểm tách nhánh d560fa9…)`; lần chạy đầu ĐỎ (gofumpt, 3 đoạn jscpd) đã sửa |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Q1, Q2, Q3, Q8 ở trên. Sửa ngoài `vtp/**`: `internal/adapters/carrier/registry.go` (1 dòng),
  `internal/adapters/carrier/carriertest/contract_test.go` (bỏ `vtp` khỏi `TestContractFrames`),
  `internal/adapters/carrier/carriertest/contract.go` (nhánh `LookupByRef = false` của ca tạo lại, nhánh `Track = false`
  của `GetStatus` trong ca `Create/ok`).
- `carriertest/testdata/vtp/statuses.json` giữ `{}`: bộ contract chỉ đọc tệp đó; bảng mã của VTP nằm ở
  `vtp/testdata/statuses.json` và được kiểm bằng `TestMapCanonical` của gói vtp (có cả mã ra `unknown`, mà tệp của
  contract không cho phép).

## Việc phát hiện thêm

- Nối `vtp.New(vtp.Config{HTTPClient: …})` vào `carrier.All` (registry) khi chủ shop OK gọi thật (Task 49).
- Task 126 (Track/Webhook): khi bật khả năng cần mã thô ra `in_transit` trong `carriertest/testdata/vtp/statuses.json`
  (contract `Track`, `Cancel` đẩy sự kiện theo tệp này).
- Task 126 / spec B10 ("webhook chỉ là tín hiệu, tra lại bằng API có xác thực"): tài liệu VTP không có API đọc trạng
  thái hay hành trình (I15 đã ghi "chưa xác minh endpoint tra cứu"); Task 126 phải chọn cách tin webhook (kiểm `TOKEN`
  bí mật) hoặc hỏi hãng — cần chủ shop quyết khi tới Task 126.
- Gửi Viettel Post 7 câu hỏi ở `docs/carriers/vtp.md` (chủ shop gửi qua người phụ trách tài khoản).

## Reviewer

Kết luận: ĐẠT (lần 1, 21/11/2026).

- Phát hiện phải sửa: không có.
- Ảnh hưởng: `vtp.New` đổi chữ ký, cả 2 chỗ gọi đã sửa; registry giữ hành vi khung (khóa `vtp`, mọi khả năng false);
  hai nhánh mới của `carriertest` (`LookupByRef = false`, `Track = false`) không làm yếu contract: nhánh `true` không
  đổi, nhánh mới vẫn chặt (lỗi không rõ, đúng một kiện, `Calls(create) == 2`; `GetStatus` phải `ErrNotSupported`).
- Bằng chứng: mọi dòng trỏ tới test có thật và kiểm đúng tiêu chí; không có test bị tắt hay nới.
- Ghi chú không bắt buộc (đã có trong tài liệu, chuyển Task 49/126): `carriertest/testdata/vtp/statuses.json` còn `{}`;
  503 khi tạo coi là chưa gửi, an toàn nhờ `CHECK_UNIQUE` (Task 49 xác minh 206); `Total` của `getPriceAll` không gồm
  phụ phí (COD) nên có thể thấp hơn phí thật khi so giá giữa các hãng.
