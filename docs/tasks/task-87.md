# Task 87 — M2-30: Màn Mã giảm giá và Cài đặt đơn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 87 = M2-30). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ chạm màn hình `console/`, không sửa Go.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn mới chỉ **gọi** listCoupons, createCoupon, getCoupon, updateCoupon, createCouponBatch, getCouponBatch, listCouponRedemptions (Task 71, M2-14); getOrderSettings, updateOrderSettings, listTrafficSources, createTrafficSource, updateTrafficSource (Task 72, M2-15). Client sinh `console/src/api/**` không đổi.
- Chỗ dùng trên màn hình:
  - Mới `console/src/screens/ma-giam-gia/` (route `/ma-giam-gia`, `/ma-giam-gia/:id`) và `console/src/screens/cai-dat-don/` (route `/cai-dat/don`), kèm test.
  - `console/src/app/nav.ts`: `NAV_GROUPS` thêm nhóm "Khuyến mãi" sau "Khách hàng" (khung mockup M2 `docs/mockups/m2/khung.js`). Nơi dùng NAV_GROUPS: `loadScreens.ts` (kiểm nhóm), `navSections`, `app/screens.test.tsx` (xếp theo NAV_GROUPS, không liệt kê tay) → không cần sửa chỗ khác.
  - Dùng lại, không sửa: `useSources`, `useOrderSettings`, `usePerms`, `ORDER_TOPIC` (`screens/don/orderData.ts`); `Dialog`, `RefusalBox` (`screens/staff/staffData.tsx`); `formatMoney` (`products/productRules.ts`); `moneyInput`, `parseMoney` (`products/editRules.ts`); `useDebounced`, `SEARCH_DELAY_MS` (`inventory/Balances.tsx`); `stubDialog` (`don/donTestkit.tsx`). Màn Lên đơn đọc cùng khóa `ORDER_TOPIC "traffic-sources"`/`"settings"` → sau khi sửa nguồn, cài đặt thì Lên đơn đọc bản mới.
  - Mới `console/src/app/screenTestkit.tsx` (chỉ test dùng): `Write`, `reply`, `NOT_FOUND`, `bodyOf`, `renderScreens` — gom từ `khach/khachTestkit.tsx` khi jscpd báo testkit mới trùng; `khachTestkit.tsx` đổi sang nhập từ đây (không đổi hành vi; test màn Khách và `e2e/khach.spec.ts` vẫn qua).
  - `console/src/app/errors.ts`: chỉ thêm dòng nếu thiếu (dự kiến không: mọi mã của các API trên đã có câu).
- Câu sqlc, bảng, pha migration: không áp dụng (không chạm database).
- Test sẽ chạy: `cd console && npx vitest run src/screens/ma-giam-gia src/screens/cai-dat-don src/app`, `npx tsc --noEmit`, `npx eslint .`; cổng kiểm của task (ci-task) một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `console/src/shared` (useWriteAction, unwrap, errorMessage, queryKey, shortTime); grep `parseMoney|moneyInput|formatMoney|datetime-local|Blob|download|csv|xlsx` trong `console/src`.
- Dùng lại / viết mới vì:
  - Dùng lại các hàm nêu ở Ảnh hưởng (tiền, hộp thoại, hộp báo từ chối, quyền, nguồn đơn, cài đặt đơn, tìm có trễ).
  - Viết mới trong màn (không dùng chung): đọc ô phần trăm → phần vạn; giờ ↔ phút và % ↔ điểm cơ bản của cài đặt; ô `datetime-local` ↔ ISO theo giờ Việt Nam (chưa màn nào có ô ngày giờ; `CustomerProfile` chỉ có ô ngày); trạng thái hiện của mã (Đang chạy, Hết lượt, Hết hạn, Chưa tới ngày, Đã tắt); tạo tệp CSV danh sách mã của đợt (chưa có chỗ nào tạo tệp tải về ở trình duyệt). Chỉ màn này dùng và đổi theo luật riêng (mã giảm giá, cài đặt đơn) → để trong thư mục màn, không đưa vào `shared/`.
  - Cảnh báo trùng jscpd (ci-task lần 1: 12 đoạn mới) — đều gộp: API giả và dựng màn của testkit thành `app/screenTestkit.tsx` (cùng mục đích với testkit màn Khách, khach dùng lại); ô chữ có nhãn thành `TextField`, chân hộp tạo thành `CreateFooter`, hàng giảm giá thành `discountRow` (`CouponForm.tsx`, dùng ở Tạo mã, Tạo đợt, Chi tiết); hàng "Tải thêm" thành `MoreBar.tsx` (danh sách mã, lượt dùng); bảng mã tách `CouponTable`; bước lặp trong test thành hàm (`openThu10`, `saveOnce`, `fill`). Không đụng màn Đơn, Khách ngoài testkit. Sau sửa: `make jscpd` → "Found 0 clones".
  - Không thêm thư viện (console không có thư viện xlsx trong ADR-44) → danh sách mã tải dạng CSV (xem Quyết định thay chủ shop).

