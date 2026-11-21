package files

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // ETag kiểu S3 (MD5 nội dung), không dùng để bảo mật
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MemStore là FileStore trong bộ nhớ cho test của mọi mốc, mô phỏng đúng luật mà CRM dựa vào ở S3: không ghi đè
// (Put, đường dẫn PUT ký sẵn có If-None-Match: *), đường dẫn ký sẵn có hạn, gồm kích thước và loại, Delete chỉ thêm
// dấu xóa (Versions còn thấy bản cũ). MemStore là http.Handler phục vụ đường dẫn ký sẵn: test dựng
// httptest.NewServer(m) rồi SetBaseURL (filestest.MemFileStore làm sẵn).
type MemStore struct {
	mu      sync.Mutex
	baseURL string
	secret  []byte
	objects map[string][]memVersion // "<bucket>/<key>" → các phiên bản, mới nhất cuối; dấu xóa có deleted
	now     func() time.Time
}

type memVersion struct {
	data        []byte
	contentType string
	deleted     bool
}

// NewMemStore trả kho rỗng với khóa ký ngẫu nhiên; gọi SetBaseURL trước khi ký đường dẫn.
func NewMemStore() *MemStore {
	secret := make([]byte, 32)
	_, _ = rand.Read(secret) // crypto/rand.Read không trả lỗi (Go ≥ 1.24)
	return &MemStore{secret: secret, objects: map[string][]memVersion{}, now: time.Now}
}

// SetBaseURL đặt gốc của đường dẫn ký sẵn (địa chỉ máy chủ HTTP đang phục vụ m), không có "/" cuối.
func (m *MemStore) SetBaseURL(u string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.baseURL = strings.TrimSuffix(u, "/")
}

func memPath(b Bucket, key string) string { return string(b) + "/" + key }

// current trả phiên bản hiện hành (không phải dấu xóa). Gọi khi giữ m.mu.
func (m *MemStore) current(b Bucket, key string) (memVersion, bool) {
	vs := m.objects[memPath(b, key)]
	if len(vs) == 0 || vs[len(vs)-1].deleted {
		return memVersion{}, false
	}
	return vs[len(vs)-1], true
}

// putIfAbsent ghi phiên bản mới nếu khóa chưa có. Gọi khi giữ m.mu.
func (m *MemStore) putIfAbsent(b Bucket, key string, data []byte, contentType string) error {
	if _, ok := m.current(b, key); ok {
		return ErrObjectExists
	}
	p := memPath(b, key)
	m.objects[p] = append(m.objects[p], memVersion{data: data, contentType: contentType})
	return nil
}

func etag(data []byte) string {
	sum := md5.Sum(data) //nolint:gosec // như trên
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// sign trả chữ ký HMAC của các trường của một đường dẫn.
func (m *MemStore) sign(fields ...string) string {
	h := hmac.New(sha256.New, m.secret)
	for _, f := range fields {
		_, _ = h.Write([]byte(f))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// PresignPut implements FileStore.
func (m *MemStore) PresignPut(_ context.Context, b Bucket, key string, size int64, contentType string, ttl time.Duration) (PresignedPut, error) {
	if !b.valid() || size <= 0 || ttl <= 0 {
		return PresignedPut{}, fmt.Errorf("ký PUT %s: tham số sai", b)
	}
	return m.presignPut(b, key, strconv.FormatInt(size, 10), contentType, ttl), nil
}

// PresignPutAnySize implements FileStore: chữ ký không có len, PUT nhận mọi kích thước.
func (m *MemStore) PresignPutAnySize(_ context.Context, b Bucket, key string, contentType string, ttl time.Duration) (PresignedPut, error) {
	if !b.valid() || ttl <= 0 || contentType == "" {
		return PresignedPut{}, fmt.Errorf("ký PUT %s: tham số sai", b)
	}
	return m.presignPut(b, key, "", contentType, ttl), nil
}

// presignPut ký PUT; n là kích thước (chuỗi số) hoặc "" khi không ký kích thước.
func (m *MemStore) presignPut(b Bucket, key, n, contentType string, ttl time.Duration) PresignedPut {
	exp := m.now().Add(ttl).Truncate(time.Second)
	e := strconv.FormatInt(exp.Unix(), 10)
	q := url.Values{"ct": {contentType}, "exp": {e}, "sig": {m.sign("PUT", string(b), key, n, contentType, e)}}
	if n != "" {
		q.Set("len", n)
	}
	return PresignedPut{
		URL:       m.base() + "/" + memPath(b, key) + "?" + q.Encode(),
		Headers:   map[string]string{"Content-Type": contentType, "If-None-Match": "*"},
		ExpiresAt: exp,
	}
}

// PresignGet implements FileStore.
func (m *MemStore) PresignGet(_ context.Context, b Bucket, key string, ttl time.Duration, attachmentName string) (string, error) {
	if !b.valid() || ttl <= 0 {
		return "", fmt.Errorf("ký GET %s: tham số sai", b)
	}
	e := strconv.FormatInt(m.now().Add(ttl).Unix(), 10)
	cd := contentDisposition(attachmentName)
	q := url.Values{"exp": {e}, "cd": {cd}, "sig": {m.sign("GET", string(b), key, cd, e)}}
	return m.base() + "/" + memPath(b, key) + "?" + q.Encode(), nil
}

func (m *MemStore) base() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.baseURL
}

// Head implements FileStore.
func (m *MemStore) Head(_ context.Context, b Bucket, key string) (ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.current(b, key)
	if !ok {
		return ObjectInfo{}, fmt.Errorf("head %s/%s: %w", b, key, ErrObjectNotFound)
	}
	return ObjectInfo{Size: int64(len(v.data)), ContentType: v.contentType, ETag: etag(v.data)}, nil
}

// Open implements FileStore.
func (m *MemStore) Open(_ context.Context, b Bucket, key string, maxBytes int64) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.current(b, key)
	switch {
	case !ok:
		return nil, fmt.Errorf("mở %s/%s: %w", b, key, ErrObjectNotFound)
	case int64(len(v.data)) > maxBytes:
		return nil, fmt.Errorf("mở %s/%s: %d byte > %d: %w", b, key, len(v.data), maxBytes, ErrTooLarge)
	}
	return io.NopCloser(bytes.NewReader(v.data)), nil
}

// CopyIfAbsent implements FileStore.
func (m *MemStore) CopyIfAbsent(_ context.Context, src Bucket, srcKey string, dst Bucket, dstKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.current(src, srcKey)
	if !ok {
		return fmt.Errorf("chép %s/%s: %w", src, srcKey, ErrObjectNotFound)
	}
	if _, exists := m.current(dst, dstKey); exists {
		return nil
	}
	return m.putIfAbsent(dst, dstKey, v.data, v.contentType)
}

// Put implements FileStore.
func (m *MemStore) Put(_ context.Context, b Bucket, key string, r io.Reader, size int64, contentType string) error {
	if !b.valid() || size <= 0 {
		return fmt.Errorf("ghi %s/%s: tham số sai", b, key)
	}
	data, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return fmt.Errorf("ghi %s/%s: đọc nội dung: %w", b, key, err)
	}
	if int64(len(data)) != size {
		return fmt.Errorf("ghi %s/%s: khai %d byte, có %d", b, key, size, len(data))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.putIfAbsent(b, key, data, contentType); err != nil {
		return fmt.Errorf("ghi %s/%s: %w", b, key, err)
	}
	return nil
}

// Delete implements FileStore: thêm dấu xóa.
func (m *MemStore) Delete(_ context.Context, b Bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := memPath(b, key)
	m.objects[p] = append(m.objects[p], memVersion{deleted: true})
	return nil
}

// Versions trả mọi phiên bản của khóa, cũ trước; dấu xóa là nil (cho test, như crm-admin đọc theo phiên bản).
func (m *MemStore) Versions(b Bucket, key string) [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out [][]byte
	for _, v := range m.objects[memPath(b, key)] {
		if v.deleted {
			out = append(out, nil)
		} else {
			out = append(out, bytes.Clone(v.data))
		}
	}
	return out
}

// Keys trả mọi khóa hiện có (không tính khóa chỉ còn dấu xóa) của bucket b (cho test).
func (m *MemStore) Keys(b Bucket) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	prefix := string(b) + "/"
	for p := range m.objects {
		if key, ok := strings.CutPrefix(p, prefix); ok {
			if _, live := m.current(b, key); live {
				out = append(out, key)
			}
		}
	}
	return out
}

