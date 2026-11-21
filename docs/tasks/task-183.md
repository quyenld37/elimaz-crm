# Task 183 — M5-00: Tiêu chí nghiệm thu, đối chiếu giả định với mã M1–M4, threat model M5

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 183 = M5-00). Loại việc (quy trình mục 3): A, chỉ đổi tài liệu. Dùng và bổ sung bảng "Đối chiếu với mã thật (Task 439)" ở đầu kế hoạch M5.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task không sửa mã Go).
- Gói phụ thuộc ngược (go list): không có.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có (chỉ đọc `migrations/*.sql` để đối chiếu). `docs/adr/0046-thu-tu-khoa.md` được **chèn** bảng của M5 (nhóm 2, 7, 8; nhóm 10, 11 đã có sẵn) — tệp này do `tests/arch/lockorder_frame_test.go` đọc (`TestLockOrder_*`: danh sách M0–M2 và khung M3/M4 phải còn nguyên, đúng thứ tự), nên chèn phải giữ dạng dòng `| <nhóm> | … | … | … |` và chỉ thêm tên bảng vào ô thứ ba.
- Test sẽ chạy: `go test ./tests/arch/ -run 'LockOrder'` (bảo đảm phần chèn không phá test đọc ADR-46); kiểm tài liệu bằng lệnh đếm mã ca (bảng Bằng chứng).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: không viết hàm, kiểu nào.
- Dùng lại / viết mới vì: theo mẫu có sẵn `docs/m3/doi-chieu-m2.md` (bảng đối chiếu của Task 91), `docs/threat-model-m3.md` (STRIDE), tiêu chí nghiệm thu theo dạng bảng của Task 91; bảng "Đối chiếu với mã thật (Task 439)" trong kế hoạch M5 được dùng lại làm điểm xuất phát, chỉ bổ sung dòng mới và sửa dòng sai.

## Soát thiết kế

Không áp dụng mục 5.1–5.12, 6.1–6.13: task chỉ đổi tài liệu, không chạm database, tiền, quyền, hàng đợi. Riêng mục 6.3 (thứ tự khóa): phần chèn vào ADR-46 theo đúng vị trí Global Constraints của kế hoạch M5 ghi (C16: không chép lại nhóm của mốc trước); năm đường của M5 đã đối chiếu với bảng sau khi chèn (ghi trong `docs/m5/doi-chieu-gia-dinh.md` mục "Thứ tự khóa"). Các điểm thiết kế phát hiện khi đối chiếu được ghi thành quyết định ở mục "Quyết định thay chủ shop" và giao cho task M5 tương ứng, không làm ở đây.

## Quyết định thay chủ shop

Chủ shop vắng tới sáng 22/11 và giao toàn quyền trong phạm vi kế hoạch, spec. Mỗi dòng chọn phương án đúng spec, an toàn, dễ đảo ngược.

