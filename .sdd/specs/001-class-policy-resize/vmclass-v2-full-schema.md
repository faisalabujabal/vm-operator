# Worklist: `VirtualMachineConfigSpec` field-by-field elevation decisions

- **Status**: Draft worklist — decision-ready inputs, not a final schema
- **Companion to**: [`vmclass-v2-design.md`](./vmclass-v2-design.md), whose §6.2 migration blocker this informs, and [`research-vmclass-as-policy.md`](./research-vmclass-as-policy.md)
- **Audience**: principal engineers, architecture review

## Purpose

`vmclass-v2-design.md` §6.2 identifies the hard migration blocker: v2 carries no opaque `configSpec` field, so every field a shipped `VirtualMachineClass` today expresses through `configSpec` needs either a typed home in the new schema or an explicit decision not to carry it forward. This document works that blocker field-by-field.

`VirtualMachineConfigSpec` is a public vSphere API type (`vim25/types.VirtualMachineConfigSpec` in govmomi), not something internal or `tera`-specific. The VMODL2 interface `VirtualMachineClasses` (already reviewed this session) confirms this directly — its `configSpec` field is declared `@Vmodl1Type(name="VirtualMachineConfigSpec")`, i.e. a pointer at this same public type, not a redefinition of it. wcpsvc's vmclass write path only *references* this type when passing `configSpec` through opaquely; it does not define its own schema for it. The struct enumerated below has 78 fields.

Three sources besides the struct itself informed the calls in the table:

