# Task 15 — Tách gói, định tuyến tenant, sự kiện vào

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi ký hiệu cũ. Mới trong `webhooks`:
  `SplitHandler`, `NewSplitHandler`, `SplitOptions`, `InboundKind`, `Inbound`, `InboundHandler`, `InboundJobHandlers`.
  Mới trong `tenancy`: `ResolveTx` (cùng câu sqlc với `Resolve`, chạy trên `tx` của bên gọi). `tenancy.Resolve` giữ
  nguyên chữ ký; đang dùng ở `internal/modules/identity/users.go:41` (hành vi giữ nguyên). `webhooks.SplitKind`, `Source`, `SplitResult`,
  `InboundEvent` (Task 14) không đổi.
- Gói phụ thuộc ngược (go list): `webhooks` ← `cmd/crm`, `selftest` (và test của chúng); `tenancy` ← `cmd/crm`,
  `httpapi`, `modules/identity`, `platform/{commands,idempotency,queue,schedules,webhooks}`, `testkit`.
- Operation OpenAPI bị chạm, oasdiff: không có (worker không phục vụ HTTP).
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: thêm câu `GetWebhookDelivery :one` (đọc `source, body` theo `id`) vào
  `queries/platform/webhooks.sql`; `make gen`. Không migration mới (crm_app đã có `SELECT` trên
  `platform.webhook_deliveries`, `SELECT/INSERT` trên `platform.jobs`, `SELECT` trên `platform.tenant_routes`).
- Test sẽ chạy: `go test -race ./internal/platform/webhooks/... ./internal/platform/selftest/...`; vùng rủi ro cao
  `tenancy` bị chạm (thêm `ResolveTx`) nên chạy cả bộ `./internal/platform/tenancy/... ./internal/platform/queue/...`;
  `./cmd/crm/... ./tests/e2e/...` (main.go đổi); `./internal/modules/identity/... ./internal/httpapi/...` (dùng `tenancy.Resolve`); `make ci`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `tenancy`
  (`Resolve`, `WithTenant`), `queue` (`JobQueue`, `NewQueue`, `Handler`, `ErrPermanent`, `DefaultPolicy`),
  `events` (`ConsumerKind`, `Registry.JobHandlers`: cùng dạng "một interface nghiệp vụ → queue.Handler"),
  `metrics.Registry.Inc`, `webhooks.Save`/`SplitKind`/`Source`. Tìm `Resolve`, `tenant_routes`, `inbound` trong mã.
- Dùng lại / viết mới vì: dùng lại `queue.NewQueue().Enqueue` (chống trùng tầng 1, thứ tự), `queue.DefaultPolicy`,
  `ErrPermanent`, `Source.Split` của Task 14, câu sqlc `ResolveTenantRoute` (qua `tenancy`), `metrics.Registry`.
  Viết mới `tenancy.ResolveTx` vì `Resolve` lấy thêm một kết nối từ pool trong lúc handler đang giữ transaction
  (Concurrency 8 việc tách mỗi việc giữ 2 kết nối → nguy cơ cạn pool, ADR-42); `Resolve` gọi chung một hàm riêng với
  `ResolveTx`, không chép câu. `InboundJobHandlers` giống hình `events.Registry.JobHandlers` nhưng giữ riêng: khác
  mục đích (sự kiện vào từ bên ngoài, không chặn khi dead) và khác lý do thay đổi (theo adapter kênh), đúng luật
  "logic nghiệp vụ ở lại trong module".

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: `webhook_deliveries`, `jobs`, `tenant_routes` là bảng nền tảng của `platform`; chỉ mã trong
  `internal/platform/webhooks` và `tenancy` đụng tới.
- 5.2, 5.3: không ghi, không đọc bảng của module nghiệp vụ; handler nghiệp vụ nhận `Inbound` và `tx` có tenant.
- 5.4 Nguyên tử: mỗi gói một transaction (đọc bản nhận, tìm tenant, ghi mọi việc `inbound.*`); mỗi sự kiện một
  transaction riêng ở việc `inbound.*` (handler + `done` cùng transaction, lõi hàng đợi lo).
