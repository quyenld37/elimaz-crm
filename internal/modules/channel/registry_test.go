package channel_test

import (
	"errors"
	"fmt"
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

// Task 149: vùng đăng ký webhook có hai lệnh (đăng ký, hủy đăng ký); Task 150: sức khỏe kênh thêm ba lệnh chỉ đọc và
// bốn lịch (mỗi lịch một việc nền cùng Kind; channel.graph_version không theo tenant).
func TestWiringCommands(t *testing.T) {
	t.Parallel()
	var names []string
	for _, h := range channel.CommandHandlers(channel.Deps{}) {
		names = append(names, h.Kind().Name)
	}
	if !slices.Equal(names, []string{
		channel.SubscribePageKind, channel.UnsubscribePageKind, channel.HealthCheckKind,
		channel.BackfillKind, channel.CommentPollKind,
	}) {
		t.Errorf("lệnh của channel = %v", names)
	}
	var sch, jobs []string
	for _, s := range channel.Schedules(channel.Deps{}) {
		sch = append(sch, fmt.Sprintf("%s %s %v %v", s.Name, s.Kind.Name, s.Every, s.PerTenant))
	}
	for _, h := range channel.JobHandlers(channel.Deps{}) {
		jobs = append(jobs, h.Kind().Name)
	}
	want := []string{
		"channel.health task.channel.health 1h0m0s true", "channel.silence task.channel.silence 5m0s true",
		"channel.backfill task.channel.backfill 10m0s true", "channel.graph_version task.channel.graph_version 24h0m0s false",
	}
	if !slices.Equal(sch, want) || !slices.Equal(jobs, []string{
		"task.channel.health", "task.channel.silence",
		"task.channel.backfill", "task.channel.graph_version",
	}) {
		t.Errorf("lịch = %v, việc nền = %v", sch, jobs)
	}
}
