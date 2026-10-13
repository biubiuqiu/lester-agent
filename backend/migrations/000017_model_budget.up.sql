ALTER TABLE model_deployments ADD COLUMN context_window integer NOT NULL DEFAULT 0 CHECK (context_window >= 0);
ALTER TABLE model_deployments ADD COLUMN output_reserve integer NOT NULL DEFAULT 0 CHECK (output_reserve >= 0);
ALTER TABLE model_deployments ADD COLUMN policy_window integer NOT NULL DEFAULT 0 CHECK (policy_window >= 0);
-- Counting/structured output are not implemented by the neutral runtime API.
UPDATE model_deployments SET capabilities = capabilities || '{"TokenCounting":false,"StructuredOutput":false}'::jsonb;
