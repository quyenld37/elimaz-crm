# Review độc lập toàn M2 (Task 89, M2-32)

Ngày 19/11/2026. Năm reviewer mới (agent riêng, chỉ có spec 2.1, kế hoạch M2, threat model và mã; không có lập luận của
người viết, không đọc tệp task hay commit message làm bằng chứng), chia năm vùng: A khách hàng, B vòng đời đơn và tồn,
C tiền và mã giảm giá, D màn hình M2, E chéo toàn M2 (ca nghiệm thu → test, hợp đồng OpenAPI, threat model, sự kiện,
tên mốc sau dựa vào). Bản được soát: main `01173b9` (M2 task 57–87, 90, 425, 427). Thử tải M2 (Task 88) dời theo chủ
shop 19/11, không tính là phát hiện. Các chỗ màn hình khác mockup vì API thiếu đã do điều phối viên gom riêng, không
báo lại. Lời giao chung ở cuối tệp; chi tiết từng vùng (mô tả, bằng chứng, đề xuất, "Đã kiểm, không thấy lỗi") ở Phụ lục.

**Tổng:** 0 nghiêm trọng, 7 lớn, 17 nhỏ, 4 nghi ngờ (28 phát hiện; A-02 và C-01 là một lỗi thấy từ hai vùng).
Sửa ngay trong Task 89: C-02, A-03, D-03 (mã, mỗi sửa có test: `TestEdit_DiscountApprovalAmountUp`,
`TestStatsPromotesRootAfterMerge`, vitest "lọc Hôm nay theo ngày hiện tại khi màn mở qua nửa đêm"), E-01, E-03, E-04,
E-07, E-09 (tài liệu, hợp đồng). Phần còn lại tách thành task bù 430–439 (bảng dưới; điều phối viên ghi vào
`docs/superpowers/plans/danh-so-task-toan-cuc.md`).

**Ca kiểm:** 19/19 ca M2 của `11-kich-ban-kiem-thu.md` có test máy kiểm đúng tình huống, chạy lại đều đạt (bảng ở Phụ
lục E, mục 1; tổng hợp ở `docs/nghiem-thu-m2.md`). Ca AT/WAT: AT05, WAT07 chỉ có test cho một nửa (E-05), WAT31 thiếu
ca database có giảm cấp đơn sau đổi giá (E-06), AT07 phần "đổi kho" không thuộc M2 (E-07).

## Bảng phát hiện

| Mã | Mức | Tệp:dòng chính | Việc | Xử lý |
|---|---|---|---|---|
| A-01 | lớn | internal/modules/customer/fororder.go:126-134; console/src/screens/len-don/AddressFields.tsx:133-160 | Lên đơn "khách mới" với SĐT của hồ sơ có sẵn (ô "Số này là của khách" bật) → đơn gắn vào hồ sơ đó, không cảnh báo; màn hình lại báo "Không tự gộp" | Task 435 (mã đúng kế hoạch Task M2-09; cần chủ shop chọn hành vi) |
| A-02 | lớn | internal/modules/promotion/evaluate.go:72; redeem.go:102, 278-279 | Mã gắn khách bị từ chối sau khi khách được gộp; lượt mỗi khách đếm theo gốc lúc dùng nên vượt giới hạn sau gộp | Task 430 |
| A-03 | nhỏ | internal/modules/customer/merge.go:97-159; stats.go:125-161 | Gộp không đổi `lifecycle` của gốc: nhóm đã mua vẫn hiện "Tiềm năng" | **Đã sửa** (`recompute` đưa gốc lead sang customer khi nhóm có đơn; test `TestStatsPromotesRootAfterMerge`) |
| A-04 | nhỏ | internal/modules/customer/fororder.go:90-96 | Tìm-hoặc-tạo khi không gắn SĐT đọc gốc không khóa; gộp xen giữa → promote và mã dùng gốc cũ | Task 430 |
| A-05 | nghi ngờ | console/src/screens/len-don/orderRules.ts:169-176 | Khách có sẵn chưa có SĐT không có đường gắn SĐT khi lên đơn đầu (spec: ô mặc định bật) | Task 435 |
| B-01 | lớn | internal/modules/order/shiphooks.go:119-125 | `OnShipmentEvent(picked_up/delivered)` từ chối đơn `cancel_requested`; kế hoạch M3 (VC12) gọi đúng thứ tự này → sự kiện hãng lỗi mãi | Task 431 (chốt với kế hoạch M3) |
| B-02 | nhỏ | internal/modules/order/shiphooks.go:73-91 | `DetachShipment` không kiểm trạng thái đơn: đơn `cancel_requested` mất vận đơn thì kẹt | Task 431 |
| B-03 | nhỏ | internal/modules/order/duplicates.go:364-387 | Xử lý đơn trùng "hủy" đơn đã gửi hãng ghi kết luận `cancelled` dù đơn chỉ `cancel_requested` | Task 432 |
| B-04 | nhỏ | internal/modules/order/cancel.go:191-223 | Hủy thường không gỡ cờ `duplicate_suspect` của đơn kia trong cặp | Task 432 |
| B-05 | nhỏ | internal/modules/order/drafts.go:97; duplicates.go:192 | Gọi `time.Now()` trong module (Global Constraints "Đồng hồ") | Task 436 |
| B-06 | nghi ngờ | internal/httpapi/handlers_order_returns.go:65-103 | Phiếu đổi tạo đơn thay thế (giữ hàng, có thể `confirmed`) chỉ với quyền `order.return`, không kiểm phạm vi kho | Task 436 (quyết định quyền) |
| C-01 | lớn | internal/modules/promotion/evaluate.go:72; manage.go:194 | Như A-02 (thấy từ vùng mã giảm giá) | Task 430 |
| C-02 | lớn | internal/modules/order/edit.go:483 | Sửa đơn: nhân viên không có quyền duyệt tăng tiền giảm tay tùy ý nếu tỷ lệ không vượt tỷ lệ đã duyệt | **Đã sửa** (test `TestEdit_DiscountApprovalAmountUp`) |
| C-03 | nhỏ | internal/modules/promotion/redeem.go:201; order/edit.go:552 | Sửa đơn khi mã đã hết hạn/tắt → `coupon_rejected`; luật SĐT gắn mã lúc sửa khác lúc tạo | Task 433 |
| C-04 | nghi ngờ | internal/modules/order/returns.go:633 | Đơn hãng hoàn (giao không thành) không trả lượt mã; Q2 chưa nói ca này | Task 433 (cần chủ shop) |
| C-05 | nhỏ | internal/modules/promotion/redeem.go:81-95 | Mã tính ra giảm 0đ vẫn tiêu một lượt | Task 433 |
| D-01 | nhỏ | console/src/screens/don/DuplicatePairs.tsx:42-43, 112-116 | Hộp "Hủy đơn này" ở màn Đơn trùng hứa nhả hàng, trả lượt cả khi đơn đã gửi hãng | Task 432 |
| D-02 | nhỏ | console/src/screens/len-don/OrderForm.tsx:245-258 | Sửa đơn có mã %: tổng tạm tính dùng số giảm cũ khi dòng đổi | Task 437 |
| D-03 | nhỏ | console/src/screens/don/OrderList.tsx:139 | Bộ lọc "Hôm nay" cố định lúc mở màn, sai khi màn mở qua nửa đêm | **Đã sửa** (`useVnToday`; vitest `OrderList.test.tsx`) |
| E-01 | lớn | openapi/openapi.yaml (34 operation) | Mô tả "Chưa làm… (501)" cho operation đã có handler | **Đã sửa** (xóa mô tả, `make gen`) |
| E-02 | lớn | kế hoạch M3 (G-M2-3), M5; kế hoạch M2 Task 79 Interfaces | Chữ ký móc nối trong kế hoạch lệch mã thật (`order.Actor`, `sellerID`, `Edit`, phương thức `Writer`) | Task 439 (sửa kế hoạch theo mã) |
| E-03 | nhỏ | docs/threat-model-m2.md | Threat model chưa trỏ tên test | **Đã sửa** |
| E-04 | nhỏ | docs/threat-model-m2.md (bảng 4) | Ghi `checkCoupon` trả lý do chung; mã trả lý do cụ thể (KM02) | **Đã sửa** (ghi đúng; M5 phải gộp `not_found`) |
| E-05 | nhỏ | internal/modules/order/confirm.go | AT05, WAT07: không test nào khẳng định xác nhận đơn không giữ hàng, không ghi lượt mã lần hai | Task 438 |
| E-06 | nhỏ | internal/modules/order/returns_test.go | WAT31 thiếu ca database: trả một phần đơn có giảm cấp đơn/mã sau khi đổi giá | Task 438 |
| E-07 | nhỏ | docs/nghiem-thu-m2.md | AT07 "đổi kho" không có trong M2 mà nghiệm thu ghi đạt trọn | **Đã sửa** (ghi "phần tăng lượng") |
| E-08 | nghi ngờ | openapi.yaml `resolveOrderDuplicate` | Hủy qua xử lý đơn trùng không mang `version` | Task 432 |
| E-09 | nhỏ | docs/views.md | Topic `order.delivery_reversed` (consumer M2 đã nghe) chưa có trong bảng sự kiện | **Đã sửa** (dòng giữ chỗ "M3 phát") |

## Task bù (điều phối viên ghi vào `docs/superpowers/plans/danh-so-task-toan-cuc.md`)

| Số | Tên | Phát hiện | Ghi chú |
|---|---|---|---|
| 430 | Gộp khách nhất quán với mã giảm giá | A-02, C-01, A-04 | So mã gắn khách và đếm lượt theo nhóm (gốc hiện tại); khóa gốc trước promote khi lên đơn không gắn SĐT. Test gộp + mã. (A-03 đã sửa ở Task 89) |
| 431 | Móc nối vận đơn khi đơn `cancel_requested` | B-01, B-02 | Chọn một: `OnShipmentEvent(picked_up)` nhận `cancel_requested`, hoặc sửa kế hoạch M3 gọi `EffCancelRejected` trước; `DetachShipment` chỉ nhận `confirmed`. Phải làm trước M3 Task 8 |
| 432 | Đơn trùng: hủy đơn đã gửi hãng, cờ nghi trùng, version | B-03, B-04, D-01, E-08 | Không đóng cặp khi đơn chỉ `cancel_requested`; gỡ cờ khi hủy thường (khóa theo id tăng dần); câu hộp xác nhận theo trạng thái |
| 433 | Luật mã khi sửa đơn; mã giảm 0đ; lượt khi hãng hoàn | C-03, C-05, C-04 | C-04 cần chủ shop (mở rộng Q2) |
| 434 | Lệnh vận hành tính lại chỉ số khách cho một gốc | (RUNBOOK, Task 89) | Hiện chỉ thử lại việc `dead` được; xem `docs/RUNBOOK.md` mục M2 |
| 435 | Lên đơn: SĐT của hồ sơ có sẵn khi chọn "khách mới"; gắn SĐT cho khách có sẵn chưa có SĐT | A-01, A-05 | Cần chủ shop chọn: gắn vào hồ sơ có SĐT (như đơn web) hay tạo khách mới + cảnh báo "có thể là cùng một khách" |
| 436 | Đồng hồ trong module order; quyền của phiếu đổi | B-05, B-06 | B-06 cần ghi quyết định vào `docs/quyen.md` |
| 437 | Màn hình: tổng tạm tính khi sửa đơn có mã % hay có mức tối thiểu | D-02 | (D-03 đã sửa ở Task 89) |
| 438 | Test nghiệm thu còn thiếu: AT05, WAT07 (xác nhận không giữ thêm), WAT31 (database) | E-05, E-06 | Chỉ thêm test |
| 439 | Sửa kế hoạch M3, M5 và Interfaces Task 79 (M2) theo chữ ký thật | E-02 | Chỉ tài liệu, ngoài repo CRM |

## Phụ lục — chi tiết từng vùng

### Vùng A — Khách hàng

Phạm vi đã đọc: `internal/modules/customer/*.go` (trừ test, đọc lướt tên test), `queries/customer/*.sql`,
`migrations/0201_customer.sql`, `0207_customer_search_fn.sql`, `internal/httpapi/handlers_customer_merge.go`, phần
`x-permission` của khách trong `openapi/openapi.yaml`, chỗ order gọi customer (`order/create_m2.go`, `create_coupon.go`),
`promotion/evaluate.go`, `queries/promotion/redeem.sql` (chỗ dùng gốc khách), màn `console/src/screens/len-don/{OrderForm,
AddressFields,orderRules}.tsx|ts`, `console/src/screens/khach/{CustomerProfile,MergeView,MergeHistory}.tsx`.

Lệnh đã chạy: `go test -count=1 ./internal/modules/customer/...` → `ok elimaz.com.vn/crm/internal/modules/customer 67.715s`.

Tổng: 0 nghiêm trọng, 2 lớn, 2 nhỏ, 1 nghi ngờ.

---

##### A-01: Lên đơn "khách mới" với SĐT đã thuộc hồ sơ khác → đơn tự gắn vào hồ sơ đó, không cảnh báo; màn hình lại báo "Không tự gộp"
- Mức: lớn
- Tệp:dòng: internal/modules/customer/fororder.go:126-134; console/src/screens/len-don/AddressFields.tsx:133-160; console/src/screens/len-don/orderRules.ts:170-175
- Spec/kế hoạch: 05 mục 1 bảng quy tắc ("SĐT đã thuộc hồ sơ khác: Không tự gộp trong bất kỳ trường hợp nào. Hiện 'Có thể là cùng một khách'…; người có quyền bấm gộp"); ca DH12, DH13; Review Focus 4 ("SĐT người nhận tự gắn thành SĐT của khách").
- Mô tả: Màn Lên đơn, chọn "khách mới", ô "Số này là của khách" **bật sẵn**, gửi `customer: {fullName, phone: <SĐT người nhận>, phoneIsCustomers: true}`. Máy chủ (`findOrCreate`) thấy SĐT đã là danh tính của hồ sơ B thì **dùng luôn hồ sơ B** (`Created=false`), bỏ tên vừa gõ, và không trả `PhoneConflictWith` (chỉ nhánh `useExisting` mới trả), nên đơn không có cảnh báo `phone_belongs_to_other_customer`. Tình huống DH12: con đặt hộ, nhân viên tạo "khách mới" tên con, nhập số của mẹ (mẹ đã là khách) và quên tắt ô → đơn, chỉ số, lượt mã giảm giá đều thành của mẹ — đúng thứ DH12 cấm ("hồ sơ khách không bị gộp nhầm"). Trong khi đó màn hình hiện hộp "Có thể là cùng một khách … Không tự gộp." (nhánh `customer?.kind !== "existing"`), tức hứa một hành vi máy chủ không làm.
- Bằng chứng:
  ```go
  // fororder.go:126
  if linkPhone {
      o, found, err := identityOwner(ctx, tx, IdentityPhone, "", string(out.Phone))
      ...
      if found {
          out.CustomerID, out.RootID = o.customerID, o.rootID
          return nil            // không PhoneConflictWith, không tạo khách mới
      }
  }
  ```
  Test `TestFindOrCreateForOrder/không mã khách, SĐT của B` (fororder_test.go ~dòng 117) khẳng định đúng hành vi này: `FullName: "Ai đó", Phone: <SĐT của B>, PhoneIsCustomers: true` → `got.CustomerID == b.ID`, không tạo khách.
  Màn hình: `AddressFields.tsx:136-139` `"…Không tự gộp."` cho khách mới; `:149-160` ô bật sẵn chỉ cho `kind === "new"`.
