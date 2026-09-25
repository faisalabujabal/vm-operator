# `VirtualMachineClass` v2: the governed-value type system

- **Status**: Draft design
- **Companion to**: [`vmclass-v2-design.md`](./vmclass-v2-design.md) (the authoritative mechanism design — `governs`, zones, `parentClassRefs`) and [`vmclass-v2-full-schema.md`](./vmclass-v2-full-schema.md) (the field-by-field `ConfigSpec` elevation worklist this document does not duplicate)
- **Audience**: principal engineers, architecture review

## Purpose

`vmclass-v2-design.md` §2 specifies one governed-value shape — `{min, max, default}` for numeric ranges — and leaves the rest (booleans, enums, lists, `extraConfig`) implicit. This document works out the remaining shapes, the rule for when a field needs governance at all, and a handful of concrete gaps found while doing that (some already fixed directly in `vmclass-v2-design.md`, some still open).

## §1. The governing question: does an inline twin exist on `VirtualMachineSpec`?

A class field only needs `{min,max,default}`-style ceiling semantics if there is a live, VM-spec field a user can edit inline that the ceiling would bound. If no such field exists, a "ceiling" bounds nothing — the class can only ever supply a fixed value. This was checked against the real API, not assumed:

- `spec.resources.size.{cpu,memory}` → `ConfigSpec.NumCPUs`/`MemoryMB`, inline-editable → class-side ceiling makes sense.
- `spec.resources.requests/limits.{cpu,memory}` → `CpuAllocation`/`MemoryAllocation.Reservation`/`.Limit`, inline-editable → same.
- `spec.cpuAdvanced.topology.coresPerSocket` → `ConfigSpec.NumCoresPerSocket`, inline-editable → same.
- `spec.cpuAdvanced.{hotAddEnabled,iommuEnabled,nestedHardwareVirtualizationEnabled,performanceCountersEnabled}`, `spec.memoryAdvanced.{hotAddEnabled,reservationLockedToMax}` → all inline-editable booleans with real `ConfigSpec` twins.
- `spec.extraConfig []KeyValuePair` → inline-editable, already webhook-governed by reserved-prefix denial today.
- `spec.hardware.{cdrom,ideControllers,nvmeControllers,sataControllers,scsiControllers}` → inline-editable device-attachment lists.
- Checked and **confirmed absent**: `MaxMksConnections`, `SgxInfo.epcSize`, `NetworkShaper`, `VmOpNotificationTimeout`, `Firmware`, boot options, and vGPU/DynamicDirectPathIO device selection (`VGPUs` only appears in `VirtualMachineHardwareStatus`, never in `spec` — a VM cannot select a vGPU profile inline today, it only ever inherits what the class attaches).

**Rule:** range/bool/enum/list ceiling semantics only for fields with a confirmed inline twin. Everything else is preset-only until an inline twin actually appears.

## §2. Four value shapes, not one

### 2.1 `Range` (numeric) — extending, not replacing, `IntRange`/`LongRange`/`ResourceQuantityRange`

