package facebook

import (
	"encoding/json"
	"net/http"
	"time"
)

// slowDownPct là mức dùng (phần trăm) từ đó bên gọi phải giãn lời gọi (spec 02 mục 6).
const slowDownPct = 80

// Meta là thông tin kèm câu trả lời Graph.
type Meta struct {
	Usage Usage
}

// Usage là mức dùng Meta báo trong header (phần trăm hạn mức, lấy lớn nhất giữa các header và các loại) và thời gian
// phải chờ khi đã bị chặn.
type Usage struct {
	CallCountPct, TotalTimePct, TotalCPUPct int
	RegainAfter                             time.Duration
}

// ShouldSlowDown báo mức dùng đã từ 80% hoặc Meta đã báo thời gian chờ.
func (u Usage) ShouldSlowDown() bool {
	return max(u.CallCountPct, u.TotalTimePct, u.TotalCPUPct) >= slowDownPct || u.RegainAfter > 0
}

type usageEntry struct {
	CallCount  int `json:"call_count"`
	TotalTime  int `json:"total_time"`
	TotalCPU   int `json:"total_cputime"`
	RegainMins int `json:"estimated_time_to_regain_access"`
}

func (u *Usage) merge(e usageEntry) {
	u.CallCountPct = max(u.CallCountPct, e.CallCount)
	u.TotalTimePct = max(u.TotalTimePct, e.TotalTime)
	u.TotalCPUPct = max(u.TotalCPUPct, e.TotalCPU)
	u.RegainAfter = max(u.RegainAfter, time.Duration(e.RegainMins)*time.Minute)
}

// ParseUsage đọc X-App-Usage, X-Page-Usage và X-Business-Use-Case-Usage; header thiếu hay JSON hỏng thì bỏ qua.
func ParseUsage(h http.Header) Usage {
	var u Usage
	for _, name := range []string{"X-App-Usage", "X-Page-Usage"} {
		var e usageEntry
		if v := h.Get(name); v != "" && json.Unmarshal([]byte(v), &e) == nil {
			u.merge(e)
		}
	}
	var biz map[string][]usageEntry
	if v := h.Get("X-Business-Use-Case-Usage"); v != "" && json.Unmarshal([]byte(v), &biz) == nil {
		for _, list := range biz {
			for _, e := range list {
				u.merge(e)
			}
		}
	}
	return u
}
