<img align="right" alt="dgo project mark" src="../../docs/img/discordgo.svg" width="200">

## dgo auto moderation example

This example demonstrates how to use dgo to manage auto moderation
rules and triggers.

For dgo-specific problems, use [GitHub Issues](https://github.com/darui3018823/dgo/issues).
For general Go discussion, visit [Discord Gophers](https://discord.gg/golang).

### Build

This assumes you already have a working Go environment setup and that
Go 1.24 or newer and the module dependencies are available.

From within the auto_moderation example folder, run the below command to compile the
example.

```sh
go build
```

### Usage

```
Usage of auto_moderation:
  -channel string
    	ID of the testing channel
  -guild string
    	ID of the testing guild
  -token string
    	Bot authorization token
```

The below example shows how to start the bot from the auto_moderation example folder.

```sh
./auto_moderation -channel YOUR_TESTING_CHANNEL -guild YOUR_TESTING_GUILD -token YOUR_BOT_TOKEN
```
