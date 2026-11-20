---
name: them-module
description: Các bước dựng một module nghiệp vụ mới của CRM theo mẫu catalog và inventory (schema, RLS, seller_id, view _v, sqlc, public API nhận tx, lỗi sentinel, handler mỏng, test trên PostgreSQL thật, thứ tự khóa ADR-46). Dùng khi một task tạo module mới trong internal/modules hoặc thêm bảng đầu tiên cho một schema mới.
---

# Thêm một module nghiệp vụ mới

**Bản nháp** (Task 405, M1-55d; Q30 kế hoạch M1). Agent không được sửa `.claude/`: chủ shop tự chép tệp này vào
`.claude/skills/them-module/SKILL.md` khi đồng ý. Tệp chỉ chỉ đường tới mã mẫu và luật đã có; luật gốc nằm ở chỗ được
dẫn, khác nhau thì chỗ được dẫn đúng.

Mẫu đọc trước: `internal/modules/catalog` (module có chiều người bán, đường ghi là `Writer`), `internal/modules/inventory`
(số dư đổi bằng câu UPDATE có điều kiện + một dòng sổ, thứ tự khóa), `internal/modules/identity` (module và đường đọc
đầu tiên, Task 7).

## 0. Trước khi viết

1. Module có trong bảng "Sở hữu" và cột "Được gọi" của spec `04-kien-truc.md` mục 3 chưa? Chưa có thì **dừng hỏi** (loại
   việc C, quy trình mục 3): thêm module là đổi thiết kế.
2. Tên gói là danh từ nói việc (`inventory`, `promotion`), không `util`, `common`, `shared`. Tên schema PostgreSQL theo
   05 quy ước chung (module `order` dùng schema `ord` vì `order` là từ khóa).
3. Đọc `docs/danh-muc-dung-chung.md` (hàm dùng chung có dòng "khi nào không dùng"): `tenancy`, `db.RetryTx`, `events`,
   `queue`, `commands`, `idempotency`, `textnorm`, `personal`, `testkit`.
4. Điền mục Ảnh hưởng của tệp task, kể cả: dòng mới trong `moduleCalls()` của `tests/arch/boundaries_test.go`, dòng
   trong `docs/adr/0046-thu-tu-khoa.md`, `docs/views.md`, `docs/quyen.md`. Các tệp viết tay này thường do **một** task
   đầu mốc viết trước cho mọi task của mốc (Q37 kế hoạch M1); task sau chỉ kiểm dòng đã có khớp.

## 1. Schema và migration

Mẫu: `migrations/0112_m1_schemas.sql` (tạo schema), `0114_catalog.sql`, `0115_inventory.sql`.

- Dải số migration theo mốc (M1 0100–0199, M2 02xx…). Pha **mở rộng**: chỉ `CREATE`/`ADD`. Lệnh thu hẹp cần dòng
  `-- contract: <bản đã ngừng dùng>` ngay trên nó. Không sửa migration đã commit.
- `CREATE SCHEMA <s>; GRANT USAGE ON SCHEMA <s> TO crm_app, crm_search;` — quyền bảng cấp **từng bảng**, không
  `ALTER DEFAULT PRIVILEGES`.
- Mỗi bảng có `tenant_id uuid NOT NULL REFERENCES platform.tenants (id)`, `UNIQUE (tenant_id, id)`, mọi unique khác bắt
  đầu bằng `tenant_id`, khóa ngoại tới bảng có tenant nối cả `tenant_id`; rồi `SELECT platform.enable_tenant_rls('<s>.<bảng>');`.
- Bảng có chủ là người bán (ADR-29): cột `seller_id`, mọi unique khác `(tenant_id, id)` có `seller_id` ngay sau
  `tenant_id` (miễn thì ghi chú `seller-exempt: <lý do>` trên index và dòng ở `docs/views.md`).
