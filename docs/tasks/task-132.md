# Task 132 — M3-41: Màn Kết nối vận chuyển và cài đặt M3

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 132, M3-41). Loại việc (quy trình mục 3): A

Mockup đã chốt: `docs/mockups/m3/ket-noi-van-chuyen.html` (trạng thái A–F), khung `docs/mockups/m3/khung.js`. Hành vi
HV01–HV06 (chủ shop OK 21/11 trong chat, ghi vào `docs/mockups/m3/README.md` cùng HV07–HV16).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu nào có sẵn. Thêm phương thức
  `(*handler).GetWarehousePickup` (hiện thực interface ogen `gen.Handler` sinh lại, 1 chỗ gọi: router sinh). Dùng lại
  `inventory.GetWarehousePickup` (đã có, 1 chỗ gọi hiện tại: `handlers_warehouse_pickup.go`; thêm 1 chỗ).
- Gói phụ thuộc ngược (go list): `internal/httpapi` ← `cmd/crm`. Không chạm module nghiệp vụ.
- Operation OpenAPI bị chạm, oasdiff: **thêm** `getWarehousePickup` (GET `/api/v1/inventory/warehouses/{id}/pickup`,
  `x-permission: fulfillment.settings.manage`, trả `WarehousePickup` có sẵn) — thêm, không phá vỡ. Không đổi operation
  nào khác. Màn dùng: `listCarrierAccounts`, `createCarrierAccount`, `updateCarrierAccount`, `putCarrierCredentials`,
  `checkCarrierAccount`, `setCarrierAccountStatus`, `getWarehousePickup` (mới), `setWarehousePickup`,
  `syncCarrierLocations`, `listUnmappedLocations`, `getFulfillmentSettings`, `updateFulfillmentSettings`,
  `listWarehouses`, `listAdminUnits`, `getMe`.
- Chỗ dùng trên màn hình: thư mục mới `console/src/screens/carriers/` (đường `/settings/carriers`,
  `/settings/carriers/:id`, `/settings/fulfillment`); khung: `console/src/app/nav.ts` (thêm nhóm "Vận chuyển", "Tiền"
  sau "Đơn hàng" — HV01; trường tùy chọn `badge` của Screen cho số đếm — HV02), `console/src/app/loadScreens.ts` (kiểm
  `badge`), `console/src/shell/Layout.tsx` (vẽ số đếm), `console/src/shell/NavCount.tsx` (mới, xuất ở
  `console/src/shared/index.ts`, danh mục `docs/danh-muc-dung-chung.md` sinh lại), `console/src/styles.css` (`.nav-n`,
  `.sr-only`). `console/src/screens/len-don/AddressFields.tsx`: chỉ thêm `export` cho `WardField` (1 chỗ dùng mới:
  `carriers/PickupPanel.tsx`). `docs/mockups/m3/README.md`: cột OK của HV01–HV16. Client sinh
  `console/src/api/**` sinh lại (thêm `getWarehousePickup`). Trang chi tiết đơn (Task 133) không chạm.
