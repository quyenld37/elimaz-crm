package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Đường mua flash sale theo 04 mục 15 bước 4 và 05 mục 7.1 (mã đo của Task 1, không phải mã sản phẩm):
//  1. bước ngắn, commit riêng: ghi khóa chống trùng `running` (hoặc trả kết quả đã lưu, 409, 422);
//  2. một transaction: giới hạn mỗi khách, lấy đơn vị SKIP LOCKED, tạo đơn nháp + dòng đơn + giữ hàng,
//     chuyển khóa sang `done` kèm kết quả.
// Kết quả cuối (bought, sold_out, over_limit) được lưu; "retry" và 503 thì không (dòng khóa bị xóa).
// Mọi transaction đi qua retryTx: bản sao tối giản của db.RetryTx theo ADR-46 (mã đo không nhập internal/).

const (
	keyHold      = 60 * time.Second
	retryAfterMS = 500
	maxQty       = 100

	// ADR-46, giá trị gợi ý cho lượt mua flash sale; đặt theo transaction.
	txLockTimeout      = "200ms"
	txStatementTimeout = "2s"
	maxTxRetries       = 3
)

var (
	errKeyBusy     = errors.New("khóa chống trùng đang chạy")
	errKeyMismatch = errors.New("khóa chống trùng dùng lại với nội dung khác")
	errKeyLost     = errors.New("mất khóa chống trùng giữa chừng")
	errRetry       = errors.New("đơn vị đang bị khóa, thử lại")
	errRetryLater  = errors.New("database bận: hết lượt chạy lại transaction")

	keyPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

type buyServer struct {
	pool    *pgxpool.Pool
	limit   int
	sem     chan struct{}
	mux     *http.ServeMux
	retries txRetryCounts
}

// newBuyServer trả handler của POST /buy. limit: số suất tối đa mỗi khách; maxInflight: số lượt mua
// được xử lý cùng lúc, quá thì 503 retry ngay, không chạm database (chế độ "Valkey ngừng" của ADR-40).
func newBuyServer(pool *pgxpool.Pool, limit, maxInflight int) *buyServer {
	s := &buyServer{pool: pool, limit: limit, sem: make(chan struct{}, maxInflight), mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /buy", s.handleBuy)
	return s
}

func (s *buyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

type buyRequest struct {
	ItemID     string `json:"item_id"`
	CustomerID string `json:"customer_id"`
	Qty        int    `json:"qty"`
}

// hash là mã băm của nội dung đã chuẩn hóa: cùng nội dung khác khoảng trắng vẫn cùng mã.
func (r buyRequest) hash() []byte {
	h := sha256.Sum256(fmt.Appendf(nil, "%s|%s|%d", r.ItemID, r.CustomerID, r.Qty))
	return h[:]
}

// response là câu trả lời đã chuẩn hóa: body là JSON khóa xếp theo chữ cái, nên câu trả lời phát lại
// từ jsonb giống từng byte câu trả lời lần đầu.
type response struct {
	status int
	body   map[string]any
}

func retryResponse() response {
	return response{http.StatusServiceUnavailable, map[string]any{"result": "retry", "after_ms": retryAfterMS}}
}

func errorResponse(status int, code string) response {
	return response{status, map[string]any{"error": code}}
}

func (s *buyServer) handleBuy(w http.ResponseWriter, r *http.Request) {
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		writeResponse(w, retryResponse())
		return
	}

	key := r.Header.Get("Idempotency-Key")
	if !keyPattern.MatchString(key) {
		writeResponse(w, errorResponse(http.StatusBadRequest, "bad_idempotency_key"))
		return
	}
	tenant := strings.ToLower(r.Header.Get("X-Tenant"))
	if !uuidPattern.MatchString(tenant) {
		writeResponse(w, errorResponse(http.StatusBadRequest, "bad_tenant"))
		return
	}
	var req buyRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeResponse(w, errorResponse(http.StatusBadRequest, "bad_body"))
		return
	}
	req.ItemID, req.CustomerID = strings.ToLower(req.ItemID), strings.ToLower(req.CustomerID)
	if !uuidPattern.MatchString(req.ItemID) || !uuidPattern.MatchString(req.CustomerID) || req.Qty < 1 || req.Qty > maxQty {
		writeResponse(w, errorResponse(http.StatusBadRequest, "bad_body"))
		return
	}

	res, err := s.buy(r.Context(), tenant, key, req)
	switch {
	case err == nil:
		writeResponse(w, res)
	case errors.Is(err, errKeyBusy), errors.Is(err, errKeyLost):
		writeResponse(w, errorResponse(http.StatusConflict, "idempotency_key_in_progress"))
	case errors.Is(err, errKeyMismatch):
		writeResponse(w, errorResponse(http.StatusUnprocessableEntity, "idempotency_key_reused"))
	case errors.Is(err, errRetry), errors.Is(err, errRetryLater):
		writeResponse(w, retryResponse())
	case r.Context().Err() != nil:
		// Khách đã ngắt kết nối; không còn ai nhận câu trả lời.
	default:
		slog.Error("mua", "err", err)
		writeResponse(w, errorResponse(http.StatusInternalServerError, "internal"))
	}
}

// buy chạy hai bước. Mọi lối ra không phải kết quả cuối đều xóa dòng khóa của chính lượt này.
func (s *buyServer) buy(ctx context.Context, tenant, key string, req buyRequest) (response, error) {
	token, stored, err := s.claimKey(ctx, tenant, key, req.hash())
	if err != nil {
		return response{}, err
	}
	if stored != nil {
		return *stored, nil
	}
	res, err := s.purchase(ctx, tenant, key, token, req)
	if err != nil {
		// Dọn cả khi khách đã ngắt kết nối; xóa có điều kiện nên không đụng khóa đã `done`.
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if rerr := s.releaseKey(cctx, tenant, key, token); rerr != nil {
			err = errors.Join(err, fmt.Errorf("xóa dòng khóa: %w", rerr))
		}
		return response{}, err
	}
	return res, nil
}

// claimKey là bước 1. Trả token (locked_until của lượt này) khi giữ được khóa, hoặc câu trả lời đã lưu.
func (s *buyServer) claimKey(ctx context.Context, tenant, key string, hash []byte) (time.Time, *response, error) {
	for range 3 {
		var (
			token  time.Time
			stored *response
			gone   bool
		)
		err := s.retryTx(ctx, tenant, func(tx pgx.Tx) error {
			token, stored, gone = time.Time{}, nil, false // chạy lại được: gán lại mỗi lần
			err := tx.QueryRow(ctx, `
				INSERT INTO spike.idempotency_keys AS k (tenant_id, key, request_hash, status, locked_until)
				VALUES ($1, $2, $3, 'running', now() + $4::interval)
				ON CONFLICT (tenant_id, key) DO UPDATE SET locked_until = EXCLUDED.locked_until
				WHERE k.status = 'running' AND k.locked_until < now() AND k.request_hash = EXCLUDED.request_hash
				RETURNING k.locked_until`, tenant, key, hash, keyHold).Scan(&token)
			if err == nil {
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("ghi khóa: %w", err)
			}
			var (
				status, body string
				prevHash     []byte
				respStatus   *int
			)
			err = tx.QueryRow(ctx, `
				SELECT status, request_hash, response_status, coalesce(response_body::text, '')
				FROM spike.idempotency_keys WHERE tenant_id = $1 AND key = $2`, tenant, key).
				Scan(&status, &prevHash, &respStatus, &body)
			if errors.Is(err, pgx.ErrNoRows) {
				gone = true // lượt trước vừa xóa dòng khóa ("retry"): thử ghi lại
				return nil
			}
			if err != nil {
				return fmt.Errorf("đọc khóa: %w", err)
			}
			switch {
			case string(prevHash) != string(hash):
				return errKeyMismatch
			case status == "done" && respStatus != nil:
				var m map[string]any
				if err := json.Unmarshal([]byte(body), &m); err != nil {
					return fmt.Errorf("đọc kết quả đã lưu: %w", err)
				}
				stored = &response{*respStatus, m}
				return nil
			default:
				return errKeyBusy
			}
		})
		if err != nil {
			return time.Time{}, nil, err
		}
		if !gone {
			return token, stored, nil
		}
	}
	return time.Time{}, nil, errKeyBusy
}

// purchase là bước 2: một transaction. Trả errRetry (đã lùi) khi lấy thiếu đơn vị vì có dòng đang bị khóa.
func (s *buyServer) purchase(ctx context.Context, tenant, key string, token time.Time, req buyRequest) (response, error) {
	var res response
	err := s.retryTx(ctx, tenant, func(tx pgx.Tx) error {
		res = response{} // chạy lại được: gán lại mỗi lần
		tag, err := tx.Exec(ctx, `
			INSERT INTO spike.flash_sale_claims AS c (tenant_id, item_id, customer_id, quantity)
			SELECT $1::uuid, $2::uuid, $3::uuid, $4::int WHERE $4::int <= $5::int
			ON CONFLICT (tenant_id, item_id, customer_id) DO UPDATE SET quantity = c.quantity + EXCLUDED.quantity
			WHERE c.quantity + EXCLUDED.quantity <= $5`,
			tenant, req.ItemID, req.CustomerID, req.Qty, s.limit)
		if err != nil {
			return fmt.Errorf("ghi giới hạn khách: %w", err)
		}
		if tag.RowsAffected() == 0 {
			res = response{http.StatusOK, map[string]any{"result": "over_limit"}}
			return finishKey(ctx, tx, tenant, key, token, res)
		}

		rows, err := tx.Query(ctx, `
			SELECT id FROM spike.flash_sale_units
			WHERE tenant_id = $1 AND item_id = $2 AND status = 'available'
			ORDER BY id LIMIT $3 FOR UPDATE SKIP LOCKED`, tenant, req.ItemID, req.Qty)
		if err != nil {
			return fmt.Errorf("lấy đơn vị: %w", err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[pgtype.UUID])
		if err != nil {
			return fmt.Errorf("lấy đơn vị: %w", err)
		}

		if len(ids) < req.Qty {
			// Lấy thiếu. Còn dòng `available` (đang bị transaction khác khóa) thì "thử lại", không báo hết suất.
			var anyAvailable bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM spike.flash_sale_units
				               WHERE tenant_id = $1 AND item_id = $2 AND status = 'available')`,
				tenant, req.ItemID).Scan(&anyAvailable); err != nil {
				return fmt.Errorf("kiểm còn suất: %w", err)
			}
			if anyAvailable {
				return errRetry
			}
			// Hết suất là kết quả cuối: trả lại phần giới hạn vừa ghi (dòng đang bị chính transaction này khóa).
			if _, err := tx.Exec(ctx, `
				UPDATE spike.flash_sale_claims SET quantity = quantity - $4
				WHERE tenant_id = $1 AND item_id = $2 AND customer_id = $3`,
				tenant, req.ItemID, req.CustomerID, req.Qty); err != nil {
				return fmt.Errorf("trả phần giới hạn: %w", err)
			}
			res = response{http.StatusOK, map[string]any{"result": "sold_out"}}
			return finishKey(ctx, tx, tenant, key, token, res)
		}

		var (
			orderID pgtype.UUID
			held    int
		)
		if err := tx.QueryRow(ctx, `
			WITH o AS (
				INSERT INTO spike.orders (tenant_id, customer_id, expires_at)
				VALUES ($1, $2, now() + interval '15 minutes') RETURNING id),
			oi AS (
				INSERT INTO spike.order_items (tenant_id, order_id, item_id, qty)
				SELECT $1::uuid, o.id, $3::uuid, $4::int FROM o RETURNING id),
			sr AS (
				INSERT INTO spike.stock_reservations (tenant_id, order_item_id, qty)
				SELECT $1::uuid, oi.id, $4::int FROM oi),
			u AS (
				UPDATE spike.flash_sale_units SET status = 'held', order_item_id = oi.id
				FROM oi WHERE tenant_id = $1 AND flash_sale_units.id = ANY($5::uuid[]) RETURNING 1)
			SELECT o.id, (SELECT count(*) FROM u) FROM o`,
			tenant, req.CustomerID, req.ItemID, req.Qty, ids).Scan(&orderID, &held); err != nil {
			return fmt.Errorf("tạo đơn nháp: %w", err)
		}
		if held != req.Qty {
			return fmt.Errorf("giữ %d đơn vị, muốn %d", held, req.Qty)
		}
		res = response{http.StatusOK, map[string]any{"result": "bought", "order_id": orderID.String()}}
		return finishKey(ctx, tx, tenant, key, token, res)
	})
	if err != nil {
		return response{}, err
	}
	return res, nil
}

// finishKey chuyển khóa của chính lượt này sang `done` kèm kết quả, trong transaction nghiệp vụ.
func finishKey(ctx context.Context, tx pgx.Tx, tenant, key string, token time.Time, res response) error {
	body, err := json.Marshal(res.body)
	if err != nil {
		return fmt.Errorf("mã hóa kết quả: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE spike.idempotency_keys SET status = 'done', response_status = $4, response_body = $5
		WHERE tenant_id = $1 AND key = $2 AND status = 'running' AND locked_until = $3`,
		tenant, key, token, res.status, body)
	if err != nil {
		return fmt.Errorf("ghi kết quả khóa: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errKeyLost
	}
	return nil
}

// releaseKey xóa dòng khóa `running` của chính lượt này (token khớp), để lượt thử lại chạy từ đầu.
func (s *buyServer) releaseKey(ctx context.Context, tenant, key string, token time.Time) error {
	return s.retryTx(ctx, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			DELETE FROM spike.idempotency_keys
			WHERE tenant_id = $1 AND key = $2 AND status = 'running' AND locked_until = $3`, tenant, key, token)
		return err
	})
}

// inTenantTx chạy fn trong một transaction có ngữ cảnh tenant (set_config cục bộ của transaction).
func inTenantTx(ctx context.Context, pool *pgxpool.Pool, tenant string, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenant); err != nil {
			return fmt.Errorf("đặt tenant: %w", err)
		}
		return fn(tx)
	})
}

// retryTx là bản sao tối giản của db.RetryTx (ADR-46): một transaction đặt lock_timeout, statement_timeout
// và tenant trong cùng một câu set_config; gặp 40P01, 55P03, 40001 (kể cả lúc commit) thì lùi, chờ
// 20/40/80 ms × [0,5; 1,5] rồi chạy lại toàn bộ fn, tối đa 3 lần; hết lượt trả lỗi bọc errRetryLater.
// Lỗi khác (kể cả 57014) trả nguyên, không chạy lại. fn phải chạy lại được an toàn (không gọi mạng,
// gán lại kết quả mỗi lần).
func (s *buyServer) retryTx(ctx context.Context, tenant string, fn func(pgx.Tx) error) error {
	for attempt := 0; ; attempt++ {
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT set_config('lock_timeout', $1, true),
				set_config('statement_timeout', $2, true), set_config('app.tenant_id', $3, true)`,
				txLockTimeout, txStatementTimeout, tenant); err != nil {
				return fmt.Errorf("đặt giới hạn và tenant: %w", err)
			}
			return fn(tx)
		})
		code := retryableCode(err)
		if code == "" {
			return err
		}
		if attempt == maxTxRetries {
			s.retries.add("exhausted")
			return fmt.Errorf("%w: %w", errRetryLater, err)
		}
		s.retries.add(code)
		wait := time.Duration(float64(20*time.Millisecond<<attempt) * (0.5 + rand.Float64()))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// retryableCode trả SQLSTATE khi lỗi là loại chắc chắn chưa commit và chạy lại được (ADR-46), ngược lại "".
func retryableCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "40001", "40P01", "55P03":
			return pgErr.Code
		}
	}
	return ""
}

// txRetryCounts đếm số lần chạy lại theo SQLSTATE và số lần hết lượt ("exhausted").
type txRetryCounts struct {
	mu sync.Mutex
	n  map[string]int
}

func (c *txRetryCounts) add(code string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == nil {
		c.n = map[string]int{}
	}
	c.n[code]++
}

func (c *txRetryCounts) snapshot() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int, len(c.n))
	for k, v := range c.n {
		out[k] = v
	}
	return out
}

func writeResponse(w http.ResponseWriter, res response) {
	body, err := json.Marshal(res.body)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.status)
	_, _ = w.Write(body)
}
