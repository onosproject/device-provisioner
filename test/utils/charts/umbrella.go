// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package charts ...
package charts

import (
	"github.com/onosproject/helmit/pkg/helm"
	"github.com/onosproject/onos-test/pkg/onostest"
)

// CreateUmbrellaRelease creates a helm release for an onos-umbrella instance
func CreateUmbrellaRelease() *helm.HelmRelease {
	return helm.Chart("onos-umbrella", onostest.OnosChartRepo).
		Release("onos-umbrella").
		Set("onos-topo.image.tag", "latest").
		Set("import.onos-config.enabled", false).
		Set("import.onos-cli.enabled", false) // not needed - can be enabled by adding '--set onos-umbrella.import.onos-cli.enabled=true' to helmit args for investigations

}
