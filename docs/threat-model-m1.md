# Threat model M1 — kho tệp, xử lý ảnh, nhập Excel, hãng giả

Viết ở Task M1-24 (18/11/2026), trước khi có mã M1. Chỉ phủ **sáu ranh giới mới** của M1; ranh giới của M0 (trình duyệt → `api` qua Cloudflare Access, Meta → `ingest`, tiến trình → PostgreSQL) thuộc `docs/threat-model-m0.md` (Task M0-23). Nguồn: kế hoạch M1 (Task M1-31, 38, 40–47), spec `04-kien-truc.md` mục 16, 17; ADR-18, 36, 45.

Cột "Xử lý" ghi cách chặn; cột "Task / test" ghi nơi làm và test chứng minh. Dòng ghi **Chưa có** là lỗ hổng chưa có trong kế hoạch, gom ở mục cuối kèm đề xuất.

STRIDE: **S** giả danh, **T** sửa trộm, **R** chối bỏ, **I** lộ thông tin, **D** làm ngừng dịch vụ, **E** nâng quyền.

## Sơ đồ dữ liệu

```
                          (1) PUT ký sẵn
 Trình duyệt nhân viên ───────────────────────────────▶ SeaweedFS  crm-tmp
        │  createUpload / completeUpload (qua Access)       ▲   │ crm-public, crm-originals, crm-private
        ▼                                                   │   │
       api ──── jobs (PostgreSQL) ──── worker ──(2)──▶ imager ──(3)── PUT/GET ký sẵn ──┘   │
                                         │  ▲ đọc tệp Excel (5)                            │
                                         │  └──────────────── crm-private ◀────────────────┤
                                         │                                                 │
 fakecarrier ──(6) webhook HMAC──▶ ingest ──▶ jobs ──▶ worker ──▶ fakecarrier GET trạng thái│
                                                                                           │
 crm-filesync (rclone copy, mỗi giờ) ◀─────────────────── đọc (danh tính crm-filesync) ────┘
        │ (4) chép thêm, bucket riêng tư mã hóa trước khi rời máy
        ▼
   Kho ngoài (không thuộc Cloudflare, chủ shop tạo)     ── INSERT file_sync_runs ──▶ PostgreSQL (role crm_filesync)
```

Mạng: `imager` chỉ ở mạng `imaging` (cùng `worker` và bộ chuyển cổng `s3-imaging`, chỉ chuyển cổng S3 tới SeaweedFS), không tới được PostgreSQL, không tới được master, volume, filer của SeaweedFS (không xác thực; Task 410, review độc lập M1 C-01). Danh tính kho tệp: `crm-app` (api, worker: đọc, ghi, liệt kê; không xóa vĩnh viễn), `crm-filesync` (đọc kho chính), `crm-admin` (không tiến trình ứng dụng nào dùng).

## (1) Trình duyệt → SeaweedFS: tải thẳng bằng đường dẫn ký sẵn

Dữ liệu: ảnh sản phẩm, tệp Excel nhập hàng. Trình duyệt chỉ có đường dẫn `PUT` do `api` ký cho một khóa `tmp/<tenantId>/<fileId>`, hạn 15 phút.