**Confirmed in discussion: `Default` is new.** `external/vim/api/v1alpha1/range_types.go`'s `IntRange`/`LongRange`/`ResourceQuantityRange` today only have `Min`/`Max` (both required) — `VirtualMachineConfigPolicy` never needed a third field because it only ever bounded, never supplied a literal starting value. v2 combines the bounding role (`ConfigPolicy`'s job) with the preset role (`VirtualMachineClass`'s job), and that's exactly why `Default` needs to exist now, as part of this merge — not an oversight in the original `ConfigPolicy` types.

```go
type IntRange struct {
    // +optional
    Default *int32 `json:"default,omitempty"`
    // +optional
    Min *int32 `json:"min,omitempty"`
    // +optional
    Max *int32 `json:"max,omitempty"`
}
```

All three pointer-typed. This isn't just consistency with the rest of the design — it's load-bearing for two independent reasons:
1. §8.3 already requires field-absence to be distinguishable from field-zero, for `parentClassRefs` fallthrough.
2. It decouples two administrative intents that a non-pointer, `Default == Min`-by-convention shape conflates: *"change what new VMs start with"* versus *"change what's permitted."* Today, because `default` defaults to `min`, editing the starting value for new VMs and editing the ceiling are the same edit — an admin cannot move one without the other. A real `Default` field lets an admin adjust the preset without touching the ceiling at all, which changes nothing for already-admitted VMs (they're pinned to an immutable `ClassInstance` snapshot regardless, see §3 below) and triggers no `governs.existingVMs` drift check (the ceiling didn't move).

**Additive-growth for fields without an inline twin, per §1's rule:** ship the same struct, but only populate `Default` — `Min`/`Max` stay `nil` until a real inline twin appears. Adding `Min`/`Max` later is a purely additive change (new optional sibling fields on an already-struct-typed field) — not the scalar→struct breaking change the constitution warns about (citing k8s#111703), because the field was never a bare scalar to begin with. This is a strictly better fit for the constitution's additive-safety rule than an earlier draft of this reasoning, which proposed shipping full `{min,max,default}` on every numeric field regardless of whether it's used yet — that works, but pays an unnecessary UX cost (exposing `min`/`max` sub-fields that don't bound anything) that shipping `{default}`-only and growing the struct later avoids entirely.

### 2.2 `BoolPolicy` — new, no `ConfigPolicy` precedent

```go
type BoolPolicy struct {
    // +optional
    Default *bool `json:"default,omitempty"`
    // +optional
    AllowOverride *bool `json:"allowOverride,omitempty"`
}
```

**`ConfigPolicy` did not do this.** Its booleans (`SMCPresent`, `SEVSupported`, `SEVSNPSupported`, `TDXSupported`, `IOMMUSupported`, `RSSSupported`, `UDPRSSSupported`, `LROSupported`, `CPULockedToMaxSupported`, `MemoryLockedToMaxSupported`, `HugePagesSupported`) are plain, non-pointer `bool` — pure capability flags answering "is X possible in this zone," never a literal value handed to a VM. `ConfigPolicy` never played the preset role, so it never needed a default/override pair. This shape is new work introduced by combining the two use cases, exactly parallel to `Range` gaining `Default`.

