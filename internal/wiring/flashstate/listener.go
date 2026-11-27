package flashstate

import (
	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/wiring/webcache"
)

// enqueue chạy trong goroutine của hub: không chặn. Chỉ nhận báo hiệu của đợt, món flash sale (tính lại đúng phạm vi);
// system.resync → một lượt đầy đủ.
func (l *Loop) enqueue(s realtime.Signal) {
	var tg target
	if s.Topic == realtime.TopicSystemResync {
		tg.all = true
		l.push(tg)
		return
	}
	switch s.Entity.Type {
	case webcache.EntityFlashItem, webcache.EntityFlashSale:
		id, err := uuid.Parse(s.Entity.ID)
		if err != nil {
			return
		}
		tg.tenant = s.TenantID
		if s.Entity.Type == webcache.EntityFlashItem {
			tg.item = id
		} else {
			tg.sale = id
		}
	default:
		return
	}
	l.push(tg)
}

// push đưa tg vào hàng chờ không chặn; hàng đầy thì bỏ: mỗi nhịp đã là một lượt đầy đủ, nên việc bị bỏ được làm ở nhịp
// kế tiếp (≤ every, nhỏ hơn hạn sống 10 s của trạng thái).
func (l *Loop) push(tg target) {
	select {
	case l.queue <- tg:
	default:
	}
}
