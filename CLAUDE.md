# CRM ELIMAZ — luật cho mọi phiên làm việc trong `apps/crm`

Tệp này viết tay, giữ dưới 200 dòng. Chỉ ghi điều không đọc được từ code. Kiến trúc và lý do nằm ở spec, không chép lại ở đây.

- Spec (bản 2.1): `../../docs/crm/specs/2026-11-10-crm-rieng/`. ADR-44 là danh sách thư viện được dùng.
- Kế hoạch đang chạy: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`.
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

0. Tạo tệp task: `scripts/agent/new-task.sh NN "Tên task"` (tạo từ mẫu `docs/tasks/_mau.md`, đặt làm task đang chạy).
1. Đọc task trong kế hoạch, Interfaces của các task nó dùng, mục spec và ADR liên quan. Spec mơ hồ hay mâu thuẫn, cần đổi thiết kế, cần việc ngoài máy: dừng và hỏi chủ shop.
2. **Trước khi viết test**, điền tệp task:
   - **Ảnh hưởng:** ký hiệu Go xuất ra ngoài sẽ đổi và số chỗ tham chiếu (công cụ LSP `findReferences`), gói phụ thuộc ngược (`go list`), operation OpenAPI, chỗ dùng trên màn hình, câu sqlc và bảng, pha migration, test sẽ chạy.
   - **Tái dùng:** đã kiểm gì trong `docs/danh-muc-dung-chung.md`; dùng lại cái nào, hoặc viết mới vì sao.
   - **Soát thiết kế:** chỉ những câu ở mục 5, 6 của quy trình liên quan tới phần sẽ chạm (database, tiền, quyền, hàng đợi…).
3. Test thất bại trước, rồi code cho qua, rồi dọn code. Thay đổi cấu trúc (dời, đổi tên, tách) và thay đổi hành vi ở hai commit khác nhau.
4. Sửa định nghĩa một ký hiệu thì sửa mọi chỗ dùng trong cùng task (đổi tên dùng rename của gopls). Chuyển sang cách mới thì **xóa** cách cũ, không bọc cách cũ trong một điều kiện.
5. Trước khi báo xong: hook Stop tự chạy build, vet, lint, test của gói đổi và gói phụ thuộc. Không qua thì sửa tiếp. Hook chặn tới lần thứ 8 liền thì dừng và báo chủ shop, không tìm cách vượt hook.
6. Task có mã: chạy reviewer chỉ đọc (agent `reviewer`) trên phần đã đổi, chép kết luận vào mục Reviewer của tệp task. Chỉ sửa phát hiện ảnh hưởng tính đúng; bỏ qua phong cách. Task chỉ đổi tài liệu, cấu hình: không cần reviewer.
7. Điền bảng bằng chứng: tiêu chí → tên test → lệnh đã chạy → trích kết quả. "Mọi test đều qua" một mình không phải bằng chứng.
8. Commit cục bộ (commit có mã bị hook chặn khi tệp task chưa đủ hoặc reviewer chưa ĐẠT), đặt `Trạng thái: xong` trong `docs/tasks/hien-tai.md`, báo chủ shop bằng tiếng Việt.

Dấu hiệu đang chệch hướng, gặp là dừng và báo: chạy vòng lặp sửa đi sửa lại; làm thêm thứ không ai yêu cầu; muốn tắt, xóa hay nới một test.

## Lệnh

Có dần theo kế hoạch; lệnh nào chưa có thì task tương ứng sẽ tạo.

- `make ci` — toàn bộ cổng kiểm (từ Task 3): build, vet, golangci-lint, sinh lại mã và đòi không chênh, `sqlc vet`, `go test -race ./...`, govulncheck, test kiến trúc, oasdiff, jscpd, `test-guard.sh`, danh mục dùng chung, gitleaks.
- Test một gói: `go test -race ./internal/platform/queue/...`
- Chọn test: chỉ gói vừa đổi và gói nhập chúng. Ngoại lệ: chạm vào vùng rủi ro cao thì chạy cả bộ của vùng đó. Vùng rủi ro cao: `internal/platform/{tenancy,queue,commands,idempotency}`, `internal/modules/identity`, `internal/httpapi/guard.go`.
- `make gen` — sinh lại mã ogen, sqlc, bảng route (từ Task 4 và Task 6).
- Màn hình (từ Task 17): `cd console && npm test`, `npx tsc --noEmit`, `npx eslint .`
- PostgreSQL dev: `docker compose -f docker/compose.dev.yml up -d` (cổng 5442). Không dùng cổng 4175, 4176, 4180, 4181.

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

Chưa có. Task 7 thêm mẫu cho module và đường đọc; Task 12 thêm mẫu cho đường ghi. Khi đã có, module và đường mới làm theo đúng mẫu đó.

## Bẫy đã gặp

Thêm một dòng mỗi khi chủ shop hoặc reviewer phải sửa cùng một lỗi lần thứ hai. Ghi lỗi gì, cách đúng là gì.

- (chưa có)
