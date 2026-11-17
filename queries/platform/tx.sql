-- Câu đầu tiên của mỗi transaction trong db.RetryTx (ADR-46 điểm 6): đặt mọi giới hạn và ngữ cảnh theo
-- transaction (set_config(…, true)) trong một lượt đi về. names[i] nhận vals[i].

-- name: SetTxConfig :exec
SELECT count(set_config((@names::text[])[i], (@vals::text[])[i], true))
FROM generate_subscripts(@names::text[], 1) AS i;
