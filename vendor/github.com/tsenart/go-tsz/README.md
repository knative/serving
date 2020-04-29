# go-tsz

* Package tsz implement time-series compression http://www.vldb.org/pvldb/vol8/p1816-teller.pdf in Go*

[![Master Branch](https://img.shields.io/badge/branch-master-lightgray.svg)](https://github.com/tsenart/go-tsz/tree/master)
[![Master Build Status](https://secure.travis-ci.org/tsenart/go-tsz.svg?branch=master)](https://travis-ci.org/tsenart/go-tsz?branch=master)
[![Master Coverage Status](https://coveralls.io/repos/tsenart/go-tsz/badge.svg?branch=master&service=github)](https://coveralls.io/github/tsenart/go-tsz?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/tsenart/go-tsz)](https://goreportcard.com/report/github.com/tsenart/go-tsz)
[![GoDoc](https://godoc.org/github.com/tsenart/go-tsz?status.svg)](http://godoc.org/github.com/tsenart/go-tsz)

## Description

Package tsz is a fork of [github.com/dgryski/go-tsz](https://github.com/dgryski/go-tsz) that implements
improvements over the [original Gorilla paper](http://www.vldb.org/pvldb/vol8/p1816-teller.pdf) developed by @burmann
in his [Master Thesis](https://aaltodoc.aalto.fi/bitstream/handle/123456789/29099/master_Burman_Michael_2017.pdf?sequence=1)
and released in https://github.com/burmanm/gorilla-tsc.


### Differences from the original paper
- Maximum number of leading zeros is stored with 6 bits to allow up to 63 leading zeros, which are necessary when storing long values.

- Timestamp delta-of-delta is stored by first turning it to a positive integer with ZigZag encoding and then reduced by one to fit in the necessary bits. In the decoding phase all the values are incremented by one to fetch the original value.

- The compressed blocks are created with a 27 bit delta header (unlike in the original paper, which uses a 14 bit delta header). This allows to use up to one day block size using millisecond precision.

## Getting started

This library is written in Go language, please refer to the guides in https://golang.org for getting started.

This project include a Makefile that allows you to test and build the project with simple commands.
To see all available options:
```bash
make help
```

## Running all tests

Before committing the code, please check if it passes all tests using
```bash
make qa
```
