package fake_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
)

// Kênh giả làm CDN và hồ sơ trong bộ nhớ (Task 155): chỉ tải từ máy fake.CDNHost qua https (máy khác → lỗi chặn, không
// tính lời gọi), tệp không có → không còn, quá trần → quá lớn; loại nhận dạng bằng nội dung; hồ sơ trả đúng tên và
// ảnh, lỗi đặt trước được trả nguyên (phân lớp bằng ClassifyError).
func TestChat_DownloadAndProfile(t *testing.T) {
	t.Parallel()
	a := fake.New(secret)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 100)...)
	u := a.AddFile("/p/a.png", png)
	if u != "https://"+fake.CDNHost+"/p/a.png" {
		t.Fatalf("URL = %s", u)
	}
	rc, info, err := a.Download(t.Context(), u, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, png) || info.ContentType != "image/png" || info.Size != int64(len(png)) || len(info.SHA256) != 64 {
		t.Fatalf("info = %+v", info)
	}
	for _, c := range []struct {
		url  string
		max  int64
		want error
	}{
		{"https://evil.example/a.png", 1 << 20, channel.ErrDownloadBlocked},
		{"http://" + fake.CDNHost + "/p/a.png", 1 << 20, channel.ErrDownloadBlocked},
		{"https://" + fake.CDNHost + "/khong-co", 1 << 20, channel.ErrDownloadUnavailable},
		{u, 10, channel.ErrDownloadTooLarge},
	} {
		if _, _, err := a.Download(t.Context(), c.url, c.max); !errors.Is(err, c.want) {
			t.Errorf("%s (trần %d): %v, mong %v", c.url, c.max, err, c.want)
		}
	}
	if n := a.DownloadCalls(); n != 3 {
		t.Errorf("lời gọi tới CDN giả = %d, mong 3 (máy lạ, http không tính)", n)
	}

	a.SetProfile("u1", channel.Profile{Name: "Lan Nguyễn", AvatarURL: u})
	p, err := a.FetchProfile(t.Context(), channel.ChannelAccount{}, channel.Credentials{}, "u1")
	if err != nil || p.Name != "Lan Nguyễn" || p.AvatarURL != u {
		t.Fatalf("hồ sơ = %+v, %v", p, err)
	}
	a.FailProfile("u2", fake.ErrPermanent)
	if _, err := a.FetchProfile(t.Context(), channel.ChannelAccount{}, channel.Credentials{}, "u2"); a.ClassifyError(err) != channel.ErrorPermanent {
		t.Fatalf("lỗi hồ sơ đặt trước: %v", err)
	}
	if _, err := a.FetchProfile(t.Context(), channel.ChannelAccount{}, channel.Credentials{}, "u3"); a.ClassifyError(err) != channel.ErrorPermanent {
		t.Fatalf("hồ sơ không có: %v", err)
	}
	var _ channel.AttachmentDownloader = a
	var _ channel.ProfileFetcher = a
}
