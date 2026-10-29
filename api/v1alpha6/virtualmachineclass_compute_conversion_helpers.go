// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

import (
	"fmt"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"math"
)

type classComputeBoolField struct {
	path   string
	policy **BoolPolicy
}

func initClassCompute(spec *VirtualMachineClassSpec) {
	if spec.CPUAdvanced == nil {
		spec.CPUAdvanced = &VirtualMachineClassCPUAdvancedSpec{}
	}
	if spec.MemoryAdvanced == nil {
		spec.MemoryAdvanced = &VirtualMachineClassMemoryAdvancedSpec{}
	}
	if spec.CPUAdvanced.Topology == nil {
		spec.CPUAdvanced.Topology = &VirtualMachineClassCPUTopologySpec{}
	}
}

func trimClassCompute(spec *VirtualMachineClassSpec) {
	if spec.CPUAdvanced != nil {
		if spec.CPUAdvanced.Topology != nil && apiequality.Semantic.DeepEqual(*spec.CPUAdvanced.Topology, VirtualMachineClassCPUTopologySpec{}) {
			spec.CPUAdvanced.Topology = nil
		}
		if apiequality.Semantic.DeepEqual(*spec.CPUAdvanced, VirtualMachineClassCPUAdvancedSpec{}) {
			spec.CPUAdvanced = nil
		}
	}
	if spec.MemoryAdvanced != nil && apiequality.Semantic.DeepEqual(*spec.MemoryAdvanced, VirtualMachineClassMemoryAdvancedSpec{}) {
		spec.MemoryAdvanced = nil
	}
}

func classComputeBoolFields(spec *VirtualMachineClassSpec) []classComputeBoolField {
	return []classComputeBoolField{
		{"cpuHotAddEnabled", &spec.CPUAdvanced.HotAddEnabled},
		{"cpuHotRemoveEnabled", &spec.CPUAdvanced.HotRemoveEnabled},
		{"flags.vvtdEnabled", &spec.CPUAdvanced.IOMMUEnabled},
		{"nestedHVEnabled", &spec.CPUAdvanced.NestedHardwareVirtualizationEnabled},
		{"vPMCEnabled", &spec.CPUAdvanced.PerformanceCountersEnabled},
		{"virtualNuma.exposeVnumaOnCpuHotadd", &spec.CPUAdvanced.ExposeVNUMAOnCPUHotAdd},
		{"memoryHotAddEnabled", &spec.MemoryAdvanced.HotAddEnabled},
		{"memoryReservationLockedToMax", &spec.MemoryAdvanced.ReservationLockedToMax},
	}
}

func promoteClassCompute(spec *VirtualMachineClassSpec, doc *ClassConfigSpecDocument) error {
	// Generated converters do not clear fields absent from the old API when
	// the destination is reused. Start this category from its old-wire values.
	spec.CPUAdvanced, spec.MemoryAdvanced = nil, nil
	initClassCompute(spec)
	defer trimClassCompute(spec)
	for _, field := range classComputeBoolFields(spec) {
		var value bool
		if ok, err := doc.Get(field.path, &value); err == nil && ok {
			*field.policy = FixedClassBool(value)
			if err := doc.Delete(field.path); err != nil {
				return err
			}
		}
	}
	if err := promoteClassCPUTopology(spec, doc); err != nil {
		return err
	}
	return promoteClassLatency(spec, doc)
}

func promoteClassCPUTopology(spec *VirtualMachineClassSpec, doc *ClassConfigSpecDocument) error {
	var cores int32
	if ok, err := doc.Get("numCoresPerSocket", &cores); err == nil && ok && cores >= 0 {
		spec.CPUAdvanced.Topology.CoresPerSocket = FixedClassInt(cores)
		if err := doc.Delete("numCoresPerSocket"); err != nil {
			return err
		}
	}
	var coresPerNode int32
	if ok, err := doc.Get("virtualNuma.coresPerNumaNode", &coresPerNode); err == nil && ok {
		cpu := ClassResourceDefault(spec.Resources.Size.CPU)
		cpuCount, cpuWhole := cpu.AsInt64()
		var count *int32
		if coresPerNode == 0 {
			zero := int32(0)
			count = &zero
		} else if coresPerNode > 0 && cpuWhole && cpuCount > 0 && cpuCount <= math.MaxInt32 && cpuCount%int64(coresPerNode) == 0 && cores > 0 && (coresPerNode%cores == 0 || cores%coresPerNode == 0) {
			quotient := cpuCount / int64(coresPerNode)
			if quotient > 0 && quotient <= math.MaxInt32 {
				n := int32(quotient)
				count = &n
			}
		}
		if count != nil {
			spec.CPUAdvanced.Topology.VNUMANodeCount = FixedClassInt(*count)
			if err := doc.Delete("virtualNuma.coresPerNumaNode"); err != nil {
				return err
			}
		}
	}
	return nil
}

func promoteClassLatency(spec *VirtualMachineClassSpec, doc *ClassConfigSpecDocument) error {
	var level string
	if ok, err := doc.Get("latencySensitivity.level", &level); err == nil && ok {
		var selected string
		var threads int32
		threadsSet, err := doc.Get("simultaneousThreads", &threads)
		// Invalid raw leaf types are not representable presets; keep them raw.
		switch {
		case err == nil && level == "normal" && (!threadsSet || threads == 1):
			selected = string(VirtualMachineLatencySensitivityNormal)
		case err == nil && level == "high" && (!threadsSet || threads == 1):
			selected = string(VirtualMachineLatencySensitivityHigh)
		case err == nil && level == "high" && threadsSet && threads == 2:
			selected = string(VirtualMachineLatencySensitivityHighWithHyperthreading)
			if err := doc.Delete("simultaneousThreads"); err != nil {
				return err
			}
		}

		if selected != "" {
			allowed := []string{selected}
			required := true
			spec.CPUAdvanced.LatencySensitivity = &EnumPolicy{Default: &selected, Allowed: &allowed, Required: &required}
			if err := doc.Delete("latencySensitivity.level"); err != nil {
				return err
			}
		}
	}
	return nil
}

