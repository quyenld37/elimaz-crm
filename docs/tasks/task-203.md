# Task 203 — Điểm cuối mua cho website: chuỗi chắn bốn lớp (M5-20)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 203, M5-20). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi: không đổi chữ ký nào đã có. Thêm mới: `httpapi.WriteTxWith` (biến thể của `WriteTx` nhận `db.TxOptions`, mở rộng M0 thứ năm theo dòng M0 của bảng Interfaces); `WriteTx` (13 chỗ gọi ngoài test, grep `WriteTx(`) giữ nguyên chữ ký và hành vi, thân chuyển sang gọi `WriteTxWith(…, writeTxOptions(d), fn)`. Thêm `httpapi.NewFlashAdmission`, `httpapi.FlashAdmissionOptions`; `web.Flash`, `web.NewFlash`, `web.FlashBuyInput`. Đọc (không sửa) `order.Writer.BuyFlash`, `order.ErrFlashRetry`, `promotion.GetFlashItemForBuy`, `customer.Writer.FindOrCreateForOrder`, `cache.AdmissionGate`, `cache.RateLimiter`, `cache.Limits.FlashCustomer/FlashIP`.
- Gói phụ thuộc ngược (grep import `internal/httpapi`): `cmd/crm`, `internal/httpapi/web` (test), `internal/platform/realtime` (test), `tests/arch`, `tests/drills`, `tests/e2e`. `internal/httpapi/web` được nhập bởi `internal/httpapi`.
- Operation OpenAPI bị chạm, oasdiff: thêm operation mới `webFlashBuy` `POST /api/v1/web/flash-sales/{saleId}/items/{itemId}/buy` (x-auth service_key, x-permission web.flash.buy, x-admission flash, Idempotency-Key bắt buộc, header X-Attempt), schema `WebFlashBuyRequest`, `WebFlashBuyResult`; thêm mã lỗi `phone_required` vào `x-error-codes`. Chỉ thêm, oasdiff không có thay đổi phá vỡ.
- Chỗ dùng trên màn hình: không có màn mới; `console/src/api/**` sinh lại (schema mới), `console/src/app/errors.ts` thêm câu tiếng Việt cho `phone_required` (test errors.test.ts đòi mỗi mã có câu).
- Câu sqlc, bảng, pha migration: không có câu SQL, bảng hay migration mới. Đọc/ghi qua public API của order (BuyFlash), customer (FindOrCreateForOrder), promotion (GetFlashItemForBuy); bảng chạm gián tiếp: platform.idempotency_keys, promotion.flash_sale_claims, inventory.flash_sale_units, stock_reservations, ord.orders/order_items, customer.customers/identities.
- `httpapi.Deps` thêm trường `FlashGate cache.AdmissionGate` (dựng cờ hết suất sau sold_out) và trường riêng `webFlash`; `errors_web.go` thêm `codePhoneRequired`, `errPhoneRequired` (+ dòng snapshot); `admission.go` có `retryAfterWriter` (thêm Retry-After: 1 cho 503 của route flash). `docs/danh-muc-dung-chung.md` sinh lại (trường Config mới).
- Cấu hình: `internal/platform/config/config.go` thêm `FLASH_ADMIT_PER_SECOND`, `FLASH_ADMIT_GLOBAL_PER_SECOND`, `FLASH_MAX_INFLIGHT` (tùy chọn, 0 → mặc định QĐ23 của cache.GateOptions). `cmd/crm/servicekey.go`/`main.go` dựng `cache.NewAdmissionGate` trên kết nối Valkey gate đã có và nối `Deps.Admission["flash"]`.
- Test sẽ chạy: `internal/httpapi/web` (`TestWebFlashBuy_*` mới), `internal/httpapi` (`TestFlashAdmission_*`, `TestWriteTxWith_*` mới; test cũ của WriteTx, webroute, errors_snapshot), `internal/platform/config`, `cmd/crm` (build), `console` errors.test.ts; cả bộ vùng rủi ro cao chạm gián tiếp do ci-task chọn. E2E `tests/e2e/flash_http_test.go` viết sẵn, không chạy.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `internal/platform/cache` (AdmissionGate, RateLimiter, SubjectKey, Limits), `httpapi` (WriteTx, writeError, retryAfterSeconds, tooFastError/writeTooFast, CouponGuard.subjects cho IP /64), `customer.NormalizePhone`, `order.BuyFlash`.
- Dùng lại: `cache.AdmissionGate` (Admit/SoldOut/SetState) và `cache.RateLimiter.Allow` với `cache.Limits.FlashCustomer`/`FlashIP` (đọc đè RATE_LIMITS_JSON), `cache.SubjectKey` (HMAC CACHE_KEY_SECRET) cho SĐT và IP; `retryAfterSeconds`, `writeError`; `customer.NormalizePhone`; `customer.Writer.FindOrCreateForOrder`; `order.Writer.BuyFlash` (lõi M5-18); `WriteTx` (thêm biến thể nhận TxOptions thay vì chép thân); chuẩn hóa IPv6 /64 giống CouponGuard (một dòng `netip.PrefixFrom(...).Masked()`; không tách hàm chung vì CouponGuard còn nhánh khóa dịch vụ khác luật).
- `internal/modules/promotion/flashtest.Schedule` (mới, chỉ cho test): dựng đợt đã lên lịch một món; trước đó đoạn này nằm chép trong `order/flashbuy_test.go` — không đổi test của order (test module tự dựng qua API để thấy từng bước).
- Viết mới vì: middleware cổng nhận vào cho flash chưa có (chỗ cắm `Deps.Admission` đã có từ Task 187); handler + `web.Flash` (gộp tìm-tạo khách và lượt mua) chưa có.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

