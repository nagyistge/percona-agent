#!/bin/bash

PCT_TEST_MYSQL_DSN=${PCT_TEST_MYSQL_DSN:-"percona:percona@tcp(127.0.0.1:3306)/?parseTime=true"}

if [ ! -d ".git" ]; then
   echo "./.git directory not found.  Run this script from the root dir of the repo." >&2
   exit 1
fi

UPDATE_DEPENDENCIES="no";
set -- $(getopt u "$@")
while [ $# -gt 0 ]
do
    case "$1" in
    (-u) UPDATE_DEPENDENCIES="yes";;
    (--) shift; break;;
    (-*) echo "$0: error - unrecognized option $1" 1>&2; exit 1;;
    (*)  break;;
    esac
    shift
done

# Update dependencies (Yeah, no comments about this code)
if [ "$UPDATE_DEPENDENCIES" == "yes" ]; then
    thisPkg=$(go list -e)
    nonStdDeps=$(go list -f '{{join .Deps "\n"}}{{"\n"}}{{join .XTestImports "\n"}}' ./... | xargs go list -e -f '{{if not .Standard}}{{.ImportPath}}{{end}}' | sort | uniq)
    extDeps=$(echo -e "$nonStdDeps" | grep -v "$thisPkg") # extDeps = nonStdDeps - thisPkg*
    # Run `go get -u` only if there are any dependencies
    if [ "$extDeps" != "" ]; then
        echo -e "$extDeps" | xargs go get -v -u
    fi
fi

failures="/tmp/go-test-failures.$$"
coverreport="/tmp/go-test-coverreport.$$"

touch "$coverreport"
# Find test files ending with _test.go but ignore those starting with _
for t in $(find . -regex '.*/[^_][^\/]*_test.go' -print); do
   echo "$t"
   dir=$(dirname "$t")
   (
      cd $dir
      # Run tests
      go test -coverprofile=c.out -timeout 1m
   )
   if [ $? -ne 0 ]; then
      echo "$t" >> "$failures"
   elif [ -f "$dir/c.out" ]; then
      echo "$t" >> "$coverreport"
      go tool cover -func="$dir/c.out" >> "$coverreport"
      rm "$dir/c.out"
   fi
done

echo
echo "###############################"
echo "#       Cover Report          #"
cat          "$coverreport"
echo "#    End of Cover Report      #"
echo "###############################"
rm "$coverreport"

if [ -s "$failures" ]; then
   echo "SOME TESTS FAIL" >&2
   cat "$failures" >&2
   rm -f "$failures"
   exit 1
else
   echo "ALL TESTS PASS"
   rm -f "$failures"
   exit 0
fi
