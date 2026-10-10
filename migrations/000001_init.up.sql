-- =====================================================================
-- nexus-payments: core schema
-- =====================================================================

BEGIN;

-- ---------------------------------------------------------------------
-- accounts: the virtual account registry
-- ---------------------------------------------------------------------
CREATE TABLE accounts (
    id              TEXT         PRIMARY KEY,
    bank_id         TEXT         NOT NULL,
    external_ref    TEXT         NOT NULL,
    currency        CHAR(3)      NOT NULL,
    holder          TEXT         NOT NULL,
    status          TEXT         NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT accounts_status_valid
        CHECK (status IN ('ACTIVE', 'FROZEN', 'CLOSED')),
    CONSTRAINT accounts_currency_len
        CHECK (char_length(currency) = 3),
    CONSTRAINT accounts_bank_external_unique
        UNIQUE (bank_id, external_ref)
);

CREATE INDEX accounts_bank_id_idx ON accounts (bank_id);
CREATE INDEX accounts_status_idx  ON accounts (status);

-- ---------------------------------------------------------------------
-- transfers: lifecycle of a single movement of money
-- ---------------------------------------------------------------------
CREATE TABLE transfers (
    id              TEXT         PRIMARY KEY,
    from_account    TEXT         NOT NULL REFERENCES accounts(id),
    to_account      TEXT         NOT NULL REFERENCES accounts(id),
    amount_minor    BIGINT       NOT NULL,
    currency        CHAR(3)      NOT NULL,
    status          TEXT         NOT NULL,
    reason          TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT transfers_status_valid
        CHECK (status IN ('INITIATED', 'VALIDATED', 'SETTLED', 'FAILED')),
    CONSTRAINT transfers_amount_positive
        CHECK (amount_minor > 0),
    CONSTRAINT transfers_no_self
        CHECK (from_account <> to_account),
    CONSTRAINT transfers_currency_len
        CHECK (char_length(currency) = 3)
);

CREATE INDEX transfers_from_idx  ON transfers (from_account, created_at DESC);
CREATE INDEX transfers_to_idx    ON transfers (to_account, created_at DESC);
CREATE INDEX transfers_status_idx ON transfers (status);

-- ---------------------------------------------------------------------
-- ledger_entries: append-only truth
--
-- No UPDATE, no DELETE. Enforced by trigger (see below).
-- The amount is SIGNED: negative for DEBIT, positive for CREDIT.
-- Invariant: SUM(amount_minor) per transfer_id == 0.
-- ---------------------------------------------------------------------
CREATE TABLE ledger_entries (
    id              TEXT         PRIMARY KEY,
    transfer_id     TEXT         NOT NULL REFERENCES transfers(id),
    account_id      TEXT         NOT NULL REFERENCES accounts(id),
    direction       TEXT         NOT NULL,
    amount_minor    BIGINT       NOT NULL,
    currency        CHAR(3)      NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT ledger_entries_direction_valid
        CHECK (direction IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_entries_amount_sign
        CHECK (
            (direction = 'DEBIT'  AND amount_minor < 0) OR
            (direction = 'CREDIT' AND amount_minor > 0)
        ),
    CONSTRAINT ledger_entries_currency_len
        CHECK (char_length(currency) = 3)
);

CREATE INDEX ledger_entries_account_idx  ON ledger_entries (account_id, created_at DESC);
CREATE INDEX ledger_entries_transfer_idx ON ledger_entries (transfer_id);

-- Append-only enforcement: reject UPDATE and DELETE at the database level.
-- This is the strongest guarantee we can give — even a buggy app can't
-- rewrite history.
CREATE OR REPLACE FUNCTION ledger_entries_forbid_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries is append-only: % not allowed', TG_OP;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ledger_entries_no_update
    BEFORE UPDATE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION ledger_entries_forbid_mutation();

CREATE TRIGGER ledger_entries_no_delete
    BEFORE DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION ledger_entries_forbid_mutation();

-- ---------------------------------------------------------------------
-- idempotency_keys: replay protection
--
-- A client sends an Idempotency-Key header; we store it and the response.
-- If the same key arrives again, we return the stored response.
-- ---------------------------------------------------------------------
CREATE TABLE idempotency_keys (
    key             TEXT         PRIMARY KEY,
    request_hash    TEXT         NOT NULL,
    response_body   BYTEA        NOT NULL,
    response_status INT          NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ  NOT NULL,

    CONSTRAINT idempotency_status_range
        CHECK (response_status BETWEEN 100 AND 599)
);

CREATE INDEX idempotency_keys_expires_at_idx ON idempotency_keys (expires_at);

COMMIT;
