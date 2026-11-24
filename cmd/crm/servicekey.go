package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// serviceKeyCommand là lệnh `crm service-key create|revoke …` (kế hoạch M5 Task 189): tạo, thu hồi khóa dịch vụ của
// website trên dev và staging khi chưa có màn hình. Production dùng màn "Khóa cho website" (khóa hiện một lần).
const serviceKeyCommand = "service-key"

var (
	errServiceKeyProduction = errors.New("lệnh service-key chỉ chạy trên dev, test, staging (production dùng màn hình)")
	errServiceKeyUsage      = errors.New("cách gọi: `crm service-key create --staff-email <email> --name <tên> " +
		"--perm web.catalog.read,… [--cidr 10.0.0.0/8,…]` hoặc `crm service-key revoke --id <mã khóa>`")
	errServiceKeyUnknown = errors.New("không có khóa dịch vụ với mã này ở tenant nào")
)

// webKeys là phần của đường khóa dịch vụ mà vai trò api nối vào httpapi.Deps: bộ kiểm khóa (goroutine ghi
// last_used_at có chủ), giới hạn tần suất theo khóa, giới hạn đơn web theo SĐT (Task 194) và giới hạn nhập mã giảm giá
// sai (Task 482) trên Valkey gate. stop
// dừng goroutine, chờ nó xong, đóng Valkey.
type webKeys struct {
	keys         *httpapi.KeyVerifier
	limiter      httpapi.WebRateLimiter
	orderLimiter httpapi.WebOrderLimiter
	couponGuard  *httpapi.CouponGuard
	stop         func()
}

// newWebKeys đọc RATE_LIMITS_JSON (cache.ParseLimits; sai → không khởi động), quay số Valkey gate (URL trống → Conn nil,
// bộ đếm trong tiến trình với FallbackMax, ghi cảnh báo một lần), dựng RateLimiter và bộ kiểm khóa nghe báo hiệu thu
// hồi qua hub.
func newWebKeys(ctx context.Context, cfg config.Config, d *db.DB, hub *realtime.PGHub, reg *metrics.Registry,
	logger *slog.Logger,
) (webKeys, error) {
	limits, err := cache.ParseLimits(cfg.RateLimitsJSON)
	if err != nil {
		return webKeys{}, fmt.Errorf("đọc giới hạn tần suất: %w", err)
	}
	var gate *cache.Conn
	if cfg.ValkeyGateURL == "" {
		logger.Warn("VALKEY_GATE_URL trống: giới hạn tần suất của website đếm trong tiến trình (ngưỡng dự phòng)")
	} else if gate, err = cache.Dial(ctx, cfg.ValkeyGateURL, cache.DialOptions{Name: "gate", Metrics: reg, Tracing: true}); err != nil {
		return webKeys{}, fmt.Errorf("nối Valkey gate: %w", err)
	}
	var bus realtime.Bus
	if hub != nil {
		bus = hub
	}
	v := httpapi.NewServiceKeyVerifier(d, bus, httpapi.ServiceKeyOptions{Logger: logger})
	rctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() { v.Run(rctx); close(done) }()
	rl := cache.NewRateLimiter(gate, nil, reg)
	return webKeys{
		keys:         v,
		limiter:      httpapi.NewWebKeyLimiter(rl, limits.WebKey),
		orderLimiter: httpapi.NewWebOrderLimiter(rl, limits.WebOrderCustomer, []byte(cfg.CacheKeySecret)),
		couponGuard: httpapi.NewCouponGuard(rl, limits.CouponGuessCustomer, limits.CouponGuessIP,
			[]byte(cfg.CacheKeySecret)),
		stop: func() {
			cancel()
			<-done
			gate.Close()
		},
	}, nil
}

// isServiceKeyCommand: args là lệnh `crm service-key <việc> …`.
func isServiceKeyCommand(args []string) bool { return len(args) >= 2 && args[0] == serviceKeyCommand }

// serviceKeyExit in lỗi của lệnh service-key ra stderr và trả mã thoát (sai cách gọi, production: exitConfig).
func serviceKeyExit(err error, stderr io.Writer) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errServiceKeyUsage), errors.Is(err, errServiceKeyProduction):
		_, _ = fmt.Fprintln(stderr, serviceKeyCommand+":", err)
		return exitConfig
	}
	_, _ = fmt.Fprintln(stderr, serviceKeyCommand+":", err)
	return exitFail
}