- 5.5: không áp dụng: không phát sự kiện nội bộ.
- 5.6: không áp dụng: không gọi ra ngoài; handler sự kiện vào không gọi mạng (ghi trong doc `InboundHandler`).
- 5.7 Ngữ cảnh shop: việc tách không tenant (bảng nền tảng không RLS, spec 04 mục 11); việc `inbound.*` mang
  `TenantID` đã tìm qua `tenant_routes`, worker chạy handler trong `tenancy.WithTenant`.
- 5.8: không nhập `net/http` mới (gói đã có ingest; phần tách không dùng HTTP).
- 5.9: không áp dụng: không có đường ghi HTTP.
- 5.10: không áp dụng: không Valkey.
- 5.11: không thêm thư viện.
- 5.12: theo mẫu `events.Registry.JobHandlers` (Task 13) và đường vào Task 14.

Mục 6:
- 6.1 Hai worker cùng tách một gói: việc tách được nhận bằng `SKIP LOCKED`; chạy lại (giữ chỗ hết hạn, `RetryJob`)
  thì `Enqueue` trùng `(tenant, kind, dedupe_key)` không ghi thêm → không nhân đôi.
- 6.2 Gửi lại: hai gói khác thân cùng `eventId` → `DedupeKey` giống nhau → một việc (test).
- 6.3 Thứ tự khóa: transaction tách chỉ đọc `webhook_deliveries`, `tenant_routes` và chèn `jobs` cuối cùng, đúng
  ADR-46 mục (a).
- 6.4, 6.5: `RetryTx` của worker lo lỗi tạm; READ COMMITTED mặc định.
- 6.6 Việc nền lỗi: thân không đọc được, nguồn lạ, bản nhận không còn → `ErrPermanent` (dead ngay); lỗi database →
  thử lại theo lịch. Sự kiện vào dùng `DefaultPolicy` (không `BlockOnDead`).
- 6.7: không áp dụng: không gọi bên ngoài.
- 6.8 Chết giữa chừng: transaction tách lùi, việc về hàng chờ, chạy lại không tạo trùng.
- 6.9 Treo: handler giới hạn 30 s của `DefaultPolicy`; gói tối đa 1 MiB.
- 6.10: không áp dụng: không có tiền.
- 6.11: không áp dụng: không thuộc vùng có quy luật rapid bắt buộc; quy luật "chạy lại không thêm việc" có test.
- 6.12 Dữ liệu cá nhân: payload việc `inbound.*` chỉ mang mã (`deliveryId`, vị trí trong gói, `dedupeKey`, `type`,
  `occurredAt`), **không** mang nội dung sự kiện; handler đọc lại nội dung từ bản nhận (xem Quyết định thay chủ shop).
  Nhãn chỉ số chỉ có tên nguồn và loại sự kiện. Lỗi không kèm nội dung thân.
