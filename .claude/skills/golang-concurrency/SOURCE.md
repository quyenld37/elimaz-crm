# Nguồn: golang-concurrency

- Repo: https://github.com/samber/cc-skills-golang
- Commit: `d68d80aec298e1b74b37c2a472c002b5217248b3`
- Thư mục gốc: `skills/golang-concurrency/` (bỏ `evals/`)
- Giấy phép: MIT (tác giả samber).
- Lấy ngày 15/11/2026 bằng `gh api`; không chạy script nào.

## Tệp đã chép

| Tệp | Gốc (byte) | Bản dự án (byte) |
|---|---|---|
| SKILL.md | 9.540 | 9.658 |
| references/channels-and-select.md | 4.450 | 4.450 (không đổi) |
| references/pipelines.md | 7.381 | 7.277 |
| references/sync-primitives.md | 8.850 | 10.167 |

Description: 732 → 667 ký tự.

## Sửa và bỏ (lý do)

SKILL.md
1. Frontmatter: bỏ `allowed-tools`, `compatibility`, `metadata.openclaw`. Description: bỏ `errgroup`, `singleflight` khỏi danh sách chủ đề và hai lời trỏ skill samber chưa cài; thêm "every goroutine owned; golang.org/x/sync needs approval".
2. Bỏ "Orchestration mode … `ultracode`"; Audit mode chỉ chạy song song sub-agent khi người dùng yêu cầu. "Parallelizing Concurrency Audits" → "Concurrency Audits" (giữ 5 việc cần kiểm).
3. Thêm khối luật dự án: goroutine có chủ; `golang.org/x/sync` không có trong ADR-44 (cần duyệt, ghi `docs/phu-thuoc.md`); không biến toàn cục thay đổi được, không `init()`.
4. Nguyên tắc 9 (goleak) → synctest + test tắt êm + `-race`.
5. Bảng "WaitGroup vs errgroup": thêm cột cách làm bằng thư viện chuẩn (mặc định), cột errgroup ghi "cần duyệt". Bảng sync primitives: `singleflight`/`errgroup` ghi "cần duyệt".
6. Checklist: thêm câu "Who owns it?". Common mistakes: `errgroup.SetLimit` → worker cố định hoặc semaphore channel.
7. Pipelines: bỏ nhắc `samber/ro`. Cross-References → skill đã cài.
8. Goroutine leak profile Go 1.27: giữ, gắn "chưa xác minh"; thêm "pprof chỉ trên listener nội bộ"; dòng `goleak` → synctest.

references/pipelines.md
9. Mục errgroup gắn "(cần duyệt)" kèm ghi chú; mục semaphore viết lại bằng `wg.Go` + `errors.Join` + `ctx.Done()` (bản gốc bỏ qua lỗi và kết luận "prefer errgroup").
10. Bỏ mục `samber/ro` (thư viện ngoài ADR-44). Mục goleak → synctest. Bảng lỗi: bỏ `errgroup.SetLimit`. Bỏ lời trỏ skill samber.

references/sync-primitives.md
11. Thêm ghi chú luật dự án ở đầu tệp.
12. Ví dụ dùng biến gói `requestCount`, `shuttingDown`, `cache sync.Map`, `bufPool`, `loadConfig = sync.OnceValue(...)`, `group singleflight.Group` → trường của struct. Lý do: cấm biến toàn cục thay đổi được.
13. Ví dụ `sync.Once` khởi tạo lười `sql.Open("postgres", dsn)` rồi `panic` → dựng tường minh trong `main`; giữ `sync.Once` cho `Close` (pgx). `OnceValue` + `panic(err)` → `OnceValues` trả lỗi, giữ trong trường struct.
14. Các câu "use errgroup" ở mục WaitGroup → kết hợp `WaitGroup` + `context.WithCancelCause` + ô kết quả riêng (hoặc errgroup khi được duyệt). Mục singleflight/errgroup gắn "(cần duyệt)". Bỏ lời trỏ skill samber.

## Ghi chú

- channels-and-select.md giữ nguyên: các ví dụ đã có ctx và lối thoát.
- Không thấy script, hook, lệnh mạng hay telemetry (lệnh `curl localhost:6060/debug/pprof/...` là ví dụ đọc pprof cục bộ, không tự chạy).
