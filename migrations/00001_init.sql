-- +goose Up
CREATE TABLE skins (
    id              uuid PRIMARY KEY,
    name            varchar(255) NOT NULL CHECK (length(btrim(name)) > 0),
    base_price_usd  numeric(18, 2) NOT NULL CHECK (base_price_usd > 0),
    is_available    boolean NOT NULL DEFAULT true,
    created_at_utc  timestamptz NOT NULL DEFAULT now(),
    updated_at_utc  timestamptz,
    is_deleted      boolean NOT NULL DEFAULT false,
    deleted_at_utc  timestamptz,
    CHECK ((is_deleted AND deleted_at_utc IS NOT NULL) OR (NOT is_deleted AND deleted_at_utc IS NULL))
);

CREATE INDEX idx_skins_name ON skins (name);
CREATE INDEX idx_skins_available ON skins (is_available) WHERE is_deleted = false;

CREATE TABLE purchases (
    id                uuid PRIMARY KEY,
    skin_id           uuid NOT NULL REFERENCES skins (id) ON DELETE RESTRICT,
    price_usd_final   numeric(18, 2) NOT NULL CHECK (price_usd_final > 0),
    btc_usd_rate      numeric(18, 2) NOT NULL CHECK (btc_usd_rate >= 0),
    rate_source       varchar(20) NOT NULL,
    purchased_at_utc  timestamptz NOT NULL DEFAULT now(),
    buyer_id          varchar(100) NOT NULL CHECK (length(btrim(buyer_id)) > 0),
    idempotency_key   varchar(100)
);

CREATE INDEX idx_purchases_skin_id ON purchases (skin_id);
CREATE INDEX idx_purchases_buyer_id ON purchases (buyer_id);
CREATE INDEX idx_purchases_purchased_at ON purchases (purchased_at_utc DESC);
CREATE UNIQUE INDEX idx_purchases_buyer_idempotency
    ON purchases (buyer_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

INSERT INTO skins (id, name, base_price_usd, is_available, created_at_utc)
VALUES
    ('11111111-1111-1111-1111-111111111111', 'Creeper Classic', 9.99, true,  '2024-01-01T00:00:00Z'),
    ('22222222-2222-2222-2222-222222222222', 'Enderman Elite', 14.99, true,  '2024-01-01T00:00:00Z'),
    ('33333333-3333-3333-3333-333333333333', 'Dragon Scale', 19.99, true,  '2024-01-01T00:00:00Z'),
    ('44444444-4444-4444-4444-444444444444', 'Piglin Warrior', 12.99, false, '2024-01-01T00:00:00Z')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS purchases;
DROP TABLE IF EXISTS skins;
