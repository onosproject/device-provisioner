// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package main launches the integration tests
package main

import (
	"github.com/onosproject/device-provisioner/test/basic"
	"github.com/onosproject/helmit/pkg/registry"
	"github.com/onosproject/helmit/pkg/test"
)

func main() {
	registry.RegisterTestSuite("basic", &basic.TestSuite{})
	test.Main()
}
