# Task 143 — Hợp đồng API M4 và khung

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 143, M4-03). Loại việc (quy trình mục 3): A

Căn cứ: Interfaces và Step của Task 143 trong kế hoạch M4; "Giả định về mốc trước", Global Constraints, Q1–Q36 của
kế hoạch; spec 04 mục 3, 4, 11; ADR-18, 41, 44, 46; ghi chú điều phối 25/11 (đối chiếu với mã thật trên `main`,
migration dải 04xx, `ci-task.sh`, Features của kênh).

## Đối chiếu "Giả định về mốc trước" với mã thật (Step 0, `main` ở 897a1a1)

| Dòng giả định | Mã thật | Kết luận |
|---|---|---|
| M0: `webhooks.{Source, InboundEvent, SplitResult, InboundHandler, Inbound, InboundKind}`, `commands.{Handler, CommandKind, Abandon}`, `queue`, `schedules.Schedule`, `realtime.Bus` | Có, đúng tên (`internal/platform/webhooks/source.go`, `inbound.go`, `commands/command.go`, `indoubt.go`) | Khớp |
| Task chung: `webhooks.Inbound.ReceivedAt`, `Via` (`ViaWebhook…ViaHistory`), `EnqueueInbound` | Có (`inbound.go`, `source.go`, `enqueue.go`) | Khớp |
| Task chung: `fieldcrypt` (`ParseKeyring`, `Keyring`), `FIELD_ENC_KEYS` | Có; `config.Config.FieldEncKeys` đọc tùy chọn ở api, worker, chú thích "mốc dùng thêm dòng yêu cầu trong Load" | Khớp; M4 thêm yêu cầu (xem Quyết định 2) |
| Khung ADR-46 của M4 (nhóm 3, 6, 13; C11) | Có đủ trong `docs/adr/0046-thu-tu-khoa.md`, test `TestLockOrderFrame` giữ (Task M2-33) | Khớp; task này chỉ ghi "đã đối chiếu" và thêm test bảng `04*.sql` |
| M1: `identity.DefaultRoles`, `EnsureDefaultRoles`, `docs/quyen.md` + `TestPermissionsDoc_MatchesCode` | Có; thêm `permissionLabels` (nhãn màn Vai trò, `TestPermissionCatalog_Complete`) và luật Task 315: quyền chính sách toàn shop là `ScopeShop` | Lệch nhỏ: `channel.manage` theo luật mới (Quyết định 1) |
| M1: `httpapi` nhúng `gen.UnimplementedHandler` | Đã có (`handlers_health.go`), operation chưa làm trả 501 `not_implemented` sau `auth`, `guard` | Khớp, không sửa `server.go` |
| M2: view `ord.orders_v` có `conversation_id`, `channel_account_id`, `draft_expires_at`, `version`; `customer.customers_v` 4 cột đầu; `customer.customer_identities_v` | Có trong `docs/views.md` (giữ khớp database bằng `TestViewsDoc*`); M3, M5 nối thêm cột cuối | Khớp |
| M2: `TimelineItem.kind` của `getCustomerTimeline` | Là chuỗi mở (không enum), mô tả đã ghi "M4 thêm conversation" | Chỉ thêm `call` vào mô tả, không đổi kiểu |
| M5: migration 05xx, `/api/v1/web/*`, `x-auth`, `x-admission`, `x-body-limit`, quyền `web.*` | Không trùng đường, operationId hay quyền của M4; migration M4 dải 04xx chưa có tệp nào trên `main` | Không ảnh hưởng; task này không có migration |
| M3: adapter hãng giả khóa `fake` (`/hooks/fake`, `fake.CarrierKey`) | Có | **Trùng** với khóa `fake` của adapter nhắn tin giả M4 (Task 145): Quyết định 6 |
| Luật ranh giới (f), (g), (h) của kế hoạch | Chữ (f)–(m) đã dùng; (k) "module không nhập `internal/adapters/**`" đã có (= luật (g) của kế hoạch) | Luật (f) kế hoạch thành (n); (h) là dòng `moduleCalls` (Quyết định 4) |

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký có sẵn.
  - `identity.Permissions` thêm 5 quyền (`channel.manage`, `conversation.reply`, `conversation.assign`,
    `conversation.settings`, `live.operate`); `DefaultRoles` (gọi ở `EnsureDefaultRoles`, `SyncDefaultRoles`, test
    vai trò): `sale` thêm `conversation.read`, `conversation.reply`, `live.operate`, `files.upload`; `manager`,
    `owner` tự nhận (dựng từ `Permissions`); `permissionLabels` thêm 5 dòng (nhóm `groupChat` đã có).
  - `config.Config` thêm trường `Meta MetaConfig` (Graph, app, webhook, OAuth, máy chủ tệp đính kèm) và
    `ConversationRetentionMonths`; kiểu mới `config.MetaConfig`. `Load` (10 gói nhập `config`) thêm kiểm M4.
  - Gói mới: `internal/modules/channel` (kiểu, `MessagingAdapter`, giao diện tùy chọn, sự kiện chuẩn hóa,
    `Registry`, `Deps`, khung `CommandHandlers`/`Schedules`), `internal/modules/conversation` (`Deps`, chủ đề realtime,
    khung `InboundHandlers`/`CommandHandlers`/`JobHandlers`/`Schedules`/`Background`, 11 gói khung
    `internal/{inbound,send,automation,live,assign,templates,presence,reminders,calls,history,access}`),
    `internal/adapters/facebook`, `internal/adapters/fake` (mỗi gói `doc.go` + hàm khởi tạo trả nil).
  - `httpapi.Routes` (mã sinh) thêm 64 route M4; `gen.Handler` thêm phương thức (đã có `UnimplementedHandler` trả 501).
