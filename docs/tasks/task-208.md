# Task 208 — Màn Khóa dịch vụ và phần Hãng trên website

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 208, M5-25). Loại việc (quy trình mục 3): A (chỉ màn hình)

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ sửa `console/`, không chạm mã Go.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn dùng client sinh có sẵn: khóa dịch vụ (Task 189) `listServiceKeys`, `createServiceKey` (`x-idempotency: no-replay`, đọc `secret` của 201), `revokeServiceKey`; hãng trên web (Task 198, M3) `listCarrierAccounts`, `updateCarrierAccount` (`showOnWeb`, `webSort`), `getWebCarrierSettings` (kèm `carriers[].shipments30d`), `saveWebCarrierSettings`. Mã lỗi đã có câu trong `app/errors.ts`: `already_processed`, `version_conflict`, `carrier_not_on_web`, `last_web_carrier`, `setting_out_of_range`, `permission_not_allowed_for_key`, `invalid_input`. oasdiff không chênh.
- Chỗ dùng trên màn hình:
  - `console/src/app/nav.ts`: `NAV_GROUPS` thêm nhóm "Website" giữa "Khuyến mãi" và "Sản phẩm" (kế hoạch mục "Xếp module", HV01). Task 462 (chưa làm) **dùng lại nhóm này, không thêm lần hai** (ghi chú trong `nav.ts`). `app/screens.test.tsx` chỉ kiểm quy ước và thứ tự theo `NAV_GROUPS` → không phải sửa; thêm một ca kiểm thứ tự nhóm.
  - Thư mục mới `console/src/screens/service-keys/` (`route.tsx` tự đăng ký, `/website/khoa-dich-vu`, quyền `identity.service_keys.manage`, order 50 trong nhóm Website).
  - Thư mục mới `console/src/screens/web-carriers/`: `WebCarriers.tsx` là thẻ "Hãng trên website" gắn ở cuối màn Kết nối vận chuyển (`carriers/CarrierAccountsPage.tsx` sửa: vẽ thẻ khi có tài khoản; nhận cờ cuộn tới thẻ); `route.tsx` khai mục menu "Hãng trên web" (`/website/hang-tren-web`, nhóm Website, order 30) mở chính màn Kết nối vận chuyển và cuộn tới thẻ (kế hoạch: "mục menu trỏ tới phần Hãng trên website của màn Kết nối vận chuyển, không phải màn thứ hai").
  - `carriers/CarrierAccountsPage.test.tsx`: chạy lại (màn có thêm thẻ; API giả của `carriersTestkit` thêm đường `GET /api/v1/web-carrier-settings`).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: vitest `src/screens/service-keys/`, `src/screens/web-carriers/`, `src/screens/carriers/`, `src/app/screens.test.tsx`, `src/app/errors.test.ts`; `npx tsc --noEmit`; `npx eslint` các tệp sửa; `make jscpd`; `go test ./tests/arch/`; ci-task. Playwright và tests/e2e không chạy (chủ shop 24/11); kế hoạch Task 208 không có spec Playwright riêng ("so ảnh với mockup" để tới khi chạy Playwright).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ liệt kê mã Go (không áp dụng cho console). Trong console đã tìm: đường ghi chống trùng (`app/idempotency.ts` `useWriteAction` — tự gửi lại cùng khóa khi mạng lỗi, nên lần gửi lại `createServiceKey` nhận đúng 409 `already_processed`), hộp thoại (`staff/staffData.tsx` `Dialog`), quyền (`don/orderData.ts` `usePerms`), danh sách tài khoản hãng (`carriers/carrierData.ts` `useAccounts`, `accountsKey`), nhãn trạng thái và tên hãng (`carriers/carrierRules.ts` `statusChip`, `CARRIER_ACCOUNT_TOPIC`), tiền (`products/productRules.ts` `formatMoney`, `products/editRules.ts` `parseMoney`, `moneyInput`), giờ (`app/time.ts` `shortTime`), khóa truy vấn (`app/queryClient.ts` `queryKey`), bộ test (`app/screenTestkit.tsx` `stubApi`, `renderScreens`, `emptyWrites`, `reply`; `don/donTestkit.tsx` `stubDialog`; `carriers/carriersTestkit.tsx` `account`), chép vào bộ nhớ tạm (chỉ có `navigator.clipboard` gọi thẳng ở CarrierAccountDetail, không có hàm chung).
