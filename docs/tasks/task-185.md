# Task 185 — M5-02: ReadCache; báo hiệu hệ thống qua RealtimeBus; consumer làm mới bộ đệm

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 185, M5-02; đối chiếu giả định
`docs/m5/doi-chieu-gia-dinh.md` dòng A4 và M0). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `realtime.Signal` thêm trường `System bool` (thêm trường, không đổi trường cũ): 16 tệp dựng `realtime.Signal{…}`
    theo tên trường, không tệp nào dựng theo vị trí → không phải sửa chỗ dùng.
  - `realtime.Bus` thêm phương thức `SubscribeSystem(topicPrefix string, fn func(Signal)) (cancel func())`: 17 chỗ nhắc
    `realtime.Bus` (trường `Realtime realtime.Bus` của các `Writer`, `httpapi.Deps`, `background`, `files`, `selftest`,
    `tools/loadkit`, `cmd/crm`) chỉ dùng kiểu, không hiện thực. Bản hiện thực: `PGHub` (sửa), `realtimetest.RecordingBus`
    (thêm `SubscribeSystem` không nhận gì), `statusSpy` trong `env_test.go` (nhúng `realtime.Bus`, tự có). Các interface
    hẹp phía dùng (`Publish` một phương thức, danh mục dòng 465, 999) không đổi.
  - `PGHub.dispatch` thêm nhánh `System`: báo hiệu hệ thống chỉ tới người đăng ký hệ thống, không bao giờ tới kết nối SSE.
    `PGHub.listen` sau mỗi lần LISTEN được: thêm `Signal{Topic: "system.resync"}` cho người đăng ký hệ thống.
  - Mới: `cache.ReadCache`, `cache.NewReadCache`, `cache.ReadCacheOptions`, `cache.Space` (+10 hằng), `cache.Spaces`,
    `cache.Source` (+3 hằng), `cache.ErrBusy`; `realtime.TopicSystemResync`; gói mới `internal/wiring/webcache`
    (`Consumer`, `NewConsumer`, `Invalidator`, `NewInvalidator`, `TopicInvalidate`, hằng `Entity*`).
- Gói phụ thuộc ngược (go list): `internal/platform/realtime` được nhập bởi `cmd/crm`, `internal/httpapi`, module
  `cash`, `catalog`, `customer`, `fulfillment`, `inventory`, `order`, `promotion`, `platform/{background,files,selftest}`,
  `realtime/realtimetest`, `tools/loadkit`. `internal/platform/cache` được nhập bởi `platform/observability`. Gói mới
  `internal/wiring/webcache` chưa ai nhập (nối vào `cmd/crm` ở M5-07).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có (báo hiệu hệ thống không tới SSE; khung `changed`, `resync` của màn hình giữ nguyên).
- Câu sqlc, bảng, pha migration: không có câu mới; `Publish` dùng `pg_notify` sẵn có; tenant đọc bằng
  `tenancy.ListTenantIDs` sẵn có.
- Test sẽ chạy: `go test -race ./internal/platform/cache/... ./internal/platform/realtime/... ./internal/wiring/...
  ./tests/arch/...`, rồi `scripts/agent/ci-task.sh` (gói đổi + gói phụ thuộc).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `cache` (Conn.Do có
  cầu dao và hạn giờ, `ErrUnavailable`, `EntityKey`/`keyPart`), `realtime` (Bus, PGHub), `events` (Consumer, Registry),
  `tenancy.ListTenantIDs`, `metrics.Registry`; tìm "singleflight", "semaphore": không có bản dùng chung trong mã.
- Dùng lại / viết mới vì: ReadCache đi qua `Conn.Do` (cầu dao, hạn 50 ms, chỉ số valkey_ms), kiểm phần khóa bằng
  `keyPart` của `keys.go`. Gộp lời gọi cùng khóa: viết một bản nhỏ trong `readcache.go` (mutex + map lời gọi) thay vì
  nhập `golang.org/x/sync/singleflight` (ngoài ADR-44, chỉ là phụ thuộc gián tiếp). Semaphore theo space là kênh có
  đệm. Consumer dùng hằng topic, kiểu payload của `catalog`, `inventory` (A4), không viết chuỗi rời; topic chưa có hằng
  ở module (flash_sale.*, carrier.web_settings_changed, order.cancelled) là hằng riêng trong `webcache` cho tới task tạo
  chúng.
