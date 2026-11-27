# terraform-provider-phron

Terraform provider for [Phron](https://phron.net): Servers and Object Storage, managed through the public Phron API ([phron-net/api](https://github.com/phron-net/api)).

```hcl
terraform {
  required_providers {
    phron = {
      source = "phron-net/phron"
    }
  }
}

provider "phron" {} # credentials from the environment, see below
```

**Status: pre-release.** The provider builds and passes its unit tests in CI. It has not been published to the Terraform Registry and has not yet been run against the live API.

## Credentials

The provider signs in as an **API client**: an OAuth client-credentials machine user of your Organization, created on the console's API clients page (an Organization Admin makes one, picks its Role and sees the client ID and secret once). The page also shows the project id used in the scope. The Role of the API client decides what the provider may do; a Viewer cannot create or delete anything.

| Attribute | Environment variable | Default |
|---|---|---|
| `client_id` | `PHRON_CLIENT_ID` | none (required) |
| `client_secret` (sensitive) | `PHRON_CLIENT_SECRET` | none (required) |
| `project_id` | `PHRON_PROJECT_ID` | none; builds the scope below |
| `scope` | `PHRON_SCOPE` | built from `project_id`; overrides it when set |
| `endpoint` | `PHRON_ENDPOINT` | `https://api.phron.net` |
| `token_url` | `PHRON_TOKEN_URL` | `https://auth.atestum.com/oauth/v2/token` |

Either `project_id` or `scope` is required. The scope built from the project id is

```
openid urn:zitadel:iam:org:project:id:<project id>:aud urn:zitadel:iam:user:resourceowner
```

```sh
export PHRON_CLIENT_ID=...
export PHRON_CLIENT_SECRET=...
export PHRON_PROJECT_ID=...
terraform init && terraform apply
```

## Resources and data sources

| Name | Purpose |
|---|---|
| `phron_server` | A Server. Create waits until it is running; a size change resizes it in place; destroy deletes it, its disk and its backups for good. |
| `phron_ssh_key` | A saved SSH public key that Servers install at first boot. |
| `phron_storage_bucket` | An S3-compatible bucket, optionally with Object Lock and a size limit. |
| `phron_storage_bucket_limit` | The size limit of an existing bucket (use instead of `limit_bytes` on the bucket, not together). |
| `phron_storage_key` | A Storage Access Key. |
| `data.phron_locations` | Locations a Server can live in. |
| `data.phron_operating_systems` | Operating Systems a Server can be created from. |
| `data.phron_server_quote` | Prices and checks a Server size. |
| `data.phron_storage_overview` | State, endpoint, usage and price of a Workspace's Object Storage. |

Full descriptions of every attribute: [docs/index.md](docs/index.md). Examples: [examples/server](examples/server) and [examples/bucket](examples/bucket).

## Secrets live in the state

`phron_storage_key.secret_access_key` is returned by the API once, at creation, so Terraform keeps it in the state file (marked sensitive, but stored in plain text). `phron_server.user_data` is kept from your configuration (the API never returns it) and is also marked sensitive. Store the state in a backend that encrypts it and restricts who can read it, and do not put secrets in `user_data`: anyone who can read the guest's disk can read it.

## Behaviour notes

- A Server, SSH key, bucket or key that was deleted outside Terraform is removed from the state on the next refresh (the API answers `NOT_FOUND`).
- Creating a Server is idempotent: the provider makes one request id per create and reuses it on its own retries, so a retry cannot create a second Server. Calls the API answers with `UNAVAILABLE` (including a gateway rate limit) are retried a few times.
- The first bucket of a Workspace also creates its storage. That needs a positive balance and can take minutes; the provider waits.
- Destroying a bucket needs it to be empty.

## Development

Unit tests run against an in-process Connect server (`internal/provider/*_test.go`). Acceptance tests (`TestAcc*`) create real, billed objects and run only with `TF_ACC=1` and credentials in the environment; CI does not run them.

## License

Apache License 2.0. See [LICENSE](LICENSE).
