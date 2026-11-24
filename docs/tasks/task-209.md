# Task 209 — Màn Quà sinh nhật

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 209, M5-26; mở rộng bốn tab ở Task 450, API của Task 468). Mockup: `docs/mockups/m5/birthday-gifts.html` (HV01–HV25, chủ shop OK 21/11). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ sửa thân `promotion.ListBirthdayCustomers`
  (`internal/modules/promotion/birthday_list.go`): tìm theo SĐT so thêm với dạng trong nước `0…` của số đã lưu `+84…`
  (Việc phát hiện thêm của Task 468). Chữ ký giữ nguyên; nơi gọi: `internal/httpapi/handlers_birthdays.go`
  (`ListBirthdayCustomers`, `ExportBirthdayCustomers`) — hành vi đổi theo đúng ý: gõ `0912…` giờ ra khách.
- Gói phụ thuộc ngược (go list): gói nhập `promotion`: `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`,
  `internal/modules/order`, `internal/wiring/webcache`. Không đổi kiểu hay chữ ký nên chúng chỉ cần build lại; test
  chạy: gói `promotion` (ca mới trong `TestListBirthdayCustomers_States`), cả gói ở ci-task.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn dùng client sinh có sẵn: `getBirthdaySettings`,
  `saveBirthdaySettings`, `runBirthdayGifts`, `listBirthdayTodo`, `markBirthdayMessaged`, `listBirthdayCustomers`,
  `exportBirthdayCustomers` (Task 468), `listCouponBatches?purpose=birthday` (cả `imported`), `getCouponBatch`,
  `listCoupons?batchId` (Task 197, M2). Mã lỗi đã có câu trong `app/errors.ts`: `another_run_in_progress`,
  `invalid_gift_settings`, `version_conflict`, `forbidden`, `already_processed`.
- Chỗ dùng trên màn hình: thư mục mới `console/src/screens/birthday-gifts/` (`route.tsx` tự đăng ký
  `/khuyen-mai/qua-sinh-nhat`, nhóm "Khuyến mãi" như crumb của mockup, quyền `promotion.read` — mọi API đọc của màn đòi
  quyền này; nút ghi hiện theo `promotion.birthday.manage`). Không sửa tệp chung (`nav.ts`, `screens.ts` tự nạp).
  `web-campaigns/CampaignList.tsx`: chỉ thêm `export` cho `Pick` (ô chọn của thanh lọc) để màn này dùng lại, không đổi
  hành vi (test `src/screens/web-campaigns/` chạy lại). Dùng lại (chỉ nhập, không sửa): `ma-giam-gia/couponData.ts`
  (`useBatch`), `ma-giam-gia/couponRules.ts` (`discountText`), `../don/don.css`; (`saveTextFile`, `COUPON_TOPIC`, `usePerms`, `headers`),
  `ma-giam-gia/MoreBar.tsx`, `web-campaigns/campaignRules.ts`
  (`WEBSITE_ORIGIN`), `products/productRules.ts` (`formatMoney`), `products/editRules.ts` (`parseMoney`,
  `moneyInput`), `inventory/Balances` (`useDebounced`, `SEARCH_DELAY_MS`).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `go test -race -run '^TestListBirthdayCustomers_States$' ./internal/modules/promotion/`; vitest
  `src/screens/birthday-gifts/`, `src/screens/web-campaigns/`, `src/app/`; `npx tsc --noEmit`; `npx eslint` tệp mới; `make jscpd`;
  `go test ./tests/arch/`; ci-task. Playwright/e2e không chạy (chủ shop 24/11); kế hoạch Task 209 không có spec
  Playwright riêng ("so ảnh với mockup" để tới đợt chạy Playwright).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (mã Go): chuẩn hóa SĐT
  là `customer.NormalizePhone` — chỉ nhận số đủ 10–11 chữ, không dùng được cho chuỗi tìm một phần (`0912`); viết hàm
  nhỏ trong `birthday_list.go` đổi `+84xxx` → `0xxx` để so. Console: đường ghi chống trùng `app/idempotency.ts`
  (`useWriteAction`), quyền `don/orderData.ts` (`usePerms`), tải tệp `ma-giam-gia/couponData.ts` (`saveTextFile`),
  mức giảm `ma-giam-gia/couponRules.ts` (`discountText`; `couponState` không hợp vì đợt sinh nhật cần thêm "đã tặng/chưa tặng" → `codeState` riêng), đợt `useBatch`, ô chọn `Pick` của `CampaignList`, hàng tải thêm `MoreBar`, tiền `formatMoney`/`parseMoney`/
  `moneyInput`, tìm trễ `useDebounced`, gốc website `WEBSITE_ORIGIN`, giờ `app/time.ts` (`shortTime`), bộ test
  `app/screenTestkit.tsx` (`stubApi`, `renderScreens`, `reply`, `page`, `meReply`), `ma-giam-gia/couponTestkit.tsx`
  (`grantsOf`).
