package nhanh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// Tải ảnh sản phẩm của Nhanh về (kế hoạch M7, Task 541; ADR-48). Địa chỉ ảnh nằm trong bản chụp do Nhanh trả, nên
// coi là không tin cậy: chỉ https, không đi vào địa chỉ nội bộ (loopback, riêng, link-local), không theo chuyển
// hướng sang http, giới hạn dung lượng và thời gian. Chỉ GET.

// Giới hạn tải ảnh.
const (
	MaxImageBytes = 10 << 20
	imageTimeout  = 30 * time.Second
	maxRedirects  = 3
)

// Lỗi tải ảnh (không chứa địa chỉ: địa chỉ ảnh có thể mang mã truy cập).
var (
	ErrImageURL      = errors.New("nhanh: địa chỉ ảnh không hợp lệ (chỉ https)")
	ErrImageTooLarge = errors.New("nhanh: ảnh quá lớn")
	ErrImageStatus   = errors.New("nhanh: máy chủ ảnh trả lỗi")
	ErrImageType     = errors.New("nhanh: không phải ảnh WebP, JPEG, PNG")
	errBlockedAddr   = errors.New("nhanh: địa chỉ nội bộ bị chặn")
)

// ImageFetcher tải ảnh. Zero value dùng được (chặn địa chỉ nội bộ); AllowPrivate chỉ cho test (máy chủ giả ở loopback).
type ImageFetcher struct {
	AllowPrivate bool
	// HTTPClient thay khách mặc định (test); nil: khách riêng chặn địa chỉ nội bộ.
	HTTPClient *http.Client
}

func (f ImageFetcher) client() *http.Client {
	if f.HTTPClient != nil {
		return f.HTTPClient
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	if !f.AllowPrivate {
		d.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return errBlockedAddr
			}
			ip := net.ParseIP(host)
			if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
				ip.IsUnspecified() || ip.IsMulticast() {
				return errBlockedAddr
			}
			return nil
		}
	}
	return &http.Client{
		Timeout:   imageTimeout,
		Transport: &http.Transport{DialContext: d.DialContext, TLSHandshakeTimeout: 10 * time.Second, Proxy: nil},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects || req.URL.Scheme != "https" && !f.AllowPrivate {
				return ErrImageURL
			}
			return nil
		},
	}
}

// Fetch tải ảnh ở rawURL: trả nội dung và loại (image/webp, image/jpeg, image/png theo đầu tệp, không theo header).
func (f ImageFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && !(f.AllowPrivate && u.Scheme == "http")) || u.Host == "" {
		return nil, "", ErrImageURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, "", ErrImageURL
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("tải ảnh: %w", scrubURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%w: %d", ErrImageStatus, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("đọc ảnh: %w", scrubURL(err))
	}
	if len(data) > MaxImageBytes {
		return nil, "", ErrImageTooLarge
	}
	ct := ImageType(data)
	if ct == "" {
		return nil, "", ErrImageType
	}
	return data, ct, nil
}

// scrubURL bỏ địa chỉ khỏi lỗi của net/http (url.Error chứa cả địa chỉ).
func scrubURL(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return ue.Err
	}
	return err
}

// ImageType nhận loại ảnh theo đầu tệp; rỗng khi không phải WebP, JPEG, PNG.
func ImageType(b []byte) string {
	switch {
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "image/webp"
	case len(b) >= 3 && bytes.Equal(b[:3], []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	}
	return ""
}
