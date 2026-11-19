-- Dòng thời gian của cả nhóm khách (kế hoạch M2, Task M2-12): đơn và đổi trạng thái đơn (đọc ord.orders_v,
-- ord.order_status_history_v — ngoại lệ chỉ đọc view, Q19), ghi chú, gộp và hoàn tác gộp, nhãn đang gắn. Chạy trong
-- tenancy.WithTenant. Trang theo keyset (at, kind, ref_id) giảm dần; (kind, ref_id) không trùng giữa các mục.
-- summary chỉ mang mã, trạng thái, tên nhãn: không SĐT, địa chỉ, tên khách; nội dung ghi chú chỉ ở cột note.

-- name: ListTimeline :many
WITH grp AS (SELECT c.id FROM customer.customers AS c WHERE c.root_id = @root_id),
items AS (
    SELECT o.created_at                                     AS at,
           'order_created'::text                            AS kind,
           o.id                                             AS ref_id,
           o.created_by                                     AS actor_id,
           jsonb_build_object('code', o.code, 'status', o.status, 'grand_total', o.grand_total::text) AS summary,
           NULL::text                                       AS note
    FROM ord.orders_v AS o
    WHERE o.customer_id IN (SELECT id FROM grp)
    UNION ALL
    -- Đổi trạng thái; bỏ dòng tạo đơn (from_status rỗng) vì đã có mục order_created.
    SELECT h.occurred_at,
           'order_status',
           h.id,
           h.actor_id,
           jsonb_strip_nulls(jsonb_build_object('order_id', o.id::text, 'code', o.code, 'from_status', h.from_status,
               'to_status', h.to_status, 'from_pending', h.from_pending, 'to_pending', h.to_pending,
               'reason', h.reason)),
           NULL
    FROM ord.order_status_history_v AS h
    JOIN ord.orders_v AS o ON o.tenant_id = h.tenant_id AND o.id = h.order_id
    WHERE o.customer_id IN (SELECT id FROM grp)
      AND h.from_status <> ''
    UNION ALL
    SELECT n.created_at, 'note', n.id, n.author_id, '{}'::jsonb, n.body
    FROM customer.customer_notes AS n
    WHERE n.customer_id IN (SELECT id FROM grp)
    UNION ALL
    SELECT m.created_at,
           'merge',
           m.id,
           m.actor_id,
           jsonb_build_object('survivor_id', m.survivor_id::text, 'merged_id', m.merged_id::text,
               'matched_by', m.matched_by),
           NULL
    FROM customer.customer_merges AS m
    WHERE m.survivor_id IN (SELECT id FROM grp) OR m.merged_id IN (SELECT id FROM grp)
    UNION ALL
    SELECT m.undone_at,
           'merge_undone',
           m.id,
           m.undone_by,
           jsonb_build_object('survivor_id', m.survivor_id::text, 'merged_id', m.merged_id::text),
           NULL
    FROM customer.customer_merges AS m
    WHERE m.undone_at IS NOT NULL
      AND (m.survivor_id IN (SELECT id FROM grp) OR m.merged_id IN (SELECT id FROM grp))
    UNION ALL
    -- Nhãn đang gắn của nhóm: một mục mỗi nhãn, lúc gắn sớm nhất.
    SELECT min(l.created_at),
           'tag',
           t.id,
           NULL::uuid,
           jsonb_build_object('name', t.name, 'color', t.color, 'source', max(l.source)),
           NULL
    FROM customer.customer_tag_links AS l
    JOIN customer.customer_tags AS t ON t.tenant_id = l.tenant_id AND t.id = l.tag_id
    WHERE l.customer_id IN (SELECT id FROM grp)
    GROUP BY t.id, t.name, t.color
    -- Chỗ chèn nguồn mới (M4 hội thoại, C7; P2 chăm sóc): thêm một nhánh "UNION ALL SELECT at, kind, ref_id,
    -- actor_id, summary, note FROM …" ngay trên dòng này, kind là chuỗi mới chưa dùng, (kind, ref_id) không trùng.
)
SELECT i.at::timestamptz AS at, i.kind::text AS kind, i.ref_id::uuid AS ref_id, i.actor_id, i.summary::jsonb AS summary,
       i.note
FROM items AS i
WHERE sqlc.narg(after_at)::timestamptz IS NULL
   OR (i.at, i.kind COLLATE "C", i.ref_id)
      < (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_kind)::text COLLATE "C", sqlc.narg(after_ref)::uuid)
ORDER BY i.at DESC, i.kind COLLATE "C" DESC, i.ref_id DESC
LIMIT @lim::int;
