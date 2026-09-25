# Contract: configuration changes

## notification (`container.yaml`)

```yaml
platform_email:                 # optional; absent = no platform channel (warning)
  host: mx01.example.net        # required; must match the relay certificate
  port: 587                     # 1..65535
  tls: starttls                 # implicit | starttls (default) | none
  username: tangra@example.net  # optional; refused with tls: none
  password_file: /run/secrets/smtp.password   # required with username; file mode <= 0640
  from: tangra@example.net      # required
  reply_to: ""                  # optional
  allow_plaintext: false        # required true for tls: none; warned at start
platform_tenant_id: 00000000-0000-0000-0000-000000000001   # default; auth's platform tenant
limits_notification:
  system_send_per_minute: 300   # NEW, per calling service, 1..10000
```

Refusals at load: `platform_email.password` (literal) → "use password_file";
`tls: none` without `allow_plaintext`; `username` with `tls: none`;
unreadable/empty/world-readable `password_file`.
`smtp.allow_plaintext` (tenant channels) keeps its production refusal;
`platform_email.allow_plaintext` is the explicit opt-out named by
Constitution I and is warned at every start.

## auth

```yaml
email:
  transport: notification       # NEW default | log (development only)
  # host, port, username, password, from, allow_plaintext: accepted, ignored,
  # reported in one start-up warning (removed in v5)
```

`transport: smtp` is accepted as `notification` with the same warning.

## warden

```yaml
mail:
  transport: notification       # NEW default | log (development only)
  # relay keys: accepted, ignored, warned (as auth)
```
