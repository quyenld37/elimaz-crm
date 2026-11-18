# Task 31 — Vận chuyển tối thiểu, hãng giả (M1-31)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 31 (M1-31)). Loại việc (quy trình mục
3): A. Vùng rủi ro cao: `internal/modules/fulfillment/status.go`, đường gọi `order.ApplyShipmentProgress` (đơn, tồn),
lệnh ra ngoài (`commands`, chỉ dùng, không sửa).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu có sẵn nào đổi chữ ký. Thêm trong `order`:
  `order.Lock(ctx, tx, id) error` (khóa đơn FOR UPDATE cho đường bắt đầu từ vận đơn, ADR-46 luật "bắt đầu từ nhóm
  sau"; dùng lại `lockOrder` sẵn có). Gói mới `fulfillment` (kiểu, `CarrierAdapter`, `NewShipmentCode`, `Advance`,
  `CreateForOrder`, `Get`, hai lệnh, handler tín hiệu), `internal/adapters/fakecarrier` (client HTTP + nguồn webhook).
- Gói phụ thuộc ngược (go list): `order` ← `internal/httpapi`, (mới) `fulfillment`; `fulfillment` ← `internal/httpapi`,
  `cmd/crm`, `internal/adapters/fakecarrier` (← `cmd/crm`, `tools/fakecarrier`). `internal/httpapi` ← `cmd/crm`, `tests/arch`, `tests/e2e`.
- Operation OpenAPI bị chạm, oasdiff: thêm mới `POST /api/v1/orders/{id}/shipments` `createShipment`
  (`fulfillment.ship`, 202) và `GET /api/v1/shipments/{id}` `getShipment` (`fulfillment.read`); mã lỗi mới
  `shipment_exists` 409. Chỉ thêm, oasdiff không phá vỡ.
- Chỗ dùng trên màn hình: không (client `console/src/api` sinh lại, chưa màn hình nào dùng).
- Câu sqlc, bảng, pha migration: migration mới `0118_fulfillment.sql` (pha mở rộng: CREATE bảng
  `fulfillment.carrier_accounts`, `shipments`, `shipment_events`, view `fulfillment.shipments_v`, GRANT). Câu mới
  `queries/fulfillment/{accounts,shipments,events}.sql`, khối sqlc mới → `internal/modules/fulfillment/internal/store`.
  Thêm dòng `docs/views.md` (ngoại lệ luật (g) cho `carrier_accounts_ref_key`; sự kiện `shipment.status_changed`).
  Không đổi bảng `ord.*` (cột `current_shipment_id` có từ 0117).
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/... ./internal/modules/order/...
  ./internal/adapters/fakecarrier/... ./tools/fakecarrier/... ./internal/httpapi/... ./cmd/crm/... ./tests/arch/...`;
  `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`commands.CommandKind`,
  `Handler`, `Looker`, `DoneHook`, `ResolveInDoubt`; `webhooks.Source`, `InboundHandler`, `InboundJobHandlers`,
  `NewSplitHandler`; `selftest.Source` làm mẫu chữ ký; `events.Registry.Emit`; `tenancy.AddRoute`; `queue.NewQueue`;
  `logging.Phone`); `order` (`AttachShipment`, `Writer.ApplyShipmentProgress`, `Get`, `lockOrder`); httpapi
  (`refusableWrite`, `canAt`, `warehouseScope`, `errForbiddenTarget`, `audit`); tìm "FOR UPDATE", "HMAC",
  "base32", "Crockford".
- Dùng lại / viết mới vì: dùng lại nguyên `commands` (Runner, in_doubt, ResolveInDoubt), `webhooks` (ingest, split,
  định tuyến, chống trùng tầng 1), `events.Emit`, `order.AttachShipment`/`ApplyShipmentProgress`, `canAt` của httpapi,
  `invtest.Shop`. Viết mới: `NewShipmentCode` (chỗ sinh mã vận đơn duy nhất theo kế hoạch; base32 Crockford không có
  sẵn trong thư viện chuẩn — `encoding/base32` có bảng chữ khác, dùng `base32.NewEncoding` với bảng Crockford), chữ ký
  HMAC của nguồn `fakecarrier` (cùng cách `selftest.Source`, nhưng header và khóa khác nhau theo nguồn; hai nguồn có lý
  do đổi khác nhau nên không gộp), `order.Lock` (công khai `lockOrder` cho fulfillment, không SQL mới).
