package fakegraph

import (
	"slices"
	"strconv"
	"time"
)

// graphTime là dạng thời gian Meta dùng trong created_time.
const graphTime = "2006-01-02T15:04:05-0700"

// Page là một Page trong máy giả. Token là token Page; Scopes là quyền debug_token trả về (rỗng: bộ quyền M4 đủ).
type Page struct {
	ID, Name, Token string
	Scopes          []string
}

// User là một người dùng Facebook quản trị các Page; Code là mã đăng nhập đổi được lấy Token (oauth/access_token).
type User struct {
	Code, Token string
	PageIDs     []string
}

// Profile là hồ sơ một người nhắn tin cho Page (PSID).
type Profile struct {
	PSID, FirstName, LastName, ProfilePic string
}

// Post là một bài viết của Page.
type Post struct {
	ID, PageID, Message, PermalinkURL string
	Created                           time.Time
}

// Comment là một comment trên bài viết (ParentID là mã bài), trả lời comment (ParentID là mã comment) hoặc comment
// live (ParentID là mã video live).
type Comment struct {
	ID, ParentID, FromID, FromName, Message string
	Created                                 time.Time
	Hidden                                  bool
}

// LiveVideo là một phiên live của Page.
type LiveVideo struct {
	ID, PageID, VideoID, Status, Title string
}

// Message là một tin trong hội thoại Page–người dùng: tin khách gửi (AddInboundMessage) hoặc tin Page gửi qua
// {page-id}/messages. Body là thân yêu cầu gốc của lần gửi.
type Message struct {
	ID, PageID, PSID, CommentID, Text, Metadata string
	FromPage                                    bool
	At                                          time.Time
	Body                                        []byte
}

// defaultScopes là quyền M4 cần (spec 02 mục 1).
func defaultScopes() []string {
	return []string{
		"pages_messaging", "pages_manage_metadata", "pages_read_engagement", "pages_manage_engagement",
		"pages_read_user_content", "pages_show_list", "business_management",
	}
}

type tokenKind int

const (
	tokenPage tokenKind = iota + 1
	tokenUser
)

type tokenInfo struct {
	kind    tokenKind
	pageID  string
	user    *User
	revoked bool
}

// state là dữ liệu trong bộ nhớ; mọi truy cập dưới Server.mu.
type state struct {
	seq          int
	pages        map[string]*Page
	users        map[string]*User // theo Code
	tokens       map[string]*tokenInfo
	profiles     map[string]*Profile
	posts        map[string]*Post
	comments     map[string]*Comment
	lives        map[string]*LiveVideo
	liveComments map[string][]*Comment
	liveTotals   map[string]int
	messages     []*Message
	privateUsed  map[string]bool
	subscribed   map[string][]string
	files        map[string]file
	redirects    map[string]string
}

type file struct {
	body        []byte
	contentType string
}

func newState() state {
	return state{
		pages: map[string]*Page{}, users: map[string]*User{}, tokens: map[string]*tokenInfo{},
		profiles: map[string]*Profile{}, posts: map[string]*Post{}, comments: map[string]*Comment{},
		lives: map[string]*LiveVideo{}, liveComments: map[string][]*Comment{}, liveTotals: map[string]int{}, privateUsed: map[string]bool{},
		subscribed: map[string][]string{}, files: map[string]file{}, redirects: map[string]string{},
	}
}

func (s *state) nextID(prefix string) string {
	s.seq++
	return prefix + strconv.Itoa(1000+s.seq)
}

// AddPage thêm (hoặc thay) một Page và token của nó.
func (s *Server) AddPage(p Page) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(p.Scopes) == 0 {
		p.Scopes = defaultScopes()
	}
	s.st.pages[p.ID] = &p
	s.st.tokens[p.Token] = &tokenInfo{kind: tokenPage, pageID: p.ID}
}

// AddUser thêm một người dùng quản trị các Page đã thêm.
func (s *Server) AddUser(u User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.users[u.Code] = &u
	s.st.tokens[u.Token] = &tokenInfo{kind: tokenUser, user: &u}
}

// AddProfile thêm hồ sơ của một PSID.
func (s *Server) AddProfile(p Profile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.profiles[p.PSID] = &p
}

// AddPost thêm một bài viết.
func (s *Server) AddPost(p Post) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.posts[p.ID] = &p
}

// AddComment thêm một comment (của khách) vào bài hay comment ParentID.
func (s *Server) AddComment(c Comment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.comments[c.ID] = &c
}

// AddLiveVideo thêm một phiên live.
func (s *Server) AddLiveVideo(v LiveVideo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.lives[v.ID] = &v
}

// SetLiveStatus đổi trạng thái phiên live (LIVE, LIVE_STOPPED, VOD…).
func (s *Server) SetLiveStatus(videoID, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v := s.st.lives[videoID]; v != nil {
		v.Status = status
	}
}

// AddInboundMessage thêm một tin khách psid gửi cho Page (đọc lại qua {page-id}/conversations).
func (s *Server) AddInboundMessage(pageID, psid, mid, text string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.messages = append(s.st.messages, &Message{ID: mid, PageID: pageID, PSID: psid, Text: text, At: at})
}

// AddFile phục vụ body ở đường dẫn path (không có tiền tố phiên bản) với Content-Type khai contentType, cho test tải
// tệp đính kèm. Yêu cầu tới path không cần token.
func (s *Server) AddFile(path string, body []byte, contentType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.files[trimSlash(path)] = file{body: body, contentType: contentType}
}

// AddRedirect trả 302 tới target cho yêu cầu tới path.
func (s *Server) AddRedirect(path, target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.redirects[trimSlash(path)] = target
}

// Messages trả các tin Page đã gửi (theo thứ tự).
func (s *Server) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Message
	for _, m := range s.st.messages {
		if m.FromPage {
			out = append(out, *m)
		}
	}
	return out
}

// CommentByID trả một comment (kể cả trả lời của Page) và có tìm thấy không.
func (s *Server) CommentByID(id string) (c Comment, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.st.comments[id]; p != nil {
		c, ok = *p, true
	}
	return c, ok
}

// Subscribed trả các trường webhook ứng dụng đã đăng ký trên Page (nil: chưa đăng ký).
func (s *Server) Subscribed(pageID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.st.subscribed[pageID])
}
