<img align="right" alt="dgo project mark" src="../../docs/img/discordgo.svg" width="200">

## dgo context menu commands example

This example demonstrates how to use dgo to create and use context
menu commands. This example heavily relies on `slash_commands` example in
command handling and registration, therefore it is recommended to be read
before proceeding.

For dgo-specific problems, use [GitHub Issues](https://github.com/darui3018823/dgo/issues).
For general Go discussion, visit [Discord Gophers](https://discord.gg/golang).

### Build

This assumes you already have a working Go environment setup and that
Go 1.24 or newer and the module dependencies are available.

From within the context_menus example folder, run the below command to compile the
example.

```sh
go build
```

### Usage

```
Usage of context_menus:
  -app string
    	Application ID
  -cleanup
    	Cleanup of commands (default true)
  -guild string
    	Test guild ID
  -token string
    	Bot access token
```

The below example shows how to start the bot from the context_menus example folder.

```sh
./context_menus -app YOUR_APPLICATION_ID -guild YOUR_TESTING_GUILD -token YOUR_BOT_TOKEN
```
