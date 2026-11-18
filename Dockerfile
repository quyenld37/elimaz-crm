# syntax=docker/dockerfile:1
# Image chính của CRM: một tệp chạy Go tĩnh cho mọi vai trò (CRM_ROLE: ingest, api, worker, migrate). Kế hoạch M0
# bản 5, Task 21; ADR-43 (image dựng ở CI), Global Constraints (CGO_ENABLED=0, -trimpath, không root, không secret).
#   docker build -t crm:dev .
# Image chạy là distroless static: không shell, không trình quản lý gói; chạy bằng nonroot (65532). Kiểm sức khỏe
# bằng chính tệp chạy (`crm healthcheck`).

# golang:1.27.4-trixie, digest lấy ngày 18/11/2026 (cùng bản với docker/imager/Dockerfile)
ARG GO_IMAGE=golang:1.27.4-trixie@sha256:f18c3d73a184fb0582828510722e723b63a93f7188d11dd223d9be6f6880e65d
# gcr.io/distroless/static-debian12:nonroot, digest của image index đa nền tảng, lấy ngày 18/11/2026
ARG RUN_IMAGE=gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/crm ./cmd/crm
# Thư mục tệp tạm của ingest (SPOOL_DIR): volume mới gắn vào đây nhận chủ 65532 từ image.
RUN mkdir -p /out/spool && chmod 0700 /out/spool

FROM ${RUN_IMAGE}
COPY --from=build /out/crm /crm
COPY --from=build --chown=65532:65532 /out/spool /var/spool/crm
USER 65532:65532
EXPOSE 4190 4191
HEALTHCHECK --interval=10s --timeout=5s --start-period=20s --retries=3 CMD ["/crm", "healthcheck"]
ENTRYPOINT ["/crm"]
