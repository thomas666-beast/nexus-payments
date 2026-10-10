BEGIN;

DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS transfers;
DROP TABLE IF EXISTS accounts;

DROP FUNCTION IF EXISTS ledger_entries_forbid_mutation();

COMMIT;
