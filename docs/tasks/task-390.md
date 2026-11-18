# Task 390 — Tìm sản phẩm với từ khóa rộng không đọc cả bảng biến thể (M1-39c)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 390, M1-39c). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu Go nào. Chữ ký SQL của
  `catalog.search_variants(uuid, text, text, int)` giữ nguyên (cột trả về, thứ tự cuối `hạng, sku`); chỉ thân hàm đổi.
  Bên gọi (tìm theo chữ, đều trong `internal/queries`): `SearchVariants` (`products_dynsql.go`), `BalancesSQL` của
  `ListBalances` (`balances_dynsql.go`, lọc `Q`); test `explain_test.go`, `products_dynsql_test.go`,
  `balances_dynsql_test.go`.
- Gói phụ thuộc ngược (go list): `internal/queries` ← `internal/httpapi`, `internal/queries/orderview`,
  `internal/modules/order/internal/state`. Mã Go không đổi nên chỉ hành vi của hàm SQL tới được họ qua
  `searchVariants`, `listBalances`.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Hành vi `GET /api/v1/catalog/variants/search`
  (`searchVariants`) và `GET /api/v1/inventory/balances?q=` (`listBalances`) chỉ khác khi một nhóm khớp có hơn
  `lim` dòng (xem Quyết định Q1).
- Chỗ dùng trên màn hình: ô tìm của Tồn kho (Task 51) và mọi chỗ gọi `searchVariants` — không đổi mã màn.
- Câu sqlc, bảng, pha migration: không câu sqlc. Migration mới `0129_catalog_search_broad.sql` (số kế tiếp chưa dùng
  của dải M1 trên `main` và các nhánh task đang mở; 0126 đã cấp cho Task 45): pha mở rộng, chỉ
  `CREATE OR REPLACE FUNCTION catalog.search_variants` cùng chữ ký (giữ chủ `crm_search` và quyền EXECUTE; `crm_owner`
  là thành viên `crm_search` nên thay được). Không index mới: dùng `products_name_search_trgm`, `products_code_prefix`,
  `variants_sku_prefix`, `variants_option_key` (tenant_id, seller_id, product_id, …), `variants_barcode_key`,
  khóa `(tenant_id, seller_id, id)` của `products`.
- Test sẽ chạy: `go test -race ./internal/queries/...` (thêm ca tìm rộng ở `explain_test.go`; tệp mới
  `search_broad_test.go` với `testSearchSameAsBefore`, `testSearchBroadName`, gọi từ `TestProducts` trong
  `products_dynsql_test.go`); `./tests/arch/...` (migration, viewsdoc); `./internal/platform/tenancy/...`
  (CheckSchemaRules trên migration mới); `./internal/httpapi/...` qua `ci-task.sh` (gói phụ thuộc).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `testkit.ExplainAsApp`, `RequireNoSeqScan`,
  `EnableAutoExplain` (đọc kế hoạch bên trong hàm SECURITY DEFINER), fixture 10.000 mẫu × 5 biến thể của
  `internal/queries/fixture_test.go`, helper `search`/`hitCodes` của `products_dynsql_test.go`, `textnorm.Fold` (đã
  dùng trong `SearchVariants`). Tìm "relevance", "similarity", "search": không có hàm xếp độ liên quan dùng chung.
- Dùng lại / viết mới vì: dùng lại tất cả; fixture có sẵn lớn hơn yêu cầu (10.000 mẫu, 50.000 biến thể so với
  5.000 × 6) và "dam" đã khớp 2.002 mẫu nên không dựng fixture mới (xem Khác kế hoạch). Thân hàm viết lại trong
  migration mới (không sửa 0123 đã commit).

## Soát thiết kế

- 5.1, 5.2: hàm và bảng thuộc `catalog`; chỉ đọc. 5.3: `internal/queries` vẫn chỉ gọi hàm công bố trong
  `docs/views.md` (cập nhật mô tả thứ tự chọn khi nhóm khớp quá giới hạn).
- 5.4–5.6, 5.9, 5.10: không áp dụng (chỉ đọc, không sự kiện, không gọi ngoài, không đường ghi, không Valkey).
- 5.7 Ngữ cảnh shop: hàm SECURITY DEFINER chủ `crm_search` (BYPASSRLS) nên **mọi** nhánh, kể cả câu con LATERAL đọc
  biến thể, tự lọc `tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid` và `seller_id = p_seller`;
  `search_path` cố định, tên đủ schema. Test cũ tenant khác, ngoài ngữ cảnh (0 dòng) chạy lại.
- 5.8: không đổi Go. 5.11: không thư viện mới. 5.12: theo mẫu hàm tìm của 0123.
- 6.1–6.3: không áp dụng (không sửa, không khóa). 6.4: đọc trong `db.TxAPI`, chạy lại vô hại. 6.5: một câu, READ
  COMMITTED, nhất quán trong câu. 6.6–6.8, 6.10, 6.11: không áp dụng (không việc nền, không tiền).
