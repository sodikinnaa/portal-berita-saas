-- Migration: update_siap_digital_hide_npwp
-- Created at: 2026-07-03

-- Update page_about_content to hide the NPWP number if it was already populated
UPDATE site_settings 
SET value = REPLACE(value, '10.000.000.1-017.9263 (1000000010179263)', 'Tersedia atas permintaan (keperluan administrasi)')
WHERE key = 'page_about_content' AND value LIKE '%10.000.000.1-017.9263%';
