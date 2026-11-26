package facebook_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

const (
	page1, page2 = "104000000000001", "104000000000002"
	user1, user2 = "7000000000000011", "7000000000000022"
	post1        = page1 + "_900000000000001"
	crmAppID     = "1626301872631379"
	suiteAppID   = "263902037430900"
)

// want là một sự kiện mong đợi: loại, khóa chống trùng, khóa thứ tự, Page, giờ.
type want struct {
	Type, Dedupe, Order, Page string
	At                        time.Time
}

func ms(v int64) time.Time { return time.UnixMilli(v).UTC() }

func textKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

// fixtureWants là bảng mong đợi cho mọi fixture (contract (1)); Skipped mong đợi rỗng trừ khi ghi.
func fixtureWants() map[string][]want {
	fb1 := "fb:" + page1 + ":" + user1
	fbc := "fbc:" + page1 + ":" + post1 + ":" + user2
	return map[string][]want{
		"message_text.json":        {{channel.EventMessageReceived, "m:m_text_0001", fb1, page1, ms(1763632800123)}},
		"message_quick_reply.json": {{channel.EventMessageReceived, "m:m_qr_0001", fb1, page1, ms(1763632801123)}},
		"message_attachments.json": {{channel.EventMessageReceived, "m:m_att_0001", fb1, page1, ms(1763632802123)}},
		"postback.json":            {{channel.EventMessageReceived, "m:m_pb_0001", fb1, page1, ms(1763632803123)}},
		"echo_crm.json":            {{channel.EventMessageEcho, "m:m_echo_crm_0001", fb1, page1, ms(1763632804123)}},
		"echo_business_suite.json": {{channel.EventMessageEcho, "m:m_echo_bs_0001", fb1, page1, ms(1763632805123)}},
		"delivery.json":            {{channel.EventMessageDelivered, "d:" + user1 + ":1763632806000", fb1, page1, ms(1763632806123)}},
		"read.json":                {{channel.EventMessageRead, "r:" + user1 + ":1763632807000", fb1, page1, ms(1763632807123)}},
		"reaction.json": {{
			channel.EventMessageReaction, "x:m_echo_crm_0001:love:react:1763632808123", fb1, page1,
			ms(1763632808123),
		}},
		"message_edit.json": {{channel.EventMessageEdited, "e:m_text_0001:1763632809123", fb1, page1, ms(1763632809123)}},
		"referral.json":     {{channel.EventMessageReferral, "f:" + user1 + ":1763632810123", fb1, page1, ms(1763632810123)}},
		"policy_enforcement.json": {{
			channel.EventPolicyEnforcement, "p:" + page1 + ":1763632811123", "fbp:" + page1, page1,
			ms(1763632811123),
		}},
		"comment_add.json": {{
			channel.EventCommentChanged,
			"c:900000000000001_800000000000001:add:" + textKey("Chốt 1 áo size M"), fbc, page1, time.Unix(1763632812, 0).UTC(),
		}},
		"comment_edited.json": {{
			channel.EventCommentChanged,
			"c:900000000000001_800000000000001:edited:" + textKey("Chốt 2 áo size M"), fbc, page1, time.Unix(1763632812, 0).UTC(),
		}},
		"comment_page.json": {{
			channel.EventCommentChanged,
			"c:900000000000001_800000000000002:add:" + textKey("Shop đã inbox chị ạ"), "fbc:" + page1 + ":" + post1 + ":" + page1,
			page1, time.Unix(1763632814, 0).UTC(),
		}},
		"comment_remove.json": {{
			channel.EventCommentChanged, "c:900000000000001_800000000000001:remove:" + textKey(""), fbc,
			page1, time.Unix(1763632815, 0).UTC(),
		}},
		"comment_hide.json": {{
			channel.EventCommentChanged,
			"c:900000000000001_800000000000001:hide:" + textKey("Chốt 2 áo size M"), fbc, page1, time.Unix(1763632816, 0).UTC(),
		}},
		"live_videos.json": {{
			channel.EventLiveStatus, "l:500000000000001:LIVE", "fbl:" + page1 + ":500000000000001", page1,
			time.Unix(1763632817, 0).UTC(),
		}},
		"batch_2pages_7events.json": {
			{channel.EventMessageReceived, "m:m_batch_0001", fb1, page1, ms(1763632820100)},
			{channel.EventMessageEcho, "m:m_batch_0002", fb1, page1, ms(1763632820200)},
			{channel.EventMessageRead, "r:" + user1 + ":1763632820250", fb1, page1, ms(1763632820300)},
			{
				channel.EventCommentChanged, "c:900000000000001_800000000000003:add:" + textKey("Chốt"), fbc, page1,
				time.Unix(1763632820, 0).UTC(),
			},
			{channel.EventMessageReceived, "m:m_batch_0003", "fb:" + page2 + ":" + user2, page2, ms(1763632821100)},
			{channel.EventMessageDelivered, "d:" + user2 + ":1763632821150", "fb:" + page2 + ":" + user2, page2, ms(1763632821100 + 100)},
			{
				channel.EventLiveStatus, "l:500000000000002:LIVE_STOPPED", "fbl:" + page2 + ":500000000000002", page2,
				ms(1763632821500),
			},
		},
	}
}

