# dgo

<img align="right" alt="dgo project mark" src="img/dgo.svg" width="200">

dgo provides Go bindings for Discord's REST, Gateway, and Voice APIs. It
supports low-level protocol access together with helpers for bot sessions,
event handling, state tracking, rate limiting, interactions, and voice.

dgo is an independent hard fork of
[bwmarrin/discordgo](https://github.com/bwmarrin/discordgo). Use the dgo module
path, documentation, and issue tracker when working with this repository.

## Start here

- [Getting started](GettingStarted.md)
- [Migration, compatibility, and deprecation policy](Migration.md)
- [Webhook Events and Application Identity Profiles](WebhookEvents.md)
- [v1.1.0 release notes](releases/v1.1.0.md)
- [Public API inventory](API.md)
- [Upstream synchronization decisions](UpstreamSync.md)
- [Package reference](https://pkg.go.dev/github.com/darui3018823/dgo)
- [Examples on GitHub](https://github.com/darui3018823/dgo/tree/master/examples)

## Design goals

- Correct implementation of current public Discord APIs
- Safe handling of credentials, rate limits, and event concurrency
- Explicit Gateway intents and mention behavior
- Voice transport and DAVE interoperability
- Predictable compatibility and deprecation guidance

For dgo-specific support, use
[GitHub Issues](https://github.com/darui3018823/dgo/issues). The
[Discord Gophers](https://discord.gg/golang) community is available for
general Go discussion.
