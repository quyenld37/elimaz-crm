# Task 72 — M2-15: Nguồn đơn và cài đặt đơn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task M2-15). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm** ký hiệu mới trong `order`, không sửa chữ ký có sẵn: `Settings`, `SettingsPatch`, `SettingsChange`, `SettingField` (+ hằng), `SettingOutOfRangeError`, `LoadSettings`, `(Writer).UpdateSettings`, `Source`, `SourcePatch`, `SourceChange`, `ListSources`, `(Writer).CreateSource`, `(Writer).UpdateSource`, `ActiveSource`, sentinel mới `ErrTrafficSourceNameTaken`. Dùng, không đổi: `ErrSettingOutOfRange`, `ErrTrafficSourceInactive`, `ErrVersionConflict`, `ErrNotFound`, `ErrInvalidInput`, `Actor`, `Writer`. Trong `httpapi`: thêm trường `Field` vào `errorBody` (chỉ dùng trong errors.go), thêm nhánh vào `detailedError`, một dòng vào `orderError`; năm phương thức handler mới thay `UnimplementedHandler` (ListTrafficSources, CreateTrafficSource, UpdateTrafficSource, GetOrderSettings, UpdateOrderSettings). `catalogRead` (handlers_catalog.go) được dùng lại cho hai đường đọc mới, chỉ sửa chú thích (7 chỗ gọi cũ không đổi).
- Gói phụ thuộc ngược (go list): `order` ← `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`, `internal/testkit/shiptest` (và gói con của order); `httpapi` ← `cmd/crm`, `tests/e2e`, `tests/arch`. Không đổi chữ ký có sẵn nên chỉ cần build và test các gói này.
- Operation OpenAPI bị chạm, oasdiff: `listTrafficSources`, `createTrafficSource`, `updateTrafficSource`, `getOrderSettings`, `updateOrderSettings` (đã khai ở Task M2-02, đang trả 501): bỏ câu "Chưa làm…" trong `description`. Schema `Error` thêm thuộc tính tùy chọn `field` (chỉ có ở `setting_out_of_range`) — thêm trường tùy chọn vào phản hồi, không phá vỡ. Không thêm mã lỗi mới (tên nguồn trùng dùng `already_exists` có sẵn).
- Chỗ dùng trên màn hình: chưa có (màn Cài đặt đơn là Task M2-30). `console/src/api/**` sinh lại theo hợp đồng (thêm `field?` vào kiểu Error).
- Câu sqlc, bảng, pha migration: không migration (bảng `ord.order_settings`, `ord.traffic_sources`, quyền UPDATE theo cột và RLS đã có ở 0204). `queries/ord/settings.sql` thêm `GetOrderSettings`, `LockOrderSettings`, `UpdateOrderSettings`; tệp mới `queries/ord/sources.sql`: `ListTrafficSources`, `InsertTrafficSource`, `LockTrafficSource`, `UpdateTrafficSource`, `TrafficSourceActive`. Câu có sẵn `EnsureOrderSettings`, `InsertTrafficSourceIfMissing`, `TrafficSourceByKey` giữ nguyên.
- Test sẽ chạy: `go test -race ./internal/modules/order/ -run 'Settings|Source'`, `go test -race ./internal/httpapi/ -run 'OrderSettings|TrafficSource|ErrorCode|M2Error'`, rồi cổng ci-task một lần (gói đổi và gói phụ thuộc; `tests/arch` vì đổi errors.go và hợp đồng).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (db, tenancy, idempotency, textnorm); mẫu sửa có version và khóa dòng ở `inventory.UpdateSupplier`; đường ghi HTTP qua `refusableWrite` và `h.audit` (`internal/httpapi/refusal.go`, `handlers_inventory.go`); `WriteTx`; `detailedError` cho lỗi có trường chi tiết; mã `already_exists`, `version_conflict`, `setting_out_of_range` đã có; `identity.DefaultRoles` để lấy đúng quyền của vai trò `sale` trong test; `testkit.OpenStaff`, `Grant`, `serveAs`, `expect`.
- jscpd (lượt ci-task đầu) báo 2 đoạn trùng mới, đã trả lời: (1) hàm đọc theo tenant mới trùng `catalogRead` → **gộp**: bỏ hàm mới, gọi `catalogRead` (cùng việc: đọc dữ liệu chung cả shop trong tenant người gọi; sửa chú thích cho đúng phạm vi). (2) đổi lỗi unique tên nguồn giống `inventory.mapSupplierError` → **giữ riêng**: khác module, khác bảng, khác lý do thay đổi (luật "logic nghiệp vụ ở lại module"); viết lại thành vị từ `sourceNameTaken` ngắn. Chạy lại jscpd: 0 đoạn trùng.
- Dùng lại / viết mới vì: dùng lại toàn bộ các phần trên. Viết mới chỉ phần nghiệp vụ của order (kiểm khoảng cài đặt, nguồn đơn) — logic riêng của module, ở lại trong `order`. Nhận diện vi phạm unique `traffic_sources_name_key` viết trong order (mỗi module tự đổi lỗi PostgreSQL của bảng mình, như `inventory.mapSupplierError`; platform không có helper chung cho việc này).

