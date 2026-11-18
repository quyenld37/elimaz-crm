# Task 61 — M2-04: Danh mục đơn vị hành chính hai cấp

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task M2-04, số toàn cục 61). Loại việc (quy trình mục 3): A

**Trạng thái: CHƯA XONG.** Commit này là phần A (mã, schema, API, tool; test trên dữ liệu mẫu). Phần B (tệp danh mục
thật, README nguồn, migration dữ liệu, test trên dữ liệu thật) chờ chủ shop OK tải tệp — xem "Dừng để hỏi". Không đóng
Task 61 (không chạy `finish-task.sh`); Task M2-09 vẫn chờ phần B như kế hoạch ghi.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ thêm, không đổi ký hiệu có sẵn. Thêm ở `customer`:
  `AdminUnit`, `AdminUnitPair`, `AdminLevel` (+ `AdminProvince`, `AdminWard`), `ListProvinces`, `ListWards`,
  `ValidateUnits`, `MaxWardHits`. Dùng sentinel có sẵn `ErrUnknownAdminUnit`, `ErrWardNotInProvince` (Task M2-02 khai,
  đã ánh xạ 422 ở `internal/httpapi/errors.go`; ngoài bảng ánh xạ và `errors_m2_test.go` chưa ai dùng). `*handler`
  thêm phương thức `ListAdminUnits` (thay `gen.UnimplementedHandler.ListAdminUnits` đang trả 501).