| STRIDE | Mối đe dọa | Xử lý | Task / test |
|---|---|---|---|
| S | Người ngoài tải tệp lên không qua đăng nhập | Chỉ `api` (sau Access, quyền `files.upload`) ký đường dẫn; kho không nhận yêu cầu không ký | M1-41: `createUpload` cần `files.upload` |
| S | Trang web khác dùng trình duyệt nhân viên để tải lên | CORS của `crm-tmp` chỉ nhận `PUT` từ `CONSOLE_ORIGINS` | M1-40 mục 8 (CORS) |
| T | Tải tệp khác kích thước đã khai (khai 1 KB, gửi 1 GB) | Chữ ký có `content-length` và `content-type`; `completeUpload` `Head` lại, sai kích thước → `rejected size_mismatch` | M1-40 mục 2; M1-41: "tải 900 byte khi khai 1.000", `size_mismatch` |
| T | Ghi đè tệp đã có (của người khác, hoặc ảnh gốc đã xử lý) | Khóa do hệ thống sinh theo `fileId`/mã băm; `If-None-Match: *` hoặc `HeadObject` trước; `CopyIfAbsent`, `Put` không ghi đè | M1-40 mục 9; M1-41: `CopyIfAbsent` lên khóa có nội dung khác → không đổi, `Put` → `ErrObjectExists` |
| T | Tên tệp độc (`../../etc/passwd.jpg`, `<script>.png`) | Tên người dùng chỉ lưu ở cột `original_name` (≤ 255), không vào khóa; khi tải về dùng `attachmentName` do hệ thống dựng | M1-41: "khóa sinh ra không chứa `original_name`" |
| T | Tệp khai là ảnh nhưng là HTML, SVG có script | Không tin loại do trình duyệt khai: `imager` kiểm bằng nội dung; danh sách trắng JPEG, PNG, WebP; SVG bị từ chối ngay khi xin tải lên | M1-41: `image/svg+xml` → 422; M1-42: `TestImager_RejectsHTMLAsJPEG` |
| R | Nhân viên chối đã tải tệp lên | Dòng `platform.files` có `created_by`, `created_at`; audit cho thao tác gắn ảnh vào mẫu | M1-41 (cột), M1-44 (`RecordAudit`) |
| I | Đường dẫn ký sẵn lộ (chia sẻ, lọt vào log) | `PUT` hạn 15 phút, chỉ một khóa trong `crm-tmp`; `GET` riêng tư hạn ngắn (ảnh gốc 5 phút), cần quyền để xin mới, `Content-Disposition: attachment` | M1-40 mục 3; M1-44: `TestOriginalURL_ExpiresAndNeedsPermission` (TP05). Log không chứa chữ ký: **Chưa có** (L3) |
| I | Đoán mã tệp của shop khác | Khóa có `tenantId`; bảng `files` có RLS; `getFile`, `completeUpload`, `DownloadURL` của tenant khác → 404 | M1-41: "Tenant B `GET`/`complete` tệp của A → 404" (PQ08) |
| D | Tải lên hàng loạt, tệp khổng lồ, bỏ dở | Giới hạn theo mục đích (ảnh ≤ 20 MiB, Excel ≤ 5 MiB) trước khi ký; chữ ký khóa kích thước; `crm-tmp` tự xóa sau 1 ngày; giới hạn tần suất theo nhân viên của M0 | M1-41: `TestCreateUpload_TooLarge` (TP03); M1-40 mục 6 |
| E | Ứng dụng bị chiếm rồi xóa vĩnh viễn mọi ảnh | `crm-app` không có quyền `DeleteObject` theo `VersionId`; bucket giữ phiên bản; xóa chỉ là dấu xóa | M1-40 mục 4, 5; M1-41: "`Delete` bằng `crm-app` rồi đọc bằng `crm-admin` theo phiên bản → còn" |

## (2) Worker → `imager`: yêu cầu xử lý ảnh

Dữ liệu: JSON gồm một đường dẫn `GET` ký sẵn của ảnh thô, các đường dẫn `PUT` ký sẵn cho từng đầu ra, giới hạn. `imager` không có khóa bí mật, không xác thực bên gọi (ADR-45).

