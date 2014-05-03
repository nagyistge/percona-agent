#!/bin/bash

# You can run this script locally, but it's main purpose is to be ran from
# percona.com like:
#
#   wget -qO- percona.com/downloads/TESTING/percona-agent/install | sudo bash
#
# This script is mostly a wrapper around the percona-agent-installer binary
# which does the heay lifting: creating API resources, configuring service, etc.

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
   error "$BIN only runs on Linxu; detected $KERNEL"
fi

PLATFORM=`uname -m`
if [ "$PLATFORM" != "x86_64" -a "$PLATFORM" != "i686" -a "$PLATFORM" != "i386" ]; then
   error "$BIN only support x86_64 and i686 platforms; detected $PLATFORM"
fi

WGET="$(which wget)"
CURL="$(which curl)"
if [ -z "$WGET" -a -z "$CURL" ]; then
   error "Neither wget nor curl is installed or in PATH"
fi

echo "Detected $KERNEL $PLATFORM" 

# Set up variables.
PKG="$BIN.tar.gz"
DOWNLOAD_URL="${DOWNLOAD_URL:-"http://www.percona.com/downloads/TESTING/$BIN/$PKG"}"
INSTALL_DIR="/usr/local/percona"

TMP_DIR="$(mktemp -d /tmp/$BIN.XXXXXX)" || exit 1
TMP_PKG="$TMP_DIR/$PKG"

# BASEDIR here must match BASEDIR in percona-agent sys-init script.
BASEDIR="$INSTALL_DIR/$BIN" 
mkdir -p "$BASEDIR" \
   || error "'mkdir -p $BASEDIR' failed"

# ###########################################################################
# Download and extract package
# ###########################################################################

echo "Downloading $DOWNLOAD_URL to $TMP_PKG..."
if [ "$WGET" ]; then 
   "$WGET" "$DOWNLOAD_URL" -O "$TMP_PKG"
else
   "$CURL" -L "$DOWNLOAD_URL" -o "$TMP_PKG"
fi
if [ $? -ne 0 ] ; then
   error "Failed to download $DOWNLOAD_URL"
fi

echo "Extracting $TMP_PKG to $INSTALL_DIR..."
tar -xzf "$TMP_PKG" -C "$INSTALL_DIR" --overwrite \
   || error "Failed to extract $TMP_PKG"

rm -rf "$TMP_DIR" \
   || echo "Cannot remove $TMP_DIR (ignoring)"

# ###########################################################################
# Install percona-agent (percona-agent-setup install)
# ###########################################################################

"$BASEDIR/bin/$BIN-installer" -basedir "$BASEDIR"
if [ $? -ne 0 ]; then
   error "Failed to install $BIN"
fi

"$BASEDIR/bin/$BIN" -ping
if [ $? -ne 0 ]; then
   error "Installed $BIN but ping test failed"
fi

# ###########################################################################
# Install sys-int script
# ###########################################################################

if [ "$KERNEL" != "Darwin" ]; then
   cp -f "$INSTALL_DIR/$BIN/sys-init/$BIN" "/etc/init.d"
   chmod a+x "/etc/init.d/$BIN"

   # Check if the system has chkconfig or update-rc.d.
   if hash update-rc.d 2>/dev/null; then
           echo "Using update-rc.d to install $BIN service" 
           update-rc.d  $BIN defaults
   elif hash chkconfig 2>/dev/null; then
           echo "Using chkconfig to install $BIN service" 
           chkconfig $BIN on
   else
      echo "Cannot find chkconfig or update-rc.d.  $BIN is installed but"
      echo "it will not restart automatically with the server on reboot.  Please"
      echo "email the follow to cloud-tools@percona.com:"
      cat /etc/*release
   fi

   /etc/init.d/$BIN start
   if [ $? -ne 0 ]; then
      error "Failed to start $BIN"
   fi
else
   echo "Mac OS detected, not installing sys-init script.  To start $BIN:"
   echo "$BASEDIR/bin/$BIN -basedir $BASEDIR"
fi

# ###########################################################################
# Cleanup
# ###########################################################################

echo "$BIN install successful"
exit 0
