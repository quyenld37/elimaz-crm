package facebook

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"syscall"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/urlerr"
)

const (
	// MaxDownload là cỡ tối đa một tệp đính kèm được tải (25 MiB); trần lớn hơn truyền vào bị hạ về đây.
	MaxDownload int64 = 25 << 20
	// downloadTimeout là hạn của một lần tải tệp (Global Constraints M4).
	downloadTimeout = 30 * time.Second
	dialTimeout     = 10 * time.Second
	// maxRedirects là số lần chuyển hướng tối đa.
	maxRedirects = 3
)

// Lỗi của Download. Mỗi lỗi vĩnh viễn bọc thêm lỗi chung của channel (Task 155): ErrHostNotAllowed, ErrPrivateAddress,
// ErrTooManyRedirects → channel.ErrDownloadBlocked; ErrTooLarge → channel.ErrDownloadTooLarge; ErrDownloadStatus với
// 403, 404, 410 → channel.ErrDownloadUnavailable (mã khác: lỗi tạm).
var (
	ErrHostNotAllowed   = fmt.Errorf("facebook: tệp không nằm trên máy chủ của Meta (hoặc không phải https): %w", channel.ErrDownloadBlocked)
	ErrPrivateAddress   = fmt.Errorf("facebook: địa chỉ tệp trỏ vào mạng nội bộ: %w", channel.ErrDownloadBlocked)
	ErrTooLarge         = fmt.Errorf("facebook: tệp quá lớn: %w", channel.ErrDownloadTooLarge)
	ErrTooManyRedirects = fmt.Errorf("facebook: quá nhiều lần chuyển hướng: %w", channel.ErrDownloadBlocked)
	ErrDownloadStatus   = errors.New("facebook: máy chủ tệp trả lỗi")
)

// downloader tải tệp đính kèm chỉ từ máy chủ của Meta (spec 04 mục 13). resolve và allow chỉ đặt trong test
// (export_test.go): mã thật luôn nil và không cho phép địa chỉ nào trong danh sách chặn.
type downloader struct {
	hosts   []string
	hc      *http.Client
	resolve func(host string) (string, bool)
	allow   netip.Addr
}

func newDownloader(base *http.Client, hosts []string) *downloader {
	d := &downloader{}
	for _, h := range hosts {
		if h = strings.ToLower(strings.Trim(strings.TrimSpace(h), ".")); h != "" {
			d.hosts = append(d.hosts, h)
		}
	}
	tr := &http.Transport{
		Proxy:                 nil, // không qua proxy: kiểm địa chỉ ở lúc nối phải là địa chỉ thật
		DialContext:           d.dial,
		TLSClientConfig:       tlsOf(base),
		TLSHandshakeTimeout:   dialTimeout,
		ResponseHeaderTimeout: downloadTimeout,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	}
	d.hc = &http.Client{Transport: tr, Timeout: downloadTimeout, CheckRedirect: d.checkRedirect}
	return d
}

// tlsOf lấy cấu hình TLS của http.Client bên gọi (gốc tin cậy trong test), mặc định TLS ≥ 1.2.
func tlsOf(base *http.Client) *tls.Config {
	if base != nil {
		if t, ok := base.Transport.(*http.Transport); ok && t.TLSClientConfig != nil {
			return t.TLSClientConfig.Clone()
		}
	}
	return &tls.Config{MinVersion: tls.VersionTLS12}
}

// Download tải rawURL (chỉ https, tên máy kết thúc bằng một hậu tố AttachmentHosts, không địa chỉ nội bộ kể cả sau DNS
// và sau chuyển hướng, tối đa 3 lần chuyển, hạn 30 s), đọc tối đa maxBytes (≤ 25 MiB) vào bộ nhớ. ContentType nhận
// dạng bằng nội dung, không tin header. Không gửi token. Lỗi không chứa URL (URL của Meta có chữ ký).
func (c *Client) Download(ctx context.Context, rawURL string, maxBytes int64) (io.ReadCloser, channel.DownloadInfo, error) {
	return c.dl.download(ctx, rawURL, maxBytes)
}

