# Task 542 — Chuyển khách hàng từ Nhanh

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu cũ nào. Thêm mới trong gói `customer`: `LegacyCustomer`, `LegacyImporter` (+ `Run`), `LegacyResult`, ba hằng lý do cần xem. Sửa nội bộ (không xuất): `importRowInput` thêm hai trường (`morePhones`, `legacyID`), `Importer.importRow` thêm hai bước chỉ chạy khi các trường đó có giá trị (đường Excel không đổi hành vi; `importRow` có 1 chỗ gọi ở `importer_job.go`). `cmd/crm`: thêm lệnh `nhanh-import-customers` vào `isOneOffCommand`/`runOneOffCommand` (2 chỗ gọi) và câu báo "lệnh lạ"; `tests/arch/no_nhanh_test.go` không đổi (tệp lệnh mới nằm dưới tiền tố `cmd/crm/nhanh_snapshot`, không nhập gói adapter: đọc JSON bản chụp bằng kiểu riêng).
- Gói phụ thuộc ngược (go list): `customer` được nhập bởi `cmd/crm`, `httpapi`, `conversation/*`, `order`, `promotion`, testkit; không ai trong số đó dùng ký hiệu mới, thay đổi chỉ thêm. Gói `customer` test lại cả gói ở ci-task.
- Operation OpenAPI bị chạm, oasdiff: không (lệnh CLI, chưa có route; màn "danh sách cần xem" thuộc task màn sau).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: migration mới `0742_customer_legacy_import.sql` (pha expand, chỉ thêm): bảng `customer.legacy_customers` (mã Nhanh → khách) và `customer.legacy_review` (danh sách cần xem), RLS, khóa ngoại ghép tenant, GRANT chèn/đọc cho `crm_app`. Câu sqlc mới `queries/customer/legacy.sql`: `LegacyMapped`, `LegacyPhoneHolders`, `InsertLegacyCustomer`, `InsertLegacyReview`. Số 0742: số 07xx của kế hoạch đã chia hết cho task khác; 0742 theo số task, đã kiểm main và mọi nhánh task-* không dùng.
- Test sẽ chạy: `go test -race -count=1 -run 'Legacy' ./internal/modules/customer/`, `go test -race -count=1 -run 'NhanhImport' ./cmd/crm/`, `go test -count=1 -run 'TestNhanh|TestNoNhanh' ./tests/arch/` (chạy ở ci-task), lược đồ `TestSchema*` của customer (migration mới).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (nhập, SĐT, danh tính); `customer.Importer` (Task 70: `importRow`, `ensureTags`, `ExistingPhones`, `parseTags`, `validName`, `validNote`, `NormalizePhone`, `insertIdentity`); `Writer.ImportBirthdays` (Task 491: nhập theo SĐT qua savepoint); `cmd/crm/import_website*.go` (mẫu lệnh một lần: cờ `--tenant`, `--dry-run|--apply`, `openCommandDB`, `newModules`, người làm hệ thống `importSystemID`).
- Dùng lại / viết mới vì: dùng lại `importRow` (tạo hồ sơ + danh tính phone nguồn import + nhãn + ghi chú + sự kiện `customer.created`, không audit từng khách) và `ensureTags`, `NormalizePhone`, `validName`; viết mới phần quyết định trùng (bảng `legacy_*`, phân loại theo chủ của SĐT) vì Excel chỉ có một SĐT một dòng và không có mã nguồn để chạy lại. Mở rộng `importRow` thay vì sao chép; không dùng đường Excel vì tệp Excel không mang nhiều SĐT và `legacy_id`.

## Soát thiết kế

