# Danh mục dùng chung

> Sinh tự động bằng `make catalog` (`go run ./tools/catalog`) từ tài liệu Go của `internal/platform/*`. Không sửa tay; dòng "Khi nào không dùng" sửa ở `docs/danh-muc-ghi-chu.yaml`.

## `internal/platform/background` (gói `background`)

Package background giữ kiểu chung của phần chạy nền (việc nền, consumer sự kiện, lịch) mà mỗi module nghiệp vụ khai trong module.go (kế hoạch M2, Task M2-02): phụ thuộc Deps và kết quả Parts.

**Khi nào không dùng:** Không đặt phụ thuộc riêng của một module vào Deps (thêm vào kiểu riêng của module) và không dùng cho đường yêu cầu HTTP (httpapi.Deps). Không dựng Deps hay gọi Background của module ở chỗ khác ngoài cmd/crm (newModules), mỗi tiến trình một lần: gọi hai lần sinh hai sổ consumer lệch nhau. Parts chỉ liệt kê việc nền, consumer, lịch; không chạy gì lúc dựng.

```go
func Collect[T any](parts ...[]T) []T
func UniqueNames[T any](what string, items []T, name func(T) string) error
type Deps struct {
	DB *db.DB

	Queue queue.JobQueue

	Bus realtime.Bus

	Events *events.Registry

	Now func() time.Time
}
func (d Deps) Clock() func() time.Time
type Parts struct {
	JobHandlers []queue.Handler
	Consumers   []events.Consumer
	Schedules   []schedules.Schedule
}
```

## `internal/platform/cache` (gói `cache`)

Package cache là lớp nối Valkey duy nhất của CRM (ADR-39, kế hoạch M5): kết nối có hạn giờ mỗi lệnh, cầu dao, khóa băm không mang dữ liệu cá nhân.

**Khi nào không dùng:** Không dùng cho bất kỳ quyết định nghiệp vụ nào (còn hàng, đơn đã tạo, mua được, tiền: luôn PostgreSQL; ADR-39) và không gọi trong hàm của RetryTx hay handler transactional (gọi mạng, có thể chạy lại). Module nghiệp vụ không nhập gói này (luật kiến trúc l): đi qua ReadCache, RateLimiter, AdmissionGate. Mọi đường phải chạy khi Do trả ErrUnavailable (Conn nil, Valkey ngừng, cầu dao mở). Không ghép chuỗi tùy ý vào khóa: EntityKey cho mã đối tượng, SubjectKey (HMAC bằng CACHE_KEY_SECRET) cho SĐT, IP, email. Không đặt dữ liệu vào tên thao tác op (nhãn chỉ số), không mở Conn thứ hai cho cùng tiến trình Valkey, không nhập valkey-go ở gói khác.

```go
func EntityKey(prefix string, tenant uuid.UUID, parts ...string) (string, error)
func SubjectKey(secret []byte, kind, value string) string
type Admission struct {
	Admitted   bool
	RetryAfter time.Duration
	Reason     string
	Degraded   bool
}
func (a *Admission) Release()
type AdmissionGate interface {
	Admit(ctx context.Context, tenant, item uuid.UUID) *Admission

	SoldOut(ctx context.Context, tenant, item uuid.UUID) (soldOut, known bool)

	SetState(ctx context.Context, tenant, item uuid.UUID, remaining int, soldOut bool) error

	ClearState(ctx context.Context, tenant, item uuid.UUID) error

	Remaining(ctx context.Context, tenant uuid.UUID, items []uuid.UUID) map[uuid.UUID]int
}
func NewAdmissionGate(c *Conn, o GateOptions, m *metrics.Registry) AdmissionGate
type Breaker struct {
}
func NewBreaker(name string, o BreakerOptions, m *metrics.Registry) *Breaker
func (b *Breaker) Do(ctx context.Context, fn func(context.Context) error) error
type BreakerOptions struct {
	FailThreshold int
	OpenFor       time.Duration
	Now           func() time.Time
}
type Conn struct {
}
func Dial(ctx context.Context, rawURL string, o DialOptions) (*Conn, error)
func (c *Conn) Close()
func (c *Conn) Do(ctx context.Context, op string, fn func(ctx context.Context, cl valkey.Client) error) error
func (c *Conn) Health(ctx context.Context) Health
func (c *Conn) Ping(ctx context.Context) error
type DialOptions struct {
	OpTimeout time.Duration
	Breaker   BreakerOptions
	Metrics   *metrics.Registry
	Name      string
	Tracing   bool
}
type GateOptions struct {
	PerSecond, GlobalPerSecond, FallbackPerSecond, FallbackGlobalPerSecond, MaxInflight int
	Now                                                                                 func() time.Time
}
type Health string
type Limit struct {
	Name        string
	Max         int
	Window      time.Duration
	FallbackMax int
}
type Limits struct {
	WebKey Limit

	FlashCustomer Limit

	FlashIP Limit

	WebOrderCustomer Limit

	CouponGuessCustomer Limit

	CouponGuessIP Limit
}
func DefaultLimits() Limits
func ParseLimits(raw string) (Limits, error)
type RateDecision struct {
	Allowed    bool
	RetryAfter time.Duration
	Degraded   bool
}
type RateLimiter interface {
	Allow(ctx context.Context, l Limit, subjectKey string) RateDecision

	Peek(ctx context.Context, l Limit, subjectKey string) RateDecision
}
func NewRateLimiter(c *Conn, now func() time.Time, m *metrics.Registry) RateLimiter
type ReadCache interface {
	GetOrLoad(ctx context.Context, tenant uuid.UUID, space Space, id string, ttl time.Duration,
		load func(context.Context) ([]byte, error)) ([]byte, Source, error)

	Invalidate(ctx context.Context, tenant uuid.UUID, space Space, ids ...string) error

	BumpSpace(ctx context.Context, tenant uuid.UUID, space Space) error
}
func NewReadCache(c *Conn, o ReadCacheOptions) ReadCache
type ReadCacheOptions struct {
	FallbackConcurrency int
	MaxValueBytes       int
	Metrics             *metrics.Registry
}
type Source string
type Space string
func Spaces() []Space
```

## `internal/platform/commands` (gói `commands`)

Package commands là đường ra của CRM: lệnh ra ngoài (gửi tin, trả lời comment, tạo/hủy vận đơn) chạy như một việc mode command của hàng đợi (spec 04 mục 5.3; vùng rủi ro cao).

**Khi nào không dùng:** Không gọi mạng ra ngoài ở chỗ khác: mọi tác động ra ngoài là một lệnh (Kind từ CommandKind) ghi bằng JobQueue.Enqueue trong transaction nghiệp vụ, Handler.Execute gửi. Không trả StatusNotSent khi chưa chắc bên ngoài chưa nhận (lỗi, quá giờ → in_doubt). Không tự gửi lại lệnh in_doubt: chỉ ResolveInDoubt (Looker nói chắc chắn chưa có) hoặc người có quyền chạy lại. Lệnh không đụng database thì không cần commands; việc chỉ đụng database là Handler transactional của queue.

