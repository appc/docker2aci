#!/bin/sh
set -e
grep -q file1 file1
grep -q file1 file2
grep -q file3 file3
grep -q file4 file4
if [ "$rendered" != "true" ] ; then
	test -e file1
	test -e file2
	test $(stat -c %i file1) -eq $(stat -c %i file2)
	test $(stat -c %i file3) -ne $(stat -c %i file4)
fi
echo "SUCCESS"
