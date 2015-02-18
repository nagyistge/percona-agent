#!/bin/sh

set -eu

err() {
   echo "$@" >&2
   exit 1
}

REL="${1:-""}"
[ ! "$REL" ] && err "Specify a dev build number like '$0 01'"

BIN="percona-agent"
BUILD_DIR="$PWD"

PLATFORM=`uname -m`
if [ "$PLATFORM" = "x86_64" ]; then
   ARCH="x86_64"  # no change
elif [ "$PLATFORM" = "i686" -o "$PLATFORM" = "i386" ]; then
   ARCH="i386"
else
   error "Unknown platform: $PLATFORM"
fi

# Build percona-agent
cd ../bin/percona-agent
REV="$(git rev-parse HEAD)"
VER="$(awk '/var VERSION/ {print $5}' ../../agent/agent.go | sed 's/"//g')"
go build -ldflags "-X github.com/percona/percona-agent/agent.REVISION $REV -X github.com/percona/percona-agent/agent.REL -$REL"
./percona-agent -version

mv $BIN $BUILD_DIR/$BIN-$REL-dev
