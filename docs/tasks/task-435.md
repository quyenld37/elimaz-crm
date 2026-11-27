# Task 435 — Lên đơn: SĐT của khách khác, gắn SĐT cho khách chưa có số (A-01, A-05)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi: không đổi chữ ký nào. `customer.Writer.FindOrCreateForOrder` (4 chỗ gọi mã sản xuất: `order/create_m2.go:159`, `order/web.go:321` qua Source web; test ~20) đổi hành vi nhánh `findOrCreate` cho nguồn khác web. `OrderCustomer.PhoneConflictWith` nay cũng trả ở nhánh này. Không thêm ký hiệu xuất.
- Gói phụ thuộc ngược: `order` (create_m2, web), `httpapi` (handlers_order); `promotion` chỉ đọc kết quả.
- Operation OpenAPI: `createOrder` thêm trường tùy chọn `phoneIsCustomers` ở gốc thân (chỉ có nghĩa với `customerId`; gửi cùng `customer` → 400). Chỉ thêm, oasdiff không phá.
- Màn hình: `console/src/screens/len-don/AddressFields.tsx`, `orderRules.ts` (`buildRequest`), `OrderForm.tsx`; console/src/api sinh lại bằng `make gen`.
- Không đổi câu sqlc, bảng, migration.
- Test: `customer` (`TestFindOrCreateForOrder`, race, lock), `order` (`create_m2_test`, `create_coupon_test`, web), `httpapi` (createOrder), vitest `orderRules.test.ts`, `OrderForm.test.tsx`; chạy ở ci-task.

## Tái dùng

- Đã kiểm `docs/danh-muc-dung-chung.md` và grep `PhoneConflictWith`, `identityOwner`, `lockFound`: đã có đủ.
- Dùng lại `identityOwner`, `lockFound`, cơ chế `PhoneConflictWith` và cảnh báo `phone_belongs_to_other_customer` của order. Không viết hàm dùng chung mới.

## Soát thiết kế

Chỉ các mục liên quan:
- Khóa (ADR-46 nhóm 5): nhánh mới tạo khách không gắn SĐT, sau đó `lockFound` không dùng nữa cho nguồn nhân viên; hồ sơ chủ SĐT không bị khóa (chỉ đọc `identityOwner`), nên không thêm khóa mới, không đổi thứ tự khóa. Test race giữ nguyên (nguồn mặc định `order` giờ tạo khách mới không gắn số khi SĐT đã có chủ; ca song song cùng SĐT mới của nhân viên: bên thua `taken` → tạo lại không SĐT + `PhoneConflictWith`).
- Tính đúng (6): không tự gộp, không gắn SĐT của nhóm khác (DH12, DH13); đơn web giữ hành vi cũ (nguồn `web`).
- Dữ liệu cá nhân: `PhoneConflictWith` chỉ mang mã gốc, cảnh báo chỉ mã.
- Còn lại không áp dụng: không migration, không hàng đợi, không tiền.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| A-01 máy chủ: nhân viên chọn khách mới, SĐT đã của B → khách mới không số, PhoneConflictWith = gốc B, không gộp | `TestFindOrCreateForOrder/A-01_nhân_viên_chọn_khách_mới,_SĐT_của_B` | `go test -race -count=1 -run '^(TestFindOrCreateForOrder\|TestFindOrCreateForOrderRace\|TestFindOrCreateForOrderRaceStaff\|TestFindOrCreateForOrder_LocksRootAgainstMerge)$' ./internal/modules/customer/` | ok (8,0 s) |
| Đơn web giữ hành vi gắn vào hồ sơ có SĐT, cả khi song song | `.../không_mã_khách,_SĐT_của_B_(đơn_web)`, `TestFindOrCreateForOrderRace`, `TestFindOrCreateForOrder_LocksRootAgainstMerge` | cùng lệnh | ok |
| A-01 song song (nhân viên, cùng SĐT mới): đúng 1 danh tính, 9 bên thua có khách riêng + cảnh báo | `TestFindOrCreateForOrderRaceStaff` | cùng lệnh | ok |
| A-01 ở đơn: cảnh báo phone_belongs_to_other_customer, B không đổi; "Dùng hồ sơ B" không cảnh báo | `TestCreate_NewCustomerWithPhoneOfOther_A01` | `go test -race -count=1 -run '^(TestCreate_NewCustomerWithPhoneOfOther_A01\|TestCreate_ExistingCustomerWithoutPhone_A05\|TestCreate_PhoneOwnership)$' ./internal/modules/order/` | ok (2,3 s) |
| A-05 đơn: khách chưa SĐT + cờ → gắn số; khách có SĐT không cờ → không thêm | `TestCreate_ExistingCustomerWithoutPhone_A05` | cùng lệnh | ok |
| A-05 API: `phoneIsCustomers` với customerId; cùng customer → 400 | `TestOrderAPI_PhoneIsCustomersWithCustomerID` | `go test -race -count=1 -run '^(TestOrderAPI_PhoneIsCustomersWithCustomerID\|TestOrderAPI_OrderForSomeoneElse)$' ./internal/httpapi/` | ok (2,8 s) |
| Màn hình A-01: câu "Số này đang là của khách X", hai nút, Dùng hồ sơ chọn khách có sẵn, Vẫn tạo khách mới | `OrderForm.test.tsx` 2 ca A-01 | `cd console && npx vitest run src/screens/len-don/` | 4 tệp, 290 test qua |
| Màn hình A-05: ô bật sẵn cho khách chưa SĐT, gửi/không gửi cờ; khách có SĐT không có ô | `OrderForm.test.tsx` 2 ca A-05, `orderRules.test.ts` 2 ca | cùng lệnh; `npx tsc --noEmit`; `npx eslint src/screens/len-don/` | qua, sạch |

