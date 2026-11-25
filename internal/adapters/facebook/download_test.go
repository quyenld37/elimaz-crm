package facebook_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/facebook"
)

const cdnHost = "scontent.fsgn5-1.fbcdn.net"

// jpegBytes là đầu một tệp JPEG (đủ để nhận dạng bằng nội dung).
func jpegBytes() []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}, bytes.Repeat([]byte{0x42}, 600)...)
}

// dlEnv là env có DNS giả: mọi tên máy trong dns trỏ về địa chỉ đã cho (127.0.0.1 là máy giả).
func dlEnv(t *testing.T, dns map[string]string) env {
	t.Helper()
	e := newEnv(t, "")
	e.c.UseTestResolver(dns)
	return e
}

func (e env) at(host, path string) string { return "https://" + host + ":" + e.fg.Port() + path }

func TestDownload_FromMetaHostSniffsType(t *testing.T) {
	t.Parallel()
	e := dlEnv(t, map[string]string{cdnHost: "127.0.0.1"})
	body := jpegBytes()
	e.fg.AddFile("/v/x.jpg", body, "text/html")
	rc, info, err := e.c.Download(t.Context(), e.at(cdnHost, "/v/x.jpg?oh=abc"), facebook.MaxDownload)
	if err != nil {
		t.Fatalf("tải: %v", err)
	}
	defer func() { _ = rc.Close() }()
	got, _ := io.ReadAll(rc)
	sum := sha256.Sum256(body)
	if !bytes.Equal(got, body) || info.ContentType != "image/jpeg" || info.Size != int64(len(body)) ||
		info.SHA256 != hex.EncodeToString(sum[:]) || info.FileName != "x.jpg" {
		t.Fatalf("info = %+v, %d byte", info, len(got))
	}
	if calls := e.fg.Calls("v/x.jpg"); len(calls) != 0 {
		t.Fatal("tệp không đi qua đường Graph có token")
	}
}

func TestDownload_Rejects(t *testing.T) {
	t.Parallel()
	e := dlEnv(t, map[string]string{
		cdnHost: "127.0.0.1", "evil.example": "127.0.0.1", "private.fbcdn.net": "10.0.0.5",
		"meta.fbcdn.net": "169.254.169.254", "ok.fbsbx.com": "127.0.0.1",
	})
	e.fg.AddFile("/x.jpg", jpegBytes(), "image/jpeg")
	e.fg.AddRedirect("/to-metadata", "https://169.254.169.254/latest/meta-data/")
	e.fg.AddRedirect("/to-evil", e.at("evil.example", "/x.jpg"))
	e.fg.AddRedirect("/to-http", "http://"+cdnHost+":"+e.fg.Port()+"/x.jpg")
	e.fg.AddRedirect("/to-private-name", e.at("private.fbcdn.net", "/x.jpg"))
	// Bốn lần chuyển: loop0 → loop1 → loop2 → loop3 → x.jpg.
	e.fg.AddRedirect("/loop0", e.at(cdnHost, "/loop1"))
	e.fg.AddRedirect("/loop1", e.at(cdnHost, "/loop2"))
	e.fg.AddRedirect("/loop2", e.at(cdnHost, "/loop3"))
	e.fg.AddRedirect("/loop3", e.at(cdnHost, "/x.jpg"))
	cases := []struct {
		name string
		url  string
		want error
	}{
		{"máy lạ", e.at("evil.example", "/x.jpg"), facebook.ErrHostNotAllowed},
		{"http", "http://" + cdnHost + ":" + e.fg.Port() + "/x.jpg", facebook.ErrHostNotAllowed},
		{"tên hợp lệ, DNS trả địa chỉ riêng", e.at("private.fbcdn.net", "/x.jpg"), facebook.ErrPrivateAddress},
		{"tên hợp lệ, DNS trả link-local", e.at("meta.fbcdn.net", "/x.jpg"), facebook.ErrPrivateAddress},
		{"IP riêng viết thẳng", "https://10.0.0.5/x.jpg", facebook.ErrPrivateAddress},
		{"IP công khai viết thẳng", "https://157.240.1.1/x.jpg", facebook.ErrHostNotAllowed},
		{"hậu tố giả", e.at("fbcdn.net.evil.example", "/x.jpg"), facebook.ErrHostNotAllowed},
		{"chuyển hướng tới 169.254.169.254", e.at(cdnHost, "/to-metadata"), facebook.ErrPrivateAddress},
		{"chuyển hướng tới máy lạ", e.at(cdnHost, "/to-evil"), facebook.ErrHostNotAllowed},
		{"chuyển hướng sang http", e.at(cdnHost, "/to-http"), facebook.ErrHostNotAllowed},
		{"chuyển hướng tới tên trỏ địa chỉ riêng", e.at(cdnHost, "/to-private-name"), facebook.ErrPrivateAddress},
		{"quá 3 lần chuyển", e.at(cdnHost, "/loop0"), facebook.ErrTooManyRedirects},
	}
	for _, tc := range cases {
		_, _, err := e.c.Download(t.Context(), tc.url, facebook.MaxDownload)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, mong %v", tc.name, err, tc.want)
		}
	}
	e.fg.AddRedirect("/to-ok", e.at("ok.fbsbx.com", "/x.jpg"))
	if _, _, err := e.c.Download(t.Context(), e.at(cdnHost, "/to-ok"), facebook.MaxDownload); err != nil {
		t.Errorf("chuyển hướng tới máy Meta khác: %v", err)
	}
}

