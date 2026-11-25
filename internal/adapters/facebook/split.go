package facebook

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// ErrBadPacket báo thân webhook không phải gói JSON đọc được (khung ngoài sai). Không mang nội dung thân.
var ErrBadPacket = errors.New("facebook: thân webhook không đọc được")

// labelPattern là tên loại được dùng làm nhãn Skipped (nhãn chỉ số inbound_skipped): không bao giờ là nội dung.
var labelPattern = regexp.MustCompile(`^[a-z0-9_]{1,30}$`)

// label trả s khi s là tên loại hợp lệ, không thì "other".
func label(s string) string {
	if labelPattern.MatchString(s) {
		return s
	}
	return "other"
}

// Nhãn Skipped cố định.
const (
	skipInvalid    = "invalid"
	skipStandby    = "standby"
	keyMessaging   = "messaging"
	fieldFeed      = "feed"
	fieldLive      = "live_videos"
	itemComment    = "comment"
	commentSkipPfx = "comment_"
)

// Khung gói webhook của Meta: {"object":"page","entry":[{"id":<Page ID>,"time":<ms>,"messaging":[…],"changes":[…]}]}.
type packet struct {
	Object string            `json:"object"`
	Entry  []json.RawMessage `json:"entry"`
}

type entry struct {
	ID        string            `json:"id"`
	Time      int64             `json:"time"`
	Messaging []json.RawMessage `json:"messaging"`
	Changes   []json.RawMessage `json:"changes"`
	Standby   []json.RawMessage `json:"standby"`
}

// splitter gom kết quả tách một gói.
type splitter struct {
	res webhooks.SplitResult
}

func (s *splitter) skip(kind string) { s.res.Skipped[kind]++ }

func (s *splitter) add(typ, dedupe, order, page string, at time.Time, payload any) {
	b, err := json.Marshal(payload)
	if err != nil { // các kiểu payload của channel luôn mã hóa được; phòng thủ
		s.skip(typ + ":" + skipInvalid)
		return
	}
	s.res.Events = append(s.res.Events, webhooks.InboundEvent{
		Type: typ, DedupeKey: dedupe, OrderKey: order, OccurredAt: at, Payload: b,
		Route: webhooks.Route{Kind: RouteKind, ExternalKey: page},
	})
}

// Split tách một gói webhook thành các sự kiện chuẩn hóa của channel (tuyến fb_page theo Page ID của từng mục).
// object khác "page" → mọi mục vào Skipped["object:<object>"]; loại lạ → Skipped["<field>:<item>"] (hay
// "messaging:<loại>", "standby"); mục sai kiểu hay thiếu mã → Skipped["<loại>:invalid"], các mục khác vẫn tách (tệp
// task 147, Quyết định 4). Thân không phải gói JSON → ErrBadPacket. Thuần: cùng thân luôn ra cùng sự kiện, cùng khóa.
func (*Channel) Split(raw []byte) (webhooks.SplitResult, error) {
	var p packet
	if err := json.Unmarshal(raw, &p); err != nil {
		return webhooks.SplitResult{}, ErrBadPacket
	}
	s := &splitter{res: webhooks.SplitResult{Skipped: map[string]int{}}}
	if p.Object != "page" {
		s.res.Skipped["object:"+label(p.Object)] += max(1, len(p.Entry))
		return s.res, nil
	}
	for _, rawEntry := range p.Entry {
		var e entry
		if err := json.Unmarshal(rawEntry, &e); err != nil || e.ID == "" {
			s.skip("entry:" + skipInvalid)
			continue
		}
		for _, m := range e.Messaging {
			s.messaging(e, m)
		}
		for _, c := range e.Changes {
			s.change(e, c)
		}
		if len(e.Standby) > 0 {
			s.res.Skipped[skipStandby] += len(e.Standby)
		}
	}
	return s.res, nil
}

// secondsBelow: mốc thời gian nhỏ hơn số này là giây (trước năm 5138 tính theo giây; theo mili giây là trước tháng
// 3/1973, không có sự kiện nào thật).
const secondsBelow = 100_000_000_000

// ms đổi mốc thời gian của Meta sang giờ UTC. timestamp của messaging[] và watermark là mili giây; entry.time là mili
// giây ở gói messaging nhưng là giây ở gói changes[] (feed, live_videos): nhận cả hai, số nhỏ hơn secondsBelow là giây.
func ms(v int64) time.Time {
	if v < secondsBelow {
		return time.Unix(v, 0).UTC()
	}
	return time.UnixMilli(v).UTC()
}

