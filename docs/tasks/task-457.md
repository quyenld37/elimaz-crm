# Task 457 — Nhật ký thao tác: API đọc audit

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md`, Task 457 (M5-45), làn L1 sau 455. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn (0 chỗ tham chiếu bị ảnh hưởng). Thêm
  mới trong `identity`: `AuditFilter`, `AuditCursor` (+ `String`), `ParseAuditCursor`, `AuditLog`, `ListAuditLogs`,
  `AuditPageSize`. Thêm `(*handler).ListAuditLogs` (hiện thực `gen.Handler` mới sinh).
- Gói phụ thuộc ngược (go list): gói nhập `internal/modules/identity`: `cmd/crm`, `internal/adapters/cfaccess`,
  `internal/httpapi`, `modules/catalog`, `modules/customer`, `platform/files`, `platform/realtime`(+`realtimetest`),
  `tools/loadkit` — chỉ thêm ký hiệu nên không gãy; ci-task test gói đổi + gói phụ thuộc.
- Operation OpenAPI bị chạm, oasdiff: thêm `listAuditLogs` `GET /api/v1/identity/audit-logs` (x-permission
  `audit.read`), schema mới `AuditLog`, `AuditLogPage`, `AuditActorType`. Chỉ thêm → oasdiff không báo phá vỡ. Không thêm
  mã lỗi mới (dùng `invalid_input` sẵn có qua `identity.ErrInvalidInput`).
- Chỗ dùng trên màn hình: chưa có (màn Nhật ký là Task 462). `console/src/api/**` sinh lại từ openapi (chỉ thêm kiểu).
- Câu sqlc, bảng, pha migration: câu mới `ListAuditLogs`, `AuditActorNames` trong `queries/identity/audit_list.sql`
  (đọc `identity.audit_logs`, `identity.users`, `identity.service_keys` — đều của identity). Migration
  `0527_audit_list_index.sql` pha mở rộng, chỉ thêm index trên `identity.audit_logs` (theo thời gian; theo hành động;
  theo người làm) vì danh sách không lọc theo thực thể không có index nào phục vụ `ORDER BY created_at DESC, id DESC`
  (index có sẵn `audit_logs_entity` chỉ cho lọc theo thực thể).
- Test sẽ chạy: test mới `identity` (`TestListAuditLogs_*`, `TestListAuditLogs_Plan`), httpapi
  `TestAuditLogsAPI_*`; ci-task chạy gói đổi + gói phụ thuộc (identity là vùng rủi ro cao → cả bộ identity).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (cursor, audit, explain);
  `identity.ListEntityAudit` (đọc audit theo một thực thể, tối đa 500, không trang); `customer.ListCursor`/`pageOf`
  (con trỏ (created_at, id) của góp ý); `queries.Cursor`, `order.ReturnCursor`; `testkit.ExplainAsApp`,
  `testkit.RequireNoSeqScan`, `testkit.OpenStaffSeeded`; httpapi `staffNames`, `catalogRead`, `couponEnv`.
- Dùng lại / viết mới vì: dùng lại `testkit.ExplainAsApp` + `RequireNoSeqScan` + `OpenStaffSeeded` cho đo kế hoạch,
  `catalogRead` (đọc trong tenant), `couponEnv` + `expect` cho test HTTP, `identity.ErrInvalidInput` (đã đổi sang 400).
  Viết mới con trỏ `AuditCursor` trong identity: `customer.ListCursor` nằm trong module customer (identity không được
  nhập customer — customer nhập identity, sẽ thành vòng); dời con trỏ chung vào `internal/platform` là đổi cấu trúc
  nhiều module, ngoài phạm vi (ghi ở Việc phát hiện thêm). Không dùng `ListEntityAudit`: khác mục đích (một thực thể,
  không trang, đọc `after.fields`). Không dùng `staffNames` của httpapi (gọi `GetStaff` từng người, không có tên khóa dịch
  vụ): tên người làm đọc bằng một câu `AuditActorNames` trong identity cho cả nhân viên và khóa dịch vụ.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Chủ dữ liệu: `identity.audit_logs`, `users`, `service_keys` đều của identity; hàm đọc đặt ở `identity`.
- 5.2 Không ghi bảng nào (chỉ đọc). 5.3 Không đọc bảng module khác.
- 5.4, 5.5, 5.6 không áp dụng: chỉ đọc, không sự kiện, không gọi ra ngoài.
- 5.7 Ngữ cảnh shop: đọc trong `tenancy.WithTenant` (`catalogRead`); RLS của `audit_logs` lọc tenant; test tenant khác
  không thấy dòng.
- 5.8 Module không nhập `net/http`/ogen; handler mỏng (đọc tham số → `identity.ListAuditLogs` → đổi kiểu).
- 5.9 Không có đường ghi (GET). Khai `x-permission: audit.read`.
- 5.10 Không dùng Valkey. 5.11 Không thư viện mới. 5.12 Theo module mẫu identity (sqlc, sentinel, test `_test`
  trên PostgreSQL thật); con trỏ theo mẫu góp ý (Task 455).
- 6.1–6.4 không áp dụng: không ghi, không khóa. 6.5 READ COMMITTED đủ: trang theo khóa (created_at, id) ổn định,
  dòng mới chèn trong lúc lật trang (mới hơn trang đầu) không làm trùng hay sót trang sau.
- 6.6–6.8, 6.10, 6.11 không áp dụng (không việc nền, không gọi ngoài, không tiền, không quy luật kho).
- 6.9 Treo lâu: câu theo index (EXPLAIN trên 100.000 dòng không Seq Scan), LIMIT 51; `db.TxAPI` có thời hạn câu.
- 6.12 Dữ liệu cá nhân: API không trả `before`, `after`, `ip` (chỉ tên trường đổi); test ghi audit có SĐT trong
  before/after và kiểm thân trả về không chứa giá trị. Không trả tên khách khi người làm là khách (chỉ loại + mã).
- 6.13 Quyền: `audit.read` (owner, manager); test bán hàng → 403.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| 120 dòng → 3 trang 50/50/20, không trùng không sót, mới nhất trước (40 dòng cùng mốc giờ: thứ tự theo id), con trỏ qua chuỗi | `identity_test.TestListAuditLogs_Pages` | `go test -race -count=1 -run '^(TestListAuditLogs_Pages\|TestListAuditLogs_Filters\|TestListAuditLogs_ActorNamesNoValues\|TestListAuditLogs_Plan)$' ./internal/modules/identity/` | `ok elimaz.com.vn/crm/internal/modules/identity 2.257s` |
| Lọc hành động, người làm, cả hai, loại thực thể, thực thể, từ, tới, khoảng giờ, không khớp (10 ca); tenant B không lẫn; entityId thiếu entityType, from = to, from > to → ErrInvalidInput; con trỏ hỏng (5 dạng) → ErrInvalidInput | `TestListAuditLogs_Filters` | như trên | `ok` |
| Tên người làm: nhân viên, khóa dịch vụ, hệ thống, mã lạ, mã không phải uuid, khách (không tên), loại service mang mã nhân viên; không giá trị SĐT/địa chỉ/IP, không trường before/after/ip; tên trường đổi `[address phone status]` | `TestListAuditLogs_ActorNamesNoValues` (+ `assertNoAuditValues` dùng chung, nay generic) | như trên | `ok` |
| 100.000 dòng: 9 dạng lọc không Seq Scan (`[Limit Index Scan]`, thực thể: `[Limit Incremental Sort Index Scan]`); lặp 3 lượt trên một kết nối: 36 kế hoạch theo giá trị, 0 kế hoạch chung | `TestListAuditLogs_Plan` | `go test -race -count=1 -run '^TestListAuditLogs_Plan$' -v ./internal/modules/identity/` | `kế hoạch theo giá trị: 36, kế hoạch chung: 0` · `--- PASS` |
| Index 0527 là cần: bỏ tạm 0527 → Seq Scan | `TestListAuditLogs_Plan` (chạy tay khi dời 0527 ra ngoài, rồi trả lại) | `go test -count=1 -run '^TestListAuditLogs_Plan$' -v ./internal/modules/identity/` | `kế hoạch có Seq Scan: [Limit Gather Merge Sort Seq Scan]` |
| HTTP: bán hàng → 403 forbidden; quản lý 120 dòng → 3 trang qua nextCursor; lọc người làm có `actorName`; thân không có SĐT, IP, `"before"`, `"after"`, `"ip"`; khoảng giờ [10, 13) phút → 3 dòng; dòng hệ thống không có actorId/actorName; con trỏ hỏng, from = to, entityId thiếu entityType → 400 invalid_input; quản lý tenant khác rỗng | `httpapi.TestAuditLogsAPI_List` | `go test -race -count=1 -run '^TestAuditLogsAPI_List$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.265s` |
| Cổng kiểm của task (gen-check, lint, jscpd, gitleaks, oasdiff, test-guard, console, test kiến trúc, test gói đổi + phụ thuộc) | — | `ci-task.sh` (log ngoài worktree) | `ok …/internal/httpapi 23.080s`, `ok …/internal/modules/identity 31.273s`, `CI-TASK XANH (so với điểm tách nhánh 8ae2543…)` |

## Test bị xóa hoặc tắt

(không có. `assertNoAuditValues` trong `audit_list_test.go` thành hàm generic và kiểm thêm khóa `ip` — chặt hơn, không nới.)

## Khác kế hoạch

- Thêm câu `AuditActorNames` vào `queries/identity/audit_list.sql` (kế hoạch chỉ nêu tệp) để trả tên người làm.
- Không có tham số `limit`: kế hoạch ghi "50 mỗi trang" cố định.

## Quyết định thay chủ shop

- **LIMIT hằng 51 trong SQL, không theo tham số.** LIMIT theo tham số làm kế hoạch chung (pgx giữ câu đã chuẩn bị theo
  kết nối) ước 10% bảng và chọn quét cả bảng. Phương án khác: SQL dựng lúc chạy (`*_dynsql.go`) — chắc hơn nhưng lệch
  danh sách tệp của kế hoạch; để dành nếu đo thật cho thấy cần.
- **Kiểm kế hoạch chung bằng hành vi thật** (`pg_prepared_statements`: 0 lần dùng kế hoạch chung sau 27 lần chạy trên
  100.000 dòng) thay vì ép `force_generic_plan`: với bộ lọc "NULL hoặc bằng", kế hoạch chung ép buộc vẫn quét bảng, nhưng
  PostgreSQL chỉ dùng kế hoạch chung khi nó rẻ ngang kế hoạch theo giá trị (bảng nhỏ), và lập lại khi thống kê đổi.
- **Ba index trong 0527** (thời gian; hành động; người làm, chỉ dòng có actor_id). Đo: bỏ 0527 → Seq Scan. Dễ đảo: thêm
  migration DROP INDEX.
- **entityId phải đi kèm entityType** (400 nếu thiếu): index thực thể bắt đầu bằng loại; màn Nhật ký luôn biết loại.
- **from ≥ to → 400** (khoảng rỗng là lỗi nhập, không trả trang rỗng).
- **Hệ thống không có tên trong API** (`actorType: system`, không `actorName`): màn hình (Task 462) tự ghi "Hệ thống"
  theo loại; API không trả chuỗi tiếng Việt cố định. Người làm là khách: chỉ loại + mã, không tên (dữ liệu cá nhân).
  Hãng (carrier): chỉ loại + mã.
- **Không trả `ip`, `request_id`** (IP là dữ liệu cá nhân của nhân viên).

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 457 không có e2e; đủ ca ở test gói identity và httpapi)

## Việc phát hiện thêm

- Con trỏ (created_at, id) nay có nhiều bản riêng (`customer.ListCursor`, `identity.AuditCursor`, `order.*Cursor`…): có
  thể gom vào một gói `internal/platform/<việc>` ở một task cấu trúc riêng.
- Các câu danh sách có bộ lọc "NULL hoặc bằng" và `LIMIT @lim` (vd. `customer.ListFeedback`) có cùng nguy cơ kế hoạch
  chung quét bảng khi bảng lớn; nên đo như `TestListAuditLogs_Plan`.

## Reviewer

Kết luận: ĐẠT (lần 1, trên phần đã stage sau ci-task xanh). Phát hiện phải sửa: không có. Ảnh hưởng khớp (tên enum
sinh ra là `AuditLogActorType`, mục Ảnh hưởng ghi `AuditActorType` — lệch tên, không ảnh hưởng). Bảng bằng chứng: mọi
dòng trỏ tới test có thật và kiểm đúng tiêu chí. Ghi chú không bắt buộc: `ParseAuditCursor` không chặn micro giây quá
lớn (con trỏ tự sửa kiểu `7fffffffffffffff_<uuid>` có thể ra 500 thay vì 400; con trỏ máy chủ phát không bao giờ như
vậy, người gọi cần `audit.read`) — chưa sửa, ghi lại để Task 462 hay task sau thêm mốc trên nếu cần; quyết định không
trả `actorName` cho dòng hệ thống đã ghi và có test.