1. **Documented, customer-facing usage** (`docs/concepts/workloads/vm-class.md`) — the only fields shown in a `configSpec` example today are `numCPUs`, `memoryMB`, `firmware`, `extraConfig` (via `OptionValue` entries), and `deviceChange` (for vGPU and Dynamic DirectPath I/O passthrough).
2. **The current `VirtualMachineClass` typed schema** (`api/v1alpha6/virtualmachineclass_types.go`) — `hardware.cpus`, `hardware.memory`, `hardware.devices.{vgpuDevices,dynamicDirectPathIODevices}`, `hardware.instanceStorage`, `policies.resources.{requests,limits}.{cpu,memory}` already cover several `ConfigSpec` fields today; these are marked **Elevate (already covered)** rather than proposed again under a new name.
3. **The being-removed `VirtualMachineConfigPolicy` CRD** (`external/vim/api/v1alpha1/virtualmachine_config_policy_types.go`) — it already elevated a *capability* surface (what a zone's hardware supports: `SEVSupported`, `TDXSupported`, `NumSimultaneousThreads`, `LatencySensitivityLevels`, `HugePagesSupported`, `IOMMUSupported`, `RSSSupported`, `UDPRSSSupported`, `LROSupported`, `TxRxThreadModels`, `SMCPresent`, `CPULockedToMaxSupported`, `MemoryLockedToMaxSupported`, `NumCPUCores`, `NumNUMANodes`, `Memory`, `ExtraConfig`) for a subset of these `ConfigSpec`-adjacent concerns. §2 below reconciles these explicitly — most of them are *feasibility* (what's available), not *request* (what a class wants), and the two are different fields in the v2 model (`status.zones`/`ConfigTarget` intersection per design doc §3.1a/§3.4, versus a class's own declared `spec` value).

As a side note on precedent: `VirtualMachineConfigPolicy`'s `IntRange`/`ResourceQuantityRange` types (`Min`/`Max`, both required, no `Default`) are the same shape the design doc's `{min, max, default}` model generalizes — the range concept is not a new invention for this repo, only the addition of a `default`.

**Legend:** *Elevate* = needs (or already has) a typed field in `VirtualMachineClass` v2. *Defer* = a real, plausible use case, but not required for the first cut. *Never* = not a class/policy-level concern; either VM/image-instance-specific, internal bookkeeping, deprecated, or already covered by a different, more specific mechanism elsewhere in this API surface.

## Field-by-field table

| ConfigSpec field | Call | Proposed home in v2 | Range-capable? | Source / use case |
|---|---|---|---|---|
| `ChangeVersion` | Never | N/A | No | Optimistic-concurrency guard for a single reconfigure call; not a class-level concept. |
| `Name` | Never | N/A | No | A VM's own display name; a class describes many VMs, not one. |
| `Version` | Never | N/A | No | VM's own config version string; VM identity, not class-level. |
| `CreateDate` | Never | N/A | No | Per-VM creation timestamp. |
| `Uuid` | Never | N/A | No | Per-VM SMBIOS UUID. |
| `InstanceUuid` | Never | N/A | No | Per-VM VC-assigned instance UUID. |
| `NpivNodeWorldWideName` | Never | N/A | No | Per-VM NPIV WWN assignment; no known class-level use case. |
| `NpivPortWorldWideName` | Never | N/A | No | Same as above. |
| `NpivWorldWideNameType` | Never | N/A | No | Internal NPIV WWN-assignment-source flag. |
| `NpivDesiredNodeWwns` | Never | N/A | No | Per-VM NPIV WWN count; no known class-level use case. |
| `NpivDesiredPortWwns` | Never | N/A | No | Same as above. |
| `NpivTemporaryDisabled` | Never | N/A | No | Per-VM operational NPIV toggle. |
| `NpivOnNonRdmDisks` | Never | N/A | No | Per-VM NPIV/RDM interaction detail. |
| `NpivWorldWideNameOp` | Never | N/A | No | Internal NPIV operation flag. |
| `LocationId` | Never | N/A | No | Internal per-VM file-location hash. |
| `GuestId` | Never | N/A | No | Guest OS identifier — set by the VM/image, not the class, in vm-operator's existing model. |
| `AlternateGuestName` | Never | N/A | No | Pairs with `GuestId`; same reasoning. |
| `Annotation` | Never | N/A | No | Free-text per-VM note; the class already has its own `spec.description`. |
| `Files` | Never | N/A | No | Per-VM file/datastore paths; infra detail, not a template concern. |
| `Tools` | Defer | `spec.policies.tools` | Partial (mostly enum/bool) | Real potential policy surface (e.g. tools-upgrade behavior) but no documented or coded usage today. |
| `Flags` | Defer | `spec.hardware.flags` | No (booleans) | Contains real toggles (e.g. `diskUuidEnabled`, relevant to CSI); no documented usage on a class today. |
| `ConsolePreferences` | Never | N/A | No | Legacy console-viewer power-on preference; no known use case. |
| `PowerOpInfo` | Defer | `spec.policies.powerOpInfo` | No (enum) | Default guest-shutdown/standby behavior; plausible policy, not requested. |
| `RebootPowerOff` | Never | N/A | No | Per-VM operational flag for the *next* reboot; not a template property. |
| `NumCPUs` | Elevate (already covered) | `spec.hardware.cpus` | Yes | Documented `configSpec` example (`numCPUs: 4`); already typed today. |
| `VcpuConfig` | Defer | `spec.hardware.vcpuConfig` | No | Per-vCPU advanced config (pinning); no documented usage. |
| `NumCoresPerSocket` | Elevate | `spec.hardware.coresPerSocket` | Yes, but must validate against `hardware.cpus` | Real topology setting (licensing/NUMA-driven); no documented usage yet, but plausible enough to elevate directly rather than defer. |
| `MemoryMB` | Elevate (already covered) | `spec.hardware.memory` | Yes | Documented `configSpec` example (`memoryMB: 8192`); already typed today. |
| `MemoryHotAddEnabled` | Elevate | `spec.hardware.memoryHotAddEnabled` | No (bool) | Real, known vSphere feature toggle; no ConfigPolicy or class-level equivalent exists yet. |
| `CpuHotAddEnabled` | Elevate | `spec.hardware.cpuHotAddEnabled` | No (bool) | Same reasoning as memory hot-add. |
| `CpuHotRemoveEnabled` | Elevate | `spec.hardware.cpuHotRemoveEnabled` | No (bool) | Same reasoning. |
| `VirtualICH7MPresent` | Never | N/A | No | Legacy Apple-hardware chipset flag; no known use case beyond `VirtualSMCPresent` below. |
| `VirtualSMCPresent` | Elevate (merge) | `spec.hardware.smcPresent` | No (bool) | ConfigPolicy already elevated this concept as the capability field `SMCPresent`; v2 needs the class's own *request* field, distinct from the zone's *supported* field. See §2. |
| `DeviceChange` | Never (raw list) | N/A — see specific device fields | No | Documented usage is entirely for vGPU and Dynamic DirectPath I/O passthrough, both already elevated as `hardware.devices.{vgpuDevices,dynamicDirectPathIODevices}`. The raw list itself is not proposed for elevation; specific device kinds are, individually. |
| `CpuAllocation` | Elevate (already covered) | `spec.policies.resources.{requests,limits}.cpu` | Yes | Already typed today. |
| `MemoryAllocation` | Elevate (already covered) | `spec.policies.resources.{requests,limits}.memory` | Yes | Already typed today. |
| `LatencySensitivity` | Elevate (merge) | `spec.policies.latencySensitivity` | No (enum) | ConfigPolicy elevated the *supported* levels (`LatencySensitivityLevels`, a feasibility/capability list); v2 needs the class's own *requested* level as a separate field. See §2. |
| `CpuAffinity` | Never | N/A | No | Explicit pinning to physical CPU IDs is host-specific and does not generalize across a portable class template. |
| `MemoryAffinity` | Never | N/A | No | Deprecated since vSphere 6.0. |
| `NetworkShaper` | Never | N/A | No | Per-NIC traffic shaping belongs to network/NIC configuration, not the VM class. |
| `CpuFeatureMask` | Defer | `spec.hardware.cpuFeatureMask` | No | Real advanced use case (CPUID masking for vMotion compatibility or licensing) but no documented usage today. |
| `ExtraConfig` | Elevate (already covered, by design) | `spec.extraConfig.{allowed,denied}` | No | Documented usage exists, but per the design doc's ExtraConfig discussion this stays a key-pattern allow/deny surface rather than being fully field-by-field elevated — `ExtraConfig` is an open, unbounded key space (guest tools, cloud-init, third-party conventions), unlike `ConfigSpec`'s closed, enumerable field list. |
| `SwapPlacement` | Defer | `spec.policies.swapPlacement` | No (enum) | Real infra-placement policy (e.g. force local swap for performance); no documented usage today. |
| `BootOptions` | Elevate (partial) | `spec.hardware.firmware` covers the overlapping `bootOptions.firmware` sub-field; remainder deferred | No | Overlaps with the top-level `Firmware` field below — vSphere carries firmware in two places for legacy reasons. Secure-boot/boot-retry sub-fields are real but deferred, no documented usage. |
| `VAppConfig` | Never | N/A | No | OVF product-property configuration is a per-deployment (image/VM) concern, not a class template concern. |
| `FtInfo` | Defer | N/A | No | Fault Tolerance eligibility; advanced HA feature, no documented usage. |
| `RepConfig` | Never | N/A | No | vSphere Replication (DR) config is per-VM, not a class template property. |
| `VAppConfigRemoved` | Never | N/A | No | Internal cleanup flag paired with `VAppConfig`. |
| `VAssertsEnabled` | Never | N/A | No | Internal VMware debugging/assertion flag. |
| `ChangeTrackingEnabled` | Elevate | `spec.policies.changeTrackingEnabled` | No (bool) | Changed Block Tracking is directly relevant to this repo's existing VM backup/restore feature area; a class-level default is a real, well-motivated ask even without a documented example yet. |
| `Firmware` | Elevate (already documented) | `spec.hardware.firmware` | No (enum: bios/efi) | Documented `configSpec` example (`firmware: "efi"`); not yet typed today, but customer-facing usage already exists. |
| `MaxMksConnections` | Defer | `spec.policies.maxMksConnections` | Yes (but niche) | Minor remote-console policy; no documented usage. |
| `GuestAutoLockEnabled` | Defer | `spec.policies.guestAutoLockEnabled` | No (bool) | Minor guest-OS policy; no documented usage. |
| `ManagedBy` | Never | N/A | No | Marks a VM as owned by a third-party extension; instance-level ownership metadata, not a class template property. |
| `MemoryReservationLockedToMax` | Elevate (merge) | `spec.policies.resources.memoryReservationLockedToMax` | No (bool) | ConfigPolicy elevated the capability (`MemoryLockedToMaxSupported`); v2 needs the class's own request, separately. See §2. |
| `NestedHVEnabled` | Elevate | `spec.hardware.nestedHVEnabled` | No (bool) | Real, known customer need (nested ESXi/KVM workloads); no documented usage yet but plausible enough to elevate directly. |
| `VPMCEnabled` | Defer | `spec.hardware.vPMCEnabled` | No (bool) | Niche profiling/performance-counter feature; no documented usage. |
| `ScheduledHardwareUpgradeInfo` | Defer | `spec.policies.hardwareVersionUpgradePolicy` | No (enum-ish) | Distinct from the already-elevated `maxHardwareVersion` ceiling — this is the *upgrade behavior* policy (auto-upgrade timing), not the ceiling itself. Real, but not required for the first cut. |
| `VmProfile` | Never | N/A | No | Generic storage/other profile associations; storage policy is already covered via `hardware.instanceStorage.storageClass` and the VM's own storage-class references outside the class. |
| `MessageBusTunnelEnabled` | Never | N/A | No | Legacy VIX/guest message-bus automation feature. |
| `Crypto` | Never (covered elsewhere) | N/A | No | VM encryption is already a dedicated feature area in this repo (`external/byok`, `EncryptionClass`) — a more specific mechanism than a raw `ConfigSpec` passthrough. Not proposed as a class-level `ConfigSpec` elevation to avoid a second, competing surface for the same concern. |
| `MigrateEncryption` | Never (covered elsewhere) | N/A | No | Same reasoning as `Crypto`. |
| `SgxInfo` | Defer | `spec.hardware.sgxInfo` | No | Niche security hardware feature (Intel SGX enclaves); no documented usage. |
| `FtEncryptionMode` | Never | N/A | No | Bundled with the deferred/niche `FtInfo` feature. |
| `GuestMonitoringModeInfo` | Defer | `spec.policies.guestMonitoringModeInfo` | No | Niche guest-introspection security feature (NSX-adjacent); no documented usage. |
| `SevEnabled` | Elevate (merge) | `spec.hardware.sevEnabled` | No (bool) | ConfigPolicy elevated the capability (`SEVSupported`); v2 needs the class's own request, separately. See §2. |
| `VirtualNuma` | Defer | `spec.hardware.virtualNuma` | Partial | Explicit vNUMA topology override — real for HPC/telco workloads (the meeting notes this design is based on specifically mention telco hardware requirements), but advanced and not documented today. Flagged as higher-priority-than-typical among the "Defer" items. |
| `MotherboardLayout` | Never | N/A | No | Obscure motherboard/chipset layout setting; no known use case. |
| `PmemFailoverEnabled` | Defer | N/A | No | Pairs with the deferred `Pmem` field below; niche persistent-memory feature. |
| `VmxStatsCollectionEnabled` | Never | N/A | No | Internal vmx telemetry/stats toggle. |
| `VmOpNotificationToAppEnabled` | Defer | `spec.policies.vmOpNotification` | No (bool) | App-HA/quiesce-adjacent guest notification feature; niche, no documented usage. |
| `VmOpNotificationTimeout` | Defer | `spec.policies.vmOpNotification` | Yes | Pairs with the field above. |
| `DeviceSwap` | Defer | `spec.hardware.deviceSwap` | No | Advanced device hot-swap behavior (vSphere 8.0+); no documented usage. |
| `SimultaneousThreads` | Elevate (merge) | `spec.hardware.simultaneousThreads` | Yes | ConfigPolicy elevated the capability (`NumSimultaneousThreads`, a range); v2 needs the class's own requested value, separately. See §2. |
| `Pmem` | Defer | `spec.hardware.pmem` | Partial | Persistent memory (NVDIMM) device config; real but niche, no documented usage. |
| `DeviceGroups` | Defer | `spec.hardware.devices.deviceGroups` | No | Grouped passthrough devices (e.g. multi-GPU NVLink groups, vSphere 8.0+) — an emerging, real use case adjacent to the already-elevated vGPU/DirectPath device fields, but no documented usage yet. |
| `FixedPassthruHotPlugEnabled` | Defer | `spec.hardware.devices.fixedPassthruHotPlugEnabled` | No (bool) | Niche passthrough-device hot-plug toggle. |
| `MetroFtEnabled` | Never | N/A | No | Bundled with the deferred/niche Fault Tolerance feature area. |
| `MetroFtHostGroup` | Never | N/A | No | Same as above. |
| `TdxEnabled` | Elevate (merge) | `spec.hardware.tdxEnabled` | No (bool) | ConfigPolicy elevated the capability (`TDXSupported`); v2 needs the class's own request, separately. See §2. |
| `SevSnpEnabled` | Elevate (merge) | `spec.hardware.sevSnpEnabled` | No (bool) | ConfigPolicy elevated the capability (`SEVSNPSupported`); v2 needs the class's own request, separately. See §2. |
| `TagSpecs` | Never | N/A | No | vSphere-native tag assignment; vm-operator already has its own Kubernetes label/annotation mechanism for this kind of metadata. |
| `VmPlacementPolicies` | Never (covered elsewhere) | N/A | No | vSphere-native placement-policy references; this repo already has a dedicated placement/governance mechanism (`vsphere.policy.vmware.com`'s `ComputePolicy`), a more specific fit than a raw `ConfigSpec` passthrough. |

## §2. Fields with a differently-named elevated equivalent already in `VirtualMachineConfigPolicy`

These are the cases where simply reusing `VirtualMachineConfigPolicy`'s field name would be wrong, because that field answers a different question than the `ConfigSpec` field does. `VirtualMachineConfigPolicy`'s fields below are *feasibility/capability* — synced from a zone's `ConfigTarget`, answering "is this available here" — while each corresponding `ConfigSpec` field is a *request* — "does this specific class want this turned on." In the v2 model these stay two different things: the capability side folds into the `ConfigTarget`-derived feasibility check already described in `vmclass-v2-design.md` §3.1a/§3.4 (never a `spec` field a class author sets), while the request side becomes a new typed `spec` field on the class, checked for feasibility against that same capability data at admission — not by comparing two `spec` values against each other.

| Capability field (`VirtualMachineConfigPolicy`) | Request field (`ConfigSpec`) | New v2 home for the request |
|---|---|---|
| `SMCPresent` | `VirtualSMCPresent` | `spec.hardware.smcPresent` |
| `SEVSupported` | `SevEnabled` | `spec.hardware.sevEnabled` |
| `SEVSNPSupported` | `SevSnpEnabled` | `spec.hardware.sevSnpEnabled` |
| `TDXSupported` | `TdxEnabled` | `spec.hardware.tdxEnabled` |
| `NumSimultaneousThreads` (range) | `SimultaneousThreads` | `spec.hardware.simultaneousThreads` |
| `MemoryLockedToMaxSupported` | `MemoryReservationLockedToMax` | `spec.policies.resources.memoryReservationLockedToMax` |
| `LatencySensitivityLevels` (supported list) | `LatencySensitivity` | `spec.policies.latencySensitivity` |
| `NumCPUCores` / `NumNUMANodes` / `Memory` (ranges) | `NumCPUs` / (NUMA n/a) / `MemoryMB` | Already covered by `spec.hardware.cpus` / `spec.hardware.memory` — no NUMA-node-count request field exists in `ConfigSpec` itself, so nothing to merge there. |
| `ExtraConfig` (allow/deny rules) | `ExtraConfig` (`[]OptionValue`) | Already the same shape and purpose — `VirtualMachineConfigPolicy`'s `ExtraConfig` *is* the design v2 carries forward as `spec.extraConfig`, not a field needing separate elevation. |
| `CPULockedToMaxSupported`, `HugePagesSupported`, `IOMMUSupported`, `RSSSupported`, `UDPRSSSupported`, `LROSupported`, `TxRxThreadModels` | *(no direct `ConfigSpec` field — these describe NIC/host capabilities outside `ConfigSpec`'s scope)* | Pure feasibility signals with no class-level request counterpart; fold into the `ConfigTarget`-derived compatibility check only, nothing to elevate. |

## §3. Fields deliberately marked Never

Grouped by rationale, one line each beyond what the table above already states:

- **Per-VM identity/instance state** (`Name`, `Version`, `CreateDate`, `Uuid`, `InstanceUuid`, `LocationId`, `RebootPowerOff`, `ManagedBy`, `Annotation`, `Files`) — these describe one running VM, not a reusable template.
- **Guest identity, set by image/VM, not class** (`GuestId`, `AlternateGuestName`) — vm-operator's existing model derives these from the `VirtualMachineImage`/VM spec.
- **NPIV fibre-channel WWN assignment** (`NpivNodeWorldWideName`, `NpivPortWorldWideName`, `NpivWorldWideNameType`, `NpivDesiredNodeWwns`, `NpivDesiredPortWwns`, `NpivTemporaryDisabled`, `NpivOnNonRdmDisks`, `NpivWorldWideNameOp`) — per-VM storage identity assignment, no known class-level use case.
- **Deprecated or legacy** (`MemoryAffinity` — deprecated since 6.0; `VirtualICH7MPresent`, `ConsolePreferences`, `MotherboardLayout` — legacy/obscure chipset and console settings; `MessageBusTunnelEnabled` — legacy VIX automation).
- **Host-specific, non-portable** (`CpuAffinity`) — pinning to physical CPU IDs cannot generalize across a class template meant to work on any compatible host.
- **Already covered by a dedicated, more specific mechanism elsewhere in this API surface** (`Crypto`, `MigrateEncryption` — BYOK/`EncryptionClass`; `VmPlacementPolicies` — `vsphere.policy.vmware.com` `ComputePolicy`; `TagSpecs` — Kubernetes labels/annotations; `VmProfile` — existing storage-class/instance-storage fields) — proposing a second elevation path for something already handled elsewhere would create two competing surfaces for the same concern.
- **Internal bookkeeping/debugging** (`ChangeVersion`, `VAssertsEnabled`, `VAppConfigRemoved`, `VmxStatsCollectionEnabled`) — not customer-facing concerns at all.
- **Out of a class's scope entirely** (`VAppConfig` — per-deployment OVF properties; `RepConfig` — per-VM DR replication; `NetworkShaper` — per-NIC config).
- **Bundled with a deferred feature area, not independently useful** (`FtEncryptionMode`, `MetroFtEnabled`, `MetroFtHostGroup` — all depend on the deferred `FtInfo`/Fault Tolerance eligibility).

---

— Faisal + Claude