// textKey là 8 ký tự hex đầu của sha256(text): phân biệt hai lần sửa comment mà khóa không mang nội dung.
func textKey(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:8]
}

// unknownKey trả tên khóa lạ đầu tiên (theo thứ tự chữ) của một mục, bỏ các khóa chung known.
func unknownKey(raw json.RawMessage, known ...string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return skipInvalid
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		if !slices.Contains(known, k) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return skipInvalid
	}
	slices.Sort(keys)
	return label(keys[0])
}

// flexTime là created_time của Meta: số giây Unix (webhook) hoặc chuỗi giờ ISO (Graph API).
type flexTime struct{ t time.Time }

func (f *flexTime) UnmarshalJSON(b []byte) error {
	if n, err := strconv.ParseInt(string(b), 10, 64); err == nil {
		f.t = time.Unix(n, 0).UTC()
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err //nolint:wrapcheck // lỗi giải mã của json, Split chỉ đếm invalid
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			f.t = t.UTC()
			return nil
		}
	}
	return ErrBadPacket
}

// commentVerb chuẩn hóa động từ comment của Meta (tệp task 147, Quyết định 5); ok = false khi không xử lý.
func commentVerb(v string) (string, bool) {
	switch v {
	case "add", "hide", "unhide":
		return v, true
	case "edit", "edited", "update":
		return "edited", true
	case "remove", "delete":
		return "remove", true
	}
	return "", false
}

// change tách một mục changes[] (feed, live_videos).
func (s *splitter) change(e entry, raw json.RawMessage) {
	var c struct {
		Field string          `json:"field"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		s.skip("changes:" + skipInvalid)
		return
	}
	field := label(c.Field)
	switch c.Field {
	case fieldFeed:
		s.feed(e, c.Value)
	case fieldLive:
		s.live(e, c.Value)
	default:
		var v struct {
			Item string `json:"item"`
		}
		_ = json.Unmarshal(c.Value, &v) // value không phải object: item rỗng → "other"
		s.skip(field + ":" + label(v.Item))
	}
}

type feedValue struct {
	Item      string `json:"item"`
	Verb      string `json:"verb"`
	CommentID string `json:"comment_id"`
	PostID    string `json:"post_id"`
	ParentID  string `json:"parent_id"`
	From      struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"from"`
	Message     string   `json:"message"`
	Photo       string   `json:"photo"`
	CreatedTime flexTime `json:"created_time"`
}

// feed tách thay đổi feed: chỉ comment (comment.changed); bài, cảm xúc… vào Skipped["feed:<item>"].
func (s *splitter) feed(e entry, raw json.RawMessage) {
	var v feedValue
	if err := json.Unmarshal(raw, &v); err != nil {
		s.skip(fieldFeed + ":" + skipInvalid)
		return
	}
	if v.Item != itemComment {
		s.skip(fieldFeed + ":" + label(v.Item))
		return
	}
	verb, ok := commentVerb(v.Verb)
	if !ok {
		s.skip(fieldFeed + ":" + label(commentSkipPfx+v.Verb))
		return
	}
	at := v.CreatedTime.t
	if at.IsZero() && e.Time > 0 {
		at = ms(e.Time)
	}
	if v.CommentID == "" || v.PostID == "" || at.IsZero() {
		s.skip(fieldFeed + ":" + skipInvalid)
		return
	}
	s.add(channel.EventCommentChanged, "c:"+v.CommentID+":"+verb+":"+textKey(v.Message),
		"fbc:"+e.ID+":"+v.PostID+":"+v.From.ID, e.ID, at, channel.CommentPayload{
			PageID: e.ID, PostID: v.PostID, CommentID: v.CommentID, ParentID: v.ParentID, FromID: v.From.ID,
			FromName: v.From.Name, Text: v.Message, PhotoURL: v.Photo, Verb: verb,
			FromPage: v.From.ID == e.ID, IsHidden: verb == "hide", CreatedAt: at,
		})
}

// live tách trạng thái phiên live (live.status); Status viết hoa như Meta trả ở Graph API (LIVE, LIVE_STOPPED…).
func (s *splitter) live(e entry, raw json.RawMessage) {
	var v struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &v); err != nil || v.ID == "" || v.Status == "" || e.Time <= 0 {
		s.skip(fieldLive + ":" + skipInvalid)
		return
	}
	status := strings.ToUpper(v.Status)
	s.add(channel.EventLiveStatus, "l:"+v.ID+":"+status, "fbl:"+e.ID+":"+v.ID, e.ID, ms(e.Time),
		channel.LiveStatusPayload{PageID: e.ID, LiveVideoID: v.ID, Status: status, At: ms(e.Time)})
}

