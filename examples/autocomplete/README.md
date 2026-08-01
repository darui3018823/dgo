<img align="right" alt="dgo project mark" src="../../docs/img/dgo.svg" width="200">

## dgo slash command autocomplete example

This example demonstrates how to use dgo to create and use
autocomplete options in Slash Commands. As this example uses interactions,
slash commands and slash command options, it is recommended to read
`slash_commands` example before proceeding.

For dgo-specific problems, use [GitHub Issues](https://github.com/darui3018823/dgo/issues).
For general Go discussion, visit [Discord Gophers](https://discord.gg/golang).

### Build

This assumes you already have a working Go environment setup and that
Go 1.26.5 or newer and the module dependencies are available.

From within the autocomplete example folder, run the below command to compile the
example.

```sh
go build
```

### Usage

```
./autocomplete --help
Usage of ./autocomplete:
  -guild string
    	Test guild ID. If not passed - bot registers commands globally
  -rmcmd
    	Whether to remove all commands after shutting down (default true)
  -token string
        Bot token
```

The below example shows how to start the bot from the autocomplete example folder.

```sh
./autocomplete -guild YOUR_TESTING_GUILD_ID -token YOUR_BOT_TOKEN
```
