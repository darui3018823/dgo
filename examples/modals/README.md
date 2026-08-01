<img align="right" alt="dgo project mark" src="../../docs/img/dgo.svg" width="200">

## dgo modals example

This example demonstrates how to use dgo to send and process text
inputs in modals. If you have not read `slash_commands` and `components`
examples yet it is recommended to do so before proceeding. As this example
is built using interactions and Slash Commands.

For dgo-specific problems, use [GitHub Issues](https://github.com/darui3018823/dgo/issues).
For general Go discussion, visit [Discord Gophers](https://discord.gg/golang).

### Build

This assumes you already have a working Go environment setup and that
Go 1.26.5 or newer and the module dependencies are available.

From within the modals example folder, run the below command to compile the
example.

```sh
go build
```

### Usage

```
Usage of modals:
  -app string
    	Application ID
  -cleanup
    	Cleanup of commands (default true)
  -guild string
    	Test guild ID
  -results string
    	Channel where send survey results to
  -token string
    	Bot access token
```

The below example shows how to start the bot from the modals example folder.

```sh
./modals -app YOUR_APPLICATION_ID -guild YOUR_TESTING_GUILD -results YOUR_TESTING_CHANNEL -token YOUR_BOT_TOKEN
```
