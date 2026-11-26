# CRM ELIMAZ — luật cho mọi phiên làm việc trong `apps/crm`

Tệp này viết tay, giữ dưới 200 dòng. Chỉ ghi điều không đọc được từ code. Kiến trúc và lý do nằm ở spec, không chép lại ở đây.

- Spec (bản 2.1): `../../docs/crm/specs/2026-11-10-crm-rieng/`. ADR-44 là danh sách thư viện được dùng.
- Kế hoạch đang chạy: M0 `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`, M1 `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md`; các mốc sau cùng thư mục. Số task toàn cục: `../../docs/superpowers/plans/danh-so-task-toan-cuc.md`.
- Luật của website (Playwright, cổng 4175/4176, mockup DESIGN.md) **không áp dụng** ở đây.
- Mở phiên CRM với thư mục làm việc là `apps/crm`. Hook (`.claude/settings.json`) và luật theo vùng (`.claude/rules/`) chỉ có hiệu lực khi phiên bắt đầu ở đây.

## Người làm và người duyệt

- Claude viết code; chủ shop duyệt. Chủ shop không đọc code: mọi báo cáo viết tiếng Việt, giữ thuật ngữ tiếng Anh và giải thích một lần.
- Chỉ làm đúng task đang được giao. Việc ngoài phạm vi ghi vào "Việc phát hiện thêm" của tệp task, không tự làm.
- Commit cục bộ sau mỗi task là được phép. **Không push, không deploy, không vào máy chủ, không tạo tài khoản** khi chưa có OK riêng của chủ shop trong chat.
- CRM không bao giờ gọi Nhanh, không đọc dữ liệu Nhanh.
- Không chọn công nghệ vì "quen", "có sẵn", "tải nhỏ nên đủ". Thêm thư viện ngoài ADR-44 thì ghi một dòng vào `docs/phu-thuoc.md` (tên, phiên bản, giấy phép, việc nó làm, vì sao thư viện chuẩn không đủ).

## Cách làm mỗi task

Quy trình đầy đủ: `docs/quy-trinh-lam-viec.md`. Mỗi task một phiên sạch (`/clear` giữa hai task).

0. Tạo tệp task: `scripts/agent/new-task.sh NN "Tên task"` (tạo từ mẫu `docs/tasks/_mau.md`, đặt làm task đang chạy; trạng thái lưu ngoài git, trong thư mục git của worktree, không cần commit).
1. Đọc task trong kế hoạch, Interfaces của các task nó dùng, mục spec và ADR liên quan. Spec mơ hồ hay mâu thuẫn, cần đổi thiết kế, cần việc ngoài máy: dừng và hỏi chủ shop.
2. **Trước khi viết test**, điền tệp task:
   - **Ảnh hưởng:** ký hiệu Go xuất ra ngoài sẽ đổi và số chỗ tham chiếu (công cụ LSP `findReferences`), gói phụ thuộc ngược (`go list`), operation OpenAPI, chỗ dùng trên màn hình, câu sqlc và bảng, pha migration, test sẽ chạy.
   - **Tái dùng:** đã kiểm gì trong `docs/danh-muc-dung-chung.md`; dùng lại cái nào, hoặc viết mới vì sao.
   - **Soát thiết kế:** chỉ những câu ở mục 5, 6 của quy trình liên quan tới phần sẽ chạm (database, tiền, quyền, hàng đợi…).
3. Test thất bại trước, rồi code cho qua, rồi dọn code. Thay đổi cấu trúc (dời, đổi tên, tách) và thay đổi hành vi ở hai commit khác nhau.
4. Sửa định nghĩa một ký hiệu thì sửa mọi chỗ dùng trong cùng task (đổi tên dùng rename của gopls). Chuyển sang cách mới thì **xóa** cách cũ, không bọc cách cũ trong một điều kiện.
5. Trước khi báo xong: hook Stop tự chạy build, vet, lint, test của gói đổi và gói phụ thuộc. Không qua thì sửa tiếp. Hook chặn tới lần thứ 8 liền thì dừng và báo chủ shop, không tìm cách vượt hook.
6. Điền bảng bằng chứng: tiêu chí → tên test → lệnh đã chạy → trích kết quả. "Mọi test đều qua" một mình không phải bằng chứng.
7. Task có mã: stage đúng các tệp sẽ commit bằng `git add <tệp>` (lệnh riêng, không `-A`), rồi chạy agent `reviewer` (nó review `git diff --cached`). Hook tự ghi dấu duyệt gắn với đúng phần mã đã stage; sửa mã sau đó thì phải stage và review lại. Chép kết luận vào mục Reviewer của tệp task. Chỉ sửa phát hiện ảnh hưởng tính đúng; bỏ qua phong cách. Task chỉ đổi tài liệu, cấu hình: không cần reviewer.
8. `git commit -m "..."` (không kèm đường dẫn, không `-a`, không `--no-verify`; git hook chặn khi tệp task chưa đủ hoặc chưa có dấu duyệt), rồi `scripts/agent/finish-task.sh`, rồi báo chủ shop bằng tiếng Việt.