- Đề xuất sửa: với nhân viên lên đơn (không phải đơn web), SĐT đã có chủ thì không dùng hồ sơ chủ: tạo khách mới không gắn SĐT và trả `PhoneConflictWith` (như nhánh `useExisting`), hoặc từ chối để màn hình bắt chọn "dùng hồ sơ có sẵn" / "khách khác". Giữ hành vi "gắn vào hồ sơ có SĐT" chỉ cho nguồn `web` theo đúng dòng spec. Sửa câu trên màn cho khớp. Cỡ: nhỏ (backend) + nhỏ (màn hình).

##### A-02: Mã giảm giá gắn khách và giới hạn lượt/khách so theo gốc lúc dùng → sai sau khi gộp
- Mức: lớn
- Tệp:dòng: internal/modules/promotion/evaluate.go:72; internal/modules/promotion/redeem.go:102, 278-279; queries/promotion/redeem.sql:14-32 (chạm vùng promotion, nguyên nhân là ngữ nghĩa gộp của customer)
- Spec/kế hoạch: 05 mục 1 "Mọi màn 'của khách này' tra theo `root_id`… Các con số tổng hợp tính lại từ dữ liệu gốc sau khi gộp hoặc hoàn tác"; Review Focus 2 (lượt mã không vượt).
- Mô tả: (a) Mã gắn khách lưu `bound_customer_id` = gốc lúc tạo mã (M). Sau khi gộp M vào S, đơn của chính người đó có `CustomerRootID = S` → `boundMatches` sai → mã bị từ chối `bound_other` dù đúng người (trừ khi mã cũng gắn SĐT và đơn khớp SĐT). (b) Lượt mỗi khách đếm ở dòng `coupon_customer_uses(customer_id = gốc lúc dùng)`. M đã dùng 1/1 lượt; sau khi gộp M→S, đơn mới của nhóm có gốc S → `CustomerUsed(S) = 0` → được dùng thêm, vượt `per_customer_limit` của cùng một người. Sau hoàn tác thì ngược lại. Không có bước nào sau gộp/hoàn tác dồn hay tra lượt theo nhóm.
- Bằng chứng:
  ```go
  // evaluate.go:72
  if c.BoundCustomerID != uuid.Nil && c.BoundCustomerID == in.CustomerRootID { return true }
  ```
  ```sql
  -- redeem.sql CustomerUsed
  SELECT coalesce(max(used), 0)::int AS used FROM promotion.coupon_customer_uses
  WHERE coupon_id = @coupon_id AND customer_id = @customer_id;
  ```
  `merge.go` chỉ đổi `root_id` của hồ sơ, không chạm promotion; `StatsConsumer` chỉ tính chỉ số đơn.
- Đề xuất sửa: so khách gắn mã theo nhóm (`customer.RootOf(bound) == CustomerRootID`), đếm lượt theo nhóm (tổng `used` của mọi `customer_id` có cùng gốc hiện tại, đọc qua `customer.customers_v`, khóa theo thứ tự ADR-46) — hoặc ghi rõ quyết định "lượt tính theo hồ sơ lúc dùng" vào spec nếu chủ shop chấp nhận. Cỡ: nhỏ–lớn (chạm Redeem, Check, cần test gộp + mã).

##### A-03: Gộp không cập nhật `lifecycle` của gốc giữ lại → khách đã mua hiện "Tiềm năng"
- Mức: nhỏ
- Tệp:dòng: internal/modules/customer/merge.go:97-159; internal/modules/customer/stats.go:125-161; console/src/screens/khach/CustomerProfile.tsx:428-429
- Spec/kế hoạch: 05 mục 1 ("Các con số tổng hợp tính lại … sau khi gộp hoặc hoàn tác"; lifecycle lead → customer ở đơn đầu tiên).
- Mô tả: Gốc S là `lead` (tạo tay, chưa đơn); gộp nhóm M (đã có đơn, `customer`) vào S. `mergeOnce` chỉ đổi `root_id`; consumer `customer_stats` tính lại số đơn cho S (order_count > 0) nhưng không đụng `lifecycle`. Hồ sơ (đọc `lifecycle` của gốc) hiện chip "Tiềm năng" dù nhóm đã mua; quy tắc/bộ lọc theo lifecycle (mốc sau) sẽ sai. Chỉ tự sửa khi có đơn mới (promote).
- Bằng chứng: `grep -n "lifecycle\|promote" merge.go stats.go queries/customer/merge.sql queries/customer/stats.sql` → không có kết quả.
- Đề xuất sửa: trong `recompute` (hoặc `mergeOnce`) đặt gốc sang `customer` khi `order_count > 0` của nhóm (dùng `PromoteCustomer`). Cỡ: nhỏ.

##### A-04: Tìm-hoặc-tạo khách khi lên đơn dùng gốc đọc không khóa cho promote và mã giảm giá
- Mức: nhỏ
- Tệp:dòng: internal/modules/customer/fororder.go:90-96 (`useExisting` khi không gắn SĐT), 127-133 (`findOrCreate` nhánh tìm thấy), 78-82 (`promote(out.RootID)`); internal/modules/order/create_coupon.go:38-40 (`CustomerRootID: cust.RootID`)
- Spec/kế hoạch: ADR-46 điểm 4 (đọc lại dưới khóa); 05 mục 1 (đơn tạo dở lúc gộp không lạc).
- Mô tả: Hai nhánh này đọc `root_id` bằng `RootOf`/`GetIdentityOwner` không khóa. Một lần gộp commit giữa lúc đọc và `promote` → `PromoteCustomer` đổi `lifecycle` của hồ sơ đã thành con (gốc thật vẫn `lead`), và `Redeem` dùng `CustomerRootID` cũ (lượt/gắn mã tính cho nhóm cũ — xem A-02). Đơn vẫn gắn đúng `customer_id` (không mất dữ liệu). Nhánh có gắn SĐT thì đã dùng `lockPair` nên không bị.
- Bằng chứng: `fororder.go:90 root, err := RootOf(ctx, tx, id)` rồi `:95 if !linkPhone { return nil }` — không khóa; `:79 w.promote(ctx, tx, out.RootID, …)`.
- Đề xuất sửa: luôn `lockPair(id, root)` (hoặc khóa gốc rồi đọc lại) trước `promote`, kể cả khi không gắn SĐT. Cỡ: nhỏ.

##### A-05: Lead có sẵn (chưa có SĐT) không bao giờ được gắn SĐT khi lên đơn từ màn hình
- Mức: nghi ngờ
- Tệp:dòng: console/src/screens/len-don/orderRules.ts:169-176; console/src/screens/len-don/AddressFields.tsx:149
- Spec/kế hoạch: 05 mục 1 "Nó chỉ thành danh tính `phone` của khách khi nhân viên giữ ô 'Số này là của khách' (mặc định bật cho đơn đầu của hồ sơ chưa có SĐT)".
- Mô tả: Ô "Số này là của khách" và trường `phoneIsCustomers` chỉ có khi `customer.kind === "new"`; chọn khách có sẵn thì request không bao giờ mang `phoneIsCustomers`, nên hồ sơ có sẵn chưa có SĐT (ví dụ lead tạo tay, sau này lead từ chat M4) không có đường gắn SĐT khi lên đơn đầu, dù máy chủ hỗ trợ (`useExisting` + `linkPhone`). Có thể là lựa chọn có chủ đích (mockup); nhờ điều phối viên đối chiếu mockup Lên đơn.
- Bằng chứng: `orderRules.ts:169 ...(f.customer?.kind === "existing" ? { customerId: f.customer.id } : {})` — không kèm `customer.phone/phoneIsCustomers`.
- Đề xuất sửa: với khách có sẵn mà hồ sơ chưa có SĐT, hiện ô (bật sẵn) và gửi `customer: {phone, phoneIsCustomers}` kèm `customerId`. Cỡ: nhỏ.

---

#### Đã kiểm, không thấy lỗi

- **RLS / tenant (PQ08):** mọi câu trong `queries/customer/*.sql` chạy dưới `tenancy.WithTenant`; chèn lấy `tenant_id` từ `NULLIF(current_setting('app.tenant_id', true), '')`; mã khách/nhãn/lần gộp/lần nhập của tenant khác → `ErrNotFound` (404) qua `RootOf`, `GetMerge`, `GetImport`, `tagExists`. Hàm `customer.search_by_name` (0207): `SECURITY DEFINER`, chủ `crm_search`, `search_path` cố định, tự lọc `tenant_id = NULLIF(current_setting(...))` (ngoài ngữ cảnh → NULL → 0 dòng), `REVOKE … FROM PUBLIC`, chỉ `GRANT EXECUTE` cho crm_app; thoát `\ % _` trong mẫu LIKE; mảng từ rỗng → 0 dòng.
- **Gộp / hoàn tác (DH14, Review Focus 4):** khóa `{gốc giữ lại} ∪ nhóm bị gộp` trong một câu `ORDER BY id FOR UPDATE`, đọc lại gốc và nhóm dưới khóa, savepoint + thử lại 3 lần; FK `root_id` DEFERRABLE; hoàn tác khóa `moved_ids` trước `customer_merges` (ADR-46), kiểm `undone_at` dưới khóa (hai lần hoàn tác song song → 409), chặn khi survivor đã bị dời bởi lần gộp sau (`BlockingMerge`), kiểm mọi hồ sơ dời còn trỏ survivor; không hồ sơ nào trỏ vào hồ sơ không phải gốc trong các chuỗi gộp tôi lần (M→S rồi S→Y; P→M rồi M→S; hoàn tác ngược thứ tự). Đơn, địa chỉ, ghi chú, danh tính giữ `customer_id` nên hoàn tác sau khi có đơn mới không lạc dữ liệu. Sự kiện gộp/hoàn tác → consumer tính lại chỉ số và nhãn quy tắc cho cả hai gốc. Quyền `customer.merge` chỉ owner, manager; test HTTP có ca sale → 403.
- **Đồng thời gộp × lên đơn × chỉ số:** đường lên đơn có gắn SĐT khóa (hồ sơ, gốc) tăng dần; gộp và `lockGroups` khóa cả tập trong một câu tăng dần; consumer chỉ số khóa nhóm trước khi chạm `customer_tag_links`; không thấy vòng chờ. Có test `TestMergeVsForOrderNoDeadlock`, `TestStatsConcurrentWithMerge`, `TestFindOrCreateForOrderRace`, `TestMergeConcurrentOpposite`.
- **DH12 phía máy chủ:** không `PhoneIsCustomers` thì SĐT không thành danh tính, không tìm theo SĐT (test `DH12`, `NewNotCustomers`). Có `CustomerID` + SĐT thuộc nhóm khác → không gắn, trả `PhoneConflictWith` (DH13).
- **Danh tính:** `UNIQUE (tenant_id, kind, scope, value)`, `scope NOT NULL DEFAULT ''`; chèn `ON CONFLICT DO NOTHING` rồi đọc chủ, thử lại khi chủ vừa bị gỡ; tạo khách trong savepoint nên SĐT bị chiếm không để lại hồ sơ mồ côi.
- **Chuẩn hóa SĐT:** `0`/`84`/`+84` + 9–10 chữ số, phần sau không bắt đầu bằng 0; tìm theo đuôi số bằng khoảng trên `value_reversed` (không LIKE trên bảng dưới RLS).
- **Chỉ số khách:** `coalesce(sum…, 0)`, `coalesce(max…, 'epoch')` + `order_count = 0` → NULL; refund chỉ trừ cho đơn delivered, không âm; hồ sơ con về 0.
- **Nhập Excel (KH01):** SĐT đã có hay lặp trong tệp → skipped; mỗi lô một transaction, khóa `customer_imports` cuối cùng và so tiến độ (chạy song song → lùi cả lô); lỗi dòng chỉ `{row, column, code}`, không giá trị; một dòng audit khi xong; tệp kiểm mục đích/trạng thái/tenant.
- **Dữ liệu cá nhân:** kiểu `Name`, `Birthday`, `NoteBody`, `IdentityValue`, `logging.Phone/Address` tự che; audit chỉ tên trường, mã; payload sự kiện chỉ mã (`Created`, `MergeEvent`); lỗi `IdentityTakenError`/`MergeNotLatestError` chỉ mang mã; dòng thời gian không đưa tên/SĐT vào `summary` (`reason` của lịch sử đơn là mã theo CHECK `^[a-z][a-z0-9_]{0,63}$`); `search.go` không log `q`.
- **Địa chỉ, ghi chú, nhãn:** khóa hồ sơ trước địa chỉ (ADR-46), một địa chỉ mặc định nhờ unique index một phần; ghi chú chỉ ghi thêm (trigger theo câu lệnh); nhãn quy tắc không gỡ tay được (`ErrTagRuleManaged`); lịch đêm khóa gốc theo id tăng dần.
- Bộ test gói `customer` chạy qua (67,7 s).

### Vùng B — Vòng đời đơn và tồn

Người soát: agent vùng B. Chỉ đọc mã ở `W` (nhánh task-89 = main). Không có phát hiện mức nghiêm trọng.
Tổng: 0 nghiêm trọng, 1 lớn, 4 nhỏ, 1 nghi ngờ.

##### B-01: Móc nối vận đơn từ chối "hãng đã lấy hàng" khi đơn đang `cancel_requested`, lệch hợp đồng mà M3 dựa vào
- Mức: lớn
- Tệp:dòng: internal/modules/order/shiphooks.go:119-125 (`OnShipmentEvent`)
- Spec/kế hoạch: 05 mục 2, sơ đồ vận đơn `cancel_requested --> shipping: hãng đã lấy trước khi hủy`; kế hoạch M3 Task 8
  bảng chuyển, dòng VC12: `cancel_requested` + `PickedUp` → tác động `[EffPickedUp, EffCancelRejected]` theo đúng thứ tự đó;
  kế hoạch M3 dòng 959 (`rejected` → `CanonPickedUp` → `EffPickedUp` + `EffCancelRejected`). G-M2-3 nói M3 "dùng nguyên,
  không viết bản thứ hai".
