# Nguồn: use-modern-go

- Repo: https://github.com/JetBrains/go-modern-guidelines
- Commit: `155dc7ca10da5e1f6c841503086957b1b37f5815`
- Giấy phép: Apache-2.0 (JetBrains). Bản dự án là bản đã sửa đổi: các thay đổi liệt kê dưới đây (Apache-2.0 mục 4b).
- Lấy ngày 15/11/2026 bằng `gh api`: `plugin/skills/use-modern-go/SKILL.md`, `scripts/VERSION`, `scripts/run-tool.sh`, `scripts/run-tool.ps1`, `README.md`, `FEATURES.md`. Không chạy script nào, không `go install`.

## Tệp đã chép

| Tệp dự án | Lấy từ | Gốc (byte) | Bản dự án (byte) |
|---|---|---|---|
| SKILL.md | `plugin/skills/use-modern-go/SKILL.md` | 2.612 | 3.728 (viết lại) |
| references/FEATURES.md | `FEATURES.md` ở gốc repo | 41.285 | 42.332 |

Không chép: `scripts/` (cả ba tệp), `README.md`.

Description: 151 → 366 ký tự (bản gốc chỉ nói "dùng CLI"; bản dự án liệt kê nội dung để agent biết khi nào mở skill).

## Script đã xem nhưng không chép

`scripts/run-tool.sh` (và bản PowerShell `run-tool.ps1`) làm đúng các việc sau:
- Đọc `scripts/VERSION` (= `v0.1.1`).
- Nếu biến môi trường `GO_MODERN_GUIDELINES_DEV` được đặt: chạy tệp `~/.cache/go-modern-guidelines/dev/go-modern-guidelines` (hoặc `%LOCALAPPDATA%`) — chạy bất kỳ tệp nào nằm ở đường đó.
- Nếu chưa có tệp chạy `~/.cache/go-modern-guidelines/v0.1.1/go-modern-guidelines`: chạy `go install github.com/JetBrains/go-modern-guidelines@v0.1.1` với `GOFLAGS=` `GOWORK=off` `CGO_ENABLED=0` `GOBIN=<thư mục tạm>` → **tải mã nguồn qua mạng** (Go module proxy), biên dịch, kiểm `--version` khớp, rồi chuyển vào `~/.cache`. Ghi tệp ngoài repo (`~/.cache`). Nếu toolchain cũ hơn 1.25 và `GOTOOLCHAIN=auto`, Go có thể tự tải thêm toolchain.
- Sau đó `exec` tệp chạy với tham số (`list`, `explain`).
- Không thấy telemetry trong script. Mã của chính CLI (`main.go`, `internal/`) **không được xem** trong vòng này.

Lý do không chép: chủ trương không cài công cụ ngoài ADR-44 và không để skill tự tải/biên dịch mã khi dùng lần đầu. Nội dung CLI trả về được sinh từ cùng `internal/guidelines/guidelines.json` với `FEATURES.md`, nên dùng `FEATURES.md` thay cho CLI.

## Sửa và bỏ (lý do)

SKILL.md (viết lại)
1. Bỏ toàn bộ hướng dẫn gọi `sh "<skill-dir>/scripts/run-tool.sh" list|explain` và "On first use, the wrapper installs the CLI". Thay bằng: đọc bản Go trong `go.mod`, tra `references/FEATURES.md`. Ghi rõ CLI không được cài.
2. Bỏ câu "Treat returned guidelines as authoritative … follow it even when nearby code or repository convention uses an older pattern" và "Always write modern …" → "Luật dự án thắng", không hiện đại hóa mã cũ ngoài task; thay đổi `modernize` ở commit riêng (luật CRM: thay đổi cấu trúc và hành vi tách commit).
3. Bỏ lệnh "Do not pipe the output through head, tail, grep…" (chỉ áp dụng cho CLI).
4. Thêm danh sách ngắn các hướng dẫn hay gặp và cảnh báo các mục Go 1.27 chưa đối chiếu release notes.
5. Frontmatter: thêm `license`, `metadata` (tác giả, bản CLI gốc), `paths: **/*.go`; không có `allowed-tools` (bản gốc cũng không có).

references/FEATURES.md
6. Dòng đầu "Generated … DO NOT EDIT" → ghi nguồn, commit, giấy phép và việc đã sửa; thêm ghi chú các mục Go 1.27 chưa xác minh.
7. `json_v2`: thêm ghi chú JSON của API do ogen sinh, mục này chỉ cho JSON viết tay.
8. `time_tick_gc`: thêm ghi chú dự án — ví dụ "After" `for range time.Tick(...)` không có lối thoát, trái luật goroutine dừng được theo ctx; thêm ví dụ `select` với `ctx.Done()` (ticker tạo một lần ngoài vòng).

## Ghi chú

- `maps_keys_values_iter` khuyên `for k := range maps.Keys(m)` thay cho `for k := range m`; với việc chỉ duyệt khóa, cách cũ vẫn đúng và đơn giản hơn. Giữ nguyên văn bản gốc, chỉ ghi nhận ở đây.
- `stdlib_uuid` (`import "uuid"`), `generic_methods`, `promoted_field_literals`, `strings.CutLast`, `URL.Clone` là khẳng định của upstream về Go 1.27, chưa kiểm.