func projectClassCompute(spec *VirtualMachineClassSpec, doc *ClassConfigSpecDocument) error {
	projected := spec.DeepCopy()
	initClassCompute(projected)
	for _, field := range classComputeBoolFields(projected) {
		if value := ClassBoolDefault(*field.policy); value != nil {
			if err := doc.Set(field.path, *value); err != nil {
				return err
			}
		}
	}
	if err := projectClassCPUTopology(projected, doc); err != nil {
		return err
	}
	return projectClassLatency(projected, doc)
}

func projectClassCPUTopology(spec *VirtualMachineClassSpec, doc *ClassConfigSpecDocument) error {
	if value := ClassIntDefault(spec.CPUAdvanced.Topology.CoresPerSocket); value != nil {
		if err := doc.Set("numCoresPerSocket", *value); err != nil {
			return err
		}
	}
	if value := ClassIntDefault(spec.CPUAdvanced.Topology.VNUMANodeCount); value != nil {
		var cores int32
		if *value == 0 {
			cores = 0
		} else {
			cpu := ClassResourceDefault(spec.Resources.Size.CPU)
			cpuCount, cpuWhole := cpu.AsInt64()
			if *value < 0 || !cpuWhole || cpuCount <= 0 || cpuCount > math.MaxInt32 || cpuCount%int64(*value) != 0 {
				return fmt.Errorf("class vnumaNodeCount requires a divisible CPU size default")
			}
			quotient := cpuCount / int64(*value)
			if quotient < 0 || quotient > math.MaxInt32 {
				return fmt.Errorf("class vNUMA cores per node exceeds int32")
			}
			cores = int32(quotient)
			cps := ClassIntDefault(spec.CPUAdvanced.Topology.CoresPerSocket)
			if cps == nil || *cps <= 0 || (cores%*cps != 0 && *cps%cores != 0) {
				return fmt.Errorf("class vnumaNodeCount requires compatible coresPerSocket default")
			}
		}
		if err := doc.Set("virtualNuma.coresPerNumaNode", cores); err != nil {
			return err
		}
	}
	return nil
}

func projectClassLatency(spec *VirtualMachineClassSpec, doc *ClassConfigSpecDocument) error {
	if value := ClassEnumDefault(spec.CPUAdvanced.LatencySensitivity); value != nil {
		var level string
		switch VirtualMachineLatencySensitivityLevel(*value) {
		case VirtualMachineLatencySensitivityNormal:
			level = "normal"
		case VirtualMachineLatencySensitivityHigh:
			level = "high"
		case VirtualMachineLatencySensitivityHighWithHyperthreading:
			level = "high"
			if err := doc.Set("simultaneousThreads", int32(2)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported class latencySensitivity %q", *value)
		}
		if VirtualMachineLatencySensitivityLevel(*value) != VirtualMachineLatencySensitivityHighWithHyperthreading {
			var threads int32
			if ok, err := doc.Get("simultaneousThreads", &threads); err != nil {
				return err
			} else if ok && threads != 1 {
				if err := doc.Set("simultaneousThreads", int32(1)); err != nil {
					return err
				}
			}
		}
		if err := doc.Set("latencySensitivity.level", level); err != nil {
			return err
		}
	}
	return nil
}

func restoreClassCompute(dst, preserved, projected *VirtualMachineClassSpec) {
	initClassCompute(dst)
	p, j := preserved.DeepCopy(), projected.DeepCopy()
	initClassCompute(p)
	initClassCompute(j)
	dstFields, pFields, jFields := classComputeBoolFields(dst), classComputeBoolFields(p), classComputeBoolFields(j)
	for i := range dstFields {
		restoreUnchangedClassPolicy(dstFields[i].policy, *pFields[i].policy, *jFields[i].policy)
	}
	restoreUnchangedClassPolicy(&dst.CPUAdvanced.LatencySensitivity, p.CPUAdvanced.LatencySensitivity, j.CPUAdvanced.LatencySensitivity)
	restoreUnchangedClassPolicy(&dst.CPUAdvanced.Topology.CoresPerSocket, p.CPUAdvanced.Topology.CoresPerSocket, j.CPUAdvanced.Topology.CoresPerSocket)
	restoreUnchangedClassPolicy(&dst.CPUAdvanced.Topology.VNUMANodeCount, p.CPUAdvanced.Topology.VNUMANodeCount, j.CPUAdvanced.Topology.VNUMANodeCount)
	trimClassCompute(dst)
	if dst.CPUAdvanced != nil {
		var pTopology, jTopology *VirtualMachineClassCPUTopologySpec
		if preserved.CPUAdvanced != nil {
			pTopology = preserved.CPUAdvanced.Topology
		}
		if projected.CPUAdvanced != nil {
			jTopology = projected.CPUAdvanced.Topology
		}
		restoreUnchangedClassPolicy(&dst.CPUAdvanced.Topology, pTopology, jTopology)
	}
	restoreUnchangedClassPolicy(&dst.CPUAdvanced, preserved.CPUAdvanced, projected.CPUAdvanced)
	restoreUnchangedClassPolicy(&dst.MemoryAdvanced, preserved.MemoryAdvanced, projected.MemoryAdvanced)
}