func split(t *testing.T, raw []byte) webhooks.SplitResult {
	t.Helper()
	res, err := newAdapter().Split(raw)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	return res
}

func got(res webhooks.SplitResult) []want {
	out := make([]want, 0, len(res.Events))
	for _, e := range res.Events {
		out = append(out, want{e.Type, e.DedupeKey, e.OrderKey, e.Route.ExternalKey, e.OccurredAt})
	}
	return out
}

// Contract (1), (2): mỗi fixture tách đúng sự kiện mong đợi (loại, khóa, Page, giờ), tuyến fb_page; tách hai lần ra
// cùng khóa; không fixture nào có mục bị bỏ qua.
func TestSplit_Fixtures(t *testing.T) {
	t.Parallel()
	for name, w := range fixtureWants() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkFixture(t, fixture(t, name), w)
		})
	}
}

func checkFixture(t *testing.T, raw []byte, w []want) {
	t.Helper()
	res := split(t, raw)
	if g := got(res); !slices.Equal(g, w) {
		t.Errorf("sự kiện\n got %+v\nwant %+v", g, w)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("Skipped = %v, mong rỗng", res.Skipped)
	}
	for _, e := range res.Events {
		if e.Route.Kind != facebook.RouteKind || !json.Valid(e.Payload) {
			t.Errorf("tuyến %q, payload %s", e.Route.Kind, e.Payload)
		}
	}
	again := split(t, raw)
	if !slices.Equal(got(again), got(res)) {
		t.Error("tách hai lần ra khóa khác nhau")
	}
	for i := range res.Events {
		if string(res.Events[i].Payload) != string(again.Events[i].Payload) {
			t.Errorf("sự kiện %d: payload khác giữa hai lần tách", i)
		}
	}
}

