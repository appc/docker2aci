#!/bin/sh
set -e
set -x

test ! -e layer1-file1
test   -e layer1-file2
test   -e layer1-file3

test   -e layer2-file1
test ! -e layer2-file2
test   -e layer2-file3

test   -e layer3-file1
test   -e layer3-file2
test ! -e layer3-file3

grep -q layer1 layer1-file2
grep -q layer1 layer1-file3

grep -q layer2 layer2-file1
grep -q layer2 layer2-file3

grep -q layer3 layer3-file1
grep -q layer3 layer3-file2

grep -q NEW layer4-file1
grep -q NEW layer4-file2
grep -q NEW layer4-file3

grep -q line1 layer5-file1
grep -q line1 layer5-file2
grep -q line1 layer5-file3

# # Docker with AUFS storage backend does not handle this test correctly and
# # Semaphore uses AUFS
if [ "$DOCKER_STORAGE_BACKEND" != aufs ] ; then
	grep -q line2 layer5-file1
	grep -q line2 layer5-file2
	grep -q line2 layer5-file3
	cmp layer5-file1 layer5-file2
	cmp layer5-file1 layer5-file3
fi

echo "SUCCESS"
