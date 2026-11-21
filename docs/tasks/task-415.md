# Task 415 — M1-r10: Ghi cho M2 và kế hoạch; đồng bộ tên kế hoạch theo mã

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (task bù review độc lập M1, dòng 415 của `danh-so-task-toan-cuc.md`; phạm vi A-03, D-04, D-05, D-06, E-03 trong `docs/review-doc-lap-m1.md`) và kế hoạch M2 `2026-11-18-crm-m2-khach-don.md` (đồng bộ tên theo mã). Loại việc (quy trình mục 3): A, chỉ tài liệu (không sửa mã, không cần test mới).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ sửa tài liệu. Ký hiệu chỉ được đọc để đối chiếu: `identity.ReplaceGrants`, `identity.SetLocked` (`staff.go`, trả `(Change, error)`), `order.AttachShipment`/`Writer.AttachShipmentBy`, `Writer.OnShipmentEvent`/`ApplyShipmentProgress`, `customer.Create`/`Writer.CreateCustomer`, sửa đơn (`edit.go`, `DeleteOrderItems`).
- Gói phụ thuộc ngược (go list): không áp dụng: không đổi gói Go.
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`; chỉ sửa tên operation `getOriginalImageURL` trong kế hoạch M1 theo hợp đồng đã gộp (E-03).
- Chỗ dùng trên màn hình: không đổi console.
- Câu sqlc, bảng, pha migration: không đổi.
- Tài liệu đổi: trong repo `docs/review-doc-lap-m1.md` (cột Xử lý của A-03, D-04, D-05, D-06, E-03), `docs/views.md` (Seq của đơn không bảo đảm liền nhau, D-06), tệp task này. Ngoài repo (thư mục kế hoạch của ELIMAZ, không có git): kế hoạch M1 (tên theo mã, E-03, mục tổng hợp tên theo mã), kế hoạch M2 (mục tổng hợp tên theo mã), một dòng giả định của kế hoạch M7 (`SetLocked`, E-03). Không sửa kế hoạch M3, M5 (Task 439).
- Test sẽ chạy: không có test Go mới. Cổng kiểm của task (`ci-task.sh`) một lần: cổng tĩnh, không gói Go nào đổi. Đối chiếu tên bằng `grep`/`go doc` trên main, trích vào bảng bằng chứng.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: không áp dụng cho mã (không viết hàm, kiểu). Với tài liệu: dùng lại cách ghi của `docs/m2/doi-chieu-m1.md` (Task 57: bảng "kế hoạch ghi → mã thật → kế hoạch đã sửa") và mục "Khác kế hoạch" của từng `docs/tasks/task-*.md` làm nguồn.
- Dùng lại / viết mới vì: không tạo tài liệu song song trong repo; tên thật sửa thẳng vào kế hoạch (luật Task M1-24 "sửa thẳng vào tệp kế hoạch khi lệch") kèm một mục tổng hợp trong mỗi kế hoạch để mốc sau tra nhanh.

## Soát thiết kế

Mục 5.1–5.12 và 6.1–6.13: không áp dụng: task chỉ sửa tài liệu, không đổi database, tiền, quyền, hàng đợi hay mã chạy. Các điểm nội dung tài liệu dính tới thiết kế được đối chiếu với mã thật, không đổi thiết kế:
- 6.x (đồng thời, A-03): ghi rằng chốt "chỉ xóa dòng đơn khi giữ hàng đã nhả" hiện nằm ở mã (`edit.go` nhả `held` rồi `DeleteOrderItems` trong cùng transaction; đơn đã có vận đơn hiện hành không sửa dòng), chưa có chốt ở database.
- 5.5 (sự kiện, D-06): ghi luật Seq của đơn chỉ tăng nhưng có thể nhảy — khớp cách hàng đợi `OrderStrict` chỉ so thứ tự.

## Bằng chứng

Task chỉ sửa tài liệu: bằng chứng là đối chiếu tên trên mã (main `90eeee6`), không có test mới. Diff kế hoạch (ngoài git) lưu ở thư mục scratchpad của phiên: `ke-hoach-2026-11-18-crm-m1-san-pham-kho.diff` (612 dòng), `…-m2-khach-don.diff` (558 dòng), `…-m7-dang-nhap-khoi-dong.diff` (11 dòng).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| A-03: không còn giữ hàng `held` mồ côi khi sửa đơn bỏ dòng | `TestEdit_RemoveAndAddLines` (có sẵn, Task 77) | `grep -n "ReleaseHeldByItems\|DeleteOrderItems" internal/modules/order/edit.go` | `edit.go:688 e.stock.ReleaseHeldByItems(…, releaseOrderEdited, a)` rồi `edit.go:710 e.q.DeleteOrderItems(…)` cùng tx; test kiểm `release_reason = 'order_edited' AND order_item_id IS NULL` (edit_test.go:160) |
| D-04: khách tạo qua đường M1 có `name_search`, `lifecycle` | `TestCreateM1Wrapper` (có sẵn, Task 66) | `sed -n 54,68p internal/modules/customer/customer.go` | `Create` gọi `w.CreateCustomer(ctx, tx, CreateInput{FullName: fullName, Source: SourceStaff}, …)`; `write.sql` `InsertCustomerProfile` ghi `name_search, lifecycle` |
| D-05: bước vận đơn trên đơn `cancel_requested` không lỗi mãi | `TestOnShipmentEvent_PickedUpWhileCancelRequested`, `TestOnShipmentEvent_DeliveredWhileCancelRequested` (có sẵn, Task 431) | `grep -n "StatusConfirmed, StatusCancelRequested" internal/modules/order/shiphooks.go` | `174: case StatusConfirmed, StatusCancelRequested:` |
| D-06: gắn vận đơn có sự kiện; luật Seq ghi rõ | không áp dụng (tài liệu) | `grep -rn 'order\.AttachShipment(\|AttachShipmentBy(' internal \| grep -v _test.go` | chỉ `fulfillment/area_create.go:158 ow.AttachShipmentBy(…)` (phát `order.updated`, `shiphooks.go:37 emitUpdated`); lớp bọc M1 không còn chỗ gọi ngoài test. `docs/views.md` thêm câu "Seq chỉ tăng, có thể nhảy" |
| E-03: kế hoạch M1, M7 theo chữ ký thật | không áp dụng | `grep -n "func ReplaceGrants\|func SetLocked" internal/modules/identity/staff.go`; `grep -n operationId: openapi/openapi.yaml \| grep -i original` | `189: func ReplaceGrants(…, version int64, grants []GrantInput) (Change, error)`, `246: func SetLocked(…, locked bool) (Change, error)`; `operationId: getOriginalImageURL` — kế hoạch M1 dòng Task 26, 321, 325 và M7 dòng 614 đã sửa (diff ở scratchpad) |
| Kế hoạch M1, M2 đồng bộ tên theo mã | không áp dụng | `diff` bản gốc ↔ bản sửa; soát mẫu `go doc`/grep: `promotion.Writer.Redeem(ctx, tx, code, OrderRef, EvalInput)`, `order.Background(background.Deps) background.Parts`, `files.Preset(version int)`, `testkit.StartPostgresSeeded(t, Seed)`, `order.Writer.Edit(ctx, tx, sellerID, id, in, a, now)` | đều khớp mã. M1: 89 chỗ sửa thẳng + mục "Tên theo mã" 54 dòng; M2: ~80 chỗ + mục "Tên theo mã" 39 dòng; 0 dòng checkbox đổi |
| Cổng kiểm | — | `ci-task.sh` | 21/11, sau khi gộp main `90eeee6`: `CI-TASK XANH (so với điểm tách nhánh e318c1e…)`; catalog-check, jscpd (0 clone), gitleaks (no leaks), oasdiff, test-guard, `ok elimaz.com.vn/crm/tests/arch` |

## Quyết định thay chủ shop

- **Nơi ghi đồng bộ tên:** sửa thẳng câu trong kế hoạch M1, M2 (luật Task M1-24) và thêm mỗi kế hoạch một mục "Tên theo mã (đồng bộ ở Task 415)" để mốc sau tra nhanh. Phương án khác: chỉ một tệp đối chiếu riêng trong repo (như `docs/m2/doi-chieu-m1.md`) — bỏ vì kế hoạch sẽ vẫn ghi tên sai. Đảo ngược: chép lại bản gốc (diff lưu đủ).
- **Kế hoạch M7 (ngoài M1, M2):** chỉ sửa đúng một dòng giả định `SetLocked` mà phát hiện E-03 nêu đích danh; không soát phần khác của M7. Không đụng M3, M5 (Task 439).
- **A-03 không thêm trigger database:** task này chỉ sửa tài liệu; chốt ở mã đã có và có test. Trigger BEFORE DELETE ghi vào Việc phát hiện thêm. Phương án khác: thêm migration ngay — ngoài phạm vi "chỉ tài liệu".
- **D-06:** chọn ghi luật "Seq có thể nhảy" vào `docs/views.md` (đề xuất thứ nhất của review) thay vì cho lớp bọc M1 phát sự kiện (sửa mã, lớp bọc không còn chỗ gọi).
- Lệch về cách làm (không phải tên) không viết lại task trong kế hoạch; chỉ ghi ở bảng khi mốc sau cần biết.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch M1, M2 nằm ngoài git (thư mục kế hoạch của ELIMAZ), nên commit chỉ gồm tài liệu trong repo; phần sửa kế hoạch kiểm bằng diff lưu ở scratchpad.

## Việc phát hiện thêm

- A-03: chưa có chốt ở database cho "chỉ xóa dòng đơn khi mọi giữ hàng của nó đã `released`/`consumed`" — nên thêm trigger BEFORE DELETE trên `ord.order_items` kèm test khi có task chạm sửa đơn.
- Khi soát M1: tệp task cho Task 55 (M1-56), 332, 424 không có trong `docs/tasks`; Task 31 ghi có `SignalHandler` nhưng mã không có kiểu/hàm tên đó (không ghi vào kế hoạch).
- Khi soát M2: tệp task cho 88, 433, 435 không có (434 có, mục Khác kế hoạch không có lệch tên); `data/admin-units/` và migration `0203` mà kế hoạch M2 nêu chưa có trong mã (chỉ ghi ở bảng "Tên theo mã").

## Reviewer

Kết luận: ĐẠT (vòng 1, 21/11). Không có phát hiện phải sửa. Reviewer đối chiếu mọi câu nói về mã trong phần stage, hơn 40 dòng mỗi diff kế hoạch M1, M2 và dòng M7 với mã trên nhánh: không lệch. Kết luận D-04, D-05, D-06 "đã sửa" và A-03 "chốt ở mã, chưa chốt database" đúng; câu Seq trong `docs/views.md` đúng (thứ tự nghiêm chỉ so `(order_seq, id) <` ở `queries/platform/jobs.sql:80-81`; consumer duy nhất đọc `e.Seq` là `cash`, dùng làm khóa bút toán, không đòi liền nhau). Ghi chú không bắt buộc: dấu `|` thừa ở dòng 415 của bảng task trong `review-doc-lap-m1.md` — đã sửa; mốc đối chiếu sau khi gộp là `e318c1e` (merge Task 447), phần mã đã kiểm không đổi so với `90eeee6`.