## Soát thiết kế

Mục 5 (khớp kiến trúc):

- 5.1 Chủ dữ liệu: cài đặt đơn, nguồn đơn thuộc `order` (schema `ord`).
- 5.2 Không ghi bảng module khác: chỉ ghi `ord.*`; audit ghi bằng `identity.RecordAudit` ở lớp HTTP (order không được nhập identity — `tests/arch/boundaries_test.go`), như `createOrder`.
- 5.3 Đọc module khác qua view: không đọc module khác.
- 5.4 Nguyên tử: sửa, audit, lưu kết quả chống trùng trong một transaction (`WriteTx`).
- 5.5 Việc sau commit: không áp dụng: không ai nghe thay đổi cài đặt ở M2, không phát sự kiện.
- 5.6 Gọi ra ngoài: không áp dụng.
- 5.7 Ngữ cảnh shop: mọi câu chạy trong `tenancy.WithTenant` (WriteTx, đọc qua `catalogRead`); `InsertTrafficSource` lấy `tenant_id` từ `current_setting('app.tenant_id')` như `cash/cod.sql`; bảng đã có RLS và unique ghép `tenant_id` (0204).
- 5.8 Nghiệp vụ không biết HTTP: module không nhập `net/http`, ogen; tên trường trong `SettingOutOfRangeError.Field` là hằng của module (tên ổn định, trùng tên trường hợp đồng để màn hình báo theo tên).
- 5.9 Đường ghi có khóa chống trùng và quyền: hợp đồng đã có `IdempotencyKey` và `x-permission: order.settings.manage`; đọc cần `order.read`.
- 5.10 Valkey: không áp dụng.
- 5.11 Thư viện mới: không.
- 5.12 Theo module mẫu: đường ghi là phương thức `Writer` (Q-T57-1), sentinel trong `order/errors.go`, đổi HTTP ở `errors.go`, test gói `_test` trên PostgreSQL thật.

Mục 6 (đúng khi chạy đồng thời):

- 6.1 Hai người cùng sửa: khóa dòng `FOR UPDATE` rồi so `version`; `UPDATE … WHERE version = $v` tăng `version`; version cũ → `ErrVersionConflict` (409). Tạo nguồn trùng tên song song: unique `(tenant_id, name)` quyết định, bên thua → `ErrTrafficSourceNameTaken` (409 `already_exists`).
- 6.2 Gửi lại: `Idempotency-Key` qua WriteTx; từ chối nghiệp vụ không lưu (refusableWrite).
- 6.3 Thứ tự khóa: mỗi đường ghi chỉ khóa một dòng của một bảng (`traffic_sources` hoặc `order_settings`, nhóm 7 ADR-46); không khóa nhiều bảng. `ActiveSource` chỉ đọc, không khóa (đơn mang nguồn vừa bị tắt cùng lúc là chấp nhận được: tắt nguồn chỉ chặn đơn mới, khóa ngoại vẫn đúng).
- 6.4 Lỗi tạm: `WriteTx`/`db.RetryTx` có sẵn; hàm gán lại kết quả mỗi lần chạy.
- 6.5 Mức cách ly: mặc định cộng khóa dòng.
- 6.6–6.8: không áp dụng: không việc nền, không gọi ra ngoài.
- 6.9 Treo lâu: timeout mặc định của transaction API.
- 6.10 Tiền: không áp dụng (ngưỡng là điểm cơ bản, `int`).
- 6.11 Quy luật: không thuộc vùng rủi ro cao; test bảng cho biên của từng khoảng (dưới, trên, đúng biên).
- 6.12 Dữ liệu cá nhân: audit chỉ có số cài đặt, tên nguồn, trạng thái (không phải dữ liệu cá nhân).
- 6.13 Quyền: kiểm ở API (bảng route và guard); test `sale` đọc 200, sửa 403.

