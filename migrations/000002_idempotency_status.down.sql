BEGIN;

ALTER TABLE idempotency_keys DROP CONSTRAINT IF EXISTS idempotency_keys_completed_has_response;
ALTER TABLE idempotency_keys DROP CONSTRAINT IF EXISTS idempotency_keys_status_valid;
ALTER TABLE idempotency_keys DROP COLUMN IF EXISTS status;

ALTER TABLE idempotency_keys
    ALTER COLUMN response_body SET NOT NULL,
    ALTER COLUMN response_status SET NOT NULL;

COMMIT;
