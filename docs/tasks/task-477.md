# Task 477 — Sửa TestRealContractWritesRequired: route ghi miễn khóa có lý do, Auth service_key

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

Đề bài (không có mục trong kế hoạch): main đỏ ở `tools/routegen` `TestRealContractWritesRequired`, viết từ M0, chỉ cho mọi route ghi `required` + `access` trừ `createServiceKey`. Task 192 thêm `webQuote` (exempt có lý do, `service_key`). Sửa test theo luật hợp đồng API hiện hành (luật openapi 3 và 6), giữ chặt bằng ca âm; không đổi `openapi.yaml`, không đổi mã sinh.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ sửa `tools/routegen/main_test.go` (gói `main`, không ai nhập); `main.go` không đổi.
- Gói phụ thuộc ngược (go list): không có (`tools/routegen` là lệnh, chỉ `make gen` chạy nó).
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`, không đổi `routes_gen.go`. Test đọc hợp đồng thật, hiện có 2 route ghi không `required`: `createServiceKey` (no-replay, Task 189) và `webQuote` (exempt, service_key, Task 192).
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `go test -count=1 ./tools/routegen/` (cả gói, nhỏ, dưới 1 s), ci-task một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có bộ đọc dòng route sinh; trong gói đã có hằng `modeRequired/modeExempt/modeNoReplay`, `authAccess/authServiceKey`, `webPathPrefix` (main.go).
- Dùng lại / viết mới vì: dùng lại các hằng đó trong test (không gõ lại chuỗi); viết mới một hàm test nhỏ `writeRouteProblems` đọc các trường của một dòng route ghi đã sinh và kiểm theo luật openapi 3, 6 — dùng chung cho hợp đồng thật và ca âm.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.12: không áp dụng: chỉ sửa test của công cụ sinh mã, không chạm module, database, tiền, quyền, hàng đợi, gọi ra ngoài.
- 6.1–6.13: không áp dụng: test thuần (đọc tệp, so chuỗi), không đồng thời, không database.
- Giữ độ chặt: test cũ khóa cứng "mọi route ghi required + access trừ createServiceKey"; test mới kiểm theo luật: chế độ thuộc {required, exempt, no-replay}; required thì không có lý do; exempt/no-replay thì lý do không rỗng và chỉ POST; Auth của route ghi thuộc {access, service_key}; service_key ⇔ path `/api/v1/web/`; thiếu trường bắt buộc (OperationID, Pattern, IdempotencyMode, Auth) là lỗi. Mỗi nhánh có ca âm để chứng minh test không luôn qua.

## Quyết định thay chủ shop

- Kiểm theo luật thay vì danh sách operation ngoại lệ cố định. Vì sao: đề bài yêu cầu đúng luật openapi 3, 6; danh sách cố định sẽ đỏ lại mỗi khi M5 thêm route hợp lệ (báo phí ship). Phương án khác: giữ danh sách cho phép (createServiceKey, webQuote) — chặt hơn nhưng gãy ở mỗi task thêm route hợp lệ; đảo ngược dễ (thêm danh sách vào test).
- Route ghi `Auth: "public"` (bước đăng nhập M7, luật 3 cho phép exempt) chưa có trong hợp đồng; test chỉ nhận access, service_key cho route ghi. Vì sao: an toàn hơn, task M7 sẽ phải nới có chủ đích. Ghi ở "Việc phát hiện thêm".

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Trước khi sửa: main (070272d) đỏ đúng chỗ đề bài nêu | `TestRealContractWritesRequired` (bản cũ) | `go test -count=1 ./tools/routegen/` | `main_test.go:187: route ghi của M0 phải required, không lý do: ... OperationID: "webQuote" ... IdempotencyMode: "exempt" ... Auth: "service_key"` → `FAIL` |
| Hợp đồng thật (gồm webQuote exempt + service_key, createServiceKey no-replay) đạt luật openapi 3, 6; có ít nhất một route ghi required | `TestRealContractWritesRequired` | `go test -count=1 -v -run '^(TestRealContractWritesRequired\|TestWriteRouteProblems)$' ./tools/routegen/` | `--- PASS: TestRealContractWritesRequired (0.04s)` |
| Bộ kiểm không luôn qua: 4 dòng đúng luật qua; 14 dòng sai bị bắt đúng lý do (exempt/no-replay thiếu lý do hay lý do trắng, required có lý do, chế độ lạ "safe", thiếu chế độ, Auth "staff"/"public"/thiếu, service_key ngoài /api/v1/web/, path web mà access, exempt trên PUT, no-replay trên DELETE, thiếu OperationID) | `TestWriteRouteProblems` | như trên | `--- PASS: TestWriteRouteProblems (0.00s)` |
| Cổng của task (sau khi gộp main: "Already up to date") | ci-task | `scripts/agent/ci-task.sh` | `ok elimaz.com.vn/crm/tools/routegen 3.978s`, `ok elimaz.com.vn/crm/tests/arch 177.502s`, `CI-TASK XANH (so với điểm tách nhánh 070272d…)` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Task M7 thêm bước đăng nhập công khai (POST exempt, `Auth: "public"`) cần nới `writeRouteProblems` cho Auth public kèm exempt.

## Reviewer

Kết luận: ĐẠT (lần 1). Không có phát hiện phải sửa. Reviewer đối chiếu: bỏ hẳn cách so đuôi dòng cũ; định dạng dòng khớp `render` (main.go, `strconv.Quote` ↔ `strconv.Unquote`, `IdempotencyReason` chỉ có khi khác rỗng); đếm đúng 4 ca đúng, 14 ca sai, mỗi ca sai chỉ khớp thông báo của đúng nhánh; ci-task xanh. Ghi chú không bắt buộc: route ghi `Auth: public` (đăng nhập M7) đang bị từ chối — đã ghi ở Việc phát hiện thêm; tên test không còn tả đúng "required" (chỉ chuyện đặt tên, giữ nguyên để không đổi tên test mà điều phối viên đang theo dõi); luật x-admission do routegen kiểm (main.go:241).
