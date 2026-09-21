-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'notification_app') THEN
    GRANT USAGE ON SCHEMA public TO notification_app;
    GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO notification_app;
    REVOKE UPDATE, DELETE ON notification_audit_events FROM notification_app;
  END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
