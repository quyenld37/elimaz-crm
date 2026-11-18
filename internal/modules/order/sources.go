package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// trafficSourceNameKey là unique (tenant_id, name) của ord.traffic_sources (migration 0204).
const trafficSourceNameKey = "traffic_sources_name_key"

// maxSourceNameRunes khớp CHECK length(btrim(name)) BETWEEN 1 AND 100 của bảng.
const maxSourceNameRunes = 100

// Source là một nguồn đơn của shop (kế hoạch M2 Task M2-15). Key rỗng với nguồn nhân viên tạo; nguồn hệ thống có key
// cố định (SourceKey…). Danh sách xếp theo Position rồi tên.
type Source struct {
	ID       uuid.UUID
	Key      string
	Name     string
	Active   bool
	Position int32
	Version  int64
}

// SourcePatch là phần sửa nguồn đơn: trường nil giữ nguyên.
type SourcePatch struct {
	Name     *string
	Active   *bool
	Position *int32
}

// SourceChange là nguồn trước và sau một lần sửa (bên gọi ghi audit).
type SourceChange struct {
	Before, After Source
}

// AuditFields là giá trị đưa vào audit (không có dữ liệu cá nhân).
func (s Source) AuditFields() map[string]any {
	return map[string]any{
		"key": s.Key, "name": s.Name, "active": s.Active, "position": s.Position, "version": s.Version,
	}
}

// ListSources trả mọi nguồn đơn của tenant đang chạy (kể cả nguồn đã tắt), theo vị trí rồi tên.
func ListSources(ctx context.Context, tx pgx.Tx) ([]Source, error) {
	rows, err := store.New(tx).ListTrafficSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc nguồn đơn: %w", err)
	}
	out := make([]Source, 0, len(rows))
	for _, r := range rows {
		out = append(out, Source(r))
	}
	return out, nil
}

// CreateSource tạo nguồn đơn tên name (bỏ khoảng trắng hai đầu; rỗng hay quá 100 ký tự → ErrInvalidInput), đang bật,
// đứng cuối danh sách. Trùng tên một nguồn có sẵn (so phân biệt hoa thường như unique của bảng) →
// ErrTrafficSourceNameTaken. Nguồn thuộc tenant của tx.
func (Writer) CreateSource(ctx context.Context, tx pgx.Tx, name string) (Source, error) {
	name, err := sourceName(name)
	if err != nil {
		return Source{}, err
	}
	r, err := store.New(tx).InsertTrafficSource(ctx, name)
	if sourceNameTaken(err) {
		return Source{}, ErrTrafficSourceNameTaken
	}
	if err != nil {
		return Source{}, fmt.Errorf("tạo nguồn đơn: %w", err)
	}
	return Source(r), nil
}

// UpdateSource đổi tên, bật tắt, vị trí (≥ 0) của nguồn id khi version còn khớp: khóa dòng, so version (khác →
// ErrVersionConflict). Không có (hay của tenant khác) → ErrNotFound; tên sai → ErrInvalidInput; tên trùng →
// ErrTrafficSourceNameTaken. Nguồn hệ thống đổi tên được, key giữ nguyên.
func (Writer) UpdateSource(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, p SourcePatch) (SourceChange, error) {
	q := store.New(tx)
	cur, err := q.LockTrafficSource(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return SourceChange{}, ErrNotFound
	}
	if err != nil {
		return SourceChange{}, fmt.Errorf("khóa nguồn đơn: %w", err)
	}
	if cur.Version != version {
		return SourceChange{}, ErrVersionConflict
	}
	next := Source(cur)
	if p.Name != nil {
		if next.Name, err = sourceName(*p.Name); err != nil {
			return SourceChange{}, err
		}
	}
	if p.Active != nil {
		next.Active = *p.Active
	}
	if p.Position != nil {
		if *p.Position < 0 {
			return SourceChange{}, fmt.Errorf("%w: vị trí nguồn âm", ErrInvalidInput)
		}
		next.Position = *p.Position
	}
	r, err := q.UpdateTrafficSource(ctx, store.UpdateTrafficSourceParams{
		Name: next.Name, Active: next.Active, Position: next.Position, ID: id, Version: version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return SourceChange{}, ErrVersionConflict // không xảy ra khi đã giữ khóa dòng; giữ để an toàn
	}
	if sourceNameTaken(err) {
		return SourceChange{}, ErrTrafficSourceNameTaken
	}
	if err != nil {
		return SourceChange{}, fmt.Errorf("sửa nguồn đơn: %w", err)
	}
	return SourceChange{Before: Source(cur), After: Source(r)}, nil
}

// ActiveSource trả nil khi nguồn id có trong tenant đang chạy và đang bật; không có, của tenant khác hay đã tắt →
// ErrTrafficSourceInactive. Chỉ đọc, không khóa (Task M2-16 gọi khi lên đơn).
func ActiveSource(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	active, err := store.New(tx).TrafficSourceActive(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return ErrTrafficSourceInactive
	}
	if err != nil {
		return fmt.Errorf("đọc nguồn đơn: %w", err)
	}
	return nil
}

func sourceName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > maxSourceNameRunes {
		return "", fmt.Errorf("%w: tên nguồn rỗng hoặc quá %d ký tự", ErrInvalidInput, maxSourceNameRunes)
	}
	return s, nil
}

// sourceNameTaken: err là vi phạm unique tên nguồn (traffic_sources_name_key).
func sourceNameTaken(err error) bool {
	pg, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pg.Code == "23505" && pg.ConstraintName == trafficSourceNameKey
}
