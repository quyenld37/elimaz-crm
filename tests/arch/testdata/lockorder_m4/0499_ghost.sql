-- Mẫu cho TestLockOrder_M4Sample: một bảng không có trong ADR-46 (ghost) và một bảng có (messages).
CREATE TABLE conversation.ghost (id uuid PRIMARY KEY);
CREATE TABLE IF NOT EXISTS conversation.messages (id uuid PRIMARY KEY);