Dấu hiệu đang chệch hướng, gặp là dừng và báo: chạy vòng lặp sửa đi sửa lại; làm thêm thứ không ai yêu cầu; muốn tắt, xóa hay nới một test.

## Lệnh

Có dần theo kế hoạch; lệnh nào chưa có thì task tương ứng sẽ tạo.

- `scripts/agent/ci-task.sh` — cổng kiểm của một task, chạy trước khi review (thay `make ci` trong phiên làm task, từ 18/11): đủ mọi cổng tĩnh như `make ci`, test chỉ gói đổi + gói phụ thuộc + cả bộ vùng rủi ro cao nếu chạm (đổi go.mod, testkit thì toàn bộ; migration mới chỉ thêm → gói dùng đối tượng nó chạm, migration thu hẹp hay sửa tệp cũ → toàn bộ, Task 473); so hợp đồng và test-guard với điểm tách nhánh. Toàn bộ test chạy trên `main` do điều phối viên sau mỗi đợt gộp.
- Test từ xa trên GitHub Actions (Task 493, repo private, gói Free 2.000 phút/tháng): `scripts/ci/remote-test.sh <tree|commit> <base> <tệp gói> [tệp gói -race]` (tệp gói: mỗi dòng một gói, hoặc `ALL`) đẩy commit tạm lên `ci/<worktree>`, chạy `.github/workflows/test-remote.yml` (cổng tĩnh `scripts/ci/static-gates.sh` + test chia tối đa 6 phần), in kết quả, phút tính tiền, dòng FAIL; thoát 0 xanh, 1 đỏ, 2 sai tham số, 3 không chạy được từ xa. Điều phối viên gọi trực tiếp khi gộp. Sau khi chủ shop áp `scripts/ci/ci-task-tu-xa.patch`, ci-task chạy từ xa mặc định (`CRM_CI_REMOTE=0` ép chạy máy; `CRM_BASE_BRANCH` chọn nhánh gốc, mặc định dò m4/main). Ước một lượt `ALL` và cập nhật `scripts/ci/test-times.txt`: xem tệp đó.
- Chia máy (Task 494): `scripts/ci/pool.py` giữ chỗ trống nhanh nhất blacksmith (4) → mac (1, chung lượt run-in-slot) → github (3) → vps (1, chỉ ≤ 15 gói, không ALL); hết chỗ thì chờ chỗ đầu tiên trống; bể lỗi hạ tầng tạm hỏng 30 phút; sổ phút tháng (blacksmith ×2, hết ở 2.700; github hết ở 1.600). `pool.py status` xem chỗ bận, phút. `CRM_CI_RUNNER=blacksmith|mac|github|vps` ép bể (cả cho remote-test.sh). Điều phối viên test bản đã gộp: `scripts/ci/merge-test.sh [commit|tree] [base] [tệp gói] [tệp -race]` (mặc định HEAD, base cha thứ nhất, ALL), in `MERGE-TEST XANH|ĐỎ (<bể>)`, thoát 0 khi xanh. Bản vá ci-task (cùng tệp `.patch`) cho ci-task đi qua bộ điều phối.
- `make ci` — toàn bộ cổng kiểm (từ Task 3): build, vet, golangci-lint, sinh lại mã và đòi không chênh, `sqlc vet`, `go test -race ./...`, govulncheck, test kiến trúc, oasdiff, jscpd, `test-guard.sh`, danh mục dùng chung, gitleaks.
- Test một gói: `go test -race ./internal/platform/queue/...`
- Chọn test: chỉ gói vừa đổi và gói nhập chúng. Ngoại lệ: chạm vào vùng rủi ro cao thì chạy cả bộ của vùng đó. `-race` chỉ cho gói có mã viết tay hay test bị sửa và vùng rủi ro cao bị chạm; gói chỉ dùng tới chạy test thường (`affected.py --split`, Task 473; toàn bộ `-race` vẫn chạy trên `main` sau mỗi đợt gộp). Vùng rủi ro cao (một nguồn: `HIGH_RISK_DIRS` trong `scripts/agent/lib.sh`): `internal/platform/{tenancy,queue,commands,idempotency,db,webhooks,fieldcrypt,files}`, `internal/modules/{identity,inventory,order,promotion,cash}`, `internal/imager`, `internal/httpapi/guard.go`, `fulfillment/status.go`, `catalog/internal/importer/apply.go`, `catalog/article_html.go` (mở rộng 20/11 sau đóng M1, M2).
- Test mới của gói lớn (order, fulfillment, inventory, customer, catalog…) gọi `t.Parallel()` ở dòng đầu khi có database riêng (`testkit.OpenStaff`) và không dùng trạng thái chung của tiến trình (`t.Setenv`, biến cấp gói, `CaptureSpans`, cổng cố định); test con dùng chung một env chạy lần lượt. Gói order, fulfillment, inventory giới hạn 6 test song song bằng `testkit.SetParallel(6)` trong `TestMain` (máy chủ thử 300 kết nối, nhiều phiên); đổi bằng `-parallel N` (Task 469, 470).
- `make gen` — sinh lại mã ogen, sqlc, bảng route (từ Task 4 và Task 6).
- Màn hình (từ Task 17): `cd console && npm test`, `npx tsc --noEmit`, `npx eslint .`
- PostgreSQL dev: `make db-up` (PostgreSQL 5442, Jaeger 16686, OTLP 4318). Không dùng cổng 4175, 4176, 4180, 4181.
- Nhiều phiên song song trên một máy: mỗi phiên một tên dự án compose và cổng riêng, `make db-up COMPOSE_PROJECT=crm-tNN CRM_PG_PORT=54x2` (thêm `CRM_JAEGER_UI_PORT`, `CRM_OTLP_PORT` nếu cần); test đọc `CRM_TEST_DATABASE_URL` (crm_app, Makefile dựng từ `CRM_PG_PORT`). Chỉ xóa container, volume của dự án compose của mình.
- Phép đo flash sale (module riêng `spikes/flashsale`): `make spike-test`; cách đo tải ở `docs/do-luong/flash-sale-go.md`.
- Test thời gian (nhãn build `perf`, chạy trên máy rảnh): `make perf-test`. Test đúng/sai không lấy ngưỡng giờ thật làm tiêu chí (Task 400).