- Mô tả: đơn đang `cancel_requested` (TD03), hãng lấy hàng trước khi kịp hủy. M3 gọi `EffPickedUp` → `OnShipmentEvent(…,
  picked_up)` trước, rồi mới `EffCancelRejected`. `OnShipmentEvent` chỉ nhận đơn `confirmed`/`delivered`; với
  `cancel_requested` nó trả `ErrStatusConflict`. Cả transaction của fulfillment lùi lại: vận đơn không sang `shipping`,
  giữ hàng không sang `in_transit`, sự kiện của hãng xử lý lại mãi vẫn lỗi. Ca `delivered` tới khi đơn còn
  `cancel_requested` (hãng giao luôn) cũng bị chặn như vậy. Test của M2 coi lỗi này là bình thường:
  `TestCancel_Invariants` (cancel_race_test.go:228-231) cho phép bước `pickup` trả `ErrStatusConflict`. Vì vậy chưa có
  ca nào kiểm luồng "đang yêu cầu hủy thì hãng lấy hàng".
- Bằng chứng:
  ```go
  switch o.Status {
  case StatusDelivered:
      return false, nil
  case StatusConfirmed:
  default:
      return false, fmt.Errorf("%w: áp %s lên đơn %s", ErrStatusConflict, step, o.Status)
  }
  ```
- Đề xuất sửa: thống nhất một trong hai cách. (a) `OnShipmentEvent(picked_up)` nhận đơn `cancel_requested`: chuyển
  held → in_transit, đơn giữ `cancel_requested` để `OnShipmentCancelRejected` đưa về `confirmed`. Ca `delivered` thì đi
  `cancel_requested → confirmed → delivered` hoặc trả lỗi rõ. (b) Giữ mã như hiện tại nhưng sửa kế hoạch M3 (VC12, dòng
  959) để gọi `EffCancelRejected` trước `EffPickedUp`. Kèm một test cho ca TD03 có hãng lấy hàng. Cỡ: nhỏ.

##### B-02: `DetachShipment` không kiểm trạng thái đơn — đơn `cancel_requested` mất vận đơn thì kẹt vĩnh viễn
- Mức: nhỏ (phòng thủ; đường gây ra thuộc M3)
- Tệp:dòng: internal/modules/order/shiphooks.go:73-91; queries/ord/shiphooks.sql:12-17
- Spec/kế hoạch: 05 mục 2, `cancel_requested` chỉ rời đi qua "hãng xác nhận hủy" hoặc "hãng từ chối". Kế hoạch M3 dòng
  880: `OnTerminal` `dead` do `rejected` → `order.DetachShipment`; vận đơn có `cancel_intent = order_cancel` →
  `OnShipmentCancelConfirmed`.
- Mô tả: `DetachShipment` gỡ `current_shipment_id` khi vận đơn còn là vận đơn hiện hành, với bất kỳ trạng thái đơn nào.
  Ví dụ: đơn `cancel_requested` mà vận đơn đang `creating`, lệnh tạo bị hãng từ chối. Nếu M3 gọi `DetachShipment` (hoặc
  gọi nó trước `OnShipmentCancelConfirmed`), đơn còn `cancel_requested` nhưng không còn vận đơn. Sau đó không đường nào
  đưa đơn ra khỏi trạng thái này: `Cancel` từ chối `cancel_requested` (cancel.go:67-68); hai hàm
  `OnShipmentCancelConfirmed`/`Rejected` cần đúng vận đơn hiện hành; `Edit` chỉ cho sửa người phụ trách. Hàng `held` không
  bao giờ được nhả. Gỡ vận đơn của đơn `delivered`/`returned` cũng không bị chặn.
- Bằng chứng: câu SQL chỉ có `WHERE … AND current_shipment_id = @shipment_id`, không điều kiện `status`; Go không kiểm
  `o.Status`.
- Đề xuất sửa: `DetachShipment` chỉ nhận đơn `confirmed`, còn lại trả `ErrInvalidTransition`. Hoặc với đơn
  `cancel_requested` thì gọi luôn `finishCancel`. Thêm test. Cỡ: nhỏ.

##### B-03: Xử lý đơn trùng "hủy" một đơn đã gửi hãng ghi kết luận `cancelled` dù đơn mới chỉ `cancel_requested`
- Mức: nhỏ
- Tệp:dòng: internal/modules/order/duplicates.go:364-387
- Spec/kế hoạch: DH09 ("không tự hủy"; nhân viên quyết định), TD03.
- Mô tả: `ResolveDuplicate(cancel_other|cancel_this)` gọi `Cancel`. Với đơn `confirmed` đã có vận đơn hiện hành,
  `Cancel` chỉ đưa đơn sang `cancel_requested`. Dù vậy cặp vẫn bị ghi `resolution = 'cancelled'` và cờ nghi trùng của
  hai đơn bị gỡ. Nếu hãng từ chối hủy (đã lấy hàng), đơn về `confirmed` và vẫn được giao. Lúc đó cả hai đơn trùng đều đi
  giao, nhưng cặp đã biến khỏi danh sách "Đơn trùng" và không còn cờ cảnh báo.
- Bằng chứng: `Cancel(...)` trả về không lỗi cho nhánh `requestCancel`, rồi gọi ngay `q.ResolveDuplicateLink(...,
  Resolution: resolutionCancelled)` và `clearSuspectIfClosed`.
- Đề xuất sửa: khi đơn sau `Cancel` chưa ở `cancelled` thì không đóng cặp. Có thể trả lỗi để nhân viên hủy qua hãng, hoặc
  ghi `not_duplicate` thay vì `cancelled`. Cỡ: nhỏ.

##### B-04: Hủy đơn trực tiếp không gỡ cờ `duplicate_suspect` của đơn kia trong cặp
- Mức: nhỏ
- Tệp:dòng: internal/modules/order/cancel.go:191-223 (`cancelAll`); queries/ord/duplicates.sql:45-49, 63-73
- Spec/kế hoạch: Task M2-23 (cờ nghi trùng phản ánh cặp còn mở).
- Mô tả: cờ chỉ được gỡ trong `ResolveDuplicate` (`clearSuspectIfClosed`). Đơn A và B nghi trùng; nhân viên hủy A bằng
  nút Hủy thường, hoặc đơn nháp A hết hạn. `HasOpenDuplicateLink` của B lúc đó đã là false, vì A đã `cancelled`. Nhưng
  `duplicate_suspect` của B vẫn true mãi: bộ lọc `duplicateSuspect=true` của danh sách đơn (list_dynsql.go:274-275) và
  huy hiệu trên đơn B còn báo nghi trùng, trong khi màn "Đơn trùng" không còn cặp nào để xử lý.
- Bằng chứng: grep `SetDuplicateSuspect` chỉ thấy trong duplicates.go (linkAll, clearSuspectIfClosed).
- Đề xuất sửa: trong `cancelAll`, sau khi hủy, gọi `clearSuspectIfClosed` cho các đơn còn cặp với đơn vừa hủy. Đơn kia
  phải được khóa theo id tăng dần trước nhóm 10/11, hoặc gỡ cờ ở consumer khi nghe `order.cancelled`. Cỡ: nhỏ.

##### B-05: Gọi `time.Now()` trong module order
- Mức: nhỏ
- Tệp:dòng: internal/modules/order/drafts.go:97; internal/modules/order/duplicates.go:192
- Spec/kế hoạch: Global Constraints M2, mục "Đồng hồ": "Không gọi `time.Now()` trong module".
- Mô tả: handler việc nền `expireDraftsHandler.Handle` và `DuplicatesConsumer` (`linkAll`, mốc `created_at` của cặp) lấy
  giờ máy ngay trong gói `order`. Test không cố định được giờ của cặp nghi trùng, và đồng hồ máy app có thể lệch giờ
  database.
- Bằng chứng: hai dòng trên.
- Đề xuất sửa: lấy `now()` của transaction (`store.TxNow`, như lớp bọc M1), hoặc nhận đồng hồ từ `background.Deps`.
  Cỡ: nhỏ.

##### B-06: Phiếu đổi tạo đơn mới (giữ hàng, có thể thẳng `confirmed`) chỉ với quyền `order.return`
- Mức: nghi ngờ
- Tệp:dòng: internal/httpapi/handlers_order_returns.go:65-80, 87-103; internal/modules/order/returns.go:257-283
- Spec/kế hoạch: Task 411 (D-02): tạo đơn phải có `order.create` ở kho giữ hàng (`orderWarehouseAt`); `order.confirm`
  là quyền riêng để xác nhận.
- Mô tả: `createReturn` loại `exchange` gọi `order.Writer.Create` trong cùng transaction. Đơn thay thế giữ hàng ở kho của
  đơn gốc, và tự đặt intent `confirmed` khi địa chỉ đủ. Handler không gọi `orderWarehouseAt`, cũng không kiểm
  `order.create` hay `order.confirm`. Người chỉ có `order.return` (hoặc `order.return` theo phạm vi khác kho) vẫn tạo được
  đơn đã xác nhận, giữ hàng ở kho mà họ không có quyền lên đơn. Nếu kế hoạch cố ý coi `order.return` là đủ thì đây không
  phải lỗi. Tôi không thấy câu nào trong kế hoạch nói rõ điều đó.
- Bằng chứng: `in.Replacement = replacementInput(...)` → `w.CreateReturn(...)`; không có `orderWarehouseAt` trong
  handlers_order_returns.go.
- Đề xuất sửa: với phiếu đổi, gọi `orderWarehouseAt(ctx, tx, u, origWarehouse)`, và kiểm `order.confirm` trước khi để
  đơn thay thế sang `confirmed`; hoặc ghi quyết định vào `docs/quyen.md`. Cỡ: nhỏ.

#### Đã kiểm, không thấy lỗi

- **Thứ tự khóa ADR-46**
  - Create: khách (5) → chèn đơn (dòng mới) → `Redeem` (10) → `Reserve` theo `variant_id` tăng dần (11).
  - Cancel: khóa đơn → `promotion.Release` → `ReleaseHeldByItems` (khóa số dư rồi giữ hàng theo thứ tự).
  - ExpireDrafts: `FOR UPDATE SKIP LOCKED ORDER BY id` → `LockCoupons` một lần → nhả tồn một lần.
  - Edit: đơn → dòng đơn (`LockOrderItems ORDER BY id`) → mã (`LockCoupons` cũ + mới) → tồn theo biến thể tăng dần.
  - Phiếu trả: hai đơn theo id tăng dần → phiếu → dòng phiếu → tồn.
  - `ResolveDuplicate`/`linkAll`: các đơn theo id tăng dần → dòng cặp.
  - `lockInState` của inventory: số dư trước giữ hàng; `ResizeHeld` cũng đi qua nó.
- **Máy trạng thái:** `state.Check` có đúng 12 cặp. `UpdateOrderStatus` là câu duy nhất đổi `status`. Bỏ hạn nháp khi
  rời draft khớp CHECK `orders_draft_expiry_check`. `confirmed_at` chỉ ghi lần đầu. `cancelled_at` được ghi.
- **cancel_requested:** không nhả hàng, không trả lượt mã. Hãng xác nhận hủy thì nhả held, trả lượt, gỡ vận đơn (WB12).
  Hãng từ chối thì về `confirmed` và bỏ lý do. Vận đơn cũ/lạ không đổi gì. Hàng đã `in_transit` thì
  `ErrInvalidTransition`.
- **Nhả đúng một lần:** `SetReservationState … WHERE state = from`, có kiểm số dòng. `ReleaseHeldByItems` không còn dòng
  held thì trả `ErrStateConflict`. Cancel chỉ nhả khi `states[held] > 0`.
- **Khóa sửa khi đã gửi hãng:** `state.Editable` chỉ mở người phụ trách và nguồn đơn khi có vận đơn hiện hành
  (`ErrLockedByShipment`). Đơn đã kết thúc chỉ sửa được người phụ trách.
- **Giá chụp lúc đặt (KH11):** dòng giữ nguyên dùng `item.UnitPrice`; chỉ dòng mới lấy giá hiện hành.
  `UpdateEditedItems` không đổi đơn giá hay ảnh chụp. UNIQUE `order_items_variant_key` chặn trùng biến thể.
- **Tiền sau sửa:** `cod_amount = GREATEST(grand − prepaid, 0)` khớp CHECK. `editedPaymentStatus` hợp lý.
- **Version:** mọi đường ghi nhân viên khóa rồi so version (`changeLocked`, `Edit`, `lockReturn`). Sự kiện có seq bằng
  version mới. Nhận phiếu khách trả tăng version đơn gốc (`BumpOrderVersion`).
- **Sự kiện, báo hiệu, audit:** đều trong cùng tx. Handler ghi audit qua `h.audit` hoặc `identity.RecordAudit`. Từ chối
  nghiệp vụ trả `final=false`, nên `WriteTx` lùi toàn bộ và nhả khóa chống trùng (idempotency.go:223-246); không còn khách
  hay đơn dở dang.
- **RetryTx chạy lại:** handler CreateOrder gán lại biến kết quả. `editRun.run` gán lại trường kết quả. Create, Cancel,
  ExpireDrafts không giữ trạng thái giữa hai lần chạy.
- **Khóa nghiệp vụ (DH07/AT06):** `orderBusinessKey` lấy từ người dùng và Idempotency-Key. Cùng nội dung thì replay (200),
  khác nội dung thì `ErrDuplicateOrder`. Unique `orders_idempotency_key_key` là lưới cuối.
- **Trả/đổi (TD05–TD08, WAT28/29, AT13/14):** số trả ≤ số mua − Σ các phiếu chưa hủy, dưới khóa dòng đơn. Tạo phiếu
  không đổi tồn, không đổi trạng thái đơn gốc. Kho nhận: sellable → on_hand +; damaged → on_hand + và damaged +; missing
  → không ghi kho, hoàn 0. Phiếu hoàn của hãng: `in_transit → released`, rồi đơn → `returned`. Phiếu hoàn trùng bị chặn
  bằng `ON CONFLICT` trên index một phần `returns_carrier_shipment_key`.
- **AT47:** `delivered` lần hai trả `applied=false`, không trừ tồn lần hai.
- **Sentinel → HTTP:** mọi sentinel của order mà HTTP có thể gặp đều nằm trong `orderError` (errors.go:422-456).
  `ErrStatusConflict` và `inventory.ErrStateConflict` không có trong bảng, nhưng chỉ xảy ra khi bất biến đã hỏng (500 là
  hợp lý).
- **Đọc theo người bán:** `list_dynsql.go` chỉ ghép hằng của `labelCond`, mọi giá trị đi qua `$n`, luôn có
  `seller_id = $1`. `GetDetail` và phiếu trả đọc qua `OrderExists`/JOIN đơn theo `seller_id`.
- **Nối việc nền:** lịch `order.expire_drafts` và consumer đơn trùng được nối ở `cmd/crm/modules.go:35`.
- **Ca nghiệm thu:** cả 27 ca của vùng (DH01, DH04, DH06–DH11, DH15, KH11, TD01–TD03, TD05–TD08, AT05–AT07, AT13, AT14,
  AT21, AT28, AT47, WAT28, WAT29) đều được nhắc tên trong ít nhất một tệp test của `order` hoặc `httpapi`.
