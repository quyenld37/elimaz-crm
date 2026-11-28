package main

import (
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/testkit/loadstats"
)

// m2Good là một lượt đạt mọi ngưỡng của kế hoạch M2 Task 88.
func m2Good() m2ReportInput {
	return m2ReportInput{
		Search: k6(map[string]map[string]float64{
			"search_ms": {"p(95)": 120}, "search_req": {"count": 6000}, "search_bad": {"count": 0},
			"search_exact": {"count": 1500}, "search_miss": {"count": 0},
		}),
		Orders: k6(map[string]map[string]float64{
			"create_ms": {"p(95)": 150}, "list_ms": {"p(95)": 60}, "create_req": {"count": 1200}, "create_ok": {"count": 1000},
			"create_oos": {"count": 150}, "create_coupon_rejected": {"count": 49}, "create_bad": {"count": 1},
			"list_req": {"count": 3000}, "list_bad": {"count": 2},
		}),
		Series: []m2Stats{
			{DB: loadstats.Stats{Deadlocks: 2}, M2: loadstats.M2{TxExhausted: 1}},
			{
				DB: loadstats.Stats{Deadlocks: 2, OnHand: 2_000_050, Held: 900, Reserved: 900},
				M2: loadstats.M2{TxExhausted: 5, Coupons: []loadstats.CouponStat{
					{Code: "A", Used: 30, Active: 30}, {Code: "B", MaxUses: 100, Used: 100, Active: 100},
				}},
			},
		},
	}
}

func TestM2Evaluate_AllPass(t *testing.T) {
	t.Parallel()
	vs := evaluateM2(m2Good())
	if len(vs) != 12 {
		t.Fatalf("%d tiêu chí, muốn 12: %+v", len(vs), vs)
	}
	for _, v := range vs {
		if v.Status != statusPass {
			t.Errorf("%s: %s (%s)", v.Name, v.Status, v.Got)
		}
	}
}

// Mỗi ngưỡng của kế hoạch: vượt (hay đúng bằng) thì KHÔNG ĐẠT, chỉ tiêu chí đó.
func TestM2Evaluate_EachThreshold(t *testing.T) {
	t.Parallel()
	setK6 := func(m k6Metrics, name, stat string, v float64) {
		e := m[name]
		if e.Values == nil {
			e.Values = map[string]float64{}
		}
		e.Values[stat] = v
		m[name] = e
	}
	for _, c := range []struct {
		crit string
		edit func(*m2ReportInput)
	}{
		{critM2SearchP95, func(in *m2ReportInput) { setK6(in.Search, "search_ms", "p(95)", 300) }},
		{critM2SearchErr, func(in *m2ReportInput) { setK6(in.Search, "search_bad", "count", 6) }}, // 0,1% đúng bằng
		{critM2SearchFound, func(in *m2ReportInput) { setK6(in.Search, "search_miss", "count", 1) }},
		{critM2CreateP95, func(in *m2ReportInput) { setK6(in.Orders, "create_ms", "p(95)", 400) }},
		{critM2ListP95, func(in *m2ReportInput) { setK6(in.Orders, "list_ms", "p(95)", 200.1) }},
		{critM2CreateErr, func(in *m2ReportInput) { setK6(in.Orders, "create_bad", "count", 2) }}, // 2/1200 > 0,1%
		{critM2CreateErr, func(in *m2ReportInput) { // không đơn nào tạo được (mọi lượt bị từ chối nghiệp vụ)
			setK6(in.Orders, "create_ok", "count", 0)
			setK6(in.Orders, "create_bad", "count", 0)
		}},
		{critM2ListErr, func(in *m2ReportInput) { setK6(in.Orders, "list_bad", "count", 3) }},
		{critFBHolds, func(in *m2ReportInput) { in.Series[1].DB.OverHeld = 1 }},
		{critFBHolds, func(in *m2ReportInput) { in.Series[1].DB.Reserved = 899 }},
		{critM2Coupons, func(in *m2ReportInput) { in.Series[1].Coupons[0].Active = 29 }},
		{critM2Coupons, func(in *m2ReportInput) {
			in.Series[1].Coupons[1] = loadstats.CouponStat{Code: "B", MaxUses: 100, Used: 101, Active: 101}
		}},
		{critFBDeadlocks, func(in *m2ReportInput) { in.Series[1].DB.Deadlocks = 3 }},
		{critM2Exhausted, func(in *m2ReportInput) { in.Series[1].TxExhausted = 13 }}, // 12 / 1200 = 1%
		{critFBNoDead, func(in *m2ReportInput) { in.Series[1].DB.Dead = 1 }},
	} {
		in := m2Good()
		c.edit(&in)
		for _, v := range evaluateM2(in) {
			want := statusPass
			if v.Name == c.crit {
				want = statusFail
			}
			if v.Status != want {
				t.Errorf("sửa %s: %s là %s (%s), muốn %s", c.crit, v.Name, v.Status, v.Got, want)
			}
		}
	}
}

