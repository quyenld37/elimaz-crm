# Task 90 — M2-33: Chung trước M3, M4 — mã hóa trường, webhook, lệnh, tuyến tenant, migration lệch thứ tự, khung thứ tự khóa

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 90, M2-33). Loại việc (quy trình mục 3): A
(vùng rủi ro cao: `webhooks`, `commands`, `tenancy`, `db`; mã hóa trường).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (tìm bằng grep, LSP không dùng trong phiên này):
  - **Mới, chưa ai dùng** (M3, M4, M5, M7 dùng sau): gói `internal/platform/fieldcrypt` (`Keyring`, `ParseKeyring`, `AAD`,
    `Seal`, `Open`, `NeedsRewrap`, `CurrentKeyID`, `ErrUnknownKey`, `ErrDecrypt`, `ErrInvalidAAD`), gói con
    `fieldcrypt/keyspec` (`Parse`, `Key`, `KeySize`, `ErrInvalid`); `webhooks.RoutedSource`, `webhooks.EnqueueInbound`,
    `webhooks.ErrInvalidEvent`, hằng `Via*`;
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
    và `body_kind text NOT NULL DEFAULT 'packet'` (CHECK `packet|event`; Q-T90-1), CHECK `event` không có `route_key`.
    Câu sqlc đổi: `InsertWebhookDelivery` (thêm `route_key`), `GetWebhookDelivery` (thêm `route_key`, `body_kind`,
    `received_at`); mới: `InsertWebhookEvent`, `FindWebhookDelivery`. Mã sinh `models.go` của mọi khối sqlc đổi theo
    (struct `PlatformWebhookDelivery` thêm hai cột).
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

- **Q-T90-1 — sự kiện không đến từ webhook lưu ở đâu.** `EnqueueInbound` (M4 hỏi bù, đọc comment, luồng live) có sự
  kiện nhưng không có bản nhận, mà spec 04 mục 11 đòi payload của việc chỉ mang mã bản ghi. Chọn: lưu sự kiện đã chuẩn
  hóa thành một dòng `platform.webhook_deliveries` với `body_kind = 'event'` (cột mới ở 0291), việc vào trỏ tới dòng đó
  như việc của webhook; cùng nội dung → một dòng (băm có tiền tố `event`), dọn sau 30 ngày như bản nhận. Khác: để nội
  dung trong payload của việc (đơn giản hơn, nhưng trái spec 04 mục 11 và đưa nội dung tin vào bảng `jobs`, màn việc lỗi
  đọc được). Đảo ngược: bỏ cột, đổi `EnqueueInbound` (chỉ gói `webhooks` biết).
- **Q-T90-2 — chống trùng bản nhận có `route_key`.** M3 giao task chung quyết. Chọn: giữ `UNIQUE (source, body_sha256)`
  của 0009 (bỏ ràng buộc là lệnh thu hẹp, bản ingest cũ còn dùng `ON CONFLICT` trên nó), nhưng với bản nhận có
  `route_key` thì `body_sha256` = sha256(`route\0` + route_key + `\0` + thân): hai tuyến cùng thân là hai bản nhận (kẻ
  gửi trước thân giống hệt tới tuyến khác không nuốt được gói thật), cùng tuyến gửi lại thì một dòng; bản nhận không
  `route_key` giữ đúng băm của thân như M0. Khác: thêm unique mới có `route_key` rồi bỏ cái cũ ở một bản sau (hai pha).
