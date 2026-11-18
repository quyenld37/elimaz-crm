# Task 334 — Danh sách tồn theo kho

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 334, M1-39b). Làm trong worktree `task-51` trước Task 51 (Màn Tồn kho phụ thuộc task này, xem Quyết định thay chủ shop). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu đã có. Thêm mới trong `internal/queries`:
  `ListBalances`, `BalanceFilter`, `BalanceRow`, `BalanceCursor` (`String`) + `ParseBalanceCursor`. `gen.Handler` thêm
  `ListBalances` (ogen sinh); `*handler` hiện thực trong `handlers_lists.go`. Dùng lại `queries.ErrInvalidInput`,
  `MaxListLimit`, `foldQuery` (đã có, không đổi).
- Gói phụ thuộc ngược (go list): `internal/queries` ← `internal/httpapi` (duy nhất). `internal/httpapi` ← `cmd/crm`,
  `tests/arch`, `tests/e2e`.
- Operation OpenAPI bị chạm, oasdiff: thêm `GET /api/v1/inventory/balances` `listBalances` (`inventory.read`,
  `warehouseId` bắt buộc), schema mới `BalancePage`, `Balance`. Chỉ thêm → oasdiff không báo phá vỡ.
- Chỗ dùng trên màn hình: màn Tồn kho (Task 51, làm ngay sau trong cùng worktree); `console/src/api/**` sinh lại.
- Câu sqlc, bảng, pha migration: không câu sqlc, không migration. SQL dựng lúc chạy ở `balances_dynsql.go`, đọc view
  `inventory.stock_available_v`, `catalog.variants_v` và hàm `catalog.search_variants` (đều có trong `docs/views.md`).
- Test sẽ chạy: `go test -race ./internal/queries/...` (mới `balances_dynsql_test.go`, thêm ca ở `explain_test.go`),
  `-run 'TestListBalancesAPI|TestListsAPI' ./internal/httpapi/`, `./tests/arch/...`; một lần `make ci` trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `tenancy.WithTenant`, `readAsDefaultSeller`
  (người bán từ phiên), `canAt` + `warehouseScope` (phạm vi kho), `foldQuery` + `catalog.search_variants` (tìm của
  Task 39), `queries.ErrInvalidInput` (đã đổi sang 400 ở `errors.go`), fixture 50.000 biến thể của `fixture_test.go`,
  `testkit.ExplainAsApp`/`RequireNoSeqScan`, `newInvAPIEnv`/`variant` của `handlers_inventory_test.go`.
- Dùng lại / viết mới vì: dùng lại tất cả. Viết mới `BalanceCursor` (một uuid): `queries.Cursor` là cặp
  `(updated_at, id)` của danh sách mẫu; danh sách tồn sắp theo `variant_id` (đi theo index `stock_balances_warehouse`)
  nên con trỏ chỉ cần mã biến thể. Không gộp: hai thứ tự khác nhau, lý do đổi khác nhau.

## Soát thiết kế

- 5.1–5.2: chỉ đọc, không thêm dữ liệu.
- 5.3: chỉ đọc view `_v` và hàm tìm công bố trong `docs/views.md`.
- 5.4–5.6: không áp dụng (chỉ đọc, không sự kiện, không gọi ngoài).
- 5.7: trong `tenancy.WithTenant` (qua `readAsDefaultSeller`); view `security_invoker` nên RLS áp; hàm tìm tự lọc tenant.
- 5.8: `internal/queries` trả sentinel; đổi mã ở `errors.go` (đã có `queriesError`).
- 5.9: không có đường ghi (GET, `x-permission: inventory.read`).
- 5.10–5.11: không áp dụng; không thêm thư viện. 5.12: theo `listProducts` của Task 39.
- 6.1–6.4, 6.6–6.8: không áp dụng (chỉ đọc, không khóa, không việc nền).
- 6.5: một trang là một câu (READ COMMITTED) nên nhất quán trong trang; keyset theo `variant_id` (không đổi khi số dư
  đổi) nên đi hết trang không lặp, không sót dòng đã có (có test đi hết > 50.000 dòng).
