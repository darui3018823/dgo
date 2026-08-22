<img align="right" alt="dgo project mark" src="../../docs/img/dgo.svg" width="200">

## dgo scheduled events example

This example demonstrates how to use dgo to manage scheduled events
in a guild.

For dgo-specific problems, use [GitHub Issues](https://github.com/darui3018823/dgo/issues).
For general Go discussion, visit [Discord Gophers](https://discord.gg/golang).

### Build

This assumes you already have a working Go environment setup and that
Go 1.26.6 or newer and the module dependencies are available.

From within the scheduled_events example folder, run the below command to compile the
example.

```sh
go build
```

### Usage

```
./scheduled_events --help
Usage of ./scheduled_events:
  -guild string
    	Test guild ID
  -token string
    	Bot token
  -voice string
    	Test voice channel ID
```

The below example shows how to start the bot from the scheduled_events example folder.

```sh
./scheduled_events -guild YOUR_TESTING_GUILD_ID -token YOUR_BOT_TOKEN -voice YOUR_VOICE_CHANNEL_ID
```
