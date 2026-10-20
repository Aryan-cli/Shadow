#!/bin/bash
set -e

REDIS_SERVER=$(nix-shell -p redis --run "which redis-server" 2>/dev/null || echo "/nix/store/pnc74r60iz1g5bpqv4qh76a8cc3g0n97-redis-7.2.10/bin/redis-server")
$REDIS_SERVER --daemonize yes --logfile /tmp/redis.log

go run ./cmd/server/main.go