- Dùng lại / viết mới vì: dùng lại mọi thứ trên. Viết mới `birthdayRules.ts` (tám trạng thái và chữ, che SĐT, đổi
  `+84` → `0` cho Zalo, tin chép theo đúng `giftMessage` của website cũ `storefront-birthday/src/server/birthday/rules.ts`,
  kiểm form cài đặt theo giới hạn của API, câu ngày sinh nhật) — nghiệp vụ riêng của màn, không có bản chung.

## Soát thiết kế

Task chủ yếu màn hình; phần máy chủ chỉ là so chuỗi tìm trong bộ nhớ (không SQL, không khóa, không tiền). Mục 5, 6
của quy trình chỉ liên quan ở các câu sau; còn lại không áp dụng (không chạm database, hàng đợi, tồn, transaction).

- 5.9 Chống trùng: mọi lệnh ghi (`saveBirthdaySettings`, `runBirthdayGifts`, `markBirthdayMessaged`,
  `exportBirthdayCustomers`) qua `useWriteAction` (khóa sinh khi bấm, giữ khi tự gửi lại). Xuất CSV là `no-replay`:
  gửi lại cùng khóa → 409 `already_processed` → câu "Tệp đã xuất ở lần bấm trước… bấm Xuất lại".
- 5.12 Theo mẫu: cấu trúc như `web-campaigns` (Task 458): `route.tsx`, `*Rules.ts` thuần + test, `*Testkit.tsx`.
- 6.1 Hai người cùng sửa cài đặt: gửi `version` đã đọc; 409 `version_conflict` → hộp "Cài đặt vừa được người khác
  đổi", tải lại bản mới, bỏ bản nháp (mockup C).
- 6.2 Bấm hai lần / ghi đang chạy: `useWriteAction` trả lại lời hứa đang chạy; nút của thao tác đang chạy khóa lại
  (Lưu, Tạo mã ngay, Đã nhắn một/nhiều mã, Xuất CSV). Hai lượt phát cùng lúc: máy chủ 409 `another_run_in_progress` →
  hộp báo như mockup.
- 6.13 Quyền: màn cần `promotion.read`; nút ghi chỉ hiện với `promotion.birthday.manage` (thiếu → câu chỉ xem); 403
  của API → hộp "Bạn không có quyền xem khuyến mãi" (mockup D), không vẽ như danh sách rỗng.
- Dữ liệu cá nhân: SĐT trên bảng chỉ hiện che giữa (`0912 *** 678`); số đầy đủ chỉ nằm trong liên kết Zalo và tin chép
  (đúng như admin cũ), CSV do máy chủ dựng. Màn không ghi `console.*`, không `localStorage`/`sessionStorage` (test quét).
  Máy chủ: chuỗi tìm không vào log (không thêm log).
- Phân trang: "Tất cả khách" còn `nextCursor` thì có "Tải thêm", không coi là hết (Bẫy đã gặp, Task 84). Lúc tải, lỗi
  403 không vẽ như dữ liệu rỗng.

## Quyết định thay chủ shop

1. **Tìm SĐT gõ số 0 đầu (việc của Task 468):** sửa ở máy chủ (`phoneHas` trong `birthday_list.go`): chuỗi số so với
   cả số đã lưu `+84…` và dạng trong nước `0…`, nên `0912…`, `0912 000 006`, `+84912…`, `84912…` đều ra. Không dùng
   `customer.NormalizePhone` vì nó chỉ nhận số đủ độ dài (tìm một phần như đuôi số vẫn phải chạy). Màn gửi nguyên chuỗi.
2. **"Đến hạn" khi quà đang tắt (việc của Task 468):** giữ trạng thái `due` của máy chủ (đúng luật admin cũ, đổi luật
   là việc riêng), màn hiện rõ: hộp đầu trang "Đang tắt" + câu "Quà đang tắt: CRM không tạo mã mới, kể cả cho khách đã
   đến hạn…", nhãn dòng "Đến hạn · quà đang tắt" (màu thường, không đỏ), số tổng ghi "quà đang tắt, chưa tạo mã".
   Phương án khác: máy chủ trả trạng thái mới `disabled` (đổi hợp đồng, 8 trạng thái của admin thành 9).