func (d *downloader) download(ctx context.Context, rawURL string, maxBytes int64) (io.ReadCloser, channel.DownloadInfo, error) {
	if maxBytes <= 0 || maxBytes > MaxDownload {
		maxBytes = MaxDownload
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, channel.DownloadInfo{}, fmt.Errorf("tải tệp đính kèm: %w: địa chỉ hỏng", ErrHostNotAllowed)
	}
	if err := d.checkURL(u); err != nil {
		return nil, channel.DownloadInfo{}, fmt.Errorf("tải tệp đính kèm: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, channel.DownloadInfo{}, fmt.Errorf("tải tệp đính kèm: %w", urlerr.Strip(err))
	}
	resp, err := d.hc.Do(req)
	if err != nil {
		return nil, channel.DownloadInfo{}, fmt.Errorf("tải tệp đính kèm: %w", urlerr.Strip(err))
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden, http.StatusNotFound, http.StatusGone:
		return nil, channel.DownloadInfo{}, fmt.Errorf("tải tệp đính kèm: %w: %w: HTTP %d", ErrDownloadStatus,
			channel.ErrDownloadUnavailable, resp.StatusCode)
	default:
		return nil, channel.DownloadInfo{}, fmt.Errorf("tải tệp đính kèm: %w: HTTP %d", ErrDownloadStatus, resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return nil, channel.DownloadInfo{}, fmt.Errorf("tải tệp đính kèm: %w", ErrTooLarge)
	}
	buf, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, channel.DownloadInfo{}, fmt.Errorf("đọc tệp đính kèm: %w", urlerr.Strip(err))
	}
	if int64(len(buf)) > maxBytes {
		return nil, channel.DownloadInfo{}, fmt.Errorf("tải tệp đính kèm: %w", ErrTooLarge)
	}
	sum := sha256.Sum256(buf)
	info := channel.DownloadInfo{
		ContentType: http.DetectContentType(buf), Size: int64(len(buf)), SHA256: hex.EncodeToString(sum[:]),
		FileName: fileName(resp.Request.URL),
	}
	return io.NopCloser(bytes.NewReader(buf)), info, nil
}

func fileName(u *url.URL) string {
	if b := path.Base(u.Path); b != "/" && b != "." {
		return b
	}
	return ""
}

// checkURL: chỉ https, không thông tin đăng nhập; tên máy là IP thì IP nội bộ → ErrPrivateAddress, IP khác →
// ErrHostNotAllowed; tên máy phải bằng hoặc kết thúc bằng "."+ một hậu tố được phép.
func (d *downloader) checkURL(u *url.URL) error {
	if u.Scheme != "https" || u.User != nil {
		return ErrHostNotAllowed
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if a, err := netip.ParseAddr(host); err == nil {
		if blockedAddr(a) {
			return ErrPrivateAddress
		}
		return ErrHostNotAllowed
	}
	for _, s := range d.hosts {
		if host == s || strings.HasSuffix(host, "."+s) {
			return nil
		}
	}
	return ErrHostNotAllowed
}

func (d *downloader) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return ErrTooManyRedirects
	}
	return d.checkURL(req.URL)
}

// dial nối tới addr; Control kiểm địa chỉ thật sẽ nối (sau DNS) và từ chối địa chỉ nội bộ.
func (d *downloader) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if d.resolve != nil {
		if host, port, err := net.SplitHostPort(addr); err == nil {
			if ip, ok := d.resolve(host); ok {
				addr = net.JoinHostPort(ip, port)
			}
		}
	}
	dl := &net.Dialer{Timeout: dialTimeout, Control: d.control}
	conn, err := dl.DialContext(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("nối máy chủ tệp: %w", err)
	}
	return conn, nil
}

func (d *downloader) control(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return ErrPrivateAddress
	}
	a := ap.Addr().Unmap()
	if d.allow.IsValid() && a == d.allow {
		return nil
	}
	if blockedAddr(a) {
		return ErrPrivateAddress
	}
	return nil
}

// blockedPrefixes là các dải không được nối ngoài các loại netip tự nhận biết: "mạng này" 0.0.0.0/8, CGNAT
// 100.64.0.0/10, IETF 192.0.0.0/24, đo thử 198.18.0.0/15, dành riêng 240.0.0.0/4 (gồm 255.255.255.255), NAT64
// 64:ff9b::/96 và 6to4 2002::/16 (có thể dẫn về IPv4 nội bộ), site-local cũ fec0::/10.
func blockedPrefixes() []netip.Prefix {
	return []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("64:ff9b::/96"),
		netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("fec0::/10"),
	}
}

// blockedAddr báo địa chỉ thuộc loopback, riêng tư (gồm IPv6 ULA), link-local, multicast, chưa xác định hay một dải
// trong blockedPrefixes. IPv4 viết dạng IPv6 (::ffff:a.b.c.d) được xét như IPv4.
func blockedAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() || a.IsUnspecified() {
		return true
	}
	for _, p := range blockedPrefixes() {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