- jscpd (lần ci-task 1) báo 4 đoạn trùng mới, đều là khung nhỏ trùng hình với mã khác, không cùng mục đích để gộp:
  `counter` trong test (giống `metricRows` của fulfillment) → viết lại thành `counts` gom theo khóa; khung mở pool và
  đăng ký dò trong `webcache/env_test.go` (giống `realtime/env_test.go`) → `openDB` và dò bằng `system.resync`;
  `cache.Spaces()` (giống `identity.permissionGroups`) → mảng cố định rồi cắt. Không thêm vào mốc nền.

## Soát thiết kế

Khớp kiến trúc (mục 5):
- 5.1–5.3: không có bảng hay dữ liệu mới; consumer chỉ đọc payload của sự kiện.
- 5.4: không áp dụng (không có việc cần nguyên tử ngoài Publish trong tx của consumer).
- 5.5: báo hiệu đi bằng `NOTIFY` trong tx của consumer (chỉ tới khi commit); xóa khóa Valkey ở `api`, ngoài transaction
  (ADR-46 cấm gọi Valkey trong transaction, QĐ14).
- 5.6: không áp dụng (không gọi dịch vụ ngoài).
- 5.7: consumer chạy trong `WithTenant` của sự kiện (worker); `ListTenantIDs` đọc bảng tenant không RLS (hàm sẵn có).
- 5.8: không chạm HTTP; `ErrBusy` đổi thành 503 ở task nối handler (M5-07), không ở đây.
- 5.9: không áp dụng (không có đường ghi API).
- 5.10: Valkey chỉ là bộ đệm: Valkey lỗi, cầu dao mở hay Conn nil → đọc thẳng `load` (PostgreSQL) dưới semaphore; mất
  một lần xóa khóa chỉ để bản cũ sống tới hết TTL (ngắn, QĐ14), và Invalidator làm `resync` (BumpSpace mọi space) khi
  Valkey trở lại.
- 5.11: không thêm thư viện (tự viết gộp lời gọi, xem Tái dùng).
- 5.12: consumer theo mẫu events.Consumer; gói `internal/wiring/**` có luật kiến trúc mới.

Đúng khi chạy đồng thời (mục 6):
- 6.1: nhiều yêu cầu cùng khóa đang thiếu → một `load` (gộp lời gọi trong tiến trình). Bản cũ ghi đè sau khi xóa:
  ghi bằng script `SET … NX PX` chỉ khi `gen` chưa đổi; `Invalidate` ghi dấu "đã xóa" (tombstone, 5 s) thay vì `DEL`
  để một lần tải bắt đầu trước khi xóa không ghi lại bản cũ; đọc dấu này là thiếu.
- 6.2: không áp dụng (không có đường ghi API); consumer chạy lại được (Publish lặp chỉ thêm một lần xóa khóa thừa).
- 6.3: không khóa dòng nào.
- 6.4, 6.5: không áp dụng.
- 6.6: payload sai (thiếu `variantIds`, mã rỗng) → ghi log, không báo hiệu, trả nil (chạy lại không sửa được payload;
  bộ đệm tự lành theo TTL). Lỗi `Publish` → trả lỗi, worker chạy lại theo lịch.
- 6.7: không áp dụng.
- 6.8: `api` chết → bộ đệm tự lành theo TTL; LISTEN rớt rồi nối lại → `system.resync` → BumpSpace mọi space.
- 6.9: script Valkey qua `Conn.Do` (hạn 50 ms); `load` của lượt gộp chạy với ctx không hủy theo người gọi đầu (một
  khách đóng kết nối không làm hỏng các khách đang chờ cùng khóa), hạn của câu SQL do bên gọi và pool đặt.
- 6.10: không áp dụng (không có tiền).
- 6.11: quy luật "100 lời gọi đồng thời → 1 load" và "semaphore đầy → ErrBusy" có test đồng thời; không cần rapid.
- 6.12: khóa Valkey chỉ có tenant (uuid), space (hằng), mã đối tượng (kiểm `^[A-Za-z0-9_-]{1,100}$`); báo hiệu chỉ có
  loại và mã; log không có dữ liệu cá nhân.
- 6.13: báo hiệu hệ thống không có `Requires` và không bao giờ tới kết nối SSE (test: nhân viên có
  `realtime.subscribe` phạm vi all không nhận).

## Quyết định thay chủ shop

- (QĐ-185-1) Luật kiến trúc cho `internal/wiring/**` đặt tên **(m)**, không phải (g) như kế hoạch: chữ (g) đã là luật
  của imager (M1, Task 414). Nội dung giữ đúng kế hoạch.
