# dgo

[![Go Reference](https://pkg.go.dev/badge/github.com/darui3018823/dgo.svg)](https://pkg.go.dev/github.com/darui3018823/dgo)
[![CI](https://github.com/darui3018823/dgo/actions/workflows/ci.yml/badge.svg)](https://github.com/darui3018823/dgo/actions/workflows/ci.yml)

dgo is a Go library for Discord's REST, Gateway, and Voice APIs. It is an
independent hard fork of [bwmarrin/discordgo](https://github.com/bwmarrin/discordgo)
with its own compatibility and release policy.

## Highlights

- Discord API v10 REST and Gateway bindings
- Gateway event handling, state tracking, and voice connections
- DAVE end-to-end voice encryption support
- Context-aware REST rate limiting and bounded retries
- Structured logging with credential redaction
- Safe defaults: no Gateway intents and no parsed mentions unless enabled
- Go 1.26.6 or newer

## Install

```sh
go get github.com/darui3018823/dgo@v1.0.0
```

Import the package as `dgo`:

```go
import "github.com/darui3018823/dgo"
```

Create a bot session from a raw bot token. `NewBot` adds the required `Bot `
authorization prefix:

```go
session, err := dgo.NewBot(token)
if err != nil {
	return err
}
session.Identify.Intents = dgo.IntentsGuilds
```

Gateway connections accept bot credentials only. OAuth2 bearer credentials
may be used with applicable REST endpoints. Automated user accounts and
private client routes are not supported.

## Documentation

- [Getting started](docs/GettingStarted.md)
- [Migration, compatibility, and deprecation policy](docs/Migration.md)
- [v1.0.0 release notes](docs/releases/v1.0.0.md)
- [Public API inventory](docs/API.md)
- [Package reference](https://pkg.go.dev/github.com/darui3018823/dgo)
- [Examples](examples)

The exported `VERSION` value and `Version()` function are resolved from Go
build information. Tagged module builds report their tag, pseudo-version
builds report the pseudo-version, and local builds report a `devel` version
with VCS information when available.

## Support and contributing

For dgo bugs and feature requests, use
[GitHub Issues](https://github.com/darui3018823/dgo/issues). For general Go
discussion, visit the [Discord Gophers](https://discord.gg/golang) community.

Before opening a pull request:

1. Open or reference an issue describing the change.
2. Follow the repository's current naming and compatibility conventions.
3. Run the root and nested-module verification described by CI.
4. Target the `master` branch.

## Attribution

dgo retains the BSD-3-Clause lineage and attribution of the upstream project.
Thanks to the upstream maintainers and contributors, including
[Chris Rhodes](https://github.com/iopred), who created the original upstream
project logo used by the inherited documentation assets.
