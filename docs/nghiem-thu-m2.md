# Nghiệm thu M2 — Khách hàng, đơn hàng, mã giảm giá

Viết ở Task M2-00 (số toàn cục 57, 18/11/2026), trước khi có mã M2. Nguồn: kế hoạch
`docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (mục "Xong M2 khi", "Review Focus", "Ánh xạ ca kiểm M2 → task"),
spec `08-roadmap.md` hàng M2 và `11-kich-ban-kiem-thu.md` (các ca Mốc = M2). Kế hoạch là nguồn gốc; tệp này viết lại bằng
lời nghiệp vụ để chủ shop đọc và duyệt. Hai nơi khác nhau thì sửa cả hai trong cùng commit.

Cột "Kết quả" để trống tới khi task tương ứng xong; Task M2-32 điền lần cuối, kèm kết quả nhân viên chạy trên staging.
Số task là số nội bộ M2 (M2-NN); số toàn cục = 57 + NN (M2-33 = 90).

## Xong M2 khi

| # | Tiêu chí (lời nghiệp vụ) | Task M2 | Kết quả |
|---|---|---|---|
| 1 | **Lên đơn nhanh.** Nhân viên tìm khách bằng 4 số cuối SĐT hoặc tên không dấu, chọn hàng, bấm "Tạo đơn": tổng tiền đúng, đơn ở "Đã xác nhận", hàng được giữ ngay. Bấm "Tạo đơn" hai lần chỉ ra một đơn. | 07, 10, 16, 27 | M đạt (DH01, DH02 phần đúng kết quả, DH07). V: chờ Điểm dừng C |
| 2 | **Hai nhân viên tranh chiếc cuối.** Chỉ một đơn giữ được hàng, người kia thấy "hết hàng"; tồn không bao giờ âm. Sửa đơn tăng số lượng khi kho không đủ thì bị từ chối trọn vẹn, đơn cũ còn nguyên. | 16, 20 | M đạt (DH08, DH10, AT05; nửa "xác nhận không giữ thêm" thêm ở Task 438) |
| 3 | **Giảm giá không lệch một đồng.** Giảm cả đơn được chia về từng món; cộng lại đúng bằng số giảm. Giảm quá ngưỡng thì đơn chờ người có quyền duyệt. | 05, 07, 17, 19 | M đạt (DH05, DH16, AT29). Review C-02 (sửa đơn tăng tiền giảm tay không cần duyệt lại) đã sửa ở Task 89 |
| 4 | **Mã giảm giá đúng lượt.** Mã có hạn và giới hạn lượt; còn một lượt mà hai đơn dùng cùng lúc thì chỉ một đơn được; hủy đơn đã dùng mã thì lượt được trả, không trả hai lần. Mã không hợp lệ bị từ chối kèm lý do tiếng Việt. | 06, 14, 17, 21 | M đạt (KM01, KM02, KM03, WAT07 phần CRM; nửa "xác nhận không dùng lượt lần hai" thêm ở Task 438). Còn: lượt mỗi khách và mã gắn khách sau khi gộp khách (review A-02, task bù 430) |
| 5 | **Hồ sơ khách sạch.** SĐT người nhận (đặt hộ) không tự thành SĐT của khách; SĐT trùng hồ sơ khác chỉ gợi ý, không tự gộp; gộp nhầm hoàn tác được trong 30 ngày, đơn về đúng chỗ. Nhập danh sách khách bằng Excel không tạo trùng. | 09, 11, 13 | M đạt (DH12, DH13, DH14, `TestImport_RowErrorsAndReimport`). A-03 (gộp không đổi trạng thái khách) đã sửa ở Task 89. Còn: review A-01 (cần chủ shop, task bù 435), A-04 (task bù 430) |
| 6 | **Hủy, trả, đổi đúng tồn.** Hủy đơn chưa gửi hãng: hàng về "có thể bán" ngay. Đơn đã gửi hãng: thành "yêu cầu hủy", chưa nhả hàng cho tới khi hãng xác nhận. Trả, đổi hàng ghi đúng kho (hàng tốt, hàng lỗi). | 18, 21, 22 | M đạt (TD01, TD03 `TestCancel_TD03RequestThenCarrierConfirms`, TD05–TD08, WAT29). Còn: hãng lấy hàng khi đơn đang yêu cầu hủy (review B-01, task bù 431, trước M3) |
| 7 | **Đơn trùng, lịch sử, ghi chú.** Cùng SĐT, cùng sản phẩm trong vài giờ → đơn được đánh dấu "Đơn trùng" để nhân viên xử lý; mỗi đơn có lịch sử ai làm gì lúc nào. | 23, 24 | M đạt (DH09, DH06). Còn: xử lý đơn trùng với đơn đã gửi hãng, cờ nghi trùng sau hủy (review B-03, B-04, task bù 432) |
| 8 | **Tìm khách nhanh trên 1 triệu khách.** Tìm theo đuôi SĐT p95 dưới 300 ms (1 triệu khách giả). Tạo đơn p95 dưới 400 ms, đọc danh sách đơn p95 dưới 200 ms ở tải 10 lần bình thường, không deadlock, lần thử lại transaction cạn dưới 1%. | 31 | Dời theo chủ shop 19/11 (Task 88, M2-31) tới trước ngày chạy thật; chưa đo. Kế hoạch truy vấn tìm khách đã kiểm (`TestSearchPlans`, không Seq Scan) |
| 9 | **Đủ ca kiểm thử, màn hình đã duyệt.** 19 ca M2 của `11-kich-ban-kiem-thu.md` đạt; các ca nghiệm thu spec 10/2026 thuộc M2 đạt (AT05, AT06, AT07, AT21, AT28, AT29, WAT07 phần CRM; phần phía đơn của AT13, AT14, WAT28, WAT29, WAT31). Màn Lên đơn, Đơn, Khách, Mã giảm giá làm đúng mockup đã chốt (Điểm dừng B). Mutation testing vùng rủi ro cao đạt ngưỡng, review độc lập đạt. Nhân viên chạy phần "V" trên staging (cần OK của chủ shop, Điểm dừng C). | 26–30, 32 | 19/19 ca M2 có test máy, chạy lại đạt; AT/WAT: bảng dưới. Màn hình theo mockup Điểm dừng B (chỗ khác mockup vì API thiếu: điều phối viên gom). Mutation: đạt ngưỡng 80% cả ba vùng (order 89,53% → 91,01% sau test thêm; promotion 86,84% → 88,42%; customer merge/fororder/stats 90,00%; `docs/mutation.md`; chế độ nhanh, chưa vào `make mutation`). Review độc lập: 0 nghiêm trọng, 7 lớn (2 sửa ở Task 89: C-02, E-01; 5 tách task bù 430–439), 17 nhỏ (6 sửa ở Task 89) — chưa đạt trọn. V: chờ Điểm dừng C |

## Tiêu chí → ca kiểm thử

"M" là máy tự kiểm (tên test ghi ở tệp task khi viết), "V" là nhân viên chạy ở Điểm dừng C.

| Ca | Tình huống (lời nghiệp vụ) | Máy kiểm ở | Màn hình, nhân viên | Kết quả |
|---|---|---|---|---|
| DH01 | Lên đơn nhanh: tổng đúng, Đã xác nhận, hàng tạm giữ | 07, 16 | 27, 32 (V) | M đạt: `TestCreate_ConfirmedTotals`, `TestCompute_DH01`, `TestOrderAPI_CreateM2Confirmed`; V: chờ Điểm dừng C |
| DH02 | Tìm khách bằng đuôi SĐT hoặc tên không dấu dưới 0,3 giây | 10, 31 | 29 | M đạt: `TestSearchClassifies`, `TestSearchTenantIsolation`, `TestSearchPlans` (không Seq Scan, 100.000 khách); p95 trên 1 triệu khách: dời theo chủ shop 19/11 |
| DH04 | Đơn nhập tay, nguồn "Nhập tay" | 16 | 27, 32 (V) | M đạt: `TestCreate_SourceAndChannel`; V: chờ Điểm dừng C |
| DH05 | Giảm cả đơn chia về từng món, không lệch 1 đồng | 05, 07, 17 | 27 | M đạt: `TestCompute_DH05`, `TestAllocate_Invariants`, `TestVectors`, `TestSchemaAllocatedDiscountAtCommit` |
| DH06 | Lịch sử thao tác trên đơn | 24 | 28, 32 (V) | M đạt: `TestTimeline_DH06`, `TestOrderRead_EditCancelTimeline`; V: chờ Điểm dừng C |
| DH07 | Bấm "Tạo đơn" hai lần | 16 | 27 | M đạt: `TestOrderAPI_SameKey50`, `TestOrderAPI_BusinessKeyAfterExpiry`, vitest `OrderForm.test.tsx` |
| DH08 | Hai nhân viên tranh chiếc cuối | 16 | — | M đạt: `TestCreate_LastUnitRace` (20 lần), `TestCreate_ConcurrentReservations` (rapid) |
| DH09 | Cùng SĐT, cùng sản phẩm trong vài giờ → "Đơn trùng" | 23 | 28, 32 (V) | M đạt: `TestDuplicates_DH09`, `TestOrderAPI_Duplicates`; V: chờ Điểm dừng C |
| DH10 | Sửa tăng số lượng khi kho không đủ → từ chối trọn | 20 | 28 | M đạt: `TestEdit_DH10_Quantity`, `TestOrderEdit_Quantity` |
| DH11 | Hai người cùng sửa một đơn → người sau được báo "đơn đã thay đổi" | 09, 19, 20 | 28 | M đạt: `TestEdit_DH11_VersionConflict`, `TestOrderEdit_ConflictAndApproval` |
| DH12 | Đặt hộ: SĐT người nhận không thành SĐT khách | 09, 16 | 27 | M đạt: `TestFindOrCreateForOrder` (DH12), `TestCreate_PhoneOwnership`; xem review A-01 (task bù 435) |
| DH13 | SĐT trùng hồ sơ khác → gợi ý; chỉ người có quyền mới gộp | 09, 11, 16 | 27, 29, 32 (V) | M đạt: `TestFindOrCreateForOrder` (DH13), `TestCustomerMergeAPI`; V: chờ Điểm dừng C |
| DH14 | Gộp nhầm → hoàn tác trong 30 ngày, đơn về đúng chỗ | 11, 25 | 29 | M đạt: `TestMergeDH14`, `TestMergeUndoProperty`, `TestStatsFromSource` |
| DH16 | Giảm quá mức → chờ người có quyền duyệt | 17, 19, 20 | 27, 28 | M đạt: `TestCreate_DiscountApproval`, `TestEdit_DiscountApproval`, `TestEdit_DiscountApprovalAmountUp`, `TestOrderConfirm_DiscountApproval` |
| KH11 | Đổi giá: đơn cũ giữ giá, đơn mới giá mới | 16, 20 | — | M đạt: `TestCreate_PriceSnapshot`, `TestEdit_KH11_PriceSnapshot` |
| TD01 | Hủy đơn chưa gửi hãng → hàng về "có thể bán" ngay | 21 | 28, 32 (V) | M đạt: `TestCancel_TD01ReleasesStock`, `TestOrderAPI_Cancel`; V: chờ Điểm dừng C |
| KM01 | Mã có hạn và giới hạn lượt; hết lượt thì từ chối | 06, 14, 17 | 30, 32 (V) | M đạt: `TestCreateCoupon_Exhausted`, `TestRedeem_ConcurrentMaxUses`, `TestCouponAPI_CreateAndCheck`; V: chờ Điểm dừng C |
| KM02 | Mã hết hạn hoặc không đủ điều kiện → từ chối, nêu lý do | 06, 14, 17 | 27, 30 | M đạt: `TestCreateCoupon_Rejected`, `TestEvaluate`, `TestCouponAPI_CreateAndCheck` |
| KM03 | Hủy đơn đã dùng mã → lượt được trả | 06, 21 | — | M đạt: `TestCancel_KM03ReleasesCoupon`, `TestCancel_Parallel50SameVersion`, `TestRelease` |

### Ca nghiệm thu spec 10/2026 thuộc M2 (điền ở Task M2-32)

| Ca | Test máy | Kết quả |
|---|---|---|
| AT05 | `TestCreate_LastUnitRace`, `TestCreate_ConcurrentReservations`, `TestConfirm_NoSecondHoldOrRedemption` | Đạt (xác nhận ở 5 đường không giữ thêm: Task 438) |
| AT06 | `TestOrderAPI_SameKey50`, `TestOrderAPI_BusinessKeyAfterExpiry`, `TestOrderConfirm_Parallel`, `TestOrderAPI_CancelSameKey50` | Đạt phần CRM (dispatch, thanh toán: M3) |
| AT07 | `TestEdit_DH10_Quantity`, `TestOrderEdit_Quantity` | Đạt phần "tăng lượng"; "đổi kho" khi sửa đơn không có ở M2 |
| AT13 (phía đơn) | `TestCreateReturn_TD05PerLine`, `TestReturns_Invariants` | Đạt |
| AT14 (phía đơn) | `TestCreateReturn_TD06Exchange`, `TestReturnAPI_Exchange` | Đạt (cấn trừ tiền: M3) |
| AT21 | `TestCreate_PriceSnapshot`, `TestEdit_KH11_PriceSnapshot` | Đạt |
| AT28 | `TestEdit_DH11_VersionConflict`, `TestOrderEdit_ConflictAndApproval` | Đạt |
| AT29 | như DH16 | Đạt |
| AT47 | `TestOnShipmentEvent_PickedUpThenDelivered` | Đạt phần đơn và kho (tài chính: M3) |
| WAT07 (CRM) | `TestCreateCoupon_ConcurrentLastUse`, `TestRedeem_ConcurrentMaxUses`, `TestConfirm_NoSecondHoldOrRedemption` | Đạt phần CRM (xác nhận không dùng lượt lần hai: Task 438) |
| WAT28 (phía đơn) | `TestCreateReturn_TD05PerLine`, `TestCreateReturn_LastUnitRace` | Đạt |
| WAT29 (phía đơn) | `TestReceiveReturn_TD08Restock` | Đạt |
| WAT31 (phía đơn) | `TestCreateReturn_RoundingLastUnit`, `TestCreateReturn_WAT31OrderDiscountAfterPriceChange`, `TestReturns_Invariants` (rút cả giảm cấp đơn), `TestRefundFor`, `TestRefundFor_SplitSumsToNet` | Đạt phần giảm cấp đơn, mã (Task 438: ca database đổi giá trước khi trả); phí ship, hoa hồng: M3, M6 |

Tên test lấy từ review độc lập vùng E (`docs/review-doc-lap-m2.md`, Phụ lục E mục 1), chạy lại ngày 19/11/2026.

Ca của mốc sau mà M2 phủ phần phía đơn (nghiệm thu đầy đủ ở mốc của chúng): DH03, DH15, TD02, TD03, TD05–TD08, WB11,
WB12, KH14, PQ02, PQ08 (xem kế hoạch M2, mục "Ánh xạ ca kiểm").

## Vùng rủi ro cao của M2

`internal/modules/order/**`, `internal/modules/promotion/**`, `internal/modules/customer/{merge.go,fororder.go,stats.go}`,
phần M2 thêm vào `internal/modules/inventory` (`byitems.go`, `returns.go`), migration 0201–0208. Có test sinh tình huống
ngẫu nhiên (rapid) và phải qua mutation testing ở Task M2-32.

## Ngoài phạm vi M2

Gửi đơn cho hãng thật, đối soát COD, hoàn tiền (M3); khung chat, tạo đơn từ hội thoại (M4); đơn từ website, flash sale
(M5); báo cáo (M6); tài khoản khách trên web (M7). M2 không gọi dịch vụ ngoài nào và không đọc dữ liệu Nhanh.
