#!/bin/bash

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
   error "$BIN uninstall requires root user; detected effective user ID $EUID"
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
INSTALL_DIR="/usr/local/percona"

# ###########################################################################
# Stop agent and uninstall sys-int script
# ###########################################################################
INIT_SCRIPT="/etc/init.d/$BIN"
if [ "$KERNEL" != "Darwin" ]; then
   if [ -x "$INIT_SCRIPT" ]; then
       echo "Stopping agent ..."
       ${INIT_SCRIPT} stop
       if [ $? -ne 0 ]; then
          error "Failed to stop $BIN"
       fi
   fi

   echo "Uninstalling sys-init script ..."
   # Check if the system has chkconfig or update-rc.d.
   if hash update-rc.d 2>/dev/null; then
           echo "Using update-rc.d to uninstall $BIN service"
           update-rc.d -f "$BIN" remove
   elif hash chkconfig 2>/dev/null; then
           echo "Using chkconfig to install $BIN service" 
           chkconfig --del "$BIN"
   else
      echo "Cannot find chkconfig or update-rc.d.  $BIN is installed but"
      echo "it will not restart automatically with the server on reboot.  Please"
      echo "email the follow to cloud-tools@percona.com:"
      cat /etc/*release
   fi

   # Remove init script
   echo "Removing $INIT_SCRIPT ..."
   rm -f "$INIT_SCRIPT"
else
   echo "Mac OS detected, no sys-init script. To stop $BIN:"
   echo "killall $BIN"
fi


# ###########################################################################
# Uninstall percona-agent
# ###########################################################################

# BASEDIR here must match BASEDIR in percona-agent sys-init script.
BASEDIR="$INSTALL_DIR/$BIN"
echo "Removing dir $BASEDIR ..."
rm -rf "$BASEDIR"

# ###########################################################################
# Cleanup
# ###########################################################################

echo "$BIN uninstall successful"
exit 0
