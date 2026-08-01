<img align="right" alt="dgo project mark" src="../../docs/img/dgo.svg" width="200">

## dgo echo example

This example demonstrates how to use dgo to create a simple,
slash commands based bot, that will echo your messages.

For dgo-specific problems, use [GitHub Issues](https://github.com/darui3018823/dgo/issues).
For general Go discussion, visit [Discord Gophers](https://discord.gg/golang).

### Build

This assumes you already have a working Go environment setup and that
Go 1.26.5 or newer and the module dependencies are available.

From within the echo example folder, run the below command to compile the
example.

```sh
go build
```

### Usage

```
./echo --help
Usage of ./echo:
  -app string
        Application ID
  -guild string
        Guild ID
  -token string
        Bot token

```

Run the command below to start the bot.

```sh
./echo -guild YOUR_TESTING_GUILD_ID -app YOUR_APPLICATION_ID -token YOUR_BOT_TOKEN
```
