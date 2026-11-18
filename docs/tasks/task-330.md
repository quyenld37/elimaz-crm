# Task 330 — Màn Sự kiện lỗi (M1-53b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 330, M1-53b). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task chỉ chạm `console/` và tệp task).
- Gói phụ thuộc ngược (go list): không áp dụng, không đổi gói Go nào.
- Operation OpenAPI bị chạm, oasdiff: chỉ đọc `getPlatformHealth` (`GET /api/v1/platform/health`, `x-permission: platform.health.read`). Không sửa `openapi.yaml`: `ErrorSummary` đã có `fingerprint, name, role, message (≤300, đã qua observability.ScrubMessage — Task 319), count, firstSeenAt, lastSeenAt`, nên "lần đầu" và nội dung lỗi đã có trong API tóm tắt; oasdiff không đổi.
- Chỗ dùng trên màn hình: thư mục màn mới `console/src/screens/errors/` (`route.tsx` tự đăng ký, `ErrorsPage.tsx`, `errorsRules.ts`, `errors.css`, test đơn vị) và `console/e2e/errors.spec.ts`. Tệp chung chỉ đổi một dòng test: `console/src/app/screens.test.tsx` (danh sách màn mong đợi thêm `/he-thong/su-kien-loi`; tương đương "thêm dòng Screen" của kế hoạch). Mã chung (`src/app/*.ts(x)` khác, `styles.css`, `e2e/fixtures.ts`) giữ nguyên; menu nhóm "Hệ thống" tự có mục "Sự kiện lỗi" cho người có `platform.health.read`.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run` (đơn vị của màn + kiểm nạp màn `src/app/screens.test.tsx`), `npx tsc --noEmit`, `npx eslint .`, `npx playwright test e2e/errors.spec.ts` (chromium, 1512 × 767 và 390 × 844, thêm 360); cổng kiểm theo task (ci-task.sh) một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` phần console/src/shared (`unwrap`, `errorMessage`, `ApiError`, `queryKey`, `onRealtimeResync`, `hasPermission`, quy ước `loadScreens`); `console/e2e/fixtures.ts` (`forEachViewport`, `noHorizontalScroll`, `json`, `api.live`); tìm hàm định dạng giờ, số (`grep -rn "toLocale\|DateTimeFormat" console/src`): chưa có hàm dùng chung nào.
- Dùng lại / viết mới vì: dùng lại `unwrap`/`errorMessage`/`ApiError` (lỗi API), `queryKey` và `onRealtimeResync` (resync tải lại mọi truy vấn đang mở, màn không cần mã riêng), lớp CSS của khung (`tbl`, `tbl-frame`, `clamp2`, `wrap-any`, `empty`, `page-h`, `btn-quiet`), fixture e2e. Viết mới `errorsRules.ts` (giờ "HH:mm dd/MM" theo giờ Việt Nam, số lần "1.204", mã nhận diện 6 ký tự đầu) vì chưa có hàm chung và hiện chỉ màn này dùng; màn Sức khỏe (Task 53, song song) có thể cần định dạng giờ tương tự → ghi ở Việc phát hiện thêm, không gom đoán trước.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.10: không áp dụng: màn chỉ đọc, không có dữ liệu mới, bảng, transaction, sự kiện, gọi ra ngoài hay đường ghi.
- 5.11: không thêm thư viện (React, TanStack Query, React Router đã chọn ở `docs/chon-cong-cu-console.md`).
- 5.12: theo màn mẫu tự đăng ký (`src/app/loadScreens.ts`, màn Tồn kho Task 51): `route.tsx` + trang + luật thuần có test đơn vị + e2e với API giả.
- 6.1–6.11: không áp dụng: không ghi, không khóa, không việc nền, không tiền. Tải lại mỗi 20 s (ADR-42) bằng `refetchInterval` của TanStack Query (chỉ khi màn đang mở), resync qua `onRealtimeResync` có sẵn.
- 6.12 Dữ liệu cá nhân: nội dung lỗi đã che ở máy chủ (`observability.ScrubMessage`, Task 319); màn chỉ hiện, không log, không gửi đi đâu.
- 6.13 Quyền: API kiểm `platform.health.read`; màn khai cùng khóa quyền nên người không có quyền không thấy mục menu, mở thẳng URL thì gặp trang "Không có trang này, hoặc bạn chưa được cấp quyền xem" của khung; API trả 403 khi đang mở (bị thu quyền) → khối "Không có quyền" như mockup Sức khỏe trạng thái C.