**Bước nào chạm database (thứ tự của một lượt mua):**
1. serviceKeyAuth (bộ kiểm khóa có đệm), guard, serviceRateLimit (Valkey) — có sẵn.
2. `flashAdmission` (mới): đọc thân ≤ 2 KiB (quá → 413 của bodyLimit, route khai x-body-limit 2048), kiểm SĐT → 400 `phone_required`, IP sai/thiếu → 400 `bad_request`, X-Attempt ngoài 1–10 → 400 `bad_request`; giới hạn SĐT (10/10 s) rồi IP (30/10 s) trên Valkey → 429 `too_fast` + Retry-After; X-Attempt ≤ 1 và cờ hết suất biết chắc → 200 `{result:"sold_out"}`; `Admit` không được → 503 `retry_later` + Retry-After. **Không chạm PostgreSQL.**
3. idempotent: `idempotency.Acquire` (database) — chỉ yêu cầu đã qua cổng mới giữ khóa.
4. Handler: (a) transaction `TxAPI` riêng: `customer.FindOrCreateForOrder` (tên cố định "Khách website", SĐT là danh tính, nguồn web) — commit, khóa dòng khách nhả ngay; (b) `WriteTxWith(TxFlashBuy)`: kiểm món thuộc đợt (`promotion.GetFlashItemForBuy`) → `order.BuyFlash` → lưu kết quả cùng transaction.
5. Sau WriteTx: `sold_out` → `AdmissionGate.SetState(item, 0, true)` (Valkey, ngoài transaction, lỗi bỏ qua).

**Câu trả lời nào được lưu theo khóa:** 200 `bought` (code, externalRef, expiresAt), `sold_out`, `over_limit`, `not_open` — kết quả cuối, lưu trong cùng transaction. **Không lưu:** 400 (phone_required, bad_request), 429 too_fast, 503 retry_later (cổng, `ErrFlashRetry`, `db.ErrRetryLater`), 200 sold_out của cờ (trước khóa), 404 not_found (món không có, món không thuộc đợt), 400 invalid_input (order.ErrInvalidInput), 422 variant_not_for_sale — đều trả lại khóa để gửi lại cùng khóa chạy từ đầu. Ngoại lệ: mã ngoài đã dùng cho đơn khác (`order.ErrDuplicateOrder`, 422) đi theo luật chung của WriteTx: không lưu, dòng khóa giữ running tới 60 s (gửi lại trong lúc đó nhận 409) — không sai dữ liệu.