- Dùng lại / viết mới vì: dùng lại mọi thứ ở trên. Viết mới: `serviceKeyRules.ts` (nhãn chín quyền `web.*` theo `docs/quyen.md`, đọc ô IP, kiểm form — nghiệp vụ riêng của màn), `webCarrierRules.ts` (sắp hàng hiện, lý do không bật được, tính `webSort` khi đổi thứ tự, kiểm form ưu tiên/phí dự phòng); CSS riêng từ khối `<style>` của mockup (`.sk`, `.once`, `.copy`, `.perm`, `.olist`, `.oitem`, `.sw`) gắn tiền tố màn.

## Soát thiết kế

Task chỉ màn hình; mục 5, 6 của quy trình chỉ liên quan ở các câu sau, còn lại không áp dụng (không chạm database, hàng đợi, tồn; tiền chỉ là phí dự phòng nhập/hiện bằng số đồng `int64` máy chủ nhận).

- 5.9 Chống trùng: mọi lệnh ghi qua `useWriteAction` (khóa sinh khi bấm, giữ khi tự gửi lại); mã đối tượng và `version` nằm trong thân của `run`. Đổi thứ tự cần ghi `webSort` của nhiều tài khoản: một thao tác, mỗi tài khoản khóa `<khóa thao tác>-<i>` (khớp mẫu `^[A-Za-z0-9_-]{1,128}$`), gửi lại cùng thao tác dùng lại đúng các khóa.
- 5.12 Theo mẫu: cấu trúc như `web-campaigns` (Task 458): route.tsx, *Rules.ts thuần + test, *Testkit.tsx.
- 6.1 Hai người cùng sửa: `updateCarrierAccount` và `saveWebCarrierSettings` gửi `version` đã đọc; 409 `version_conflict` → hộp "Người khác vừa đổi phần này", tải lại, bỏ bản nháp (mockup C); `revokeServiceKey` 409 → "Khóa vừa được người khác đổi", tải lại danh sách (mockup D).
- 6.2 Bấm hai lần / ghi đang chạy: `useWriteAction` trả lại lời hứa đang chạy; khi một lần ghi của thẻ hãng đang chạy thì khóa mọi ô bật/tắt, nút ↑ ↓ và nút Lưu (Bẫy đã gặp: khóa mọi thao tác khi một lần ghi đang chạy).
- 6.13 Quyền: màn Khóa dịch vụ chỉ hiện với `identity.service_keys.manage`; thẻ hãng chỉ có nút ghi với `fulfillment.carrier_accounts.manage` (không có thì chỉ xem, câu lý do như mockup C); máy chủ vẫn kiểm.
- Bí mật của khóa (ADR-18): chỉ nằm trong state React của hộp "Đã tạo khóa"; đóng hộp là xóa; không vào `localStorage`/`sessionStorage`, không vào bộ đệm React Query, không `console.*` (test quét cả ba). 409 `already_processed` (gửi lại cùng khóa chống trùng) → hộp "Khóa đã được tạo ở lần bấm trước, không hiện lại được…".
- Luật của máy chủ (Task 198 + bổ sung 450) được vẽ trước ở màn, máy chủ vẫn kiểm: ẩn hãng active cuối cùng → màn chặn, ô giữ tích, câu "Phải hiện ít nhất 1 hãng" (máy chủ 422 `last_web_carrier`); ẩn hãng đang chọn sẵn → máy chủ tự bỏ chọn sẵn, màn báo; chọn sẵn chỉ được hãng active đang hiện (máy chủ 422 `carrier_not_on_web`); tài khoản không active không bật được (có câu lý do).
- Màn chỉ vẽ dữ liệu API có (luật Bẫy đã gặp): xem "Quyết định thay chủ shop".

## Quyết định thay chủ shop

