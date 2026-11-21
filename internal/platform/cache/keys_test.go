package cache_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/cache"
)

func TestSubjectKey(t *testing.T) {
	s1 := []byte("khoa-bi-mat-thu-nhat-0123456789abcdef")
	s2 := []byte("khoa-bi-mat-thu-hai-0123456789abcdef")
	k := cache.SubjectKey(s1, "phone", "0912345678")
	if strings.Contains(k, "0912345678") || strings.Contains(k, "912345678") {
		t.Fatalf("khóa chứa SĐT gốc: %q", k)
	}
	if k != cache.SubjectKey(s1, "phone", "0912345678") {
		t.Fatal("gọi hai lần ra hai chuỗi khác nhau")
	}
	if k == cache.SubjectKey(s2, "phone", "0912345678") {
		t.Fatal("khóa bí mật khác mà ra cùng chuỗi")
	}
	kind, hash, ok := strings.Cut(k, ":")
	if !ok || kind != "phone" || len(hash) != 32 || strings.Trim(hash, "0123456789abcdef") != "" {
		t.Fatalf("dạng khóa %q: muốn phone:<32 ký tự hex>", k)
	}
	// Phân cách kind và value: ("ab", "c") khác ("a", "bc") dù ghép chuỗi giống nhau.
	if cache.SubjectKey(s1, "ab", "c")[3:] == cache.SubjectKey(s1, "a", "bc")[2:] {
		t.Fatal("kind và value không được phân cách trong phần băm")
	}
}

func TestEntityKey(t *testing.T) {
	tenant := uuid.MustParse("6f1c2a2e-5b5e-4d4b-9f8a-0d3c1e2f4a5b")
	id := uuid.MustParse("0b7f5d7e-2c1a-4e3b-8d9c-1a2b3c4d5e6f").String()
	got, err := cache.EntityKey("c", tenant, "product", id)
	if err != nil || got != "c:"+tenant.String()+":product:"+id {
		t.Fatalf("EntityKey = %q, %v", got, err)
	}
	long := strings.Repeat("a", 101)
	for _, parts := range [][]string{
		{"product", "a b"},
		{"product", ""},
		{"product", "x:y"},
		{"product", "*"},
		{"product", long},
		{"product", "số"},
		{"product", "a\nb"},
	} {
		if _, err := cache.EntityKey("c", tenant, parts...); !errors.Is(err, cache.ErrBadKey) {
			t.Errorf("EntityKey(%q) lỗi = %v, muốn ErrBadKey", parts, err)
		}
	}
	if _, err := cache.EntityKey("c:x", tenant, "product"); !errors.Is(err, cache.ErrBadKey) {
		t.Errorf("tiền tố sai: lỗi = %v, muốn ErrBadKey", err)
	}
	if _, err := cache.EntityKey("c", tenant, strings.Repeat("a", 100)); err != nil {
		t.Errorf("phần dài 100: %v", err)
	}
}
