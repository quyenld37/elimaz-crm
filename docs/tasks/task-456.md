# Task 456 — order: tạm dừng nhận đơn web

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 456, M5-44; làn L1 sau M5-11).
Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký có sẵn (0 chỗ gọi phải sửa).
  - `order`: kiểu `WebIntake`, `WebIntakeChange`, `WebIntakeChanged` (payload); hằng `TopicWebIntakeChanged`
    (`order.web_intake.changed`), `MaxWebIntakeReasonRunes`; hàm `GetWebIntake`; phương thức `Writer.SetWebIntake`;
    sentinel `ErrReasonRequired`, `ErrWebIntakePaused`. `Writer.CreateFromWeb` giữ chữ ký, thêm một bước đọc
    `ord.web_intake` (không khóa) sau bước tìm đơn cùng `ExternalRef`.
  - `identity`: thêm quyền `order.web_intake.manage` (ScopeShop) vào `Permissions` và nhãn trong `roles.go`; owner,
    manager tự nhận qua `defaultRoles()` (không thuộc `notManager`).
  - `cache`: thêm vùng `SpaceWebIntake` vào `Spaces()` (11 → 12; `invalidator_test` đếm số vùng qua `len(Spaces())`
    nếu có số cứng thì sửa số, điều kiểm giữ nguyên).
  - `webcache`: thêm `EntityWebIntake`, chủ đề `order.TopicWebIntakeChanged` trong `Topics()`, ánh xạ → làm mới cả
    vùng `SpaceWebIntake`.
  - `web` (lớp HTTP website): thêm `(*Catalog).IntakeStatus`.
  - `httpapi`: 3 handler mới do ogen sinh interface (`GetWebIntake`, `SetWebIntake`, `WebIntakeStatus`); hàm đổi lỗi
    `webError` mới trong `errors_web.go`, thêm vào `domainErrorMappers` (errors.go); `WebCreateOrder` coi
    `ErrWebIntakePaused` là từ chối không lưu theo khóa. Tên người đổi (`changedByName`) đọc bằng `staffNames` có sẵn
    (`handlers_customer_timeline.go`, qua `identity.GetStaff`).
  - Test có sẵn phải thêm dòng (không nới điều kiện): `identity/permissions_test.go` (`TestPermissionsList`: thêm
    quyền mới vào danh sách mong đợi), `webcache/consumer_test.go` (`TestConsumer_NameTopics` thêm chủ đề; thêm ca
    ánh xạ `order.web_intake.changed`), `webcache/invalidator_test.go` (11 → 12 vùng; thêm ca `web_intake` → làm mới
    vùng), `httpapi/errors_snapshot_test.go` (hai dòng mới).
- Gói phụ thuộc ngược (go list): `order` ← `cmd/crm`, `httpapi`, `httpapi/web`, `cash`, `cash/internal/refunds`,
  `fulfillment`, `testkit/codtest`, `testkit/shiptest`, `wiring/webcache`; `httpapi/web` ← `httpapi`;
  `platform/cache` ← `cmd/crm`, `httpapi`, `httpapi/web`, `platform/observability`, `wiring/webcache`;
  `wiring/webcache` ← `cmd/crm`; `identity` ← `cmd/crm`, `adapters/cfaccess`, `httpapi`, `catalog`, `customer`,
  `platform/files`, `platform/realtime`, `realtime/realtimetest`, `tools/loadkit`.
- Operation OpenAPI bị chạm, oasdiff: chỉ thêm. Nhân viên: `getWebIntake` `GET /api/v1/web-intake` (`order.read`),
  `setWebIntake` `PUT /api/v1/web-intake` (`order.web_intake.manage`, `Idempotency-Key`). Website (`x-auth:
  service_key`, `web.catalog.read`): `webIntakeStatus` `GET /api/v1/web/intake` → `{paused}`. `webCreateOrder`: chỉ
  sửa mô tả (thêm 503 `web_orders_paused`). Mã lỗi mới trong `x-error-codes`: `web_orders_paused` (câu tiếng Việt ở
  `console/src/app/errors.ts`).
- Chỗ dùng trên màn hình: màn Nhận đơn web là Task 462 (không làm ở đây). `console/src/api/**` sinh lại bằng `make gen`;
  thêm câu lỗi vào `errors.ts`. Màn Vai trò hiện nhãn quyền mới (lấy từ API, không sửa mã màn).
