# Phiên bản ghim của công cụ dựng và kiểm (kế hoạch M0 bản 5, Global Constraints; ADR-44).
# Đổi một dòng ở đây là một thay đổi phải ghi trong tệp task.

GO_TOOLCHAIN        := go1.27.4
GOPLS_VERSION       := v0.24.0
GOFUMPT_VERSION     := v0.12.0
GOIMPORTS_VERSION   := v0.52.0   # golang.org/x/tools/cmd/goimports
GOLANGCI_VERSION    := v2.15.0
GOVULNCHECK_VERSION := v1.9.0
SQLC_VERSION        := v1.31.1
OGEN_VERSION        := v1.25.0
GOOSE_VERSION       := v3.30.1
GREMLINS_VERSION    := v0.6.0
OASDIFF_VERSION     := v1.38.0
JSCPD_VERSION       := 5.4.3
K6_VERSION          := v2.0.0    # Homebrew, chốt ở Task 1
TS_LS_VERSION       := 6.1.0     # typescript-language-server, cho plugin typescript-lsp
TYPESCRIPT_VERSION  := 7.0.2
