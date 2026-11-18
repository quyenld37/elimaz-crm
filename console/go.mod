// Tệp này chỉ để công cụ Go bỏ qua thư mục console/ (màn hình TypeScript).
// console/node_modules có gói npm chứa tệp .go; không có tệp này thì `go build ./...` và lint của module gốc quét vào đó.
module elimaz.com.vn/crm/console-ignore

go 1.27
