# Chọn công cụ cho màn hình nhân viên (`console/`)

Task 17 (M0-17), 18/11/2026. Mỗi lựa chọn ghi bốn điều của ADR-32: (1) vì sao đúng với việc, (2) cái giá vận hành,
(3) mục tiêu bằng số và cách đo, (4) ngưỡng để đổi. Phiên bản ghim đúng trong `console/package.json` (`--save-exact`)
và `console/package-lock.json`; thông tin giấy phép, ngày phát hành đọc từ npm (`npm view`) ngày 18/11/2026.
"Quen dùng", "có sẵn" không phải lý do (ADR-32).

Bộ định tuyến, thư viện lấy dữ liệu, danh sách ảo, test trên trình duyệt: chọn ở Task 48 (M1-48), 18/11/2026, theo đề
xuất Q31 của kế hoạch M1; xem mục "M1" cuối tệp. Cả bốn đạt bốn điều ADR-32, không phải dừng hỏi.

Node chạy công cụ: v26.12.0 (máy dev). Bản dựng là tệp tĩnh; Node không chạy trên máy chủ (ADR-34).

## Khung: React 19.3.1 + Vite 8.3.8 (`@vitejs/plugin-react` 6.3.0)

Đã chốt ở ADR-34; ghi phiên bản. MIT. Bản dựng `vite build` → `console/dist`, 233 kB JS (74 kB gzip) cho khung rỗng.
Ngưỡng xem lại: bản dựng của khung vượt 400 kB gzip, hoặc Vite ngừng hỗ trợ bản lớn đang dùng.

## Bộ sinh client từ OpenAPI: `@hey-api/openapi-ts` 0.99.0 (MIT)

1. **Vì sao:** sinh từ đúng `openapi/openapi.yaml` (nguồn của ogen phía Go) thành kiểu TypeScript cho mọi operation,
   tham số và header (ví dụ `Idempotency-Key` là trường bắt buộc trong kiểu của mọi route ghi, nên quên khóa là lỗi
   biên dịch) cùng hàm gọi (`healthLive`, `getMe`, …) dùng `fetch` của trình duyệt; không thêm thư viện chạy lúc
   chạy (client fetch được sinh vào `console/src/api/client`). Mã sinh qua `tsc` strict của TypeScript 7.0.2 không
   cần nới luật nào. `make gen` sinh lại; `make gen-check` đỏ khi lệch (hai lần sinh liên tiếp giống hệt nhau, đã thử).
2. **Giá vận hành:** một gói dev (10 phụ thuộc). Bản 0.x: bản lớn có thể đổi cách sinh; ghim đúng bản, nâng có chủ ý.
3. **Đo:** `make gen-check` không chênh; `tsc --noEmit` trên mã sinh sạch; sinh mất khoảng 50 ms.
4. **Ngưỡng đổi:** dự án ngừng phát hành quá 6 tháng, hoặc mã sinh cần nới luật `tsc`. Thay thế: `openapi-typescript`
   7.13.0 + `openapi-fetch` 0.17.0 (MIT; chỉ sinh kiểu, client gọn nhất) — **không chọn lúc này** vì
   `openapi-typescript` đòi `typescript ^5.x` (peer) và bản cuối 11/02/2026, không khớp TypeScript 6/7 của dự án;
   `orval` 8.48.0 (MIT): mạnh khi đã chọn thư viện lấy dữ liệu (sinh hook React Query/SWR), chọn lại cùng lúc với thư
   viện lấy dữ liệu ở M1.

## Bộ chạy test: Vitest 5.1.0 (MIT), jsdom 31.0.1, Testing Library (`@testing-library/react` 16.3.3, `dom` 10.4.2)

1. **Vì sao:** dùng chính cấu hình, bộ biến đổi của Vite (không có cấu hình dựng thứ hai như Jest + Babel); chạy
   test lớp realtime trên máy chủ HTTP thật của Node (`node:http`) và `fetch` thật; test giao diện trong jsdom.
2. **Giá:** gói dev; không chạy trên máy chủ.
3. **Đo:** cả bộ của Task 17 chạy dưới 10 s (`npm test`).
4. **Ngưỡng đổi:** cần test trên trình duyệt thật (Safari, điện thoại): thêm Vitest browser mode hoặc Playwright cho
   các màn đó ở M1.

## Formatter: Prettier 3.10.1 (MIT)

1. **Vì sao:** một cách định dạng duy nhất cho TS, TSX, CSS, JSON, HTML; hey-api gọi Prettier để định dạng mã sinh
   (mã sinh ổn định giữa các lần sinh). Chỉ định dạng; lint là ESLint.