func TestM2Evaluate_NoData(t *testing.T) {
	t.Parallel()
	for _, v := range evaluateM2(m2ReportInput{}) {
		if v.Status != statusNA {
			t.Errorf("không có số đo: %s là %s, muốn %s", v.Name, v.Status, statusNA)
		}
	}
}

// Log của docker compose logs postgres: hai lần cùng câu (giữ lần chậm hơn), một câu khác, một dòng log thường xen giữa,
// dòng "Query Parameters" (giá trị có thể là SĐT) không được vào kết quả.
const slowLogSample = `postgres-1  | 2026-11-28 10:00:00.000 UTC [10] LOG:  duration: 120.500 ms  plan:
postgres-1  | 	Query Text: SELECT DISTINCT c.root_id
postgres-1  | 	FROM customer.customer_identities AS i
postgres-1  | 	Query Parameters: $1 = '3210', $2 = '3211'
postgres-1  | 	Limit  (cost=0.85..10.00 rows=20 width=16) (actual rows=20.00 loops=1)
postgres-1  | 	  Buffers: shared hit=40
postgres-1  | 2026-11-28 10:00:01.000 UTC [11] LOG:  checkpoint starting: time
postgres-1  | 2026-11-28 10:00:02.000 UTC [10] LOG:  duration: 340.000 ms  plan:
postgres-1  | 	Query Text: SELECT DISTINCT c.root_id
postgres-1  | 	    FROM customer.customer_identities AS i
postgres-1  | 	Limit  (cost=0.85..10.00 rows=20 width=16) (actual rows=20.00 loops=1)
postgres-1  | 	  Buffers: shared hit=40 read=300
postgres-1  | 2026-11-28 10:00:03.000 UTC [12] LOG:  duration: 200.000 ms  plan:
postgres-1  | 	Query Text: SELECT s.id FROM customer.search_by_name($1, $2)
postgres-1  | 	Function Scan on search_by_name s  (cost=0.25..10.25 rows=1000 width=32) (actual rows=500.00 loops=1)
`

func TestParseSlowLog(t *testing.T) {
	t.Parallel()
	ps, err := parseSlowLog(strings.NewReader(slowLogSample))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("%d câu, muốn 2: %+v", len(ps), ps)
	}
	if p := ps[0]; p.Query != "SELECT DISTINCT c.root_id FROM customer.customer_identities AS i" || p.MaxMs != 340 || p.Count != 2 ||
		len(p.Plan) != 2 || !strings.Contains(p.Plan[1], "read=300") {
		t.Errorf("câu chậm nhất: %+v", p)
	}
	if ps[1].MaxMs != 200 || !strings.HasPrefix(ps[1].Plan[0], "Function Scan") {
		t.Errorf("câu thứ hai: %+v", ps[1])
	}
	out := renderSlow(ps, 3)
	if strings.Contains(out, "3210") || strings.Contains(out, "Query Parameters") || strings.Contains(out, "checkpoint") {
		t.Errorf("kết quả lộ tham số hay log khác:\n%s", out)
	}
	if !strings.Contains(out, "1. Lớn nhất 340.0 ms, 2 lần") {
		t.Errorf("thiếu dòng câu 1:\n%s", out)
	}
	if got := renderSlow(nil, 3); !strings.Contains(got, "Không câu nào") {
		t.Errorf("không có câu chậm: %s", got)
	}
}
