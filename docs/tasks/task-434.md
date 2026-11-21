# Task 434 — M2-r5: Lệnh vận hành tính lại chỉ số khách cho một gốc

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task M2-25 consumer `customer_stats`, Task
M2-32 RUNBOOK dòng "đọc chỉ số khách sai (chạy lại `customer_stats` cho một gốc)"); nguồn task bù:
`docs/review-doc-lap-m2.md` dòng 434, `docs/RUNBOOK.md` mục "Chỉ số khách sai". Loại việc (quy trình mục 3): A

Việc: hiện chỉ thử lại được việc `event.customer_stats` đã `dead`; khi không có việc lỗi mà chỉ số một khách vẫn sai
thì chưa có cách nào tính lại ngoài đợi đơn đổi trạng thái. Thêm lệnh `crm recompute-customer-stats <mã khách>`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ thêm ký hiệu không xuất trong `package main`
  (`cmd/crm/recompute.go`: hằng `recomputeCommand`, `errCustomerUnknown`, kiểu `recomputeResult`, hàm
  `recomputeCustomerStats`, `runRecompute`) và sửa thân `run` trong `cmd/crm/main.go` (nhận thêm lệnh, sửa câu báo lệnh
  lạ, rẽ nhánh theo `args[0]` thay cho `len(args) == 2`). Dùng lại không đổi: `customer.Writer.RecomputeStats`,
  `customer.Writer.ApplyTagRules`, `customer.RootOf`, `customer.StatsFor`, `tenancy.ListTenantIDs`, `tenancy.WithTenant`,
  `realtime.NewPGHub`.
- Gói phụ thuộc ngược (go list): `cmd/crm` là `package main`, không gói nào nhập (đếm `go list` các gói nhập
  `elimaz.com.vn/crm/cmd/crm`: chỉ chính nó). Không đổi gói `customer`.
- Operation OpenAPI bị chạm, oasdiff: không có (lệnh dòng lệnh, không qua HTTP).
- Tài liệu: `docs/RUNBOOK.md` mục "Chỉ số khách sai" (thay câu "chưa có lệnh" bằng cách gọi lệnh, mã thoát).
- Chỗ dùng trên màn hình: không có. Báo hiệu `customer.changed` (đã có) tới màn hồ sơ khách khi lệnh chạy.
- Câu sqlc, bảng, pha migration: không câu mới, không migration. Đọc `platform.tenants` (crm_app có SELECT, migration
  0001), ghi `customer.customers`, `customer.customer_tag_links`, audit qua các hàm sẵn có.
