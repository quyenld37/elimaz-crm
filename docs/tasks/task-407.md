# Task 407 — Nhập Excel lớn: áp theo lô có trần thời gian (M1-r2)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 323 M1-47b; bù review độc lập M1, phát hiện B-01 trong `docs/review-doc-lap-m1.md`, dòng 407 của `danh-so-task-toan-cuc.md`). Loại việc (quy trình mục 3): A. Vùng: `internal/modules/catalog` (đường áp bản nhập, Task 323 ghi là vùng rủi ro cao).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào. Thêm một hằng xuất ra `catalog.CodeApplyTooLong` ("apply_too_long", cạnh `CodeTooManyRows` trong `imports_validate.go`; ký hiệu mới, 0 chỗ dùng cũ). Đổi hành vi bên trong của `(*ImportApplier).Handle` (chỗ gọi: `queue.Worker` qua đăng ký ở `cmd/crm/main.go`, và test catalog) — chữ ký giữ nguyên. Hàm không xuất bị sửa: `apply` (thêm tham số lượt `attempts`, một chỗ gọi trong `Handle`), `write` (imports_apply_write.go); thêm trường không xuất `ImportApplier.now` (nil = `time.Now`), hàm `overBudget`, `applyTooLongError`, sentinel `errApplyOverBudget`. Tệp test mới `internal/modules/catalog/export_test.go` (gói `catalog`, chỉ khi test): `NewImportApplierWithClock` cho test dời đồng hồ.
- Gói phụ thuộc ngược (go list): `internal/modules/catalog` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/modules/catalog/catalogtest`, `internal/modules/inventory/invtest`, `internal/modules/order`, `internal/testkit`. Không gói nào dùng `ImportApplier` ngoài `cmd/crm` (đăng ký) và test.
- Operation OpenAPI bị chạm, oasdiff: không. `CatalogImportError.row` đã nhận 0 = lỗi cả tệp; `code` là chuỗi tự do (mô tả chỉ liệt kê ví dụ) → mã mới không đổi hợp đồng.
- Chỗ dùng trên màn hình: `console/src/screens/imports/ImportErrors.tsx` hiện `rowText(row)` + `message` của mọi lỗi (đã có lỗi cả tệp `too_many_rows`, `file_too_large`) → không sửa màn hình.
- Câu sqlc, bảng, pha migration: không câu mới, không migration. Dùng lại `FailApply` (đặt failed + errors + summary).
- Test sẽ chạy: `go test -race ./internal/modules/catalog/...` (bộ `TestApply_*`, `TestImport_*`, rapid tất cả hoặc không), `./cmd/crm/`, `./internal/httpapi/ -run TestCatalogAPI_Import`; cổng kiểm task (ci-task) một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` tìm "budget", "deadline", "ngân sách" → không có hàm chung về ngân sách thời gian của handler. Mẫu gần nhất: `customer.Importer` (Task 70, `internal/modules/customer/importer_job.go`): lô 500 dòng, mỗi lô **một transaction riêng đã commit**, tự dừng sau `importBudget` 20 s và hẹn lượt sau. Lỗi `ImportError` cấp tệp (Row 0): `fileError` trong `imports_validate.go`. Đặt failed + lỗi: `(*ImportApplier).fail`.
- Dùng lại / viết mới vì: dùng lại `(*ImportApplier).fail` (ghi `FailApply`, tóm tắt, audit `catalog.import.apply_failed`) và quy ước lỗi cấp tệp Row 0. **Không** dùng lại cách lô-nhiều-transaction của nhập khách: khác mục đích — nhập khách chấp nhận ghi từng phần (dòng lỗi bị bỏ qua, chạy tiếp), còn nhập sản phẩm theo Q10 phải "ghi tất cả hoặc không trong một transaction" (Quyết định 1). Chỉ lấy ý "ngân sách thời gian nhỏ hơn hạn handler, kiểm giữa các bước". Viết mới một sentinel và hàm nhỏ trong catalog vì chỉ một chỗ dùng; chưa có hai chỗ cùng lý do thay đổi để đưa lên `internal/platform`.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Chủ dữ liệu: bản nhập, mẫu, biến thể thuộc catalog; không bảng mới.
- 5.2, 5.3: không ghi/đọc bảng module khác (audit qua `identity.RecordAudit` như cũ).
- 5.4 Nguyên tử: giữ nguyên **một transaction** cho cả plan (Q10). Hết ngân sách → lùi savepoint, không còn gì của plan; chỉ ghi trạng thái failed của bản nhập (hay trả lỗi để chạy lại, cả tx lùi).
- 5.5, 5.6: không áp dụng: không thêm sự kiện, lệnh ra ngoài.
- 5.7: Handle vẫn chạy trong tx tenant của worker.
- 5.8: module không nhập net/http; không đổi lớp HTTP.
- 5.9: không áp dụng: không đường ghi API mới.
- 5.10, 5.11: không áp dụng: không Valkey, không thư viện mới.
- 5.12: theo mẫu đường áp của Task 323 (stepError/describeStep, fail).
- 6.1 Đồng thời: khóa không đổi (tư vấn theo người bán, khóa bản nhập, danh mục rồi mẫu). Lượt chạy lại sau khi hết ngân sách đi lại toàn bộ đường khóa + Recheck.
- 6.2 Gửi lại: không đổi (DedupeKey = mã bản nhập). Trạng thái failed ghi có điều kiện (`FailApply` chỉ khi applying) trong tx đang giữ khóa bản nhập.
- 6.3 Deadlock: không thêm khóa.
- 6.4 Lỗi tạm: hết ngân sách ở lượt 1, 2 → trả lỗi (không `ErrPermanent`) để hàng đợi chạy lại theo lịch (5 s, 30 s); lượt ≥ 3 → failed với lý do rõ, việc done. Lỗi hạ tầng khác giữ như cũ (chạy lại tới 8 lượt rồi OnTerminal → failed).
- 6.5: READ COMMITTED như cũ.
- 6.6 Việc nền lỗi: như 6.4. Bản nhập quá lớn không còn thành dead sau hàng giờ (không cảnh báo dead).
- 6.7: không áp dụng: không gọi bên ngoài. 6.8: như cũ (chết giữa chừng → tx lùi, về hàng chờ).
- 6.9 Treo lâu: phần chính — ngân sách = hạn còn lại của ctx trừ 5 s dự trữ (lùi savepoint + ghi failed + audit + commit), kiểm trước mỗi mẫu; ctx không có hạn (gọi thẳng trong test) → không giới hạn.
- 6.10 Tiền: không áp dụng (không đổi cách ghi giá).
- 6.11 Quy luật: rapid tất cả hoặc không cũ vẫn chạy; thêm test hết ngân sách → không mẫu, biến thể nào được ghi.
- 6.12 Dữ liệu cá nhân: câu lỗi và audit chỉ có số lượng; không SĐT.
- 6.13 Quyền: không đổi.