## Quyết định thay chủ shop

1. **Thư mục màn `console/src/screens/errors/` thay vì `screens/health/ErrorsPage.tsx` của kế hoạch.** Vì: Task 53 (Sức khỏe) chạy song song và sẽ tạo `screens/health/route.tsx`; hai task cùng tạo một tệp là xung đột chắc chắn; điều phối giao "thư mục màn riêng". Phương án khác: chung `health/` rồi gộp hai `route.tsx` lúc merge. Đảo ngược dễ: dời thư mục.
2. **Đường dẫn `/he-thong/su-kien-loi`, nhóm menu "Hệ thống", thứ tự 20** (mockup `khung.js`: Sức khỏe → Sự kiện lỗi → Tệp bị mất). Phương án khác: `/su-kien-loi` ở gốc.
3. **Không quyền, mở thẳng URL: giữ trang "không tìm thấy / chưa được cấp quyền" chung của khung**, như màn Vai trò, Nhân viên đang làm (e2e `roles.spec.ts`, `staff.spec.ts` kiểm câu này). Không sửa `router.tsx` chung trong task này vì Task 53 có thể đổi cùng chỗ (kế hoạch Task 53: "URL trực tiếp hiện Không có quyền") → nếu Task 53 đổi router theo cách chung, màn này tự hưởng. Màn vẫn có khối "Không có quyền" (câu như mockup Sức khỏe C) khi API trả 403. Phương án khác: thêm route "Không có quyền" cho mọi màn trong `router.tsx`.
4. **Giờ hiện theo múi Asia/Ho_Chi_Minh** cố định (shop ở Việt Nam, test ổn định không phụ thuộc máy). Phương án khác: theo múi của trình duyệt.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test thất bại trước khi có mã | `errorsRules.test.ts`, `ErrorsPage.test.tsx` | `cd console && npx vitest run src/screens/errors` (chưa có mã) | `Test Files 2 failed (2)` |
| e2e không rỗng: bỏ `route.tsx` thì đỏ | `e2e/errors.spec.ts` | `CRM_E2E_PORT=5130 npx playwright test e2e/errors.spec.ts --project=chromium` (tạm dời route.tsx) | `7 failed, 2 passed` (2 ca không quyền vốn đúng nhờ khung) |
| 10 loại lỗi hiện đủ, đúng thứ tự máy chủ, tên, mã 6 ký tự, tiến trình, số lần "1.204", lần đầu, lần cuối; 1512 và 390 | e2e `đủ 10 loại lỗi; nội dung 300 ký tự cắt 2 dòng, bấm thì mở đủ, không tràn ngang` (1512 × 767, 390 × 844); đơn vị `hiện đủ 10 loại theo thứ tự máy chủ, có tiến trình, số lần, lần đầu, lần cuối, mã nhận diện` | `CRM_E2E_PORT=5130 npx playwright test e2e/errors.spec.ts --project=chromium`; `npx vitest run src/screens/errors` | `9 passed`; `Tests 68 passed` (cùng src/app) |
| `message` 300 ký tự không tràn: cắt ≤ 2 dòng, bấm mở đủ, không cuộn ngang (cả 360) | e2e trên + `360 × 740 › điện thoại hẹp: thẻ lỗi không tràn ngang, kể cả khi mở nội dung dài`; đơn vị `nội dung dài 300 ký tự có đủ trong khối mở được (details)` | như trên | qua |
| Không quyền `platform.health.read` → không có mục menu, URL trực tiếp không mở màn, không gọi API | e2e `không quyền platform.health.read → không có mục menu, URL trực tiếp không mở màn, không gọi API` (1512, 390); đơn vị `API trả 403 → khối Không có quyền`, `bị thu quyền khi đang mở (lần tải lại trả 403) → ẩn bảng cũ, hiện Không có quyền` (thêm sau ghi chú của reviewer lần 2; bỏ điều kiện `forbidden` trong `ErrorsPage.tsx` thì ca này đỏ: `1 failed | 13 passed`) | như trên | qua |
| Có quyền → mục trong nhóm "Hệ thống" | e2e `có quyền → mục Sự kiện lỗi trong nhóm Hệ thống của menu`; `src/app/screens.test.tsx` | như trên | qua |
| Tự tải lại mỗi 20 s (ADR-42); nút Tải lại | e2e `tự tải lại mỗi 20 giây` (page.clock); đơn vị `tự tải lại mỗi 20 giây`, `nút Tải lại gọi API lần nữa` | như trên | qua |
| Trạng thái B trống có giờ kiểm; lỗi 500 → hộp lỗi | e2e `không có lỗi → trạng thái trống có giờ kiểm`; đơn vị `không có lỗi → …`, `API lỗi 500 → hộp lỗi có câu tiếng Việt` | như trên | qua |
| Giờ Việt Nam, số có dấu chấm, mã 6 ký tự | `errorsRules.test.ts` (4 describe, 6 ca) | `npx vitest run src/screens/errors` | qua |
| Đúng mockup | ảnh chụp 1512 và 390 so với `docs/mockups/m1/png/su-kien-loi-{1512,390}.png` (cột, chữ, cắt 2 dòng, thẻ trên điện thoại khớp) | Playwright chụp tạm, không commit | khớp |
| Cổng tĩnh như ci-task.sh | — | `make build vet lint gen-check sqlc-vet catalog-check jscpd gitleaks console-check`, `make oasdiff OASDIFF_BASE=-base-ref=<điểm tách>`, `go test ./tests/arch/...` (chạy từng lệnh vì hook bashguard chặn gọi `scripts/agent/ci-task.sh`, lệnh này chưa có trong danh sách được phép) | tất cả XANH; `console-check`: 24 tệp, 279 test qua. Chưa chạy được `test-guard.sh` (cùng lý do; git hook commit chạy kiểm của nó) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thư mục `console/src/screens/errors/` thay vì `screens/health/ErrorsPage.tsx`; không có `console/src/app/screens.tsx` (đã thay bằng tự đăng ký `route.tsx`, Task 393). Xem Quyết định 1.
- Step 1 ("không quyền → như màn Sức khỏe", tức URL thẳng hiện "Không có quyền"): mở thẳng URL khi không có quyền hiện trang chung của khung "Không có trang này, hoặc bạn chưa được cấp quyền xem", như Vai trò, Nhân viên; khối "Không có quyền" chỉ hiện khi API trả 403. Xem Quyết định 3.

