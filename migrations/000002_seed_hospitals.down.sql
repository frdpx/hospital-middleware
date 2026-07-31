DELETE FROM hospitals h
WHERE h.code IN ('hospital-a', 'hospital-b')
  AND NOT EXISTS (SELECT 1 FROM staff s WHERE s.hospital_id = h.id)
  AND NOT EXISTS (SELECT 1 FROM hospital_patients hp WHERE hp.hospital_id = h.id);
