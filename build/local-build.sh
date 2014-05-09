#!/bin/sh

set -exu

err() {
   echo "$@" >&2
   exit 1
}

BIN="percona-agent"
CWD="$PWD"

# Install/update deps
./agent-build/agent-build -build=false

# Chdir to repo root and set vendor dir in GOPATH
cd ../
VENDOR_DIR="$PWD/vendor"
export GOPATH="$VENDOR_DIR:$GOPATH"

# Build percona-agent
cd bin/percona-agent
VER="$(awk '/VERSION[ ]+=/ {print $3}' ../../agent/agent.go | sed 's/"//g')"
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
if [ -d $BIN ]; then
   rm -rf $BIN/*
fi
mkdir -p "$BIN/bin" "$BIN/init.d"

cp ../install/install.sh $BIN/install
cp ../COPYING ../README.md ../Changelog.md ../Authors $BIN/
cp ../bin/$BIN/$BIN ../bin/$BIN-installer/$BIN-installer $BIN/bin
cp ../install/$BIN $BIN/init.d

tar cvfz $BIN-$VER.tar.gz $BIN/