- jscpd (lần `make ci` đầu, 9 đoạn trùng mới) — trả lời từng đoạn:
  - `MapStatus` của hãng giả trong bộ nhớ và của client HTTP: khác lý do đổi (bản thử của module và giao thức của
    chương trình hãng giả) → giữ riêng, viết bản trong bộ nhớ dạng bảng tra.
  - Kiểm chữ ký `fakecarrier` giống `selftest.VerifySignature`: không gộp vào `webhooks` (đổi gói M0 ngoài phạm vi; mỗi
    nguồn có header, khóa riêng) → viết lại bằng so `Sign(raw)` với header, thời gian hằng.
  - Test: dựng việc tách gói giống `tests/e2e/stack_test.go` → gộp lỗi bằng `errors.Join`; tạo đơn trong
    `handlers_shipment_test.go` trùng `handlers_order_test.go` → gộp thành `orderAPIEnv.mustCreate` dùng ở cả hai;
    đọc đơn, số dư trùng test của `order` (gói test khác, không nhập được) → viết `readAs`, `stockIs` riêng cho
    `fulfillment`; hai test refresh trùng phần dựng → `shipEnv.shipped`.

## Soát thiết kế

- 5.1 Chủ dữ liệu: tài khoản hãng, vận đơn, sự kiện vận đơn → `fulfillment` (schema `fulfillment`); đơn, tồn giữ ở
  `order`, `inventory`.
- 5.2 Không ghi bảng module khác: gắn vận đơn qua `order.AttachShipment`, đổi đơn và tồn qua
  `order.Writer.ApplyShipmentProgress`; fulfillment không nhập `inventory` (luật (a)).
- 5.3 Đọc qua view/API: đọc đơn (COD, địa chỉ giao) qua `order.Get` công khai, không SQL vào `ord.*`.
- 5.4 Nguyên tử: `CreateForOrder` = khóa đơn → chèn vận đơn `creating` → `AttachShipment` → `Enqueue` lệnh tạo, một tx.
  `refresh_status.OnDone` = khóa đơn → khóa vận đơn → chèn sự kiện → `Advance` → cập nhật vận đơn → áp lên đơn/tồn →
  `events.Emit`, một tx (tx ghi kết quả của Runner).
- 5.5 Không nguyên tử: gọi hãng chỉ ở `Execute`/`Lookup` (ngoài tx); `shipment.status_changed` qua outbox.
- 5.6 Gọi ra ngoài: lệnh `fulfillment.create_shipment` (in_doubt khi không rõ, `Lookup` theo mã vận đơn của CRM, không
  gửi lại khi không chắc); `fulfillment.refresh_status` chỉ đọc nên tự thử lại (`not_sent`) và `Lookup` luôn
  `not_sent` (đọc lại an toàn).
- 5.7 Ngữ cảnh shop: ba bảng có `tenant_id`, RLS (`enable_tenant_rls`), `UNIQUE (tenant_id, id)`, khóa ngoại ghép
  tenant; webhook định tuyến `carrier_account:<accountKey>` qua `platform.tenant_routes`.
- 5.8 Không biết HTTP: module `fulfillment` không nhập `net/http`; client HTTP và nguồn webhook (cần `http.Header`) ở
  `internal/adapters/fakecarrier` (mẫu `cfaccess`), `cmd/crm` nối.
- 5.9 Đường ghi: `createShipment` có `x-permission`, `IdempotencyKey`; quyền theo đích kho của tài khoản hãng.
- 5.10 Valkey: không áp dụng. 5.11 Thư viện mới: không (chỉ thư viện chuẩn).
- 5.12 Module mẫu: như `catalog`/`order` (`Writer` tự ghi sự kiện; sentinel trong `errors.go`; adapter ở
  `internal/adapters`).
- 6.1 Đồng thời: hai `createShipment` cùng đơn xếp hàng ở khóa đơn; unique một phần `(tenant_id, order_id) WHERE status
  <> 'cancelled'` là chốt cuối (`INSERT … ON CONFLICT DO NOTHING` rồi phân biệt). `refresh_status` khóa đơn rồi vận
  đơn; `Advance` chỉ tiến. Cập nhật vận đơn có điều kiện `version`.
- 6.2 Gửi lại: `Idempotency-Key` cho HTTP; webhook trùng: `DedupeKey = eventId` ở việc inbound và lệnh refresh;
  `shipment_events` `ON CONFLICT DO NOTHING` theo `(tenant_id, shipment_id, event_key)`; hãng giả trả lại kiện cũ khi
  cùng `code`.