5.1: dữ liệu mới là của module `customer` (hai bảng trong schema `customer`). 5.2: không ghi bảng module khác; khách, nhãn, ghi chú ghi qua mã của chính `customer`. 5.3: không đọc bảng module khác. 5.4: mỗi lô 500 khách một transaction tenant (khách + danh tính + nhãn + ghi chú + dòng đối chiếu + sự kiện cùng transaction). 5.5: sự kiện `customer.created` qua `events.Emit` trong transaction như Task 70, consumer chạy sau commit; không gọi ngoài trong transaction. 5.6: không gọi ngoài trong lệnh nhập (đọc tệp bản chụp trên đĩa trước khi mở transaction). 5.7: mọi câu chạy trong `tenancy.WithTenant`; bảng mới có RLS, khóa chính và khóa ngoại ghép `tenant_id`. 5.8: module không nhập `net/http` hay gói adapter Nhanh (lệnh `cmd/crm` đọc JSON, đưa vào module bằng kiểu `LegacyCustomer` trung tính). 5.9: không có route; đường ghi là lệnh vận hành của chủ shop, chạy lại không trùng (khóa tự nhiên `(tenant, legacy_id)` và SĐT). 5.10: không Valkey. 5.11: không thư viện mới. 5.12: theo mẫu Task 70 và lệnh một lần của Task 467.
6.1: hai lượt chạy cùng lúc cùng một khách: SĐT unique (`customer_identities_value_key`), `legacy_customers` khóa chính; bên thua lùi savepoint và tính "đã có". 6.2: chạy lại cùng bản chụp: khách đã có mã Nhanh bị bỏ qua trước mọi việc khác; không thêm dòng. 6.3: thứ tự khóa ADR-46 nhóm 5 giữ như Task 70 (khách → danh tính → địa chỉ/nhãn/ghi chú → dòng đối chiếu); hai lượt song song có thể khóa danh tính ngược thứ tự → `db.RetryTx` của `tenancy.WithTenant` thử lại (40P01). 6.4: lỗi tạm thử lại cả transaction lô (đã có ở `tenancy.WithTenant`); kết quả của lô gán lại trong thân hàm, không cộng dồn. 6.5: mức cách ly mặc định. 6.6, 6.7: không việc nền, không bên ngoài. 6.8: chết giữa chừng: lô đã commit giữ nguyên, chạy lại tiếp tục nhờ mã Nhanh và SĐT. 6.9: lô 500 khách (Task 70 đo ~7 ms/khách) dưới hạn 30 s. 6.10: không tiền. 6.11: quy luật "không bao giờ gộp, không bao giờ trùng SĐT" có test nhiều trường hợp và test chạy lại; vùng rủi ro cao không bị chạm (`customer` không nằm trong `HIGH_RISK_DIRS`). 6.12: không log giá trị: lệnh chỉ in số đếm; lỗi chỉ `{row, column, code}`; audit chỉ số đếm; bảng đối chiếu chỉ mã Nhanh, lý do và mã khách (không tên, SĐT). 6.13: lệnh chạy bằng `DATABASE_URL` (crm_app, RLS áp), người làm là tài khoản hệ thống cố định; không có quyền người dùng.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Khách đủ trường: nhiều SĐT, ngày sinh, nhãn, ghi chú, mã cũ, sự kiện; audit chỉ số đếm, không audit từng khách | `TestLegacyImport_CreatesFullCustomerAndMap` | `go test -race -count=1 -run '^TestLegacyImport_' ./internal/modules/customer/` | ok |
| Chạy lại không trùng (kể cả khi đổi SĐT khách đã chuyển) | `TestLegacyImport_RerunNoDuplicates`, `TestLegacyImport_BatchesResumeAfterPartialRun` (1.101 khách, 3 lô, lượt dở rồi lượt đủ) | như trên | ok |
| SĐT đã có không ghi đè | `TestLegacyImport_ExistingPhoneIsSkippedNotOverwritten` | như trên | ok |
| Không chắc → cần xem, không tự gộp (`phones_split`, `phone_not_added`, `duplicate_in_source`; 0 dòng `customer_merges`) | `TestLegacyImport_UncertainGoesToReviewNeverMerges` | như trên | ok |
| Lỗi `{row, column, code}` không giá trị; trường phụ sai không làm mất khách | `TestLegacyImport_RowErrorsCarryNoValues`, `TestLegacyImport_BadOptionalFieldsAreDropped` | như trên | ok |
| Chạy khô không ghi gì; hai lượt song song mỗi khách một hồ sơ; tenant cô lập; Writer chưa nối | `TestLegacyImport_DryRunWritesNothing`, `…ConcurrentRunsCreateEachOnce`, `…TenantsAreIsolated`, `…NotWiredAndEmpty` | như trên | ok |
| Đường Excel (Task 70) không đổi hành vi | `TestImport_*` | `go test -race -count=1 -run '^TestImport_' ./internal/modules/customer/` | ok |
| Lệnh CLI: đổi JSON Nhanh → khách, kiểm tệp/phiên bản, đúng đường `run` (khô, apply, chạy lại, lỗi → thoát 1, tenant lạ/thiếu cờ/tệp sai → thoát 2, không in tên/SĐT) | `TestNhanhImportCustomers_ParseCustomers`, `…SnapshotChecks`, `TestRun_NhanhImportCustomersCommandLine` | `go test -race -count=1 -run 'NhanhImportCustomers' ./cmd/crm/` | ok |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch ghi "sinh lượt nhập khách của Task 70". Lượt nhập Task 70 đọc tệp Excel (một SĐT, một dòng, không mã nguồn) nên không dùng được tệp trung gian; thay vào đó dùng chung hàm tạo khách `importRow` và `ensureTags` của Task 70, với đường vào riêng `customer.LegacyImporter` (không có bảng `customer_imports`, không việc nền: lệnh vận hành chạy một lần, tiến độ nằm ở bảng đối chiếu).
- Địa chỉ: mã địa phương của Nhanh không khớp danh mục hành chính của CRM → địa chỉ cũ ghi vào ghi chú, không tạo địa chỉ giao.

