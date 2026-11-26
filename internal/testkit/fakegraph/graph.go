package fakegraph

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// dispatch chọn đường theo đoạn đầu (mã đối tượng, "me", "debug_token") và cạnh.
func (s *Server) dispatch(w http.ResponseWriter, c *Call, ti *tokenInfo) {
	id, edge, _ := strings.Cut(c.Path, "/")
	switch {
	case id == "me" && edge == "accounts":
		s.meAccounts(w, ti)
	case id == "debug_token":
		s.debugToken(w, c)
	default:
		s.object(w, c, ti, id, edge)
	}
}

// object phục vụ {id} và {id}/{edge} theo loại của id trong trạng thái.
func (s *Server) object(w http.ResponseWriter, c *Call, ti *tokenInfo, id, edge string) {
	s.mu.Lock()
	_, isPage := s.st.pages[id]
	_, isComment := s.st.comments[id]
	_, isPost := s.st.posts[id]
	_, isLive := s.st.lives[id]
	_, isProfile := s.st.profiles[id]
	s.mu.Unlock()
	switch {
	case isPage:
		s.page(w, c, ti, id, edge)
	case isComment:
		s.comment(w, c, id, edge)
	case isLive:
		s.live(w, c, id, edge)
	case isPost && edge == "":
		s.post(w, id)
	case isPost && edge == "comments":
		s.listComments(w, id, func(*Comment) bool { return true })
	case isProfile && edge == "":
		s.profile(w, id)
	default:
		writeError(w, GraphErrorSpec{Code: 100, Subcode: 33, Type: "GraphMethodException", Message: "Unsupported get request. Object with ID '" + id + "' does not exist"})
	}
}

func notFound(w http.ResponseWriter, what string) {
	writeError(w, GraphErrorSpec{Code: 100, Type: "GraphMethodException", Message: "Unknown path components: /" + what})
}

func (s *Server) meAccounts(w http.ResponseWriter, ti *tokenInfo) {
	if ti.kind != tokenUser {
		writeError(w, GraphErrorSpec{Code: 100, Type: "OAuthException", Message: "(#100) Tried accessing nonexisting field (accounts)"})
		return
	}
	s.mu.Lock()
	data := []map[string]any{}
	for _, pid := range ti.user.PageIDs {
		if p := s.st.pages[pid]; p != nil {
			data = append(data, map[string]any{"id": p.ID, "name": p.Name, "access_token": p.Token, "tasks": pageTasks()})
		}
	}
	s.mu.Unlock()
	writeJSON(w, map[string]any{"data": data})
}

// pageTasks là vai trò của người dùng quản trị trên mọi Page của máy giả (me/accounts và đọc {page-id}).
func pageTasks() []string {
	return []string{"MANAGE", "MODERATE", "MESSAGING", "ANALYZE", "ADVERTISE", "CREATE_CONTENT"}
}

func (s *Server) debugToken(w http.ResponseWriter, c *Call) {
	in := c.Query.Get("input_token")
	s.mu.Lock()
	ti, ok := s.st.tokens[in]
	data := map[string]any{"app_id": s.o.AppID, "is_valid": ok && !ti.revoked, "expires_at": 0}
	if ok {
		switch ti.kind {
		case tokenPage:
			data["type"], data["profile_id"], data["scopes"] = "PAGE", ti.pageID, s.st.pages[ti.pageID].Scopes
		case tokenUser:
			data["type"], data["scopes"] = "USER", defaultScopes()
		}
	}
	s.mu.Unlock()
	writeJSON(w, map[string]any{"data": data})
}

// canPage báo token được dùng cho Page id: token của chính Page, hoặc token người dùng quản trị Page.
func canPage(ti *tokenInfo, id string) bool {
	switch ti.kind {
	case tokenPage:
		return ti.pageID == id
	case tokenUser:
		return slices.Contains(ti.user.PageIDs, id)
	}
	return false
}

func (s *Server) page(w http.ResponseWriter, c *Call, ti *tokenInfo, id, edge string) {
	if !canPage(ti, id) {
		writeError(w, GraphErrorSpec{Code: 200, Type: "OAuthException", Message: "(#200) Requires pages_messaging permission to manage the object"})
		return
	}
	switch edge {
	case "":
		s.mu.Lock()
		p := *s.st.pages[id]
		s.mu.Unlock()
		writeJSON(w, map[string]any{"id": p.ID, "name": p.Name, "access_token": p.Token, "tasks": pageTasks()})
	case "subscribed_apps":
		s.subscribedApps(w, c, id)
	case "messages":
		s.sendMessage(w, c, id)
	case "conversations":
		s.conversations(w, c, id)
	case "feed":
		s.feed(w, id)
	case "live_videos":
		s.liveVideos(w, id)
	default:
		notFound(w, edge)
	}
}