- **Q-T90-3 — `selftest.Command` có `AutoRetryNotSent = false`**, khác câu của kế hoạch ("selftest … có
  `AutoRetryNotSent = true`"). Theo Q-5 lệnh selftest tra thấy `not_sent` giờ thành `dead` thay vì `pending`. Giữ
  nguyên `false` (đúng ý của lệnh: không bao giờ tự gửi lại), không sửa selftest: bên ngoài của selftest ở máy thật là
  `Loopback` luôn báo đã có, nên `Lookup` không bao giờ trả `not_sent`; test e2e `workercrash_test` (ca `done`) không
  đổi. Khác: đổi selftest sang `true` để giữ đúng hành vi cũ ở ca không xảy ra.
- **Q-T90-4 — route `…/abandon` của M0 không gọi lại `commands.Abandon`.** Kế hoạch ghi "nếu có". Route của M0 dùng
  `queue.AbandonJob`: người bỏ việc `dead`/`in_doubt` của mọi loại việc, tenant tường minh, trả bản tóm tắt trước/sau
  cho audit; `commands.Abandon` là mã nghiệp vụ bỏ lệnh `pending`/`in_doubt` của chính nó. Chuyển route sang
  `commands.Abandon` sẽ đổi hành vi (việc `dead` không bỏ được nữa, việc `pending` bỏ được, mất bản tóm tắt). Giữ riêng.
- **Q-T90-5 — `Abandon` với lệnh đã kết thúc hay đang chạy.** Kế hoạch chỉ nói `in_doubt`/`pending` → `expired`, gọi
  lại không đổi. Chọn: `done`, `dead`, `expired` → không đổi, `nil` (M3 bỏ lệnh tạo vận đơn đã `dead` theo Q-5 không
  lỗi); `running` → `ErrCommandRunning` (có thể đang gửi, không được giả là đã bỏ); không thấy trong tenant của tx →
  `ErrCommandNotFound`. Không gọi `OnTerminal` (bên gọi đang tự đổi dữ liệu của mình). Lọc `tenant_id` theo
  `app.tenant_id` của tx vì `jobs` không RLS.
- **Q-T90-6 — `config` không nhập được `fieldcrypt`** (vòng `config → fieldcrypt → logging → config`). Chọn: gói con
  `fieldcrypt/keyspec` (chỉ thư viện chuẩn) đọc chuỗi khóa; `config` kiểm định dạng bằng nó và giữ chuỗi trong
  `Config.FieldEncKeys`; `fieldcrypt.ParseKeyring(logging.Secret)` dùng cùng `keyspec`. `cmd/crm` của M3/M4 dựng
  `Keyring` từ `Config.FieldEncKeys`. Chỉ đọc ở `api`, `worker` (hai vai trò M3, M4 cần; `ingest`, `migrate` không giữ
  khóa). Khác: chép logic đọc vào `config` (hai chỗ cùng luật).
- **Q-T90-7 — `hideRouteKey`.** otelhttp ghi `url.path` vào span; `routeKey` là bí mật của URL (ai biết thì gửi được
  tín hiệu cho tài khoản). Chọn: đường có `routeKey` đưa cho span bản sao yêu cầu có đường dẫn
  `/hooks/<nguồn>/[Redacted]` (giữ path value cho handler). Có test.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit cấu trúc (Tidy First): tách lõi `enqueueEvent` khỏi `SplitHandler`, không đổi hành vi | toàn bộ test `webhooks` (`TestSplit*`, `TestInbound*`, ingest, spool) và `selftest` không sửa dòng nào | `go test -race -count=1 ./internal/platform/webhooks/... ./internal/platform/selftest/...`; `golangci-lint run ./internal/platform/webhooks/...` | `ok webhooks 10.8s`, `ok selftest 2.0s`; `0 issues.` |
| Test đỏ trước khi viết mã (commit 2) | `fieldcrypt` (khung trả rỗng): `TestSealOpenRoundTrip`, `TestOpenTamperedFails` (panic), keyspec `TestParse`; config `TestLoadFieldEncKeys` (chưa có trường); tenancy `TestPutRoute*` (khung trả nil); commands `TestResolveNotSentNoAutoRetryIsDead` (`pending`), `TestAbandon` (`in_doubt`); webhooks `enqueue_test`, `routed_test` (chưa có `EnqueueInbound`, `RoutedSource`, `Via*`); arch `TestLockOrder*` trên ADR-46 của HEAD | `go test -count=1 …` từng gói | `--- FAIL` / `[build failed]` ở mọi gói nêu |
| fieldcrypt: `Open(Seal("EAAB-token-1"))` đúng; đổi một byte bất kỳ, bản mã ngắn → `ErrDecrypt`; AAD khác `RowID`, `TenantID`, `Table`, `Column` → `ErrDecrypt`; AAD sai mẫu → `ErrInvalidAAD` | `TestSealOpenRoundTrip`, `TestOpenTamperedFails`, `TestOpenWrongAADFails`, `TestInvalidAAD` | `go test -race -count=1 ./internal/platform/fieldcrypt/...` | `ok fieldcrypt 1.5s`, `ok keyspec 1.9s` |
| `"k2:<b>,k1:<a>"`: Seal dùng k2, bản mã k1 vẫn mở, `NeedsRewrap("k1") = true`, `("k2") = false`; kid lạ → `ErrUnknownKey` | `TestRotation` | như trên | ok |
| Khóa 31/33 byte, trùng `key_id`, rỗng, thiếu `:`, `key_id` sai mẫu, base64 hỏng → lỗi không chứa khóa; `slog.Any` → `[Redacted]`, fmt `%v %+v %#v %s` (con trỏ và giá trị) không lộ khóa | `TestParseKeyringRejects`, `TestKeyringNeverPrintsKeys` | như trên | ok |
| Thuộc tính rapid 200 lượt: `Open(Seal(x)) = x` (x, AAD ngẫu nhiên), hai lần Seal khác bản mã, đổi byte → `ErrDecrypt` | `TestPropertySealOpen` (`TestMain` đặt 200 lượt) | như trên | ok |
| config: `FIELD_ENC_KEYS` vắng → bốn vai trò khởi động; hợp lệ → giữ trong Config, LogValue không lộ; sai → `ErrInvalid` nêu tên biến, không in giá trị | `TestLoadFieldEncKeys`, `checkBadFieldKeys` | `go test -race -count=1 ./internal/platform/config/` | `ok config 1.4s` |
| PutRoute: A ghi `fb_page/123`, `service_key/ab12cd34`, `carrier_account/<32>` → có tuyến, gọi lại một dòng; B cùng khóa → `ErrRouteTaken`, tuyến giữ A | `TestPutRoute` | `go test -race -count=1 ./internal/platform/tenancy/` | `ok tenancy 10.4s` |
| Ngoài ngữ cảnh tenant, `kind` `staff_email`/`selftest`/`abc`/rỗng, khóa rỗng → lỗi, không ghi; `crm_app` `INSERT` thẳng → 42501; 4 tenant cùng lúc → đúng một thắng | `TestPutRouteRejects`, `TestPutRouteConcurrent` | như trên | ok |
| Q-5: `AutoRetryNotSent = false`, Lookup `not_sent` → `dead` `confirmed_not_sent`, `OnTerminal(dead)` một lần, không gửi, tra lại không đổi; `true` → `pending` như cũ | `TestResolveNotSentNoAutoRetryIsDead`; `TestResolveNotSentRequeues` (M0, không sửa) | `go test -race -count=1 ./internal/platform/commands/` | `ok commands 15.4s` |
| Abandon: `in_doubt`, `pending` → `expired` `abandoned: order_cancelled`, gọi lại không đổi không lỗi, worker không gửi; `running` → `ErrCommandRunning`; tenant khác, mã lạ → `ErrCommandNotFound` | `TestAbandon` | như trên | ok |
| Webhook: `POST /hooks/routed/<32>` → 200, bản nhận có `route_key`; cùng thân cùng khóa một dòng, khóa khác dòng riêng; đường không khóa `route_key` rỗng | `TestRoutedPostStoresRouteKey` | `go test -race -count=1 ./internal/platform/webhooks/...` | `ok webhooks 9.5s` |
| `routeKey` 31/33 ký tự, ký tự lạ, nguồn không `RoutedSource`, nguồn lạ → 404 với database chết (không 503, không tệp tạm, không tách) | `TestRoutedBadKeyIs404WithoutDatabase` | như trên | ok |
| Tệp tạm mang `routeKey`, nạp lại đúng `route_key`; `routeKey` sai trong tệp → `.corrupt` | `TestRoutedSpoolKeepsRouteKey`, `TestRoutedSpoolBadKeyIsCorrupt` | như trên | ok |
| Span không chứa `routeKey` (bỏ `hideRouteKey` thì test đỏ — đã thử) | `TestRoutedSpanHidesRouteKey` | như trên; thử đột biến `-run TestRoutedSpanHidesRouteKey` | đột biến: `FAIL`; bản thật: `ok` |
| Worker: bản nhận có `route_key` → `SplitRouted(raw, rk)`; không → `Split`; handler nhận `ReceivedAt` = giờ nhận, `Via = webhook` | `TestRoutedSplitUsesRouteKey` | như trên | ok |
| `EnqueueInbound` cùng sự kiện ba lần (backfill, backfill, comment_poll) → `true, false, false`, một việc; payload có `receivedAt`, `via`, không nội dung; `order_key`, `order_seq` như SplitHandler; handler nhận đúng Data, ReceivedAt, Via | `TestEnqueueInboundDedupesAndDelivers` | như trên | ok |
| Sự kiện đã vào qua webhook rồi hỏi bù → `false`; via lạ, tenant/nguồn/giờ rỗng → `ErrInvalidEvent`, không ghi | `TestEnqueueInboundAfterWebhookIsDuplicate`, `TestEnqueueInboundRejects` | như trên | ok |
| Mọi test `split_test`, `ingest_test`, `spool_test` và `selftest` của M0 vẫn đạt (chỉ mở thêm hai khóa payload, xem mục dưới) | `TestSplit*`, `TestPost*`, `TestSpool*`, gói `selftest` | như trên + `./internal/platform/selftest/...` | ok |
| Khung ADR-46: mã nhóm đúng một lần theo thứ tự 12 < 12a < 12b < 12c < 13; dòng M0–M2 còn nguyên (chỉ chèn); bảng khung M3, M4 đúng nhóm, đúng thứ tự; không bảng nào hai lần | `TestLockOrderGroupCodes`, `TestLockOrderFrame` | `go test -count=1 -run TestLockOrder ./tests/arch/` | ADR của HEAD: `FAIL` cả hai; ADR mới: `ok` |
| Migration lệch thứ tự (đã có từ Task 318) | `TestMigrateAppliesOutOfOrderFile`, `TestMigrateOutOfOrderStillRejectsChangedAppliedFile` … (`db/migrate_test.go`, không sửa) | trong cổng kiểm của task (gói `db` chạy vì migration đổi) | ok |
| Cổng của task trên bản đã gộp `main` (4cdaa7a) | build, vet, golangci-lint, sqlc vet, test mọi gói (migration đổi → toàn bộ), oasdiff, jscpd, test-guard, gitleaks | `ci-task.sh` của dự án; rồi sau khi stage: `make gen-check catalog-check` | mọi gói `ok` (kể cả `tests/e2e` 68.9s, `tests/arch`, `queue`); lần đầu `gen-check`, `catalog-check` đỏ vì mã sinh và danh mục chưa stage (hai cổng so cây với index); stage xong chạy lại hai cổng: không chênh |

## Test bị xóa hoặc tắt

- `internal/platform/webhooks/split_test.go` `checkRoutedJobs`: danh sách khóa được phép trong payload việc vào thêm
  `receivedAt`, `via` (kế hoạch M2-33 đòi "payload có receivedAt, via"; hai khóa là giờ và tên đường, không phải nội
  dung). Mọi khóa khác vẫn bị từ chối như M0.

## Khác kế hoạch

- Commit 1 (cấu trúc) chỉ tách lõi ghi việc `enqueueEvent` (tệp mới `webhooks/enqueue.go`); `EnqueueInbound` công khai và
  mọi thay đổi hành vi ở commit 2. `SplitHandler` gọi lõi chung `enqueueEvent` chứ không gọi `EnqueueInbound` (việc của
  webhook trỏ tới bản nhận gốc và vị trí; `EnqueueInbound` lưu sự kiện thành bản nhận riêng — gọi nó từ `SplitHandler`
  sẽ đổi hành vi). Hai đường cùng DedupeKey, OrderKey, OrderSeq vì cùng một hàm.
- 0291 thêm cột `body_kind` (Q-T90-1) ngoài `route_key`; `route_key` có CHECK đúng mẫu 32 ký tự.
- **Migration lệch thứ tự (A30): không làm lại.** Đối chiếu với `main`: `db.Migrate` bật `goose.WithAllowOutofOrder(true)`
  (Task 318, `internal/platform/db/migrate.go`), `migrate(ctx, url, fsys)` đã tách (test gọi `db.MigrateFS`). Các ca
  của kế hoạch đã có trong `internal/platform/db/migrate_test.go`: tệp số nhỏ đến sau được áp dụng
  (`TestMigrateAppliesOutOfOrderFile`, đúng một tệp, lần sau rỗng), sửa tệp đã áp dụng → lỗi nêu tên tệp và
  `checksum` (`TestMigrateAppliesOutOfOrderFile` phần cuối, `TestMigrateOutOfOrderStillRejectsChangedAppliedFile`), luật
  `-- contract:` (`TestMigrateOutOfOrderRejectsContractWithoutMarker`), tệp lỗi không để lại gì, hai lần chạy song song.
  Không tạo `migrate_outoforder_test.go`, không sửa `migrate.go`. 0290, 0291 của task này chạy được dù `main` đã có
  0126, 0202 gộp sau.
- Test khung thứ tự khóa đặt ở `tests/arch/lockorder_frame_test.go` (M4-03 tạo `tests/arch/lockorder_test.go`, tránh
  trùng tệp). Thêm mục (f) "Bảng M3 ngoài thứ tự nhóm" (`fulfillment.callback_task_logs`, `cash.reconcile_findings`,
  chỉ chèn) theo Global Constraints của M3.
- `fieldcrypt` có thêm `CurrentKeyID()` và `ErrInvalidAAD` (Table/Column rỗng hay có ký tự ngoài `[a-z0-9_.]`, để chuỗi
  AAD `tenant|table|column|row` không mơ hồ); `webhooks` có thêm hằng `Via*` và `ErrInvalidEvent`.

## Việc phát hiện thêm

Ghi chú không bắt buộc của reviewer (không ảnh hưởng tính đúng, không sửa trong task này):
- `FindWebhookDelivery` không lọc `body_kind = 'event'`: thân gốc không route key mà bắt đầu bằng `event\0…` có thể
  trùng `eventHash` (phải có chữ ký hợp lệ và đoán đúng từng byte; hậu quả: mất một sự kiện, `ErrPermanent`). Có thể
  thêm điều kiện `body_kind` ở task sau chạm `webhooks`.
- `resolver.go`: khóa kết quả `requeued` giờ gồm cả lệnh thành `dead` theo Q-5 (chỉ số thông tin).
- 0291 `ADD COLUMN … CHECK` quét bảng `webhook_deliveries` (30 ngày) dưới khóa ACCESS EXCLUSIVE: lưu ý lúc deploy
  (ingest có tệp tạm làm dự phòng).
- `tenancy.PutRoute` ghi `platform.tenant_routes` (nhóm 1 ADR-46, chỉ chèn): M3, M4 gọi đầu transaction hoặc ghi ngoại
  lệ "chỉ chèn" khi đối chiếu ADR-46 ở M3-04/M4-03.
- M3: đường `/hooks/{source}` không route key vẫn mở cho `RoutedSource` (đúng kế hoạch); hãng không ký thân gói thì
  `VerifySignature` không được trả true vô điều kiện.

## Reviewer

- Commit 1 (cấu trúc, tách `enqueueEvent`): **ĐẠT**, không phát hiện phải sửa. Đối chiếu từng trường: kind,
  DedupeKey, payload 5 trường, OrderSeq, OrderKey có tiền tố, đếm `Enqueued` không đổi; chỉ câu lỗi dài hơn (vẫn bọc `%w`).
- Commit 2 (hành vi, trên bản đã gộp `main` 4cdaa7a): **ĐẠT**, không phát hiện phải sửa. Đã kiểm: AAD không mơ hồ,
  lỗi không chứa khóa; `put_tenant_route` có `search_path = ''`, tên đủ schema, tenant từ `app.tenant_id`, `REVOKE` khỏi
  PUBLIC; `receiveRouted` trả 404 trước mọi bước chạm database; ADR-46 chỉ chèn; Q-T90-1…7 hợp lý (Q-T90-1 đúng spec 04
  mục 11; hai tenant cùng nội dung dùng chung bản nhận nhưng việc không bị nuốt vì unique của `jobs` có `tenant_id`).
  Năm ghi chú không bắt buộc chép ở "Việc phát hiện thêm".
