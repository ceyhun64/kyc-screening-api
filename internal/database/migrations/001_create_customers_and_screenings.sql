CREATE TABLE customers (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name     text        NOT NULL,
    date_of_birth date        NOT NULL,
    country       char(2)     NOT NULL,
    status        text        NOT NULL DEFAULT 'pending'
                              CHECK (status IN ('pending', 'clear', 'review')),
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX customers_created_at_idx ON customers (created_at DESC);

-- Screenings are append-only: every check is kept as an audit record.
CREATE TABLE screenings (
    id           uuid             PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id  uuid             NOT NULL REFERENCES customers (id),
    result       text             NOT NULL CHECK (result IN ('clear', 'potential_match')),
    matched_name text,
    score        double precision NOT NULL,
    list_version text             NOT NULL,
    created_at   timestamptz      NOT NULL DEFAULT now()
);

CREATE INDEX screenings_customer_id_idx ON screenings (customer_id, created_at DESC);