- Câu sqlc, bảng, pha migration: `migrations/0524_order_web_intake.sql` (pha mở rộng: CREATE TABLE, RLS, GRANT) bảng
  `ord.web_intake` (một dòng mỗi tenant, nhóm 7 ADR-46 cuối nhóm — đã có trong ADR). Câu mới `queries/ord/web_intake.sql`
  (khối sqlc `order` có sẵn): `GetWebIntake` (không khóa), `EnsureWebIntake`
  (INSERT … ON CONFLICT DO NOTHING), `LockWebIntake` (FOR UPDATE), `UpdateWebIntake`. sqlc sinh lại `models.go` của mọi
  gói store (thêm một bảng).
- Test sẽ chạy: `order -run 'WebIntake'` (mới) và các test `CreateFromWeb` hiện có (`-run 'CreateFromWeb'`);
  `httpapi -run 'WebIntake|WebCreateOrder|ErrorSnapshot|ErrorCodes'`; `httpapi/web -run 'Intake'`; `wiring/webcache`
  (cả gói, nhỏ); `identity -run 'Permission'`; `platform/cache -run 'Spaces'`; console `npm test` (errors.test.ts);
  `tests/arch`; cả gói và vùng rủi ro cao (`order`) ở `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `refusableWrite`, `WriteTx`, `h.audit`, `catalogRead`
  (httpapi); `events.Registry.Emit`, `realtime.Bus`; `cache.ReadCache.GetOrLoad` + `etag` của `httpapi/web`;
  `order.ErrVersionConflict`, `order.ErrInvalidInput`; mẫu `order.Writer.UpdateSettings` (khóa dòng, so version);
  mẫu `promotion.Writer.campaignChanged` (sự kiện + báo hiệu màn hình); `identity.users_v` (tên nhân viên).
- Dùng lại / viết mới vì: dùng lại tất cả các mục trên; `version` lệch dùng `ErrVersionConflict` (409) có sẵn, lý do quá
  dài dùng `ErrInvalidInput` (400). Viết mới `ErrReasonRequired` (order chưa có; cùng ý với `inventory`, `cash` nhưng lỗi
  nghiệp vụ ở lại module sinh ra nó) và `ErrWebIntakePaused`. Không gộp với `order_settings` (kế hoạch Q10: bảng riêng
  để `version` của hai màn không đụng nhau). `handlers_web_intake.go` giống khung `handlers_order_settings.go` (đọc
  bằng `catalogRead`, ghi bằng `refusableWrite`) — giữ riêng vì khác lỗi từ chối, audit và sự kiện.

## Soát thiết kế

- 5.1 Chủ dữ liệu: cài đặt kênh website thuộc `order` (kế hoạch, bảng "Sở hữu" M5); bảng trong schema `ord`.
- 5.2 Không ghi bảng module khác: chỉ ghi `ord.web_intake`; audit qua `identity.RecordAudit` ở lớp HTTP (order không
  nhập identity).
- 5.3 Đọc module khác: `order` không đọc module nào (04 mục 3: order không gọi identity — `TestSQLUsageProductCode`
  bắt bản đầu nối `identity.users_v`, đã bỏ); tên người đổi do lớp HTTP đọc qua `identity.GetStaff`.
- 5.4 Nguyên tử: đổi trạng thái + sự kiện + báo hiệu + audit + kết quả chống trùng trong một transaction (`refusableWrite`).
- 5.5 Không nguyên tử: làm mới bộ đệm website qua sự kiện `order.web_intake.changed` → consumer `webcache` → báo hiệu
  hệ thống sau commit.
- 5.6 Gọi ra ngoài: không áp dụng (không gọi dịch vụ ngoài).
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant`; bảng mới khóa chính `tenant_id`, RLS
  (`platform.enable_tenant_rls`), khóa ngoại `tenant_id` → `platform.tenants`.
- 5.8 Không biết HTTP: `order` không nhập `net/http`; handler mỏng.
- 5.9 Đường ghi: `setWebIntake` có `Idempotency-Key` và `x-permission: order.web_intake.manage`; hai đường đọc có
  `x-permission` (`order.read`, `web.catalog.read`).