- (QĐ-185-2) `NewInvalidator(bus, rc, d *db.DB, log)` thêm tham số `*db.DB`: `system.resync` phải lấy danh sách tenant
  bằng `tenancy.ListTenantIDs(ctx, d)`, chữ ký trong kế hoạch không có đường tới database. Phương án khác: truyền hàm
  liệt kê tenant (thêm một chỗ cắm chỉ để test).
- (QĐ-185-3) `Invalidate` ghi dấu "đã xóa" sống 5 s và lần ghi sau khi tải dùng `NX` + so `gen` (xem Soát 6.1), thay cho
  `DEL` thuần: chặn bản cũ được ghi lại khi một lần tải chạy song song với lần xóa. Dễ đảo ngược (một script).
- (QĐ-185-4) Payload sai ở consumer: ghi log và bỏ qua (trả nil), không cho việc thành `dead`: kế hoạch ghi "lỗi ghi
  log, không báo hiệu"; chạy lại không sửa được payload và bộ đệm tự lành theo TTL.
- (QĐ-185-5) Invalidator: hub gọi hàm nhận trong goroutine của nó nên hàm chỉ đẩy vào hàng đợi có đệm (1.024); hàng
  đầy hoặc một lần gọi Valkey lỗi → đánh dấu cần `resync` (BumpSpace mọi space, mọi tenant), thử lại mỗi 2 s tới khi
  được.
- (QĐ-185-6) Mỗi lần hub LISTEN được (cả lần đầu) đều gửi `system.resync` cho người đăng ký hệ thống, như khung
  `resync` của SSE: báo hiệu trước lần nghe đầu có thể đã mất.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| GetOrLoad hai lần cùng khóa → load 1 lần, lần hai `cache`; tenant khác là khóa khác | `TestReadCache_HitMissInvalidateBump/lần hai đọc từ bộ đệm` (`rcFromCache`) | `go test -race -count=1 -v ./internal/platform/cache/ -run ReadCache` | `--- PASS: TestReadCache_HitMissInvalidateBump (0.81s)` |
