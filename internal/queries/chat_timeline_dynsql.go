package queries

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Loại mục hội thoại của dòng thời gian khách (C7, Q-X6; chuỗi mở TimelineItem.kind của openapi.yaml).
const (
	TimelineConversation = "conversation"
	TimelineCall         = "call"
)

// TimelineKey là khóa keyset của dòng thời gian khách (at giảm, kind theo byte giảm, ref_id giảm), cùng thứ tự với
// customer.Timeline.
type TimelineKey struct {
	At    time.Time
	Kind  string
	RefID uuid.UUID
}

// ChatTimelineRow là một mục hội thoại hay cuộc gọi. Summary chỉ mang mã và kết quả (không nội dung tin, ghi chú cuộc
// gọi): hội thoại {conversationId, pageId, kind: inbox|comment}; cuộc gọi {outcome, conversationId?}. ActorID là người
// gọi (cuộc gọi), nil ở hội thoại.
type ChatTimelineRow struct {
	At      time.Time
	Kind    string
	RefID   uuid.UUID
	ActorID *uuid.UUID
	Summary map[string]string
}

// chatTimelineSQL: hội thoại (mỗi hội thoại một mục lúc tạo) và cuộc gọi của các hồ sơ $1, lọc Page ($2 = mọi Page,
// hoặc Page thuộc $3; cuộc gọi không gắn hội thoại luôn hiện, gắn hội thoại của Page không được xem thì bỏ); keyset
// ($4, $5, $6) giảm dần như customer.ListTimeline. Đọc view của conversation (security_invoker, RLS của tenant).
const chatTimelineSQL = `WITH items AS (
    SELECT c.created_at AS at, 'conversation'::text AS kind, c.id AS ref_id, NULL::uuid AS actor_id,
           jsonb_build_object('conversationId', c.id::text, 'pageId', c.channel_account_id::text, 'kind', c.kind) AS summary
    FROM conversation.conversations_v AS c
    WHERE c.customer_id = ANY($1::uuid[])
      AND ($2::boolean OR c.channel_account_id = ANY($3::uuid[]))
    UNION ALL
    SELECT l.created_at, 'call'::text, l.id, l.created_by,
           jsonb_strip_nulls(jsonb_build_object('outcome', l.outcome, 'conversationId', l.conversation_id::text))
    FROM conversation.call_logs_v AS l
    LEFT JOIN conversation.conversations_v AS c ON c.tenant_id = l.tenant_id AND c.id = l.conversation_id
    WHERE l.customer_id = ANY($1::uuid[])
      AND (l.conversation_id IS NULL OR $2::boolean OR c.channel_account_id = ANY($3::uuid[]))
)
SELECT i.at, i.kind, i.ref_id, i.actor_id, i.summary
FROM items AS i
WHERE $4::timestamptz IS NULL
   OR (i.at, i.kind COLLATE "C", i.ref_id) < ($4::timestamptz, $5::text COLLATE "C", $6::uuid)
ORDER BY i.at DESC, i.kind COLLATE "C" DESC, i.ref_id DESC
LIMIT $7::int`

// ChatTimeline trả tối đa limit mục hội thoại và cuộc gọi của các hồ sơ memberIDs (cả nhóm khách), mới nhất trước,
// sau khóa before (nil: từ đầu). pageIDs nil = mọi Page; khác nil = chỉ các Page đó (rỗng: không hội thoại nào, chỉ
// cuộc gọi không gắn hội thoại). Chạy trong tenancy.WithTenant.
func ChatTimeline(ctx context.Context, tx pgx.Tx, memberIDs, pageIDs []uuid.UUID, before *TimelineKey, limit int,
) ([]ChatTimelineRow, error) {
	all := pageIDs == nil
	if pageIDs == nil {
		pageIDs = []uuid.UUID{}
	}
	var at pgtype.Timestamptz
	var kind pgtype.Text
	var ref pgtype.UUID
	if before != nil {
		at = pgtype.Timestamptz{Time: before.At, Valid: true}
		kind = pgtype.Text{String: before.Kind, Valid: true}
		ref = pgtype.UUID{Bytes: before.RefID, Valid: true}
	}
	rows, err := tx.Query(ctx, chatTimelineSQL, memberIDs, all, pageIDs, at, kind, ref, limit)
	if err != nil {
		return nil, fmt.Errorf("đọc hội thoại, cuộc gọi của khách: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ChatTimelineRow, error) {
		var (
			row     ChatTimelineRow
			actor   pgtype.UUID
			summary []byte
		)
		if err := r.Scan(&row.At, &row.Kind, &row.RefID, &actor, &summary); err != nil {
			return ChatTimelineRow{}, err //nolint:wrapcheck // bọc ở dưới
		}
		if actor.Valid {
			id := uuid.UUID(actor.Bytes)
			row.ActorID = &id
		}
		if err := json.Unmarshal(summary, &row.Summary); err != nil {
			return ChatTimelineRow{}, fmt.Errorf("đọc tóm tắt mục %s: %w", row.Kind, err)
		}
		return row, nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc hội thoại, cuộc gọi của khách: %w", err)
	}
	return out, nil
}
