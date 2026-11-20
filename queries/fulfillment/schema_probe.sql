-- Một câu đọc mỗi bảng, view mới của M3 (kế hoạch M3, Task M3-01) để sqlc vet kiểm tên cột trên schema thật. Chưa đường
-- nào gọi; task dùng bảng viết câu riêng. Chạy trong tenancy.WithTenant.

-- name: ProbeCallbackTask :one
SELECT id, shipment_id, order_id, attempt_no, failed_event_id, failed_reason, failed_at, due_at, status, outcome,
       next_call_at, assignee_id, version
FROM fulfillment.callback_tasks
WHERE id = @id;

-- name: ProbeCallbackTaskLog :one
SELECT id, task_id, outcome, note, next_call_at, actor_id, created_at
FROM fulfillment.callback_task_logs
WHERE id = @id;

-- name: ProbeAddressChangeRequest :one
SELECT id, shipment_id, new_address, mode, status, job_id, requested_by, resolved_by, resolved_at, version
FROM fulfillment.address_change_requests
WHERE id = @id;

-- name: ProbeShipmentAnomaly :one
SELECT id, shipment_id, kind, detail, status, resolution, resolved_by, resolved_at
FROM fulfillment.shipment_anomalies
WHERE id = @id;

-- name: ProbeLabelBatch :one
SELECT id, carrier_account_id, shipment_ids, status, file_id, error_code, created_by
FROM fulfillment.label_batches
WHERE id = @id;

-- name: ProbeShippingQuote :one
SELECT id, order_id, request_id, carrier_account_id, carrier_key, status, service, service_name, fee_total,
       fee_breakdown, expected_days, error_code, input_hash, valid_until
FROM fulfillment.shipping_quotes
WHERE id = @id;

-- name: ProbeCarrierLocation :one
SELECT carrier_key, level, our_code, carrier_code, carrier_name, synced_at
FROM fulfillment.carrier_locations
WHERE carrier_key = @carrier_key AND level = @level AND our_code = @our_code;
