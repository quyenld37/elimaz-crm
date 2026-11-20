package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
)

// Chế độ của hãng giả (POST /admin/mode). Tạo kiện: ok, timeout_after_create, fail_before_create, forge (M1), reject,
// auth_revoked, rate_limited. Thao tác khác: ok, reject, auth_revoked, rate_limited, timeout_after_send,
// fail_before_send.
const (
	modeOK                 = "ok"
	modeTimeoutAfterCreate = "timeout_after_create"
	modeFailBeforeCreate   = "fail_before_create"
	modeForge              = "forge"
	modeReject             = "reject"
	modeAuthRevoked        = "auth_revoked"
	modeRateLimited        = "rate_limited"
	modeTimeoutAfterSend   = "timeout_after_send"
	modeFailBeforeSend     = "fail_before_send"
)

// Thao tác có chế độ riêng (POST /admin/mode {op}); op rỗng = create như M1.
const (
	opCreate    = "create"
	opCancel    = "cancel"
	opQuote     = "quote"
	opTrack     = "track"
	opLabels    = "labels"
	opAddress   = "address"
	opRedeliver = "redeliver"
	opLocations = "locations"
	opHealth    = "health"
)

// Trạng thái thô của kiện (khớp internal/adapters/carrier/fake).
const (
	rawCreated        = fake.RawCreated
	rawPickedUp       = fake.RawPickedUp
	rawDelivered      = fake.RawDelivered
	rawCancelled      = fake.RawCancelled
	rawDeliveryFailed = fake.RawDeliveryFailed
	rawOutForDelivery = fake.RawOutForDelivery
	rawInTransit      = fake.RawInTransit
)

// Kết quả hủy, sửa (khớp fulfillment.CancelResult, fulfillment.UpdateResult).
const (
	resultDone             = "done"
	resultRejected         = "rejected"
	resultRejectedPickedUp = "rejected_picked_up"
)

// event là một sự kiện hành trình của kiện.
type event struct {
	Key    string    `json:"key"`
	Status string    `json:"status"`
	At     time.Time `json:"at"`
}

// parcelFee là phí cố định của mỗi kiện (đồng).
const parcelFee = 25000

// parcel là một kiện.
type parcel struct {
	Tracking   string    `json:"tracking"`
	Code       string    `json:"code"`
	AccountKey string    `json:"accountKey"`
	CODAmount  int64     `json:"codAmount"`
	Fee        int64     `json:"fee"`
	Status     string    `json:"status"`
	Seq        int       `json:"seq"`
	UpdatedAt  time.Time `json:"updatedAt"`
	RouteKey   string    `json:"routeKey,omitempty"`
	Events     []event   `json:"events,omitempty"`
}

// state là phần ghi ra tệp JSON. Mode là chế độ tạo kiện (M1); Modes là chế độ của thao tác khác.
type state struct {
	Mode    string             `json:"mode"`
	Modes   map[string]string  `json:"modes,omitempty"`
	Next    int                `json:"next"`
	Parcels map[string]*parcel `json:"parcels"` // theo tracking
}

// Store giữ trạng thái trong bộ nhớ, ghi tệp JSON (ghi tệp tạm rồi đổi tên) sau mỗi lần đổi để sống qua khởi động
// lại; path rỗng thì chỉ trong bộ nhớ.
type Store struct {
	mu   sync.Mutex
	path string
	st   state
}

var errBadMode = errors.New("fakecarrier: chế độ lạ")

// OpenStore đọc tệp path (không có tệp → trạng thái rỗng, chế độ ok).
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, st: state{Mode: modeOK, Parcels: map[string]*parcel{}}}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("đọc tệp trạng thái: %w", err)
	}
	if err := json.Unmarshal(b, &s.st); err != nil {
		return nil, fmt.Errorf("đọc tệp trạng thái: %w", err)
	}
	if s.st.Parcels == nil {
		s.st.Parcels = map[string]*parcel{}
	}
	return s, nil
}

// saveLocked ghi trạng thái ra tệp; gọi khi đang giữ mu.
func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	b, err := json.Marshal(s.st)
	if err != nil {
		return fmt.Errorf("mã hóa trạng thái: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("ghi tệp trạng thái: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("đổi tên tệp trạng thái: %w", err)
	}
	return nil
}

func (s *Store) mode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Mode
}