| # | Câu hỏi | Chọn | Vì sao | Phương án khác |
|---|---|---|---|---|
| QĐ-183-1 | `cancel_reason` khi khách tự hủy trên web (M5-10, M5-13) | Dùng lại `customer_changed_mind`; "hủy từ website" thấy qua người làm `service` + audit | CHECK Q18 của M2 đã có giá trị đúng nghĩa; không cần migration thu hẹp/mở rộng | Thêm `web_customer_cancelled` vào CHECK (migration mở rộng ở 0503) nếu chủ shop muốn lọc riêng trên màn Đơn |
| QĐ-183-2 | Cờ "cần xem lại giá" của QĐ15 lưu ở đâu (M5-10) | Thêm `price_review` vào CHECK `orders_pending_reason_check` ở 0503 (`NOT VALID` rồi `VALIDATE`) | `pending_reason` là chỗ M2 đã dùng cho "đơn chờ nhân viên xử lý" và màn Đơn đã hiện; đơn web vào ở `new` nên CHECK `orders_pending_status_check` cho phép | Cột boolean riêng `price_review` (thêm một trường nữa trên màn) |
| QĐ-183-3 | Khách mới từ web/flash sale không có tên (M5-10, M5-20) | Đặt tên cố định "Khách website" (như Task 439) | `validName` của M2 đòi 1–120 ký tự; không nới luật của module `customer` | Sửa `customer` cho tên rỗng (đổi luật của mốc trước, không chọn) |
| QĐ-183-4 | Báo phí ship cho web khi production chưa nối adapter hãng (M5-15, M5-31) | Giữ QĐ19: phí dự phòng mặc định **tắt**, CRM trả `503 retry_later`, website giữ cách xử lý hiện có; ghi việc cho chủ shop: nhập phí dự phòng trên màn "Hãng trên website" trước đêm mở | Không tự đặt con số tiền thay chủ shop; tích hợp hãng đang tạm dừng (quyết định 21/11) | Đặt sẵn một phí mặc định (cần chủ shop chọn số) |
| QĐ-183-5 | Chia M5 thành hai đợt (yêu cầu 21/11) | Đợt 1 website qua CRM: M5-00 → 15 trừ 04, 22 (trừ màn flash), 24 → 32, 35 (bỏ ca flash), 38 (phần đợt 1), 39; đợt 2 flash sale: M5-04, 16 → 21, 23, 33, 34, 36, 37. FS13 và HT27 tách mỗi ca hai dòng (phần đọc/đặt hàng thường ở đợt 1) | Đúng danh sách chủ shop nêu (199–204, 206, 216, 217, 219, 220, 187); Valkey bộ đệm và giới hạn tần suất (M5-01 → 03) phục vụ website nên ở đợt 1 | Dời cả Valkey sang đợt 2 (website đọc thẳng PostgreSQL; HT27 đợt 1 thành không áp dụng) |
| QĐ-183-6 | Nghĩa "Đã bán 30 ngày" trong CRM (M5-08) | Tổng số lượng dòng theo biến thể, đơn tạo trong 30 ngày, mọi kênh, trừ `draft`, `cancelled`, `returned`; không có cờ đơn thử | Đúng nghĩa website đang hiện (`unitsSold`: mọi kênh, bỏ đơn hủy/hoàn và đơn thử); đơn thử e2e không tới CRM thật | Thêm cờ đơn thử ở task riêng nếu cần |
| QĐ-183-7 | Chèn `fulfillment.channel_carrier_settings` vào ADR-46 khi M5-15 còn có phương án dùng khóa `platform.settings` | Chèn (đúng Global Constraints), ghi kèm "nếu M5-15 chọn khóa `platform.settings` thì bảng này không tạo, khóa thuộc nhóm 12c" | M5-00 là lần duy nhất M5 sửa thứ tự khóa; chèn tên thừa không gây sai thứ tự | Không chèn, để M5-15 sửa ADR-46 (trái luật "một lần ở task hợp đồng") |
| QĐ-183-8 | Có chạy reviewer cho task chỉ đổi tài liệu | Có (lệnh giao việc yêu cầu), dù kế hoạch ghi "Không reviewer" | Thêm một lượt soát không hại; commit vẫn là commit tài liệu | Bỏ reviewer theo CLAUDE.md bước 7 |

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `tieu-chi-nghiem-thu.md` đủ 33 ca, mỗi ca ít nhất một dòng | đếm mã ca | `grep -oE "^\| (WB[0-9]{2}\|FS[0-9]{2}\|HT27)" docs/m5/tieu-chi-nghiem-thu.md \| sort -u \| wc -l` (và không `sort -u`) | `33` mã khác nhau, `36` dòng (WB14, FS13, HT27 mỗi ca hai dòng theo đợt) |
| `doi-chieu-gia-dinh.md` có mọi dòng giả định A1–A6, B1–B3, C1–C2, D1, M0, M0b, M1r với kết quả khớp/khác tên/thiếu | đếm dòng | `grep -cE "^\| (A[0-9]\|B[0-9]\|C[0-9]\|D1\|M0\|M1r)" docs/m5/doi-chieu-gia-dinh.md` | `21` (A1, A2, A2b, A2c, A2d, A3, A4, A5, A5b, A6, B1, B2, B2b, B3, C1, C2, D1, M0, M0 luật hợp đồng, M0b, M1r); không dòng "thiếu" |
| `threat-model-m5.md` có sơ đồ và STRIDE cho năm ranh giới (B1–B5), mỗi dòng có cách xử lý và test | đếm dòng STRIDE | `grep -c "^\| [STRIDE, ]* \|" docs/threat-model-m5.md` | `34` dòng trong năm bảng B1–B5 |
| Chèn bảng M5 vào ADR-46 không phá danh sách M0–M2 và khung M3/M4 | `TestLockOrder_*` (`tests/arch/lockorder_frame_test.go`, `lockorder_m3_test.go`) | `go test ./tests/arch/ -run 'LockOrder' -count=1` | `ok  elimaz.com.vn/crm/tests/arch 0.589s` |
| Cổng kiểm của task (sau khi gộp `main` `19ef392`) | toàn bộ cổng tĩnh + test kiến trúc | `ci-task.sh` (một lần, log ngoài worktree) | `CI-TASK XANH (so với điểm tách nhánh 19ef3925…)`; `ok elimaz.com.vn/crm/tests/arch 21.234s` |
| Điểm dừng A | đọc mã (mục 6 của `doi-chieu-gia-dinh.md`) | xem bảng mục 1 | không dòng "thiếu" chạm vùng rủi ro cao → không dừng |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Tên tệp task theo lệnh giao việc ("M5-00: Tiêu chí nghiệm thu, …") thay "Chuẩn bị M5" của Step 0.
- Kế hoạch M5 (ngoài repo, không có git) được bổ sung khối "Bổ sung Task 183" sau bảng Task 439 và đánh dấu C2-16/C3-4 đã xong; bản trước khi sửa giữ ở scratchpad của phiên (`plan-m5-before-183.md`).

