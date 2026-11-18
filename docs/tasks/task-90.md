# Task 90 — M2-33: Chung trước M3, M4 — mã hóa trường, webhook, lệnh, tuyến tenant, migration lệch thứ tự, khung thứ tự khóa

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 90, M2-33). Loại việc (quy trình mục 3): A
(vùng rủi ro cao: `webhooks`, `commands`, `tenancy`, `db`; mã hóa trường).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (tìm bằng grep, LSP không dùng trong phiên này):
  - **Mới, chưa ai dùng** (M3, M4, M5, M7 dùng sau): gói `internal/platform/fieldcrypt` (`Keyring`, `ParseKeyring`, `AAD`,
    `Seal`, `Open`, `NeedsRewrap`, `CurrentKeyID`, `ErrUnknownKey`, `ErrDecrypt`, `ErrInvalidAAD`), gói con
    `fieldcrypt/keyspec` (`Parse`, `Key`, `ErrInvalid`); `webhooks.RoutedSource`, `webhooks.EnqueueInbound`, hằng `Via*`;
    `commands.Abandon`, `commands.ErrCommandNotFound`, `commands.ErrCommandRunning`; `tenancy.PutRoute`,
    `tenancy.ErrRouteTaken`; `config.Config.FieldEncKeys`.
  - **Đổi kiểu có sẵn (chỉ thêm trường):** `webhooks.Inbound` thêm `ReceivedAt`, `Via` (chỗ dựng giá trị: chỉ
    `inbound.go` trong gói; chỗ đọc: `selftest/inbound.go`, `fulfillment` handler vào — chỉ đọc trường cũ, không vỡ);
    `webhooks.Entry` thêm `RouteKey` (dựng ở `ingest.go`, test `webhooks`; JSON tệp tạm thêm `routeKey` tùy chọn — tệp
    tạm cũ vẫn đọc được).
  - **Đổi hành vi:** `commands.ResolveInDoubt` (Q-5): handler `AutoRetryNotSent() == false` mà `Lookup` trả `not_sent`
    → `dead` `confirmed_not_sent` + `OnTerminal` (trước: `pending`). Nơi gọi: `commands/resolver.go`,
    `tests/e2e/workercrash_test.go` (chỉ ca `done`). Handler có `Looker` hiện có: `selftest.Command`
    (`AutoRetryNotSent = false`, xem Quyết định Q-T90-3), lệnh của M1 (`fulfillment` create/refresh, `files`, `catalog`,
    `inventory`, `alertjobs`: đều `true`, không đổi). `SplitHandler` gọi lõi ghi việc chung với `EnqueueInbound` (commit
    cấu trúc riêng, không đổi hành vi); `Ingest.Register` thêm `POST /hooks/{source}/{routeKey}`.
- Gói phụ thuộc ngược (go list): `webhooks` ← `cmd/crm`, `adapters/fakecarrier`, `modules/fulfillment`,
  `platform/selftest`; `commands` ← `cmd/crm`, `modules/{catalog,fulfillment,inventory}`, `platform/{files,selftest,
  observability/alertjobs}`; `tenancy` ← 13 gói (chỉ thêm tệp `put_route.go`, không sửa `routes.go`); `config` ←
  `cmd/crm`, `httpapi`, `platform/{logging,metrics,observability,telemetry}`, `testkit` (chỉ thêm trường). `config`
  không nhập được `fieldcrypt` (vòng `config → fieldcrypt → logging → config`): config kiểm định dạng bằng
  `fieldcrypt/keyspec` (không nhập `logging`).
