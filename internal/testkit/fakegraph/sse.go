package fakegraph

import (
	"encoding/json"
	"net/http"
	"slices"
)

// streamBuffer là số comment chờ gửi tối đa của một luồng; đầy thì bỏ comment (bên đọc đối chiếu bằng hỏi định kỳ).
const streamBuffer = 256

// stream là một kết nối SSE /{live-video-id}/live_comments đang mở.
type stream struct {
	events chan []byte
	done   chan struct{}
}

// PushLiveComment thêm một comment live (đọc lại qua {live-video-id}/comments) và đẩy tới mọi luồng đang mở.
func (s *Server) PushLiveComment(videoID string, c Comment) {
	if c.ParentID == "" {
		c.ParentID = videoID
	}
	b, _ := json.Marshal(commentJSON(&c))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.liveComments[videoID] = append(s.st.liveComments[videoID], &c)
	for _, st := range s.streams[videoID] {
		select {
		case st.events <- b:
		default:
		}
	}
}

// DropStream đóng mọi luồng SSE đang mở của videoID (như Meta cắt luồng).
func (s *Server) DropStream(videoID string) {
	s.mu.Lock()
	sts := s.streams[videoID]
	delete(s.streams, videoID)
	s.mu.Unlock()
	for _, st := range sts {
		close(st.done)
	}
}

// StreamCount là số luồng SSE đang mở của videoID.
func (s *Server) StreamCount(videoID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.streams[videoID])
}

func (s *Server) closeStreams() {
	s.mu.Lock()
	all := s.streams
	s.streams = map[string][]*stream{}
	s.mu.Unlock()
	for _, sts := range all {
		for _, st := range sts {
			close(st.done)
		}
	}
}

// serveStream giữ một luồng SSE: mỗi comment một dòng "data: <json>", tới khi bên gọi đóng hoặc DropStream.
func (s *Server) serveStream(w http.ResponseWriter, r *http.Request, videoID string) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	st := &stream{events: make(chan []byte, streamBuffer), done: make(chan struct{})}
	s.mu.Lock()
	s.streams[videoID] = append(s.streams[videoID], st)
	s.mu.Unlock()
	defer s.removeStream(videoID, st)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-st.done:
			return
		case b := <-st.events:
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
			fl.Flush()
		}
	}
}

func (s *Server) removeStream(videoID string, st *stream) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streams[videoID] = slices.DeleteFunc(s.streams[videoID], func(x *stream) bool { return x == st })
	if len(s.streams[videoID]) == 0 {
		delete(s.streams, videoID)
	}
}
