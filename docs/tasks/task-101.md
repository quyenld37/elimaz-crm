# Task 101 — M3-10: Webhook hãng vào ingest

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 101, M3-10). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Xóa `fulfillment.SignalHandler`, `fulfillment.NewSignalHandler` (3 chỗ gọi: `cmd/crm/wire_m3.go`, `tests/e2e/stack_test.go`,
    `internal/modules/fulfillment/env_test.go`, cộng `internal/adapters/carrier/fake/fake_test.go`) — thay bằng
    `inbound.Handler` do `Module.InboundHandlers()` trả, mỗi hãng có khả năng Webhook một handler.
  - `fulfillment.Signal` đổi trường (M1: `CarrierKey, AccountKey, TrackingCode, EventID` → `RouteKey, EventKey,
    TrackingCode, ShipmentCode`); chỗ dùng: `fake/source.go` (xóa), `fake_test.go`, `env_test.go`.
  - Thêm `fulfillment.SplitSignals(a CarrierAdapter, raw, routeKey)`, `fulfillment.ErrNoRouteKey`,
    `fulfillment.ErrShipmentNotYetKnown`; thêm `carrier.WebhookSource(a, secret) webhooks.RoutedSource`,
    `carrier.WebhookSources(adapters, secret func(key) string)`.
  - Xóa `fake.Source`, `fake.NewSource`, `fake.Key` ("fakecarrier") của gói `internal/adapters/carrier/fake`; thêm
    `fake.Verify(raw, h, secret)` (giữ `fake.Sign`, `SignatureHeader`, `Packet`, `Event`). Chỗ dùng: `wire_m3.go`,
    `wire_m3_test.go`, `tests/e2e/stack_test.go`, `tools/fakecarrier/server/{server_test,harness_test}.go`, `fake_test.go`.
  - Thêm `fulfillment.EnsureAccountRoute(ctx, tx, accountID) (routeKey, error)`: đăng ký tuyến
    `carrier_account:<route_key>` qua `tenancy.PutRoute` (put_tenant_route) cho tài khoản dựng bằng
    `EnsureCarrierAccount` (không đổi). Chỗ gọi: `cmd/crm/seed.go`, `tests/e2e/stack_test.go`,
    `internal/adapters/carrier/webhook_test.go`. Không gọi trong `shiptest.NewShop` (test M1 dựng shiptest trên lược đồ
    trước 0300).
  - Lệnh tạo vận đơn (M1, `create_shipment.go` `Shipment.account()`) nay truyền `Account.RouteKey` cho hãng (đọc
    `route_key` trong `GetShipment`): thiếu nó hãng giả gửi webhook về `/hooks/fake` không khóa tuyến (phát hiện khi
    chạy `TestMotDonDiTron`).
  - Sửa chú thích `wire.go` (2 dòng, không đổi mã), `cmd/crm/main.go` (chú thích `ingestSources`).
