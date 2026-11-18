# Task 53 — Màn Sức khỏe hệ thống

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 53, M1-53; Sự kiện lỗi và Tệp bị mất
đã tách sang 330, 331 — không làm ở đây). Mockup đã duyệt: `docs/mockups/m1/suc-khoe.html` (+ `png/suc-khoe-{1512,390}.png`,
trạng thái A, B, C). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (chỉ màn hình; không đổi mã Go).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng; dùng `getPlatformHealth` (`GET /api/v1/platform/health`,
  `x-permission: platform.health.read`, Task M0-20) qua client sinh.
- Chỗ dùng trên màn hình:
  - Màn mới `/suc-khoe`, thư mục `console/src/screens/health/` (`route.tsx` tự đăng ký, `HealthPage.tsx`,
    `healthRules.ts` + test, `health.css`); nhóm menu "Hệ thống", quyền `platform.health.read`; thêm một dòng kỳ
    vọng vào `src/app/screens.test.tsx` (danh sách SCREENS).
  - `console/src/app/router.tsx`: người mở thẳng đường dẫn của một màn có thật nhưng thiếu quyền thấy trang
    "Không có quyền" (trạng thái C của mockup) thay cho câu "không tìm thấy". Đổi chung cho mọi màn, nên ba chỗ đang
    kiểm câu cũ cho trường hợp thiếu quyền phải đổi kỳ vọng: `src/shell/Layout.test.tsx` (ca "/nhan-vien"),
    `e2e/staff.spec.ts` (ca menu Nhân viên), `e2e/roles.spec.ts` (ca không quyền Vai trò). Đường không có thật vẫn
    "không tìm thấy" (giữ ca "/khong-co").
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npm test` (gồm `healthRules.test.ts`, `Layout.test.tsx`), `npx tsc --noEmit`,
  `npx eslint .`; Playwright `e2e/health.spec.ts` (mới, 1512 và 390), cùng `e2e/roles.spec.ts`, `e2e/staff.spec.ts`,
  `e2e/shell.spec.ts` (menu, định tuyến bị đổi); `scripts/agent/ci-task.sh` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` phần console
  (`unwrap`, `errorMessage`, `queryKey`, `onRealtimeResync` đã nối toàn cục), `hasPermission`/`allowedScreens`
  (`app/nav.ts`), lớp CSS chung trong `styles.css` (`page-h`, `box-err`, `box-ok`, `card`, `cols`, `chip-err`,
  `row-between`, `err-text`, `empty`, `btn-quiet`, `btn-line`, `hint`, `stack`); fixture e2e `api`, `forEachViewport`,
  `noHorizontalScroll`. Tìm "giây trước", "phút", `toLocaleString` trong `console/src`: chưa có hàm định dạng thời lượng.
- Dùng lại / viết mới vì: dùng lại tất cả trên. Viết mới các hàm định dạng thời lượng và quy tắc "ngừng" trong
  `screens/health/healthRules.ts` (chỉ màn này dùng; luật console 6: chưa đưa vào `shared/` khi chưa có màn thứ hai
  cần). Lớp `kpis`, `kpi`, `dot`, `ok-text` chưa có trong console: chép nguyên từ `docs/mockups/m1/m1.css` vào
  `screens/health/health.css`.

## Soát thiết kế

- 5.1–5.6: không áp dụng — màn chỉ đọc một API có sẵn, không bảng, không ghi.
- 5.7: không áp dụng phía màn; API đã chạy trong tenant (Task M0-20).
- 5.8: không áp dụng (không mã Go).
- 5.9: không đường ghi ("Tải lại ngay" chỉ đọc lại GET).
- 5.10: không áp dụng. 5.11: không thêm thư viện (TanStack Query `refetchInterval` có sẵn).
- 5.12: theo màn mẫu `screens/inventory` (route.tsx tự đăng ký, `unwrap`, `queryKey`).
- 6.1–6.8, 6.10, 6.11: không áp dụng (chỉ đọc, không tiền, không vùng rủi ro cao).
- 6.9: tải lại mỗi 20 s (ADR-42) bằng `refetchInterval`, không chồng yêu cầu (TanStack không gửi khi lần trước còn
  chạy); lỗi giữ dữ liệu cũ và hiện hộp lỗi.
- 6.12: API không trả dữ liệu cá nhân (message đã lọc ở máy chủ); màn chỉ hiện `name`, `role`, khóa cảnh báo.
- 6.13: quyền `platform.health.read` kiểm ở API (403); màn chỉ ẩn menu và hiện "Không có quyền", không thay thế kiểm ở
  API.

