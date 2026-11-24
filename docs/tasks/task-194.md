# Task 194 — Điểm cuối tạo đơn web

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 194, M5-11; bảng M5-11 "Mã lỗi web" dòng 127; việc chuyển tiếp ở `docs/tasks/task-193.md` mục Việc phát hiện thêm). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `web.NewOrders(d, now)` → `web.NewOrders(d, w order.Writer, now)` — 1 chỗ gọi (`internal/httpapi/server.go` `prepare`). Thêm: `web.Orders.Create` (chạy `order.Writer.CreateFromWeb` trong savepoint), `web.OrderInput` (dịch thân gen → `order.WebOrderInput`, chuẩn hóa SĐT bằng `customer.NormalizePhone`), `web.QuoteJSON` (thân `quote` của lỗi `quote_changed`, dùng chung `quoteOut` của báo giá); trong `httpapi`: handler `WebCreateOrder`, `Deps.WebOrderLimiter`, interface `WebOrderLimiter`, `NewWebOrderLimiter`; trường `errorBody.Quote`, `errorBody.VariantIDs`. `cmd/crm`: `webKeys` thêm bộ giới hạn đơn theo khách (`cache.Limits.WebOrderCustomer`, `CACHE_KEY_SECRET`). Dùng (không đổi): `order.Writer.CreateFromWeb`, `order.Get`, `Order.AuditFields`, `catalog.DefaultSeller`, `identity.RecordAudit`, `db.Savepoint`, `WriteTx`, `cache.SubjectKey`, `cache.NewRateLimiter`.
- Gói phụ thuộc ngược (go list): `internal/httpapi/web` ← `internal/httpapi` ← `cmd/crm`, `tests/e2e` (test). Không module nghiệp vụ nào đổi.
- Operation OpenAPI bị chạm, oasdiff: thêm `webCreateOrder` `POST /api/v1/web/orders` (`x-auth: service_key`, `x-permission: web.orders.create`, `x-idempotency` required — mặc định của route ghi), schema mới `WebOrderRequest`, `WebOrderCreated`; `Error` thêm thuộc tính tùy chọn `quote`, `variantIds`; `x-error-codes` thêm `quote_changed`, `item_unavailable`, `coupon_invalid`, `too_fast`. Chỉ thêm → oasdiff không báo phá vỡ. `make gen` sinh lại `internal/httpapi/gen`, `routes_gen.go`, `console/src/api`.
- Chỗ dùng trên màn hình: không màn hình nào gọi route này; `console/src/app/errors.ts` thêm câu tiếng Việt cho 4 mã mới (test `errors.test.ts` đòi đủ mã).
- Câu sqlc, bảng, pha migration: không (dùng `CreateFromWeb` của Task 193, migration 0503 đã có).
- Test sẽ chạy: test mới `TestWebCreateOrder_*` (`internal/httpapi/web/orders_test.go`; khung `env` của gói thêm `serve(mod)` để dựng lại máy chủ với `Deps` riêng — `catalog_test.go`) và `TestWebOrderErrors` (`internal/httpapi/errors_web_test.go`) bằng `-run`; `Deps.WebOrderLimiter` nil → `prepare` dựng bộ đếm trong tiến trình (`server.go`); `TestDomainErrorSnapshot*`, `TestErrorCodes_*` của `httpapi`; `TestWebQuote*` (đổi `NewOrders`); `tools/routegen` `TestRealContractWritesRequired`; console `errors.test.ts`; cả gói `httpapi`, `httpapi/web`, `cmd/crm` qua `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`db.Savepoint`, `cache.SubjectKey`, `cache.RateLimiter`, `idempotency`); `WriteTx` (lưu kết quả cuối cùng tx), `refusableWrite` (từ chối không lưu — không hợp vì kế hoạch đòi lưu 422/409), `createOrder` (mẫu audit `order.create`), `stockShortage`/`detailedError` (thân lỗi chi tiết), `webKeyLimiter` (mẫu nối `cache.RateLimiter`), `quoteOut` (báo giá ra JSON), `shipAddressIn` (dịch địa chỉ), khung test `env` của `httpapi/web` (`newEnv`, `serviceKey`, `product`, `stock`, `campaign`, `post`).
- Dùng lại / viết mới vì: thân `out_of_stock` dùng đúng `stockShortage` (mã và dạng của M1); thân `quote` dùng `quoteOut` của báo giá (cùng hình dạng `WebQuote`); bộ giới hạn theo SĐT dùng `cache.RateLimiter` + `SubjectKey` (khóa không mang SĐT, QĐ22). Viết mới: handler (lưu cả từ chối dứt khoát nên không dùng được `refusableWrite`), `webOrderRefusal` (một hàm ở `errors_web.go` quyết định thân + mã cho cả lần lưu và lần ghi lỗi), `WebOrderLimiter` (interface khác `WebRateLimiter` vì chủ thể là SĐT, cần secret HMAC).

## Soát thiết kế

5.1 Không dữ liệu mới; đơn của `order`, audit của `identity`. 5.2 Ghi qua public API: `order.Writer.CreateFromWeb`, `identity.RecordAudit`. 5.3 Đọc đơn để audit qua `order.Get`. 5.4 Một transaction (`WriteTx`): tạo đơn + audit + kết quả khóa chống trùng; từ chối dứt khoát lùi savepoint (không còn khách mới, giữ hàng, lượt mã) rồi lưu câu trả lời cùng tx. 5.5 Không việc sau commit mới (sự kiện `order.created` của `CreateFromWeb`, outbox). 5.6 Không gọi ra ngoài. 5.7 `WriteTx` = `tenancy.WithTenant` của tenant khóa dịch vụ. 5.8 Handler mỏng; `order` không biết HTTP; mã HTTP chỉ ở `errors*.go`. 5.9 Route ghi `required` có `Idempotency-Key`, `x-permission: web.orders.create`, `x-auth: service_key`, path `/api/v1/web/…` (luật Task 477). 5.10 Giới hạn theo SĐT trên Valkey chỉ điều tiết (Valkey ngừng → bộ đếm trong tiến trình `FallbackMax`); giá, tồn, mã đọc PostgreSQL. 5.11 Không thư viện mới. 5.12 Theo mẫu `handlers_jobs.go`/`createOrder`.
6.1 Tranh chiếc cuối và mã còn một lượt do `CreateFromWeb` (UPDATE có điều kiện, khóa dòng mã); test HTTP 20 lượt và mã 1 lượt. 6.2 `Idempotency-Key` (lưu 201, 422, 409 `out_of_stock` cùng tx; 503 không lưu) + khóa nghiệp vụ `(seller, web, external_ref)` (hai khóa khác nhau cùng `externalRef` → cùng đơn). 6.3 Thứ tự khóa như `Create` (ADR-46); audit chỉ chèn. 6.4 55P03/40P01: `RetryTx` chạy lại cả `fn` (savepoint lùi trước), hết lượt → 503 `retry_later` + `Retry-After`, khóa trả lại; `fn` gán lại mọi biến kết quả mỗi lần. 6.5 READ COMMITTED; lệch giá giữa hai câu đọc do `CreateFromWeb` xử lý. 6.6–6.8 không áp dụng: không việc nền, không gọi ngoài. 6.9 `db.TxAPI` (lock_timeout; test hạ 50 ms). 6.10 Tiền `int64`, hợp đồng `integer/int64` tối đa 2^53−1. 6.11 Quy luật tiền/tồn đã có rapid ở Task 193; ở lớp HTTP test lặp 20 lần tranh chiếc cuối. 6.12 SĐT chỉ dạng `logging.Phone`; khóa giới hạn là `SubjectKey("phone", …)` (HMAC, không SĐT); audit `order.create` dùng `AuditFields` (đã che); thân lỗi chỉ mã, số tiền, mã biến thể. 6.13 Mặc định từ chối: khóa thiếu `web.orders.create` → 403 (guard).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| [WB07] Máy chủ HTTP thật, 50 yêu cầu song song cùng khóa cùng thân → đúng 1 đơn; mỗi câu trả lời 201 cùng thân hoặc 409 `request_in_progress`; đơn `new`, tổng = báo giá; 1 dòng audit `order.create` actor `service` có `grandTotal`, không có SĐT, tên, địa chỉ | `TestWebCreateOrder_FiftyParallelSameKey` | `go test -race -count=3 -run '^(TestWebCreateOrder_.*\|TestWebQuote.*)$' ./internal/httpapi/web/` (trước khi có mã: mọi ca 501 `not_implemented`) | `ok internal/httpapi/web 13.617s`; sau khi sửa câu kiểm audit: `-run '^TestWebCreateOrder_FiftyParallelSameKey$'` `ok 2.643s` |
| Hai khóa khác nhau cùng `externalRef` → 1 đơn, cả hai 201 cùng `orderId`; [WB07] máy chủ commit rồi đóng kết nối không trả lời → gửi lại cùng khóa → 201 `Idempotent-Replayed: true`, 1 đơn | `TestWebCreateOrder_SameExternalRefAndLostReply` | như trên | ok |
| [WB08] Tồn 1, hai khách cùng lúc, 20 lần → đúng một 201 và một 409 `out_of_stock` có `variantId`, `available: 0`; gửi lại khóa bên thua → cùng byte, `Idempotent-Replayed`; bên thua 0 đơn; tồn `on_hand 1, held 1` | `TestWebCreateOrder_LastUnitRace` | như trên | ok |
| [WB11] Mã còn 1 lượt, hai đơn sync cùng lúc → một 201 (tổng 330.000 có mã), một 422 `coupon_invalid` `reason: exhausted`; `used_count = 1` | `TestWebCreateOrder_LastCouponUse` | như trên | ok |
| [WB13], [WB10] Giá sửa trong thân → 422 `quote_changed` kèm báo giá CRM (350.000, tổng 380.000, `changed`), 0 đơn; gửi lại cùng khóa → cùng byte, đã lưu; queued cùng giá cũ → 201 `priceReview`, tổng CRM; [WB09] biến thể ngừng bán → 422 `item_unavailable` kèm `variantIds` | `TestWebCreateOrder_QuoteChangedAndUnavailable` | như trên | ok |
| [WB06], [WB01] Chiến dịch 20 % → 201 tổng 310.000; dòng đơn có `web_campaign_id` = chiến dịch, kênh `web` | `TestWebCreateOrder_Campaign` | như trên | ok |
| Transaction khác giữ khóa dòng `stock_balances`, `WriteLockTimeout` 50 ms → 503 `retry_later` có `Retry-After`, 0 dòng `idempotency_keys`, 0 đơn; nhả khóa, gửi lại cùng khóa → 201, 1 đơn | `TestWebCreateOrder_LockTimeout` | `go test -race -count=2 -run '^TestWebCreateOrder_LockTimeout$' ./internal/httpapi/web/` | `ok 6.589s` |
| Khóa `web.catalog.read` → 403; thân 1 MiB + 1 byte → 413 `body_too_large`; SĐT sai → 422 `invalid_phone`, không lưu; ngưỡng 3 đơn/phút một SĐT (đồng hồ cố định) → đơn thứ 4 429 `too_fast` có `Retry-After`, không lưu, 0 đơn; SĐT khác → 201 | `TestWebCreateOrder_GuardsAndTooFast` | như dòng đầu | ok |
| Thân lỗi chi tiết (cả khi bọc): `quote_changed` + `quote`, `item_unavailable` + `variantIds` đúng thứ tự, `coupon_invalid` + `reason`; `webOrderRefusal` cho cùng thân + `out_of_stock` (409, variantId, available), `mixed_sellers`; lỗi khác không lưu; `too_fast` `Retry-After` làm tròn lên | `TestWebOrderErrors` | `go test -count=1 -run '^(TestWebOrderErrors\|TestDomainErrorSnapshot.*\|TestErrorCodes.*)$' ./internal/httpapi/` | `ok 1.147s` |
| Bảng chụp ánh xạ lỗi thêm 4 dòng (3 kiểu lỗi của `order`, `tooFastError`); mọi mã có trong `x-error-codes` | `TestDomainErrorSnapshot`, `TestErrorCodes_DeclaredInContract` | như trên | ok |
| Route ghi website `required`, `service_key`, path `/api/v1/web/…` (luật Task 477) | `tools/routegen` `TestRealContractWritesRequired` | `go test -count=1 ./tools/routegen/` | `ok 0.706s` |
| Màn hình có câu tiếng Việt cho 4 mã mới | `console/src/app/errors.test.ts` | `npx vitest run src/app/errors.test.ts` | `17 passed` |
| Cổng kiểm của task (build, vet, lint, gen-check, catalog, jscpd, gitleaks, oasdiff, test-guard, console-check, test -race `cmd/crm`, `httpapi`, `httpapi/web`, `tests/arch`; test thường `platform/realtime`, `tests/e2e`) | — | `ci-task.sh` (sau `git merge main`: "Already up to date", main = ced61a7) | Lần 1: bị dừng ở giới hạn 10 phút của lệnh nền khi đang chạy test; trước đó `gen-check` đỏ (mã sinh chưa stage, cổng so với index) và `jscpd` 1 bản trùng 6 dòng (mở transaction giữ khóa giống `cash/schema_m3_test.go`) → đổi sang `holdStockLock` dùng `staff.TryExec`. Lần 2 (đã stage): `CI-TASK XANH` (httpapi 55,9 s, web 14,1 s, tests/arch 108,8 s, e2e 211,9 s) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Tệp mã: không có thư mục `internal/httpapi/web/orders.go` riêng cho handler — handler ở `handlers_web_orders.go` (cạnh `WebQuote`, gói `httpapi` có `WriteTx`), phần dịch thân và savepoint ở `internal/httpapi/web/orders.go`; mã lỗi mới ở `errors_web.go` (bảng M5-11 dòng 127, luật Task 441) chứ không ở `errors.go` (errors.go chỉ thêm hai trường thân lỗi và hai lời gọi).
- SĐT người đặt sai → 422 `invalid_phone` (mã có sẵn của M2, `customer.ErrInvalidPhone`), không phải 400 như Interfaces (Quyết định 2).
- Không viết `tests/e2e/web_orders_test.go` (Quyết định 5).
- Ca "giữ khóa 2 s": test giữ khóa dòng tồn tới khi nhận 503 rồi nhả (không ngủ theo giờ thật, luật Task 400).

## Quyết định thay chủ shop

1. **Từ chối dứt khoát được lưu nhờ savepoint:** `web.Orders.Create` chạy `CreateFromWeb` trong `db.Savepoint`; khi bị từ chối (422 `quote_changed`/`item_unavailable`/`coupon_invalid`/`mixed_sellers`, 409 `out_of_stock`) savepoint lùi mọi thứ lần tạo đã ghi (khách mới, giữ hàng, lượt mã) rồi câu trả lời được lưu vào khóa chống trùng trong cùng tx. Phương án khác: lùi cả tx (như `createOrder` nhân viên) — trái kế hoạch ("từ chối dứt khoát, được lưu").
2. **SĐT sai trả 422 `invalid_phone`** (mã M2 đã có, màn hình đã có câu) thay vì 400: không thêm cách thứ hai báo cùng lỗi. Không lưu theo khóa. Đảo lại: đổi một dòng ở `errors_customer.go` (ảnh hưởng cả màn nhân viên) hoặc bọc lỗi riêng.
3. **Lỗi đầu vào khác không lưu** (`isOrderRefusal`: `ErrInvalidInput`, địa chỉ thiếu, mã hành chính lạ…), như `createOrder` nhân viên: website sửa thân rồi gửi lại cùng khóa thì máy chủ xét lại (khóa được trả lại, `releaseClaim`).
4. **429 `too_fast` là mã mới** (đúng Interfaces), khác `rate_limited` của giới hạn theo khóa dịch vụ: website phân biệt "khách đặt quá nhanh" với "khóa quá tải". Giới hạn đếm trong handler, sau middleware chống trùng: gửi lại đơn đã có (phát lại) không tốn lượt; lượt bị từ chối không lưu. Khóa đếm là `SubjectKey(CACHE_KEY_SECRET, "phone", SĐT)`; không có secret (dev, không Valkey) → secret ngẫu nhiên của tiến trình.
5. **Không thêm test e2e riêng:** `orders_test.go` đã chạy máy chủ HTTP thật (`httptest.NewServer(httpapi.NewServer(…))`), bộ kiểm khóa dịch vụ thật và PostgreSQL thật; khung `tests/e2e` (`newStack`) chỉ có xác thực nhân viên — thêm khóa dịch vụ vào đó là việc khung test riêng (ghi ở Việc phát hiện thêm). Đảo được: thêm tệp sau mà không đổi mã.
6. **Thân từ chối đã lưu giữ `request_id` của lần đầu** (phát lại giống từng byte, Task 396), như kết quả 201.
7. **Đơn đã có của cùng `externalRef` (khóa khác) trả 201** cùng `orderId` (Interfaces), không ghi audit lần hai.

## Việc phát hiện thêm

- Khung `tests/e2e` chưa có khóa dịch vụ: khi cần e2e cho route website (đơn web, flash sale M5-35) nên thêm `stack.serviceKey(perm)` và `postParallel` có header `Authorization`.
- Hợp đồng `webCreateOrder` (theo reviewer): website dùng Idempotency-Key = `externalRef`, từ chối được lưu theo khóa, nên sau `quote_changed`/`out_of_stock`/`coupon_invalid` muốn đặt lại phải dùng `externalRef` mới (gửi cùng khóa với thân khác → 422 `idempotency_key_reused`). Nên thêm một câu vào mô tả operation khi làm phía website (M5-29).
- `web.orders.create` dùng chung cho báo giá và tạo đơn; giới hạn theo SĐT chỉ áp cho tạo đơn (báo giá không ghi).

## Reviewer

Vòng 1 (agent `reviewer`, trên phần đã stage sau ci-task lần 2 xanh): ĐẠT. Phát hiện phải sửa: không có. Xác nhận: 201 và từ chối dứt khoát lưu cùng tx sau khi savepoint lùi; lỗi đầu vào, SĐT sai, `too_fast`, 503 không lưu (khóa trả lại); khóa giới hạn là HMAC của SĐT; audit không có dữ liệu cá nhân; mọi chỗ gọi `NewOrders` đã đổi. Ghi chú không bắt buộc (chưa sửa, ghi ở Việc phát hiện thêm và dưới đây): (1) mô tả `webCreateOrder` nên nói rõ sau từ chối phải đặt lại bằng `externalRef` mới (cùng khóa khác thân → 422 `idempotency_key_reused`); (2) mã web không thêm hàm vào `domainErrorMappers` mà gọi qua `detailedError`/`writeDomainError` vì có trường chi tiết và header `Retry-After`; (3) thân `out_of_stock` của đơn web dựng ở `webOrderRefusal` (cùng hình dạng với `stockShortage`, test so), không gọi thẳng `stockShortage`.

Kết luận: ĐẠT