## Không được sửa tay

- Mã sinh: `internal/httpapi/gen/**`, `**/internal/store/**`, `internal/httpapi/routes_gen.go`, `console/src/api/**`. Sửa nguồn (`openapi/openapi.yaml`, `queries/**/*.sql`) rồi `make gen`.
- Migration đã commit: thêm migration mới, không sửa tệp cũ. Lệnh thu hẹp (`DROP`, `RENAME`, đổi kiểu, `SET NOT NULL`) cần dòng `-- contract: <bản đã ngừng dùng>` ngay phía trên.
- Hook PreToolUse chặn hai loại trên; bị chặn nghĩa là đang làm sai chỗ, không phải lỗi của hook.

## Dùng lại trước khi viết mới

- Trước khi viết một hàm hay kiểu có thể dùng chung: đọc `docs/danh-muc-dung-chung.md` (sinh tự động từ `go doc`, có dòng "khi nào không dùng"), rồi tìm theo tên và theo việc nó làm.
- Mã chung chỉ nằm trong `internal/platform/<việc>`, tên gói là danh từ nói rõ việc (`tenancy`, `queue`, `idempotency`). Không tạo gói `util`, `common`, `helpers`, `shared`.
- Logic nghiệp vụ của một module ở lại trong module đó, kể cả khi trông giống module khác. Chỉ gộp hai đoạn khi chúng cùng mục đích **và** cùng lý do thay đổi.
- Cảnh báo trùng của `dupl` hay jscpd là câu hỏi phải trả lời trong mục Tái dùng (gộp, hoặc giữ kèm lý do), không phải lệnh gộp.

## Lỗi và log