```go
func Abandon(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, reason string) error
func CommandKind(name string, expiresAfter, timeout time.Duration) queue.Kind
func ResolveInDoubt(ctx context.Context, d *db.DB, hs []Handler, limit int) (done, requeued, still int, err error)
func ResolveManually(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, result json.RawMessage, hs []Handler) error
func Status(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) (string, error)
type DoneHook interface {
	OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error
}
type Handler interface {
	Kind() queue.Kind

	AutoRetryNotSent() bool

	Execute(ctx context.Context, j queue.Job) (Outcome, error)
}
func NewInDoubtResolver(d *db.DB, hs []Handler) Handler
type Looker interface {
	Lookup(ctx context.Context, j queue.Job) (string, error)
}
type Outcome struct {
	Status string

	Reason string

	RetryAfter time.Duration

	KeepAttempt bool

	Result json.RawMessage
}
type Runner struct {
}
func NewRunner(d *db.DB, hs []Handler) *Runner
func (r *Runner) Kinds() []queue.Kind
func (r *Runner) Run(ctx context.Context, j queue.Job, workerID string) (string, error)
func (r *Runner) TerminalHook(kind string) queue.TerminalHook
```

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

	WebsiteOrigin string

	S3Endpoint        string
	S3Region          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3BucketPrefix    string

	FilesPublicBaseURL string

	ImagerURL          string
	ImagePresetVersion int

	S3ImagerEndpoint string

	ImagerMaxParallel int

	ImageRetryMax time.Duration

	FakeCarrierURL           string
	FakeCarrierWebhookSecret string

	FakeChatWebhookSecret logging.Secret

	CarrierWebhookSecrets map[string]logging.Secret

	BackupExpected    bool
	DBDiskBudgetBytes int64
	DBConnBudget      int

	FileSyncExpected bool

	JobsDoneRetentionDays int

	FieldEncKeys string

	ValkeyCacheURL string
	ValkeyGateURL  string
	CacheKeySecret logging.Secret

	RateLimitsJSON string

	FlashAdmitPerSecond, FlashAdmitGlobalPerSecond, FlashMaxInflight int

	Meta MetaConfig

	ConversationRetentionMonths int

	Report ReportConfig

	Migrate *MigrateConfig
}
func Load(role Role, getenv func(string) string) (Config, error)
func (c Config) LogValue() slog.Value
type MetaConfig struct {
	Enabled bool

	GraphVersion        string
	GraphVersionExpires time.Time

	GraphBaseURL     string
	StreamingBaseURL string

	AppID     string
	AppSecret logging.Secret

	LoginConfigID    string
	OAuthRedirectURL string

	VerifyToken        logging.Secret
	HooksPublicBaseURL string

	AttachmentHosts []string

	DataUseCheckupDue time.Time
}
type MigrateConfig struct {
	OwnerURL string
}
type ReportConfig struct {
	APISlots           int
	InteractiveTimeout time.Duration
	MaxPendingPerUser  int
	LinkTTL            time.Duration

	WorkerSlots       int
	BackgroundTimeout time.Duration
	TmpDir            string
	MaxRows           int
	FileTTL           time.Duration
	CustomerExportTTL time.Duration
}
type Role string
func ParseRole(s string) (Role, error)
```

## `internal/platform/datadeletion` (gói `datadeletion`)

Package datadeletion là callback xóa dữ liệu của Meta (kế hoạch M4, Task 179; spec 02 mục 5; Q30): ingest nhận yêu cầu (Endpoint: kiểm chữ ký, ghi platform.data_deletion_requests hoặc tệp tạm, trả mã xác nhận và trang trạng thái); worker chạy việc platform.data_deletion (Processor) gọi các Deleter do module đăng ký ở mọi tenant rồi đánh dấu xong.

**Khi nào không dùng:** Chỉ cho yêu cầu xóa dữ liệu do nhà cung cấp gửi tới (callback xóa của Meta, Task 179): không dùng để xóa dữ liệu khách theo yêu cầu của khách (việc của module customer, Q30). Không đọc, ghi platform.data_deletion_requests ở gói khác; module thêm phần xóa bằng một Deleter truyền vào NewProcessor ở cmd/crm (chạy lại được, không gọi mạng, chỉ xóa trong tenant của tx). Không lưu hay log mã người dùng gốc: chỉ UserRefHash.

```go
func UserRefHash(id string) string
type Deleter func(ctx context.Context, tx pgx.Tx, userRefHash string) (int64, error)
type Drainer struct {
	Spool  *Spool
	DB     *db.DB
	Logger *slog.Logger
}
func (d *Drainer) Run(ctx context.Context)
type Endpoint struct {
}
func NewEndpoint(o EndpointOptions) (*Endpoint, error)
func (e *Endpoint) Register(mux *http.ServeMux)
type EndpointOptions struct {
	DB    *db.DB
	Spool *Spool

	Source string

	BaseURL string

	Parse  func(signedRequest string) (string, error)
	Logger *slog.Logger
}
type Processor struct {
}
func NewProcessor(deleters ...Deleter) *Processor
func (p *Processor) Handle(ctx context.Context, _ queue.Job, tx pgx.Tx) (json.RawMessage, error)
func (*Processor) Kind() queue.Kind
func (p *Processor) Schedule() schedules.Schedule
type Spool struct {
	Dir string
}
func (s *Spool) Drain(ctx context.Context, d *db.DB) (n int, err error)
func (s *Spool) Write(r request) error
```

## `internal/platform/db` (gói `db`)

Package db mở pool pgx có giới hạn thời gian (ADR-26), chạy transaction có thử lại (db.RetryTx, ADR-46) và chạy migration goose có checksum và luật mở rộng trước (Migrate, LintContract).

**Khi nào không dùng:** Không mở transaction bằng Begin/BeginTx/pgx.BeginFunc ở gói khác (txusage_test chặn) và không tự viết vòng thử lại: dùng RetryTx (hoặc tenancy.WithTenant từ Task 5). Hàm trong RetryTx không gọi mạng, không cộng dồn vào biến bên ngoài vì có thể chạy tới 4 lần. Không gọi Migrate ngoài vai trò migrate; không mở pool thứ hai cho cùng vai trò. NullUUID: không dùng cho cột lấy uuid.Nil làm "không có" (dựng pgtype.UUID với Valid riêng); không chép lại hàm đổi *uuid.UUID → pgtype.UUID trong module.

```go
func Backoff(n int, rnd func() float64) time.Duration
func LintContract(sql string) []string
func Migrate(ctx context.Context, ownerURL string) (applied []string, err error)
func NullUUID(id *uuid.UUID) pgtype.UUID
func Savepoint(ctx context.Context, tx pgx.Tx, fn func(pgx.Tx) error) error
func TransientCode(err error) (code string, ok bool)
func Unavailable(err error) bool
func Unreachable(err error) bool
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

## `internal/platform/events` (gói `events`)

Package events là sự kiện nội bộ (spec 04 mục 5.4): module phát sự kiện bằng Registry.Emit trong transaction nghiệp vụ; sự kiện được ghi vào platform.domain_events (chỉ ghi thêm) và mỗi consumer nghe topic đó nhận một việc event.<consumer> riêng trên hàng đợi chung, nên mỗi consumer có tiến độ, thử lại và thứ tự riêng.

**Khi nào không dùng:** Không gọi Emit ngoài transaction nghiệp vụ (tx phải là WithTenant của Event.TenantID) và không lấy Seq từ bộ đếm riêng hay giờ: Seq là cột version của đối tượng, tăng dưới khóa dòng. Không dùng sự kiện cho việc cần nguyên tử với dữ liệu (cái đó nằm trong cùng transaction) hay cho lệnh ra ngoài (commands). Consumer không gọi mạng, phải chạy lại được; Payload chỉ mang mã bản ghi. Không đổi Name của consumer đã chạy (mất tiến độ, thứ tự).

