# Task 462 — Nhóm menu Website, màn Nhận đơn web, màn Nhật ký thao tác

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 462, M5-50, làn L4). Loại việc (quy trình mục 3): A (chỉ màn hình console).
Mockup: `docs/mockups/m5/web-intake.html` (A–D), `docs/mockups/m5/audit.html` (A–B), `menu.html`; HV01, HV02, HV23, HV24, HV25 (chủ shop OK 21/11).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ sửa `console/` (không đổi Go, không đổi
  `openapi.yaml`).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: chỉ **dùng** (không đổi hợp đồng) `getWebIntake`, `setWebIntake` (Task 456),
  `listAuditLogs` (Task 457), `getMe` (quyền). oasdiff: không chênh.
- Chỗ dùng trên màn hình: tạo `console/src/screens/web-intake/` (route `/website/nhan-don-web`, nhóm "Website", order 40
  — giữa "Hãng trên web" 30 và "Khóa dịch vụ" 50, đúng thứ tự HV01) và `console/src/screens/audit/` (route
  `/nhat-ky-thao-tac`, nhóm "Quản trị", order 30 sau "Nhân viên" 10, "Vai trò" 20). Sửa `console/src/app/nav.ts` (chỉ
  ghi chú của nhóm Website: nhóm đã có từ Task 208, không thêm lần hai) và `console/src/app/screens.test.tsx` (thêm test
  thứ tự mục của nhóm Website và chỗ của Nhật ký). Màn tự đăng ký qua `loadScreens` (không sửa danh sách màn nào khác).
- Mã chung của console bị chạm: `MoreBar` (`screens/ma-giam-gia/MoreBar.tsx`) thêm prop tùy chọn `label` (mặc định
  "Tải thêm", không đổi 8 chỗ dùng: CouponList, CouponDetail, FeedbackList, CampaignList, RequestList, BatchDetail,
  BatchList, CustomersTab); `app/time.ts` thêm `fullTime` và tách phần đọc giờ thành hàm nội bộ `parts` dùng chung với
  `shortTime` (đầu ra `shortTime` giữ nguyên, test cũ của `time.test.ts` vẫn qua; 30 tệp dùng `shortTime` không đổi).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run src/screens/web-intake src/screens/audit src/app/screens.test.tsx`,
  `npx tsc --noEmit`, `npx eslint` các tệp sửa; cổng kiểm của task (ci-task) một lần. Không chạy Playwright/e2e (lệnh
  chủ shop 24/11).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` là mã Go (không có phần
  console); tìm trong `console/src`: hộp thoại (`Dialog` ở `screens/staff/staffData.tsx`, màn Khóa dịch vụ dùng), đường
  ghi chống trùng (`useWriteAction`, `app/idempotency.ts`), hàng "Tải thêm" theo con trỏ (`MoreBar` ở
  `screens/ma-giam-gia/MoreBar.tsx`, màn Góp ý dùng), quyền (`usePerms` ở `screens/don/orderData.ts`), giờ VN
  (`shortTime`, `VN_TIME_ZONE` ở `app/time.ts`), lỗi (`unwrap`, `errorMessage`, `ApiError`), khóa truy vấn
  (`queryKey`, chủ đề realtime ở đầu), bộ test (`screenTestkit.tsx`, `routeTestkit.ts`).
- Dùng lại / viết mới vì: dùng lại toàn bộ các mục trên. Viết mới: nhãn tiếng Việt của mã hành động, loại thực thể, loại
  người làm, tên trường của nhật ký (`auditRules.ts`, chỉ màn Nhật ký dùng) và cách đổi ngày VN → mốc giờ `[from, to)`
  (cũng ở `auditRules.ts`; chưa màn nào khác lọc theo khoảng ngày gửi lên API, nên chưa đưa vào `app/time.ts`). Định
  dạng "HH:mm · dd/mm/yyyy" của màn Nhận đơn web: thêm `fullTime` vào `app/time.ts` cạnh `shortTime` (chưa có hàm chung
  có năm; `fullDate` của Khóa dịch vụ chỉ có ngày).

## Soát thiết kế

Task chỉ đổi màn hình; phần máy chủ (khóa, version, quyền, audit) đã có ở Task 456, 457.