- Operation OpenAPI bị chạm, oasdiff: không (ingest không có hợp đồng openapi; đường `/hooks/*` ở `webhooks`).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: pha mở rộng.
  - `0290_platform_tenant_route_fn.sql`: hàm `platform.put_tenant_route` (`SECURITY DEFINER`, `search_path = ''`),
    `REVOKE … FROM PUBLIC`, `GRANT EXECUTE … TO crm_app`. Câu sqlc mới `PutTenantRoute` (`queries/platform/tenant_routes.sql`).
  - `0291_webhook_route_key.sql`: `platform.webhook_deliveries` thêm `route_key text` (rỗng được, CHECK 32 ký tự chữ số)
    và `via text NOT NULL DEFAULT 'webhook'` (CHECK 6 giá trị; xem Khác kế hoạch). Câu sqlc đổi:
    `InsertWebhookDelivery` (thêm `route_key`), `GetWebhookDelivery` (thêm `route_key`, `via`, `received_at`); mới:
    `InsertWebhookEvent`, `FindWebhookDelivery`.
  - Câu sqlc mới của `commands`: `ResolveInDoubtDead`, `AbandonCommand`, `CommandStatus` (`queries/platform/commands.sql`).
- Test sẽ chạy: `go test -race` các gói `internal/platform/{fieldcrypt/...,webhooks,commands,tenancy,config,db,selftest}`,
  `tests/arch` (khung thứ tự khóa, luật biên), `tests/e2e` (đường ingest, worker crash); vùng rủi ro cao chạm
  `webhooks`, `commands`, `tenancy`, `db` → cả bộ vùng qua `scripts/agent/ci-task.sh` (migration đổi → toàn bộ).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (tìm "crypt", "seal",
  "AES", "mã hóa", "key", "secretbox") — không có gói mã hóa nào; `logging.Secret` là kiểu che bí mật khi log (dùng lại
  làm tham số `ParseKeyring`). Tìm "route": `tenancy.AddRoute` (chủ bảng, dev seed) và `platform.add_staff_route` (chỉ
  `staff_email`) — khác quyền và khác loại khóa; hàm SQL mới theo đúng mẫu `add_staff_route`. Tìm "abandon":
  `queue.AbandonJob` (người bỏ việc `dead`/`in_doubt` qua route `…/abandon`, có tenant tường minh, trả bản tóm tắt) —
  khác việc với `commands.Abandon` (mã nghiệp vụ bỏ lệnh `pending`/`in_doubt` của chính nó); không gộp (Q-T90-4).
- Dùng lại / viết mới vì: dùng lại `queue.JobQueue.Enqueue`, `tenancy.ResolveTx`, `eventDedupeKey`, `loadPacket`,
  `sourceMap`, `apply`/`runTx` của `commands`, `pgconn.PgError` cho 23505. Viết mới `fieldcrypt` (thư viện chuẩn
  `crypto/aes`, `crypto/cipher`, `crypto/rand`; không thư viện ngoài). Migration lệch thứ tự: **không viết lại** —
  `db.Migrate` đã bật `goose.WithAllowOutofOrder(true)` từ Task 318, `migrate(ctx, url, fsys)` (xuất cho test là
  `db.MigrateFS`) đã tách, các ca của kế hoạch đã có trong `migrate_test.go` (đối chiếu ở mục Khác kế hoạch).

## Soát thiết kế

- 5.1 chủ dữ liệu: `fieldcrypt` chỉ là phép tính, không bảng; `webhook_deliveries`, `jobs`, `tenant_routes` là bảng nền
  tảng do `internal/platform/{webhooks,queue/commands,tenancy}` sở hữu.
- 5.2, 5.3: module không ghi bảng nền tảng: tuyến ghi qua hàm SQL `put_tenant_route` (crm_app không có `INSERT`), việc
  vào qua `webhooks.EnqueueInbound`, bỏ lệnh qua `commands.Abandon`.
- 5.4: `EnqueueInbound`, `PutRoute`, `Abandon` nhận `tx` của bên gọi để nguyên tử với dữ liệu nghiệp vụ.
- 5.5, 5.6: không gọi mạng; Q-5 làm đúng 04 mục 5.3 (lệnh không tự gửi lại thì "chắc chưa gửi" không về hàng chờ).
- 5.7: `put_tenant_route` lấy tenant từ `app.tenant_id` (thiếu → lỗi), không nhận tham số tenant; `Abandon` lọc
  `tenant_id` theo ngữ cảnh của tx (bảng `jobs` không RLS); `EnqueueInbound` nhận tenant từ bên gọi (như `SplitHandler`).
