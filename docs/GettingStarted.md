# Getting started

This guide creates a minimal dgo bot session using the public Discord Bot API.

## Requirements

- Go 1.26.6 or newer
- A Discord application with a bot user
- A bot token stored outside source control

Create applications in the
[Discord Developer Portal](https://discord.com/developers/applications).
Automated user accounts, raw user tokens, and private Discord client routes
are not supported.

## Install

Create or open a Go module, then add dgo:

```sh
go mod init example.com/my-bot
go get github.com/darui3018823/dgo@v1.1.0
```

There is no need to copy the repository into `GOPATH` or run `go install` for
the library.

## Minimal session

```go
package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/darui3018823/dgo"
)

func main() {
	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_BOT_TOKEN is required")
	}

	session, err := dgo.NewBot(token)
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()

	// dgo starts with no Gateway intents. Request only what the bot needs.
	session.Identify.Intents = dgo.IntentsGuilds
	session.AddHandler(func(_ *dgo.Session, ready *dgo.Ready) {
		log.Printf("connected as %s", ready.User.Username)
	})

	if err := session.Open(); err != nil {
		log.Fatal(err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
}
```

Run it with the token in the environment:

```sh
go run .
```

## Safe defaults

- `New`, `NewBot`, and `NewOAuth2` default to `IntentsNone`. Add only the
  intents required by the bot before opening a Gateway connection.
- Messages default to an empty `allowed_mentions.parse` list. Opt in to
  mentions explicitly when needed.
- `NewBot` accepts a raw bot token and adds the required `Bot ` prefix. If you
  use the generic `New` constructor, pass the full `Bot <token>` credential.
- Interaction and webhook tokens may appear in request URLs; dgo redacts them
  from its diagnostic metadata and logs.

Privileged intents must also be enabled for the application in the Developer
Portal. See Discord's
[Gateway intents documentation](https://docs.discord.com/developers/events/gateway#gateway-intents).

## Next steps

- Browse the [examples on GitHub](https://github.com/darui3018823/dgo/tree/master/examples).
- Review the [migration and compatibility guide](Migration.md).
- Check the [public API inventory](API.md).
- Use the [package reference](https://pkg.go.dev/github.com/darui3018823/dgo)
  for exported types and methods.