- 6.13: không áp dụng: không có quyền nhân viên.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Gói 5 sự kiện → 3 việc `inbound.selftest` đúng tenant (2 A, 1 B), `inbound_skipped{selftest:mystery}` = 1, `inbound_unrouted{selftest}` = 1, việc tách `done`; handler chạy với `app.tenant_id` của sự kiện; payload chỉ có mã | `TestSplitRoutesEvents` | `go test -race -count=1 -v -run 'TestSplit\|TestInbound' ./internal/platform/webhooks/` | `--- PASS: TestSplitRoutesEvents (1.17s)` |
| Chạy lại việc tách → vẫn 3 việc | `TestSplitRoutesEvents` (đoạn cuối) | như trên | PASS |
| Hai gói khác thân cùng `eventId` X → 1 việc cho X | `TestSplitDedupesAcrossPackets` | như trên | `--- PASS (1.17s)` |
| Gói 50 sự kiện, lỗi ở sự kiện thứ 7 → 49 `done`, 1 `pending` `attempts = 1` | `TestSplitOneFailureDoesNotBlockPacket` | như trên | `--- PASS (1.27s)` |
| Hai sự kiện cùng `OrderKey` cách 1 ms ghi ngược → `Handle` theo thời gian (Concurrency 4) | `TestSplitOrdersByOccurredAt` | như trên | `--- PASS (1.18s)` |
| Thân không phải JSON → việc tách `dead` (1 lần chạy), 0 việc inbound | `TestSplitUnreadableBodyIsDead` | như trên | `--- PASS (1.15s)` |
| Nguồn lạ → `dead`; `InboundKind`; tùy chọn sai → `ErrInvalidOptions` | `TestSplitUnknownSourceIsDead`, `TestInboundKindAndOptions` | như trên | PASS |
| `tenancy.ResolveTx` cùng kết quả với `Resolve` | `TestResolveAndListTenantIDs` | `go test -race -count=1 ./internal/platform/tenancy/...` | `ok … tenancy 16.842s` |
| Gói phụ thuộc và vùng rủi ro cao | cả bộ | `go test -race -count=1 ./internal/platform/{webhooks,selftest,tenancy,queue}/... ./cmd/crm/... ./tests/e2e/...` | tất cả `ok` (queue 36.9s, e2e 25.0s) |
| `identity` (dùng `Resolve`, vùng rủi ro cao) | cả bộ | `go test -race -count=1 ./internal/modules/identity/... ./internal/httpapi/...` | `ok … identity 7.885s`, `ok … httpapi 14.786s` |
| Toàn bộ cổng | `make ci` (sau khi stage) | `make ci` | `exit=0`, gitleaks "no leaks found" |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Payload việc `inbound.*` là `{type, deliveryId, index, dedupeKey, occurredAt}`, **không** có `data` (kế hoạch ghi
  `{type, deliveryId, occurredAt, data}`); `Inbound.Data` vẫn có, handler đọc lại từ bản nhận. Lý do ở mục dưới.
- `InboundJobHandlers(sources []Source, hs []InboundHandler) ([]queue.Handler, error)`: thêm `sources` (để đọc lại
  sự kiện) và lỗi (handler không có nguồn hoặc trùng). `SplitHandler` dựng bằng `NewSplitHandler(SplitOptions{Sources,
  Metrics})`.
- Tìm tenant bằng `tenancy.ResolveTx` (mới) trên tx của việc tách thay cho `tenancy.Resolve` (lấy thêm kết nối pool).
- `OrderKey` của việc là `"inbound.<nguồn>:" + InboundEvent.OrderKey`.

## Quyết định thay chủ shop

1. **Không để nội dung sự kiện trong payload việc.** Chọn: payload chỉ mang mã, handler đọc lại bản nhận và tách lại
   lấy sự kiện thứ `index` (kiểm `dedupeKey` khớp). Vì sao: spec 04 mục 11 "payload của việc chỉ mang mã bản ghi,
   không mang nội dung" và doc `queue.NewJob.Payload`; tin nhắn Meta có dữ liệu cá nhân, chép vào `jobs.payload` là
   thêm một chỗ chứa. Giá: mỗi việc đọc lại một bản nhận (≤ 1 MiB) và tách lại. Phương án khác: theo đúng kế hoạch,
   để `data` trong payload (đổi lại dễ: chỉ `inboundPayload` và `inboundJobHandler.Handle`).
2. **Sự kiện chưa định tuyến: bỏ qua và đếm** (như kế hoạch). Spec 04 mục 11 nói "giữ lại ở trạng thái chưa định
   tuyến": bản nhận nguyên văn còn trong `webhook_deliveries` 30 ngày, đó là phần "giữ lại". Chưa có đường giao lại:
   `RetryJob` đòi tenant và trạng thái `dead`/`in_doubt`/`expired`, việc tách không tenant và đã `done` (ghi ở Việc
   phát hiện thêm). Phương án khác: bảng/việc "chưa định tuyến" riêng (cần migration, để khi có màn vận hành).
