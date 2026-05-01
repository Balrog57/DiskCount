CREATE TABLE IF NOT EXISTS subscribers (
    chat_id BIGINT PRIMARY KEY,
    username VARCHAR(255),
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    enabled BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS authorized_users (
    telegram_user_id BIGINT PRIMARY KEY,
    label VARCHAR(120) NOT NULL,
    is_admin BOOLEAN NOT NULL DEFAULT FALSE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS alerts (
    id SERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL,
    owner_user_id BIGINT NOT NULL,
    name VARCHAR(120) NOT NULL,
    min_capacity_tb DOUBLE PRECISION,
    max_capacity_tb DOUBLE PRECISION,
    capacity_presets JSONB NOT NULL DEFAULT '[]',
    conditions JSONB NOT NULL DEFAULT '[]',
    media_types JSONB NOT NULL DEFAULT '[]',
    drive_categories JSONB NOT NULL DEFAULT '[]',
    interfaces JSONB NOT NULL DEFAULT '[]',
    sources JSONB NOT NULL DEFAULT '[]',
    max_price_per_tb NUMERIC(10,2),
    min_discount_pct REAL NOT NULL DEFAULT 5.0,
    cooldown_hours INTEGER NOT NULL DEFAULT 24,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_alerts_chat ON alerts(chat_id);
CREATE INDEX IF NOT EXISTS idx_alerts_owner ON alerts(owner_user_id);

CREATE TABLE IF NOT EXISTS products (
    id VARCHAR(80) PRIMARY KEY,
    source VARCHAR(40) NOT NULL,
    external_id VARCHAR(255),
    title TEXT NOT NULL,
    url TEXT NOT NULL,
    capacity_tb NUMERIC(10,3) NOT NULL,
    condition VARCHAR(20),
    media_type VARCHAR(30),
    form_factor VARCHAR(120),
    technology VARCHAR(120),
    drive_category VARCHAR(40),
    interfaces JSONB NOT NULL DEFAULT '[]',
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_products_src ON products(source);

CREATE TABLE IF NOT EXISTS price_observations (
    id SERIAL PRIMARY KEY,
    product_id VARCHAR(80) NOT NULL REFERENCES products(id),
    source VARCHAR(40) NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    price_eur NUMERIC(10,2) NOT NULL,
    price_per_tb NUMERIC(10,2) NOT NULL,
    raw_json JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_obs_pid ON price_observations(product_id);
CREATE INDEX IF NOT EXISTS idx_obs_ts ON price_observations(observed_at);

CREATE TABLE IF NOT EXISTS notifications (
    id SERIAL PRIMARY KEY,
    alert_id INTEGER NOT NULL REFERENCES alerts(id),
    product_id VARCHAR(80) NOT NULL REFERENCES products(id),
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    price_eur NUMERIC(10,2) NOT NULL,
    price_per_tb NUMERIC(10,2) NOT NULL,
    discount_pct NUMERIC(6,2),
    reason VARCHAR(80) NOT NULL,
    title TEXT NOT NULL,
    url TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notif_aid ON notifications(alert_id);
CREATE INDEX IF NOT EXISTS idx_notif_pid ON notifications(product_id);
CREATE INDEX IF NOT EXISTS idx_notif_ts ON notifications(sent_at);
