![Build](https://github.com/onosproject/device-provisioner/workflows/build/badge.svg)
![Coverage](https://img.shields.io/badge/Coverage-54.5%25-yellow)


<!--
SPDX-FileCopyrightText: 2022 Intel Corporation

SPDX-License-Identifier: Apache-2.0
-->


# Device-provisioner
The main objective of this project is to provide an SDN application for provisioning of 
device pipeline config automatically using [P4Runtime][P4Runtime] and [gNMI][gNMI]  as the devices in a network get discovered/provisioned using 
[onos-topo] subsystem. 


## Architecture
The application uses [onos-p4-sdk][onos-p4-sdk], [Atomix][Atomix], and [onos-p4-plugins][onos-p4-plugins] to 
implement a reconciliation loop controller that
brings the actual state device pipeline config to a desired state for each device. Each device is defined as a programmable entity 
in [onos-topo][onos-topo] and provides information about the pipelines for each device. The following Figure shows the 
interactions of the app with micro-onos subsystems and data plane. 

![design](docs/images/arch.png)


### Device Pipeline Configuration Using P4Runtime API
The application uses P4Runtime *SetForwardingPipelineConfig* RPC to set/update pipelines in a P4 programmable device. At very high level, the user specifies
the pipelines and type of configuration action for each pipeline (e.g.  VERIFY, VERIFY_AND_COMMIT, RECONCILE_AND_COMMIT, etc)
when the device entity is created in [onos-topo][onos-topo]. The pipeline config 
reconciliation loop controller listens to topology changes and tries to reconcile the pipeline config  and reach to the desired state
based on given information in topo.
Furthermore, the application also uses Atomix primitives (a map) for keeping the internal device pipeline config state per target.

### Usage of P4 Plugins 
Device provisioner uses [onos-p4-plugins][onos-p4-plugins] to implement a P4 program agnostic device pipeline 
provisioning mechanism. Each P4 plugin provides an interface that allows the application to get access to the required information for 
provisioning a device pipeline such as P4Info and P4 device config. Device provisioner gets pipeline information from
onos-topo that user provides  (i.e. name, version, architecture of a P4 program). The applications uses pipeline info to
find appropriate P4 plugin using a plugin registry mechanism in the app. 

# Getting Started 
## Deployment 

**Prerequisites**: a Running kubernetes cluster, kubectl and helm installed. 

To deploy the app and all required subsystems, the following steps should be followed :

1) First we should add onosproject and Atomix helm repos and deploy Atomix controllers and onos operators 
that are required for deploying micro-onos subsystems and the application.

```bash
helm repo add atomix https://charts.atomix.io
helm repo add onos https://charts.onosproject.org
helm repo update

helm install atomix-controller atomix/atomix-controller -n kube-system --wait
helm install atomix-raft-storage atomix/atomix-raft-storage -n kube-system  --wait
helm install -n kube-system atomix-runtime-controller atomix/atomix-runtime-controller --wait
helm install -n kube-system atomix-multi-raft-controller atomix/atomix-multi-raft-controller --set node.image.pullPolicy=Always --wait
helm install onos-operator onos/onos-operator -n kube-system --wait 

```

2) Use the following commands to deploy the device-provisioner app and required micro-onos components using onos umbrella chart

```bash
kubect create ns micro-onos
helm -n micro-onos install micro-onos  onos/onos-umbrella --set import.device-provisioner.enabled=true
```

if you deploy all of required subsystems successfully, you should be able to see the following pods using kubectl:

```bash
kubectl get pods -n micro-onos
NAME                                  READY   STATUS    RESTARTS   AGE
device-provisioner-59f4657b46-85n4z   2/2     Running   0          2m14s
device-provisioner-store-0            1/1     Running   0          2m14s
onos-cli-6d8b5b5d7d-s277n             1/1     Running   0          2m14s
onos-config-b54c89548-lfj58           6/6     Running   0          2m14s
onos-consensus-store-0                1/1     Running   0          2m14s
onos-topo-6c966588c8-qgnj7            3/3     Running   0          2m14s
```


> **Note**
> onos-cli can be used to verify the first interactions of the application with onos-topo subsystem.
> When the app deployment becomes ready, it creates a CONTROLLER entity 
> in onos-topo to represent itself as a control plan entity. 



```bash
$ kubectl exec -it -n micro-onos deployments/onos-cli -- /bin/bash
$ onos topo get entities --kind controller
Entity ID                                  Kind ID      Labels   Aspects
p4rt:device-provisioner-59f4657b46-85n4z   controller   <None>   onos.topo.Lease,onos.topo.ControllerInfo
```

## How to Use/Test the device-provisioner application?
For testing purposes, developers can use [stratum-simulator][stratum-simulator] chart which deploys
[stratum mininet enabled docker image][stratum-image]. 

1) First, deploy the stratum-simulator chart using the following command:

```bash
helm install stratum-simulator  -n micro-onos onos/stratum-simulator
```

> **Note**
> stratum-simulator helm chart creates a linear topology with 2 switches by default. Each switch exposes a different gRPC port, 
> starting from 50001 and increasing.

2) Switch entities should be created in onos-topo that allows the application to make a connection to the P4Runtime server
running on each device. To do so, onos-topo-operator can be used to create those entities in onos-topo
To create two switch entities in onos-topo:
 - Create the following yaml file and name it topo.yaml
 - Apply it in the namespace where the app deployed using kubectl or helm.