// Contract (3): loại lạ vào Skipped và được đếm, không lỗi; thân không phải JSON thì lỗi (việc tách dead theo M0).
func TestSplit_SkippedAndErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		raw  []byte
		want map[string]int
	}{
		"feed status": {fixture(t, "feed_status.json"), map[string]int{"feed:status": 1}},
		"object user": {fixture(t, "object_user.json"), map[string]int{"object:user": 1}},
		"messaging lạ, standby, field lạ": {
			[]byte(`{"object":"page","entry":[{"id":"1","time":1,` +
				`"messaging":[{"sender":{"id":"2"},"recipient":{"id":"1"},"timestamp":5,"optin":{"ref":"x"}}],` +
				`"standby":[{"sender":{"id":"2"}}],` +
				`"changes":[{"field":"ratings","value":{"item":"rating"}},{"field":"feed","value":{"item":"reaction","verb":"add"}},` +
				`{"field":"feed","value":{"item":"comment","verb":"block","comment_id":"c1","post_id":"p1","from":{"id":"2"}}}]}]}`),
			map[string]int{"messaging:optin": 1, "standby": 1, "ratings:rating": 1, "feed:reaction": 1, "feed:comment_block": 1},
		},
		"mục sai kiểu không làm mất mục khác": {
			[]byte(`{"object":"page","entry":[{"id":"1","time":1,"messaging":[` +
				`{"sender":{"id":"2"},"recipient":{"id":"1"},"timestamp":"x","message":{"mid":"m1"}},` +
				`{"sender":{"id":"2"},"recipient":{"id":"1"},"timestamp":7,"message":{"mid":"m2","text":"a"}},` +
				`{"sender":{"id":"2"},"recipient":{"id":"1"},"timestamp":8,"message":{"text":"không mid"}}],` +
				`"changes":[{"field":"feed","value":{"item":"comment","verb":"add"}},{"field":"feed","value":7}]}]}`),
			map[string]int{"messaging:invalid": 2, "feed:invalid": 2},
		},
		"entry không id": {
			[]byte(`{"object":"page","entry":[{"time":1,"messaging":[{"message":{"mid":"m"}}]}]}`),
			map[string]int{"entry:invalid": 1},
		},
		"tên loại lạ bị lọc": {[]byte(`{"object":"Pa ge<script>","entry":[]}`), map[string]int{"object:other": 1}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := split(t, c.raw)
			if !maps.Equal(res.Skipped, c.want) {
				t.Errorf("Skipped = %v, mong %v", res.Skipped, c.want)
			}
		})
	}
	res := split(t, cases["mục sai kiểu không làm mất mục khác"].raw)
	if len(res.Events) != 1 || res.Events[0].DedupeKey != "m:m2" {
		t.Errorf("mục đúng phải còn: %+v", got(res))
	}
	for _, bad := range []string{``, `{`, `[]`, `"x"`, `{"object":"page","entry":{}}`, `{"object":7}`} {
		if _, err := newAdapter().Split([]byte(bad)); err == nil {
			t.Errorf("thân %q phải lỗi", bad)
		} else if strings.Contains(err.Error(), "page") {
			t.Errorf("lỗi không được trích thân: %v", err)
		}
	}
}

func decodeOne[T any](t *testing.T, name string, v *T) *T {
	t.Helper()
	res := split(t, fixture(t, name))
	if len(res.Events) != 1 {
		t.Fatalf("%s: %d sự kiện", name, len(res.Events))
	}
	if err := json.Unmarshal(res.Events[0].Payload, v); err != nil {
		t.Fatal(err)
	}
	return v
}

// Payload chuẩn hóa của tin, echo, watermark, cảm xúc, sửa, chính sách.
func TestSplit_MessagePayloads(t *testing.T) {
	t.Parallel()
	m := *decodeOne(t, "message_text.json", new(channel.MessagePayload))
	if m.PageID != page1 || m.UserID != user1 || m.MID != "m_text_0001" || m.Text != "Shop ơi áo này còn size M không?" ||
		m.ReplyToMID != "m_prev_0000" || m.IsEcho || !m.SentAt.Equal(ms(1763632800123)) {
		t.Errorf("tin chữ: %+v", m)
	}
	if q := *decodeOne(t, "message_quick_reply.json", new(channel.MessagePayload)); q.QuickReplyPayload != "SIZE_CHART" ||
		q.Text != "Xem bảng size" {
		t.Errorf("trả lời nhanh: %+v", q)
	}
	att := decodeOne(t, "message_attachments.json", new(channel.MessagePayload)).Attachments
	wantAtt := []channel.AttachmentRef{
		{Type: "image", URL: "https://scontent.xx.fbcdn.net/v/t1/anh-ao.jpg"},
		{Type: "sticker", URL: "https://scontent.xx.fbcdn.net/v/t1/sticker.png", StickerID: "369239263222822"},
		{Type: "file", URL: "https://cdn.fbsbx.com/v/t59/don-hang.pdf", Name: "don-hang.pdf"},
		{Type: "fallback"},
	}
	if !slices.Equal(att, wantAtt) {
		t.Errorf("tệp đính kèm\n got %+v\nwant %+v", att, wantAtt)
	}
	if p := *decodeOne(t, "postback.json", new(channel.MessagePayload)); p.PostbackPayload != "GET_STARTED" || p.Text != "Bắt đầu" ||
		p.MID != "m_pb_0001" || p.UserID != user1 {
		t.Errorf("postback: %+v", p)
	}
	crm := *decodeOne(t, "echo_crm.json", new(channel.MessagePayload))
	if !crm.IsEcho || crm.EchoAppID != crmAppID || crm.Metadata != "0193a4b2-7c00-7000-8000-000000000001" ||
		crm.UserID != user1 || crm.PageID != page1 || crm.Text != "Dạ còn ạ" {
		t.Errorf("echo CRM: %+v", crm)
	}
	if bs := *decodeOne(t, "echo_business_suite.json", new(channel.MessagePayload)); !bs.IsEcho || bs.EchoAppID != suiteAppID ||
		bs.Metadata != "" || bs.UserID != user1 {
		t.Errorf("echo Business Suite: %+v", bs)
	}
}

