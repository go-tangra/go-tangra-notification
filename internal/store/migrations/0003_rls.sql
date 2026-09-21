-- +goose Up
-- Row-level security: the application role (no BYPASSRLS) sets app.tenant_id
-- per transaction; workers (scheduler, audit writer) run with app.system = on.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_tenant_matches(tid uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
  SELECT tid::text = current_setting('app.tenant_id', true)
      OR current_setting('app.system', true) = 'on'
$$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['channels','templates','grants','message_categories','messages','inbox','notification_log','notification_audit_events']
  LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (app_tenant_matches(tenant_id)) WITH CHECK (app_tenant_matches(tenant_id))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