type idRef struct {
	ID string `json:"id"`
}

type fbAttachment struct {
	Type    string `json:"type"`
	Payload *struct {
		URL       string      `json:"url"`
		StickerID json.Number `json:"sticker_id"`
		Title     string      `json:"title"`
	} `json:"payload"`
}

type fbMessage struct {
	MID        string      `json:"mid"`
	Text       string      `json:"text"`
	IsEcho     bool        `json:"is_echo"`
	AppID      json.Number `json:"app_id"`
	Metadata   string      `json:"metadata"`
	QuickReply *struct {
		Payload string `json:"payload"`
	} `json:"quick_reply"`
	ReplyTo *struct {
		MID string `json:"mid"`
	} `json:"reply_to"`
	Attachments []fbAttachment `json:"attachments"`
}

type fbPolicy struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

// messagingItem là một mục messaging[] của Meta; đúng một trong các trường con có giá trị.
type messagingItem struct {
	Sender    idRef      `json:"sender"`
	Recipient idRef      `json:"recipient"`
	Timestamp int64      `json:"timestamp"`
	Message   *fbMessage `json:"message"`
	Postback  *struct {
		Title   string `json:"title"`
		Payload string `json:"payload"`
		MID     string `json:"mid"`
	} `json:"postback"`
	Delivery *struct {
		MIDs      []string `json:"mids"`
		Watermark int64    `json:"watermark"`
	} `json:"delivery"`
	Read *struct {
		Watermark int64 `json:"watermark"`
	} `json:"read"`
	Reaction *struct {
		MID      string `json:"mid"`
		Action   string `json:"action"`
		Reaction string `json:"reaction"`
	} `json:"reaction"`
	MessageEdit *struct {
		MID  string `json:"mid"`
		Text string `json:"text"`
	} `json:"message_edit"`
	Referral *json.RawMessage `json:"referral"`
	// Tài liệu Meta ghi "policy-enforcement"; nhận cả "policy_enforcement".
	Policy    *fbPolicy `json:"policy-enforcement"`
	PolicyAlt *fbPolicy `json:"policy_enforcement"`
}

// msgCtx là phần chung của một mục messaging đã kiểm: Page, người dùng (PSID), khóa thứ tự, giờ.
type msgCtx struct {
	page, user, order string
	at                time.Time
	ts                int64
}

// messaging tách một mục messaging[]; mục thiếu người dùng, mã tin hay giờ → Skipped["messaging:invalid"].
func (s *splitter) messaging(e entry, raw json.RawMessage) {
	var m messagingItem
	if err := json.Unmarshal(raw, &m); err != nil {
		s.skip(keyMessaging + ":" + skipInvalid)
		return
	}
	ts := m.Timestamp
	if ts <= 0 {
		ts = e.Time
	}
	if p := firstNonNil(m.Policy, m.PolicyAlt); p != nil {
		if ts <= 0 {
			s.skip(keyMessaging + ":" + skipInvalid)
			return
		}
		tsStr := strconv.FormatInt(ts, 10)
		s.add(channel.EventPolicyEnforcement, "p:"+e.ID+":"+tsStr, "fbp:"+e.ID, e.ID, ms(ts),
			channel.PolicyPayload{PageID: e.ID, Action: p.Action, Reason: p.Reason})
		return
	}
	user := m.Sender.ID
	if m.Message != nil && m.Message.IsEcho {
		user = m.Recipient.ID // echo: Page gửi cho khách
	}
	if user == "" || ts <= 0 {
		s.skip(keyMessaging + ":" + skipInvalid)
		return
	}
	c := msgCtx{page: e.ID, user: user, order: "fb:" + e.ID + ":" + user, at: ms(ts), ts: ts}
	if !s.messagingEvent(c, m) {
		kind := unknownKey(raw, "sender", "recipient", "timestamp")
		if m.Message != nil || m.Postback != nil || m.Delivery != nil || m.Read != nil || m.Reaction != nil ||
			m.MessageEdit != nil {
			kind = skipInvalid // loại biết nhưng thiếu mã
		}
		s.skip(keyMessaging + ":" + kind)
	}
}

// firstNonNil trả con trỏ khác nil đầu tiên.
func firstNonNil[T any](a, b *T) *T {
	if a != nil {
		return a
	}
	return b
}