- Test sẽ chạy: `go test -race ./cmd/crm/...` (test mới `TestRecomputeCustomerStats`, `TestRunRecompute`,
  `TestRunRecomputeArgs` + test cũ của gói); `go test -race -run 'Stats|TagRule' ./internal/modules/customer/` (không
  đổi mã, chạy để chắc hai hàm dùng lại vẫn như cũ). Cổng: `scripts/agent/ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (tenancy:
  `ListTenantIDs`, `WithTenant`); tìm `RecomputeStats`, `ApplyTagRules`, `recompute` trong `internal/modules/customer`;
  cách lệnh phụ `seed-dev`, `healthcheck` nối trong `cmd/crm/main.go`.
- Dùng lại / viết mới vì: dùng lại `RecomputeStats` + `ApplyTagRules` (cùng hai bước consumer `customer_stats` làm:
  tính lại từ `ord.orders_v`/`ord.returns_v` của cả nhóm, đưa gốc lead có đơn sang customer, rồi áp quy tắc nhãn) trong
  một transaction tenant — không thêm hàm công khai mới cho module customer. Viết mới chỉ phần nối lệnh trong `cmd/crm`
  (tìm tenant của khách, in kết quả), theo đúng mẫu `seed-dev` (tệp riêng, `run` rẽ nhánh).

## Soát thiết kế

- 5.1 Chủ dữ liệu: chỉ số, nhãn thuộc `customer`; lệnh chỉ gọi public API của customer. 5.2 Không ghi bảng module khác
  (cmd không có SQL). 5.3 Đọc đơn, phiếu qua view `ord.orders_v`, `ord.returns_v` (trong `RecomputeStats` sẵn có).
  5.4 Tính lại + áp nhãn trong một transaction (như consumer). 5.5 không áp dụng: lệnh tay, đồng bộ; không có việc
  sau commit ngoài báo hiệu (pg_notify trong tx). 5.6 không áp dụng: không gọi ra ngoài. 5.7 Mọi câu chạm bảng có
  `tenant_id` trong `tenancy.WithTenant`; tìm tenant bằng cách thử từng tenant (RLS ẩn khách tenant khác →
  `ErrNotFound` → tx lùi, thử tenant kế); không bảng mới. 5.8 không áp dụng: không HTTP. 5.9 không áp dụng: không đường
  ghi HTTP; lệnh chạy lại được (tính lại từ gốc, cùng kết quả), không cần khóa chống trùng. 5.10 không Valkey.
  5.11 không thư viện mới. 5.12 theo mẫu lệnh phụ `seed-dev` (cmd/crm) và mẫu module customer.
- 6.1 Chạy cùng lúc với consumer hay gộp: `RecomputeStats` khóa cả nhóm (ADR-46 nhóm 5, đọc lại dưới khóa, lùi khi
  nhóm đổi); `ApplyTagRules` khóa lại cùng nhóm trong cùng tx (đã giữ khóa) — đúng thứ tự consumer đang dùng, không
  thêm thứ tự khóa mới. 6.2 Chạy lại hai lần: cùng kết quả (test chạy hai lần). 6.3 Không thứ tự khóa mới. 6.4 Lỗi tạm:
  `WithTenant` chạy lại tối đa 3 lần (thân hàm gán lại biến kết quả mỗi lần, không cộng dồn); hết lượt → lệnh báo lỗi,
  mã thoát 1, người vận hành chạy lại. 6.5 READ COMMITTED + khóa dòng. 6.6 không áp dụng: không phải việc nền.
  6.7 không áp dụng. 6.8 Tiến trình chết giữa chừng: tx tự lùi, chạy lại lệnh. 6.9 Giới hạn thời gian mặc định của pool
  (`poolOptions(cfg)`). 6.10 Tiền `int64` đồng (đọc ra in, không tính). 6.11 không áp dụng: quy luật tính số đã có test
  rapid/ca ở customer; lệnh không thêm quy luật. 6.12 Kết quả in ra và log chỉ có mã tenant, mã khách, các số đếm và
  tổng tiền; không tên, SĐT, địa chỉ. 6.13 Quyền: lệnh chạy trong container (người có quyền vào máy chủ), bằng role
  `crm_app` (DATABASE_URL), không chủ bảng; RLS vẫn áp.

## Quyết định thay chủ shop

1. **Dạng lệnh: lệnh dòng lệnh `crm recompute-customer-stats <mã khách>`**, không phải API/nút trên màn hình. Vì sao:
   RUNBOOK gọi là "lệnh vận hành", việc này của người vận hành khi soát số sai, giống `seed-dev`/`healthcheck`; không
   cần hợp đồng OpenAPI, quyền mới, khóa chống trùng, màn hình. Phương án khác: `POST /api/v1/customers/{id}/recompute-stats`
   (quyền chủ shop) — làm sau được nếu chủ shop muốn bấm trên màn; lệnh này không cản.
2. **Không cần mã tenant**: lệnh thử lần lượt mọi tenant (RLS ẩn khách tenant khác), dừng ở tenant có khách. Vì sao:
   người vận hành có sẵn mã khách (đường dẫn hồ sơ, payload việc), không có sẵn mã tenant; số tenant nhỏ. Phương án
   khác: bắt buộc truyền `<tenant>`.
3. **Chạy được ở mọi vai trò** (dùng DATABASE_URL = crm_app của container đang chạy, thường là worker); không mở chủ
   bảng. Phương án khác: chỉ cho `CRM_ROLE=worker`.
4. **Không ghi audit riêng cho lệnh** (consumer cũng không ghi audit khi tính lại; thay đổi lifecycle và nhãn vẫn được
   audit bởi các hàm sẵn có với người làm `system`); log một dòng ở mép với mã tenant, mã gốc. Phương án khác: thêm
   hành động audit `customer.stats_recompute` (cần thêm vào danh sách hành động).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Truyền mã hồ sơ con → tính lại cho gốc hiện tại, đúng tenant (bỏ qua tenant khác), số lệch về số từ đơn/phiếu (0) ở cả gốc và hồ sơ con | `TestRecomputeCustomerStats` | `go test -race -count=1 -v -run Recompute ./cmd/crm/` | `--- PASS: TestRecomputeCustomerStats (0.27s)` |
| Áp quy tắc nhãn sau khi tính lại (gỡ nhãn rule gắn theo số lệch); báo hiệu `customer.changed` của gốc ở đúng tenant | `TestRecomputeCustomerStats` | như trên | như trên. Đột biến bỏ `ApplyTagRules` → đỏ `nhãn theo quy tắc còn trên X ("rule")`; đột biến bỏ `continue` khi `ErrNotFound` → đỏ `tính lại chỉ số khách ở tenant …: customer: không có` (đã khôi phục mã) |
| Chạy lại cho cùng kết quả | `TestRecomputeCustomerStats` (vòng `for i := range 2`) | như trên | như trên |
| Khách không có ở tenant nào → `errCustomerUnknown`; qua `run` → mã thoát 1 | `TestRecomputeCustomerStats`, `TestRunRecompute` | như trên | `--- PASS: TestRunRecompute (0.31s)` |
| Đường lệnh thật `run([recompute-customer-stats, mã])` trên database đã migrate: mã thoát 0, in tenant, gốc, "0 đơn", số về 0 | `TestRunRecompute` | như trên | như trên |
| Thiếu mã, mã sai dạng, thừa đối số → mã thoát 2, không mở database, stderr nêu cách gọi | `TestRunRecomputeArgs` | như trên | `--- PASS: TestRunRecomputeArgs (0.00s)` |
| Hai hàm dùng lại không đổi hành vi | test sẵn có `TestStats*`, `TestTagRule*` | `go test -race -count=1 -run 'Stats\|TagRule' ./internal/modules/customer/` | `ok elimaz.com.vn/crm/internal/modules/customer 5.882s` |
| Lệnh RUNBOOK chạy bằng tệp chạy thật | (chạy tay) | `CRM_ROLE=migrate CRM_ENV=dev DATABASE_URL=$CRM_TEST_DATABASE_URL … go run ./cmd/crm recompute-customer-stats sai` | `mã khách "sai" không phải UUID; cách gọi: …`, mã thoát 2. Mã hợp lệ trên database `crm` chung (chưa migrate; chỉ là máy chủ cho database thử): `đọc danh sách tenant: ERROR: relation "platform.tenants" does not exist`, mã thoát 1 — đường thành công chạy qua `run` trong `TestRunRecompute`. Dòng `docker compose … exec crm-worker` chưa chạy (không có, không được vào máy chủ) |
| Cổng tĩnh + test gói đổi | — | ci-task (`scripts/agent/ci-task.sh`) | lần 1: chỉ jscpd đỏ (đoạn đọc cột chỉ số trùng `stats_test.go`) → viết lại thành `statsSum`; `make jscpd` → `Found 0 clones.`; lần 2: `ok elimaz.com.vn/crm/cmd/crm 3.175s`, `ok elimaz.com.vn/crm/tests/arch 32.888s`, `CI-TASK XANH (so với điểm tách nhánh 242ceee…)` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Dòng `docker compose … exec -T crm-worker /crm recompute-customer-stats <mã>` trong RUNBOOK chưa chạy thật trên máy
  chủ (không có quyền vào máy chủ); tệp chạy và đường `run` đã chạy thật (bảng bằng chứng). Đường dẫn `/crm` đối chiếu
  với `Dockerfile` (dòng 26 `COPY … /crm`, 31 `ENTRYPOINT ["/crm"]`, image distroless không có `/` trong PATH) và
  `docker/compose.prod.yml:56` (`/crm healthcheck`) — reviewer vòng 1 bắt lỗi viết `crm` không đường dẫn. Lần đầu lên
  staging nên chạy dòng đó một lần và ghi kết quả.

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (vòng 2).

- Vòng 1 — CẦN SỬA: `docs/RUNBOOK.md` gọi `crm recompute-customer-stats` trong container; image distroless đặt tệp chạy
  ở `/crm` (Dockerfile:26, 31), `/` không có trong PATH → lệnh sẽ báo không tìm thấy tệp chạy. Đã sửa thành `/crm …`.
  Phần mã: đủ yêu cầu, bằng chứng thật, ảnh hưởng khớp, dùng lại đúng, không test bị nới, luật dự án đạt.
- Vòng 2 — ĐẠT: không còn phát hiện phải sửa. Ghi chú không bắt buộc: Writer dựng với `events.NewRegistry()` rỗng
  hiện không sao vì đường tính lại không phát sự kiện; nếu sau này `promote` phát sự kiện thì cần sổ thật. Lần đầu lên
  staging nên chạy dòng `docker compose … exec` một lần và ghi kết quả.
