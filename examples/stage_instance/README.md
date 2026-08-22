<img align="right" alt="dgo project mark" src="../../docs/img/dgo.svg" width="200">

## dgo stage instance example

This example demonstrates how to use dgo to manage stage instances.

For dgo-specific problems, use [GitHub Issues](https://github.com/darui3018823/dgo/issues).
For general Go discussion, visit [Discord Gophers](https://discord.gg/golang).

### Build

This assumes you already have a working Go environment setup and that
Go 1.26.6 or newer and the module dependencies are available.

From within the stage_instance example folder, run the below command to compile the
example.

```sh
go build
```

### Usage

```
./stage_instance --help
Usage of ./stage_instance:
  -guild string
    	Test guild ID
  -stage string
    	Test stage channel ID
  -token string
    	Bot token
```

The below example shows how to start the bot from the stage_instance example folder.

```sh
./stage_instance -guild YOUR_TESTING_GUILD_ID -stage YOUR_STAGE_CHANNEL_ID -token YOUR_BOT_TOKEN
```
