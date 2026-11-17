# Danh mục dùng chung

> Sinh tự động bằng `make catalog` (`go run ./tools/catalog`) từ tài liệu Go của `internal/platform/*`. Không sửa tay; dòng "Khi nào không dùng" sửa ở `docs/danh-muc-ghi-chu.yaml`.

## `internal/platform/config` (gói `config`)

Package config đọc và kiểm biến môi trường theo vai trò của tiến trình (ingest, api, worker, migrate).

**Khi nào không dùng:** Không đọc biến môi trường ở gói khác (linter chặn os.Getenv); không thêm cấu hình nghiệp vụ thay đổi lúc chạy (cái đó nằm trong database). Truyền Config đã kiểm vào hàm khởi tạo.

```go
func FromOS(key string) string
type Config struct {
	Role        Role
	Env         string
	DatabaseURL string
	LogDir      string

	TelegramBotToken     string
	TelegramChatID       string
	TelegramUrgentChatID string

	OTelEnabled      bool
	OTelEndpoint     string
	TracesSampler    string
	TracesSamplerArg float64

	HeartbeatURL string
	DBMaxConns   int32

	Port int

	SpoolDir              string
	SelftestWebhookSecret string

	AccessTeamDomain string
	AccessAUD        string
	AccessMFAClaim   string
	AccessMFAValues  []string
	ConsoleOrigins   []string
	HealthToken      string

	Migrate *MigrateConfig
}
func Load(role Role, getenv func(string) string) (Config, error)
func (c Config) LogValue() slog.Value
type MigrateConfig struct {
	OwnerURL string
}
type Role string
func ParseRole(s string) (Role, error)
```

## `internal/platform/logging` (gói `logging`)

Package logging dựng logger JSON của CRM: che sâu dữ liệu cá nhân và bí mật ở mọi cấp, thêm trace_id/span_id từ ctx, và khi có LOG_DIR thì ghi thêm vào tệp theo ngày.

**Khi nào không dùng:** Không tự dựng slog.Handler khác hay ghi thẳng ra stdout; không dựa vào che theo tên khóa cho nội dung tự do (chuỗi trong thông điệp không bị che): dữ liệu cá nhân đi bằng kiểu Phone, Address, Secret hoặc khóa có tên đúng.

```go
func NewLogger(cfg config.Config, stdout io.Writer) (*slog.Logger, io.Closer, error)
func Redact(v any, maxDepth int) any
func SensitiveKey(key string) bool
type Address string
func (Address) LogValue() slog.Value
type DailyFile struct {
	Dir      string
	Role     string
	Keep     int
	MaxBytes int64
	Now      func() time.Time
}
func NewDailyFile(dir, role string) *DailyFile
func (d *DailyFile) Check() error
func (d *DailyFile) Close() error
func (d *DailyFile) Dropped() int64
func (d *DailyFile) Write(p []byte) (int, error)
type Phone string
func (Phone) LogValue() slog.Value
type Secret string
func (Secret) LogValue() slog.Value
```

## `internal/platform/observability` (gói `observability`)

Package observability phục vụ các điểm kiểm sức khỏe của tiến trình (/health/live, /health/ready).

**Khi nào không dùng:** Không đặt kiểm tra nghiệp vụ hay dữ liệu khách vào /health/*; kiểm sâu (/health/deep, cần token) thuộc Task 19.

```go
type Health struct {
}
func NewHealth(db Pinger) *Health
func (h *Health) Register(mux *http.ServeMux)
type Pinger interface {
	Ping(ctx context.Context) error
}
```

## `internal/platform/telemetry` (gói `telemetry`)

Package telemetry dựng dấu vết OpenTelemetry của tiến trình: tracer provider toàn cục, sampler theo cấu hình, exporter OTLP HTTP tới Jaeger, và bộ truyền ngữ cảnh W3C traceparent.

**Khi nào không dùng:** Không tự tạo TracerProvider hay exporter ở gói khác; không gắn SĐT, địa chỉ, nội dung tin, token vào thuộc tính span. Gọi Setup một lần trong cmd/crm.

```go
func Setup(ctx context.Context, cfg config.Config, opts ...Option) (shutdown func(context.Context) error, err error)
type Option func(*options)
func WithSyncExporter(exp sdktrace.SpanExporter) Option
```
