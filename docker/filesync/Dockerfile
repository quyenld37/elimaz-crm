# Image của container filesync (Task M1-45; ADR-36, kế hoạch M1 Q19): rclone chép kho chính sang kho ngoài mỗi giờ,
# psql ghi kết quả vào platform.file_sync_runs (role crm_filesync), curl gửi nhịp sống ra ngoài. Dựng từ gốc apps/crm:
#   docker build -f docker/filesync/Dockerfile -t crm-filesync:dev .
# Không mã Go, không khóa nào trong image: mọi khóa đến từ biến môi trường lúc chạy (scripts/filesync/lib.sh).

# rclone/rclone:1.74.4 (Alpine 3.24), cùng digest với docker/compose.backup-drill.yml: một bản rclone cho cả repo.
FROM rclone/rclone:1.74.4@sha256:c61954aaa32328a5486715dd063a81c7879f5195ad3505cd362deddd509dc4a1

# Bản gói ghim (Alpine 3.24, lấy ngày 19/11/2026); nâng bản thì chạy lại tests/e2e/filesync_test.go.
RUN apk add --no-cache postgresql18-client=18.7-r0 curl=8.23.0-r0 jq=1.8.2-r0 tzdata=2026b-r0 \
 && mkdir -p /state && chown 65534:65534 /state

COPY --chmod=0555 scripts/filesync/ /opt/filesync/

ENV HOME=/tmp TZ=UTC FILESYNC_STATE_DIR=/state
USER 65534:65534
VOLUME ["/state"]
ENTRYPOINT ["/opt/filesync/run.sh"]
