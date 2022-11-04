# xtwid

`xtwid` is a tool that builds a twid binary with external plug-in modules. It is
inspired by [xcaddy](https://github.com/caddyserver/xcaddy).

## Install

You need the Go toolchain/compiler.

## Usage

```sh
# Print help
go run github.com/diamondburned/twikit/cmd/xtwid -h

# Install with twidiscord. Multiple import-module flags can be stated.
go run github.com/diamondburned/twikit/cmd/xtwid \
	-import-module github.com/diamondburned/twidiscord
```