- 6.9: `Limit` ≤ 100, khớp tìm ≤ 100; EXPLAIN bằng `crm_app` trong tenant: không Seq Scan cho trang đầu, chỉ thiếu,
  thiếu + tìm, trang sau.
- 6.10: không có tiền. 6.11: không thuộc vùng rủi ro cao.
- 6.12: không dữ liệu cá nhân; chuỗi tìm không vào log.
- 6.13: guard kiểm `inventory.read` (bất kỳ phạm vi); handler kiểm thêm `canAt(warehouseId)` → thiếu ở kho đó là 403
  (test: phạm vi `w1` gọi `w2` → 403).

## Quyết định thay chủ shop

- **Q1 — Làm Task 334 trong phiên Task 51.** Màn Tồn kho (Task 51) phụ thuộc `listBalances` (Task 334, bảng phụ thuộc
  kế hoạch) mà task này chưa làm, không có worktree nào đang làm. Chọn: làm Task 334 trước, đủ quy trình (tệp task,
  test, reviewer, commit riêng), rồi mới làm Task 51. Phương án khác: dừng Task 51 chờ — mất một đêm; hoặc làm màn với
  API giả và hợp đồng chưa có — trái luật "không sửa mã sinh tay". Đảo ngược dễ: phiên điều phối không cần mở Task 334.
- **Q2 — Thứ tự và con trỏ.** Kế hoạch ghi `ListBalances(...) ([]BalanceRow, *Cursor, error)` nhưng không nói thứ tự.
  Chọn: biến thể mới tạo trước (`variant_id` giảm, uuidv7), con trỏ riêng `BalanceCursor` (một uuid). Vì: đi đúng
  index `(tenant_id, warehouse_id, variant_id)` nên không Seq Scan, không sắp xếp cả kho mỗi trang; khóa sắp không đổi
  khi số dư đổi nên trang không nhảy. Phương án khác: `updated_at` giảm (cần index mới = migration, kế hoạch ghi
  "không có migration"), hoặc theo SKU (nối vòng qua bảng biến thể, chậm khi lọc thiếu).
- **Q3 — Cột của một dòng.** Kế hoạch không liệt kê cột. Chọn theo mockup `kho-ton.html`: SKU, barcode, mã mẫu, tên
  mẫu, màu, size, trạng thái mẫu và biến thể (chip "Ngừng bán"), tồn, lỗi, đang giữ, đang giao, có thể bán.
- **Q5 — Mẫu đã xóa mềm còn số dư vẫn hiện** khi không tìm (hàng vật lý còn trong kho phải thấy để xử lý); khi tìm thì
  không ra vì `search_variants` bỏ mẫu đã xóa. Phương án khác: ẩn hẳn — mất dấu hàng tồn thật.
- **Q4 — Tìm.** `q` dùng `catalog.search_variants` (barcode, SKU, mã mẫu theo tiền tố, tên không dấu), tối đa 100
  biến thể khớp (giới hạn của hàm). Phương án khác: `search_products` (≤ 1.000 mẫu) — gõ barcode sẽ ra cả mẫu, kém đúng.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `ExplainAsApp` bằng `crm_app` trong tenant, danh sách tồn (trang đầu, `OnlyShort`, thiếu + tìm, trang sau) → `RequireNoSeqScan` | `TestPlansHaveNoSeqScan/tồn_*` | `go test -race -count=1 -timeout 30m ./internal/queries/...` | `ok elimaz.com.vn/crm/internal/queries 430.738s` |
