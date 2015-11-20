#!/bin/bash -e

ORG_PATH="github.com/appc"
REPO_PATH="${ORG_PATH}/docker2aci"

if [ ! -h gopath/src/${REPO_PATH} ]; then
	mkdir -p gopath/src/${ORG_PATH}
	ln -s ../../../.. gopath/src/${REPO_PATH} || exit 255
fi

export GOBIN=${PWD}/bin
export GOPATH=${PWD}/gopath

eval $(go env)

echo "Fetching dependencies..."
go get -d -v ./...
echo "Building docker2aci..."
go build -o $GOBIN/docker2aci ${REPO_PATH}/

