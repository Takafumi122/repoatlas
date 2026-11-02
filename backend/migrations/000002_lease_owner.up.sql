-- Add lease_owner token column for worker fencing and heartbeat verification.
ALTER TABLE analysis_runs
    ADD COLUMN IF NOT EXISTS lease_owner TEXT;
