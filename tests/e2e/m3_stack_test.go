package e2e

import (
	"bytes"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/carrier"
	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// m3Wiring là hai module M3 nối như cmd/crm (wire_m3.go, modules.go) trên hãng giả thật (tools/fakecarrier) và kho tệp
// trong bộ nhớ (bảng kê COD, nhãn): fulfillment, cash và phần chạy nền của ba module M2 (customer, order, promotion).
// Stack dùng khi stackOptions.m3 (đường trọn M3, Task M3-48).
type m3Wiring struct {
	client *fake.Client
	ful    *fulfillment.Module
	csh    *cash.Module
	files  files.FileStore
	// jobs là việc nền của ba module M2 (ngoài consumer).
	jobs      []queue.Handler
	schedules []schedules.Schedule
}

// wireM3 dựng hai module M3 và đăng ký consumer của chúng cùng consumer của ba module M2 vào evs (thay consumer sổ COD
// cash.Consumer của M1: cash.Module đã gồm nó).
func (s *stack) wireM3(fakeCarrierURL string, hub realtime.Bus, evs *events.Registry, reg *metrics.Registry) *m3Wiring {
	s.t.Helper()
	cash.RegisterFilePurposes()
	fulfillment.RegisterFilePurposes()
	w := &m3Wiring{client: fake.NewClient(fakeCarrierURL, secret32, nil), files: filestest.MemFileStore(s.t)}
	carriers, err := fulfillment.NewRegistry(w.client)
	if err != nil {
		s.t.Fatal(err)
	}
	w.ful = fulfillment.New(fulfillment.Deps{
		DB: s.app, Queue: queue.NewQueue(), Bus: hub, Events: evs, Carriers: carriers, Metrics: reg, Files: w.files,
		ManualCarriers: carrier.ManualKeys(),
	})
	w.csh = cash.New(cash.Deps{
		DB: s.app, Queue: queue.NewQueue(), Bus: hub, Events: evs, Carriers: carriers, Metrics: reg, Files: w.files,
	})
	if err := errors.Join(w.ful.Check(), w.csh.Check(), cash.CheckStatementFormats(carriers)); err != nil {
		s.t.Fatal(err)
	}
	deps := background.Deps{DB: s.app, Queue: queue.NewQueue(), Bus: hub, Events: evs}
	consumers := background.Collect(w.ful.Consumers(), w.csh.Consumers())
	for _, p := range []background.Parts{customer.Background(deps), order.Background(deps), promotion.Background(deps)} {
		consumers = append(consumers, p.Consumers...)
		w.jobs = append(w.jobs, p.JobHandlers...)
		w.schedules = append(w.schedules, p.Schedules...)
	}
	w.schedules = append(w.schedules, background.Collect(w.ful.Schedules(), w.csh.Schedules())...)
	for _, c := range consumers {
		if err := evs.Register(c); err != nil {
			s.t.Fatal(err)
		}
	}
	return w
}

// apiDeps nối phần M3 vào lớp HTTP: Fulfillment, Cash, kho tệp.
func (w *m3Wiring) apiDeps(d httpapi.Deps) httpapi.Deps {
	d.Fulfillment, d.Cash, d.Files = w.ful.Service, w.csh.Service, w.files
	return d
}

// worker trả nguồn webhook (tách gói), handler vào, lệnh và việc nền của M3 cho worker, như cmd/crm: nguồn
// /hooks/fake/<route_key> (worker không kiểm chữ ký), lệnh của hai module cùng lệnh sửa địa chỉ ở hãng.
func (w *m3Wiring) worker() ([]webhooks.Source, []webhooks.InboundHandler, []commands.Handler, []queue.Handler, error) {
	sources := append([]webhooks.Source{carrier.WebhookSource(w.client, "")}, w.ful.WebhookSources()...)
	inbound := background.Collect(w.ful.InboundHandlers(), w.csh.InboundHandlers())
	cmds := background.Collect(w.ful.CommandHandlers(), w.csh.CommandHandlers())
	addr, err := w.ful.AddressCommand(httpapi.RecordFulfillmentAudit)
	if err != nil {
		return nil, nil, nil, nil, err //nolint:wrapcheck // test
	}
	if addr != nil {
		cmds = append(cmds, addr)
	}
	return sources, inbound, cmds, w.jobs, nil
}

// runSchedule ghi ngay một việc của lịch name (như Ticker ghi khi tới kỳ slot) cho tenant của stack; worker của stack
// chạy nó. Lịch không có → test đỏ.
func (s *stack) runSchedule(name string, slot time.Time) {
	s.t.Helper()
	for _, sc := range s.m3.schedules {
		if sc.Name != name {
			continue
		}
		job := queue.NewJob{
			Payload: map[string]any{"slot": slot.UTC()}, DedupeKey: name + ":" + slot.UTC().Format(time.RFC3339),
		}
		if sc.PerTenant {
			job.TenantID = &s.tenant
		}
		if err := tenancy.WithTenant(s.t.Context(), s.app, s.tenant, func(tx pgx.Tx) error {
			_, _, err := queue.NewQueue().Enqueue(s.t.Context(), tx, sc.Kind, job)
			return err //nolint:wrapcheck // test
		}); err != nil {
			s.t.Fatal(err)
		}
		return
	}
	s.t.Fatalf("không có lịch %s", name)
}

// partnerRoute dựng tuyến webhook carrier_account:<route_key> cho tài khoản hãng giả của người bán PARTNER1 (fake-2),
// như seed-dev làm cho fake-1.
func (s *stack) partnerRoute() {
	s.t.Helper()
	if err := tenancy.WithTenant(s.t.Context(), s.owner, s.tenant, func(tx pgx.Tx) error {
		_, err := fulfillment.EnsureAccountRoute(s.t.Context(), tx, s.shop.PartnerAccount)
		return err //nolint:wrapcheck // test
	}); err != nil {
		s.t.Fatal(err)
	}
}

// uploadFile tải body (mục đích purpose) lên kho tệp của stack như cmd/crm (CreateUpload → Put → CompleteUpload); trả
// mã tệp.
func (s *stack) uploadFile(purpose, name, contentType string, body []byte) uuid.UUID {
	s.t.Helper()
	ctx := s.t.Context()
	var f files.File
	if err := tenancy.WithTenant(ctx, s.app, s.tenant, func(tx pgx.Tx) error {
		var err error
		f, _, err = files.CreateUpload(ctx, tx, s.m3.files, files.UploadInput{
			TenantID: s.tenant, Purpose: purpose, FileName: name, ContentType: contentType, SizeBytes: int64(len(body)),
		}, uuid.Nil)
		return err //nolint:wrapcheck // test
	}); err != nil {
		s.t.Fatal(err)
	}
	if err := s.m3.files.Put(ctx, files.BucketTmp, files.TmpKey(s.tenant, f.ID), bytes.NewReader(body), int64(len(body)),
		contentType); err != nil {
		s.t.Fatal(err)
	}
	if _, err := files.CompleteUpload(ctx, s.app, s.m3.files, queue.NewQueue(), s.tenant, f.ID); err != nil {
		s.t.Fatal(err)
	}
	return f.ID
}