| STRIDE | Mối đe dọa | Xử lý | Task / test |
|---|---|---|---|
| S | Tiến trình khác gọi `imager` | Chỉ nghe trên mạng `imaging` (worker, `s3-imaging`); bên gọi chỉ nhận lại đường dẫn chính nó đã ký, nên giả danh không lấy thêm được gì | M1-40 (mạng compose), M1-42 |
| S | `imager` bị lừa tải URL nội bộ khác (SSRF qua `SourceURL`) | `imager` chỉ ra mạng tới kho tệp; không tới PostgreSQL. Kiểm host của `SourceURL`, `PutURL` trùng địa chỉ kho: **Chưa có** (L2) | M1-42: "từ container `imager`, nối `postgres:5432` → thất bại" |
| T | Ảnh độc làm sai kết quả trả về (báo `ok` mà không ghi đủ đầu ra) | Worker chỉ đặt `ready` khi `OnDone` nhận đủ danh sách; `Looker` kiểm mọi khóa đầu ra có trên kho | M1-43: `Looker` "đầu ra đã đủ trên kho → `done`" |
| R | Không rõ ảnh nào bị từ chối vì sao | `reject_reason` trên dòng tệp; chỉ số `image_rejected` theo lý do; lệnh `dead` có `last_error` | M1-43 (chỉ số), M1-41 (cột) |
| I | Khóa bí mật lọt qua `imager` bị chiếm | `imager` không có biến bí mật nào, không có danh tính kho | M1-42: `env` không khớp `(?i)secret\|password\|token\|key\|database` |
| D | Ảnh treo libvips, ảnh ngốn bộ nhớ, nhiều ảnh cùng lúc | Mỗi bước là tiến trình con: hạn `IMAGER_STEP_TIMEOUT`, `prlimit` bộ nhớ, giết cả nhóm; `IMAGER_MAX_PARALLEL` → 503; lệnh `file.process` hạn 30 s (ADR-45); quá giờ, bận, không tới được là lỗi hạ tầng: không tính lượt, thử lại lùi dần tới trần `IMAGE_RETRY_MAX_HOURS` (23 giờ, phải nhỏ hơn hạn 24 giờ của `crm-tmp`, Task 426) rồi `rejected processing_failed` + `dead` (cảnh báo) — Task 409; 3 lượt chỉ cho lần chạy có tính (in_doubt); worker chạy việc khác bình thường | M1-42: "Treo", "Hết bộ nhớ", "3 yêu cầu cùng lúc"; M1-43: "Treo … 20 việc `selftest` và 5 lệnh khác đều xong" (tiêu chí Xong 7) |
| D | Bom điểm ảnh (tệp 60 KB khai 30.000 × 30.000) | `vipsheader` đọc kích thước, vượt `MaxPixels` → từ chối **trước khi giải mã** | M1-42: `TestImager_PixelBombRejectedBeforeDecode` (TP03) |
| E | Lỗ hổng libvips cho chạy mã trong `imager` | Không cgo trong tiến trình Go nào khác; container người dùng thường, hệ thống tệp chỉ đọc trừ `/tmp`, giới hạn 2 GiB, 2 CPU; không tới database; libvips ghim bản, vá riêng | M1-42 (image, compose); ADR-45 |

## (3) `imager` → kho tệp: đọc ảnh thô, ghi biến thể

Dữ liệu: ảnh thô từ `crm-tmp`; biến thể ghi vào `crm-public` (ảnh sản phẩm) hoặc `crm-private` (mục đích riêng tư, R10). `imager` chỉ dùng đường dẫn ký sẵn, không có danh tính kho.