## Quyết định thay chủ shop

1. **Không áp theo lô nhiều transaction.** Tiêu đề task là "áp theo lô", nhưng Q10 (bảng quyết định kế hoạch M1; spec README liệt kê là quyết định kế thừa) chốt "kiểm hết trước, **ghi tất cả hoặc không ghi gì trong một transaction**", và spec 04 mục 5.1 (A8) giới hạn mỗi handler 30 s (worker còn kẹp ở nửa giữ chỗ = 45 s). Lô nhiều transaction như nhập khách (Task 70) để lại nửa tệp khi lô sau gặp dữ liệu đổi → trái Q10. Lô trong cùng một transaction không vượt được hạn 30 s. Vì vậy phần "tệp > ~3.000 biến thể áp được" để chủ shop chọn (mục Dừng để hỏi). Phương án khác đã cân nhắc: hạ trần dòng lúc kiểm (đổi giới hạn người dùng thấy — lệnh giao bảo dừng để hỏi), ghi hàng loạt bằng SQL riêng (trái luật Task 323 "dùng chính hàm công khai"), nâng hạn handler cho riêng việc này (trái A8).
2. **Làm phần trong phạm vi: trần thời gian của lần áp.** Lần áp kiểm thời gian còn lại trước mỗi mẫu; còn dưới 5 s thì dừng, lùi savepoint (không ghi gì của plan). Lượt 1, 2: trả lỗi để hàng đợi chạy lại (máy bận nhất thời). Từ lượt 3: bản nhập `failed` với một lỗi cả tệp (dòng 0) mã `apply_too_long`, câu báo bảo chia tệp; việc done. Trước đây: 8 lượt giãn tới hàng giờ → dead → failed không lý do. Người dùng chỉ thấy khác ở ca vốn đã hỏng (biết lý do sau khoảng một phút thay vì hàng giờ); tệp áp được hôm nay vẫn áp như cũ; trần 20.000 dòng lúc kiểm không đổi. Đảo ngược dễ: bỏ một nhánh trong `apply` (imports_apply.go). Phương án khác: failed ngay lượt 1 (sai khi máy bận nhất thời), giữ nguyên (nhân viên chờ hàng giờ).
3. Số "khoảng 2.000 dòng mỗi tệp" trong câu báo: dưới mức ~3.000–4.000 biến thể đo ở Task 323 (6,3 ms/biến thể lúc máy tải 12–17) để còn dư khi máy bận.

