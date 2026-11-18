# Task 08 — Nhật ký thao tác chỉ ghi thêm

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md` (Task M0-08). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ thêm mới trong `internal/modules/identity`: `AuditEntry`,
  `RecordAudit`, `ErrInvalidAudit`. Không sửa, không đổi tên ký hiệu nào có sẵn → 0 chỗ tham chiếu cũ bị ảnh hưởng.
  Trong M0 chưa có đường nào gọi `RecordAudit` (Task 12 là người gọi đầu tiên).
- Gói phụ thuộc ngược (go list): `internal/modules/identity` được nhập bởi `cmd/crm`, `internal/adapters/cfaccess`,
  `internal/httpapi`. Chỉ thêm ký hiệu nên các gói này không đổi hành vi.
- Operation OpenAPI bị chạm, oasdiff: không có (task không có route; đọc audit qua API là việc sau).
- Chỗ dùng trên màn hình: không có (chưa có console).
- Câu sqlc, bảng, pha migration: mới `queries/identity/audit.sql` (`InsertAuditLog :exec`) → sinh lại
  `internal/modules/identity/internal/store` bằng `make gen`. Bảng mới `identity.audit_logs`, migration
  `0004_audit.sql`, pha **mở rộng** (chỉ tạo bảng, hàm trigger, index, quyền; không lệnh thu hẹp). Bảng đã có trong
  `docs/adr/0046-thu-tu-khoa.md`, mục "Bảng M0 ngoài thứ tự nhóm" (a) chỉ chèn.
- Test sẽ chạy: identity là vùng rủi ro cao → cả bộ `go test -race ./internal/modules/identity/...`; gói phụ thuộc
  ngược `./internal/httpapi/... ./internal/adapters/... ./cmd/...`; `./internal/platform/tenancy/...` (CheckSchemaRules
  trên schema thật có bảng mới), `./internal/platform/db/...` (migrate), `./tests/arch/...` (sqlc vet, ranh giới, SQL).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` có
  `logging.Redact(v, maxDepth)` và `logging.SensitiveKey(key)`; tìm `redact|sensitive|changed|diff|audit` trong
  `internal/**`: không có hàm so khác nhau giữa hai map. `tenancy.WithTenant` lo ngữ cảnh tenant (bên gọi mở tx);
  `platform.enable_tenant_rls` (migration 0002) bật RLS.
- Dùng lại: `logging.Redact` để che giá trị (nó đã đi sâu vào map, slice, struct, kiểu `LogValuer` như
  `logging.Phone`, và dùng đúng `SensitiveKey`), `platform.enable_tenant_rls` cho RLS, mẫu module identity (store
  sqlc riêng, lỗi sentinel trong module). Viết mới: `changedFields` (so từng khóa cấp đầu của Before/After theo dạng
  JSON) — không có sẵn; chỉ audit dùng nên ở lại trong identity, không đưa lên platform.

## Soát thiết kế

5.1 Chủ dữ liệu: audit thuộc identity (plan, Cấu trúc file; spec 05 mục bảng `audit_logs`), schema `identity`.
5.2 Không ghi bảng module khác: chỉ ghi `identity.audit_logs`. Module khác gọi API công khai `identity.RecordAudit`.
5.3 không áp dụng: không đọc dữ liệu module khác.
5.4 Nguyên tử: `RecordAudit` nhận `pgx.Tx` của bên gọi, ghi trong cùng transaction với thay đổi (spec 04 mục 11);
transaction lỗi thì không có dòng audit (test).
5.5 không áp dụng: không có việc sau commit.
5.6 không áp dụng: không gọi ra ngoài.
5.7 Bảng có `tenant_id NOT NULL`, khóa ngoại tới `platform.tenants`, `UNIQUE (tenant_id, id)`, RLS ép qua
`enable_tenant_rls`; `CheckSchemaRules` trên schema thật phải rỗng. Bảng có RLS nên **không** vào danh sách
`platformTables` (danh sách đó là bảng nền tảng không RLS được miễn kiểm) — xem Quyết định thay chủ shop.
5.8 Module không nhập `net/http`; IP nhận dạng chuỗi, kiểm bằng `net/netip`.
5.9 không áp dụng: không có đường ghi HTTP trong task này.
5.10 không áp dụng: không có Valkey.
5.11 Không thêm thư viện (chỉ stdlib, pgx, uuid, sqlc đã có).
5.12 Theo module mẫu identity: câu SQL trong `queries/identity`, store riêng, sentinel trong module, test gói `_test`
trên PostgreSQL thật (`testkit.OpenStaff`), quy luật bằng rapid.

