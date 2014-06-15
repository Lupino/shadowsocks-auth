#!/usr/bin/env bash

DEPS=(
    github.com/shadowsocks/shadowsocks-go/shadowsocks
    github.com/garyburd/redigo/redis
    github.com/cyfdecyf/leakybuf
    code.google.com/p/go.crypto/blowfish
)

echo "Check deps..."

for dep in ${DEPS[@]};do
    [ -d $GOPATH/src/$dep ] || go get $dep
done
