# Image của tiến trình imager (ADR-45, Task M1-42). Dựng từ gốc apps/crm:
#   docker build -f docker/imager/Dockerfile -t crm-imager:dev .
# Stage `test` (make test-imager) chạy test của internal/imager với đúng bản libvips của image này.
#
# libvips: gói Debian 13 `libvips-tools` 8.16.1 có bộ ghi AVIF (`heifsave`, nén av1) khi có
# `libheif-plugin-aomenc` (đo 18/11/2026, docs/do-luong/imager-m1.md), nên không dựng libvips từ mã nguồn (Q14).
# Bản gói ghim dưới đây; nâng bản thì chạy lại make test-imager và phép đo.
#
# Không có biến bí mật, không có gì nối database. Chạy bằng người dùng 65534; compose đặt read_only, /tmp tmpfs.

# debian:trixie-slim (Debian 13), digest lấy ngày 18/11/2026
ARG DEBIAN_IMAGE=debian:trixie-slim@sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2
# golang:1.27.4-trixie (cùng Debian 13 với image chạy), digest lấy ngày 18/11/2026
ARG GO_IMAGE=golang:1.27.4-trixie@sha256:f18c3d73a184fb0582828510722e723b63a93f7188d11dd223d9be6f6880e65d

ARG VIPS_PKGS="libvips-tools=8.16.1-1+deb13u2 libheif-plugin-aomenc=1.23.6-2~deb13u1"

# ---- dựng tệp chạy tĩnh, không cgo
FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -o /out/imager ./cmd/imager

# ---- test: Go + libvips cùng bản (make test-imager)
FROM ${GO_IMAGE} AS test
ARG VIPS_PKGS
RUN apt-get update && apt-get install -y --no-install-recommends ${VIPS_PKGS} \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# ---- image chạy
FROM ${DEBIAN_IMAGE}
ARG VIPS_PKGS
# tini làm PID 1: gặt tiến trình con mồ côi khi imager giết cả nhóm tiến trình của một lệnh treo.
# util-linux (có sẵn, gói Essential) cho prlimit.
RUN apt-get update && apt-get install -y --no-install-recommends ${VIPS_PKGS} tini=0.19.0-3+b8 \
    && rm -rf /var/lib/apt/lists/* \
    && command -v prlimit vips vipsheader
COPY --from=build /out/imager /usr/local/bin/imager
USER 65534:65534
ENV PORT=4193
EXPOSE 4193
HEALTHCHECK --interval=5s --timeout=3s --retries=5 CMD ["/usr/local/bin/imager", "healthcheck"]
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/imager"]