## Soát thiết kế

Màn hình chỉ gọi API sẵn có; mọi luật nghiệp vụ, quyền, đồng thời nằm ở máy chủ (Task 71, 72).

- 5.1–5.7: không áp dụng: không thêm dữ liệu, bảng, truy vấn, transaction hay lời gọi ra ngoài.
- 5.8: không áp dụng (không có Go). Màn gọi API chỉ qua client sinh (luật console 2), không `as` trên dữ liệu API (luật 3).
- 5.9 Đường ghi có khóa chống trùng: mọi lệnh ghi (tạo/sửa mã, tạo đợt, sửa cài đặt, tạo/sửa nguồn) đi qua `useWriteAction` (`Idempotency-Key` sinh khi bấm, giữ khi tự gửi lại). Quyền đã khai trong `openapi.yaml`; màn ẩn nút theo quyền, máy chủ vẫn chặn.
- 5.10, 5.11: không áp dụng: không dùng Valkey; không thêm thư viện.
- 5.12 Theo mẫu: như màn Khách, Đơn (Task 85, 86): thư mục `src/screens/<tên>/` tự đăng ký (Q-T57-8), TanStack Query với khóa bắt đầu bằng chủ đề realtime (`promotion.coupon`, `ORDER_TOPIC`), testkit fetch giả trả đúng kiểu client sinh.
- 6.1 Hai người cùng sửa: mọi PATCH gửi `version` đang xem; 409 version_conflict → hộp báo có nút Tải lại (cài đặt đơn theo mockup B). Sắp xếp nguồn đơn: mỗi nguồn đổi vị trí là một PATCH riêng có version; lỗi giữa chừng → dừng, báo, tải lại danh sách.
- 6.2 Bấm hai lần, mạng chập: useWriteAction trả lại lời hứa đang chạy, gửi lại cùng khóa; nút khóa khi đang gửi. Tạo đợt 500 mã chạy đồng bộ trong một transaction ở máy chủ; gửi lại cùng khóa nhận lại đúng đợt đã tạo.
- 6.3–6.9: không áp dụng: không có khóa, transaction, việc nền phía màn hình.
- 6.10 Tiền: ô tiền đọc bằng `parseMoney` (số nguyên đồng); phần trăm gửi `value` phần vạn số nguyên (10% → 1000, 12,5% → 1250), không số thực.
- 6.11: không áp dụng (không phải vùng rủi ro cao); luật đổi đơn vị có test bảng.
- 6.12 Dữ liệu cá nhân: màn không hiện SĐT (API mã không trả SĐT gắn mã, lượt dùng không có SĐT); không log.
- 6.13 Quyền: nút ghi theo `promotion.manage` / `order.settings.manage`; màn Cài đặt đơn chỉ hiện với ai có `order.settings.manage` và `order.read`; máy chủ vẫn kiểm.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Nhập 10% → gửi `value 1000` (phần vạn); 12,5% → 1250; tiền theo đồng | `CouponList.test.tsx` "nhập 10% → gửi value 1000 …", "số tiền: value theo đồng …"; `CouponDetail.test.tsx` "mọi ô điều khoản sửa được; 12,5% → value 1250"; `couponRules.test.ts` "parsePercent: ô % → phần vạn", "readTerms …" | `cd console && npx vitest run src/screens/ma-giam-gia src/screens/cai-dat-don` | `Test Files 7 passed (7)`, `Tests 71 passed (71)` |
| Mã đã dùng → ô giá trị khóa, ghi rõ vì sao; chỉ gửi tên, hạn cuối, tổng lượt; tổng lượt dưới số đã dùng bị chặn; máy chủ trả coupon_locked_after_use thì hiện câu của mã | `CouponDetail.test.tsx` "ô điều khoản khóa, ghi rõ vì sao …", "lưu chỉ gửi tên, hạn cuối, tổng lượt kèm version", "tổng lượt dưới số đã dùng …", "máy chủ vẫn trả coupon_locked_after_use …", "chưa dùng nhưng có lượt đã trả … → khóa", "lưu xong: đọc lại mã và báo Đã lưu" | như trên | như trên |
| Tạo đợt 500 mã → hiện tiến độ (đang sinh, nút tải khóa) và tải được danh sách 500 mã (5 trang × 100) | `CouponList.test.tsx` "500 mã: đang sinh hiện tiến độ; xong tải được tệp đủ 500 mã"; `couponRules.test.ts` "codesCsv …" | như trên | như trên |
| Cài đặt ngoài giới hạn → 422 setting_out_of_range báo dưới đúng ô theo `field` | `OrderSettings.test.tsx` "ngoài giới hạn → 422 setting_out_of_range: báo dưới đúng ô theo tên trường"; `settingsRules.test.ts` "câu ngoài khoảng theo tên trường …" | như trên | như trên |
| Cài đặt hiện giá trị hiện hành và giới hạn; đổi đơn vị giờ→phút, %→điểm cơ bản; version_conflict có Tải lại | `OrderSettings.test.tsx` "hiện giá trị hiện hành …", "lưu đổi đơn vị …", "người khác vừa lưu (409 version_conflict) …" | như trên | như trên |
| Nguồn đơn: thêm, trùng tên, đổi tên, đưa xuống (mỗi nguồn một PATCH có version, khóa khác nhau), tắt, bật lại, nguồn hệ thống không tắt | `OrderSettings.test.tsx` mục "Nguồn đơn" (5 test) | như trên | như trên |
| Danh sách: chip trạng thái, mã thuộc đợt, lọc q/status/batchId, ẩn nút khi thiếu quyền, lỗi API | `CouponList.test.tsx` mục "Danh sách mã" (4 test); `CouponDetail.test.tsx` "chỉ có promotion.read …" | như trên | như trên |
| Đăng ký route, nhóm menu "Khuyến mãi", quyền màn Cài đặt đơn | `ma-giam-gia/route.test.tsx`, `cai-dat-don/route.test.tsx`; `app/screens.test.tsx` | `npx vitest run src` | `Test Files 71 passed (71)`, `Tests 889 passed (889)` |
| Cổng kiểm của task | — | ci-task (lần 3; lần 1 đỏ jscpd đã gộp, lần 2 đỏ do chạy giữa lúc đang sửa test) | `CI-TASK XANH (so với điểm tách nhánh 654592f)` |
| Sau khi gộp main (f7173a5, Task 411, fast-forward, không chạm tệp của task) | toàn bộ test console | `cd console && npx tsc --noEmit && npx eslint . && npx vitest run src` | `Test Files 71 passed (71)`, `Tests 890 passed (890)` |
| Typecheck, lint | — | `npx tsc --noEmit`; `npx eslint src/screens/ma-giam-gia src/screens/cai-dat-don src/app` | không lỗi |
| Bố cục 1512×767, 390×844, 360 không tràn ngang; ảnh so mockup | `e2e/ma-giam-gia.spec.ts` (7 test), `e2e/cai-dat-don.spec.ts` (2 test), `e2e/shell.spec.ts` (menu) | `CRM_E2E_PORT=5187 npx playwright test e2e/ma-giam-gia.spec.ts e2e/cai-dat-don.spec.ts e2e/shell.spec.ts --project=chromium` | `13 passed` (lần 2 sau khi gộp trùng, thêm `e2e/khach.spec.ts`: `22 passed`); ảnh `console/test-results/ma-giam-gia-{danh-sach,tao-ma,tao-dot,chi-tiet}-{1512,390}.png`, `ma-giam-gia-{danh-sach,chi-tiet}-360.png`, `cai-dat-don{,-loi}-{1512,390}.png` |