**5. Khớp kiến trúc**
- 5.1 Chủ dữ liệu: không bảng mới; đơn nháp thuộc order, claim thuộc promotion, đơn vị suất thuộc inventory, khách thuộc customer — mỗi phần do module đó ghi (BuyFlash điều phối).
- 5.2 Không ghi bảng module khác: lớp HTTP chỉ gọi public API (`customer.Writer`, `order.Writer`, `promotion.GetFlashItemForBuy`).
- 5.3 Đọc qua view: không đọc bảng trực tiếp ở lớp HTTP.
- 5.4 Nguyên tử: lượt mua (claim + đơn vị + đơn nháp + giữ hàng) + lưu kết quả khóa chống trùng trong một transaction (WriteTxWith). Tìm-tạo khách ở transaction riêng trước đó theo Interfaces (giữ transaction mua ngắn, không giữ khóa dòng khách); khách tạo mà lượt mua sau đó lỗi thì vẫn là hồ sơ hợp lệ của SĐT đó, lần sau tìm lại đúng hồ sơ (không trùng).
- 5.5 Việc không nguyên tử sau commit: `SetState` lên Valkey sau commit; sự kiện order.created do BuyFlash ghi trong tx (outbox).
- 5.6 Gọi ra ngoài: không áp dụng (Valkey là hạ tầng nội bộ, không phải bên ngoài có in_doubt; không gọi trong RetryTx).
- 5.7 Ngữ cảnh shop: mọi truy vấn trong `tenancy.WithTenant` của tenant khóa dịch vụ; khóa Valkey gắn tenant (`EntityKey`/`SubjectKey`) — SubjectKey của SĐT/IP không có tenant: giới hạn theo SĐT tính chung mọi shop trên cùng Valkey (chấp nhận: một website một shop hiện nay; ghi ở Quyết định).
- 5.8 Nghiệp vụ không biết HTTP: middleware và handler ở `internal/httpapi`, nghiệp vụ ở module.
- 5.9 Mọi đường ghi có khóa chống trùng và khai quyền: `Idempotency-Key` bắt buộc, `x-permission: web.flash.buy`.
- 5.10 PostgreSQL là nguồn sự thật: cờ hết suất chỉ trả lời "sold_out" sớm; "known=false" luôn xuống database; không bán vượt chốt bằng SKIP LOCKED trong PG. Bỏ Valkey (dừng, FLUSHALL) vẫn đúng — có test.
- 5.11 Thư viện mới: không.
- 5.12 Theo module mẫu: handler theo mẫu WebCreateOrder (WriteTx, từ chối không lưu trả lại khóa), lỗi đổi ở errors_web.go.

