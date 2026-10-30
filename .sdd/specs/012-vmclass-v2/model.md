# Model: Flex VM Classes (`VirtualMachineClass` v2)

- **Spec**: [`spec.md`](spec.md)
- **Full field list**: WIKI page 3118085467 — "Design: VM Service: Flex VM Classes: Full Schema". This file records the API surface and the rules; the full schema page lists every typed field, its `configSpec` source and the capability data it is checked against.

All types are in `vmoperator.vmware.com/v1alpha6`, which has not shipped and is reshaped in place. v1alpha5 and older convert to it.

---

## Value shapes

Every governable field has one shape. Each shape is a concrete, named Go struct (no generics, so `controller-gen` can build the schema). All sub-fields are optional pointers, so absent is distinct from zero. Each shape has a field set no other shape has, so a client can tell the shape from the CRD schema alone; a schema test checks that every governed field matches exactly one shape.

| Shape | Fields | Rules |
|---|---|---|
| Number (`Range`) | `min`, `max`, `default` | `default` is required when `min` or `max` is set, and must be within them. An unset bound is open: limited by the hardware, or by a parent class in Milestone 2. A fixed value is `min == max == default`. A wholly absent struct leaves the value to vpxd. |
| `EnumPolicy` | `allowed`, `default`, `required` | Unset `allowed` = all values; `[]` = none. `default` is required when `allowed` has more than one value; with one value, that value is the default. A set `default` must be in `allowed`. A VM that omits the field inherits `default`; an explicit `""` means no value; `required: true` rejects an empty value after defaulting. [NEEDS CLARIFICATION: whether `""` means "no value" for every enum field or only for `reservedProfileName`.] |
| `BoolPolicy` | `allowOverride`, `default` | `allowOverride: false` fixes the value and needs a `default`. Otherwise an unset `default` leaves the value to vpxd. |
| `ListPolicy` | `default`, `allowed`, `denied` | `default` is filled into the VM at admission; the VM may add or remove items within `allowed` and `denied`. Items are flat structs; `default` is a map list (`+listType=map`) keyed by the item's key fields. A matcher is the item's key fields, each optional: an unset key field matches anything; a numeric key field matches exactly; a string key field is `{fixed, glob, regex}` with exactly one set. Matcher lists are atomic. `denied` wins. `allowed` only is closed; `denied` only is open. A class can't require an item. |
| `SetPolicy` | `default`, `allowed`, `required` | The value is one set. `default` is a set; `allowed` is a list of approved sets; sets compare with unique members and order ignored; no matchers and no `denied`. Unset `allowed` = any set of valid members; `required: true` rejects an empty set. Members' type and domain come from the field. Each `allowed` entry is `{members: [...]}`, with `members` a set list (`+listType=set`). Go types are concrete per member type (`Int32SetPolicy`), as with `IntRange` and `LongRange`. |

A collection field's shape follows what its value is. A collection of independent items, each of which can be added or removed on its own, is a `ListPolicy` (`advanced.extraConfig`, devices). A collection that is one setting, whose meaning changes when any member changes, is a `SetPolicy`. The admin chooses the approved sets.