6.1 Hai yêu cầu cùng lúc: chỉ `INSERT`, không khóa dòng có sẵn, không unique ngoài khóa chính → không tranh chấp.
6.2 Gửi lại: chống trùng do Task 9 (khóa ghi "đã xong" cùng transaction); audit nằm trong transaction đó nên lần phát
lại không ghi thêm dòng. Không áp dụng trong task này.
6.3 Thứ tự khóa: nhóm (a) "chỉ chèn" của ADR-46; INSERT không khóa dòng khác ngoài khóa chia sẻ trên dòng
`platform.tenants` do khóa ngoại (FOR KEY SHARE, không xung đột với khóa khác cùng loại).
6.4 Lỗi tạm: `RecordAudit` chạy trong tx của `db.RetryTx` của bên gọi; không tự thử lại; hàm chạy lại được (không
cộng dồn biến ngoài).
6.5 READ COMMITTED mặc định; không cần gì thêm.
6.6–6.8 không áp dụng: không có việc nền, không gọi bên ngoài.
6.9 Giới hạn thời gian theo tx của bên gọi; câu INSERT một dòng.
6.10 không áp dụng: không có tiền.
6.11 Quy luật (rapid, vùng rủi ro cao): (1) `changed_fields` đúng bằng tập khóa có giá trị khác nhau; (2) không giá
trị nào nằm dưới khóa nhạy cảm (ở mọi cấp) xuất hiện trong JSON lưu; (3) giá trị không nhạy cảm cấp đầu được giữ.
6.12 Dữ liệu cá nhân: khóa khớp `logging.SensitiveKey` (và kiểu tự che như `logging.Phone`) lưu "[Redacted]", tên
trường vẫn có trong `changed_fields` (spec 04 mục 11: ghi tên trường đã đổi, không ghi giá trị). Lỗi trả về không
chứa giá trị của Before/After.
6.13 Quyền: `crm_app` chỉ có `SELECT, INSERT`; `UPDATE, DELETE, TRUNCATE` bị thu; trigger chặn cả chủ bảng (`UPDATE`,
`DELETE`, `TRUNCATE`). Đọc audit qua API (`audit.read`) là việc sau.

## Quyết định thay chủ shop

1. **Danh sách `platformTables` trong `schemarules_test.go`:** ghi chú điều phối nói "thêm bảng audit". Danh sách đó
   (cả trong `tenancy` và `identity`) là *bảng nền tảng không RLS được miễn kiểm*; `audit_logs` có RLS (Global
   Constraints của kế hoạch). Thêm vào sẽ tắt kiểm RLS của chính bảng này. Chọn: **không thêm**; thay vào đó
   `TestSchemaRulesStillClean` (identity) và `TestCheckSchemaRulesCleanSchema` (tenancy) kiểm `audit_logs` như mọi
   bảng nghiệp vụ, cộng một test riêng xác nhận `audit_logs` bật và ép RLS. Phương án khác: thêm vào danh sách miễn
   (bị loại: nới một cổng kiểm). Dễ đảo ngược: một dòng.
2. **Trigger chạy theo câu lệnh (`FOR EACH STATEMENT`) và chặn cả `TRUNCATE`:** kế hoạch ghi `BEFORE UPDATE OR
   DELETE`. Trigger theo dòng không chạy khi RLS lọc hết dòng (chủ bảng ngoài ngữ cảnh tenant), và `TRUNCATE` bỏ qua
   trigger theo dòng. Theo câu lệnh thì mọi câu `UPDATE/DELETE/TRUNCATE` đều bị chặn. Chặt hơn kế hoạch, không trái.
3. **"Bỏ giá trị" của khóa nhạy cảm = giữ khóa, giá trị thành `"[Redacted]"`** (đúng cách `logging.Redact` làm), để
   người đọc audit thấy trường có mặt. Phương án khác: xóa hẳn khóa (mất thông tin trường có trong bản ghi).
4. **`changed_fields`** là khóa cấp đầu, xếp theo chữ; hai giá trị bằng nhau khi dạng JSON giống nhau (1 và 1.0 là
   một). Khóa chỉ có ở một bên cũng là khác. Before nil (tạo mới) → mọi khóa của After.
5. **Kiểm đầu vào bằng `ErrInvalidAudit`:** thiếu tenant, `actor_type` ngoài danh sách, `action`/`entity_type` rỗng,
   `IP` không phải địa chỉ (nhận cả dạng `ip:port`, lưu phần ip) → lỗi, không ghi. Chuỗi rỗng của `actor_id`,
   `entity_id`, `ip`, `request_id` lưu NULL. Thêm `CHECK (action <> '')`, `CHECK (entity_type <> '')`, khóa ngoại
   `tenant_id → platform.tenants` (như các bảng identity khác).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đỏ trước khi có mã | cả tệp `audit_test.go` | `go test ./internal/modules/identity/` (trước `audit.go`, migration) | `FAIL ... [build failed]: undefined: auditPayload` |