// runServiceKey chạy `crm service-key create|revoke` bằng DATABASE_URL (crm_app, RLS vẫn áp). create in khóa đầy đủ
// ra stdout đúng một lần (không ghi log); revoke tìm tenant của khóa (thử lần lượt), thu hồi với người làm system và
// phát báo hiệu thu hồi.
func runServiceKey(ctx context.Context, cfg config.Config, args []string, stdout io.Writer, logger *slog.Logger) error {
	if cfg.Env == "production" {
		return errServiceKeyProduction
	}
	if len(args) == 0 || (args[0] != "create" && args[0] != "revoke") {
		return errServiceKeyUsage
	}
	fs := flag.NewFlagSet(serviceKeyCommand+" "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	email := fs.String("staff-email", "", "email của nhân viên tạo khóa")
	name := fs.String("name", "", "tên khóa")
	perm := fs.String("perm", "", "quyền web.*, cách nhau bằng dấu phẩy")
	cidr := fs.String("cidr", "", "dải IP được phép, cách nhau bằng dấu phẩy (trống: mọi IP)")
	id := fs.String("id", "", "mã khóa cần thu hồi")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return errServiceKeyUsage
	}
	d, err := openCommandDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer d.Pool.Close()
	if args[0] == "revoke" {
		keyID, err := uuid.Parse(*id)
		if err != nil {
			return errServiceKeyUsage
		}
		hub := realtime.NewPGHub(realtime.PGHubOptions{URL: cfg.DatabaseURL, Logger: logger})
		if err := revokeServiceKeyAnyTenant(ctx, d, hub, keyID); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "đã thu hồi khóa %s\n", keyID)
		return nil
	}
	in, err := serviceKeyInput(*name, *perm, *cidr)
	if err != nil || *email == "" {
		return errServiceKeyUsage
	}
	actor, err := identity.LoadUser(ctx, d, *email)
	if err != nil {
		return fmt.Errorf("nạp nhân viên tạo khóa: %w", err)
	}
	in.Actor = actor
	var (
		k    identity.ServiceKey
		full string
	)
	err = tenancy.WithTenant(ctx, d, actor.TenantID, func(tx pgx.Tx) error {
		var err error
		k, full, err = identity.CreateServiceKey(ctx, tx, in) // chạy lại được: mỗi lần một khóa mới, chỉ bản đã commit được in
		return err                                            //nolint:wrapcheck // identity đã bọc
	})
	if err != nil {
		return err //nolint:wrapcheck // như trên
	}
	_, _ = fmt.Fprintf(stdout, "khóa %s (mã %s, tiền tố %s) — chỉ hiện một lần:\n%s\n", k.Name, k.ID, k.KeyPrefix, full)
	logger.Info("đã tạo khóa dịch vụ", slog.String("service_key_id", k.ID.String()), slog.String("key_prefix", k.KeyPrefix))
	return nil
}

// serviceKeyInput đọc tên, danh sách quyền và CIDR (cách nhau bằng dấu phẩy) của lệnh create.
func serviceKeyInput(name, perms, cidrs string) (identity.CreateServiceKeyInput, error) {
	in := identity.CreateServiceKeyInput{Name: name}
	for p := range strings.SplitSeq(perms, ",") {
		if p = strings.TrimSpace(p); p != "" {
			in.Permissions = append(in.Permissions, identity.Permission(p))
		}
	}
	for c := range strings.SplitSeq(cidrs, ",") {
		if c = strings.TrimSpace(c); c == "" {
			continue
		}
		p, err := netip.ParsePrefix(c)
		if err != nil {
			return identity.CreateServiceKeyInput{}, fmt.Errorf("CIDR %q: %w", c, err)
		}
		in.AllowedCIDRs = append(in.AllowedCIDRs, p)
	}
	return in, nil
}

// revokeServiceKeyAnyTenant thử từng tenant (RLS ẩn khóa của tenant khác: không có → ErrServiceKeyNotFound, thử tenant
// kế): đọc version hiện tại, thu hồi (người làm system), phát báo hiệu thu hồi trong cùng transaction.
func revokeServiceKeyAnyTenant(ctx context.Context, d *db.DB, bus realtime.Bus, id uuid.UUID) error {
	tenants, err := tenancy.ListTenantIDs(ctx, d)
	if err != nil {
		return err //nolint:wrapcheck // đã bọc trong tenancy
	}
	for _, tenantID := range tenants {
		err := tenancy.WithTenant(ctx, d, tenantID, func(tx pgx.Tx) error { return revokeInTenant(ctx, tx, bus, tenantID, id) })
		if errors.Is(err, identity.ErrServiceKeyNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("thu hồi khóa dịch vụ ở tenant %s: %w", tenantID, err)
		}
		return nil
	}
	return fmt.Errorf("%s: %w", id, errServiceKeyUnknown)
}

// revokeInTenant: trong tx của tenantID, đọc version hiện tại của khóa id (không có → ErrServiceKeyNotFound), thu hồi
// với người làm system, phát báo hiệu thu hồi.
func revokeInTenant(ctx context.Context, tx pgx.Tx, bus realtime.Bus, tenantID, id uuid.UUID) error {
	keys, err := identity.ListServiceKeys(ctx, tx)
	if err != nil {
		return err //nolint:wrapcheck // identity đã bọc
	}
	i := slices.IndexFunc(keys, func(k identity.ServiceKey) bool { return k.ID == id })
	if i < 0 {
		return identity.ErrServiceKeyNotFound
	}
	k, err := identity.RevokeServiceKey(ctx, tx, id, keys[i].Version, identity.AuthUser{TenantID: tenantID})
	if err != nil {
		return err //nolint:wrapcheck // như trên
	}
	if err := bus.Publish(ctx, tx, realtime.Signal{
		TenantID: tenantID, Topic: identity.TopicServiceKeyRevoked, System: true,
		Entity: realtime.Entity{Type: identity.RouteKindServiceKey, ID: k.KeyPrefix},
	}); err != nil {
		return fmt.Errorf("báo hiệu thu hồi khóa: %w", err)
	}
	return nil
}