| STRIDE | Mối đe dọa | Xử lý | Task / test |
|---|---|---|---|
| S | `imager` ghi vào khóa không được giao | Mỗi `PutURL` ký cho đúng một khóa, hạn ngắn; không có danh tính để ký thêm | M1-43 (ký trong worker), M1-42: "`PutURL` hết hạn → `upload_failed`" |
| T | `imager` bị chiếm ghi nội dung độc (HTML) vào bucket công khai | Chữ ký `PUT` gồm `content-type` ảnh; khóa biến thể theo mã băm ảnh gốc. Trước `ready` (và trước khi tin bản kê có sẵn) worker `Head` mọi biến thể, `content-type` phải là `image/<định dạng>` đã ký, khác thì tệp `rejected output_invalid`, không công bố biến thể, không ghi bản kê (L1, Task 414). Header `X-Content-Type-Options: nosniff` cho bucket công khai: **Chưa có** — cần cấu hình SeaweedFS hoặc quy tắc Cloudflare, chờ chủ shop (task-414.md "Dừng để hỏi") | M1-41 (chữ ký có `content-type`); Task 414: `TestFileProcess_RejectsVariantWithWrongType` |
| T | Siêu dữ liệu vị trí (GPS) còn trong ảnh công khai | Mã hóa lại bằng `vips thumbnail … keep=none` | M1-42: `photo-gps.jpg` → không còn GPS, EXIF; M1-43 `files_pipeline_test` |
| I | Ảnh riêng tư của khách lọt sang bucket công khai | Mục đích chế độ `private` chỉ đi lệnh `file.process_private`, biến thể chỉ ở `crm-private`; `file.process` từ chối mục đích này | M1-43: `TestProcessPrivate_NeverInPublicBucket`, `TestFileProcess_RefusesPrivatePurpose` |
| D | Kho tệp chậm, đầy | Lệnh hạn 30 s, `not_sent` thì chạy lại, 3 lần thì `dead` và tệp `rejected processing_failed`; cảnh báo theo lệnh `dead` của M0 | M1-43: ca "Treo" |
| E | — | `imager` không có quyền nào ngoài các đường dẫn được giao | — |

## (4) `crm-filesync` → kho ngoài: chép bản sao mỗi giờ

Dữ liệu: mọi đối tượng của `crm-public` (không mã hóa), `crm-originals`, `crm-private` (mã hóa bằng rclone crypt trước khi rời máy, hai mật khẩu do chủ shop giữ). `crm-tmp` không chép.

| STRIDE | Mối đe dọa | Xử lý | Task / test |
|---|---|---|---|
| S | Kẻ khác giả kho ngoài (sai địa chỉ, DNS) | Kết nối HTTPS tới nhà cung cấp; dữ liệu riêng tư đã mã hóa nên kho giả không đọc được | M1-45 (cấu hình rclone) |
| T | Xóa nhầm hay hỏng ở kho chính lan sang bản sao | Chỉ `rclone copy --immutable --checksum`; không bao giờ `sync`, `move`, `delete`; tệp đích khác nội dung → `ok = false`, cảnh báo | M1-45 `filesync_test`: "xóa 1 đối tượng ở kho chính → kho ngoài vẫn còn", "ghi đè ở kho ngoài → lượt sau `ok = false`" |
| T | Chép ngừng mà không ai biết | Mỗi lượt một dòng `file_sync_runs`; quá 90 phút không có lượt tốt → cảnh báo gấp; đối chiếu số tệp mỗi ngày; nhịp sống gửi ra ngoài | M1-45: `TestRule_FileSyncAge` (TP10) |
| R | Không rõ lượt chép nào lỗi | `file_sync_runs` chỉ `INSERT`, có `detail` | M1-45 |
| I | Nhà cung cấp kho ngoài hoặc người có tài khoản đọc dữ liệu khách | `crm-originals`, `crm-private` mã hóa trước khi rời máy, khóa không nằm ở kho ngoài; câu pháp lý về nơi đặt dữ liệu còn mở | M1-45 `filesync_test`: "đọc thẳng ở kho ngoài không ra byte gốc"; TP11 (O1, chờ chủ shop) |
| I | Container chép giữ khóa ghi của ứng dụng | Dùng danh tính `crm-filesync` (chỉ đọc kho chính), không có `S3_SECRET_ACCESS_KEY` của `crm-app` | M1-45: "Container `filesync` không có biến … của `crm-app`" |
| D | Mất mật khẩu crypt thì bản sao vô dụng | Chủ shop giữ ở hai nơi ngoài máy chủ (bảng "Việc cần chủ shop"); diễn tập dựng lại có bấm giờ | M1-46: `restore-drill.sh` (TP09) |
| E | Role database của container chép bị lạm dụng | `crm_filesync` chỉ `INSERT` vào `platform.file_sync_runs`, không gì khác, tối đa 1 kết nối | M1-45: "`SELECT` bất kỳ bảng nào → lỗi quyền" |
| — | Tài khoản kho ngoài bị chiếm và xóa bản sao | Phụ thuộc khóa chống xóa của nhà cung cấp, ADR-36 ghi "chưa xác minh" | Rủi ro còn lại (L4) |