- 6.3 Thứ tự khóa (ADR-46): đơn (7) → `fulfillment.shipments` (8) → `shipment_events` (8, chỉ chèn) → số dư (11, trong
  `ApplyShipmentProgress`) → outbox/jobs (13). Lệnh tạo: `OnDone` chỉ khóa `shipments` (8) rồi jobs (13). Bảng mới đã
  có trong ADR-46 nhóm 8 (Task M1-25).
- 6.4 Lỗi tạm: tx của Runner/Worker chạy lại bằng `RetryTx`; OnDone/Handle không giữ trạng thái giữa lần chạy.
- 6.5 Cách ly: READ COMMITTED + khóa dòng + câu điều kiện.
- 6.6 Việc nền lỗi: lịch thử lại của hàng đợi; payload hỏng → `ErrPermanent`.
- 6.7 Quá tải: không áp dụng ở hãng giả (M3 với hãng thật dùng `RetryAfter`).
- 6.8 Chết giữa chừng: lệnh tạo hết giữ chỗ → `in_doubt` → `ResolveInDoubt` tra theo mã; refresh → `Lookup` not_sent.
- 6.9 Treo: Timeout lệnh 30 s; client HTTP theo ctx.
- 6.10 Tiền: `cod_amount`, `fee` là `int64` đồng, CHECK 0…9007199254740991.
- 6.11 Quy luật: test rapid cho `Advance` (chuỗi trạng thái ngẫu nhiên → không bao giờ lùi, kết quả là max theo hạng)
  và `NewShipmentCode` (mọi `rnd` → khớp mẫu).
- 6.12 Dữ liệu cá nhân: `CreateRequest.ReceiverPhone` là `logging.Phone`; payload việc/sự kiện chỉ mang mã; log lỗi
  hãng không chép thân trả lời; `last_error` chỉ mã lý do.
- 6.13 Quyền: `fulfillment.ship`/`fulfillment.read` theo đích `warehouse` = kho của tài khoản hãng; thiếu → 403.

## Quyết định thay chủ shop

1. **Client HTTP và nguồn webhook của hãng giả ở `internal/adapters/fakecarrier`, không ở
   `internal/modules/fulfillment/internal/carriers/fake`.** Lý do: module không được nhập `net/http` (CLAUDE.md, luật
   (b) của test kiến trúc; `webhooks.Source.VerifySignature` nhận `http.Header`), và `cmd/crm` không nhập được gói
   `internal` của module. `internal/carriers/fake/adapter.go` vẫn có, là hãng giả trong bộ nhớ (không HTTP) cho test
   của module. Phương án khác: nới luật (b) cho thư mục con — trái CLAUDE.md, bỏ.
2. **Lệnh tạo vận đơn xong qua đường tra lại (`ResolveInDoubt` → done, `OnDone` nhận result nil) thì ghi lệnh
   `refresh_status` (khóa chống trùng `created:<id>`); `refresh_status` thấy vận đơn chưa có mã hãng thì tra
   `LookupByCode` rồi điền `tracking_code` và trạng thái.** Lý do: `commands` chỉ truyền result nil khi done do tra lại,
   gửi lại lệnh tạo có thể tạo trùng kiện ở hãng thật. `fee` để NULL ở đường này (LookupResult của kế hoạch không có
   phí; M3 thêm trường). Phương án khác: sửa `commands` cho Looker trả result — chạm vùng rủi ro cao của M0, bỏ.
3. **API của hãng giả xác thực bằng `Authorization: Bearer <FAKECARRIER_WEBHOOK_SECRET>`** (cùng bí mật với chữ ký
   webhook; cấu hình chỉ có một bí mật). Trang `/admin/*` không xác thực (công cụ dev, chỉ nghe 127.0.0.1 trong compose).
4. **Webhook của vận đơn lạ (không có `tracking_code` trong tenant, hoặc khác tài khoản) bỏ qua, không lỗi** — webhook
   chỉ là tín hiệu; lỗi vĩnh viễn sẽ làm việc `dead` và báo động giả.
5. **Trạng thái thô không biết của hãng → lệnh refresh `rejected` (dead), không đổi vận đơn** — để người có quyền thấy
   trong danh sách việc lỗi.
