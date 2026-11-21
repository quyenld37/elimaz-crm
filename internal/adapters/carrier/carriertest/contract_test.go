package carriertest_test

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/carrier/best"
	"elimaz.com.vn/crm/internal/adapters/carrier/carriertest"
	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/adapters/carrier/manual"
	"elimaz.com.vn/crm/internal/adapters/carrier/spx"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/tools/fakecarrier/server"
)

const secret = "carriertest-fake-secret-0123456789abcdef"

// fakeFactory dựng máy chủ hãng giả (tools/fakecarrier/server) trong httptest và adapter fake nói chuyện với nó.
func fakeFactory(t *testing.T) (carriertest.Factory, *fake.Client) {
	t.Helper()
	srv := server.New(server.Options{Secret: secret, Hang: 2 * time.Second})
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	c := fake.NewClient(hs.URL, secret, hs.Client())
	acc := fulfillment.Account{
		ExternalAccountRef: "fake-1", CarrierKey: fake.CarrierKey, Environment: "sandbox", RouteKey: "rk-fake-1",
		Creds: fulfillment.Credentials{"token": secret},
		Pickup: fulfillment.Address{
			Name: "Kho MAIN", Phone: carriertest.SamplePhone, Line: "1 Hàng Bài", ProvinceCode: "01", WardCode: "00070",
		},
	}
	return carriertest.Factory{Adapter: c, Server: srv, Account: acc}, c
}

// Hãng giả đủ khả năng (trừ bảng kê) qua mọi ca của contract.
func TestContractFake(t *testing.T) {
	f, _ := fakeFactory(t)
	carriertest.Run(t, f)
}

// Khung các hãng chưa có hiện thực (mọi khả năng false; vtp có contract riêng trên vtptest) qua contract: chỉ kiểm ErrNotSupported, không cần máy chủ.
func TestContractFrames(t *testing.T) {
	var cfg config.Config
	for _, a := range []fulfillment.CarrierAdapter{manual.New("self"), best.New(cfg), spx.New(cfg)} {
		t.Run(a.Key(), func(t *testing.T) {
			if a.Capabilities() != (fulfillment.Capabilities{}) {
				t.Fatalf("khung %s phải khai mọi khả năng false: %+v", a.Key(), a.Capabilities())
			}
			carriertest.Run(t, carriertest.Factory{Adapter: a})
		})
	}
}

// liar khai Cancel: true nhưng Cancel trả ErrNotSupported.
type liar struct{ *fake.Client }

func (liar) Cancel(context.Context, fulfillment.Account, string) (fulfillment.CancelResult, error) {
	return "", fulfillment.ErrNotSupported
}

// Contract bắt adapter khai sai: chạy contract trên liar ở tiến trình con, phải đỏ đúng ca Cancel và nêu tên khả năng.
func TestContractCatchesLie(t *testing.T) {
	if os.Getenv("CARRIERTEST_LIAR") == "1" {
		f, c := fakeFactory(t)
		f.Adapter = liar{c}
		carriertest.Run(t, f)
		return
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestContractCatchesLie$", "-test.v") //nolint:gosec // G204: chạy lại chính tệp test
	cmd.Env = append(os.Environ(), "CARRIERTEST_LIAR=1")
	out, err := cmd.CombinedOutput()
	s := string(out)
	if err == nil {
		t.Fatalf("contract không bắt adapter khai sai:\n%s", s)
	}
	if !strings.Contains(s, "--- FAIL: TestContractCatchesLie/Cancel ") ||
		!strings.Contains(s, "khả năng Cancel khai true nhưng Cancel trả ErrNotSupported") {
		t.Fatalf("thiếu báo đỏ nêu khả năng Cancel:\n%s", s)
	}
	for line := range strings.SplitSeq(s, "\n") {
		if strings.Contains(line, "--- FAIL: TestContractCatchesLie/") && !strings.Contains(line, "/Cancel ") {
			t.Errorf("ca khác cũng đỏ: %s", line)
		}
	}
}
