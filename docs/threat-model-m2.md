# Threat model M2 — khách hàng, đơn hàng, mã giảm giá

Bản đầu viết ở Task M2-00 (số toàn cục 57, 18/11/2026), trước khi có mã M2, theo mẫu `docs/threat-model-m1.md`.
Task M2-32 hoàn thiện: điền đúng tên test có trong mã, đổi dòng **Chưa có** thành test hoặc ghi vì sao chấp nhận.
Chỉ phủ phần mới của M2; ranh giới của M0 (Access, ingest, tiến trình → PostgreSQL) và M1 (kho tệp, ảnh, Excel
catalog, hãng giả) ở `docs/threat-model-m0.md` (Task M0-23, chưa có) và `docs/threat-model-m1.md`.

Nguồn: kế hoạch `docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Global Constraints, Review Focus, Task M2-01–25),
spec `04-kien-truc.md` mục 3, 7, 16; ADR-08, 29, 46. Cột "Task / test" ghi task M2 làm và tên hàm test có trong mã
(điền ở Task M2-32, 19/11/2026; mọi test được nêu chạy lại đạt trong review độc lập M2, `docs/review-doc-lap-m2.md`).

STRIDE: **S** giả danh, **T** sửa trộm, **R** chối bỏ, **I** lộ thông tin, **D** làm ngừng dịch vụ, **E** nâng quyền.

## Sơ đồ dữ liệu

```
 Trình duyệt nhân viên ──(Access)──▶ api ── WriteTx (một transaction tenant) ──────────────────────────┐
   Lên đơn, Đơn, Khách,               │  createOrder / updateOrder / cancelOrder / confirmOrder …          │
   Mã giảm giá                        │      order ──▶ customer (tìm-hoặc-tạo khách, nhóm 5)               │
                                      │            ──▶ catalog  (giá hiện hành, VariantsForSale)           ▼
                                      │            ──▶ promotion (đánh giá, giữ lượt mã, nhóm 10)     PostgreSQL
                                      │            ──▶ inventory.Writer (giữ, nhả, xuất hàng, nhóm 11)  schema customer,
                                      │  searchCustomers ──▶ customer.search_by_name (SECURITY DEFINER,  ord, promotion
                                      │                       chủ crm_search, tự lọc tenant)              │
                                      │  customer imports ──▶ files.ReadContent (kho, ngoài transaction)  │
                                      ▼                                                                    │
                          realtime (order.changed, customer.changed, coupon.changed: chỉ mã) ◀─────────────┤
                          events order.* (payload chỉ mã và số) ──▶ worker: đơn trùng, chỉ số khách,      │
                                                                     đơn nháp hết hạn, nhập khách ─────────┘
 fulfillment (M1/M3) ──▶ order.AttachShipment / OnShipmentEvent (chỉ vận đơn hiện hành đổi đơn và tồn)
