<img align="right" alt="dgo project mark" src="../../docs/img/dgo.svg" width="200">

## dgo ping-pong example

This example demonstrates how to use dgo to create a ping-pong bot.

This Bot will respond to "ping" with "Pong!" and "pong" with "Ping!".

For dgo-specific problems, use [GitHub Issues](https://github.com/darui3018823/dgo/issues).
For general Go discussion, visit [Discord Gophers](https://discord.gg/golang).

### Build

This assumes you already have a working Go environment setup and that
Go 1.26.5 or newer and the module dependencies are available.


From within the pingpong example folder, run the below command to compile the
example.

```sh
go build
```

### Usage

This example supports bot tokens only. Automated user accounts and raw user
credentials are prohibited and are not supported by dgo.

```
./pingpong --help
Usage of ./pingpong:
  -t string
        Bot token
```

The below example shows how to start the bot

```sh
./pingpong -t YOUR_BOT_TOKEN
Bot is now running.  Press CTRL-C to exit.
```
