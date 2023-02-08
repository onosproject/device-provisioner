package configuration

import (
	"context"
	"github.com/gogo/protobuf/proto"
	"github.com/onosproject/device-provisioner/pkg/southbound"
	configstore "github.com/onosproject/device-provisioner/pkg/store/config"
	"github.com/onosproject/device-provisioner/pkg/store/topo"
	"github.com/onosproject/onos-api/go/onos/provisioner"
	topoapi "github.com/onosproject/onos-api/go/onos/topo"
	"github.com/onosproject/onos-lib-go/pkg/controller"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-net-lib/pkg/p4rtclient"
	"github.com/onosproject/onos-net-lib/pkg/p4utils"
	p4info "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/protobuf/encoding/prototext"
	"time"
)

var log = logging.GetLogger()

const (
	defaultTimeout      = 30 * time.Second
	provisionerRoleName = "provisioner"
)

// NewController returns a new gNMI connection  controller
func NewController(topo topo.Store, conns p4rtclient.ConnManager, configStore configstore.ConfigStore, realmLabel string, realmValue string) *controller.Controller {
	c := controller.NewController("configuration")
	c.Watch(&TopoWatcher{
		topo:       topo,
		realmValue: realmValue,
		realmLabel: realmLabel,
	})

	c.Reconcile(&Reconciler{
		topo:        topo,
		conns:       conns,
		configStore: configStore,
	})
	return c
}

// Reconciler reconciles pipeline and chassis configuration
type Reconciler struct {
	conns       p4rtclient.ConnManager
	topo        topo.Store
	configStore configstore.ConfigStore
}

func (r *Reconciler) Reconcile(id controller.ID) (controller.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	targetID := id.Value.(topoapi.ID)
	log.Infof("Reconciling Device Pipeline config for Target '%s'", targetID)
	target, err := r.topo.Get(ctx, targetID)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Errorf("Failed reconciling Target '%s'", targetID, err)
			return controller.Result{}, err
		}
		return controller.Result{}, nil
	}

	deviceConfigAspect := &provisioner.DeviceConfig{}
	if err := target.GetAspect(deviceConfigAspect); err != nil {
		return controller.Result{}, nil
	}

	if len(deviceConfigAspect.PipelineConfigID) > 0 {
		err = r.reconcilePipelineConfiguration(ctx, target, deviceConfigAspect)
		if err != nil {
			return controller.Result{}, err
		}
	}
	if len(deviceConfigAspect.ChassisConfigID) > 0 {
		err = r.reconcileChassisConfiguration(ctx, target, deviceConfigAspect)
		if err != nil {
			return controller.Result{}, err
		}
	}
	return controller.Result{}, nil
}

// Runs pipeline configuration reconciliation logic
func (r *Reconciler) reconcilePipelineConfiguration(ctx context.Context, object *topoapi.Object, deviceConfig *provisioner.DeviceConfig) error {
	log.Infof("Reconciling pipeline configuration for %s...", object.ID)

	// If the DeviceConfig matches PipelineConfigState and cookie is not 0, we're done.
	pcState := &provisioner.PipelineConfigState{}
	if err := object.GetAspect(pcState); err == nil {
		if pcState.ConfigID == deviceConfig.PipelineConfigID && pcState.Cookie > 0 {
			log.Infof("Pipeline configuration is up-to-date for %s", object.ID)
			return nil
		}
	}

	// Otherwise... get the pipeline config artifacts
	artifacts, err := r.getArtifacts(deviceConfig.PipelineConfigID, 2)
	if err != nil {
		return err
	}

	pcState.Cookie, err = r.setPipelineConfig(ctx, object, artifacts[provisioner.P4InfoType], artifacts[provisioner.P4BinaryType], pcState.Cookie)
	if err != nil {
		log.Warnf("Unable to reconcile Stratum P4Runtime pipeline config for %s: %+v", object.ID, err)
		return err
	}

	// Update PipelineConfigState aspect
	pcState.ConfigID = deviceConfig.PipelineConfigID
	pcState.Updated = time.Now()
	err = r.updateObjectAspect(ctx, object, "pipeline", pcState)
	if err != nil {
		return err
	}
	return nil
}

