# Task 246 — Threat model STRIDE M7

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md` (Task 246 = M7-01). Loại việc (quy trình mục 3): A, chỉ đổi tài liệu. Kết quả: `docs/threat-model-m7.md`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task không sửa mã Go).
- Gói phụ thuộc ngược (go list): không có.
- Operation OpenAPI bị chạm, oasdiff: không có (chỉ đọc kế hoạch; operation M7 do Task 248, 260 viết).
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: không có test Go nào bị ảnh hưởng. Kiểm tài liệu: mọi tên test M0/M1 trích trong threat model có thật (`grep -rln` ở bảng Bằng chứng); mỗi dòng STRIDE có ít nhất một test hoặc ghi rõ "rủi ro chấp nhận" (lệnh đếm ở bảng Bằng chứng).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: không viết hàm, kiểu nào.
- Dùng lại / viết mới vì: theo mẫu `docs/threat-model-m6.md` (sơ đồ dữ liệu, bảng mỗi ranh giới `STRIDE | Mối đe dọa | Cách xử lý | Task | Test`, rủi ro chấp nhận); tên thật của mốc trước lấy từ `docs/khoi-dong/doi-chieu-gia-dinh-m7.md` (Task 245); không nhắc lại ranh giới của M0–M6 (dẫn tới `threat-model-m{0,1,…}.md`, test M0/M1 có thật như `TestClientIP`, `TestReadZipBomb`).

## Soát thiết kế

Không áp dụng mục 5.1–5.12, 6.1–6.13: task chỉ đổi tài liệu, không chạm database, tiền, quyền, hàng đợi. Nội dung của tài liệu chính là soát thiết kế bảo mật cho các task M7 sau; điểm thiếu ghi ở "Việc phát hiện thêm" cho task tương ứng.

## Quyết định thay chủ shop

Chủ shop vắng tới sáng, giao toàn quyền trong phạm vi kế hoạch, spec.

| # | Câu hỏi | Chọn | Vì sao | Phương án khác |
|---|---|---|---|---|
| QĐ-246-1 | Kế hoạch đòi bảng STRIDE cho cả nhóm đăng nhập riêng 248–259, nhưng đợt 28/11 để sau nhóm này | Viết đủ bảng cho mọi task M7 (cả task để sau), đánh dấu *(để sau)* | Mối đe dọa không mất đi khi task hoãn; khi mở task lối khẩn cấp gọn (gom 248–259) đã có danh sách phải phủ; Task 280 hoàn thiện như M6-21 | Chỉ viết phần của task đợt này (mất dấu khi gom 248–259) |
| QĐ-246-2 | Step 2: ca thiếu "báo chủ shop (có thể phải sửa kế hoạch)" | Không sửa kế hoạch; ghi từng ca thiếu vào "Việc phát hiện thêm" kèm task và tên test đề xuất, dòng threat model ghi **(thêm)** | Sửa kế hoạch là việc của điều phối viên/chủ shop; task nhận việc đọc threat model ở Step 0 (Global Constraints: review độc lập đối chiếu `threat-model-m7.md`) nên ca thêm không bị sót | Tự sửa kế hoạch (ngoài phạm vi task này) |
| QĐ-246-3 | Danh sách chương ASVS 5.0.0 khi không đọc được bản gốc (không gọi mạng ra ngoài trong task này) | Ghi tên chương theo trí nhớ, gắn **chưa xác minh**; Task 280 đọc bản gốc và sửa | Đúng câu chữ kế hoạch ("chỉ tên chương… chưa xác minh") | Tra web bây giờ (kế hoạch giao việc đọc bản gốc cho Task 280) |

## Phụ thuộc task để sau

Các dòng *(để sau)* của `docs/threat-model-m7.md` thuộc 248–259, 266–268, 270–272, 274–280; test của chúng chưa viết. Không task nào của đợt 28/11 bị chặn.

## Dừng để hỏi

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Sơ đồ dữ liệu có đủ sáu luồng của kế hoạch | đọc tệp | `for p in "crm-direct" "DIRECT_LOGIN_UNTIL" "NGOÀI hệ thống" "kho tệp riêng tư" "Meta Graph" "web.catalog.preview"; do grep -c "$p" docs/threat-model-m7.md; done` | `crm-direct=1 DIRECT_LOGIN_UNTIL=3 NGOÀI hệ thống=1 kho tệp riêng tư=2 Meta Graph=1 web.catalog.preview=3` (mỗi luồng ≥ 1) |
| Đủ 15 mối đe dọa kế hoạch nêu (mật khẩu đoán được, dùng lại TOTP, phiên chờ, đổi `DIRECT_LOGIN_UNTIL`, sửa tệp nhập, ai đặt lại bước hai, ai bấm mở bán, liệt kê email, bí mật trong `idempotency_keys`, log/cảnh báo, tệp dự phòng, argon2id, tệp rất lớn, tự đặt lại cho mình, quyền xem trước đặt hàng) | đọc tệp | `grep -c -- "<cụm>" docs/threat-model-m7.md` cho từng cụm: "Mật khẩu đoán được", "Dùng lại mã TOTP", "Phiên chờ bước hai dùng như phiên thật", "đặt \`DIRECT_LOGIN_UNTIL\`", "Gắn tệp sai", "Chối đã đặt lại bước hai", "Chối đã quyết", "Liệt kê email", "idempotency_keys", "lọt vào log", "Tệp đơn dự phòng", "argon2id (19 MiB", "Tệp rất lớn", "Tự đặt lại bước hai cho mình", "dùng để \*\*đặt hàng\*\*" | 15/15 cụm có (≥ 1 lần mỗi cụm) |
| Mỗi dòng STRIDE có test, hoặc ghi rủi ro chấp nhận (Step 2) | đếm | `awk -F'\|' '/^\| [STRIDE, ]+ \|/ && !/^\| STRIDE \|/ {n++; t=$(NF-1); …; if ((t==""\|\|t=="—") && $0 !~ /rủi ro chấp nhận/) bad++}'` | `dòng STRIDE: 72 thiếu test: 0 chấp nhận: 5 thêm: 13` (thêm điều kiện đếm `/\(thêm/`); 13 dòng có ca **(thêm)** (F1–F13 ở "Việc phát hiện thêm") |
| Tên test M0/M1 trích dẫn có thật | grep | `grep -rln "func TestProdCompose_OnlyCloudflareIngress\|func TestClientIP\|func TestReadZipBomb(\|func TestReadZipBombLyingHeader\|func TestReadSizeLimits\|func TestReadTooManyRows" tests internal` | `tests/deploy/compose_test.go`, `internal/platform/xlsx/read_test.go`, `internal/platform/webhooks/lockout_test.go`; đếm hàm: 6/6 |
| Mẫu kiểm tệp có thật (F2) | đọc mã | `sed -n 68,85p internal/modules/catalog/imports.go` | `files.Status` → `ErrNotFound` (không có, tenant khác, đã xóa), `ErrFileNotReady` (sai mục đích, chưa `uploaded`) |
| Thuật toán `fieldcrypt` ghi trong mục ASVS V11 | đọc mã | `grep -n GCM internal/platform/fieldcrypt/*.go` | `fieldcrypt.go:2` AES-256-GCM, `keyring.go:36` `cipher.NewGCM` |
| Cổng tĩnh của task (gitleaks, danh mục, lint tài liệu) | ci-task | chạy trên bản đã sửa sau review (lượt đầu bị giới hạn 10 phút của lệnh nền dừng giữa chừng; lượt hai dừng vì đã sửa theo reviewer) | `CI-TASK XANH (từ xa trên github…)`, run 46066692528: `static: success`, test: không gói nào |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch Step 2 "báo chủ shop": báo trong báo cáo cuối phiên và mục dưới, không sửa kế hoạch (QĐ-246-2).

## Việc phát hiện thêm

Ca kế hoạch chưa có (dòng **(thêm)** trong `docs/threat-model-m7.md`). Task nhận việc thêm ca vào danh sách test ở Step 1 của mình:

| # | Task | Mối đe dọa | Test đề xuất |
|---|---|---|---|
| F1 | 254 (để sau) | Liệt kê email bằng thời gian: nhánh `locked`, `not_enrolled`, đang khóa tạm trả ngay không băm, nhanh hơn email lạ (`DummyVerify`) | Ba nhánh gọi `DummyVerify`; `login_test.go`: mọi đường thất bại đúng 1 lần băm (đếm bằng móc `Hasher`) |
| F2 | 263 (đợt này) | Gắn tệp sai mục đích / chưa tải xong / tenant khác vào lệnh nhập số đếm | Kiểm `files.Status` như `catalog.CreateImport` (`ErrFileNotReady`); `count_import_test.go`: mục đích `catalog_import` → 422, chưa `complete` → 422, tenant khác → 404 |
| F3 | 263 (đợt này) | Nhập số đếm vào phiên của kho không được giao | `count_import_test.go`: `inventory.stocktake` ở kho khác → 403 |
| F4 | 268 (để sau) | Như F2 cho `createFallbackImport` | `fallback_import_test.go` cùng ba ca |
| F5 | 268 (để sau) | Tệp dự phòng quá 5 MiB (kế hoạch chỉ có ca này ở 263) | `fallback_import_test.go`: `.xlsx` 6 MiB → `file_too_large` |
| F6 | 268 (để sau) | Tệp dự phòng (SĐT, địa chỉ) nằm lại trên kho mãi, kế hoạch không có hạn | Hạn (đề xuất 30 ngày sau `done`) qua `ExpiresAt` của `files` + lượt dọn; test `expires_at` có và lượt dọn đánh dấu xóa |
| F7 | 261 (đợt này) | Chối đã hiện/ẩn sản phẩm: kế hoạch có audit trong Interfaces nhưng không có ca test | `launch_test.go`: mỗi sản phẩm hiện/ẩn đúng 1 dòng audit |
| F8 | 266 (để sau) | Đọc báo cáo T2 của Page không được giao | test handler: quyền ở P1 đọc P2 → 403 |
| F9 | 270 (để sau) | Chối quyết định "đi/dừng", "mở bán": Interfaces chỉ có audit cho `decision go`; `decision stop` và `openSales` không có audit (đổi nhỏ thiết kế: thêm audit) và không ca nào kiểm | Thêm audit cho `stop`, `openSales`; `launch_test.go`: `decision go`, `decision stop`, `openSales` mỗi cái 1 dòng audit `actor_id = decider` |
| F10 | 253 (để sau) | Chối đã bật TOTP: Interfaces có audit `identity.totp_activated` nhưng Step 1 chỉ kiểm audit `identity.password_set` | `handlers_me_directlogin_test.go`: `confirmMyTotp` đúng → 1 dòng `identity.totp_activated`, không chứa bí mật, mã khôi phục |
| F11 | 268 (để sau) | Mẫu bảng tính dự phòng lộ dữ liệu khách (Step 1 chỉ có ca 3 trang, 300 Mã dòng, `=HYPERLINK`) | `fallback_template_test.go`: trang "Đơn" chỉ có Mã dòng, không trang nào chứa tên, SĐT, địa chỉ khách của dữ liệu thử |
| F12 | 269 (đợt này) | Báo cáo sẵn sàng "xanh" giả cho mục chưa có nguồn: quyết định điều phối 28/11 (mục chưa có nguồn → `warn` "chưa có nguồn") chưa có trong Step 1 (chỉ 14 cặp `fail` → `ok`) | `readiness_test.go`: mỗi mục `identity.enrollment_complete`, `identity.no_grant_users`, `conversation.t2_pass`, phần của Task 267 → `warn`, `Value` "chưa có nguồn", không `ok` |
| F13 | 276 (để sau) | Ba rủi ro chấp nhận dựa vào câu tài liệu mà Interfaces của Task 276 chưa bắt buộc | Thêm vào nội dung bắt buộc: `dang-nhap-du-phong.md` — quản trị xác minh người gọi trước khi đọc mã đặt lại (gọi lại số đã lưu, hỏi trực tiếp); `du-phong.md` — bảng tính đã điền chỉ lưu ở máy shop, xóa sau khi nhập lại; `so-tay-dem-mo.md` — thu hồi khóa dịch vụ xem trước của website thử sau đêm mở |

Báo chủ shop: F2, F3, F7, F12 thuộc task đợt này (261, 263, 269) — phiên của task đó thêm ca ở Step 1 (ca thêm, không đổi Interfaces). F1, F4–F6, F8–F11, F13 thuộc task để sau; khi mở task cần sửa kế hoạch thêm ca. F6 (hạn giữ tệp dự phòng, chủ shop chốt số ngày) và F9 (thêm audit cho `stop`, `openSales`) là đổi nhỏ thiết kế, không chỉ thêm ca test.

## Reviewer

Kết luận: **ĐẠT** (lượt 2).

- Lượt 1: CẦN SỬA, 5 phát hiện về tính đúng: (1) F9 nói audit có sẵn cho cả ba quyết định, thật ra kế hoạch chỉ có cho `go`; (2) dòng "đã bật TOTP" không có ca test; (3) ca "mẫu không có dữ liệu khách" của Task 268 bị trình bày như ca có sẵn; (4) ca "mục chưa có nguồn → `warn`" của Task 269 không có trong kế hoạch; (5) ba rủi ro chấp nhận dựa vào câu tài liệu mà Task 276 chưa bắt buộc. Đã sửa: dòng tương ứng đánh dấu **(thêm)**, thêm F10–F13, F9 ghi rõ đổi nhỏ thiết kế.
- Lượt 2: ĐẠT, không phát hiện mới. Ghi chú không bắt buộc: phiên Task 269 kiểm lại `identity.no_grant_users` — nếu đọc được grant không cần Task 253 thì tính `ok`/`fail` thật thay vì `warn`.
