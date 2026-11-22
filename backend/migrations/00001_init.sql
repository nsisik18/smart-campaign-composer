-- +goose Up
-- Şema BLUEPRINT §5 ile birebir. Davranış verisi bilerek denormalize
-- (last_cart_at, last_purchase_at, total_orders): segment sorgusu tek tablo
-- taraması + index ile kalır, event tablosu yok (ADR-13).

CREATE TABLE customers (
  id               BIGSERIAL PRIMARY KEY,
  first_name       TEXT NOT NULL,
  last_name        TEXT NOT NULL,
  email            TEXT NOT NULL UNIQUE,
  phone            TEXT NOT NULL UNIQUE,
  city_key         TEXT NOT NULL,            -- kanonik: istanbul, ankara, ...
  language         TEXT NOT NULL DEFAULT 'tr',
  sms_opt_in       BOOLEAN NOT NULL,
  email_opt_in     BOOLEAN NOT NULL,
  last_cart_at     TIMESTAMPTZ,
  last_purchase_at TIMESTAMPTZ,
  total_orders     INT NOT NULL DEFAULT 0,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_customers_city_cart ON customers (city_key, last_cart_at);

CREATE TABLE campaigns (
  id            BIGSERIAL PRIMARY KEY,
  brief         TEXT NOT NULL,
  status        TEXT NOT NULL,
  version       INT  NOT NULL DEFAULT 1,
  segment       JSONB,
  sms_text      TEXT,
  email_subject TEXT,
  email_body    TEXT,
  approved_at   TIMESTAMPTZ,
  completed_at  TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT chk_campaign_status CHECK (status IN
    ('draft','analyzed','generated','approved','sending','completed','completed_with_errors'))
);

CREATE TABLE deliveries (
  id                  BIGSERIAL PRIMARY KEY,
  campaign_id         BIGINT NOT NULL REFERENCES campaigns(id),
  customer_id         BIGINT NOT NULL REFERENCES customers(id),
  channel             TEXT NOT NULL CHECK (channel IN ('sms','email')),
  recipient           TEXT NOT NULL,            -- telefon veya email (onay anı snapshot'ı)
  rendered_subject    TEXT,
  rendered_body       TEXT NOT NULL,
  status              TEXT NOT NULL DEFAULT 'queued'
                      CHECK (status IN ('queued','in_flight','retry_wait','sent','failed','skipped')),
  attempts            INT  NOT NULL DEFAULT 0,
  next_attempt_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  lease_until         TIMESTAMPTZ,
  idempotency_key     TEXT NOT NULL,
  provider_message_id TEXT,
  last_error          TEXT,
  sent_at             TIMESTAMPTZ,
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- Aynı kampanyada aynı kişiye aynı kanaldan ikinci iş satırı yapısal olarak imkânsız.
  CONSTRAINT uq_delivery UNIQUE (campaign_id, customer_id, channel),
  CONSTRAINT uq_delivery_key UNIQUE (idempotency_key)
);
-- Claim sorgusunun iki kolu için iki partial index (BLUEPRINT §6.2).
CREATE INDEX idx_deliveries_ready ON deliveries (channel, next_attempt_at)
  WHERE status IN ('queued','retry_wait');
CREATE INDEX idx_deliveries_lease ON deliveries (channel, lease_until)
  WHERE status = 'in_flight';
CREATE INDEX idx_deliveries_campaign ON deliveries (campaign_id, status);

CREATE TABLE delivery_attempts (
  id           BIGSERIAL PRIMARY KEY,
  delivery_id  BIGINT NOT NULL REFERENCES deliveries(id),
  attempt_no   INT NOT NULL,
  http_status  INT,
  outcome      TEXT NOT NULL,     -- sent, retry, failed, timeout
  duration_ms  INT NOT NULL,
  occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_delivery_attempts_recent ON delivery_attempts (occurred_at DESC);

-- +goose Down
DROP TABLE delivery_attempts;
DROP TABLE deliveries;
DROP TABLE campaigns;
DROP TABLE customers;