- Gói phụ thuộc ngược (go list): `internal/platform/config` 10 gói nhập trực tiếp; `internal/modules/identity` 9;
  `internal/httpapi` 1 (`cmd/crm`). Gói mới chỉ `cmd/crm` nhập.
- Operation OpenAPI bị chạm, oasdiff: thêm 64 operation (bảng kế hoạch, mỗi dòng gộp tách đủ); thêm mã vào
  `x-error-codes`; mô tả `TimelineItem.kind` thêm `call`. Không đổi operation có sẵn: oasdiff chỉ có thêm.
- Chỗ dùng trên màn hình: `console/src/api/**` sinh lại (thêm hàm, kiểu); `console/src/app/errors.ts` thêm câu
  tiếng Việt cho mã lỗi M4 (test `errors.test.ts`). Chưa có màn M4 (Task 174–178).
- Câu sqlc, bảng, pha migration: không (không migration: `ord.orders_v` của M2 đã đủ cột, kế hoạch vòng 2).
- Tệp sẽ đổi: `openapi/openapi.yaml`; mã sinh (`internal/httpapi/gen/**`, `routes_gen.go`, `console/src/api/**`);
  `internal/modules/identity/{permissions.go,roles.go}`, `docs/quyen.md`; `internal/platform/config/config.go`;
  `cmd/crm/{wire_m4.go (mới),wire_m4_test.go (mới),modules.go,main.go}` (`newInbound`, `newCommands` thêm tham số
  `m4Parts`, gọi duy nhất trong `runWorker`; `runWorker` đăng ký lịch, việc nền M4 và chạy luồng nền qua hàm mới
  `startBackground`; `ingestSources` thêm nguồn M4); gói mới ở trên; `internal/platform/config/meta.go` (mới);
  `internal/httpapi/m3_routes_test.go` (`m3Pending` dùng hàm chung `pendingOps`; 501 hợp lệ gồm cả `m4_pending.txt`);
  `tests/arch/lockorder_m3_test.go` (dùng hàm chung `createdTables`, jscpd); bảng mong đợi của test có sẵn
  `TestDefaultRoles_Content`, `TestPermissionsList` thêm dòng M4; `tests/arch/{boundaries_test.go,
  lockorder_test.go,views_m4_test.go,m4contract_test.go}`, `tests/arch/testdata/{boundaries_m4/**,lockorder_m4/**,
  m4_pending.txt}`;
  `internal/httpapi/m4_contract_test.go`; `console/src/app/errors.ts`; `docs/adr/0046-thu-tu-khoa.md` (ghi đã đối
  chiếu); `docs/danh-muc-dung-chung.md` (sinh).
