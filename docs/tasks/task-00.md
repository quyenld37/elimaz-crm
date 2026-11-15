# Task 00 — Cài đặt agent cho `apps/crm`

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`, Task 0. Chủ shop duyệt kế hoạch và Task 0 ngày 15/11/2026.

## Ảnh hưởng

- Repo mới `apps/crm`, chưa có mã Go hay TypeScript: không có ký hiệu, gói, operation, bảng nào bị chạm.
- Ngoài repo: CLAUDE.md gốc của thư mục "Thời trang" (luật website) — **chưa sửa**, chờ chủ shop chọn cách (xem "Việc còn mở").
- Cài đặt Claude Code: hai plugin `gopls-lsp`, `typescript-lsp` — **chưa cài**, cần Go và các công cụ (xem "Việc còn mở").

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

Thử trong một repo tạm có cùng script (`scratchpad/hooktest`), ngày 15/11/2026:

| Tiêu chí | Cách thử | Kết quả |
|---|---|---|
| Sửa mã sinh bị chặn | `guard-edit.sh` với `internal/httpapi/gen/x.go` | Thoát 2, thông báo "là mã sinh tự động" |
| Sửa migration đã commit bị chặn | `guard-edit.sh` với `migrations/0001_a.sql` đã commit | Thoát 2 |
| Migration mới, tệp thường không bị chặn | `migrations/0002_b.sql`, `internal/platform/queue/q.go` | Thoát 0 |
| Test bị xóa, `t.Skip` mới, bớt câu kiểm ở vùng cao bị báo khi chưa ghi lý do | Xóa `b_test.go`; thêm `t.Skip` và bớt một `t.Errorf` trong `internal/platform/queue/a_test.go` | Thoát 1, nêu đủ ba mục |
| Đã ghi tên và lý do thì qua | Ghi hai tệp vào mục "Test bị xóa hoặc tắt" | Thoát 0 |
| Hook Stop chặn khi có tệp Go đổi mà thiếu công cụ | `stop-check.sh` với `go.mod` và tệp `.go`, máy chưa có Go | Thoát 2, "Thiếu công cụ 'go'" |
| Sửa lỗi tìm thấy khi thử | Lần đầu đếm câu kiểm theo dòng nên bỏ sót; đổi sang đếm từng câu | Lần thử lại báo "số câu kiểm giảm từ 2 xuống 1" |

Chưa thử được (cần Go): định dạng `gofumpt`, cổng build/vet/lint/test trên gói thật, tính gói phụ thuộc ngược bằng `affected.py`, plugin LSP. Thử lại ngay sau khi cài Go.

## Test bị xóa hoặc tắt

(không có)

## Việc còn mở

1. Cài Go và công cụ (cần chủ shop OK việc tải về).
2. Cách xử lý luật website trong CLAUDE.md gốc (cần chủ shop chọn).
3. Kiểm tách luật bằng một phiên CRM và một phiên website (Step 3 của Task 0), sau khi chọn cách ở mục 2.

## Reviewer

Chưa chạy (Task 0 không có mã ứng dụng; reviewer dùng từ Task 3).
