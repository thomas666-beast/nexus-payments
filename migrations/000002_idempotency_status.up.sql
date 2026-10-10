BEGIN;

ALTER TABLE idempotency_keys
    ADD COLUMN status TEXT NOT NULL DEFAULT 'COMPLETED';

ALTER TABLE idempotency_keys
    ALTER COLUMN response_body DROP NOT NULL,
    ALTER COLUMN response_status DROP NOT NULL;

ALTER TABLE idempotency_keys
    ADD CONSTRAINT idempotency_keys_status_valid
        CHECK (status IN ('IN_PROGRESS', 'COMPLETED', 'FAILED'));

ALTER TABLE idempotency_keys
    ADD CONSTRAINT idempotency_keys_completed_has_response
        CHECK (
            (status = 'IN_PROGRESS' AND response_body IS NULL AND response_status IS NULL) OR
            (status IN ('COMPLETED', 'FAILED') AND response_body IS NOT NULL AND response_status IS NOT NULL)
        );

COMMIT;
