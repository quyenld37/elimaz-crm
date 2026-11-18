-- customer đọc thẳng bảng ord: sai luật (1 dòng).
SELECT id FROM ord.orders WHERE customer_id = $1;