func TestDownload_SizeLimit(t *testing.T) {
	t.Parallel()
	e := dlEnv(t, map[string]string{cdnHost: "127.0.0.1"})
	big := make([]byte, facebook.MaxDownload+1)
	copy(big, jpegBytes())
	e.fg.AddFile("/big.jpg", big, "image/jpeg")
	if _, _, err := e.c.Download(t.Context(), e.at(cdnHost, "/big.jpg"), facebook.MaxDownload); !errors.Is(err, facebook.ErrTooLarge) {
		t.Fatalf("25 MiB + 1: %v, mong ErrTooLarge", err)
	}
	small := jpegBytes()[:100]
	e.fg.AddFile("/s.jpg", small, "image/jpeg")
	if _, _, err := e.c.Download(t.Context(), e.at(cdnHost, "/s.jpg"), 99); !errors.Is(err, facebook.ErrTooLarge) {
		t.Fatalf("100 byte với trần 99: %v", err)
	}
	rc, info, err := e.c.Download(t.Context(), e.at(cdnHost, "/s.jpg"), 100)
	if err != nil || info.Size != 100 {
		t.Fatalf("100 byte với trần 100: %v %+v", err, info)
	}
	_ = rc.Close()
	// Trần lớn hơn 25 MiB vẫn bị giới hạn ở 25 MiB.
	if _, _, err := e.c.Download(t.Context(), e.at(cdnHost, "/big.jpg"), 1<<30); !errors.Is(err, facebook.ErrTooLarge) {
		t.Fatalf("trần 1 GiB: %v, mong ErrTooLarge", err)
	}
}

func TestBlockedIP(t *testing.T) {
	t.Parallel()
	blocked := []string{
		"127.0.0.1", "127.9.9.9", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1", "169.254.169.254",
		"224.0.0.1", "239.255.255.250", "0.0.0.0", "0.1.2.3", "100.64.0.1", "100.127.255.254", "255.255.255.255",
		"::", "::1", "fc00::1", "fd12:3456::1", "fe80::1", "ff02::1", "::ffff:10.0.0.1", "::ffff:127.0.0.1",
		"::ffff:169.254.169.254",
	}
	allowed := []string{"157.240.1.1", "31.13.70.36", "2a03:2880:f12f:83:face:b00c::25de", "100.128.0.1", "172.32.0.1", "100.63.255.255"}
	for _, ip := range blocked {
		if !facebook.IsBlockedIP(ip) {
			t.Errorf("%s phải bị chặn", ip)
		}
	}
	for _, ip := range allowed {
		if facebook.IsBlockedIP(ip) {
			t.Errorf("%s không được chặn", ip)
		}
	}
}