- 5.1–5.8, 5.10: không áp dụng — không đổi Go, database, module.
- 5.9 Đường ghi: màn chỉ gọi `setWebIntake` qua `useWriteAction` (gửi `Idempotency-Key`, giữ khóa khi gửi lại vì mạng
  hay bận); hợp đồng đã khai `x-permission` và `IdempotencyKey`.
- 5.11 Thư viện: không thêm thư viện.
- 5.12 Theo mẫu: màn tự đăng ký (`route.tsx` + `route.test.tsx`), testkit API giả như màn Góp ý, Khóa dịch vụ.
- 6.1 Hai người cùng đổi: gửi `version` đang thấy; 409 `version_conflict` → tải lại, đóng hộp, báo "vừa được người khác
  đổi" kèm trạng thái mới, không tự gửi lại.
- 6.2 Bấm hai lần, mạng chập: nút khóa khi đang lưu (cả ô lý do và nút Đóng); `useWriteAction` dùng lại khóa với cùng
  nội dung.
- 6.3–6.11: không áp dụng (không database, hàng đợi, tiền ở task này).
- 6.12 Dữ liệu cá nhân: API nhật ký không trả giá trị trước/sau, IP; màn chỉ hiện tên trường. Lý do tạm dừng là chữ của
  nhân viên, chỉ nằm trong bộ nhớ React Query; không ghi log, localStorage.
- 6.13 Quyền: menu ẩn màn khi thiếu `order.read` / `audit.read`; nút Tạm dừng/Mở lại khóa kèm câu khi thiếu
  `order.web_intake.manage` (API vẫn kiểm); 403 của API có câu riêng, không vẽ như dữ liệu rỗng.
- Bẫy "nextCursor": còn `nextCursor` thì ghi "còn nữa" và có nút Xem thêm; không ghi "hết", không coi là 0.

## Quyết định thay chủ shop

- **Lọc "Người làm" lấy từ những người đã hiện trong các trang đã tải** (cộng người đang chọn). API nhật ký lọc theo mã
  người làm nhưng không có danh sách người cho quyền `audit.read` (`listStaff` cần `identity.users.manage`). Phương án
  khác: thêm API danh sách người làm (việc máy chủ, ngoài phạm vi) — ghi ở Việc phát hiện thêm.
- **Lọc "Thao tác" là danh sách cố định các mã hành động đang có trong mã máy chủ** kèm nhãn tiếng Việt; mã chưa có nhãn
  hiện nguyên mã trên dòng. Tạm dừng và mở lại nhận đơn là một mã `order.web_intake.set` (API không tách), nhãn
  "Tạm dừng / mở lại nhận đơn web"; trường đổi cho biết là gì.
- **Cột "Mục" hiện loại thực thể (nhãn) + mã thực thể** (API chỉ trả mã, không trả tên như "YC0000144" hay tên chiến
  dịch trong mockup). Thực thể toàn shop (nhận đơn web, banner) chỉ hiện nhãn.
- **Không có ô lọc thực thể** trên màn (mockup HV24 và kế hoạch chỉ có hành động, người, khoảng ngày).
- **"Đến ngày" tính hết ngày đó** (gửi `to` = 00:00 ngày kế tiếp giờ VN); "Từ ngày" sau "Đến ngày" → câu lỗi tại chỗ,
  không gọi API (API trả 400).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Thứ tự nhóm menu đúng "Xếp module" (Website giữa Khuyến mãi và Sản phẩm; mục Banner, Bộ phối, Hãng trên web, Nhận đơn web, Khóa dịch vụ; Nhật ký cuối Quản trị) | `screens.test.tsx`: "NAV_GROUPS đúng mục 'Xếp module'…", "nhóm Website, Quản trị xếp mục đúng…"; `web-intake/route.test.tsx`, `audit/route.test.tsx` | `cd console && npx vitest run src/app/screens.test.tsx src/screens/web-intake src/screens/audit` | qua (trong 124/124 của lần chạy lẻ) |