6. **`shipments` không có cột `seller_id`** (kế hoạch không ghi); người bán của vận đơn suy từ tài khoản hãng
   (`carrier_accounts.seller_id`), `CreateForOrder` đọc đơn bằng `order.Get(sellerID của tài khoản)` nên tài khoản của
   người bán khác không tạo được vận đơn cho đơn. Có khóa ngoại `(tenant_id, order_id) → ord.orders` (khóa ngoại xuyên
   module, đánh đổi có chủ ý như `stock_reservations`, ADR-27 mục 4).
7. **`UNIQUE (tenant_id, carrier_key, external_account_ref)` được miễn luật (g)** (`seller-exempt`): một tài khoản ở hãng
   chỉ thuộc một người bán trong shop.
8. **Tuyến webhook `carrier_account:<accountKey>` không kèm tên hãng** (đúng kế hoạch); M3 có nhiều hãng thì cần tách
   theo hãng — ghi ở "Việc phát hiện thêm".
9. **Đơn không giao được (không có, của người bán khác tài khoản hãng, chưa ở `confirmed`) → 404 `not_found`**, không
   thêm mã lỗi mới (kế hoạch chỉ khai `shipment_exists`). Ở M1 đơn chỉ rời `confirmed` sang `delivered` sau khi đã có
   vận đơn, nên trường hợp này thực tế chỉ là đơn không có; M2 có đơn hủy thì cân nhắc mã riêng.
10. **Lệnh `refresh_status` thứ tự `best_effort`** (cùng `OrderKey = shipment:<id>` như kế hoạch): một lần tra `dead`
    (trạng thái lạ) không chặn mãi các lần tra sau của vận đơn; hai lần tra cùng vận đơn vẫn không chạy song song, và
    `OnDone` khóa đơn rồi vận đơn nên thứ tự không làm sai trạng thái (chỉ tiến). Phương án khác: `strict` mặc định —
    một lần tra lỗi chặn vĩnh viễn, bỏ.
11. **Khóa chống trùng có tiền tố theo nguồn:** `create:<shipmentId>` (lệnh tạo), `evt:<eventId>` (tín hiệu webhook),
    `created:<shipmentId>` (tra sau khi lệnh tạo xong do tra lại) để các không gian tên không đụng nhau trong cùng loại
    lệnh. Kế hoạch ghi `DedupeKey = eventId`; nghĩa giữ nguyên.
