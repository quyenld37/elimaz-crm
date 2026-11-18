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

- `scripts/agent/ci-task.sh` — cổng kiểm của một task, chạy trước khi review (thay `make ci` trong phiên làm task, từ 18/11): đủ mọi cổng tĩnh như `make ci`, test chỉ gói đổi + gói phụ thuộc + cả bộ vùng rủi ro cao nếu chạm (đổi migration, go.mod, testkit thì toàn bộ); so hợp đồng và test-guard với điểm tách nhánh. Toàn bộ test chạy trên `main` do điều phối viên sau mỗi đợt gộp.
- `make ci` — toàn bộ cổng kiểm (từ Task 3): build, vet, golangci-lint, sinh lại mã và đòi không chênh, `sqlc vet`, `go test -race ./...`, govulncheck, test kiến trúc, oasdiff, jscpd, `test-guard.sh`, danh mục dùng chung, gitleaks.
- Test một gói: `go test -race ./internal/platform/queue/...`
- Chọn test: chỉ gói vừa đổi và gói nhập chúng. Ngoại lệ: chạm vào vùng rủi ro cao thì chạy cả bộ của vùng đó. Vùng rủi ro cao: `internal/platform/{tenancy,queue,commands,idempotency,db,webhooks}`, `internal/modules/{identity,inventory}`, `internal/httpapi/guard.go` (mở rộng 18/11 sau Task 23).
- `make gen` — sinh lại mã ogen, sqlc, bảng route (từ Task 4 và Task 6).
- Màn hình (từ Task 17): `cd console && npm test`, `npx tsc --noEmit`, `npx eslint .`
- PostgreSQL dev: `make db-up` (PostgreSQL 5442, Jaeger 16686, OTLP 4318). Không dùng cổng 4175, 4176, 4180, 4181.
- Nhiều phiên song song trên một máy: mỗi phiên một tên dự án compose và cổng riêng, `make db-up COMPOSE_PROJECT=crm-tNN CRM_PG_PORT=54x2` (thêm `CRM_JAEGER_UI_PORT`, `CRM_OTLP_PORT` nếu cần); test đọc `CRM_TEST_DATABASE_URL` (crm_app, Makefile dựng từ `CRM_PG_PORT`). Chỉ xóa container, volume của dự án compose của mình.
- Phép đo flash sale (module riêng `spikes/flashsale`): `make spike-test`; cách đo tải ở `docs/do-luong/flash-sale-go.md`.

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
- Đổi lỗi sang mã HTTP ở **một chỗ**: `internal/httpapi/errors.go`. Body lỗi luôn `{"error": "<mã>", "request_id": "<id>"}`, không bao giờ có vết lỗi.
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
- **Gọi dịch vụ ngoài:** bộ nối ở `internal/adapters/<tên>` (mẫu: `cfaccess`), hiện thực interface do module khai;
  `cmd/crm` nối. Module không nhập `net/http`.
- **Đường ghi (Task 12):** `POST /api/v1/platform/jobs/{id}/retry` (`internal/httpapi/handlers_jobs.go`) — hợp đồng
  có `x-permission` và `IdempotencyKey` → middleware chống trùng → `WriteTx` (một transaction tenant: khóa và đổi dòng
  qua public API của gói sở hữu, `identity.RecordAudit`, lưu kết quả; truyền con trỏ `&gen.X{…}`) → lỗi nghiệp vụ là
  sentinel, đổi sang HTTP ở `errors.go`. Test: 50 yêu cầu song song cùng khóa, `retry_later` không lưu, tenant khác 404.
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
