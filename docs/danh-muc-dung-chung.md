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

## `internal/platform/db` (gói `db`)

Package db mở pool pgx có giới hạn thời gian (ADR-26), chạy transaction có thử lại (db.RetryTx, ADR-46) và chạy migration goose có checksum và luật mở rộng trước (Migrate, LintContract).

**Khi nào không dùng:** Không mở transaction bằng Begin/BeginTx/pgx.BeginFunc ở gói khác (txusage_test chặn) và không tự viết vòng thử lại: dùng RetryTx (hoặc tenancy.WithTenant từ Task 5). Hàm trong RetryTx không gọi mạng, không cộng dồn vào biến bên ngoài vì có thể chạy tới 4 lần. Không gọi Migrate ngoài vai trò migrate; không mở pool thứ hai cho cùng vai trò.

```go
func Backoff(n int, rnd func() float64) time.Duration
func LintContract(sql string) []string
func Migrate(ctx context.Context, ownerURL string) (applied []string, err error)
func TransientCode(err error) (code string, ok bool)
type DB struct {
	Pool *pgxpool.Pool

	OnRetry func(code string, attempt int)

	OnExhausted func(code string)
}
func Open(ctx context.Context, url string, o PoolOptions) (*DB, error)
func (d *DB) RetryTx(ctx context.Context, o TxOptions, fn func(pgx.Tx) error) error
type PoolOptions struct {
	MaxConns         int32
	StatementTimeout time.Duration
	IdleInTxTimeout  time.Duration
	LockTimeout      time.Duration

	DefaultTx TxOptions

	Tracing bool
}
type Setting struct {
	Name, Value string
}
type TxOptions struct {
	LockTimeout      time.Duration
	StatementTimeout time.Duration
	IsoLevel         pgx.TxIsoLevel
	Settings         []Setting
}
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

## `internal/platform/metrics` (gói `metrics`)

Package metrics gom chỉ số của tiến trình trong bộ nhớ và ghi mỗi phút một lần vào platform.metrics_minute (spec 04 mục "Đo lường"; không có /metrics, ADR C6), ghi nhịp sống vào platform.process_heartbeats và gửi nhịp sống ra dịch vụ bên ngoài (ADR-38).

**Khi nào không dùng:** Không mở endpoint /metrics hay thêm thư viện chỉ số (ADR C6); không tạo Registry thứ hai trong một tiến trình (cmd/crm tạo một cái và truyền xuống). Không đặt đường dẫn thật, tham số, SĐT hay mã khách vào tên hoặc nhãn chỉ số (nhãn route dùng mẫu của ServeMux). Không dùng Flusher cho dữ liệu nghiệp vụ hay số phải chính xác tuyệt đối: lỗi commit không rõ kết quả có thể ghi một lô hai lần.

```go
func HeartbeatAge(ctx context.Context, d *db.DB, process string) (time.Duration, error)
func Middleware(reg *Registry, next http.Handler) http.Handler
func P95FromHist(hist []int) float64
func ProcessName(role config.Role) string
type Flusher struct {
	DB      *db.DB
	Reg     *Registry
	Process string
	Role    config.Role
	Pinger  *Pinger
	Log     *slog.Logger
}
func (f *Flusher) Beat(ctx context.Context, now time.Time) error
func (f *Flusher) Flush(ctx context.Context, now time.Time) error
func (f *Flusher) Run(ctx context.Context)
type Kind string
type Pinger struct {
	URL    string
	Client *http.Client
}
func (p *Pinger) Ping(ctx context.Context) error
type Registry struct {
}
func NewRegistry() *Registry
func (r *Registry) Drain() []Row
func (r *Registry) Gauge(name string, v float64, label string)
func (r *Registry) Inc(name, label string, by int64)
func (r *Registry) Observe(name string, v float64, label string)
type Row struct {
	Name, Label string
	Kind        Kind
	Count       int64
	Sum, Max    float64
	Hist        []int
}
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
