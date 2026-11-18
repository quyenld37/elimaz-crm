package urlerr_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/platform/urlerr"
)

func TestStripRemovesURL(t *testing.T) {
	inner := errors.New("connection refused")
	err := fmt.Errorf("gọi: %w", &url.Error{Op: "Post", URL: "https://api.telegram.org/botSECRET123/sendMessage", Err: inner})
	got := urlerr.Strip(err)
	if strings.Contains(got.Error(), "SECRET123") || strings.Contains(got.Error(), "telegram.org") {
		t.Fatalf("còn URL trong lỗi: %q", got)
	}
	if !errors.Is(got, inner) {
		t.Fatalf("mất lỗi gốc: %v", got)
	}
}

func TestStripKeepsDeadline(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "https://x.test/?token=abc", Err: context.DeadlineExceeded}
	if got := urlerr.Strip(err); !errors.Is(got, context.DeadlineExceeded) || strings.Contains(got.Error(), "token") {
		t.Fatalf("muốn còn DeadlineExceeded, không URL: %v", got)
	}
}

func TestStripPassesOtherErrors(t *testing.T) {
	plain := errors.New("khác")
	if got := urlerr.Strip(plain); got != plain { //nolint:errorlint // so đúng giá trị trả lại
		t.Fatalf("lỗi không có *url.Error phải giữ nguyên: %v", got)
	}
	if urlerr.Strip(nil) != nil {
		t.Fatal("nil phải trả nil")
	}
}
