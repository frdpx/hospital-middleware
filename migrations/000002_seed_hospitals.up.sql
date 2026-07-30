-- Seed data. Hospitals are reference data provisioned by an operator, not
-- created through the public API, so /staff/create can validate against them.
INSERT INTO hospitals (code, name, his_adapter_type, his_base_url) VALUES
    ('hospital-a', 'Hospital A', 'hospital_a', 'https://hospital-a.api.co.th'),
    ('hospital-b', 'Hospital B', 'hospital_a', 'https://hospital-b.api.co.th')
ON CONFLICT DO NOTHING;
