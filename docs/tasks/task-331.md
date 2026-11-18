# Task 331 — Màn Tệp bị mất (M1-53c)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 331, M1-53c; ca TP12 phần màn hình). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không sửa tay ký hiệu Go nào. `make gen` thêm vào mã sinh
  `internal/httpapi/gen/**` các phương thức `ListLostFiles`, `VerifyFiles` của `gen.Handler` (và kiểu của hai
  operation); `handler` (không xuất) nhận bản 501 qua `gen.UnimplementedHandler` có sẵn
  (`internal/httpapi/handlers_health.go:19`) tới Task 46. `routes_gen.go` thêm hai dòng route → quyền.
- Gói phụ thuộc ngược (go list): `internal/httpapi/gen` ← `internal/httpapi` ← `cmd/crm`, `tests/**` (chỉ thêm
  phương thức, không đổi chữ ký cũ). Test chạy: `go test -race ./internal/httpapi/...` (gồm test bảng route/quyền).
- Operation OpenAPI bị chạm, oasdiff: **thêm** `verifyFiles` (`POST /api/v1/platform/files/verify`, 202 `{jobId}`) và
  `listLostFiles` (`GET /api/v1/platform/files/lost?cursor&limit`), cả hai `x-permission: platform.files.manage`
  (quyền có sẵn trong `docs/quyen.md`), đúng Interfaces của Task 46. Chỉ thêm → oasdiff không phá vỡ.
- Chỗ dùng trên màn hình: màn mới `console/src/screens/lost-files/` (route.tsx tự đăng ký, nhóm "Hệ thống",
  path `/he-thong/tep-bi-mat`); client sinh `console/src/api/**` thêm `listLostFiles`, `verifyFiles`. Không màn
  nào khác đổi. Nút "Mở mẫu để tải lại" dẫn sang `/san-pham/:id` có sẵn (Task 324/325). `src/app/screens.test.tsx`
  (danh sách màn nạp được) thêm một dòng cho màn mới. Trong lúc Task 46 chưa xong, API thật trả 501 → màn hiện câu
  "Chức năng này chưa có" (`errors.ts`), không vẽ hành vi giả.
- Câu sqlc, bảng, pha migration: không có (máy chủ đọc/ghi thuộc Task 46).
- Test sẽ chạy: `cd console && npx vitest run src/screens/lost-files` (quy tắc màn), `npx playwright test
  e2e/lost-files.spec.ts` (TP12, 1512 và 390), `npx tsc --noEmit`, `npx eslint .`; `go test -race
  ./internal/httpapi/...` sau `make gen`; cuối cùng cổng kiểm theo task (`ci-task.sh`) một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục
  `console/src/shared`: `useWriteAction` (POST có khóa chống trùng, tự gửi lại), `unwrap`/`errorMessage` (lỗi API ra
  câu tiếng Việt), `queryKey`/`TOPICS.file` (tải lại theo báo hiệu realtime `files.file` và resync 20 s),
  `loadScreens` (tự đăng ký màn). e2e: `e2e/fixtures.ts` (`test`, `api`, `forEachViewport`, `writeRecorder`,
  `noHorizontalScroll`). Tìm "DateTimeFormat|toLocale" trong `console/src`: chưa có hàm định dạng giờ nào.
- Dùng lại / viết mới vì: dùng lại toàn bộ các mục trên; lớp CSS dùng lớp của khung (`page-h`, `tbl-frame`, `tbl`,
  `btn`, `card`, `empty`, `cell-a`) như màn Tồn kho. Viết mới trong thư mục màn (`lostFilesRules.ts`): tên mục đích
  tệp tiếng Việt và định dạng "HH:mm dd/MM" giờ Việt Nam — chỉ màn này dùng; màn khác cần thì lúc đó mới đưa lên
  `src/shared` (một nơi dùng chưa đủ lý do làm mã chung).

## Soát thiết kế

Task chỉ có màn hình và phần hợp đồng (không có handler, bảng, hàng đợi):

- 5.1–5.7, 5.10: không áp dụng — không có dữ liệu, bảng, transaction, sự kiện, lệnh ra ngoài, Valkey trong task này;
  đọc mẫu qua `catalog.products_v` là việc của máy chủ (Task 46).
- 5.8: màn hình chỉ gọi API qua client sinh; không có mã nghiệp vụ Go.
- 5.9: `verifyFiles` là đường ghi: hợp đồng có `IdempotencyKey` và `x-permission`; màn gửi qua `useWriteAction`
  (khóa sinh lúc bấm, gửi lại cùng khóa khi mạng/bận). `listLostFiles` chỉ đọc, có `x-permission`.
