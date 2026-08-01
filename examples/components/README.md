<img align="right" alt="dgo project mark" src="../../docs/img/dgo.svg" width="200">

## dgo components example

This example demonstrates how to use dgo to create and use message
components, such as buttons and select menus. For usage of the text input
component and modals, please refer to the `modals` example.

For dgo-specific problems, use [GitHub Issues](https://github.com/darui3018823/dgo/issues).
For general Go discussion, visit [Discord Gophers](https://discord.gg/golang).

### Build

This assumes you already have a working Go environment setup and that
Go 1.26.5 or newer and the module dependencies are available.

From within the components example folder, run the below command to compile the
example.

```sh
go build
```

### Usage

```
./components --help
Usage of ./components:
  -app string
    	Application ID
  -guild string
    	Test guild ID
  -token string
        Bot token
```

The below example shows how to start the bot from the components example folder.

```sh
./components -app YOUR_APPLICATION_ID -guild YOUR_TESTING_GUILD_ID -token YOUR_BOT_TOKEN
```
