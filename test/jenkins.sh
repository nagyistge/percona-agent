#!/bin/bash

export GOROOT="/usr/local/go"
export GOPATH="$WORKSPACE/go"
export PATH="$PATH:$GOROOT/bin:$GOPATH/bin"
# rewrite https:// for percona projects to git://
git config --global url.git@github.com:percona/.insteadOf httpstools://github.com/percona/
repo="$WORKSPACE/go/src/github.com/percona/percona-agent"
[ -d "$repo" ] || mkdir -p "$repo"
cd "$repo"

# Run tests
test/runner.sh -u
exit $?
