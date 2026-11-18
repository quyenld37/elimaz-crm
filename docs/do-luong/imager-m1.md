# Đo tiến trình `imager` (Task M1-42, ADR-45)

Ngày đo: 18/11/2026. Máy: MacBook (Apple Silicon, arm64), Docker Desktop 29.2.1. Container giới hạn như compose dev:
`--cpus 2 --memory 2g`, libvips 8.16.1 (gói Debian 13 `libvips-tools` 8.16.1-1+deb13u2, `libheif-plugin-aomenc`
1.23.6, libaom 3.12.1). Mỗi lệnh con chạy qua `prlimit --as=1GiB`, `VIPS_CONCURRENCY=2`, `MALLOC_ARENA_MAX=2`.

**Lưu ý độ tin của số đo:** máy đang chạy song song nhiều phiên khác (load average 16–22 trên máy chủ lúc đo), nên
cùng một ảnh lệch tới 3 lần giữa các lần chạy. Ghi cả khoảng, không chỉ một số. Đo lại trên máy staging (ít tải) ở
Task M1-55 trước khi quyết theo "Xem lại khi" của ADR-45.

## Cách đo

- Trọn một ảnh (tải nguồn qua HTTP, 1 `vipsheader`, 12 `vips thumbnail` gồm AVIF/WebP/JPEG × 320/640/1080/1600, PUT
  qua HTTP cục bộ): `TestImager_Measure` (chỉ chạy khi đặt `IMAGER_MEASURE_DIR`):

  ```sh
  docker build -q -f docker/imager/Dockerfile --target test -t crm-imager-test .
  docker run --rm --init --cpus 2 --memory 2g -v <thư mục ảnh .jpg>:/b:ro -e IMAGER_MEASURE_DIR=/b \
    crm-imager-test go test -count=1 -run TestImager_Measure -v ./internal/imager/
  ```

  Mỗi ảnh 3 lần; in thời gian từng lệnh con, tổng, và `vips --version` × 13 qua `prlimit` (khởi động tiến trình rỗng).
- CPU (user + sys) và bộ nhớ đỉnh (RSS) mỗi lệnh: chạy đúng 13 lệnh trên bằng `/usr/bin/time -f "%e %U %S %M"` trong
  cùng container (script đo tạm, không commit; lệnh giống hệt `internal/imager/pipeline.go`).

Ảnh: `testdata/images/big-24mp.jpg` (6000×4000, tổng hợp từ dải màu và sóng, 0,9 MB — rất dễ mã hóa, không giống
ảnh chụp); một ảnh chụp thật 3700×2467 (9,1 MP, ảnh vải, 8,4 MB); chính ảnh đó phóng lên 4255×2837 (12,1 MP, 4,8 MB) vì
máy không có ảnh 12 MP gốc. Ảnh thật không commit.

## Kết quả

### Tổng một ảnh, 12 đầu ra (ms, ba lượt × ba lần)

| Ảnh | Lượt 1 | Lượt 2 | Lượt 3 |
|---|---|---|---|
| big-24mp.jpg (tổng hợp) | 2235, 1670, 1288 | 2022, 1896, 1991 | 2875, 2475, 3533 |
| ảnh thật 9,1 MP | 2985, 3503, 3365 | 5467, 5069, 10499 | 3793, 4091, 3545 |
| ảnh thật 12,1 MP | — | 4498, 6131, 4284 | 4922, 4144, 4414 |

Lệnh con chiếm 98–99% tổng; tải nguồn và 12 PUT qua HTTP cục bộ dưới 70 ms. **Mọi lần đều dưới 30 s** (lớn nhất
10,5 s, lúc máy tải nặng nhất).

### Từng bước (ms, một lần đại diện của lượt 1)