## Quyết định thay chủ shop

- **Q1 — Ngưỡng "ngừng" của tiến trình: 90 giây**, đúng `metrics.HeartbeatMaxAge` mà `/health/deep` và luật cảnh báo
  dùng; "Ngừng N phút" = số phút tròn xuống của tuổi nhịp sống (200 s → 3 phút; từ 60 phút: giờ; từ 24 giờ: ngày).
  Phương án khác: ngưỡng riêng cho màn — có thể lệch với cảnh báo.
- **Q2 — Tiến trình không có nhịp sống nào:** `worker` và `ingest` (hai vai trò luật cảnh báo kiểm) luôn có ô; thiếu
  dòng thì ô đỏ "CHƯA CÓ NHỊP SỐNG", như `/health/deep` coi là hỏng. Vai trò khác (api, imager…) chỉ hiện khi API trả.
  Phương án khác: chỉ hiện vai trò API trả — tiến trình chưa từng chạy sẽ biến mất khỏi màn.
- **Q3 — Đường dẫn `/suc-khoe`** (theo tên mockup, như `/vai-tro`, `/nhan-vien`); nút "Xem đủ N loại" trỏ
  `/su-kien-loi` (tên mockup của Task 330, chạy song song). Phương án khác: `/he-thong/...` — không màn nào đang dùng
  tiền tố nhóm.
- **Q4 — Trang "Không có quyền" làm chung ở `router.tsx`** cho mọi màn có thật mà người xem thiếu quyền (mockup
  trạng thái C; kế hoạch Task 53 Step 1 đòi; Task 330, 331 ghi "như màn Sức khỏe"). Không có cách làm trong riêng
  thư mục màn vì route của màn thiếu quyền không được đăng ký. Nút về: "Về trang <màn đầu tiên có quyền>" (mockup:
  "Về trang Sản phẩm"); không có màn nào thì không có nút. Phương án khác: giữ câu "không tìm thấy" chung — trái
  mockup và tiêu chí của task. Đảo ngược: bỏ một nhánh trong `AppRoutes`.
- **Q5 — Trạng thái B (gọn) chỉ khi mọi thứ ổn:** mọi tiến trình đang chạy, `dead` = 0, `inDoubt` = 0, không cảnh
  báo và không lỗi trong 24 giờ; còn lại dùng bố cục A (hộp đỏ đầu trang chỉ khi có tiến trình ngừng). Câu
  "(máy dev)" của mockup bỏ: màn không biết đang chạy ở máy nào; ghi "Chưa có bản nào".
- **Q6 — Một ô mỗi vai trò, lấy nhịp sống mới nhất** khi API trả nhiều tiến trình cùng vai trò (`ListHeartbeatAges`
  trả từng tiến trình; dòng của container cũ sau deploy vẫn còn), giống luật cảnh báo (`HeartbeatAgesByRole`,
  `max(at)`). Phương án khác: một ô mỗi tiến trình — container cũ sẽ hiện đỏ mãi.
