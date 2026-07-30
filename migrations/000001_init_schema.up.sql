-- Core schema for the Hospital Middleware service.
--
-- Design note: a person (patients) is kept separate from that person's
-- registration at a given hospital (hospital_patients), because the same human
-- can be a patient at several hospitals and each hospital's HIS assigns its own
-- local HN. hospital_patients is also the column every patient search filters
-- on, which is what enforces "staff may only see their own hospital's data".

CREATE TABLE hospitals (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- stable slug used as the "hospital" field in /staff/create and /staff/login
    code              TEXT        NOT NULL,
    name              TEXT        NOT NULL,
    -- selects which HISClient implementation handles this hospital
    his_adapter_type  TEXT        NOT NULL DEFAULT 'hospital_a',
    his_base_url      TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX ux_hospitals_code ON hospitals (lower(code));
CREATE UNIQUE INDEX ux_hospitals_name ON hospitals (lower(name));

CREATE TABLE staff (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    hospital_id    UUID        NOT NULL REFERENCES hospitals (id) ON DELETE CASCADE,
    username       TEXT        NOT NULL,
    password_hash  TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- username is unique WITHIN a hospital, not globally: two hospitals may each
-- employ a "jsmith". This is precisely why /staff/login also takes `hospital`.
CREATE UNIQUE INDEX ux_staff_hospital_username ON staff (hospital_id, lower(username));
CREATE INDEX ix_staff_hospital_id ON staff (hospital_id);

CREATE TABLE patients (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    national_id     TEXT,
    passport_id     TEXT,
    first_name_th   TEXT        NOT NULL DEFAULT '',
    middle_name_th  TEXT,
    last_name_th    TEXT        NOT NULL DEFAULT '',
    first_name_en   TEXT        NOT NULL DEFAULT '',
    middle_name_en  TEXT,
    last_name_en    TEXT        NOT NULL DEFAULT '',
    date_of_birth   DATE,
    phone_number    TEXT,
    email           TEXT,
    gender          TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT ck_patients_gender
        CHECK (gender IN ('M', 'F', '')),
    -- a canonical person must be identifiable across hospitals by at least one
    -- globally unique government id, otherwise we cannot dedupe them
    CONSTRAINT ck_patients_has_identifier
        CHECK (national_id IS NOT NULL OR passport_id IS NOT NULL)
);

-- Partial unique indexes: NULLs are simply not indexed, so many patients may
-- have a NULL passport_id while every non-NULL one stays unique.
CREATE UNIQUE INDEX ux_patients_national_id ON patients (national_id) WHERE national_id IS NOT NULL;
CREATE UNIQUE INDEX ux_patients_passport_id ON patients (passport_id) WHERE passport_id IS NOT NULL;

-- Name search is case-insensitive and matches either the Thai or English form.
CREATE INDEX ix_patients_first_name_en ON patients (lower(first_name_en));
CREATE INDEX ix_patients_last_name_en  ON patients (lower(last_name_en));
CREATE INDEX ix_patients_first_name_th ON patients (lower(first_name_th));
CREATE INDEX ix_patients_last_name_th  ON patients (lower(last_name_th));
CREATE INDEX ix_patients_date_of_birth ON patients (date_of_birth);
CREATE INDEX ix_patients_phone_number  ON patients (phone_number);
CREATE INDEX ix_patients_email         ON patients (lower(email));

CREATE TABLE hospital_patients (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    hospital_id  UUID        NOT NULL REFERENCES hospitals (id) ON DELETE CASCADE,
    patient_id   UUID        NOT NULL REFERENCES patients (id) ON DELETE CASCADE,
    -- hospital-local hospital number, assigned by that hospital's HIS
    patient_hn   TEXT        NOT NULL,
    synced_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX ux_hospital_patients_hospital_patient ON hospital_patients (hospital_id, patient_id);
CREATE UNIQUE INDEX ux_hospital_patients_hospital_hn      ON hospital_patients (hospital_id, patient_hn);
CREATE INDEX ix_hospital_patients_patient_id              ON hospital_patients (patient_id);
