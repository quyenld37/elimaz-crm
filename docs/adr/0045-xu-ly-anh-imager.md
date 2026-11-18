# ADR-45 — Xử lý ảnh bằng libvips trong tiến trình `imager` riêng

Trạng thái: đã duyệt (spec 07-adr, 15/11/2026); thay chữ "bằng sharp" của ADR-36
Ngày: 15/11/2026
Kiểm bằng: internal/imager (pipeline_test.go, runner_test.go, server_test.go, compose_test.go; `make test-imager`, `make test-imager-compose`); `TestFileProcess_HangDoesNotBlockWorker` (internal/platform/files/process_test.go); số đo `docs/do-luong/imager-m1.md`

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-45. Tệp này không chép lại quyết định.

Tiến trình thứ tư `imager`, image riêng có libvips; không giữ khóa bí mật, không nối database; worker gửi đường dẫn ký
sẵn có hạn. Mỗi ảnh đi bốn bước có giới hạn (đọc tối đa dung lượng, kiểm loại bằng nội dung, `vipsheader` kiểm số điểm
ảnh trước khi giải mã, `vips` mã hóa lại bỏ siêu dữ liệu), mỗi lệnh là tiến trình con có hạn giờ và giới hạn bộ nhớ.

## Kiểm ở đâu

- Bước (2), (3): `TestImager_RejectsHTMLAsJPEG`, `TestImager_PixelBombRejectedBeforeDecode`, `TestImager_TypeNotAllowed` (internal/imager/pipeline_test.go).
- Lệnh treo bị giết đúng hạn, worker không bị ảnh hưởng: `TestImager_HangKilledWithinDeadline` (pipeline_test.go); `TestExecRunner_TimeoutKillsProcessGroup`,
  `TestExecRunner_MemoryLimit` (internal/imager/runner_test.go); `TestFileProcess_HangDoesNotBlockWorker` (internal/platform/files/process_test.go).
- Ảnh ra không còn GPS: `TestImager_PhotoGPS_TwelveOutputsNoMetadata` (pipeline_test.go); `TestFilesPipeline_PublicPhotoGPS` (tests/e2e/files_pipeline_test.go).
- `imager` không nối được database, không bí mật, chỉ đọc: `TestImagerCompose_CannotReachPostgres`, `TestImagerCompose_NoSecretsInEnvironment`,
  `TestImagerCompose_ReadOnlyAndUnprivileged` (internal/imager/compose_test.go); luật (g) trong tests/arch/boundaries_test.go.
- Đo thời gian và CPU mỗi ảnh: `TestImager_Measure` (pipeline_test.go), `docs/do-luong/imager-m1.md`.
