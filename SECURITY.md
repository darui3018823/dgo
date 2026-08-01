# Security Policy

## Supported versions

Security fixes are made on the `master` branch and released in the latest
published version. Older releases are not maintained once a replacement is
available.

| Version | Supported |
| --- | --- |
| `v1.0.x` | Yes |
| `master` | Yes, pre-release |
| `v0.x` and older releases | No |

## Reporting a vulnerability

Please report suspected vulnerabilities privately through
[GitHub private vulnerability reporting](https://github.com/darui3018823/dgo/security/advisories/new).
Do not open a public issue for an undisclosed vulnerability.

Include:

- the affected version or commit;
- the smallest safe reproduction;
- the security impact and expected behavior;
- any suggested mitigation; and
- a secure way to contact you.

Do not include live Discord bot tokens, webhook tokens, interaction tokens,
voice session keys, personal data, or credentials. Revoke and replace any
credential accidentally exposed during testing.

We aim to acknowledge a report within three business days, provide an initial
assessment within seven business days, and coordinate disclosure after a fix is
available. Timelines may vary with severity and complexity.

## Scope

Reports about authentication, credential exposure, Gateway or Voice session
handling, DAVE/MLS cryptography, request signing, rate limiting, unsafe API
behavior, and dependency vulnerabilities are in scope.

Discord platform abuse that does not arise from a defect in dgo should be
reported to Discord through its own safety or security channels.

## Code scanning triage

CodeQL and dependency alerts remain open until a fix is merged or the finding
is proven not to affect this repository. A dismissal requires:

- a written technical rationale linked to the affected code path;
- a second maintainer's review;
- a regression test or other evidence when the alert is reachable; and
- a re-review date for `risk accepted` decisions.

Dismissals are reviewed before every release and at least quarterly. An alert
must be reopened when its assumptions, dependency version, or affected code
changes. `False positive` is reserved for a demonstrated analyzer mismatch,
not for low exploitability or inconvenient remediation.
