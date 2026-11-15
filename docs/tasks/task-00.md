# Task 00 — Cài đặt agent cho `apps/crm`

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`, Task 0. Chủ shop duyệt kế hoạch và Task 0 ngày 15/11/2026.

## Ảnh hưởng

- Repo mới `apps/crm`, chưa có mã Go hay TypeScript: không ký hiệu, gói, operation, bảng nào bị chạm.
- Ngoài repo, đã sửa theo lựa chọn của chủ shop (15/11):
  - CLAUDE.md gốc của thư mục "Thời trang": **chỉ thêm** một đoạn "Scope" ở đầu (luật website không áp dụng cho `apps/crm`); không dời, không sửa luật website. Chủ shop chọn cách này thay cho "dời sang `.claude/rules/website.md`" vì luật theo đường dẫn chỉ nạp khi agent đã chạm tệp website.
  - Máy Mac: cài Go 1.27.4 (Homebrew), gopls v0.24.0, gofumpt v0.12.0, goimports v0.52.0 vào `~/go/bin`; thêm `~/go/bin` vào PATH ở cuối `~/.zshrc` (bản cũ: `~/.zshrc.bak-2026-11-15-crm`).
  - Claude Code: plugin `gopls-lsp@claude-plugins-official`, phạm vi project (ghi trong `.claude/settings.json` của CRM).

## Tái dùng

- Không có mã cũ để dùng lại. Hook dùng chung một tệp `scripts/agent/lib.sh` (danh sách vùng rủi ro cao, mẫu đường dẫn mã sinh, tệp đổi) để ba script không lặp nhau.

## Đã làm

| Hạng mục | Tệp |
|---|---|
| Luật cho mọi phiên CRM, viết tay, dưới 200 dòng | `CLAUDE.md` |
| Bốn tệp luật theo vùng, có `paths:` | `.claude/rules/{go-backend,migrations,openapi,console}.md` |
| Reviewer chỉ đọc (Read, Grep, Glob) | `.claude/agents/reviewer.md` |
| Ba hook: chặn sửa mã sinh và migration đã commit; định dạng sau mỗi lần sửa; cổng Stop | `.claude/settings.json`, `scripts/agent/{guard-edit,format,stop-check,test-guard}.sh`, `scripts/agent/{lib.sh,affected.py}` |
| Phiên bản công cụ ghim | `tools/versions.mk` |
| Thư viện, công cụ ngoài ADR-44 | `docs/phu-thuoc.md` |
| Skill Go đã soát | `.claude/skills/`, `docs/tasks/skills-vetting.md` |

## Bằng chứng

Hook thử trong hai repo tạm có cùng script (`scratchpad/hooktest`, `hooktest2`), ngày 15/11/2026:

| Tiêu chí | Cách thử | Kết quả |
|---|---|---|
| Sửa mã sinh bị chặn | `guard-edit.sh` với `internal/httpapi/gen/x.go` | Thoát 2, thông báo "là mã sinh tự động" |
| Sửa migration đã commit bị chặn | `guard-edit.sh` với `migrations/0001_a.sql` đã commit | Thoát 2 |
| Migration mới, tệp thường không bị chặn | `migrations/0002_b.sql`, `internal/platform/queue/q.go` | Thoát 0 |
| Test bị xóa, `t.Skip` mới, bớt câu kiểm ở vùng cao bị báo khi chưa ghi lý do | Xóa `b_test.go`; thêm `t.Skip` và bớt một `t.Errorf` trong `internal/platform/queue/a_test.go` | Thoát 1, nêu đủ ba mục |
| Đã ghi tên và lý do thì qua | Ghi các tệp vào mục "Test bị xóa hoặc tắt" | Thoát 0 |
| Định dạng sau mỗi lần sửa | `format.sh` trên `func   A( ) int {return 1}` | Thành `func A() int { return 1 }` |
| Tính gói bị ảnh hưởng | Đổi `internal/a`; `b` nhập `a`, `c` không | `affected.py` trả `a` và `b`, không có `c` |
| Hook Stop qua khi sạch, chặn khi test của gói phụ thuộc hỏng | Đổi `a.A()` làm `TestB` sai | Thoát 0 khi sạch; thoát 2, nêu `FAIL example.com/t/internal/b` |
| Hook Stop chặn khi build hỏng | `return "x"` trong hàm trả `int` | Thoát 2, nêu lỗi biên dịch |
| Hook Stop chặn khi thiếu công cụ | Trước khi cài Go | Thoát 2, "Thiếu công cụ 'go'" |
| Phiên CRM nạp đúng luật, có LSP | `claude --print` trong `apps/crm` | Nạp CLAUDE.md gốc (có đoạn Scope) và `apps/crm/CLAUDE.md`; luật cổng 4175 của website không áp dụng; có công cụ `LSP` |
| Phiên website không bị ảnh hưởng | `claude --print` ở thư mục gốc, hỏi về đổi màu nút trang sản phẩm | Vẫn theo đủ: mockup trước, DESIGN.md, test theo phạm vi, `radius-scale.spec.ts` |
| Skill không có quyền công cụ, script, lệnh cài, ký tự ẩn | Quét `.claude/skills/` | Không thấy; thư viện ngoài ADR-44 chỉ còn được nhắc ở dạng "không dùng" |
| Sửa lỗi tìm thấy khi thử | Lần đầu `test-guard.sh` đếm câu kiểm theo dòng nên bỏ sót | Đổi sang đếm từng câu; lần thử lại báo "số câu kiểm giảm từ 2 xuống 1" |

Chưa thử: `golangci-lint` và jscpd trong hook (chưa cài; Task 3 cài và thử); plugin LSP trên mã thật của CRM (từ Task 3).

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Đề xuất 2: chủ shop chọn giữ luật website tại chỗ và thêm đoạn phạm vi, không dời sang `.claude/rules/website.md`.
- Đề xuất 15: chỉ cài `gopls-lsp`. `typescript-lsp` để sang Task 17: TypeScript 7.0.2 trên npm chỉ có lệnh `tsc`, không có `tsserver` mà `typescript-language-server` cần; quyết cùng câu hỏi tương thích TypeScript 7 của Task 17.

## Việc còn mở

- Skill concurrency giữ `errgroup`, `singleflight` của `golang.org/x/sync` với ghi chú "cần duyệt": thư viện này ngoài ADR-44, chỉ dùng khi có task cần và ghi vào `docs/phu-thuoc.md`.

## Reviewer

Chưa chạy (Task 0 không có mã ứng dụng; reviewer dùng từ Task 3).