## Dừng để hỏi

- **Chủ shop chọn cách cho tệp nhập sản phẩm lớn (> ~3.000 biến thể) áp được** (B-01, phần còn lại sau task này):
  (a) hạ trần dòng của tệp nhập sản phẩm (ví dụ 2.000–3.000) và báo `too_many_rows` ngay lúc kiểm — đơn giản nhất, giữ Q10; đổi giới hạn người dùng thấy (hiện 20.000);
  (b) áp theo lô nhiều transaction như nhập khách — tệp lớn áp được nhưng bỏ "tất cả hoặc không" của Q10;
  (c) đường ghi hàng loạt riêng cho lần áp (chèn nhiều dòng một câu, sự kiện/audit gộp theo mẫu) — giữ Q10, nhanh, nhưng là "SQL ghi thứ hai" mà Task 323 cấm và tốn công (vùng rủi ro cao);
  (d) cho riêng việc `catalog.import.apply` hạn dài hơn 30 s — trái A8 (spec 04 mục 5.1), phải đổi `queue` và giữ chỗ.
  Đề xuất: (a) trần 3.000 dòng (khớp đề xuất của review B-01 và Task 323). Dù chọn gì, sau task này tệp quá lớn đã báo lỗi rõ thay vì treo.

## Bằng chứng

