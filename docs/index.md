# Phron provider

Manage Phron Servers and Object Storage. See the [README](../README.md) for credentials and behaviour notes.

## Provider arguments

- `client_id` (optional): the API client's client ID. Default: `PHRON_CLIENT_ID`.
- `client_secret` (optional, sensitive): the API client's secret. Default: `PHRON_CLIENT_SECRET`.
- `project_id` (optional): the Phron project id from the console's API clients page; builds the scope. Default: `PHRON_PROJECT_ID`.
- `scope` (optional): the complete OAuth scope; overrides `project_id`. Default: `PHRON_SCOPE`.
- `endpoint` (optional): API base URL. Default: `PHRON_ENDPOINT`, then `https://api.phron.net`.
- `token_url` (optional): OAuth token endpoint. Default: `PHRON_TOKEN_URL`, then `https://auth.atestum.com/oauth/v2/token`.

## Resource `phron_server`

Required: `name`, `operating_system_id`, `ssh_key_ids` (set), `location`, `vcpus`, `memory_gib`, `disk_gib`.
Optional: `tier` (`shared`, the default, or `dedicated`), `workspace_id`, `user_data` (cloud-init, sensitive), `restore_from_backup_id`.
Computed: `id`, `state`, `power`, `ipv6_prefix`, `ssh_address`.

Changing `vcpus`, `memory_gib` or `disk_gib` resizes the Server in place (the disk never shrinks). Every other argument forces a new Server. Import: `terraform import phron_server.web <server id>` (then set `user_data` as configured; the API does not return it).

## Resource `phron_ssh_key`

Required: `name`, `public_key`. Computed: `id`, `fingerprint`. Both arguments force a new key. Import by id.

## Resource `phron_storage_bucket`

Required: `name`. Optional: `location`, `workspace_id`, `object_lock` (bool, set at creation only), `object_lock_default_retention_days` (needs `object_lock`), `limit_bytes`.
Computed: `id` and `full_name` (the bucket name S3 tools use: the shared Cell's prefix plus `name`).

## Resource `phron_storage_bucket_limit`

Required: `bucket_name` (the bucket's `full_name`), `limit_bytes`. Optional: `location`, `workspace_id`. Computed: `id`, `phase` (`applying`, `applied`, `not_converged`, `not_verifiable`). Destroying it removes the limit.

## Resource `phron_storage_key`

Required: `name`, `preset` (`full`, `read_write_objects` or `read_only_objects`). Optional: `bucket_names` (set of full bucket names), `expires_at` (RFC 3339), `location`, `workspace_id`.
Computed: `id`, `access_key_id`, `secret_access_key` (sensitive, readable only at creation, stored in the state), `status`, `created_by`. Every argument forces a new key.

## Data sources

- `phron_locations`: `locations` list of `id`, `name`, `country`, `available`, `unavailable_reason`.
- `phron_operating_systems`: `operating_systems` list of `id`, `display_name`, `min_disk_gib`, `login_user`.
- `phron_server_quote`: inputs `vcpus`, `memory_gib`, `disk_gib`, optional `tier`, `location`, `operating_system_id`; outputs `hourly_usd`, `backup_hourly_usd`, `own_prefix`, `available`, `unavailable_reason`, `problems`.
- `phron_storage_overview`: optional `workspace_id`, `location`; outputs `state`, `endpoint_url`, `region`, `bucket_name_prefix`, usage counters, `month_to_date_cost`, `currency`, `price_per_gb_month_usd`, `locations`.