| Collection | Shape | Milestone |
|---|---|---|
| `advanced.extraConfig` | `ListPolicy` | 1 |
| `hardware.devices.vgpuDevices` | `ListPolicy`; item `{profileName, count}`, key `profileName` | 1 |
| `hardware.devices.dynamicDirectPathIODevices` | `ListPolicy`; item `{vendorID, deviceID, customLabel, count}`, key `vendorID` + `deviceID`. `customLabel`, when set, is passed through to the device backing; it is not a key. | 1 |
| `advanced.pnumaNodeAffinity` | `Int32SetPolicy` of node IDs, e.g. `{default: [0,1], allowed: [{members: [0,1]}, {members: [2,3]}], required: true}`. Unset `allowed` = any set of valid node IDs; `required: false` permits an explicit `[]` (no pinning). Each node ID is checked against `ConfigTarget.numNumaNodes`. | 1 |
| `hardware.cpuFeatureMask` | Preset of the top class only in Milestone 1 (no VM field, so nothing below it to govern); checked against config options `cpuFeatureMaskSupported`. [NEEDS CLARIFICATION: shape for Milestone 2 child classes. Each CPUID leaf's mask is one setting, but the leaves are independent items.] | 2 |

Device items: independent count bounds and required entries are deferred. A device's quantity is its `count`.

**Matching names.** An item's key fields have the same names in the environment-browser entry, the class item and the VM item, so choosing an option copies the entry's key fields. Environment-browser entries may carry extra display fields (`vendorName`, `deviceName`, `customLabel`); class and VM items add payload such as `count`.

| Item | Environment-browser entry | Class item | VM item |
|---|---|---|---|
| Dynamic DirectPath I/O | ConfigTarget `dynamicPassthroughDevices[]`: `vendorID`, `deviceID` (+ `vendorName`, `deviceName`, `customLabel`) | `vendorID`, `deviceID`, `customLabel`, `count` | same as the class item |
| vGPU | ConfigTarget `vgpuProfile[]`: `profileName` (+ display fields) | `profileName`, `count` | same as the class item |
| pNUMA node | ConfigTarget `numNumaNodes` (IDs 0..n-1) | node ID | `pnumaNodeAffinity []int32` |

## `VirtualMachineClass.spec`

| Field | Type | Rules |
|---|---|---|
| `description` | string | As today. |
| `externalID` | string | The client's identity for the class; immutable after create. Upgrade sets it to the name. |
| `zones` | `*[]string` | Availability. Unset = every zone of the namespace, including later ones; `[]` = none; a list = only those. |
| `governs` | struct | Absent = the class doesn't govern. |
| `governs.zones` | `*[]string` | Unset = same as `zones`; `[]` = none; a list must be a subset of `zones`. |
| `governs.enforcement` | enum | One value, `Deny`, set by the mutating webhook. |
| `governs.existingVMs` | enum | `AllowOnViolation` or `DenyPowerOn`. Required on a governing class with no parent. In Milestone 2 only the top class sets it; children inherit it. |
| `parentClassRefs` | list of `{name}` | Milestone 2. Rejected in Milestone 1. One entry; same namespace. |
| `resources.size.{cpu,memory}` | Number | CPU in vCPUs. |
| `resources.{requests,limits}.{cpu,memory}` | Number | CPU in vCPUs. The incremental migration retains the existing Supervisor-wide minimum host frequency for conversion to VM MHz. Cluster-specific conversion remains an open question and must not change the vCPU definition implicitly. |
| `cpuAdvanced.topology.{coresPerSocket,vnumaNodeCount}` | `IntRange` | Preserve the existing VM topology path. vNUMA node count projects to `VirtualNuma.CoresPerNumaNode` using the effective CPU size; zero selects automatic sizing. |
| `cpuAdvanced.{hotAddEnabled,hotRemoveEnabled,iommuEnabled,nestedHardwareVirtualizationEnabled,performanceCountersEnabled}` | `BoolPolicy` | Includes the missing VM hot-remove boolean counterpart. CPU count decreases still require power-off. |
| `cpuAdvanced.latencySensitivity` | `EnumPolicy` | Existing VM values `Normal`, `High`, `HighWithHyperthreading`; unsupported legacy levels stay in residual `configSpec`. |
| `memoryAdvanced.{hotAddEnabled,reservationLockedToMax}` | `BoolPolicy` | Same VM paths. |
| `cpuAdvanced.*`, `memoryAdvanced.*`, `bootOptions.*`, `advanced.*` | per field | Same paths as `VirtualMachine.spec`. |
| `advanced.extraConfig` | `ListPolicy` | At the VM's path. |
| `hardware.devices.{vgpuDevices,dynamicDirectPathIODevices}` | `ListPolicy` of device items | See "Value shapes". |
| `hardware.*` | per field | Settings that aren't in the VM spec but are exposed or checked. |
| hardware version | Number | [NEEDS CLARIFICATION: shape, path, and upgrade or reject below the minimum.] |
| `hardware.instanceStorage` | as today | Preset only. |
| `reservedProfileName` | `EnumPolicy` of profile names | Same path as the VM's `spec.reservedProfileName`. Every name must resolve to a `VirtualMachineReservedProfile` in the namespace. Unset `allowed` is not "all profiles": `allowed` is required when `default` is set. See "Reserved capacity". |
| `reservedSlots` | int32 | Deprecated. Same name and meaning as today: the namespace's desired slot total across zones. Preserved through conversion and the namespace copy, including zero; never overwritten with observed totals. |
| `configSpec` | `VirtualMachineConfigSpec` | Pass-through settings with no typed field; presets only, never governance. An elevated path is moved for older writers and rejected for v1alpha6 writers. Only the top class of a chain may set it. |

Label: the schema-version label records the last CRD version that wrote the class. A write through an older version is rejected once a newer one has written it. The provider's class API shows it as read-only `schemaVersion`.

## `VirtualMachineClass.status`

| Field | Meaning |
|---|---|
| `zones[].name` | Covers `spec.zones`, or every zone when unset. |
| `zones[].conditions` | `HardwareCompatible`, `WithinGovernance`, `Governing`. |
| `zones[]` resolved limits | Per-VM limits and options a VM can obtain in that zone, for every governed field: the class within the governing class (and the parent in Milestone 2), within what the environment browser reports for the zone. Ranges as `{min, max}`; enums, sets and device lists as the available values, with the entry's display fields. A field the class doesn't set is limited by the environment browser alone. Fields without a finite domain (extraConfig) carry their effective `allowed` and `denied`. Updated when the environment-browser objects change. Never pool totals, never what governance forbids. `pnumaNodeAffinity` options are the class's approved sets when set, otherwise the node IDs from `numNumaNodes`; a per-VM limit, not a pool total. |
| `observedGeneration`, `conditions[Ready]` | As required for every controller. |

## `VirtualMachine`

| Field | Rules |
|---|---|
| `spec.reservedProfileName` | Optional string (`*string` in Go): nil is omitted; a pointer to `""` explicitly selects no profile. Filled by the mutation webhook from the class's `reservedProfileName.default` (or the governing class's), like any other default; explicit `""` = no profile. Must be in `allowed`. On a class-backed VM it is class-supplied and can't be edited inline; a classless VM sets or inherits it. Can't change from one set value to another while the VM is powered on; vm-operator may set it from unset (schema upgrade). When set, the VM's CPU count, CPU reservation and limit, memory reservation and limit, and device identities and quantities must equal the profile's; overhead is not compared. vm-operator writes the vCenter profile ID from this field and removes it when the field is empty. |
| `status.governingClassNames` | Optional string set of the names of the classes governing the VM's zone, computed live. |
| `conditions[GovernanceCompliant]` | `True`/`False`; reason `NoGoverningClass` when the zone has none (False for classless VMs, True for class-backed VMs). |
| `spec.hardware.devices.vgpuDevices` | Map list keyed by `profileName`; entries have a nonempty profile name and required positive `count`. |
| `spec.hardware.devices.dynamicDirectPathIODevices` | Map list keyed by `vendorID` + `deviceID`; PCI IDs are 0–65535, `count` is required and positive, and optional `customLabel` selects the backing without joining the key. |
| `status.hardware.firmware` | Observed `bios` or `efi`; omitted until observed. |
| `status.changeBlockTracking` | Existing optional boolean, reused. |
| `status.hardware.cpu.hotRemoveEnabled` | Optional observed booleans; nil differs from false. |
| `status.hardware.{sevEnabled,sevSnpEnabled,tdxEnabled,smcPresent}` | Optional observed booleans; nil differs from false. |
| `status.hardware.dynamicDirectPathIODevices` | Observed PCI devices and quantities, with the same item shape and map keys as the inline device list. |