Ghi chú trung thực: mã sửa viết trước khi chạy các test mới lần đầu (không có lần chạy đỏ trước); ca cũ "SĐT của B" của nhân viên đã đổi kỳ vọng theo hành vi mới.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

Task bù sau review độc lập M2 (A-01, A-05), không có trong kế hoạch. Hành vi theo chốt của điều phối 27/11 khuya.

## Quyết định thay chủ shop

- A-05 qua API: thêm trường tùy chọn `phoneIsCustomers` ở gốc thân `createOrder` (đi cùng `customerId`), vì hợp đồng cũ không cho gửi cờ cùng `customerId`. Vì sao: máy chủ đã hỗ trợ (`useExisting` + `linkPhone`), chỉ thiếu đường vào; chỉ thêm nên không phá. Phương án khác: cho gửi `customer` cùng `customerId` (đổi luật "đúng một trong hai", rộng hơn).
- "Khách đã có SĐT khác → số mới chỉ là SĐT người nhận": làm ở màn hình (không hiện ô, không gửi cờ), máy chủ vẫn tôn trọng cờ tường minh. Vì sao: chặn ở máy chủ sẽ cấm việc thêm số thứ hai có chủ ý của nhân viên.
- "Vẫn tạo khách mới" chỉ ghi nhận lựa chọn trên màn (đã có hành vi an toàn mặc định: khách mới không giữ số, đơn ghi cảnh báo); không chặn lưu khi chưa bấm. Phương án khác: bắt buộc chọn một nút trước khi lưu.
- Chỉ nguồn `web` giữ hành vi "gắn vào hồ sơ có SĐT"; mọi nguồn khác (nhân viên, import…) theo luật mới.

## Việc phát hiện thêm

(không có)

## E2E viết sẵn, chưa chạy

(không có: kế hoạch không yêu cầu e2e; đủ ca ở test gói customer, order, httpapi và vitest)

## Reviewer

Kết luận: ĐẠT (reviewer, vòng 1; ci-task từ xa xanh, run 46044993842). Không có phát hiện ảnh hưởng tính đúng. Ghi chú: màn chỉ hiện ô gắn số cho khách có sẵn sau khi tải xong hồ sơ; bấm lưu trước lúc đó thì không gửi cờ (an toàn: số chỉ là SĐT người nhận). Mục Ảnh hưởng nhắc `create_coupon_test` nhưng tệp đó không đổi.