**Field name is deliberately not `locked`.** `ConfigSpec.MemoryReservationLockedToMax` (and the VM's own `spec.memoryAdvanced.reservationLockedToMax`) already uses "Locked" in this same struct family with an unrelated meaning — "pin the reservation equal to max." Reusing "locked" nearby for "this field cannot be overridden" risks exactly the kind of collision a reviewer trips over mid-review. `AllowOverride` states the actual question ("may the VM change this?") without borrowing vocabulary from something else in the same domain. Also deliberately not reusing `VirtualMachineConfigPolicyMode` (`Allow`/`Deny`) — that enum is a per-*object* mode (`CreateMode`/`UpdateMode`/`PowerOnMode`) on a kind this design deletes; a per-*field* override flag borrowing that vocabulary would import meaning from something being retired.

**A bare `{locked: bool}`, with no paired value, does not work.** "SEV must be forced off" and "SEV must be forced on" are both real policies, and a single flag with no value can't express which — `AllowOverride` alone says nothing about what the forced value *is*. The pair is necessary, not optional convenience.

**No inline twin today** (`VirtualICH7MPresent`, `VirtualSMCPresent`, `GuestAutoLockEnabled`, `VAssertsEnabled`, `ChangeTrackingEnabled`, `MessageBusTunnelEnabled`, `VmxStatsCollectionEnabled`, `FixedPassthruHotPlugEnabled`, `PmemFailoverEnabled`, `MetroFtEnabled`, `VmOpNotificationToAppEnabled`) → ship `{default}`-only, same additive-growth rule as §2.1.

### 2.3 `EnumPolicy[T]` — generalizes a pattern `ConfigPolicy` already used, minus the default

`ConfigPolicy`'s `LatencySensitivityLevels []LatencySensitivityLevel` and `TxRxThreadModels []TxRxThreadModel` are already "list of allowed enum values" — the governance half of this shape, with no preset because `ConfigPolicy` never supplied one:

```go
type LatencySensitivityPolicy struct {
    // +optional
    Default *VirtualMachineLatencySensitivityLevel `json:"default,omitempty"`
    // +optional
    Allowed []VirtualMachineLatencySensitivityLevel `json:"allowed,omitempty"`
}
```

`LatencySensitivity` has a real inline twin (`spec.cpuAdvanced.latencySensitivity`) → full shape from day one. `Firmware`, `SwapPlacement`, `GuestMonitoringModeInfo`'s mode → no inline twin → `{default}`-only for now, same growth rule.

**Implementation note, not a design question — see §5:** this is written as `EnumPolicy[T]` for brevity here, but `controller-gen` (pinned at `sigs.k8s.io/controller-tools v0.16.1`) needs concrete, monomorphized types for CRD/OpenAPI-schema generation, not literal Go generics with type parameters. Every instance above (`LatencySensitivityPolicy`, a `FirmwarePolicy`, etc.) is written out as its own concrete struct when this becomes real code — exactly the same reason `IntRange`/`LongRange`/`ResourceQuantityRange` are three separate structs today instead of one generic `Range[T]`.

### 2.4 `ListPolicy[T]` / `Matcher[T]` — generalizes `ConfigPolicy.ExtraConfig` directly

`ConfigPolicy`'s `VirtualMachineConfigPolicyExtraConfigSpec{Allowed, Denied []VirtualMachineConfigPolicyExtraConfigKey}`, each entry `{Type: MatchType, Key: string}` (`Fixed`/`Regex`/`Glob`), is the first real instance of this shape — just not yet named as a reusable pattern. Generalized:

```go
type ListPolicy[T] struct {
    Entries []T          `json:"entries,omitempty"`  // preset values this class contributes
    Allowed []Matcher[T] `json:"allowed,omitempty"`   // governance: what may appear, class-contributed or VM-inline
    Denied  []Matcher[T] `json:"denied,omitempty"`
}
```

Same monomorphization note as §2.3 applies to any real implementation.

## §3. Why `Allowed` **and** `Denied`, not just one

Confirmed this is a real question, not redundancy — the two express different default postures for anything matched by neither list:

- **`Allowed`-only** is closed-world: anything not explicitly listed is denied. Fits a locked-down namespace that permits only a small, admin-blessed set of keys.
- **`Denied`-only** is open-world: anything not explicitly listed is allowed. Fits `vmclass-v2-design.md` UC3b (`no-guestinfo-extraconfig`) — block one glob pattern, leave everything else unrestricted.
- **Both, at once**, express something neither alone can: a broad rule plus a narrower exception *within* it. `Allowed: ["guestinfo.*"]` plus `Denied: ["guestinfo.secret.*"]` — permit the `guestinfo` namespace generally, carve out one sub-namespace — cannot be written as a single list in either direction without enumerating every individual key on the wrong side of the carve-out, which globs don't let you do.

`ConfigPolicy`'s existing doc comment already states the tie-break this depends on: **"Denied takes precedent over Allowed."** This carries over unchanged to the generalized `ListPolicy[T]` shape — it's what makes the third case above resolve to one answer instead of an ambiguity.

**Yes, this is a subfield structure**, exactly as `ConfigPolicy` already ships it (`Allowed []Matcher`, `Denied []Matcher`), not a new invention — `§2.4` above is a straight generalization of the existing type, not a redesign of it.

## §4. `spec.extraConfig`

1. **`spec.extraConfig` is a bounded, governable fallback, not an escape hatch.** It is typed `[]KeyValuePair` with governable keys (§2.4/§3 above), unlike an arbitrary `ConfigSpec` passthrough. It is permanent by design: vSphere's own VMX key space grows faster than any typed schema tracks it. (The design doc's leftover-only `configSpec` is a separate, preset-only surface; see `vmclass-v2-design.md` §2.1.)
2. **The class-vs-class bounding (`vmclass-v2-design.md` §3.2) covers `extraConfig.entries`, conditioned on `governs`.** The check only fires when a governing class applies to one of the candidate class's zones. Without it, a class author could bypass a governing class's `denied` key rule by baking the denied key into their own preset rather than going through a VM's inline edit.