- **Q7 — "Nhịp sống cuối N giây trước"** ghi số giây khi dưới một giờ (mockup "200 giây"), từ một giờ ghi giờ/ngày.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Worker nhịp sống 200 s → ô đỏ "Ngừng 3 phút", hộp đỏ nêu worker và giờ cảnh báo gấp; 1512 và 390 | `health.spec.ts` › `1512 × 767` / `390 × 844` › "trạng thái A: worker ngừng 3 phút, …" | `cd console && CRM_E2E_PORT=5153 npx playwright test e2e/health.spec.ts e2e/roles.spec.ts e2e/staff.spec.ts e2e/shell.spec.ts --project=chromium` | `47 passed (21.7s)` |
| Cảnh báo gấp và thường phân biệt bằng chữ ("Gấp" / "Thường", câu giải thích) | cùng ca trạng thái A (`items.nth(0)` "Gấp"; `nth(1)`, `nth(2)` "Thường") | như trên | qua |
| Không quyền `platform.health.read` → không có mục menu, URL thẳng hiện "Không có quyền"; 1512 và 390 | `health.spec.ts` › "không có quyền: không có mục menu, …" (hai khổ); `Layout.test.tsx` › "màn có thật nhưng không có quyền …", "không có quyền và không có màn nào được phép → không có nút về", "trang không có thật → câu không tìm thấy" | như trên; `cd console && npx vitest run` | e2e qua; `Tests 278 passed (278)` |
| Trạng thái B (mọi thứ bình thường, chưa sao lưu) | `health.spec.ts` › "trạng thái B: …" (hai khổ); `healthRules.test.ts` › `allClear` | như trên | qua |
| Tự tải lại mỗi 20 s, "Tải lại ngay" | `health.spec.ts` › "tự tải lại mỗi 20 giây và khi bấm Tải lại ngay; …" (đồng hồ giả `fastForward(20_000)`) | như trên | qua |
| API lỗi: hộp lỗi, giữ số cũ / lần đầu không có số | `health.spec.ts` › "API lỗi: hiện hộp lỗi, giữ số đã có", "API lỗi ngay lần đầu: …" | như trên | qua |
| Ngưỡng 90 s, gộp theo vai trò, định dạng thời lượng, số, giờ | `healthRules.test.ts` (10 ca: `ageText`, `beatText`, `hoursText`, `count`, `clock, clockDay`, `processRows` ×3, `downAlertAt`, `allClear`) | `cd console && npx vitest run src/screens/health` | `Tests 10 passed (10)` |
| Màn có trong SCREENS đúng nhóm, quyền, thứ tự | `screens.test.tsx` › "đủ các màn đã có, …" | `npx vitest run` | qua |
| Đúng mockup | ảnh chụp 1512, 390 (A, B, C) so với `docs/mockups/m1/png/suc-khoe-{1512,390}.png` | spec chụp tạm (đã xóa) | khớp bố cục, chữ, màu; dữ liệu thử có tên lỗi dài: xuống dòng, không tràn |
| Cổng kiểm của task | — | `ci-task.sh` (thư mục script của agent) | `CI-TASK XANH (so với điểm tách nhánh a6fb21b…)`; lần 1 đỏ: jscpd 2 đoạn trùng trong test (đã gộp thành `openStateA`, STATE_B dựng từ STATE_A), golangci-lint "parallel golangci-lint is running" do phiên khác |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch ghi "Modify `console/src/app/screens.tsx`"; nay màn tự đăng ký ở `src/screens/health/route.tsx` (quy ước
  Task 393), chỉ thêm một dòng kỳ vọng vào `src/app/screens.test.tsx`.
- Sửa thêm `src/app/router.tsx` (Q4) và đổi kỳ vọng ở ba test đang kiểm câu cũ cho trường hợp thiếu quyền
  (`Layout.test.tsx`, `e2e/roles.spec.ts`, `e2e/staff.spec.ts`): không xóa, không nới ca nào; ca "đường không có thật"
  vẫn kiểm câu "không tìm thấy"; thêm ca "không có màn nào được phép → không có nút về".

## Việc phát hiện thêm

- Task 330 (Sự kiện lỗi) cần dùng đường dẫn `/su-kien-loi` để nút "Xem đủ N loại" của màn này trỏ đúng (hằng
  `ERRORS_PATH` trong `HealthPage.tsx`); nếu 330 chọn đường khác thì sửa hằng này khi gộp.
- Task 330, 331 cũng thêm một dòng vào `src/app/screens.test.tsx` (nhóm "Hệ thống"): khi gộp giữ cả ba dòng.

## Reviewer

Lượt 1: **ĐẠT** — không có phát hiện phải sửa; 12 tệp khớp mục Ảnh hưởng; cách cũ trong `router.tsx` đã xóa, câu
"không tìm thấy" vẫn cho đường không có thật; test cũ được đổi kỳ vọng chặt hơn, không nới; ngưỡng 90 s và khóa
`<vai trò>-heartbeat` khớp `metrics.HeartbeatMaxAge`, `rules.go`. Ghi chú không bắt buộc đã làm: ca tự tải lại đòi
"đúng một lần gọi" sau khi bấm có thể chập chờn khi resync chạy cùng lúc → đổi thành "ít nhất một lần"
(`health.spec.ts`, chạy lại 10 ca qua). Ghi chú còn lại: Task 330 nên dùng cùng giới hạn 10 loại lỗi của máy chủ.

Lượt 2 (sau sửa trên): **ĐẠT** — không có phát hiện phải sửa; chỉ `health.spec.ts:211` và mục này đổi so với lượt 1;
ca vẫn đòi ô worker hiện lại "Ngừng 3 phút" sau khi bấm nên một lần tải mới thật sự phải chạy.

Kết luận: ĐẠT