## Việc phát hiện thêm

- M5-07: tệp ảnh chuyển `ready` sau khi gắn không đổi `products_v.updated_at` — chọn cách để `changes?since=` thấy (mục 3 `doi-chieu-gia-dinh.md`).
- M5-05: `routegen` chưa đọc `x-auth`, `x-admission`, `x-body-limit` (luật đã có); không nhầm với `x-authenticated`.
- M5-10: thay CHECK `pending_reason` (QĐ-183-2) có câu `DROP CONSTRAINT` → cần dòng `-- contract:` theo CLAUDE.md.
- Chủ shop (trước đêm mở, M7): nhập phí ship dự phòng cho web hoặc mở lại tích hợp hãng (QĐ-183-4).

## Reviewer

Kết luận: ĐẠT (lượt 2).

- Lượt 1: CẦN SỬA — WB14 và HT27 đợt 1 ghi "D ở M5-38" mà Task 221 không có buổi diễn tập đó; WB14 mất phần phủ M5-37 bước (e). Đã sửa: WB14 đợt 1 lấy D là diễn tập `crm-outage.spec.ts` ở M5-35; thêm dòng "WB14 (phần đợt 2)" phủ bằng M5-37 bước (e); HT27 đợt 1 bỏ "D ở M5-38". Ghi chú nhỏ đã sửa: dải 183–198, `evaluate.go:21`, `write.go:384–391`, `state.ts` không tồn tại.
- Lượt 2: ĐẠT, không phát hiện phải sửa. Ghi chú không bắt buộc: QĐ-183-5 chỉ nhắc FS13, HT27 tách hai dòng (WB14 cũng tách, xem tệp tiêu chí).