The VM and Zone API foundation (T108–T109) adds schema and conversion support. These definitions are present independently of the capability; runtime behavior remains gated in its implementing stories. vmop-4335 populates VM governance and observed status; vmop-4323 populates the Zone mirror. VM fields without an older-version home survive v1alpha1–v1alpha5 round trips in the conversion annotation, including explicit empty profile selections and false observed booleans.

**vmx-23 fields.** CPU `reservationLockedToMax` and NUMA fixed auto-affinity (`cpuAdvanced.numaFixedAutoAffinityEnabled`) are requested for release 9.2 but exist in vSphere only from hardware version vmx-23. Each is a VM spec field, a `BoolPolicy` at the same class path, and an observed status field, available only when the hardware version is vmx-23 or later. Tracked separately (vmop-4428) and not part of the rest of the VM API. [NEEDS CLARIFICATION: final VM path name for the CPU lock (`cpuAdvanced.reservationLockedToMax` or `cpuAdvanced.cpuReservationLockedToMax`), and the environment-browser capability names in 9.2.]

## `Zone` (`external/tanzu-topology`)

| Field | Rules |
|---|---|
| `status.governingClasses` | Optional string set of class names. Mirror of the classes that govern the zone; written by one controller with an optimistic lock; never read by governance. |

## Reserved capacity

A reserved profile is a platform object that holds one VM shape: vCPU count, CPU reservation and limit (equal), memory reservation and limit (equal), and passthrough device profiles. The platform places slots of a profile on hosts, gives a powered-on VM that names the profile a free slot, and takes it back at power-off. vm-operator never enforces slot counts.

- `VirtualMachineReservedProfile` is not a vm-operator API type. It is published by the provider with each profile's shape and per-zone counts, backfilled for existing allocations before the capability is enabled (spec, "Provider contract"). vm-operator only reads it (read RBAC), as it reads the environment-browser objects. A profile name that doesn't resolve is a clear admission failure, with no fallback.
- A class that lists profiles: the class's default configuration must match its default profile, and every allowed profile must fit within the class's resource and device policy; listing a profile does not fix the class's bounds.
- A VM with a selected profile must match the profile's shape after defaulting, whether or not it names a class. A resize past the profile is rejected at admission.
- Class edits through v1alpha6 are rejected only when they change an allocated profile's shape or make the class's default VM invalid for it. The provider's older class API keeps today's block on update and delete while capacity is assigned.