- **Lệnh đã chạy:** `go test -count=1 ./internal/modules/order/` — KẾT_QUẢ_TEST

### Vùng C — Tiền và mã giảm giá

Phạm vi đã đọc: `internal/modules/promotion/{evaluate,redeem,manage,batch}.go`, `queries/promotion/*.sql`,
`internal/modules/order/internal/pricing/{pricing,refund}.go`, `order/{create_coupon,create_m2,discountpolicy,confirm,edit,cancel,drafts,settings,sources}.go`,
phần tiền của `returns.go`, `queries/ord/{create,edit,transition}.sql`, `migrations/0204,0206,0209`, `0117` (CHECK tiền M1),
`internal/httpapi/{handlers_promotion,handlers_order_confirm,refusal,idempotency}.go`, `handlers_order.go` (orderActor),
`console/src/screens/ma-giam-gia/couponRules.ts` (múi giờ, đơn vị phần trăm).
Test đã chạy: `go test -count=1 ./internal/modules/promotion/...` → `ok (83s)`; `go test -count=1 ./internal/modules/order/internal/pricing/...` → `ok`.

Tổng: 0 nghiêm trọng, 2 lớn, 2 nhỏ, 1 nghi ngờ.

---

##### C-01: Mã gắn khách bị từ chối sau khi gộp khách; giới hạn lượt mỗi khách tính lệch sau gộp
- Mức: lớn
- Tệp:dòng: internal/modules/promotion/evaluate.go:72; internal/modules/promotion/manage.go:194; queries/promotion/redeem.sql (IncrementCustomerUse)
- Spec/kế hoạch: 05 mục 2 "coupons" (bound_customer_id gắn mã với một khách, dùng hồ sơ gốc); Task 63/71 (`CustomerRootID` "hồ sơ gốc của khách (sau gộp)"); KM02 (chỉ từ chối khi thật sự không đủ điều kiện).
- Mô tả: Lúc tạo mã, `CreateCoupon` lưu `bound_customer_id` = hồ sơ gốc **tại thời điểm tạo**. `boundMatches` so bằng
  `c.BoundCustomerID == in.CustomerRootID` (gốc **hiện tại** của khách trên đơn). Gộp khách chỉ đổi `customer.customers.root_id`;
  không đường nào sửa `promotion.coupons.bound_customer_id` (crm_app không có quyền UPDATE cột này — 0209 — và
  `grep coupon` trong `internal/modules/customer` không có gì). Tình huống: tạo mã gắn khách A (A là gốc); sau đó gộp A vào B
  (gốc B). Lên đơn cho khách đó → `CustomerRootID = B` ≠ `A` → `bound_other`, mã hợp lệ bị từ chối; nhân viên không có cách
  nào sửa (bound_* không sửa được). Hoàn tác gộp cũng gây lệch ngược lại. Cùng gốc rễ: `coupon_customer_uses` khóa theo
  gốc lúc dùng, nên sau gộp lượt cũ nằm ở gốc cũ và `per_customer_limit` của gốc mới bắt đầu từ 0 (khách đã dùng hết lượt
  ở hồ sơ A dùng thêm được sau khi A gộp vào B).
- Bằng chứng: `evaluate.go:72  if c.BoundCustomerID != uuid.Nil && c.BoundCustomerID == in.CustomerRootID {`;
  `manage.go:189-194  root, err := CustomerRoot(ctx, tx, in.BoundCustomerID) … p.BoundCustomerID = &root`;
  `0209: GRANT UPDATE (name, discount_kind, …) — "Không cấp sửa … bound_*"`. Không test nào trong `promotion/*_test.go` hay
  `order/*coupon*_test.go` có gộp khách sau khi tạo mã gắn khách.
- Đề xuất sửa: trong `Redeem`/`Check`/`Reprice` đổi `c.BoundCustomerID` về gốc hiện tại (`customer.customers_v.root_id`, một câu
  đọc, hoặc JOIN trong `CouponByCodeForUpdate`) trước khi so; với giới hạn mỗi khách: đếm `coupon_customer_uses` theo mọi hồ
  sơ cùng gốc hiện tại (hoặc ghi rõ là chấp nhận và thêm test). Thêm test: tạo mã gắn A → gộp A vào B → lên đơn khách B đạt.
  Cỡ: nhỏ (phần so khách); phần giới hạn mỗi khách: lớn nếu chọn đếm theo nhóm.

##### C-02: Sửa đơn: không đòi duyệt lại khi tỷ lệ giảm tay không tăng, dù tiền giảm tay tăng bao nhiêu cũng được
- Mức: lớn
- Tệp:dòng: internal/modules/order/edit.go:483
- Spec/kế hoạch: Kế hoạch Task 77 (M2-20) Interfaces bước 6: "ngưỡng duyệt: đơn new/draft **vượt ngưỡng** → đặt discount_approval;
  đơn confirmed vượt ngưỡng mà người sửa không có quyền duyệt → ErrDiscountNeedsApproval" — không có điều kiện "lớn hơn tỷ lệ cũ".
  Q3 (người không có quyền không được tự giảm tay vượt ngưỡng). Điều kiện "và lớn hơn tỷ lệ cũ" chỉ có ở Quyết định 9 trong
  `docs/tasks/task-77.md`, không có trong kế hoạch.
- Mô tả: `decideApproval` của Edit bỏ qua mọi kiểm khi `bp <= old`. Với đơn đã được duyệt ở tỷ lệ cao, người **không** có quyền
  duyệt có thể thay toàn bộ dòng và nâng tiền giảm tay tùy ý, miễn tỷ lệ không vượt tỷ lệ đã duyệt. Ví dụ: Quản lý duyệt đơn
  1 món 200.000đ giảm 50% (100.000đ). Nhân viên `sale` sửa: bỏ món đó, thêm hàng 20.000.000đ, `orderDiscount` 10.000.000đ →
  `bp = 5000 = old` → `return nil`: đơn `confirmed` giữ nguyên, không 403, không chờ duyệt; `discount_approved_by` vẫn là
  người duyệt cũ (người duyệt "đã duyệt" khoản giảm 10 triệu mà họ chưa thấy).
- Bằng chứng: `edit.go:478-484`
  `bp := pricing.ManualDiscountBP(t); old := pricing.ManualDiscountBP(pricing.Totals{Subtotal: e.cur.Subtotal, …}); if bp == 0 || bp <= old { return nil }`.
- Đề xuất sửa: xét `overThreshold(t, s)` như kế hoạch (vượt ngưỡng → người có quyền: ghi duyệt; confirmed: 403; new/draft: chờ),
  hoặc nếu giữ ý "không đòi duyệt lại" thì kèm điều kiện tiền giảm tay tuyệt đối không tăng
  (`LineDiscountTotal+OrderDiscount` mới ≤ cũ) và hỏi chủ shop. Thêm test ca trên. Cỡ: nhỏ.

##### C-03: Sửa đơn (đổi dòng, giảm giá) bị chặn hoặc mất mã chỉ vì mã đã hết hạn/tắt sau lúc dùng; luật SĐT lúc sửa khác lúc tạo
- Mức: nhỏ
- Tệp:dòng: internal/modules/promotion/redeem.go:201 (Reprice → Evaluate đủ luật); internal/modules/order/edit.go:552; internal/modules/order/create_coupon.go:31
- Spec/kế hoạch: Kế hoạch Task 77 bước 5 ("không đổi mã mà base đổi → Reprice; điều kiện không đạt → coupon_rejected") — mã làm
  đúng chữ, nhưng chữ "điều kiện" bao cả luật thời điểm; kế hoạch Task 74: SĐT đưa vào luật mã lúc tạo là "danh tính SĐT của
  khách nếu có, không thì SĐT người nhận".
- Mô tả: `Reprice` gọi `Evaluate` đủ: `disabled`, `expired`, `not_started`, `bound_other`… Mã đã hợp lệ lúc lên đơn; sau giờ
  `valid_to` (hay khi chủ shop tắt mã), mọi lần sửa đổi `base` (đổi số lượng, thêm/bớt dòng, đổi giảm tay) trả 422
  `coupon_rejected expired`; muốn sửa phải bỏ mã → khách mất ưu đãi đã được hứa (mất 1 lượt cũng được trả nên không tái dùng
  được vì mã hết hạn). Thêm: lúc tạo, luật `bound_phone` dùng SĐT danh tính của khách trước (`create_coupon.go:31`), lúc sửa
  dùng SĐT người nhận (`edit.go:552`): đơn đặt hộ (người nhận khác) dùng mã gắn SĐT khách → tạo được, nhưng sửa số lượng bị
  `bound_other`.
- Bằng chứng: `redeem.go:200-204  discount, reason := Evaluate(c, in); if reason != "" { return 0, &RejectError{Reason: reason} }`;
  `edit.go:552  Phone: e.p.addr.Phone`; `create_coupon.go:31-34  phone := cust.Phone; if phone == "" && p.addr != nil { phone = p.addr.Phone }`.
  Không có test sửa đơn sau `valid_to` (`grep -i expired edit_test.go edit_property_test.go` rỗng).
- Đề xuất sửa: Reprice chỉ tính lại số tiền và kiểm `min_subtotal` (luật phụ thuộc base), bỏ luật thời điểm/trạng thái/kênh/gắn
  khách đã kiểm lúc dùng; hoặc hỏi chủ shop rồi ghi quyết định. Cỡ: nhỏ.

##### C-04: Lượt mã không trả khi đơn bị hãng hoàn (giao không thành, confirmed → returned)
- Mức: nghi ngờ
- Tệp:dòng: internal/modules/order/returns.go:633 (receiveCarrier → state.Returned, không gọi promotion.Release)
- Spec/kế hoạch: Q2 (KM03): "Đơn bị hủy **trước khi giao** … trả lại lượt; trả hàng **sau khi giao** không trả lượt. Lý do: khách
  chưa nhận hàng thì chưa hưởng ưu đãi".
- Mô tả: Đơn hoàn do giao không thành không phải "hủy" và cũng không phải "trả sau khi giao"; theo lý do của Q2 (khách chưa nhận
  hàng) lẽ ra phải trả lượt, nhưng đường `receiveCarrier` chỉ nhả hàng và chuyển `returned`. `used_count` vẫn tính đơn hoàn;
  khách bị "bom hàng" giữ lượt; mã 1 lượt (đợt mã gắn SĐT, max_uses = 1) không dùng lại được dù khách chưa nhận hàng.
  Q2 không nói rõ ca này → cần chủ shop quyết.
- Bằng chứng: `grep -n "promotion\." internal/modules/order/returns.go` → không có lời gọi nào; chỉ `cancel.go:204` gọi `promotion.Release`.
- Đề xuất sửa: hỏi chủ shop; nếu trả lượt thì gọi `promotion.Release` (nhóm 10, trước inventory nhóm 11) trong nhánh carrier return,
  kèm test. Cỡ: nhỏ.

##### C-05: Dùng mã khi phần giảm bằng 0 vẫn trừ một lượt
- Mức: nhỏ
- Tệp:dòng: internal/modules/promotion/redeem.go:81-95; internal/modules/order/create_coupon.go:44-46
- Spec/kế hoạch: KM01 (hết lượt thì từ chối — lượt phải phản ánh lần thật sự được giảm).
- Mô tả: `Evaluate` trả `(0, "")` khi `Base = 0` (vd. giảm tay đã bằng 100% tiền hàng, mã `min_subtotal = 0`) hoặc mã phần trăm
  nhỏ trên base nhỏ (floor về 0). `Redeem` vẫn tăng `used_count`, lượt của khách, ghi dòng redemption `discount 0`.
  `applyCoupon` thấy `Discount == 0` thì bỏ qua — đơn không được giảm đồng nào nhưng mã giới hạn lượt (mã 1 lượt của đợt) bị tiêu.
- Bằng chứng: `evaluate.go:33  base := max(in.Base, 0)` … không có nhánh từ chối khi phần giảm = 0; `redeem.go:91  n, err := q.IncrementUsed(ctx, c.ID)` chạy vô điều kiện sau Evaluate.
- Đề xuất sửa: phần giảm 0 → không Redeem (trả RejectError lý do riêng hoặc bỏ qua mã, báo cho người lên đơn). Cỡ: nhỏ.

---

#### Đã kiểm, không thấy lỗi

- **pricing.Compute / Allocate (DH05, Q4):** kiểm số âm, trần 2^53−1, tích 128 bit; giảm dòng ≤ Gross; giảm cấp đơn + mã ≤ Σ Base;
  chia floor rồi dồn phần dư vào dòng base lớn nhất, không vượt base dòng → Σ phần chia = số giảm, 0 ≤ phần chia ≤ base.
  `GrandTotal = Subtotal − DiscountTotal + Ship`. Có test rapid và vector chung với màn hình. DB chốt lại bằng CHECK
  `net_amount = qty×price − line_discount − allocated` (0117), `discount_total = ld + od + cd`, `cod_amount = max(grand − prepaid, 0)`,
  `grand_total = subtotal − discount_total + ship` và trigger hoãn `check_order_totals` (Σ allocated = order_discount + coupon_discount).
- **ManualDiscountBP / ngưỡng (Q3, DH16):** làm tròn lên, so `>` ngưỡng; không tính mã; Subtotal 0 có giảm → MaxInt64 (luôn chờ duyệt).
  Create: đơn vượt ngưỡng của người không có quyền → `new`/`draft` + `discount_approval`; người có quyền → ghi `discount_approved_by`.
  State machine chặn `new/draft → confirmed` khi đang `discount_approval`; `SetPending` không bỏ được lý do này.
- **Người duyệt lấy từ phiên:** `orderActor(u)` tính `CanApproveDiscount` bằng `identity.Can(u, order.discount.approve)`;
  `VersionRequest` chỉ có `version`; route `approveOrderDiscount` có `x-permission order.discount.approve`; `ApproveDiscount` kiểm
  lại quyền trong module; `RecordDiscountApproval` ghi `a.id()`.
- **Máy chủ không nhận đơn giá từ client:** `OrderLineInput`/`EditOrderLine` chỉ có variantId, qty, lineDiscount; giá lấy từ
  `catalog.VariantsForSale` (dòng mới) hoặc `item.UnitPrice` (dòng giữ nguyên, KH11).
- **Hai đơn tranh lượt cuối (WB11/WAT07):** `CouponByCodeForUpdate` khóa dòng mã, `IncrementUsed … used_count < max_uses` có điều kiện,
  CHECK `used_count <= max_uses`; test `TestRedeem_ConcurrentMaxUses`, `TestCreateCoupon_ConcurrentLastUse`.
