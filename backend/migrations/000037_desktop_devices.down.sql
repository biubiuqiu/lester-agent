-- Device metadata and derived model definitions only. Local files and local
-- transcripts remain on their devices; historical cloud messages are retained.
UPDATE conversations SET model_deployment_id=NULL WHERE model_deployment_id IN (SELECT deployment_id FROM desktop_device_models);
UPDATE runs SET model_deployment_id=NULL WHERE model_deployment_id IN (SELECT deployment_id FROM desktop_device_models);
DELETE FROM model_connections WHERE id IN (SELECT connection_id FROM desktop_device_models);
DROP TABLE desktop_conversations;
DROP TABLE desktop_device_models;
DROP TABLE desktop_devices;
