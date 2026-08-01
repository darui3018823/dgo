# Migration and compatibility

This document describes migration from upstream discordgo or older dgo
releases and defines how dgo changes its public API.

## v1.0.0 baseline

v1.0.0 establishes dgo's stable public API and compatibility policy. Upgrade
from v0.30.x by using the v1 module version explicitly, then review the
behavior changes in the next section and the full
[v1.0.0 release notes](releases/v1.0.0.md).

```sh
go get github.com/darui3018823/dgo@v1.0.0
go mod tidy
```

## Migrating the module path

Replace old imports with the dgo module:

```diff
- import "github.com/bwmarrin/discordgo"
+ import "github.com/darui3018823/dgo"
```

If the old package was imported without an alias, update package references
from `discordgo` to `dgo`, then run:

```sh
go mod tidy
gofmt -w .
go test ./...
```

Do not use the retired `github.com/darui3018823/discordgo` fork path. It is not
an alias for this module.

## Important behavior changes

| Area | dgo behavior | Migration |
| --- | --- | --- |
| Go version | Go 1.26.5 or newer | Update the toolchain before upgrading. |
| Gateway credentials | Only non-empty `Bot ` credentials may connect | Keep bot tokens out of source control. Pass a raw token to `NewBot`, or pass the full `Bot <token>` credential to `New`. |
| Default intents | `IntentsNone` | Set the smallest required intent set before `Open`. |
| Allowed mentions | Parsing is disabled by default | Supply `MessageAllowedMentions` when mentions are intentional. |
| Version reporting | Derived from Go build information | Use `Version()` or `VERSION`; do not assume a hard-coded release string. |
| Removed/private routes | Compatibility methods return typed errors | Use the public replacement listed below. |
| Interaction followups | Discord always returns the created message | Use `FollowupMessageCreateComplex`; the legacy wait argument is ignored. |

OAuth2 bearer tokens remain valid for applicable REST endpoints, but cannot
open a Gateway connection. dgo does not support self-bots or private Discord
client APIs.

## Deprecated and disabled API inventory

These methods remain temporarily for source compatibility. They do not restore
removed Discord behavior.

| Deprecated API | Current behavior or replacement |
| --- | --- |
| `InviteAccept` | Returns `ErrInviteAcceptUnsupported`; install bots with OAuth2. |
| `GuildCreate` and `GuildCreateWithTemplate` | Return `ErrGuildCreateUnsupported`. |
| `ThreadsActive` | Returns `ErrChannelActiveThreadsUnsupported`; use `GuildThreadsActive`. |
| `GuildIntegrationCreate` and `GuildIntegrationEdit` | Return `ErrGuildIntegrationMutationUnsupported`. |
| `ApplicationCommandPermissionsBatchEdit` | Returns `ErrCommandPermissionsBatchUnsupported`; edit commands individually. |
| Legacy OAuth2 application CRUD helpers | Return `ErrOAuthApplicationCRUDUnsupported`; use `CurrentApplication` or `CurrentApplicationEdit`. |
| `ChannelMessagesPinned` | Use `ChannelMessagesPins` for current pagination. |
| `FollowupMessageCreate` | Use `FollowupMessageCreateComplex`; the legacy `wait` parameter is ignored. |
| `LogLevel` fields | Configure an appropriate `slog.Handler` and use `Session.SetLogger`. |

The package reference is the authoritative source for individual `Deprecated:`
annotations.

## Compatibility and deprecation policy

dgo follows Semantic Versioning. v1.0.0 is the stable compatibility baseline;
future breaking changes require a new major version.

- Safe source compatibility is preferred for public names and common call
  patterns.
- A normal deprecation remains available for at least one minor release and
  at least 90 days before removal.
- Removal is scheduled for the next explicitly announced breaking release
  after that window. Release notes and this document must identify the
  replacement.
- APIs that violate Discord policy, expose private client routes, leak
  credentials, or call a removed upstream route may be disabled immediately.
  Where practical, the old symbol remains as a stub returning a typed error.
- Deprecated compatibility fields must not be used to implement new features.

Before each release, maintainers should generate and compare the
[public API inventory](API.md), review new deprecations, and update the table
above.

## Upgrade checklist

1. Read release notes and this migration guide.
2. Regenerate the public API inventory for the old and new versions.
3. Compile and test root and nested modules.
4. Run race detection and static analysis.
5. Exercise Gateway, Voice, and interaction flows in a test application with
   only the required intents and permissions.