1. **Mục menu "Hãng trên web":** kế hoạch nói đây là mục menu trỏ tới phần cuối màn Kết nối vận chuyển, không phải màn thứ hai. Menu chỉ nhận route, nên khai `/website/hang-tren-web` (nhóm Website) dựng **chính** `CarrierAccountsPage` với cờ `focusWeb` và cuộn tới thẻ "Hãng trên website"; `/settings/carriers` vẫn có thẻ đó ở cuối, không cuộn. Phương án khác: đường `/settings/carriers#web` (menu không khớp được mục), hay màn riêng (trái kế hoạch). Dễ đổi: một route.
2. **Nhóm menu "Website" tạo ở task này** (kế hoạch giao Task 462, chưa làm): thêm vào `NAV_GROUPS` sau "Khuyến mãi", ghi chú trong `nav.ts` để Task 462 dùng lại, không thêm lần hai. Order trong nhóm theo thứ tự HV01: Banner 10, Bộ phối 20, Hãng trên web 30, Nhận đơn web 40, Khóa dịch vụ 50.
3. **Mockup:** bảng "Kết luận duyệt" của `docs/mockups/m5/README.md` còn ghi "Chờ điều phối viên duyệt", nhưng HV09–HV13 đã có OK của chủ shop 21/11 và điều phối viên đã giao task (Task 207, 458 cùng tình trạng đã gộp). Làm theo mockup.
4. **Số vận đơn 30 ngày:** API (`getWebCarrierSettings.carriers`) chỉ trả số cho tài khoản đang hiện trên web. Dòng chưa hiện không ghi số (mockup ghi "SPX · 0 vận đơn"), không tự đếm ở màn. Ghi Việc phát hiện thêm.
5. **Đổi thứ tự (↑ ↓):** API chỉ sửa từng tài khoản. Một lần bấm gửi `webSort` mới (1, 2, 3…) cho đúng các tài khoản có số đổi, lần lượt, mỗi tài khoản một khóa con `<khóa>-<i>` của cùng thao tác (gửi lại dùng lại đúng khóa, máy chủ trả kết quả đã lưu). Lỗi giữa chừng → báo lỗi, tải lại, thứ tự có thể đổi một nửa (vẫn hợp lệ, khách thấy thứ tự đó). Phương án khác: API đổi thứ tự cả danh sách một transaction (ghi Việc phát hiện thêm).
6. **Nhắc "trước khi website dùng báo phí CRM"** (yêu cầu của điều phối viên): ô đầu thẻ nêu hai việc (bật hiện ít nhất một hãng đang hoạt động, nhập phí dự phòng) và "Còn thiếu: …"; viền đỏ khi còn thiếu, xanh khi đủ. Mockup ghi phí dự phòng "mặc định tắt" — giữ mặc định tắt, chỉ nhắc.
7. **Tài khoản không hoạt động mà vẫn đang hiện** (ví dụ vừa tạm ngừng): vẫn tắt được hiện trên web (máy chủ cho; luật "hãng cuối" chỉ đếm tài khoản active), chỉ không bật được.
8. **Quyền của khóa:** mockup vẽ 10 ô (có "Gửi góp ý" riêng); máy chủ có 9 quyền `web.*` (`identity.ServiceKeyPermissions`) — góp ý nằm trong `web.requests.write` (docs/quyen.md). Vẽ đúng 9 ô, ô đó ghi "Gửi yêu cầu về đơn hàng, góp ý". Tích sẵn mọi quyền trừ "Mua flash sale" như mockup.
9. **Cột Quyền:** từ 4 quyền trở lên ghi "n quyền website" (mockup "9 quyền website"), ít hơn thì tên từng quyền.
10. **Lần dùng cuối:** giờ tuyệt đối "08:14 · 21/11" (cùng cách viết của console) thay "2 phút trước" của mockup (một dạng cho mọi dòng).
11. **409 khi thu hồi / khi lưu hãng:** API không trả ai đổi, lúc nào → câu không nêu tên, giờ (mockup nêu "Trần Minh Thư, 11:20", "thu hồi lúc 10:05").
12. **Chỉ xem (thiếu `fulfillment.carrier_accounts.manage`):** vẽ cùng danh sách nhưng không ô bật, không nút thứ tự, kèm hãng chọn sẵn, phí dự phòng bằng chữ và câu lý do (mockup C vẽ gọn ba dòng chữ).
13. **0 tài khoản hoạt động:** câu trống không có nút "Thêm tài khoản" riêng (nút đó đã ở đầu màn Kết nối vận chuyển, cùng trang).
14. **Ô IP:** kiểm dạng ở màn (IPv4 `a.b.c.d/0–32`, IPv6 `…/0–128`, mỗi dòng một dải, nêu số dòng sai); máy chủ vẫn kiểm (`netip.ParsePrefix`, 400 `invalid_input`).

