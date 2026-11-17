-- Chỉ số theo phút và nhịp sống của tiến trình (kế hoạch M0 bản 5, Task 10; spec 04 mục "Đo lường", ADR-38).
-- Pha mở rộng: hai bảng nền tảng mới, không RLS (Global Constraints). Chỉ internal/platform/metrics ghi.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md, mục "Bảng M0 ngoài thứ tự nhóm" (b): chỉ sửa trong transaction riêng.

-- +goose Up
-- hist: số giá trị rơi vào từng ngăn của metrics.HistBoundsMs, phần tử cuối là "lớn hơn ngăn cuối"; NULL với
-- counter và gauge. Gauge: sum là giá trị cuối, max là giá trị lớn nhất trong phút.
CREATE TABLE platform.metrics_minute (
    bucket  timestamptz      NOT NULL,
    process text             NOT NULL,
    name    text             NOT NULL,
    label   text             NOT NULL DEFAULT '',
    count   bigint           NOT NULL,
    sum     double precision NOT NULL,
    max     double precision NOT NULL,
    hist    int[],
    PRIMARY KEY (bucket, process, name, label)
);

CREATE TABLE platform.process_heartbeats (
    process text        PRIMARY KEY,
    role    text        NOT NULL,
    at      timestamptz NOT NULL
);

GRANT SELECT, INSERT, UPDATE ON platform.metrics_minute, platform.process_heartbeats TO crm_app;
