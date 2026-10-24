-- Remove empty BSMI query results, preserving any vendor or certificate data.
DELETE FROM `registration`
WHERE TRIM(`tax_id`) = ''
  AND TRIM(`applicant`) = ''
  AND TRIM(`contact_addr`) = ''
  AND TRIM(`company_addr`) = ''
  AND TRIM(`phone`) = ''
  AND TRIM(`note`) = ''
  AND NOT EXISTS (
    SELECT 1 FROM `certificate`
    WHERE `certificate`.`registration_id` = `registration`.`id`
  );