- Gói phụ thuộc ngược (go list): `fulfillment` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/adapters/carrier/**`,
  `internal/modules/{cash,order}` (đọc kiểu), `internal/testkit/shiptest`, `tests/e2e`, `tools/fakecarrier/server`.
  Gói `fake` được nhập bởi `internal/adapters/carrier`, `cmd/crm`, `tests/e2e`, `tools/fakecarrier/**`,
  `internal/adapters/carrier/carriertest`, `internal/httpapi` (test).
- Operation OpenAPI bị chạm, oasdiff: không (đường `/hooks/{source}/{routeKey}` của ingest có sẵn, không thuộc openapi.yaml).
- Chỗ dùng trên màn hình: không (WebhookURL `/hooks/<hãng>/<route_key>` của Task 100 giờ thật sự nhận được).
- Câu sqlc, bảng, pha migration: tệp mới `queries/fulfillment/inbound.sql` (`InboundAccountByRouteKey`,
  `InboundShipmentByCode`, `InboundShipmentByTracking`, đọc `fulfillment.carrier_accounts`, `fulfillment.shipments`);
  xóa `ShipmentByTracking` (M1, tìm theo `external_account_ref` — cách cũ); `GetShipment` đọc thêm `a.route_key`.
  Không migration (kế hoạch: task này không có migration; số 0313 không dùng).
- Đường webhook của hãng giả đổi `/hooks/fakecarrier` → `/hooks/fake/<route_key>` (nguồn = khóa hãng `a.Key()`):
  `tests/e2e/{ingest_test,mot_don_di_tron_test,stack_test,seed_test}.go`, `docs/RUNBOOK.md`, `docs/threat-model-m1.md`.
  Chưa đổi (luật phiên, xem "Dừng để hỏi"): `docker/compose.dev.yml:284`, `tests/deploy/compose_demo_test.go:16`; bị ảnh
  hưởng gián tiếp: `scripts/demo/mot-don-di-tron.sh` (`make demo-mot-don`).
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/... ./internal/adapters/carrier/... ./cmd/crm/...
  ./tools/fakecarrier/... ./internal/testkit/...`; `tests/e2e` (pipeline, dboutage, ingest, seed, một đơn đi trọn);
  `tests/arch`; vùng rủi ro cao `internal/platform/webhooks` không sửa (ci-task không chọn gói đó; đường vào thật của
  nó chạy trong `TestWebhookRouted_*` và `tests/e2e`).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `webhooks.RoutedSource`, `webhooks.InboundHandler`,
  `webhooks.InboundJobHandlers`, `tenancy.PutRoute`, `queue.JobQueue.Enqueue`, `metrics.Registry.Inc`,
  `fulfillment.enqueueRefresh` (M1), `shipmentOrderKey`, `selftest` (mẫu nguồn).
- jscpd lần chạy đầu báo 3 đoạn trùng mới ở `webhook_test.go` (dựng tenant/shop, `app`, `ownerExec` giống
  `cash/env_test.go`, `customer/stats_test.go`, `fulfillment/env_test.go`): sửa bằng cách dùng `testkit.InTenant`,
  `Staff.TryExec` có sẵn thay vì chép helper; lần sau 0 đoạn trùng mới.
- Dùng lại / viết mới vì: dùng lại toàn bộ phần nền tảng webhook (không sửa `internal/platform/webhooks`), lệnh tra
  `fulfillment.refresh_status` (Task 102) và `RefreshCommandKind`/`shipmentOrderKey`; viết mới `inbound.Handler` (thay
  `SignalHandler` của M1, xóa cách cũ) và nguồn chung `carrier.WebhookSource` (thay `fake.Source` riêng của hãng giả).

## Soát thiết kế

- 5.1 Chủ dữ liệu: chỉ đọc bảng của `fulfillment` (tài khoản, vận đơn) trong module `fulfillment`; tuyến ghi qua
  `platform.put_tenant_route` (SECURITY DEFINER, hàm của nền tảng).
- 5.2, 5.3: không ghi, không đọc bảng module khác.
- 5.4, 5.5: handler chỉ ghi lệnh `fulfillment.refresh_status` (Enqueue) trong tx của việc vào — nguyên tử với việc
  vào `done`; không gọi hãng trong tx.
- 5.6: gọi hãng chỉ ở lệnh tra (đã có, Task 102). Webhook chỉ là tín hiệu (B10): không đổi trạng thái vận đơn, đơn, tồn.
- 5.7: handler chạy trong tx có `app.tenant_id` (InboundJobHandlers); tìm tài khoản theo `route_key` nhờ RLS chỉ thấy
  tenant hiện tại, unique `(tenant_id, route_key)`; vận đơn tìm theo `(carrier_account_id, code)` rồi
  `(carrier_account_id, tracking_code)`, không bao giờ chỉ theo mã hãng (Q-7, VC13).
- 5.8: module không nhập `net/http` → `webhooks.Source` (chữ ký có `http.Header`) đặt ở `internal/adapters/carrier`
  (xem Quyết định thay chủ shop).
- 5.9: không có đường ghi HTTP mới; ingest kiểm chữ ký trên thân gốc như M0.
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện mới).
- 5.12: theo mẫu `selftest` (nguồn) và handler vào của M1.
- 6.1: không sửa dòng nào; hai việc vào cùng vận đơn cùng lúc → Enqueue chống trùng theo `DedupeKey`
  `sig:<shipment>:<eventKey>`; khác sự kiện → hai lệnh tra (lệnh tra áp kết quả theo version, Task 102).
- 6.2: chống trùng tầng 1 ở việc vào (`DedupeKey = routeKey:eventKey`, theo tài khoản hãng), tầng 2 ở lệnh tra
  (`sig:<shipment>:<eventKey>`). Cùng `eventId` 3 lần → 1 lệnh tra.
- 6.3: không khóa dòng.
- 6.4, 6.6: vận đơn chưa biết → `ErrShipmentNotYetKnown` (lỗi tạm) → lịch thử lại M0, hết 8 lần → `dead` + cảnh báo; thân
  không đọc được, thiếu trường, gói không có `routeKey` → `ErrPermanent` (dead ngay). Hàm chạy lại được (không biến
  cộng dồn; chỉ số tăng theo lần chạy như `inbound_unrouted` của M0).
- 6.5, 6.7, 6.8, 6.9: mặc định của hàng đợi (handler 30 s).
- 6.10, 6.11: không áp dụng (không tiền; quy luật "webhook không đổi trạng thái" có test ca thân nói Delivered).
- 6.12: không log mã vận đơn, thân, header; chỉ số chỉ mang nhãn khóa hãng.
- 6.13: không áp dụng (không quyền người dùng; xác thực là chữ ký hãng + routeKey ngẫu nhiên 32 ký tự).
- Trùng thân: unique chống trùng của `webhook_deliveries` do task chung quyết (có `route_key`); hai tài khoản nhận cùng
  một thân byte-for-byte không xảy ra với hãng (thân có mã tài khoản hoặc mã vận đơn); khóa chống trùng sự kiện có
  tiền tố `routeKey` nên hai tài khoản không gộp nhau.

## Quyết định thay chủ shop

1. **Nguồn webhook đặt ở `internal/adapters/carrier` (`carrier.WebhookSource`), không ở `fulfillment.WebhookSource`
   như kế hoạch.** Lý do: `webhooks.Source.VerifySignature` nhận `http.Header`; module không được nhập `net/http` (luật
   (b), `tests/arch/boundaries_test.go` chặn). Phần logic (khóa chống trùng, tuyến, OrderKey, payload) vẫn ở
   `fulfillment.SplitSignals` (test ở `webhook_source_test.go`); gói adapter chỉ nối `VerifyWebhook` + `SplitSignals`.
   Phương án khác: thêm `net/http` vào fulfillment và nới test kiến trúc (trái luật, không chọn).
2. **Đường hãng giả là `/hooks/fake/<route_key>`** (nguồn = khóa hãng như kế hoạch), bỏ `/hooks/fakecarrier`. Test e2e,
   RUNBOOK, threat model đổi theo. `docker/compose.dev.yml` còn `.../hooks/fakecarrier` — không sửa (luật phiên: không
   đổi hạ tầng `docker/`), xem "Dừng để hỏi".
3. **Payload tín hiệu mang thêm `routeKey`, `eventKey`** (kế hoạch ghi `{trackingCode, shipmentCode}`): handler cần
   `routeKey` để tìm tài khoản trong tenant và `eventKey` cho khóa `sig:<id>:<eventKey>`; `webhooks.Inbound` không mang
   khóa tuyến. Không mang trạng thái hãng.
4. **Tài khoản không thấy theo `route_key` hoặc khác hãng của nguồn** (tuyến có trong `tenant_routes` nhưng tài khoản
   của hãng khác): bỏ tín hiệu, tăng `carrier_signal_unknown_account` (kế hoạch không nói; không thử lại vì tài khoản
   không tự xuất hiện; vận đơn vẫn được tra bù theo lịch).
5. **Lệnh tra có payload `{shipmentId, reason: "webhook"}`** (theo kế hoạch); `RefreshCommand` bỏ qua `reason`.
6. **Hàm mới `EnsureAccountRoute` (seed, test) đăng ký tuyến theo `route_key`** thay cho `tenancy.AddRoute(...,
   "fake-1")` ở seed và stack e2e; tuyến cũ `carrier_account:fake-1` không còn ghi. Tuyến cũ đã có trên database dev
   cũ không bị xóa (vô hại: không nguồn nào định tuyến theo `fake-1` nữa).
7. **`OccurredAt` của tín hiệu để trống** (`WebhookSignal` không mang giờ): việc vào xếp theo `OrderBestEffort`, tín
   hiệu chỉ là "hỏi lại", thứ tự không đổi kết quả.

## Dừng để hỏi

- **Cần OK trước khi gộp vào main (hoặc điều phối viên áp ngay sau gộp):** `docker/compose.dev.yml` dòng 284
  `FAKECARRIER_WEBHOOK_URL: ${CRM_FAKECARRIER_SECRET:+http://crm-ingest:4191/hooks/fakecarrier}` phải đổi đuôi thành
  `/hooks/fake`, cùng hằng `fakeCarrierHook` ở `tests/deploy/compose_demo_test.go:16` (`.../hooks/fake`). Chưa đổi vì luật
  phiên cấm sửa `docker/`.
- Hậu quả nếu chưa áp (reviewer chỉ ra, đã kiểm): trên compose dev, hãng giả gửi webhook tới
  `/hooks/fakecarrier/<route_key>` → ingest trả 404, webhook mất. `make demo-mot-don` (`scripts/demo/mot-don-di-tron.sh`,
  chờ `WAIT_SECONDS` 10 s) **thất bại ở bước "vận đơn shipping"**: tra bù theo lịch (Task 102) chạy mỗi 600 s, không kịp.
  Không ảnh hưởng production (không có hãng giả), không ảnh hưởng `go test` (e2e dựng stack riêng với `/hooks/fake`).
  `docs/RUNBOOK.md` đã ghi `/hooks/fake` nên tới khi áp bản vá, RUNBOOK và compose khác nhau.
- Database dev cũ: việc `inbound.fakecarrier` còn chờ (nếu có) không còn handler, sẽ dead; vô hại, chỉ ở dev.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `POST /hooks/fake/<route_key>` chữ ký đúng → 200; bản nhận có `route_key`; 1 việc vào của tenant A; 1 lệnh tra cho V1; V1 không đổi (thân nói delivered — VC17) | `TestWebhookRouted_SignalOnly` | `go test -race -count=1 -run TestWebhookRouted -v ./internal/adapters/carrier/` | `--- PASS: TestWebhookRouted_SignalOnly (0.42s)` |
| VC17: chữ ký sai → 401, 0 bản nhận | `TestWebhookRouted_SignalOnly` (đầu test) | như trên | PASS |
| M1 Task 31 trên đường routeKey: cùng eventId 3 lần → 1 việc vào, 1 lệnh tra; 3 lần → 1 dòng `shipment_events`; forge | `TestWebhookRouted_SignalOnly`; `TestRefresh_SameEventThreeTimes`; `TestRefresh_ForgedSignal` | trên; `go test -race -count=1 ./internal/modules/fulfillment/` | `--- PASS` cả ba |
| routeKey không có trong `tenant_routes` → `inbound_unrouted` +1, không việc vào | `TestWebhookRouted_SignalOnly` | như dòng 1 | PASS |
| Đường `/hooks/fake` không routeKey → nguồn trả lỗi, việc tách dead | `TestWebhookRouted_SignalOnly`; `TestSplitSignals_Errors` (ErrNoRouteKey) | như dòng 1; `go test -run TestSplitSignals ./internal/modules/fulfillment/` | PASS |
| Tài khoản revoked → `carrier_signal_revoked_account` +1, không lệnh tra; route lạ, khác hãng → `carrier_signal_unknown_account` | `TestWebhookRouted_EarlyAndRevoked`; `TestSignal_AccountDropped` | như trên | PASS |
| VC10: tín hiệu trước khi có mã hãng → việc vào `pending`, `attempts = 1`; gắn mã, chạy lại → 1 lệnh tra, việc vào `done` | `TestWebhookRouted_EarlyAndRevoked`; `TestSignal_ShipmentNotYetKnown` | như trên | PASS |
| VC13: A1 (`fake`), A2 (`fake_b`) cùng mã `X123`; tín hiệu vào tuyến A2 → lệnh tra chỉ cho vận đơn A2 | `TestSignal_SameTrackingTwoAccounts` | `go test -race -count=1 ./internal/modules/fulfillment/` | PASS |
| Vận đơn theo `(tài khoản, mã CRM)` rồi `(tài khoản, mã hãng)`, không thấy ở tài khoản khác; lệnh tra `{shipmentId, reason: webhook}`, khóa `sig:<id>:<eventKey>`, OrderKey `shipment:<id>`; vận đơn không đổi | `TestSignal_EnqueuesRefreshOnly`; `TestSignal_ShipmentNotYetKnown` (tuyến đối tác) | như trên | PASS |
| `DedupeKey = routeKey:EventKey`, rỗng → `routeKey:sha256(thân):chỉ số`; khác tài khoản khác khóa; Route, OrderKey, payload; thiếu mã → invalid | `TestSplitSignals` | như trên | PASS |
| Payload thiếu trường → ErrPermanent | `TestSignal_BadPayload` | như trên | PASS |
| Chữ ký `x-fake-signature` giữ như M1; SplitWebhook | `TestVerifyAndSplitWebhook` | `go test -race -count=1 ./internal/adapters/carrier/fake/` | `ok` |
| Bí mật webhook chỉ ở vai trò ingest; nguồn là RoutedSource khóa `fake` | `TestWireM3_FakeWebhookSecretByRole`, `TestWireM3_KeepsM1Parts` | `go test -race -count=1 ./cmd/crm/` | `ok` |
| Lệnh tạo gửi `route_key` cho hãng | `TestCreateShipment_SendsRouteKey` | `go test -count=1 -run TestCreateShipment_SendsRouteKey ./internal/modules/fulfillment/` | bỏ `RouteKey: s.routeKey` → `FAIL`; có → `PASS` |
| Seed: tuyến `carrier_account:<route_key>`, không còn `fake-1`, chạy 2 lần 1 dòng | `TestSeedDev` (`checkSeedCarrier`) | `go test -count=1 -run TestSeedDev ./tests/e2e/` | `--- PASS: TestSeedDev` |
| selftest M0 còn xanh; một đơn đi trọn qua webhook hãng giả thật trên `/hooks/fake/<route_key>`; production → 404 | `TestPipelineUnderTwoSecondsOneTrace`, `TestBinaryWorkerHandlesSelftest`, `TestDatabaseOutageKeepsWebhooks`, `TestIngestServesHooks`, `TestMotDonDiTron`, `TestMotDonDiTron_HaiDonSongSongTon1`, `TestIngestFakeCarrierOnlyOutsideProduction` | `go test -race -count=1 ./tests/e2e/ -run '…'` | `--- PASS` tất cả |
| Cổng của task | ci-task | lần 2, sau khi sửa lint, gitleaks, jscpd của lần 1 | `CI-TASK XANH (so với điểm tách nhánh fcbcdcf…)`; `ok tests/e2e 52.6s`, `ok fulfillment 59.6s`, `ok tests/arch 26.1s` |

## Test bị xóa hoặc tắt

- `TestSignal_UnknownShipmentIgnored` (`refresh_status_test.go`, M1: mã hãng lạ → bỏ qua, không lỗi): kế hoạch đổi
  hành vi (vận đơn chưa biết → `ErrShipmentNotYetKnown`, thử lại). Thay bằng `TestSignal_ShipmentNotYetKnown`.
- `TestSourceSignatureAndSplit`, `TestIngestPipeline` (`internal/adapters/carrier/fake/fake_test.go`, M1): nguồn
  `fake.Source` bị xóa. Phần chữ ký, tách gói giữ ở `TestVerifyAndSplitWebhook`; phần đường vào (401, tài khoản lạ,
  cùng eventId 3 lần) chạy lại trên đường có routeKey ở `TestWebhookRouted_SignalOnly`.

## Khác kế hoạch

- Xem "Quyết định thay chủ shop" 1–7.
- Test của handler nằm ở `internal/modules/fulfillment/inbound_signal_test.go` (gói `fulfillment_test`, dùng lại
  `shipEnv` của M1) thay cho `internal/inbound/inbound_test.go`; test đường vào đầy đủ (ingest → tách gói → handler)
  ở `internal/adapters/carrier/webhook_test.go` (nơi có nguồn webhook).

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 2).

- Lượt 1: CẦN SỬA — `docker/compose.dev.yml:284`, `tests/deploy/compose_demo_test.go:16` còn `/hooks/fakecarrier` nên
  `make demo-mot-don` hỏng, mà "Dừng để hỏi" ghi sai là "vẫn chạy nhờ tra bù". Sửa: luật phiên cấm sửa `docker/` nên ghi
  đúng hậu quả, bản vá hai dòng, điều kiện cần OK trước khi gộp; mục Ảnh hưởng nêu docker, tests/deploy, scripts/demo.
- Lượt 2: ĐẠT. Không còn phát hiện phải sửa; mọi ký hiệu xóa không còn chỗ dùng, cách cũ đã xóa hẳn, mỗi dòng bằng
  chứng trỏ tới test có thật và kiểm đúng điều đó; ba test bị xóa có lý do và test thay. Ghi chú: phải áp hai dòng
  compose/test deploy cùng lúc rồi chạy `go test ./tests/deploy/` và `make demo-mot-don`; adapter hãng thật sau này phải
  giữ quy ước `VerifyWebhook` (không chữ ký + bí mật rỗng → true; có bí mật mà sai → false).
