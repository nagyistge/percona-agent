#!/bin/bash

if [ ! -d "bin" ]; then
   echo "./bin directory not found.  Run this script from the root dir of the repo." >&2
   exit 1
fi

export GOROOT="/usr/local/go"
export GOPATH="$HOME/go"
export PATH="$PATH:/usr/local/go/bin:$GOPATH/bin"

for t in $(find . -name \*_test.go -print); do
   echo "$t"
   dir=$(dirname "$t")
   (
      cd $dir
      go test -timeout 1m
   )
   echo
done
