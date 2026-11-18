# Image của hãng vận chuyển giả (Task M1-31), chỉ dev và staging. Dựng từ gốc apps/crm:
#   docker build -f tools/fakecarrier/Dockerfile -t crm-fakecarrier:dev .
# Ghim cùng image dựng và image chạy với Dockerfile chính (digest lấy ngày 18/11/2026).
ARG GO_IMAGE=golang:1.27.4-trixie@sha256:f18c3d73a184fb0582828510722e723b63a93f7188d11dd223d9be6f6880e65d
ARG RUN_IMAGE=gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY internal ./internal
# migrations: gói nhúng mà internal/platform/db cần (fakecarrier nhập adapters/fakecarrier → fulfillment → db).
COPY migrations ./migrations
COPY tools/fakecarrier ./tools/fakecarrier
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -o /out/fakecarrier ./tools/fakecarrier

FROM ${RUN_IMAGE}
COPY --from=build /out/fakecarrier /fakecarrier
USER 65532:65532
EXPOSE 4194
ENTRYPOINT ["/fakecarrier"]
