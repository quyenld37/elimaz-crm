package server

import (
	"log/slog"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Phần điều khiển dùng từ test trong cùng tiến trình (cài carriertest.Harness): không qua HTTP, không xác thực.

// Reset xóa mọi kiện, đưa mọi chế độ về ok, xóa bộ đếm lời gọi.
func (s *Server) Reset() {
	if err := s.st.reset(); err != nil {
		s.logger.Error("xóa trạng thái hãng giả", logging.Err(err))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = map[string]int{}
}

// SetMode đặt chế độ lỗi của thao tác op (như POST /admin/mode); chế độ sai chỉ ghi log.
func (s *Server) SetMode(op, mode string) {
	if err := s.st.setMode(op, mode); err != nil {
		s.logger.Error("đặt chế độ hãng giả", slog.String("op", op), slog.String("mode", mode), logging.Err(err))
	}
}

// Push đẩy một sự kiện hành trình vào kiện tracking (đổi trạng thái thô theo ev.RawStatus; không gửi webhook).
func (s *Server) Push(tracking string, ev fulfillment.CarrierEvent) {
	at := ev.OccurredAt
	if at.IsZero() {
		at = s.now()
	}
	ok, err := s.st.push(tracking, ev.Key, ev.RawStatus, at)
	if err != nil || !ok {
		s.logger.Error("đẩy sự kiện hãng giả", slog.String("tracking", tracking), slog.Bool("found", ok), logging.Err(err))
	}
}

// Has cho biết đã có kiện theo mã vận đơn CRM (mọi tài khoản).
func (s *Server) Has(shipmentCode string) bool {
	_, ok := s.st.byCode(shipmentCode, "")
	return ok
}

// Calls là số yêu cầu có xác thực của thao tác op từ lần Reset gần nhất (create, lookup, status, cancel, quote, track,
// labels, address, redeliver, locations, health).
func (s *Server) Calls(op string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[op]
}