- 5.10 PostgreSQL là nguồn sự thật: chặn đơn quyết bằng câu đọc PostgreSQL trong transaction của `CreateFromWeb`;
  Valkey chỉ giữ `{paused}` 5 s cho website hiện thông báo.
- 5.11 Không thêm thư viện.
- 5.12 Theo module mẫu: `Writer.SetWebIntake` như `UpdateSettings` (khóa dòng, so version); sentinel trong
  `order/errors.go`, đổi HTTP ở `errors_web.go`; test `_test` trên PostgreSQL thật, `t.Parallel()`.
- 6.1 Hai người cùng sửa: khóa dòng `FOR UPDATE` rồi so `version` (lệch → 409). Dòng chưa có: `INSERT … ON CONFLICT DO
  NOTHING` rồi khóa, hai lần đầu song song không tạo hai dòng (khóa chính `tenant_id`).
- 6.2 Gửi lại: `Idempotency-Key` (middleware); test 50 yêu cầu song song cùng khóa → một lần đổi, một audit.
  `webCreateOrder` bị chặn: 503 **không** lưu theo khóa (xem Quyết định Q2).
- 6.3 Thứ tự khóa: `SetWebIntake` chỉ khóa một dòng `ord.web_intake` (nhóm 7, cuối nhóm, ADR-46 đã ghi). `CreateFromWeb`
  đọc không khóa (không thêm khóa vào đường tạo đơn, không đổi thứ tự khóa của nó).
- 6.4 Lỗi tạm: `refusableWrite`/`WriteTx` chạy lại cả transaction; fn gán lại biến kết quả mỗi lần.
- 6.5 Cách ly: READ COMMITTED. Đơn web đang tạo dở đúng lúc bấm tạm dừng (đã đọc `paused = false` trước khi lệnh tạm
  dừng commit) vẫn được nhận — chấp nhận theo kế hoạch: tạm dừng chỉ chặn đơn bắt đầu sau khi tạm dừng đã commit.
- 6.6–6.8: không áp dụng (không việc nền mới; consumer `webcache` có sẵn lịch thử lại).
- 6.9 Treo lâu: dùng giới hạn câu lệnh, chờ khóa mặc định của `db`.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: "đơn sync không vào khi đang tạm dừng; queued luôn vào" có test bảng ca; không phải vùng sinh ngẫu
  nhiên (một cờ).
- 6.12 Dữ liệu cá nhân: lý do là chữ của nhân viên (không phải dữ liệu khách) — ghi vào audit như lý do của chiến dịch
  (Task 452). `webIntakeStatus` chỉ trả `paused` (không lý do, người, giờ). Log không ghi lý do.
- 6.13 Quyền: ghi cần `order.web_intake.manage` (toàn shop, chỉ owner, manager mặc định); đọc `order.read`; website
  `web.catalog.read`; mặc định từ chối, test 403.

## Quyết định thay chủ shop

- **Q1 — Dòng chưa có = đang nhận đơn, version 1.** Không chèn dòng sẵn cho tenant cũ trong migration (migration không
  biết tenant; RLS); `GetWebIntake` trả mặc định `{paused: false, version: 1}`, lần ghi đầu `INSERT … ON CONFLICT DO
  NOTHING` rồi khóa. Phương án khác: thêm vào `EnsureDefaults` + migration điền sẵn — nhiều chỗ chạm hơn, cùng kết quả.
- **Q2 — 503 `web_orders_paused` không lưu theo `Idempotency-Key`.** Website dùng `Idempotency-Key = externalRef`; nếu
  lưu, đơn đó không bao giờ tạo được sau khi mở lại. Như `retry_later`: trạng thái tạm, gửi lại cùng khóa sau khi mở lại
  thì xử lý lại. Phương án khác: lưu như từ chối dứt khoát — sai với ý "tạm" dừng.
- **Q3 — Kiểm tạm dừng sau bước tìm đơn cùng `ExternalRef`.** Website gửi lại đơn đã tạo (mạng chập) vẫn nhận 201 đơn đó
  trong lúc tạm dừng; chỉ đơn mới bị chặn. Phương án khác: chặn trước — website tưởng đơn đã có bị từ chối.
