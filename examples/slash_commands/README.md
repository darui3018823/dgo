<img align="right" alt="dgo project mark" src="../../docs/img/discordgo.svg" width="200">

## dgo slash commands example

This example demonstrates how to use dgo to create a slash-command bot,
which would be able to listen and respond to interactions. This example covers all aspects
of slash command interactions: options, choices, responses and followup messages.
To avoid confusion, this example is more of a **step-by-step tutorial**, than a demonstration bot.

For dgo-specific problems, use [GitHub Issues](https://github.com/darui3018823/dgo/issues).
For general Go discussion, visit [Discord Gophers](https://discord.gg/golang).

### Build

This assumes you already have a working Go environment setup and that
Go 1.24 or newer and the module dependencies are available.

From within the slash_commands example folder, run the below command to compile the
example.

```sh
go build
```

### Usage

```
Usage of slash_commands:
  -guild string
    	Test guild ID. If not passed - bot registers commands globally
  -rmcmd
    	Whether to remove all commands after shutting down (default true)
  -token string
    	Bot access token
```

The below example shows how to start the bot from the slash_commands example folder.

```sh
./slash_commands -guild YOUR_TESTING_GUILD -token YOUR_BOT_TOKEN
```