| `changed_fields` có `phone`, `status`; JSON dòng không chứa `0911`, `0922`; `after.status = confirmed`; cột khác đúng | `TestRecordAuditStoresFieldNamesNotPersonalValues` | `go test -race -count=1 -v -run 'Audit\|SchemaRulesStillClean' ./internal/modules/identity/` | `--- PASS (0.26s)` |
| `RecordAudit` rồi transaction lỗi → 0 dòng | `TestRecordAuditRolledBackWithTransaction` | như trên | `--- PASS (0.15s)` |
| `UPDATE`/`DELETE`/`TRUNCATE` bằng chủ bảng (trong và ngoài ngữ cảnh tenant) → `append-only`; bằng `crm_app` → `permission denied`; còn đúng 1 dòng | `TestAuditLogsAppendOnly` | như trên | `--- PASS (0.15s)` |
| Tenant B không đọc dòng của A; ngoài ngữ cảnh 0 dòng; ghi dòng A trong ngữ cảnh B → vi phạm RLS | `TestAuditTenantIsolation` | như trên | `--- PASS (0.14s)` |
| Đầu vào sai → `ErrInvalidAudit`, không ghi, lỗi không chứa giá trị; cột tùy chọn rỗng → NULL; đủ 5 `actor_type`; IP `[v6]:port` | `TestRecordAuditValidation` | như trên | `--- PASS (0.15s)` |
| Bảng audit bật và ép RLS, không nằm trong danh sách miễn; schema thật qua mọi quy tắc | `TestAuditLogsRLSForced`, `TestSchemaRulesStillClean`, `TestCheckSchemaRulesCleanSchema` | như trên; `go test -race -count=1 -run TestCheckSchemaRulesCleanSchema ./internal/platform/tenancy/` | `--- PASS`; `ok tenancy 1.507s` |
| Ca biên của `changed_fields` (tạo mới, xóa, 1 = 1.0, khóa một bên, lồng nhau); `logging.Phone` không lộ | `TestAuditChangedFieldsEdges` | như trên | `--- PASS` |
| Quy luật rapid (200 lượt): `changed_fields` đúng mô hình; không giá trị dưới khóa nhạy cảm nào lọt vào JSON; giá trị thường được giữ | `TestAuditPayloadProperty` | như trên | `--- PASS (0.12s)` |
| Test không rỗng (đột biến thử tay, đã trả lại mã): bỏ che → đỏ; bỏ "khóa chỉ một bên" → đỏ | `TestAuditPayloadProperty`, `TestRecordAuditStoresFieldNamesNotPersonalValues`, `TestAuditChangedFieldsEdges` | `go test -count=1 -run 'Property\|Stores'` / `-run 'Property\|Edges'` trên mã đã đột biến | `--- FAIL` ở cả hai lần |
| Gói phụ thuộc và gói liên quan | cả bộ | `go test -race -count=1 ./internal/modules/identity/... ./internal/platform/tenancy/... ./internal/platform/db/... ./internal/httpapi/... ./internal/adapters/... ./cmd/... ./tests/...` | `ok` cho identity, tenancy, db, httpapi, cfaccess, tests/arch, tests/e2e |
| Cổng tĩnh | — | `go build ./... && go vet ./...`, `golangci-lint run ./...`, `make sqlc-vet catalog-check jscpd test-guard gitleaks oasdiff` | `0 issues.`; `ok tests/arch`; jscpd không còn clone mới; gitleaks `no leaks found`; các mục khác không lỗi |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

Xem "Quyết định thay chủ shop" mục 2, 5 (chặt hơn kế hoạch). Thêm `internal/modules/identity/export_test.go` (mở
`auditPayload` cho test quy luật, không vào mã chạy). `make gen` cũng sinh lại `internal/platform/internal/store/models.go`
(sqlc đọc chung thư mục migrations nên mọi store có kiểu `IdentityAuditLog`), như Task 7 đã có với bảng identity.

## Việc phát hiện thêm

- Database dev cục bộ đã chạy tới 0006 (Task 10 gộp trước) sẽ bị goose từ chối 0004 vì M0 không bật
  `WithAllowOutofOrder` (đúng kế hoạch). Database thử dựng mới mỗi lần nên không ảnh hưởng; chưa có môi trường nào
  deploy. Cách xử lý: dựng lại database dev (`make db-down`/`db-up` của dự án compose đó). Ghi để điều phối viên biết.
- Người gọi `RecordAudit` (từ Task 12) không được đặt dữ liệu cá nhân vào `ActorID`, `EntityID`, `RequestID` (lưu
  nguyên, không che).
- `logging.Redact` giữ nguyên `err.Error()` và kết quả `MarshalText` dưới khóa không nhạy cảm: nếu Before/After chứa
  `error` hay kiểu TextMarshaler mang dữ liệu cá nhân thì sẽ lưu thô. Thuộc gói `logging`, không sửa trong task này.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, một lượt, trên `git diff --cached` của 9 tệp).

Không có phát hiện phải sửa. Reviewer xác nhận: trigger theo câu lệnh chặn UPDATE/DELETE/TRUNCATE kể cả chủ bảng và khi
RLS lọc hết dòng; crm_app chỉ SELECT, INSERT; RLS ép, không vào danh sách miễn; giá trị nhạy cảm che ở mọi cấp qua
`logging.Redact`, lỗi không mang giá trị; `changed_fields` đúng nghĩa; mô hình của test rapid viết độc lập với mã.
Ba ghi chú không bắt buộc đã chép vào "Việc phát hiện thêm" (goose ngoài thứ tự trên database dev cũ; người gọi không
đặt dữ liệu cá nhân vào ActorID/EntityID/RequestID; `logging.Redact` giữ nguyên `error`/TextMarshaler).
