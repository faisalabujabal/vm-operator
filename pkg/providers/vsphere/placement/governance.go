// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package placement

import (
	"context"
	"fmt"
	"math"

	"github.com/vmware/govmomi/vim25"
	vimtypes "github.com/vmware/govmomi/vim25/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	"github.com/vmware-tanzu/vm-operator/pkg/governance"
	"github.com/vmware-tanzu/vm-operator/pkg/providers/vsphere/vcenter"
	vmopv1util "github.com/vmware-tanzu/vm-operator/pkg/util/vmopv1"
)

func filterGovernanceCandidates(ctx context.Context, reader ctrlclient.Reader, vcClient *vim25.Client, vm *vmopv1.VirtualMachine, config vimtypes.VirtualMachineConfigSpec, candidates map[string][]string) (map[string][]string, error) {
	if !pkgcfg.FromContext(ctx).Features.VMServiceClasslessVM {
		return candidates, nil
	}
	if vm == nil {
		return nil, fmt.Errorf("VM configuration is required for governance placement")
	}
	effective := vm.DeepCopy()
	// The placement ConfigSpec includes resolved image/class resource values.
	// Do not modify the VM or choose governing defaults while filtering zones.
	placementResourceValues(effective, config)
	needFrequency := effective.Spec.Resources != nil && ((effective.Spec.Resources.Requests != nil && effective.Spec.Resources.Requests.CPU != nil) || (effective.Spec.Resources.Limits != nil && effective.Spec.Resources.Limits.CPU != nil))
	frequencyForPool := func(pool string) (int64, error) {
		if !needFrequency {
			return 0, nil
		}
		cluster, err := rpMoIDToCluster(ctx, vcClient, vimtypes.ManagedObjectReference{Type: "ResourcePool", Value: pool})
		if err != nil {
			return 0, err
		}
		mhz, err := vcenter.ClusterMinCPUFreq(ctx, cluster)
		if err != nil {
			return 0, err
		}
		if mhz == 0 || mhz > math.MaxInt64 {
			return 0, fmt.Errorf("cluster CPU frequency is unavailable")
		}
		return int64(mhz), nil
	}
	return filterGovernancePools(ctx, reader, effective, candidates, frequencyForPool)
}

func filterGovernancePools(ctx context.Context, reader ctrlclient.Reader, vm *vmopv1.VirtualMachine, candidates map[string][]string, frequencyForPool func(string) (int64, error)) (map[string][]string, error) {
	selected := vm.Status.Zone
	if selected == "" {
		selected = vm.Labels[corev1.LabelTopologyZone]
	}
	allowed := map[string][]string{}
	frequencies := map[string]int64{}
	for zone, pools := range candidates {
		if selected != "" && zone != selected {
			continue
		}
		for _, pool := range pools {
			frequency, err := frequencyForPool(pool)
			if err != nil {
				return nil, fmt.Errorf("resolve zone CPU frequency: %w", err)
			}
			result, err := governance.ResolveZonesWithFrequency(ctx, reader, vm, []string{zone}, map[string]int64{zone: frequency})
			if err != nil {
				return nil, err
			}
			if len(result.Zones) > 0 {
				allowed[zone] = append(allowed[zone], pool)
				frequencies[zone] = frequency
			}
		}
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("no zone satisfies the VM configuration: %w", ErrNoPlacementCandidates)
	}
	names := make([]string, 0, len(allowed))
	for zone := range allowed {
		names = append(names, zone)
	}
	result, err := governance.ResolveZonesWithFrequency(ctx, reader, vm, names, frequencies)
	if err == nil {
		err = result.Error()
	}
	if err != nil {
		return nil, err
	}
	return allowed, nil
}

// The resize synchronization helper deliberately clears zero reservations and
// unlimited limits. Placement must retain them as concrete values, not confuse
// them with defaults that have not been resolved yet.
func placementResourceValues(vm *vmopv1.VirtualMachine, config vimtypes.VirtualMachineConfigSpec) {
	vmopv1util.SyncClassSizeAndAllocationToSpec(vm, config)
	for _, allocation := range []struct {
		value  *vimtypes.ResourceAllocationInfo
		memory bool
	}{{config.CpuAllocation, false}, {config.MemoryAllocation, true}} {
		if allocation.value == nil {
			continue
		}
		if vm.Spec.Resources == nil {
			vm.Spec.Resources = &vmopv1.VirtualMachineResourcesSpec{}
		}
		for _, field := range []struct {
			value  *int64
			target **vmopv1.VirtualMachineResourceQuantity
		}{{allocation.value.Reservation, &vm.Spec.Resources.Requests}, {allocation.value.Limit, &vm.Spec.Resources.Limits}} {
			if field.value == nil {
				continue
			}
			if *field.target == nil {
				*field.target = &vmopv1.VirtualMachineResourceQuantity{}
			}
			value := resource.NewQuantity(*field.value, resource.DecimalSI)
			if allocation.memory {
				if *field.value >= 0 {
					q := resource.MustParse(fmt.Sprintf("%dMi", *field.value))
					value = &q
				}
				(*field.target).Memory = value
			} else {
				(*field.target).CPU = value
			}
		}
	}
}
