# Task 425 — Lịch sử đơn mất lần sửa mới khi > 500 audit; test dọn DB thử chập chờn (M2-24b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 425, M2-24b). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào. `identity.ListEntityAudit` giữ chữ ký,
  đổi thứ tự trả về (mới nhất trước) và trần 500 áp lên phần **mới nhất**; chỗ gọi (grep): `internal/httpapi/handlers_order_read.go:145`
  (`orderAudit`), `internal/modules/order/timeline_test.go:22` (`auditSource`), `internal/modules/identity/audit_list_test.go`.
  `order.Timeline` tự sắp lại toàn bộ mục nên thứ tự đầu vào không ảnh hưởng kết quả. Hàm không xuất của testkit:
  `cleanupStale`, `dropStale` thêm tham số khóa (kiểu `cleanupLocks`); chỗ gọi: `pgtemplate.go` (cloneFromTemplate),
  `pgseed.go` (cloneSeeded), `pgtemplate_test.go`, `pgseed_test.go`.
- Gói phụ thuộc ngược (go list): `identity` được hầu hết gói nhập (vùng rủi ro cao → cổng kiểm chạy cả bộ identity);
  hành vi chỉ đổi ở câu `ListEntityAudit`, người dùng duy nhất là `httpapi` (lịch sử đơn). `testkit` được 33 gói nhập
  (test); chỉ đổi hàm không xuất, đường dọn của `StartPostgres`/`StartSeeded` dùng đúng khóa máy chủ như cũ.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. `GET /api/v1/orders/{id}/timeline` đổi dữ liệu trả về
  (có lần sửa mới nhất khi đơn > 500 audit), không đổi dạng.
- Test đỏ sẵn trên `main` bị hook chặn: `internal/httpapi/errors_m2_test.go` › `TestM2Routes_Unimplemented` (xem
  "Quyết định thay chủ shop", "Test bị xóa hoặc tắt"); dùng `writeHandlerError`, `requestID` (không xuất) của httpapi,
  không đổi mã chạy.
- Chỗ dùng trên màn hình: màn chi tiết đơn, thẻ lịch sử thao tác — không sửa console.
- Câu sqlc, bảng, pha migration: `queries/identity/audit.sql` › `ListEntityAudit`: `ORDER BY created_at DESC, id DESC
  LIMIT 500` (khớp chỉ mục `audit_logs_entity (tenant_id, entity_type, entity_id, created_at DESC)`); `make gen` sinh
  lại `internal/modules/identity/internal/store`. Bảng `identity.audit_logs` không đổi; không migration.