## Quyết định thay chủ shop

- Q-T72-1: Tên nguồn trùng trả 409 `already_exists` (mã có sẵn, màn hình đã có câu "Đã có mục cùng tên hoặc cùng mã"), sentinel mới `order.ErrTrafficSourceNameTaken`. Khác: mã mới `traffic_source_name_taken` (thêm mã vào hợp đồng và câu ở errors.ts, không thêm thông tin). Dễ đảo: đổi một dòng ở errors.go.
- Q-T72-2: `setting_out_of_range` nêu tên trường bằng thuộc tính mới `field` trong thân lỗi (tùy chọn, như `reason`, `ownerId`). Khác: đặt tên trường vào mã lỗi (nhiều mã) — trái hợp đồng đã khai một mã.
- Q-T72-3: Đường ghi là phương thức của `order.Writer` (theo Q-T57-1) và trả `SettingsChange{Before, After}` / `SourceChange{Before, After}` thay cho chỉ `Settings`: order không được nhập identity nên audit ghi ở lớp HTTP, và giá trị trước phải lấy từ dòng đã khóa trong cùng transaction (đọc riêng trước khi khóa có thể cũ). Khác: order nhập identity (trái bảng ranh giới).
- Q-T72-4: Nguồn không có (hay của tenant khác) khi sửa → `ErrNotFound` (404 `not_found`). `ActiveSource` trả `ErrTrafficSourceInactive` cho cả nguồn không có và nguồn đã tắt (đúng mô tả sentinel của Task M2-02).
- Q-T72-5: Tên nguồn bỏ khoảng trắng hai đầu trước khi lưu và so trùng; so trùng phân biệt hoa thường như unique của bảng (0204). Nguồn mới đứng cuối (vị trí lớn nhất + 1).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test đỏ trước khi có mã | settings_test.go, sources_test.go | `go vet ./internal/modules/order/` | `undefined: order.Settings` (chưa có mã) |
| Mặc định (1440, 1440, 1000, 7) | `TestSettings_Defaults`; `TestOrderSettingsAPI` (GET bằng `sale`) | `go test -race ./internal/modules/order/ -run 'Settings\|Sources' -v` | `--- PASS: TestSettings_Defaults` |
| `PATCH {draftTtlMinutes: 5}` → 422 `setting_out_of_range` nêu `draftTtlMinutes` | `TestOrderSettingsAPI` (thân có `field: draftTtlMinutes`); biên từng trường: `TestSettings_OutOfRange` (9 ca); chi tiết thân: `TestDomainErrors_M2Details` | `go test -race ./internal/httpapi/ -run 'OrderSettings\|TrafficSource\|DomainErrors\|ErrorCode' -v` | `--- PASS: TestOrderSettingsAPI`, `--- PASS: TestSettings_OutOfRange`, `--- PASS: TestDomainErrors_M2Details` |
| `version` cũ → 409 | `TestOrderSettingsAPI` (`version_conflict`), `TestSettings_UpdateAndVersion`, `TestTrafficSourcesAPI`, `TestSources_CreateUpdate` | như trên | PASS |
| Đổi ngưỡng thành 2000 → 200, audit có `1000 → 2000` | `TestOrderSettingsAPI` (đếm `identity.audit_logs` action `order.settings.update`, before 1000, after 2000, `changed_fields` có trường); `TestSettings_UpdateAndVersion` (Before/After) | như trên | PASS |
| Tắt nguồn "Zalo" → `ActiveSource` lỗi | `TestSources_ActiveSource` (ErrTrafficSourceInactive; cả nguồn không có, nguồn tenant khác); `TestTrafficSourcesAPI` (PATCH active=false, audit before/after) | như trên | `--- PASS: TestSources_ActiveSource` |
| Tên nguồn trùng → 409 | `TestTrafficSourcesAPI` (`already_exists` khi tạo và khi đổi tên); `TestSources_CreateUpdate` (trùng sau khi bỏ khoảng trắng); `TestDomainErrors_M2` | như trên | PASS |
| Vai trò `sale` đọc được, sửa → 403 | `TestOrderSettingsAPI`, `TestTrafficSourcesAPI` (quyền lấy từ `identity.DefaultRoles` "sale": GET 200; PATCH cài đặt, POST/PATCH nguồn 403 `forbidden`) | như trên | PASS |
| Gửi lại cùng khóa trả lại câu trả lời đã lưu | `TestOrderSettingsAPI` (gửi lại `s3` → version 2, không sửa lần hai) | như trên | PASS |
| Toàn gói order, httpapi | cả gói | `go test -race ./internal/modules/order/ ./internal/httpapi/` | `ok …/order 91.268s`, `ok …/httpapi 110.077s` |
| Cổng tĩnh (ci-task lượt 2, sau khi stage) | build, vet, gen-check, sqlc-vet, catalog-check, jscpd (0 đoạn trùng), gitleaks, oasdiff, test-guard, console (tsc, eslint, prettier) | ci-task | các cổng này không báo ĐỎ; lint báo 1 wrapcheck ở `sources_test.go:55` → đã sửa, `golangci-lint run ./...` → `0 issues.` |
| Gói đổi và gói phụ thuộc (sau khi sửa lint, DB chung đã lên lại) | order (+pricing, state), httpapi, fulfillment, testkit, cmd/crm, tests/arch, tests/e2e | `go test -race ./internal/modules/order/... ./internal/httpapi/... ./internal/modules/fulfillment/... ./internal/testkit/... ./cmd/crm/... ./tests/arch/...`; `go test -race ./tests/e2e/...` | tất cả `ok` (order 50.5s, fulfillment 15.1s, arch 12.6s, e2e 77.5s) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `UpdateSettings`, `UpdateSource` trả `SettingsChange` / `SourceChange` (trước và sau) thay vì `Settings` (Q-T72-3); ba hàm ghi là phương thức `Writer` (Q-T57-1).

