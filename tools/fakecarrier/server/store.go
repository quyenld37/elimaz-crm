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
)

// Chế độ của hãng giả (POST /admin/mode).
const (
	modeOK                 = "ok"
	modeTimeoutAfterCreate = "timeout_after_create"
	modeFailBeforeCreate   = "fail_before_create"
	modeForge              = "forge"
)

// Trạng thái thô của kiện (khớp internal/adapters/carrier/fake).
const (
	rawCreated   = "created"
	rawPickedUp  = "picked_up"
	rawDelivered = "delivered"
)

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
}

// state là phần ghi ra tệp JSON.
type state struct {
	Mode    string             `json:"mode"`
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

func (s *Store) setMode(m string) error {
	switch m {
	case modeOK, modeTimeoutAfterCreate, modeFailBeforeCreate, modeForge:
	default:
		return errBadMode
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Mode = m
	return s.saveLocked()
}

// create tạo kiện cho code, hoặc trả kiện đã có cùng code (cùng tài khoản).
func (s *Store) create(code, accountKey string, cod int64, now time.Time) (parcel, error) {
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
		Status: rawCreated, Seq: 1, UpdatedAt: now.UTC(),
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
	p.Status, p.Seq, p.UpdatedAt = to, p.Seq+1, now.UTC()
	return *p, true, s.saveLocked()
}

func (s *Store) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.st.Parcels)
}
