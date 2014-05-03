#!/bin/bash

# Install from web
# wget -qO- http://www.percona.com/downloads/TESTING/percona-agent/install | sudo bash

error() {
   echo "ERROR: $1" >&2
   exit 1
}

# ###########################################################################
# Sanity checks and setup
# ###########################################################################

# Check if script is run as root as we need write access to /etc, /usr/local
if [ $EUID -ne 0 ]; then
   error "percona-agent install requires root user; detected effective user ID $EUID"
fi

# Check compatibility
KERNEL=`uname -s`
if [ "$KERNEL" != "Linux" -a "$KERNAL" != "Darwin" ]; then
   error "percona-agent only runs on Linxu; detected $KERNEL"
fi

PLATFORM=`uname -m`
if [ "$PLATFORM" != "x86_64" -a "$PLATFORM" != "i686" -a "$PLATFORM" != "i386" ]; then
   error "percona-agent only support x86_64 and i686 platforms; detected $PLATFORM"
fi

echo "percona-agent install detected $KERNEL $PLATFORM" 

# Set up variables
TARGZ_NAME="percona-agent.tar.gz"
DOWNLOADPATH="http://www.percona.com/downloads/TESTING/percona-agent/$TARGZ_NAME"
INSTALL_DIR="/usr/local/percona/"
BASEDIR="$INSTALL_DIR/percona-agent"

TMP_PATH="$(mktemp -d /tmp/percona-agent.XXXXXX)" || exit 1
TMP_FILENAME="$TMP_PATH/$TARGZ_NAME"

mkdir -p "$BASEDIR" \
   || error "'mkdir -p $BASEDIR' failed"

# ###########################################################################
# Download and extract package
# ###########################################################################

echo "Downloading $DOWNLOADPATH to $TMP_FILENAME..."
wget "$DOWNLOADPATH" -O "$TMP_FILENAME"
if [ $? -ne 0 ] ; then
   error "'wget $DOWNLOADPATH' failed"
fi

echo "Extracting $TMP_FILENAME to $INSTALL_DIR..."
tar -xzf "$TMP_FILENAME" -C "$INSTALL_DIR" --overwrite \
   || error "Failed to extract $TMP_FILENAME"

rm -rf "$TMP_PATH" \
   || error "Failed to remove $TMP_PATH"

# ###########################################################################
# Install percona-agent (percona-agent-setup install)
# ###########################################################################

"$TMP_PATH/percona-agent-installer" -basedir "$BASEDIR"
if [ $? -ne 0 ]; then
   error "Failed to install percona-agent"
fi

# ###########################################################################
# Install sys-int script
# ###########################################################################

cp -f "$INSTALL_DIR/percona-agent/sys-init/percona-agent" "/etc/init.d"
chmod a+x "/etc/init.d/percona-agent"

# Check if the system has chkconfig or update-rc.d.
if hash update-rc.d 2>/dev/null; then
        echo "Using update-rc.d to install percona-agent service" 
        update-rc.d  percona-agent defaults
elif hash chkconfig 2>/dev/null; then
        echo "Using chkconfig to install percona-agent service" 
        chkconfig percona-agent on
else
   error "Cannot find chkconfig or update-rc.d.  Please email the follow to cloud-tools@percona.com:
$(cat /etc/*release)
"
fi

# ###########################################################################
# Cleanup
# ###########################################################################

echo "percona-agent install successful"
exit 0