// messagingEvent ghi sự kiện của mục m; false khi mục không thuộc loại nào biết hay thiếu mã.
func (s *splitter) messagingEvent(c msgCtx, m messagingItem) bool {
	tsStr := strconv.FormatInt(c.ts, 10)
	switch {
	case m.Message != nil:
		return s.message(c, m.Message)
	case m.Postback != nil:
		pb := m.Postback
		dedupe := "m:" + pb.MID
		if pb.MID == "" {
			dedupe = "b:" + c.user + ":" + tsStr
		}
		s.add(channel.EventMessageReceived, dedupe, c.order, c.page, c.at, channel.MessagePayload{
			PageID: c.page, UserID: c.user, MID: pb.MID, Text: pb.Title, PostbackPayload: pb.Payload, SentAt: c.at,
		})
	case m.Delivery != nil:
		return s.watermark(c, channel.EventMessageDelivered, "d:", m.Delivery.Watermark, m.Delivery.MIDs)
	case m.Read != nil:
		return s.watermark(c, channel.EventMessageRead, "r:", m.Read.Watermark, nil)
	case m.Reaction != nil:
		x := m.Reaction
		if x.MID == "" || x.Action == "" {
			return false
		}
		s.add(channel.EventMessageReaction, "x:"+x.MID+":"+x.Reaction+":"+x.Action+":"+tsStr, c.order, c.page, c.at,
			channel.ReactionPayload{PageID: c.page, UserID: c.user, MID: x.MID, Reaction: x.Reaction, Action: x.Action})
	case m.MessageEdit != nil:
		ed := m.MessageEdit
		if ed.MID == "" {
			return false
		}
		s.add(channel.EventMessageEdited, "e:"+ed.MID+":"+tsStr, c.order, c.page, c.at,
			channel.EditPayload{PageID: c.page, UserID: c.user, MID: ed.MID, Text: ed.Text, EditedAt: c.at})
	case m.Referral != nil:
		s.add(channel.EventMessageReferral, "f:"+c.user+":"+tsStr, c.order, c.page, c.at,
			channel.MessagePayload{PageID: c.page, UserID: c.user, SentAt: c.at})
	default:
		return false
	}
	return true
}

// message ghi tin khách gửi (message.received) hay echo của Page (message.echo); thiếu mid → false.
func (s *splitter) message(c msgCtx, msg *fbMessage) bool {
	if msg.MID == "" {
		return false
	}
	p := channel.MessagePayload{
		PageID: c.page, UserID: c.user, MID: msg.MID, Text: msg.Text, IsEcho: msg.IsEcho, SentAt: c.at,
		Attachments: attachments(msg.Attachments),
	}
	if msg.QuickReply != nil {
		p.QuickReplyPayload = msg.QuickReply.Payload
	}
	if msg.ReplyTo != nil {
		p.ReplyToMID = msg.ReplyTo.MID
	}
	typ := channel.EventMessageReceived
	if msg.IsEcho {
		typ, p.EchoAppID, p.Metadata = channel.EventMessageEcho, msg.AppID.String(), msg.Metadata
	}
	s.add(typ, "m:"+msg.MID, c.order, c.page, c.at, p)
	return true
}

// watermark ghi "đã nhận"/"đã xem" tới mốc wm (mili giây); wm không dương → false.
func (s *splitter) watermark(c msgCtx, typ, prefix string, wm int64, mids []string) bool {
	if wm <= 0 {
		return false
	}
	s.add(typ, prefix+c.user+":"+strconv.FormatInt(wm, 10), c.order, c.page, c.at,
		channel.WatermarkPayload{PageID: c.page, UserID: c.user, MIDs: mids, Watermark: ms(wm)})
	return true
}

// attachments chuẩn hóa tệp đính kèm: ảnh có sticker_id là sticker; loại khác image, video, audio, file thành
// fallback (mẫu, vị trí, chia sẻ…).
func attachments(in []fbAttachment) []channel.AttachmentRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]channel.AttachmentRef, 0, len(in))
	for _, a := range in {
		r := channel.AttachmentRef{Type: a.Type}
		if a.Payload != nil {
			r.URL, r.StickerID, r.Name = a.Payload.URL, a.Payload.StickerID.String(), a.Payload.Title
		}
		switch {
		case a.Type == "image" && r.StickerID != "":
			r.Type = "sticker"
		case a.Type == "image", a.Type == "video", a.Type == "audio", a.Type == "file":
		default:
			r.Type = "fallback"
		}
		out = append(out, r)
	}
	return out
}