**Class shape, both roles present:**

```yaml
spec:
  extraConfig:
    entries: [...]   # preset values this class contributes
    allowed: [...]   # governance bound (only load-bearing where a governing class/parentClassRef applies)
    denied: [...]
```

All under the one surviving `VirtualMachineClass` kind — `VirtualMachineConfigPolicy` as a separate CRD is not coming back; only the shape of its `ExtraConfig` types carries forward as plain Go structs.

## §5. Storage/disk range model and other today-configurable fields not sourced from `ConfigSpec`

`vmclass-v2-design.md`'s "Phasing" section flags this as unresolved — checked directly against the current schema, and it's real, plus one more gap found alongside it.

### 5.1 `hardware.instanceStorage` — already configurable today, not from `ConfigSpec` at all

`api/v1alpha6/virtualmachineclass_types.go` already ships `hardware.instanceStorage.{storageClass string, volumes[].size resource.Quantity}` today. This is why it never appeared in the `ConfigSpec`-driven worklist in `vmclass-v2-full-schema.md` — it's a vm-operator-native concept (PVC-backed instance storage), not part of `vim25.types.VirtualMachineConfigSpec` at all, so a survey scoped to `ConfigSpec` fields alone will always miss it.

**Checked for an inline VM-spec twin: none exists.** No `instanceStorage` field appears anywhere on `VirtualMachineSpec`. Per §1's rule, this resolves the "doesn't reduce to `{min,max,default}` cleanly" concern the design doc raised — not by solving a hard list-of-sized-things range shape, but by not needing one yet: **keep `instanceStorage` exactly as it is in v1 today (`entries`-only preset, no governance fields) for Phase 1.** If per-VM instance-storage sizing ever becomes a real inline-editable feature, the range/governance question becomes real then, and gets the same additive-growth treatment as everything else in §2 — not before.

### 5.2 `reservedProfileID`/`reservedSlots` — the doc's own cross-reference doesn't resolve

A reserved profile ID is written into a VM's ExtraConfig (`resourcepool.vmResourceProfileId`), which places the VM in a capacity-reservation slot sized to the class; per-zone counts live in the namespace's `ZoneSpec.vmReservations`. Slot guarantees don't make sense for a class with genuine range spread, so reservations apply only to fixed classes. The validation rules are proposed in `vmclass-v2-design.md` §7.

### 5.3 `maxHardwareVersion` — appears in the design doc's examples, does not exist in the current schema

`vmclass-v2-design.md`'s example YAML (lines 76, 351) shows `maxHardwareVersion: vmx-21` on the class — but grepping `api/v1alpha6/*.go` for hardware-version fields finds **no such field on `VirtualMachineClass` today, in any version.** The only hardware-version field that exists is `VirtualMachineSpec.MinHardwareVersion int32` — inline-editable on the VM directly, with no class-level ceiling at all today.

This is a real, newly-identified field, not previously in the `ConfigSpec`-sourced worklist (hardware-version upgrades are a distinct vCenter operation, `UpgradeVM_Task`, not a `ConfigSpec` field — this is why `vmclass-v2-full-schema.md` correctly separates `ScheduledHardwareUpgradeInfo`, deferred, from this ceiling, which it assumes already exists). Given `VirtualMachineSpec.MinHardwareVersion` is a confirmed inline twin, this fits §1's rule directly — it needs a real `IntRange`-shaped (or, since the VM only ever sets a *minimum*, a `Max`-only-populated) field on the class, asymmetric with the VM's own `Min`-only field. **This needs to be added to the schema, not just kept in example YAML** — flagging as a concrete gap for whoever does the Phase 1 implementation pass.

## §6. `EnumPolicy`/`ListPolicy`/generics and `controller-gen` — the plan