- Câu sqlc, bảng, pha migration: không có (handler đọc bằng hàm có sẵn của inventory, câu sqlc `GetWarehousePickup` có sẵn).
- Test sẽ chạy: `go test -race ./internal/httpapi/ -run 'TestWarehousePickupAPI|TestErrorsSnapshot|TestRoutes'`,
  `cd console && npx vitest run src/screens/carriers src/app src/shell`, `npx tsc --noEmit`, `npx eslint .`;
  cuối cùng `scripts/agent/ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (phần console/src/shared:
  `useWriteAction`, `unwrap`, `errorMessage`, `ApiError`, `queryKey`, `shortTime`); màn có sẵn: `screens/staff/staffData`
  (`Dialog`, `RefusalBox`), `screens/don/orderData` (`usePerms`), `screens/warehouse/warehouseRules` (`carrierName`,
  `SELF_CARRIER`), `screens/ma-giam-gia/couponRules` (`percentOrZero`, `percentInput`: đổi % ↔ điểm cơ bản),
  `screens/len-don/AddressFields` (danh mục tỉnh/xã `listAdminUnits`), `app/screenTestkit` (`renderScreens`, `reply`,
  `bodyOf`), `don/donTestkit` (`stubDialog`).
- Dùng lại / viết mới vì: dùng lại tất cả các mục trên. Viết mới: `carrierRules.ts` (nhãn trạng thái tiếng Việt, ô khóa
  theo hãng, đọc/ghi các ô cài đặt vận chuyển) vì là luật riêng của màn này; CSS `carriers.css` chép đúng các lớp m3.css
  của mockup mà console chưa có (`.unit`, `.copy`, `.set-g`) cùng `.acts/.act`, `.radios` (đang nằm trong `don.css`, chỉ
  nạp ở màn Đơn) — giữ riêng theo màn vì gói CSS của màn khác chỉ nạp khi màn đó mở; `.nav-n` vào `styles.css` (khung).

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1–5.3: không áp dụng cho phần màn hình; operation đọc mới đọc dữ liệu inventory qua public API của inventory
  (`inventory.GetWarehousePickup`), không đọc bảng của module khác.
- 5.4–5.6: không áp dụng: chỉ thêm một đường đọc; kiểm kết nối, đồng bộ địa giới đã là việc nền (`commands`) ở máy chủ.
- 5.7: đường đọc mới chạy trong transaction tenant (`catalogRead`, như `GetFulfillmentSettings`); kho tenant khác → 404.
- 5.8: handler mỏng ở `internal/httpapi`, module không nhập HTTP.
- 5.9: đường mới là GET (không cần khóa chống trùng), có `x-permission`. Mọi lệnh ghi của màn đi qua `useWriteAction`
  (khóa `Idempotency-Key` sinh khi bấm).
- 5.10: không áp dụng (không Valkey).
- 5.11: không thêm thư viện.
- 5.12: màn theo mẫu Cài đặt đơn (Task 87) và Kho (Task 136): `route.tsx` tự đăng ký, dữ liệu TanStack Query theo chủ
  đề realtime, test vitest với API giả.

Mục 6 (đúng khi chạy đồng thời):
- 6.1: sửa tài khoản, cài đặt, địa chỉ lấy hàng gửi `version` đang xem; 409 `version_conflict` → câu "vừa được người
  khác sửa — tải lại" kèm nút Tải lại; không tự ghi đè. PATCH chỉ gửi trường người dùng đổi (Bẫy Task 85).
- 6.2: mọi lệnh ghi mang khóa chống trùng; nút khóa khi đang gửi.
- 6.3–6.9: không áp dụng (không khóa, không việc nền mới).
- 6.10: không áp dụng (không tiền trên màn này).
- 6.11: không áp dụng (không vùng rủi ro cao).
- 6.12: khóa API: ô mật khẩu `autocomplete="new-password"`, không vào `localStorage`, xóa khỏi state của form ngay sau
  khi gửi thành công (HV04); khóa không bao giờ hiện lại (API không trả). SĐT kho không phải dữ liệu khách; không log.
- 6.13: nút sửa, URL webhook, thay khóa chỉ hiện với `fulfillment.carrier_accounts.manage`; API vẫn kiểm (URL webhook
  máy chủ chỉ trả cho người có quyền manage). Đường đọc mới đòi `fulfillment.settings.manage` (cùng quyền với ghi).

## Quyết định thay chủ shop

1. **Thêm operation đọc `getWarehousePickup`.** Kế hoạch chỉ có `setWarehousePickup`, mà lệnh ghi đòi `version` và màn
   cần hiện địa chỉ đang lưu (mockup C). Không có đường đọc thì ô luôn trống và lần lưu nào cũng 409. Chọn: thêm GET cùng
   path, cùng quyền `fulfillment.settings.manage`, trả đúng schema `WarehousePickup` có sẵn (thay đổi thêm, không phá vỡ;
   dễ bỏ). Phương án khác: thêm địa chỉ vào `Warehouse` của `listWarehouses` (lộ SĐT kho cho mọi người có `inventory.read`).
2. **Trang sửa `/settings/carriers/:id` đọc tài khoản từ `listCarrierAccounts`** (không có `getCarrierAccount`; số tài
   khoản của shop nhỏ, đọc hết các trang rồi tìm theo mã). Phương án khác: thêm operation đọc một tài khoản — để lại.
3. **Ô khóa theo hãng:** hợp đồng để `CarrierCredentials` là map tự do (Task 131 để lại). Màn khai ô theo hãng: `vtp`
   `username`/`password` (khớp `internal/adapters/carrier/vtp/auth.go`); hãng khác chưa có adapter thật nên một ô "Khóa
   API (token)" khóa `token` (khớp hãng giả dùng ở dev). Chủ shop tạm dừng tích hợp API: "Nối API của hãng" vẫn chọn
   được, máy chủ trả 422 `capability_not_supported` → hộp báo rõ "Hãng này chưa nối API trong CRM — chọn Nhập mã vận
   đơn tay". Mặc định chọn "Nhập mã vận đơn tay" cho mọi hãng (chạy được ở production, Task 448).
4. **Số đếm trên menu (HV02):** các màn có số (Cần gọi lại, Đơn kẹt, …) thuộc Task 135, 137 chưa có. Task này làm chỗ
   cắm: `Screen.badge` (phần tử React do màn tự khai, tự lấy số và tự nghe báo hiệu) và kiểu `.nav-n`/`.is-late`; khung
   vẽ cạnh tên mục. Phương án khác: khung tự gọi API đếm — gắn khung với API của màn khác, bỏ.
5. **Nhóm "Tiền" trong `NAV_GROUPS`:** thêm sẵn cùng "Vận chuyển" (HV01); nhóm không có màn thì không hiện.
6. **"Đồng bộ lần cuối" của địa giới** không có trong API (Task 131 để lại): thẻ địa giới hiện số mục chưa khớp từ
   `listUnmappedLocations` theo hãng, không hiện mốc đồng bộ.
7. **Ô "Đang giao quá hạn" (agedDays.inTransit) và năm ô đơn kẹt** hiện như mockup F; chu kỳ tra (`pollMinutes`) không
   hiện, gửi lại nguyên giá trị đang lưu (HV06).

8. **Bỏ lựa chọn "Hãng khác (chỉ nhập mã tay)" của mockup B:** máy chủ chỉ nhận tài khoản nhập tay của self, vtp, best,
   spx (`carrier.ManualKeys()`, Task 448); hãng khác sẽ bị 422. Thêm lại khi danh mục hãng có hãng mới.
9. **"Thu hồi" có hộp xác nhận "Thu hồi hẳn"** (mockup chỉ vẽ nút): không đảo ngược được, nên hỏi lại một lần.
10. **Hộp Thêm mặc định "Nhập mã vận đơn tay"**; "Lưu và kiểm tra kết nối" chỉ bật khi có khóa. Tài khoản `auth_failed`
    mở hộp Thay khóa ngay từ dòng của danh sách.
11. **`WardField` của màn Lên đơn được xuất ra** (thêm `export`, không đổi hành vi) để thẻ địa chỉ lấy hàng dùng lại ô
    xã/phường.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Danh sách 5 tài khoản (VTP 1, Best 3, SPX 1) đúng trạng thái, nhãn tiếng Việt | `CarrierAccountsPage.test.tsx` "5 tài khoản (VTP 1, Best 3, SPX 1)…"; `carrierRules.test.ts` "trạng thái tài khoản" (8 ca) | `cd console && npx vitest run src/screens/carriers` | `Tests 53 passed (53)` |
| `auth_failed` → "Cần cấp quyền lại" và nút "Thay khóa" (VC15) | `CarrierAccountsPage.test.tsx` "auth_failed: …", "Thay khóa từ dòng…"; `CarrierAccountDetail.test.tsx` "auth_failed: hộp đỏ…" | như trên | qua |
| Thêm tài khoản gửi đúng thân; ô khóa trống sau khi lưu; không localStorage (VC01, HV04) | `CarrierAccountsPage.test.tsx` "mặc định Nhập mã vận đơn tay…", "Nối API Viettel Post: …ô khóa trống sau khi lưu" | như trên | qua |
| Hãng chưa nối API → 422 báo rõ (chủ shop tạm dừng tích hợp) | `CarrierAccountsPage.test.tsx` "hãng chưa nối API → 422 capability_not_supported…" | như trên | qua |
| "Kiểm tra kết nối" → "Đang kiểm tra" rồi cập nhật khi nhận báo hiệu | `CarrierAccountDetail.test.tsx` "Kiểm tra kết nối → 'Đang kiểm tra' rồi cập nhật khi nhận báo hiệu fulfillment.carrier_account" | như trên | qua |
| Chỉ quyền `.read`: không nút sửa, không URL webhook | `CarrierAccountsPage.test.tsx` "chỉ quyền xem…"; `CarrierAccountDetail.test.tsx` "không nút sửa, thay khóa, kiểm tra; không URL webhook; câu quyền" | như trên | qua |
| `returnRisk.highRateBp`: "30%" gửi 3000, "0" lỗi tại ô (quyền `fulfillment.settings.manage`) | `FulfillmentSettingsPage.test.tsx` 'nhập "30%" gửi highRateBp 3000…', 'nhập "0" ở tỷ lệ → lỗi tại ô…'; `route.test.tsx` (quyền) | như trên | qua |
| Sửa chỉ gửi trường đổi kèm version; 409 tải lại | `CarrierAccountDetail.test.tsx` "chỉ gửi trường đã đổi…", "409 version_conflict…"; `FulfillmentSettingsPage.test.tsx` "409 version_conflict → câu tải lại" | như trên | qua |
| Tên 60 ký tự không tràn ở 1280 (và 1512, 390); không cuộn ngang | `e2e/carriers.spec.ts` (3 khổ × 3 trang; `inside`, `noHorizontalScroll`) | `CRM_E2E_PORT=5232 npx playwright test e2e/carriers.spec.ts e2e/shell.spec.ts --project=chromium` | `13 passed (5.7s)` |
| So ảnh mockup 1512/390 | `console/test-results/ket-noi-{danh-sach,them,sua,cai-dat}-{1512,1280,390}.png` so với `docs/mockups/m3/png/ket-noi-van-chuyen-{1512,390}.png` | xem bằng mắt | Danh sách khớp từng cột, chip, màu dòng lỗi/thu hồi, nút; trang sửa, cài đặt theo bố cục trạng thái C, F |
| HV01, HV02: nhóm "Vận chuyển", "Tiền" sau "Đơn hàng"; số đếm, quá hạn nền đỏ | `Layout.test.tsx` "nhóm Vận chuyển, Tiền và số đếm…" (2 ca); `screens.test.tsx` (badge sai kiểu, giữ badge) | `npx vitest run src/screens/carriers src/app src/shell` | `Tests 165 passed (165)` |
| Đọc địa chỉ lấy hàng (operation mới) | `carrier_accounts_test.go` `TestWarehousePickupAPI_Get` (403, chưa ghi, sau ghi, 404) | `go test -race ./internal/httpapi/ -run TestWarehousePickupAPI` | `ok elimaz.com.vn/crm/internal/httpapi 2.165s` |
| Cổng của task (ci-task) | toàn bộ cổng | lần 1 (trước khi stage) | test Go xanh (httpapi 75 s, cmd/crm, realtime, arch, e2e); đỏ gen-check, catalog-check (chưa stage) và jscpd 8 đoạn trùng → đã gộp (`QueryState`, `AccountHead`, `openAdd`, testkit). Lần 2 sau khi stage: dòng dưới |
| Cổng của task, lần 2 (đã stage, đã gộp main 13306a3) | toàn bộ cổng | ci-task | `CI-TASK XANH (so với điểm tách nhánh 13306a3f…)`; `ok …/tests/arch 12.735s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm operation `getWarehousePickup` (Quyết định 1).
- Tệp thêm ngoài danh sách kế hoạch: `CarrierAccountDetail.tsx` (trang sửa), `PickupPanel.tsx`, `QueryState.tsx`,
  `carrierRules.ts`, `carrierData.ts`, `carriersTestkit.tsx`, `carriers.css`, `e2e/carriers.spec.ts`; khung:
  `shell/NavCount.tsx` (số đếm menu, HV02).

