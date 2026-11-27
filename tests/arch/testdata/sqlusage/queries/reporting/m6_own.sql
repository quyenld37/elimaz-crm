-- reporting ghi bảng của chính mình và đọc view đã công bố: đúng luật.
INSERT INTO reporting.report_runs (id) SELECT o.id FROM ord.orders_v o;
DELETE FROM reporting.report_runs WHERE id = $1;
