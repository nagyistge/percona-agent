#!/bin/sh

set -exu

err() {
   echo "$@" >&2
   exit 1
}

BIN="percona-agent"
CWD="$PWD"

PLATFORM=`uname -m`
if [ "$PLATFORM" = "x86_64" ]; then
   ARCH="x86_64"  # no change
elif [ "$PLATFORM" = "i686" -o "$PLATFORM" = "i386" ]; then
   ARCH="i386"
else
   error "Unknown platform: $PLATFORM"
fi

# Install/update deps
./agent-build/agent-build -build=false

# Chdir to repo root and set vendor dir in GOPATH
cd ../
VENDOR_DIR="$PWD/vendor"
export GOPATH="$VENDOR_DIR:$GOPATH"

# Build percona-agent
cd bin/percona-agent
if [ -n "${1:-""}" ]; then
   VER="$1"
else
   VER="$(awk '/var VERSION/ {print $5}' ../../agent/agent.go | sed 's/"//g')"
fi
REV="$(git rev-parse HEAD)"
go build -ldflags "-X github.com/percona/percona-agent/agent.REVISION $REV"
./percona-agent -version

# Check that bin was compiled with pkgs from vendor dir
strings percona-agent | grep "$VENDOR_DIR/src/github.com/percona/cloud-protocol" \
   || err "ERROR: percona-agent not built with vendor deps ($VENDOR_DIR)"

# Build percona-agent-installer
cd ../$BIN-installer/
go build

# Build the package
cd $CWD
[ -f $BIN.tar.gz ] && rm -f $BIN.tar.gz

PKG_DIR="$BIN-$VER-$ARCH"
if [ -d $PKG_DIR ]; then
   rm -rf $PKG_DIR/*
fi
mkdir -p "$PKG_DIR/bin" "$PKG_DIR/init.d"

cp ../install/install.sh $PKG_DIR/install
cp ../COPYING ../README.md ../Changelog ../Authors $PKG_DIR/
cp ../bin/$BIN/$BIN ../bin/$BIN-installer/$BIN-installer $PKG_DIR/bin
cp ../install/$BIN $PKG_DIR/init.d

tar cvfz $BIN-$VER-$ARCH.tar.gz $PKG_DIR/