- Gói phụ thuộc ngược (go list): `customer` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/testkit/shiptest`
  (chỉ thêm ký hiệu, không gói nào phải sửa). Tool mới `tools/adminunits` là `package main` riêng, chỉ nhập
  `internal/platform/textnorm`.
- Operation OpenAPI bị chạm, oasdiff: `listAdminUnits` (GET `/api/v1/admin-units`, `customer.read`) đã có trong
  `openapi.yaml` từ Task M2-02; task này hiện thực và chỉ thay câu `description` "Chưa làm ở Task M2-02 (501)" bằng
  mô tả thật (không đổi tham số, phản hồi: oasdiff không có thay đổi phá vỡ). `make gen` sinh lại chú thích trong
  `internal/httpapi/gen/*` và `console/src/api/sdk.gen.ts`; sqlc sinh thêm kiểu `CustomerAdminUnit` vào `models.go` của
  mọi gói store (mọi khối sqlc đọc chung thư mục migrations — mã sinh, không sửa tay).
- Test có sẵn bị chạm: `TestSchemaViewsAndRules` (kiểm index `tenant_id` của `customer.*` gặp bảng không tenant) —
  xem "Test bị xóa hoặc tắt".
- Chỗ dùng trên màn hình: chưa có (console chưa gọi `listAdminUnits`; ô chọn địa chỉ thuộc task màn hình sau).
- Câu sqlc, bảng, pha migration: bảng mới `customer.admin_units` (migration `0202_admin_units.sql`, pha mở rộng, chỉ
  tạo mới). Câu sqlc mới trong `queries/customer/admin_units.sql`: `ListAdminProvinces`, `ListAdminWards`,
  `GetAdminUnits`. `0203_admin_units_data.sql` (dữ liệu thật) **chưa làm** — xem "Dừng để hỏi".
- Test sẽ chạy: `go test -race ./tools/adminunits/... ./internal/modules/customer/... ./internal/httpapi/...`
  (gói đổi + gói nhập `customer`), `./internal/platform/tenancy/...` (CheckSchemaRules), rồi cổng kiểm task
  (`ci-task.sh`) một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`textnorm`); grep
  `admin_unit|province|ward` (chỉ có cột `province_code`, `ward_code` của `customer_addresses`, chưa có danh mục); mẫu
  tìm `name_search LIKE … ESCAPE` (`inventory.ListSuppliers`); mẫu tool sinh tệp có test (`tools/routegen`,
  `tools/catalog`: hàm `run(args, stderr) int`).
- Dùng lại / viết mới vì: chuẩn hóa chuỗi tìm dùng `customer.NormalizeName` (= `textnorm.Fold`); tool dùng thẳng
  `textnorm.Fold` (tool không nhập module nghiệp vụ) nên `name_search` trong SQL sinh ra khớp đúng hàm Go. Thoát ký
  tự `\ % _` của LIKE viết lại một dòng như `inventory.ListSuppliers` (khác module; logic nghiệp vụ ở lại module, một
  `strings.NewReplacer` không đáng thành gói platform). Lỗi dùng sentinel có sẵn của Task M2-02. Handler theo mẫu
  `catalogRead` (`tenancy.WithTenant` của người gọi, `db.TxAPI`).

## Soát thiết kế

- 5.1 Đúng chủ dữ liệu: danh mục thuộc module `customer` (kế hoạch Q15), bảng `customer.admin_units`.
- 5.2, 5.3: không ghi bảng module khác; module khác chưa đọc bảng này (M2-09 cùng module gọi `ValidateUnits`). Không
  công bố view ở task này; khi M3 cần sẽ thêm view kèm dòng `docs/views.md` (R13).
- 5.4–5.6: không áp dụng — không có đường ghi, sự kiện, gọi ra ngoài.
- 5.7 Ngữ cảnh shop: bảng **không** có `tenant_id`, không RLS (dữ liệu tham chiếu chung, kế hoạch Interfaces);
  `CheckSchemaRules` chỉ xét bảng có `tenant_id` → test hiện có vẫn `[]`. Hàm đọc nhận `pgx.Tx` của bên gọi (chạy được
  trong `tenancy.WithTenant`).
- 5.8 Nghiệp vụ không biết HTTP: module trả kiểu Go; handler mỏng đổi sang `gen.AdminUnitList`.
- 5.9 Đường ghi: không có (chỉ GET). crm_app chỉ `SELECT`; chỉ migration ghi (ADR-46: `admin_units` "không khóa").
- 5.10: không áp dụng (không Valkey). 5.11: không thêm thư viện. 5.12: theo mẫu `identity`/`catalog` (gói gốc công
  khai, sqlc trong `internal/store`, test `_test` trên PostgreSQL thật).
- 6.1–6.11: không áp dụng — lúc chạy bảng chỉ đọc: không ghi, không khóa, không tiền, không hàng đợi.
- 6.12 Dữ liệu cá nhân: tên tỉnh, xã là dữ liệu công khai; lỗi không mang giá trị người dùng nhập.
- 6.13 Quyền: route khai `x-permission: customer.read` (guard mặc định từ chối). Test: thiếu `customer.read` → 403;
  crm_app `INSERT`/`UPDATE`/`DELETE` vào bảng → lỗi quyền 42501.

## Quyết định thay chủ shop

- **Q-T61-1 — Chưa tải tệp danh mục thật.** Kế hoạch (mục "Việc cần chủ shop trong M2" và Step 0) ghi rõ OK tải tệp
  danh mục là việc chủ shop OK riêng trong chat, "không việc nào do Claude làm thay"; chưa thấy OK nào trong kế hoạch,
  tệp task hay soát chéo. Tải tệp là việc ngoài máy → không tải. Chọn: làm mọi phần không cần tệp thật (tool kiểm và
  sinh SQL, migration 0202 schema, câu sqlc, `ListProvinces`/`ListWards`/`ValidateUnits`, `listAdminUnits`), test trên
  dữ liệu mẫu do test tự chèn bằng chủ bảng (mã có chữ `T…`; test chỉ xét mã `T…` nên vẫn xanh khi đã có dữ liệu thật).
  **Lệch chữ kế hoạch:** Step 0 ghi "chưa OK thì dừng task này". Tôi chỉ dừng phần cần OK (tải tệp, phần B), không dừng
  cả task, theo chỉ dẫn của điều phối viên (chỉ dừng việc ngoài máy hoặc trái spec); reviewer lưu ý điều phối viên
  hoặc chủ shop phải xác nhận cách tách này. Đảo ngược: bỏ commit phần A là về trạng thái kế hoạch (chỉ thêm bảng rỗng,
  không ai đọc ngoài route mới). Phương án khác: dừng cả task — không chọn vì phần A không phụ thuộc nội dung tệp.
  Hệ quả: tới khi có phần B, bảng rỗng ⇒ `ValidateUnits` trả `ErrUnknownAdminUnit` cho mọi mã, `listAdminUnits` trả
  rỗng; Task M2-09 vốn đã chờ OK này.
  **Số migration của phần B:** kế hoạch đặt `0203`. Nếu lúc làm phần B đã có migration số lớn hơn được gộp vào `main`
  (vd. 0204/0205 của Task 62), dữ liệu lấy số kế tiếp còn trống theo Global Constraints (gộp theo thứ tự số), không
  chen `0203` vào sau; tool nhận `-out` nên không đổi mã.
- **Q-T61-2 — Định dạng tệp đầu vào của tool:** CSV UTF-8 có dòng tiêu đề `code,level,parent_code,name`, mỗi đơn vị
  một dòng (`level` = `province`|`ward`; tỉnh để trống `parent_code`). Đây là dạng mà Step 1 giả định ("một xã trỏ
  tỉnh không có"). Nếu tệp gốc chủ shop OK có dạng khác (vd. XLS của Tổng cục Thống kê), task tiếp sẽ thêm bước đọc
  dạng đó vào tool, không đổi phần còn lại. `effective_from` (mặc định 2025-07-01) và `source` là cờ của tool.
- **Q-T61-3 — `parent` không phải mã tỉnh có trong danh mục** → `listAdminUnits` trả danh sách rỗng (200), không 422:
  màn hình chỉ gửi mã lấy từ danh sách tỉnh; kiểm chặt là việc của `ValidateUnits` khi lưu địa chỉ. Xã sắp theo
  `name_search`, tối đa 50 (`MaxWardHits`); tỉnh sắp theo mã.

## Dừng để hỏi

- Cần chủ shop OK tải tệp danh mục đơn vị hành chính hai cấp công khai (đề xuất: danh mục của Tổng cục Thống kê
  `danhmuchanhchinh.gso.gov.vn`, xuất bản sau sắp xếp có hiệu lực 01/07/2025; vài trăm KB). Sau OK, một lượt riêng:
  tải tệp vào `data/admin-units/`, viết `README.md` (nguồn, URL, ngày tải, căn cứ công khai, số tỉnh, số xã), chạy
  `go run ./tools/adminunits` sinh `migrations/0203_admin_units_data.sql`, thêm test sau migrate: số tỉnh bằng số tool
  in ra, mọi xã có cha, `ListWards(<mã TP.HCM>, "ben thanh")` có phường Bến Thành, `ValidateUnits(HCM, <phường Hà
  Nội>)` → `ErrWardNotInProvince`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tool: tệp mẫu 2 tỉnh, 5 xã → SQL đúng 7 dòng, in "2 tỉnh, 5 xã", bỏ dấu, thoát nháy, tỉnh trước xã | `TestGenerateOK` | `go test -race ./tools/adminunits/` | `ok elimaz.com.vn/crm/tools/adminunits` (trước khi có mã, `run` rỗng: cả bốn test FAIL, vd. `TestGenerateBatches … stdout: ""`) |
| Tool: một xã trỏ tỉnh không có → lỗi nêu mã xã, không ghi tệp | `TestGenerateOrphanWard` | như trên | PASS (stderr nêu `T09003`) |
| Tool: trùng mã, xã trỏ xã, tỉnh có cha, xã không cha, cấp lạ, tên/mã rỗng, thiếu cột, không có tỉnh, sai tiêu đề, thiếu `-source`, ngày sai | `TestGenerateInvalid` (9 ca bảng + 3 ca con) | như trên | PASS |
| Tool: một câu INSERT mỗi 500 dòng (1001 → 500/500/1), `-effective-from` | `TestGenerateBatches` | như trên | PASS |
| `ListProvinces` theo mã, đúng tên và cấp (chỉ xét mã mẫu `T…`, chịu được dữ liệu thật) | `TestListProvinces` | `go test -race -run 'Admin\|ListProvinces\|ListWards\|ValidateUnits' ./internal/modules/customer/` | PASS (trước khi có mã: `undefined: customer.AdminUnit`) |
| `ListWards`: có dấu/không dấu/hoa thường, chỉ xã của tỉnh chọn, `%` `_` là chữ thường, limit, tỉnh lạ → rỗng | `TestListWards` (10 ca) | như trên | PASS |
| `ListWards` tối đa 50 dù xin 1000 | `TestListWardsLimitCapped` | như trên | PASS |
| `ValidateUnits`: cặp đúng; xã tỉnh khác → `ErrWardNotInProvince`; mã lạ/rỗng/sai cấp → `ErrUnknownAdminUnit` | `TestValidateUnits` (1 + 7 ca) | như trên | PASS |
| crm_app `INSERT`/`UPDATE`/`DELETE`/`TRUNCATE` → 42501; đọc được | `TestAdminUnitsReadOnlyForApp` | như trên | PASS |
| Ràng buộc bảng (cấp, cha, khóa ngoại, trùng mã); mọi xã sau migrate có tỉnh cha | `TestAdminUnitsConstraints` | như trên | PASS |
| `GET /api/v1/admin-units`: thiếu quyền 403; chỉ tỉnh, không `parentCode` (xét mã mẫu `T…`); `parent&q=ben` → 200, đúng 50 dòng của tỉnh đó; q có dấu; tỉnh lạ rỗng; q > 100 ký tự 400 `bad_request` (lỗi kiểm tham số của ogen) | `TestListAdminUnitsAPI` | `go test -race -run TestListAdminUnitsAPI ./internal/httpapi/` | PASS (trước khi có handler: `mã 501, mong 200`) |
| Bảng `customer.*` không `tenant_id` chỉ là `admin_units`; index của bảng tenant bắt đầu bằng `tenant_id` | `TestSchemaViewsAndRules` | `go test -race ./internal/modules/customer/` | PASS (trước khi sửa phạm vi: `index không bắt đầu bằng tenant_id: [admin_units_parent_idx admin_units_name_search_idx]`) |
| Số tỉnh thật, Bến Thành ở TP.HCM, phường Hà Nội ≠ TP.HCM | — | — | **Chưa làm**: chờ OK tải tệp (Dừng để hỏi) |

## Test bị xóa hoặc tắt

- Không xóa, không tắt. **Sửa phạm vi** một kiểm trong `TestSchemaViewsAndRules` (`internal/modules/customer/schema_test.go`,
  Task 60): "mọi index của bảng `customer.*` bắt đầu bằng `tenant_id`" nay chỉ xét bảng có cột `tenant_id`, vì kế
  hoạch đặt `customer.admin_units` là dữ liệu tham chiếu không `tenant_id` (index `(parent_code)`, GIN `name_search`
  theo Interfaces). Để không nới: thêm kiểm mới trong cùng test — tập bảng `customer.*` không có `tenant_id` phải đúng
  bằng `[admin_units]`; thêm bảng không tenant nào khác là đỏ.

## Khác kế hoạch

- Chưa có `data/admin-units/<tệp gốc>`, `data/admin-units/README.md`, `migrations/0203_admin_units_data.sql` và test
  trên dữ liệu thật (số tỉnh, Bến Thành, phường Hà Nội) — chờ OK tải tệp (Q-T61-1, Dừng để hỏi). Test chạy trên dữ
  liệu mẫu mã `T…` do test chèn bằng chủ bảng.
- Thêm `CHECK ((level = 'province') = (parent_code IS NULL))` (kế hoạch không ghi; chặn tỉnh có cha, xã mồ côi ở mức
  database). "Cha của xã là tỉnh" không đặt được bằng ràng buộc đơn giản: tool kiểm trước khi sinh, test
  `TestAdminUnitsConstraints` kiểm lại sau migrate.

## Việc phát hiện thêm

- `TestRecordAfterLongExecute` (`internal/platform/commands`, không thuộc task này) đỏ một lần ở lần chạy cổng kiểm
  thứ hai (`ghi in_doubt: timeout: context deadline exceeded`, máy và PostgreSQL chung đang tải nặng); xanh ở lần
  chạy thứ nhất và 3/3 khi chạy lại riêng (`go test -race -count=3 -run TestRecordAfterLongExecute
  ./internal/platform/commands/` → `ok`). Test phụ thuộc thời gian, nên xem ngưỡng ở task riêng.

## Cổng kiểm

- Lần 1: đỏ ở lint (3 `wrapcheck` trong test, 1 `nolintlint`), gen-check (mã sinh chưa stage), `TestSchemaViewsAndRules`
  — đã sửa cả ba.
- Lần 2 (sau khi stage): build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard,
  console-check xanh; test toàn bộ xanh trừ `TestRecordAfterLongExecute` chập chờn (trên). Sau review lần 1 chỉ sửa
  hai test (`TestListProvinces`, `TestListAdminUnitsAPI`) và tệp task: chạy lại hai test đó + golangci-lint gói đổi.

## Reviewer

Kết luận: ĐẠT (lần 2).

- Lần 1: CẦN SỬA — (1) `TestListProvinces`, `TestListAdminUnitsAPI` so đúng bằng danh sách tỉnh, sẽ đỏ khi có dữ liệu
  thật → sửa: chỉ xét mã mẫu `T…`, kiểm mọi dòng là tỉnh và sắp theo mã; (2) Q-T61-1 lệch chữ Step 0 → tệp task ghi
  rõ CHƯA XONG, phần A/B, không đóng task, số migration của phần B; xác nhận cách tách thuộc điều phối viên.
- Lần 2: ĐẠT, không phát hiện phải sửa. Ghi chú không bắt buộc: ca "trùng mã" của `TestGenerateInvalid` chỉ đòi stderr
  chứa `T01` (kiểm hơi lỏng, không sai); `quote` đủ cho tên có ký tự lạ ở phần B.