**6. Đúng khi đồng thời**
- 6.1 Hai yêu cầu cùng khóa: idempotency (ADR-41) — một chạy, một 409 request_in_progress hoặc phát lại.
- 6.2 Hai khóa cùng externalRef: BuyFlash bước 2/5 trả cùng đơn (Task 201).
- 6.3 Tranh suất: SKIP LOCKED; thiếu do bị khóa → `ErrFlashRetry` → 503 không lưu, website gửi lại.
- 6.4 Lỗi tạm 40P01/55P03: RetryTx của WriteTxWith với TxFlashBuy (lock_timeout 200 ms) chạy lại ≤ 3 lần, hết → 503 retry_later, trả lại khóa.
- 6.5 Mức cách ly: READ COMMITTED + khóa dòng của BuyFlash (không đổi).
- 6.6 Việc nền: không áp dụng (không có job mới).
- 6.7 Bên ngoài 429: không áp dụng.
- 6.8 Tiến trình chết: transaction tự lùi; khóa chống trùng running hết hạn 60 s; vé cổng là bộ đếm trong tiến trình (chết thì mất cùng tiến trình), bộ đếm Valkey theo giây tự hết hạn.
- 6.9 Treo lâu: TxFlashBuy statement_timeout 2 s, lock_timeout 200 ms; mỗi lệnh Valkey có OpTimeout của Conn.
- 6.10 Tiền: giá flash lấy từ món (int64), không nhận giá từ website.
- 6.11 Quy luật: không bán vượt, ≤ giới hạn mỗi khách — test 200 người mua/30 suất, 100 yêu cầu một SĐT, Valkey dừng/FLUSHALL; rapid đã có ở BuyFlash (Task 201).
- 6.12 Dữ liệu cá nhân: SĐT, IP chỉ đi vào Valkey dưới dạng HMAC (SubjectKey); không log SĐT/IP; span của cổng chỉ có thuộc tính kết quả (test quét exporter trong bộ nhớ không thấy SĐT, IP).
- 6.13 Quyền: route service_key, `web.flash.buy`, mặc định từ chối (guard có sẵn).
- Thứ tự khóa (ADR-46): không thêm khóa mới; FindOrCreateForOrder khóa dòng khách ở transaction riêng đã commit trước khi BuyFlash khóa claim → không có chu trình.
- RetryTx chạy lại: fn gán lại `out`, `refused` mỗi lần.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| [FS03], [FS02] 30 suất, 200 khách HTTP đồng thời, website tự gửi lại cùng khóa khi 503 → đúng 30 bought, 170 sold_out, bất biến M5-18 | `web.TestWebFlashBuy_ThirtySlotsTwoHundredBuyers` | `go test -race -count=1 -v -run '^(TestWebFlashBuy_ThirtySlotsTwoHundredBuyers\|…)$' ./internal/httpapi/web/` | `--- PASS: TestWebFlashBuy_ThirtySlotsTwoHundredBuyers (2.68s)` |
| [FS07] Sau hết suất, X-Attempt 1 → sold_out, 0 câu SQL, không dòng khóa; khóa đã bought + X-Attempt 2 khi cờ bật → bought đã lưu, Idempotent-Replayed | `web.TestWebFlashBuy_SoldOutFlagSkipsDatabase`; `httpapi.TestFlashAdmission_SoldOutShortCircuit` | `go test -race -count=1 -run '^(TestWebFlashBuy_PhoneRequired\|TestWebFlashBuy_SoldOutFlagSkipsDatabase)$' ./internal/httpapi/web/`; `go test -race -count=1 -run '^(TestFlashAdmission_.*\|TestWriteTxWith_UsesTxOptions)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi/web 2.846s`; `ok elimaz.com.vn/crm/internal/httpapi 2.571s` |
| [FS05] Một SĐT 100 lượt song song, giới hạn 1 → đúng 1 bought, còn lại 429/over_limit, ≥ 50 lượt 429 | `web.TestWebFlashBuy_OnePhoneHundredParallel`; `httpapi.TestFlashAdmission_RateLimits` | như dòng 1; như dòng 2 | `--- PASS: TestWebFlashBuy_OnePhoneHundredParallel (1.48s)` |
| [FS04] PerSecond 5, 50 lượt trong một giây → ≤ 5 dòng idempotency_keys mỗi giây, còn lại 503 có Retry-After; gửi lại cùng khóa → mỗi khóa đúng một đơn | `web.TestWebFlashBuy_AdmitPerSecond`; `httpapi.TestFlashAdmission_GateBusyAndRetryAfter` | như dòng 1 | `--- PASS: TestWebFlashBuy_AdmitPerSecond (2.02s)` |
| [FS18] Thiếu/sai SĐT → 400 phone_required, không giữ khóa | `web.TestWebFlashBuy_PhoneRequired`; `httpapi.TestFlashAdmission_BadInput` | như dòng 2 | `ok … web 2.846s`; `ok … httpapi 2.571s` |
| [FS10] Dừng Valkey gate giữa 200 khách → 30 bought, không 5xx ngoài 503, gate_degraded > 0 | `web.TestWebFlashBuy_ValkeyStopped` | `go test -race -count=1 -v -run '^TestWebFlashBuy_ValkeyStopped$' ./internal/httpapi/web/` | `--- PASS: TestWebFlashBuy_ValkeyStopped (9.95s)` |
| [HT27] FLUSHALL → không bán vượt; lượt kế xuống database (SQL > 0), sold_out, cờ dựng lại | `web.TestWebFlashBuy_FlushAll` | như dòng 1 | `--- PASS: TestWebFlashBuy_FlushAll (1.66s)` |
| Dấu vết có flash.admission, câu SQL khóa chống trùng, flash.buy; không SĐT/IP | `web.TestWebFlashBuy_TraceHasNoPhoneOrIP`; `httpapi.TestFlashAdmission_SpanHasNoPersonalData` | như dòng 1, 2 | `--- PASS: TestWebFlashBuy_TraceHasNoPhoneOrIP (0.79s)` |
| Vé cổng được trả sau handler | `httpapi.TestFlashAdmission_ReleasesTicket` | như dòng 2 | `ok … httpapi 2.571s` |
| WriteTxWith dùng TxFlashBuy (lock_timeout 200ms), WriteTx giữ TxAPI, kết quả lưu và phát lại | `httpapi.TestWriteTxWith_UsesTxOptions` | như dòng 2 | `ok … httpapi 2.571s` |
| Cấu hình FLASH_* mặc định 100/300/64, đè, sai → không khởi động | `config.TestLoadFlashAdmission` | `go test -count=1 -run '^TestLoadFlashAdmission$' ./internal/platform/config/` | `ok elimaz.com.vn/crm/internal/platform/config 0.434s` |
| Mã phone_required có câu tiếng Việt | `console/src/app/errors.test.ts` | `npx vitest run src/app/errors.test.ts` | `Tests 17 passed (17)` |
| Cổng kiểm của task (cổng tĩnh: build, vet, lint, gen-check, catalog-check, jscpd, gitleaks, oasdiff, test-guard, console-check, test kiến trúc; test -race gói đổi + vùng rủi ro cao) | `scripts/agent/ci-task.sh` (bể mac, chỗ 1/1) | `scripts/agent/ci-task.sh > …/ci-203.log 2>&1` | `STATIC XANH`; `ok …/internal/httpapi 43.550s`; `ok …/internal/httpapi/web 29.200s`; `ok …/internal/modules/order 112.460s`; `ok …/internal/modules/promotion 60.944s`; `ok …/internal/platform/config 1.474s`; `ok …/cmd/crm 6.664s`; `CI-TASK XANH (so với điểm tách nhánh 213a2943…)` |
| Trước khi có mã: các test trên không biên dịch (undefined: NewFlashAdmission, WriteTxWith, FlashGate, codePhoneRequired) | — | `go vet` / LSP | FAIL như mong |