- **Q4 — `webIntakeStatus` qua bộ đệm đọc (vùng mới `web_intake`, 5 s) và làm mới ngay theo sự kiện.** Đúng chữ "hạn
  sống 5 s" của kế hoạch, giống `webAvailability`; quyết định chặn vẫn ở PostgreSQL.
- **Q5 — Mở lại không đòi lý do; lý do gửi kèm (nếu có) được lưu, rỗng thì xóa.** Kế hoạch chỉ đòi lý do khi tạm dừng.
  Câu trả lời có `changedByName` (tên nhân viên, lớp HTTP đọc qua `identity.GetStaff`) để màn 462 hiện "ai" không cần
  gọi thêm.
- **Q6 — Lý do: cắt khoảng trắng hai đầu; > 300 ký tự → 400 `invalid_input`; tạm dừng mà lý do rỗng → 422
  `invalid_input`** (như `cash.ErrReasonRequired`; không thêm mã lỗi mới cho việc này).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Chưa có dòng → đang nhận, version 1, đọc không chèn | `TestWebIntake_DefaultOpen` | `go test -race -count=1 -run '^(TestWebIntake_…|TestCreateFromWeb_IntakePaused)$' ./internal/modules/order/` | `ok … 2.445s` |
| Tạm dừng thiếu lý do → ErrReasonRequired; 301 ký tự → ErrInvalidInput; 300 được, cắt khoảng trắng | `TestWebIntake_PauseReasonRules` | như trên | `ok` |
| Lưu lý do, người, giờ; version tăng; sự kiện seq = version; báo hiệu; version cũ → 409; mở lại không lý do | `TestWebIntake_PauseAndReopen` | như trên | `ok` |
| Ghi đầu song song (chưa có dòng) → đúng một thành công, một dòng | `TestWebIntake_ConcurrentFirstWrite` | như trên | `ok` |
| Tạm dừng: sync → ErrWebIntakePaused, không đơn; queued nhận; gửi lại đơn cũ → Replayed; mở lại → sync nhận [WB01] | `TestCreateFromWeb_IntakePaused` | như trên | `ok` |
| API nhân viên: 403 sale ghi; 422 thiếu lý do; 400 301 ký tự; 200 có lý do, người, tên, giờ; 409 version cũ; mở lại; 2 audit có lý do | `TestWebIntakeAPI_PauseReopen` | `go test -race -count=1 -run '^(TestWebIntakeAPI_…|TestDomainErrorSnapshot.*)$' ./internal/httpapi/` | `ok … 2.719s` |
| 50 `setWebIntake` song song cùng khóa → một lần đổi, một audit, một sự kiện, mọi 200 cùng thân | `TestWebIntakeAPI_ConcurrentSameKey` | như trên | `ok` |
| Tenant khác riêng biệt | `TestWebIntakeAPI_OtherTenant`, `TestWebIntake_OtherTenant` | như trên | `ok` |
| `webCreateOrder` sync khi tạm dừng → 503 `web_orders_paused`, không Retry-After, không lưu khóa; queued 201; mở lại cùng khóa → 201; `webIntakeStatus` chỉ `{paused}`; khóa thiếu quyền 403 [WB01] | `TestWebIntake_PauseBlocksSyncOrders` | `go test -race -count=1 -run '^TestWebIntake_PauseBlocksSyncOrders$' ./internal/httpapi/web/` | `ok … 2.492s` |
| Mã lỗi mới đổi đúng HTTP | `TestDomainErrorSnapshot` (2 dòng mới) | như dòng API nhân viên | `ok` |
| Sự kiện → làm mới vùng `web_intake` của bộ đệm website | `TestConsumer_Payloads`, `TestConsumer_NameTopics` (ca `order.web_intake.changed`), `TestInvalidator_Mapping` (ca `web_intake`), `TestInvalidator_SystemResync` (12 vùng) | `go test -race -count=1 ./internal/wiring/webcache/` | `ok … 3.849s` |
| Quyền mới toàn shop, khớp `docs/quyen.md` | `TestPermissionsList`, `TestPermissionsDoc_MatchesCode` | `go test -count=1 -run 'Permission|Role|Doc' ./internal/modules/identity/` | `ok … 19.196s` |
| Câu lỗi màn hình cho mã mới | `errors.test.ts` | `npx vitest run src/app/errors.test.ts` | `17 passed` |
| Cổng kiểm của task: vet, lint, gen-check, sqlc vet, danh mục, jscpd, gitleaks, oasdiff, test-guard, console, test `-race` gói đổi + vùng cao `order` + `tests/arch`, test thường gói phụ thuộc | (cả bộ) | `ci-task.sh` (sau khi gộp `main` 7503f48) | `CI-TASK XANH`; `order` 70 s, `httpapi` 33 s, `tests/arch` 132 s (lần chạy đầu đỏ gen-check vì chưa stage mã sinh, jscpd vì vòng song song trùng test chiến dịch — đã viết lại) |
| Luật kiến trúc (order không gọi identity) | `TestSQLUsageProductCode` | `go test -count=1 -run 'TestSQLUsage' ./tests/arch/` | `ok` (lần đầu đỏ với `users_v`, đã sửa) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Bảng: CHECK lý do khi tạm dừng viết `NOT paused OR coalesce(length(reason), 0) >= 1` thay `length(reason) >= 1` của
  kế hoạch — với `reason` NULL, `length` ra NULL và CHECK cho qua; bản `coalesce` chặn đúng ý.
