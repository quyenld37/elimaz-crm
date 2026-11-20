# ADR-21 — `MessagingAdapter` với `sendPolicy`; thu hẹp lời hứa adapter

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026); thay phần interface của ADR-06
Ngày: 12/11/2026
Kiểm bằng: `CarrierAdapter` giả: internal/adapters/carrier/fake/fake_test.go; `MessagingAdapter` và `sendPolicy`: chưa có, làm ở mốc M4; website dùng khóa dịch vụ: chưa có, làm ở mốc M5

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-21. Tệp này không chép lại quyết định.

Hai interface ở MVP: `MessagingAdapter` (có `sendPolicy(conversation)`, `challenge`, `verifySignature`, `split`,
`lookup`) và `CarrierAdapter`. Website là client của API bằng khóa dịch vụ. `PaymentAdapter`, `NotifierAdapter` định
khi làm tới (P2). Không hứa gì về sàn.

## Kiểm ở đâu

- `CarrierAdapter` (bộ contract test trên hãng giả): `TestClientCreateShipment`, `TestClientLookupAndStatus`,
  `TestSourceSignatureAndSplit` (internal/adapters/carrier/fake/fake_test.go). Ba hãng thật: chưa có, làm ở mốc M3.
- Bộ contract chung của `CarrierAdapter` (M3 Task 3): `TestContractFake`, `TestContractFrames`, `TestContractCatchesLie`
  (internal/adapters/carrier/carriertest/contract_test.go); testdata từng hãng ở `carriertest/testdata/<hãng>/`.
- Phần nguồn webhook dùng chung (`challenge`, `verifySignature`, `split`): `TestChallenge`, `TestVerifySignature`, `TestSplit`
  (internal/platform/selftest/source_test.go).
- `MessagingAdapter`, `sendPolicy`: chưa có, làm ở mốc M4.
- Website qua khóa dịch vụ: chưa có, làm ở mốc M5.
