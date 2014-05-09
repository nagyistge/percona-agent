#!/bin/bash

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
   error "$BIN only runs on Linux; detected $KERNEL"
fi

PLATFORM=`uname -m`
if [ "$PLATFORM" != "x86_64" -a "$PLATFORM" != "i686" -a "$PLATFORM" != "i386" ]; then
   error "$BIN only support x86_64 and i686 platforms; detected $PLATFORM"
fi

echo "Detected $KERNEL $PLATFORM"

# Set up variables.
INSTALLER_DIR=$(dirname $0)
INSTALL_DIR="/usr/local/percona"

# ###########################################################################
# Install percona-agent
# ###########################################################################

# BASEDIR here must match BASEDIR in percona-agent sys-init script.
BASEDIR="$INSTALL_DIR/$BIN"
mkdir -p "$BASEDIR/"{bin,init.d} \
   || error "'mkdir -p $BASEDIR/{bin,init.d}' failed"

# Install agent binary
cp -f "$INSTALLER_DIR/bin/$BIN" "$BASEDIR/bin/"

# Copy init script (for backup, as we are going to install it in /etc/init.d)
cp -f "$INSTALLER_DIR/init.d/$BIN" "$BASEDIR/init.d/"

# Run installer and forward all remaining parameters to it with "$@"
"$INSTALLER_DIR/bin/$BIN-installer" -basedir "$BASEDIR" "$@"
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
   cp -f "$INSTALL_DIR/$BIN/init.d/$BIN" "/etc/init.d/"
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
