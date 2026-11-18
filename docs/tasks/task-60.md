# Task 60 — Schema khách

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md`, Task 60 (M2-03): schema khách (mở rộng bảng của
M1), chuẩn hóa SĐT và tên, kiểu dữ liệu cá nhân, view `customer`. Loại việc (quy trình mục 3): A

Cột và thứ tự hiện hành của `customer.customers_v` (R3, chép từ `docs/m2/doi-chieu-m1.md` dòng G7 và mục kiểm):
`id, root_id, full_name, created_at` — đúng thứ tự, kiểu `uuid, uuid, text, timestamptz`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi ký hiệu cũ. Gói `customer` thêm
  `NormalizePhone`, `ReverseDigits`, `NormalizeName`, `Name`, `Birthday`, `Lifecycle` (+ hằng), `IdentityKind` (+ hằng).
  `customer.Create` giữ nguyên chữ ký và hành vi (0 chỗ gọi phải sửa; chỗ gọi hiện có: `internal/httpapi/handlers_order.go`,
  `internal/testkit/shiptest/shop.go`, `internal/modules/order/{create_test,env_test}.go`). Gói `logging`: không đổi ký
  hiệu; đổi giá trị biến chưa xuất `sensitiveKey` (thêm `full_name|recipient|birthday|email|note`), ảnh hưởng hành vi của
  `SensitiveKey`, `Redact`, bộ ghi log (chỗ dùng: `logging/logger.go`, `identity/audit.go`, `webhooks/ingest.go`).
- Gói phụ thuộc ngược (go list): `customer` ← `cmd/crm`, `internal/httpapi`, `internal/modules/order` (test),
  `internal/testkit/shiptest`, `tests/arch`. `logging` ← 19 gói (mọi gói ghi log); thay đổi chỉ làm che **nhiều hơn**.
  Đã grep log: không chỗ nào ghi log với khóa `note`, `email`, `full_name`, `birthday`, `recipient*` mà mong thấy giá
  trị. Chỗ đổi hành vi thực tế (theo hướng che nhiều hơn, không lộ gì): `redact_test.go` dòng 21, 108 dùng `note` làm ví
  dụ khóa không che; `webhooks/ingest_test.go` dùng header `X-Request-Note` làm ví dụ header thường (header có chữ
  `note`, `email` nay bị che khi lưu `webhook_deliveries.headers`); audit nhà cung cấp `httpapi/handlers_inventory.go`
  `supplierAudit` khóa `noteLength` nay ghi `[Redacted]` (như `hasPhone` từ trước) — reviewer phát hiện.
- Operation OpenAPI bị chạm, oasdiff: không có (hợp đồng M2 đã có từ Task 59; task này không chạm `openapi.yaml`).
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: migration mới `0201_customer.sql` (pha **mở rộng**): `ALTER customer.customers` thêm 12
  cột, điền dữ liệu dòng có sẵn, `SET NOT NULL` cho `code` (có dòng `-- contract:`: cột mới có `DEFAULT`, không mã nào
  ghi `NULL`), ràng buộc mới `NOT VALID` → `VALIDATE`; khóa ngoại `root_id` đổi sang `DEFERRABLE INITIALLY DEFERRED`;
  trigger `BEFORE INSERT` đặt `root_id`; 8 bảng mới (`customer_identities`, `customer_addresses`, `customer_merges`,
  `customer_tags`, `customer_tag_links`, `customer_tag_rules`, `customer_notes`, `customer_imports`);
  `CREATE OR REPLACE VIEW customer.customers_v` (giữ 4 cột đầu, nối 11 cột) và 4 view mới. Câu sqlc mới
  `queries/customer/customers_base.sql` (`GetCustomerBase`); câu M1 `InsertCustomer` không đổi. `docs/views.md` dòng
  `customer.customers_v` sửa cùng commit (R13). Mã sinh đổi theo (`make gen`, không sửa tay): `models.go` của mọi gói
  `internal/**/internal/store` (sqlc đọc cả thư mục `migrations` làm schema nên mọi module thấy kiểu bảng mới) và
  `internal/modules/customer/internal/store/customers_base.sql.go`.
- `internal/testkit/personaldata.go`: thêm `CaptureLog` (chỉ thêm; testkit đổi → bộ test toàn phần).
- Test có sẵn phải sửa theo hành vi mới: `internal/platform/logging/redact_test.go` (khóa `note` thành khóa bị che),
  `tests/arch/m2contract_test.go` `TestM2Contract_ViewsDoc` (dòng `customer.customers_v` của Task 59 ghi rõ "sửa ở
  Task M2-03": nay 15 cột), `internal/platform/webhooks/ingest_test.go` `TestHeadersStoredRedacted` (header ví dụ).
- Test sẽ chạy: trong lúc làm `go test -race` các gói `internal/modules/customer`, `internal/platform/logging`,
  `internal/modules/order`, `internal/modules/identity`, `tests/arch`; script cổng kiểm của task một lần trước review
  (migration đổi → bộ toàn phần theo luật của script).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `logging`
  (`Phone`, `Address`, `Secret`, `SensitiveKey`, `Redact`), mục `textnorm` (`Fold`); grep `Normalize`, `Phone(`,
  `phoneRe`: có `identity.NormalizeEmail` (email nhân viên), `inventory` làm sạch SĐT nhà cung cấp (8–15 chữ số, kể cả
  số nước ngoài, không đổi về `+84`).
- Dùng lại / viết mới vì: dùng lại `logging.Phone` làm kiểu trả của `NormalizePhone` (không khai kiểu SĐT thứ hai);
  `NormalizeName` gọi thẳng `textnorm.Fold` (không bảng dấu thứ hai trong Go, C22). Viết mới `NormalizePhone` vì luật
  khác SĐT nhà cung cấp (chỉ số Việt Nam, dạng chuẩn `+84…` làm khóa unique của danh tính) — cùng việc "làm sạch SĐT"
  nhưng khác lý do đổi (05 mục 1 so với danh bạ nhà cung cấp), giữ riêng. Bảng bỏ dấu bằng SQL trong migration (điền
  `name_search` cho dòng có sẵn) là bản thứ hai bắt buộc theo kế hoạch, chỉ chạy một lần; test so với `textnorm.Fold`.
  `Name`, `Birthday` theo mẫu `logging.Phone` (kiểu có `LogValue()`).
- jscpd báo `pii_test.go` trùng 6 dòng dựng logger với `logging/redact_test.go` và `logging/scrub_test.go`: thêm
  `testkit.CaptureLog(t) (*slog.Logger, *bytes.Buffer)` (logger thật của ứng dụng ghi vào bộ đệm) và dùng ở test mới;
  hai test cũ của `logging` giữ nguyên (cặp trùng giữa chúng đã có trong baseline jscpd, ngoài phạm vi task).

## Soát thiết kế

Kế hoạch giao: mục 5 câu 1, 7; mục 6 câu 12. Các câu khác của mục 5, 6: không áp dụng — task chỉ dựng schema, hàm thuần
và kiểu; không có đường ghi, khóa dòng, hàng đợi, tính tiền, quyền API, gọi ra ngoài (đường ghi ở Task 66–70, 82).

- 5.1 Đúng chủ dữ liệu: mọi bảng mới nằm trong schema `customer` của module `customer` (04 mục 3: khách, danh tính, địa
  chỉ, nhãn, ghi chú, gộp, nhập khách thuộc `customer`). Module khác đọc qua 5 view công bố.
- 5.7 Ngữ cảnh shop: mọi bảng mới có `tenant_id uuid NOT NULL`, `platform.enable_tenant_rls`, `UNIQUE (tenant_id, id)`
  (bảng có `id`), mọi unique và index bắt đầu bằng `tenant_id` (GIN cần `btree_gin` cho cột `uuid`), khóa ngoại ghép
  `(tenant_id, …)`. Test: tenant B không đọc được dòng của A ở mọi bảng mới và mọi view; `CheckSchemaRules` → `[]`.
- 6.12 Dữ liệu cá nhân: SĐT là `logging.Phone`; tên `customer.Name`, ngày sinh `customer.Birthday` có `LogValue()` che;
  mẫu khóa log thêm `full_name|recipient|birthday|email|note` (Q21). `ErrInvalidPhone` không mang giá trị.
  Test: dòng log không chứa chữ số SĐT; khóa lồng trong map bị che.

## Quyết định thay chủ shop

- **QĐ1 — Danh sách khóa màu nhãn.** Kế hoạch ghi `CHECK IN (<danh sách khóa màu của console>)` nhưng console chưa có
  danh sách nào (màn nhãn ở Task 86). Chọn 10 khóa trung tính `gray, red, orange, yellow, green, teal, blue, purple, pink,
  brown`; console ánh xạ khóa → màu thật. Vì sao: lưu khóa (không lưu mã màu) để đổi bảng màu không phải migrate; danh
  sách ngắn đủ phân biệt. Phương án khác: mã hex tự do — linh hoạt hơn nhưng khó giữ hài hòa với DESIGN. Đổi lại: một
  migration thay `CHECK` (thêm ràng buộc mới `NOT VALID` rồi `VALIDATE`, bỏ ràng buộc cũ).
- **QĐ2 — `customer_tags` có thêm `version`, `updated_at`.** Hợp đồng API (Task 59) trả `CustomerTag.version` và Task 69
  (`UpdateTag(id, version, …)`) cần kiểm phiên bản; danh sách cột nhãn của Task 60 thiếu hai cột này. Thêm
  `version bigint NOT NULL DEFAULT 1`, `updated_at timestamptz NOT NULL DEFAULT now()`; không thêm vào view
  `customer_tags_v` (cột view đúng dòng `docs/views.md`). Phương án khác: Task 69 tự thêm bằng migration riêng — tốn một
  số migration ngoài bảng số đã cấp.
- **QĐ3 — `btree_gin` tạo trong schema `platform`** (`CREATE EXTENSION IF NOT EXISTS btree_gin SCHEMA platform`). Trên
  máy chủ, `crm_owner` không có quyền `CREATE` trên schema `public` (ghi chú ở script role của hạ tầng), nên tạo
  extension ở `public` sẽ hỏng khi migrate thật; `platform` do `crm_owner` làm chủ và `btree_gin` là extension "trusted".
  PostgreSQL tìm operator class mặc định không theo `search_path`, nên index GIN `(tenant_id, …)` dùng được. Nếu quản trị
  đã tạo sẵn `btree_gin` ở chỗ khác thì câu này không làm gì. Phương án khác: thêm dòng vào script role (đổi hạ tầng,
  ngoài phạm vi) hoặc bỏ `tenant_id` khỏi GIN (trái luật "index bắt đầu bằng `tenant_id`").
- **QĐ4 — Quyền của `crm_app` trên bảng mới** (kế hoạch chỉ ghi "GRANT cho crm_app"): `SELECT, INSERT` mọi bảng;
  `UPDATE` theo cột cho đúng các cột đường ghi M2 đổi (hồ sơ, chỉ số, `root_id`; địa chỉ; nhãn; quy tắc nhãn; hoàn tác
  gộp; trạng thái nhập); `DELETE` chỉ trên `customer_identities` (gỡ danh tính) và `customer_tag_links` (gỡ nhãn).
  `customer_notes` không `UPDATE`/`DELETE` (`REVOKE` + trigger). Không cấp `DELETE` trên `customers` (M1 cũng không);
  Task 66 tự thêm nếu cần xóa hồ sơ vừa chèn khi thua tranh chấp. Đổi lại: `GRANT`/`REVOKE` trong migration sau.
- **QĐ5 — `customers_base.sql`.** Kế hoạch tạo tệp nhưng không ghi câu. Đặt một câu đọc `GetCustomerBase` (mọi cột hồ
  sơ theo `id`) làm nền cho `Get` của Task 66.
- **QĐ6 — `Name.LogValue()` / `Birthday.LogValue()` trả `[Redacted]`** (cùng chữ của `logging.Phone`), không phải
  `[redacted]` như kế hoạch viết, để log có một dạng che duy nhất.

## Bằng chứng

Lệnh chung: `CRM_TEST_DATABASE_URL=postgres://crm_app:…@localhost:5600/crm go test -race -count=1 <gói> -run <test>`
(bộ Docker chung crm-shared, mỗi test một database riêng). Trước khi có mã: `internal/modules/customer` không biên dịch
(`undefined: customer.NormalizePhone`…), `TestSensitiveKey` đỏ (`"note" phải bị che`…); bỏ tạm `0201` thì
`TestSchemaExtendsM1Customers` (`column "code" does not exist`), `TestSchemaNotesAppendOnly`, `TestSchemaViewsAndRules`
đỏ. Sau khi viết: tất cả xanh (trích dưới).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `NormalizePhone`: 7 dạng hợp lệ → `+84…` (gồm `"0912 345 678"`, `"84912345678"`, `"+84 912.345.678"`, `"02838123456"` → `+842838123456`); 13 dạng sai (`"0912"`, `"912345678"`, `"0912a45678"`, chữ số toàn khổ…) → `ErrInvalidPhone` | `TestNormalizePhone` | `go test ./internal/modules/customer -run TestNormalizePhone` | `--- PASS: TestNormalizePhone` |
| `ReverseDigits("+84912345678") == "87654321948"` | `TestReverseDigits` | như trên `-run TestReverseDigits` | `--- PASS: TestReverseDigits` |
| `NormalizeName`: `"Nguyễn Thị Hồng Nhung"` → `"nguyen thi hong nhung"`, `"ĐẶNG  Văn   Ơn"` → `"dang van on"`, rỗng → rỗng, luôn bằng `textnorm.Fold` | `TestNormalizeName` | `-run TestNormalizeName` | `--- PASS: TestNormalizeName` |
| Log: `slog.Any("p", NormalizePhone(...))` không chứa `912345`; `Name`, `Birthday` che; khóa `full_name`, `birthday`, `recipient_name`, `email`, `note` lồng trong map bị che, khóa `city` giữ | `TestPersonalDataLogsRedacted`, `TestPersonalDataLogValue`, `TestSensitiveKey` | `go test ./internal/modules/customer ./internal/platform/logging` | `--- PASS` cả ba; `ok …/logging` |
| Mở rộng bảng M1: 3 khách tạo bằng `customer.Create` + 13 mẫu bỏ dấu trước 0201 → sau migrate `root_id = id`, `code` khớp `^KH\d{6,}$` và khác nhau, `name_search = textnorm.Fold(full_name)` (SQL = Go trên 11 mẫu khác rỗng của M1 Task 27, gồm mẫu NFD, cộng 2 mẫu M2-03); `customers_v` giữ 4 cột đầu cùng tên, kiểu; `customer.Create` sau migrate chạy (root_id do trigger, code từ sequence), câu đọc view của M1 chạy | `TestSchemaExtendsM1Customers` | `-run TestSchemaExtendsM1Customers` | `--- PASS: TestSchemaExtendsM1Customers (9.12s)` |
| Test của M1 Task 30 vẫn đạt | bộ `internal/modules/order` | `go test -race ./internal/modules/order/...` | `ok …/order 214.983s` |
| Chèn `customers` không `root_id` → `root_id = id`; hai danh tính `(phone, '', +84912345678)` khác khách → `23505`; `scope` NULL → `23502`; cùng SĐT ở tenant B được | `TestSchemaRootDefaultAndIdentityUnique` | `-run TestSchemaRootDefault` | `--- PASS` |
| `customer_notes`: `UPDATE`/`DELETE` bằng chủ bảng → lỗi chứa `append-only`; bằng `crm_app` → `42501 permission denied` | `TestSchemaNotesAppendOnly` | `-run TestSchemaNotesAppendOnly` | `--- PASS` |
| Tenant B không đọc dòng của A ở 8 bảng mới, 4 view mới và `customers_v`; `crm_app` của A đọc được | `TestSchemaTenantIsolation` | `-run TestSchemaTenantIsolation` | `--- PASS` |
| Năm view có `security_invoker`, cột đúng dòng `docs/views.md`; mọi index `customer.*` bắt đầu bằng `tenant_id`; `CheckSchemaRules` → `[]` | `TestSchemaViewsAndRules` | `-run TestSchemaViewsAndRules` | `--- PASS` |
| `CHECK` mới: `lifecycle` ngoài danh sách, chỉ số âm → `23514`; hằng `Lifecycle*`, `Identity*` khớp `CHECK`; màu nhãn ngoài danh sách → `23514` | `TestSchemaChecks` | `-run TestSchemaChecks` | `--- PASS` |
| Cổng kiểm của task (build, vet, lint, gen-check, sqlc-vet, catalog, jscpd, gitleaks, oasdiff, test-guard, test toàn bộ vì đổi migration và testkit) | toàn bộ | script cổng kiểm của task (lần 2, sau khi stage) | mọi cổng tĩnh qua; test: chỉ đỏ `webhooks` `TestHeadersStoredRedacted` (sửa như mục "Test bị xóa hoặc tắt", chạy lại `go test -race ./internal/platform/webhooks/` → `ok 31.277s`) và `tests/arch` `TestPackagePatternSkipsStrayDirs` (`parallel golangci-lint is running` — phiên khác chạy lint cùng lúc; chạy lại riêng → `ok`) |
| `viewsdoc_test` của M1 xanh (dòng `customers_v` 15 cột khớp view sau 0201; `ord.orders_v` vẫn 11 cột); dòng M2 đúng kế hoạch | `TestViewsDocMatchesDatabase`, `TestViewsDocParses`, `TestM2Contract_ViewsDoc` | `go test ./tests/arch -run 'TestM2Contract\|TestViewsDoc'` | `ok …/tests/arch` |

## Test bị xóa hoặc tắt

Không xóa, không tắt test nào. Hai assertion có sẵn đổi theo hành vi mới mà kế hoạch yêu cầu (không nới):
- `internal/platform/logging/redact_test.go`: `TestSensitiveKey` — `note` chuyển từ danh sách "không được che" sang
  "phải che" (Q21), thêm `full_name`, `recipient_name`, `birthday`, `email`… vào danh sách phải che, thêm `name`,
  `display_name` vào danh sách không che; `TestRedactNested` — khóa ví dụ "giữ nguyên" đổi từ `note` sang `status` (giữ
  đúng ý của test: khóa thường không bị che).
- `internal/platform/webhooks/ingest_test.go`: `TestHeadersStoredRedacted` — header ví dụ "giữ nguyên" đổi từ
  `X-Request-Note` sang `X-Request-Tag` (cùng ý; `note` nay là khóa bị che theo Q21). Phát hiện ở lần chạy cổng kiểm.
- `tests/arch/m2contract_test.go`: `TestM2Contract_ViewsDoc` — dòng `customer.customers_v` mong 15 cột (Task 59 ghi rõ
  dòng này sửa ở Task M2-03 cùng migration, R13).

## Khác kế hoạch

- Thêm ngoài danh sách kế hoạch, cùng hướng spec: `CHECK (code ~ '^KH[0-9]{6,}$')` (như mã đơn của M1); tên nhãn
  `CHECK` 1–50 ký tự (khớp `maxLength: 50` của `CreateCustomerTagRequest`); `CHECK` không âm cho số dòng của lần nhập,
  `errors` là mảng JSON; index phụ bắt đầu bằng `tenant_id`: `customer_addresses (tenant_id, customer_id)`,
  `customer_tag_links (tenant_id, tag_id)`, `customer_notes (tenant_id, customer_id, created_at)`,
  `customer_imports (tenant_id, created_at DESC)` (đường đọc của Task 66, 69, 70).
- Điền dữ liệu dòng có sẵn chạy bằng chủ bảng nên tạm `NO FORCE ROW LEVEL SECURITY` rồi `FORCE` lại trong cùng
  transaction migration (RLS ép cả chủ bảng; không thì `UPDATE` thấy 0 dòng). `CheckSchemaRules` sau migrate xác nhận
  RLS vẫn ép.
- Test "khách có sẵn trước 0201" chạy goose trên bộ tệp lọc `< 0201` ngay trong test (như `db.Migrate`), rồi
  `db.Migrate` thật; không thêm hàm xuất mới vào `internal/platform/db` (vùng rủi ro cao).

## Việc phát hiện thêm

- Mẫu khóa log mới che cả khóa `noteLength` trong audit nhà cung cấp (`handlers_inventory.go` `supplierAudit`), giống
  `hasPhone` vốn đã bị che từ trước: audit mất độ dài ghi chú (không lộ dữ liệu). Nếu cần giữ, đổi tên khóa (ví dụ
  `memoChars`) ở một task của `inventory`.
- Hợp đồng `TagColor` (Task 59) chỉ có `pattern` `^[a-z][a-z0-9_]{0,31}$`, rộng hơn `CHECK` 10 khóa màu (QĐ1): Task 69
  cần kiểm khóa màu trước khi ghi (trả 400 `invalid_input`) hoặc thêm `enum` vào hợp đồng.
- Cho tới Task 66 (`Create` thành lớp bọc), khách tạo bằng `customer.Create` của M1 có `name_search = ''` (kế hoạch giữ
  nguyên `Create` ở task này); tìm theo tên (Task 67) chạy sau Task 66 nên không bị ảnh hưởng.
- Máy chủ: `btree_gin` sẽ được tạo trong schema `platform` bởi `crm_owner` (QĐ3); nếu script dựng database của máy chủ
  khác dev (không cấp `CREATE ON DATABASE` cho `crm_owner`) thì quản trị cần tạo sẵn `btree_gin` trước lần migrate này.

## Reviewer

Kết luận: ĐẠT (lần 2, trên `git diff --cached` sau khi sửa `webhooks/ingest_test.go`).

- Lần 1: ĐẠT, không phát hiện phải sửa. Kiểm thêm theo Step R đều đạt: `UNIQUE (tenant_id, kind, scope, value)` và
  `scope NOT NULL DEFAULT ''` khớp 05 mục 1; mọi index (kể cả PK ghép) bắt đầu bằng `tenant_id`, có test quét
  `pg_index`; bảng M1 chỉ `ALTER` đúng luật mở rộng trước (`-- contract:` ngay trên `SET NOT NULL`, `CHECK` `NOT VALID`
  → `VALIDATE`, tên khóa ngoại `customers_tenant_id_root_id_fkey` đúng, `crm_app` có `USAGE` trên sequence). Ghi chú:
  mẫu khóa mới che `noteLength` của audit nhà cung cấp — đã thêm vào Ảnh hưởng và "Việc phát hiện thêm".
- Sau lần 1, cổng kiểm toàn bộ đỏ ở `TestHeadersStoredRedacted` (header ví dụ có chữ `note`) → đổi ví dụ.
- Lần 2: ĐẠT, không phát hiện phải sửa; xác nhận test webhook không bị nới (assertion giữ nguyên, header mới không khớp
  mẫu che). Ghi chú không bắt buộc (liệt kê `ingest_test.go` trong mục Ảnh hưởng) đã làm.
