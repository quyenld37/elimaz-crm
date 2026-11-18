-- customer đọc view ord được ngoại lệ (kế hoạch M2 Q19): đúng luật.
SELECT o.id FROM ord.orders_v o WHERE o.customer_id = $1;
