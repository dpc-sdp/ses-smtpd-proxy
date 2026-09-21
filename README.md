# SMTP to SES Mail Proxy

This is a tiny little proxy that speaks unauthenticated SMTP on the front side
and calls the SES
[SendRawEmail](https://docs.aws.amazon.com/ses/latest/APIReference/API_SendRawEmail.html)
API on the back side.

It uses AWS SDK for Go v2 and its default credential chain: environment
variables, shared configuration and profiles, IRSA web identity credentials,
and container or instance roles. The proxy continues to call `SendRawEmail`,
so its IAM role needs `ses:SendRawEmail`; `ses:SendEmail` alone is not
sufficient.

## Prometheus Integration
By default the server will log some Prometheus metrics for messages
sent and errors. The Prometheus metrics will be served on ``:2501``
at the path ``/metrics`` by default. The bind address and port can be
customized by passing ``--prometheus-bind=bind-string`` in the format
expected by Go's http.Server.

Prometheus metric serving (though not metric aggregation) can be
disabled by passing ``--disable-prometheus`` on the command line.

Exported metrics:

- `smtpd_email_send_success_total`: Total number of successfully sent emails.
- `smtpd_email_send_fail_total{type="<reason>"}`: Total number of failed email sends, labeled by failure type.
- `smtpd_ses_error_total`: Total number of SES API errors.

## Health Check Integration

A simple health check can be enabled by passing `--enable-health-check` 
on the command line. A JSON response will be served on `:3000` at the 
path `/health` by default. The bind address and port can be
customized by passing `--health-check-bind=bind-string` in the format
expected by Go's http.Server. A sample response:

```json
{ "name": "ses-smtp-proxy", "status": "ok", "version": "v1.3.0" }
```

## Usage
By default the command takes no arguments and will listen on port 2500 on all
interfaces. The listen interfaces and port can be specified as the only
argument separated with a colon like so:

```
./ses-smtpd-proxy 127.0.0.1:2600
```

Configure credentials through the AWS SDK for Go v2 default credential chain.
For example, a Kubernetes sidecar can use IRSA credentials supplied by its
service account.

All CLI flags can also be set using environment variables with the
`SES_SMTPD_PROXY_` prefix. Flag names are uppercased and `-` is replaced with
`_`:

- `--disable-prometheus` → `SES_SMTPD_PROXY_DISABLE_PROMETHEUS`
- `--prometheus-bind` → `SES_SMTPD_PROXY_PROMETHEUS_BIND`
- `--assume-role` → `SES_SMTPD_PROXY_ASSUME_ROLE`
- `--assume-role-session-name` → `SES_SMTPD_PROXY_ASSUME_ROLE_SESSION_NAME`
- `--version` → `SES_SMTPD_PROXY_VERSION`
- `--configuration-set-name` → `SES_SMTPD_PROXY_CONFIGURATION_SET_NAME`
- `--enable-health-check` → `SES_SMTPD_PROXY_ENABLE_HEALTH_CHECK`
- `--health-check-bind` → `SES_SMTPD_PROXY_HEALTH_CHECK_BIND`

Command-line flags take precedence over environment variables.

## Assuming an IAM role

Use `--assume-role` to assume an IAM role for all SES calls, and optionally
set `--assume-role-session-name` (default: local hostname, fallback `ses-smtpd-proxy`):

```
./ses-smtpd-proxy \
  --assume-role=arn:aws:iam::<acct>:role/<role> \
  --assume-role-session-name=ses-smtpd-proxy \
  0.0.0.0:2500
```

Base credentials still come from the default credential chain, such as IRSA.
The assumed credentials are cached and refreshed automatically before expiry;
the pod does not need a restart to pick up refreshed credentials. On startup
and refresh, the proxy logs:

```
assume-role: base identity <arn>
assume-role: assuming <role-arn> (session <name>)
assume-role: refreshed credentials for <role>, expire <RFC3339>
assume-role: assumed <assumed-role-arn>, credentials expire <RFC3339>
```

Failures are logged as `assume-role: ERROR resolving base credentials: <err>`,
`assume-role: ERROR refreshing credentials for <role>: <err>`, or
`assume-role: ERROR assuming <role>: <code>: <message>` and cause startup to
fail.

The base role needs `sts:AssumeRole` on the target role. The target role's
trust policy must name the base role ARN, and the target role needs
`ses:SendRawEmail` on the SES identity. Its policy can optionally restrict
sending with a `ses:FromAddress` condition.

For example, a Kubernetes sidecar can be configured with:

```yaml
args: ["--assume-role=arn:aws:iam::<acct>:role/<role>", "0.0.0.0:2500"]
```

When the target role is in the account that owns the SES identity, sending
uses that account's identity and is not cross-account sending authorization.
The pod's own account therefore does not need SES production access.

## Security Warning
This server speaks plain unauthenticated SMTP (no TLS) so it's not suitable for
use in an untrusted environment nor on the public internet. I don't have these
use-cases but I would accept pull requests implementing these features if you
do have the use-case and want to add them.

## Building

Images are published to `ghcr.io/dpc-sdp/ses-smtpd-proxy`. They receive a
short SHA tag, a branch tag, semver tags for `v*` releases, and `latest` only
from the default branch. Pull an image with:

```
docker pull ghcr.io/dpc-sdp/ses-smtpd-proxy:latest
```

For a local build, run `make docker`; its default image is
`ghcr.io/dpc-sdp/ses-smtpd-proxy:latest`.

## Releases

Releases are managed with Release Please from commits merged to `master`.
When a release is created, the workflow publishes matching semver image tags to
`ghcr.io/dpc-sdp/ses-smtpd-proxy`.

## Dependency updates

Self-hosted Renovate runs weekly and updates Go modules, Docker base images,
and GitHub Actions. Configure the `RENOVATE_TOKEN` repository secret as a
GitHub App token or fine-grained personal access token.

## Contributing
If you would like to contribute please visit the project's GitHub page and open
a pull request with your changes. To have the best experience contributing,
please:

* Don't break backwards compatibility of public interfaces
* Update the readme, if necessary
* Follow the coding style of the current code-base
* Ensure that your code is formatted by gofmt
* Validate that your changes work with Go 1.24+

All code is reviewed before acceptance and changes may be requested to better
follow the conventions of the existing API.

## Contributors
This project is made possible by the contributions of the following
individuals; listed here in the order they first contributed to the
project.

* Mike Crute (@mcrute)
* Thomas Dupas (@thomasdupas)
* Quentin Loos (@Kent1)
* Moriyoshi Koizumi (@moriyoshi)
* Jesse Mandel (@supergibbs)
