# Task 430 — M2-r1: Gộp khách nhất quán với mã giảm giá

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (task bù M2-r1, bảng `danh-so-task-toan-cuc.md`; nguồn: `docs/review-doc-lap-m2.md` A-02, C-01, A-04). Loại việc (quy trình mục 3): A

Mục tiêu: (1) mã gắn khách so theo nhóm hiện tại (gốc hiện tại của khách gắn mã), không theo gốc lúc tạo mã; (2) giới hạn lượt mỗi khách đếm theo nhóm hiện tại (tổng lượt của mọi hồ sơ cùng gốc), đúng cả sau gộp và hoàn tác; (3) lên đơn không gắn SĐT khóa hồ sơ và gốc trước khi promote, để gốc dùng cho promote và cho mã là gốc đọc dưới khóa.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `promotion.EvalInput` thêm trường `CustomerID` (hồ sơ của đơn; chỉ thêm; 48 tham chiếu trong 8 tệp, chỗ dựng giá trị ngoài gói: `order/create_coupon.go:37`, `order/edit.go:553`, `httpapi/handlers_promotion.go:230` — Check không cần trường này, để trống). Đổi hành vi, không đổi chữ ký: `promotion.Writer.Redeem` (dòng lượt mỗi khách ghi theo `CustomerID`, điều kiện giới hạn đếm theo nhóm; `in.CustomerID` rỗng → lỗi lập trình), `promotion.Check` (đếm theo nhóm), `promotion.Check`/`Redeem`/`Reprice` (khách gắn mã đổi về gốc hiện tại trước `Evaluate`). `promotion.Evaluate` giữ thuần, không đổi. `customer.Writer.FindOrCreateForOrder`: không đổi chữ ký; hàm không xuất `useExisting`, `findOrCreate` luôn khóa hồ sơ + gốc (`lockPair`) trước promote.
- Gói phụ thuộc ngược (go list): `promotion` ← `order`, `httpapi`, `cmd/crm`; `customer` ← `order`, `promotion`, `httpapi`, `testkit/shiptest`, `cmd/crm`.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Hành vi của `createOrder`, `editOrder` (dùng mã), `checkCoupon` đổi đúng chỗ lỗi (mã gắn khách sau gộp được nhận, lượt mỗi khách tính theo nhóm). oasdiff: không có thay đổi.
- Chỗ dùng trên màn hình: không đổi màn hình (Lên đơn, Sửa đơn nhận kết quả `coupon-check` và lỗi `coupon_rejected` như cũ).
- Câu sqlc, bảng, pha migration: `queries/promotion/redeem.sql`: `IncrementCustomerUse` (điều kiện giới hạn chuyển sang tổng `used` của nhóm qua `customer.customers_v`), `CustomerUsed` (tổng theo nhóm). Dùng lại `CustomerRoot` (`manage.sql`) cho khách gắn mã. Bảng `promotion.coupon_customer_uses`, `coupon_redemptions`: không đổi cấu trúc; `customer_id` của dòng mới là hồ sơ của đơn (trước: gốc lúc dùng). Không migration.
- Tệp test đổi theo: `promotion/redeem_test.go` (helper `input` đặt cả `CustomerID`), `httpapi/handlers_promotion_test.go` (`TestCouponAPI_Redemptions` gọi Redeem thẳng: thêm `CustomerID`), `order/create_coupon_test.go` (helper `coupon` nhận thêm `perCustomer`, `boundCustomer` để test mới dùng lại, không chép lời gọi CreateCoupon — jscpd). Test mới: `promotion/merge_test.go`, `customer/fororder_lock_test.go`, `order/create_coupon_merge_test.go`.
- Test sẽ chạy: `go test -race` các gói `internal/modules/promotion`, `internal/modules/customer`, `internal/modules/order`, `internal/httpapi` (promotion, order là vùng rủi ro cao: cả bộ), rồi cổng `ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có helper "gốc hiện tại"/"nhóm khách" ở platform — đúng, đó là nghiệp vụ của customer); tìm `RootOf`, `CustomerRoot`, `GroupMembers`, `lockPair`, `customers_v`.
- Dùng lại / viết mới vì: dùng lại `promotion.CustomerRoot` (câu `CustomerRoot` đọc `customer.customers_v`) để đổi khách gắn mã về gốc hiện tại; dùng lại `customer.lockPair` (khóa hồ sơ + gốc theo mã tăng dần, đọc lại gốc dưới khóa) cho hai nhánh còn thiếu khóa; dùng lại `customer.MergeCustomers`/`UndoMerge` trong test. Viết mới: phần đếm theo nhóm nằm trong câu SQL có sẵn (không thêm hàm Go đọc số ra rồi ghi lại).

## Soát thiết kế

- 5.1 Chủ dữ liệu: lượt mã thuộc promotion (bảng `promotion.*`); nhóm khách (`root_id`) thuộc customer. Không bảng mới.
- 5.2 Không ghi bảng module khác: promotion chỉ ghi bảng của mình; gộp khách không chạm promotion (không cần: lượt tính theo nhóm lúc dùng).
- 5.3 Đọc qua view: promotion đọc `customer.customers_v(id, root_id)` (đã có; câu `CustomerRoot` đã dùng).
- 5.4 Nguyên tử: kiểm và tăng lượt của nhóm trong một câu `INSERT … SELECT … WHERE tổng nhóm < giới hạn`, trong tx tạo/sửa đơn của bên gọi.
- 5.5, 5.6: không áp dụng: không sự kiện, lệnh mới; payload `coupon.used` giữ `customerId` = gốc (đúng `docs/views.md`).
- 5.7 Tenant: mọi câu trong tx tenant của bên gọi; view `security_invoker`, RLS lọc.
- 5.8 Không HTTP trong module: giữ nguyên. 5.9 Đường ghi: không thêm đường ghi. 5.10 Valkey: không áp dụng. 5.11 Thư viện: không thêm. 5.12 Module mẫu: câu ghi có điều kiện (0 dòng → lý do `customer_limit`), như `applyDelta`.
- 6.1 Đồng thời: hai đơn cùng mã → cùng khóa dòng mã nên tuần tự; đơn và gộp chạy song song → lên đơn giữ khóa hồ sơ + gốc (`lockPair`, nhóm 5, trước đơn nhóm 7 và mã nhóm 10); gộp vào nhóm này phải khóa gốc giữ lại (`MergeCustomers` khóa `s` và cả nhóm bị gộp) nên chờ đơn commit; hoàn tác chỉ làm nhóm nhỏ đi → nếu xen giữa thì đếm thừa (an toàn: không vượt giới hạn). Sửa đơn (khóa đơn nhóm 7 trước) không khóa khách được mà không trái ADR-46 → ghi ở "Việc phát hiện thêm".
- 6.2 Gửi lại: không đổi (khóa chống trùng của tạo/sửa đơn).
- 6.3 Thứ tự khóa: lên đơn: customers (nhóm 5) → orders (7) → coupons → coupon_customer_uses → coupon_redemptions (10). Câu đếm nhóm chỉ đọc `customers_v` (không khóa). Có test gộp song song với lên đơn dùng mã (`TestCreateCoupon_MergeRaceNoDeadlock`): không deadlock, kết quả chỉ là một trong hai thứ tự hợp lệ.
- 6.4 Lỗi tạm: không đổi (RetryTx của bên gọi). 6.5 Cách ly: READ COMMITTED; câu đếm là một câu, đọc nhóm tại lúc câu chạy, dưới khóa gốc.
- 6.6–6.9: không áp dụng: không việc nền, gọi ngoài.
- 6.10 Tiền: không đổi cách tính phần giảm.
- 6.11 Quy luật: "lượt của một nhóm khách với một mã ≤ per_customer_limit" — test gộp, hoàn tác, song song.
- 6.12 Dữ liệu cá nhân: không log gì mới.
- 6.13 Quyền: không đổi.

## Quyết định thay chủ shop

1. **Lượt mỗi khách tính theo nhóm hiện tại** (phương án review đề xuất), không ghi spec "lượt theo hồ sơ lúc dùng". Vì: spec 05 mục 1 "mọi màn của khách này tra theo `root_id`, số tổng hợp tính lại sau gộp/hoàn tác"; không ghi spec thì khách đã dùng hết lượt dùng thêm được sau gộp. Khác: dồn lượt khi gộp (customer phải ghi bảng promotion — trái 5.2; hoàn tác phải tách lại).
2. **Dòng `coupon_customer_uses` ghi theo hồ sơ của đơn** (không theo gốc lúc dùng). Vì: nhóm đổi (gộp, hoàn tác) thì tổng theo nhóm tự đúng, kể cả lượt dùng trong thời gian đã gộp rồi hoàn tác. Khác: giữ khóa theo gốc lúc dùng (sau hoàn tác, lượt dùng lúc đang gộp nằm ở nhóm giữ lại). Đảo ngược dễ: chỉ là giá trị truyền vào câu ghi; dữ liệu cũ (gốc lúc dùng) vẫn là một hồ sơ nên vẫn được đếm đúng nhóm.
3. **Mã gắn khách so theo gốc hiện tại của khách gắn mã**, đọc ở `Redeem`/`Check`/`Reprice` trước `Evaluate` (Evaluate giữ thuần). Cột `bound_customer_id` không đổi (không có quyền sửa, không cần).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| C-01/A-02a: mã gắn A, gộp A vào B → hồ sơ B và A dùng được ở Check, Redeem, Reprice; hoàn tác → B bị `bound_other`, A dùng được; mã gắn B: A dùng được lúc gộp, không sau hoàn tác | `TestBoundCustomer_FollowsMerge` (promotion) | `go test -race -count=1 ./internal/modules/promotion/` | trước sửa: `sau gộp, hồ sơ B Check mã của A: {OK:false … Reason:bound_other}`; sau sửa: `ok … promotion 27.916s` |
| A-02b: 1 lượt/khách, A đã dùng, gộp A vào B → Check/Redeem của B và A `customer_limit`, used_count giữ 1; hoàn tác → B dùng được, A vẫn hết | `TestPerCustomerLimit_FollowsMerge` | như trên | trước sửa: `sau gộp, Check của B: {OK:true …}`; sau sửa: ok |
| Đếm theo tổng nhóm (không phải lớn nhất): giới hạn 2, A và B mỗi người 1, gộp → `customer_limit` | `TestPerCustomerLimit_SumsGroup` | như trên | trước sửa: `Check của nhóm đã dùng 2: {OK:true …}`; sau sửa: ok |
| Lượt ghi theo hồ sơ của đơn: dùng lúc đang gộp, hoàn tác → lượt về A, B dùng được; `coupon_redemptions.customer_id` = A | `TestPerCustomerLimit_UseDuringMergeThenUndo` | như trên | trước sửa: `sau hoàn tác, Check của B: {OK:false … customer_limit}`; sau sửa: ok |
| Trả lượt sau gộp về đúng dòng của A; nhóm dùng lại được | `TestRelease_AfterMerge` | như trên | ok (giữ hành vi, chặn hồi quy) |
| Redeem thiếu `CustomerID` → lỗi lập trình, không đổi lượt | `TestRedeem_RequiresCustomerID` | như trên | trước sửa: `lỗi = <nil>`; sau sửa: ok |
| A-04: lên đơn (hồ sơ có sẵn không gắn SĐT; tìm theo SĐT) chờ lần gộp chưa commit, sau đó RootID = gốc mới S, S thành customer, X (con) vẫn lead | `TestFindOrCreateForOrder_LocksRootAgainstMerge` (2 ca) | `go test -race -count=1 ./internal/modules/customer/` | trước sửa: cả 2 ca `RootID <X>; mong … <S>`; sau sửa: `ok … customer 51.068s` |
| Lên đơn thật (order.Create): mã gắn A + 1 lượt/khách, gộp A vào B → đơn của B giảm 25.000; đơn tiếp của hồ sơ A → `customer_limit` | `TestCreateCoupon_BoundCustomerAfterMerge` (order) | `go test -race -count=1 -run TestCreateCoupon ./internal/modules/order/` | `ok … order 8.005s` |
| Gộp song song với lên đơn dùng mã, 10 vòng: không deadlock, chỉ hai kết quả hợp lệ | `TestCreateCoupon_MergeRaceNoDeadlock` | như trên; cả gói `go test -race -count=1 ./internal/modules/order/` | `ok … order 180.362s` |
| Đơn hàng có sẵn của API lượt dùng mã vẫn chạy (EvalInput có CustomerID) | `TestCouponAPI_Redemptions` (httpapi) | `go test -race -count=1 -run 'TestCouponAPI|TestCheckCoupon' ./internal/httpapi/` | `ok … httpapi 3.390s` |
| Cổng kiểm của task (build, vet, lint, gen-check, sqlc-vet, jscpd, gitleaks, oasdiff, test-guard, test gói đổi + phụ thuộc + vùng cao) | cổng `ci-task.sh` | chạy trên phần đã stage, sau khi gộp main 242ceee | `CI-TASK XANH (so với điểm tách nhánh 242ceee…)`; test 309 giây. Lần chạy trước đó ĐỎ (gci 2 tệp, nolintlint, 2 clone jscpd trong test, `TestCouponAPI_Redemptions` thiếu `CustomerID`) → đã sửa |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Sửa đơn (`order/edit.go`, `evalInput`) đọc gốc của khách bằng `customer.RootOf` không khóa: đường sửa khóa đơn (nhóm 7) trước, khóa khách (nhóm 5) sau đó thì trái ADR-46. Gộp chạy xen đúng lúc sửa đơn đổi mã → câu đếm nhóm đọc nhóm tại lúc chạy (đã commit) nên chỉ lệch trong khe gộp chưa commit, nhiều nhất thêm 1 lượt cho nhóm. Muốn kín: sửa đơn khóa khách trước đơn (đổi thứ tự khóa của Edit) — việc riêng, chưa làm.
- `docs/nghiem-thu-m2.md` dòng 4, 5 còn ghi "task bù 430": điều phối viên cập nhật khi gộp.

## Reviewer

Kết luận: ĐẠT (lần 1, trên phần đã stage sau khi gộp main 242ceee).

- Phát hiện phải sửa: không có. Reviewer kiểm: `currentBound` gọi ở Redeem, Reprice, Check trước `Evaluate`; câu `IncrementCustomerUse` đếm theo nhóm có `coalesce`, chạy dưới khóa dòng mã; trả lượt dùng `r.CustomerID` của chính dòng redemption nên đúng cả với dữ liệu cũ; `lockPair`/`lockFound` khóa theo id tăng dần như `LockCustomers` của gộp; nơi gọi duy nhất `create_m2.go:142` khóa khách trước đơn (ADR-46).
- Ảnh hưởng dự kiến khớp thực tế; không sót chỗ dựng `EvalInput`; payload `coupon.used` vẫn là gốc, khớp `docs/views.md`.
- Ghi chú không bắt buộc (không sửa): `waitLockWaiter` đếm mọi phiên chờ khóa của database thử (database riêng mỗi test nên đủ; test đã đỏ trước khi sửa); `TestCreateCoupon_MergeRaceNoDeadlock` chỉ là bằng chứng không deadlock, không chứng minh thứ tự; `t.Fatal` trong `waitLockWaiter` để goroutine gộp treo (chỉ khi test đã đỏ); sửa đơn chưa khóa khách — đã ghi ở Việc phát hiện thêm.