2. **Giá:** gói dev. Chạy bằng `npm run format`; `make console-check` chạy `prettier --check`.
3. **Đo:** `prettier --check .` sạch; định dạng một tệp dưới 1 s.
4. **Ngưỡng đổi:** Biome 2.6.8 (MIT/Apache-2.0, nhanh hơn nhiều) khi số tệp làm `--check` chậm quá 10 s, hoặc khi
   Biome thay được cả ESLint với đủ luật `strict-type-checked` (hôm nay chưa).
- **Hook:** kế hoạch muốn thêm formatter vào `PostToolUse` cho `console/**`. `scripts/agent/format.sh` thuộc phần
  cấu hình quy trình mà agent không được sửa (hook chặn); dòng cần thêm ghi ở mục "Việc phát hiện thêm" của
  `docs/tasks/task-17.md` cho chủ shop.

## Cổng kiểu và lint: TypeScript 7.0.2, ESLint 10.15.0, typescript-eslint 8.72.2

- `tsc --noEmit` chạy **TypeScript 7.0.2** (gói `@typescript/native` là bí danh npm của `typescript@7.0.2`; lệnh
  `npx tsc` trong `console/` là bản 7.0.2), `strict: true`, thêm `noUncheckedIndexedAccess`.
- **typescript-eslint 8.72.2 không chạy với TypeScript 7.0** (đã thử 18/11: gói tự dừng với lỗi "typescript-eslint
  does not support TS 7.0"; peer `typescript >=4.8.4 <6.1.0`). TypeScript 7.0 không có API JavaScript cho công cụ
  khác; API mới dự kiến ở 7.1. Cách chính hãng (bài công bố TypeScript 7.0, 08/07/2026, mục "Running side-by-side with
  TypeScript 6.0"): cài gói `@typescript/typescript6` (6.0.2, Apache-2.0) dưới tên `typescript`, để công cụ cần API
  (typescript-eslint) đọc kiểu bằng TypeScript 6, còn `tsc` là 7.0. Task 17 làm đúng cách đó: không hạ cổng `tsc`,
  không tắt luật nào. ESLint chạy `strict-type-checked` + `stylistic-type-checked` qua `projectService: true`,
  `react-hooks` (7.1.1), cấm `any`, cấm ép kiểu (`assertionStyle: "never"`), cấm `!`, cấm `fetch` thẳng ngoài
  `src/realtime` trong `src/**` trừ tệp test. Test `src/lint-guard.test.ts` đòi lint đỏ với `any`, `as`, `fetch`.
  Đây là **quyết định thay chủ shop** (kế hoạch ghi "không chạy được thì dừng, báo chủ shop"); xem tệp task.
- **Ngưỡng đổi:** khi typescript-eslint hỗ trợ TypeScript ≥ 7.1 (theo dõi typescript-eslint issue 10940): bỏ bí danh
  `typescript`, để cả hai đọc cùng 7.x. Khác biệt kiểu giữa 6.0 và 7.0 (nếu có) chỉ ảnh hưởng luật lint dựa trên kiểu;
  cổng kiểu là `tsc` 7.0.2.

## Máy chủ ngôn ngữ cho plugin `typescript-lsp`

- TypeScript 7.0.2 có máy chủ ngôn ngữ riêng: `tsc --lsp --stdio` (đã thử 18/11: trả lời `initialize` đúng chuẩn LSP).
- Với bí danh ở trên, gói `typescript` trong `console/node_modules` là TypeScript 6.0.2 có `tsserver`, nên
  `typescript-language-server` 6.1.0 (đã ghi ở `docs/phu-thuoc.md`, Task 0) cũng chạy được.
- **Chọn:** máy chủ riêng của TypeScript 7 (`console/node_modules/.bin/tsc --lsp --stdio`) khi plugin cho đặt lệnh;
  không thì `typescript-language-server --stdio` (đọc TypeScript 6 của bí danh). Bật plugin là sửa
  `.claude/settings.json`, phần agent không được sửa: chủ shop bật (ghi ở "Việc phát hiện thêm").

## Rủi ro đã nhận (npm audit, 18/11/2026)

`npm audit --omit=dev`: 0 lỗ hổng (bản dựng chỉ có `react`, `react-dom`). Phụ thuộc dev có báo cáo về ReDoS/CPU trong
`picomatch` ≤ 4.0.7 (chưa có bản vá; kéo theo bởi Vite, Vitest), `minimatch` < 11.0.1 (trong typescript-eslint 8.72.2;
có ở 8.73.0, kế hoạch ghim 8.72.2). Đầu vào của các gói này là mẫu đường dẫn trong cấu hình của chính repo, không phải
dữ liệu người ngoài; không chạy trên máy chủ. `js-yaml` 4.2.0 (đọc `openapi.yaml` trong bộ sinh) được ép lên 4.3.2
bằng `overrides`. Xem lại khi có bản vá `picomatch`, hoặc khi nâng typescript-eslint.

## M1 (Task 48, 18/11/2026): định tuyến, lấy dữ liệu, danh sách ảo, test trình duyệt

Số thông báo lỗ hổng năm 2026 đếm trên GitHub Advisory Database (`gh api /advisories?ecosystem=npm&affects=<gói>`)
ngày 18/11/2026. Phiên bản, giấy phép, ngày phát hành: `npm view`. Ghim đúng bản (`--save-exact`), cài với
`--ignore-scripts` (không gói nào cần chạy mã lúc cài).

### Bộ định tuyến: React Router 8.5.1 (`react-router`, MIT), chế độ khai báo

1. **Vì sao:** màn hình cần đường dẫn cho khoảng 12 màn M1 (có trang con như sửa mẫu), mục menu sáng theo đường dẫn,
   nút quay lại của trình duyệt, và bộ lọc nằm trong URL. Chế độ khai báo (`BrowserRouter`, `Routes`) làm đúng chừng đó;
   dữ liệu do TanStack Query lấy, nên không dùng loader/action, không dựng trên máy chủ (ADR-34). Một gói, không cần
   plugin Vite hay bộ sinh tuyến.
2. **Giá vận hành:** một gói chạy trong bản dựng; không có tiến trình máy chủ. Bản dựng của khung từ 74 kB lên 97,6 kB
   gzip (cùng TanStack Query và React Router).
3. **Đo:** đổi màn trong trình duyệt dưới 50 ms (không tải lại trang); test `Layout.test.tsx`, `shell.spec.ts`.
4. **Ngưỡng đổi:** cần kiểu chặt cho tham số tìm kiếm của nhiều màn lọc (lỗi do gõ sai tên tham số lặp lại) → TanStack
   Router; hoặc React Router có lỗ hổng trúng chế độ khai báo mà chưa vá trong 14 ngày.
- **Lỗ hổng 2026:** 18 thông báo cho `react-router`; **không cái nào trúng 8.5.1** (bản cao nhất trúng là `< 8.3.0`,
  GHSA-qwww-vcr4-c8h2, chế độ RSC). Hầu hết thuộc chế độ framework/SSR/RSC mà CRM không dùng; nhóm còn lại là chuyển
  hướng mở khi đưa chuỗi ngoài vào `navigate`/`<Link>` — luật trong `src/app/router.tsx`: không đưa chuỗi từ API, URL
  vào điều hướng mà không kiểm là đường nội bộ.
- **Đã cân nhắc:** TanStack Router 1.170.46 (MIT): kiểu chặt cho đường dẫn và tham số tìm kiếm là điểm mạnh thật,
  nhưng cần plugin sinh cây tuyến hoặc khai tay dài; 1 thông báo năm 2026 là **mã độc chuỗi cung ứng** (GHSA-g7cv-rxg3-
  hmpx, 11/05/2026: 84 bản độc trên 42 gói `@tanstack/*` của kho `TanStack/router`, đánh cắp khóa lúc cài). Gói của
  `TanStack/query` và `TanStack/virtual` không nằm trong danh sách. Viết tay bằng History API: tự lo khớp đường dẫn,
  trang con, nút quay lại — không đáng so với một gói đã ổn định.

### Lấy dữ liệu: TanStack Query 5.108.0 (`@tanstack/react-query`, MIT)

1. **Vì sao:** việc chính của màn CRM là "báo hiệu realtime → tải lại đúng danh sách đang mở". TanStack Query có đúng
   phép đó: `invalidateQueries({ queryKey: [chủ đề], refetchType: "active" })` chỉ tải lại truy vấn có màn đang xem,
   truy vấn khác chỉ bị đánh dấu cũ (`src/app/queryClient.ts`, test `queryClient.test.ts`); gộp yêu cầu trùng, hủy khi
   rời màn, giữ dữ liệu cũ khi đang tải lại. Đường ghi đi qua `useWriteAction` riêng (khóa chống trùng ADR-41), không
   dùng cơ chế thử lại của thư viện cho yêu cầu ghi.
2. **Giá:** một gói chạy trong bản dựng (lõi `@tanstack/query-core`); 0 thông báo lỗ hổng năm 2026.
3. **Đo:** báo hiệu `changed` tới truy vấn đang mở được tải lại trong một vòng sự kiện (test); không có yêu cầu thừa
   cho truy vấn không ai xem.
4. **Ngưỡng đổi:** cần bộ nhớ đệm chuẩn hóa theo đối tượng (một sản phẩm hiện ở nhiều danh sách, sửa một chỗ phải đổi
   mọi chỗ mà tải lại không kịp) → xem lại; hoặc bản 6 bỏ API đang dùng (`fetchQuery` đã báo bỏ, test dùng `query`).
- **Đã cân nhắc:** SWR 2.5.1 (MIT, 0 thông báo 2026): gọn hơn nhưng làm mới theo khóa riêng lẻ, không có phép "đánh dấu
  cũ cả nhóm theo tiền tố, chỉ tải lại cái đang mở" sẵn; hey-api có plugin sinh `queryOptions` cho TanStack Query nhưng
  khóa sinh theo tên operation, không theo chủ đề realtime, nên chưa dùng (xem lại khi số màn lớn). orval: chỉ đáng khi
  đổi bộ sinh client; giữ hey-api.

### Danh sách ảo: TanStack Virtual 3.14.26 (`@tanstack/react-virtual`, MIT) — chọn, cài ở màn đầu tiên cần

1. **Vì sao:** ADR-34 đòi danh sách dài dùng danh sách ảo (sản phẩm, tồn, sổ kho có thể hàng nghìn dòng); thư viện chỉ
   tính dòng nào đang hiện, không áp kiểu dáng, hợp với bảng theo mockup M1 (bảng thành thẻ trên điện thoại).
2. **Giá:** một gói nhỏ trong bản dựng; 0 thông báo 2026; kho `TanStack/virtual` không nằm trong vụ mã độc 05/2026.
3. **Đo:** cuộn danh sách 5.000 dòng giữ 60 khung hình/giây trên điện thoại tầm trung (đo ở task màn đầu tiên dùng).
4. **Ngưỡng đổi:** danh sách nào cũng dưới vài trăm dòng có phân trang → không cần; cần lưới hai chiều phức tạp → xem lại.
- **Chưa cài ở Task 48:** chưa có màn nào dùng; cài đúng bản này ở task màn danh sách đầu tiên (M1-50 hoặc M1-51) và
  thêm dòng vào `docs/phu-thuoc.md` lúc đó.

### Test trên trình duyệt: Playwright 1.65.1 (`@playwright/test`, Apache-2.0)

1. **Vì sao:** kế hoạch đòi Chromium cho mọi spec và **WebKit** (Safari điện thoại) cho spec `.phone.spec.ts`, ở 1512 ×
   767 và 390 × 844; Playwright chạy được cả hai bằng một API, chặn và giả API bằng `page.route` (`e2e/fixtures.ts`),
   tự bật Vite (`webServer`). Website của shop cũng dùng Playwright, nhưng đó không phải lý do (ADR-32).
2. **Giá:** gói dev; trình duyệt tải về `~/Library/Caches/ms-playwright` (Chromium 1255, WebKit 2388); không vào bản
   dựng, không chạy trên máy chủ. CI (GitHub) chưa chạy e2e; `make console-e2e` chạy trên máy dev.
3. **Đo:** 6 test khung chạy dưới 15 s (`npx playwright test`).
4. **Ngưỡng đổi:** Vitest browser mode (dùng chính Playwright làm bộ điều khiển) khi muốn test thành phần trong trình duyệt
   thật mà không cần cả trang. Cypress không chạy WebKit ổn định nên không chọn.
- **Lỗ hổng 2026:** 0 cho `@playwright/test` và `playwright` (thông báo gần nhất 10/2025, tải trình duyệt không kiểm
  chứng chỉ, đã vá trước 1.55).

### Phông chữ: `@fontsource/mulish` 5.3.0, `@fontsource/playfair-display` 5.3.0 (OFL-1.1)

Mockup M1 dùng Mulish (tiêu đề, menu) và Playfair Display (chữ ELIMAZ) từ Google Fonts; CSP của `crm-console`
(`default-src 'self'`, `docs/RUNBOOK.md`) chặn tải phông từ ngoài. Hai gói này đưa tệp phông vào bản dựng (chỉ bộ latin
và vietnamese, Mulish 400/600/700, Playfair 500: khoảng 60 kB woff2). 0 thông báo lỗ hổng. Ngưỡng đổi: hệ thiết kế
đổi phông.
