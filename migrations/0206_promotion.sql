-- Khuyến mãi M2 (kế hoạch M2, Task M2-06; spec 05 mục 2 "coupons", "coupon_redemptions"; Q-X2, Q10, Q27, B9, B10).
-- Schema promotion mới: đợt mã, mã giảm giá, lượt dùng theo khách, lượt dùng theo đơn; view công bố coupons_v.
-- M2 là chủ bốn bảng này: mốc sau (M5) chỉ ALTER … ADD cột thật sự mới, không tạo lại bảng.
-- Lượt dùng chỉ đổi qua promotion.Redeem/Release/Detach/Reprice bằng câu có điều kiện (used_count < max_uses,
-- used < per_customer_limit); CHECK ở đây là chốt cuối.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 10 (coupon_batches → coupons → coupon_customer_uses →
-- coupon_redemptions). Pha mở rộng: chỉ CREATE, GRANT.

-- +goose Up
CREATE SCHEMA promotion;
GRANT USAGE ON SCHEMA promotion TO crm_app, crm_search;

-- Đợt mã: điều khoản chung nằm trong template theo đúng tên cột của coupons (discount_kind, value, max_discount,
-- min_subtotal, valid_from, valid_to; Q-X2).
CREATE TABLE promotion.coupon_batches (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    name        text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    code_prefix text NOT NULL DEFAULT '' CHECK (code_prefix ~ '^[A-Za-z0-9-]{0,16}$'),
    quantity    int NOT NULL CHECK (quantity BETWEEN 1 AND 100000),
    template    jsonb NOT NULL CHECK (jsonb_typeof(template) = 'object'),
    created_by  uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id)
);
SELECT platform.enable_tenant_rls('promotion.coupon_batches');

-- Mã giảm giá. value: percent → phần vạn (1–10000), fixed → số đồng. channels rỗng = mọi kênh; seller_id rỗng = mọi
-- người bán (Q10); bound_customer_id, bound_phone (dạng +84…, dữ liệu cá nhân, không vào view) gắn mã với một khách.
CREATE TABLE promotion.coupons (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    code               citext NOT NULL CHECK (code ~ '^[A-Za-z0-9-]{3,32}$'),
    batch_id           uuid,
    name               text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    discount_kind      text NOT NULL CHECK (discount_kind IN ('percent', 'fixed')),
    value              bigint NOT NULL,
    max_discount       bigint CHECK (max_discount BETWEEN 1 AND 9007199254740991),
    min_subtotal       bigint NOT NULL DEFAULT 0 CHECK (min_subtotal BETWEEN 0 AND 9007199254740991),
    channels           text[] NOT NULL DEFAULT '{}'
        CHECK (channels <@ ARRAY['facebook', 'web', 'manual']::text[]),
    seller_id          uuid,
    bound_customer_id  uuid,
    bound_phone        text CHECK (bound_phone ~ '^\+84[1-9][0-9]{8,9}$'),
    max_uses           int CHECK (max_uses >= 1),
    used_count         int NOT NULL DEFAULT 0,
    per_customer_limit int CHECK (per_customer_limit >= 1),
    release_on_cancel  boolean NOT NULL DEFAULT true,
    valid_from         timestamptz NOT NULL,
    valid_to           timestamptz NOT NULL,
    status             text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_by         uuid NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    version            bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT coupons_code_key UNIQUE (tenant_id, code),
    CONSTRAINT coupons_used_count_check CHECK (used_count >= 0 AND (max_uses IS NULL OR used_count <= max_uses)),
    CONSTRAINT coupons_valid_range_check CHECK (valid_to > valid_from),
    CONSTRAINT coupons_value_check CHECK ((discount_kind = 'percent' AND value BETWEEN 1 AND 10000)
        OR (discount_kind = 'fixed' AND value BETWEEN 1 AND 9007199254740991)),
    FOREIGN KEY (tenant_id, batch_id) REFERENCES promotion.coupon_batches (tenant_id, id),
    FOREIGN KEY (tenant_id, seller_id) REFERENCES catalog.sellers (tenant_id, id),
    FOREIGN KEY (tenant_id, bound_customer_id) REFERENCES customer.customers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('promotion.coupons');
COMMENT ON INDEX promotion.coupons_code_key IS
    'seller-exempt: khách gõ mã ở một ô chung của shop, mã duy nhất trên cả shop (05 mục 2)';
CREATE INDEX coupons_batch ON promotion.coupons (tenant_id, batch_id) WHERE batch_id IS NOT NULL;

-- Lượt dùng của một khách (hồ sơ gốc) với một mã: số lượt đang hiệu lực, giới hạn bởi per_customer_limit.
CREATE TABLE promotion.coupon_customer_uses (
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    coupon_id   uuid NOT NULL,
    customer_id uuid NOT NULL,
    used        int NOT NULL CHECK (used >= 0),
    PRIMARY KEY (tenant_id, coupon_id, customer_id),
    FOREIGN KEY (tenant_id, coupon_id) REFERENCES promotion.coupons (tenant_id, id),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customer.customers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('promotion.coupon_customer_uses');

-- Lượt dùng mã của một đơn. Trả lượt đổi status sang released (không xóa); dùng lại mã cho cùng đơn đổi dòng cũ về
-- active. Mỗi đơn tối đa một lượt active (Q27).
CREATE TABLE promotion.coupon_redemptions (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      uuid NOT NULL REFERENCES platform.tenants (id),
    coupon_id      uuid NOT NULL,
    order_id       uuid NOT NULL,
    customer_id    uuid NOT NULL,
    discount       bigint NOT NULL CHECK (discount BETWEEN 0 AND 9007199254740991),
    status         text NOT NULL CHECK (status IN ('active', 'released')),
    released_at    timestamptz,
    release_reason text CHECK (length(release_reason) BETWEEN 1 AND 100),
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT coupon_redemptions_order_key UNIQUE (tenant_id, coupon_id, order_id),
    CONSTRAINT coupon_redemptions_released_check
        CHECK ((status = 'released') = (released_at IS NOT NULL) AND (status = 'released') = (release_reason IS NOT NULL)),
    FOREIGN KEY (tenant_id, coupon_id) REFERENCES promotion.coupons (tenant_id, id),
    FOREIGN KEY (tenant_id, order_id) REFERENCES ord.orders (tenant_id, id),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customer.customers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('promotion.coupon_redemptions');
CREATE UNIQUE INDEX coupon_redemptions_active_order_key ON promotion.coupon_redemptions (tenant_id, order_id)
    WHERE status = 'active';

-- View công bố (Q-X2, B2-11): không có bound_phone, bound_customer_id. Cột đúng docs/views.md.
CREATE VIEW promotion.coupons_v WITH (security_invoker = true) AS
SELECT id, tenant_id, code, batch_id, name, discount_kind, value, max_discount, min_subtotal, channels, seller_id,
       max_uses, used_count, per_customer_limit, valid_from, valid_to, status, created_at, updated_at
FROM promotion.coupons;

GRANT SELECT ON promotion.coupons_v TO crm_app;
GRANT SELECT, INSERT ON promotion.coupon_batches, promotion.coupons, promotion.coupon_customer_uses,
    promotion.coupon_redemptions TO crm_app;
GRANT UPDATE (used_count) ON promotion.coupons TO crm_app;
GRANT UPDATE (used) ON promotion.coupon_customer_uses TO crm_app;
GRANT UPDATE (customer_id, discount, status, released_at, release_reason) ON promotion.coupon_redemptions TO crm_app;
