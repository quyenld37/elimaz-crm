package main

import (
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

// goodInput là một lần chạy đạt mọi ngưỡng của bài thường (04 mục 1).
func goodInput() reportInput {
	hist := make([]int, len(metrics.HistBoundsMs)+1)
	hist[0] = 1000 // mọi lần nhận việc ≤ 50 ms
	return reportInput{
		Mode: modeNormal,
		K6: k6Metrics{
			"webhook_ms":  {Values: map[string]float64{"p(95)": 40, "p(99)": 90}},
			"read_ms":     {Values: map[string]float64{"p(95)": 30, "p(99)": 80}},
			"webhook_ok":  {Values: map[string]float64{"count": 500}},
			"webhook_bad": {Values: map[string]float64{"count": 0}},
		},
		SSE: &sseResult{
			Run: "r1", Clients: []clientResult{{Unique: 500}, {Unique: 500}},
			Latency: latencyResult{Count: 1000, P95: 300, P99: 900},
		},
		DB: dbStats{InboundDone: 500, PickupHist: hist},
	}
}

func statusOf(t *testing.T, vs []verdict, name string) string {
	t.Helper()
	for _, v := range vs {
		if v.Name == name {
			return v.Status
		}
	}
	t.Fatalf("không có tiêu chí %q trong %+v", name, vs)
	return ""
}

func allPass(t *testing.T, vs []verdict) {
	t.Helper()
	for _, v := range vs {
		if v.Status != statusPass {
			t.Fatalf("%s: %s (muốn %s, nhận %s)", v.Name, v.Status, v.Want, v.Got)
		}
	}
}

func TestEvaluateNormalAllPass(t *testing.T) {
	vs := evaluate(goodInput())
	allPass(t, vs)
	for _, name := range []string{
		critWebhookP95, critPipeP95, critPipeP99, critPickupP95, critPickupP99, critReadP95,
		critReadP99, critWebhookNon200, critSSEExactlyOnce, critNoLoss,
	} {
		statusOf(t, vs, name)
	}
}

// Mỗi ngưỡng vượt một chút thì đúng tiêu chí đó không đạt.
func TestEvaluateEachThresholdFails(t *testing.T) {
	cases := []struct {
		name    string
		crit    string
		breakIt func(*reportInput)
	}{
		{"webhook p95", critWebhookP95, func(in *reportInput) { in.K6["webhook_ms"].Values["p(95)"] = 300 }},
		{"đường ống p95", critPipeP95, func(in *reportInput) { in.SSE.Latency.P95 = 2000 }},
		{"đường ống p99", critPipeP99, func(in *reportInput) { in.SSE.Latency.P99 = 5000 }},
		{"nhận việc p95", critPickupP95, func(in *reportInput) { in.DB.PickupHist[0], in.DB.PickupHist[7] = 940, 60 }},
		{"nhận việc p99", critPickupP99, func(in *reportInput) { in.DB.PickupHist[0], in.DB.PickupHist[8] = 980, 20 }},
		{"đọc p95", critReadP95, func(in *reportInput) { in.K6["read_ms"].Values["p(95)"] = 200 }},
		{"đọc p99", critReadP99, func(in *reportInput) { in.K6["read_ms"].Values["p(99)"] = 500 }},
		{"webhook khác 200", critWebhookNon200, func(in *reportInput) { in.K6["webhook_bad"].Values["count"] = 1 }},
		{"client thiếu", critSSEExactlyOnce, func(in *reportInput) { in.SSE.Clients[1].Unique = 499 }},
		{"client trùng", critSSEExactlyOnce, func(in *reportInput) { in.SSE.Clients[0].Duplicates = 1 }},
		{"mất sự kiện", critNoLoss, func(in *reportInput) { in.DB.InboundDone = 499 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := goodInput()
			tc.breakIt(&in)
			vs := evaluate(in)
			if s := statusOf(t, vs, tc.crit); s != statusFail {
				t.Fatalf("%s = %s, muốn %s", tc.crit, s, statusFail)
			}
			for _, v := range vs {
				if v.Name != tc.crit && v.Status != statusPass {
					t.Fatalf("chỉ %s được đổi, %s = %s", tc.crit, v.Name, v.Status)
				}
			}
		})
	}
}

// Thiếu số đo (không có tệp SSE, không có histogram) là CHƯA ĐO, không phải ĐẠT.
func TestEvaluateMissingDataIsNotPass(t *testing.T) {
	in := goodInput()
	in.SSE = nil
	in.DB.PickupHist = nil
	delete(in.K6, "read_ms")
	vs := evaluate(in)
	for _, c := range []string{critPipeP95, critSSEExactlyOnce, critPickupP95, critReadP95} {
		if s := statusOf(t, vs, c); s != statusNA {
			t.Fatalf("%s = %s, muốn %s", c, s, statusNA)
		}
	}
}

func TestEvaluateChaos(t *testing.T) {
	in := goodInput()
	in.Mode = modeChaos
	in.SSE.Clients[1].Unique = 100 // mất kết nối lúc giết api: không đòi đủ, chỉ đòi không trùng
	in.SSE.Latency.P99 = 60_000    // số đo lúc hỏng chỉ để tham khảo
	in.DB.CommandsTotal = 40
	in.Chaos = []chaosEvent{{Fault: "kill crm-api", RecoverySec: 12, Recovered: true}, {Fault: "kill crm-worker", RecoverySec: 30, Recovered: true}}
	vs := evaluate(in)
	allPass(t, vs)
	statusOf(t, vs, critCommandsOnce)
	statusOf(t, vs, critRecovery+": kill crm-api")
	for _, v := range vs {
		if v.Name == critPipeP99 || v.Name == critSSEExactlyOnce {
			t.Fatalf("bài hỏng giữa tải không chấm %s", v.Name)
		}
	}
	in.DB.CommandsResent = 1
	in.Chaos[1] = chaosEvent{Fault: "kill crm-worker", RecoverySec: 121, Recovered: true}
	in.SSE.Clients[0].Duplicates = 2
	vs = evaluate(in)
	for _, c := range []string{critCommandsOnce, critRecovery + ": kill crm-worker", critSSENoDup} {
		if s := statusOf(t, vs, c); s != statusFail {
			t.Fatalf("%s = %s, muốn %s", c, s, statusFail)
		}
	}
	in.Chaos[1] = chaosEvent{Fault: "kill crm-worker", Recovered: false}
	if s := statusOf(t, evaluate(in), critRecovery+": kill crm-worker"); s != statusFail {
		t.Fatalf("không trở lại = %s", s)
	}
	in.Chaos = nil
	if s := statusOf(t, evaluate(in), critRecovery); s != statusNA {
		t.Fatalf("không có lần hỏng nào = %s", s)
	}
}

func memSamples(service string, start time.Time, every time.Duration, mib ...float64) []statSample {
	out := make([]statSample, len(mib))
	for i, m := range mib {
		out[i] = statSample{At: start.Add(time.Duration(i) * every), Service: service, MemBytes: int64(m * (1 << 20))}
	}
	return out
}

func TestEvaluateSoak(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	in := goodInput()
	in.Mode = modeSoak
	in.Start = start
	// 8 giờ, mẫu mỗi 30 phút (17 mẫu): giờ đầu 100 MiB, cuối 115 MiB (+15%).
	mib := []float64{50, 80, 100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 114, 115}
	in.Stats = memSamples("crm-api", start, 30*time.Minute, mib...)
	in.DBSeries = []dbStats{
		{At: start.Add(time.Hour), JobsBytes: 900, JobsDeadTuples: 1000},
		{At: start.Add(2 * time.Hour), JobsBytes: 1000, JobsDeadTuples: 2000},
		{At: start.Add(8 * time.Hour), JobsBytes: 1000, JobsDeadTuples: 3000},
	}
	vs := evaluate(in)
	allPass(t, vs)
	statusOf(t, vs, critSoakMemory+": crm-api")
	statusOf(t, vs, critSoakDeadTuples)
	statusOf(t, vs, critSoakJobsSize)

	in.Stats = memSamples("crm-api", start, 30*time.Minute, append(mib[:16:16], 121)...)
	in.DBSeries[2].JobsBytes = 1001
	in.DBSeries[1].JobsDeadTuples = 50_000
	vs = evaluate(in)
	for _, c := range []string{critSoakMemory + ": crm-api", critSoakJobsSize, critSoakDeadTuples} {
		if s := statusOf(t, vs, c); s != statusFail {
			t.Fatalf("%s = %s, muốn %s", c, s, statusFail)
		}
	}

	// Ngâm rút ngắn (40 phút): không đủ giờ đầu, giờ thứ hai → CHƯA ĐO, không ĐẠT.
	in.Stats = memSamples("crm-api", start, 10*time.Minute, 50, 60, 70, 80, 90)
	in.DBSeries = []dbStats{{At: start.Add(40 * time.Minute), JobsBytes: 10, JobsDeadTuples: 5}}
	vs = evaluate(in)
	for _, c := range []string{critSoakMemory + ": crm-api", critSoakJobsSize} {
		if s := statusOf(t, vs, c); s != statusNA {
			t.Fatalf("%s = %s, muốn %s", c, s, statusNA)
		}
	}
	if s := statusOf(t, vs, critSoakDeadTuples); s != statusPass {
		t.Fatalf("dòng chết 5 = %s", s)
	}
}

func TestParseDockerStats(t *testing.T) {
	in := strings.Join([]string{
		"1700000000,crm-t22-crm-api-1,1.50%,12.5MiB / 512MiB",
		"1700000030,crm-t22-crm-worker-1,105.00%,1.2GiB / 2GiB",
		"1700000060,crm-t22-postgres-1,0.00%,900KiB / 7.6GiB",
		"1700000090,crm-t22-crm-ingest-1,--,0B / 0B",
		"dòng hỏng",
	}, "\n")
	got, err := parseDockerStats(strings.NewReader(in), "crm-t22")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("%d mẫu: %+v", len(got), got)
	}
	if got[0].Service != "crm-api" || got[0].CPUPct != 1.5 || got[0].MemBytes != 13_107_200 {
		t.Fatalf("mẫu 0 = %+v", got[0])
	}
	if got[1].Service != "crm-worker" || got[1].MemBytes != parseSize("1.2GiB") || got[1].MemBytes < 1_288_000_000 || got[1].CPUPct != 105 {
		t.Fatalf("mẫu 1 = %+v", got[1])
	}
	if got[2].Service != "postgres" || got[2].MemBytes != 900<<10 {
		t.Fatalf("mẫu 2 = %+v", got[2])
	}
	if got[3].CPUPct != 0 || got[3].MemBytes != 0 {
		t.Fatalf("mẫu 3 (container đang chết) = %+v", got[3])
	}
}

func TestParseChaos(t *testing.T) {
	in := "fault,start,recovered\nkill crm-api,1700000000,1700000012.5\nkill crm-worker,1700000180,\n"
	got, err := parseChaos(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Recovered || got[0].RecoverySec != 12.5 || got[1].Recovered {
		t.Fatalf("chaos = %+v", got)
	}
}

// Bảng markdown có từng tiêu chí và tóm tắt CPU, bộ nhớ theo dịch vụ.
func TestRenderMarkdown(t *testing.T) {
	in := goodInput()
	start := time.Unix(1_700_000_000, 0)
	in.Stats = append(memSamples("crm-api", start, time.Minute, 10, 20), statSample{At: start, Service: "crm-api", CPUPct: 50})
	out := renderMarkdown(in, evaluate(in))
	for _, want := range []string{"| " + critWebhookP95 + " |", statusPass, "| crm-api |", "20.0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("thiếu %q trong:\n%s", want, out)
		}
	}
}
