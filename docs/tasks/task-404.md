# Task 404 — Đóng M1: review độc lập toàn M1 (M1-55c)

Tiêu đề: Task 404 (M1-55c). Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (mục "Task 56 (M1-55)", đoạn "Chia nhỏ 19/11", Step 3). Loại việc (quy trình mục 3): A (review; sửa nhỏ có test).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào. Đổi hành vi: `files.Register` (0 chỗ gọi ngoài gói `files` và test của nó — `grep -rn "files\.Register("` trên nhánh và trên main), `fulfillment.CreateForOrder` (2 chỗ gọi: `internal/httpapi/handlers_shipment.go`, `internal/testkit/shiptest/shop.go`; test fulfillment, cash, httpapi, e2e dùng tài khoản hãng cùng kho với đơn), `article.Validate` (gói internal của catalog: `products.go:220`, `article_html.go:51`). Thêm: trường `importer.Refs.ExistingBarcodes` (gói internal), hàm sinh `store.ListVariantSKUsByBarcodes`, hàm riêng `existingBarcodes`, `barcodesOf`.
- Gói phụ thuộc ngược (go list): `internal/platform/files` ← cmd/crm, internal/httpapi, internal/modules/catalog, files/filestest, internal/testkit; `internal/modules/fulfillment` ← cmd/crm, internal/adapters/fakecarrier, internal/httpapi, internal/modules/cash, internal/testkit/shiptest; `internal/modules/catalog` ← cmd/crm, internal/httpapi, catalog/catalogtest, internal/modules/order.
- Operation OpenAPI bị chạm, oasdiff: không (D-01 dùng lại `ErrOrderNotShippable` → 404 `not_found` đã có của `createShipment`; không thêm mã lỗi).
- Chỗ dùng trên màn hình: không đổi màn. Màn Nhập Excel hiện lỗi `invalid_barcode` đã có ở bước kiểm (B-03); tạo vận đơn bằng tài khoản kho khác nay 404 như tài khoản của người bán khác.
- Câu sqlc, bảng, pha migration: thêm câu đọc `ListVariantSKUsByBarcodes` (`queries/catalog/imports.sql`, bảng `catalog.variants`, theo `seller_id`, dùng unique `variants_barcode_key`); không migration.
- Test sẽ chạy: `go test -race` các gói `internal/platform/files/...`, `internal/modules/{catalog,fulfillment,cash,order}/...`, `internal/httpapi -run 'Shipment|Order'`; rồi cổng kiểm của task (ci-task) một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (files, catalog, fulfillment); tìm "barcode" trong `queries/catalog/*.sql` (chỉ có `ListVariantsBySKUs` theo SKU, `NextBarcodeNumber`, `search_products` — không câu nào trả SKU theo danh sách barcode của một người bán); lỗi sẵn có của fulfillment (`ErrOrderNotShippable`), của files (`ErrPurposeMode`).
- Dùng lại / viết mới vì: dùng lại `ErrOrderNotShippable`, `ErrPurposeMode`, mã lỗi `invalid_barcode` và đường `checkBarcode` của importer; viết mới một câu sqlc nhỏ vì câu theo SKU không thấy biến thể có SKU ngoài tệp (chính là lỗi B-03). Không có cảnh báo trùng.

## Soát thiết kế

Phần review (năm reviewer chỉ đọc) không chạm mã. Trả lời dưới đây cho bốn sửa nhỏ.