**Plan, concretely:**
1. Every governed-value type ships as a concrete, named Go struct — never a literal Go generic with a type parameter — matching the existing `IntRange`/`LongRange`/`ResourceQuantityRange` precedent (three concrete types, not one `Range[T]`). `EnumPolicy[T]`/`ListPolicy[T]`/`Matcher[T]` in §2 above are notation for this document only, not a proposal to write generic Go for the CRD types.
2. Before writing the real schema, run a small spike: define one trial type using `sigs.k8s.io/controller-tools v0.16.1` (the version this repo already pins, `hack/tools/go.mod`) with a `+kubebuilder:validation` marker on a generic-looking field, and run `make generate-manifests` against it, to confirm current behavior rather than relying on general recollection of `controller-gen`'s generics support (which has shifted across versions and has known caveats with markers/`oneOf` schemas). This is a cheap, half-day check, not a blocker to starting the rest of the schema work.
3. Regardless of the spike's outcome, default to concrete types for the CRD-facing schema. If the spike confirms clean generic support, that's an option for an *internal* implementation-only refactor later (e.g., shared validation/defaulting logic parameterized over the concrete types) — never a reason to expose a generic type parameter in the public API surface itself.

## §7. `ConfigSpec` real-world usage — findings, not a full survey

`vmclass-v2-design.md` §6.2 calls for "a survey of real-world `configSpec` usage" to decide which leftover fields to elevate first. From this repo, the best available proxy is `docs/concepts/workloads/vm-class.md`'s documented `configSpec` examples, plus the shipped class fixtures:

- **Shipped fixtures (`config/virtualmachineclasses/*.yaml`) set no `configSpec` at all** — every t-shirt-size example (`guaranteed-large`, `best-effort-*`, etc.) only ever sets `hardware.{cpus,memory}` and `policies.resources.requests.{cpu,memory}`. Zero evidence of `configSpec` usage in-repo beyond documentation.
- **`docs/concepts/workloads/vm-class.md`'s documented examples use exactly:** `numCPUs`, `memoryMB`, `firmware` (`"efi"`), `extraConfig` (as `[]OptionValue`), and `deviceChange` for two device kinds — vGPU (`VirtualPCIPassthroughVmiopBackingInfo`, e.g. `vgpu: "grid_v100d-4q"`) and Dynamic DirectPath I/O (`VirtualPCIPassthroughDynamicBackingInfo` with `allowedDevice[].{vendorId,deviceId}`). All five are already accounted for in `vmclass-v2-full-schema.md`.
- **One finding worth surfacing directly:** the documented "large, NUMA-tuned" example (`vm-class.md`, ~line 352) sets `numa.nodeAffinity` and `numa.vcpu.preferHT` **through `extraConfig`**, not through any first-class field — i.e., there is a real, documented, customer-facing use case for NUMA tuning today, and it's being done entirely via the `extraConfig` long-tail fallback rather than a typed field. `VirtualMachineSpec` already has a first-class inline twin for this on the VM side (`spec.pnumaNodeAffinity []int32`), so per §1's rule this is a real elevation candidate for v2 (`hardware.pnumaNodeAffinity` or similar), not something to leave permanently in `extraConfig` just because that's how it's done today.