- 5.11: không thêm thư viện. 5.12: theo mẫu màn Tồn kho (Task 51) và quy ước route.tsx (Task 393).
- 6.1: hai người bấm "Kiểm lại" cùng lúc — máy chủ chỉ giữ một lệnh `file.verify` mỗi tenant (Task 46); màn khóa nút
  khi đang gửi và sau khi máy chủ nhận. 6.2: bấm hai lần → cùng promise (`useWriteAction`), mạng chập → cùng khóa.
- 6.3–6.11: không áp dụng — không chạm database, việc nền, tiền, quy luật vùng rủi ro cao ở task này.
- 6.12: màn không ghi log; dữ liệu hiện ra chỉ là mục đích tệp, mã mẫu, giờ (không có dữ liệu cá nhân).
- 6.13: menu chỉ hiện khi có `platform.files.manage`; API kiểm quyền (route → quyền sinh từ `x-permission`); lỗi từ
  API (403, 5xx) hiện câu lỗi riêng trên màn (có ca test).

## Quyết định thay chủ shop

- Lượt 2: phiên trước bị dừng giữa một lần gộp chưa commit với `main` cũ (6fba6c3, thiếu Task 389). Chọn bỏ lần gộp
  dở (`git merge --abort`; chưa có commit nên đảo ngược được) và gộp lại với `main` hiện tại, để chỉ có một commit gộp
  và reviewer duyệt đúng bản có Task 389. Phương án khác: commit lần gộp cũ rồi gộp thêm lần nữa (hai lần gộp, trái
  yêu cầu "chỉ một lần").

1. **Task 46 (máy chủ `verifyFiles`/`listLostFiles`) chưa làm, chưa có trong `openapi.yaml`.** Chọn: thêm **hợp đồng**
   hai operation đúng Interfaces của Task 46 vào `openapi.yaml`, `make gen`; máy chủ trả 501 (`UnimplementedHandler`)
   tới khi Task 46 viết handler — giống tiền lệ `healthDeep`, `getPlatformHealth` (Task 6, 19, 20). Màn và e2e chạy
   trên API giả đúng kiểu client sinh. Vì sao: đúng spec, chỉ thêm (oasdiff không phá vỡ), dễ đảo ngược, Task 46
   không phải viết lại hợp đồng. Phương án khác: dừng chờ Task 46 (chậm cả mốc), hoặc tự viết handler (ngoài phạm vi,
   đụng việc của Task 46).
2. **Dạng trả của `listLostFiles`:** kế hoạch ghi `[{fileId, purpose, productId?, productCode?, lostAt}]` kèm
   `cursor&limit`. Chọn trang `{items: [...], nextCursor?}` như `BalancePage` (mọi danh sách có con trỏ trong hợp
   đồng đều dạng này; mảng trần không có chỗ trả con trỏ trang sau). Trường của từng dòng giữ đúng kế hoạch.
3. **Thư mục màn:** kế hoạch ghi `console/src/screens/health/LostFilesPage.tsx` và sửa `src/app/screens.tsx`; từ Task
   393 màn tự đăng ký bằng `src/screens/<tên>/route.tsx` và Task 53, 330 cũng đặt màn ở `health/` (ba task song song sẽ
   giành một `route.tsx`). Chọn thư mục riêng `console/src/screens/lost-files/` (`LostFilesPage.tsx`, `route.tsx`).
4. **Trạng thái B "Đang kiểm lại":** API chỉ trả `jobId`, chưa có API hỏi một việc đã xong chưa. Chọn: sau khi máy
   chủ nhận (202), màn hiện thẻ "Đang kiểm toàn bộ tệp…" (nút trong thẻ bị khóa, nút đầu trang ẩn để không có hai
   nút cùng tên) tới khi rời trang; danh sách tự tải lại theo báo
   hiệu `files.file` và lưới an toàn 20 s của khung — đúng câu chữ mockup "Có thể rời trang; danh sách tự cập nhật
   khi xong". Bấm lại sau khi quay lại trang là an toàn (máy chủ một lệnh mỗi tenant). Phương án khác: thêm API trạng
   thái việc (ngoài phạm vi, đổi hợp đồng thêm).
5. **Tệp không gắn mẫu** (ví dụ tệp nhập Excel): cột "Thuộc về" hiện "—", không có nút (mockup chỉ vẽ ảnh sản phẩm).

## Lượt 2: gộp main (19/11)

- Gộp `main` (13a9bf9: có Task 330 màn Sự kiện lỗi, Task 59 hợp đồng M2, Task 389 test đăng ký màn theo thư mục) vào
  nhánh, một lần, bằng `git merge main`. Lần gộp dở trước đó (phiên bị dừng 00:35) dựa trên 6fba6c3, chưa có Task 389,
  chưa commit: đã `git merge --abort` rồi gộp lại với `main` mới nhất (xem Quyết định thay chủ shop).