So ảnh với mockup (`docs/mockups/m2/png/ma-giam-gia-*.png`, `cai-dat-don-*.png`): đầu trang hai nút, thanh tìm + hai ô lọc, bảng 6 cột cùng thứ tự và chip, "Tải thêm"; hộp Tạo mã cùng thứ tự ô và lưới 3 cột; chi tiết hai cột (form có hộp giải thích + ô "· khóa" | Lượt dùng); Cài đặt đơn bốn hàng tiêu đề/mô tả | ô, nguồn đơn với nút ↑ ↓ Đổi tên Tắt, nguồn đã tắt mờ có Bật lại. Chỗ lệch có lý do — xem "Cần chủ shop xem".

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `console/src/app/screenTestkit.tsx` và sửa `khach/khachTestkit.tsx` dùng nó (gộp trùng theo jscpd); thêm `MoreBar.tsx`.
- Đường tệp: kế hoạch ghi `console/src/features/coupons/…`, `features/order-settings/…`; theo Q-T57-8 (như Task 84–86) mọi tệp ở `console/src/screens/ma-giam-gia/` và `console/src/screens/cai-dat-don/`. Thêm `couponRules.ts`, `couponData.ts`, `couponTestkit.tsx`, `ma-giam-gia.css`, `settingsRules.ts`, `cai-dat-don.css`; hai spec Playwright chụp ảnh.
- `console/src/app/nav.ts`: `NAV_GROUPS` thêm nhóm "Khuyến mãi" (khung mockup M2). `errors.ts` không cần thêm dòng (mọi mã lỗi của các API này đã có câu).