- 5.1 Chủ dữ liệu: mỗi sửa nằm trong module sở hữu (files, catalog, fulfillment). 5.2: không ghi bảng module khác (D-01 chỉ đọc `order.Get`, public API đã dùng sẵn). 5.3: không đọc thêm bảng của module khác. 5.4: D-01 kiểm trong cùng transaction tạo vận đơn, sau khi khóa đơn. 5.5, 5.6: không áp dụng: không thêm sự kiện, lệnh ra ngoài. 5.7: câu sqlc mới chạy trong `tenancy.WithTenant` (cùng closure với `existingRefs`), lọc `seller_id` (ADR-29). 5.8: không đụng HTTP. 5.9: không thêm đường ghi. 5.10: không áp dụng: không Valkey. 5.11: không thêm thư viện (`maps` của thư viện chuẩn). 5.12: theo mẫu catalog (câu sqlc trong `queries/catalog`, test PostgreSQL thật).
- 6.1: D-01 so kho sau `order.Lock` (đơn đã khóa); B-03 chỉ là bước kiểm, lúc áp unique của database vẫn chặn (`ErrBarcodeTaken` đã có). 6.2–6.9: không áp dụng: không đổi khóa chống trùng, thứ tự khóa, mức cách ly, việc nền, gọi ngoài, hạn giờ. 6.10: không áp dụng: không tiền. 6.11: không đổi quy luật vùng rủi ro cao (rapid `TestApply_AllOrNothingProperty` vẫn chạy trong gói catalog). 6.12: không log mới. 6.13: D-01 siết quyền theo kho ở tầng nghiệp vụ (kho lệch → từ chối).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Review độc lập 5 vùng, ghi `docs/review-doc-lap-m1.md` | — (5 agent mới, chỉ đọc, không lập luận người viết) | Agent general-purpose ×5 (vùng C từ phiên trước; A, B, D, E phiên này) | 33 phát hiện: 0 nghiêm trọng, 9 lớn, 20 nhỏ, 4 nghi ngờ; bảng ở đầu tệp |
| Mọi tên test của bảng ca nghiệm thu có trong mã (C10) | (vùng E) | grep + `go test -race -count=1 -run …` từng gói | đủ tên; 33 test Go đạt, 362 ca vitest đạt (phụ lục vùng E) |
| B-02: bài viết sát trần đọc lại, sửa được | `TestArticle_NearLimitReadsBack` | `go test -race -count=1 ./internal/modules/catalog/ -run TestArticle` | trước sửa: FAIL "đọc bài viết của mẫu …: bài viết không hợp lệ"; sau: `ok … catalog 5.893s` |
| B-03: barcode trùng biến thể ngoài tệp báo ở bước kiểm | `TestImport_BarcodeTakenInDatabase` | `go test -race -count=1 ./internal/modules/catalog/... -run 'TestImport\|TestApply\|TestValidate'` | trước: FAIL `ready []`; sau: `ok … catalog 96.080s`, importer, article `ok` |
| C-04: Register từ chối mục đích ảnh standard | `TestRegister_Refusals/mục_đích_ảnh_chế_độ_standard` | `go test -race -count=1 ./internal/platform/files/ -run TestRegister` | trước: FAIL "nhận <nil>"; sau: `ok … files 2.279s` |
| D-01: tài khoản hãng kho khác → từ chối, đơn và tồn không đổi | `TestShipment_AccountOfOtherWarehouse` | `go test -race -count=1 ./internal/modules/fulfillment/...` | trước: FAIL "<nil>, muốn ErrOrderNotShippable"; sau: `ok … fulfillment 5.170s` |
| Cổng kiểm của task (sau khi gộp main ac53f01) | gói đổi + phụ thuộc + vùng cao (40 gói `ok`, kể cả tests/e2e, httpapi, cash, order, inventory, identity) | `ci-task.sh` (lần 2, đã stage) | build, vet, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard xanh; lint + `TestLintCleanOnRealCode` đỏ vì một tham số `t` không dùng ở `create_shipment_test.go:167` → sửa thành `_`, rồi `golangci-lint run ./internal/modules/fulfillment/...` → `0 issues`, `go test ./tests/arch -run TestLintCleanOnRealCode` → `ok` (lần 1 đỏ jscpd do đoạn test trùng → gom thành `importHeader`, `expectOneRowError`, `expectNotShippable`) |
| Bên gọi CreateForOrder không vỡ | `TestShipmentAPI`, gói cash, order | `go test -race -count=1 ./internal/modules/cash/... ./internal/modules/order/...`; `go test -race -count=1 ./internal/httpapi/ -run 'Shipment\|Order'` | `ok … cash 35.959s`, `ok … order 58.225s`, `ok … httpapi 6.073s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

Task bù 406–415 (đã ghi vào bảng số task toàn cục; chi tiết, mức, tệp:dòng ở `docs/review-doc-lap-m1.md`):

- 406 (A-01, lớn): kiểm kho cho on_hand < phần giữ (Q8) → giao xong và ghi số đếm lỗi CHECK, đơn kẹt. Cần chủ shop chọn hành vi.
- 407 (B-01, lớn): tệp nhập > ~3.000 biến thể vượt hạn 30 s ở mọi lượt, không bao giờ áp được.
- 408 (D-03): lệnh tạo vận đơn kết thúc hỏng để vận đơn kẹt `creating`, hàng giữ mãi.
- 409 (C-02 lớn, C-07): lỗi tạm của imager biến ảnh hợp lệ thành `rejected` vĩnh viễn; `processing` không bao giờ ghi. Cần chủ shop (Q22).
- 410 (C-01 lớn, C-03 lớn, C-06, C-08): hạ tầng kho tệp (cô lập SeaweedFS khỏi imager, filesync trên máy chủ, tiền tố bucket, TTL crm-tmp). Cần OK hạ tầng.
- 411 (D-02): lên đơn không kiểm phạm vi kho của kho giữ hàng. Cần quyết định quyền.
- 412 (E-07 lớn, E-01, E-02, E-06, E-08): màn hình khớp quyền máy chủ.
- 413 (B-04..B-07): catalog nhỏ.
- 414 (A-02, C-05, E-04, E-05): gia cố, ca chạy tay có lịch chạy, threat model L1/L3.
- 415 (A-03, D-04, D-05, D-06, E-03): ghi cho M2 và đồng bộ tên kế hoạch theo mã.

## Quyết định thay chủ shop

- **D-01 dùng lại `ErrOrderNotShippable` (404 `not_found`)** thay vì thêm mã lỗi mới: cùng cách đối xử với "tài khoản hãng của người bán khác" đã có, không đổi hợp đồng OpenAPI (enum mã lỗi), dễ đảo ngược. Phương án khác: mã mới `account_wrong_warehouse` 422 (rõ hơn cho màn hình, nhưng đổi hợp đồng và `console/src/app/errors.ts`) — để Task 412 xét nếu màn cần câu báo riêng.
- **B-02 giữ trần 200 KB trên dạng JSON gọn (`Check`), byte thô chỉ chặn ở 2× trước khi giải mã:** đúng giới hạn Task 27 mà không khóa mẫu đã lưu. Phương án khác: chuẩn hóa lại byte đọc từ database (thêm một lần mã hóa mỗi lần đọc).
- **Phát hiện cần đổi chính sách hay hạ tầng không sửa trong task này** (A-01 đụng Q8, C-02 đụng Q22, C-01/C-03 đổi docker, D-02 quyền): tách task bù ghi rõ "cần chủ shop / OK hạ tầng", không tự đổi thiết kế (đúng đoạn Chia nhỏ 19/11).
- **E-03 (tên lệch kế hoạch):** mã đã gộp, có bên dùng; đề xuất sửa kế hoạch theo mã (Task 415), không đổi mã.
- **Phiên trước bị dừng:** kết quả vùng C của phiên trước còn nguyên nên dùng lại; bốn vùng A, B, D, E chạy lại bằng agent mới, cùng lời giao.

## Reviewer

Kết luận: ĐẠT (agent reviewer, lần 1, trên `git diff --cached` sau khi gộp main).

- Phát hiện phải sửa: không có.
- Ảnh hưởng khớp thực tế: `files.Register` chỉ test trong gói gọi (mục đích ảnh duy nhất `product_image`); `CreateForOrder` 2 chỗ gọi, tài khoản của shop thử cùng kho với đơn; `article.Validate` vẫn giữ trần 200 KB qua `Check`; câu sqlc mới lọc `seller_id`, trong `tenancy.WithTenant`.
- Bằng chứng: bốn test kiểm đúng điều tiêu chí nói; không test nào bị xóa, tắt, nới (hai test cũ chỉ chuyển assertion vào hàm giúp, điều kiện giữ nguyên).
- Ghi chú không bắt buộc: bảng số task (406–415) nằm ở repo cha, ngoài commit này; `checkSKUs` vẫn không hỗ trợ hoán đổi barcode giữa hai dòng — như trước khi sửa, không phải lỗi mới.