```go
func ConsumerKind(name string) queue.Kind
type Consumer interface {
	Name() string

	Topics() []string

	Handle(ctx context.Context, e StoredEvent, tx pgx.Tx) error
}
type Event struct {
	TenantID      uuid.UUID
	AggregateType string
	AggregateID   string

	Seq   int64
	Topic string

	Payload any
}
type Registry struct {
}
func NewRegistry() *Registry
func (r *Registry) Emit(ctx context.Context, tx pgx.Tx, q queue.JobQueue, e Event) (uuid.UUID, int, error)
func (r *Registry) JobHandlers() []queue.Handler
func (r *Registry) Register(c Consumer) error
type StoredEvent struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	AggregateType string
	AggregateID   string
	Seq           int64
	Topic         string
	Payload       json.RawMessage
}
```

## `internal/platform/fieldcrypt` (gói `fieldcrypt`)

Package fieldcrypt mã hóa từng trường bí mật lưu trong database (khóa API hãng của M3, token Page của M4, bí mật của M7): AES-256-GCM của thư viện chuẩn, nonce 12 byte ngẫu nhiên, bản mã gắn với đúng dòng bằng AAD (tenant|bảng|cột|dòng) nên chép bản mã sang dòng, cột hay tenant khác thì không mở được.

**Khi nào không dùng:** Không dùng cho mật khẩu hay mã cần so khớp (dùng băm argon2id) và không mã hóa cả dòng, cột tìm kiếm được: chỉ cho bí mật phải đọc lại (khóa API hãng, token Page). Không tự dựng AES hay đọc FIELD_ENC_KEYS ở gói khác: một Keyring dựng ở cmd/crm từ Config.FieldEncKeys. AAD luôn là đúng tenant, bảng, cột, mã dòng của ô; lưu kid cạnh bản mã; NeedsRewrap đúng thì ghi lại bằng Seal. Không log, không đưa bản rõ vào lỗi hay audit.

```go
type AAD struct {
	TenantID      uuid.UUID
	Table, Column string
	RowID         uuid.UUID
}
type Keyring struct {
}
func ParseKeyring(spec logging.Secret) (*Keyring, error)
func (k *Keyring) CurrentKeyID() string
func (Keyring) GoString() string
func (Keyring) LogValue() slog.Value
func (k *Keyring) NeedsRewrap(kid string) bool
func (k *Keyring) Open(ct []byte, kid string, aad AAD) ([]byte, error)
func (k *Keyring) Seal(plain []byte, aad AAD) (ct []byte, kid string, err error)
func (Keyring) String() string
```

## `internal/platform/files` (gói `files`)

Package files là kho tệp dùng chung của CRM (kế hoạch M1, Task M1-41; spec 05 mục 7.3, ADR-36, ADR-45, ADR-46; vùng rủi ro cao): FileStore (S3 qua aws-sdk-go-v2, MemStore cho test), bảng platform.files có RLS, mục đích tệp (Purpose), tải thẳng từ trình duyệt bằng đường dẫn ký sẵn (CreateUpload, CompleteUpload), ghi tệp từ máy chủ (FileStore.Put rồi Register) và đường đọc có kiểm (DownloadURL).

**Khi nào không dùng:** Không gọi aws-sdk-go-v2 hay viết SQL lên platform.files, file_variants ở chỗ khác: dùng FileStore và các hàm của gói. Không gọi FileStore có mạng (Head, Put, Open, CopyIfAbsent, Delete) trong transaction (ADR-46): Put trước rồi Register, Head trước rồi ApplyUpload. Không tự đặt khóa kho từ tên tệp người dùng (dùng TmpKey, ServerKey…), không ký đường dẫn tới tệp khi chưa qua DownloadURL (kiểm ready, tenant, hạn) và quyền của mốc mình. Mục đích mới: một dòng INSERT trong migration của mốc cộng RegisterPurpose trong cmd/crm; ảnh riêng tư (mode private) không đi Register.