## Bằng chứng

Lệnh chung: `cd console && npx vitest run src/screens/service-keys src/screens/web-carriers src/screens/carriers src/app/screens.test.tsx` → `Test Files 12 passed (12)`, `Tests 136 passed (136)`. Test viết trước, chạy FAIL (thiếu module `./route`, `./serviceKeyRules`, `./webCarrierRules`, thiếu nhóm "Website" trong `NAV_GROUPS`) rồi mới viết mã.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tạo khóa → hộp hiện khóa một lần (ô chỉ đọc, nút Chép, lời nhắc); đóng là mất | `ServiceKeys.test.tsx` › tạo khóa › "mặc định tích 8 quyền (trừ Mua flash sale); gửi tên, quyền, IP; hộp hiện khóa một lần, Chép; đóng là mất" (sau khi đóng `document.body` không còn khóa) | lệnh chung | qua |
| Gửi lại cùng khóa chống trùng → 409 `already_processed` → hộp "khóa đã tạo, không hiện lại được; thu hồi và tạo khóa mới" | › "mạng rớt, gửi lại cùng khóa chống trùng → 409 already_processed…" (lần 1 lỗi mạng, `useWriteAction` gửi lại đúng khóa cũ, hai lần ghi cùng `Idempotency-Key`, không có ô khóa) | lệnh chung | qua |
| Khóa không vào `localStorage`, `sessionStorage`, log của trình duyệt (test quét) | › "khóa không vào localStorage, sessionStorage, bộ đệm truy vấn, log của trình duyệt" (quét hai kho, mọi lần `setItem`, `console.log/info/warn/error/debug`); › "bộ đệm React Query không giữ khóa" (quét query cache và mutation cache) | lệnh chung | qua |
| Thu hồi cần xác nhận; 409 → tải lại | › thu hồi › "cần xác nhận; Đóng thì không gửi; xác nhận gửi version đã đọc, tải lại danh sách", "409 version_conflict → hộp khóa vừa được người khác đổi, tải lại danh sách" | lệnh chung | qua |
| Danh sách chỉ hiện tiền tố, 4 khóa đủ trạng thái, tên 80 ký tự; 0 khóa; lỗi tải không vẽ như 0 khóa | › danh sách › "4 khóa: tiền tố (không khóa đầy đủ)…", "0 khóa → chưa có khóa nào…", "lỗi tải → hộp lỗi, không vẽ như 0 khóa" | lệnh chung | qua |
| Form tạo khóa: tên 1–80, ít nhất một quyền, IP đúng dạng; 422 `permission_not_allowed_for_key` | › "kiểm form: tên rỗng, quá 80 ký tự, không quyền nào, IP sai dạng…", "máy chủ 422 permission_not_allowed_for_key…"; `serviceKeyRules.test.ts` (chín quyền đúng `identity.ServiceKeyPermissions`, `parseCidrs`, `validateCreate`, chip) | lệnh chung | qua |
| Hãng: bật/tắt hiện trên web và thứ tự bằng `updateCarrierAccount` (`showOnWeb`, `webSort`, `version`) | `WebCarriers.test.tsx` › "bật SPX → PATCH showOnWeb, webSort cuối…", "ẩn hãng đang chọn sẵn → PATCH showOnWeb false; báo đã bỏ chọn sẵn…", "↓ ở VTP → ghi webSort 2 cho VTP, 1 cho Best; hai khóa khác nhau của cùng thao tác"; `webCarrierRules.test.ts` › `reorder`, `nextWebSort` | lệnh chung | qua |
| Ẩn hãng cuối bị chặn (màn + 422 `last_web_carrier`); tài khoản không hoạt động không bật được | › "ẩn hãng đang hoạt động cuối cùng → không gửi, ô giữ tích…", "máy chủ từ chối 422 last_web_carrier…"; trạng thái A › "đang hiện theo thứ tự…" (Tạm ngừng, Cần cấp quyền lại bị khóa, có câu lý do) | lệnh chung | qua |
| Tài khoản ưu tiên phải đang hiện (màn kiểm; API vẫn 422 `carrier_not_on_web`) | trạng thái A (radio chỉ có hãng active đang hiện); `webCarrierRules.test.ts` › "kiểm: chọn sẵn phải đang hiện…"; › "422 carrier_not_on_web → câu của mã lỗi" | lệnh chung | qua |
| Chọn sẵn, phí dự phòng bằng `saveWebCarrierSettings` giữ `version`; 409 → tải lại | › "chọn rẻ nhất, bật phí 30.000 → PUT đúng thân…", "phí 600.000 → câu lỗi dưới ô, nút Lưu khóa", "409 khi lưu → hộp người khác vừa đổi, bỏ bản nháp, tải lại", "409 version_conflict → hộp người khác vừa đổi, tải lại" (khi bật/tắt) | lệnh chung | qua |
| Số vận đơn 30 ngày, chọn sẵn, thứ tự khách thấy | trạng thái A › "đang hiện theo thứ tự (số vận đơn 30 ngày, chọn sẵn)…" | lệnh chung | qua |
| Nhắc shop nhập phí dự phòng và bật hiện trên web trước khi website dùng báo phí CRM | › "nhắc việc trước khi website dùng báo phí của CRM: thiếu phí dự phòng → báo còn thiếu", "đủ hai việc → báo đã đủ" | lệnh chung | qua |
| Đang lưu khóa mọi thao tác | › "đang lưu → mọi ô bật/tắt, nút thứ tự và nút Lưu khóa; dòng đang lưu ghi Đang lưu…" (bật/ẩn), "đang lưu chọn sẵn → ô bật/tắt, nút thứ tự, ô chọn khóa; không gửi lệnh ghi thứ hai" (lưu cài đặt) | lệnh chung | qua |
| Bản nháp gửi đúng version đã đọc (không lấy bản vừa tải lại ngầm) | › "bản nháp giữ version đã đọc: cài đặt tải lại ngầm (version 6) rồi Lưu → gửi version 5 (máy chủ trả 409)" | lệnh chung | qua |
| Chỉ xem, 0 tài khoản hoạt động, lỗi tải | › "không có quyền manage → không ô, không nút…", "không tài khoản nào hoạt động…", "lỗi đọc cài đặt → hộp lỗi…" | lệnh chung | qua |
| Menu: nhóm Website sau Khuyến mãi; "Hãng trên web" mở màn Kết nối vận chuyển và cuộn tới thẻ; Khóa dịch vụ chỉ cho `identity.service_keys.manage` | `app/screens.test.tsx` › "NAV_GROUPS đúng mục 'Xếp module'…"; `service-keys/route.test.tsx`; `web-carriers/route.test.tsx`; `WebCarriers.test.tsx` › "mục menu Hãng trên web cuộn tới thẻ; mở Kết nối vận chuyển thì không cuộn" | lệnh chung | qua |
| Màn Kết nối vận chuyển cũ không vỡ | `carriers/*.test.tsx` (không sửa ca nào) | lệnh chung | qua |
| Cổng kiểm của task | build, vet, lint, catalog, jscpd, gitleaks, oasdiff, test-guard, console (tsc, eslint, prettier, vitest toàn console, vite build, gitleaks dist), test kiến trúc | ci-task của task (log ngoài worktree) | `CI-TASK XANH` (lần chạy cuối, sau sửa review); vitest `Test Files 99 passed (99)`, `Tests 1192 passed (1192)`; jscpd 0 clone. Lần 1 ĐỎ gitleaks: tiền tố/khóa giả trong `serviceKeysTestkit.tsx` bị nhận là khóa API → đổi sang chuỗi entropy thấp. Sau sửa review lần 1: ĐỎ jscpd (đoạn lặp trong `WebCarriers.test.tsx`) → gom thành hàm phụ `holdWrites`, `pickVtp`, `saveOnce`, `clickLockedThenRelease` |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 208 không đòi spec Playwright; so ảnh với mockup để tới khi chủ shop cho chạy Playwright)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ngoài `ServiceKeys.tsx`, `WebCarriers.tsx` và test: thêm `serviceKeyRules.ts`, `webCarrierRules.ts` (luật thuần + test), `route.tsx` của hai thư mục, CSS riêng, `serviceKeysTestkit.tsx`. Không sửa `main.tsx` (màn tự đăng ký qua `route.tsx`, quy ước `src/app/loadScreens.ts` từ Task 393).
- Sửa `nav.ts` (nhóm Website) — kế hoạch giao Task 462; xem Quyết định 2.
- `carriers/carriersTestkit.tsx`: API giả thêm `GET /api/v1/web-carrier-settings` và `hold` (giữ lệnh ghi để xem "đang lưu").
- "So ảnh với mockup" (Step 4) để tới khi chủ shop cho chạy Playwright (chủ shop 24/11).