- Lỗi nghiệp vụ là sentinel (`var ErrX = errors.New(...)`) hoặc kiểu lỗi đặt trong module sinh ra nó. Bọc một lần bằng `fmt.Errorf("<việc đang làm>: %w", err)`, kiểm bằng `errors.Is`/`errors.As`, không so chuỗi.
- Đổi lỗi sang mã HTTP ở **một chỗ**: nhóm tệp `internal/httpapi/errors*.go`. Body lỗi luôn `{"error": "<mã>", "request_id": "<id>"}`, không bao giờ có vết lỗi.
- Thêm lỗi cho một vùng (Task 441): chỉ sửa `internal/httpapi/errors_<vùng>.go` (dòng `errorMapping` đúng thứ tự kiểm; mã chỉ vùng đó trả khai ở const của tệp đó, mã nhiều vùng dùng ở `errors.go`), thêm mã vào `x-error-codes` của `openapi.yaml` và dòng vào `errors_snapshot_test.go`; vùng mới thêm hàm vào `domainErrorMappers` (errors.go).
- Log chỉ bằng `log/slog`, ghi **một lần** ở mép (handler HTTP, worker), không vừa log vừa trả lỗi.
- Không bao giờ để SĐT, địa chỉ, nội dung tin, token vào log, dấu vết, cảnh báo hay audit. Dữ liệu cá nhân dùng các kiểu có `LogValue()` tự che.

## Cơ sở dữ liệu

- Mọi truy vấn của tiến trình ứng dụng chạy bằng role `crm_app`, trong `tenancy.WithTenant` khi chạm bảng có `tenant_id`.
- SQL tĩnh nằm trong `queries/**/*.sql` qua sqlc. SQL dựng lúc chạy chỉ nằm trong tệp `*_dynsql.go`, có `*_dynsql_test.go` chạy trên PostgreSQL thật.
- Cấm chế độ simple protocol của pgx. Tiền là `int64` đồng.

## Module mẫu

- **Module và đường đọc (Task 7):** `internal/modules/identity`. Phần công khai ở gói gốc (kiểu, sentinel lỗi, hàm
  nhận `ctx` và `*db.DB`); câu SQL trong `queries/identity/*.sql`, sqlc sinh `internal/modules/identity/internal/store`
  (khối riêng trong `sqlc.yaml`); mọi truy vấn trong `tenancy.WithTenant`; test ở gói `_test` trên PostgreSQL thật
  (`testkit.OpenStaff`), quy luật bằng rapid. Đường đọc mẫu: `GET /api/v1/me` — hợp đồng `openapi.yaml` (`x-permission`)
  → `auth` + `guard` → handler mỏng `internal/httpapi/handlers_me.go` → kết quả.
- **Module có chiều người bán (Task 27):** `internal/modules/catalog` — mọi hàm đọc nhận `sellerID` (ADR-29),
  sửa có `version` (khóa dòng rồi so), đường ghi là phương thức của một kiểu `Writer` tự ghi sự kiện, báo hiệu, audit
  trong transaction của bên gọi; module khác đọc qua view `catalog.*_v` (`docs/views.md`).
- **Số dư và sổ chỉ ghi thêm (Task 28):** `internal/modules/inventory` — mỗi lần đổi tồn là một câu UPDATE có điều kiện
  (`movement.go`, `applyDelta`; 0 dòng → sentinel) cùng một dòng sổ trong tx của bên gọi, không đọc số ra Go rồi ghi lại;
  khóa theo ADR-46 nhóm 11 (số dư theo khóa tăng dần, rồi giữ hàng); test quy luật rapid (`property_test.go`), test hai
  đường song song không deadlock (`deadlock_test.go`); module khác nạp tồn bằng `invtest`. Dựng module mới: bản nháp
  `docs/skill-drafts/them-module.md`.
- **Gọi dịch vụ ngoài:** bộ nối ở `internal/adapters/<tên>` (mẫu: `cfaccess`), hiện thực interface do module khai;
  `cmd/crm` nối. Module không nhập `net/http`.
- **Đường ghi (Task 12):** `POST /api/v1/platform/jobs/{id}/retry` (`internal/httpapi/handlers_jobs.go`) — hợp đồng
  có `x-permission` và `IdempotencyKey` → middleware chống trùng → `WriteTx` (một transaction tenant: khóa và đổi dòng
  qua public API của gói sở hữu, `identity.RecordAudit`, lưu kết quả; truyền con trỏ `&gen.X{…}`) → lỗi nghiệp vụ là
  sentinel, đổi sang HTTP ở `errors.go`. Test: 50 yêu cầu song song cùng khóa, `retry_later` không lưu, tenant khác 404.
