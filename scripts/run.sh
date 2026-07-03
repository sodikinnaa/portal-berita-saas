#!/usr/bin/env sh
set -eu

ADDR="${ADDR:-:8080}" go run ./cmd/portal