3. **Tiền tố `OrderKey`** `inbound.<nguồn>:` vì `order_key` của `platform.jobs` dùng chung mọi loại việc trong một
   tenant (câu `ClaimJobs` không lọc theo `kind`); không có tiền tố thì việc `event.*` và `inbound.*` trùng khóa có
   thể chặn nhau. Phương án khác: để nguyên khóa của nguồn.
4. **Test 50 sự kiện dùng 50 `tenantKey` (50 hội thoại) cùng tenant A.** Nguồn selftest đặt `OrderKey = tenantKey`;
   nếu 50 sự kiện cùng khóa thì việc thứ 7 đang `pending` chờ thử lại chặn các việc sau theo lõi hàng đợi Task 11
   (xem Việc phát hiện thêm). Đúng ý spec 04 mục 5.2 "một sự kiện lỗi không làm 49 sự kiện hợp lệ cùng gói bị treo".
5. **Worker dựng nguồn selftest không có bí mật**: worker chỉ gọi `Split`, không kiểm chữ ký; cấu hình worker không
   đọc `SELFTEST_WEBHOOK_SECRET`.

## Việc phát hiện thêm

- Spec 04 mục 5.1: "một tin nhắn lỗi **không** chặn các tin sau của cùng hội thoại", nhưng `ClaimJobs` (Task 11)
  cho việc `pending` đang chờ thử lại chặn việc sau cùng `(tenant, order_key)` tới khi nó `done` hoặc `dead` (tới
  khoảng 7 giờ với lịch 5 s → 6 giờ). Với `BlockOnDead = false` chỉ việc `dead` thôi chặn. Cần quyết định khi làm
  kênh thật (M1): ví dụ sự kiện vào chỉ chặn khi việc trước đang `running`, hoặc tối đa N lần thử. Không sửa ở đây
  (vùng rủi ro cao `queue`, ngoài phạm vi).
- Chưa có công cụ vận hành chạy lại một việc tách đã `done` (không tenant) để giao sự kiện chưa định tuyến sau khi
  thêm định tuyến; cần khi có kênh thật (thêm Page mới mà định tuyến tới muộn).
- Tới Task 18, worker thật chưa có `InboundHandler` cho `selftest`: việc `inbound.selftest` sẽ `dead` ("no handler").
  Chỉ ảnh hưởng môi trường dev.
- Chỉ số `inbound_skipped`, `inbound_unrouted` đếm theo lần chạy việc tách (chạy lại, hoặc transaction lùi rồi chạy
  lại, đếm thêm); đủ cho cảnh báo, không dùng làm số đúng tuyệt đối.

## Reviewer

Kết luận: ĐẠT

- Lượt 1: ĐẠT, không phát hiện về tính đúng. Ghi chú: (1) lệch spec 04 mục 5.1 do `ClaimJobs` cho việc pending chặn
  việc sau cùng `OrderKey` (đã ở Việc phát hiện thêm); (2) chú thích `metricUnrouted` và Quyết định 2 nói sai rằng
  chạy lại được việc tách đã `done` → đã sửa câu chữ, thêm việc "công cụ chạy lại việc tách"; (3) Ảnh hưởng ghi sai
  chỗ dùng `tenancy.Resolve` (có `identity/users.go:41`) → đã sửa, chạy thêm test `identity`, `httpapi`;
  (4) tách lại gói cho mỗi việc tốn n lần tách gói ≤ 1 MiB: chấp nhận, đo khi có nguồn gói lớn.
- Lượt 2 (sau sửa, toàn bộ phần stage): ĐẠT. Ghi chú: đổi cách tách của một nguồn giữa hai bản phát hành làm việc
  pending cũ dead (an toàn, không xử lý nhầm); kênh thật nên giới hạn số nhãn `inbound_skipped`.
