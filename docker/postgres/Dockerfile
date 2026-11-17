# syntax=docker/dockerfile:1
# PostgreSQL 18 + WAL-G (ADR-37) + pg_stat_statements. Kế hoạch M0 bản 5, Task 2.
# Image gốc ghim digest; WAL-G ghim phiên bản và sha256 của tệp tải về, cho cả arm64 và amd64.
FROM postgres:18-bookworm@sha256:ece55f7868da684e400f82f1030a050718adbdd65c23aa500c486f6e2950dc0f

ARG TARGETARCH
ARG WALG_VERSION=v3.0.9
# Bản dựng cho Ubuntu 22.04 (glibc 2.35) chạy được trên Debian 12 (glibc 2.36); bản 24.04 cần glibc 2.39 thì không.
ARG WALG_SHA256_ARM64=20191a37ad091a880c25f4cc907e6f0deb7f12bdd8a53fd62dbc7fcbf2eac712
ARG WALG_SHA256_AMD64=4f03ee4679db7f660bfd1b2b8291ac5df247cd2ec48074b60c112f4897299f24

RUN set -eux; \
    case "${TARGETARCH}" in \
      arm64) arch=aarch64; sum="${WALG_SHA256_ARM64}" ;; \
      amd64) arch=amd64;   sum="${WALG_SHA256_AMD64}" ;; \
      *) echo "kiến trúc không hỗ trợ: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl; \
    curl -fsSL -o /tmp/wal-g.tar.gz \
      "https://github.com/wal-g/wal-g/releases/download/${WALG_VERSION}/wal-g-pg-22.04-${arch}.tar.gz"; \
    echo "${sum}  /tmp/wal-g.tar.gz" | sha256sum -c -; \
    tar -xzf /tmp/wal-g.tar.gz -C /tmp; \
    install -m 0755 "/tmp/wal-g-pg-22.04-${arch}" /usr/local/bin/wal-g; \
    rm -f /tmp/wal-g.tar.gz "/tmp/wal-g-pg-22.04-${arch}"; \
    apt-get purge -y --auto-remove curl; \
    rm -rf /var/lib/apt/lists/*; \
    wal-g --version

COPY conf.d/ /etc/postgresql/conf.d/
# Chạy lúc khởi tạo cụm: nối include_dir vào postgresql.conf. Cụm khôi phục từ bản sao lưu mang sẵn dòng này.
COPY initdb/000-crm-conf.sh /docker-entrypoint-initdb.d/000-crm-conf.sh
