#!/bin/bash

VERSION="1.0.5"

set -u

error() {
   echo "ERROR: $1" >&2
   exit 1
}

# To avoid typos for the most repeated and important word in the script:
BIN="percona-agent"

# ###########################################################################
# Sanity checks and setup
# ###########################################################################

# Check if script is run as root as we need write access to /etc, /usr/local
if [ $EUID -ne 0 ]; then
   error "$BIN install requires root user; detected effective user ID $EUID"
fi

# Check compatibility
KERNEL=`uname -s`
if [ "$KERNEL" != "Linux" -a "$KERNEL" != "Darwin" ]; then
   error "$BIN only runs on Linux; detected $KERNEL"
fi

PLATFORM=`uname -m`
if [ "$PLATFORM" != "x86_64" -a "$PLATFORM" != "i686" -a "$PLATFORM" != "i386" ]; then
   error "$BIN only support x86_64 and i686 platforms; detected $PLATFORM"
fi

echo "Detected $KERNEL $PLATFORM"

# For i686 arch we use i386 binary
if [ "$PLATFORM" == "i686" ]; then
    PLATFORM="i386"
fi
PACKAGE_LOCATION="http://www.percona.com/redir/downloads/percona-agent/LATEST/percona-agent-${VERSION}-${PLATFORM}.tar.gz"
echo "Downloading required data..."
if hash curl 2>/dev/null; then
    curl -L -o "/tmp/percona-agent-${VERSION}-${PLATFORM}.tar.gz" "${PACKAGE_LOCATION}"
elif hash wget 2>/dev/null; then
    wget -cO "/tmp/percona-agent-${VERSION}-${PLATFORM}.tar.gz" "${PACKAGE_LOCATION}"
else
    error "Unable to download installer data. Please install wget or curl."
fi
INSTALLER_DIR="/tmp/percona-agent-${VERSION}-${PLATFORM}"
tar -C "/tmp" -xzf "/tmp/percona-agent-${VERSION}-${PLATFORM}.tar.gz"
"/tmp/percona-agent-${VERSION}-${PLATFORM}/install" $@

# Cleanup
rm -rf "${INSTALLER_DIR}"
