# Upstream synchronization

dgo is an independent hard fork, but upstream discordgo changes are reviewed
deliberately rather than ignored or merged wholesale.

## Review policy

Maintainers perform an upstream review before every release and at least once
per month:

1. Fetch `upstream/master` and list commits not reachable from the dgo release
   branch.
2. Classify every commit as `cherry-pick`, `equivalent`, `superseded`, or
   `rejected`.
3. Record the decision and the dgo commit or test that provides equivalent
   behavior. A rejection must include a compatibility or security rationale.
4. Run root and nested-module tests, race tests, `go vet`, `staticcheck`, and
   `govulncheck` before release.

Security fixes are reviewed immediately. Protocol changes require fixtures or
interoperability tests; documentation-only equivalence is not sufficient.

## 2026-07-29 reconciliation

The five upstream-only commits identified by the audit have equivalent dgo
implementations:

| Upstream commit | Capability | dgo disposition |
| --- | --- | --- |
| `f43dd94` | Select menu and text input `required` fields | Equivalent in `ca2c130`, including current modal validation tests |
| `9f6aa81` | File upload component | Superseded by the broader component implementation in `ca2c130` |
| `547840c` | Guild role member counts | Equivalent REST model and endpoint in `23013e5` |
| `0dcdfb7` | Reaction-remove-emoji event | Equivalent typed event and fixture in `7b520b8` |
| `54ae40d` | AES-256-GCM voice transport | Already supported by `4a0c009`, with transport selection and packet tests retained on this branch |

Future reconciliations should append a dated section instead of rewriting past
decisions. This preserves an auditable compatibility history.
