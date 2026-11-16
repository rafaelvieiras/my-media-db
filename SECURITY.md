# Security Policy

MyMediaDB reads your media library, stores a catalog, and serves it over HTTP.
It handles local file paths and optional third-party API keys. We take security
reports seriously.

## Reporting a vulnerability

**Do not open a public issue for a security problem.**

Report it privately through GitHub's [private vulnerability reporting][pvr] (the
**Security** tab, then **Report a vulnerability**), or by e-mail to
**rafaelvieiras@pm.me**.

Please include:

- What the issue is and what an attacker could do with it.
- Steps to reproduce, with a proof of concept when possible.
- The affected version or commit, and your platform.

We aim to acknowledge a report within a few days and keep you informed while we
work on a fix. When a fix ships, we are glad to credit you unless you prefer to
stay anonymous.

[pvr]: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

## Known limitation: no authentication yet

**There is no authentication.** Any client that reaches the port can read and
change the catalog. Run MyMediaDB on a trusted network, or behind a reverse proxy
that enforces access control. Tokens and scopes are planned, not implemented.

We still want to hear about issues that go beyond this known gap: path traversal,
command injection, SQL injection, stored XSS, and any way a crafted media file or
provider response can escape the sandbox of the request.

## Secrets in the code

Secrets never belong in the repository. A key lives in the database through the
Integrations screen, or in a bootstrap environment variable. If you find a
credential in the history, report it privately so it can be rotated.

## Supported versions

MyMediaDB is pre-1.0 and under active development. Security fixes land on the
latest release and on `master`. Older releases are not maintained.