## Việc phát hiện thêm

- (Máy chủ, fulfillment) `getWebCarrierSettings.carriers` chỉ có tài khoản đang hiện; màn không có số vận đơn 30 ngày của tài khoản chưa hiện (mockup HV12 ghi cho mọi dòng).
- (Máy chủ, fulfillment) Chưa có API đổi thứ tự hãng trên web cả danh sách trong một transaction; màn gửi nhiều `updateCarrierAccount`.
- (Máy chủ, identity, fulfillment) Lỗi 409 không kèm người, giờ đổi gần nhất để màn nêu như mockup.

## Reviewer

Lần 1 (agent reviewer, 24/11, `git diff --cached` 20 tệp): **CẦN SỬA**, hai phát hiện, đã sửa:
1. Bản nháp "Hãng chọn sẵn / Phí dự phòng" gửi `version` của bản mới nhất trong bộ đệm (tải lại ngầm do báo hiệu, resync 20 s, hay sau khi bật/ẩn) → có thể ghi đè thay đổi của người khác không báo 409. Sửa: bản nháp giữ `version` của lần đọc nó dựa vào (`Draft { form, version }` trong `WebCarriers.tsx`); thêm test "bản nháp giữ version đã đọc…".
2. Đang lưu chọn sẵn/phí thì ô bật/tắt và nút ↑ ↓ chưa khóa. Sửa: `save` đưa lên `WebCarriers`, `busy = patch.pending || save.pending` khóa mọi thao tác ghi; thêm test "đang lưu chọn sẵn → …".
Ghi chú không bắt buộc đã làm luôn: thẻ chỉ vẽ câu trống khi không tài khoản nào hoạt động **và** không tài khoản nào đang hiện (còn tài khoản tạm ngừng đang hiện thì vẫn vẽ để ẩn được, khớp Quyết định 7). Ghi chú còn lại (không sửa): `nextWebSort` chặn ở 1000 nên tài khoản bật thêm khi đã có số 1000 trùng số, xếp theo id — vô hại.

