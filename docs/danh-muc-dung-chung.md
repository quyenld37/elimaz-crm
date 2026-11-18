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

## `internal/platform/idempotency` (gói `idempotency`)

Package idempotency là khóa chống trùng của yêu cầu ghi (ADR-41; vùng rủi ro cao): bên gọi gửi một khóa, Acquire ghi nhận khóa ở trạng thái running bằng một transaction ngắn riêng, rồi Claim.Complete ghi "đã xong" kèm kết quả **trong transaction nghiệp vụ**.

**Khi nào không dùng:** Không gọi Acquire hay Claim.Complete trực tiếp từ handler: route ghi dùng httpapi.WriteTx (middleware đã ghi nhận khóa). Complete phải là câu cuối của transaction nghiệp vụ (ADR-46 nhóm 13), không gọi ngoài transaction đó. Không dùng thay khóa nghiệp vụ unique (orders.idempotency_key, jobs.dedupe_key): khóa yêu cầu không chặn hai khóa khác nhau cho cùng một việc. Không lưu bí mật trả một lần: route đó khai x-idempotency: no-replay.

```go
func Acquire(ctx context.Context, d *db.DB, s Scope, key string, reqHash []byte, now time.Time) (Claim, *Stored, error)
func RequestHash(method, route string, body []byte) []byte
type Claim interface {
	Complete(ctx context.Context, tx pgx.Tx, status int, body []byte) error

	Release(ctx context.Context) error
}
type Scope struct {
	TenantID uuid.UUID
	Actor    string
	Route    string
}
type Stored struct {
	Status int
	Body   []byte
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
func (h *Health) Check(ctx context.Context) error
func (h *Health) Register(mux *http.ServeMux)
type Pinger interface {
	Ping(ctx context.Context) error
}
```

## `internal/platform/queue` (gói `queue`)

Package queue là lõi hàng đợi việc nền trên bảng platform.jobs (spec 04 mục 5.1): ghi việc trong transaction của bên gọi (JobQueue), worker nhận việc bằng FOR UPDATE SKIP LOCKED, thử lại theo lịch, thứ tự theo đối tượng (OrderKey), hết hạn, in_doubt khi giữ chỗ hết hạn, tắt êm, dấu vết đi qua hàng đợi.

**Khi nào không dùng:** Không viết SQL lên platform.jobs ở gói khác: ghi việc bằng JobQueue.Enqueue trong transaction của dữ liệu, xử lý bằng Handler. Handler transactional không gọi mạng, không gửi gì ra ngoài (lệnh ra ngoài là commands, Task 12) và phải chạy lại được (có thể bị gọi lại). Không đặt nội dung hay dữ liệu cá nhân vào Payload, DedupeKey, OrderKey hay chuỗi lỗi: chỉ mã bản ghi. Không dùng hàng đợi cho việc cần nguyên tử với dữ liệu (cái đó nằm trong cùng một transaction), không tạo Worker thứ hai trong một tiến trình.

```go
func NextDelay(attempt int, rnd func() float64) time.Duration
type Handler interface {
	Kind() Kind

	Handle(ctx context.Context, j Job, tx pgx.Tx) (json.RawMessage, error)
}
type Job struct {
	ID       uuid.UUID
	TenantID *uuid.UUID
	Kind     string
	Payload  json.RawMessage

	Attempts  int
	CreatedAt time.Time
}
type JobQueue interface {
	Enqueue(ctx context.Context, tx pgx.Tx, k Kind, j NewJob) (id uuid.UUID, created bool, err error)
}
func NewQueue() JobQueue
type Kind struct {
	Name   string
	Policy Policy
}
type NewJob struct {
	TenantID *uuid.UUID

	Payload any

	DedupeKey string

	OrderKey string
	OrderSeq int64

	RunAt time.Time
}
type Policy struct {
	Mode string

	MaxAttempts int

	BlockOnDead bool

	OnLeaseExpired string

	ExpiresAfter time.Duration

	Timeout time.Duration
}
type TerminalHook interface {
	OnTerminal(ctx context.Context, j Job, status string, tx pgx.Tx) error
}
type Worker struct {
}
func NewWorker(d *db.DB, hs []Handler, o WorkerOptions) *Worker
func (w *Worker) RunOnce(ctx context.Context, limit int) (int, error)
func (w *Worker) Start(ctx context.Context) error
func (w *Worker) Stop(grace time.Duration) error
type WorkerOptions struct {
	WorkerID string

	Concurrency int

	Lease time.Duration

	Poll time.Duration

	Metrics *metrics.Registry

	Logger *slog.Logger
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

## `internal/platform/tenancy` (gói `tenancy`)

Package tenancy đặt ngữ cảnh tenant cho transaction (WithTenant), tìm tenant từ khóa ngoài (Resolve, ListTenantIDs) và kiểm quy tắc schema của bảng nghiệp vụ (CheckSchemaRules).

**Khi nào không dùng:** Không tự đặt app.tenant_id bằng set_config hay SET (kể cả SET LOCAL) ở gói khác: dùng WithTenant. Không dùng Resolve, ListTenantIDs để đọc dữ liệu nghiệp vụ (chỉ là bảng định tuyến). Không gọi CheckSchemaRules bằng crm_app (cần chủ bảng, chỉ dùng trong test và cổng kiểm). Bảng mới bật RLS bằng platform.enable_tenant_rls, không viết policy tay.

```go
func AddRoute(ctx context.Context, owner *db.DB, kind, externalKey string, tenantID uuid.UUID) error
func CheckSchemaRules(ctx context.Context, owner *pgxpool.Pool, platformTables []string) ([]string, error)
func ListTenantIDs(ctx context.Context, d *db.DB) ([]uuid.UUID, error)
func Resolve(ctx context.Context, d *db.DB, kind, externalKey string) (uuid.UUID, bool, error)
func SeedTenant(ctx context.Context, owner *db.DB, name string) (uuid.UUID, error)
func WithTenant(ctx context.Context, d *db.DB, tenantID uuid.UUID, fn func(pgx.Tx) error, o ...db.TxOptions) error
```
