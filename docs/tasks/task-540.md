# Task 540 — Chuyển dữ liệu Nhanh: sửa spec, lệnh chụp chỉ đọc

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md` (Task 540 = M7-40; bản chốt `2026-11-28-crm-m7-chuyen-du-lieu-nhanh.md`). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu cũ nào. Thêm mới gói `internal/adapters/nhanh` (Client, ErrPathNotAllowed, Collect, WriteSnapshot, PruneSnapshots) và `cmd/crm/nhanh_snapshot.go`. Sửa `run` (`cmd/crm/main.go`, 1 chỗ gọi `main`, test `main_test.go`) thêm một nhánh đầu hàm cho lệnh mới và câu báo "lệnh lạ". `tests/arch/no_nhanh_test.go`: `skipRepo` (3 chỗ gọi trong cùng tệp) thêm đúng hai đường dẫn được phép.
- Gói phụ thuộc ngược (go list): gói mới chỉ `cmd/crm` nhập (thêm test kiến trúc đòi đúng như vậy). `cmd/crm` là gói main, không ai nhập.
- Operation OpenAPI bị chạm, oasdiff: không (lệnh CLI, không có route).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không có (không database; bản chụp ra tệp; xem "Khác kế hoạch" về kho tệp).
- Test sẽ chạy: `go test ./internal/adapters/nhanh/`, `go test -run 'Nhanh' ./cmd/crm/`, `go test -run 'Nhanh' ./tests/arch/`; ci-task chạy đủ gói đổi.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (mục thử lại, giới hạn tốc độ): `cache.RateLimiter` là của Valkey cho đường vào của mình; `db.Backoff` là cho transaction; không có gì là khách HTTP gọi ngoài có ERR_429 theo `unlockedAt`. Adapter mẫu `cfaccess`, `carrier/*` (khách HTTP, bộ nối ở `internal/adapters/<tên>`). `cmd/crm/import_website.go` (mẫu lệnh một lần).
- Dùng lại / viết mới vì: theo mẫu adapter + lệnh một lần; viết mới khách Nhanh vì chưa có nơi nào trong CRM nói chuyện với Nhanh (cố ý, ADR-11). Mẫu chờ theo `unlockedAt` chép ý của `apps/storefront-seo/src/server/nhanh/client.ts` (website, TypeScript, khác ngôn ngữ nên không dùng lại mã). Không thêm thư viện (chỉ stdlib).

## Soát thiết kế

5.1–5.3, 5.7, 5.9: không áp dụng: lệnh CLI không database, không module, không route, không tenant.
5.4, 5.5: không áp dụng: không transaction, không sự kiện.
5.6 (gọi ra ngoài): đây là đường ĐỌC; mọi lời gọi là POST đọc, thử lại an toàn vì không đổi gì trên Nhanh; không có trạng thái `in_doubt` vì không có kết quả phải tránh gửi hai lần. Đường cho phép là danh sách trắng chỉ gồm đường đọc; đường khác lỗi trước khi gửi.
5.8: gói adapter không nhập module nghiệp vụ; module không nhập adapter (test kiến trúc).
5.10: không Valkey. 5.11: chỉ thư viện chuẩn. 5.12: theo mẫu adapter + lệnh một lần.
6.1–6.5, 6.8–6.11, 6.13: không áp dụng: không database, không tiền, không quyền (chạy trong container server do chủ shop).
6.7 (429): chờ tới `unlockedAt` rồi mới gọi lại, không gọi trong lúc khóa (gọi tiếp làm tăng khóa); chờ quá trần thì dừng báo lỗi.
6.9: mỗi lời gọi có thời gian chờ 30 s; cả lệnh theo ctx (SIGTERM).
6.12: bản chụp chứa dữ liệu khách thật: chỉ ghi tệp 0600 trong thư mục 0700 trên server, không log/in giá trị (chỉ đếm), lỗi không chép thân trả lời, token không vào lỗi; xóa tệp quá 30 ngày; test dùng dữ liệu giả.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đường ghi/lạ bị chặn trước khi gửi (0 lời gọi mạng) | `TestCall_RejectsNonReadPathsBeforeSending`, `TestReadPaths_NoWriteVerb` | `go test -race -count=1 ./internal/adapters/nhanh/` | ok |
| Phân trang, lỗi giới hạn tốc độ (chờ tới `unlockedAt`, quá trần thì dừng), 5xx thử lại, lỗi không lộ thân/token | `TestCall_RequestShapeAndPaging`, `TestCall_RateLimitWaitsUntilUnlockedAt`, `TestCall_RateLimitBeyondMaxWaitFails`, `TestCall_ErrorsDoNotLeakBodyOrToken`, `TestCall_RetriesServerErrors` | như trên | ok |
| Mặc định 50 mẫu mới nhất + biến thể + chi tiết + tồn; trường thiếu thành Gap; khách qua 3 trang | `TestCollect_DefaultFiftyNewestWithVariantsDetailStock` | như trên | ok |
| Hạn cứng 50, lỗi trước mọi lời gọi; danh sách mã tự chọn; `--since`; con trỏ trang lặp | `TestCollect_HardLimitFifty`, `TestCollect_ExplicitProductIDs`, `TestCollect_SinceSendsUpdatedAtFrom`, `TestCollect_StuckPaginatorFails`, `TestCollect_SurvivesRateLimit` | như trên | ok |
| Bản chụp 0600/0700, ghi nguyên tử, xóa sau 30 ngày | `TestWriteSnapshot_PrivateAtomic`, `TestPruneSnapshots_ThirtyDays` | như trên | ok |
| Lệnh CLI: chỉ in số đếm, không tên/SĐT/token; sai cách gọi không gọi mạng; lỗi API không ghi tệp | `TestNhanhSnapshot_*`, `TestParseSince` | `go test -race -count=1 -run 'Nhanh\|ParseSince' ./cmd/crm/` | ok |
| Ngoại lệ ADR-48 hẹp: mọi nơi khác vẫn cấm, chỉ lệnh nhập gói adapter | `TestNoNhanh_RealCode`, `TestNoNhanh_ArchDir`, `TestNoNhanh_FindsSample`, `TestNhanhAdapter_OnlySnapshotCommandImports` | `go test -count=1 -run 'TestNhanh\|TestNoNhanh' ./tests/arch/` | ok |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- **Kho tệp → thư mục riêng trên server.** Kế hoạch nói lưu bản chụp vào kho tệp mục đích `nhanh_snapshot` (xóa sau 30 ngày). Mục đích mới cần migration (`platform.file_purposes`) và luật hết hạn theo tiền tố của bucket (`docker/seaweedfs/bootstrap.sh`, hạ tầng: cấm đổi ở phiên này). Bản chụp ghi ra `CRM_NHANH_SNAPSHOT_DIR` (mặc định `/var/lib/crm/nhanh-snapshot`, tệp 0600, thư mục 0700) và chính lệnh xóa tệp quá 30 ngày mỗi lần chạy. Task 541/542 đọc tệp từ đó. Đổi sang kho tệp sau là việc nhỏ, đứng sau hàm `WriteSnapshot`.
- Spec ở `docs/crm/specs/2026-11-10-crm-rieng/` (ADR-48 trong 07, 06 mục 1, 2, 5, 00-tom-tat, README) nằm NGOÀI repo git của apps/crm nên được sửa trên đĩa, không nằm trong commit; bản nhắc ADR-48 ở `docs/adr/0048-*.md` và dòng trong CLAUDE.md thì nằm trong commit. Kịch bản kiểm thử 11 nhóm "Dữ liệu ban đầu" chưa sửa (kế hoạch chốt chỉ nêu 07, 06, 00, README).
- Chưa xác minh với API Nhanh thật (chờ token, server thử): khuôn thân yêu cầu (`filters`/`paginator`), tên trường (`id`, `parentId`, `status`, `content`, `mobile`), `product/detail` trả object hay mảng (code nhận cả hai), `product/inventory` lọc theo `ids`. Lệnh giữ JSON gốc nên sai tên trường chỉ làm sai phần chọn mẫu/đếm trường thiếu, không mất dữ liệu.

## Quyết định thay chủ shop

- Nơi lưu bản chụp: thư mục server thay kho tệp (vì: không đổi hạ tầng, không migration; dễ đảo ngược; phương án khác: mục đích tệp `nhanh_snapshot` + migration 0712 + luật bucket, để Task 541/542 hay sau).
- Lệnh không cần database và chạy trước khi nạp cấu hình vai trò (vì: chạy được ở bất kỳ container nào có `CRM_NHANH_*`; phương án khác: chạy như worker một lần).
- Ngoại lệ test kiến trúc theo đường dẫn (gói adapter + hai tệp lệnh) kèm test chỉ-lệnh-nhập (vì: giữ nguyên ý MO07/TI06 cho phần còn lại).

## Việc phát hiện thêm

- Task 541/542 cần: đọc tệp bản chụp (`internal/adapters/nhanh.Snapshot`, version 1), nhập qua đường sẵn có; danh sách `Gap` cho nhân viên soát.
- Sau khi chạy thật cần xác minh các điểm "chưa xác minh" ở trên và sửa tên trường nếu lệch.

## Reviewer

Kết luận: CHƯA REVIEW