Lần 2 (agent reviewer, 24/11, `git diff --cached` 20 tệp): **Kết luận: ĐẠT**. Phát hiện phải sửa: không có. Đã kiểm riêng hai phát hiện lần 1: bản nháp giữ `version` đã đọc (`Draft { form, version }`, test "bản nháp giữ version đã đọc…" gửi version 5 sau khi máy chủ giả đổi sang 6 — mã cũ thì đỏ); `busy = patch.pending || save.pending` khóa ô bật/tắt, ↑ ↓, ô chọn, nút Lưu (test "đang lưu chọn sẵn → …"). Mục 4–7: dùng lại đúng, không gói util/common, không test bị tắt hay nới, không gọi API ngoài client sinh, không ép kiểu dữ liệu API, mọi lệnh ghi có `Idempotency-Key`, khóa bí mật không vào log hay bộ nhớ trình duyệt.
Ghi chú không bắt buộc (không sửa, ghi để chủ shop biết):
- Sửa form rồi tự chỉnh về đúng giá trị cũ: bản nháp vẫn giữ `version` cũ; nếu người khác lưu trong lúc đó, lần sửa tiếp có thể nhận một hộp 409 không cần thiết (hướng an toàn, không ghi đè ai).
- Test "đang lưu → …" (nhánh bật/ẩn) có tên nhắc nút Lưu, nhưng lúc đó nút Lưu đã tắt sẵn vì form chưa đổi; việc khóa nút Lưu khi đang lưu do mã `disabled={… || busy}` bảo đảm, test nhánh lưu cài đặt kiểm ô chọn bị khóa.