// ServeHTTP phục vụ đường dẫn ký sẵn: PUT /<bucket>/<key>?len&ct&exp&sig, GET /<bucket>/<key>?exp&cd&sig. Sai chữ
// ký, hết hạn, sai kích thước, sai loại, thiếu If-None-Match hay khóa đã có → 403/412 như S3; không có → 404.
func (m *MemStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bucket, key, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	b := Bucket(bucket)
	if !ok || !b.valid() || key == "" {
		http.Error(w, "NoSuchBucket", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	exp, err := strconv.ParseInt(q.Get("exp"), 10, 64)
	if err != nil || m.now().Unix() > exp {
		http.Error(w, "AccessDenied: Request has expired", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPut:
		m.servePut(w, r, b, key, q)
	case http.MethodGet:
		m.serveGet(w, b, key, q)
	default:
		http.Error(w, "MethodNotAllowed", http.StatusMethodNotAllowed)
	}
}

func (m *MemStore) servePut(w http.ResponseWriter, r *http.Request, b Bucket, key string, q url.Values) {
	n, ct := q.Get("len"), q.Get("ct")
	// Không có len: chữ ký của PresignPutAnySize (len rỗng trong chữ ký), nhận mọi kích thước.
	if !hmac.Equal([]byte(q.Get("sig")), []byte(m.sign("PUT", string(b), key, n, ct, q.Get("exp")))) ||
		(n != "" && strconv.FormatInt(r.ContentLength, 10) != n) || r.ContentLength < 0 ||
		r.Header.Get("Content-Type") != ct || r.Header.Get("If-None-Match") != "*" {
		http.Error(w, "SignatureDoesNotMatch", http.StatusForbidden)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, r.ContentLength+1))
	if err != nil || int64(len(data)) != r.ContentLength {
		http.Error(w, "IncompleteBody", http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	err = m.putIfAbsent(b, key, data, ct)
	m.mu.Unlock()
	if err != nil {
		http.Error(w, "PreconditionFailed", http.StatusPreconditionFailed)
		return
	}
	w.Header().Set("ETag", etag(data))
	w.WriteHeader(http.StatusOK)
}

func (m *MemStore) serveGet(w http.ResponseWriter, b Bucket, key string, q url.Values) {
	cd := q.Get("cd")
	if !hmac.Equal([]byte(q.Get("sig")), []byte(m.sign("GET", string(b), key, cd, q.Get("exp")))) {
		http.Error(w, "SignatureDoesNotMatch", http.StatusForbidden)
		return
	}
	m.mu.Lock()
	v, ok := m.current(b, key)
	m.mu.Unlock()
	if !ok {
		http.Error(w, "NoSuchKey", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", v.contentType)
	if cd != "" {
		w.Header().Set("Content-Disposition", cd)
	}
	_, _ = w.Write(v.data)
}