## Việc phát hiện thêm

- Màn Sức khỏe (Task 53) và màn này cùng cần định dạng giờ "HH:mm dd/MM": khi cả hai đã gộp, cân nhắc gom về `console/src/shared`.
- Hook `bashguard.py` (danh sách `SANCTIONED`) chưa cho chạy `scripts/agent/ci-task.sh`, nên phiên làm task không gọi được cổng kiểm theo task; cần chủ shop thêm vào danh sách (tệp quy trình, agent không được sửa).
- Trang thiếu quyền khi mở thẳng URL: kế hoạch Task 53 muốn "Không có quyền" riêng; nếu làm thì nên làm chung ở `router.tsx` cho mọi màn (Quyết định 3).

## Reviewer

Kết luận: ĐẠT (lần 3).

- Lần 1: CẦN SỬA — bảng bằng chứng ghi `errorsRules.test.ts` 7 ca, thực tế 6. Đã sửa; thêm dòng Quyết định 3 vào "Khác kế hoạch" theo gợi ý.
- Lần 2: ĐẠT; ghi chú không bắt buộc: lần tải lại trả 403 vẫn hiện bảng cũ dưới khối "Không có quyền". Đã sửa (`ErrorsPage.tsx`: `forbidden ? []`) kèm test đơn vị (đỏ khi bỏ sửa).
- Lần 3: ĐẠT, không có phát hiện phải sửa. Ghi chú: resync dựa vào `onRealtimeResync` sẵn có (không cần test riêng); lỗi 500 lúc tự tải lại giữ bảng cũ kèm hộp lỗi là chấp nhận được; `test-guard.sh` để git hook kiểm lúc commit.