func (s *Server) subscribedApps(w http.ResponseWriter, c *Call, pageID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch c.Method {
	case http.MethodPost:
		fields := strings.Split(param(c, "subscribed_fields"), ",")
		s.st.subscribed[pageID] = slices.DeleteFunc(fields, func(f string) bool { return f == "" })
	case http.MethodDelete:
		delete(s.st.subscribed, pageID)
	default:
		data := []map[string]any{}
		if f, ok := s.st.subscribed[pageID]; ok {
			data = append(data, map[string]any{"id": s.o.AppID, "subscribed_fields": f})
		}
		writeJSON(w, map[string]any{"data": data})
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

// sendBody là thân POST {page-id}/messages mà CRM gửi (JSON).
type sendBody struct {
	Recipient struct {
		ID        string `json:"id"`
		CommentID string `json:"comment_id"`
	} `json:"recipient"`
	Message struct {
		Text     string `json:"text"`
		Metadata string `json:"metadata"`
	} `json:"message"`
}

func (s *Server) sendMessage(w http.ResponseWriter, c *Call, pageID string) {
	var b sendBody
	if c.Method != http.MethodPost || json.Unmarshal(c.Body, &b) != nil || (b.Recipient.ID == "" && b.Recipient.CommentID == "") {
		writeError(w, GraphErrorSpec{Code: 100, Type: "OAuthException", Message: "(#100) The parameter recipient is required"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	psid := b.Recipient.ID
	if cid := b.Recipient.CommentID; cid != "" {
		if s.st.privateUsed[cid] {
			writeError(w, GraphErrorSpec{Code: 10900, Type: "OAuthException", Message: "Activity already replied to"})
			return
		}
		s.st.privateUsed[cid] = true
		if cm := s.st.comments[cid]; cm != nil {
			psid = cm.FromID
		}
	}
	m := &Message{
		ID: s.st.nextID("m_"), PageID: pageID, PSID: psid, CommentID: b.Recipient.CommentID, Text: b.Message.Text,
		Metadata: b.Message.Metadata, FromPage: true, At: time.Now().UTC(), Body: c.Body,
	}
	s.st.messages = append(s.st.messages, m)
	writeJSON(w, map[string]any{"recipient_id": psid, "message_id": m.ID})
}

func (s *Server) conversations(w http.ResponseWriter, c *Call, pageID string) {
	psid := c.Query.Get("user_id")
	s.mu.Lock()
	msgs := []map[string]any{}
	for _, m := range slices.Backward(s.st.messages) {
		if m.PageID != pageID || (psid != "" && m.PSID != psid) {
			continue
		}
		from, to := m.PSID, pageID
		if m.FromPage {
			from, to = pageID, m.PSID
		}
		msgs = append(msgs, map[string]any{
			"id": m.ID, "message": m.Text, "created_time": m.At.Format(graphTime),
			"from": map[string]any{"id": from}, "to": map[string]any{"data": []map[string]any{{"id": to}}},
		})
	}
	s.mu.Unlock()
	data := []map[string]any{{"id": "t_" + psid, "messages": map[string]any{"data": msgs}}}
	writeJSON(w, map[string]any{"data": data})
}

func (s *Server) feed(w http.ResponseWriter, pageID string) {
	s.mu.Lock()
	data := []map[string]any{}
	for _, p := range s.st.posts {
		if p.PageID == pageID {
			data = append(data, postJSON(p))
		}
	}
	s.mu.Unlock()
	writeJSON(w, map[string]any{"data": data})
}

func postJSON(p *Post) map[string]any {
	return map[string]any{"id": p.ID, "message": p.Message, "permalink_url": p.PermalinkURL, "created_time": p.Created.Format(graphTime)}
}

func (s *Server) post(w http.ResponseWriter, id string) {
	s.mu.Lock()
	p := postJSON(s.st.posts[id])
	s.mu.Unlock()
	writeJSON(w, p)
}

func (s *Server) profile(w http.ResponseWriter, id string) {
	s.mu.Lock()
	p := *s.st.profiles[id]
	s.mu.Unlock()
	writeJSON(w, map[string]any{
		"id": p.PSID, "first_name": p.FirstName, "last_name": p.LastName,
		"name": strings.TrimSpace(p.FirstName + " " + p.LastName), "profile_pic": p.ProfilePic,
	})
}

func commentJSON(c *Comment) map[string]any {
	return map[string]any{
		"id": c.ID, "message": c.Message, "created_time": c.Created.Format(graphTime), "is_hidden": c.Hidden,
		"from": map[string]any{"id": c.FromID, "name": c.FromName},
	}
}

func (s *Server) comment(w http.ResponseWriter, c *Call, id, edge string) {
	switch {
	case edge == "" && c.Method == http.MethodPost:
		hidden, err := strconv.ParseBool(param(c, "is_hidden"))
		if err != nil {
			writeError(w, GraphErrorSpec{Code: 100, Type: "OAuthException", Message: "(#100) Param is_hidden must be a boolean"})
			return
		}
		s.mu.Lock()
		s.st.comments[id].Hidden = hidden
		s.mu.Unlock()
		writeJSON(w, map[string]any{"success": true})
	case edge == "":
		s.mu.Lock()
		out := commentJSON(s.st.comments[id])
		s.mu.Unlock()
		writeJSON(w, out)
	case edge == "comments" && c.Method == http.MethodPost:
		s.replyComment(w, c, id)
	case edge == "comments":
		s.listComments(w, id, func(*Comment) bool { return true })
	default:
		notFound(w, edge)
	}
}

func (s *Server) replyComment(w http.ResponseWriter, c *Call, parentID string) {
	msg := param(c, "message")
	if msg == "" {
		writeError(w, GraphErrorSpec{Code: 100, Type: "OAuthException", Message: "(#100) The parameter message is required"})
		return
	}
	s.mu.Lock()
	pageID := ""
	if ti := s.st.tokens[c.Token]; ti != nil {
		pageID = ti.pageID
	}
	reply := &Comment{ID: s.st.nextID(parentID + "_r"), ParentID: parentID, FromID: pageID, Message: msg, Created: time.Now().UTC()}
	s.st.comments[reply.ID] = reply
	s.mu.Unlock()
	writeJSON(w, map[string]any{"id": reply.ID})
}

func (s *Server) listComments(w http.ResponseWriter, parentID string, keep func(*Comment) bool) {
	s.mu.Lock()
	data := []map[string]any{}
	for _, cm := range s.st.comments {
		if cm.ParentID == parentID && keep(cm) {
			data = append(data, commentJSON(cm))
		}
	}
	s.mu.Unlock()
	writeJSON(w, map[string]any{"data": data})
}

func (s *Server) liveVideos(w http.ResponseWriter, pageID string) {
	s.mu.Lock()
	data := []map[string]any{}
	for _, v := range s.st.lives {
		if v.PageID == pageID {
			data = append(data, liveJSON(v))
		}
	}
	s.mu.Unlock()
	writeJSON(w, map[string]any{"data": data})
}

func liveJSON(v *LiveVideo) map[string]any {
	return map[string]any{"id": v.ID, "status": v.Status, "title": v.Title, "video": map[string]any{"id": v.VideoID}}
}

func (s *Server) live(w http.ResponseWriter, c *Call, id, edge string) {
	switch edge {
	case "":
		s.mu.Lock()
		out := liveJSON(s.st.lives[id])
		s.mu.Unlock()
		writeJSON(w, out)
	case "comments":
		s.liveCommentsPage(w, c, id)
	default:
		notFound(w, edge)
	}
}

// liveCommentsPage trả comment live sau since (giây Unix) theo thứ tự thêm, từng trang limit comment (không có limit:
// một trang; limit=0: không comment nào) với con trỏ after, kèm summary.total_count khi được hỏi.
func (s *Server) liveCommentsPage(w http.ResponseWriter, c *Call, videoID string) {
	var since time.Time
	if v, err := strconv.ParseInt(c.Query.Get("since"), 10, 64); err == nil {
		since = time.Unix(v, 0)
	}
	s.mu.Lock()
	all := s.st.liveComments[videoID]
	var match []*Comment
	for _, cm := range all {
		if cm.Created.After(since) || since.IsZero() {
			match = append(match, cm)
		}
	}
	total, ok := s.st.liveTotals[videoID]
	if !ok {
		total = len(all)
	}
	s.mu.Unlock()
	from, _ := strconv.Atoi(c.Query.Get("after"))
	from = min(max(from, 0), len(match))
	to := len(match)
	if v, err := strconv.Atoi(c.Query.Get("limit")); err == nil {
		to = min(from+max(v, 0), len(match))
	}
	data := []map[string]any{}
	for _, cm := range match[from:to] {
		j := commentJSON(cm)
		if cm.ParentID != videoID { // trả lời một comment live: Meta trả parent
			j["parent"] = map[string]any{"id": cm.ParentID}
		}
		data = append(data, j)
	}
	out := map[string]any{"data": data}
	if to < len(match) {
		out["paging"] = map[string]any{
			"cursors": map[string]any{"after": strconv.Itoa(to)},
			"next":    "https://graph.facebook.com/" + videoID + "/comments?after=" + strconv.Itoa(to),
		}
	}
	if strings.Contains(c.Query.Get("summary"), "total_count") {
		out["summary"] = map[string]any{"total_count": total}
	}
	writeJSON(w, out)
}

// param đọc tham số từ query, thân JSON (trường cấp một), rồi thân form.
func param(c *Call, key string) string {
	if v := c.Query.Get(key); v != "" {
		return v
	}
	var m map[string]any
	if json.Unmarshal(c.Body, &m) == nil {
		switch v := m[key].(type) {
		case string:
			return v
		case bool:
			return strconv.FormatBool(v)
		case []any:
			parts := make([]string, 0, len(v))
			for _, p := range v {
				if ps, ok := p.(string); ok {
					parts = append(parts, ps)
				}
			}
			return strings.Join(parts, ",")
		}
	}
	if q, err := parseForm(c.Body); err == nil {
		return q.Get(key)
	}
	return ""
}