// Payload chuẩn hóa của đã nhận, đã xem, cảm xúc, sửa, referral, chính sách.
func TestSplit_OtherMessagingPayloads(t *testing.T) {
	t.Parallel()
	if d := *decodeOne(t, "delivery.json", new(channel.WatermarkPayload)); d.UserID != user1 || d.PageID != page1 ||
		!slices.Equal(d.MIDs, []string{"m_echo_crm_0001"}) || !d.Watermark.Equal(ms(1763632806000)) {
		t.Errorf("đã nhận: %+v", d)
	}
	if r := *decodeOne(t, "read.json", new(channel.WatermarkPayload)); r.UserID != user1 || !r.Watermark.Equal(ms(1763632807000)) {
		t.Errorf("đã xem: %+v", r)
	}
	if x := *decodeOne(t, "reaction.json", new(channel.ReactionPayload)); x != (channel.ReactionPayload{
		PageID: page1, UserID: user1, MID: "m_echo_crm_0001", Reaction: "love", Action: "react",
	}) {
		t.Errorf("cảm xúc: %+v", x)
	}
	if e := *decodeOne(t, "message_edit.json", new(channel.EditPayload)); e.MID != "m_text_0001" || e.UserID != user1 ||
		e.Text != "Shop ơi áo này còn size L không?" || !e.EditedAt.Equal(ms(1763632809123)) {
		t.Errorf("sửa tin: %+v", e)
	}
	if f := *decodeOne(t, "referral.json", new(channel.MessagePayload)); f.UserID != user1 || f.PageID != page1 ||
		!f.SentAt.Equal(ms(1763632810123)) {
		t.Errorf("referral: %+v", f)
	}
	if p := *decodeOne(t, "policy_enforcement.json", new(channel.PolicyPayload)); p != (channel.PolicyPayload{
		PageID: page1, Action: "block", Reason: "The bot violated our Platform Policies",
	}) {
		t.Errorf("chính sách: %+v", p)
	}
	// Khóa policy_enforcement (gạch dưới) cũng nhận.
	alt := strings.ReplaceAll(string(fixture(t, "policy_enforcement.json")), "policy-enforcement", "policy_enforcement")
	if res := split(t, []byte(alt)); len(res.Events) != 1 || res.Events[0].Type != channel.EventPolicyEnforcement {
		t.Errorf("policy_enforcement: %+v", got(res))
	}
}

