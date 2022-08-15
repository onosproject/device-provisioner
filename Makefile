# SPDX-FileCopyrightText: 2022-present Intel Corporation
#
# SPDX-License-Identifier: Apache-2.0

SHELL = bash -e -o pipefail




export CGO_ENABLED=1
export GO111MODULE=on

.PHONY: build

DEVICE_PROVISIONER_APP_VERSION ?= latest

build-tools:=$(shell if [ ! -d "./build/build-tools" ]; then mkdir -p build && cd build && git clone https://github.com/onosproject/build-tools.git; fi)
include ./build/build-tools/make/onf-common.mk


mod-update: # @HELP Download the dependencies to the vendor folder
	go mod tidy
	go mod vendor

mod-lint: mod-update # @HELP ensure that the required dependencies are in place
	# dependencies are vendored, but not committed, go.sum is the only thing we need to check
	bash -c "diff -u <(echo -n) <(git diff go.sum)"


linters:
	golangci-lint run --timeout 15m || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b `go env GOPATH`/bin v1.48.0

build: # @HELP build the Go binaries and run all validations (default)
build: mod-update
	go build -mod=vendor -o build/_output/device-provisioner ./cmd/device-provisioner
test: # @HELP run the unit tests and source code validation producing a golang style report
test: mod-lint build linters license
	go test -race github.com/onosproject/device-provisioner/...


device-provisioner-app-docker:  # @HELP build device-provisioner base Docker image
	docker build --platform linux/amd64 . -f build/device-provisioner/Dockerfile \
		-t ${DOCKER_REPOSITORY}device-provisioner:${DEVICE_PROVISIONER_APP_VERSION}

images: # @HELP build all Docker images
images: device-provisioner-app-docker


publish: images
	docker push onosproject/device-provisioner:latest

ifdef TAG
	docker tag onosproject/device-provisioner:latest onosproject/device-provisioner:$(TAG)
	docker push onosproject/device-provisioner:$(TAG)
endif



kind: # @HELP build Docker images and add them to the currently configured kind cluster
kind: images kind-only

kind-only: # @HELP deploy the image without rebuilding first
kind-only:
	@if [ "`kind get clusters`" = '' ]; then echo "no kind cluster found" && exit 1; fi
	kind load docker-image --name ${KIND_CLUSTER_NAME} ${DOCKER_REPOSITORY}device-provisioner:${DEVICE_PROVISIONER_APP_VERSION}


clean:: # @HELP remove all the build artifacts
	rm -rf ./build/_output ./vendor ./cmd/device-provisioner/device-provisioner ./cmd/onos/onos
	go clean -testcache github.com/onosproject/device-provisioner/...