- Xung đột: `openapi.yaml` tự gộp được (giữ cả hai bên). Mã sinh (`internal/httpapi/gen/**`, `routes_gen.go`,
  `console/src/api/{index,sdk.gen}.ts`): không sửa tay — lấy bản `main` rồi `make gen` từ `openapi.yaml` đã gộp;
  phần khác `main` trong mã sinh chỉ còn hai operation `listLostFiles`, `verifyFiles` như lượt 1.
- `console/src/app/screens.test.tsx`: lấy bản `main` (Task 389: tệp này chỉ kiểm quy ước). Thêm
  `console/src/screens/lost-files/route.test.tsx` (test đăng ký riêng của màn, theo mẫu `errors/route.test.tsx`).
- Không đổi hành vi, không đổi mã viết tay của màn; Ảnh hưởng, Tái dùng, Soát thiết kế của lượt 1 giữ nguyên
  (thêm đúng một tệp test `lost-files/route.test.tsx`, dùng lại `routeRows` của `src/app/routeTestkit.ts`).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Màn Tệp bị mất tự đăng ký đúng path, nhãn, nhóm, quyền, order 30 | `lost-files/route.test.tsx › route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order` | `npx vitest run src/app/screens.test.tsx src/screens/lost-files src/screens/errors` | `Test Files 6 passed (6)`, `Tests 44 passed (44)` |
| Hai màn Hệ thống vẫn chạy sau gộp | `e2e/lost-files.spec.ts`, `e2e/errors.spec.ts` (1512, 390) | `CRM_E2E_PORT=5131 npx playwright test e2e/lost-files.spec.ts e2e/errors.spec.ts --project=chromium` | `16 passed (16.2s)` |
| Kiểu TS khớp client sinh lại | — | `npx tsc --noEmit` | không lỗi |
| Cổng theo task | cổng tĩnh, gen-check, oasdiff, test gói đổi | `ci-task.sh` (chạy một lần, trên bản gộp đã stage) | build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard qua; test toàn bộ (bản gộp đổi migration/go.mod/testkit) `51` gói `ok`, không `FAIL`; console-check đỏ một lần vì tệp tạm `lint-guard-sample-*.ts` do `lint-guard.test.ts` tạo khi tôi chạy vitest song song, chạy lại `make console-check`: `Tests 334 passed (334)`, `✓ built`, `no leaks found`, thoát 0 |

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test viết trước, đỏ trước khi có mã | `lostFilesRules.test.ts`, `e2e/lost-files.spec.ts` | `npx vitest run src/screens/lost-files`; `npx tsc --noEmit` | trước khi có mã: `Test Files 1 failed`; `TS2305: Module '"../src/api"' has no exported member 'LostFile'`, `TS2307: Cannot find module './lostFilesRules'` |
| TP12: 2 tệp mất → 2 dòng có mã mẫu (link `/san-pham/<id>`), "Mở mẫu để tải lại", giờ "03:00 18/11"; "Kiểm lại toàn bộ tệp" (bấm đúp) gọi `verifyFiles` đúng một lần, POST có `Idempotency-Key`, rồi hiện "Đang kiểm", nút khóa; không cuộn ngang; 1512 và 390 | `TP12: 2 tệp mất → 2 dòng có mã mẫu, nút mở mẫu để tải lại; Kiểm lại gọi verifyFiles một lần` (2 khổ) | `CRM_E2E_PORT=5131 npx playwright test e2e/lost-files.spec.ts --project=chromium` | `✓ 1512 × 767 › TP12…`, `✓ 390 × 844 › TP12…`; `7 passed (6.6s)` |
| Trạng thái C trống; `verifyFiles` lỗi 403 → câu báo, không "Đang kiểm", bấm lại được với khóa mới | `không có tệp mất → trạng thái trống; Kiểm lại lỗi → câu báo, nút bấm lại được` (2 khổ) | như trên | `✓` cả 1512 và 390 |
| Tệp không gắn mẫu: "—", không link; mục đích lạ hiện nguyên khóa | `tệp không gắn mẫu: Thuộc về là —, không có nút; tên mục đích lạ hiện nguyên khóa` | như trên | `✓` |
| `listLostFiles` lỗi → câu lỗi, không bảng | `danh sách lỗi (403) → câu lỗi, không có bảng` | như trên | `✓` |
| Không có `platform.files.manage` → không có mục menu | `không có quyền platform.files.manage → không có mục Tệp bị mất trên menu` | như trên | `✓` |
| Tên mục đích, giờ Việt Nam (qua nửa đêm, sang năm), chuỗi hỏng → "—" | `purposeLabel`, `lostAtText` (3 ca) | `npx vitest run src/screens/lost-files` | `Tests 3 passed (3)` |
| Màn tự đăng ký đúng path, nhóm, quyền (lượt 1; từ lượt 2 thay bằng `lost-files/route.test.tsx`, vì `screens.test.tsx` của Task 389 chỉ kiểm quy ước) | `SCREENS nạp từ src/screens/*/route.tsx › đủ các màn đã có…` | `make console-check` (vitest 269 ca, tsc, eslint, build) | `Tests 269 passed` sau khi thêm dòng; build `✓ built`, gitleaks `no leaks found` |
| Hợp đồng thêm, không phá vỡ; mã sinh khớp; route → quyền sinh đúng | `make gen-check`, `make oasdiff OASDIFF_BASE=-base-ref=<điểm tách>`; `routes_gen.go` có `listLostFiles`/`verifyFiles` → `platform.files.manage`, `verifyFiles` `IdempotencyMode: "required"` | như cột Test | gen-check qua (sau stage); apicheck không báo phá vỡ |
| Máy chủ vẫn chạy, operation mới 501 không làm hỏng route/test cũ | `go test -race ./internal/httpapi/...`, `./cmd/crm`, `./tests/arch/...` | `go test -race -count=1 …` | `ok internal/httpapi 197.899s`, `ok cmd/crm`, `ok tests/arch 23.021s` |
| Cổng tĩnh như `ci-task.sh` | build, vet, lint, sqlc-vet, catalog-check, jscpd, gitleaks | `make <cổng>` từng cái (hook của worktree chặn gọi thẳng `ci-task.sh`, xem Việc phát hiện thêm) | tất cả qua |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thư mục màn `lost-files/` thay vì `health/`, tự đăng ký bằng `route.tsx` thay vì sửa `src/app/screens.tsx`
  (Quyết định 3).
