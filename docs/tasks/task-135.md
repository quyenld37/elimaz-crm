# Task 135 — Danh sách vận hành — Cần gọi lại, Đơn kẹt, Cần xem, Giữ hàng quá hạn (M3-44)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 135, M3-44). Mockup đã chốt:
`docs/mockups/m3/van-hanh.html` (màn 4, trạng thái A–F), hành vi HV01, HV02. Loại việc (quy trình mục 3): A (chỉ màn hình, không đổi Go, hợp đồng hay SQL).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task chỉ thêm tệp trong `console/src/screens/ops-lists/`).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Dùng sẵn: `listCallbackTasks`, `recordCallbackOutcome`,
  `assignCallbackTask`, `listStuckShipments`, `listShipmentsForReview`, `resolveShipmentAnomaly`,
  `resolveShipmentCreate`, `listAgedReservations`, `syncShipment` (nút "Đồng bộ lại"), `getOrder` ("Hiện số", theo quyết
  định ở Task 110), `listStaff` (hộp "Giao lại"), `getMe` (quyền). Không thêm operation, không thêm mã lỗi.
- Chỗ dùng trên màn hình: thư mục mới `console/src/screens/ops-lists/` (route `/ops/callbacks`, `/ops/stuck`,
  `/ops/review`, `/ops/aged-holds`, nhóm menu "Vận chuyển" có sẵn); số đếm trên menu (`badge` + `NavCount` có sẵn) cho
  Cần gọi lại, Đơn kẹt, Cần xem. Dùng lại (không sửa) `screens/staff` (`Dialog`), `screens/len-don/AddressFields`,
  `screens/shipping/shipmentRules` (`statusText`, `syncErrorText`, `toVnLocal`, `fromVnLocal`). Hiện không màn nào khác
  dùng 8 operation trên (grep: 0). `console/src/app/screens.test.tsx` không ghim nhãn nhóm "Vận chuyển" nên không cần sửa (kiểm quy ước route, quyền, order không trùng đều qua); thêm `ops-lists/route.test.tsx` ghim 4 route. Còn dùng `listRoles` (qua `useStaffData`), `listWarehouses` (tên kho, chỉ khi có `inventory.read`).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run src/screens/ops-lists src/app` (đúng thư mục màn + khung), `npx tsc --noEmit`,
  `npx eslint src/screens/ops-lists src/app`; cuối cùng `scripts/agent/ci-task.sh` một lần. Không chạy Playwright/e2e.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục console/src/shared
  (`useWriteAction`, `unwrap`, `errorMessage`, `queryKey`, `NavCount`, `hasPermission`); `screens/cod/Paged.tsx` (danh sách
  theo trang), `screens/cod/codTestkit.tsx` (API giả), `screens/live/LiveCommentList.tsx` (danh sách ảo
  `@tanstack/react-virtual` + `fakeLayout` của test), `screens/order-requests/RequestNavCount.tsx` (số đếm menu),
  `screens/shipping/AddressChangeDialog.tsx` (ô địa chỉ + tỉnh/xã), `screens/shipping/shipmentRules.ts`.
- Dùng lại / viết mới vì:
  - Dùng lại `useWriteAction`, `unwrap`, `NavCount`, `Dialog`, `DialogActions`/`DialogNotes`, `ProvinceField`/`WardField`,
    `statusText`, `syncErrorText`, `toVnLocal`/`fromVnLocal`, `renderScreens`/`reply`/`bodyOf`/`page`/`stubApi` của
    `app/screenTestkit`, các lớp CSS sẵn (`tbl`, `chip`, `box`, `dlg`, `field`, `btn`).
  - Viết mới `ops-lists/opsRules.ts` (nhãn quá hạn "Quá hạn 3 giờ", nhãn kết quả cuộc gọi, nhãn loại "Cần xem", `tel:`):
    riêng của màn này. Không dùng lại `cod/Paged.tsx` (là bảng `<table>`, còn "Cần gọi lại" cần danh sách ảo dạng
    dòng) — `usePaged` của COD lấy ra dùng lại cho Đơn kẹt, Cần xem, Giữ hàng quá hạn bằng cách nhập từ `../cod/Paged`
    sẽ kéo phụ thuộc chéo màn → viết lại ngắn `useOpsPaged` trong `opsData.ts` (khác chủ đề realtime, khác khóa).
  - Tên hãng: dùng `carrierName` của `shipping/QuoteTable` (có hàm dự phòng), không thêm bảng thứ ba.

## Soát thiết kế

5.1–5.12 (khớp kiến trúc): không áp dụng cho Go (task chỉ có màn hình). Riêng 5.9 (đường ghi có khóa chống trùng và
quyền): mọi nút ghi đi qua `useWriteAction` (khóa `Idempotency-Key` giữ nguyên khi gửi lại); nút ẩn/mờ theo quyền
(`fulfillment.callbacks.work`, `fulfillment.correct`, `fulfillment.ship`, `identity.users.manage`), API vẫn là nơi chặn.
5.11: không thêm thư viện (đã có `@tanstack/react-virtual`).
6.1 (hai người sửa cùng việc): ghi kết quả gửi `version` đã xem; 409 → câu "việc vừa đổi — đã tải lại" và tải lại danh sách.
6.2 (bấm hai lần): `useWriteAction` nhận lại lời hứa đang chạy; nút khóa khi đang gửi.
6.12 (dữ liệu cá nhân): danh sách không có SĐT; SĐT chỉ đọc qua `getOrder` khi bấm "Hiện số", giữ trong state của dòng,
không vào URL, log, khóa truy vấn dùng chung (khóa riêng không cache lâu: `gcTime` 0); ghi chú tự do không đưa vào
nhãn. Phần tử `tel:` chỉ dựng sau khi hiện số. 6.13 (quyền): mặc định từ chối; trang vắng quyền đọc không mở được (menu ẩn,
API 403 hiện câu lỗi, không vẽ như rỗng). 6.3–6.11: không áp dụng (không database, hàng đợi, tiền, tồn).
Hành vi màn: lúc đang tải / lỗi 403 / rỗng là ba trạng thái riêng; khóa mọi nút ghi khi một lần ghi đang chạy; còn
`nextCursor` thì chưa coi là hết.

## Quyết định thay chủ shop

- **QĐ-135-a — "Giao lại" chỉ cho người có `identity.users.manage`; còn lại chỉ "Nhận việc" (giao cho chính mình).**
  Vì `listStaff` đòi quyền đó, và spec không có operation liệt kê người có quyền gọi lại. Hộp chỉ liệt kê nhân viên
  đang hoạt động có quyền `fulfillment.callbacks.work` trong `grants`. Mockup có "đang có N việc" cạnh tên: hợp đồng
  không có số này → bỏ chữ đó. Phương án khác: thêm operation riêng (đổi hợp đồng, ngoài phạm vi). Dễ đảo ngược.
- **QĐ-135-b — "Hiện số" đọc `getOrder` (quyền `order.read`)** đúng quyết định ghi ở Task 110. Không có `order.read` thì
  nút "Hiện số" hiện "Không có quyền xem số" (mờ). Phương án khác: operation riêng (chưa có).
- **QĐ-135-c — Số đếm "Giữ hàng quá hạn" không có trên menu và ở thanh bốn tab** (hợp đồng không có số này, kế hoạch đã
  ghi); thẻ tab này không có số. Ba số kia lấy từ `counts` của `listShipmentsForReview?limit=1`.
- **QĐ-135-d — Lọc loại ở "Cần xem" làm ở trình duyệt** trên các việc đã tải (API không có tham số loại); số cạnh mỗi loại
  là số trong phần đã tải, ghi rõ "đang hiện N". Phương án khác: thêm tham số vào API (ngoài phạm vi).
- **QĐ-135-e — Nút xử lý "Cần xem" theo loại:** `in_doubt_create` → hai nút "Đã tạo"/"Chưa tạo" (`resolveShipmentCreate`;
  "Đã tạo" đòi mã hãng); loại khác → "Đã xử lý" (hộp ghi cách xử lý, `resolveShipmentAnomaly`) và "Mở đơn". "Ghi giao
  một phần" mở đơn (`/don/:id`, nơi có sẵn khối Vận chuyển): màn này không dựng lại hộp giao một phần (ngoài phạm vi).
  `resolveShipmentAnomaly` trả 409 cho `manual_cancel`, `in_doubt_cancel` → hiện câu lỗi và gợi ý "Mở đơn".
- **QĐ-135-f — Tự tải lại:** theo báo hiệu `callback_task.changed`, `shipment.changed` (khóa đầu là chủ đề) và lưới
  an toàn 20 s của lớp realtime; sau mỗi lần ghi tự tải lại (đóng anomaly không phát `shipment.changed`, Task 111).

- **QĐ-135-g — "Chưa ai nhận" lọc ở trình duyệt** trên các việc đã tải (API chỉ có `assignee=me|any`); "Của tôi" gọi API
  `assignee=me`. Chọn trạng thái "Đang mở" = không gửi `status` (máy chủ mặc định lấy việc mở và đang xử lý), để dòng vừa
  ghi "Không nghe máy" vẫn còn trong danh sách với nhãn "Đang xử lý · gọi lại 15:00".

## E2E viết sẵn, chưa chạy

(không có: kế hoạch chỉ đòi vitest cho task này; không viết Playwright)

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Quá hạn đứng đầu, nhãn "Quá hạn 3 giờ"; tên dài, nhãn Hay hoàn | `CallbacksPage.test.tsx` "việc quá hạn đứng đầu…" | `cd console && npx vitest run src/screens/ops-lists` | 4 file, 28 test qua |
| 200 việc: dựng < 30 dòng, cuộn tải trang kế bằng con trỏ | `CallbacksPage.test.tsx` "200 việc…" | như trên | qua |
| SĐT chỉ hiện khi bấm "Hiện số", `tel:` | `CallbacksPage.test.tsx` "SĐT không có sẵn…", "thiếu quyền xem đơn…" | như trên | qua |
| "Không nghe máy" hẹn 15:00 → dòng "Đang xử lý · gọi lại 15:00"; version, khóa chống trùng, giờ 08:00Z | `CallbacksPage.test.tsx` "ghi 'Không nghe máy'…" | như trên | qua |
| Hẹn lại đòi giờ; địa chỉ mới đòi địa chỉ/tỉnh/xã; 409 version_conflict giữ hộp + tải lại | `CallbacksPage.test.tsx` "'Khách hẹn gọi lại' đòi giờ…", "409 version_conflict…" | như trên | qua |
| Nhận việc (assign cho mình + version); Giao lại chỉ cho người quản lý, chỉ nhân viên có quyền gọi lại | `CallbacksPage.test.tsx` "'Nhận việc'…", "'Giao lại' chỉ liệt kê…" | như trên | qua |
| Lỗi 403 ≠ rỗng; rỗng; số trên thanh tab theo báo hiệu | `CallbacksPage.test.tsx` "lỗi 403…", "còn đúng 1 việc…" | như trên | qua |
| Đơn kẹt: giờ, lần hỏi hãng, Đồng bộ lại, đang gửi hãng | `OtherLists.test.tsx` "Đơn kẹt" (2 ca) | như trên | qua |
| Cần xem: câu hướng dẫn theo loại, lọc, Đã tạo/Chưa tạo/Đã xử lý, 409, thiếu quyền | `OtherLists.test.tsx` "Cần xem" (4 ca) | như trên | qua |
| Giữ hàng quá hạn: không nút nhả hàng, chỉ Mở đơn; lọc; rỗng | `OtherLists.test.tsx` "Giữ hàng quá hạn" (2 ca) | như trên | qua |
| Số đếm menu cập nhật (12 · 3 quá hạn đỏ, 4, 6; 0 không vẽ) | `OtherLists.test.tsx` "Số đếm cạnh mục menu"; `route.test.tsx` | như trên | qua |
| Quy tắc thuần (nhãn quá hạn, tel:, loại) | `opsRules.test.ts` | như trên | qua; đột biến đổi chữ "Quá hạn" làm 2 test đỏ |
| Khung màn (quy ước route, quyền thuộc docs/quyen.md, order không trùng) | `src/app/screens.test.tsx` | `npx vitest run src/screens/ops-lists src/app src/shell` | 12 file, 145 test qua |
| Kiểu và lint | — | `npx tsc --noEmit`; `npx eslint src/screens/ops-lists src/app` | không lỗi |
| Cổng tĩnh + test console ở bể (jscpd, lint, kiến trúc) | — | ci-task (bể github) | `CI-TASK XANH`, run 46055775129 (lần 1 đỏ jscpd 7 clone → gộp Seg/MoreBar, rút gọn testkit) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Không so ảnh với mockup (Playwright bị cấm trong lúc làm task): dựng theo cấu trúc mockup, chưa chụp so pixel; để dành khi chạy e2e cuối mốc.
- Thứ tự làm: bản nháp mã màn viết trước, test viết ngay sau và qua ngay lần đầu; đã kiểm test thật sự bắt lỗi bằng một đột biến (đổi chữ nhãn quá hạn → 2 test đỏ, rồi hoàn lại).
- Không có ngưỡng giờ cạnh "80 giờ" (mockup ghi "ngưỡng 72"): hợp đồng `StuckShipment` không có ngưỡng.

## Việc phát hiện thêm

- (Reviewer, không chặn) Sau 409 `version_conflict`, hộp "Ghi kết quả"/"Giao lại" vẫn giữ `version` cũ nên bấm lưu lần nữa lại 409 tới khi đóng-mở lại hộp; dữ liệu an toàn (máy chủ chặn). Cách sửa gọn: đóng hộp khi 409. Chưa sửa (ngoài phạm vi review ĐẠT, tránh vòng review mới).
- (Reviewer, không chặn) Chưa có test kiểm thân `newAddress` gửi đi (người nhận, SĐT lấy từ đơn, tỉnh, xã) — chỉ có test chặn nút lưu khi thiếu địa chỉ.
- QĐ-135-e: với 409 của `manual_cancel`/`in_doubt_cancel` hộp chỉ hiện câu lỗi chung; nút "Mở đơn" có sẵn trên dòng.

## Reviewer

Kết luận: ĐẠT (reviewer, 28/11). Không có phát hiện phải sửa. Đã đối chiếu: SĐT chỉ ở state của dòng, mọi lệnh ghi đi qua
`useWriteAction`, ba trạng thái tải/lỗi/rỗng tách riêng, danh sách ảo, dừng phân trang theo `nextCursor`, quyền nút khớp
`openapi.yaml`, "28 test" khớp số ca thật. Reviewer không chạy được test (hook chặn); số test qua dựa vào ci-task xanh.
