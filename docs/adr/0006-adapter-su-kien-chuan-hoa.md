# ADR-06 — Adapter và sự kiện chuẩn hóa

Trạng thái: đã duyệt (spec 07-adr, 11/11/2026); phần bốn interface và lời hứa "thêm kênh chỉ cần adapter" thay bởi ADR-21
Ngày: 11/11/2026
Kiểm bằng: `TestSourceSignatureAndSplit`, `TestIngestPipeline` (internal/adapters/carrier/fake/fake_test.go); adapter thật: chưa có, làm ở mốc M3 (hãng) và M4 (Facebook)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-06. Tệp này không chép lại quyết định.

Nguyên tắc còn hiệu lực: lõi chỉ làm việc với sự kiện đã chuẩn hóa và cờ năng lực; kiểu dữ liệu của nhà cung cấp
không ra khỏi thư mục adapter; mỗi adapter có contract test trên payload thật đã ghi lại.

## Kiểm ở đâu

- Adapter hãng giả (kiểm chữ ký, tách gói, đường nhận): `TestClientCreateShipment`, `TestSourceSignatureAndSplit`,
  `TestIngestPipeline` (internal/adapters/carrier/fake/fake_test.go).
- Nguồn selftest theo cùng interface: `TestVerifySignature`, `TestChallenge`, `TestSplit` (internal/platform/selftest/source_test.go).
- Contract test trên payload thật của hãng: chưa có, làm ở mốc M3; của Facebook: chưa có, làm ở mốc M4.
