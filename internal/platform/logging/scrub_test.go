package logging_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Dữ liệu giả dùng chung: SĐT, địa chỉ, token phải biến mất khỏi mọi nội dung lỗi.
const (
	fakePhone   = "0912345678"
	fakeAddress = "12 Nguyễn Trãi, Thanh Xuân, Hà Nội"
	fakeToken   = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.c2lnbmF0dXJlMTIz"
)

func TestScrubTextRemovesPersonalData(t *testing.T) {
	cases := []struct {
		name, in string
		gone     []string
		keep     []string
	}{
		{"SĐT liền", "gọi 0912345678 lỗi", []string{"0912345678"}, []string{"gọi", "lỗi"}},
		{"SĐT có dấu cách", "gọi 0912 345 678 lỗi", []string{"0912 345 678", "345 678"}, []string{"gọi", "lỗi"}},
		{"SĐT có chấm, gạch", "sđt 091.234.5678 hoặc 091-234-5678", []string{"091.234.5678", "091-234-5678"}, nil},
		{"SĐT +84", "gửi tới +84 912 345 678", []string{"912 345 678"}, []string{"gửi tới"}},
		{"email", "mail a.b@shop.vn hỏng", []string{"a.b@shop.vn"}, []string{"mail", "hỏng"}},
		{"địa chỉ số nhà + tên đường", "giao tới " + fakeAddress + ": hết hàng", []string{"Nguyễn Trãi", "Thanh Xuân", "Hà Nội"}, []string{"giao tới", "hết hàng"}},
		{"địa chỉ có phường, quận", "địa chỉ 123/4 Lê Lợi, phường Bến Nghé, quận 1, TP.HCM", []string{"Lê Lợi", "Bến Nghé", "TP.HCM"}, nil},
		{"địa chỉ theo ngõ, phố", "nhà số 5 ngõ 10 phố Huế không nhận", []string{"phố Huế", "ngõ 10"}, []string{"không nhận"}},
		{"nhãn địa chỉ", "địa chỉ: 45B Trần Hưng Đạo; mã 7", []string{"Trần Hưng Đạo", "45B"}, []string{"mã 7"}},
		{"khóa=giá trị", "insert customer_phone=0987 x, address=1 Hai Bà Trưng", []string{"0987", "Hai Bà Trưng"}, []string{"insert"}},
		{"JSON", `body {"phone":"0123","token":"abc"} sai`, []string{`"0123"`, `"abc"`}, []string{"sai"}},
		{"khóa address có dấu phẩy", "address=12 Nguyễn Trãi, Thanh Xuân, Hà Nội; mã 7", []string{"Nguyễn Trãi", "Thanh Xuân", "Hà Nội"}, []string{"mã 7"}},
		{"khóa address không số nhà", "ship_address=Thanh Xuân, Hà Nội: lỗi", []string{"Thanh Xuân", "Hà Nội"}, []string{"lỗi"}},
		{"JSON address có dấu phẩy", `{"address":"Thanh Xuân, Hà Nội","n":1}`, []string{"Thanh Xuân", "Hà Nội"}, []string{`"n":1`}},
		{"chi tiết PostgreSQL", "Key (phone)=(0912000111) already exists", []string{"0912000111"}, []string{"already exists"}},
		{"JWT", "thẻ " + fakeToken + " hết hạn", []string{fakeToken, "eyJhbGciOiJIUzI1NiJ9"}, []string{"hết hạn"}},
		{"Bearer", "Authorization: Bearer s3cr3tVALUE hỏng", []string{"s3cr3tVALUE"}, []string{"hỏng"}},
		{"token dài", "key sk_live_4eC39HqLyjWDarjtT1zdp7dc lỗi", []string{"4eC39HqLyjWDarjtT1zdp7dc"}, []string{"lỗi"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := logging.ScrubText(c.in)
			for _, g := range c.gone {
				if strings.Contains(got, g) {
					t.Errorf("ScrubText(%q) = %q, còn %q", c.in, got, g)
				}
			}
			for _, k := range c.keep {
				if !strings.Contains(got, k) {
					t.Errorf("ScrubText(%q) = %q, mất phần không nhạy cảm %q", c.in, got, k)
				}
			}
		})
	}
}

