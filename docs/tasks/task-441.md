# Task 441 — Tách bảng ánh xạ lỗi httpapi theo vùng

Kế hoạch: task chung của điều phối viên (21/11, nhóm 1), dòng `| chung (21/11) | x-errors | 441 |` trong
`../../docs/superpowers/plans/danh-so-task-toan-cuc.md`. Loại việc (quy trình mục 3): A (thay đổi cấu trúc, không đổi
hành vi).

Lý do (đo 21/11): `internal/httpapi/errors.go` là điểm xung đột lặp lại khi gộp task song song (Task 100, 106, 108,
117): mọi task thêm hằng mã lỗi và dòng ánh xạ vào cùng một khối.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu xuất ra nào của `httpapi` đổi (mọi hàm ánh xạ
  là chữ thường trong gói). Hàm trong gói giữ nguyên tên, chữ ký: `identityError` (1 chỗ gọi: writeDomainError),
  `catalogError` (+ handlers_catalog.go:188), `filesError` (1), `inventoryError` (+ handlers_inventory.go:335),
  `orderError` (+ handlers_order_settings.go:130), `fulfillmentError` (+ handlers_shipment.go:82), `cashError`
  (+ handlers_cod_statements.go:36), `customerError` (+ handlers_customer_write.go:159, handlers_promotion.go:270),
  `promotionError` (+ handlers_promotion.go:267), `paymentsError` (chỉ trong cashError), `queriesError`
  (handlers_lists.go, không đổi). Xóa `order.ErrLabelNotFilterable` (sentinel xuất ra của order): 0 chỗ dùng trong mã,
  chỉ dòng ánh xạ errors.go:483 và hai câu chú thích test của order (list_test.go:166, list_m3_dynsql_test.go:247 —
  chỉ nhắc tên cũ "trước là ErrLabelNotFilterable", giữ nguyên).
