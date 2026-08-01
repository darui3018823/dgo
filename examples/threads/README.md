<img align="right" alt="dgo project mark" src="../../docs/img/dgo.svg" width="200">

## dgo threads example

This example demonstrates how to use dgo to manage channel threads.

For dgo-specific problems, use [GitHub Issues](https://github.com/darui3018823/dgo/issues).
For general Go discussion, visit [Discord Gophers](https://discord.gg/golang).

### Build

This assumes you already have a working Go environment setup and that
Go 1.26.5 or newer and the module dependencies are available.

From within the threads example folder, run the below command to compile the
example.

```sh
go build
```

### Usage

```
./threads --help
Usage of ./threads:
  -token string
    	Bot token
```

The below example shows how to start the bot from the threads example folder.

```sh
./threads -token YOUR_BOT_TOKEN
```