- Thêm hợp đồng `verifyFiles`, `listLostFiles` vào `openapi.yaml` (vốn thuộc Task 46), dạng trang `{items,
  nextCursor}` (Quyết định 1, 2).

## Việc phát hiện thêm

- Lượt 2: `ImageManager.test.tsx › dời ảnh 1 sang phải …` (không thuộc task) đỏ một lần khi chạy cả bộ vitest song song
  với `ci-task.sh`; chạy riêng `19 passed`. Có thể là test phụ thuộc thời gian khi máy tải nặng.

- Task 46 cần hiện thực `ListLostFiles`, `VerifyFiles` theo hợp đồng đã thêm ở đây (hiện trả 501), và nên phát báo
  hiệu realtime `files.file` khi `file.verify` đánh dấu xong để màn tự cập nhật ngay (không phải chờ 20 s).
- Hook Bash (`bashguard.py`, mẫu `SANCTIONED`) chưa cho chạy `ci-task.sh` dù CLAUDE.md bảo chạy nó trước review; đã
  chạy tay từng cổng tương đương (không vượt hook). Chủ shop/điều phối viên thêm `ci-task` vào mẫu cho phép.

## Reviewer

Lượt 2: ĐẠT (agent `reviewer`, lần 1, trên bản gộp đã stage, so với 13a9bf9).

- Phát hiện phải sửa: không có. Phần khác 13a9bf9 đúng 24 tệp của lượt 1, trừ `screens.test.tsx` (lấy bản `main`) và
  thêm `lost-files/route.test.tsx`; `openapi.yaml` chỉ thêm, không mất operation nào của `main`; mã sinh chỉ khác ở hai
  operation mới (`oas_router_gen.go` xếp lại nhánh `/platform/files/`); `route.test.tsx` so đủ 7 trường với `route.tsx`.
- Ghi chú không bắt buộc đã làm: ghi rõ dòng bằng chứng `SCREENS…` của lượt 1 nay thay bằng `route.test.tsx`.

Lượt 1:

Kết luận: ĐẠT (agent `reviewer`, lần 1, trên phần đã stage sau khi gộp main — main không có gì mới).

- Phát hiện phải sửa: không có. Đủ yêu cầu (hai operation có `x-permission`, `verifyFiles` có `IdempotencyKey`, 202
  `{jobId}`; ba trạng thái A, B, C khớp mockup); bằng chứng trỏ tới test có thật và kiểm đúng; Ảnh hưởng khớp hai
  chiều; không có test bị nới; năm quyết định thay chủ shop không trái spec, không mở lỗ an toàn.
- Ghi chú không bắt buộc (không sửa): nút "Tải thêm 50" chưa có ca e2e (giống `Balances.tsx`); Task 46 phải giữ
  dạng `LostFilePage` và nên phát báo hiệu `files.file` khi `file.verify` xong (đã ghi ở Việc phát hiện thêm).