- Gói phụ thuộc ngược (go list): `internal/httpapi` chỉ được `cmd/crm` nhập. `internal/modules/order` (vùng rủi ro
  cao): xóa một sentinel không ai dùng; chạy test cả bộ order theo cổng kiểm của task.
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`. Xóa 11 dòng duyệt ngày 2026-11-21 của Task 119 ở
  `openapi/breaking-approved.yaml` (thay đổi đó đã vào main) → `approved: []`.
- Chỗ dùng trên màn hình: không (mã lỗi, mã HTTP, thân trả về giữ nguyên).
- Câu sqlc, bảng, pha migration: không.
- Test sẽ chạy: `go test -race ./internal/httpapi/...` (gồm `errors_m2_test.go`, `errorcodes_test.go`, test handler),
  `go test ./tests/arch/...` (`errors_mapped_test.go`), `go test -race ./internal/modules/order/...`; rồi cổng kiểm
  ci-task một lần. Hai test đọc thẳng tệp `errors.go` phải đổi chỗ đọc sang mọi `errors*.go` không phải test:
  `tests/arch/errors_mapped_test.go` (mappedSentinels) và `internal/httpapi/errorcodes_test.go` (errorsGoCodes) — mở
  rộng chỗ quét cho đúng bố cục mới, không nới kỳ vọng.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có bảng ánh xạ lỗi
  (đổi lỗi sang HTTP chỉ ở lớp httpapi). Trong gói httpapi, chín hàm `<vùng>Error` chép cùng một vòng
  `for _, m := range []struct{err; status; code}{…}` + `errors.Is`.
- Dùng lại / viết mới vì: viết một kiểu `errorMapping` và hàm `matchError(bảng, err)` trong `errors.go` thay cho chín
  struct vô danh + vòng lặp chép nhau; mỗi vùng chỉ khai bảng của mình trong `errors_<vùng>.go`. Không gói mới.

## Soát thiết kế

- 5.1–5.7, 5.9, 5.10: không áp dụng: không dữ liệu, bảng, transaction, sự kiện, lời gọi ngoài, đường ghi mới.
- 5.8: vẫn đổi lỗi sang HTTP chỉ trong lớp httpapi, giờ ở nhóm tệp `errors.go` + `errors_<vùng>.go` (cùng gói); module
  không nhập gì mới. CLAUDE.md (mục Lỗi và log) ghi lại một dòng cách thêm lỗi cho vùng.
- 5.11: không thư viện mới. 5.12: lỗi nghiệp vụ vẫn là sentinel của module.
- 6.1–6.11: không áp dụng: không database, khóa, hàng đợi, tiền (chỉ dời bảng tra tĩnh).
- Thứ tự kiểm (điểm dễ sai): `writeDomainError` giữ đúng thứ tự cũ stockShortage → detailedError → identity, catalog,
  files, order, customer, promotion, inventory, queries, fulfillment, cash(+payments); trong mỗi bảng giữ đúng thứ tự
  dòng cũ (có lỗi bọc nhau hay trùng giá trị: `cash.ErrPreviewStale` trước `ErrAdjustmentBlocked`,
  `ErrAdjustTargetNotFound`; `order.ErrStockShort` = `inventory.ErrStockShort`; `errForbiddenTarget` ở inventory và
  fulfillment). Bảng là hàm trả slice mới mỗi lần gọi (không biến toàn cục thay đổi được, luật Go mục 4).
- 6.12: thân lỗi không đổi, không thêm dữ liệu cá nhân. 6.13: quyền không đổi (`errForbiddenTarget` vẫn 403).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mọi sentinel đã ánh xạ trước khi tách ra đúng status + mã sau khi tách (trần và bọc `%w`) | `TestDomainErrorSnapshot` (153 dòng, sinh tự động từ errors.go cũ) | `go test -race -run TestDomainErrorSnapshot ./internal/httpapi/` trên bản `git archive HEAD` (mã cũ) và trên mã mới | cả hai `ok` |
| Mỗi sentinel còn ở đúng hàm vùng cũ (handler dùng thẳng `catalogError`, `customerError`…) | `TestDomainErrorSnapshot_TableMembership` (154 dòng) | như trên | cả hai `ok` |
| Thứ tự kiểm giữa các vùng và trong bảng cash giữ y cũ | `TestDomainErrorSnapshot_Order` (12 ca, bọc hai lỗi theo hai chiều) | như trên | cả hai `ok` |
| Test bắt được sai | thử tay: đổi chỗ `orderError`/`customerError` trong `domainErrorMappers` → 2 lỗi; đổi status `customer.ErrInvalidPhone` → 4 lỗi; trả lại mã | `go test -run TestDomainErrorSnapshot ./internal/httpapi/` | đỏ đúng chỗ, rồi xanh lại |
| Test có sẵn qua không sửa kỳ vọng (`errors_m2_test.go`, test handler) | cả gói httpapi | `go test -race ./internal/httpapi/` | `ok … 49.284s` |
| Mọi mã lỗi (104) vẫn có trong `x-error-codes` | `TestErrorCodes_DeclaredInContract` (đọc mọi `errors*.go`, ngưỡng nâng 10 → 100) | như trên | `ok` |
| Mọi sentinel customer/order/promotion vẫn có dòng ánh xạ | `TestErrorsMappedRealCode`, `TestErrorsMappedSample` (quét `errors.go` + `errors_<vùng>.go`) | `go test ./tests/arch/ -run ErrorsMapped` | `ok 0.630s` |
| Xóa `ErrLabelNotFilterable` (0 chỗ dùng) | build + test order | `go build ./...`; ci-task (order `ok 178.001s`) | `ok` |
| Cổng kiểm task, lần 1 (chưa stage) | ci-task | ci-task | ĐỎ: gen-check (chỉ vì thay đổi chưa stage: diff chỉ gồm tệp của task, mã sinh không đổi), jscpd (1 đoạn trùng trong test mới — đã gộp thành `checkDomainError`, jscpd chạy lại: 0 clone), test: 2 test chập chờn ở gói không đổi (xem Việc phát hiện thêm); oasdiff, test-guard, catalog-check, lint xanh |
| Cổng kiểm task, lần 2 (đã stage, sau gộp main — main chưa có commit mới, "Already up to date") | ci-task | ci-task | build, vet, lint, gen-check, catalog-check, jscpd, gitleaks, oasdiff, test-guard xanh; test: httpapi, order (`ok 378.847s`), cash, customer, queries, tests/arch, tests/e2e `ok`; ĐỎ 2 test chập chờn ở gói không đổi: `TestShip_ResolveWhileRetried` (fulfillment, "trạng thái việc … pending") và `TestVerifier_ChangedFileNotMarked` (platform/files, "checked":0), tải máy 13–16; chạy riêng `go test -race -count=3 -run <tên>` cả hai `ok` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Hai test đọc thẳng tên tệp `errors.go` (`tests/arch/errors_mapped_test.go`, `internal/httpapi/errorcodes_test.go`)
  đổi chỗ quét sang `errors.go` + `errors_<vùng>.go` (không phải test). Không nới kỳ vọng; ngưỡng tự kiểm của
  `errorsGoCodes` nâng từ 10 lên 100 để bắt quét thiếu tệp.
- `queriesError` vẫn ở `handlers_lists.go` (trước đây cũng không ở errors.go), không dời.
- `detailedError`, `stockShortage` (lỗi có thân chi tiết, nhiều vùng) giữ trong `errors.go`.

## Quyết định thay chủ shop

1. Cơ chế đăng ký: mỗi vùng giữ hàm `<vùng>Error` (cùng tên, chữ ký cũ vì handler gọi thẳng) gọi
   `matchError(err, []errorMapping{…})`; thứ tự giữa các vùng ở một hàm `domainErrorMappers()` trong errors.go. Vì sao:
   không biến toàn cục (luật Go mục 4), giữ nguyên mọi chỗ gọi, thứ tự kiểm một nơi. Phương án khác: slice cấp gói
   mỗi tệp — vướng gochecknoglobals; đăng ký bằng `init()` — cấm.
2. Mã lỗi: mã chỉ một vùng dùng dời vào const của tệp vùng; mã nền tảng hoặc nhiều vùng dùng (29 mã) ở errors.go. Vì
   sao: task sau thêm mã cho vùng chỉ chạm tệp vùng. Dễ đảo ngược (chỉ dời hằng trong cùng gói).
3. `paymentsError` đổi từ `switch` sang bảng cùng thứ tự (kết quả như nhau: case đầu tiên khớp thắng).
4. Xóa luôn sentinel `order.ErrLabelNotFilterable` ở `internal/modules/order/errors.go` (không chỉ dòng ánh xạ): test
   kiến trúc đòi mọi sentinel order có dòng ánh xạ, nên giữ sentinel mà xóa dòng sẽ đỏ; 0 chỗ dùng.
5. Tệp luật Go của thư mục rules (mục 2 nhắc `errors.go`) không sửa: hook chỉ cho chủ shop đổi; ghi ở Việc phát hiện thêm.

## Việc phát hiện thêm

- Tệp `go-backend.md` trong thư mục rules của apps/crm, mục 2, còn ghi "đổi sang HTTP chỉ ở `internal/httpapi/errors.go`"
  — đề nghị chủ shop đổi thành `internal/httpapi/errors*.go`.
- ci-task lần 1: `TestCancel_PendingThenTrack/hãng_hủy` (fulfillment, "vận đơn = cancel_requested, muốn cancelled") và
  `TestDescribe_SkipsPrivateVariantKeepsRest` (platform/files) đỏ khi máy tải nặng; chạy riêng `-count=3` đều qua; hai
  gói không đổi trong task này. Chuyển Task 446 (test chập chờn).
- `docs/m3/doi-chieu-m2.md` còn nhắc `ErrLabelNotFilterable` (bản đối chiếu lịch sử, giữ).

## Reviewer

Kết luận: ĐẠT (lượt 1, trên phần đã stage sau gộp main).

- Phát hiện phải sửa: không có.
- Reviewer so từng dòng với `git show HEAD:internal/httpapi/errors.go`: mọi dòng ánh xạ đúng bảng, đúng thứ tự, đúng
  status và mã (chỉ bỏ dòng `ErrLabelNotFilterable`); `paymentsError` đổi sang bảng cùng thứ tự; `domainErrorMappers`
  khớp từng chữ danh sách cũ; đủ 104 hằng `code*`, giá trị không đổi; hai test quét chỉ mở rộng chỗ quét, ngưỡng chặt
  hơn; không test nào bị tắt hay nới.
- Ghi chú không bắt buộc (không sửa, ghi lại): chú thích trong module (`order/errors.go:10`, `catalog/errors.go:11`,
  `fulfillment/errors.go:5`, `openapi.yaml:4360`, `ogen.yml:3`) còn nhắc `errors.go` — chỉ là chữ; vài mã ghi "chỉ vùng
  X trả" vẫn được `stockShortage`/`detailedError` trong errors.go dùng (cùng gói, đúng).