## (5) Tệp Excel của nhân viên → worker

Dữ liệu: tệp `.xlsx` (mục đích `catalog_import`, bucket `crm-private`, ≤ 5 MiB), đọc ở lệnh `catalog.import.validate`, ghi database ở việc `catalog.import.apply`. Tệp mẫu do hệ thống sinh và tải về.

| STRIDE | Mối đe dọa | Xử lý | Task / test |
|---|---|---|---|
| S | Người không có quyền nhập hàng loạt | `catalog.import` trên mọi operation nhập; tệp phải là của tenant đó, `uploaded` | M1-47 (quyền, RLS) |
| T | Bom nén (giải nén ra 1 GB), tệp không phải xlsx, quá nhiều dòng | `Limits`: 5 MiB, 50 MiB giải nén (`UnzipSizeLimit`), 20.000 dòng, 2.000 ký tự mỗi ô; chỉ đọc trang đầu | M1-38: `bom-zip.xlsx` → `ErrTooLarge` dưới 2 s, cấp thêm dưới 100 MB; HTML đổi đuôi → `ErrNotXlsx`; 20.001 dòng → `ErrTooManyRows` |
| T | Ô công thức trong tệp nhập | Ô có công thức → lỗi dòng `formula_not_allowed`, không lấy giá trị | M1-38 |
| T | Tệp xuất ra (mẫu, sau này báo cáo) chứa công thức độc khi mở bằng Excel | Mọi ô qua `xlsx.Sanitize` (thêm `'` trước `=`, `+`, `-`, `@`, tab, CR, LF; bỏ ký tự XML lạ) | M1-38: `xlsx.Write` với `=HYPERLINK(…)`, thuộc tính rapid của `Sanitize` |
| T | Ghi nửa chừng, hoặc tệp cũ ghi đè thay đổi tay mới hơn | Ghi tất cả hoặc không trong một transaction; kiểm lại SKU lúc áp; `version` của bản nhập | M1-47: thuộc tính rapid "đúng bằng trước, hoặc trước + plan"; ca "nhân viên tạo tay SKU giữa lúc kiểm và áp" |
| R | Không rõ ai áp bản nhập nào | `catalog.imports` có `created_by`, `applied_by`, `applied_at`; audit | M1-47 |
| I | Câu lỗi làm lộ nội dung tệp | Câu lỗi tiếng Việt, không chứa nội dung ô quá 50 ký tự; tệp ở bucket riêng tư | M1-38 (`RowError`), M1-41 (mục đích `catalog_import` ở `private`) |
| D | Tệp ngẫu nhiên làm sập bộ đọc | Fuzz `Read` trên byte ngẫu nhiên không panic; lệnh có hạn giờ | M1-38 (fuzz 60 s) |
| D | Nhập lại cùng tệp tạo trùng | Mã băm tệp → `duplicate`; 50 lần `apply` cùng khóa → 1 việc | M1-47: `TestApply_SameFileTwice` (KH01) |
| E | Thư viện đọc Excel có lỗ hổng | excelize chỉ được nhập trong `internal/platform/xlsx`; kiểm OSV ngày làm, có lỗ hổng chưa vá thì dừng hỏi | M1-38 (test kiến trúc, `docs/phu-thuoc.md`) |

## (6) `fakecarrier` → `ingest`: webhook hãng giả

Chỉ dev và staging (`CRM_ENV != production` và có `FAKECARRIER_URL`). Là bài tập cho đường hãng thật của M3, nên xử lý như hãng thật.

