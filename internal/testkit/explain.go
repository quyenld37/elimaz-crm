package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// errNoAutoExplain: kế hoạch có Function Scan (thân hàm, ví dụ hàm SECURITY DEFINER, không hiện trong EXPLAIN) mà
// auto_explain chưa bật cho crm_app hoặc không gửi kế hoạch nào về, nên không đọc được kế hoạch bên trong hàm.
var errNoAutoExplain = errors.New("testkit: kế hoạch có Function Scan nhưng auto_explain chưa bật (gọi EnableAutoExplain trước khi mở pool)")

// EnableAutoExplain bật auto_explain cho crm_app trong database của adminURL (tham số của role, áp cho kết nối mở sau
// lời gọi): ghi kế hoạch của mọi câu, kể cả câu lồng trong hàm, dạng JSON, mức NOTICE để ExplainAsApp đọc được.
// Database thử bị xóa cùng tham số khi test xong.
func EnableAutoExplain(t testing.TB, adminURL string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := connect(ctx, t, adminURL)
	defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
	exec(ctx, t, c, `DO $$
DECLARE
	s text[];
BEGIN
	FOREACH s SLICE 1 IN ARRAY ARRAY[
		['session_preload_libraries', 'auto_explain'],
		['auto_explain.log_min_duration', '0'],
		['auto_explain.log_nested_statements', 'on'],
		['auto_explain.log_level', 'notice'],
		['auto_explain.log_format', 'json']
	] LOOP
		EXECUTE format('ALTER ROLE crm_app IN DATABASE %I SET %s = %L', current_database(), s[1], s[2]);
	END LOOP;
END $$`)
}

// ExplainAsApp chạy EXPLAIN (ANALYZE, FORMAT JSON) của sql với args bằng crm_app trong ngữ cảnh tenantID, trên một kết
// nối riêng dựng từ cấu hình pool của appDB (để nhận NOTICE), rồi đóng. Trả tên mọi nút của kế hoạch theo thứ tự duyệt
// sâu, cộng các nút của câu lồng trong hàm mà auto_explain ghi (cần EnableAutoExplain). Câu thật sự chạy: chỉ dùng cho
// câu đọc trong test.
func ExplainAsApp(ctx context.Context, appDB *db.DB, tenantID uuid.UUID, sql string, args ...any) ([]string, error) {
	nc, err := dialNotices(ctx, appDB)
	if err != nil {
		return nil, err
	}
	defer nc.close(ctx)
	conn := nc.conn

	var nested string
	if err := conn.QueryRow(ctx, "SELECT set_config('app.tenant_id', $1, false), coalesce(current_setting('auto_explain.log_nested_statements', true), '')",
		tenantID.String()).Scan(new(string), &nested); err != nil {
		return nil, fmt.Errorf("testkit: đặt ngữ cảnh tenant: %w", err)
	}
	nc.take()
	var raw []byte
	if err := conn.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON, COSTS OFF, TIMING OFF, BUFFERS OFF) "+sql, args...).Scan(&raw); err != nil {
		return nil, fmt.Errorf("testkit: EXPLAIN: %w", err)
	}
	var top []struct {
		Plan planNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("testkit: đọc kế hoạch JSON: %w", err)
	}
	var nodes []string
	for _, p := range top {
		nodes = p.Plan.appendTypes(nodes)
	}
	hasFunction := slices.Contains(nodes, "Function Scan")
	if hasFunction && nested != "on" {
		return nil, errNoAutoExplain
	}
	logged := 0
	for _, msg := range nc.take() {
		_, body, ok := strings.Cut(msg, "plan:")
		if !ok {
			continue
		}
		var entry struct {
			Plan planNode `json:"Plan"`
		}
		if err := json.Unmarshal([]byte(body), &entry); err != nil {
			return nil, fmt.Errorf("testkit: đọc kế hoạch của auto_explain: %w", err)
		}
		nodes = entry.Plan.appendTypes(nodes)
		logged++
	}
	if hasFunction && logged == 0 {
		return nil, errNoAutoExplain
	}
	return nodes, nil
}

// planNode là một nút của kế hoạch JSON của PostgreSQL (chỉ phần cần).
type planNode struct {
	NodeType string     `json:"Node Type"`
	Relation string     `json:"Relation Name"`
	Plans    []planNode `json:"Plans"`
}

// seqScanned thêm vào out các bảng trong tables bị Seq Scan ở p hay nút con (mỗi bảng một lần).
func (p planNode) seqScanned(tables, out []string) []string {
	if p.NodeType == "Seq Scan" && slices.Contains(tables, p.Relation) && !slices.Contains(out, p.Relation) {
		out = append(out, p.Relation)
	}
	for _, c := range p.Plans {
		out = c.seqScanned(tables, out)
	}
	return out
}

