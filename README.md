# plugin-api

[![Build Status](https://travis-ci.org/gotify/plugin-api.svg?branch=master)](https://travis-ci.org/gotify/plugin-api)
[![GoDoc](https://godoc.org/github.com/gotify/plugin-api?status.svg)](https://godoc.org/github.com/gotify/plugin-api)

This repository consisted of APIs used in gotify plugins.

## Usage

### Plugin API

https://gotify.net/docs/plugin

### CLI tools

#### `github.com/gotify/cmd/gomod-cap`

Since `go.mod` follows a [minimal version selection](https://github.com/golang/proposal/blob/master/design/24301-versioned-go.md), packages are built with the lowest common version requirement defined in `go.mod`. This poses a problem when developing plugins:

If the gotify server is built with the following go.mod:
```
require some/package v0.1.0
```
But when the plugin is built, it used a newer version of this package:
```
require some/package v0.1.1
```
Since the server is built with `v0.1.0` and the plugin is built with `v0.1.1` of `some/package`, the built plugin could not be loaded due to different import package versions.

`gomod-cap` is a simple util to ensure that plugin `go.mod` files does not have higher version requirements than the main gotify `go.mod` file.

To resolve all incompatible requirements:
```bash
$ go run github.com/gotify/plugin-api/cmd/gomod-cap -from /path/to/gotify/server/go.mod -to /path/to/plugin/go.mod
```
To only check for incompatible requirements(useful in CI):
```bash
$ go run github.com/gotify/plugin-api/cmd/gomod-cap -from /path/to/gotify/server/go.mod -to /path/to/plugin/go.mod -check=true
```
