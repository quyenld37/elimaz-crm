-- reporting đọc view ord đã công bố (ngoại lệ chỉ đọc view, kế hoạch M6 Task 226): đúng luật.
SELECT o.id FROM ord.orders_v o JOIN fulfillment.shipments_v s ON s.order_id = o.id WHERE o.customer_id = $1;