```yaml
apiVersion: topo.onosproject.org/v1beta1
kind: Entity
metadata:
  name: p4rt.s1
spec:
  uri: p4rt:s1 # protocol:switch_id
  kind:
    name: switch
  aspects:
    onos.topo.Switch:
      model_id: "test"
      role: "role-test"
    onos.topo.P4RTServerInfo:
      control_endpoint:
        address: stratum-simulator
        port: 50001
      device_id: 1
      pipelines:
        - name: "middleblock"
          version: "1.0.0"
          architecture: "v1model"
          configuration_action: "VERIFY_AND_COMMIT"
    onos.topo.TLSOptions:
      plain: true
      insecure: true
---
apiVersion: topo.onosproject.org/v1beta1
kind: Entity
metadata:
  name: p4rt.s2
spec:
  uri: p4rt:s2 # protocol:switch_id
  kind:
    name: switch
  aspects:
    onos.topo.Switch:
      model_id: "test"
      role: "role-test"
    onos.topo.P4RTServerInfo:
      control_endpoint:
        address: stratum-simulator
        port: 50002
      device_id: 1
      pipelines:
        - name: "middleblock"
          version: "1.0.0"
          architecture: "v1model"
          configuration_action: "VERIFY_AND_COMMIT"
    onos.topo.TLSOptions:
      plain: true
      insecure: true

```


```bash
$ kubectl apply -f topo.yaml -n micro-onos
entity.topo.onosproject.org/p4rt.s1 created
entity.topo.onosproject.org/p4rt.s2 created
```

when the entities created, you can check new entities and relations between them

To see the new switch entities, use onos-cli and run the following command:

```bash
$ onos topo get entity
Entity ID                                 Kind ID       Labels   Aspects
p4rt:s2                                   switch        <None>   onos.topo.Switch,onos.topo.MastershipState,onos.topo.TLSOptions,onos.topo.P4RTServerInfo
p4rt:s1                                   switch        <None>   onos.topo.MastershipState,onos.topo.Switch,onos.topo.TLSOptions,onos.topo.P4RTServerInfo
service:device-provisioner/p4rt:s1        service       <None>   onos.topo.Service
service:device-provisioner/p4rt:s2        service       <None>   onos.topo.Service
gnmi:onos-config-5b8574d546-wqtj4         onos-config   <None>   onos.topo.Lease
app:device-provisioner-695c7cc458-fwwmm   controller    <None>   onos.topo.ControllerInfo,onos.topo.Lease
````

To see the new relations that are created by the app, run the following command.
```bash
$ onos topo get relations
Relation ID                                  Kind ID      Source ID                                 Target ID                            Labels   Aspects
uuid:7c65fda2-971e-4a1a-859a-5311de5985e7    connection   app:device-provisioner-695c7cc458-fwwmm   service:device-provisioner/p4rt:s1   <None>   <None>
service:device-provisioner/p4rt:s2:p4rt:s2   controls     service:device-provisioner/p4rt:s2        p4rt:s2                              <None>   <None>
service:device-provisioner/p4rt:s1:p4rt:s1   controls     service:device-provisioner/p4rt:s1        p4rt:s1                              <None>   <None>
uuid:ec3b5e9c-f403-4493-a33f-abbcea9e94a4    connection   app:device-provisioner-695c7cc458-fwwmm   service:device-provisioner/p4rt:s2   <None>   <None>
```

> **Note**: 
> CONTROLLER entity: each app instance creates a controller entity to represent itself in onos-topo
> SERVICE entity: this is an intermediate  entity which connects a controller entity to a data plane entity (e.g. SWITCH) and allows each app to store mastership state for its role. 
> CONNECTION relations represents each connection from the app to the target. 
> CONTROL relation is a connection between the SERVICE entity and target.

To verify the devices are configured successfully, you can check device-provisioner app logs. We are planning to provide
CLI that you can check the state of pipeline config per target. 
Here is the sample output in the logs

```bash
kubectl logs -n micro-onos deployments/device-provisioner device-provisioner --follow | grep "Device pipelineConfig is completed successfully"
2022-08-17T23:45:31.719Z	INFO	github.com/onosproject/device-provisioner/pkg/controller/pipeline	pipeline/controller.go:289	Device pipelineConfig is completed successfully	{"pipelineConfig ID": "p4rt:s1-middleblock-1.0.0-v1model", "targetID": "p4rt:s1", "Action": "VERIFY_AND_COMMIT"}
2022-08-17T23:45:31.719Z	INFO	github.com/onosproject/device-provisioner/pkg/controller/pipeline	pipeline/controller.go:289	Device pipelineConfig is completed successfully	{"pipelineConfig ID": "p4rt:s2-middleblock-1.0.0-v1model", "targetID": "p4rt:s2", "Action": "VERIFY_AND_COMMIT"}
```


[onos-p4-sdk]: https://github.com/onosproject/onos-p4-sdk
[Atomix]: https://github.com/atomix
[P4Runtime]: https://p4.org/p4-spec/p4runtime/main/P4Runtime-Spec.html
[onos-topo]: https://github.com/onosproject/onos-topo
[onos-p4-plugins]: https://github.com/onosproject/onos-p4-plugins
[helm]: https://helm.sh/
[stratum-simulator]: https://github.com/onosproject/onos-helm-charts/tree/master/stratum-simulator
[stratum-image]: https://hub.docker.com/r/opennetworking/mn-stratum
[gNMI]: https://github.com/openconfig/reference/blob/master/rpc/gnmi/gnmi-specification.md