## E2E viết sẵn, chưa chạy

- `tests/e2e/flash_http_test.go`: `TestFlashBuyHTTP_ValkeyDownAndTrace` (200 khách / 30 suất qua HTTP thật, Valkey gate dừng giữa chừng, dấu vết không SĐT/IP). `go vet ./tests/e2e/` sạch.

## Quyết định thay chủ shop

- Q1 Tên khách mới từ flash sale: "Khách website" (đúng dòng B1 của bảng đối chiếu kế hoạch; `FullName` rỗng bị customer từ chối). Khác: sửa customer cho phép tên rỗng — chạm vùng M2, CHECK database, không cần.
- Q2 Mở rộng WriteTx: thêm `WriteTxWith(ctx, d, tenant, opts, fn)`, `WriteTx` gọi nó với tùy chọn cũ. Khác: tham số biến thiên trên WriteTx (đổi 13 chỗ gọi hoặc thêm `...Option`) — rộng hơn, khó đảo ngược hơn.
- Q3 Handler ở tệp riêng `internal/httpapi/handlers_web_flash.go` (phương thức của `*handler` phải ở gói httpapi); phần ghép khách + lượt mua ở `web/flash.go` như kế hoạch.
- Q4 `clientIp` bắt buộc trong thân (website luôn biết IP khách); thiếu/sai → 400 bad_request trước mọi bước. Khác: thiếu IP thì đếm theo khóa dịch vụ (như CouponGuard) — sẽ khóa cả website ở 30 lượt/10 s, sai cho flash sale.
- Q5 `X-Attempt` ngoài 1–10 hay không phải số → 400 bad_request (không đoán).
- Q6 503 của `ErrFlashRetry` cần `Retry-After`: middleware cổng thêm `Retry-After: 1` cho mọi 503 của route này nếu handler chưa đặt (không sửa bảng lỗi chung).
- Q7 `saleId` trên đường dẫn phải là đợt của món; khác → 404 not_found, không lưu. Đọc thêm một dòng theo khóa chính trong transaction mua.
- Q8 Không ghi audit cho đơn nháp flash ở bước mua (kế hoạch không yêu cầu; giữ transaction mua ngắn); audit tạo đơn thuộc bước hoàn tất (M5-21). Ghi vào Việc phát hiện thêm để Task 204 xét.
- Q9 Giới hạn theo SĐT/IP dùng SubjectKey không gắn tenant (giống WebOrderLimiter, CouponGuard đã có): một SĐT bị đếm chung mọi shop trên cùng Valkey; hiện một shop.
- Q10 Cấu hình `FLASH_ADMIT_PER_SECOND`, `FLASH_ADMIT_GLOBAL_PER_SECOND`, `FLASH_MAX_INFLIGHT` tùy chọn (trống → 100/300/64 của QĐ23 trong `cache.GateOptions`); số âm hay không phải số → không khởi động.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Handler ở `internal/httpapi/handlers_web_flash.go` (ngoài danh sách Files; phương thức của `*handler` phải ở gói httpapi). `cmd/crm/servicekey.go` dựng cổng (cùng chỗ dựng các bộ giới hạn khác trên Valkey gate), `main.go` chỉ nối Deps.
- Middleware không "đặt vé vào ctx": handler không cần vé; vé được trả bằng `defer Release` ngay trong middleware sau khi handler xong (cùng tác dụng).
- Thêm `Deps.FlashGate` để handler dựng cờ sau sold_out (kế hoạch nêu việc dựng cờ, không nêu chỗ cắm).
- Thêm `x-body-limit: 2048` cho route (kế hoạch: đọc thân ≤ 2 KiB).
- Lần ci-task đầu đỏ ở cổng tĩnh (lint 9 lỗi nhỏ trong test, jscpd 4 đoạn trùng; gen-check/catalog-check đỏ vì chưa stage). Sửa: thêm gói dựng dữ liệu thử `internal/modules/promotion/flashtest` (dùng chung cho test web và e2e, thay đoạn chép từ `order/flashbuy_test.go`), hàm `nowUTC` chung cho `web.Orders`/`web.Flash` (quote.go), tách hàm trong test; lần hai XANH.
- Ca [FS10] và [HT27] chạy ở test gói `internal/httpapi/web` (Valkey container riêng của test, `testkit.StartValkey`), không dừng container `valkey-gate` của compose; bản e2e viết sẵn ở `tests/e2e`.