| 100 goroutine cùng khóa đang thiếu → load đúng 1 lần | `TestReadCache_SingleFlight` | như trên | `--- PASS: TestReadCache_SingleFlight (0.26s)` |
| Invalidate(p1) → lần sau load; BumpSpace(products_page) → mọi id tải lại, space khác và tenant khác giữ | `rcInvalidateOne`, `rcBumpSpace` (trong `TestReadCache_HitMissInvalidateBump`) | như trên | PASS |
| Bản tải bắt đầu trước Invalidate/BumpSpace không ghi lại bản cũ (QĐ-185-3) | `TestReadCache_StaleLoadNotWrittenBack` | như trên | `--- PASS (0.26s)` |
| TTL 100 ms → đọc sau đó gọi load | `rcTTL` | như trên | PASS |
| Giá trị 2 MiB → trả đúng, không ghi (`EXISTS` = 0) | `rcTooLarge` | như trên | PASS |
| Mã sai → ErrBadKey, không gọi load; lỗi load trả lên, không ghi | `rcBadID`, `rcLoadError` | như trên | PASS |
| Chỉ số cache_hit, cache_miss, cache_fallback, cache_busy theo space | `TestReadCache_HitMissInvalidateBump`, `TestReadCache_ValkeyDownFallsBack` (5), `TestReadCache_FallbackBusy` (3) | như trên | PASS |
| Valkey dừng: 5 lần GetOrLoad → load 5 lần, `db_fallback`; Invalidate/BumpSpace → ErrUnavailable | `TestReadCache_ValkeyDownFallsBack` | như trên | `--- PASS (0.33s)` |
| FallbackConcurrency 2: 2 lần tải giữ chỗ, 3 lần sau ErrBusy, space khác riêng, trả chỗ xong gọi tiếp được; ca kế hoạch 5 lời gọi đồng thời, load 200 ms → 2 thành công, 3 ErrBusy | `TestReadCache_FallbackBusy`, `TestReadCache_FallbackBusyConcurrent` | như trên | PASS (0.00s, 0.20s) |
| Publish System → nhân viên `realtime.subscribe` phạm vi all không nhận; `SubscribeSystem("cache.")` nhận; tiền tố khác không nhận; tx lùi → không ai nhận | `TestSystemSignals` (báo hiệu đánh dấu phát sau, thứ tự NOTIFY) | `go test -race -count=1 -v ./internal/platform/realtime/ -run System` | `--- PASS: TestSystemSignals (0.30s)` |
| Báo hiệu hệ thống có Requires/Scope, thiếu tenant, thiếu chủ đề → ErrInvalidSignal | `TestSystemSignalValidation` | như trên | `--- PASS (0.27s)` |
| `pg_terminate_backend` kết nối LISTEN → mọi người đăng ký hệ thống nhận `system.resync`; hủy đăng ký thì không nhận | `TestSystemResyncOnReconnect` | như trên | `--- PASS (1.33s)` |
| Ba `price.changed` + một `product.updated` của P → báo hiệu P, v1, v2, v3 khi commit; tx lùi → 0 | `TestConsumer_SignalsOnlyOnCommit` | `go test -race -count=1 -v ./internal/wiring/...` | `--- PASS (0.17s)` |
| `variantIds` hai phần tử → hai báo hiệu biến thể; thiếu/rỗng/mã rỗng, thiếu productId → log có mã sự kiện, không báo hiệu; flash_sale.*, carrier, order.cancelled (bỏ qua trước M5-19) | `TestConsumer_Payloads` (17 ca) | như trên | `--- PASS (0.01s)` |
| Name, Topics đúng kế hoạch, Register được; Publish lỗi → Handle trả lỗi | `TestConsumer_NameTopics`, `TestConsumer_PublishError` | như trên | PASS |
| Bảng ánh xạ product/variant/flash_sale/flash_item/carrier_settings; loại lạ bỏ qua; Stop thì hết nhận | `TestInvalidator_Mapping` | như trên | `--- PASS (0.10s)` |
| `system.resync` → BumpSpace đủ 10 space của 2 tenant thử | `TestInvalidator_SystemResync` | như trên | `--- PASS (0.11s)` |
| Valkey lỗi khi xóa → resync, thử lại tới khi được; hàng chờ đầy không chặn hub → resync (QĐ-185-5) | `TestInvalidator_FailureLeadsToResync`, `TestInvalidator_QueueOverflowResyncs` | như trên | PASS (0.10s, 0.09s) |
| Từ đầu tới cuối (PostgreSQL, Valkey thật): P trong bộ đệm; Emit price.changed; worker chạy consumer; GetOrLoad(P) gọi lại load | `TestEndToEnd_PriceChangedInvalidatesProduct` | như trên | `--- PASS (0.36s)` |
| Luật (m): testdata `internal/httpapi` nhập `internal/wiring/webcache` → báo, wiring nhập phần riêng module → báo; `cmd/crm` hợp lệ; mã thật → 0 | `TestBoundariesTestdata`, `TestBoundariesWiringRule`, `TestBoundariesRealCode` | `go test -count=1 -v ./tests/arch/ -run Boundaries` | PASS cả 6 test |
| Cổng kiểm của task | gói đổi + phụ thuộc + vùng cao | ci-task (lần 1) | test mọi gói `ok` (408 s); đỏ catalog-check (danh mục chưa stage) và jscpd 4 đoạn trùng mới → đã sửa (xem Tái dùng); chạy lại `jscpd` → `Found 0 clones`, `golangci-lint` các gói đổi → `0 issues`, `go test ./tests/arch/...` → `ok 21.446s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Luật (m) thay (g) (QĐ-185-1); `NewInvalidator` thêm `*db.DB` (QĐ-185-2).
- `sse.go` không phải sửa: báo hiệu hệ thống không bao giờ thành `Frame`, lọc ở `PGHub.dispatch`.

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 1, trên `git diff --cached` của 20 tệp).

- Phát hiện phải sửa: không có. Đủ 7 mục: Interface và test của kế hoạch có đủ; bằng chứng trỏ đúng test; không sót
  nơi gọi (`statusSpy` nhúng Bus, `RecordingBus` đã thêm `SubscribeSystem`); tái dùng `Conn.Do`, `keyPart`; không có
  test bị nới; không vừa log vừa trả lỗi, không cất ctx, goroutine có chủ, không có dữ liệu cá nhân trong log.
- Ghi chú không bắt buộc (không sửa, không ảnh hưởng tính đúng): `TestReadCache_FallbackBusyConcurrent` chỉ đòi
  `ok >= 2` (ca tất định ở `TestReadCache_FallbackBusy`); trong lượt gộp, nếu lần đọc lại Valkey lỗi thì `load` chạy
  ngoài semaphore dự phòng (một lượt mỗi khóa, chỉ lúc chuyển sang Valkey lỗi); mục Ảnh hưởng thiếu vài ký hiệu mới
  (đã bổ sung: `TopicSystemResync`, `Spaces`, `NewConsumer`, `TopicInvalidate`, `Entity*`).