Môi trường: bộ chung crm-shared (`CRM_TEST_DATABASE_URL` cổng 5600), test tự tạo database riêng; máy tải ~20 trên 10 nhân (nhiều phiên khác).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Trước khi viết mã: test đỏ | `TestApply_OverTimeBudgetRetries`, `TestApply_OverTimeBudgetFailsWithReason` | `go vet ./internal/modules/catalog/` | build FAIL: `export_test.go:7:30: unknown field now in struct literal of type ImportApplier` (và `catalog.CodeApplyTooLong` chưa có) |
| Hết ngân sách giữa lúc ghi (đồng hồ nhảy sau 20 mẫu đã ghi) ở lượt 1 và 2 → Handle trả lỗi không `ErrPermanent`; bản nhập applying, không lỗi; 0 mẫu, dấu dữ liệu catalog như trước (20 mẫu đã ghi bị lùi) | `TestApply_OverTimeBudgetRetries` | `go test -race -count=1 -run 'TestApply_OverTimeBudget\|TestApply_TimeoutMidWriteRetries\|TestApply_ThreePriceChanges\|TestApply_200Models' ./internal/modules/catalog/` | `ok … catalog 30.445s` |
| Lượt 3 vẫn hết ngân sách → Handle không lỗi (việc xong, không chạy lại tới hàng giờ); bản nhập failed, đúng 1 lỗi dòng 0, cột rỗng, mã `apply_too_long`, câu bảo chia tệp; tóm tắt 1 lỗi; 0 mẫu, 0 biến thể, dấu dữ liệu như trước; 1 audit `catalog.import.apply_failed`; tệp nhỏ sau đó vẫn applied qua worker | `TestApply_OverTimeBudgetFailsWithReason` | như trên | PASS |
| Test bắt được lỗi: tắt tạm dòng kiểm ngân sách trong `write` (`if false && a.overBudget(ctx)`) → hai test đỏ; trả lại → xanh | hai test trên | `go test -race -count=1 -run 'TestApply_OverTimeBudget' ./internal/modules/catalog/` | mã tắt: `FAIL … đồng hồ chỉ được đọc 0 lần, mong hơn 20`; mã thật: `ok` |
| Tệp áp được hôm nay vẫn áp như cũ (KH01 200 mẫu, sửa giá 3 biến thể, hết hạn 300 ms vẫn chạy lại) và cả bộ catalog (gồm rapid tất cả hoặc không) | `TestApply_200Models`, `TestApply_ThreePriceChanges`, `TestApply_TimeoutMidWriteRetries`, cả gói | cổng kiểm task (ci-task) | `ok … internal/modules/catalog 134.477s`; 33 gói `ok` (gồm `cmd/crm`, `httpapi`, `order`, `identity`, `inventory`, `tests/e2e`) |
| Cổng tĩnh | build, vet, gitleaks, oasdiff, test-guard, catalog-check | cổng kiểm task (ci-task) lần 1 | qua; gen-check, sqlc-vet bỏ (không đổi nguồn sinh, SQL); `no leaks found` |
| Lần ci-task 1 đỏ ở 3 cổng, cùng một nguyên nhân là test mới: `wrapcheck` (`imports_apply_test.go:492` `return err`, cũng làm `tests/arch` `TestLintCleanOnRealCode` đỏ) và jscpd 1 đoạn trùng (phần chuẩn bị của hai test mới). Sửa: `//nolint:wrapcheck // test` như các hàm phụ test khác, gộp phần chuẩn bị vào `applyingBig`; chạy lại riêng các cổng đỏ | lint, jscpd, `TestLintCleanOnRealCode`, hai test mới | `golangci-lint run ./internal/modules/catalog/...`; `npx jscpd --config .jscpd.json --similarity --baseline jscpd-baseline.json --fail-on-new-clones=0 .`; `go test -count=1 -run TestLintCleanOnRealCode ./tests/arch/`; `go test -race -count=1 -run 'TestApply_OverTimeBudget\|TestApply_TimeoutMidWriteRetries' ./internal/modules/catalog/` | `0 issues.`; `Found 0 clones.`; `ok … tests/arch 2.810s`; `ok … catalog 6.957s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Tiêu đề "áp theo lô": không làm lô nhiều transaction (Quyết định 1); làm trần thời gian trong một transaction (Quyết định 2). Phần cho tệp lớn áp được: Dừng để hỏi.
- `TestApply_TimeoutMidWriteRetries` (Task 323, không sửa): ctx 300 ms nay dừng ở chỗ kiểm ngân sách trước mẫu đầu (hạn còn < 5 s) thay vì hết hạn giữa câu lệnh; điều test đòi (lỗi chạy lại được, applying, 0 mẫu) vẫn đúng và vẫn kiểm.

## Việc phát hiện thêm

- Trần thời gian chỉ kiểm trong bước ghi (trước mỗi mẫu). Nếu riêng bước khóa + đọc lại + Recheck đã vượt 30 s (máy cực bận, tệp gần 20.000 dòng) thì vẫn là lỗi hạ tầng như cũ (chạy lại tới 8 lượt rồi failed không lý do). Bước kiểm đo 0,82 s cho 20.000 dòng (Task 323) nên rất khó xảy ra; xử lý cùng lúc với lựa chọn ở Dừng để hỏi.
- Test catalog đỏ khi máy bận (`TestApply_ThreePriceChanges`, `TestApply_200Models`): nếu một lượt áp quá hạn thì việc dời 5 s mà hàm `drain` của test chỉ chạy việc đến hạn → test thấy applying. Task này không đổi chuyện đó (lượt quá ngân sách vẫn chạy lại theo lịch). Có thể cho `drain` chờ việc dời ngắn ở một task test riêng.

## Reviewer

Lần 1 (agent `reviewer`, trên `git diff --cached` sau khi gộp main — main không có gì mới, a9a6339): **ĐẠT**. Không có phát hiện phải sửa. Đã đối chiếu: `Job.Attempts` đếm cả lần đang chạy (lượt 1, 2 chạy lại; từ lượt 3 failed); hạn là `context.WithTimeout(ctx, p.Timeout)` của worker; `errApplyOverBudget` không bọc trong `stepError` nên savepoint lùi hết và `errors.Is` ở `apply` nhận ra; nhánh failed dùng lại `fail` (`FailApply` có điều kiện, trong tx giữ khóa bản nhập); không SQL mới, không dữ liệu cá nhân; `TestApply_TimeoutMidWriteRetries` không sửa vẫn đúng. Bảng bằng chứng khớp test.
Ghi chú không bắt buộc: (1) chữ "nhánh trong `applyPlan`" ở Quyết định 2 đúng ra là `apply` — đã sửa chữ trong tệp này; (2) lượt 1, 2 hết ngân sách đi qua `recordError` của worker như lỗi ứng dụng (đã lường trước; nếu cảnh báo ồn thì xem ở task khác); (3) phần cho tệp lớn áp được để ở Dừng để hỏi là việc chủ shop quyết.