## Conversion

| From v1alpha5 | To v1alpha6 |
|---|---|
| scalar field `v` | `{min: v, max: v, default: v}` |
| `hardware.{cpus,memory}` | `resources.size.{cpu,memory}` |
| `policies.resources.{requests,limits}` | `resources.{requests,limits}` |
| `configSpec` path that has a typed field | that typed field |
| (none) | `externalID` = name; `zones` unset |
| `reservedProfileID: X` | `reservedProfileName: {default: X, allowed: [X], required: true}` |
| `reservedSlots` | `reservedSlots`, unchanged, including zero |

`reservedProfileID` is removed in v1alpha6 and exists only in v1alpha5 and older. Back to v1alpha5, `reservedProfileID` = `reservedProfileName.default`. Up again, if `reservedProfileID` differs from the default stored in the conversion annotation (an older-API writer changed it), the field is reset to `{default: Y, allowed: [Y], required: true}`; otherwise it is restored from the annotation.

Fields with no v1alpha5 home round-trip through the conversion annotation.

## Examples

A fixed class after upgrade:

```yaml
spec:
  externalID: best-effort-small
  resources:
    size:
      cpu: {min: 2, max: 2, default: 2}
      memory: {min: 4Gi, max: 4Gi, default: 4Gi}
```

A governing class for a zone:

```yaml
spec:
  zones: [zone-a]
  governs:
    existingVMs: AllowOnViolation
  resources:
    size:
      cpu: {min: 1, max: 16, default: 2}
      memory: {min: 1Gi, max: 64Gi, default: 4Gi}
  advanced:
    extraConfig:
      allowed: [{key: {glob: "guestinfo.*"}}]
      denied: [{key: {glob: "guestinfo.secret.*"}}]
```

A reserved GPU class:

```yaml
spec:
  reservedProfileName:
    default: gpu-large
    allowed: [gpu-large]
    required: true
  resources:
    size:
      cpu: {min: 8, max: 8, default: 8}
      memory: {min: 64Gi, max: 64Gi, default: 64Gi}
  memoryAdvanced:
    reservationLockedToMax: {allowOverride: false, default: true}
```

### Advanced field batch (vmop-4315)

`advanced` contains `BoolPolicy` fields `changeBlockTracking`, `preferHtEnabled`, `hugePages1GEnabled`, `timeTrackerLowLatencyEnabled`, `cpuAffinityExclusiveNoStatsEnabled` and `vmxSwapEnabled`; `pnumaNodeAffinity` is an `Int32SetPolicy`; `extraConfig` is a concrete key/value list policy. `extraConfig.default` uses the VM's string key/value items, keyed by `key`; allowed/denied items contain an optional `key` string matcher with exactly one of `fixed`, `glob`, or `regex`. An omitted key matches any key. Governance enforcement remains separate.

`hugePages1GEnabled` is the existing VM's boolean enable flag for 1 GB memory backing (`sched.mem.lpage.enable1GPage`). It does not select page sizes. The environment-browser schemas expose no page-size choice, so the ambiguous full-schema `hugePages` shorthand is represented by the existing VM field and `BoolPolicy`.

Conversion lifts `changeTrackingEnabled` and supported VMX string values from `configSpec`, leaving unknown JSON and non-string ExtraConfig values in the residual document. A converted legacy boolean is fixed; a NUMA set becomes its default and one approved set. Round-trip annotations preserve policy metadata and each older API edit takes precedence for the corresponding field.

CPU/memory deferrals: `cpuAdvanced.numaFixedAutoAffinityEnabled` and `cpuAdvanced.reservationLockedToMax` require a verified supported wire mapping before class/VM fields, reconciliation and tests can ship. The pinned govmomi model exposes neither advertised property. The proposed `cpuAdvanced.vnuma` container is omitted: pinned `VirtualMachineVirtualNuma` has only `coresPerNumaNode` and `exposeVnumaOnCpuHotadd`; node count maps the former, and the latter is deferred separately. It has no remaining configurable members.

`cpuAdvanced.exposeVnumaOnCpuHotadd` is deferred from both typed class and VM APIs. The vSphere request only permits ESXi to consider exposure, while `NumaInfo.VnumaOnCpuHotaddExposed` reports effective observed topology; these are not proven interchangeable for reconciliation. Keep the raw `configSpec.virtualNuma.exposeVnumaOnCpuHotadd` path unchanged until a supported configured-state source and convergence semantics are verified (T103p).
