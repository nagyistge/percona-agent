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
   error "$BIN supports only x86_64 and i686 platforms; detected $PLATFORM"
fi

echo "Detected $KERNEL $PLATFORM"

# Set up variables.
INSTALLER_DIR=$(dirname $0)
INSTALL_DIR="/usr/local/percona"

# BASEDIR here must match BASEDIR in percona-agent sys-init script.
BASEDIR="$INSTALL_DIR/$BIN"
INIT_SCRIPT="/etc/init.d/$BIN"

# ###########################################################################
# Version comparision
# https://gist.github.com/livibetter/1861384
# ###########################################################################
_ver_cmp_1() {
  (( "10#$1" == "10#$2" )) && return 0
  (( "10#$1" >  "10#$2" )) && return 1
  (( "10#$1" <  "10#$2" )) && return 2
  exit 1
}

ver_cmp() {
  local A B i result
  A=(${1//./ })
  B=(${2//./ })
  i=0
  while (( i < ${#A[@]} )) && (( i < ${#B[@]})); do
    _ver_cmp_1 "${A[i]}" "${B[i]}"
    result=$?
    [[ $result =~ [12] ]] && return $result
    let i++
  done
  _ver_cmp_1 "${#A[i]}" "${#B[i]}"
  return $?
}

install() {
    # ###########################################################################
    # Check if already installed and upgrade if needed
    # ###########################################################################

    newVersion=$("$INSTALLER_DIR/bin/$BIN" -version | cut -f2 -d" ")
    echo "Version provided by installer: $newVersion"
    if [ -x "$BASEDIR/bin/$BIN" ]; then
        currentVersion=$("$BASEDIR/bin/$BIN" -version | cut -f2 -d" ")
        echo "Version currently installed: $currentVersion"
        cmpVer=0
        ver_cmp "$currentVersion" "$newVersion" || cmpVer=$?
        if [ "$cmpVer" == "2" ]; then
            echo "Upgrading to $newVersion..."
            if [ "$KERNEL" != "Darwin" ]; then
                ${INIT_SCRIPT} stop
            else
                echo "killall $BIN"
            fi

            # Install agent binary
            cp -f "$INSTALLER_DIR/bin/$BIN" "$BASEDIR/bin/"

            # Copy init script (for backup, as we are going to install it in /etc/init.d)
            cp -f "$INSTALLER_DIR/init.d/$BIN" "$BASEDIR/init.d/"

            if [ "$KERNEL" != "Darwin" ]; then
                cp -f "$INSTALL_DIR/$BIN/init.d/$BIN" "/etc/init.d/"
                chmod a+x "/etc/init.d/$BIN"
                ${INIT_SCRIPT} start
            else
               echo "Mac OS detected, not installing sys-init script.  To start $BIN:"
               echo "$BASEDIR/bin/$BIN -basedir $BASEDIR"
            fi
            echo
            echo "Success! $BIN was upgraded to $newVersion and restarted."
            echo
            exit 0
        elif [ "$cmpVer" == "1" ]; then
            echo "Newer version already installed, exiting."
            exit 1
        else
            echo "Same version already installed, exiting."
            exit 1
        fi
    fi

    # ###########################################################################
    # Create dir structure if not exist
    # ###########################################################################

    mkdir -p "$BASEDIR/"{bin,init.d} \
        || error "'mkdir -p $BASEDIR/{bin,init.d}' failed"

    # ###########################################################################
    # Run installer and forward all remaining parameters to it with "$@"
    # ###########################################################################

    "$INSTALLER_DIR/bin/$BIN-installer" -basedir "$BASEDIR" $@
    exitStatus=$?
    if [ "$exitStatus" -eq "10" ]; then
       echo "  -uninstall: Stop agent and uninstall it (USE WITH CAUTION!)"
       exit $?
    elif [ "$exitStatus" -ne "0" ]; then
       echo
       error "Failed to install $BIN"
    fi

    # ###########################################################################
    # Install sys-int script and percona-agent binary
    # ###########################################################################

    # Install agent binary
    cp -f "$INSTALLER_DIR/bin/$BIN" "$BASEDIR/bin/"

    # Copy init script (for backup, as we are going to install it in /etc/init.d)
    cp -f "$INSTALLER_DIR/init.d/$BIN" "$BASEDIR/init.d/"

    "$BASEDIR/bin/$BIN" -ping >/dev/null
    if [ $? -ne 0 ]; then
       error "Installed $BIN but ping test failed"
    fi

    if [ "$KERNEL" != "Darwin" ]; then
       cp -f "$INSTALL_DIR/$BIN/init.d/$BIN" "/etc/init.d/"
       chmod a+x "/etc/init.d/$BIN"

       # Check if the system has chkconfig or update-rc.d.
       if hash update-rc.d 2>/dev/null; then
               echo "Using update-rc.d to install $BIN service"
               update-rc.d  $BIN defaults >/dev/null
       elif hash chkconfig 2>/dev/null; then
               echo "Using chkconfig to install $BIN service"
               chkconfig $BIN on >/dev/null
       else
          echo "Cannot find chkconfig or update-rc.d.  $BIN is installed but"
          echo "it will not restart automatically with the server on reboot.  Please"
          echo "email the follow to cloud-tools@percona.com:"
          cat /etc/*release
       fi

       ${INIT_SCRIPT} start
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
    hostname=$(hostname)
    echo
    echo "Success! $BIN $newVersion is installed and running."
    echo "Go to https://cloud.percona.com and look for $hostname."
    echo
    exit 0
}

uninstall() {
    # ###########################################################################
    # Stop agent and uninstall sys-int script
    # ###########################################################################
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
               echo "Using chkconfig to uninstall $BIN service"
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
    echo "Removing dir $BASEDIR ..."
    rm -rf "$BASEDIR"

    # ###########################################################################
    # Cleanup
    # ###########################################################################

    echo "$BIN uninstall successful"
    exit 0
}

if [ "$*" == "--help" -o "$*" == "-help" -o "$*" == "-h" -o "$*" == "-?" ]; then
   "$INSTALLER_DIR/bin/$BIN-installer" -h
   echo "See http://cloud-docs.percona.com/Install.html for more information."
   exit 0
fi
[[ $* == *-uninstall* ]] && uninstall
install $@