- **Lệnh điều phối nhiều module (Task 73):** `internal/modules/order` — `create_m2.go` (`Writer.Create`: kiểm không
  database → đọc không khóa → khách → đơn → tồn theo ADR-46, một transaction của bên gọi, lỗi module khác bọc `%w`)
  và `transition.go` (đổi trạng thái đơn chỉ qua `applyTransition`; câu SQL chỉ ở `queries/ord/transition.sql`).
- **Gọi ra ngoài:** lệnh `commands` (Kind từ `CommandKind`, ghi bằng `Enqueue` trong transaction nghiệp vụ); không chắc
  đã gửi → `in_doubt`, không tự gửi lại. Module và đường mới làm theo đúng các mẫu trên.

## Bẫy đã gặp

Thêm một dòng mỗi khi chủ shop hoặc reviewer phải sửa cùng một lỗi lần thứ hai. Ghi lỗi gì, cách đúng là gì.
(Gom ngày 18/11 từ kết luận reviewer của Task 1–326.)

- RLS áp cả cho chủ bảng: mọi câu chạm bảng có `tenant_id` (kể cả câu kiểm trong test) chạy trong `tenancy.WithTenant` / transaction có `set_config('app.tenant_id', …, true)`; không thì ra 0 dòng mà không lỗi.
- `sum()`, `max()` trên tập rỗng trả NULL: dùng `coalesce(…, 0)` và có ca test tập rỗng.
- Hàm chạy trong `db.RetryTx` có thể chạy lại: gán lại mọi biến kết quả trong thân hàm, không cộng dồn, không gửi ra ngoài.
- Khóa nhiều bảng: đúng thứ tự `docs/adr/0046-thu-tu-khoa.md`; có test hai đường song song không deadlock.
- Mốc thời gian "đã gửi/đã xong" ghi khi việc thật sự xong, không ghi lúc bắt đầu (việc kết thúc giữa chừng bị bỏ sót).
- Đổi tên hay chữ ký: grep mọi chỗ gọi (cả test, console, tài liệu `docs/views.md`, `docs/quyen.md`) trước khi stage.
- Mục Ảnh hưởng, bảng bằng chứng: số ca, tên test, tên hàm phải khớp mã thật; reviewer đối chiếu từng dòng.
- Màn hình: chỉ vẽ hành vi mà máy chủ thật sự làm (quyền, chặn, lỗi); ca lỗi của API có trạng thái hiện riêng; mock trong test khớp quyền thật.
- Lỗi pgx tạm thời (57014, 57P01, 57P03, 53300, lỗi lúc nối): phân loại đúng là tạm thời, có test.
- Màn hình đọc dữ liệu phân trang: còn `nextCursor` thì chưa được coi là "hết"/"0" (Task 84); sửa (PATCH) chỉ gửi trường người dùng đổi (Task 85).
- Test không lấy một route, một ca "chưa làm" cụ thể của mã thật làm mẫu (gãy khi task khác làm nó): dò tự động hoặc dùng handler giả (Task 69, 82, 427).
- (M1, gom 21/11 ở Task 405.) Lượt thử và kết quả không chắc: bận/429/503 chờ theo `Retry-After`, không tiêu lượt nhưng có trần; mọi đường lỗi (kể cả khi ghi trạng thái lỗi cũng lỗi) phải tới `dead` có cảnh báo; không chắc đã gửi thì `in_doubt`, không tự hủy hay gửi lại; mỗi nhánh một test (Task 35, 43, 48, 408, 409).
- Chỉ đưa kết quả của transaction vào biến, bộ nhớ sau khi commit thành công (tx lùi thì giữ bản đã đọc); bản kê, `manifest` chỉ tin khi đối chiếu được với kho thật (Task 46, 409).
- Test so thời gian chỉ dùng một đồng hồ (không so `now()` của database với `time.Now()` của máy test); đúng/sai chờ theo sự kiện, giờ thật chỉ làm chốt chống treo (Task 400, 403, 420).
- Màn hình lúc đang tải, đang lưu, vừa lỗi 403: không vẽ như dữ liệu rỗng hay bảng cũ; khóa mọi thao tác (cả kéo thả) khi một lần ghi đang chạy (Task 48, 325, 330).
- Lệnh trong `RUNBOOK.md`, kể cả đường lui, chạy thật một lần và trích kết quả vào bảng bằng chứng (SQL trong ngữ cảnh tenant, biến compose bắt buộc) (Task 33, 410).
