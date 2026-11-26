package facebook

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/urlerr"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

const (
	// liveCommentRate là tốc độ phát comment Meta đẩy qua luồng (02 mục 2; chưa xác minh, E4).
	liveCommentRate = "ten_per_second"
	// maxSSELine là độ dài tối đa một dòng của luồng text/event-stream.
	maxSSELine = 64 << 10
	// streamQueue là số comment đã đọc chờ Next tối đa; maxStreamBatch là cỡ tối đa một lô Next trả.
	streamQueue    = 256
	maxStreamBatch = 100
	// maxStreamErrorBody là cỡ tối đa thân lỗi đọc khi mở luồng hỏng.
	maxStreamErrorBody = 64 << 10
)

// errStreamClosed là lỗi Next sau khi luồng đã đóng (Close hay Meta cắt mà không báo lỗi).
var errStreamClosed = errors.New("facebook: luồng comment live đã đóng")

// StreamComments mở luồng SSE comment live của videoID: GET <META_STREAMING_BASE_URL>/{live-video-id}/live_comments
// ?comment_rate=ten_per_second&fields=…&appsecret_proof=… (token ở header Authorization như mọi lời gọi Graph, không
// nằm trong URL). Hạn 10 s tới khi có header; sau đó luồng mở tới khi Close, ctx bị hủy hay Meta cắt. Mỗi "data:" là
// một comment → comment.changed verb add (cùng khóa như webhook feed), bên gọi ghi với via = live_stream. Luồng không
// có since: chỗ hở do bên gọi lấp bằng PollComments.
func (c *Channel) StreamComments(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials, videoID string, //nolint:ireturn // channel.LiveSource đòi channel.CommentStream
	_ time.Time,
) (channel.CommentStream, error) {
	path := videoID + "/live_comments"
	q := url.Values{"comment_rate": {liveCommentRate}, "fields": {liveCommentFields}}
	start := c.client.o.Now()
	st, err := c.client.openStream(ctx, path, cred.AccessToken, q)
	c.client.record(ctx, http.MethodGet, path, c.client.o.Now().Sub(start), err)
	if err != nil {
		return nil, err
	}
	st.convert = func(data []byte) (webhooks.InboundEvent, bool) {
		var cm graphComment
		if json.Unmarshal(data, &cm) != nil {
			return webhooks.InboundEvent{}, false
		}
		s := &splitter{res: webhooks.SplitResult{Skipped: map[string]int{}}}
		s.liveComment(acc.ExternalID, videoID, cm)
		if len(s.res.Events) != 1 {
			return webhooks.InboundEvent{}, false
		}
		return s.res.Events[0], true
	}
	st.start()
	return st, nil
}

// openStream gửi yêu cầu luồng tới gốc streaming (không tiền tố phiên bản) và trả luồng khi Meta trả 200
// text/event-stream; lỗi luôn là *GraphError (URL đã bỏ, token đã che).
func (c *Client) openStream(ctx context.Context, path string, token logging.Secret, q url.Values) (*commentStream, error) {
	v := url.Values{}
	for k, vals := range q {
		v[k] = append([]string(nil), vals...)
	}
	v.Set("appsecret_proof", c.proof(token))
	u := strings.TrimRight(c.o.StreamingBaseURL, "/") + "/" + strings.TrimLeft(path, "/") + "?" + v.Encode()
	sctx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(sctx, http.MethodGet, u, nil)
	if err != nil {
		cancel()
		return nil, &GraphError{Sent: NotSent, Message: "dựng yêu cầu luồng", build: true, err: urlerr.Strip(err)}
	}
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Accept", "text/event-stream")
	hc := *c.o.HTTP
	hc.Timeout = 0 // luồng mở lâu: hạn chỉ áp tới khi có header (headerTimer), sau đó theo ctx và Close
	headerTimer := time.AfterFunc(callTimeout, cancel)
	resp, err := hc.Do(req)
	headerTimer.Stop()
	if err != nil {
		cancel()
		return nil, transportError(err, false, token)
	}
	if resp.StatusCode != http.StatusOK || !isEventStream(resp.Header.Get("Content-Type")) {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxStreamErrorBody))
		_ = resp.Body.Close()
		cancel()
		if ge := decodeError(resp.StatusCode, raw, token); ge != nil {
			return nil, ge
		}
		return nil, &GraphError{HTTPStatus: resp.StatusCode, Sent: NotSent, Message: "luồng comment không phải text/event-stream"}
	}
	return &commentStream{body: resp.Body, cancel: cancel, events: make(chan webhooks.InboundEvent, streamQueue), done: make(chan struct{})}, nil
}

