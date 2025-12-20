# DiscordGo

[![Go Reference](https://pkg.go.dev/badge/github.com/bwmarrin/discordgo.svg)](https://pkg.go.dev/github.com/bwmarrin/discordgo) [![Go Report Card](https://goreportcard.com/badge/github.com/bwmarrin/discordgo)](https://goreportcard.com/report/github.com/bwmarrin/discordgo) [![CI](https://github.com/bwmarrin/discordgo/actions/workflows/ci.yml/badge.svg)](https://github.com/bwmarrin/discordgo/actions/workflows/ci.yml) [![Discord Gophers](https://img.shields.io/badge/Discord%20Gophers-%23discordgo-blue.svg)](https://discord.gg/golang) [![Discord API](https://img.shields.io/badge/Discord%20API-%23go_discordgo-blue.svg)](https://discord.com/invite/discord-api)

<img align="right" alt="DiscordGo logo" src="docs/img/discordgo.svg" width="400">

## ⚠️ Patched Fork Notice

**This is a patched fork of [bwmarrin/discordgo](https://github.com/bwmarrin/discordgo) with critical voice encryption fixes.**

### Purpose

This fork addresses Discord's voice encryption protocol changes that are not yet supported in the upstream repository (as of v0.29.0). Without these patches, voice connections may fail or produce corrupted audio when Discord servers negotiate modern encryption modes.

### Key Changes

- **AEAD XChaCha20-Poly1305 Support**: Implemented the `aead_xchacha20_poly1305_rtpsize` encryption mode, which Discord now prefers for voice connections. This mode provides authenticated encryption with associated data (AEAD) for enhanced security.

- **Atomic Nonce Management**: Introduced thread-safe nonce handling using `sync/atomic` operations to prevent race conditions in concurrent voice packet encryption/decryption scenarios.

- **Encryption Mode Priority**: Updated the encryption mode selection to prioritize modern AEAD modes while maintaining backward compatibility with legacy XSalsa20-Poly1305 modes.

### Technical Details

The implementation in [`voice.go`](voice.go) includes:

- **XChaCha20-Poly1305-RTPSIZE Mode**: Uses a 4-byte incrementing nonce appended to each RTP packet, with the remaining 20 bytes of the 24-byte XChaCha20 nonce zero-padded. The RTP header is used as additional authenticated data (AAD).

- **Thread-Safe Nonce Counter**: The `getAndIncrementNonce()` method uses `atomic.AddUint32()` to safely increment the nonce counter across multiple goroutines without mutex overhead.

- **Encryption Preference Order**:
  1. `aead_xchacha20_poly1305_rtpsize` (preferred)
  2. `xsalsa20_poly1305_lite`
  3. `xsalsa20_poly1305_suffix`
  4. `xsalsa20_poly1305` (fallback)

### Maintenance Status

This is a **temporary maintenance fork** intended to provide a working solution until the upstream repository implements official support for Discord's updated voice encryption protocols. Users are encouraged to migrate back to the official repository once these features are merged upstream.

---

DiscordGo is a [Go](https://golang.org/) package that provides low level 
bindings to the [Discord](https://discord.com/) chat client API. DiscordGo 
has nearly complete support for all of the Discord API endpoints, websocket
interface, and voice interface.

If you would like to help the DiscordGo package please use 
[this link](https://discord.com/oauth2/authorize?client_id=173113690092994561&scope=bot)
to add the official DiscordGo test bot **dgo** to your server. This provides 
indispensable help to this project.

* See [dgVoice](https://github.com/bwmarrin/dgvoice) package for an example of
additional voice helper functions and features for DiscordGo.

* See [dca](https://github.com/bwmarrin/dca) for an **experimental** stand alone
tool that wraps `ffmpeg` to create opus encoded audio appropriate for use with
Discord (and DiscordGo).

**For help with this package or general Go discussion, please join the [Discord 
Gophers](https://discord.gg/golang) chat server.**

## Getting Started

### Installing

This assumes you already have a working Go environment, if not please see
[this page](https://golang.org/doc/install) first.

`go get` *will always pull the latest tagged release from the master branch.*

```sh
go get github.com/darui3018823/discordgo
```

### Usage

Import the package into your project.

```go
import "github.com/darui3018823/discordgo"
```

Construct a new Discord client which can be used to access the variety of 
Discord API functions and to set callback functions for Discord events.

```go
discord, err := discordgo.New("Bot " + "authentication token")
```

See Documentation and Examples below for more detailed information.


## Documentation

**NOTICE**: This library and the Discord API are unfinished.
Because of that there may be major changes to library in the future.

The DiscordGo code is fairly well documented at this point and is currently
the only documentation available. Go reference (below) presents that information in a nice format.

- [![Go Reference](https://pkg.go.dev/badge/github.com/darui3018823/discordgo.svg)](https://pkg.go.dev/github.com/darui3018823/discordgo) 
- Hand crafted documentation coming eventually.


## Examples

Below is a list of examples and other projects using DiscordGo.  Please submit 
an issue if you would like your project added or removed from this list. 

- [DiscordGo Examples](https://github.com/darui3018823/discordgo/tree/master/examples) - A collection of example programs written with DiscordGo
- [Awesome DiscordGo](https://github.com/darui3018823/discordgo/wiki/Awesome-DiscordGo) - A curated list of high quality projects using DiscordGo

## Troubleshooting
For help with common problems please reference the 
[Troubleshooting](https://github.com/darui3018823/discordgo/wiki/Troubleshooting) 
section of the project wiki.


## Contributing
Contributions are very welcomed, however please follow the below guidelines.

- First open an issue describing the bug or enhancement so it can be
discussed.  
- Try to match current naming conventions as closely as possible.  
- This package is intended to be a low level direct mapping of the Discord API, 
so please avoid adding enhancements outside of that scope without first 
discussing it.
- Create a Pull Request with your changes against the master branch.


## List of Discord APIs

See [this chart](https://abal.moe/Discord/Libraries.html) for a feature 
comparison and list of other Discord API libraries.

## Special Thanks

[Chris Rhodes](https://github.com/iopred) - For the DiscordGo logo and tons of PRs.
