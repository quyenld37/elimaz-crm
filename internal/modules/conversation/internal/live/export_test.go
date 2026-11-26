package live

// Crash giả lập tiến trình chết (LV10): luồng dừng khi ctx của Run bị hủy nhưng không nhả khóa.
func (m *Manager) Crash() { m.crashed.Store(true) }