- 5.8: `ingest` là mép HTTP của `webhooks` (đã có), không module nghiệp vụ nào nhập `net/http`.
- 5.9: không route ghi `/api/v1` mới. 5.10: không Valkey. 5.11: chỉ thư viện chuẩn và rapid (ADR-44).
- 5.12: theo mẫu `add_staff_route` (hàm SQL), mẫu `commands` (câu có điều kiện, hook trước câu đổi `jobs`).
- 6.1: `put_tenant_route` dùng `INSERT … ON CONFLICT DO NOTHING` rồi đọc chủ khóa (hai tenant cùng lúc: một thắng, một
  `23505`); `Abandon` là `UPDATE` có điều kiện trạng thái (worker vừa nhận → 0 dòng → `ErrCommandRunning`); kết quả
  tra in_doubt chỉ áp cho đúng lần in_doubt đã đọc (`finished_at`), như M0.
- 6.2: chống trùng việc vào bằng `jobs.dedupe_key` (khóa sự kiện, như webhook); bản nhận sự kiện lưu theo băm thân (cùng
  sự kiện hỏi bù nhiều lần → một dòng). Chống trùng bản nhận có `route_key` tính băm trên `route_key` + thân để hai tuyến
  không nuốt gói của nhau (Q-T90-2).
- 6.3: ADR-46: `webhook_deliveries` chỉ chèn (nhóm ngoài thứ tự (a)), `jobs` cuối (nhóm 13); `Abandon` sửa `jobs` —
  bên gọi gọi nó cuối transaction (ghi trong doc). Khung dòng M3, M4 chỉ chèn.
- 6.4, 6.5, 6.9: dùng `db.RetryTx`/`tenancy.WithTenant` có sẵn; READ COMMITTED (đọc chủ khóa sau `ON CONFLICT` thấy dòng
  đã commit).
- 6.6–6.8: Q-5 thêm nhánh `dead` (lệnh không tự gửi lại); in_doubt giữ như M0.
- 6.10: không áp dụng: không tiền.
- 6.11: rapid: `Open(Seal(x)) = x` với `x`, `AAD` ngẫu nhiên; hai lần `Seal` khác bản mã; đổi một byte bất kỳ → `ErrDecrypt`.
- 6.12: khóa không bao giờ vào lỗi, log (`Keyring.LogValue` → `[Redacted]`, lỗi chỉ nêu vị trí mục); `routeKey` (bí mật
  của đường dẫn) không vào log, chỉ số, tên tệp tạm, dấu vết (đường dẫn trong span bị thay bằng `[Redacted]`); payload
  việc vào chỉ mang mã bản nhận (spec 04 mục 11), kể cả sự kiện không đến từ webhook.
- 6.13: `put_tenant_route` chỉ `crm_app` gọi được (`REVOKE … FROM PUBLIC`); `kind` giới hạn ba loại.

## Quyết định thay chủ shop

(điền khi làm)

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit cấu trúc (Tidy First): tách lõi `enqueueEvent` khỏi `SplitHandler`, không đổi hành vi | toàn bộ test `webhooks` (`TestSplit*`, `TestInbound*`, ingest, spool) và `selftest` không sửa dòng nào | `go test -race -count=1 ./internal/platform/webhooks/... ./internal/platform/selftest/...`; `golangci-lint run ./internal/platform/webhooks/...` | `ok webhooks 10.8s`, `ok selftest 2.0s`; `0 issues.` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Commit 1 (cấu trúc) chỉ tách lõi ghi việc `enqueueEvent` (tệp mới `webhooks/enqueue.go`); `EnqueueInbound` công khai và
  mọi thay đổi hành vi ở commit 2.

## Việc phát hiện thêm

(không có)

## Reviewer

- Commit 1 (cấu trúc, tách `enqueueEvent`): **ĐẠT**, không phát hiện phải sửa. Đối chiếu từng trường: kind,
  DedupeKey, payload 5 trường, OrderSeq, OrderKey có tiền tố, đếm `Enqueued` không đổi; chỉ câu lỗi dài hơn (vẫn bọc `%w`).