| Trạng thái đang nhận / đang tạm dừng kèm lý do, người, giờ; chưa ai đổi → không có dòng | `WebIntake.test.tsx` › Trạng thái (4 ca, gồm 403 và lỗi khác → Thử lại) | như trên | qua |
| Tạm dừng thiếu lý do (cả chỉ khoảng trắng) → nút khóa kèm câu; gửi lý do đã cắt, version, Idempotency-Key | `WebIntake.test.tsx` › "thiếu lý do (kể cả chỉ khoảng trắng)…" | như trên | qua |
| Đang lưu khóa nút gửi, Đóng, ô lý do; bấm lại không gửi thêm | `WebIntake.test.tsx` › "đang lưu: …" | như trên | qua |
| 409 → tải lại, đóng hộp, báo trạng thái mới; 422 → câu lỗi, giữ lý do | `WebIntake.test.tsx` › "409 (người khác vừa đổi)…", "422 → …" | như trên | qua |
| Mở lại cần xác nhận; chỉ có quyền xem → nút khóa kèm câu | `WebIntake.test.tsx` › Mở lại, Quyền (3 ca) | như trên | qua |
| Nhật ký 120 dòng → 3 trang không trùng không sót, còn nextCursor ghi "còn nữa" | `AuditLog.test.tsx` › "120 dòng → 3 trang theo con trỏ…" | như trên | qua (con trỏ gửi: null, "50", "100") |
| Dòng nhật ký: người làm 3 loại, mã lạ hiện nguyên, chỉ tên trường | `AuditLog.test.tsx` › "mỗi dòng: …"; `auditRules.test.ts` (10 ca) | như trên | qua |
| Lọc hành động, người (từ người đã hiện), khoảng ngày VN; ngày đầu sau ngày cuối → câu lỗi, không gọi API; rỗng, 403 | `AuditLog.test.tsx` › 4 ca lọc/rỗng/403; `auditRules.test.ts` › dayRange (5 ca) | như trên | qua |
| `fullTime` giờ VN có năm | `time.test.ts` › fullTime (2 ca) | `npx vitest run src/app/time.test.ts` | qua |
| Cổng kiểm của task (tsc, eslint, vitest cả console, jscpd, test kiến trúc…) | — | ci-task (log ngoài worktree `…/scratchpad/logs/ci-462.log`) | `CI-TASK XANH`; console `Test Files 121 passed`, `Tests 1387 passed`; jscpd 0 bản lặp mới; `ok elimaz.com.vn/crm/tests/arch` |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 462 không có spec Playwright; đủ ca ở test vitest)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ngoài danh sách tệp của kế hoạch: `screens/ma-giam-gia/MoreBar.tsx` (prop `label` để nút ghi "Xem thêm" như mockup
  audit) và `app/time.ts`, `app/time.test.ts` (`fullTime` — gom định dạng giờ có năm vào chỗ chung vì jscpd báo lặp với
  `shortTime`). Tệp màn đặt tên `AuditLog.tsx` như kế hoạch, thành phần tên `AuditLogScreen` (tránh trùng kiểu `AuditLog`
  của client sinh).

## Việc phát hiện thêm

- API nhật ký chưa có danh sách người làm cho người có `audit.read` (lọc người phải lấy từ dòng đã tải).
- (Reviewer, không bắt buộc) `content.look.save` ghi mã thực thể là mã shop nhưng `content.looks` chưa ở `SHOP_WIDE` của
  `auditRules.ts`: cột "Mục" hiện UUID shop. Ô lọc "Thao tác" chưa có `inventory.stocktake.count`,
  `inventory.document.lines` (dòng vẫn hiện nguyên mã). Có thể bổ sung ở task sau.

## Reviewer

Kết luận: ĐẠT (lần 1, trên phần đã stage sau ci-task XANH). Phát hiện phải sửa: không có. Reviewer đối chiếu hợp
đồng (quyền, IdempotencyKey, version, maxLength 300), 409 không tự gửi lại, khóa khi đang lưu, 403 riêng, khóa truy vấn
khớp chủ đề realtime `order.web_intake`, mã hành động có thật trong `internal/`, mục Ảnh hưởng và từng dòng bảng bằng
chứng khớp test. Ghi chú không bắt buộc (chưa sửa, ghi ở Việc phát hiện thêm): Bộ phối hiện UUID shop ở cột Mục; ô lọc
thiếu hai mã kiểm kho/phiếu kho; mục Tái dùng ghi lệch chỗ đặt hàm giờ — đã sửa câu trong tệp task.
