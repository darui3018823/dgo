# Third-Party Notices

This fork includes DAVE voice support adapted from upstream `bwmarrin/discordgo`
pull requests #1701 and #1704, authored by yeongaori and contributors.

The adapted code is used under the upstream repository's BSD-3-Clause license.
See `LICENSE` for the license terms preserved in this repository.

Upstream references:

- https://github.com/bwmarrin/discordgo/pull/1701
- https://github.com/bwmarrin/discordgo/pull/1704

## MLS implementation

DAVE group state uses
[`github.com/thomas-vilte/mls-go`](https://github.com/thomas-vilte/mls-go)
v1.6.0, copyright Thomas Vilte and contributors, under the MIT License. The
dependency implements RFC 9420 in Go. Its upstream interoperability report
records 21/21 cross-ciphersuite tests with mlspp and a 12/12 tested subset with
OpenMLS. Those upstream results support the implementation choice; dgo also
tests DAVE proposal, commit, Welcome, exporter convergence, and competing
commit rollback locally.

## Accepted module-only advisory

As of 2026-07-30, `govulncheck -scan=module` reports
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) for the unmaintained
`golang.org/x/crypto/openpgp` package contained in `x/crypto` v0.54.0. dgo does
not import or call `openpgp`, and the symbol-reachability scan
(`govulncheck ./...`) reports no vulnerabilities. The advisory has no fixed
version. Re-evaluate this exception when `x/crypto`, the advisory, or dgo's
imports change, and during each release review.
