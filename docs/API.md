# Public API inventory

The complete public API is the set of exported declarations documented at
[pkg.go.dev/github.com/darui3018823/dgo](https://pkg.go.dev/github.com/darui3018823/dgo).
This page identifies the main surfaces and defines the reproducible inventory
used during releases.

## Main surfaces

| Surface | Primary entry points |
| --- | --- |
| Session and configuration | `New`, `NewBot`, `NewOAuth2`, `Session`, `Version`, request options |
| REST | Typed `Session` methods and the low-level `Request*` methods, including Application Identity Profiles |
| Gateway | `Open`, `OpenWithContext`, event handlers, intents, state |
| Voice and DAVE | `VoiceConnection`, voice join methods, packet channels |
| Interactions and Webhook Events | Interaction models, response helpers, webhook event envelopes, signature verification |
| Models | Guild, channel, message, user, role, application, entitlement, subscription, and audit-log types |
| Extensibility | Custom HTTP clients, WebSocket dialers, loggers, raw events |

Typed helpers are preferred over low-level `Request*` methods. Raw REST access
does not make private Discord client routes supported.

## Generate an inventory

From a clean checkout at the release commit:

```sh
go doc -all github.com/darui3018823/dgo > dgo-api.txt
go list -json github.com/darui3018823/dgo > dgo-package.json
```

`dgo-api.txt` is the human-readable exported declaration inventory.
`dgo-package.json` records module and package metadata. Generate the same files
for the previous release and review their diff before tagging.

The release review must classify every removed or changed declaration as one
of:

- compatible addition;
- documented deprecation;
- announced breaking change;
- immediate safety or Discord-policy enforcement.

Update [Migration.md](Migration.md) whenever the review identifies a
deprecation, replacement, or breaking change.

## Current compatibility stubs

Methods retained only as disabled compatibility stubs are listed in
[Migration.md](Migration.md#deprecated-and-disabled-api-inventory). They are
exported for source migration, not supported Discord operations.