3. **Bốn số tổng:** mockup có "Sắp gửi · 7 ngày tới" và "Mã đã dùng · năm 2026 38/112" mà API không có. Chỉ vẽ số API
   có (luật Bẫy đã gặp): Cần nhắn (số mã ở tab Cần nhắn, trần 500 → "500+"), Đến hạn chờ tạo mã, Đã dùng (quà gần
   nhất), Khách đã lưu sinh nhật + số đã hủy nhận (`stateCounts` của `listBirthdayCustomers` không lọc, `limit 1`).
4. **Đợt mã:** `listCouponBatches` không trả số đã tặng/dùng/hết hạn (chỉ `getCouponBatch` có) → danh sách có Đợt (tên,
   ngày tạo), Mã (số lượng), Mở; các số hiện ở chi tiết đợt. `listCoupons` không trả SĐT gắn với mã (`bound` chỉ là cờ)
   → bỏ cột "Gắn với số"; "Đã dùng" không kèm mã đơn (cần gọi lượt dùng cho từng mã; xem ở màn Mã giảm giá), "Đã tặng"
   không kèm kênh (API không có). "Mã chuyển từ website cũ" là lựa chọn "Loại đợt" (`purpose=imported`).
5. **Tin chép:** dùng đúng câu `giftMessage` của website cũ (`storefront-birthday/.../rules.ts`, cũng là câu website gửi
   Zalo/email) thay câu mẫu "chị Lan" của mockup — một câu cho mọi kênh, không đoán giới tính. Liên kết
   `https://elimaz.com.vn/?qua=<mã>` (`WEBSITE_ORIGIN` có sẵn).
6. **Tab giữ trên địa chỉ** (`?tab=can|khach|cai-dat|dot-ma`, đợt đang mở `?dot=`) để chép đường dẫn, nút quay lại
   chạy đúng. Một route, quyền `promotion.read`, nhóm "Khuyến mãi" (crumb của mockup), order 30 sau Chiến dịch.
7. **Hộp 409 của mockup** nêu tên, giờ ("Trần Minh Thư, 10:12", "bắt đầu 10:00"): API lỗi không trả các thông tin đó →
   câu không nêu; lượt chạy cuối vẫn hiện ở hộp đầu trang (tải lại sau mỗi lệnh).
8. **Xuất CSV gửi lại cùng khóa (409 `already_processed`, `no-replay`):** câu riêng "Tệp đã xuất ở lần bấm trước nhưng
   chưa tải về được… Bấm Xuất CSV lần nữa" (lần bấm sau dùng khóa mới).
9. **Không quyền ghi:** nút Tạo mã ngay, ô chọn, Đã nhắn, Xuất CSV, Lưu ẩn; ô cài đặt khóa; câu chỉ xem. Chép tin, Mở
   Zalo vẫn có (chỉ đọc dữ liệu đã được xem với `promotion.read`, như Task 468 đã thiết kế quyền).

## E2E viết sẵn, chưa chạy

Không có: kế hoạch Task 209 không có spec Playwright hay tests/e2e. "So ảnh với mockup" (Step 4) để tới đợt chạy
Playwright sau toàn bộ task (chủ shop 24/11).

## Khác kế hoạch

- Kế hoạch ghi tệp `BatchList.tsx`, `BatchDetail.tsx`, sửa `main.tsx` và menu: màn tự đăng ký qua `route.tsx`
  (quy ước Task 389), không sửa `main.tsx`/`nav.ts`; thêm các tab của Task 450 trong cùng thư mục.

## Bằng chứng

