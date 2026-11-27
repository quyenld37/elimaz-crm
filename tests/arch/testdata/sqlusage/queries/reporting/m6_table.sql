-- reporting đọc thẳng bảng của order: sai luật.
SELECT o.id FROM ord.orders o WHERE o.customer_id = $1;
