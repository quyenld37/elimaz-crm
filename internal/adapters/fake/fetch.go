package fake

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sync"

	"elimaz.com.vn/crm/internal/modules/channel"
)

const (
	// CDNHost là máy chủ tệp duy nhất kênh giả tải từ (như máy chủ của Meta với Facebook).
	CDNHost = "cdn.fakechat.test"
	// maxDownload là trần một lần tải của kênh giả (bằng Facebook, 25 MiB).
	maxDownload int64 = 25 << 20
)

// cdn là tệp và hồ sơ trong bộ nhớ của kênh giả (Task 155).
type cdn struct {
	mu          sync.Mutex
	files       map[string][]byte
	profiles    map[string]channel.Profile
	profileErrs map[string]error
	calls       int
}

func newCDN() *cdn {
	return &cdn{files: map[string][]byte{}, profiles: map[string]channel.Profile{}, profileErrs: map[string]error{}}
}

// AddFile đặt tệp body ở đường p của CDN giả và trả URL https tới nó.
func (a *Chat) AddFile(p string, body []byte) string {
	a.cdn.mu.Lock()
	defer a.cdn.mu.Unlock()
	a.cdn.files[p] = bytes.Clone(body)
	return "https://" + CDNHost + p
}

// DownloadCalls là số lần Download đã tới CDN giả (máy lạ, không phải https thì không tới).
func (a *Chat) DownloadCalls() int {
	a.cdn.mu.Lock()
	defer a.cdn.mu.Unlock()
	return a.cdn.calls
}

// SetProfile đặt hồ sơ của người dùng userID.
func (a *Chat) SetProfile(userID string, p channel.Profile) {
	a.cdn.mu.Lock()
	defer a.cdn.mu.Unlock()
	a.cdn.profiles[userID] = p
}

// FailProfile làm mọi lần đọc hồ sơ userID trả err (lỗi mẫu của gói: ErrAuth, ErrPermanent, ErrNotSent…).
func (a *Chat) FailProfile(userID string, err error) {
	a.cdn.mu.Lock()
	defer a.cdn.mu.Unlock()
	a.cdn.profileErrs[userID] = err
}

// Download tải tệp đính kèm từ CDN giả: chỉ https tới CDNHost (khác → channel.ErrDownloadBlocked, không tới CDN);
// không có tệp → channel.ErrDownloadUnavailable; lớn hơn maxBytes (≤ 25 MiB) → channel.ErrDownloadTooLarge. Loại
// nhận dạng bằng nội dung.
func (a *Chat) Download(_ context.Context, rawURL string, maxBytes int64) (io.ReadCloser, channel.DownloadInfo, error) {
	if maxBytes <= 0 || maxBytes > maxDownload {
		maxBytes = maxDownload
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Hostname() != CDNHost {
		return nil, channel.DownloadInfo{}, fmt.Errorf("fakechat: tải tệp: %w", channel.ErrDownloadBlocked)
	}
	a.cdn.mu.Lock()
	a.cdn.calls++
	body, ok := a.cdn.files[u.Path]
	a.cdn.mu.Unlock()
	switch {
	case !ok:
		return nil, channel.DownloadInfo{}, fmt.Errorf("fakechat: tải tệp: %w", channel.ErrDownloadUnavailable)
	case int64(len(body)) > maxBytes:
		return nil, channel.DownloadInfo{}, fmt.Errorf("fakechat: tải tệp: %w", channel.ErrDownloadTooLarge)
	}
	sum := sha256.Sum256(body)
	info := channel.DownloadInfo{
		ContentType: http.DetectContentType(body), Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:]),
		FileName: path.Base(u.Path),
	}
	return io.NopCloser(bytes.NewReader(body)), info, nil
}

// FetchProfile trả hồ sơ đã đặt bằng SetProfile; lỗi đặt bằng FailProfile trả nguyên; không có → ErrPermanent.
func (a *Chat) FetchProfile(_ context.Context, _ channel.ChannelAccount, _ channel.Credentials, userID string) (channel.Profile, error) {
	a.cdn.mu.Lock()
	defer a.cdn.mu.Unlock()
	if err := a.cdn.profileErrs[userID]; err != nil {
		return channel.Profile{}, fmt.Errorf("fakechat: đọc hồ sơ: %w", err)
	}
	p, ok := a.cdn.profiles[userID]
	if !ok {
		return channel.Profile{}, fmt.Errorf("fakechat: đọc hồ sơ: %w", ErrPermanent)
	}
	return p, nil
}
