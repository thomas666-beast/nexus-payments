BEGIN;

CREATE TABLE bank_accounts (
    id              TEXT         PRIMARY KEY,
    currency        CHAR(3)      NOT NULL,
    balance_minor   BIGINT       NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT bank_accounts_currency_len CHECK (char_length(currency) = 3)
);

CREATE TABLE bank_reservations (
    id              TEXT         PRIMARY KEY,
    transfer_id     TEXT         NOT NULL,
    account_id      TEXT         NOT NULL REFERENCES bank_accounts(id),
    amount_minor    BIGINT       NOT NULL,
    currency        CHAR(3)      NOT NULL,
    status          TEXT         NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ  NOT NULL,

    CONSTRAINT bank_reservations_status_valid
        CHECK (status IN ('HELD', 'COMMITTED', 'ROLLED_BACK', 'EXPIRED')),
    CONSTRAINT bank_reservations_amount_positive
        CHECK (amount_minor > 0)
);

CREATE INDEX bank_reservations_status_idx ON bank_reservations (status);

CREATE TABLE bank_settled_transfers (
    transfer_id    TEXT         PRIMARY KEY,
    account_id     TEXT         NOT NULL,
    amount_minor   BIGINT       NOT NULL,
    currency       CHAR(3)      NOT NULL,
    settled_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX bank_settled_transfers_settled_at_idx ON bank_settled_transfers (settled_at);

COMMIT;
