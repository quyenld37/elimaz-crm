package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// pgQueue là JobQueue trên platform.jobs.
type pgQueue struct{}

// NewQueue trả JobQueue ghi vào platform.jobs.
func NewQueue() JobQueue { //nolint:ireturn // Interfaces của kế hoạch M0 Task 11: NewQueue trả interface JobQueue
	return pgQueue{}
}

func (pgQueue) Enqueue(ctx context.Context, tx pgx.Tx, k Kind, j NewJob) (uuid.UUID, bool, error) {
	p, err := k.Policy.normalized()
	if err != nil {
		return uuid.Nil, false, err
	}
	switch {
	case k.Name == "":
		return uuid.Nil, false, fmt.Errorf("%w: thiếu tên loại", ErrInvalidJob)
	case j.DedupeKey == "":
		return uuid.Nil, false, fmt.Errorf("%w: thiếu DedupeKey (%s)", ErrInvalidJob, k.Name)
	case j.TenantID != nil && *j.TenantID == uuid.Nil:
		return uuid.Nil, false, fmt.Errorf("%w: TenantID là uuid.Nil (%s)", ErrInvalidJob, k.Name)
	}
	payload := []byte("{}")
	if j.Payload != nil {
		if payload, err = json.Marshal(j.Payload); err != nil {
			return uuid.Nil, false, fmt.Errorf("%w: payload của %s: %w", ErrInvalidJob, k.Name, err)
		}
	}
	q := store.New(tx)
	tenant := db.NullUUID(j.TenantID)
	id, err := q.InsertJob(ctx, store.InsertJobParams{
		TenantID:       tenant,
		Kind:           k.Name,
		Mode:           p.Mode,
		Payload:        payload,
		DedupeKey:      j.DedupeKey,
		OrderKey:       pgtype.Text{String: j.OrderKey, Valid: j.OrderKey != ""},
		OrderSeq:       pgtype.Int8{Int64: j.OrderSeq, Valid: j.OrderKey != ""},
		OrderMode:      p.OrderMode,
		OnLeaseExpired: p.OnLeaseExpired,
		MaxAttempts:    int32(min(p.MaxAttempts, maxInt32)), //nolint:gosec // đã chặn trên
		RunAt:          pgtype.Timestamptz{Time: j.RunAt, Valid: !j.RunAt.IsZero()},
		ExpiresAfterMs: p.ExpiresAfter.Milliseconds(),
		TraceContext:   injectTrace(ctx),
	})
	switch {
	case err == nil:
		return id, true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, false, fmt.Errorf("ghi việc %s: %w", k.Name, err)
	}
	id, err = q.FindJobID(ctx, store.FindJobIDParams{TenantID: tenant, Kind: k.Name, DedupeKey: j.DedupeKey})
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("đọc việc trùng %s: %w", k.Name, err)
	}
	return id, false, nil
}

const maxInt32 = 1<<31 - 1

func fromPgUUID(id pgtype.UUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	u := uuid.UUID(id.Bytes)
	return &u
}
