-- name: Ok :many
SELECT o.id FROM ord.orders_v AS o JOIN fulfillment.shipments_v AS s ON s.id = o.id;