## Việc phát hiện thêm

- Việc nền đồng bộ địa giới (`syncCarrierLocations`) xong không phát báo hiệu: danh sách "chưa khớp" chỉ tự cập nhật nhờ
  lưới an toàn 20 s của realtime. Nên phát `fulfillment.carrier_account` khi đồng bộ xong.
- Lệnh kiểm kết nối không gửi được hãng (hết giờ, 503) không đổi tài khoản: màn giữ "Đang kiểm tra" tới khi có kết quả;
  API không cho đọc "lệnh kiểm đang chờ / đã dead". Câu "Không hỏi được hãng (hết giờ chờ)" của mockup B chưa hiện được.
- Không có `getCarrierAccount`, không có mốc "đồng bộ địa giới lần cuối", người và giờ đổi khóa (mockup C "cập nhật 02/11
  bởi …"): chưa hiện.

## Reviewer

Kết luận: ĐẠT (lần 1, agent `reviewer` trên phần đã stage sau khi gộp main 13306a3).

- Phát hiện phải sửa: không có.
- Ảnh hưởng dự kiến khớp thực tế (handler mới, mã sinh, khung menu, `NavCount`, `WardField`, README); `e2e/carriers.spec.ts`
  nêu ở "Khác kế hoạch".
- Bảng bằng chứng: mọi dòng trỏ tới test có thật và kiểm đúng tiêu chí; không test nào bị xóa, tắt, nới.
- Ghi chú không bắt buộc (không sửa, ảnh hưởng trải nghiệm chứ không tính đúng): ô tỉnh của địa chỉ lấy hàng trống khi
  vai trò tùy chỉnh thiếu `customer.read`; "Lưu và kiểm tra" bật khi có một ô khóa (lần kiểm sẽ báo không đạt); thẻ sửa
  dựng lại khi báo hiệu tăng version làm mất phần đang gõ dở (an toàn, không ghi đè); chú thích `CARRIER_CHOICES` còn nhắc
  "other".