| `OnlyShort` chỉ trả dòng có thể bán âm (5 biến thể của P00003, −2); thiếu + tìm | `TestProducts/Balances/OnlyShort` | như trên | qua |
| `OnlyShort` sau ca KH07 (on_hand 2, held 4) → đúng 1 dòng, `available −2` qua HTTP | `TestListBalancesAPI` | `go test -race -run 'TestListBalancesAPI\|TestListsAPI' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 11.184s` |
| Nhân viên `inventory.read` phạm vi `w1` gọi `warehouseId` của `w2` → 403; không có `inventory.read` → 403; thiếu `warehouseId` → 400 | `TestListBalancesAPI` | như trên | qua |
| Đi hết trang không trùng, không sót; con trỏ đi về; tenant, người bán, kho khác → 0 dòng; dữ liệu vào hỏng → `ErrInvalidInput` | `TestProducts/Balances/*`, `TestProducts/BalanceCursorRoundTrip` | `go test ... ./internal/queries/...` | qua |
| Cổng kiểm | `make ci` (build, vet, lint, gen-check, sqlc-vet qua; bước test quá 10 phút ở `queries`, `inventory`, `queue` và lỗi đo giờ ở `imager` do máy tải 27 — chạy lại riêng 4 gói: qua), rồi `make build vet lint gen-check vuln oasdiff catalog-check jscpd test-guard gitleaks console-check` | xem cột Test | `ok queries 430.7s`, `ok imager 14.7s`, `ok inventory 520.4s`, `ok queue 495.0s`; các bước còn lại thoát 0 |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Con trỏ là `*BalanceCursor` (một uuid), không phải `*Cursor` như dòng Interfaces (Quyết định Q2).
- Test "danh sách tồn" chạy trên database của `TestProducts` (subtest `TestProducts/Balances`) thay vì một test riêng:
  dựng hai lần 50.000 biến thể làm gói quá 10 phút của `go test` khi máy đang tải nặng.
- Lần chạy đỏ trước khi viết mã (Step 2) chỉ có ở mức biên dịch (test viết trước, gói chưa có ký hiệu → lỗi
  `undefined: queries.BalanceFilter`…); tôi viết `balances_dynsql.go` trước khi chạy test trên database.

## Việc phát hiện thêm

- `catalog.search_variants` (Task 39) với chuỗi tên rộng ("dam lua", khớp 2.500 mẫu) có Seq Scan trên
  `catalog.variants` bên trong hàm (Hash Join với kết quả trigram). Ca EXPLAIN của Task 39 dùng chuỗi hẹp nên không
  thấy. Task 334 dùng chuỗi hẹp như vậy; đề xuất một task sửa thân hàm (giới hạn mẫu khớp trước khi nối biến thể).
- `make ci` lần đầu trên máy tải nặng (load 27, nhiều phiên dùng chung PostgreSQL): `internal/modules/inventory`,
  `internal/platform/queue` quá 10 phút mặc định của `go test`, `internal/imager` lỗi ca đo thời gian
  (`TestImager_HangKilledWithinDeadline`, `TestExecRunner_TimeoutKillsProcessGroup`). Không gói nào do task này
  đổi; xem bảng bằng chứng cho lần chạy lại riêng.

## Reviewer

Kết luận: ĐẠT (lần 1, trên bản đã gộp `main` 1b3b102). Không có phát hiện ảnh hưởng tính đúng. Đã kiểm: RLS qua view
`security_invoker`, lọc người bán và kho, tham số `search_variants` đúng thứ tự, keyset `variant_id DESC` theo index
`stock_balances_warehouse`, `canAt` trước khi đọc, đổi lỗi qua `queriesError`, hợp đồng chỉ thêm. Ghi chú không bắt
buộc: (1) danh sách không tìm vẫn hiện biến thể của mẫu đã xóa mềm còn số dư, còn khi tìm thì `search_variants` bỏ
chúng — chấp nhận: hàng còn trong kho phải thấy được (Quyết định Q5); (2) giới hạn 100 biến thể khi tìm (đã ghi Q4);
(3) điều kiện `q != "" || f.Q != ""` thừa nhưng đúng.
