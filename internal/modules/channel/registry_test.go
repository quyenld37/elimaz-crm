package channel_test

import (
	"errors"
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// stubAdapter là adapter tối thiểu cho test bộ adapter: chỉ Key có nghĩa.
type stubAdapter struct {
	channel.MessagingAdapter
	key string
}

func (s stubAdapter) Key() string { return s.key }

// NewRegistry (Task 143): bỏ adapter nil (kênh chưa bật), Keys theo thứ tự chữ, Get đúng adapter; hai adapter cùng khóa
// → ErrDuplicateAdapter.
func TestRegistry(t *testing.T) {
	t.Parallel()
	r, err := channel.NewRegistry(stubAdapter{key: "zz"}, nil, stubAdapter{key: "facebook"})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Keys(); !slices.Equal(got, []string{"facebook", "zz"}) {
		t.Errorf("Keys = %v", got)
	}
	if a, ok := r.Get("facebook"); !ok || a.Key() != "facebook" {
		t.Errorf("Get(facebook) = %v, %v", a, ok)
	}
	if _, ok := r.Get("zalo"); ok {
		t.Error("Get(zalo) phải không có")
	}
	if _, err := channel.NewRegistry(stubAdapter{key: "a"}, stubAdapter{key: "a"}); !errors.Is(err, channel.ErrDuplicateAdapter) {
		t.Errorf("trùng khóa: %v", err)
	}
	empty, err := channel.NewRegistry()
	if err != nil || len(empty.Keys()) != 0 {
		t.Errorf("bộ rỗng: %v %v", empty.Keys(), err)
	}
}

// Task 149: vùng đăng ký webhook có đúng hai lệnh (đăng ký, hủy đăng ký); chưa vùng nào có lịch.
func TestWiringCommands(t *testing.T) {
	t.Parallel()
	var names []string
	for _, h := range channel.CommandHandlers(channel.Deps{}) {
		names = append(names, h.Kind().Name)
	}
	if !slices.Equal(names, []string{channel.SubscribePageKind, channel.UnsubscribePageKind}) || len(channel.Schedules(channel.Deps{})) != 0 {
		t.Errorf("lệnh của channel = %v, lịch = %d", names, len(channel.Schedules(channel.Deps{})))
	}
}