- Test sẽ chạy: `go test ./tests/arch/ -run 'M4|Boundaries|LockOrder|Routes|Money'`; `go test -race -run` đúng các test
  mới/sửa của `internal/modules/identity` (`TestPermissions_M4|TestDefaultRoles_M4|TestEnsureDefaultRoles_M4|
  TestPermissionsDoc_MatchesCode|TestPermissionCatalog_Complete`), `internal/platform/config` (`TestLoad_M4*`),
  `internal/httpapi` (`TestM4Contract*`, `TestErrorCodes*`, `TestM3Routes_PendingList`), `cmd/crm`; console
  `npx vitest run src/app/errors.test.ts`, `tsc`, `eslint`; cuối cùng `scripts/agent/ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`background.Collect`,
  `background.UniqueNames`, `background.Deps`, `fieldcrypt.Keyring`, `webhooks.Source`, `commands.Handler`,
  `schedules.Schedule`, `realtime.Bus`, `logging.Secret`); mẫu hợp đồng M3 (`tests/arch/m3contract_test.go`,
  `checkContractOps`, `testdata/m3_pending.txt`, `internal/httpapi/m3_routes_test.go` `serveAs`, `rolePerms`);
  `tests/arch/lockorder_m3_test.go` (`createTableRe`), `lockorder_frame_test.go`; `cmd/crm/wire_m3.go`
  (`checkM3Unique`); `config` (`reader.secret`, `httpURL`, `list`, `integer`, `urlVar`).
- Dùng lại / viết mới vì: dùng lại `checkContractOps` (M2) cho bảng M4; dùng lại `createTableRe`, `readViewsDoc`,
  `checkBoundaries`, `checkViolations`, `checkCases`; `serveAs`, `rolePerms`, `testkit.OpenStaff` cho test 401/403/501;
  `background.Collect`/`UniqueNames` cho gom khung và kiểm trùng; `MessagingAdapter` **nhúng** `webhooks.Source`
  (Key, Challenge, VerifySignature, Split) thay vì khai lại (module không nhập `net/http`, luật (b)). Viết mới: tệp
  `wire_m4.go` theo đúng mẫu `wire_m3.go` (M4 là mốc riêng, tách để task M4 sau chỉ sửa tệp của mình); test pending
  M4 riêng tệp (`m4_pending.txt`) để không trộn danh sách chờ của hai mốc.

## Soát thiết kế

- 5.1 Chủ dữ liệu: `channel` (tài khoản kênh, token, giới hạn gửi), `conversation` (hội thoại, live, tự động) đúng
  bảng "Sở hữu" 04 mục 3; task này chưa tạo bảng.
- 5.2, 5.3: không ghi, không đọc bảng nào; luật (a) cập nhật `moduleCalls`: `conversation` gọi `channel`, `customer`,
  `order`, `identity` (Q1), `channel` gọi `identity`; `order` không gọi `conversation` (test).
- 5.4, 5.5, 5.6: không áp dụng ở task này (khung trả nil); Global Constraints M4 giữ ở Deps (Queue cho lệnh
  `command.fb.*`, không gọi mạng trong transaction).
- 5.7: không có truy vấn; route M4 mới đều qua `auth` → `guard` (test 401/403 mọi operation).
- 5.8: `channel` không nhập `net/http`, `net/url` (nhúng `webhooks.Source`); adapter mới chỉ nhập platform và
  phần công khai `channel` (luật (n) mới); module không nhập adapter (luật (k) có sẵn).
- 5.9: mọi operation ghi M4 có `IdempotencyKey` (`x-idempotency` mặc định `required`), khai `x-permission` (test
  `TestM4Contract_Operations`).
- 5.10: không áp dụng (không Valkey).
- 5.11: không thêm thư viện (`iter`, `io`, `time` của thư viện chuẩn).
- 5.12: theo mẫu M3 (`wire_m3`, `m3contract`), module mẫu (`errors_<vùng>.go` cho mã lỗi khi có sentinel).
- 6.1–6.11: không áp dụng: task không chạm database, hàng đợi, tiền, tồn (chỉ khai báo kiểu, hợp đồng, khung).
- 6.12 Dữ liệu cá nhân: bí mật mới (`META_APP_SECRET`, `META_VERIFY_TOKEN`) đọc vào `logging.Secret`; `Config.LogValue`
  chỉ ghi cờ có/không, không giá trị; lỗi cấu hình chỉ nêu tên biến. `Credentials.AccessToken` là `logging.Secret`.
- 6.13 Quyền: 5 quyền mới mặc định từ chối; `conversation.*`, `live.operate` theo đích `page` (grant Page A không có
  hiệu lực ở Page B, test); `channel.manage` toàn shop; guard 403 trước handler cho mọi route M4 (test).

## Quyết định thay chủ shop

1. **`channel.manage` là `ScopeShop` (toàn shop)**, không `ScopeNone` như kế hoạch viết 18/11. Vì: luật đã duyệt sau
   đó (Task 315 lượt 2, `docs/quyen.md`) ghi rõ "quyền mới ghi chính sách áp cho cả shop (… kết nối kênh…) là loại
   toàn shop"; mốc trước là chuẩn. Phương án khác: `ScopeNone` (gán theo kho vẫn có quyền kết nối Page). Đảo ngược:
   đổi một dòng trong `Permissions` và `docs/quyen.md`.
2. **Biến Meta chỉ bắt buộc khi bật kênh Facebook**: một vai trò được coi là bật Facebook khi có bất kỳ biến Meta
   riêng của vai trò đó (api, worker: `META_APP_ID`, `META_APP_SECRET`…; ingest: `META_APP_SECRET`,
   `META_VERIFY_TOKEN`, `HOOKS_PUBLIC_BASE_URL`); bật rồi thì mọi biến của vai trò (kể cả `FIELD_ENC_KEYS` ở api,
   worker) bắt buộc và lỗi liệt kê đủ. Kiểm dạng (`META_APP_SECRET` 32 hex, `META_VERIFY_TOKEN` ≥ 32, phiên bản Graph,
   `CONVERSATION_RETENTION_MONTHS` 6–60, `FILES_PUBLIC_BASE_URL` khác gốc mọi `CONSOLE_ORIGINS`) luôn chạy. Vì: bắt
   buộc ngay thì staging, production (đang chạy M5 cho website) và môi trường dev, e2e không khởi động được tới khi
   chủ shop đặt bí mật Meta (việc của Task 181, ngoài máy); cách này giống mẫu có sẵn (Valkey, hãng giả: có URL thì
   bí mật bắt buộc). Phương án khác: bắt buộc luôn (đúng chữ kế hoạch) kèm sửa `docker/`, e2e. Đảo ngược: bỏ điều kiện
   `enabled` trong `reader.meta`.
3. **`MessagingAdapter` nhúng `webhooks.Source`** thay vì khai lại bốn phương thức Key, Challenge, VerifySignature,
   Split: cùng chữ ký đúng 04 mục 4, nhưng module `channel` khỏi nhập `net/http`, `net/url` (luật (b)).
4. **Chữ luật ranh giới:** (f), (g), (h) của kế hoạch đã là luật khác trong mã; luật "(g) module không nhập adapter"
   chính là (k) có sẵn; luật "(f) adapter chỉ nhập platform và `channel`" thành **(n)** áp cho mọi gói
   `internal/adapters/**` trừ `carrier/**` (luật (j)) và `cfaccess` (adapter của identity, có trước); "(h)" là dòng
   `moduleCalls` của `conversation`.
5. **Mã lỗi M4 chỉ khai ở hợp đồng và màn hình** (`x-error-codes`, `errors.ts`), không thêm hằng `code*` vào
   `errors.go` lúc này: luật Task 441 đặt mã của một vùng vào `errors_<vùng>.go` cùng sentinel, mà sentinel chỉ có ở
   task hiện thực (5, 9, 16…); hằng không dùng sẽ đỏ lint. `TestErrorCodes_DeclaredInContract` cho phép khai trước.
   Thêm `already_assigned` (Task 160 dùng, bảng mã của Task 143 thiếu).
6. **Adapter nhắn tin giả dùng khóa `fakechat`**, không `fake`: `fake` đã là khóa của hãng vận chuyển giả M3
   (`/hooks/fake`, nguồn webhook, handler vào); hai nguồn cùng khóa làm worker không khởi động. Hằng
   `fake.Key = "fakechat"` khai ở `internal/adapters/fake/wiring.go`; Task 145 dùng hằng này cho `channel IN
   ('facebook','fakechat')`. Phương án khác: đổi khóa hãng giả M3 (phá dữ liệu, test M3).
7. **Danh sách chờ 501 riêng `tests/arch/testdata/m4_pending.txt`** như M3: test 401/403 chạy mọi operation M4; 501
   chỉ đòi với operation còn trong danh sách, task hiện thực xóa dòng của mình (test không gãy khi task sau làm route,
   bẫy Task 427).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Hợp đồng: 64 operation M4 đúng đường, quyền, cờ ghi; route ghi bắt buộc `Idempotency-Key` (ADR-41); không trùng `operationId` hay `METHOD path` | `TestM4Contract_Operations` (`tests/arch/m4contract_test.go`, dùng `checkContractOps`) | ci-task bước "test kiến trúc" (`go test ./tests/arch/`) | đỏ trước `make gen` (`thiếu operation …`), sau: `ok elimaz.com.vn/crm/tests/arch 13.687s` |
| Mọi route khai quyền trong danh sách quyền, mặc định từ chối (ADR-18) | `TestRoutesPublicSetAndPermissions`, `TestRoutesAllServed`, `TestRoutesIdempotencyModes` (có sẵn, chạy trên bảng route mới) | như trên | `ok` |
| Mỗi operation M4: không thẻ 401, thẻ không quyền 403, owner 501 `not_implemented` sau auth, guard (route ghi thân `{}` được phép 400) | `TestM4Contract_StatusCodes` (`internal/httpapi/m4_contract_test.go`) | `go test -count=1 -run '^(TestM3Routes_PendingList\|TestM4Contract_StatusCodes)$' ./internal/httpapi/`; ci-task `-race` | `--- PASS: TestM4Contract_StatusCodes (2.36s)`; ci-task `ok .../internal/httpapi 34.727s` |
| Danh sách chờ 501 chỉ có operation M4, không trùng; 501 ngoài danh sách là lỗi | `TestM4Contract_PendingList`, `TestM3Routes_PendingList` (sửa: nhận cả `m4_pending.txt`) | như trên | `PASS`; trước khi sửa M3 test đỏ "`listPendingDrafts` trả 501 mà không có trong m3_pending.txt" |
| PQ04 nền: grant `conversation.reply` page:a có ở a, không ở b; `ScopesFor` = [a]; `channel.manage` gán theo Page không hiệu lực | `TestCan_M4PageScope`, `TestPermissions_M4Scopes` | `go test -race -count=1 -run '^(TestPermissions_M4Scopes\|TestCan_M4PageScope\|…)$' ./internal/modules/identity/` | `ok .../identity 4.599s`; ci-task `ok .../identity 41.740s` |
| Vai trò mẫu: sale có đúng `conversation.read`, `conversation.reply`, `live.operate` + `files.upload`; manager, owner đủ quyền M4; Kho, Kế toán không | `TestDefaultRoles_M4`, `TestDefaultRoles_Content` | như trên | đỏ trước (`mong 64 quyền, có 69`; sale lệch), sau `ok` |
| `EnsureDefaultRoles` chạy hai lần không đổi số dòng; sale đã sửa không nhận quyền M4, owner có | `TestEnsureDefaultRoles_Idempotent` (có sẵn), `TestEnsureDefaultRoles_M4` | như trên | `ok` |
| Tập quyền trong mã bằng tập dòng `docs/quyen.md`; mỗi quyền có nhãn màn Vai trò | `TestPermissionsDoc_MatchesCode`, `TestPermissionCatalog_Complete` (có sẵn) | như trên | `ok` |
| Cấu hình: api bật Facebook thiếu `META_APP_SECRET`, `FIELD_ENC_KEYS` → lỗi nêu cả hai; ingest không đòi `FIELD_ENC_KEYS`; secret 31 ký tự → lỗi; `FILES_PUBLIC_BASE_URL` trùng gốc `CONSOLE_ORIGINS` → lỗi; `CONVERSATION_RETENTION_MONTHS=3` → lỗi; không bật thì khởi động như cũ; bí mật không lộ qua `LogValue`, lỗi | `TestLoad_M4MissingWhenEnabled`, `TestLoad_M4BadValues`, `TestLoad_M4Defaults`, `TestLoad_M4FacebookValid` (`internal/platform/config/m4_test.go`) | `go test -race -count=1 ./internal/platform/config/` | đỏ trước (không biên dịch: `cfg.Meta undefined`), sau `ok 1.489s`; ci-task `ok 1.636s` |
| `ord.orders_v` có 4 cột M4 cần, 11 cột đầu của M1; `customers_v` 4 cột đầu; `customer_identities_v` có (R3) | `TestViews_M4Needs` | ci-task "test kiến trúc" | `ok` (xanh từ đầu: M2 đã đủ cột, đúng như kế hoạch dự kiến; là chốt chặn) |
| Luật ranh giới mới: testdata đúng 3 lỗi (n, k, a); mã thật 0 lỗi | `TestBoundariesTestdataM4`, `TestBoundariesM4Rules`, `TestBoundariesRealCode` | `go test -count=1 -run Boundaries ./tests/arch/` | đỏ trước khi thêm luật (`mong đúng 3 vi phạm, có 2`), sau `ok` |
| ADR-46: bảng trong `migrations/04*.sql` phải có tên trong tệp ADR; mẫu `conversation.ghost` bị nêu tên | `TestLockOrder_M4TablesListed`, `TestLockOrder_M4Sample`; khung M4 có sẵn `TestLockOrderFrame` | ci-task "test kiến trúc" | `ok` |
| Bộ adapter bỏ nil, khóa trùng → lỗi; khung chưa có lệnh, lịch; cmd nối được ở mọi môi trường | `TestRegistry`, `TestWiringEmpty` (channel, conversation), `TestWireM4_Frame` | ci-task `-race` | `ok .../channel 2.198s`, `ok .../conversation 2.624s`, `ok .../cmd/crm 6.797s` |
| Mỗi mã lỗi M4 có câu tiếng Việt | `console/src/app/errors.test.ts` | `cd console && npx vitest run src/app/errors.test.ts` | `Tests 17 passed (17)` |
| oasdiff chỉ có thêm | `tools/apicheck` | ci-task bước "oasdiff" | không báo thay đổi phá vỡ |
| Toàn bộ cổng của task | — | ci-task (cổng tĩnh, gen-check, catalog-check, jscpd, console-check, test gói đổi và phụ thuộc) | `CI-TASK XANH (so với điểm tách nhánh 897a1a1…)` |
| Gộp `main` trước khi stage | — | `git merge main` | `Already up to date.` |

## Test bị xóa hoặc tắt

(không có). Hai bảng mong đợi của test có sẵn (`TestDefaultRoles_Content`, `TestPermissionsList`) chỉ thêm dòng M4;
`TestM3Routes_PendingList` thêm ca (operation còn trong `m4_pending.txt` mà không trả 501/400 là lỗi), không bớt ca.

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 143 không có test e2e)

## Khác kế hoạch

- Các Quyết định 1–7 ở trên.
- `channel.NewRegistry` trả `(*Registry, error)` (kế hoạch: chỉ `*Registry`): hai adapter cùng khóa phải là lỗi khởi
  động, không âm thầm đè.
- Chi tiết lỗi `send_not_allowed` dùng trường `denyReason` (+ `until`), không `reason`: `Error.reason` đã là enum
  `CouponRejectReason` của M2, đổi kiểu là thay đổi phá vỡ.
- `connectFacebookPage`: một object có `pageId` bắt buộc, `sessionId`/`systemUserToken` tùy chọn (handler kiểm đúng
  một trong hai → 422 `invalid_input`, ghi trong mô tả operation) thay vì `oneOf` không có trường phân biệt; cùng dữ
  liệu.
- Khung `conversation`: kiểu dùng chung `Deps`, `Parts` ở gói riêng `internal/modules/conversation/internal/deps`
  (`conversation.Deps` là bí danh) để mỗi phần `internal/<phần>` khai `Parts(d)` mà không nhập vòng gói gốc;
  `internal/access` chỉ có chú thích gói (là thư viện kiểm quyền, không có phần chạy nền).
- Nối M4 ở tệp mới `cmd/crm/wire_m4.go` (mẫu `wire_m3.go`) thay vì viết trong `main.go`; `main.go`, `modules.go` chỉ
  thêm dòng gọi. Thêm `internal/adapters/{facebook,fake}/wiring.go` (hàm `Adapter` trả nil, hằng `Key`) để việc đăng
  ký adapter "đã có ở Task 143" như Task 145 ghi.
- `docs/views.md` không đổi: task chưa tạo view (view M4 do Task 145, 151… thêm). ADR-46 chỉ ghi "đã đối chiếu":
  khung M4 (kể cả ghi chú C11, lý do `data_deletion_requests`) đã đủ từ Task M2-33.
- `TimelineItem.kind` là chuỗi mở (không enum) từ M2: chỉ thêm `call` vào mô tả, không đổi kiểu.

## Việc phát hiện thêm

1. **Tính năng kênh trong `listChannelAccounts`** (Task 142, Việc phát hiện thêm 1; ghi chú điều phối 25/11): kế hoạch
   Task 143, 145 chưa có trường `features` (cũng như `lastWebhookAt`, `tokenKind`) trong `ChannelAccount` của API, nên
   hợp đồng chưa thêm. Chủ shop quyết; thêm sau là thêm trường tùy chọn (không phá vỡ).
2. **Số liệu phiên live cho màn Live** (Task 142, Việc phát hiện thêm 4): `LiveSession` có `receivedCount`,
   `metaCommentCount`, `activeSource`, `detectedBy`; chưa có số comment có SĐT, số đã lên đơn, số đơn thiếu địa chỉ.
   Task 170/177 đối chiếu, thêm trường tùy chọn nếu cần.
3. **`conversation.settings` theo Page nhưng nhãn, mẫu chung dùng cho cả shop** (Q7): grant `conversation.settings`
   chỉ một Page vẫn qua guard của `createConversationTag`, `createReplyTemplate`. Task 160, 161 cần quyết: đòi grant
   `all` cho tài nguyên chung (kiểm ở handler) hay chấp nhận.
4. **Task 145 dùng `fake.Key` (`fakechat`)** cho adapter giả và `CHECK (channel IN ('facebook','fakechat'))`, không
   `fake` (trùng hãng giả M3, Quyết định 6).
5. **Biến Meta cho staging/production** (Task 181): đặt `META_*`, `HOOKS_PUBLIC_BASE_URL` thì kênh Facebook bật và
   `FIELD_ENC_KEYS` bắt buộc ở api, worker.
6. **Trước lần deploy kế tiếp** (reviewer): api giờ luôn đòi `FILES_PUBLIC_BASE_URL` khác gốc mọi `CONSOLE_ORIGINS`
   (an toàn tệp khách gửi, Global Constraints M4). Mẫu `docker/compose.prod.env.example`, dev, e2e đều khác gốc; giá trị
   thật trên staging/production cần đối chiếu (việc trên máy chủ, ngoài quyền của phiên này).
7. `channel.Deps`, `conversation.Deps` có `Alerter`; `wireM4` chưa gán (chưa phần nào dùng). Task 150 (cảnh báo sức
   khỏe kênh) nối trường này trong `wire_m4.go`.

## Reviewer

Kết luận: ĐẠT (agent reviewer, 25/11, trên phần đã stage sau ci-task xanh).

- Phát hiện phải sửa: không có.
- Ảnh hưởng dự kiến khớp thực tế: mỗi hàm đổi chữ ký (`newInbound`, `newCommands`) chỉ một chỗ gọi; goroutine của
  `startBackground` có chủ (`wg.Wait()` trong `runWorker`); `openapi.yaml` chỉ thêm (2994 dòng) và sửa một mô tả.
- Bảng bằng chứng: mọi dòng trỏ tới test có thật, kiểm đúng điều tiêu chí nói; đếm lại bảng kế hoạch đúng 64
  operation.
- Tái dùng, cách cũ, test bị nới, luật dự án: không có vấn đề.
- Ghi chú không bắt buộc: kiểm `FILES_PUBLIC_BASE_URL` khác gốc màn hình chạy cả khi chưa bật Facebook (đã ghi Việc
  phát hiện thêm 6); 400 cho route ghi thân `{}` chỉ chứng minh qua guard (giống mẫu M3, chấp nhận); `Alerter` chưa
  nối (Việc phát hiện thêm 7); câu hỏi `conversation.settings` (Việc phát hiện thêm 3).