- Tiền là `bigint` đồng; số lượng có `CHECK` không âm ở database, không chỉ ở Go.
- `GRANT` hẹp nhất có thể: `crm_app` chỉ `INSERT` các cột được chèn, `UPDATE` các cột được đổi (mẫu: migration 0132
  của Task 414 cho `stock_balances`, `stock_reservations`). Bảng chỉ ghi thêm (sổ) không có `UPDATE`, `DELETE`.
- Kiểm: `tenancy.CheckSchemaRules` (luật a–g) chạy trong test của `internal/platform/tenancy`; sai một luật là đỏ.

## 2. View công bố cho module khác

- Module khác **không** đọc bảng của module này; chỉ đọc view `<s>.<tên>_v` `WITH (security_invoker = true)`,
  `GRANT SELECT … TO crm_app`. Thêm dòng đúng thứ tự cột vào `docs/views.md` trong cùng commit
  (`tests/arch/viewsdoc_test.go`); mở rộng sau này chỉ thêm cột vào cuối.
- Màn danh sách ghép dữ liệu nhiều module đọc view qua gói `internal/queries` (`*_dynsql.go` khi SQL dựng lúc chạy,
  có `*_dynsql_test.go` trên PostgreSQL thật và `explain_test.go` cho kế hoạch chạy).

## 3. Câu SQL và sqlc

- Câu tĩnh ở `queries/<s>/*.sql`; thêm một khối trong `sqlc.yaml` (sao khối `catalog`, đổi `queries`, `out:
  internal/modules/<module>/internal/store`); `make gen`. Không sửa tay `internal/store/**`.
- Mọi câu đọc của module có người bán lọc `seller_id` (`tests/arch/sellerfilter_test.go`).
- Đổi số dư, trạng thái: **một câu UPDATE có điều kiện** (`… WHERE on_hand + $delta >= held`) trả số dòng; 0 dòng →
  sentinel lỗi. Không đọc số ra Go rồi ghi lại (mẫu `inventory/movement.go`, `applyDelta`).
- Không dùng simple protocol của pgx; luật `sqlc/db-prepare` chạy trong `make sqlc-vet`.

## 4. Thứ tự khóa (ADR-46)

- Xếp mọi bảng mới vào `docs/adr/0046-thu-tu-khoa.md` (nhóm, thứ tự trong nhóm). Trong một transaction khóa nhóm số nhỏ
  trước; nhiều dòng cùng bảng thì theo khóa tăng dần (mẫu: số dư theo `(variant_id, warehouse_id)` rồi giữ hàng theo
  `id`, chú thích đầu `inventory/inventory.go`).
- Có test hai đường song song chạm cùng các dòng theo hai thứ tự đầu vào khác nhau, không deadlock
  (`inventory/deadlock_test.go`, `TestReserve_NoDeadlockOnCrossOrder`); `pg_stat_database.deadlocks` không tăng.

## 5. Public API của module

- Gói gốc chỉ có kiểu, sentinel lỗi (`errors.go`: `var ErrX = errors.New("<module>: …")`), hàm đọc nhận
  `(ctx, tx pgx.Tx, sellerID, …)`, và một kiểu `Writer` giữ phụ thuộc (`Events`, `Queue`, `Realtime`, `RequestID`): mọi
  đường ghi là phương thức của `Writer`, chạy trong **transaction của bên gọi**, tự ghi sự kiện (`events.Registry.Emit`),
  báo hiệu realtime và `identity.RecordAudit` cùng transaction. Thiếu phụ thuộc → trả lỗi (`errWriterNotWired` ở catalog, `errNotWired` ở inventory), không panic.
- Sửa dòng có `version`: khóa dòng (`FOR UPDATE`) rồi so `version`, lệch → `ErrVersionConflict` (409).
- Hàm chạy trong `db.RetryTx` có thể chạy lại: gán lại mọi biến kết quả trong thân hàm, không gọi mạng, không gửi ra
  ngoài. Việc gọi mạng là **lệnh** `commands` (`Enqueue` trong transaction, gọi ngoài transaction, kết quả ghi ở
  `OnDone`; không chắc đã gửi → `in_doubt`).