```

Dữ liệu cá nhân mới: tên khách, SĐT (khách và người nhận), địa chỉ, ngày sinh, email, nội dung ghi chú khách và đơn.

## (1) Nhân viên → `api`: lên đơn, sửa, hủy, trả hàng

| STRIDE | Mối đe dọa | Xử lý | Task / test |
|---|---|---|---|
| S | Nhân viên tự duyệt giảm giá vượt ngưỡng bằng cách giả là người duyệt | Duyệt là thao tác riêng `approveOrderDiscount`, cần `order.discount.approve`; người duyệt lấy từ phiên Access, không từ thân yêu cầu; sửa đơn tăng tiền giảm tay (kể cả cùng tỷ lệ) cần duyệt lại | M2-19, 20, 32: `TestOrderConfirm_DiscountApproval` (403 `discount_needs_approval`), `TestApproveDiscount_New`, `TestEdit_DiscountApproval`, `TestEdit_DiscountApprovalAmountUp` (review độc lập C-02) |
| T | Sửa giá trong yêu cầu (gửi `unitPrice` thấp) | Thân không có trường đơn giá; máy chủ luôn lấy giá `catalog` lúc đặt (`VariantsForSale`), ảnh chụp vào dòng đơn | M2-16, 20: `TestCreate_PriceSnapshot`, `TestEdit_KH11_PriceSnapshot` (KH11) |
| T | Giảm giá âm, vượt tiền dòng, tràn `int64` | `pricing.Compute` từ chối; database `CHECK` và trigger kiểm tổng lúc commit | M2-07, 05: `TestCompute_Invariants`, `TestAllocate_Invariants` (rapid), `TestVectors`; `TestSchemaAllocatedDiscountAtCommit`, `TestSchemaOrderMoneyChecks` |
| T | Hai người sửa cùng đơn, người sau ghi đè | Mọi sửa mang `version`; lệch → 409 `version_conflict` | M2-20: `TestEdit_DH11_VersionConflict`, `TestOrderEdit_ConflictAndApproval` (DH11). Ngoại lệ: xử lý đơn trùng hủy đơn không mang `version` (review độc lập E-08, task bù 432) |
| T | Gửi lại cùng khóa nhưng đổi nội dung để tạo đơn thứ hai | Khóa nghiệp vụ `orderBusinessKey(user, Idempotency-Key)` duy nhất trên shop; khác nội dung → 422 `idempotency_key_reused` | M2-16: `TestOrderAPI_SameKey50` (DH07, 50 song song), `TestOrderAPI_BusinessKeyAfterExpiry`, `TestCreate_BusinessKeyReplay` |
| T | Đổi đơn đã gửi hãng (COD, địa chỉ) mà hãng không biết | Đơn có vận đơn hiện hành: sửa giới hạn, đổi COD → `ErrLockedByShipment`; hủy → `cancel_requested` | M2-18, 20, 21: `TestEdit_LockedByShipment` (DH15 phía đơn), `TestCancel_TD03RequestThenCarrierConfirms` (WB12) |
| R | Nhân viên chối đã hủy, đã giảm giá, đã gộp khách | Lịch sử trạng thái (`order_status_history`, người làm, giờ) và audit (tên trường, không giá trị cá nhân) trong cùng transaction | M2-16, 24: `TestTimeline_DH06`, `TestOrderRead_EditCancelTimeline`, `TestTimeline_ManyAuditsKeepsLatestEdit` |
| I | Đoán mã đơn, mã khách của shop khác | Mọi đọc trong `tenancy.WithTenant` (RLS); mã lạ hay của tenant khác → 404 như không tồn tại; đọc đơn có `seller_id` (ADR-29) | M2-16, 24: `TestConfirm_OtherSeller`; luật kiến trúc `tests/arch/sellerfilter_test.go` |
| E | Nhân viên Kho hủy đơn, Bán hàng nhận hàng trả, gộp khách | Quyền riêng: `order.cancel`, `order.return.receive`, `customer.merge`; vai trò mẫu theo bảng Task M2-01 | M2-01: `TestEnsureDefaultRoles_M2AddOnly`, `TestSyncDefaultRoles_M2`; `TestCustomerMergeAPI` (sale gộp, hoàn tác → 403). Phiếu đổi tạo đơn thay thế chỉ với `order.return` (review độc lập B-06, nghi ngờ, task bù 436) |

## (2) Tìm khách

| STRIDE | Mối đe dọa | Xử lý | Task / test |
|---|---|---|---|
| I | Hàm tìm `SECURITY DEFINER` (chạy BYPASSRLS) trả khách tenant khác | Hàm tự lọc `tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid`; ngoài ngữ cảnh tenant → 0 dòng; `REVOKE ALL FROM PUBLIC` | M2-10: `TestSearchTenantIsolation`, `TestSearchByNameFunctionDefinition` |
| I | Từ khóa tìm (SĐT, tên) lọt vào log, dấu vết | Không ghi `q`; chỉ ghi loại tìm và độ dài | M2-10: `TestCustomerSearchAPI` (log, span không chứa chuỗi tìm); test log của M0 (`internal/httpapi/personaldata_test.go`) |
| D | Tìm chuỗi rất ngắn (`a`, `09`) quét cả bảng | Đuôi SĐT tối thiểu 4 số, tên tối thiểu 2 ký tự → 422 `query_too_short`; giới hạn tần suất theo người của M0; không `LIKE` thẳng lên bảng | M2-10: `TestCustomerSearchAPI` (`query_too_short`), `TestSearchPlans` (100.000 khách, không Seq Scan). p95 trên 1 triệu khách (Task M2-31): **dời theo chủ shop 19/11** tới trước ngày chạy thật — chấp nhận tạm, kế hoạch truy vấn đã kiểm |

## (3) Hồ sơ khách: danh tính, gộp, nhập Excel

| STRIDE | Mối đe dọa | Xử lý | Task / test |
|---|---|---|---|
| T | SĐT người nhận (đặt hộ) tự gắn thành SĐT của khách, sau đó lộ đơn của người khác | `phoneIsCustomers = false` → chỉ lưu ở địa chỉ giao; SĐT đã thuộc hồ sơ khác (khách có sẵn) → cảnh báo, không gắn, không tự gộp | M2-09, 16: `TestFindOrCreateForOrder` (ca DH12, DH13), `TestCreate_PhoneOwnership`, `TestOrderAPI_OrderForSomeoneElse`. Lên đơn "khách mới" với SĐT của hồ sơ có sẵn và giữ ô "Số này là của khách" → đơn gắn vào hồ sơ đó (đúng kế hoạch Task M2-09, cần chủ shop xác nhận; review độc lập A-01, task bù 435) |
| T | Gộp nhầm làm mất đơn, hoàn tác sai thứ tự | Gộp ghi `customer_merges`; hoàn tác chỉ lần gộp mới nhất trong 30 ngày (`merge_not_latest`, `merge_window_expired`); không hồ sơ nào trỏ vào hồ sơ không phải gốc | M2-11: `TestMergeUndoProperty` (rapid), `TestMergeDH14`, `TestUndoMergeNotLatest`, `TestUndoMergeWindow`, `TestCustomerMergeAPI` |
| T | Tệp Excel có công thức, ô rất dài, tệp nén bom | `internal/platform/xlsx` (M1): giới hạn kích thước, số dòng, giải nén, độ dài ô; ô chữ bắt đầu bằng `=` lưu nguyên dạng chữ (màn hình không chạy công thức) | M2-13: `TestImport_FileChecks`, `TestImport_JobFailsOnTooLarge`, `TestImport_RowErrorsAndReimport`; test của gói `xlsx` (M1) |
| I | Lỗi nhập trả lại giá trị ô (SĐT, tên) | Lỗi chỉ `{row, column, code}`, không giá trị | M2-13: `TestImport_RowCodes`, `TestImport_RowErrorsAndReimport` |
| I | Payload sự kiện, việc nền, realtime mang dữ liệu cá nhân | Chỉ mã bản ghi và số | M2-16, 25: `TestCreate_NoPersonalDataInEventsAndJobs` |
| R | Không biết ai nhập 5.000 khách | Một dòng audit cho cả lần nhập (`customer.import`, số dòng) | M2-13: `TestImport_RowErrorsAndReimport` (một audit `customer.import`, không audit `customer.create`) |
| D | Tệp nhập rất lớn chiếm worker | Đọc tối đa 5 MiB, 20.000 dòng; lô 500 dòng mỗi transaction, nhường lượt rồi chạy tiếp | M2-13: `TestImport_JobFailsOnTooLarge`, `TestImport_YieldsAndResumes`, `TestImport_ResumeAfterCrash` |
| E | Người không có quyền nhập, gộp, quản lý nhãn | `customer.import`, `customer.merge`, `customer.tags.manage` | M2-01: `TestSyncDefaultRoles_M2`; `TestCustomerMergeAPI` |

## (4) Mã giảm giá

| STRIDE | Mối đe dọa | Xử lý | Task / test |
|---|---|---|---|
| T | Hai đơn dùng lượt cuối cùng lúc; hủy đơn trả lượt hai lần | Khóa dòng mã (nhóm 10) trước khi ghi lượt; `used_count` = số lượt đang hiệu lực, không âm, không vượt | M2-06, 17, 21: `TestRedeem_ConcurrentMaxUses`, `TestCreateCoupon_ConcurrentLastUse` (KM01, WAT07), `TestCancel_KM03ReleasesCoupon`, `TestCancel_Parallel50SameVersion` (KM03). Lượt mỗi khách và mã gắn khách sau khi gộp khách tính theo hồ sơ gốc lúc dùng (review độc lập A-02/C-01, task bù 430) |
| T | Sửa điều kiện mã sau khi đã có người dùng | Mã đã dùng chỉ sửa được hạn và số lượt (≥ đã dùng): `coupon_locked_after_use`, `max_uses_below_used` | M2-14: `TestUpdateCoupon_LockedAfterUse`, `TestUpdateCoupon_LockProperty` |
| I | Dò mã hợp lệ bằng `checkCoupon` | Cần `order.create`; giới hạn tần suất của M0. Lý do từ chối **cụ thể** (`not_found` khác `expired`, `exhausted` — KM02 đòi báo lý do), nên người có quyền phân biệt được mã có tồn tại; chấp nhận vì chỉ nhân viên gọi được. M5 mở kiểm mã cho khách web thì phải gộp `not_found` vào lý do chung | M2-14, 17: `TestCouponAPI_CreateAndCheck` (review độc lập E-04 sửa dòng này) |
| E | Nhân viên Bán hàng tạo, sửa mã | `promotion.manage` chỉ `owner`, `manager` | M2-01: `TestSyncDefaultRoles_M2` |

## (5) Vận đơn → đơn (móc nối cho M3)

| STRIDE | Mối đe dọa | Xử lý | Task / test |
|---|---|---|---|
| T | Sự kiện của vận đơn cũ tới muộn đổi đơn, tồn | Chỉ vận đơn hiện hành (`current_shipment_id`) được đổi; khác → `applied = false`, không ghi gì | M1-30b, M2-18: `TestApplyShipmentProgress_OnlyCurrentShipment`, `TestOnShipmentEvent_StaleShipment`, `TestOnShipmentEvent_PickedUpThenDelivered` (AT47) |
| T | Hủy đơn đã gửi hãng nhả hàng sớm | `cancel_requested` không nhả hàng tới khi hãng xác nhận | M2-21: `TestCancel_TD03RequestThenCarrierConfirms`, `TestCancel_CarrierRejects`. Hãng lấy hàng khi đơn đang `cancel_requested` bị móc nối từ chối (review độc lập B-01, task bù 431) |
| D | Deadlock giữa tạo, sửa, hủy cùng đơn và cùng biến thể | Thứ tự khóa ADR-46 (đơn trước, tồn sau, biến thể tăng dần) | M2-17, 20, 21: `TestCreateCoupon_NoDeadlockWithCancel`, `TestEdit_NoDeadlock`, `TestEdit_CouponSwapNoDeadlock`, `TestCancelReturn_ExchangeRaceNoDeadlock`, `TestMergeVsForOrderNoDeadlock`. Thử tải 10× (M2-31): **dời theo chủ shop 19/11** |

## Lỗ hổng phát hiện sau khi có mã (Task M2-32)

Review độc lập M2 (`docs/review-doc-lap-m2.md`) thêm các dòng ghi chú ở trên: C-02 (đã sửa, có test), A-01, A-02/C-01,
B-01, B-06, E-04, E-08. Không còn dòng nào ghi "Chưa có"; hai phép đo hiệu năng (tìm khách p95, thử tải) dời theo chủ shop
19/11 và là việc trước ngày chạy thật.