| Bước | big-24mp | ảnh thật 9,1 MP |
|---|---|---|
| `vipsheader` (kích thước, bộ đọc) | 27 | 31 |
| JPEG 320 / 640 / 1080 / 1600 | 38 / 43 / 57 / 92 | 87 / 95 / 168 / 109 |
| WebP 320 / 640 / 1080 / 1600 | 52 / 58 / 136 / 147 | 104 / 160 / 194 / 251 |
| AVIF 320 / 640 / 1080 / 1600 | 62 / 85 / 178 / 299 | 103 / 238 / 696 / 1096 |

AVIF 1080 và 1600 chiếm khoảng một nửa thời gian của ảnh thật.

### CPU và bộ nhớ (13 lệnh, `/usr/bin/time`)

| Ảnh | Thời gian (s) | CPU user+sys (s) | RSS đỉnh của một lệnh |
|---|---|---|---|
| ảnh thật 9,1 MP | 6,55 – 9,06 | 12,5 (lần 9,06 s) | 108 MiB (AVIF 1600) |
| ảnh thật 12,1 MP | 7,53 | 10,7 | 109 MiB (AVIF 1600) |
| big-24mp.jpg | 2,21 – 5,26 | 5,7 | 100 MiB (AVIF 1600) |

`vipsheader` dùng 34 MiB. Bộ nhớ đỉnh xa dưới giới hạn 1 GiB mỗi lệnh (`IMAGER_STEP_MEMORY`) và 2 GiB của container
(2 việc song song ≈ 220 MiB). JPEG nhờ thu nhỏ ngay khi đọc (shrink-on-load) nên ảnh 24 MP không cần giải mã đủ cỡ.

### Phần tốn thêm của cách dòng lệnh

`vips --version` × 13 qua `prlimit` (khởi động libvips và các mô-đun, không làm gì): 677 ms (lượt 1), 1494 ms (lượt 2),
344 ms (lượt 3). Tỉ lệ so với tổng một ảnh trong cùng lượt:

| Ảnh | Lượt 1 | Lượt 2 | Lượt 3 |
|---|---|---|---|
| ảnh thật 9,1 MP | 20% | 15–29% | 8–10% |
| ảnh thật 12,1 MP | — | 24–35% | 7–8% |
| big-24mp.jpg | 30–53% | 75–79% | 10–14% |

Bản đầu của imager chạy 27 lệnh con mỗi ảnh (3 lần `vipsheader -f`, thêm một `vipsheader` cho mỗi đầu ra); đo thấy
tỉ lệ này cao nên giảm còn 13 (task-42.md, Q3), có test giữ con số này (`TestImager_ThirteenChildProcessesPerImage`).

## Kết luận và đề xuất (không tự đổi)

- Tổng một ảnh nằm trong giới hạn 30 s của một việc worker (04 mục 5.1) với khoảng cách lớn (≤ 10,5 s khi máy tải
  nặng, 3–5 s bình thường cho ảnh thật). Không cần tách mỗi biến thể một việc.
- Phần tốn thêm của cách dòng lệnh **vượt 30% ở một số lần đo**: luôn với ảnh tổng hợp dễ mã hóa (mỗi lệnh chỉ vài
  chục ms nên phần khởi động chiếm nhiều), và một lần với ảnh thật 12 MP lúc máy tải nặng nhất. Với ảnh chụp thật lúc
  máy ít tải: 7–20%.
- Theo "Xem lại khi" của ADR-45, vượt 30% thì chuyển sang govips trong chính `imager`. Đề xuất: **chưa đổi**; đo lại
  bằng `TestImager_Measure` trên máy staging với ảnh sản phẩm thật (Task M1-55). Nếu ảnh thật vẫn vượt 30% ở đó, báo
  chủ shop chọn: govips trong `imager` (một lần giải mã cho 12 đầu ra, mất khả năng giết từng lệnh), hoặc giữ dòng lệnh
  (chi phí thêm dưới 1 s mỗi ảnh, ảnh sản phẩm tải lên không nhiều).
- Phần nặng nhất là AVIF 1080/1600. Nếu cần nhanh hơn mà vẫn dòng lệnh: thử tham số `effort` thấp hơn của `heifsave`
  (mặc định 4) — chưa đo, cần so chất lượng ảnh trước.
