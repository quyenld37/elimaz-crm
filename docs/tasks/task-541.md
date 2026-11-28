# Task 541 — Chuyển tối đa 50 sản phẩm mới và tồn dự kiến

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu cũ nào. Thêm mới: `internal/adapters/nhanh` (`ReadSnapshot`, `LatestSnapshot`, `BuildDrafts`, `Select`, `FetchImage`, kiểu `Draft`), `catalog` (`LegacyWorkbook`, `RecordLegacyRefs`, `LegacyRefs`, `VariantsBySKU`), `inventory` (`SetOpeningExpected`, `OpeningDiff`), lệnh `cmd/crm/nhanh_import_products.go`. Sửa `cmd/crm/main.go` (`isOneOffCommand`, `runOneOffCommand`: thêm một nhánh) và `tests/arch/no_nhanh_test.go` (ngoại lệ ADR-48 mở từ một tệp lệnh sang tiền tố `cmd/crm/nhanh_`).
- Gói phụ thuộc ngược (go list): `catalog`, `inventory` đã có nhiều gói nhập; chỉ thêm hàm, không đổi chữ ký cũ. Gói adapter mới chỉ `cmd/crm` nhập (test kiến trúc).
- Operation OpenAPI bị chạm, oasdiff: không (lệnh CLI, không route).
- Chỗ dùng trên màn hình: không. Màn Nhập Excel sẵn có duyệt và áp bản nhập do lệnh tạo; cột lệch của màn kiểm kho mở sổ (Task 273) đọc `inventory.OpeningDiff` (xem "Phụ thuộc task để sau").
- Câu sqlc, bảng, pha migration: migration 0720 (pha mở rộng: hai bảng mới `catalog.legacy_refs`, `inventory.opening_expected`, RLS, GRANT; không chạm bảng cũ). Câu mới: `queries/catalog/legacy.sql` (InsertLegacyRef, ListLegacyRefs, ListVariantsBySKU), `queries/inventory/opening_expected.sql` (UpsertOpeningExpected, ListOpeningDiff).
- Test sẽ chạy: `go test ./internal/adapters/nhanh/`, `-run` các test mới của `catalog` (legacy), `inventory` (opening_expected), `cmd/crm` (`NhanhImport`); ci-task chạy đủ gói đổi và gói phụ thuộc.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md`: nhập sản phẩm hàng loạt là đường Excel của catalog (`CreateImport` → kiểm `importer.Validate` → `ApplyImport`); gắn ảnh là `Writer.AttachImage`; tải tệp lên kho là `files.CreateUpload/CompleteUpload` (mẫu `cmd/crm/import_website_content.go`); mở DB, kho tệp cho lệnh một lần `openCommandDB`, `s3Config`, `waitFileReady`; khách HTTP Nhanh đã có ở gói adapter (Task 540); xlsx ghi bằng `xlsx.Write`.
- Dùng lại / viết mới vì: tạo sản phẩm KHÔNG viết đường ghi mới: lệnh sinh tệp Excel đúng cột mẫu rồi đi qua `CreateImport` (nhân viên duyệt và áp trên màn Nhập Excel; trạng thái mẫu mới là `draft` như Task 261). Mới chỉ: (1) ánh xạ JSON Nhanh → bản nháp (adapter, thuần); (2) bảng mã cũ vì chưa có nơi nào giữ mã Nhanh (chạy lại không trùng ảnh); (3) bảng tồn dự kiến vì `stocktake_lines.expected_on_shelf` là tồn CRM (0 ở kho trống), không phải số Nhanh. Tải ảnh qua HTTP viết mới trong adapter (chặn địa chỉ nội bộ) vì chưa có khách tải ảnh ngoài.

## Soát thiết kế

5.1 (module sở hữu dữ liệu): `catalog.legacy_refs` thuộc catalog, `inventory.opening_expected` thuộc inventory; lệnh chỉ gọi hàm công khai. 5.2–5.3: không route, không quyền mới (lệnh vận hành chạy trong container server, như `import-website-data`). 5.4 (sự kiện): `AttachImage` tự phát `product.updated`; ghi mã cũ, tồn dự kiến không phát sự kiện (không đổi tồn, không đổi sản phẩm khách thấy). 5.5: mỗi bước một transaction `tenancy.WithTenant`. 5.6 (gọi ra ngoài): đọc Nhanh chỉ từ bản chụp đã có trên đĩa; tải ảnh là GET ngoài transaction, thử lại an toàn (chạy lại bỏ ảnh đã có mã cũ). 5.7–5.9: không route; adapter không nhập module (test kiến trúc). 5.10: không Valkey. 5.11: chỉ thư viện chuẩn và excelize qua `platform/xlsx`. 5.12: theo mẫu lệnh một lần.
6.1 (tenant, RLS): hai bảng mới bật RLS (`enable_tenant_rls`), mọi câu chạy trong `tenancy.WithTenant`. 6.2 (khóa): hai bảng lá, chỉ chèn/sửa ở cuối transaction; `AttachImage` khóa mẫu như đường thường; không khóa chéo. 6.3 (chạy lại): mã cũ unique `(tenant, nguồn, loại, mã)` + `ON CONFLICT DO NOTHING`; tồn dự kiến upsert theo `(kho, biến thể)`; SKU là khóa của bản nhập (tệp giống hệt → `duplicate` của Task 47b). 6.4 (tiền): giá `int64` đồng, tồn `int32` có CHECK. 6.5 (đồng thời): hai lệnh chạy cùng lúc → unique chặn, bên thua đọc lại mã cũ; kết quả đếm gán lại trong thân hàm. 6.6: không hàng chờ mới. 6.7: Nhanh không bị gọi ở bước này. 6.8: không thay đổi tồn thật, chỉ số dự kiến; tồn mở sổ vẫn là số đếm. 6.9 (thời gian chờ): tải ảnh 30 s/ảnh, tối đa 10 MB, chờ xử lý ảnh theo `--image-wait`. 6.10–6.11: không cache. 6.12 (dữ liệu cá nhân): bản chụp sản phẩm không có dữ liệu khách; không log tên, giá; in chỉ số đếm và mã Nhanh. 6.13: tệp Excel sinh ra nằm ở kho riêng tư (`catalog_import`).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Danh sách mặc định là các mẫu của bản chụp (≤ 50), bỏ/chỉ một số mã, mã lạ và quá 50 bị chặn (hạn cứng) | `TestSelect`, `TestSelect_HardLimitFifty` | `go test -race -count=1 ./internal/adapters/nhanh/` | ok |
| Ánh xạ Nhanh → bản nháp: biến thể màu/size, SKU, mã vạch (trùng/sai khuôn bỏ), giá bán (+VAT chế độ 2), giá vốn, tồn "remain"/"available" (âm về 0), danh mục, mô tả, ảnh chung và ảnh theo màu (http bị bỏ) | `TestBuildDrafts_VariantsPricesStockImages`, `TestBuildDrafts_SingleProductGapsBecomeProblems`, `TestBuildDrafts_CodeShape` | như trên | ok |
| Đọc bản chụp: mới nhất, phiên bản lạ bị từ chối | `TestReadSnapshot_LatestAndVersion` | như trên | ok |
| Tải ảnh: chỉ https, chặn địa chỉ nội bộ, ≤ 10 MB, kiểm đầu tệp, lỗi không lộ địa chỉ | `TestImageFetcher`, `TestImageFetcher_BlocksInsecureAndInternal` | như trên | ok |
| Tệp sinh ra đi qua đúng đường nhập Excel, mẫu mới là draft, giá vốn trống là NULL, barcode trống tự sinh, danh mục lạ báo và để trống, chạy lại cùng tệp không thêm mẫu | `TestLegacyWorkbook_GoesThroughImportAsDraft` | `go test -race -count=1 -run 'TestLegacy' ./internal/modules/catalog/` | ok |
| Mã cũ chạy lại không trùng và không ghi đè; tra SKU không phân biệt hoa thường; tenant khác không thấy | `TestLegacyRefs_IdempotentAndTenantScoped` | như trên | ok |
| Tồn dự kiến: cột lệch = đếm − dự kiến, chưa đếm thì trống, chụp lại thì thay số, không dòng sổ/số dư, tồn mở sổ vẫn là số đếm | `TestOpeningExpected_DiffColumnAndRewrite` | `go test -race -count=1 -run 'TestOpeningExpected' ./internal/modules/inventory/` | ok |
| Từ chối số âm/quá lớn/lặp/thiếu giờ, biến thể hay kho lạ, phiên lạ; lỗi không để lại dòng | `TestOpeningExpected_Refused`, `TestOpeningExpected_TenantIsolated` | như trên | ok |
| Lệnh prepare chỉ tạo tệp + bản nhập validating, không tạo mẫu, không tải ảnh | `TestNhanhImportProducts_PrepareCreatesImportOnly` | `go test -race -count=1 -run 'TestNhanhImportProducts' ./cmd/crm/` | ok |
| Lệnh finish: mã cũ, ảnh đúng màu (3 ảnh), tồn dự kiến 7 và 3, SKU thiếu thành dòng lỗi mà phần khác vẫn chạy, không đổi sổ tồn, chạy lại không tải/gắn/ghi thêm | `TestNhanhImportProducts_FinishLinksImagesStockAndIsRerunnable` | như trên | ok |
| Ảnh màu lạ vẫn gắn (không màu); lỗi tải ảnh chỉ là dòng lỗi (mã, không địa chỉ) và chạy lại chỉ tải ảnh lỗi | `TestNhanhImportProducts_UnknownColorImageStillAttached`, `TestNhanhImportProducts_ImageFailureIsRowNotAbort` | như trên | ok |
| Tenant khác không thấy mã cũ, ảnh, số dự kiến | `TestNhanhImportProducts_OtherTenantSeesNothing` | như trên | ok |
| Cờ sai → lỗi cách gọi trước khi mở database; `--dry-run` chỉ in số đếm; `--remove`, `--only` | `TestNhanhImportProducts_FlagsAndDryRun` | như trên | ok |
| Ngoại lệ ADR-48 giữ hẹp (tiền tố `cmd/crm/nhanh_` + gói adapter) | `TestNhanhAdapter_OnlySnapshotCommandImports`, `TestNoNhanh_RealCode` | chạy ở ci-task (không chạy test kiến trúc trên máy Mac 16 GB) | xem ci-task bên dưới |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- **"Lệnh/API cho sửa danh sách"**: làm bằng cờ lệnh (`--only`, `--remove`), không có route HTTP (không route mới, không quyền mới; phù hợp lệnh vận hành chạy trên server và "không cần mockup riêng"). Muốn THÊM mẫu ngoài bản chụp: chụp lại `crm nhanh-snapshot --product-ids …` (hạn cứng 50 ở cả hai lệnh). "Bảng đơn giản trong màn Nhập Excel": không làm (màn là của task màn hình; bản nhập do lệnh tạo hiện sẵn trong màn Nhập Excel hiện có, nhân viên xem kiểm và bấm áp ở đó).
- **Hai bước thay vì một**: `prepare` (sinh bản nhập) rồi nhân viên áp, rồi `finish` (mã cũ, ảnh, tồn dự kiến), vì mẫu chỉ có sau khi áp và áp cần người bấm (đường nhập Excel của M1, giữ nguyên luật).
- **Mã cũ (legacy_id)** ở bảng riêng `catalog.legacy_refs` (kế hoạch nói "giữ legacy_id" nhưng chưa có chỗ nào); SKU vẫn là khóa nhập của sản phẩm.
- **Cột lệch**: chỉ phần máy chủ (`inventory.OpeningDiff`); hiện lên màn kiểm kho mở sổ là của Task 273 / task màn hình. "Cho đăng số Nhanh kèm lý do" (Task 544 trong bản đề xuất cũ) KHÔNG làm: kế hoạch chốt chỉ có 540–542 và mục tiêu Task 541 chỉ nói "hiện cột lệch".
- Số migration **0720** (kế hoạch M7 chưa cấp số cho Task 541; 0703–0712 đã gán hay có người dùng, 0713 chừa cho view dự phòng; 0720 không đụng ai). Đổi số khi gộp nếu điều phối viên muốn.
- Chưa xác minh với API Nhanh thật (như Task 540): tên trường giá vốn (`prices.import`), mã vạch (`barcode`), tồn (`inventory.remain`/`available`, khóa theo `productId`/`id`), `categoryId`. Chỗ thiếu thành "trường thiếu" trong báo cáo (đếm theo trường), không làm hỏng lệnh.

## Quyết định thay chủ shop

- Nhập sản phẩm đi qua tệp Excel + bản nhập sẵn có (nhân viên áp), không viết đường ghi sản phẩm mới. Vì: tái dùng toàn bộ luật kiểm của M1, mẫu mới là `draft`, dễ đảo ngược. Phương án khác: gọi `Writer.Create` trực tiếp từ lệnh (nhanh hơn nhưng bỏ qua bước nhân viên xem).
- Giá bán = bán lẻ + VAT khi `priceVatMode` = 2 (mặc định), như website; thiếu VAT thì lấy giá chưa VAT và ghi "trường thiếu" (mẫu đang ẩn nên không lộ giá sai cho khách). Phương án khác: dừng lệnh khi thiếu VAT.
- Ảnh gắn màu chỉ khi mẫu CRM có biến thể màu đó, không thì gắn không màu (không bỏ ảnh). Mã cũ ảnh = băm SHA-256 của địa chỉ (địa chỉ có thể mang mã truy cập).
- Tải ảnh: chỉ https, chặn địa chỉ nội bộ (SSRF), ≤ 10 MB; lỗi một ảnh không dừng lệnh.
- Tồn dự kiến lấy `remain` (tồn thực), không có thì `available`; ghi vào kho mặc định (hay `--warehouse`).
- Ngoại lệ ADR-48 của test kiến trúc mở từ một tệp lệnh sang tiền tố `cmd/crm/nhanh_` (hai lệnh); CLAUDE.md thêm tên lệnh.

## Phụ thuộc task để sau

- Màn hình cột lệch của phiên kiểm kho mở sổ (Task 273 / task màn hình): đọc `inventory.OpeningDiff(stocktakeID)` (SKU, dự kiến, đã đếm, lệch). Màn Nhập Excel hiển thị bản nhập do `prepare` tạo bằng đường sẵn có.
- Chạy thật: chờ token Nhanh và server thử (xác minh tên trường như ghi ở "Khác kế hoạch"); `--category-map` do chủ shop/nhân viên lập sau khi thấy danh mục thật.

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 541 không có test e2e; đủ ca ở test gói catalog, inventory, cmd/crm, adapter.)

## Việc phát hiện thêm

- `finish` tải ảnh tuần tự (≤ vài trăm ảnh với 50 mẫu), mỗi ảnh chờ worker xử lý tới `--image-wait`; nếu chậm khi chạy thật thì tải song song có giới hạn (chưa cần).
- Ảnh đã tải lên nhưng gắn lỗi để lại tệp mồ côi (bị kho tệp dọn theo luật hết hạn tệp chưa gắn nếu có); chạy lại sẽ tải lại ảnh đó.

## Reviewer

Kết luận: CHƯA REVIEW
