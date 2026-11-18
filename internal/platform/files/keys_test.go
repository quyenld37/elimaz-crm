package files_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/files"
)

func TestKeys_Formats(t *testing.T) {
	ten := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	fid := uuid.MustParse("01900000-0000-7000-8000-0000000000ff")
	sha := strings.Repeat("ab", 32)
	cases := map[string][2]string{
		"tmp":         {files.TmpKey(ten, fid), "tmp/" + ten.String() + "/" + fid.String()},
		"server":      {files.ServerKey(ten, fid), "priv/" + ten.String() + "/" + fid.String()},
		"original":    {files.OriginalKey(sha), "orig/" + sha},
		"variant":     {files.VariantKey(sha, 1, 640, "webp"), "img/" + sha + "/v1/640.webp"},
		"private var": {files.PrivateVariantKey(ten, fid, 2, 1600, "jpeg"), "priv/" + ten.String() + "/" + fid.String() + "/v2/1600.jpeg"},
	}
	for name, c := range cases {
		if c[0] != c[1] {
			t.Errorf("%s: %q, muốn %q", name, c[0], c[1])
		}
	}
}

// Quy luật: khóa chỉ gồm tiền tố cố định, mã uuid, số và chữ thường; không có "..", không có "//".
func TestKeys_Property(t *testing.T) {
	safe := regexp.MustCompile(`^(tmp|priv)/[0-9a-f-]{36}/[0-9a-f-]{36}(/v[0-9]+/[0-9]+\.(avif|webp|jpeg))?$`)
	rapid.Check(t, func(rt *rapid.T) {
		ten := uuid.UUID(rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(rt, "tenant"))
		fid := uuid.UUID(rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(rt, "file"))
		preset := rapid.IntRange(1, 1000).Draw(rt, "preset")
		width := rapid.IntRange(1, 10000).Draw(rt, "width")
		format := rapid.SampledFrom([]string{"avif", "webp", "jpeg"}).Draw(rt, "format")
		for _, k := range []string{files.TmpKey(ten, fid), files.ServerKey(ten, fid), files.PrivateVariantKey(ten, fid, preset, width, format)} {
			if !safe.MatchString(k) || strings.Contains(k, "..") {
				rt.Fatalf("khóa không an toàn: %q", k)
			}
		}
	})
}

func TestPreset_V1(t *testing.T) {
	p, ok := files.Preset(1)
	if !ok || len(p) != 12 {
		t.Fatalf("preset v1 phải có 12 biến thể, có %d (%v)", len(p), ok)
	}
	seen := map[files.VariantSpec]bool{}
	for _, v := range p {
		seen[v] = true
		if v.Format != "avif" && v.Format != "webp" && v.Format != "jpeg" {
			t.Fatalf("định dạng lạ %q", v.Format)
		}
		switch v.Width {
		case 320, 640, 1080, 1600:
		default:
			t.Fatalf("chiều rộng lạ %d", v.Width)
		}
	}
	if len(seen) != 12 {
		t.Fatalf("12 biến thể phải khác nhau, có %d", len(seen))
	}
	if _, ok := files.Preset(99); ok {
		t.Fatal("preset 99 không tồn tại")
	}
}
