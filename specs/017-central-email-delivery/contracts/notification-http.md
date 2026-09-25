# Contract: notification HTTP API changes

Base: `/api/notification/v1` (through the gateway). Additive.

## Channel read model

```json
{ "id": "…", "name": "Platform email", "type": "email",
  "managed": true, "enabled": true, "is_default": true,
  "settings": { "host": "mx01.example.net", "port": 587, "tls": "starttls",
                "from": "tangra@example.org", "password": "__set__" },
  "permissions": { "read": true, "write": false, "delete": false, "share": false, "use": true } }
```

- `PUT /channels/{id}` and `POST /channels/{id}/remove` on a managed channel
  → `409 {"reason":"managed_channel"}`; `permissions.write/delete` are
  `false` for managed channels.
- `POST /channels/{id}/test` unchanged (allowed on managed channels).

## Template read model

```json
{ "id": "…", "name": "auth.invite", "system_key": "auth.invite",
  "subject": "…", "body": "…", "variables": ["link","valid_for","tenant"],
  "required_variables": ["link","valid_for"], "secret_variables": ["link"],
  "edited": true, "channel_type": "email", "channel_id": null }
```

- `edited` = subject/body differ from the built-in wording.
- `PUT /templates/{id}` on a system template: only `subject` and `body` may
  change (others → `422 {"reason":"system_template_field"}`); a subject/body
  that no longer references a required variable → `422
  {"reason":"missing_required_variable","variable":"link"}`.
- `POST /templates/{id}/remove` on a system template →
  `409 {"reason":"system_template"}`.
- NEW `POST /templates/{id}/restore` (system templates only, requires
  `write`) → 200 with the template restored to the built-in wording;
  non-system → `409 {"reason":"not_system_template"}`.

## Log entry read model

Adds `"template_key": "auth.invite"`; `rendered_subject`/`rendered_body`
carry `[redacted]` in place of secret variable values.