// Nội dung lỗi thường của hệ thống không bị che nhầm: mã việc (UUID), SQLSTATE, số ngắn, tên hàm, chữ tiếng Việt.
func TestScrubTextKeepsOrdinaryErrors(t *testing.T) {
	for _, s := range []string{
		"ghi lỗi ứng dụng: context deadline exceeded",
		"việc 3f1c2a9e-1b2c-4d5e-8f90-123456789abc: hết lượt",
		"ERROR: duplicate key value violates unique constraint \"jobs_pkey\" (SQLSTATE 23505)",
		"mã 1234567 còn",
		"đường ghi lỗi: elimaz.com.vn/crm/internal/platform/queue.(*Worker).runJob",
		"password authentication failed for user \"crm_app\"",
	} {
		if got := logging.ScrubText(s); got != s {
			t.Errorf("ScrubText(%q) = %q, muốn giữ nguyên", s, got)
		}
	}
}

var (
	phoneLike = regexp.MustCompile(`[0-9]{8,}`)
	emailLike = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
)

// Quy luật: SĐT (liền hoặc có dấu ngăn), email, JWT, token dài trộn vào văn bản bất kỳ đều biến mất; UTF-8 hợp lệ.
func TestScrubTextProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		secrets := rapid.SliceOfN(rapid.OneOf(
			rapid.StringMatching(`0[0-9]{9}`),
			rapid.StringMatching(`0[0-9]{3}[ .-][0-9]{3}[ .-][0-9]{3}`),
			rapid.StringMatching(`[a-z0-9._]{1,8}@[a-z0-9]{1,8}\.(vn|com)`),
			rapid.StringMatching(`eyJ[A-Za-z0-9_-]{8,20}\.[A-Za-z0-9_-]{8,20}\.[A-Za-z0-9_-]{8,20}`),
			rapid.StringMatching(`[a-z]{4}[0-9]{4}[A-Za-z0-9]{16}`),
		), 1, 4).Draw(t, "secrets")
		var b strings.Builder
		for i, s := range secrets {
			b.WriteString(rapid.String().Draw(t, fmt.Sprint("noise", i)))
			b.WriteString(" ")
			b.WriteString(s)
			b.WriteString(" ")
		}
		got := logging.ScrubText(b.String())
		for _, s := range secrets {
			if strings.Contains(got, s) {
				t.Fatalf("còn %q trong %q", s, got)
			}
		}
		if phoneLike.MatchString(got) || emailLike.MatchString(got) {
			t.Fatalf("còn SĐT hoặc email: %q", got)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("không còn là UTF-8 hợp lệ: %q", got)
		}
	})
}

func TestErrorTextAndErr(t *testing.T) {
	err := fmt.Errorf("lưu khách: %w", fmt.Errorf("sđt %s, địa chỉ %s, thẻ %s: %w", fakePhone, fakeAddress, fakeToken, errors.New("hỏng")))
	if got := logging.ErrorText(nil); got != "" {
		t.Fatalf("ErrorText(nil) = %q", got)
	}
	for _, got := range []string{logging.ErrorText(err), logging.Err(err).Value.String()} {
		assertClean(t, got)
		if !strings.Contains(got, "lưu khách") || !strings.Contains(got, "hỏng") {
			t.Fatalf("mất phần không nhạy cảm: %q", got)
		}
	}
	if a := logging.Err(err); a.Key != "error" {
		t.Fatalf("Err key = %q, muốn error", a.Key)
	}
}

// Logger của CRM tự lọc nội dung lỗi dù mã ghi thẳng error hay err.Error() dưới khóa error: lớp chặn thứ hai.
func TestLoggerScrubsErrorValues(t *testing.T) {
	var buf bytes.Buffer
	l, closer, err := logging.NewLogger("", string(config.RoleAPI), &buf)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closer.Close() }()
	e := fmt.Errorf("sđt %s, địa chỉ %s, thẻ %s", fakePhone, fakeAddress, fakeToken)
	l.Error("a", slog.Any("error", e))
	l.Error("b", slog.Any("cause", e))
	l.Error("c", slog.String("db_error", e.Error()))
	l.Error("d", slog.Any("nested", map[string]any{"why": e}))
	out := buf.String()
	assertClean(t, out)
	if strings.Count(out, "\n") != 4 {
		t.Fatalf("muốn 4 dòng log, có:\n%s", out)
	}
}

func assertClean(t *testing.T, s string) {
	t.Helper()
	for _, bad := range []string{fakePhone, "Nguyễn Trãi", "Hà Nội", fakeToken, "eyJhbGciOiJIUzI1NiJ9"} {
		if strings.Contains(s, bad) {
			t.Fatalf("còn %q trong %q", bad, s)
		}
	}
}