## Việc phát hiện thêm

- `catalogRead` (httpapi) nay dùng cho cả cài đặt đơn: nên đổi tên thành `tenantRead` ở một task dọn code riêng (đổi cấu trúc, không làm trong task này).
- Lượt ci-task đầu chạy khi chưa stage (gen-check so với phần đã stage nên đỏ; lint đỏ ở test helper) — đã sửa. Lượt hai: chỉ còn 1 wrapcheck (đã sửa); phần test đỏ hàng loạt vì PostgreSQL chung lại khởi động lại (`the database system is starting up`, 57P03) — đã chạy lại trực tiếp test các gói bị ảnh hưởng khi DB lên (bảng bằng chứng), không chạy ci-task lần ba.
- Lượt ci-task đầu (02:3x, 19/11): PostgreSQL chung crm-shared (cổng 5600) rơi vào "recovery mode" (57P03) giữa chừng, làm đỏ `internal/queries` và `tests/e2e` do không nối được; không liên quan mã của task. Không đụng vào crm-shared.

## Reviewer

Kết luận: ĐẠT (lượt 1, 19/11/2026). Phát hiện phải sửa: không có. Reviewer đối chiếu từng dòng Ảnh hưởng (khớp; ghi nhận chữ ký `SettingsChange` khác kế hoạch đã ghi ở Q-T72-3), quyền UPDATE theo cột ở 0204 đủ cho mọi cột được ghi, tên ràng buộc `traffic_sources_name_key` có thật, và từng dòng bằng chứng (test kiểm thật trên DB, kể cả audit 1000 → 2000 và gửi lại cùng khóa). Ghi chú không bắt buộc: từ chối nghiệp vụ không lưu vào khóa chống trùng (tx hỏng vì 23505 bị lùi); hai lần tạo nguồn đồng thời có thể cùng `position` (không unique, danh sách vẫn xếp ổn định theo tên, id); tenant thiếu dòng `order_settings` nhận 500 (dữ liệu lệch, đã ghi).
