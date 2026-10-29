// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

import (
	"fmt"
	"math"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
)

type classComputeBoolField struct {
	path string
	policy **BoolPolicy
}

func initClassCompute(spec *VirtualMachineClassSpec) {
	if spec.CPUAdvanced == nil { spec.CPUAdvanced = &VirtualMachineClassCPUAdvancedSpec{} }
	if spec.MemoryAdvanced == nil { spec.MemoryAdvanced = &VirtualMachineClassMemoryAdvancedSpec{} }
	if spec.CPUAdvanced.Topology == nil { spec.CPUAdvanced.Topology = &VirtualMachineClassCPUTopologySpec{} }
}

func trimClassCompute(spec *VirtualMachineClassSpec) {
	if spec.CPUAdvanced != nil {
		if spec.CPUAdvanced.Topology != nil && apiequality.Semantic.DeepEqual(*spec.CPUAdvanced.Topology, VirtualMachineClassCPUTopologySpec{}) { spec.CPUAdvanced.Topology = nil }
		if apiequality.Semantic.DeepEqual(*spec.CPUAdvanced, VirtualMachineClassCPUAdvancedSpec{}) { spec.CPUAdvanced = nil }
	}
	if spec.MemoryAdvanced != nil && apiequality.Semantic.DeepEqual(*spec.MemoryAdvanced, VirtualMachineClassMemoryAdvancedSpec{}) { spec.MemoryAdvanced = nil }
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
		if ok, err := doc.Get(field.path, &value); err != nil { return err } else if ok {
			*field.policy = FixedClassBool(value)
			if err := doc.Delete(field.path); err != nil { return err }
		}
	}
	var cores int32
	if ok, err := doc.Get("numCoresPerSocket", &cores); err != nil { return err } else if ok && cores >= 0 {
		spec.CPUAdvanced.Topology.CoresPerSocket = FixedClassInt(cores)
		if err := doc.Delete("numCoresPerSocket"); err != nil { return err }
	}
	var coresPerNode int32
	if ok, err := doc.Get("virtualNuma.coresPerNumaNode", &coresPerNode); err != nil { return err } else if ok {
		cpu := ClassResourceDefault(spec.Resources.Size.CPU)
		var count *int32
		if coresPerNode == 0 { zero := int32(0); count = &zero } else if coresPerNode > 0 && cpu.Value() > 0 && cpu.Value() <= math.MaxInt32 && cpu.Value()%int64(coresPerNode) == 0 && cores > 0 {
			n := int32(cpu.Value()/int64(coresPerNode)); count = &n
		}
		if count != nil {
			spec.CPUAdvanced.Topology.VNUMANodeCount = FixedClassInt(*count)
			if err := doc.Delete("virtualNuma.coresPerNumaNode"); err != nil { return err }
		}
	}
	var level string
	if ok, err := doc.Get("latencySensitivity.level", &level); err != nil { return err } else if ok {
		var selected string
		switch level {
		case "normal": selected = string(VirtualMachineLatencySensitivityNormal)
		case "high":
			selected = string(VirtualMachineLatencySensitivityHigh)
			var threads int32
			if ok, err := doc.Get("simultaneousThreads", &threads); err != nil { return err } else if ok && threads == 2 {
				selected = string(VirtualMachineLatencySensitivityHighWithHyperthreading)
				if err := doc.Delete("simultaneousThreads"); err != nil { return err }
			}
		}
		if selected != "" {
			allowed := []string{selected}; required := true
			spec.CPUAdvanced.LatencySensitivity = &EnumPolicy{Default: &selected, Allowed: &allowed, Required: &required}
			if err := doc.Delete("latencySensitivity.level"); err != nil { return err }
		}
	}
	return nil
}

func projectClassCompute(spec *VirtualMachineClassSpec, doc *ClassConfigSpecDocument) error {
	copy := spec.DeepCopy()
	initClassCompute(copy)
	for _, field := range classComputeBoolFields(copy) {
		if value := ClassBoolDefault(*field.policy); value != nil {
			if err := doc.Set(field.path, *value); err != nil { return err }
		}
	}
	if value := ClassIntDefault(copy.CPUAdvanced.Topology.CoresPerSocket); value != nil {
		if err := doc.Set("numCoresPerSocket", *value); err != nil { return err }
	}
	if value := ClassIntDefault(copy.CPUAdvanced.Topology.VNUMANodeCount); value != nil {
		var cores int32
		if *value == 0 { cores = 0 } else {
			cpu := ClassResourceDefault(spec.Resources.Size.CPU)
			if *value < 0 || cpu.Value() <= 0 || cpu.Value() > math.MaxInt32 || cpu.Value()%int64(*value) != 0 { return fmt.Errorf("class vnumaNodeCount requires a divisible CPU size default") }
			cores = int32(cpu.Value()/int64(*value))
		}
		if err := doc.Set("virtualNuma.coresPerNumaNode", cores); err != nil { return err }
	}
	if value := ClassEnumDefault(copy.CPUAdvanced.LatencySensitivity); value != nil {
		var level string
		switch VirtualMachineLatencySensitivityLevel(*value) {
		case VirtualMachineLatencySensitivityNormal: level = "normal"; if err := doc.Set("simultaneousThreads", int32(1)); err != nil { return err }
		case VirtualMachineLatencySensitivityHigh: level = "high"; if err := doc.Set("simultaneousThreads", int32(1)); err != nil { return err }
		case VirtualMachineLatencySensitivityHighWithHyperthreading:
			level = "high"
			if err := doc.Set("simultaneousThreads", int32(2)); err != nil { return err }
		default: return fmt.Errorf("unsupported class latencySensitivity %q", *value)
		}
		if err := doc.Set("latencySensitivity.level", level); err != nil { return err }
	}
	return nil
}

func restoreClassCompute(dst, preserved, projected *VirtualMachineClassSpec) {
	initClassCompute(dst)
	p, j := preserved.DeepCopy(), projected.DeepCopy()
	initClassCompute(p); initClassCompute(j)
	dstFields, pFields, jFields := classComputeBoolFields(dst), classComputeBoolFields(p), classComputeBoolFields(j)
	for i := range dstFields { restoreUnchangedClassPolicy(dstFields[i].policy, *pFields[i].policy, *jFields[i].policy) }
	restoreUnchangedClassPolicy(&dst.CPUAdvanced.LatencySensitivity, p.CPUAdvanced.LatencySensitivity, j.CPUAdvanced.LatencySensitivity)
	restoreUnchangedClassPolicy(&dst.CPUAdvanced.Topology.CoresPerSocket, p.CPUAdvanced.Topology.CoresPerSocket, j.CPUAdvanced.Topology.CoresPerSocket)
	restoreUnchangedClassPolicy(&dst.CPUAdvanced.Topology.VNUMANodeCount, p.CPUAdvanced.Topology.VNUMANodeCount, j.CPUAdvanced.Topology.VNUMANodeCount)
	trimClassCompute(dst)
}