- Test sẽ chạy: `go test -race ./internal/modules/identity/... ./internal/modules/order/... ./internal/httpapi/...
  ./internal/testkit/...`; lặp `-run TestCleanupStale -count=50` khi có phiên khác dùng crm-shared và khi một kết nối
  khác giữ khóa dọn của máy chủ; rồi cổng kiểm `ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (chỉ liệt kê
  `internal/platform`; hàm audit của identity không có trong đó) và grep theo tên `ListEntityAudit`, `cleanupStale`. Lịch sử thao tác đơn không có tham số trang trong hợp đồng
  (`GetOrderTimeline` chỉ nhận `OrderID`), nên không dùng con trỏ keyset của các danh sách khác.
- Dùng lại / viết mới vì: sửa tại chỗ câu `ListEntityAudit` (trần áp lên phần mới nhất — phương án thứ hai kế hoạch
  cho phép), không thêm hàm. testkit: thêm kiểu nhỏ không xuất `cleanupLocks` (khóa dọn, khóa dựng mẫu) để test dùng
  khóa riêng của lần chạy; không có sẵn thứ tương tự.

## Soát thiết kế

- 5.1–5.3: audit thuộc identity; order đọc qua public API `identity.ListEntityAudit` do lớp HTTP nối (như cũ), không
  đọc thẳng bảng. 5.4–5.6, 5.9–5.11: không áp dụng (chỉ đọc, không ghi, không gọi ngoài, không thư viện mới).
  5.7: câu chạy trong tx tenant của `readAsDefaultSeller` (RLS); test đọc trong `tenancy.WithTenant`. 5.8: không đổi.
  5.12: test theo mẫu `timeline_test.go`, `audit_list_test.go`.
- 6.1–6.8, 6.10, 6.11, 6.13: không áp dụng (chỉ đọc; không tiền, quyền, hàng đợi). 6.9: câu đọc theo chỉ mục
  `audit_logs_entity` (`created_at DESC`), trần 500 dòng giữ thời gian câu như cũ. 6.12: `ListedFields` vẫn chỉ là
  tên trường, không giá trị before/after.
- testkit (đồng thời giữa các phiên): test dọn hiện giữ khóa advisory **chung của cả máy chủ** (727391 dựng mẫu, 727392
  dọn) bằng `pg_advisory_lock` (chờ), trong hạn 1 phút; phiên khác đang dựng mẫu (migrate) hay đang dọn làm test hết
  giờ. Sửa: `cleanupStale` nhận khóa qua tham số; đường thật truyền khóa máy chủ (không đổi hành vi), test truyền cặp
  khóa ngẫu nhiên riêng của lần chạy → không chờ khóa chung; không nới tiêu chí, không retry. Database của test vẫn
  theo tiền tố riêng ngẫu nhiên nên không chạm database của phiên khác.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (1) Đơn > 500 audit vẫn hiện lần sửa mới nhất — đỏ trước khi sửa | `order_test.TestTimeline_ManyAuditsKeepsLatestEdit` (500 audit sửa `shipAddress` + 1 audit sửa `lines` sau cùng của người khác) | `go test -race -run TestTimeline_ManyAuditsKeepsLatestEdit ./internal/modules/order/` (câu SQL cũ) | `FAIL … mục sửa mới nhất: map[action:order.update shipAddress:changed] … muốn map[action:order.update lines:changed] …; 500 mục sửa` |
| (1) qua sau khi sửa | cùng test | `go test -race -run 'TestTimeline' ./internal/modules/order/` | `ok  elimaz.com.vn/crm/internal/modules/order 2.476s` |
| (1) `ListEntityAudit` trả 500 dòng **mới nhất**, mới nhất trước; dòng cũ nhất bị bỏ | `identity_test.TestListEntityAudit_KeepsNewest500`; `TestListEntityAudit` (thứ tự đổi thành mới nhất trước) | `go test -race -run 'TestListEntityAudit' ./internal/modules/identity/` | `ok  elimaz.com.vn/crm/internal/modules/identity 2.555s` |
| (2) Test dọn đỏ khi phiên khác giữ khóa chung — tái hiện trước khi sửa | `testkit.TestCleanupStaleDropsOnlyIdleOldDatabases` (bản cũ), một kết nối psql khác giữ `pg_advisory_lock(727392)` | `go test -race -count=1 -run TestCleanupStaleDropsOnlyIdleOldDatabases ./internal/testkit/` | `FAIL (60.28s) pgtemplate_test.go:238: testkit: SELECT pg_advisory_lock(727392): timeout: context deadline exceeded` |
| (2) Lặp ≥ 50 lần khi khóa chung bị giữ: đều qua | `TestCleanupStaleDropsOnlyIdleOldDatabases`, `TestCleanupStaleSkipsSeededTemplateBeingBuilt` (khóa riêng `privateLocks`) | psql khác giữ `pg_advisory_lock(727392)` suốt lúc chạy; `go test -race -count=50 -v -run 'TestCleanupStale' ./internal/testkit/` | `--- PASS` 100, `--- FAIL` 0; `ok … internal/testkit 25.143s` |
| (2) Lặp ≥ 50 lần khi tiến trình test khác chạy cùng máy chủ | như trên | song song `go test -race ./internal/modules/order/... ./internal/modules/identity/... ./internal/httpapi/...` (dựng, nhân bản, dọn database thử) và `go test -race -count=50 -v -run 'TestCleanupStale' ./internal/testkit/` | `--- PASS` 100, `--- FAIL` 0; `ok … internal/testkit 35.071s` |
| `httpapi` hết đỏ sẵn | `TestM2Routes_Unimplemented` | `go test -race -count=1 -run TestM2Routes_Unimplemented ./internal/httpapi/` | `ok  elimaz.com.vn/crm/internal/httpapi 2.185s` |
| Cổng kiểm của task (sau khi gộp `main`, đã stage) | cổng tĩnh + test toàn bộ (đổi testkit) | `ci-task.sh` (lần 2) | build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard: qua. Test toàn bộ: mọi gói `ok` trừ 2 test chập chờn ngoài phạm vi (dưới) |
| Hai test đỏ trong lần chạy toàn bộ là chập chờn của máy chủ chung | `observability.TestFileSyncRole` (`53300 too many connections for role "crm_filesync"`, `rolconnlimit` = 1, các phiên dùng chung), `loadkit.TestReadStats` (`dead = 0, muốn ≥ 1`) | `go test -race -count=3 -run TestFileSyncRole ./internal/platform/observability/`; `go test -race -count=3 -run TestReadStats ./tools/loadkit/` | `ok … observability 2.003s`; `ok … tools/loadkit 1.701s` |

## Test bị xóa hoặc tắt

(không có). `TestListEntityAudit` đổi thứ tự kiểm (mới nhất trước) theo hành vi mới; vẫn kiểm đủ ba dòng, từng
trường, không giá trị before/after. Hai test dọn bỏ việc giữ khóa chung của máy chủ (không còn cần: dùng khóa riêng),
tiêu chí giữ nguyên.

`httpapi` › `TestM2Routes_Unimplemented` (đỏ sẵn trên `main`): ca "chủ, route M2 chưa làm → 501" không còn route nào
để gọi (route dòng thời gian đơn đã làm ở Task 81; mọi operation đều có handler). Không bỏ assertion 501: chuyển sang
gọi thẳng `writeHandlerError` qua middleware `requestID` thật với lỗi bọc `ht.ErrNotImplemented` (đúng đường ogen trả
cho operation chưa làm) → vẫn kiểm 501 `not_implemented` có `request_id`. Các ca 401, 403, 400 giữ nguyên trên route
dòng thời gian đơn; thêm ca chủ gọi đơn không có → 404 `not_found`.

## Khác kế hoạch

(không có)

## Quyết định thay chủ shop

- (1) Chọn "trần áp lên phần mới nhất" (đọc 500 dòng audit mới nhất, `ORDER BY created_at DESC, id DESC`) thay vì phân
  trang keyset: hợp đồng `GET /orders/{id}/timeline` không có tham số trang, thêm trang là đổi hợp đồng và màn hình
  (ngoài phạm vi). Phương án khác: phân trang cả lịch sử thao tác (task riêng nếu cần). Dễ đảo ngược: một câu SQL.
- (2) Cho `cleanupStale` nhận khóa qua tham số; test dùng cặp khóa ngẫu nhiên riêng. Phương án khác: kéo dài hạn chờ
  (bị cấm: nới tiêu chí) hay thử lại (bị cấm).

- Sửa `TestM2Routes_Unimplemented` (đỏ sẵn trên `main`, ngoài đề task) trong task này: hook Stop chạy test gói
  phụ thuộc (`httpapi`) và chặn báo xong khi còn đỏ; không thể làm xong task mà bỏ đỏ. Giữ assertion 501 bằng cách kiểm
  thẳng chỗ đổi lỗi (không còn operation nào chưa làm để gọi qua HTTP). Phương án khác: dừng chờ điều phối viên sửa
  (task bị kẹt); bỏ hẳn ca 501 (mất kiểm hợp đồng 501). Dễ đảo ngược: chỉ một test.

## Việc phát hiện thêm

- `internal/httpapi` › `TestM2Routes_Unimplemented` đỏ trên `main` (không do task này): Task 82 lấy
  `GET /orders/{id}/timeline` làm route "chưa làm" trong khi Task 81 đã làm route đó; hai task gộp song song. Đã sửa
  trong task này (hook Stop chặn khi gói phụ thuộc đỏ) — xem "Quyết định thay chủ shop".
- Chập chờn trên máy chủ PostgreSQL chung, không sửa (ngoài phạm vi): `internal/platform/observability` ›
  `TestFileSyncRole` (vai trò `crm_filesync` của Task 410 có giới hạn 1 kết nối toàn máy chủ; hai phiên chạy cùng lúc
  thì một bên nhận 53300) và `tools/loadkit` › `TestReadStats` (đếm dòng chết từ thống kê PostgreSQL). Cả hai qua
  3/3 khi chạy riêng. Đề nghị điều phối viên giao task riêng.

## Reviewer

Kết luận: ĐẠT (agent reviewer, một lượt, trên bản đã gộp `main` và đã stage).

- Phát hiện phải sửa: không có.
- Ảnh hưởng khớp thực tế: mọi chỗ gọi `cleanupStale`/`dropStale` truyền khóa mới, đường thật vẫn `serverLocks()`;
  `ListEntityAudit` giữ chữ ký, `order.Timeline` tự sắp lại; mã sinh khớp nguồn; không đổi OpenAPI, console, migration.
- Bằng chứng: các test kiểm đúng tiêu chí; thứ tự trong cùng transaction phân xử bằng `id DESC` (uuidv7) nên đúng.
- Ghi chú không bắt buộc: câu Tái dùng nhắc danh mục dùng chung có `ListEntityAudit` (không có) — đã sửa câu chữ;
  khóa ngẫu nhiên của `privateLocks` trùng khóa tiến trình khác là không đáng kể.