- Ca "tenant khác 404" của kế hoạch: trạng thái nhận đơn là một dòng mỗi tenant, không có mã để đoán; kiểm bằng
  "tenant B thấy mặc định, đổi của B không đụng A" (`TestWebIntakeAPI_OtherTenant`, `TestWebIntake_OtherTenant`).
- Thêm (không có trong Files của kế hoạch, cần cho "hạn sống 5 s" và audit đúng tên): vùng `cache.SpaceWebIntake`,
  ánh xạ `webcache`, câu lỗi trong `console/src/app/errors.ts`, dòng sự kiện trong `docs/views.md`, nhãn quyền trong
  `identity/roles.go`.

## Việc phát hiện thêm

- (Reviewer, không chặn) `WebCreateOrder` gọi giới hạn đơn theo SĐT (`AllowWebOrder`) trước `WriteTx`: đơn sync bị chặn
  khi tạm dừng vẫn tính vào giới hạn; website tự gửi lại nhiều lần lúc tạm dừng thì lần gửi sau khi mở có thể nhận 429
  `too_fast`. Việc của phía website (Task 465: đọc `webIntakeStatus`, không gửi khi đang tạm dừng) hoặc một task sửa
  thứ tự kiểm ở CRM; không làm ở đây.
- (Reviewer, không chặn) ogen kiểm `maxLength: 300` trước khi cắt khoảng trắng: lý do đúng 300 ký tự kèm khoảng trắng
  hai đầu bị 400 ở tầng HTTP; không sai dữ liệu. Màn 462 nên cắt khoảng trắng trước khi gửi.

## Reviewer

Kết luận: ĐẠT (lần 1, agent reviewer trên `git diff --cached` sau khi gộp `main` 7503f48).

- Phát hiện phải sửa: không có. Đã soi 7 điểm: chỗ chèn `checkWebIntake` (sau tìm đơn cùng `ExternalRef`, chỉ chặn
  sync, đọc không khóa); 503 `web_orders_paused` không lưu theo khóa, không `Retry-After`; `SetWebIntake` chạy lại được
  trong `RetryTx`, Seq = version mới không trùng; CHECK `coalesce` chặn NULL; bộ đệm 5 s + ánh xạ `webcache` + resync;
  quyền `ScopeShop` cho owner, manager, audit cùng transaction; không dữ liệu cá nhân trong sự kiện, báo hiệu, log.
- Ảnh hưởng dự kiến khớp thực tế; không chữ ký cũ nào đổi. Bảng bằng chứng: mọi test có thật và kiểm đúng điều ghi.
  Không test nào bị xóa, `t.Skip` hay nới.
- Ghi chú không bắt buộc: giới hạn đơn theo SĐT tính cả đơn bị chặn; `maxLength` của ogen kiểm trước khi cắt khoảng
  trắng (đã chép vào "Việc phát hiện thêm"); lần ghi đầu bị 409 vẫn có thể để lại dòng mặc định (tương đương chưa có
  dòng, vô hại).