12. **Lệnh tạo vận đơn `dead`/`expired` (hãng từ chối, quá 10 phút chưa gửi được) để vận đơn ở `creating`** và đơn giữ
    vận đơn hiện hành; hủy vận đơn, gỡ khỏi đơn để tạo lại là việc của M3 — ghi ở "Việc phát hiện thêm".

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test thất bại trước (bước 2) | cả bộ `internal/modules/fulfillment` | `go test ./internal/modules/fulfillment/` trước khi có mã | `FAIL … [build failed]` (gói chưa có) |
| Đường thường: tạo → `awaiting_pickup` có mã hãng, phí 25000, hãng 1 kiện; picked_up → `shipping`, giữ hàng `in_transit`; delivered → đơn `delivered`, `on_hand` 5→3; 3 sự kiện `shipment.status_changed` | `TestShipment_HappyPath` | `go test -race -count=1 -v ./internal/modules/fulfillment/` | `--- PASS: TestShipment_HappyPath` |
| `timeout_after_create` → `in_doubt`; `ResolveInDoubt` → done 1; hãng vẫn 1 kiện; vận đơn có mã hãng | `TestShipment_TimeoutAfterCreate` | như trên | `--- PASS` |
| `fail_before_create` → lệnh về `pending`, 0 kiện | `TestShipment_FailBeforeCreate` | như trên | `--- PASS` |
| Vận đơn thứ hai cùng đơn → `ErrShipmentExists`; unique một phần, CHECK mã, `shipment_events` không sửa được bằng `crm_app` | `TestShipment_SecondForSameOrder`, `TestShipment_DatabaseGuards` | như trên | `--- PASS` |
| Webhook cùng `eventId` 3 lần → 1 lệnh tra, 1 dòng `shipment_events`, tồn đổi 1 lần (VC08) | `TestRefresh_SameEventThreeTimes`; `TestIngestPipeline` (đường vào thật: 1 việc `inbound.fakecarrier`) | `go test -race ./internal/modules/fulfillment/ ./internal/adapters/fakecarrier/` | `--- PASS`, `ok` |
| `forge`: webhook nói delivered, API trả trạng thái thật → vận đơn, đơn, tồn không đổi | `TestRefresh_ForgedSignal`; `TestAdvanceAndForge` (hãng giả HTTP) | như trên; `go test -race ./tools/fakecarrier/` | `--- PASS`, `ok` |
| `shipping` tới sau `delivered` → không lùi, thêm 1 dòng sự kiện (VC09) | `TestRefresh_LateShippingAfterDelivered`, `TestAdvance`, `TestAdvance_Property` (rapid 200) | như trên | `--- PASS` |
| `NewShipmentCode` rnd cố định → `E261119000000` (23:30 UTC = ngày sau giờ VN); mọi rnd khớp mẫu | `TestNewShipmentCode_Fixed`, `TestNewShipmentCode_Property` | như trên | `--- PASS` |
| `fulfillment.ship` phạm vi `warehouse:w2` dùng tài khoản hãng kho MAIN → 403; chỉ `order.read` gọi `getShipment` → 403; 202, 409 `shipment_exists`, gửi lại cùng khóa 202, audit 1 dòng | `TestShipmentAPI` | `go test -race -run TestShipmentAPI ./internal/httpapi/` | `ok` |
| Webhook chữ ký sai → 401, không lưu; tài khoản lạ → `inbound_unrouted` fakecarrier = 1, không việc | `TestIngestPipeline`, `TestSourceSignatureAndSplit` | `go test -race ./internal/adapters/fakecarrier/` | `ok` |
| `CRM_ENV=production` → `POST /hooks/fakecarrier` 404; `test` + `FAKECARRIER_URL` → 401 (nguồn có) | `TestIngestFakeCarrierOnlyOutsideProduction` (tệp chạy thật) | `go test -run 'TestIngestFakeCarrier…' ./tests/e2e/` | `ok` |
| Test kiến trúc: module fulfillment (cả `internal/carriers/fake`) không nhập `net/http`; mã thật không vi phạm | `TestBoundariesNotCalledModule`, `TestBoundariesRealCode` | `go test ./tests/arch/` | `ok` |
| Client hãng: 503/không nối được → `ErrNotSent`, 4xx → `ErrRejected`, quá giờ → lỗi không rõ | `TestClientCreateShipment`, `TestClientLookupAndStatus`, `TestFailureModes` | `go test -race ./internal/adapters/fakecarrier/ ./tools/fakecarrier/` | `ok` |
| Hãng giả: cùng `code` trả kiện cũ, Bearer sai → 401, sống qua khởi động lại | `TestParcelsAPI`, `TestPersistence`, `TestLoadConfig` | `go test -race ./tools/fakecarrier/` | `ok` |
| seed-dev: tài khoản `fake-1` ở kho MAIN, tuyến `carrier_account:fake-1`, chạy 2 lần vẫn 1 dòng | `TestSeedDev` | `go test -run TestSeedDev ./tests/e2e/` | `ok` |
| `order.Lock`: có → khóa, lạ → `ErrNotFound` | `TestLock` | `go test -race -run TestLock ./internal/modules/order/` | `ok` |
| Cổng đầy đủ | — | `make ci` trên bản đã stage, sau lần gộp `main` thứ hai (Task 396, 39) | exit 0: build, vet, golangci-lint 0, gen-check, sqlc vet, `go test -race ./...` mọi gói `ok`, govulncheck "No vulnerabilities found", oasdiff, jscpd "Found 0 clones", test-guard, catalog-check, gitleaks "no leaks found". Các lần trước: lần 1 gen-check đỏ vì chưa stage; lần 2 nhiều gói hết giờ ở `DROP DATABASE` (PostgreSQL quá tải lúc reviewer chạy song song) và SeaweedFS dựng sai cổng 8531 (biến phiên là 20031) — dựng lại compose; lần 3 jscpd 9 đoạn trùng (đã sửa, xem Tái dùng), rồi oasdiff đỏ vì `main` vừa thêm `searchVariants` → gộp `main` lại |
| Image hãng giả dựng và chạy | — | `docker build -f tools/fakecarrier/Dockerfile …; docker run …; curl /health` | `{"parcels":0}` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Tệp: thêm `internal/adapters/fakecarrier/{client.go,source.go}` (quyết định 1); `webhook_source.go` của module chỉ
  chứa handler tín hiệu (`SignalHandler`) và dạng tín hiệu chung; thêm `tools/fakecarrier/Dockerfile` (compose dev đã
  trỏ tới, `make dev` bật profile khi có tệp).
