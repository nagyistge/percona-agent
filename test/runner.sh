#!/bin/bash

if [ ! -d ".git" ]; then
   echo "./.git directory not found.  Run this script from the root dir of the repo." >&2
   exit 1
fi

export GOROOT="/usr/local/go"
export GOPATH="$HOME/go"
export PATH="$PATH:/usr/local/go/bin:$GOPATH/bin"

failures="/tmp/go-test-failures.$PID"

for t in $(find . -name \*_test.go -print); do
   echo "$t"
   dir=$(dirname "$t")
   (
      cd $dir
      go test -timeout 1m
   )
   if [ $? -ne 0 ]; then
      echo "$t" >> "/tmp/go-test-failures.$PID"
   fi
done

echo
if [ -s "$failures" ]; then
   echo "FAIL" >&2
   cat "$failures" >&2
   rm -f "$failures"
   exit 1
else
   echo "PASS"
   rm -f "$failures"
   exit 0
fi
