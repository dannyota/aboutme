-- +goose Up
-- Durable aggregate discovery generation. Exactly one checked row exists;
-- startup reads it before readiness and every membership-changing mutation
-- locks and advances it in the same transaction as the public-state write.
CREATE TABLE public_state (
    singleton boolean PRIMARY KEY DEFAULT true,
    discovery_generation bigint NOT NULL,
    CONSTRAINT public_state_singleton_check CHECK (singleton),
    CONSTRAINT public_state_discovery_generation_positive_check
        CHECK (discovery_generation > 0)
);

INSERT INTO public_state (singleton, discovery_generation) VALUES (true, 1);

-- +goose Down
-- Inert in production under the append-only migration rule. Kept accurate for
-- sqlc schema replay and the pre-UAT migration harness.
DROP TABLE public_state;