- `order.Lock` mới (một hàm công khai bọc `lockOrder`).
- `fulfillment` thêm `errors.go` (sentinel), `Writer` không cần: `CreateForOrder` là hàm của gói như kế hoạch, hai lệnh
  nhận `CommandOptions` (Events, Queue, Realtime cho bước áp lên đơn của `order.Writer`). Thêm `EnsureCarrierAccount`,
  `AccountWarehouse` (seed và kiểm quyền theo kho).
- Hãng giả trả 503 `not_created` cho `fail_before_create` (client đổi thành `ErrNotSent`); không nối được (lỗi dial)
  cũng là `ErrNotSent`.

## Việc phát hiện thêm

- M3: tuyến webhook `carrier_account:<accountKey>` chưa kèm tên hãng; hai hãng trùng mã tài khoản sẽ đụng nhau ở
  `platform.tenant_routes` (khóa toàn hệ thống). Đề xuất khóa `<carrier_key>:<accountKey>` khi có hãng thật thứ hai.
- M3: vận đơn `creating` mà lệnh tạo `dead`/`expired` không tự hủy; cần đường hủy vận đơn và gỡ vận đơn hiện hành
  khỏi đơn (`order`) để tạo lại.
- M3: `LookupResult` chưa có phí; vận đơn hoàn tất qua đường tra lại có `fee` NULL.
- M3 (reviewer): webhook tới trước khi vận đơn có `tracking_code` (hãng thật lấy hàng ngay sau khi tạo, trước khi
  `CreateCommand.OnDone` commit) bị bỏ qua; vận đơn chờ tới tín hiệu kế tiếp. Cần lịch tra định kỳ vận đơn chưa xong.
- M2 (reviewer): `RefreshCommand.OnDone` lỗi lặp lại được (ví dụ `ApplyShipmentProgress` trả `ErrStatusConflict` khi
  có đơn hủy) → lệnh `in_doubt` → `Lookup` not_sent → chạy lại mỗi ~30 s không ai thấy. M2 thêm đơn hủy thì cần chặn
  (ví dụ OnDone bỏ qua bước của đơn đã hủy, hoặc báo `rejected`).
- Điểm dừng M1-A (reviewer): `tools/fakecarrier` nghe mọi địa chỉ, `/admin/*` không xác thực — khi đưa lên staging,
  cổng 4194 không được mở ra ngoài.
- M3 (reviewer): `createShipment` kiểm quyền ở kho của tài khoản hãng, không đối chiếu kho giữ hàng của đơn (đúng kế
  hoạch); xem lại khi nhiều kho.

## Reviewer

Kết luận: ĐẠT (lần 3, trên `git diff --cached` sau lần gộp `main` thứ hai).

- Lần 3: phần gộp đúng — `writeDomainError` giữ cả `queriesError` (Task 39) và `fulfillmentError`; migration 0118 áp
  được sau 0120–0124 (goose cho phép ngoài thứ tự, có test); gửi lại cùng khóa của `createShipment` đi qua cơ chế lưu
  đúng byte của Task 396; mã sinh có đủ phần của main và task 31. Không phát hiện phải sửa.
- Lần 2 (sau khi sửa trùng jscpd): ĐẠT — kiểm chữ ký viết lại vẫn thời gian hằng, không mở đường giả mạo; test không bị
  nới.
- Lần 1:

- Không có phát hiện phải sửa. Đã soát: thứ tự khóa ADR-46 (đơn → vận đơn → sự kiện → số dư → jobs), chạy lại an toàn
  của hai OnDone, in_doubt/ResolveInDoubt (không gửi lại lệnh tạo), ba lớp chống trùng webhook, quyền theo kho, RLS và
  GRANT của 0118, dữ liệu cá nhân, hai chốt chặn fakecarrier ở production.
- Ảnh hưởng: khớp; lệch nhỏ đã ghi nhận — `tools/fakecarrier` cũng phụ thuộc `internal/adapters/fakecarrier`; `newInbound`,
  `newCommands` (không xuất) đổi tham số, mỗi hàm một nơi gọi đã sửa; `models.go` của các store khác sinh lại do sqlc.
- Bốn ghi chú không bắt buộc đã chép vào "Việc phát hiện thêm".