**Caveat, stated plainly:** this is documentation- and fixture-level evidence from this checkout, not the production-usage telemetry `configSpec` survey §6.2 actually calls for (real, currently-deployed customer classes' `configSpec` content, which lives in wcpsvc/VC data this session has no access to). Treat the findings above as "what's demonstrated as supported and documented," not as "everything real customers currently rely on" — the real survey against production data should still happen before finalizing which fields need day-one typed coverage versus can stay in the leftover `configSpec`.

---

## Appendix: earlier field-by-field tables (superseded by `vmclass-v2-full-schema.md`)

The tables below were produced early in this discussion, before `vmclass-v2-full-schema.md`'s more rigorous field-by-field worklist (documented-usage-sourced, `Elevate`/`Defer`/`Never` calls, explicit `ConfigPolicy`-capability-vs-`ConfigSpec`-request reconciliation in its §2) was found to already exist in this spec directory. They're included here per request, as the raw first-pass input, **not** as a second authoritative table — where they disagree with `vmclass-v2-full-schema.md`, that document wins. Notable disagreements worth flagging explicitly:

- This appendix originally proposed `Range` treatment for `MaxMksConnections`/`SgxInfo.epcSize`/`NetworkShaper`; `vmclass-v2-full-schema.md` marks these `Defer`/`Never` respectively. §2.1's additive-growth rule (`{default}`-only now) reconciles both: whenever any of these does get elevated, ship it `{default}`-only first regardless of which of the two tables' priority call is followed.
- This appendix marked `SimultaneousThreads` as an independent range; `vmclass-v2-full-schema.md` correctly identifies it as **merged** with `ConfigPolicy`'s `NumSimultaneousThreads` capability field, following the pattern in that document's §2 (capability vs. request, not two independent elevations) — the more accurate call.
- Neither table originally caught `instanceStorage`'s existing configurability or the `maxHardwareVersion` gap (§5 above) — both are `VirtualMachineClass`-native or VM-side fields outside `ConfigSpec`'s scope, which a `ConfigSpec`-driven survey (both tables' starting point) structurally cannot surface.

### First-pass table (by category)

| ConfigSpec field | Type | Proposed VM Class field | Range? | Notes |
|---|---|---|---|---|
| `NumCPUs` | `int32` | `hardware.cpus` | Yes → `IntRange` | Superseded: see `vmclass-v2-full-schema.md` row `NumCPUs`. |
| `NumCoresPerSocket` | `*int32` | `hardware.coresPerSocket` | Yes → `IntRange` | Confirmed consistent with `vmclass-v2-full-schema.md`. |
| `SimultaneousThreads` | `int32` | `hardware.numSimultaneousThreads` | Yes → `IntRange` | **Revise**: merge with `ConfigPolicy.NumSimultaneousThreads`, not an independent field — see `vmclass-v2-full-schema.md` §2. |
| `MemoryMB` | `int64` | `hardware.memory` | Yes → `ResourceQuantityRange` | Confirmed consistent. |
| `MemoryHotAddEnabled`/`CpuHotAddEnabled`/`CpuHotRemoveEnabled` | `*bool` | `hardware.*Enabled` | No (→ `BoolPolicy`, §2.2) | Confirmed consistent, refined to `BoolPolicy` shape. |
| `CpuAllocation`/`MemoryAllocation` | `*ResourceAllocationInfo` | `policies.resources.{requests,limits}` | Yes → `ResourceQuantityRange` | Confirmed consistent. |
| `MaxMksConnections` | `int32` | `policies.maxMksConnections` | **Revised**: `{default}`-only now, not full `Range` | See §2.1's additive-growth rule. |
| `SgxInfo.epcSize` | — | `hardware.sgxInfo` | **Revised**: `{default}`-only now | Same reasoning. |
| `NetworkShaper` | — | — | **Revised**: `Never`, per `vmclass-v2-full-schema.md` (per-NIC config, not class-level) | Original appendix proposal was wrong; superseded. |
| `Firmware` | `string` | `hardware.firmware` | No (→ `EnumPolicy`, §2.3, `{default}`-only) | Confirmed consistent. |
| `SevEnabled`/`SevSnpEnabled`/`TdxEnabled` | `*bool` | `hardware.*Enabled` | No (→ `BoolPolicy`, merged with `ConfigPolicy` capability fields) | Confirmed consistent with `vmclass-v2-full-schema.md` §2's merge pattern. |
| `ExtraConfig` | `[]BaseOptionValue` | `spec.extraConfig.{entries,allowed,denied}` | No (→ `ListPolicy`, §2.4) | Refined per §3/§4 above. |

*(The full 86-field table from the original pass is superseded in its entirety by `vmclass-v2-full-schema.md`'s field-by-field table and is not reproduced here — see that document for the authoritative per-field call.)*

---

— Faisal + Claude