// setPipelineConfig makes sure that the device has the given P4 pipeline configuration applied
func (r *Reconciler) setPipelineConfig(ctx context.Context, object *topoapi.Object, info []byte, binary []byte, cookie uint64) (uint64, error) {
	stratumAgents := &topoapi.StratumAgents{}
	if err := object.GetAspect(stratumAgents); err != nil {
		log.Warnw("Failed to extract stratum agents aspect", "error", err)
		return 0, err
	}

	// Connect to device using P4Runtime
	dest := &p4rtclient.Destination{
		TargetID: object.ID,
		Endpoint: stratumAgents.P4RTEndpoint,
		DeviceID: stratumAgents.DeviceID,
		RoleName: provisionerRoleName,
	}

	p4rtConn, err := r.conns.Connect(ctx, dest)
	if err != nil {
		log.Warnw("Cannot connect to dest", "dest", dest, "error", err)
		return 0, err
	}

	role := p4utils.NewStratumRole(dest.RoleName, 0, []byte{}, false, true)
	arbitrationResponse, err := p4rtConn.PerformMasterArbitration(role)
	if err != nil {
		log.Warnw("Failed to perform master arbitration", "error", err)
		return 0, err
	}
	electionID := arbitrationResponse.Arbitration.ElectionId
	// ask for the pipeline config cookie
	gr, err := p4rtConn.GetForwardingPipelineConfig(ctx, &p4api.GetForwardingPipelineConfigRequest{
		DeviceId:     dest.DeviceID,
		ResponseType: p4api.GetForwardingPipelineConfigRequest_COOKIE_ONLY,
	})
	if err != nil {
		log.Warnw("Failed to retrieve pipeline configuration", "error", err)
		return 0, err
	}

	// if that matches our cookie, we're good
	if cookie == gr.Config.Cookie.Cookie && cookie > 0 {
		return cookie, nil
	}

	// otherwise unmarshal the P4Info
	p4i := &p4info.P4Info{}
	if err = prototext.Unmarshal(info, p4i); err != nil {
		log.Warnw("Failed to unmarshal p4info", "error", err)
		return 0, err
	}

	// and then apply it to the device
	newCookie := uint64(time.Now().UnixNano())
	_, err = p4rtConn.SetForwardingPipelineConfig(ctx, &p4api.SetForwardingPipelineConfigRequest{
		DeviceId:   dest.DeviceID,
		Role:       dest.RoleName,
		ElectionId: electionID,
		Action:     p4api.SetForwardingPipelineConfigRequest_VERIFY_AND_COMMIT,
		Config: &p4api.ForwardingPipelineConfig{
			P4Info:         p4i,
			P4DeviceConfig: binary,
			Cookie:         &p4api.ForwardingPipelineConfig_Cookie{Cookie: newCookie},
		},
	})
	if err != nil {
		return 0, err
	}
	log.Infof("%s: pipeline configured with cookie %d", dest.TargetID, newCookie)
	return newCookie, nil
}

// Runs chassis configuration reconciliation logic
func (r *Reconciler) reconcileChassisConfiguration(ctx context.Context, object *topoapi.Object, dcfg *provisioner.DeviceConfig) error {
	log.Infof("Reconciling chassis configuration for %s...", object.ID)

	// If the DeviceConfig matches ChassisConfigState, we're done
	ccState := &provisioner.ChassisConfigState{}
	if err := object.GetAspect(ccState); err == nil {
		if ccState.ConfigID == dcfg.ChassisConfigID {
			log.Infof("Chassis configuration is up-to-date for %s", object.ID)
			return nil
		}
	}

	// Otherwise... get chassis configuration artifact
	artifacts, err := r.getArtifacts(dcfg.ChassisConfigID, 1)
	if err != nil {
		return err
	}

	// ... and apply the chassis configuration to the device using gNMI
	err = southbound.SetChassisConfig(object, artifacts[provisioner.ChassisType])
	if err != nil {
		log.Warnf("Unable to apply Stratum gNMI chassis config for %s: %+v", object.ID, err)
		return err
	}

	// Update ChassisConfigState aspect
	ccState.ConfigID = dcfg.ChassisConfigID
	ccState.Updated = time.Now()
	err = r.updateObjectAspect(ctx, object, "chassis", ccState)
	if err != nil {
		return err
	}
	return nil
}

// Retrieves the required configuration artifacts from the store
func (r *Reconciler) getArtifacts(configID provisioner.ConfigID, expectedNumber int) (configstore.Artifacts, error) {
	record, err := r.configStore.Get(context.Background(), configID)
	if err != nil {
		log.Warnf("Unable to retrieve pipeline configuration for %s: %+v", configID, err)
		return nil, err
	}

	// ... and the associated artifacts
	artifacts, err := r.configStore.GetArtifacts(context.Background(), record)
	if err != nil {
		log.Warnf("Unable to retrieve pipeline config artifacts for %s: %+v", configID, err)
		return nil, err
	}

	// Make sure the number of artifacts is sufficient
	if len(artifacts) < expectedNumber {
		log.Warnf("Insufficient number of config artifacts found: %d", len(artifacts))
		return nil, errors.NewInvalid("Insufficient number of config artifacts found")
	}
	return artifacts, err
}

// Update the topo object with the specified configuration aspect
func (r *Reconciler) updateObjectAspect(ctx context.Context, object *topoapi.Object, kind string, aspect proto.Message) error {
	log.Infof("Updating %s configuration for %s", kind, object.ID)
	entity, err := r.topo.Get(ctx, object.ID)
	if err != nil {
		log.Warnf("Unable to get object %s: %+v", object.ID, err)
		return err
	}

	if err = entity.SetAspect(aspect); err != nil {
		log.Warnf("Unable to set %s aspect for %s: %+v", kind, object.ID, err)
		return err
	}
	if err = r.topo.Update(ctx, entity); err != nil {
		log.Warnf("Unable to update %s configuration for object %s: %+v", kind, object.ID, err)
		return err
	}
	return nil
}