- **Hủy trả lượt đúng một lần (KM03):** `Cancel` khóa đơn + so version (song song → một 200, còn lại 409; `TestCancel_Parallel50SameVersion`);
  `ReleaseRedemption … status='active'`, `DecrementUsed … used_count > 0`, `DecrementCustomerUse … used > 0`; `release_on_cancel=false`
  giữ lượt; cancel_requested không trả lượt, trả khi hãng xác nhận hủy; nháp hết hạn và hủy đơn trùng đi qua `cancelAll` → Release.
  Hàm trong RetryTx không cộng dồn biến ngoài (`refusableWrite`, `editRun.run` gán lại).
- **Thứ tự khóa ADR-46:** đơn (7) → mã (10, `LockCoupons` theo id khi chạm nhiều mã: Edit đổi mã, ExpireDrafts) → tồn (11);
  `lockActive` khóa mã rồi đọc lại lượt.
- **Từ chối mã → lùi cả lần tạo/sửa:** `refusableWrite` trả `final=false` → `WriteTx` lùi transaction (không còn khách, đơn, giữ hàng).
- **Mã hết hạn theo múi giờ:** máy chủ so mốc tuyệt đối (`Now ≥ ValidTo` → expired, khoảng [from, to)); màn hình đổi ô giờ Việt Nam
  sang ISO UTC bằng hằng số +07 (`vnLocalToIso`), không theo múi giờ máy.
- **Quản lý mã (Task 71):** sau khi dùng chỉ đổi name, valid_to, max_uses (≥ used_count), status; khóa dòng mã trước khi kiểm
  `HasRedemptions`; `validateTerms` khớp CHECK của bảng; đợt mã: max_uses = 1, SĐT chuẩn hóa, không trùng, sinh lại mã trùng.
- **Tiền hoàn từng phần (WAT31):** `RefundFor` floor, đơn vị cuối nhận phần còn lại, không vượt net; dòng trùng trong một phiếu bị chặn.
- **Cài đặt đơn, nguồn đơn (Task 72):** khóa dòng, so version, kiểm khoảng như CHECK; quyền `order.settings.manage`.
- **Sửa đơn có tiền trả trước:** `cod_amount = GREATEST(grand − prepaid, 0)`, payment_status tính lại.

### Vùng D — Màn hình M2

Phạm vi đã soát: `console/src/screens/{len-don,don,khach,ma-giam-gia,cai-dat-don}` (Task 84–87), đối chiếu
`openapi/openapi.yaml` (x-permission, schema), `internal/httpapi/errors.go` (mã lỗi, trường chi tiết), các hàm máy chủ
`internal/modules/order/{confirm,cancel,returns,duplicates,discountpolicy,settings}.go`,
`internal/modules/order/internal/{state/editable.go,pricing}`, `internal/modules/customer/identity.go`.

Lệnh đã chạy:
`cd console && npx vitest run src/screens/don src/screens/len-don src/screens/khach src/screens/ma-giam-gia src/screens/cai-dat-don`
→ `Test Files 25 passed (25) · Tests 449 passed (449)`.

Không thấy lỗi nghiêm trọng hay lớn. Ba phát hiện nhỏ: màn hiện câu hoặc số không đúng với việc máy chủ làm.

---

##### D-01: Đơn trùng — hộp "Hủy đơn này" nói "hàng trả về có thể bán ngay, mã được trả lượt" cả khi đơn đã gửi hãng (máy chủ chỉ chuyển "Đang yêu cầu hủy")
- Mức: nhỏ
- Tệp:dòng: console/src/screens/don/DuplicatePairs.tsx:42-43, 112-116, 221-223, 266-270
- Spec/kế hoạch: CLAUDE.md "Bẫy đã gặp": màn chỉ vẽ hành vi mà máy chủ thật sự làm; HV18 (hủy đơn đã gửi hãng → cancel_requested), TD03.
- Mô tả: `CANCELLABLE` gồm cả `confirmed`, và nút "Hủy đơn này" hiện với mọi đơn confirmed. Máy chủ
  (`ResolveDuplicate` → `Cancel`) xử lý đơn confirmed có vận đơn hiện hành bằng `requestCancel`: đơn sang
  `cancel_requested`, **không** nhả hàng, **không** trả lượt mã. Hộp xác nhận vẫn ghi "Hàng của đơn trả về có thể bán
  ngay; mã giảm giá (nếu có) được trả lượt", và thông báo sau đó là "Đã hủy XXX". Nhân viên được báo sai rằng hàng đã về
  kho, trong khi hàng vẫn ở chỗ hãng cho tới khi hãng xác nhận. Ghi chú ở dòng 41 có nhắc ca này, nhưng câu hiện ra
  không xét tới. `CancelDialog.tsx` thì làm đúng (xét `currentShipmentId` để đổi câu và tên nút); màn đơn trùng thì không.
- Bằng chứng: DuplicatePairs.tsx:41-43
  `// Trạng thái hủy được (cancel.go): đơn đã gửi hãng thì máy chủ chuyển "Đang yêu cầu hủy". const CANCELLABLE = new Set(["draft","new","confirmed"])`;
  :268 `Đơn bị hủy với lý do "Đơn trùng". Hàng của đơn trả về có thể bán ngay; mã giảm giá (nếu có) được trả lượt.`;
  :222 `` `Đã hủy ${...code}.` ``. Máy chủ: cancel.go:69-70
  `case o.Status == StatusConfirmed && o.CurrentShipmentID != nil: return w.requestCancel(...)`
  (cancel.go:48-50: "không nhả hàng, không trả lượt").
- Đề xuất sửa: trong `Pair`, lấy `detail.currentShipmentId` (đã có từ `useOrder`) để đổi câu trong hộp và câu thông báo
  ("Gửi yêu cầu hủy… hàng chưa về kho tới khi hãng xác nhận"), giống CancelDialog; thêm một ca vitest. Cỡ: nhỏ.

##### D-02: Sửa đơn có giữ mã giảm theo % — tổng tạm tính vẫn dùng số giảm cũ khi dòng hay giảm tay đã đổi
- Mức: nhỏ
- Tệp:dòng: console/src/screens/len-don/OrderForm.tsx:245-258
- Spec/kế hoạch: "tiền hiển thị đúng, không tính ở client khác máy chủ"; HV23 (sửa đơn); máy chủ tính lại mã khi lưu.
- Mô tả: Khi sửa đơn mà vẫn giữ mã cũ (`keepCoupon`), form không gọi `checkCoupon`. Nó lấy cố định
  `couponDiscount = eo.couponDiscount` rồi tính `after`/`totals` bằng số đó. Với mã %, hay mã có `minSubtotal`, mà người
  dùng đổi số lượng, thêm hoặc bỏ dòng, hay đổi giảm tay, thì "Mã giảm giá" và "Tổng" trong TotalsPanel lệch với số máy
  chủ sẽ tính khi lưu. Ví dụ: mã 10%, tạm tính tăng từ 1.000.000 lên 2.000.000, màn vẫn trừ 100.000 thay vì 200.000. Nếu
  đơn không còn đủ `minSubtotal`, màn vẫn trừ tiền như cũ, và lỗi `coupon_rejected` chỉ hiện sau khi bấm lưu. Nhân viên
  có thể đọc tổng sai cho khách trước khi lưu. Ghi chú trong mã thừa nhận "máy chủ tính lại khi lưu", nhưng màn không
  báo rằng số đang hiện là số cũ.
- Bằng chứng: OrderForm.tsx:245-248
  `// Sửa đơn giữ mã cũ: không kiểm lại (...), hiện số giảm hiện tại.`
  `const keepCoupon = eo !== undefined && couponCode === (eo.couponCode ?? "");`
  `const couponDiscount = keepCoupon ? (eo.couponDiscount ?? 0) : ...;`
  Sau đó `compute({... couponDiscount ...})` (dòng 252-257) cho ra tổng hiện ở TotalsPanel.
- Đề xuất sửa: khi `keepCoupon` và dòng hay giảm tay đã đổi so với `eo`, có hai cách. (a) Hiện "Mã giảm giá: tính lại
  khi lưu" thay cho số, và bỏ chữ "Tổng" chắc chắn. (b) Thêm một tham số cho `checkCoupon` để bỏ qua lượt của chính đơn
  này; cách này cần đổi API, nên ghi "Việc phát hiện thêm". Cỡ: nhỏ (cách a).

##### D-03: Danh sách đơn — mốc "Hôm nay / Hôm qua / 7 ngày" cố định lúc mở màn, không đổi khi qua nửa đêm dù màn tự tải lại 20 giây
- Mức: nhỏ
- Tệp:dòng: console/src/screens/don/OrderList.tsx:139, 141-149, 154-156
- Spec/kế hoạch: HV14 (danh sách tự cập nhật, lọc theo khoảng ngày).
- Mô tả: `const [now] = useState(() => Date.now())` chỉ được tính một lần. `rangeQuery(range, now)` dựng `createdFrom`
  từ mốc đó, và `refetchInterval: 20_000` cứ thế dùng lại cùng khóa. Màn mở từ chiều hôm trước (hay gặp ở máy bàn của
  shop) thì sau 0 giờ, "Hôm nay" vẫn gồm cả đơn của hôm qua, còn "Hôm qua" chỉ ra đơn của hôm kia. Số đơn hiện ra không
  đúng với bộ lọc người dùng nghĩ mình đang xem.
- Bằng chứng: OrderList.tsx:139 `const [now] = useState(() => Date.now());`; :141-149
  `...rangeQuery(range, now)`; :154 `refetchInterval: REFRESH_MS`.
- Đề xuất sửa: tính `now` lại theo ngày (ví dụ giữ `vnMidnight(Date.now(), 0)` trong state và cập nhật nó trong
  `refetchInterval` hoặc bằng một timer tới nửa đêm), để khóa truy vấn đổi khi sang ngày mới. Cỡ: nhỏ.

---

#### Đã kiểm, không thấy lỗi

- **Khóa chống trùng (DH07, G13):** `app/idempotency.ts` sinh khóa khi bấm. Khóa được giữ qua mọi lần tự gửi lại (503
  retry_later tối đa 3 lần, 409 request_in_progress, lỗi mạng) và qua "Gửi lại" khi thân như cũ (so bằng fingerprint).
  Thân đổi thì dùng khóa mới. OrderForm (tạo và sửa), OrderDetail (approve/confirm/pending/update), CancelDialog,
  ReturnDialog, ReturnsCard, DuplicatePairs, các thao tác ghi khách, mã và cài đặt đều đi qua `useWriteAction`, và mã
  đối tượng hay version đều nằm trong thân (không lấy từ props). Có test `OrderForm.test.tsx:246` (503 hai lần rồi 201:
  ba yêu cầu cùng khóa) và `:291`.