// SeqScanTables chạy EXPLAIN (ANALYZE, FORMAT JSON) của sql với args bằng crm_app (appDB) trong ngữ cảnh tenantID; trả
// các bảng (tên không schema) trong tables bị Seq Scan và thời gian chạy của câu (ms). Dùng cho test kế hoạch trên dữ
// liệu lớn khi Seq Scan trên bảng nhỏ là kế hoạch đúng (chỉ đòi không quét bảng lớn). Câu thật sự chạy: chỉ câu đọc.
//
// Khi nào không dùng: câu gọi hàm SECURITY DEFINER (kế hoạch bên trong hàm không hiện ở đây: ExplainAsApp với
// EnableAutoExplain); đòi không Seq Scan trên mọi bảng (ExplainAsApp với RequireNoSeqScan).
func SeqScanTables(ctx context.Context, appDB *db.DB, tenantID uuid.UUID, tables []string, sql string, args ...any) ([]string, float64, error) {
	var raw []byte
	if err := tenancy.WithTenant(ctx, appDB, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON, COSTS OFF, TIMING OFF) "+sql, args...).Scan(&raw)
	}); err != nil {
		return nil, 0, fmt.Errorf("testkit: EXPLAIN: %w", err)
	}
	var top []struct {
		Plan planNode `json:"Plan"`
		Ms   float64  `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, 0, fmt.Errorf("testkit: đọc kế hoạch JSON: %w", err)
	}
	var out []string
	var ms float64
	for _, p := range top {
		out = p.Plan.seqScanned(tables, out)
		ms += p.Ms
	}
	return out, ms, nil
}

// StoreQuery đọc hằng câu name do sqlc sinh trong tệp Go file (đường dẫn từ thư mục của gói đang test, ví dụ
// "internal/store/lists.sql.go"; chuỗi thô giữa hai dấu `), để EXPLAIN đúng câu ứng dụng chạy. Không có hằng → test đỏ.
func StoreQuery(t testing.TB, file, name string) string {
	t.Helper()
	src, err := os.ReadFile(file) //nolint:gosec // G304: tệp mã sinh trong repo do test đặt
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(src), "const "+name+" = `")
	sql, _, closed := strings.Cut(rest, "`")
	if !ok || !closed {
		t.Fatalf("testkit: không thấy hằng %s trong %s", name, file)
	}
	return sql
}

func (p planNode) appendTypes(out []string) []string {
	out = append(out, p.NodeType)
	for _, c := range p.Plans {
		out = c.appendTypes(out)
	}
	return out
}

// RequireNoSeqScan đánh hỏng test khi nodes (từ ExplainAsApp) có "Seq Scan".
func RequireNoSeqScan(t testing.TB, nodes []string) {
	t.Helper()
	if slices.Contains(nodes, "Seq Scan") {
		t.Errorf("kế hoạch có Seq Scan: %v", nodes)
	}
}

// noticeConn là một kết nối riêng của crm_app (dựng từ cấu hình pool) giữ lại mọi NOTICE nó nhận (auto_explain ghi kế
// hoạch ở mức NOTICE).
type noticeConn struct {
	conn    *pgx.Conn
	mu      sync.Mutex
	notices []string
}

func dialNotices(ctx context.Context, appDB *db.DB) (*noticeConn, error) {
	nc := &noticeConn{}
	cfg := appDB.Pool.Config().ConnConfig.Copy()
	cfg.OnNotice = func(_ *pgconn.PgConn, n *pgconn.Notice) {
		nc.mu.Lock()
		nc.notices = append(nc.notices, n.Message)
		nc.mu.Unlock()
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("testkit: nối crm_app: %w", err)
	}
	nc.conn = conn
	return nc, nil
}

// take trả các NOTICE đã nhận từ lần take trước và xóa chúng.
func (nc *noticeConn) take() []string {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	out := nc.notices
	nc.notices = nil
	return out
}

func (nc *noticeConn) close(ctx context.Context) { _ = nc.conn.Close(context.WithoutCancel(ctx)) }

// AutoExplainPlansAsApp chạy sql với args bằng crm_app trong ngữ cảnh tenantID trên một kết nối riêng (bỏ các dòng kết
// quả), rồi trả nút gốc ("Plan", JSON đã giải) của mọi kế hoạch mà auto_explain ghi trong lúc chạy, kể cả câu lồng trong
// hàm. Cần EnableAutoExplain trước khi mở pool; câu thật sự chạy: chỉ dùng cho câu đọc trong test.
func AutoExplainPlansAsApp(ctx context.Context, appDB *db.DB, tenantID uuid.UUID, sql string, args ...any) ([]map[string]any, error) {
	nc, err := dialNotices(ctx, appDB)
	if err != nil {
		return nil, err
	}
	defer nc.close(ctx)
	if _, err := nc.conn.Exec(ctx, "SELECT set_config('app.tenant_id', $1, false)", tenantID.String()); err != nil {
		return nil, fmt.Errorf("testkit: đặt ngữ cảnh tenant: %w", err)
	}
	nc.take()
	rows, err := nc.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("testkit: chạy câu: %w", err)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("testkit: chạy câu: %w", err)
	}
	var plans []map[string]any
	for _, msg := range nc.take() {
		_, body, ok := strings.Cut(msg, "plan:")
		if !ok {
			continue
		}
		var entry struct {
			Plan map[string]any `json:"Plan"`
		}
		if err := json.Unmarshal([]byte(body), &entry); err != nil {
			return nil, fmt.Errorf("testkit: đọc kế hoạch của auto_explain: %w", err)
		}
		if entry.Plan != nil {
			plans = append(plans, entry.Plan)
		}
	}
	return plans, nil
}
