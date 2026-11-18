# Task 322 — Tạo lại kích thước ảnh (M1-44d)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 322, M1-44d, TP07). Loại việc (quy trình mục 3): A. Vùng rủi ro cao: không (`internal/platform/files` không trong danh sách vùng cao của CLAUDE.md; không chạm queue, commands, tenancy, idempotency, identity, guard.go).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký ký hiệu nào đã có. `files.Preset(version)` giữ chữ ký (6 chỗ gọi: `cmd/crm/main.go`, `preset.go` `SelectPreset`, `process.go` `layoutOf`, `filestest.Image`, test), chỉ đổi bên trong: đọc từ bảng `presets` (`map[int][]VariantSpec`) thay cho `if version != 1`. Thêm mới: `files.RegenerateKind`, `files.NewRegenerateHandler`, `files.EnqueueRegenerate`, `files.ErrUnknownPreset`; test-only `files.SetPresetsForTest` (export_test.go).
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/modules/catalog`, `internal/platform/files/filestest`, `internal/testkit`.
- Operation OpenAPI bị chạm, oasdiff: thêm `POST /api/v1/platform/files/regenerate` `regenerateImages` (`x-permission: platform.files.manage`, `IdempotencyKey`), schema `RegenerateImagesRequest {presetVersion}`, `RegenerateImagesResult {enqueued}`, mã lỗi mới `unknown_preset` trong enum `Error` — chỉ thêm, oasdiff không có thay đổi phá vỡ.
- Chỗ dùng trên màn hình: chưa có (màn kho tệp chưa có; `console/src/api` sinh lại có thêm kiểu, không màn nào dùng).
- Câu sqlc, bảng, pha migration: thêm câu `ListFilesMissingPreset` trong `queries/platform/files.sql` (đọc `platform.files` ⨝ `file_purposes`, `NOT EXISTS file_variants`); dùng lại `ListFileVariantsFull`, `LockFile`, `InsertFileVariant`, `GetFile`. Không bảng mới, không migration.
- Test sẽ chạy: `go test -race ./internal/platform/files/...` (gói đổi), `./internal/httpapi/...`, `./internal/modules/catalog/...`, `./cmd/crm/...`, `./internal/testkit/...` (phụ thuộc ngược); `tests/arch` (routes, boundaries); cuối cùng `make ci` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `internal/platform/files` (Processor, Preset, SelectPreset, VariantKey, OriginalKey, PublicVariants), `commands` (CommandKind, Handler, DoneHook, Looker), `queue.JobQueue.Enqueue` (DedupeKey), `httpapi.WriteTx` + `identity.RecordAudit` (đường ghi mẫu Task 12). Tìm `regenerat`, `preset` trong repo: chỉ có `Preset`/`SelectPreset` của Task 41, 44.
- Dùng lại / viết mới vì: dùng lại toàn bộ phần gọi imager của `file.process` (Task 43): `Processor.makeVariants` (bản kê, bỏ qua biến thể đã có, ký PUT, gọi imager, ghép kết quả) bằng cách dựng một `Processor` với `PresetVersion` = bản của lệnh; nguồn là `orig/<sha>` trên `BucketOriginals` (đã có từ `file.process`), nên không tải lên lại, không băm lại. Viết mới chỉ phần khác: đọc tệp ready + kiểm (`regenerate.go`), `OnDone` chỉ chèn biến thể (không đổi trạng thái tệp), `OnTerminal` không đánh dấu tệp rejected (tệp vẫn ready với bản cũ), hàm xếp lệnh hàng loạt. Đường ghi HTTP theo mẫu `changeJob` (WriteTx, audit). `SelectPreset` (lùi bản) đã có từ Task 44, `GetProduct`/`ProductImages` đã dùng — task này chỉ kiểm bằng test.

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: `platform.files`, `file_variants` thuộc kho tệp (`internal/platform/files`); lệnh và câu SQL nằm trong gói này.
- 5.2 Không ghi bảng module khác: chỉ ghi `platform.file_variants` (của gói) và `platform.jobs` qua `queue.Enqueue`; audit qua `identity.RecordAudit`.
- 5.3 Đọc module khác qua view: không đọc bảng module khác (test đọc ảnh qua `catalog.ProductImages`, public API).
- 5.4 Nguyên tử: xếp N lệnh + audit + khóa chống trùng trong một `WriteTx`.
- 5.5 Không nguyên tử đi sau commit: tạo biến thể là lệnh `file.regenerate` (commands), chạy ở worker sau commit.
- 5.6 Gọi ra ngoài: imager là "bên ngoài" qua lệnh commands như `file.process`: `AutoRetryNotSent` an toàn vì khóa đầu ra cố định theo mã băm, PUT có `If-None-Match` (không ghi đè), bản kê; lease hết → `in_doubt` (mặc định của CommandKind), `Lookup` trả `not_sent` như `file.process` (chạy lại an toàn, không có tác dụng phụ trùng).
- 5.7 Ngữ cảnh shop: đọc tệp, ghi biến thể trong `tenancy.WithTenant`/tx của Runner (tenant của lệnh); câu liệt kê chạy trong tx tenant của người gọi (RLS); không bảng mới.
- 5.8 Nghiệp vụ không biết HTTP: `regenerate.go` không nhập `net/http`; handler mỏng trong `handlers_files.go`.
- 5.9 Đường ghi có khóa chống trùng và `x-permission: platform.files.manage` trong `openapi.yaml`.
- 5.10 Valkey: không áp dụng (không dùng).
- 5.11 Thư viện mới: không.
- 5.12 Module mẫu: theo đường ghi Task 12 (`WriteTx` + audit + sentinel đổi ở `errors.go`) và lệnh `commands` của Task 43.

Mục 6:
- 6.1 Đồng thời: hai yêu cầu regenerate cùng lúc → `DedupeKey = fileId:v<preset>` (unique ở `platform.jobs`) nên mỗi tệp tối đa một lệnh; `enqueued` chỉ đếm lệnh mới tạo. Hai lệnh của hai tệp cùng nội dung chạy song song → khóa đầu ra giống nhau, PUT `If-None-Match` (412 = đã có), bản kê `Put` không ghi đè; `InsertFileVariant` `ON CONFLICT DO NOTHING`. `OnDone` khóa dòng tệp (`LockFile`, ADR-46 nhóm 12b) trước khi chèn biến thể; tệp đã xóa/không ready/đổi mã băm → bỏ.
- 6.2 Gửi lại: `Idempotency-Key` của middleware + `WriteTx` lưu kết quả; gửi lại khóa mới → các lệnh đã có không tạo lại (`enqueued: 0`).
- 6.3 Thứ tự khóa: `OnDone` khóa một dòng `files`; đường HTTP không khóa dòng nào (chỉ INSERT jobs). Không có khóa nhiều dòng.
- 6.4 Lỗi tạm DB: `WriteTx` chạy lại theo ADR-46; worker có lịch thử lại.
- 6.5 Mức cách ly: READ COMMITTED; câu liệt kê và Enqueue trong cùng tx — tệp vừa thành ready sau khi đọc thì lần gọi sau sẽ lấy.
- 6.6 Việc nền lỗi: `MaxAttempts = 3` như `file.process`; `rejected` (nội dung) → `dead`, tệp giữ `ready` với bản cũ (`SelectPreset` lùi bản), không đánh dấu `rejected`.
- 6.7 Quá tải: imager trả 503 `Retry-After` → `not_sent` với `RetryAfter` (dùng lại `callImager`).
- 6.8 Chết giữa chừng: lệnh → `in_doubt` theo commands; tra lại trả `not_sent` → chạy lại an toàn.
- 6.9 Treo: hạn mỗi lần 30 s (`CommandKind(..., 30*time.Second)`, ADR-45); số tệp lớn: một yêu cầu xếp mọi lệnh trong một tx (INSERT nhỏ; 10k tệp ≈ 10k INSERT, dưới giới hạn 30 s một câu) — ghi ở Việc phát hiện thêm nếu cần chia lô.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: "không bao giờ xếp lệnh cho tệp private", "không tải lên lại / không đổi ảnh gốc" có test cụ thể (TP07); không vùng rủi ro cao nên không rapid.
- 6.12 Dữ liệu cá nhân: payload chỉ `fileId`, `preset`; audit chỉ ghi `presetVersion`, `enqueued`; ảnh riêng tư không bị chạm (R10).
- 6.13 Quyền: `platform.files.manage` (ScopeShop) ở `x-permission`, guard mặc định từ chối; test 403 cho người không có quyền.

## Bằng chứng

Môi trường: bộ chung crm-shared (PostgreSQL 5600, `CRM_S3_PORT=20600`, `CRM_S3_OFFSITE_PORT=21600`); test tự tạo database riêng.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TP07: thêm v2 (thêm rộng 1200) → `EnqueueRegenerate(2)` xếp đúng 2 lệnh (2 ảnh sản phẩm của A), 0 lệnh cho ảnh private (R10) và ảnh của tenant B; sau worker: mỗi tệp 15 biến thể v2 có 1200, v1 giữ 12, tệp vẫn `ready`, lệnh `done` | `TestRegeneratePresetV2` | `go test -race -count=1 -run TestRegenerate ./internal/platform/files/` | `--- PASS: TestRegeneratePresetV2 (1.46s)` |
| Không tải lên lại, ảnh gốc giữ nguyên: số đối tượng `crm-tmp`, `crm-originals` không đổi, ETag ảnh gốc như cũ, ảnh gốc 1 phiên bản; imager gọi đúng 2 lần, nguồn `orig/<sha>` trên `crm-originals`, 15 đầu ra | `TestRegeneratePresetV2` (`snapshot`, `checkImagerFromOriginals`) | như trên | PASS |
| `GetProduct` (`catalog.ProductImages`) với `IMAGE_PRESET_VERSION=2`: trước khi tạo lại lùi về 12 đường dẫn v1; sau đó 15 đường dẫn `…/img/<sha>/v2/`; với 1 vẫn 12 đường dẫn v1; gọi lại → 0 lệnh | `TestRegeneratePresetV2` (`checkURLs`) | như trên | PASS |
| Bộ kích thước 0, 2 (chưa thêm), 99 → `ErrUnknownPreset`, 0 lệnh | `TestRegenerate_UnknownPreset` | như trên | `--- PASS (1.14s)` |
| Ảnh gốc mất → lệnh `dead` (`source_missing`), tệp vẫn `ready` không lý do, không biến thể v2, imager không được gọi; tệp xóa sau khi xếp lệnh → không có biến thể v2 | `TestRegenerate_SourceMissingKeepsFileReady` | như trên | `--- PASS (1.43s)` |
| HTTP `regenerateImages`: `platform.files.manage` → 200 `{"enqueued":1}` (chỉ ảnh thiếu v1; không tính ảnh đã đủ, `catalog_import`, tenant B), 1 dòng audit; gọi lại → `{"enqueued":0}`; chỉ `files.upload` → 403; 99 → 422 `unknown_preset`; 0 → 400 | `TestFilesAPI_RegenerateImages` | `go test -race -count=1 -run TestFilesAPI_RegenerateImages ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.265s` |
| Không làm hỏng phần cũ (file.process, catalog, route, kiến trúc, màn hình), hợp đồng chỉ thêm | toàn bộ | `make ci` (sau khi gộp main ff2046d, trên phần đã stage) | `EXIT 0`; 47 gói `ok`, 0 `FAIL`; `ok …/platform/files 66.9s`, `ok …/httpapi 112.3s`, `ok …/modules/catalog 47.9s`, `ok …/cmd/crm`, `ok …/tests/arch`; apicheck/oasdiff qua; console 174 test qua |

Thứ tự test trước: `regenerate_test.go` viết trước, chạy → FAIL (build: `undefined: presets`, `files.NewRegenerateHandler`, `files.EnqueueRegenerate`, `files.ErrUnknownPreset`) rồi mới viết mã. `TestFilesAPI_RegenerateImages` viết sau handler (đường HTTP mỏng), không có lần đỏ riêng.

## Quyết định thay chủ shop

- **Bảng bộ kích thước vẫn là hàm `files.Preset(v)` có sẵn (Task 41), bên trong là bảng `map[int][]VariantSpec`** thay cho tên `files.Presets map[int][]Output` của kế hoạch. Vì: `Preset` đã có 6 chỗ dùng (`main.go`, `SelectPreset`, `process.go`, `filestest`), đổi sang biến xuất ra ngoài thì phải sửa hết mà không thêm gì; biến toàn cục đổi được trái luật Go của dự án (mục 4). Bảng giữ bằng con trỏ nguyên tử, mã chạy thật chỉ đọc; test thay bảng bằng `SetPresetsForTest` (chỉ trong `export_test.go`). Dễ đảo ngược. Phương án khác: biến `Presets` xuất ra ngoài (trái luật biến toàn cục).
- **Không thêm v2 thật vào mã.** Kế hoạch ghi "thêm v2 có chiều rộng 1200" trong test; bộ kích thước thật do chủ shop chốt theo hệ thiết kế (Q13, O4), nên v2 chỉ có trong test. Khi chủ shop chốt cỡ mới: thêm một dòng vào bảng `presets`, deploy, gọi `regenerateImages`, rồi đổi `IMAGE_PRESET_VERSION`.
- **Tạo lại không đổi `files.preset_version`** (giữ bản của lần xử lý đầu), chỉ thêm dòng `file_variants`. Vì: `GetProduct` chọn bản theo `SelectPreset` trên biến thể đã có, không đọc cột này; giữ nguyên thì không cần câu UPDATE mới và chống trùng của `file.process` (tìm tệp trùng theo `preset_version`) vẫn đúng: ảnh mới tải lên với v2 không thấy tệp cũ là trùng nhưng đọc được bản kê `img/<sha>/v2/manifest.json` nên không gọi imager.
- **Hạn chờ trong hàng của `file.regenerate` là 24 giờ** (không phải 1 giờ như `file.process`): cả kho ảnh xếp một lần, imager làm 2 ảnh cùng lúc; 1 giờ có thể làm lệnh hết hạn trước khi tới lượt. Hạn mỗi lần chạy giữ 30 s (ADR-45).
- **Ảnh gốc mất → `rejected source_missing`, tệp giữ `ready`** (website tiếp tục dùng bản cũ nhờ lùi bản), không đánh dấu `rejected`/`lost`: việc đánh dấu mất thuộc Task M1-46 (đối chiếu kho).
- **Mã lỗi mới `unknown_preset` 422** cho bộ kích thước chưa có (thêm vào enum `Error` và câu tiếng Việt trong `console/src/app/errors.ts`, vì test của màn hình đòi mọi mã có câu). `presetVersion < 1` bị hợp đồng chặn (400).

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `files.Presets` (biến) → giữ hàm `files.Preset(v)` trên bảng không xuất ra ngoài (xem Quyết định).
- Kiểm "`GetProduct` với `IMAGE_PRESET_VERSION=2`" bằng `catalog.ProductImages` (hàm `getProduct` của HTTP dùng cho ảnh) trong `regenerate_test.go`; mẫu và dòng `product_images` tạo bằng `catalogtest` + một câu INSERT trong test.
- Thêm test `TestRegenerate_UnknownPreset`, `TestRegenerate_SourceMissingKeepsFileReady`, `TestFilesAPI_RegenerateImages` (ngoài ca của kế hoạch).
- Sửa thêm `console/src/app/errors.ts` (một câu cho mã lỗi mới).

## Việc phát hiện thêm

- `TestImager_HangKilledWithinDeadline` (đỏ chập chờn khi máy bận, việc treo): task này **không chạm** `internal/imager` hay `cmd/imager`, nên chưa sửa; trong lượt `make ci` của task này nó qua.
- Lệnh `file.regenerate` bị `dead` (ảnh gốc mất, imager lỗi mãi) thì gọi lại `regenerateImages` không xếp lại (khóa chống trùng `fileId:v<bản>` giữ mãi): đưa lại bằng `retryJob` ở màn việc nền. Nếu cần "chạy lại cả lô" thì là việc của màn kho tệp.
- Shop rất nhiều ảnh: một yêu cầu xếp mọi lệnh trong một transaction (mỗi tệp một INSERT). Với vài chục nghìn ảnh nên chia lô theo trang; chưa cần ở M1.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, trên `git diff --cached` sau khi gộp main ff2046d, 32 tệp).

- Không có phát hiện phải sửa. Đã kiểm: R10 chặn ba lớp (SQL `mode = 'standard'`, `EnqueueRegenerate` bỏ `Private`/không ảnh, `Execute` từ chối `Private`, biến thể chỉ ghi `BucketPublic`); tenant (RLS, `WithTenant` của lệnh, test tenant B); đồng thời (unique `dedupe_key`, `LockFile` + kiểm lại `ready`/xóa/mã băm/bản trong `OnDone`, `ON CONFLICT DO NOTHING`, từ chối vì nội dung không làm hỏng tệp `ready`); quyền, `IdempotencyKey`, lỗi đổi một chỗ; bảng bằng chứng khớp test.
- Ghi chú không bắt buộc (không sửa): `presets` đổi được qua `SetPresetsForTest` — nếu sau này gói `files` dùng `t.Parallel`, `TestRegenerate_UnknownPreset` có thể đỏ khi chạy cùng test thay bảng; lệnh `dead` không xếp lại bằng `regenerateImages` (đã ghi ở Việc phát hiện thêm).