- 6.9 Treo lâu: chính là mục đích task — mỗi nhánh dừng sau `lim` (≤ 100) dòng, biến thể đọc theo index qua vòng lặp
  lồng (LATERAL); test EXPLAIN không Seq Scan và đo thời gian từ khóa rộng.
- 6.12: không ghi log; số đo chỉ in thời gian. 6.13: quyền không đổi (`catalog.read`, EXECUTE chỉ `crm_app`).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Từ khóa rộng ("dam" khớp ≥ 2.000 mẫu; "p00" khớp 1.000 mã mẫu, 5.000 SKU) không Seq Scan (EXPLAIN bằng `crm_app`, trong tenant, kế hoạch bên trong hàm qua auto_explain), cả qua `ListBalances` | `TestPlansHaveNoSeqScan` ca "tìm tên rộng qua hàm", "tìm SKU rộng qua hàm", "tồn tìm rộng" | `go test -race -count=1 -run 'TestProducts/Search(SameAsBefore\|BroadName)\|TestPlansHaveNoSeqScan' -v ./internal/queries/` | `--- PASS: TestPlansHaveNoSeqScan (59.61s)`. Trước khi sửa (thân 0123): đỏ, Seq Scan trên `catalog.variants` ở 4 kế hoạch |
| Thời gian < 100 ms, ghi số đo và tải máy | `testSearchBroadName` | như trên | "dam" 5,1 ms; "mau" (mọi mẫu) 13,7 ms; "p0" 7,3 ms (lần nhanh nhất trong 5; máy 10 lõi, load average 22,6 lúc đo vì nhiều phiên test song song) |
| Từ khóa rộng: đủ 100 dòng, mọi dòng khớp, đúng thứ tự hạng rồi SKU, lấy mẫu liên quan nhất; không dấu / có dấu / hoa thường như nhau | `testSearchBroadName` (`requireMostRelevant`, "ĐẦM", " Đầm ") | như trên | `--- PASS: TestProducts (42.57s)`. Trước khi sửa: đỏ ở luật tên ngắn hơn |
| Từ khóa hẹp, SKU, mã mẫu, barcode, số khớp không vượt giới hạn: đúng kết quả cũ (cùng dòng, cùng thứ tự) | `testSearchSameAsBefore` (11 ca, so với `refSearchSQL` viết theo cách khớp của 0123) | như trên | PASS |
| Tenant khác, ngoài ngữ cảnh, đầu vào sai vẫn đúng | `testSearchOtherTenant`, `testSearchOutsideTenant`, `testSearchInvalid` (có sẵn) | `scripts/agent/ci-task.sh` | `ok elimaz.com.vn/crm/internal/queries 193.999s`; `CI-TASK XANH (so với điểm tách nhánh 13a9bf9…)` |
| Cổng tĩnh, migration, gói phụ thuộc | build, vet, lint, gen-check, sqlc-vet, arch, oasdiff, test-guard; test toàn bộ (đổi migration) | `scripts/agent/ci-task.sh` | `ok …/internal/httpapi 200.693s`, `ok …/internal/platform/tenancy 61.105s`, `ok …/tests/arch 18.564s`, `CI-TASK XANH` |
| Test thất bại trước (hàm của 0123) | `TestPlansHaveNoSeqScan` ca rộng, `TestProducts/SearchBroadName` | `go test -race -run '^(TestProducts\|TestPlansHaveNoSeqScan)$' ./internal/queries/` trước khi thêm 0129 | ĐỎ: còn `Seq Scan` trong kế hoạch các ca tìm rộng (Hash Join đọc cả `catalog.variants`); nhóm tên không lấy mẫu tên ngắn nhất |
| Đo lại sau khi stage (bản đã gộp `main` 13a9bf9) | `TestPlansHaveNoSeqScan` (3 ca rộng), `TestProducts/SearchBroadName`, `/SearchSameAsBefore` | `go test -race -count=1 -run '^(TestProducts\|TestPlansHaveNoSeqScan)$' -v ./internal/queries/` | `--- PASS` cả ba ca rộng (0.05s, 0.03s, 0.06s); `tìm "dam": nhanh nhất 5.26ms`, `"mau": 12.69ms`, `"p0": 6.60ms` (load average 24,3); `SearchSameAsBefore` PASS (0.24s) |
| Cổng tĩnh và test toàn bộ — lần chạy của phiên thứ hai (xem ghi chú dưới bảng) | `scripts/agent/ci-task.sh` | sau khi stage | build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard xanh; `ok internal/queries 209s`, `ok internal/platform/tenancy`, mọi gói khác `ok`; riêng `tests/arch` `TestLintCatchesPlantedMistakes` đỏ (`golangci-lint exit status 3` khi máy tải 24 với nhiều golangci-lint song song) — chạy lại riêng `go test -count=1 -run '^TestLintCatchesPlantedMistakes$' ./tests/arch/`: `PASS (1.25s)`; không liên quan phần đổi (không chạm Go ngoài test, không chạm testdata lint) |

