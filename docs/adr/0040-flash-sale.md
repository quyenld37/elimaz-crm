# ADR-40 — Đường chịu tải cao và flash sale

Trạng thái: đã duyệt (spec 07-adr, 15/11/2026); thứ tự khóa, giới hạn thời gian, chạy lại và mã `retry_later` bổ sung bởi ADR-46
Ngày: 15/11/2026
Kiểm bằng: phép đo đầu M0: spikes/flashsale/buy_test.go (`make spike-test`), kết quả `docs/do-luong/flash-sale-go.md`; tính năng thật: chưa có, làm ở mốc M5

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-40. Tệp này không chép lại quyết định.

Bốn lớp: mạng phân phối, bộ đệm đọc, cổng điều tiết (Valkey), giữ hàng và ghi đơn trong một transaction PostgreSQL.
Mỗi đơn vị suất là một dòng `flash_sale_units`, lấy bằng `SKIP LOCKED`; giới hạn mỗi khách nằm trong
`flash_sale_claims`; cờ ở Valkey luôn dựng lại từ PostgreSQL.

## Kiểm ở đâu

- Lõi đường mua (mã đo, không phải mã sản phẩm): `TestTenUnitsFiftyBuyersGetExactlyTen`, `TestOneCustomerLimitTwoOnlyTwoBought`,
  `TestLockedUnitGivesRetryNotSoldOut`, `TestLockTimeoutExhaustedGivesRetryNotStored` (spikes/flashsale/buy_test.go).
- Thử tải đột biến 20.000 người ảo / 500 suất, một khách 100 yêu cầu song song, ghi bền 50 đơn/giây trong 5 phút: `docs/do-luong/flash-sale-go.md` (laptop; đo lại trên máy thử).
- Tắt Valkey, giết `api`, bỏ giữa chừng; đóng đợt khi còn đơn nháp giữ suất: chưa có, làm ở mốc M5.
