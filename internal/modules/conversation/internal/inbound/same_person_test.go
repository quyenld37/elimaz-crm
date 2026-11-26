package inbound_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
)

// CH15: 50 vòng, mỗi vòng một PSID mới; một tin nhắn và một comment của cùng PSID trên cùng Page (khác OrderKey) được
// hai worker xử lý đồng thời → mỗi vòng đúng 1 khách (1 danh tính fb_psid), 2 hội thoại (inbox, comment) cùng
// customer_id; không việc nào dead.
func TestSamePersonMessageAndCommentOneCustomer(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	const rounds = 50
	w1, w2 := e.newWorker("w-msg"), e.newWorker("w-cmt")
	for i := range rounds {
		user := fmt.Sprintf("same-%02d", i)
		e.enqueue(facebook.Key, channel.EventMessageReceived, "m:"+user, pageExt1, user, at(i),
			channel.MessagePayload{PageID: pageExt1, UserID: user, MID: "mid-" + user, Text: "Shop ơi", SentAt: at(i)})
		e.enqueue(facebook.Key, channel.EventCommentChanged, "c:"+user, pageExt1, post1+":"+user, at(i),
			commentEv("add", post1, "cm-"+user, "", user, "Khách "+user, "Chốt", i))
		var wg sync.WaitGroup
		wg.Go(func() { e.runUntilIdle(w1) })
		wg.Go(func() { e.runUntilIdle(w2) })
		wg.Wait()
		if t.Failed() {
			return
		}
		var customers, convs, distinct int
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `SELECT
				(SELECT count(*) FROM customer.customer_identities WHERE kind = 'fb_psid' AND scope = $1 AND value = $2),
				(SELECT count(*) FROM conversation.conversations WHERE channel_account_id = $3
					AND external_thread_ref IN ($2, $4)),
				(SELECT count(DISTINCT customer_id) FROM conversation.conversations WHERE channel_account_id = $3
					AND external_thread_ref IN ($2, $4))`, pageExt1, user, e.page1, post1+":"+user).
				Scan(&customers, &convs, &distinct)
		})
		if customers != 1 || convs != 2 || distinct != 1 {
			t.Fatalf("vòng %d: danh tính %d, hội thoại %d, khách khác nhau %d; mong 1, 2, 1", i, customers, convs, distinct)
		}
	}
	e.drain()
	e.noDeadJobs()
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customers`); n != rounds {
		t.Errorf("khách = %d, mong %d", n, rounds)
	}
}
