# dgo (discordgo Hard Fork)

[![Go Reference](https://pkg.go.dev/badge/github.com/darui3018823/dgo.svg)](https://pkg.go.dev/github.com/darui3018823/dgo) [![CI](https://github.com/darui3018823/dgo/actions/workflows/lint.yml/badge.svg)](https://github.com/darui3018823/dgo/actions/workflows/lint.yml)

## ⚠️ Hard Fork Notice

**This project (`dgo`) is a hard fork of [bwmarrin/discordgo](https://github.com/bwmarrin/discordgo).**
It has been renamed and reorganized to serve as an independent library with modern features and critical voice encryption fixes.

### Purpose

This fork addresses Discord's voice encryption protocol changes (XChaCha20-Poly1305) and modernizes the codebase with updated linting, CI, and module structure.

### Key Changes
- **Renamed to `dgo`**: The package and module name are now `dgo`.
- **Discord API v10**: Updated to the latest Discord API version.
- **Context Support**: `OpenWithContext` and cancellable rate limit waiting.
- **Structured Logging**: Uses `log/slog` for modern, structured logging.
- **Improved Rate Limiter**: Supports `X-RateLimit-Bucket` headers and context cancellation.
- **Modern Go**: Requires Go 1.21+, uses `io.ReadAll` instead of deprecated `ioutil`.
- **Voice Encryption**: Includes critical patches for `aead_xchacha20_poly1305_rtpsize`.

---

dgo is a [Go](https://golang.org/) package that provides low level 
bindings to the [Discord](https://discord.com/) chat client API.

If you would like to help the dgo package please use 
[this link](https://discord.com/oauth2/authorize?client_id=173113690092994561&scope=bot)
to add the official test bot **dgo** to your server.

**For help with this package or general Go discussion, please join the [Discord 
Gophers](https://discord.gg/golang) chat server.**

## Getting Started

### Installing

```sh
go get github.com/darui3018823/dgo
```

### Usage

Import the package into your project.

```go
import "github.com/darui3018823/dgo"
```

Construct a new Discord client which can be used to access the variety of 
Discord API functions and to set callback functions for Discord events.

```go
discord, err := dgo.New("Bot " + "authentication token")
```

See Documentation and Examples below for more detailed information.

## Documentation

- [![Go Reference](https://pkg.go.dev/badge/github.com/darui3018823/dgo.svg)](https://pkg.go.dev/github.com/darui3018823/dgo) 

## Contributing

- First open an issue describing the bug or enhancement so it can be discussed.  
- Try to match current naming conventions as closely as possible.  
- Create a Pull Request with your changes against the master branch.

## List of Discord APIs

See [this chart](https://abal.moe/Discord/Libraries.html) for a feature 
comparison and list of other Discord API libraries.

## Special Thanks

[Chris Rhodes](https://github.com/iopred) - For the DiscordGo logo and tons of PRs.