## Quyết định thay chủ shop

- Tải danh sách mã của đợt dạng **CSV** (có BOM, Excel mở được), không phải .xlsx như mockup: console không có thư viện xlsx trong ADR-44, và API không có đường xuất. Phương án khác: thêm thư viện xlsx (cần dòng phu-thuoc.md và duyệt) hoặc API xuất ở máy chủ. Dễ đảo ngược: chỉ đổi `codesCsv`/`saveTextFile`.
- **Tiến độ đợt mã:** API tạo cả đợt trong một yêu cầu, không báo số đã sinh; thẻ hiện thanh chạy và "Đang sinh 500 mã…", xong hiện "500 / 500" và nút tải. Câu "Có thể rời trang" của mockup đổi thành "Giữ trang này mở; xong sẽ có nút tải" (rời trang thì máy chủ vẫn tạo xong, nhưng thẻ mất; vào lại lọc theo đợt từ danh sách).
- **Ô lọc "Đợt mã":** API chưa có danh sách đợt; ô chỉ liệt kê các đợt đang thấy (đợt của các mã đang hiện, đợt đang lọc, đợt vừa tạo).
- **Ô lọc trạng thái:** máy chủ chỉ lọc `active`/`disabled`; chữ chọn là "Đang bật" (gồm cả mã hết hạn, hết lượt) thay "Đang chạy" của mockup để không hứa sai. Chip trên từng dòng vẫn phân Đang chạy / Hết lượt / Hết hạn / Chưa tới ngày / Đã tắt.
- **Mã gắn khách:** API chỉ cho biết `bound` (không phân khách hay SĐT, không trả SĐT) → chữ "Dành riêng một khách" thay "Dành cho một SĐT".
- **Màn Cài đặt đơn** chỉ hiện với người có `order.settings.manage` (+ `order.read`), vì đây là màn để sửa; người chỉ có order.read không thấy mục menu. Phương án khác: cho xem chỉ đọc.
- **Nguồn hệ thống** (có `key`, ví dụ website) không có nút Tắt theo mockup, dù API cho tắt.
- **Ô ngày giờ** dùng `datetime-local` của trình duyệt, đổi theo giờ Việt Nam cố định (không theo múi của máy); cách hiện ngày (dd/mm hay mm/dd) theo ngôn ngữ trình duyệt.
- **Đợt mã** không có ô tổng lượt, lượt mỗi khách, trả lượt (mỗi mã dùng một lần); không làm đợt mã gắn SĐT (`boundPhones`) vì mockup không vẽ.