```go
func CheckPurposes(ctx context.Context, d *db.DB) error
func CreateUpload(ctx context.Context, tx pgx.Tx, fs FileStore, in UploadInput, actor uuid.UUID) (File, PresignedPut, error)
func DownloadURL(ctx context.Context, tx pgx.Tx, fs FileStore, fileID uuid.UUID, ttl time.Duration, attachmentName string) (string, error)
func EnqueueRegenerate(ctx context.Context, tx pgx.Tx, q queue.JobQueue, preset int) (int, error)
func EnqueueVerify(ctx context.Context, tx pgx.Tx, q queue.JobQueue) (uuid.UUID, error)
func ExportKey(prefix string, tenantID uuid.UUID, name string) string
func MarkDeleted(ctx context.Context, tx pgx.Tx, fileID uuid.UUID, actor uuid.UUID) error
func OriginalKey(sha256Hex string) string
func PrivateVariantKey(tenantID, fileID uuid.UUID, preset, width int, format string) string
func ProcessKind() queue.Kind
func ProcessPrivateKind() queue.Kind
func PublicVariants(ctx context.Context, tx pgx.Tx, fileIDs []uuid.UUID, baseURL string) (map[uuid.UUID][]VariantURL, error)
func ReadContent(ctx context.Context, d *db.DB, fs FileStore, tenantID, fileID uuid.UUID, purpose string, maxBytes int64) ([]byte, error)
func RegenerateKind() queue.Kind
func Register(ctx context.Context, tx pgx.Tx, f NewFile) (uuid.UUID, error)
func RegisterPrivateImage(ctx context.Context, tx pgx.Tx, q queue.JobQueue, in NewPrivateImage) (uuid.UUID, error)
func RegisterPurpose(p Purpose)
func ServerKey(tenantID, fileID uuid.UUID) string
func TmpKey(tenantID, fileID uuid.UUID) string
func VariantKey(sha256Hex string, preset, width int, format string) string
func VerifyKind() queue.Kind
type Bucket string
type File struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	Purpose      string
	Bucket       Bucket
	Key          string
	OriginalName string
	DeclaredType string
	SizeBytes    int64
	Status       string
	RejectReason string
	Width        int
	Height       int
	ExpiresAt    *time.Time
	Deleted      bool
	CreatedAt    time.Time
}
func ApplyUpload(ctx context.Context, tx pgx.Tx, q queue.JobQueue, c UploadCheck) (File, error)
func CompleteUpload(ctx context.Context, d *db.DB, fs FileStore, q queue.JobQueue, tenantID, fileID uuid.UUID) (File, error)
type FileStatus struct {
	Purpose, Status, RejectReason string
	Width, Height                 int
	Deleted                       bool
}
func Status(ctx context.Context, tx pgx.Tx, fileID uuid.UUID) (FileStatus, error)
type FileStore interface {
	PresignPut(ctx context.Context, b Bucket, key string, size int64, contentType string, ttl time.Duration) (PresignedPut, error)

	PresignPutAnySize(ctx context.Context, b Bucket, key string, contentType string, ttl time.Duration) (PresignedPut, error)

	PresignGet(ctx context.Context, b Bucket, key string, ttl time.Duration, attachmentName string) (string, error)

	Head(ctx context.Context, b Bucket, key string) (ObjectInfo, error)

	Open(ctx context.Context, b Bucket, key string, maxBytes int64) (io.ReadCloser, error)

	CopyIfAbsent(ctx context.Context, src Bucket, srcKey string, dst Bucket, dstKey string) error

	Put(ctx context.Context, b Bucket, key string, r io.Reader, size int64, contentType string) error

	Delete(ctx context.Context, b Bucket, key string) error
}
type Imager interface {
	Process(ctx context.Context, req imager.Request) (imager.Result, error)
}
type Info struct {
	File
	Variants []VariantURL
}
func Describe(ctx context.Context, tx pgx.Tx, fileID uuid.UUID, publicBaseURL string) (Info, error)
type LostCursor struct {
	At time.Time
	ID uuid.UUID
}
type LostFile struct {
	ID      uuid.UUID
	Purpose string
	LostAt  time.Time
}
func ListLost(ctx context.Context, tx pgx.Tx, after *LostCursor, limit int) ([]LostFile, error)
type MemStore struct {
}
func NewMemStore() *MemStore
func (m *MemStore) CopyIfAbsent(_ context.Context, src Bucket, srcKey string, dst Bucket, dstKey string) error
func (m *MemStore) Delete(_ context.Context, b Bucket, key string) error
func (m *MemStore) Head(_ context.Context, b Bucket, key string) (ObjectInfo, error)
func (m *MemStore) Keys(b Bucket) []string
func (m *MemStore) Open(_ context.Context, b Bucket, key string, maxBytes int64) (io.ReadCloser, error)
func (m *MemStore) PresignGet(_ context.Context, b Bucket, key string, ttl time.Duration, attachmentName string) (string, error)
func (m *MemStore) PresignPut(_ context.Context, b Bucket, key string, size int64, contentType string, ttl time.Duration) (PresignedPut, error)
func (m *MemStore) PresignPutAnySize(_ context.Context, b Bucket, key string, contentType string, ttl time.Duration) (PresignedPut, error)
func (m *MemStore) Put(_ context.Context, b Bucket, key string, r io.Reader, size int64, contentType string) error
func (m *MemStore) ServeHTTP(w http.ResponseWriter, r *http.Request)
func (m *MemStore) SetBaseURL(u string)
func (m *MemStore) Versions(b Bucket, key string) [][]byte
type NewFile struct {
	ID          uuid.UUID
	Purpose     string
	Key         string
	SHA256      []byte
	Size        int64
	ContentType string
	FileName    string
	ExpiresAt   *time.Time
	CreatedBy   *uuid.UUID
}
type NewPrivateImage struct {
	ID          uuid.UUID
	Purpose     string
	Size        int64
	ContentType string
	FileName    string
	CreatedBy   *uuid.UUID
}
type ObjectInfo struct {
	Size        int64
	ContentType string
	ETag        string
}
type PresignedPut struct {
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}
type Processor struct {
}
func NewProcessHandler(o ProcessorOptions) *Processor
func NewProcessPrivateHandler(o ProcessorOptions) *Processor
func (*Processor) AutoRetryNotSent() bool
func (p *Processor) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error)
func (p *Processor) Kind() queue.Kind
func (*Processor) Lookup(context.Context, queue.Job) (string, error)
func (p *Processor) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error
func (p *Processor) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error
type ProcessorOptions struct {
	DB *db.DB

	Store FileStore

	Signer FileStore
	Imager Imager

	PresetVersion int

	Bus Publisher

	Metrics *metrics.Registry

	RetryMax time.Duration
}
type Publisher interface {
	Publish(ctx context.Context, tx pgx.Tx, s realtime.Signal) error
}
type Purpose struct {
	Key          string
	Bucket       Bucket
	AllowedTypes []string
	MaxBytes     int64

	Image bool

	Upload bool

	Private bool

	Preset int

	AnyType bool

	KeyPrefix string

	MinWidth int
}
func M1Purposes() []Purpose
func PurposeOf(key string) (Purpose, bool)
type Regenerator struct {
}
func NewRegenerateHandler(o ProcessorOptions) *Regenerator
func (*Regenerator) AutoRetryNotSent() bool
func (r *Regenerator) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error)
func (r *Regenerator) Kind() queue.Kind
func (*Regenerator) Lookup(context.Context, queue.Job) (string, error)
func (r *Regenerator) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error
type Registry struct {
}
func NewRegistry(ps ...Purpose) *Registry
func (r *Registry) Check(ctx context.Context, d *db.DB) error
func (r *Registry) Lookup(key string) (Purpose, bool)
func (r *Registry) Register(p Purpose)
type S3Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string

	BucketPrefix string
}
func (c S3Config) LogValue() slog.Value
type S3Store struct {
}
func NewS3Store(cfg S3Config) (*S3Store, error)
func (s *S3Store) CopyIfAbsent(ctx context.Context, src Bucket, srcKey string, dst Bucket, dstKey string) error
func (s *S3Store) Delete(ctx context.Context, b Bucket, key string) error
func (s *S3Store) Head(ctx context.Context, b Bucket, key string) (ObjectInfo, error)
func (s *S3Store) Open(ctx context.Context, b Bucket, key string, maxBytes int64) (io.ReadCloser, error)
func (s *S3Store) PresignGet(ctx context.Context, b Bucket, key string, ttl time.Duration, attachmentName string) (string, error)
func (s *S3Store) PresignPut(ctx context.Context, b Bucket, key string, size int64, contentType string, ttl time.Duration) (PresignedPut, error)
func (s *S3Store) PresignPutAnySize(ctx context.Context, b Bucket, key string, contentType string, ttl time.Duration) (PresignedPut, error)
func (s *S3Store) Put(ctx context.Context, b Bucket, key string, r io.Reader, size int64, contentType string) error
type SignedURL struct {
	URL       string
	ExpiresAt time.Time
}
func OriginalURL(ctx context.Context, tx pgx.Tx, fs FileStore, fileID uuid.UUID, ttl time.Duration, baseName string) (SignedURL, error)
type UploadCheck struct {
}
func InspectUpload(ctx context.Context, d *db.DB, fs FileStore, tenantID, fileID uuid.UUID) (UploadCheck, error)
type UploadInput struct {
	TenantID    uuid.UUID
	Purpose     string
	FileName    string
	ContentType string
	SizeBytes   int64
}
type VariantSpec struct {
	Width   int
	Format  string
	Quality int
}
func Preset(version int) ([]VariantSpec, bool)
type VariantURL struct {
	Preset int
	Width  int
	Format string
	URL    string
}
func SelectPreset(vs []VariantURL, want int) []VariantURL
type Verifier struct {
}
func NewVerifyHandler(o ProcessorOptions) *Verifier
func (*Verifier) AutoRetryNotSent() bool
func (v *Verifier) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error)
func (*Verifier) Kind() queue.Kind
func (*Verifier) Lookup(context.Context, queue.Job) (string, error)
func (v *Verifier) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error
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
func Err(err error) slog.Attr
func ErrorText(err error) string
func NewLogger(logDir, role string, stdout io.Writer) (*slog.Logger, io.Closer, error)
func Redact(v any, maxDepth int) any
func ScrubText(s string) string
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

**Khi nào không dùng:** Không mở endpoint /metrics hay thêm thư viện chỉ số (ADR C6); không tạo Registry thứ hai trong một tiến trình (cmd/crm tạo một cái và truyền xuống). Không đặt đường dẫn thật, tham số, SĐT hay mã khách vào tên hoặc nhãn chỉ số (nhãn route dùng mẫu của ServeMux). Không dùng Flusher cho dữ liệu nghiệp vụ hay số phải chính xác tuyệt đối: lỗi commit không rõ kết quả có thể ghi một lô hai lần. DailyP95 (Task 266, M7) chỉ để báo cáo theo ngày (T2, theo dõi sau mở bán): không dùng để quyết định nghiệp vụ hay cảnh báo theo phút, không tự cộng mảng hist ở module (gọi DailyP95 hoặc QuantileFromHist).

```go
func DiskUsedPct(path string) (float64, error)
func HeartbeatAge(ctx context.Context, d *db.DB, process string) (time.Duration, error)
func Middleware(reg *Registry, next http.Handler) http.Handler
func ProcessName(role config.Role) string
func QuantileFromHist(hist []int, pct int) float64
type Flusher struct {
	DB      *db.DB
	Reg     *Registry
	Process string
	Role    config.Role
	Pinger  *Pinger
	Log     *slog.Logger

	DiskPaths map[string]string
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

Package observability phục vụ các điểm kiểm sức khỏe của tiến trình (/health/live, /health/ready, báo cáo /health/deep), cảnh báo vận hành qua Telegram (Alerter, chống lặp theo platform.alert_events), luật cảnh báo trên hàng đợi, chỉ số theo phút, nhịp sống, sao lưu, lỗi ứng dụng và thống kê PostgreSQL (CheckRules), tin tổng kết hằng ngày (SendDailyDigest) (Task 19); gom lỗi ứng dụng và tóm tắt sức khỏe (Task 20).

**Khi nào không dùng:** Không đặt kiểm tra nghiệp vụ hay dữ liệu khách vào /health/*. Không gọi Notify trong RetryTx hay handler transactional của hàng đợi (gọi mạng): gửi cảnh báo từ lệnh (commands.Handler) như alertjobs.NewAlertsCommand (gói con: observability không được nhập commands, queue vì queue nhập observability). Văn bản cảnh báo chỉ mang tên luật, số lượng, kind, mã bản ghi: không đọc payload, last_error. Không dùng Alerter để báo cho khách (C7). TenantOpsSummary (Task 271, M7) là đường duy nhất module đọc số việc nền in_doubt, dead và cảnh báo gấp: module không đọc bảng platform.jobs, platform.alert_events trực tiếp; không dùng nó cho màn sức khỏe hệ thống (đã có /health và màn nền tảng).

```go
func AlertSentWithin(ctx context.Context, d *db.DB, key string, window time.Duration) (bool, error)
func CheckRules(ctx context.Context, d *db.DB, a Alerter, o RuleOptions) ([]string, error)
func DigestDue(now time.Time) bool
func Fingerprint(err error, frames []runtime.Frame) string
func PanicFrames() []runtime.Frame
func RecordError(ctx context.Context, d *db.DB, err error, c ErrorContext) (fingerprint string, isNew bool)
func RecoverPanics(next http.Handler, onPanic func(ctx context.Context, w http.ResponseWriter, r *http.Request, pe *PanicError, wrote bool)) http.Handler
func ScrubMessage(m string) string
func ScrubPanics(next http.Handler) http.Handler
func SendDailyDigest(ctx context.Context, d *db.DB, a Alerter, now time.Time) error
func StackFrames(skip int) []runtime.Frame
type AlertSummary struct {
	Key, Severity string
	SentAt        time.Time
}
type Alerter interface {
	Notify(ctx context.Context, key, severity, text string) error
}
func NewTelegramAlerter(d *db.DB, cfg config.Config, client *http.Client, logger *slog.Logger) Alerter
type BackupHealth struct {
	LastBaseAgeH, LastDrillAgeH *int64
	LastDrillOk                 *bool
}
type DeepHealthReport struct {
	OK                  bool
	WorkerHeartbeatAgeS *int64
	IngestHeartbeatAgeS *int64
	OldestPendingAgeS   int64
	Dead, InDoubt       int64
	SpoolFiles          int64
	LastWalArchivedAgeS *int64
	LastBackupAgeH      *int64

	LastFileSyncAgeM *int64

	Valkey ValkeyHealth
}
func DeepHealth(ctx context.Context, d *db.DB, vk Valkey) (DeepHealthReport, error)
type ErrorContext struct {
	Role      string
	RequestID string
	JobID     uuid.UUID

	Frames  []runtime.Frame
	Metrics *metrics.Registry
	Logger  *slog.Logger
}
type ErrorSummary struct {
	Fingerprint, Name, Role, Message string
	Count                            int64
	FirstSeenAt, LastSeenAt          time.Time
}
type Health struct {
}
func NewHealth(db Pinger) *Health
func (h *Health) Check(ctx context.Context) error
func (h *Health) Register(mux *http.ServeMux)
type HealthSummary struct {
	Processes []ProcessHealth
	Queue     QueueHealth
	Ingest    IngestHealth
	Backup    BackupHealth
	Errors    []ErrorSummary
	Alerts    []AlertSummary
}
func Summary(ctx context.Context, d *db.DB, now time.Time) (HealthSummary, error)
type IngestHealth struct {
	SpoolFiles int64
}
type PanicError struct {
	Value  any
	Frames []runtime.Frame
}
func NewPanicError(v any) *PanicError
func (p *PanicError) Error() string
func (p *PanicError) Unwrap() error
type Pinger interface {
	Ping(ctx context.Context) error
}
type ProcessHealth struct {
	Role              string
	LastHeartbeatAgeS int64
}
type QueueHealth struct {
	OldestPendingAgeS, Pending, Dead, InDoubt int64
}
type RuleHit struct {
	Key, Severity, Text string
}
func ErrorRules(ctx context.Context, d *db.DB, now time.Time) ([]RuleHit, error)
type RuleOptions struct {
	Now               time.Time
	BackupExpected    bool
	FileSyncExpected  bool
	DBDiskBudgetBytes int64
	ConnBudget        int
}
type TelegramRetryError struct{ After time.Duration }
func (e *TelegramRetryError) Error() string
type Valkey struct{ Cache, Gate *cache.Conn }
type ValkeyHealth struct{ Cache, Gate cache.Health }
```

## `internal/platform/queue` (gói `queue`)

Package queue là lõi hàng đợi việc nền trên bảng platform.jobs (spec 04 mục 5.1): ghi việc trong transaction của bên gọi (JobQueue), worker nhận việc bằng FOR UPDATE SKIP LOCKED, thử lại theo lịch, thứ tự theo đối tượng (OrderKey), hết hạn, in_doubt khi giữ chỗ hết hạn, tắt êm, dấu vết đi qua hàng đợi.

**Khi nào không dùng:** Không viết SQL lên platform.jobs ở gói khác (ngoại lệ duy nhất: việc dọn của schedules xóa việc done/expired quá 30 ngày): ghi việc bằng JobQueue.Enqueue trong transaction của dữ liệu, xử lý bằng Handler. Handler transactional không gọi mạng, không gửi gì ra ngoài (lệnh ra ngoài là commands, Task 12) và phải chạy lại được (có thể bị gọi lại). Không đặt nội dung hay dữ liệu cá nhân vào Payload, DedupeKey, OrderKey hay chuỗi lỗi: chỉ mã bản ghi. Không dùng hàng đợi cho việc cần nguyên tử với dữ liệu (cái đó nằm trong cùng một transaction), không tạo Worker thứ hai trong một tiến trình.

```go
func AttentionStatuses() []string
func ErrorText(err error) string
func NextDelay(attempt int, rnd func() float64) time.Duration
type CommandRunner interface {
	Kinds() []Kind

	TerminalHook(kind string) TerminalHook

	Run(ctx context.Context, j Job, workerID string) (status string, err error)
}
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
type JobSummary struct {
	ID        uuid.UUID
	Kind      string
	Status    string
	Attempts  int
	LastError string
	CreatedAt time.Time
}
func AbandonJob(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID) (before, after JobSummary, err error)
func ListAttention(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, statuses []string, limit int) ([]JobSummary, error)
func RetryJob(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID) (before, after JobSummary, err error)
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

	OrderMode string

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

	Commands CommandRunner

	Role string
}
```

## `internal/platform/realtime` (gói `realtime`)

Package realtime phát báo hiệu "đối tượng X vừa đổi" tới màn hình nhân viên qua SSE (spec 04 mục 6, ADR-05, ADR-17).

**Khi nào không dùng:** Không gửi dữ liệu nghiệp vụ hay dữ liệu cá nhân qua báo hiệu: Signal chỉ mang tenant, chủ đề, quyền, phạm vi và mã đối tượng; màn hình tải lại bằng API. Không dùng làm hàng đợi hay sự kiện nội bộ (báo hiệu được phép mất: dùng events.Emit, commands). Không phát ngoài transaction nghiệp vụ (Publish nhận tx để chỉ phát khi commit) và không tự gọi pg_notify, LISTEN ở gói khác. Không tạo PGHub thứ hai trong một tiến trình.

```go
func ServeSSE(w http.ResponseWriter, r *http.Request, bus Bus, u identity.AuthUser, o SSEOptions)
func WarehouseScope(warehouseID string) *identity.Scope
type Bus interface {
	Publish(ctx context.Context, tx pgx.Tx, s Signal) error

	Subscribe(u identity.AuthUser, send func(Frame)) (cancel func())

	SubscribeSystem(topicPrefix string, fn func(Signal)) (cancel func())

	Status() (listening bool, clients int)
}
type Entity struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type Frame struct {
	Event string
	Data  json.RawMessage
}
type PGHub struct {
}
func NewPGHub(o PGHubOptions) *PGHub
func (h *PGHub) Publish(ctx context.Context, tx pgx.Tx, s Signal) error
func (h *PGHub) Run(ctx context.Context)
func (h *PGHub) Status() (listening bool, clients int)
func (h *PGHub) Subscribe(u identity.AuthUser, send func(Frame)) (cancel func())
func (h *PGHub) SubscribeSystem(topicPrefix string, fn func(Signal)) (cancel func())
type PGHubOptions struct {
	URL     string
	Metrics *metrics.Registry
	Logger  *slog.Logger

	MinBackoff, MaxBackoff time.Duration

	CheckEvery time.Duration

	PingTimeout time.Duration
}
type SSEOptions struct {
	Batch time.Duration

	Heartbeat time.Duration

	MaxPending int

	Stop <-chan struct{}
}
type Signal struct {
	TenantID uuid.UUID
	Topic    string
	Requires identity.Permission
	Scope    *identity.Scope
	Entity   Entity
	System   bool
}
```

## `internal/platform/schedules` (gói `schedules`)

Package schedules là việc định kỳ (spec 04 mục 5.1) và dọn dữ liệu: lịch nằm trong platform.schedules; tới kỳ, Ticker của vai trò worker ghi một việc (lịch toàn hệ thống) hoặc mỗi tenant một việc (lịch theo tenant) lên hàng đợi chung, với DedupeKey theo kỳ nên nhiều worker không tạo trùng.

**Khi nào không dùng:** Không tự viết vòng lặp time.Ticker để chạy việc định kỳ trong tiến trình: Define một lịch và đăng ký handler của Kind ở worker. Không dùng lịch cho việc cần chạy đúng giờ tuyệt đối (Ticker hỏi mỗi 5 s, lịch trễ chỉ chạy kỳ mới nhất). Hàm dọn của bảng có RLS đăng ký bằng RegisterTenantCleanup (RegisterCleanup chạy ngoài ngữ cảnh tenant nên thấy 0 dòng); câu xóa theo lô, FOR UPDATE SKIP LOCKED.

```go
type Cleanup struct {
}
func NewCleanup(o CleanupOptions) *Cleanup
func (c *Cleanup) Define(r *Registry) error
func (c *Cleanup) Handlers() []queue.Handler
func (c *Cleanup) RegisterCleanup(fn CleanupFunc)
func (c *Cleanup) RegisterTenantCleanup(fn CleanupFunc)
type CleanupFunc func(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error)
type CleanupOptions struct {
	JobRetention time.Duration
}
type Registry struct {
}
func NewRegistry() *Registry
func (r *Registry) Define(s Schedule) error
type Schedule struct {
	Name string

	Every time.Duration

	PerTenant bool

	Kind queue.Kind
}
type Ticker struct {
}
func NewTicker(d *db.DB, q queue.JobQueue, r *Registry, o TickerOptions) *Ticker
func (t *Ticker) Run(ctx context.Context)
func (t *Ticker) Tick(ctx context.Context, now time.Time) (int, error)
type TickerOptions struct {
	Interval time.Duration
	Logger   *slog.Logger
}
```

## `internal/platform/selftest` (gói `selftest`)

Package selftest là nguồn webhook thử "selftest" (/hooks/selftest) dùng để kiểm cả đường vào khi chưa có kênh thật: ký bằng HMAC-SHA256 với SELFTEST_WEBHOOK_SECRET, có bước GET xác minh, tách gói {"events":[…]}.

**Khi nào không dùng:** Chỉ để thử đường vào khi chưa có kênh thật: không dùng làm mẫu xác thực cho nguồn thật (mỗi kênh tự hiện thực webhooks.Source theo cách ký của nhà cung cấp) và không định tuyến sự kiện ping vào nghiệp vụ.

```go
func CommandKind() queue.Kind
type Command struct {
}
func NewCommand(ext External) *Command
func (*Command) AutoRetryNotSent() bool
func (c *Command) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error)
func (*Command) Kind() queue.Kind
func (c *Command) Lookup(_ context.Context, j queue.Job) (string, error)
type External interface {
	Send(ctx context.Context, ref string) error
	Has(ref string) bool
}
type Inbound struct {
}
func NewInbound(bus Publisher) *Inbound
func (i *Inbound) Handle(ctx context.Context, e webhooks.Inbound, tx pgx.Tx) error
func (*Inbound) Source() string
type Loopback struct{}
func (Loopback) Has(string) bool
func (Loopback) Send(context.Context, string) error
type Publisher interface {
	Publish(ctx context.Context, tx pgx.Tx, s realtime.Signal) error
}
type Source struct {
}
func NewSource(secret string) *Source
func (s *Source) Challenge(q url.Values) (string, bool)
func (*Source) Key() string
func (*Source) Split(raw []byte) (webhooks.SplitResult, error)
func (s *Source) VerifySignature(raw []byte, h http.Header) bool
```

## `internal/platform/settings` (gói `settings`)

Package settings giữ cài đặt theo tenant dạng khóa → object JSON, có version (bảng platform.settings, kế hoạch M3 Task M3-04; Q-35, B17; spec 05 mục 5).

**Khi nào không dùng:** Không dùng cho cài đặt cần cột riêng, CHECK hay khóa ngoại do database kiểm (bảng riêng của module, như ord.order_settings), cho dữ liệu nghiệp vụ, danh sách lớn hay bí mật (khóa API mã hóa bằng fieldcrypt trong bảng của module). Không gọi ngoài transaction có ngữ cảnh tenant (WithTenant, WriteTx). Không để module khác đọc khóa của module mình qua Get: đọc qua hàm của module chủ (vd. fulfillment.LoadSettings) để có mặc định và luật kiểm một nơi. PUT cài đặt sửa platform.settings một mình (ADR-46 nhóm 12c).

```go
func Get[T any](ctx context.Context, tx pgx.Tx, key string, def T) (T, int64, error)
func Put(ctx context.Context, tx pgx.Tx, key string, version int64, v any, actor string) error
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
func PutRoute(ctx context.Context, tx pgx.Tx, kind, key string) error
func Resolve(ctx context.Context, d *db.DB, kind, externalKey string) (uuid.UUID, bool, error)
func ResolveTx(ctx context.Context, tx pgx.Tx, kind, externalKey string) (uuid.UUID, bool, error)
func SeedTenant(ctx context.Context, owner *db.DB, name string) (uuid.UUID, error)
func SetTenant(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) error
func WithTenant(ctx context.Context, d *db.DB, tenantID uuid.UUID, fn func(pgx.Tx) error, o ...db.TxOptions) error
```

## `internal/platform/textnorm` (gói `textnorm`)

Package textnorm chuẩn hóa chữ tiếng Việt cho cột tìm kiếm (kế hoạch M1, Task M1-27): bỏ dấu, chữ thường, gộp khoảng trắng, để "dam lua" tìm được "Đầm Lụa".

**Khi nào không dùng:** Không dùng để hiển thị, lưu chữ gốc hay so khớp định danh (SKU, email, SĐT có chuẩn hóa riêng): Fold chỉ cho cột tìm kiếm như catalog.products.name_search, và cả cột lẫn chuỗi tìm phải qua cùng Fold. Không thêm thư viện bỏ dấu (x/text) khi bảng ánh xạ đủ.

```go
func Fold(s string) string
func Slug(s string, maxLen int) string
```

## `internal/platform/urlerr` (gói `urlerr`)

Package urlerr bỏ URL khỏi lỗi của net/http trước khi bọc hay ghi log: *url.Error chép nguyên URL của yêu cầu, mà URL có thể chứa token (Telegram bot) hay khóa của dịch vụ nhận nhịp sống.

**Khi nào không dùng:** Không thay cho bộ che dữ liệu cá nhân của logging: Strip chỉ bóc *url.Error (URL chứa token, khóa); địa chỉ máy trong *net.OpError, *net.DNSError không được bóc, nơi cần thì tự xử lý thêm (như metrics stripURL). Không thêm phụ thuộc ngoài thư viện chuẩn vào gói này (scripts/external-probe nhập nó, test kiến trúc TestExternalProbeStdlibOnly).

```go
func Strip(err error) error
```

## `internal/platform/webhooks` (gói `webhooks`)

Package webhooks là đường vào webhook ở vai trò ingest (spec 04 mục 5.2, 11): kiểm chữ ký trên thân gốc, lưu nguyên văn vào platform.webhook_deliveries cùng một việc tách gói trong một transaction rồi trả 200; database không ghi được thì ghi tệp tạm (Spool) và nạp lại sau (Drainer); chặn địa chỉ gửi sai chữ ký dồn dập (Lockout).

**Khi nào không dùng:** Không nhận webhook ở vai trò khác ingest hay bằng handler tự viết: thêm một Source (VerifySignature trên thân gốc, Challenge, Split; RoutedSource khi nguồn cần khóa tuyến /hooks/<nguồn>/<routeKey>) vào IngestOptions.Sources. Sự kiện vào không đến từ webhook (đọc chủ động, hỏi bù) ghi bằng EnqueueInbound, không tự Enqueue InboundKind. Không đọc, sửa platform.webhook_deliveries ở gói khác (bảng không RLS, không tenant: xử lý nghiệp vụ đi qua việc webhook.split chỉ mang deliveryId). Không log hay đưa thân, header vào chỉ số, dấu vết; không xóa tệp tạm bằng tay khi database còn lỗi.

```go
func CleanupDeliveries(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error)
func ClientIP(r *http.Request) string
func EnqueueInbound(ctx context.Context, tx pgx.Tx, q queue.JobQueue, tenantID uuid.UUID, source string, e InboundEvent,
	receivedAt time.Time, via string,
) (created bool, err error)
func InboundJobHandlers(sources []Source, hs []InboundHandler) ([]queue.Handler, error)
func InboundKind(source string) queue.Kind
func Save(ctx context.Context, d *db.DB, e Entry) (created bool, err error)
type Drainer struct {
	Spool *Spool
	DB    *db.DB

	Interval time.Duration

	Metrics *metrics.Registry
	Logger  *slog.Logger
}
func (d *Drainer) Run(ctx context.Context)
type Entry struct {
	Source string `json:"source"`

	Headers    json.RawMessage `json:"headers"`
	Body       []byte          `json:"bodyBase64"`
	ReceivedAt time.Time       `json:"receivedAt"`
	RouteKey   string          `json:"routeKey,omitempty"`
}
type Inbound struct {
	Type       string
	OccurredAt time.Time

	Data     json.RawMessage
	JobID    uuid.UUID
	TenantID uuid.UUID

	ReceivedAt time.Time

	Via string

	Attempts    int
	LastAttempt bool
}
type InboundEvent struct {
	Type string

	DedupeKey string

	Route Route

	OrderKey   string
	OccurredAt time.Time
	Payload    json.RawMessage
}
type InboundHandler interface {
	Source() string

	Handle(ctx context.Context, e Inbound, tx pgx.Tx) error
}
type Ingest struct {
}
func NewIngest(o IngestOptions) (*Ingest, error)
func (in *Ingest) Register(mux *http.ServeMux)
type IngestOptions struct {
	DB      *db.DB
	Sources []Source
	Spool   *Spool
	Lockout *Lockout

	MaxInFlight int

	StaleSpoolAge time.Duration

	Metrics *metrics.Registry
	Logger  *slog.Logger

	Role string
}
type Lockout struct {
}
func NewLockout(o LockoutOptions) *Lockout
func (l *Lockout) Blocked(ip string) bool
func (l *Lockout) NoteFailure(ip string)
type LockoutOptions struct {
	Threshold int
	Window    time.Duration
	Block     time.Duration
	Now       func() time.Time
}
type Route struct {
	Kind, ExternalKey string
}
type RoutedSource interface {
	Source

	SplitRouted(raw []byte, routeKey string) (SplitResult, error)
}
type Source interface {
	Key() string

	Challenge(q url.Values) (answer string, ok bool)

	VerifySignature(raw []byte, h http.Header) bool

	Split(raw []byte) (SplitResult, error)
}
type SplitHandler struct {
}
func NewSplitHandler(o SplitOptions) (*SplitHandler, error)
func (h *SplitHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error)
func (*SplitHandler) Kind() queue.Kind
type SplitOptions struct {
	Sources []Source

	Metrics *metrics.Registry
}
type SplitResult struct {
	Events  []InboundEvent
	Skipped map[string]int
}
type Spool struct {
	Dir string
}
func (s *Spool) Drain(ctx context.Context, d *db.DB) (n int, err error)
func (s *Spool) List() ([]string, error)
func (s *Spool) Stats() (files int, oldestAge time.Duration, err error)
func (s *Spool) Write(e Entry) (string, error)
```

## `internal/platform/xlsx` (gói `xlsx`)

Package xlsx đọc và ghi tệp Excel (.xlsx) cho mọi mốc của CRM (kế hoạch M1, Task M1-38, C22): nhập sản phẩm (M1), nhập khách (M2), bảng kê vận chuyển (M3), xuất báo cáo (M6), mở sổ (M7).

**Khi nào không dùng:** Không nhập excelize ở gói khác (test kiến trúc luật h) và không tự ghi ô bằng excelize bỏ qua Sanitize: mọi tệp xuất đi qua Write (tệp nhỏ, mọi ô là chữ: mẫu nhập, bảng kê) hoặc Writer (bộ ghi theo luồng của M6: báo cáo nhiều dòng, ô số, tiền, ngày; cũng gọi Sanitize). Không dùng Writer khi cần nhiều trang hay danh sách chọn (dùng Write). Không đọc tệp người dùng bằng excelize.OpenReader trần: dùng Read với Limits. Không đặt kiểm nghiệp vụ (giá, SKU, SĐT) ở đây: module dùng tự kiểm Row. Không dùng cho CSV. Không log, không đưa nội dung ô vào lỗi (tệp khách có dữ liệu cá nhân).

```go
func Read(r io.ReaderAt, size int64, lim Limits, header []string) (rows []Row, cellErrs []CellError, err error)
func Sanitize(s string) string
func Write(w io.Writer, sheets []Sheet) error
type Cell struct {
	Kind Kind
	S    string
	I    int64
	T    time.Time
}
func Date(t time.Time) Cell
func Empty() Cell
func Int(v int64) Cell
func Money(v int64) Cell
func Str(s string) Cell
type CellError struct {
	Row          int
	Column, Code string
}
type Column struct {
	Title string
	Width int
	Kind  Kind
}
type Kind int
type Limits struct {
	MaxBytes     int64
	MaxRows      int
	MaxUnzipped  int64
	MaxCellChars int
}
func DefaultLimits() Limits
type RawRow struct {
	Number   int
	Cells    []string
	Formulas []int
}
func ReadRows(r io.ReaderAt, size int64, lim Limits) (rows []RawRow, err error)
type Row struct {
	Number int
	Cells  map[string]string
}
type Sheet struct {
	Name   string
	Header []string
	Rows   [][]any
	Lists  map[string][]string
}
type Writer struct {
}
func NewWriter(w io.Writer, sheet string, cols []Column) (*Writer, error)
func (w *Writer) Close() error
func (w *Writer) Note(text string) error
func (w *Writer) Row(cells ...Cell) error
func (w *Writer) Rows() int
```

## `console/src/shared` (màn hình, TypeScript)

Export của `console/src/shared/index.ts`.

**Khi nào không dùng:** Không fetch thẳng tới /api/v1 ở màn hình (eslint chặn): dữ liệu lấy qua client sinh console/src/api; chỉ createRealtimeClient đọc /api/v1/stream. Không coi báo hiệu changed là dữ liệu: nhận báo hiệu thì tải lại đúng đối tượng bằng API; onResync tải lại mọi danh sách đang mở. Không tạo hai createRealtimeClient trong một tab (mỗi tab một kết nối, spec 04 mục 6). Không tự viết băng báo mất kết nối, phiên hết hạn, API tạm ngừng: dùng Banner (câu chữ spec 04 mục 6). Không gọi hàm ghi của client sinh thẳng từ màn: qua useWriteAction (khóa chống trùng, gửi lại cùng khóa); không tự viết vòng thử lại hay sinh Idempotency-Key ở màn. Không hiện mã lỗi máy hay chữ từ máy chủ cho nhân viên: errorMessage; mã mới thì thêm vào x-error-codes của openapi.yaml và ERROR_MESSAGES. Khóa truy vấn luôn bắt đầu bằng chủ đề realtime (queryKey), không tạo QueryClient thứ hai. Không thêm route hay mục menu ngoài quy ước màn tự đăng ký: mỗi màn là thư mục src/screens/<tên>/ có route.tsx xuất screens (src/app/loadScreens.ts); không viết danh sách màn tay, không sửa tệp chung để thêm màn. Không viết form lên đơn thứ hai (M4 cạnh hội thoại, sửa đơn): dùng OrderForm; tiền hiện trước của đơn chỉ tính bằng pricing.ts cạnh OrderForm (cùng bộ vector với máy chủ), không tự cộng trừ ở màn. Báo hiệu theo đối tượng: truy vấn của một đối tượng khai meta realtimeEntity (chỉ tải lại khi đúng đối tượng); danh sách tự tải lại từng dòng thì khai realtimeManual và nghe subscribeChanged — không dùng subscribeChanged để lấy dữ liệu từ báo hiệu, không bỏ meta mà tự lọc báo hiệu trong màn.

```ts
export { createRealtimeClient } from "../realtime/client"; // Lớp realtime: báo hiệu "đối tượng vừa đổi" qua SSE, tự canh im lặng, nối lại, nhận ra phiên hết hạn.
export type { ChangedEvent, RealtimeClient, RealtimeOptions, RealtimeState } from "../realtime/client";
export { watchApiLive } from "../shell/apiHealth"; // Canh API bằng /health/live; báo khi đổi giữa sống và tạm ngừng.
export type { ApiLiveOptions } from "../shell/apiHealth";
export { Banner, TEXT_API_DOWN, TEXT_RECONNECTING, TEXT_SESSION_EXPIRED } from "../shell/Banner"; // Băng báo của khung theo câu chữ spec 04 mục 6.
export type { BannerProps } from "../shell/Banner";
export { RealtimeStateContext, useRealtimeState } from "../shell/realtimeState"; // Trạng thái lớp realtime của tab cho màn cần nói riêng về nó (băng có nút "Tải lại ngay" của Inbox).
export { TEXT_BUSY, useWriteAction } from "../app/idempotency"; // Gửi yêu cầu ghi với khóa chống trùng; tự gửi lại cùng khóa khi máy chủ bận (503 retry_later, 409 request_in_progress, mạng).
export type { WriteAction, WriteActionOptions, WriteResult } from "../app/idempotency";
export { ApiError, ERROR_MESSAGES, NetworkError, TEXT_NETWORK, TEXT_UNKNOWN_ERROR, errorMessage, unwrap } from "../app/errors"; // Lỗi API: đổi kết quả client sinh thành dữ liệu hoặc ApiError/NetworkError; câu tiếng Việt cho mọi mã lỗi của hợp đồng.
export type { ClientResult } from "../app/errors";
export { TOPICS, createQueryClient, onRealtimeChanged, onRealtimeResync, queryKey, shouldRetry, subscribeChanged } from "../app/queryClient"; // Lớp lấy dữ liệu: khóa truy vấn theo chủ đề realtime, nối báo hiệu changed/resync vào TanStack Query.
export type { RealtimeMeta, Topic } from "../app/queryClient";
export { VN_TIME_ZONE, shortTime } from "../app/time"; // Giờ hiện trên màn hình theo múi Việt Nam: "HH:mm dd/MM".
export { NAV_GROUPS, allowedScreens, hasPermission, navSections } from "../app/nav"; // Menu và định tuyến: mỗi màn tự đăng ký bằng src/screens/<tên>/route.tsx (src/app/loadScreens.ts); menu chỉ hiện màn có quyền.
export type { NavGroup, NavSection, Screen } from "../app/nav";
export { Layout } from "../shell/Layout"; // Khung điều hướng theo mockup M1: menu trái, thanh trên 56 px, menu trượt trên điện thoại.
export type { LayoutProps } from "../shell/Layout";
export { NavCount } from "../shell/NavCount"; // Số đếm cạnh mục menu (HV02 M3): màn khai `badge: <NavCount n={…} late={…} />` trong route.tsx.
export type { NavCountProps } from "../shell/NavCount";
export { OrderForm } from "../screens/len-don/OrderForm"; // Form lên đơn nhanh (màn /don/moi, Task 84): dùng lại nguyên khối ở M4 cạnh hội thoại; gửi đơn qua useWriteAction.
export type { OrderFormProps } from "../screens/len-don/OrderForm";
```
