# Task 48 — Công cụ màn hình M1 và khung điều hướng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 48, M1-48). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ thêm một tệp test `internal/httpapi/errorcodes_test.go`
  (gói `httpapi`, đọc các hằng `code*` không xuất ra ngoài của `errors.go`).
- Gói phụ thuộc ngược (go list): không đổi mã Go nào ngoài test; `go list -deps` không đổi.
- Operation OpenAPI bị chạm, oasdiff: không operation nào đổi. Thêm phần mở rộng `x-error-codes` (danh sách mã lỗi máy
  đọc được) vào `components.schemas.Error`; ogen, routegen, hey-api bỏ qua phần mở rộng lạ, nên `make gen` không đổi mã
  sinh nào (kiểm bằng `make gen-check`); oasdiff không thấy thay đổi phá vỡ (`make oasdiff`).
- Chỗ dùng trên màn hình: `console/src/main.tsx` (bọc `QueryClientProvider`, `BrowserRouter`), `console/src/shell/App.tsx`
  (khung có menu trái, thanh trên 56 px và menu trượt trên điện thoại, nối `onResync`/`onChanged` vào lớp lấy dữ
  liệu), `console/src/styles.css` (token, lớp của mockup M1 cho khung). Mới: `console/src/app/{router.tsx,
  queryClient.ts,errors.ts,idempotency.ts,nav.ts}`, `console/src/shared/index.ts` thêm export. Chưa có màn nghiệp vụ.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npm test` (mọi test console: errors, idempotency, queryClient, nav/khung, Banner, lint-guard),
  `npx tsc --noEmit`, `npx eslint .`, `npx prettier --check .`, `npx playwright test` (shell.spec.ts trên Chromium ở
  1512 và 390, shell.phone.spec.ts trên WebKit); Go: `go test -race ./internal/httpapi/ -run TestErrorCodes`, rồi cả
  gói `internal/httpapi` (gói đổi); `make gen-check`, `make oasdiff`, `make console-check`, `make catalog-check`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: mục `console/src/shared` của `docs/danh-muc-dung-chung.md`
  (createRealtimeClient, watchApiLive, Banner); `grep -r "Idempotency-Key\|retry_later\|request_in_progress" console/src`
  → chỉ có trong mã sinh `console/src/api` (kiểu header), chưa có hàm gửi lại nào; chưa có bảng câu lỗi, bộ định tuyến,
  lớp lấy dữ liệu nào trong console.
- Dùng lại / viết mới vì: dùng lại `createRealtimeClient` (nối `onResync`, `onChanged` vào `QueryClient`), `watchApiLive`,
  `Banner` (khung giữ nguyên băng báo M0), client sinh (`unwrap` chỉ đổi kết quả `{data, error, response}` của client
  sinh thành dữ liệu hoặc `ApiError`, không gọi fetch). Viết mới `useWriteAction` (chưa có gì gửi lại cùng khóa),
  `errors.ts` (chưa có bảng câu lỗi), `nav.ts` (menu theo quyền). Token và lớp CSS chép từ `docs/mockups/m1/m1.css`
  (mockup đã duyệt), không đặt màu mới. Phía Go: test đọc hợp đồng bằng `ogen.Parse` như `tools/routegen`, không thêm
  thư viện YAML.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1–5.7, 5.10: không áp dụng: task không thêm dữ liệu, bảng, transaction, sự kiện, lệnh ra ngoài hay truy vấn nào;
  màn hình là tệp tĩnh, mọi dữ liệu qua API.
- 5.8 Nghiệp vụ không biết HTTP: màn hình không quyết nghiệp vụ, không quyết quyền: menu chỉ ẩn mục không có quyền cho
  gọn, API vẫn kiểm (ADR-18, ADR-34).
- 5.9 Đường ghi: `useWriteAction` là đường duy nhất cho yêu cầu ghi từ màn hình: sinh `Idempotency-Key` khi người dùng
  bắt đầu thao tác (`crypto.randomUUID`), giữ nguyên khóa khi tự gửi lại (503 `retry_later` theo `Retry-After`, tối đa
  3 lần; 409 `request_in_progress` chờ 1 s; lỗi mạng chờ 1 s), không gửi lại lỗi 4xx khác. Đúng ADR-41, ADR-46
  (`retry_later` không được lưu ở máy chủ nên gửi lại cùng khóa chạy lại từ đầu).
- 5.11 Thư viện: `react-router` 8.5.1, `@tanstack/react-query` 5.108.0, `@playwright/test` 1.65.1 ghim đúng bản
  (`--save-exact`, lock), bốn điều ADR-32 + giấy phép + số thông báo lỗ hổng 2026 ở `docs/chon-cong-cu-console.md`,
  một dòng mỗi gói ở `docs/phu-thuoc.md`. TanStack Virtual 3.14.26 được chọn nhưng cài ở màn đầu tiên có danh sách dài.
- 5.12 Theo mẫu: tệp đúng mục Files của kế hoạch (`console/src/app/*`, `console/playwright.config.ts`, `console/e2e/*`);
  mã dùng chung export qua `console/src/shared/index.ts` (luật console 6) để vào danh mục.

Mục 6 (đúng khi chạy đồng thời):
- 6.1 Hai yêu cầu cùng lúc: bấm hai lần khi yêu cầu đầu chưa xong → trả lại đúng lời hứa đang chạy, không gửi yêu cầu
  thứ hai (test). Sửa cùng bản ghi (version) do API kiểm (`version_conflict` có câu tiếng Việt).
- 6.2 Gửi lại: cùng khóa cho mọi lần tự gửi lại và cho lần người dùng bấm lại sau lỗi chưa rõ kết quả (mạng, bận) với
  cùng nội dung; khóa mới khi đã xong, khi lỗi 4xx dứt khoát, hoặc khi nội dung đổi (tránh `idempotency_key_reused`).
- 6.3–6.6, 6.8, 6.10, 6.11: không áp dụng: không database, hàng đợi, tiền.
- 6.7 Bên kia quá tải: theo `Retry-After` của 503 (giây, giới hạn 10 s; thiếu hay sai thì 1 s); 429 `rate_limited`
  không tự gửi lại, hiện câu tiếng Việt.
- 6.9 Treo lâu: số lần gửi lại có trần (503: 3, mạng: 3, 409: 30 lần × 1 s); hủy chờ khi màn đóng (unmount).
- 6.12 Dữ liệu cá nhân: không log gì ra console trình duyệt; câu lỗi chỉ dùng mã và số (`available`), không chèn chữ
  từ máy chủ ngoài các trường số đã biết.
- 6.13 Quyền: menu ẩn mục không có quyền (đọc `/api/v1/me`), API vẫn từ chối; 401/403 của yêu cầu ghi không tự gửi lại.

## Quyết định thay chủ shop

1. **Bốn thư viện Q31 đều đạt, không dừng hỏi.** React Router 8.5.1 (chế độ khai báo), TanStack Query 5.108.0,
   TanStack Virtual 3.14.26, Playwright 1.65.1; bốn điều ADR-32, giấy phép, số thông báo lỗ hổng 2026 ở
   `docs/chon-cong-cu-console.md`. React Router có 18 thông báo năm 2026 nhưng không cái nào trúng 8.5.1 (cao nhất
   `< 8.3.0`, chế độ RSC không dùng). Phương án khác: TanStack Router (kiểu chặt cho tham số; kho của nó bị đăng mã độc
   ngày 11/05/2026), SWR. Đổi: một tệp `src/app/router.tsx`, chưa có màn nào dựa vào.
2. **TanStack Virtual chọn nhưng chưa cài**: chưa có màn danh sách; cài đúng bản ở task màn danh sách đầu tiên (tránh
   phụ thuộc không dùng). Kế hoạch ghi "cài thư viện đã chọn".
3. **Mã lỗi đưa vào hợp đồng bằng `x-error-codes`** (trong `components.schemas.Error` của `openapi.yaml`) vì hợp đồng
   chưa có danh sách mã nào để `errors.test.ts` đọc. Thêm test Go `TestErrorCodes_DeclaredInContract` để mọi hằng `code*`
   của `errors.go` phải có trong danh sách (chiều ngược lại không bắt buộc: khai trước 31 mã của các task M1 sau theo kế
   hoạch). Không dùng `enum` vì sẽ đổi kiểu sinh của ogen/hey-api và oasdiff có thể tính là thay đổi. Hệ quả cho phiên
   song song: task thêm mã lỗi mới phải thêm dòng ở `x-error-codes` và câu ở `console/src/app/errors.ts` (test đỏ nếu
   quên). Đã thấy hiệu quả ngay khi gộp main: Task 41 thêm `file_purpose_not_allowed`, test Go đỏ, đã khai.
4. **Test trình duyệt dùng API giả** (`e2e/fixtures.ts`, `page.route`), không chạy với API dev 4190 + `seed-dev` như kế
   hoạch ghi: API chỉ nhận thẻ Cloudflare Access ký bằng khóa tải từ `https://<team>/cdn-cgi/access/certs`, không có
   đường cho máy dev; thêm đường đó là sửa xác thực (vùng rủi ro cao) ngoài phạm vi task. Ghi ở "Việc phát hiện thêm".
   Spec của khung không cần dữ liệu thật.
5. **Menu theo danh sách màn có thật** (`src/app/screens.tsx`, hiện rỗng): không vẽ mục cho màn chưa làm (tránh trang
   "đang làm"); mỗi task màn thêm một dòng. Bốn nhóm theo đúng thứ tự mockup. Đã chụp thử khung với đủ 11 mục của mockup
   (bỏ lại sau khi chụp) để so: cột trái, nhóm, mục đang mở, menu trượt khớp `docs/mockups/m1/png/kho-ton-*.png`.
6. **Ô người dùng hiện email** thay vì "Nguyễn Thị Lan / Chủ · Toàn shop" của mockup: `/api/v1/me` chỉ có `email` và
   `grants` (không tên, không tên vai trò). Thêm `displayName`, vai trò vào `Me` là việc của hợp đồng/identity, ghi ở
   "Việc phát hiện thêm". Nút tài khoản ở thanh trên (chữ đầu email) mở menu trượt, nơi có ô người dùng — mockup không
   nói nút làm gì.
7. **Câu chữ mới** (mockup không có): trang trống `TEXT_EMPTY_HOME` ("Tài khoản này chưa được giao màn hình nào. Nhờ Chủ
   shop cấp quyền"), `TEXT_NOT_FOUND` ("Không có trang này, hoặc bạn chưa được cấp quyền xem"), `TEXT_NETWORK`,
   `TEXT_UNKNOWN_ERROR` và 57 câu lỗi trong `ERROR_MESSAGES`. Đổi: một hằng trong `src/app/router.tsx`, `errors.ts`.
8. **Luật gửi lại ngoài phần kế hoạch ghi:** lỗi mạng cũng gửi lại cùng khóa (1 s, tối đa 3 lần — an toàn vì cùng khóa);
   409 `request_in_progress` có trần 30 lần (kế hoạch không ghi trần); Retry-After tối đa 10 s; bấm lại sau lỗi chưa rõ
   kết quả cùng nội dung dùng lại khóa cũ, nội dung đổi thì khóa mới (tránh `idempotency_key_reused`).
9. **Phông chữ tự chứa** (`@fontsource/mulish`, `@fontsource/playfair-display`, OFL-1.1): mockup dùng Google Fonts nhưng
   CSP của `crm-console` chặn nguồn ngoài. Chỉ bộ latin + vietnamese (~60 kB woff2).
10. **Nền mờ sau menu trượt** `rgba(43, 38, 36, 0.4)` (màu `--ink` với độ trong 40%, biến `--scrim`); mockup không vẽ
    menu đang mở. Menu trượt rộng `min(300px, 85vw)`, mục cao 44 px khi mở trên điện thoại.
11. **Dev/test cổng 5148**: Playwright bật Vite ở `CRM_E2E_PORT` (mặc định 5148, chặn 4175/4176/4180/4181); dev thường vẫn
    `CRM_VITE_PORT` (mặc định 4192 theo kế hoạch M0). `make console-e2e` mới; không thêm vào `make ci` vì cần tải trình
    duyệt (GitHub CI chưa chạy e2e).
12. **Spec điện thoại thao tác bằng chạm** (`tap`, `hasTouch: true`): WebKit giả lập iPhone không nhận `click` chuột của
    Playwright vào nút trong thanh trên dính ("html intercepts pointer events"), trong khi `elementFromPoint` trả đúng
    nút và `tap` chạy; người dùng điện thoại vốn chạm.

## Bằng chứng

Bản đã gộp main hai lần, lần cuối tới `3cfbcfc` (Task 41, 315, 18, 27). Sau lần gộp thứ hai `TestErrorCodes_DeclaredInContract`
đỏ với hai mã mới của Task 27 (`code_taken`, `not_for_sale`); đã khai trong `x-error-codes` và thêm câu ở `errors.ts`.
Sau gộp: `npx vitest run` 90 qua; `npx playwright test --repeat-each=3` 18 qua; `go test -race ./internal/httpapi/...` ok
(60,7 s); `make gen-check`, `make oasdiff`, `make jscpd`, `make lint` (`0 issues.`), `make console-check` sạch.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| 503 hai lần rồi 201 → 3 yêu cầu cùng khóa, chờ theo Retry-After | `idempotency.test.ts` "503 hai lần rồi 201…" | `cd console && npx vitest run` | `Tests 84 passed (84)`; 3 yêu cầu, khóa `{key-1}`, chờ `[1000, 2000]` |
| 503 bốn lần → câu "Hệ thống đang bận…", tổng 4 yêu cầu | "503 bốn lần → tổng 4 yêu cầu…" | như trên | qua |
| 422 → không gửi lại | "422 → không gửi lại…" | như trên | qua, 1 yêu cầu |
| Bấm hai lần liền → 1 yêu cầu | "bấm hai lần liền → 1 yêu cầu…" | như trên | qua |
| 409 request_in_progress → chờ 1 s gửi lại cùng khóa (có trần) | "409 request_in_progress…" (2 ca) | như trên | qua; 31 yêu cầu rồi dừng |
| 2xx nhưng đọc thân hỏng → giữ khóa, gửi lại cùng khóa (reviewer lần 1, phát hiện 1) | `idempotency.test.ts` "201 nhưng thân đọc hỏng…", `errors.test.ts` "2xx nhưng đọc thân lỗi…" | `npx vitest run` | `Tests 90 passed (90)`; đỏ trước khi sửa `unwrap` |
| /api/v1/me đang tải, lỗi 401, API tắt → không báo nhầm "chưa được giao màn hình"; tải xong không quyền → có câu (reviewer lần 1, phát hiện 2) | `Banner.test.tsx` "App (khung) — /api/v1/me" (3 ca) và ca API tắt; `e2e/shell.spec.ts` ca API tắt | `npx vitest run`; `npx playwright test --repeat-each=5` | qua; `30 passed (14.0s)`; đỏ trước khi sửa `App.tsx` |
| Spec quên fixture `api` vẫn bị chặn gọi API thật | `apiGuard` tự bật (`e2e/fixtures.ts`) | spec tạm không dùng `api` | đỏ với "yêu cầu tới API chưa khai trong test" (đã xóa spec tạm) |
| Khóa mới sau xong/4xx, giữ sau lỗi chưa rõ, mới khi nội dung đổi; 401/403/429 không gửi lại; đóng màn không gửi thêm | các ca còn lại của `idempotency.test.ts` | như trên | qua |
| `errors.ts` có câu cho mọi mã lỗi trong `openapi.yaml` (đọc tệp) | `errors.test.ts` "có câu cho mọi mã lỗi…" | như trên | qua; xóa thử `last_owner` khỏi hợp đồng → đỏ (đã hoàn lại) |
| `out_of_stock` → "Không đủ hàng: còn {available}" | `errors.test.ts` (ca errorMessage) | như trên | "Không đủ hàng: còn 2"; thiếu số hoặc số là chữ → không chèn |
| Mọi mã của `errors.go` có trong hợp đồng | `TestErrorCodes_DeclaredInContract`, `TestErrorCodes_ContractListWellFormed` | `go test -race ./internal/httpapi/...` | `ok elimaz.com.vn/crm/internal/httpapi 16.4s`; trước khi khai `file_purpose_not_allowed` (Task 41): FAIL nêu đúng mã |
| Realtime: `catalog.product`, `inventory.balance`, `files.file` làm mới đúng truy vấn đang mở; resync làm mới mọi truy vấn mở | `queryClient.test.ts` (7 ca) | `npx vitest run` | qua |
| Khung tải ở 1512 và 390; API tắt → băng M0 hiện | `e2e/shell.spec.ts` (4 ca, Chromium) | `make console-e2e`; `npx playwright test --repeat-each=10` | `60 passed (20.4s)` |
| Menu trượt điện thoại trên WebKit: ☰ 44 px, nền mờ và Escape đóng, focus về ☰ | `e2e/shell.phone.spec.ts` (Chromium + WebKit) | như trên | qua |
| Menu: nhóm theo mockup, chỉ mục có quyền (cả phạm vi kho), aria-current trang con, trang không quyền, ô người dùng | `src/shell/Layout.test.tsx` (12 ca) | `npx vitest run` | qua |
| Lint chặn `globalThis.fetch`, `window.fetch`, `self.fetch` (ghi chú reviewer Task 17) | ca mới của `lint-guard.test.ts` | `npx vitest run` | qua (đỏ trước khi thêm luật) |
| tsc, eslint, prettier, bản dựng, gitleaks | — | `make console-check` | sạch; `vite build` 97,6 kB gzip; `no leaks found` |
| Mã sinh không đổi, không thay đổi phá vỡ | — | `make gen-check`, `make oasdiff` | không chênh; apicheck không báo gì |
| Go lint, đoạn trùng | — | `make lint`, `make jscpd` | `0 issues.`; không đoạn trùng mới |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Test trình duyệt chạy với API giả, không với API dev 4190 (quyết định 4).
- TanStack Virtual chưa cài (quyết định 2).
- Thêm `src/app/nav.ts`, `src/app/screens.tsx`, `src/shell/Layout.tsx` (khung điều hướng) ngoài danh sách Files; thêm
  `internal/httpapi/errorcodes_test.go` và `x-error-codes` trong `openapi.yaml` (quyết định 3); `Makefile` thêm
  `console-e2e`; `.gitignore` bỏ qua kết quả Playwright.
- `console/src/shell/Banner.test.tsx`: một dòng đổi `getByText("ELIMAZ")` thành `getAllByText(...).length > 0` vì chữ
  ELIMAZ nay có ở cả cột trái và thanh trên (điều kiện của test không nới).

## Việc phát hiện thêm

- **Test trình duyệt với API thật cần đường đăng nhập cho máy dev**: `cfaccess` chỉ tải khóa từ
  `https://<ACCESS_TEAM_DOMAIN>/cdn-cgi/access/certs`. Cần một task (vùng xác thực, rủi ro cao) thêm máy chủ khóa giả
  chỉ cho dev/test (như `testkit.AccessKeys` của test Go) và fixture Playwright đặt header `Cf-Access-Jwt-Assertion`.
- **`/api/v1/me` thiếu tên hiển thị và tên vai trò** để ô người dùng giống mockup ("Nguyễn Thị Lan · Chủ · Toàn shop").
- **Luật console mục 1** ("Chưa có màn hình nào được duyệt", tệp luật vùng console của cấu hình agent) đã cũ khi mockup
  M1 được duyệt; tệp đó thuộc phần chủ shop sửa. Đề xuất thêm luật: "mã lỗi API mới → thêm vào `x-error-codes` và
  `console/src/app/errors.ts`".
- `make console-e2e` chưa có trong CI; khi GitHub CI chạy được trình duyệt thì thêm.
- Khi `/api/v1/me` lỗi 5xx kéo dài (hết lượt thử lại), nội dung để trống mà không có câu báo; nên hiện một câu chung
  (ví dụ `TEXT_UNKNOWN_ERROR`) khi lỗi không phải 401/403 (reviewer lần 2, không bắt buộc).

## Reviewer

Lần 1 (trên bản đã gộp `75f8e58`): CHƯA ĐẠT, hai phát hiện về tính đúng:
1. `unwrap`: 2xx mà đọc thân hỏng bị coi là lỗi dứt khoát → khóa bị bỏ, bấm lại có thể ghi trùng. Đã sửa: mọi 2xx có
   `error` hoặc thiếu `data` → `NetworkError` (giữ khóa, gửi lại cùng khóa); test ở `errors.test.ts`, `idempotency.test.ts`.
2. `App.tsx`: định tuyến chạy với quyền rỗng khi `/api/v1/me` đang tải hoặc lỗi → báo nhầm "chưa được giao màn hình",
   "không tìm thấy". Đã sửa: chỉ vẽ route khi có `me.data`; test ở `Banner.test.tsx`, `e2e/shell.spec.ts`.
Ghi chú không bắt buộc đã làm: `apiGuard` tự bật cho mọi spec, `Object.hasOwn` trong `errorMessage`, JSDoc "mọi thứ đổi
theo thao tác nằm trong `body`", `sku_taken` → "Mã hàng đã có" (khớp kế hoạch Task 50).

Lần 2 (sau khi sửa và gộp main lần hai tới `3cfbcfc`): **ĐẠT**. Không phát hiện phải sửa; đã đối chiếu đủ 36 mã của
`errors.go` với `x-error-codes`. Ghi chú không bắt buộc: khi `/me` lỗi 5xx kéo dài thì phần nội dung để trống, không có
câu nào (ghi ở "Việc phát hiện thêm").

Kết luận: ĐẠT
