package testkit

import (
	"io"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"
)

// SwitchProxy là proxy TCP tới PostgreSQL có công tắc: tắt thì cắt mọi kết nối đang mở và đóng ngay kết nối mới
// (database "ngừng" với riêng bên dùng proxy); bật lại thì nối thẳng như thường.
type SwitchProxy struct {
	target string
	ln     net.Listener

	mu    sync.Mutex
	down  bool
	conns map[net.Conn]struct{}
	wg    sync.WaitGroup
}

// StartSwitchProxy mở proxy tới host:port của rawURL; trả proxy và chuỗi kết nối đi qua nó. Đóng khi test xong.
func StartSwitchProxy(t *testing.T, rawURL string) (*SwitchProxy, string) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &SwitchProxy{target: u.Host, ln: ln, conns: map[net.Conn]struct{}{}}
	p.wg.Go(p.accept)
	t.Cleanup(func() {
		_ = ln.Close()
		p.SetDown(true)
		p.wg.Wait()
	})
	u.Host = ln.Addr().String()
	return p, u.String()
}

// SetDown bật (true) hoặc tắt công tắc lỗi.
func (p *SwitchProxy) SetDown(down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.down = down
	if down {
		for c := range p.conns {
			_ = c.Close()
		}
	}
}

func (p *SwitchProxy) accept() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		if p.down {
			p.mu.Unlock()
			_ = c.Close()
			continue
		}
		p.conns[c] = struct{}{}
		p.mu.Unlock()
		p.wg.Go(func() { p.pipe(c) })
	}
}

func (p *SwitchProxy) pipe(c net.Conn) {
	defer func() {
		p.mu.Lock()
		delete(p.conns, c)
		p.mu.Unlock()
		_ = c.Close()
	}()
	up, err := (&net.Dialer{Timeout: 5 * time.Second}).Dial("tcp", p.target)
	if err != nil {
		return
	}
	p.mu.Lock()
	if p.down {
		p.mu.Unlock()
		_ = up.Close()
		return
	}
	p.conns[up] = struct{}{}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.conns, up)
		p.mu.Unlock()
		_ = up.Close()
	}()
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
		_ = src.Close()
		done <- struct{}{}
	}
	go cp(up, c)
	go cp(c, up)
	<-done
	<-done
}
