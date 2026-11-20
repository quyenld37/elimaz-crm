package background_test

import (
	"errors"
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/platform/background"
)

func TestCollect(t *testing.T) {
	if got := background.Collect([]int{1}, nil, []int{2, 3}, []int{}); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("mong [1 2 3], có %v", got)
	}
	if got := background.Collect[int](); len(got) != 0 {
		t.Fatalf("không phần nào: mong rỗng, có %v", got)
	}
}

func TestUniqueNames(t *testing.T) {
	id := func(s string) string { return s }
	if err := background.UniqueNames("lệnh", []string{"a", "b", "c"}, id); err != nil {
		t.Fatalf("không trùng: %v", err)
	}
	err := background.UniqueNames("lệnh", []string{"a", "b", "a"}, id)
	if !errors.Is(err, background.ErrDuplicatePart) || err.Error() != `lệnh "a": background: hai phần chạy nền trùng tên` {
		t.Fatalf("trùng a: mong ErrDuplicatePart nêu tên, có %v", err)
	}
}