## Quyết định thay chủ shop

- Trùng SĐT "gộp theo luật Task 68" = KHÔNG tự gộp bao giờ (luật Task 68: gộp chỉ do nhân viên). SĐT đã có chủ: bỏ qua, không ghi đè; mơ hồ vào `customer.legacy_review` (3 lý do). Phương án khác: tự thêm SĐT phụ vào khách có sẵn (bị loại: đó là sửa hồ sơ, không đảo ngược được dễ).
- Trường phụ sai (ngày sinh, nhãn, ghi chú) bị bỏ lặng lẽ, không làm lỗi dòng; tên rỗng → "Khách cũ". Vì mất khách tệ hơn mất một nhãn. Chỉ SĐT và mã cũ là bắt buộc.
- Lưu bằng hai bảng mới (migration `0742`), không dùng cột mới ở bảng khách (không đổi bảng cũ; dễ đảo). Tên bảng `legacy_*` vì chuỗi `nhanh_` bị test kiến trúc ADR-11 cấm ngoài ngoại lệ ADR-48.
- Lệnh `crm nhanh-import-customers` ở tệp dưới tiền tố `cmd/crm/nhanh_snapshot` (đã được ngoại lệ) và KHÔNG nhập gói adapter (đọc JSON bản chụp bằng kiểu riêng) để không nới test kiến trúc.
- Có chế độ `--dry-run` (một transaction rồi lùi) để chạy thử trên server thử trước khi `--apply`.

## Phụ thuộc task để sau

- Màn/API xem và xử lý danh sách `customer.legacy_review` (nhân viên đánh dấu `resolved_at`, gộp bằng Task 68): chưa làm, thuộc task màn sau. Hiện đọc bằng SQL.
- Đối chiếu sau chuyển (Task 545 cũ: số khách Nhanh ↔ CRM) dùng được `legacy_customers`, `legacy_review` và số đếm của lệnh.

## Việc phát hiện thêm

- Tên trường của `customer/list` (mobile, mobile2, birthday, group, tags, note, address) CHƯA xác minh với API Nhanh thật: bảng khóa `nhanh*Keys` ở `cmd/crm/nhanh_snapshot_customers.go` nhận vài tên cho mỗi ý; sau lần chụp thật trên server thử, đối chiếu và chỉnh bảng đó. Bản chụp giữ JSON gốc nên không mất dữ liệu.
- Khách Nhanh không có SĐT hợp lệ không nhập được (SĐT là khóa); lệnh in `{dòng, cột mobile, mã}`; nhân viên bổ sung tay nếu cần.
- Số 0742 là số theo số task, ngoài bảng số migration 07xx của kế hoạch M7; điều phối viên kiểm khi gộp.

## Reviewer

Kết luận: CHƯA REVIEW
