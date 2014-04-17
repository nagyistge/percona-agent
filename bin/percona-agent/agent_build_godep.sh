#!/bin/bash

# for installing godep:
# go get github.com/tools/godep
#
# if you want to build Godeps file (with new dependencies), but without pks, run:
# godep save -copy=false
#
# if you want to build Godeps file (with dependencies list) and with
# needed pkgs copied into Godeps dir, run:
# godep save
#
# for restoring missing pkgs run:
# godep restore

godep save
godep go build percona-agent.go
gzip -f9 percona-agent

