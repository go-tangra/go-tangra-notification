# Changes to the auth service required by feature 006

## `auth.v1.Profiles/ListMembers` (service-to-service)

```proto
service Profiles {
  rpc Lookup(LookupProfilesRequest) returns (LookupProfilesResponse);      // existing
  // ListMembers pages the ids of the active members of a tenant. Policy: the
  // notification identity only (policy.yaml). Used to fan out "everyone"
  // messages at publish time.
  rpc ListMembers(ListMembersRequest) returns (ListMembersResponse);
}

message ListMembersRequest {
  string tenant_id = 1;
  string cursor = 2;        // opaque, from the previous page
  int32 limit = 3;          // 1..1000, default 1000
}
message ListMembersResponse {
  repeated string user_ids = 1;   // active users only, ordered by id
  string next_cursor = 2;         // empty on the last page
  int32 total = 3;                // active members of the tenant (first page only)
}
```

- Store: `SELECT id FROM users WHERE tenant_id=$1 AND status='active' AND id > $2 ORDER BY id LIMIT $3` (existing index on `(tenant_id, status)` plus the primary key).
- Read-only service call: logged with the caller identity and page size, not
  audited (consistent with `Lookup`); no rate limit beyond the policy.
- `services/auth/deploy/policy.yaml`: `/auth.v1.Profiles/ListMembers` allowed
  for `spiffe://example.org/svc/notification`; `/auth.v1.Profiles/Lookup` and
  `/auth.v1.Authorization/RegisterPermissions` extended with the same
  identity (as warden's).
- Contract tests: proto round trip; refusal for an unlisted identity; paging
  over 2,500 synthetic members in the memstore.

No browser-facing change: display names for the UI come from the existing
`POST /api/v1/users/lookup` and `GET /api/v1/roles`.