- Phần riêng (`internal/…`) không nhập được từ module khác. Module không nhập `net/http`, `ogen`; gọi dịch vụ ngoài qua
  interface do module khai, bộ nối ở `internal/adapters/<tên>`.
- Đầu tệp gốc: chú thích gói có "Phần công khai" và "Khi nào không dùng" (danh mục dùng chung đọc từ `go doc`;
  `make catalog`).
- Dữ liệu cá nhân (SĐT, địa chỉ, nội dung tin) dùng kiểu có `LogValue()` tự che; không vào log, audit, sự kiện.

## 6. Quyền và HTTP

- Quyền mới: dòng trong `internal/modules/identity/permissions.go` (kèm loại phạm vi), dòng `docs/quyen.md`, vai trò mẫu nhận quyền ở
  `EnsureDefaultRoles`. Quyền theo phạm vi (kho, người bán) kiểm **cả** ở handler cho đối tượng thật (kho giữ hàng của
  đơn, không chỉ kho trong yêu cầu — Task 411).
- Operation trong `openapi/openapi.yaml` có `x-permission`; đường ghi có tham số `IdempotencyKey`. `make gen`.
- Handler mỏng ở `internal/httpapi/handlers_<module>.go`: đổi kiểu `gen.*` ↔ kiểu module, mở transaction ghi (mẫu
  `catalogWriteAsDefaultSeller`, `WriteTx` của `handlers_jobs.go`), gọi `Writer`, trả `&gen.X{…}`. Không có luật nghiệp vụ.
- Sentinel lỗi mới → mã HTTP ở **một chỗ** `internal/httpapi/errors.go` (`tests/arch/errors_mapped_test.go` đỏ khi
  thiếu); body `{"error": "<mã>", "request_id": "<id>"}`. Màn hình có câu tiếng Việt cho mọi mã mới.

## 7. Test

- Gói `_test` trên PostgreSQL thật: `testkit.OpenStaff(t)` (database nhân bản từ mẫu đã migrate), dữ liệu dựng bằng
  bộ seed của module (`catalogtest`, `invtest.NewShop`, `testkit.SeedSellers`); module khác nạp dữ liệu của module này
  qua bộ seed đó, không chèn thẳng bảng.
- Mỗi luật: một ca đúng, một ca biên, một ca sai; tenant khác thấy 0 dòng/404; mọi câu kiểm trong test cũng chạy trong
  `tenancy.WithTenant` (RLS áp cả chủ bảng). Module có người bán: chạy ca chính trong `testkit.ForEachSeller`.
- Quy luật (không âm, sổ cộng ra số dư, tổng bút toán bằng 0…) bằng `rapid` (mẫu `inventory/property_test.go`).
- Đường ghi HTTP: 50 yêu cầu song song cùng `Idempotency-Key` → một kết quả; `version` cũ → 409; thiếu quyền → 403.
- Không lấy giờ thật làm tiêu chí đúng/sai (trần thời gian để nhãn `perf`); chờ theo sự kiện, không `sleep`.
- Module thuộc vùng rủi ro cao (tiền, tồn, quyền, tệp ngoài): đề xuất thêm vào `HIGH_RISK_DIRS` của
  `scripts/agent/lib.sh` (chủ shop hoặc điều phối viên sửa), chạy `make mutation` ≥ 80%.

## 8. Trước khi báo xong

- `make gen` không chênh, `scripts/agent/ci-task.sh` xanh, `CheckSchemaRules` không vi phạm, `moduleCalls()` khớp mã.
- Tệp task: Ảnh hưởng, Tái dùng, Soát thiết kế (5.x, 6.x phần chạm), bảng bằng chứng tên test khớp mã thật.
- Stage từng tệp, chạy agent `reviewer`, commit theo `CLAUDE.md`.