Đã gộp `main` (fast-forward tới `13a9bf9`, có Task 391 đổi testkit) trước khi review. Điều phối viên khởi động hai
phiên cho task này (task-390-6c lúc 00:37:36 và task-390-3c lúc 00:37:12), chạy song song trên cùng worktree; mỗi
phiên chạy `ci-task.sh` một lần. Dòng "Tenant khác…" và "Cổng tĩnh, migration…" là lần chạy xanh của task-390-6c;
dòng cuối là lần của task-390-3c. task-390-3c commit; task-390-6c được nhắn dừng.

## Quyết định thay chủ shop

- **Q1 — Nhóm khớp quá giới hạn chọn dòng nào:** kế hoạch ghi "sắp theo độ liên quan". Chọn: tên — mẫu có tên không
  dấu ngắn nhất trước (mọi mẫu khớp đều chứa các từ của q theo thứ tự, nên tên càng ngắn thì q càng chiếm phần lớn
  tên), cùng độ dài theo mã mẫu; SKU và mã mẫu — theo tiền tố nhỏ nhất theo thứ tự byte của index (`USING ~<~`; kết quả cuối sắp theo collation
  `en_US.utf8`, nên với SKU dài khác nhau như `P1-S`, `P10-S` tập được chọn có thể khác "lim SKU nhỏ nhất" theo
  collation của 0123; chỉ khi nhóm quá `lim`). Thứ tự hiện vẫn như cũ: hạng
  rồi SKU. Khi không nhóm nào quá `lim` (từ khóa hẹp, SKU, barcode) kết quả đúng như cũ (test so với cách khớp cũ).
  Phương án khác: `similarity()` của pg_trgm (tốn tính trên mọi mẫu khớp, hàm ở schema `public` ngoài `search_path`
  cố định); giữ "lim SKU nhỏ nhất của cả nhóm" như cũ (buộc đọc mọi biến thể khớp — chính lỗi cần sửa). Đổi lại dễ:
  chỉ là `ORDER BY` trong migration sau.
- **Q2 — Số migration:** `0129` (số kế tiếp chưa dùng của dải M1 trên `main` và các nhánh task đang mở; 0126 đã cấp
  cho Task 45).

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Dữ liệu thử: dùng fixture có sẵn của `internal/queries` (10.000 mẫu × 5 biến thể = 50.000 biến thể; "dam" khớp
  2.002 mẫu) thay cho 5.000 × 6 của kế hoạch — lớn hơn yêu cầu, khỏi dựng thêm database thử 2 phút.
- Không cần index mới (đủ index của 0114, 0123).

## Việc phát hiện thêm

- Từ khóa dưới 3 ký tự (ví dụ "ao", "p0"): không có trigram nên nhánh tên của `search_variants` (và
  `search_products`) đọc cả bảng `catalog.products` (không đọc bảng biến thể; đo "p0" 23 ms trên 10.000 mẫu). Cách cũ
  cũng vậy. Khi shop lớn hơn nhiều có thể cần index riêng cho từ ngắn (ví dụ tiền tố từ) — task sau nếu đo thấy chậm.
- `catalog.search_products` (lọc `Q` của danh sách mẫu) giới hạn 1.000 mẫu không theo thứ tự nào; chưa đo với từ khóa
  rộng ngoài ca "dam lua" của Task 39.

## Reviewer

Kết luận: ĐẠT — hai lượt `reviewer` trên `git diff --cached` (0129, test, `docs/views.md`, tệp task); lượt hai sau
khi ghi "thứ tự byte" vào `docs/views.md` và Q1, cũng ĐẠT, không phát hiện phải sửa.

- Phát hiện phải sửa: không có.
- Ảnh hưởng khớp thực tế: `search_variants` giữ chữ ký, cột, thứ tự cuối; mọi nhánh kể cả LATERAL lọc tenant và
  `seller_id`; câu nối cuối thêm lọc tenant (chặt hơn 0123). Không có `+goose Down`, khớp quy ước.
- Bằng chứng: các test có thật và kiểm đúng tiêu chí; `requireMostRelevant` thật sự kiểm ~96 dòng hạng 3;
  `refSearchSQL` viết lại cách khớp 0123 trên view, không phải chính hàm đang test.
- Ghi chú không bắt buộc: nhóm SKU chọn `lim` dòng theo thứ tự byte (`~<~`), câu cuối sắp theo collation
  `en_US.utf8`; khi nhóm SKU quá `lim` với SKU dài khác nhau (`P1-S`, `P10-S`) tập chọn có thể khác "lim SKU nhỏ
  nhất" của 0123 — đúng Q1 ("thứ tự index"), không phải lỗi. Bảng bằng chứng bị lặp — đã gộp.
