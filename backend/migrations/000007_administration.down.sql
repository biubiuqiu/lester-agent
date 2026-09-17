-- Detach shared models before removing the reserved scope.
UPDATE conversations SET model_deployment_id=NULL WHERE model_deployment_id IN (SELECT id FROM model_deployments WHERE workspace_id='00000000-0000-0000-0000-000000000001');
DELETE FROM workspaces WHERE id='00000000-0000-0000-0000-000000000001';
ALTER TABLE model_deployments DROP COLUMN enabled;
ALTER TABLE users DROP COLUMN disabled, DROP COLUMN role;
