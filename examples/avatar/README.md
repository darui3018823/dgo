<img align="right" alt="dgo project mark" src="../../docs/img/dgo.svg" width="200">

## dgo avatar example

This example demonstrates how to use dgo to change the avatar for
a Discord account.  This example works both with a local file or the URL of
an image.

For dgo-specific problems, use [GitHub Issues](https://github.com/darui3018823/dgo/issues).
For general Go discussion, visit [Discord Gophers](https://discord.gg/golang).

### Build

This assumes you already have a working Go environment setup and that
Go 1.26.5 or newer and the module dependencies are available.

From within the avatar example folder, run the below command to compile the
example.

```sh
go build
```

### Usage

This example supports bot tokens only. Automated user accounts and raw user
credentials are prohibited and are not supported by dgo.

```
./avatar --help
Usage of ./avatar:
  -f string
        Avatar File Name
  -t string
        Bot Token
  -u string
        URL to the avatar image
```

The below example shows how to set your Avatar from a local file.

```sh
./avatar -t TOKEN -f avatar.png
```
The below example shows how to set your Avatar from a URL.

```sh
./avatar -t TOKEN -u https://raw.githubusercontent.com/darui3018823/dgo/master/docs/img/dgo.svg
```
