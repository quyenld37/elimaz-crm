-- reporting ghi vào view của order (view công bố chỉ để đọc, ADR-27): sai luật dù view nằm trong ngoại lệ đọc.
UPDATE ord.orders_v SET customer_id = $2 WHERE id = $1;
