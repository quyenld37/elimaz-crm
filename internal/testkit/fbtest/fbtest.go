// Package fbtest dựng kênh Facebook nói chuyện với máy Meta giả (testkit/fakegraph) cho test của các phần dùng kênh:
// database thử có tenant A với một Page Facebook (token lưu mã hóa), máy giả có sẵn Page đó, adapter facebook trỏ vào
// nó, bộ adapter chỉ gồm adapter đó.
package fbtest

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// App Meta giả mà mọi Env dùng (verify token = AppSecret hai lần).
const (
	AppID     = "1626301872631379"
	AppSecret = "0123456789abcdef0123456789abcdef"
	// PageName là tên của Page trên máy giả và trong database thử.
	PageName = "Shop thử"
	keyByte  = 7 // byte lặp 32 lần làm khóa k1 của bộ khóa mã hóa trường
)

// Options là Page có sẵn trên máy giả: mã ngoài PageID, token PageToken.
type Options struct {
	PageID, PageToken string
}

// Env là database thử (Staff, tenant A, mã Page), máy Meta giả, adapter facebook (Graph v26.0, cả luồng SSE trỏ vào
// máy giả), bộ adapter và bộ khóa mã hóa trường.
type Env struct {
	Staff    *testkit.Staff
	Tenant   uuid.UUID
	Page     uuid.UUID
	Graph    *fakegraph.Server
	Channel  *facebook.Channel
	Registry *channel.Registry
	Keys     *fieldcrypt.Keyring
}

// New dựng Env theo o; lỗi thì dừng test.
func New(t testing.TB, o Options) Env {
	t.Helper()
	fg := fakegraph.New(t, fakegraph.Options{AppID: AppID, AppSecret: AppSecret})
	fg.AddPage(fakegraph.Page{ID: o.PageID, Name: PageName, Token: o.PageToken})
	fb := facebook.New(facebook.Options{
		BaseURL: fg.URL(), StreamingBaseURL: fg.URL(), Version: "v26.0", AppID: AppID,
		AppSecret: logging.Secret(AppSecret), VerifyToken: logging.Secret(AppSecret + AppSecret), HTTP: fg.Client(),
	})
	reg, err := channel.NewRegistry(fb)
	if err != nil {
		t.Fatal(err)
	}
	kr, err := fieldcrypt.ParseKeyring(logging.Secret("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{keyByte}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	e := Env{Staff: testkit.OpenStaff(t), Graph: fg, Channel: fb, Registry: reg, Keys: kr}
	e.Tenant = e.Staff.Tenant(t, "A")
	e.Page = e.Staff.ChannelAccount(t, e.Tenant, facebook.Key, o.PageID, PageName)
	e.Staff.Exec(t, e.Tenant, func(tx pgx.Tx) error {
		return channel.StoreCredentials(t.Context(), tx, kr, e.Page, logging.Secret(o.PageToken), "page_user")
	})
	return e
}