## Việc phát hiện thêm

- Comment trong `internal/httpapi/handlers_web_flash.go` (WebFlashBuy) ghi "đầu vào … không hợp lệ (422)": mã thật là 400 invalid_input cho order.ErrInvalidInput; sửa chữ ở task chạm tệp sau. Cân nhắc đưa `order.ErrDuplicateOrder` vào flashRefusal để trả lại khóa ngay.
- Audit cho đơn nháp flash: lượt mua không ghi audit `order.create` (Q8); Task 204 (hoàn tất đơn nháp) nên xét ghi audit khi đơn thành `new`.

## Reviewer

Kết luận: ĐẠT (agent reviewer, lần 1, trên phần đã stage sau khi gộp main 213a2943 và ci-task XANH).

- Phát hiện phải sửa: không có. Reviewer xác nhận: cổng đứng trước khóa chống trùng và không chạm PostgreSQL; bought/sold_out/over_limit/not_open lưu cùng transaction, flashRefusal lùi transaction (cả Claim/Unclaim) và trả lại khóa; `fn` gán lại kết quả mỗi lần, không gọi Valkey trong RetryTx; SĐT/IP chỉ vào Valkey dạng HMAC, span chỉ mang kết quả, otelpgx mặc định không ghi tham số SQL; Retry-After đủ cho 429, 503 của cổng và 503 của ErrFlashRetry.
- Ghi chú không bắt buộc (không sửa mã trong task này): (1) comment `handlers_web_flash.go` ghi lỗi đầu vào là 422, bảng lỗi thật trả 400 invalid_input — đã sửa câu chữ trong tệp task; (2) `order.ErrDuplicateOrder` không nằm trong flashRefusal → dòng khóa giữ running 60 s (luật chung WriteTx), đã ghi ở Soát thiết kế; (3) yêu cầu thiếu Idempotency-Key khi cờ hết suất bật vẫn nhận 200 sold_out (cổng đứng trước khóa, kế hoạch cho phép); (4) gom `nowUTC` (cấu trúc) chung commit với hành vi — chỉ là quy trình.