## Cần chủ shop xem

- "Sửa lần cuối: người · giờ" của mockup Cài đặt đơn **chưa hiện**: `OrderSettings` của API không trả người và lúc sửa (bảng có `updated_by`). Cần thêm trường vào API ở task sau nếu muốn.
- API không cho **bỏ một mức đã đặt** (giảm tối đa, tổng lượt, lượt mỗi khách về "không giới hạn"): màn báo "Mã đã đặt mức này, không bỏ trống được".
- Đổi thứ tự nguồn đơn là nhiều PATCH riêng (API không có lệnh sắp xếp cả danh sách): lỗi giữa chừng thì dừng, báo và đọc lại; thứ tự có thể dở dang, bấm lại để sửa.

## Việc phát hiện thêm

- API `OrderSettings` chưa trả người/lúc sửa gần nhất (mockup cai-dat-don trạng thái A có dòng này).
- API mã giảm giá chưa cho bỏ một mức đã đặt (giảm tối đa, tổng lượt, lượt mỗi khách), chưa có danh sách đợt mã, chưa có xuất .xlsx.
- `CouponDetail.tsx`: key `EditCard` nên là `id:version`; `isoToVnLocal` bỏ phần giây (ghi chú của reviewer).

## Reviewer

Kết luận: ĐẠT (lần 1, trên bản đã gộp main f7173a5).

- Phát hiện phải sửa: không có.
- Đủ 4 tiêu chí của Step 1, bảng bằng chứng khớp test thật (số test e2e, mục "Nguồn đơn", "Danh sách mã" đúng số); Ảnh hưởng khớp, nơi dùng NAV_GROUPS đọc theo danh sách; khachTestkit không còn tên cũ; không test bị tắt; mọi lệnh ghi qua useWriteAction; quyền route khớp x-permission.
- Reviewer đối chiếu thêm: PATCH nối nhau khi đổi thứ tự nguồn an toàn (không có unique trên position, migration 0204); đổi mã % sang số tiền không gửi maxDiscount là vô hại (evaluate.go bỏ qua MaxDiscount với mã số tiền).
- Ý của Interface chưa làm được: "người và lúc sửa gần nhất" của Cài đặt đơn — API không trả; đã ghi ở "Cần chủ shop xem".
- Ghi chú không bắt buộc (chưa sửa, không ảnh hưởng đường đi hiện có): `EditCard` nên dùng key `id:version` thay vì `version`; ô ngày giờ cắt phần giây nên lưu mã có giây khác 0 làm tròn xuống phút.