// setMode đặt chế độ của thao tác op (rỗng hay create: chế độ tạo kiện của M1); chế độ không hợp với op → errBadMode.
func (s *Store) setMode(op, m string) error {
	switch op {
	case "", opCreate:
		switch m {
		case modeOK, modeTimeoutAfterCreate, modeFailBeforeCreate, modeForge, modeReject, modeAuthRevoked, modeRateLimited:
		default:
			return errBadMode
		}
	case opCancel, opQuote, opTrack, opLabels, opAddress, opRedeliver, opLocations, opHealth:
		switch m {
		case modeOK, modeReject, modeAuthRevoked, modeRateLimited, modeTimeoutAfterSend, modeFailBeforeSend:
		default:
			return errBadMode
		}
	default:
		return errBadMode
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if op == "" || op == opCreate {
		s.st.Mode = m
	} else {
		if s.st.Modes == nil {
			s.st.Modes = map[string]string{}
		}
		s.st.Modes[op] = m
	}
	return s.saveLocked()
}

// opMode trả chế độ của thao tác op khác tạo kiện (chưa đặt → ok).
func (s *Store) opMode(op string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.st.Modes[op]; m != "" {
		return m
	}
	return modeOK
}

// reset xóa mọi kiện, đưa mọi chế độ về ok (bộ đếm mã hãng giữ, không dùng lại mã cũ).
func (s *Store) reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Mode, s.st.Modes, s.st.Parcels = modeOK, nil, map[string]*parcel{}
	return s.saveLocked()
}

// create tạo kiện cho code, hoặc trả kiện đã có cùng code (cùng tài khoản); routeKey là khóa tuyến webhook.
func (s *Store) create(code, accountKey, routeKey string, cod int64, now time.Time) (parcel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.st.Parcels {
		if p.Code == code && p.AccountKey == accountKey {
			return *p, nil
		}
	}
	s.st.Next++
	p := &parcel{
		Tracking: fmt.Sprintf("FC%08d", s.st.Next), Code: code, AccountKey: accountKey, CODAmount: cod, Fee: parcelFee,
		Status: rawCreated, Seq: 1, UpdatedAt: now.UTC(), RouteKey: routeKey,
		Events: []event{{Key: rawCreated + ":1", Status: rawCreated, At: now.UTC()}},
	}
	s.st.Parcels[p.Tracking] = p
	return *p, s.saveLocked()
}

func (s *Store) byCode(code, accountKey string) (parcel, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.st.Parcels {
		if p.Code == code && (accountKey == "" || p.AccountKey == accountKey) {
			return *p, true
		}
	}
	return parcel{}, false
}

func (s *Store) byTracking(tracking string) (parcel, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.st.Parcels[tracking]
	if !ok {
		return parcel{}, false
	}
	return *p, true
}

// advance đổi trạng thái kiện (kể cả lùi, để thử tin đến muộn); ok = false khi không có kiện.
func (s *Store) advance(tracking, to string, now time.Time) (parcel, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.st.Parcels[tracking]
	if !ok {
		return parcel{}, false, nil
	}
	p.setStatus("", to, now)
	return *p, true, s.saveLocked()
}

// setStatus đổi trạng thái, tăng lần đổi, thêm sự kiện hành trình (key rỗng → <trạng thái>:<lần đổi>).
func (p *parcel) setStatus(key, to string, at time.Time) {
	p.Status, p.Seq, p.UpdatedAt = to, p.Seq+1, at.UTC()
	if key == "" {
		key = fmt.Sprintf("%s:%d", to, p.Seq)
	}
	p.Events = append(p.Events, event{Key: key, Status: to, At: at.UTC()})
}

// push đổi trạng thái kiện theo sự kiện đẩy từ test (không gửi webhook); ok = false khi không có kiện.
func (s *Store) push(tracking, key, raw string, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.st.Parcels[tracking]
	if !ok {
		return false, nil
	}
	p.setStatus(key, raw, at)
	return true, s.saveLocked()
}

// change áp f lên kiện tracking (giữ khóa), lưu khi f đổi kiện; ok = false khi không có kiện.
func (s *Store) change(tracking string, f func(p *parcel) (result string, changed bool)) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.st.Parcels[tracking]
	if !ok {
		return "", false, nil
	}
	res, changed := f(p)
	if !changed {
		return res, true, nil
	}
	return res, true, s.saveLocked()
}

func (s *Store) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.st.Parcels)
}
