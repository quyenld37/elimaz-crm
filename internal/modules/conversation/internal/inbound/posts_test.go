package inbound_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/inbound"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Loại bài từ status_type của kênh: video live → live; rỗng → unknown (giữ loại đã biết); còn lại → post.
func TestPostKind(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"": "unknown", "live": "live", "live_video": "live", "added_live_video": "live", "LIVE_VIDEO": "live",
		"added_video": "post", "mobile_status_update": "post", "added_photos": "post", "shared_story": "post",
	} {
		if got := inbound.PostKind(in); got != want {
			t.Errorf("PostKind(%q) = %q, mong %q", in, got, want)
		}
	}
}

// Bài không còn trên kênh: lệnh đọc bài xong (không dead), bài giữ unknown có mốc đọc, comment vẫn lưu. Lookup của lệnh chỉ đọc
// luôn trả not_sent (chạy lại không có tác dụng phụ). Payload lệnh chỉ có mã (không nội dung, token).
func TestFetchPostMissingAndLookup(t *testing.T) {
	t.Parallel()
	e := newEnvWith(t, fakegraphFor(t))
	e.sendComment(commentEv("add", post1, "c1", "", "u-7", "Lan", "Chốt 1 áo", 0))
	if _, ok := e.commentRow("c1"); !ok {
		t.Fatal("comment phải được lưu")
	}
	var status string
	var payload []byte
	if err := e.staff.Owner.Pool.QueryRow(t.Context(), `SELECT status, payload FROM platform.jobs WHERE kind = $1 AND tenant_id = $2`,
		inbound.FetchPostKind, e.tenant).Scan(&status, &payload); err != nil {
		t.Fatal(err)
	}
	if status != "done" {
		t.Errorf("lệnh đọc bài không có: %s, mong done", status)
	}
	var p map[string]any
	if err := json.Unmarshal(payload, &p); err != nil || len(p) != 3 || p["externalId"] != post1 {
		t.Errorf("payload lệnh: %s", payload)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.posts WHERE kind = 'unknown' AND permalink IS NULL AND fetched_at IS NOT NULL`); n != 1 {
		t.Errorf("bài unknown = %d", n)
	}
	e.noDeadJobs()
	var h commands.Handler
	for _, c := range inbound.Parts(e.deps).Commands {
		if c.Kind().Name == inbound.FetchPostKind {
			h = c
		}
	}
	lk, ok := h.(commands.Looker)
	if !ok {
		t.Fatal("lệnh đọc bài phải có Lookup")
	}
	if st, err := lk.Lookup(t.Context(), queue.Job{ID: uuid.New(), TenantID: &e.tenant, Payload: payload}); err != nil || st != commands.StatusNotSent {
		t.Errorf("Lookup = %q, %v; mong not_sent", st, err)
	}
}

// Kênh không đọc được bài (kênh giả không hiện thực PostFetcher): comment lưu, không xếp lệnh đọc bài.
func TestCommentFakechatNoFetch(t *testing.T) {
	t.Parallel()
	e := newEnvWith(t, fakegraphFor(t))
	e.enqueue(fake.Key, channel.EventCommentChanged, "fc:1", fakePage, "post-1:u-1", at(0), channel.CommentPayload{
		PageID: fakePage, PostID: "post-1", CommentID: "fc-1", ParentID: "post-1", FromID: "u-1", FromName: "Hoa",
		Text: "Còn hàng không?", Verb: "add", CreatedAt: at(0),
	})
	e.drain()
	e.noDeadJobs()
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE channel_account_id = $1 AND kind = 'comment'`, e.fakePage); n != 1 {
		t.Errorf("comment kênh giả = %d, mong 1", n)
	}
	if n := e.staff.Count(t, `SELECT count(*) FROM platform.jobs WHERE kind = $1`, inbound.FetchPostKind); n != 0 {
		t.Errorf("lệnh đọc bài của kênh giả = %d, mong 0", n)
	}
}