// Comment: động từ chuẩn hóa, FromPage, sửa hai lần khác nội dung → hai khóa, cùng nội dung → trùng; live.
func TestSplit_CommentAndLive(t *testing.T) {
	t.Parallel()
	c := *decodeOne(t, "comment_add.json", new(channel.CommentPayload))
	wantC := channel.CommentPayload{
		PageID: page1, PostID: post1, CommentID: "900000000000001_800000000000001", ParentID: post1, FromID: user2,
		FromName: "Khách Thử Hai", Text: "Chốt 1 áo size M", PhotoURL: "https://scontent.xx.fbcdn.net/v/t1/comment.jpg",
		Verb: "add", CreatedAt: time.Unix(1763632812, 0).UTC(),
	}
	if c != wantC {
		t.Errorf("comment\n got %+v\nwant %+v", c, wantC)
	}
	if p := *decodeOne(t, "comment_page.json", new(channel.CommentPayload)); !p.FromPage || p.ParentID != "900000000000001_800000000000001" {
		t.Errorf("comment của Page: %+v", p)
	}
	if c := *decodeOne(t, "comment_add.json", new(channel.CommentPayload)); c.FromPage {
		t.Error("comment của khách không được FromPage")
	}
	if h := *decodeOne(t, "comment_hide.json", new(channel.CommentPayload)); h.Verb != "hide" || !h.IsHidden {
		t.Errorf("ẩn: %+v", h)
	}
	if r := *decodeOne(t, "comment_remove.json", new(channel.CommentPayload)); r.Verb != "remove" || r.Text != "" {
		t.Errorf("xóa: %+v", r)
	}
	base := string(fixture(t, "comment_edited.json"))
	key := func(raw string) string {
		res := split(t, []byte(raw))
		if len(res.Events) != 1 {
			t.Fatalf("%d sự kiện", len(res.Events))
		}
		return res.Events[0].DedupeKey
	}
	first, second := key(base), key(strings.Replace(base, "Chốt 2 áo size M", "Chốt 3 áo size M", 1))
	if first == second {
		t.Error("sửa hai lần khác nội dung phải ra hai khóa")
	}
	if key(base) != first {
		t.Error("cùng nội dung phải trùng khóa")
	}
	for metaVerb, verb := range map[string]string{
		"add": "add", "edit": "edited", "edited": "edited", "update": "edited", "remove": "remove", "delete": "remove",
		"hide": "hide", "unhide": "unhide",
	} {
		raw := strings.Replace(base, `"verb":"edited"`, `"verb":"`+metaVerb+`"`, 1)
		res := split(t, []byte(raw))
		var p channel.CommentPayload
		if len(res.Events) != 1 || json.Unmarshal(res.Events[0].Payload, &p) != nil || p.Verb != verb ||
			!strings.HasPrefix(res.Events[0].DedupeKey, "c:900000000000001_800000000000001:"+verb+":") {
			t.Errorf("verb %s → %+v, mong %s", metaVerb, got(res), verb)
		}
	}
	l := *decodeOne(t, "live_videos.json", new(channel.LiveStatusPayload))
	if l != (channel.LiveStatusPayload{
		PageID: page1, LiveVideoID: "500000000000001", Status: "LIVE",
		At: time.Unix(1763632817, 0).UTC(),
	}) {
		t.Errorf("live: %+v", l)
	}
}

// entry.time của gói changes[] Meta gửi theo giây, của gói messaging theo mili giây: cả hai ra đúng giờ. Comment
// thiếu created_time lấy giờ của entry.
func TestSplit_EntryTimeUnits(t *testing.T) {
	t.Parallel()
	want := time.Date(2025, 11, 20, 10, 0, 17, 0, time.UTC)
	for name, entryTime := range map[string]string{"giây": "1763632817", "mili giây": "1763632817000"} {
		live := `{"object":"page","entry":[{"id":"1","time":` + entryTime +
			`,"changes":[{"field":"live_videos","value":{"id":"5","status":"LIVE_STOPPED"}}]}]}`
		comment := `{"object":"page","entry":[{"id":"1","time":` + entryTime + `,"changes":[{"field":"feed","value":` +
			`{"item":"comment","verb":"add","comment_id":"c1","post_id":"1_2","from":{"id":"9"},"message":"a"}}]}]}`
		for kind, raw := range map[string]string{"live": live, "comment": comment} {
			res := split(t, []byte(raw))
			if len(res.Events) != 1 || !res.Events[0].OccurredAt.Equal(want) {
				t.Errorf("%s, entry.time theo %s: %+v, mong %v", kind, name, got(res), want)
			}
		}
	}
}