Lệnh vitest: `cd console && npx vitest run src/screens/birthday-gifts/` → `Test Files 6 passed (6) · Tests 41 passed (41)`.
ci-task (log ngoài worktree `…/scratchpad/logs/ci-209.log`): `CI-TASK XANH (so với điểm tách nhánh 0f3b2aa)`; console-check
`Tests 1233 passed (1233)`, tsc, eslint, prettier, vite build sạch; jscpd 0 clone mới; `go test -race promotion ok 27.3s`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tìm SĐT gõ `0912…`, có cách, `+84`, `84` ra đúng khách; số đầu khác không ra (việc Task 468) | `TestListBirthdayCustomers_States` (`checkBirthdayFilters`) | `go test -race -count=1 -run '^TestListBirthdayCustomers_States$' ./internal/modules/promotion/` | trước sửa: `tìm SĐT "0912000006": []` FAIL; sau: `ok … 1.891s` |
| Đợt mã: danh sách 60 đợt (50 + Tải thêm), lọc loại gửi `purpose` | `BatchList.test.tsx` › "60 đợt quà sinh nhật…" | vitest như trên | pass |
| Chi tiết 30 mã ba trạng thái (đã dùng 10, đã tặng 10, chưa tặng 10) + số đếm đợt | `BatchList.test.tsx` › "chi tiết đợt…" | vitest | pass |
| Mã hết hạn chưa dùng: dòng mờ | `BatchList.test.tsx` › "mã hết hạn chưa dùng" | vitest | pass |
| Đợt rỗng | `BatchList.test.tsx` › "đợt rỗng…" | vitest | pass |
| Không quyền → 403 hiện thông báo (Đợt mã và đầu trang), không vẽ như rỗng | `BatchList.test.tsx` › "không quyền (403)"; `BirthdayGifts.test.tsx` › "403 của API…" | vitest | pass |
| Cần nhắn: số che giữa, tên dài, sinh nhật còn/hôm nay/đã qua, hạn dùng, Zalo số đầy đủ, bảng không có số đầy đủ | `BirthdayGifts.test.tsx` › "bốn mã…" | vitest | pass |
| Chép tin đúng mẫu website; SĐT không vào console, localStorage | `BirthdayGifts.test.tsx` › "Chép tin…"; `birthdayRules.test.ts` › "Tin chép" | vitest | pass |
| Đã nhắn một mã / nhiều mã (mã đã ghi trước báo riêng), chọn cả trang, bỏ chọn, rỗng | `BirthdayGifts.test.tsx` › "Đã nhắn một mã…", "chọn nhiều…", "chọn cả trang…", "không mã nào…" | vitest | pass |
| Tạo mã ngay có khóa chống trùng; 409 `another_run_in_progress` hiện hộp | `BirthdayGifts.test.tsx` › "Tạo mã ngay…" | vitest | pass |
| Lượt chạy cuối, chưa chạy, đang chạy; bốn số tổng | `BirthdayGifts.test.tsx` › "lượt chạy cuối…", "chưa chạy lần nào…" | vitest | pass |
| Quà đang tắt hiện rõ; khách Đến hạn ghi "quà đang tắt" (việc Task 468) | `BirthdayGifts.test.tsx` › "quà đang tắt…"; `birthdayRules.test.ts` › "Tám trạng thái" | vitest | pass |
| Chỉ `promotion.read`: không nút ghi, ô cài đặt khóa, không Xuất CSV | `BirthdayGifts.test.tsx` › "chỉ promotion.read…"; `SettingsTab.test.tsx`, `CustomersTab.test.tsx` › "chỉ promotion.read…" | vitest | pass |
| Tất cả khách: 8 trạng thái chữ như mockup, tháng mặc định hiện tại, lọc, tìm, tải thêm, rỗng | `CustomersTab.test.tsx` (4 ca đầu) | vitest | pass |
| Xuất CSV gửi bộ lọc, tải tệp, báo số dòng; 409 `already_processed` câu riêng | `CustomersTab.test.tsx` › "Xuất CSV…" | vitest | pass |
| Cài đặt: lỗi 10.500, đếm lỗi, Hủy; lưu đủ trường + version; tắt; 409 tải lại; 422 | `SettingsTab.test.tsx` (6 ca); `birthdayRules.test.ts` › "Form cài đặt" | vitest | pass |
| Route khai đúng | `route.test.tsx` | vitest | pass |

## Test bị xóa hoặc tắt

(không có)

## Việc phát hiện thêm

- (Reviewer, không bắt buộc) `Pick` (ô chọn thanh lọc) giờ dùng ở hai màn, nhập chéo từ `web-campaigns/CampaignList.tsx`;
  luật console mục 6 muốn mã dùng chung ở `console/src/shared/` — dời khi có task dọn mã dùng chung của console.
- Đợt mã: `listCouponBatches` không trả số đã tặng/dùng/hết hạn, `listCoupons` không trả SĐT gắn mã, kênh đã gửi, mã
  đơn đã dùng → màn không vẽ các cột đó của mockup (Quyết định 4). Thêm vào API nếu chủ shop cần.
- Trạng thái `due` khi quà đang tắt: màn đã ghi rõ (Quyết định 2); đổi luật máy chủ (trạng thái riêng) là việc riêng.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, một vòng, trên bản đã gộp main — main chưa đổi từ 0f3b2aa). Không có phát hiện phải
sửa. Ảnh hưởng khớp thực tế (`ListBirthdayCustomers` chỉ đổi thân, hai nơi gọi trong `handlers_birthdays.go`;
`CampaignList.tsx` chỉ thêm `export`); mọi dòng bảng bằng chứng trỏ tới test có thật và kiểm đúng; không test nào bị
xóa, tắt, nới. Ghi chú không bắt buộc: `Pick` nên ở `console/src/shared/` (ghi ở Việc phát hiện thêm); `phoneHas` chỉ
đổi số `+84`, số khác so nguyên như trước (không hồi quy).