- **Xung đột version (DH11):** OrderDetail (`TEXT_CONFLICT` "Đơn đã thay đổi, tải lại để xem bản mới." kèm nút "Tải lại
  đơn"), OrderForm ở chế độ sửa (giữ phần đang gõ, có nút "Tải lại đơn", EditOrderDialog dựng lại form từ bản mới), hồ
  sơ khách, chi tiết mã (RefusalBox), Cài đặt đơn. Có test `OrderDetail.test.tsx:162, 239`. CancelDialog và ReturnsCard
  chỉ hiện câu chung của `version_conflict`, nhưng đơn tự tải lại theo báo hiệu `order.changed` nên lần bấm sau gửi
  version mới. Không coi là lỗi.
- **Nút theo quyền khớp `x-permission`:** approve → `order.discount.approve`; confirm và pending → `order.confirm`;
  cancel → `order.cancel`; sửa và ghi chú đơn → `order.update`; tạo phiếu, đóng phiếu, hủy phiếu → `order.return`; nhận
  hàng trả → `order.return.receive`; đơn trùng → `order.duplicates.resolve`; khách: write, merge, import (thêm
  `files.upload`), tags.manage; mã → `promotion.manage`; route cài đặt → `order.settings.manage`; `listStaff` chỉ gọi khi
  có `identity.users.manage`.
- **Nút theo trạng thái khớp máy chủ:** Confirm cho draft/new; approve cho draft/new đang chờ duyệt (draft giữ draft);
  SetPending chỉ cho new; Cancel cho draft/new/confirmed, không cho cancel_requested; Return chỉ khi delivered; phiếu
  requested có nhận và hủy, phiếu restocked có đóng; EditScope có hai mức (tất cả hoặc chỉ careBy/trafficSource), nên
  form khóa theo nhóm là đủ.
- **Tiền:** `returnRules.refundFor` và `returnableLines` khớp từng bước với `pricing.RefundFor` và `priceReturnLines`
  (bỏ phiếu đã hủy; dòng missing tính phần lẽ ra; nhân chia bằng BigInt). `manualDiscountBP` khớp
  `pricing.ManualDiscountBP` (làm tròn lên, so `>` ngưỡng). Chi tiết đơn hiện số của máy chủ (subtotal, grandTotal,
  codAmount); thành tiền mỗi dòng = netAmount + allocatedOrderDiscount. Các ô tiền đọc ra số nguyên đồng.
- **Mock khớp máy chủ:** out_of_stock 409 kèm `variantId` và `available`; coupon_rejected 422 kèm `reason`;
  identity_taken 409 kèm `ownerId`; merge_not_latest kèm `blockingMergeId`; setting_out_of_range 422 kèm `field`;
  invalid_tag_rule 422; code_taken 409; forbidden 403. Tên trường lấy theo `errorBody` trong errors.go.
- **Lỗi API có trạng thái hiện riêng:** lỗi đọc đơn, lịch sử, phiếu trả (không treo ở "đang tải"), lỗi gợi ý gộp,
  lượt dùng mã, cài đặt, đơn trùng.
- **Dữ liệu cá nhân:** không có `console.log` nào. SĐT ở gợi ý tìm khách hiện đã che (`maskPhone`). Lỗi chỉ hiện câu
  dựng từ mã, không chèn chữ của máy chủ.
- **Cài đặt đơn:** khoảng giá trị trong câu hiện ra khớp `settings.go` (10–10080, 0–10080, 0–10000, 0–365).
- **Mã giảm giá:** mã đã có lượt dùng chỉ gửi name, validTo, maxUses (và status riêng), khớp
  `coupon_locked_after_use`; không cho xóa trắng một mức đã đặt, vì API không có cách bỏ.
- **Gỡ danh tính khách:** chỉ phone, email, zalo_uid, khớp `RemoveIdentity` (fb_psid, web_account → invalid_input).

### Vùng E — Chéo toàn M2 và hợp đồng

Người soát: reviewer độc lập (vùng E). Ngày 19/11/2026. Nhánh `task-89` (= main), W = `apps/crm/.worktrees/task-89`.
Chỉ đọc mã, chỉ chạy test (mỗi lần một gói). Không sửa tệp nào trong W.

Tóm tắt: **0 nghiêm trọng, 2 lớn, 6 nhỏ, 1 nghi ngờ.** Mọi test của bảng ca kiểm bên dưới chạy lại đều PASS.

---

#### 1. Bảng ca kiểm M2 → test → kết quả chạy (dùng điền nghiệm thu)

Lệnh đã chạy (19/11, DB test `CRM_TEST_DATABASE_URL`, `-count=1`):

- L1 `go test -count=1 ./internal/modules/order/ -run '<39 test bên dưới>' -v` → `ok … order 77.9s`, 39/39 PASS
- L2 `go test -count=1 ./internal/modules/order/internal/pricing/ -v` → `ok 0.34s`, 19/19 PASS
- L3 `go test -count=1 ./internal/modules/customer/ -run 'TestSearch|TestFindOrCreateForOrder|TestMergeDH14|TestMergeUndoProperty|TestStatsFromSource' -v` → `ok 41.5s`, 14/14 PASS
- L4 `go test -count=1 ./internal/modules/promotion/` → `ok 78.0s` (cả gói)
- L5 `go test -count=1 ./internal/httpapi/ -run '<18 test HTTP bên dưới>' -v` → `ok 8.9s`, 18/18 PASS
- L6 `cd console && npx vitest run src/screens/len-don src/screens/khach/MergeView.test.tsx` → 5 tệp, 286 test PASS
- L7 `go test -count=1 ./internal/modules/order/ -run 'TestCancel_Parallel50SameVersion|TestCancel_Invariants|TestTimeline_ManyAuditsKeepsLatestEdit'` → `ok 40.8s` PASS
- L8 `go test -count=1 ./internal/modules/inventory/ -run 'Return|ByItems|Damaged|InTransit'` → `ok 4.2s`
- L9 `go test -count=1 ./tests/arch/ -run 'TestViews|M2Contract'` → 4/4 PASS
- L10 `go test -count=1 ./internal/modules/order/ -run 'TestEdit_NoDeadlock|TestEdit_CouponSwapNoDeadlock|TestOnShipmentEvent_StaleShipment|TestCancel_TD03RequestThenCarrierConfirms'` → `ok 76.5s`, 4/4 PASS

##### 19 ca M2 (`11-kich-ban-kiem-thu.md`)

| Ca | Tình huống (11) | Test máy (tệp: hàm) — điều được assert | Lệnh | Kết quả | Ghi chú |
|---|---|---|---|---|---|
| DH01 | Lên đơn nhanh: tổng đúng, Đã xác nhận, hàng tạm giữ | `order/create_m2_test.go: TestCreate_ConfirmedTotals` (subtotal 680.000, giảm dòng 20.000, ship 30.000 → grand/COD 690.000, `StatusConfirmed`, V1 held 2/avail 3, V2 held 1, lịch sử, sự kiện created seq1/confirmed seq2); `pricing_test.go: TestCompute_DH01`; `create_coupon_test.go: TestCreateCoupon_DH01`; HTTP `handlers_order_m2_test.go: TestOrderAPI_CreateM2Confirmed` (201, label confirmed, held, audit không PII); vitest `len-don/pricing.test.ts` "DH01" | L1 L2 L5 L6 | ĐẠT | phần V: Điểm dừng C |
| DH02 | Tìm khách bằng đuôi SĐT/tên không dấu < 0,3 s | `customer/search_test.go: TestSearchClassifies` (đuôi số, SĐT đủ, tên không dấu/thiếu dấu/hoa thường), `TestSearchGroupsByRoot`, `TestSearchTenantIsolation`; `search_explain_test.go: TestSearchPlans` (100.000 khách, crm_app, không Seq Scan); HTTP `TestCustomerSearchAPI` (log/span không chứa chuỗi tìm) | L3 L5 | ĐẠT phần đúng kết quả + kế hoạch truy vấn | p95 trên 1 triệu khách: Task 88 đã dời (không tính) |
| DH04 | Đơn nhập tay, nguồn "Nhập tay" | `create_m2_test.go: TestCreate_SourceAndChannel` (`SourceByKey(manual)`, channel manual, nguồn tắt/không có → `ErrTrafficSourceInactive`) | L1 | ĐẠT | phần V |
| DH05 | Giảm cả đơn chia về từng món, không lệch 1 đồng | `pricing_test.go: TestCompute_DH05` (100.001 → [50001,18000,32000]); `property_test.go: TestCompute_Invariants`, `TestAllocate_Invariants` (rapid); `vectors_test.go: TestVectors` (200 vector) + vitest `pricing.test.ts` cùng tệp vector; trigger `schema_test.go: TestSchemaAllocatedDiscountAtCommit` (lệch 1 đồng → 23514); `TestCreateCoupon_DH01` (Σ allocated = 50.000 qua DB) | L1 L2 L6 | ĐẠT | |
| DH06 | Lịch sử thao tác trên đơn | `order/timeline_test.go: TestTimeline_DH06` (tạo/sửa/xác nhận/hủy/ghi chú, đúng người, mới nhất trước, không lộ SĐT/địa chỉ); HTTP `handlers_order_read_test.go: TestOrderRead_EditCancelTimeline`; `TestTimeline_ManyAuditsKeepsLatestEdit` | L1 L5 L7 | ĐẠT | phần V |
| DH07 | Bấm "Tạo đơn" hai lần | HTTP `handlers_order_test.go: TestOrderAPI_SameKey50` (50 song song cùng khóa → 1 đơn, 1 khách); `handlers_order_m2_test.go: TestOrderAPI_BusinessKeyAfterExpiry` (khóa chống trùng đã xóa → 200 đơn cũ; khác dòng → 422 `idempotency_key_reused`); `create_m2_test.go: TestCreate_BusinessKeyReplay`; vitest `OrderForm.test.tsx` "503 hai lần rồi 201: ba yêu cầu cùng Idempotency-Key" | L1 L5 L6 | ĐẠT | |
| DH08 | Hai nhân viên tranh chiếc cuối | `create_race_test.go: TestCreate_LastUnitRace` (20 lần, đúng 1 OK + 1 `OutOfStockError` avail 0, held 1); `create_property_test.go: TestCreate_ConcurrentReservations` (rapid 2–6 người) | L1 | ĐẠT | |
| DH09 | Cùng SĐT, cùng SP trong vài giờ → "Đơn trùng", không tự hủy | `duplicates_test.go: TestDuplicates_DH09` (cờ nghi trùng hai đơn, liên kết phone, có trong ListDuplicates, 0 đơn cancelled, cửa sổ 24 h, V khác không liên kết); `TestDuplicates_ConcurrentCreate`; HTTP `TestOrderAPI_Duplicates` | L1 L5 | ĐẠT | phần V |
| DH10 | Sửa tăng SL khi kho không đủ → từ chối trọn | `edit_test.go: TestEdit_DH10_Quantity` (6 → `OutOfStockError` avail 3; đơn, version, dòng, giữ hàng không đổi; hai người bán); HTTP `handlers_order_edit_test.go: TestOrderEdit_Quantity` (409 `out_of_stock`) | L1 L5 | ĐẠT | |
| DH11 | Hai người cùng sửa một đơn | `edit_test.go: TestEdit_DH11_VersionConflict` (song song, đúng 1 lưu, kia `ErrVersionConflict`, dữ liệu của người thắng); HTTP `TestOrderEdit_ConflictAndApproval` (409 `version_conflict`) | L1 L5 | ĐẠT | |
| DH12 | Đặt hộ: SĐT người nhận không thành SĐT khách | `customer/fororder_test.go: TestFindOrCreateForOrder/DH12`; `order/create_m2_test.go: TestCreate_PhoneOwnership` (ship_phone = SĐT mẹ, A 0 danh tính); HTTP `TestOrderAPI_OrderForSomeoneElse` | L1 L3 L5 | ĐẠT | |
| DH13 | SĐT trùng hồ sơ khác → gợi ý; chỉ người có quyền gộp | `fororder_test.go: TestFindOrCreateForOrder/DH13` (`PhoneConflictWith` = gốc B, không gắn, không gộp); `TestCreate_PhoneOwnership` (cảnh báo `phone_belongs_to_other_customer`); HTTP `handlers_customer_merge_test.go: TestCustomerMergeAPI` (gợi ý theo SĐT địa chỉ; sale gộp/hoàn tác → 403); vitest `MergeView.test.tsx` | L1 L3 L5 L6 | ĐẠT | phần V |
| DH14 | Gộp nhầm → hoàn tác 30 ngày, đơn về đúng chỗ | `customer/merge_test.go: TestMergeDH14` (+ `checkUndoDH14`: đơn giữ customer_id, hoàn tác trả root_id, lần hai version_conflict); `merge_property_test.go: TestMergeUndoProperty` (rapid); `stats_test.go: TestStatsFromSource` (chỉ số sau gộp/hoàn tác); HTTP `TestCustomerMergeAPI` (quá 30 ngày → 422, `merge_not_latest`) | L3 L5 | ĐẠT phần CRM | "hội thoại về đúng chỗ" là M4 |
| DH16 | Giảm quá mức → chờ người có quyền duyệt | `discountpolicy_test.go: TestCreate_DiscountApproval` (100.000 → confirmed; 100.001 → new + `discount_approval`, vẫn giữ hàng; manager → confirmed, ghi người duyệt), `TestCreate_DiscountApprovalThresholdSetting`, `TestNeedsApproval`; `confirm_test.go: TestApproveDiscount_New`, `TestConfirm_ApproverConfirmsPending`; `edit_test.go: TestEdit_DiscountApproval`; HTTP `TestOrderConfirm_DiscountApproval` (422 `needs_approval`, 403 `discount_needs_approval`) | L1 L5 | ĐẠT | |
| KH11 | Đổi giá: đơn cũ giữ giá, đơn mới giá mới | `create_m2_test.go: TestCreate_PriceSnapshot`; `edit_test.go: TestEdit_KH11_PriceSnapshot` (dòng cũ giữ 250.000, dòng mới 270.000) | L1 | ĐẠT | |
| TD01 | Hủy đơn chưa gửi hãng → hàng về "có thể bán" ngay | `cancel_test.go: TestCancel_TD01ReleasesStock` (avail V2 0 → 1, held 0, lý do, lịch sử, sự kiện, đơn mới V2 tạo được); HTTP `TestOrderAPI_Cancel` | L1 L5 | ĐẠT | phần V |
| KM01 | Mã có hạn, giới hạn lượt; hết lượt → từ chối | `create_coupon_test.go: TestCreateCoupon_DH01`, `TestCreateCoupon_Exhausted` (đơn thứ 3 `exhausted`, used_count 2); `promotion/redeem_test.go: TestRedeem_ConcurrentMaxUses`; `promotion/manage_test.go` (sale10 hạn 01/12–31/12); HTTP `handlers_promotion_test.go: TestCouponAPI_CreateAndCheck` | L1 L4 L5 | ĐẠT | phần V |
| KM02 | Mã hết hạn/không đủ điều kiện → từ chối, nêu lý do | `TestCreateCoupon_Rejected` (expired, not_found, không đơn, không khách mới); `promotion/evaluate_test.go: TestEvaluate`; HTTP `TestCouponAPI_CreateAndCheck` (expired 01/01/2027, min_subtotal), `TestOrderAPI_CouponAndDiscountApproval` (422 `coupon_rejected` + `reason`); `edit_test.go: TestEdit_Coupon` (min_subtotal) | L1 L4 L5 | ĐẠT | |
| KM03 | Hủy đơn đã dùng mã → lượt có được trả (Q2) | `cancel_test.go: TestCancel_KM03ReleasesCoupon` (used_count 1→0, released, `order_cancelled`; cờ tắt → giữ); `cancel_race_test.go: TestCancel_Parallel50SameVersion` (50 hủy song song, used_count giảm đúng 1); `promotion/redeem_test.go: TestRelease` | L1 L4 L7 | ĐẠT | |

**Ca M2 không có test máy: 0.** Ca có phần chỉ người kiểm được (V): DH01, DH04, DH06, DH09, DH13, TD01, KM01 — chờ Điểm dừng C.

##### Ca nghiệm thu spec 10/2026 (AT/WAT) thuộc M2

| Ca | Tình huống (03-controls-acceptance) | Test máy | Lệnh | Kết quả | Ghi chú |
|---|---|---|---|---|---|
| AT05 | Hai người lưu cùng SKU còn 1; không cộng giữ lại khi xác nhận | `TestCreate_LastUnitRace`, `TestCreate_ConcurrentReservations` | L1 | ĐẠT một nửa | Nửa sau "không giữ thêm khi xác nhận" không có assert (xem E-05) |
| AT06 | Gửi lại confirm/…/receive cùng request: một tác động; khác payload → conflict | `TestOrderAPI_SameKey50`, `TestOrderAPI_BusinessKeyAfterExpiry`, `handlers_order_confirm_test.go: TestOrderConfirm_Parallel`, `handlers_order_cancel_test.go: TestOrderAPI_CancelSameKey50` | L5 | ĐẠT phần CRM | dispatch/payment là M3 |
| AT07 | Tăng lượng/đổi kho mà kho thiếu → rollback nguyên tử | `TestEdit_DH10_Quantity`, `TestOrderEdit_Quantity` | L1 L5 | ĐẠT phần "tăng lượng" | "đổi kho" không có trong M2 (xem E-07) |
| AT13 (phía đơn) | Trả một trong hai SKU nhiều lần: không vượt SL/giá trị gốc | `returns_test.go: TestCreateReturn_TD05PerLine`, `returns_property_test.go: TestReturns_Invariants` (rapid) | L1 | ĐẠT | doanh thu/hoa hồng: M3/M6 |
| AT14 (phía đơn) | Đổi size: có trả + đơn mới, không sửa đơn gốc | `TestCreateReturn_TD06Exchange`; HTTP `TestReturnAPI_Exchange` | L1 L5 | ĐẠT | cấn trừ tiền: M3 |
| AT21 | Đơn cũ giữ snapshot, đơn mới theo giá hiệu lực | `TestCreate_PriceSnapshot`, `TestEdit_KH11_PriceSnapshot` | L1 | ĐẠT | |
| AT28 | Hai phiên sửa cùng version → VERSION_CONFLICT | `TestEdit_DH11_VersionConflict`, `TestOrderEdit_ConflictAndApproval`, `TestConfirm_New` (version cũ) | L1 L5 | ĐẠT | |
| AT29 | Giảm quá ngưỡng: chưa xác nhận cho tới khi duyệt | như DH16 | L1 L5 | ĐẠT | |
| AT47 | Callback giao thành công lặp 3 lần → một phiếu xuất | `shiphooks_test.go: TestOnShipmentEvent_PickedUpThenDelivered` (delivered lần hai `applied=false`, on_hand không giảm thêm) | L1 | ĐẠT phần đơn/kho (2 lần) | "một kết quả tài chính": M3 |
| WAT07 (CRM) | Coupon còn 1 lượt, hai đơn đồng thời; Confirm không giữ lượt lần hai | `TestCreateCoupon_ConcurrentLastUse` (20 lần: 1 OK + 1 exhausted, used_count 1); `TestRedeem_ConcurrentMaxUses` | L1 L4 | ĐẠT một nửa | Nửa "Confirm không giữ lượt lần hai" không có test (xem E-05) |
| WAT28 (phía đơn) | Hai hồ sơ trả cùng dòng: tính cả hồ sơ mở | `TestCreateReturn_TD05PerLine` (phiếu hai khi phiếu một còn mở → `ErrReturnQtyExceeds`), `returns_race_test.go: TestCreateReturn_LastUnitRace` | L1 | ĐẠT | |
| WAT29 (phía đơn) | Duyệt return chưa nhận hàng: không tăng tồn | `TestReceiveReturn_TD08Restock` (requested → tồn không đổi, không `order.partially_returned`); `inventory/returns_test.go` | L1 L8 | ĐẠT | |
| WAT31 (phía đơn) | Hoàn một phần có giảm đơn: theo phân bổ, không theo giá hiện tại | `TestCreateReturn_RoundingLastUnit` (500.001 → 250.000 + 250.001); `pricing/refund_test.go: TestRefundFor`, `property_test.go: TestRefundFor_SplitSumsToNet` | L1 L2 | ĐẠT phần làm tròn | Không ca DB nào có giảm cấp đơn/mã rồi đổi giá trước khi trả (xem E-06) |

---

#### 2. Phát hiện

##### E-01: 34 operation M2 trong `openapi.yaml` còn ghi "Chưa làm … (trả 501 not_implemented)" dù đã có handler thật
- Mức: lớn
- Tệp:dòng: `openapi/openapi.yaml`:1474, 1591, 1769, 1799, 1822, 1838, 1862, 1886, 1905, 1929, 1953, 2001, 2018, 2042, 2060, 2079, 2103, 2117, 2140, 2164, 2182, 2239, 2259, 2282, 2299, 2323, 2347, 2371, 2395, 2412, 2436, 2475, 2493 (33 dòng) và 1443 (`createOrder`)
- Spec/kế hoạch: Q20 (operation chưa làm trả 501 tới task của nó); hợp đồng là nguồn sự thật của mã sinh và console (ADR-44).
- Mô tả: Các operation listOrders, updateOrder, listCustomers, createCustomer, getCustomer, updateCustomer, addCustomerIdentity, removeCustomerIdentity, addCustomerAddress, updateCustomerAddress, deleteCustomerAddress, listCustomerMergeCandidates, mergeCustomer, undoCustomerMerge, getCustomerTimeline, addCustomerNote, listCustomerTags, createCustomerTag, updateCustomerTag, addCustomerTag, removeCustomerTag, getCustomerImportTemplate, startCustomerImport, getCustomerImport, confirmOrder, approveOrderDiscount, setOrderPending, cancelOrder, getOrderTimeline, addOrderNote, checkOrderDuplicates, listOrderDuplicates, resolveOrderDuplicate đều có `description: Chưa làm ở Task M2-02 (trả 501 not_implemented) cho tới task ghi trong summary.`; `createOrder` ghi "trước đó gửi trường M2 → 501 not_implemented". Tất cả đã có phương thức handler thật (ví dụ `handlers_order_edit.go:26`, `handlers_customer_merge.go:22,33`, `handlers_order_confirm.go:25,35,45`, `handlers_order_cancel.go:22`, `handlers_order_duplicates.go:24,57,103`, `handlers_customer_import.go:23,39,70`); không operation M2 nào còn trả 501 thật (handler nhúng `gen.UnimplementedHandler`, mọi operation M2 đã có phương thức). Câu cũ đi vào `internal/httpapi/gen/oas_server_gen.go` và client console sinh ra; người đọc hợp đồng (M3–M7, chủ shop) hiểu sai là chưa làm.
- Bằng chứng: `grep -c "Chưa làm ở Task M2-02" openapi/openapi.yaml` → `33`; `openapi.yaml:1443` "từ Task M2-16, M2-17; trước đó gửi trường M2 → 501 not_implemented."
- Đề xuất sửa: bỏ câu "Chưa làm…" khỏi 34 mô tả trong `openapi.yaml`, chạy `make gen` (không sửa tay tệp sinh); thêm vào test hợp đồng (`tests/arch/m2contract_test.go`) một kiểm "không mô tả nào còn `not_implemented` khi operation có handler". Cỡ: nhỏ.

##### E-02: Chữ ký móc nối M2 mà kế hoạch M3 (G-M2-3) dựa vào lệch mã thật; Interfaces Task 79 của kế hoạch M2 cũng lệch
- Mức: lớn
- Tệp:dòng: `internal/modules/order/order.go:57` (`Actor`), `shiphooks.go:27,73,105`, `payments.go:27`, `cancel.go:55`, `returns.go:122,297,349,418`, `edit.go:100`; kế hoạch M3 `2026-11-18-crm-m3-van-chuyen.md:87` (G-M2-3); kế hoạch M2 dòng 1031–1036 (Interfaces Task 79)
- Spec/kế hoạch: M2 Q-T57-2, Q-T57-3; "Phát hiện soát chéo chưa sửa" A2-10/C2-5 ghi "Tên phía M2 đã khớp".
- Mô tả: Mã thật: `order.Actor{TenantID, UserID uuid.UUID; Type string; CanApproveDiscount bool}`; mọi móc nối là phương thức `Writer` và nhận `sellerID` ngay sau `tx`: `AttachShipmentBy(ctx, tx, sellerID, orderID, shipmentID, a, now)`, `DetachShipment(…sellerID…)`, `OnShipmentEvent(…sellerID…, step, a, now)`, `ApplyPaymentTotals(ctx, tx, sellerID, orderID, paidTotal, a, now)`, `Cancel(ctx, tx, sellerID, id, version, reason, note, a, now)`, `CreateReturn(ctx, tx, sellerID, orderID, …)`, `OnShipmentReturning(ctx, tx, sellerID, orderID, shipmentID, a, now)`, `CarrierReturnFor(ctx, tx, sellerID, shipmentID)`, `ReceiveReturn(ctx, tx, sellerID, returnID, version, …)`; sửa đơn là `Edit`. Kế hoạch M3 G-M2-3 vẫn ghi `type Actor struct { Type string; ID uuid.UUID; RequestID, IP string }` (đó là `customer.Actor`, không phải `order.Actor`), các hàm **không** có `sellerID`, và gọi `Update` thay `Edit`; dòng 730 viết `order.OnShipmentEvent(ctx, tx, orderID, shipmentID, …)`. Kế hoạch M2 Task 79 (Interfaces) cũng không có `sellerID` cho `CreateReturn`, `OnShipmentReturning`, `CarrierReturnFor`, `ReceiveReturn` (Q-T57-3 chỉ liệt kê 4 móc nối Task 75). M3 viết theo G-M2-3 sẽ không biên dịch được, hoặc tệ hơn, tự thêm lớp bọc bỏ `sellerID` (đi ngược ADR-29). Tương tự nhỏ hơn ở M5 B1: `customer.FindOrCreateForOrder`, `customer.AddIdentity` ghi là hàm gói, mã là phương thức `customer.Writer` (`fororder.go:47`, `identity.go:52`).
- Bằng chứng: `order.go:57-61` `type Actor struct { TenantID, UserID uuid.UUID; Type string; CanApproveDiscount bool }`; `returns.go:349` `func CarrierReturnFor(ctx context.Context, tx pgx.Tx, sellerID, shipmentID uuid.UUID) (Return, error)`; M3 plan dòng 87 "`AttachShipmentBy(ctx, tx, orderID, shipmentID uuid.UUID, a Actor, now time.Time) error` … `Update` kiểm `version` …".
- Đề xuất sửa: điều phối viên sửa G-M2-3 của kế hoạch M3 (và B1 của M5) theo chữ ký thật; thêm Q-T57-3 cho các hàm phiếu trả Task 79 trong kế hoạch M2; không sửa mã. Cỡ: nhỏ (tài liệu).

##### E-03: `docs/threat-model-m2.md` chưa trỏ tới test nào; Task M2-32 chưa hoàn thiện như chính tệp đòi
- Mức: nhỏ
- Tệp:dòng: `docs/threat-model-m2.md`:4, 11–13, 41–89
- Spec/kế hoạch: tệp tự ghi "Task M2-32 hoàn thiện: điền đúng tên test có trong mã, đổi dòng **Chưa có** thành test hoặc ghi vì sao chấp nhận"; kế hoạch Task 89.
- Mô tả: Cả 26 dòng của 5 bảng chỉ ghi số task (`M2-16 (DH07, 50 song song)`, …), không có tên hàm test. Hai tham chiếu dạng test có thật: `sellerfilter_test` (`tests/arch/sellerfilter_test.go`) và "`personaldata`" (`internal/httpapi/personaldata_test.go`, `internal/platform/queue/personaldata_test.go`). Không còn dòng "Chưa có" theo nghĩa đen, nhưng các dòng D "M2-31 (p95 < 300 ms / 1 triệu khách)" và "M2-31" (deadlock) trỏ tới Task 88 đã dời — cần ghi "chấp nhận, dời tới trước ngày chạy thật" thay vì coi như có. Các test để điền có sẵn (đã chạy PASS ở bảng mục 1): ví dụ S → `TestOrderConfirm_DiscountApproval`; T (đơn giá) → `TestCreate_PriceSnapshot`, `TestEdit_KH11_PriceSnapshot`; T (version) → `TestEdit_DH11_VersionConflict`; T (khóa) → `TestOrderAPI_SameKey50`, `TestOrderAPI_BusinessKeyAfterExpiry`; T (đã gửi hãng) → `TestEdit_LockedByShipment`, `TestCancel_TD03RequestThenCarrierConfirms`; I (tìm) → `TestSearchTenantIsolation`, `TestSearchByNameFunctionDefinition`, `TestCustomerSearchAPI`; T (gộp) → `TestMergeUndoProperty`, `TestCustomerMergeAPI`; T (mã) → `TestCreateCoupon_ConcurrentLastUse`, `TestCancel_Parallel50SameVersion`; I (payload) → `TestCreate_NoPersonalDataInEventsAndJobs`; T (vận đơn cũ) → `TestOnShipmentEvent_StaleShipment`; D (deadlock) → `TestCreateCoupon_NoDeadlockWithCancel`, `TestEdit_NoDeadlock`, `TestEdit_CouponSwapNoDeadlock`.
- Bằng chứng: `grep -c "Test" docs/threat-model-m2.md` không có tên hàm `Test…` nào; dòng 12–13 "tên hàm test viết ở tệp task khi task xong".
- Đề xuất sửa: điền cột "Task / test" bằng tên hàm như trên ở Task 89; ghi rõ dòng p95 là "chấp nhận — dời Task 88". Cỡ: nhỏ.

##### E-04: Threat model ghi `checkCoupon` trả "lý do từ chối chung", mã trả lý do cụ thể (`not_found` khác `expired`, `exhausted`)
- Mức: nhỏ
- Tệp:dòng: `docs/threat-model-m2.md` bảng (4) dòng "I | Dò mã hợp lệ bằng `checkCoupon`"; mã `internal/httpapi/handlers_order_m2_test.go:267-275`, `handlers_promotion_test.go:101-117`
- Spec/kế hoạch: KM02 ("Từ chối và báo lý do") — mã đúng spec; tài liệu an ninh sai.
- Mô tả: Người có `order.create` phân biệt được mã có tồn tại hay không (`not_found` vs `expired`/`exhausted`/…), nên biện pháp thật chỉ là quyền + giới hạn tần suất của M0. Tài liệu nói "lý do từ chối chung" → người duyệt hiểu sai mức phòng vệ. Rủi ro thật thấp (chỉ nhân viên), nhưng M5 mở `checkCoupon` cho khách web thì đúng là kênh dò mã.
- Bằng chứng: test `"c4": {"KHONGCO", "not_found"}`, `"c3": {"KM02", "expired"}` → 422 `coupon_rejected` với `reason` khác nhau.
- Đề xuất sửa: sửa dòng threat model thành "lý do cụ thể (KM02 đòi); chặn dò bằng quyền `order.create` và giới hạn tần suất; M5 khi mở cho khách phải gộp `not_found` với lý do chung". Cỡ: nhỏ.

##### E-05: AT05 và WAT07 chỉ được kiểm một nửa — không test nào khẳng định "xác nhận đơn không giữ hàng / không ghi lượt mã lần hai"
- Mức: nhỏ
- Tệp:dòng: `internal/modules/order/confirm_test.go:79-200` (không có `balance`, `held`, `used_count`); `internal/httpapi/handlers_order_confirm_test.go` (như vậy)
- Spec/kế hoạch: AT05 "không cộng giữ lại khi xác nhận"; WAT07 "nhân viên Confirm không giữ lượt lần hai"; "Xong M2 khi" (2) ghi AT05, WAT07 đạt.
- Mô tả: Hành vi hiện đúng nhờ cấu trúc (`confirm.go` không gọi `inventory` hay `promotion`: `grep inventory|promotion internal/modules/order/confirm.go` rỗng), nhưng không test nào giữ điều đó: một thay đổi sau (ví dụ cho `Confirm` gọi lại `reserve`/`Redeem` khi chốt nháp) sẽ không bị bắt. Ca draft→confirm và new(chờ duyệt)→approve là chỗ dễ thêm nhầm nhất.
- Bằng chứng: `grep -n "balance\|Held\|held\|coupon\|used_count" internal/modules/order/confirm_test.go internal/httpapi/handlers_order_confirm_test.go` → không dòng nào.
- Đề xuất sửa: trong `TestConfirm_New`, `TestConfirm_Draft`, `TestApproveDiscount_New` thêm assert `balance(v).Held` không đổi; một ca đơn nháp có mã → confirm → `used_count` vẫn 1, một dòng `coupon_redemptions`. Cỡ: nhỏ.

##### E-06: WAT31 chưa có ca DB "hoàn một phần trên đơn có giảm cấp đơn / mã, sau khi đổi giá"
- Mức: nhỏ
- Tệp:dòng: `internal/modules/order/returns_test.go:219-233` (ca duy nhất có số lẻ, chỉ giảm dòng 1 đồng, không đổi giá); `returns.go:248` (`pricing.RefundFor(it.NetAmount, …)`)
- Spec/kế hoạch: WAT31 "Theo phân bổ/policy snapshot; không tính bằng giá hiện tại"; kế hoạch Task 79 Step 1.
- Mô tả: Mã đúng — tiền hoàn tính từ `net_amount` đã chụp của dòng (gồm `allocated_order_discount`), không đọc giá `catalog`. Nhưng không test nào dựng đơn có `OrderDiscount`/mã giảm giá (phân bổ khác 0), đổi giá biến thể, rồi trả một phần và assert tiền hoàn = phần `net` (không theo giá mới, không theo `unit_price`). `TestReturns_Invariants` (rapid) chỉ sinh giảm dòng, không giảm cấp đơn. Nếu sau này ai đổi sang `unit_price × qty − line_discount` thì không test nào đỏ.
- Bằng chứng: `grep -n "OrderDiscount\|price = \|CouponCode" returns_test.go returns_property_test.go` → chỉ `returns_test.go:348` (ca chặn mã trên đơn thay thế).
- Đề xuất sửa: thêm một ca: đơn 2 dòng, `OrderDiscount 100.001`, giao, `UPDATE catalog.variants SET price…`, trả 1 món → `RefundAmount` = `RefundFor(net dòng…)`; cho rapid sinh thêm `OrderDiscount`. Cỡ: nhỏ.

##### E-07: AT07 "đổi kho" không có trong M2 nhưng nghiệm thu ghi AT07 đạt trọn
- Mức: nhỏ
- Tệp:dòng: `internal/modules/order/edit.go` (`EditInput` không có trường kho); `docs/nghiem-thu-m2.md` tiêu chí 9; kế hoạch dòng 17
- Spec/kế hoạch: AT07 "Tăng lượng/đổi kho nhưng kho mới thiếu → rollback nguyên tử".
- Mô tả: M2 chỉ cho sửa số lượng/dòng; không có đường đổi kho của đơn (một kho mặc định). Phần "tăng lượng" có test (DH10). Khi điền nghiệm thu nên ghi "AT07 phần tăng lượng; đổi kho: mốc có nhiều kho" để không báo đạt cả ca.
- Bằng chứng: `grep -n "type EditInput" -A25 edit.go | grep -i "warehouse"` → rỗng.
- Đề xuất sửa: ghi chú phạm vi ở `docs/nghiem-thu-m2.md` (và kế hoạch mục "Xong M2 khi"). Cỡ: nhỏ.

##### E-08: `resolveOrderDuplicate` (hủy một đơn) không mang `version`, trái luật hợp đồng "mọi chuyển trạng thái có `version`"
- Mức: nghi ngờ
- Tệp:dòng: `openapi/openapi.yaml` (`ResolveOrderDuplicateRequest` chỉ có `action`); `internal/modules/order/duplicates.go:359-369` (`lockOpenPair` lấy version hiện tại rồi `w.Cancel(…, versions[target], CancelDuplicate, …)`)
- Spec/kế hoạch: kế hoạch dòng 317 "mọi PATCH và chuyển trạng thái có `version` trong thân"; nhưng Interfaces Task 80 (dòng 1067) không có `version` → kế hoạch tự mâu thuẫn.
- Mô tả: Nhân viên A mở danh sách "Đơn trùng", trong lúc đó nhân viên B sửa/xác nhận đơn O2 (thêm dòng, đổi địa chỉ). A bấm "hủy đơn kia" → O2 bị hủy theo version mới mà A chưa thấy. Cặp phải còn "mở" nên không hủy hai lần, và đơn đã gửi hãng thì thành `cancel_requested`, nên không mất tiền/tồn; thiệt hại là hủy nhầm một đơn vừa được sửa. Ghi "nghi ngờ" vì có thể đây là chủ ý (chỉ khóa cặp).
- Bằng chứng: `duplicates.go:369` `if _, err := w.Cancel(ctx, tx, sellerID, target, versions[target], CancelDuplicate, "", a, now)`.
- Đề xuất sửa: thêm `version` (của đơn bị hủy) tùy chọn vào thân, so trước khi `Cancel`; hoặc ghi ngoại lệ vào kế hoạch dòng 317. Cỡ: nhỏ.

##### E-09: Consumer `customer_stats` nghe topic `order.delivery_reversed` chưa có trong `docs/views.md`
- Mức: nhỏ
- Tệp:dòng: `internal/modules/customer/stats.go:29,195-200`; `docs/views.md:80-96` (mục "Sự kiện công bố")
- Spec/kế hoạch: Task 82 Interfaces (M3 sẽ phát `order.delivery_reversed`).
- Mô tả: Đúng kế hoạch là M3 phát; nhưng tên topic đã được M2 cố định trong mã (consumer đã đăng ký) mà bảng sự kiện công bố không có dòng nào (kể cả dòng giữ chỗ "M3 phát"). M3 có thể đặt tên khác (`order.delivery_reverted`…) mà không test nào đỏ: consumer sẽ im lặng không tính lại chỉ số. So mã và tài liệu: topic có trong mã mà không có trong `views.md`: `order.delivery_reversed` (2 topic `order.partially_returned`, `order.returned` có trong dòng gộp nên không tính).
- Bằng chứng: `stats.go:29` `TopicOrderDeliveryReversed = "order.delivery_reversed"`; `grep delivery_reversed docs/views.md` rỗng.
- Đề xuất sửa: thêm dòng `order.delivery_reversed` (phát ở M3, Task 97; consumer M2 đã nghe) vào bảng sự kiện. Cỡ: nhỏ.

---

#### 3. Đã kiểm, không thấy lỗi

- **Hợp đồng (openapi, phần M2, 59 operation):** `x-permission` của cả 57 operation trong bảng Task M2-02 khớp từng dòng, mọi quyền có trong `identity/permissions.go:56-82` và `docs/quyen.md`; mọi POST/PATCH/PUT/DELETE M2 có `$ref IdempotencyKey` và `IdempotencyMode: "required"` trong `routes_gen.go`; mọi operation có `default: Error` (`{error, request_id}`); `x-error-codes` khớp hai chiều hằng `code*` của `errors.go`; mã trạng thái thành công khớp handler (201/200/202). `TestM2Contract_Operations` PASS.
- **Không operation M2 nào còn trả 501 thật.**
- **Dữ liệu cá nhân trong sự kiện/realtime/audit:** payload `order.*` (`Changed{OrderID, SellerID, Version, CustomerID, Status}`), `ReturnReceived`, `customer.created`, `customer.merged/merge_undone`, `coupon.used` chỉ mang mã và số; realtime `customer.changed`/`order.changed` chỉ mang id; audit `order.*` (`Order.AuditFields`: mã, trạng thái, tiền, số dòng), `customer.*` (tên trường + id, `addressAudit` chỉ tên trường), `promotion.coupon.*` (`AuditFields` chỉ `bound` bool, không `bound_phone`); ghi chú đơn audit chỉ `noteId`. Có test: `TestCreate_NoPersonalDataInEventsAndJobs`, `TestOrderAPI_CreateM2Confirmed` (audit), `TestCancel_TD01ReleasesStock` (ghi chú hủy không vào sự kiện), `TestTimeline_DH06`, `TestCustomerSearchAPI` (log + span).
- **`docs/views.md` khớp view thật:** `TestViewsDocMatchesDatabase`, `TestViewsDocParses`, `TestM2Contract_ViewsDoc` PASS (so tên và thứ tự cột mọi view `_v` trên database đã migrate).
- **ADR-46:** 19 bảng M2 tạo ở `migrations/02*.sql` đều có trong bảng thứ tự khóa (nhóm 5, 7, 10) hoặc mục (e) ngoài thứ tự (`customer_notes`, `order_notes`, `admin_units`, `customer_tags`). Soát tay: `UndoMerge` khóa `customers` (moved_ids tăng dần) rồi `customer_merges` (`merge.go:208-215`); `changeReturn`/`CancelReturn` khóa đơn (cả đơn thay thế) rồi phiếu (`returns.go:683-691`); tạo đơn: khách (5) → đơn mới → `coupons` (10) → `stock_balances` (11). Test song song không deadlock đã chạy PASS: `TestCreateCoupon_NoDeadlockWithCancel`, `TestEdit_NoDeadlock`, `TestEdit_CouponSwapNoDeadlock` (L10), `TestCancel_Invariants`.
- **Tên mà M3–M7 dựa vào có trong mã:** `orderview.Axes`, `LabelKey`, `Label`, `Labels`; `customer.StatsFor`, `Writer.RecomputeStats`, `Writer.CreateCustomer`, `NormalizePhone(raw) (logging.Phone, error)` (không có `customer.Phone`), `ListProvinces`, `ListWards`, `OrderCustomerInput{CustomerID, FullName, Phone, PhoneIsCustomers, Source}`; `order.SourceByKey`, `ActiveSource`, `EnsureDefaults`, `Lock`, `GetDetail`, `Get`, `Confirm`, `AddNote`, `ExpireDrafts` + lịch `order.expire_drafts`, `ErrOutOfStock`, `ErrNotForSale`, `ErrMixedSellers`, `ErrVariantNotFound`, `ErrVersionConflict`, `CreateInput.ShipAddress *ShipAddress`, `Actor.Type`; `inventory.Writer.ReleaseInTransitByItems`, `ReceiveCustomerReturn`, `MoveToDamaged`, `ReturnLine`; `promotion.Writer.Redeem`, `Release`, `Check`, `CreateBatch`, `RejectError`, `CouponTemplate`; operation `getCouponBatch`; `queries/ord/orders_base.sql`, `code.sql`; gói `internal/platform/xlsx`; quyền `fulfillment.read`; `goose.WithAllowOutofOrder` (`db/migrate.go:75`); `service_key` (`migrations/0290`). Tên M4/M5 chưa có (`FindOrCreateByIdentity`, `LockRoot`, `HasPhone`, `RegisterFilePurposes`) là hàm các mốc đó tự tạo theo kế hoạch của chúng, không phải thiếu của M2. (Lệch chữ ký: xem E-02.)
- **Ánh xạ ca kiểm:** 19/19 ca M2 có test máy kiểm đúng tình huống của `11-kich-ban-kiem-thu.md`; 13 ca AT/WAT có test cho phần thuộc M2 (ngoại lệ một nửa: E-05, E-06, E-07).
- **Màn hình:** vitest `len-don` (pricing dùng chung vector Go, gửi lại cùng khóa) và `MergeView` PASS.

## Lời giao chung cho năm reviewer

#### Review độc lập M2 — lời giao chung

Bạn là reviewer ĐỘC LẬP của mốc M2 (Khách hàng, đơn hàng, mã giảm giá) của một CRM Go + PostgreSQL + React.
Bạn chỉ có spec, kế hoạch và mã. Không có lập luận của người viết; đừng tin mô tả trong `docs/tasks/*.md` hay commit
message — kiểm bằng mã và test.

##### Nguồn
- Repo (git worktree, nhánh task-89 = main hiện tại): `apps/crm (worktree task-89)`
  (gọi tắt W). Luật dự án: `W/CLAUDE.md`, `W/docs/quy-trinh-lam-viec.md` (mục 5, 6), `W/.claude/agents/reviewer.md` (7 mục soát).
- Spec 2.1: `(gốc repo ELIMAZ)/docs/crm/specs/2026-11-10-crm-rieng/` — `05-du-lieu-erd.md` mục 1, 2, 6;
  `04-kien-truc.md` mục 3, 5.5, 11, 12, 18; `07-adr.md` ADR-23, 24, 29, 41, 46; `11-kich-ban-kiem-thu.md` (ca có Mốc = M2).
- Kế hoạch M2: `(gốc repo ELIMAZ)/docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md`
  ("Xong M2 khi" dòng 17, Quyết định thay chủ shop ~21, Global Constraints ~107, Review Focus ~138, từng task 57–90,
  "Ánh xạ ca kiểm M2 → task" ~1338).
- `W/docs/threat-model-m2.md`, `W/docs/nghiem-thu-m2.md`, `W/docs/views.md`, `W/docs/quyen.md`, `W/docs/adr/0046-thu-tu-khoa.md`.
- Phạm vi M2 trên main: các commit gộp `merge: Task NN` với NN ∈ 57–87, 90, 425, 427
  (`git -C W log --first-parent --oneline main | grep "merge: Task"`; xem một task: `git -C W show -m --first-parent --stat <merge>`).
  Migration M2: `W/migrations/02*.sql`. Module: `W/internal/modules/{customer,order,promotion}`, phần M2 của
  `W/internal/modules/inventory` (byitems.go, returns.go), handler `W/internal/httpapi/handlers_*` của M2, hợp đồng
  `W/openapi/openapi.yaml`, màn `W/console/src/screens/{orders,customers,coupons,...}`.
- Thử tải M2 (Task 88) đã được chủ shop DỜI tới trước ngày chạy thật (19/11): KHÔNG báo thiếu phép đo p95 / 1 triệu khách
  là phát hiện; chỉ ghi nếu mã có lỗi hiệu năng rõ ràng (Seq Scan trên bảng lớn, N+1 trong vòng lặp ghi).
- Các chỗ màn hình khác mockup do API thiếu đã được điều phối viên gom riêng: KHÔNG báo lại các chỗ "màn hình chưa có vì
  API thiếu"; chỉ báo khi màn hình làm SAI (hiện hành vi máy chủ không làm, bỏ kiểm quyền, sai tiền, mất dữ liệu).

##### Ràng buộc
- CHỈ ĐỌC. Không sửa, không tạo tệp trong W, không git commit/stash/checkout/merge. Chỉ ghi tệp kết quả của bạn.
- Được chạy test để xác minh: `cd W && go test -count=1 ./internal/<gói>/... -run <Tên>` (DB test đọc
  CRM_TEST_DATABASE_URL đã có sẵn; test tự tạo database riêng). Màn hình: `cd W/console && npx vitest run <tệp>`.
  KHÔNG chạy make ci, make test, make db-up/db-down/db-reset, docker compose; KHÔNG pkill/killall. Mỗi lần chạy chỉ một gói.
  Không chạy gremlins (một phiên khác đang chạy).
- Chỉ báo phát hiện về TÍNH ĐÚNG, an ninh, mất dữ liệu, đồng thời, lệch spec/kế hoạch, test thiếu cho ca nghiệm thu,
  tên mà mốc sau dựa vào bị lệch. Bỏ qua phong cách.
- Mỗi phát hiện phải kiểm chứng bằng đọc mã thật (trích dòng) — nếu chỉ nghi ngờ, ghi mức "nghi ngờ".
- Làm trong khoảng tối đa ~60–90 phút công; ưu tiên chỗ rủi ro cao (tiền, tồn, đồng thời, quyền, RLS, dữ liệu cá nhân).

##### Đầu ra (tiếng Việt, Markdown) — ghi vào tệp được giao, rồi trả lời ngắn (≤ 15 dòng) tóm tắt số phát hiện theo mức.
Mỗi phát hiện:
```
###### <mã>: <tiêu đề một dòng>
- Mức: nghiêm trọng | lớn | nhỏ | nghi ngờ
- Tệp:dòng: <đường dẫn tương đối W>:<dòng>
- Spec/kế hoạch: <mục bị lệch>
- Mô tả: <điều sai, tình huống cụ thể gây sai>
- Bằng chứng: <trích mã / lệnh đã chạy và kết quả>
- Đề xuất sửa: <ngắn>; cỡ: nhỏ (<50 dòng, 1 gói) | lớn
```
Cuối tệp: mục "Đã kiểm, không thấy lỗi" liệt kê những gì đã soát mà ổn (để biết độ phủ).
