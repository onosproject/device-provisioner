# SPDX-FileCopyrightText: 2023-present Intel Corporation
#
# SPDX-License-Identifier: Apache-2.0

SHELL = bash -e -o pipefail

export CGO_ENABLED=1
export GO111MODULE=on

.PHONY: basic

basic: # @HELP run helmit basic tests locally
	helmit test -n test ./device-provisioner-tests -c .. --suite basic --no-teardown

