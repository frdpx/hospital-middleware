-- Deliberately narrow: `staff` and `hospital_patients` both reference
-- `hospitals` with ON DELETE CASCADE, so an unconditional
-- `DELETE FROM hospitals` here would take every staff account and every
-- patient link down with the seed rows.
--
-- Rolling back a seed must not destroy the data that accumulated on top of it,
-- so only hospitals that nothing depends on are removed.
DELETE FROM hospitals h
WHERE h.code IN ('hospital-a', 'hospital-b')
  AND NOT EXISTS (SELECT 1 FROM staff s WHERE s.hospital_id = h.id)
  AND NOT EXISTS (SELECT 1 FROM hospital_patients hp WHERE hp.hospital_id = h.id);