| STRIDE | Mối đe dọa | Xử lý | Task / test |
|---|---|---|---|
| S | Kẻ khác gửi webhook giả "đã giao" | Chữ ký `x-fake-signature: sha256=<HMAC>` với khóa ≥ 32 ký tự, so thời gian hằng; sai → 401; nhiều lần sai từ một IP → chặn tạm (M0 Task 14) | M1-31: "Webhook chữ ký sai → 401" |
| T | Webhook đúng chữ ký nhưng nội dung sai, đổi thứ tự | Webhook chỉ là tín hiệu: worker hỏi trạng thái thật qua API có xác thực rồi mới đổi; trạng thái chỉ tiến; chỉ vận đơn hiện hành đổi được đơn và tồn | M1-31 (`refresh_status`, `status.go`) |
| R | Hãng nói đã gửi, CRM nói chưa nhận | `webhook_deliveries` lưu gói gốc (M0 Task 14); `shipment_events` chỉ ghi thêm | M1-31 |
| I | Payload mang dữ liệu khách vào log | Payload sự kiện chỉ mang mã; log che sâu của M0 | M1-31, M0 Task 3 |
| D | Gửi dồn dập, gửi trùng | Chống trùng theo `eventId` (`DedupeKey`); giới hạn thân và chặn IP của `ingest` (M0 Task 14) | M1-31; M0 Task 14 |
| E | Hãng giả chạy trên production | Không đăng ký nguồn khi `CRM_ENV=production` | M1-31: "`POST /hooks/fakecarrier` → 404" |
| E | Webhook của tài khoản lạ đổi đơn của shop khác | Định tuyến theo `carrier_account:<accountKey>`; không có tuyến → `inbound_unrouted`, không việc nào | M1-31: "tài khoản lạ … không việc nào" |

## Lỗ hổng chưa có trong kế hoạch (đề xuất)

Không sửa kế hoạch ở Task M1-24 (chỉ sửa chỗ lệch với mã M0). Mỗi dòng là đề xuất cho task ghi kèm; task đó thêm test trong cùng commit, hoặc ghi lý do không làm vào tệp task của nó.

| Mã | Lỗ hổng | Đề xuất | Task |
|---|---|---|---|
| L1 | `imager` bị chiếm có thể ghi tệp không phải ảnh vào `crm-public`; tệp công khai được đệm vĩnh viễn ở Cloudflare | `OnDone` của `file.process` `Head` từng biến thể, `content-type` phải đúng định dạng đã ký; bucket công khai trả `X-Content-Type-Options: nosniff` (cấu hình SeaweedFS hoặc quy tắc Cloudflare) | Kiểm đầu ra: **đã làm** ở Task 414 (`TestFileProcess_RejectsVariantWithWrongType`). Header `nosniff`: chưa — việc ngoài máy (Cloudflare) hoặc đổi hạ tầng, chờ chủ shop (task-414.md) |
| L2 | `SourceURL`, `PutURL` không được `imager` kiểm host | `imager` từ chối URL có host khác `S3_ENDPOINT` nội bộ (cấu hình không bí mật) — phòng khi mạng `imaging` bị cấu hình sai | M1-42 |
| L3 | Đường dẫn ký sẵn có thể lọt vào log hoặc dấu vết (lỗi của aws-sdk, span HTTP) | Test: log và span của `createUpload`, `file.process` không chứa `X-Amz-Signature` | **Đã làm** ở Task 414: `TestFilesAPI_SignedURLNotLogged` (createUpload, complete, getFile), `TestFileProcess_SignedURLsNotLogged` (ảnh tốt, bị từ chối, imager lỗi 500; cả cột `result`, `last_error` của lệnh) — so bằng chính chữ ký của từng đường dẫn |
| L4 | Tài khoản kho ngoài bị chiếm thì xóa được bản sao | Kiểm khóa chống xóa (object lock) của kho ngoài chủ shop chọn; ghi vào `docs/do-luong/` | M1-45 bước kho thật (cần chủ shop) |
