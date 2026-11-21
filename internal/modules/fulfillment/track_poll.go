package fulfillment

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Tra bù theo lịch, nút "Tra lại", cảnh báo đồng bộ (kế hoạch M3, Task M3-11). Lịch fulfillment.poll (600 s, theo
// tenant) chọn vận đơn mở cũ hơn PollMinutes của trạng thái, tối đa 200 mỗi tài khoản hãng mỗi lượt (Q-29), ghi mỗi vận
// đơn một lệnh tra; SyncShipment ghi một lệnh tra mỗi phút mỗi vận đơn.

const (
	// PollScheduleName là tên lịch tra bù (và loại việc của nó).
	PollScheduleName = "fulfillment.poll"
	// TrackAlertKind là tên loại lệnh gửi cảnh báo đồng bộ (stock-short:<tenant>, track-apply-failed:<tenant>).
	TrackAlertKind   = "fulfillment.track_alert"
	pollEvery        = 600 * time.Second
	pollTimeout      = 20 * time.Second
	pollPerAccount   = 200
	trackAlertExpiry = 0
	// unsyncedFactor: vận đơn chưa đồng bộ quá unsyncedFactor × PollMinutes là "quá ngưỡng" (đã lỡ ít nhất một lượt).
	unsyncedFactor = 2
	metricUnsynced = "shipments_unsynced_over_threshold"
	minuteLayout   = "200601021504"
	dayLayout      = "20060102"
)

// RefreshCommandKind là loại lệnh fulfillment.refresh_status (TrackKind của kế hoạch M3): còn chờ quá 30 phút thì
// expired, mỗi lần gọi hãng tối đa 20 s. Thứ tự best_effort (như M1): một lần tra dead không chặn các lần tra sau của
// cùng vận đơn; chỉ lần đang chạy chặn.
func RefreshCommandKind() queue.Kind {
	k := commands.CommandKind(refreshCommandName, refreshExpires, refreshTimeout)
	k.Policy.OrderMode = queue.OrderBestEffort
	return k
}

// SyncShipment ghi lệnh tra vận đơn id trong tx của bên gọi (nút "Tra lại", ngữ cảnh tenant a.TenantID); bấm nhiều lần
// trong một phút (đồng hồ của Service) chỉ ra một việc (DedupeKey manual:<id>:<phút>); trả mã việc. Không có vận đơn
// (hay tenant khác) → ErrNotFound; vận đơn nhập tay (mode manual, không có nguồn tra ở hãng) → ErrCarrierNotSupported,
// không ghi việc (Task 448). Kiểm quyền theo kho do bên gọi làm.
func (s *Service) SyncShipment(ctx context.Context, tx pgx.Tx, id uuid.UUID, a Actor) (uuid.UUID, error) {
	sh, err := s.ShipmentView(ctx, tx, id)
	if err != nil {
		return uuid.Nil, err
	}
	if sh.Mode != ModeAPI {
		return uuid.Nil, fmt.Errorf("tra vận đơn nhập tay %s: %w", sh.ID, ErrCarrierNotSupported)
	}
	job, _, err := s.queue().Enqueue(ctx, tx, RefreshCommandKind(), queue.NewJob{
		TenantID: &a.TenantID, Payload: commandPayload{ShipmentID: sh.ID},
		DedupeKey: "manual:" + sh.ID.String() + ":" + s.d.Now().UTC().Format(minuteLayout), OrderKey: shipmentOrderKey(sh.ID),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi lệnh tra vận đơn: %w", err)
	}
	return job, nil
}

// pollKind: việc của lịch tra bù là một lệnh (module không có handler transactional): còn chờ quá một kỳ thì expired.
func pollKind() queue.Kind { return commands.CommandKind(PollScheduleName, pollEvery, pollTimeout) }

func pollSchedule() schedules.Schedule {
	return schedules.Schedule{Name: PollScheduleName, Every: pollEvery, PerTenant: true, Kind: pollKind()}
}

// pollCommand là lệnh của lịch tra bù: ghi việc trong một transaction của chính nó (chống trùng theo kỳ nên chạy lại an
// toàn), không gọi hãng.
type pollCommand struct{ s *Service }

type pollPayload struct {
	Slot time.Time `json:"slot"`
}

func (pollCommand) Kind() queue.Kind       { return pollKind() }
func (pollCommand) AutoRetryNotSent() bool { return true }

// Lookup: ghi việc có khóa chống trùng theo kỳ; chạy lại không tạo trùng.
func (pollCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// Execute chọn vận đơn cần tra của tenant (PollCandidates theo PollMinutes của cài đặt, hãng có adapter Track) và ghi mỗi
// vận đơn một lệnh tra DedupeKey poll:<id>:<kỳ>; đặt gauge shipments_unsynced_over_threshold (nhãn: tenant) sau khi
// commit. Kết quả {"enqueued": số việc mới}. Lỗi database → not_sent (kỳ còn hạn thì chạy lại).
func (c pollCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p pollPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.Slot.IsZero() || j.TenantID == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil //nolint:nilerr // payload hỏng
	}
	keys := c.s.trackCarrierKeys()
	created, unsynced := 0, int64(0)
	slot := p.Slot.UTC().Format(time.RFC3339)
	err := tenancy.WithTenant(ctx, c.s.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		created, unsynced = 0, 0 // RetryTx có thể chạy lại
		if len(keys) == 0 {
			return nil
		}
		set, _, err := LoadSettings(ctx, tx)
		if err != nil {
			return err
		}
		ids, n, err := pollTargets(ctx, store.New(tx), set.PollMinutes, keys, c.s.d.Now().UTC())
		if err != nil {
			return err
		}
		unsynced = n
		for _, id := range ids {
			_, ok, err := c.s.queue().Enqueue(ctx, tx, RefreshCommandKind(), queue.NewJob{
				TenantID: j.TenantID, Payload: commandPayload{ShipmentID: id},
				DedupeKey: "poll:" + id.String() + ":" + slot, OrderKey: shipmentOrderKey(id),
			})
			if err != nil {
				return fmt.Errorf("ghi lệnh tra bù: %w", err)
			}
			if ok {
				created++
			}
		}
		return nil
	}, db.TxWorker)
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil //nolint:nilerr // chạy lại trong kỳ
	}
	if c.s.d.Metrics != nil && len(keys) > 0 {
		c.s.d.Metrics.Gauge(metricUnsynced, float64(unsynced), j.TenantID.String())
	}
	b, err := json.Marshal(map[string]int{"enqueued": created})
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả tra bù: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

// pollTargets trả vận đơn cần tra (tối đa pollPerAccount mỗi tài khoản) và số vận đơn chưa đồng bộ quá
// unsyncedFactor × PollMinutes.
func pollTargets(ctx context.Context, q *store.Queries, minutes map[string]int, keys []string, now time.Time,
) ([]uuid.UUID, int64, error) {
	m, err := json.Marshal(minutes)
	if err != nil {
		return nil, 0, fmt.Errorf("mã hóa chu kỳ tra bù: %w", err)
	}
	ids, err := q.PollCandidates(ctx, store.PollCandidatesParams{
		Minutes: m, CarrierKeys: keys, Now: now, PerAccount: pollPerAccount,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("chọn vận đơn tra bù: %w", err)
	}
	over := make(map[string]int, len(minutes))
	for k, v := range minutes {
		over[k] = v * unsyncedFactor
	}
	if m, err = json.Marshal(over); err != nil {
		return nil, 0, fmt.Errorf("mã hóa ngưỡng chưa đồng bộ: %w", err)
	}
	n, err := q.CountUnsynced(ctx, store.CountUnsyncedParams{Minutes: m, CarrierKeys: keys, Now: now})
	if err != nil {
		return nil, 0, fmt.Errorf("đếm vận đơn chưa đồng bộ: %w", err)
	}
	return ids, n, nil
}

// trackCarrierKeys: khóa hãng có adapter khai Track (lịch chỉ tra bù các hãng này), đã sắp.
func (s *Service) trackCarrierKeys() []string {
	if s.d.Carriers == nil {
		return nil
	}
	var out []string
	for _, k := range s.d.Carriers.Keys() {
		if a, ok := s.d.Carriers.Get(k); ok && a.Capabilities().Track {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// trackAlertKeys: khóa cảnh báo theo mã lỗi đồng bộ.
func trackAlertKey(code string, tenant uuid.UUID) string {
	if code == syncStockShort {
		return "stock-short:" + tenant.String()
	}
	return "track-apply-failed:" + tenant.String()
}

type trackAlertPayload struct {
	Code string `json:"code"`
}

func trackAlertKind() queue.Kind {
	return commands.CommandKind(TrackAlertKind, trackAlertExpiry, authAlertTimeout)
}

// enqueueTrackAlert ghi lệnh cảnh báo đồng bộ code của tenant trong tx; một lệnh mỗi (mã, tenant, ngày giờ Việt Nam).
func enqueueTrackAlert(ctx context.Context, tx pgx.Tx, jq queue.JobQueue, tenant uuid.UUID, code string, now time.Time,
) error {
	if _, _, err := jq.Enqueue(ctx, tx, trackAlertKind(), queue.NewJob{
		TenantID: &tenant, Payload: trackAlertPayload{Code: code},
		DedupeKey: code + ":" + tenant.String() + ":" + now.In(vietnam).Format(dayLayout),
	}); err != nil {
		return fmt.Errorf("ghi lệnh cảnh báo đồng bộ: %w", err)
	}
	return nil
}

// NewTrackAlertCommand trả lệnh TrackAlertKind: đếm vận đơn chưa kết thúc đang mang mã lỗi của lệnh (transaction ngắn
// trên d), rồi Notify(key "stock-short:<tenant>" hay "track-apply-failed:<tenant>", normal, văn bản chỉ có số lượng).
// n nil → rejected alerter_missing; đọc lỗi hay Notify lỗi → not_sent (Notify lỗi log một lần ở đây; logger nil: không
// log). cmd/crm đăng ký cùng các lệnh cảnh báo khác (dùng chung Alerter).
func NewTrackAlertCommand(d *db.DB, n Notifier, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return trackAlert{d: d, n: n, log: logger}
}

type trackAlert struct {
	d   *db.DB
	n   Notifier
	log *slog.Logger
}

func (trackAlert) Kind() queue.Kind       { return trackAlertKind() }
func (trackAlert) AutoRetryNotSent() bool { return true }

func (a trackAlert) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p trackAlertPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.Code == "" || j.TenantID == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil //nolint:nilerr // payload hỏng
	}
	if a.n == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonAlerter}, nil
	}
	var count int64
	if err := tenancy.WithTenant(ctx, a.d, *j.TenantID, func(tx pgx.Tx) (err error) {
		count, err = store.New(tx).CountSyncErrors(ctx, p.Code)
		return err //nolint:wrapcheck // bọc ở dưới
	}, db.TxWorker); err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil //nolint:nilerr // đọc lại sau
	}
	text := fmt.Sprintf("%d vận đơn không áp được kết quả tra của hãng (lỗi %s): xem mục \"Cần xem\" của vận chuyển",
		count, p.Code)
	if p.Code == syncStockShort {
		text = fmt.Sprintf("%d vận đơn hãng đã lấy hay giao nhưng tồn thực không đủ (kiểm kho ghi thấp hơn phần giữ): "+
			"điều chỉnh tồn rồi xem mục \"Cần xem\"", count)
	}
	if err := a.n.Notify(ctx, trackAlertKey(p.Code, *j.TenantID), severityNormal, text); err != nil {
		if a.log != nil {
			a.log.WarnContext(ctx, "gửi cảnh báo đồng bộ vận đơn lỗi", slog.String("code", p.Code),
				slog.String("error", queue.ErrorText(err)))
		}
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonAlertFailed}, nil
	}
	return commands.Outcome{Status: commands.StatusDone}, nil
}