func isEventStream(ct string) bool {
	mt, _, err := mime.ParseMediaType(ct)
	return err == nil && mt == "text/event-stream"
}

// commentStream là một luồng SSE đang mở: một goroutine đọc thân, đổi mỗi sự kiện thành comment và đẩy vào events;
// Next gom các comment đã tới thành một lô.
type commentStream struct {
	body    io.ReadCloser
	cancel  context.CancelFunc
	convert func([]byte) (webhooks.InboundEvent, bool)
	events  chan webhooks.InboundEvent
	done    chan struct{} // đóng khi Close
	once    sync.Once
	wg      sync.WaitGroup
	err     error // lỗi đọc; chỉ đọc sau khi events đóng
}

func (s *commentStream) start() {
	s.wg.Go(func() {
		defer close(s.events)
		err := readSSE(s.body, func(data []byte) bool {
			e, ok := s.convert(data)
			if !ok {
				return true // dữ liệu không phải comment đọc được: bỏ
			}
			select {
			case s.events <- e:
				return true
			case <-s.done:
				return false
			}
		})
		if err == nil {
			err = io.EOF
		}
		s.err = fmt.Errorf("đọc luồng comment live: %w", urlerr.Strip(err))
	})
}

// Next chặn tới khi có ít nhất một comment (rồi gom thêm các comment đã tới, tối đa 100), ctx bị hủy (trả ctx.Err(),
// luồng vẫn mở) hoặc luồng đóng (lỗi đọc, io.EOF khi Meta đóng êm).
func (s *commentStream) Next(ctx context.Context) ([]webhooks.InboundEvent, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err() //nolint:wrapcheck // bên gọi so errors.Is với lỗi của ctx của chính nó
	case <-s.done:
		return nil, errStreamClosed
	case e, ok := <-s.events:
		if !ok {
			return nil, s.err
		}
		batch := []webhooks.InboundEvent{e}
		for len(batch) < maxStreamBatch {
			select {
			case e, ok := <-s.events:
				if !ok {
					return batch, nil
				}
				batch = append(batch, e)
			default:
				return batch, nil
			}
		}
		return batch, nil
	}
}

// Close đóng kết nối và chờ goroutine đọc thoát; gọi nhiều lần được.
func (s *commentStream) Close() error {
	s.once.Do(func() {
		close(s.done)
		s.cancel()
		_ = s.body.Close()
		s.wg.Wait()
	})
	return nil
}

// readSSE đọc luồng text/event-stream (chuẩn HTML "server-sent events"): dòng "data:" (bỏ một dấu cách đầu) gom lại,
// nối bằng "\n", và phát khi gặp dòng trống; dòng ":" là chú thích, trường khác bỏ qua; sự kiện cuối chưa có dòng trống
// bị bỏ. Dòng dài quá 64 KiB → lỗi. emit trả false thì dừng, trả nil.
func readSSE(r io.Reader, emit func(data []byte) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), maxSSELine)
	var data bytes.Buffer
	has := false
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			if has && !emit(bytes.TrimSuffix(data.Bytes(), []byte("\n"))) {
				return nil
			}
			data.Reset()
			has = false
			continue
		}
		field, value, _ := bytes.Cut(line, []byte(":"))
		if len(field) == 0 { // chú thích (giữ kết nối)
			continue
		}
		if string(field) == "data" {
			data.Write(bytes.TrimPrefix(value, []byte(" ")))
			data.WriteByte('\n')
			has = true
		}
	}
	return sc.Err() //nolint:wrapcheck // bên gọi bọc
}
