# Design: VirtualMachineClass v2 — Constraint, Default, and Governance

- **Status**: Draft design
- **Summary**: `VirtualMachineConfigPolicy` is **dropped entirely**. `VirtualMachineClass` v2 supersedes both the current `VirtualMachineClass` and `VirtualMachineConfigPolicy`. There is one kind. Governance (`governs`), identity partitioning (`spec.externalID`), and per-governing-class drift handling (`governs.existingVMs`) are additive properties of that one kind, not separate objects, and ship in **Phase 1 (release 9.1.3)**. Hierarchical governance (`parentClassRefs`, tenant-based VM classes) is the same kind of additive property, but ships in **Phase 2 (9.2)** — see "Phasing" below.
- **Companion docs**: [`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) (the vCenter side: vcdb storage, the vAPI surface, the build-time vmodl generator, the CRD writers); [`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md) (the value shapes); [`vmclass-v2-full-schema.md`](./vmclass-v2-full-schema.md) (field-by-field elevation); [`research-vmclass-as-policy.md`](./research-vmclass-as-policy.md) (evidence, alternatives, and design history).
- **Audience**: principal engineers, architecture review

This document describes the single-kind design: what the new `VirtualMachineClass` looks like field-by-field, how it takes over what `VirtualMachineConfigPolicy` did without a second kind, how zone-specific hardware is handled, how classes authored at different levels of authority can be bounded by each other, and how existing classes migrate. Open items are in §7; a summary of the key decisions is in §9.

**Why one kind is cheap to adopt now:** `VirtualMachineConfigPolicy`'s enforcement controller and admission webhook are **not on `main`** — only the `Zone`-driven fan-out that creates the (currently empty-ish) object exists. Dropping the kind removes almost no shipped behavior. (The two-kind alternatives are compared in the research document, §4 and Appendix A.)

**Assumption this design depends on, outside its own control:** it assumes VCF Automation's existing, separate VAPI-based hardware-policy construct is retired in favor of this model. If that construct ships anyway, both it and this design's `governs` mechanism exist side by side, which reproduces the exact duplication problem this design exists to eliminate. This is called out explicitly because it is not a decision this document, or vm-operator, can make unilaterally.

---

## Phasing: what ships when

**Phase 1 — release 9.1.3.** Everything in §1–§7 except where a section explicitly says otherwise:
- The range/constraint schema (§2), `governs` and its zone/enforcement/drift semantics (§3), `spec.externalID` for VC-side identity partitioning (§4), the use cases (§5), and the migration plan for existing classes (§6).
- The v2 VM class vAPI, vcdb storage and CRD-writer changes, designed in [`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md).
- Storage/disk-size controls: `instanceStorage` stays as in v1 for Phase 1 ([`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md) §5.1). The `reservedProfileID`/`reservedSlots` rules are an open item (§7).

**Phase 2 — release 9.2.** Hierarchical governance via `parentClassRefs` (§8) — the mechanism that lets one class bound what another class in the same namespace is allowed to declare, which is what makes tenant-based VM classes possible. The mechanism is worked out here at the same rigor as Phase 1, because the schema choices in §2 and §4 (keeping `parentClassRefs` list-typed, keeping identity partitioning on a separate field from hierarchy) are made so Phase 2 needs no breaking change. Nothing in Phase 1 depends on §8; nothing in §8 depends on anything not already shipped in Phase 1.

Everywhere below, a cross-reference into §8 is a Phase 2 mechanism; everything else referenced is Phase 1.

---

## 1. Two independent properties on one kind

A class's **availability** (`spec.zones`, §3.1) is the base: the zones of a namespace where the class applies at all. Within its available zones, a class can play two independent roles, either or both:

- **Selectable** — a VM in one of those zones can reference this class by name in `spec.className`.
- **Governing** — the class's constraints act as a ceiling on VMs in those zones (or a subset of them, `governs.zones`), regardless of what the VMs reference. Expressed by an optional `spec.governs` block.

Availability therefore limits both roles: a class can't be selected, and can't govern, in a zone it isn't available in.

`large` can be an ordinary, selectable t-shirt size *and* the ceiling every VM in a zone is measured against — not through a special mode, and not through a second object, but because it additionally sets `spec.governs`, while remaining exactly as selectable as `small` and `medium`, which do not set it.

**Why not a `role` field (research document, Appendix A2):** `spec.governs` is additive, not a discriminator. Its presence does not change which other fields on the object are valid, does not invert what "this class exists in the namespace" means for assignment (it still just means "usable," now in either or both senses), and does not make any part of the schema meaningless for a given instance the way a `role: Preset | Policy` split would. A class with `governs` set is an ordinary class that additionally acts as a ceiling; nothing about its `hardware` or `policies` fields changes shape or meaning.

---

## 2. `VirtualMachineClass` v2

### 2.1 Shape

Every previously-scalar field becomes a constraint with three parts, following the defaulting rule from the research document (§5.2): `max` absent ⇒ `max = min`; `default` absent ⇒ `default = min`. A class that sets only `min` on every field is, byte for byte, today's fixed t-shirt size. If `min` or `max` is set, `default` is required — given explicitly, or filled with `min`; only a wholly absent struct defers to vpxd's own default.

```yaml
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata:
  name: large
  namespace: team-a
spec:
  description: "Large general-purpose"
  externalID: ""                # optional; opaque VC-side object identity, immutable after create — see §4. Carries no hierarchy meaning.
  zones: [zone-a]               # optional; unset = all namespace zones incl. future ones, [] = none, list = only these — see §3.1.
  hardware:
    cpus:
      min: 8            # max/default omitted -> both equal 8
    memory:
      min: 32Gi
    devices:
      vgpuDevices:
        allowed: ["grid_v100d-4q", "grid_v100d-8q"]
  policies:
    resources:
      requests:
        cpu: {min: 8000m}
        memory: {min: 32Gi}
  extraConfig:
    denied:
      - {type: Glob, key: "guestinfo.*"}
  maxHardwareVersion: vmx-21
  configSpec: {}           # optional; only fields with no typed home; presets only, never governance — see below
  reservedProfileID: ""    # only valid for a fixed class; rules are an open item (§7)
  reservedSlots: 0
  parentClassRefs: []          # optional, Phase 2 (9.2) — see §8. Not shipped in Phase 1; this class is a root, has none regardless.
  governs:                    # optional; entirely absent on a purely selectable class such as "medium"
    enforcement: Deny          # see §3.3 for the combination rule when several governing classes apply
    existingVMs: AllowOnViolation   # AllowOnViolation | DenyPowerOn; see §3.3a
status:
  zones: [zone-a]              # computed: subset of spec.zones actually hardware-compatible right now
  conditions: []
  observedGeneration: 0
```

**`configSpec` holds only fields with no typed home.** It carries `VirtualMachineConfigSpec` fields that vpxd supports but that do not yet have a typed field in this schema, with these rules:

- **Presets and defaults only, never governance.** Its contents are applied to VMs that select the class; a governing class's `configSpec` is not a ceiling and bounds nothing. This follows from [`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md) §1: a field needs governance only if a VM can set it inline, and every field left in the blob has, by definition, no typed field and no inline counterpart.
- **Typed fields always win, and the blob is never merged with them.** Elevated field paths are not allowed in `configSpec`. The check is per path: `bootOptions.firmware` is elevated while the rest of `bootOptions` is not; `deviceChange` is elevated per device kind. What happens when an elevated path arrives anyway is open (§7).
- **Each later elevation is a small migration.** Existing blob entries for the newly elevated field move to the typed field, and the path stops being accepted in the blob.
- `json.RawMessage` in the CRD, `@Vmodl1Type("VirtualMachineConfigSpec") Optional<DynamicStructure>` in the v2 vAPI ([`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) §4.6).

**Why not drop `configSpec` entirely:** every existing class that sets a field with no typed equivalent would have no v2 representation, so migration would have to either break those classes or block on elevating every field first; and a field vpxd adds in a new release would be unusable until this schema elevates it. A leftover-only blob avoids both, while keeping it out of governance.

### 2.2 What is deliberately absent

- **No `role` or `mode` discriminator.** `governs` is additive (§1), not a switch between two schemas.
- **No unbounded escape hatch.** There is no passthrough of an entire, unvalidated `ConfigSpec`. The leftover-only `configSpec` (§2.1) is a bounded preset surface that can never carry an elevated field and never acts as a ceiling. The other fallback, `spec.extraConfig`, is a different kind of thing: a typed key/value list whose *keys* are governable (`allowed`/`denied`, with `Fixed`/`Regex`/`Glob` matching — see [`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md)), the same way every other field here is governable. It exists because vSphere's own VMX key space grows faster than any typed schema can track — a permanent, bounded design choice, not a gap.
- **No separate `VirtualMachineConfigPolicy` kind.** Everything it would have carried is expressed by `governs` on one or more ordinary `VirtualMachineClass` objects (§3).
- **No merged zones field.** `spec.zones` (availability) and `governs.zones` (governance scope) are two fields — see §3.1 for the case that requires the split.

---

## 3. `spec.governs`: how one kind takes over what the policy did

### 3.1 Shape and semantics

`spec.zones` is top-level, not nested under `governs`, because it is meaningful with or without `governs`: it answers "where does this class apply at all" — where it can be selected, and the outer bound of where it can govern.

```yaml
zones: [zone-a, zone-b]   # optional; unset = all namespace zones incl. future ones, [] = none, list = only these
governs:
  zones: [zone-a]           # optional; unset = same as spec.zones; [] = none; list = only these (must be within spec.zones)
  enforcement: Deny          # open question in the research doc (§7 Q10); shown here for completeness
  existingVMs: AllowOnViolation   # see §3.3a
```

**Both zone fields have three states:**

| Value | `spec.zones` (availability) | `spec.governs.zones` (governance) |
|---|---|---|
| unset | Available in every zone of the namespace, **including zones added later** | Governs exactly where the class is available (follows `spec.zones`, including its "all zones, live" meaning) |
| `[]` | Available nowhere: attached to the namespace, but no new VM can select it and it governs nothing | Governs nothing |
| `[a, b]` | Only those zones | Only those zones, which must all be zones the class is available in |

**A class can only govern where it is available.** `governs.zones` is always within `spec.zones`: unset means "the same as `spec.zones`", and a list is validated to be a subset of the zones the class is available in. So `spec.zones: []` with `governs: {}` governs nothing.

**Unset means "all zones, live":** a class with unset zones starts applying in a zone added to the namespace later, without an edit to the class. This is intended. Adding a zone to a namespace is itself an explicit admin action, on the namespace — not the ambient, computed state §3.1a keeps out of governance (hardware compatibility changing with no admin action). It is also exactly how classes behave today, which is why the upgrade needs no zone backfill (§6.1a). VM admission re-derives governance live on every check (§3.2), so a zone joining the namespace changes which checks apply, not whether they are sound.

**Why not require an explicit list:** it would force an admin to edit every class each time a zone is added to a namespace, would need a backfill of every existing class on upgrade, and could not express "none" separately from "not yet configured".

**Where the value comes from:** the zones are part of the class's association with a namespace, not of the class definition in vCenter (a vCenter class is shared by many namespaces). The writer materializing the class into a namespace sets `spec.zones` and `spec.governs.zones` from that association; see [`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) §4.4.

**Unset and `[]` must stay distinct at every layer.** The Go type is `*[]string`, not `[]string`: apimachinery's `equality.Semantic.DeepEqual` treats a nil and an empty list as equal, and `controllerutil.CreateOrUpdate` skips the write when objects are `DeepEqual`, so with a plain slice a change between "all" and "none" would silently never be written. The remaining risk is a client that silently drops `[]`, which turns "none" into "all" — fail-open for availability. It is assessed as medium-low, with mitigations, in [`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) §4.5.

- **`spec.zones` set** — an admin-declared restriction: the class may only be used in these zones (or, with `[]`, nowhere). Nothing about it is computed or reconciled.
- **`governs` absent** — the class is purely selectable, exactly like `small` and `medium` in UC1/UC2 below. This is what every migrated class looks like on day one (§6).
- **`governs` present, `governs.zones` unset** — the class governs everywhere it is available, i.e. exactly `spec.zones`. This is the common case and every use case below uses it. It is a sibling field on the same object, set in the same write and validated by the same webhook call, so there is no drift vector, and a t-shirt-size-with-a-ceiling (`large` setting `zones` and `governs: {}`) doesn't restate its zone list. `governs: {}` is never a no-op unless the class is available nowhere.
- **`governs.zones` set** — a narrower governance scope than availability: the class is selectable across all of `spec.zones`, but only acts as a ceiling in this subset. Validated as a subset of `spec.zones` at admission.

**Why not let a class govern zones it isn't available in:** governance would then be detached from anything a user can see about the class in that zone, and a governance-only class would need no availability at all — which is the separate "policy" kind this design removes.

Two zones fields, not one, because the discriminating case is real: a class can legitimately be selectable in more zones than it governs. With one field, that case is inexpressible without splitting the class in two.

**Worked example.** `flex` is selectable across all three of a namespace's zones, but only meant to act as a ceiling in `zone-a` — `zone-b` and `zone-c` have their own, separately-declared governing classes:

```yaml
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata: {name: flex, namespace: team-a}
spec:
  hardware: {cpus: {min: 2, max: 16}, memory: {min: 4Gi, max: 64Gi}}
  zones: [zone-a, zone-b, zone-c]   # selectable in all three
  governs:
    zones: [zone-a]                 # but only a ceiling in zone-a
    enforcement: Deny
```

A VM in `zone-b` or `zone-c` can still set `className: flex` and get its hardware defaults — `flex` just isn't the governing ceiling there; whatever other class does that job for those zones applies instead. A VM in `zone-a` gets both: `flex`'s own values if selected, and `flex`'s ranges as the ceiling regardless of what it selected.

#### 3.1a `status.zones` is feasibility, and never feeds governance

`status.zones` is computed — the subset of the class's available zones that `ConfigTarget` currently reports as hardware-compatible with this class's constraints. It reconciles as cluster hardware changes.

It must never be read by the governance check in §3.2. If it were, a zone gaining compatible hardware would silently start being governed by a ceiling no admin turned on there, and a zone losing hardware would silently drop out of governance — a ceiling disappearing with no admin action and no event. Governance scope comes from `spec.zones`/`governs.zones`, which change only through an admin action (on the class or its namespace association, or by adding a zone to the namespace). `status.zones` answers "can this run here," never "is this permitted here" — see also §3.4.

### 3.1b Automatic zone placement: how an unspecified VM zone stays governance-compliant

`VirtualMachine.spec.zone` (backed by the `topology.kubernetes.io/zone` label) is optional — a VM can be created with no zone at all, with the concrete zone decided later. This section answers what governs a VM that has not yet been placed (the research document's open question at `research-vmclass-as-policy.md:224`), grounded in how zone placement works in this repo today.

**Zone specified.** `doesVMNeedPlacement` (`pkg/providers/vsphere/placement/zone_placement.go`) returns early with the zone already fixed, and placement operates only within that zone's clusters and hosts. Validate directly against that zone's applicable governing class(es) and `ConfigTarget` feasibility — the existing model, unchanged.

**Zone unspecified.** Two things make this sound:

- **Feasibility is already safe, because admission and placement share the same source of truth.** `ConfigTarget` — the same data `status.zones` (§3.1a) is computed from — is what placement's own candidate search is grounded in too. A zone that's hardware-infeasible for the VM's requested configuration was never going to be a placement candidate; nothing new is needed on this axis.
- **Governance is deliberately invisible to placement.** `governs` ranges have no representation in `ConfigTarget` or anything else placement consults, and that stays true. Placement — for a single VM via `Constraints.Zones`, or for a group via `preferredZoneName`/`GroupPlacement` — behaves exactly as it does today. Governance is a **detection-and-enforcement layer on top of whatever placement decides**, not a filter placement consults while deciding. This is simpler than threading a new constraint through placement, and single-VM and group-VM placement need no separate treatment.

**Admission's existential check, precisely:** when `spec.zone` is unset, a VM is admitted if **at least one** zone the namespace touches has an applicable governing class (or no governing class at all, per §3.2a) whose ranges the VM's effective configuration satisfies, and is `ConfigTarget`-feasible. This is deliberately existential ("at least one"), not universal ("all") — it is a **fail-fast diagnostic only**: it rejects a VM up front when it's already certain that no zone could ever admit it. It does not try to predict or constrain which zone placement will choose. A VM that is part of a group gets exactly this same per-VM check, independently; nested groups need no special handling.

**Once admitted, whatever zone placement picks is validated afterward, by the reconciler, using the same check admission used.** The governance-compliance check (does this VM's effective configuration, in its now-known zone, satisfy its applicable governing class(es)) is one function, exported from `pkg/` so it is callable from both the admission webhook's validator and the VM reconciler. The reconciler re-runs it on every pass once a zone is known (whether the VM was placed individually or as part of a group), and reacts as `governs.existingVMs` (§3.3a) specifies: `AllowOnViolation` surfaces a status condition and proceeds; `DenyPowerOn` withholds the power-state-reconcile step (the "Reconcile power state" step in the VM's reconcile order — see `operator-best-practices.md`) until compliant. Because enforcement happens at the reconcile-level power-on step, there is no distinction between a VM's first power-on and any later one. See §3.3a for the resulting carve-out to the "new objects are never grandfathered" rule.

**Groups need no separate design or implementation from the single-VM path.** Because governance is a post-placement detection-and-enforcement layer, group membership is irrelevant to it: each VM's compliance is evaluated from its own class and its own zone, regardless of how it got that zone. `PlaceVirtualMachineGroup`'s single `preferredZoneName`, and the fact that group members can land in different zones (`GroupPlacement`'s doc comment), are not concerns this mechanism needs to touch.

**Whatever ends up non-compliant resolves through the drift mechanism.** Whether a VM ends up in a non-compliant zone because a governing class was edited after the fact, or because placement (unaware of governance, by design) chose a zone that turns out non-compliant, the object is now an already-admitted, currently-non-compliant one — exactly what `governs.existingVMs` (§3.3a) handles, enforced by the reconciler. An already-assigned zone label continues to hard-filter placement exactly as it does today, unaffected by governance.

### 3.2 Finding the effective ceiling for a VM: per-field, not intersected

**Merging ranges field-by-field across multiple governing classes does not work.** Two classes cannot be combined by taking hardware bounds from one and CPU-reservation bounds from the other — there is no principled way to intersect two independently-authored objects into a single effective range once they disagree, or even once they cover different fields, without an arbitrary rule for which one wins. The model is **admit if any single applicable governing class matches**, not "intersect every applicable class's ranges into one merged ceiling."

Concretely: for each field the VM's effective configuration sets, find the governing class (if any) that constrains that field for the VM's zone, and check the VM's value against that one class's range for that field. A VM can be bound by `large`'s `hardware.cpus` range and, simultaneously, by `no-guestinfo-extraconfig`'s `extraConfig.denied` rule (UC3b) — two governing classes, each owning a disjoint field, neither one's ranges merged with the other's.

**Phase 1 keeps this simple by preventing overlap up front**, since the common case — one governing class per zone — needs no per-field bookkeeping at all:

- **At class admission**, creating or updating a `VirtualMachineClass` with `governs` set is rejected if it would constrain a field that another governing class already constrains for an overlapping zone. This catches the common mistake (two classes both setting `hardware.cpus` for `zone-a`) at the point an admin makes it.
- **Class-time validation alone is not sufficient**, and the VM-admission check is the real backstop: a class can be edited later to add a field another governing class already owns; a class with unset zones starts governing a zone newly added to the namespace with no class edit and no class-webhook call (§3.1); and a VM created between such a change and any subsequent reconcile still needs its own check, since the class webhook only validates the class being written. So **VM admission always re-derives, per field, which governing class(es) apply**, and if two applicable governing classes constrain the *same* field for the VM's effective configuration, the VM is rejected with an explicit conflict message naming both classes — the empty-intersection-style diagnostic the research document's §5.3 calls for, here as "these two classes conflict on this field."
- This is what "one governing class per zone" (the Phase 1 simplification) means precisely: not a hard cap enforced by counting, but a consequence of the no-overlapping-fields rule, in the common case where one class sets all of a VM's governed fields.

**This generalizes to `governs.match` later** (a future workload-shape-based extension): a class matching hardware-A workloads and a class matching hardware-B workloads apply to disjoint VMs by construction, so the same no-overlap invariant holds — `match` partitions *which VMs* a class applies to; the field-per-VM invariant is unaffected.

`vmClassMode` and `syncMode` do not exist in this model. The ceiling always applies to a VM's effective configuration, whatever combination of class-derived and inline values that resolves to; nothing here is controller-written, since `ConfigTarget` feasibility is checked independently (§3.4), not mirrored into any spec.

**The same check also bounds other classes in the same namespace, not only VMs.** When a new or edited `VirtualMachineClass` is admitted, if any existing governing class in that namespace already applies to one of its zones, the candidate class's own declared ranges must fall within that governing class's ranges for the shared field(s) and zone(s) — the identical per-field subset comparison, applied to a class object instead of a VM. This makes a "flex" class the guardrail not just for free-form VMs but for every other class an admin defines alongside it, without a new mechanism. Cross-*namespace* bounding (a provider-authored class constraining a tenant-authored class in a different namespace) is a different boundary — see §8 (**Phase 2**).

This extends to `spec.extraConfig.entries` (see [`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md)): a candidate class's literal entries are checked against an applicable governing class's `extraConfig.allowed`/`denied` rules the same way its numeric ranges are checked. Without this, a class author could bake a key a governing class denies directly into their own `entries`, bypassing the rule by never going through a VM's inline edit path. As with everything in this paragraph, it only applies when a governing class exists and its zones overlap the candidate's.

**Exception to the same-field-conflict rule, Phase 2 only: a class and any class it lists in `parentClassRefs` (§8) may both govern the same field for overlapping zones.** This is not the ambiguous case the conflict check exists to catch, because §8.4's subset validation guarantees the child's declared range is nested inside the parent's for any field both declare — they are provably ordered. For a field the child declares, checking a VM against the child (the more specific ceiling) is sufficient. For a field the child is silent on, §8.3's resolution rule applies: the parent's declaration is the effective ceiling. Either way there is exactly one authoritative range per field. See §8.3a for the worked case. **In Phase 1, two governing classes with an overlapping field are always a conflict.**

### 3.2a Classless VMs never get `className` filled in

A VM may omit `spec.className` entirely and configure hardware inline. When a governing class supplies its ceiling, `spec.className` is **not** written to record that — the VM stays classless in spec. Which governing class(es) applied is recorded only in status (§3.5). This avoids silently mutating a field the user chose to leave unset, and avoids pinning a classless VM to a governance-only class's `ClassInstance` lineage (§3.7), which would make that instance load-bearing instead of the harmless bookkeeping it is today.

**Admission invariant:** a VM must have either `spec.className` set, or at least one governing class that covers the VM's zone — the specific zone if `spec.zone` is set, or at least one zone the namespace touches otherwise (§3.1b's existential check). A classless VM with no applicable governing class anywhere is rejected outright (UC9), the same way a non-existent `className` reference is rejected today.

**Deleting a governing class is allowed.** Its ceiling simply stops applying to new admissions and to the reconciler's compliance check; any §3.5 condition it caused on a VM must be cleared.

### 3.2b Class-vs-class drift: a governing class can tighten out from under an already-validated class

The class-vs-class subset check (§3.2) only runs when the narrower candidate class itself is created or edited. It does not revisit an existing class when a *different* class — its governor — is edited afterward and tightens. A t-shirt class `large` (`cpus: 4-8`) can validate cleanly against a governing class `flex` (`cpus: 2-16`), and then `flex` gets edited down to `cpus: 2-4` — nothing re-checks `large` at that point, even though it is no longer a subset of its governor.

**This is not a VM-safety gap.** VM admission always re-derives governance live, checking a VM's effective value directly against whichever governing class currently applies — never against the selectable class's own declared range as a proxy. A VM requesting `cpus: 8` via `large` after `flex` tightens is correctly rejected against `flex`'s current range.

**The gap is that `large` itself becomes misleading.** Its spec still advertises `cpus: 4-8`, and nothing on the object says that range is no longer honored by its governor.

**The existing `VirtualMachineClass` controller re-runs the same per-field subset check on every reconcile, and surfaces a status condition when a class no longer nests inside its governor(s).** Reconciliation re-checks something the webhook can only validate at the moment of a specific edit, and makes the mismatch visible. There is no enforcement lever here: the class is not deleted, blocked, or prevented from being referenced. The condition is purely discoverability.

### 3.3 Enforcement combination rule

Because more than one governing class can apply to the same VM, and each carries its own `governs.enforcement`, a combination rule is needed: **`Deny` wins if any applicable governing class sets it.** An `Allow` on one governing class must never override a `Deny` an admin placed on another.

### 3.3a Existing-VM drift policy (`governs.existingVMs`)

When a governing class's ceiling tightens (a range narrows, or a new governing class starts applying), some already-admitted VMs may fall outside the new ceiling. Customer feedback on the right behavior is split, so this is a per-governing-class knob:

- **`AllowOnViolation`** (default) — the VM is left alone. It keeps running, and a power cycle (Off → On) succeeds normally. Non-compliance is surfaced as a status condition, not remediated.
- **`DenyPowerOn`** — vm-operator never initiates a power state change for compliance reasons. This value only declines a **user-requested** transition to `PoweredOn` while the VM is out of compliance with an applicable governing class; the request is rejected the same way any other invalid `spec.powerState` edit is rejected today. A VM that is already `PoweredOn` when the ceiling tightens keeps running, untouched, until whoever operates it next asks to power it off and back on.

**New** objects — VMs, or narrower classes under §3.2's or (Phase 2) §8.4's subset checks — are never grandfathered: an object violating an applicable ceiling at creation time is rejected at admission. `existingVMs` only governs objects that were compliant when admitted and later fall out of compliance because a ceiling tightened around them.

**Carve-out for the zone dimension (§3.1b):** "never grandfathered" only holds when the applicable ceiling is knowable at creation time. For a VM created with no zone, it isn't — admission can only run the existential check. If the zone placement later assigns turns out non-compliant, that VM is *not* rejected retroactively; it is treated exactly like drift, using this same `existingVMs` knob, enforced by the reconciler.

**Combination rule, same shape as §3.3:** when multiple applicable governing classes disagree, the strictest wins — `DenyPowerOn` overrides `AllowOnViolation`.

**This covers drift after a VM's zone is assigned as well.** `governs` is re-derived live at every admission (§3.2), and the reconciler re-runs the compliance check on every pass (§3.1b), so the drift policy applies identically whether the VM has had a zone since creation or only since placement assigned one. There is one drift mechanism.

### 3.4 Hardware feasibility is unrelated to governance, and always checked

Whether a zone's hardware can satisfy a class's constraints (a requested vGPU profile exists on that cluster, the hardware-version ceiling is achievable) is answered by checking against `ConfigTarget.status` at admission — regardless of whether the class governs anything. This admission-time validation against `ConfigTarget`, `VirtualMachineConfigOptions` and guest OS options already exists and is separate from this design. Governance answers *"is this permitted here,"* not *"is this possible here."* Both checks run; neither substitutes for the other. A class requesting a GPU profile only present in one zone fails the `ConfigTarget` check anywhere else, whether or not `governs` mentions that zone.

**Feasibility needs no new mechanism for automatic zone placement (§3.1b)**, because admission and placement share the same `ConfigTarget`-derived source of truth.

### 3.4a Visibility: status and rejection messages never exceed what a class already declared

A tenant able to read a `VirtualMachineClass` must not be able to infer cluster hardware capability the class itself doesn't already expose. Two places this can leak, with the same rule for both:

- **Status.** `status.zones` (§3.1a) reports only which zones are currently usable — a boolean-shaped signal per zone, not a copy of any `ConfigTarget` field. A class's status never carries a hardware value its own `spec` did not already declare.
- **Admission/validation error messages.** A rejection worded as `"zone-b lacks profile nvidia-a100-40c"` discloses the same capability information a status field would. One consistent rule applies:
  - **Generic about any capability or value the caller did not supply** — `ConfigTarget` data, another class's actual range values (§3.2, §8) — regardless of who is asking: `"the requested vGPU profile is not available in zone-b"`, not naming what zone-b has instead; `"requested cpus (8) exceeds an applicable governing class's max"`, not naming the governing class's other fields.
  - **Fully specific about the caller's own input** — the field they set and the value they requested: `"requested cpus (8) exceeds class large's max (8)"` is fine when `large` is the caller's own selected class.

**Why not vary the detail by caller (a `SubjectAccessReview` against whatever the message would name):** a caller-dependent message is one more thing to get wrong, and the plain rule is enough to stay actionable.

This applies equally to the hierarchical-governance check in §8 (**Phase 2**): a class rejected for exceeding the range of a class it lists in `parentClassRefs` gets pass/fail and which of its own fields is out of range, never that class's actual range values.

### 3.5 Discoverability: the cost of one kind

With a separate policy object, "what governs zone-a" would be one object to read. With one kind, it is a filter across every class in the namespace — an auditability cost, and the one place a single kind is worse than two. Two questions need two answers:

- **"Why was *this* VM constrained this way?"** — `status.governingClassNames []string` on the VM, computed live, plus a condition naming which field actually bound its effective configuration, so a rejection or a near-limit VM is explainable without a namespace-wide scan.
- **"What governs zone-a, independent of any particular VM?"** — `status.governingClasses` on the `Zone` object (`external/tanzu-topology`): a **list** of class names (a list even in Phase 1, where it will typically hold one entry) that a controller computes by watching `VirtualMachineClass` changes in namespaces that touch that zone, published as a read-only mirror. The classes' own `spec.governs`/`spec.zones` remain the only source of truth; §3.2's check must never read the mirror back (same reasoning as §3.1a: a computed mirror must never become an input to the thing it mirrors).

UIs and CLIs should use these two views rather than an ad-hoc namespace-wide scan. Neither is a new stored *policy* object; both are read-only projections of the classes.

### 3.6 Governance-only classes in class choosers

A class that exists purely to govern (never intended to be selected — `ceiling-only` in UC3 below) is namespace-scoped exactly like a selectable one. Any UI or `kubectl get vmclass` view that presents every class as a deployable option would offer it as if it were a t-shirt size.

Because a class can only govern where it is available (§3.1), a governing class is always also selectable in the zones it governs, so the schema cannot mark it as "govern only" without a discriminator. The mitigation is a naming convention (a class meant only to govern is named and described accordingly) and, optionally, a purely cosmetic UI hint — not a field that changes validity or meaning, only what a picker chooses to display. Selecting such a class directly is harmless: its ranges still apply as the ceiling, and its defaults are just a starting size.

### 3.7 `ClassInstance` for a governance-only class

The class-instance controller mints an immutable snapshot for every class unconditionally, keyed by a hash of its spec, regardless of whether any VM references it. That is unaffected by `governs` and harmless for a class no VM ever selects — instances are cheap. Skipping instance creation for governance-only classes is a possible later optimization, not a correctness requirement. The hash itself must move onto the typed fields (§7).

---

## 4. `spec.externalID`: identity partitioning (Phase 1)

`VirtualMachineClass` has been namespace-scoped since v1alpha6, so `(namespace, name)` is already a globally unique identity on the Kubernetes side. The vCenter-native object a class corresponds to (created via `VirtualMachineClasses.create()`) does not get that for free — VC's object space is flat, and VCFA disambiguates today via org-ID-prefixed names, applied at VAPI-create time, before any Kubernetes namespace is chosen (one VC-native class can be copy-distributed into several namespaces, §8.2). `(namespace, name)` cannot substitute for a dedicated identity field because the collision it needs to prevent exists a step before a namespace is assigned.

`spec.externalID` stands on its own, independent of `parentClassRefs` (§8) and of Phase 2. **Why not one `scope` field with a `Provider`/`Tenant` kind covering both identity and hierarchy:** identity partitioning and hierarchy are unrelated concerns, Supervisor and vm-operator have no concept of "provider" or "tenant," and the tiering adds nothing a plain opaque field doesn't say more simply.

### 4.1 Why this needs a dedicated field, and its shape

**An opaque `spec.externalID` string, immutable after create, enforced by webhook** — the same treatment established in this repo for `spec.id` on other managed objects (constitution: *"Managed object IDs (`spec.id`) are immutable after create; enforce via webhook"*). Alternatives considered:

- **Not a label.** A label's one real advantage — reverse lookup via `client.List` + label selector — is matched by a field indexer on a spec field, the repo's established pattern (`operator-best-practices.md`). A label also risks a charset mismatch (RFC-1123 label values are ≤63 chars, `[A-Za-z0-9._-]`) against whatever format VCFA's org-prefixed ID takes, and is editable by anyone with ordinary edit access, with no immutability story.
- **Not an annotation.** Annotations fit "opaque, caller-specific data the platform doesn't validate" (the round-trip conversion annotation in `api/utilconversion`), but that is safe because conversion data is *derived* and regenerable. An identity token that prevents a real VC-side collision is the opposite: nothing stops two classes from carrying the same annotation value or a client stripping it.
- **Uniqueness is not vm-operator's job.** The webhook enforces immutability, matching `spec.id`; it does not enforce that no two classes share the same `externalID`. VCFA guarantees that at VAPI-create time (its org-prefixing scheme); vm-operator has no information that would let it check uniqueness more meaningfully.

`externalID` and `parentClassRefs` (§8) are fully independent: a class can set either, both, or neither.

### 4.2 Upgrade path: existing classes in VC and Supervisor

This design assumes namespace-scoped resources throughout. Nothing about the upgrade path introduces or relies on cross-namespace lookup.

wcpsvc sets `dst.Name = src.ID` when it materializes a VC-native class into a namespace (`vmclass_kube.go`). So **every existing `VirtualMachineClass` object's `metadata.name` already is its VC-side ID**, and introducing `externalID` renames nothing.

- **Existing classes: the name never changes, and `externalID` is set to the name.** For every class already materialized in a Supervisor, `metadata.name` keeps its current value — including org-ID-prefixed ones — and `spec.externalID` is set to that same value. This happens as a one-time patch or lazily on first reconcile; either way nothing observable changes, and no VM referencing `spec.className` is affected.
- **New classes get the clean split.** Classes materialized after wcpsvc adopts this shape get a clean, human-facing `metadata.name`, while `spec.externalID` carries VC's own identifier. The name comes from a dedicated name field on the v2 class vAPI ([`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) §4.1).
- **`externalID`, `metadata.name` and `description` are three separate fields.** `externalID` is VC's identity, `metadata.name` is what VMs reference in `spec.className`, and `description` stays free text for people. **Why not reuse `description` for the name:** it is user-authored text shown in the vSphere UI and copied into the class today; overloading it would change the meaning of existing descriptions, and a name must follow DNS-name rules.
- **`spec.className` in a VM always resolves by the class's Kubernetes object name, never by `externalID`**, for old and new classes alike. `externalID` exists purely for wcpsvc/VCFA's VC-side bookkeeping and back-mapping; it is never a valid value for `spec.className`.
- **No forced migration gate.** Because `externalID` is optional and additive, a class whose `externalID` has not been set yet behaves exactly as it does today — nothing in Phase 1 depends on it being set.

---

## 5. Use cases, worked through concretely

### UC1 — Fixed t-shirt sizes only

*"I am a private cloud provider and want to include t-shirt-sized VM classes in the namespace for my DevOps users to deploy VMs with one of the t-shirt sizes."*

```yaml
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata: {name: small, namespace: team-a}
spec: {hardware: {cpus: {min: 2}, memory: {min: 4Gi}}}
---
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata: {name: medium, namespace: team-a}
spec: {hardware: {cpus: {min: 4}, memory: {min: 8Gi}}}
---
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata: {name: large, namespace: team-a}
spec: {hardware: {cpus: {min: 8}, memory: {min: 32Gi}}}
```

No class in this namespace sets `governs`. A VM sets `spec.className` to one of the three; nothing governs inline edits because nothing governs anything. This is byte-for-byte today's model.

### UC2 — T-shirt sizes as starters, inline edits allowed up to a ceiling

*"...also want to support updating the values inline in the VM spec up to a policy (the policy is another t-shirt size in this case, right? ... should the large act as the policy or maximum allowed)"*

Yes — `large` acts as the ceiling by setting `governs` on itself, while remaining exactly as selectable as `small` and `medium`:

```yaml
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata: {name: small, namespace: team-a}
spec: {hardware: {cpus: {min: 2}, memory: {min: 4Gi}}}
---
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata: {name: medium, namespace: team-a}
spec: {hardware: {cpus: {min: 4}, memory: {min: 8Gi}}}
---
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata: {name: large, namespace: team-a}
spec:
  hardware: {cpus: {min: 8}, memory: {min: 32Gi}}
  zones: [zone-a]
  governs: {}
```

A VM sets `spec.className: medium` (CPU 4, memory 8Gi by default), then edits `spec.resources.cpu` inline toward a higher value. Admission resolves the VM's effective configuration and checks it against the governing class that applies in the VM's zone — here, just `large` (CPU min/max 8, memory min/max 32Gi). The edit succeeds up to `large`'s ceiling and is rejected beyond it. `medium`'s own bounds are irrelevant to the check once a governing class exists — its role was only to supply the starting values. `large` is simultaneously deployable on its own via `className: large` and the ceiling for VMs that started from something else. (Whether inline edits on a class-backed VM require removing `className` is an open item, §7.)

### UC3 — Policy only, no selectable classes

*"...have no VM classes that are t-shirt sizes on the VM but ... a policy which exposes which hardware is supported and what is the maximum."*

```yaml
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata: {name: ceiling-only, namespace: team-a}
spec:
  hardware:
    cpus: {min: 1, max: 32}
    memory: {min: 1Gi, max: 128Gi}
  maxHardwareVersion: vmx-21
  zones: [zone-a]
  governs: {}
```

No other class exists in the namespace, so there is nothing to select — every VM here is classless (subject to §3.2a's admission invariant) and its inline configuration is bounded entirely by `ceiling-only`'s ranges. `ceiling-only` is an ordinary `VirtualMachineClass`; it would remain a governing ceiling even if some VM referenced it directly. How to keep it out of class choosers is covered in §3.6.

### UC3b — Sizing unconstrained, ExtraConfig restricted

An admin who wants no sizing ceiling but *does* want to restrict ExtraConfig keys across every zone of the namespace sets `governs` on a class carrying only the ExtraConfig rule, with no numeric fields set, leaving `spec.zones` unset so it covers every zone, including zones added later (§3.1):

```yaml
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata: {name: no-guestinfo-extraconfig, namespace: team-a}
spec:
  extraConfig:
    denied: [{type: Glob, key: "guestinfo.*"}]
  # zones unset -> every zone of the namespace, including future ones
  governs: {}   # governs.zones unset -> same as spec.zones -> every zone
```

This needs no separate mechanism: governing classes compose field by field, and a class can govern with only some of its fields set.

### UC5 — Resize via class swap and resize via inline edit are the same check

Whichever path a day-2 resize takes — swapping `className` to a different preset, or editing a field inline — the resulting effective configuration is checked against the same governing ceiling. Resize itself works as today: on a `className` change, or with the same-class resize annotation.

### UC7 — No governing class anywhere (the migration default)

A namespace with classes but none setting `governs` behaves exactly as today: classes are fixed presets, nothing governs inline edits, and class-derived configuration is authoritative because there is nothing to check it against. This is what every migrated namespace looks like on day one (§6).

### UC9 — Classless VM, no governing class covers its zone

A classless VM whose zone matches no governing class has no ceiling to resolve against. Per §3.2a's admission invariant and the research document's Q4/Q12, it is denied — classless inline configuration is a new, unvetted surface, and requires an explicit governing class before it is usable.

---

## 6. Migration plan

### 6.1 What migrates trivially

- **Every existing scalar field becomes `{min: <value>}`.** `max` and `default` both default to `min`, so a migrated class behaves exactly like its v1 self. No stored flag distinguishes ranged from fixed classes: `min == max` means fixed.
- **Round-trip safety.** The round-trip annotation mechanism (`api/utilconversion`) is wired up for `VirtualMachineClass`'s conversion functions before this version ships, so an older typed client (research document §3.3) reading and partially updating a migrated class does not collapse a genuine range to a single value. This is not sufficient on its own: both production writers write through v1alpha1, and wcp-namespace-operator copies only `Spec`, so annotations never reach the namespace copy. Both writers move to the new version (§8.6; [`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) §6). How this interacts with the collapse rule for older clients is open (§7).
- **No migrated class sets `governs`.** Per UC7, this is the correct migrated state — existing behavior is preserved exactly, and nothing needs to be created for an existing namespace to end up in the right state. `parentClassRefs` (§8) does not exist in Phase 1. `externalID` is set to each existing class's name (§4.2).
- **`VirtualMachineConfigPolicy` is removed as a kind.** Its CRD, the `Zone` controller's fan-out that creates one per zone, and the associated capability-gated controller registration are deleted rather than migrated. Its enforcement controller and webhook were never shipped (research document R7), so there is no enforcement behavior to carry forward.

### 6.1a Zones need no backfill

An existing class has no zone lists. Unset means "every zone of the namespace, including future ones" (§3.1), so every existing class is exactly as available as it is today, and since no existing class sets `governs`, nothing is governed. **No backfill and no zone discovery.**

The namespace vAPI change is additive. The existing `VmServiceSpec.vmClasses` field (a flat set of class names) keeps working exactly as today: a class attached through it is available in all zones of the namespace, with no governance. A new per-class list carries zones for associations that need fewer than all zones, or none; its shape (`VMClassSpec{vmClass, zones, governedZones}` in `Instances`, following the content-library precedent) and its merge rules with the old field are in [`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) §4.4. The migration does not depend on it.

The namespace's existing `Zones[].VmReservations` field is a different concept: a per-zone **capacity reservation** for a class (how many of a class are reserved in a zone), not a statement of which zones a class is available in.

### 6.2 `configSpec`

Each class's `configSpec` content that has a typed field in v2 moves into that field; everything else stays in the leftover-only `configSpec` (§2.1). Because anything that can't be elevated simply stays in the blob, **the migration has no failure case for content, and no existing class breaks.** It processes classes one at a time, records a per-class message instead of stopping wcpsvc, and is idempotent. The extraction runs in wcpsvc, so vcdb always holds the elevated shape; details in [`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) §3.4.

Typed values already win over the blob for `numCPUs`/`memoryMB` today (`configspec.go`), and wcpsvc already rejects a class whose blob `NumCPUs`/`MemoryMB` disagree with the typed values, so dropping those from the blob changes no behavior.

The vCenter-facing authoring path follows the same rule: the new v2 class vAPI has the leftover-only `configSpec`, and the existing v1 interface is kept as an adapter that builds a full `configSpec` on read and moves elevated fields out on write ([`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) §4.1, §4.6).

**A survey of real-world `configSpec` usage** decides which leftover fields to elevate first ([`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md) §7 has the documentation- and fixture-level findings).

### 6.3 Sequencing

**Phase 1 (9.1.3):**

1. Land the round-trip annotation wiring for `VirtualMachineClass` conversion (mechanical, no dependency on the rest).
2. Run the `configSpec` usage survey to choose which leftover fields to elevate first (§6.2).
3. Ship the range/constraint schema, `governs`, `spec.externalID`, the leftover-only `configSpec`, and the three-state zones additively. Every existing class validates unchanged, with no backfill (§6.1a), and no class sets `governs` yet.
   - 3a. Move both CRD writers (wcpsvc, wcp-namespace-operator) to the new CRD version, chosen per Supervisor by capability ([`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) §6).
   - 3b. wcpsvc: the new vcdb document column and the per-class, non-fatal `configSpec` extraction migration (same doc, §3).
4. Remove the `VirtualMachineConfigPolicy` kind, its CRD, and the `Zone` fan-out that creates it.

**Phase 2 (9.2):**

5. Ship `parentClassRefs` once the generated v2 vAPI (vcenter doc §5) is confirmed to round-trip its shape cleanly.

**Ongoing:** each later elevation of a leftover `configSpec` field is its own small migration (§2.1).

---

## 7. Open items

vCenter-side items (vcdb, vAPI, generator) are in [`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) §8 and not repeated here.

### Phase 1

- **Older clients and ranged classes.** Both production writers write through v1alpha1, and wcp-namespace-operator copies only `Spec`. How an older client's partial update of a ranged class is handled has to be settled together with the writer upgrade and the v1 vAPI adapter's collapse rule (vcenter doc O1).
- **Inline overrides on a VM that references a class:** whether inline edits to class-supplied fields require removing `spec.className`, or are allowed alongside it within the governing ceiling.
- **Default filling for the non-range value types** ([`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md) §2.2–§2.4), matching the `Range` rule (unset `default` is filled with `min`) for ease of use. Proposed:
  - `EnumPolicy`: an unset `Default` is filled with the first entry of `Allowed` (a single allowed value is the common case); a set `Default` must be in `Allowed`.
  - `BoolPolicy`: with `AllowOverride: false` (the value is fixed), `Default` is required — there is no ordering to fill it from, and a value fixed to vpxd's unknown default can't be written down. Otherwise an unset `Default` defers to vpxd.
  - `ListPolicy` (`extraConfig`, devices): no default to fill — `entries` is the preset, and unset `entries` adds nothing; `allowed`/`denied` only govern.
  - Any type: a wholly absent struct defers to vpxd's default.
- **Classless VMs in a governed zone.** Proposed: fill only typed fields the VM left unset from the governing class's `default`, written visibly into the VM spec at admission, only when unambiguous (zone known, or every candidate governing class agrees); otherwise reject with an actionable message; never `className`, never the blob. Prerequisite: inline sizing fields must actually be applied (today nothing reads `spec.resources.size` to set NumCPUs/memory).
- **Elevated fields arriving in `configSpec`:** auto-elevate when the typed field is unset, drop when equal, reject on conflict — or reject always (vcenter doc O3). Also whether a mutating webhook does this for Kubernetes-authored classes, with an admission warning, accepting GitOps diff noise.
- **Elevation registry** as the single source for every place an elevation touches (vcenter doc O7).
- **`reservedProfileID`/`reservedSlots` rules.** Proposed: a reserved profile is only valid for a fixed class (every ranged field `min == max`), enforced by the class webhook and by wcpsvc when a class has reservations; a class with reservations can't be made ranged; and a VM using a reserved class can't override its size inline, since the reservation is sized to the class ([`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md) §5.2).
- **Re-base the `ClassInstance` hash onto typed fields.** Today it hashes `ReservedProfileID` + `configSpec` and errors when `configSpec` is empty (`controllers/virtualmachineclass`).
- **Stranded inactive `ClassInstance` bug:** fix as part of this work or leave out of scope.
- **The chooser-list mitigation (§3.6):** naming convention versus a cosmetic UI hint, and who owns deciding which.
- Whether the discoverability cost in §3.5 needs tooling beyond the VM-level status (e.g., a computed "effective bounds for this zone" view) before this ships, or whether that can follow later.
- Whether a purely advisory "intended zones" hint on a *selectable* class is worth adding later for earlier admission-time rejection (a class requesting a GPU profile that exists in no zone the namespace touches could fail fast with a clear message instead of at placement) — not the same mechanism as `governs`, and not required for correctness, since `ConfigTarget` feasibility checking already covers it.
- Whether the per-field conflict check in §3.2 needs its own status condition (distinct from §3.5's), so a same-field conflict between two governing classes is diagnosable as "these two classes conflict," not just "rejected."
- Whether an in-namespace governing class ever needs to *exempt* a specific sibling class from the §3.2 class-vs-class subset check (a deliberate "break-glass" class) — not currently supported and not raised as a requirement, but worth flagging before the admission logic is written as unconditional.

**Implementation scoping (not design questions):**

- **The governance-compliance check (§3.1b) is an exported `pkg/` function**, callable from both the admission webhook's validator and the VM reconciler's power-state step. The constitution keeps validator logic in unexported types under `webhooks/virtualmachine/validation`, which a controller can't import; confirming this is a straightforward extraction is real scoping work.
- **The `VirtualMachineClass` controller gains a reconcile-time class-vs-class drift check** (§3.2b), reusing §3.2's per-field subset check. Visibility-only.
- **The VM reconciler gains a step that re-checks governance compliance and can withhold the power-state step for `DenyPowerOn`** (§3.1b, §3.3a).
- **The namespace vAPI gains the per-class zone list** (§6.1a) — new scope on the wcpsvc/VCFA side, not vm-operator's to build alone.

### Phase 2 (9.2, tenant-based VM classes)

- **Deletion symmetry:** §8.4 and §3.2a both settle on "deletion is unblocked, and dependents are only ever loosened, never invalidated" — for a class referencing another class via `parentClassRefs`, and for a VM referencing a class. Whether that single rule is right for both cases needs another pass.
- Whether `parentClassRefs`' cycle-freedom check (§8.3, §8.4) is a sufficient guardrail on its own, or whether an explicit maximum chain depth is worth adding once real chains longer than 2 show up.
- Whether the `parentClassRefs` shape round-trips through the generated v2 vAPI (§6.3 step 5).
- **Several governing classes on one zone.** A VM is allowed in a zone if it fully satisfies at least one class governing that zone; it can't combine one class's allowance with another's. Open: whose `existingVMs` policy applies when a VM satisfies none of them, and whose `default` is used for classless fill-in.
- **Tenant-authored classes and the leftover `configSpec`:** whether a governing class needs a way to forbid a non-empty `configSpec` in its zones, since governance can't bound the blob's contents.

---

## 8. `parentClassRefs`: hierarchical governance (Phase 2 — release 9.2, tenant-based VM classes)

**Nothing in this section ships in Phase 1.** It is designed now so the Phase 1 schema (§2, §4) doesn't need a breaking change to accommodate it — see "Phasing".

The use case: a higher-authority class needs to bound what a lower-authority class is allowed to declare (checked once, at the lower class's creation), and to supply the effective ceiling for any field the lower-authority class doesn't declare itself (resolved live, at every VM admission). This is what makes tenant-based VM classes possible — a tenant admin further subdividing an allocation a higher authority carved out for them.

**Why a plain reference, not a `Provider`/`Tenant` tier:** Supervisor and vm-operator have no concept of "provider" or "tenant," and shouldn't acquire one just to express this — those are VCF Automation tenancy concepts. A plain object reference says the same thing more simply.

### 8.1 Shape

```yaml
spec:
  parentClassRefs:                             # optional; list, phase 2 caps length at 1 by webhook (§8.2)
    - name: region-a-pool-1                    # same-namespace object reference (§8.2)
```

- Optional. A class with nothing set here participates in none of this — ordinary single-tier usage (UC1–UC9) is unaffected.
- `parentClassRefs` entries are a struct (`{name: ...}`), not a bare string, even though `name` is the only field needed at launch. Per the constitution, additive API changes are not safe once a field has shipped (k8s issue #111703) — a struct can gain a field later (e.g. `namespace`) with no breaking change; a scalar-string list cannot become a list of structs without one.
- The field is list-typed for the same reason: nothing about the resolution rule in §8.3 depends on there being exactly one entry, and a future multi-governor phase (`governs.match`) may need more than one. The list is capped at length 1 by webhook validation at launch, not by the schema — the cap can be lifted later with no API change. See §8.3 for why the cap exists.

### 8.2 Same-namespace only

**A class and every class that lists it in `parentClassRefs` must live in the same namespace.** `VirtualMachineClass` is namespace-scoped, and a VM's `className` resolves within its own namespace — there is no live mechanism for one namespace to reference a class object in another. (`VirtualMachineClassBinding`, which existed for exactly this, was retired when `VirtualMachineClass` became namespace-scoped in v1alpha6.) Cross-namespace hierarchy would mean reintroducing that retired indirection, or inventing a new one.

This does not block the real use case (a higher-authority pool bounding what gets exposed into a separately-provisioned workload namespace) — it relocates *whose job* that is. **Distributing a class definition into the namespaces that need it is already solved outside vm-operator, by copying, not by live reference.** The namespace-provisioning layer resolves a class's spec and writes an independent copy into each target namespace, then deletes stale copies when the desired set changes: on etcd-backed Supervisors this is wcp-namespace-operator (it copies `Spec` from the class in the `vmware-system-vmop` catalog namespace, and falls back to the v1 vAPI `GetVmClass`); on other Supervisors wcpsvc writes each namespace's copy directly ([`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) §2.3). Every namespace ends up with its own self-contained object. If two classes descended from one another need to be validated against each other, whatever system wants that (VCFA's translator, the namespace-provisioning controller, or a future consumer) places both copies into the same namespace. vm-operator only validates the relationship once both objects are co-located — never across a namespace boundary.

### 8.3 Resolution is a live, per-field walk with no fixed depth

For a VM's effective configuration, resolve each field independently:

1. Start at the VM's directly-applicable governing class.
2. If that class declares the field, it is the sole authority for it — validate the VM against it and stop.
3. If it doesn't, and it has an entry in `parentClassRefs`, move to that class and repeat from step 2.
4. If the chain runs out with no class having declared the field, the field is ungoverned by this chain (some other, unrelated governing class may still cover it via §3.2's per-field check).

**Nothing caps how many hops this can take**, even though each class's own `parentClassRefs` list is capped at one entry: `C → B → A` is a chain of depth 2 built from single-entry lists. The webhook rejects *cycles*, not long chains: at any class's creation/update, walking its `parentClassRefs` chain must never revisit the class being validated.

**Why the length-1 cap on each class's own list:** if a class had two parents, was silent on some field, and its two parents disagreed on that field's range, there is no rule to pick a winner — §3.2 rejects merging/intersecting as a resolution strategy. The cap defers a genuinely unspecified semantic rather than guessing one. (A possible future relaxation: require declared fields to be disjoint across a class's multiple parents, so no field ever has two candidate suppliers.)

**This resolution is always live, never baked into a status field.** A resolved-and-cached "effective ceiling" in `status` would reintroduce the problem §3.1a avoids for `status.zones`: a change to a higher class would need a controller to notice and re-patch every descendant's cached status before it took effect, and until then VM admission would check against a stale value — several cache hops (watch → workqueue → reconcile → status-patch → informer) removed from the source of truth, versus the webhook's single cache hop when it resolves live.

A read-only computed mirror in `status` — the fully resolved, per-field effective ceiling for a class — is still worth adding for discoverability (§3.5): without it, seeing a class's true effective ceiling means reading every ancestor. It never feeds admission, the same treatment `status.zones` and `Zone.status.governingClasses` get. Because every class in the chain lives in the same namespace (§8.2), a reader who can see the referencing class can already see every ancestor, so the mirror exposes nothing new — under two conditions: (1) ordinary namespace-scoped read RBAC on `VirtualMachineClass` remains the norm (§8.6); if read access were ever scoped to specific class names, the mirror would bypass that; (2) §8.2's same-namespace rule stays in force. Populating the mirror needs a controller watching class changes and requeuing anything that references the changed class (the field-indexer + mapper pattern, `operator-best-practices.md`); the cascade terminates because cycles are rejected at creation. Status fields carry none of spec's API-compatibility risk, so this is a Phase 2 detail to schedule, not a blocker.

**Field absence must be distinguishable from field-zero.** "The child didn't declare this field" and "the child declared this field as its zero value" resolve differently (fall through to the parent, versus an actual zero-valued ceiling), so the range structs use `+optional`/`omitempty` pointer-shaped fields — the repo's convention, here load-bearing for correctness.

#### 8.3a Does a parent need to govern too?

No — `governs` (§3.2) and `parentClassRefs` (§8) are independent properties. Two cases:

- **Parent governs, child also governs the same zone, and both declare the same field.** Covered by §3.2's exception: the child's range is guaranteed nested inside the parent's (§8.4), so checking a VM against the child alone is sufficient.
- **Parent does not govern at all, child does.** A higher-authority class hands out plain, selectable t-shirt-size classes, and a class derived from one via `parentClassRefs` sets `governs` on itself. **This is valid.** The child's declared ranges are still validated as a subset of the parent's at creation (§8.4).

The general rule: `parentClassRefs` only constrains what a class is *allowed to declare*, and supplies a fallthrough value for what it doesn't (§8.3); it never determines whether that class *governs* anything.

### 8.4 Validation at creation

- **Existence.** Every entry in `parentClassRefs` must resolve to an existing class **in the same namespace** (§8.2) — an ordinary `Get`. The referenced class must exist before the referencing class can be created.
- **No cycles.** Walking the chain from the class being created/updated must never revisit itself (§8.3).
- **Subset, only on fields both declare.** For any field the referencing class declares, its range must fall within the resolved effective range of the class it references — the same per-field subset comparison §3.2 uses, applied once at creation/update against the referenced class's *current* values. A referencing class never has to restate a field it doesn't intend to tighten — silence means "defer to whatever the chain resolves to" (§8.3). §3.4a's rule applies: the author sees pass/fail and which of their own fields is out of range, never the referenced class's values.
- **Deletion is unblocked, with no finalizer.** A referenced class can be deleted even while other classes still list it in `parentClassRefs`:
  - **This is how `VirtualMachineClass` deletion works today:** the validating webhook's `ValidateDelete` unconditionally allows deletion (`webhooks/virtualmachineclass/validation`), regardless of whether any VM references the class. A finalizer that blocks deletion only for referenced classes would be a narrower special case, and — since namespace deletion cascades through every object at once — could leave a namespace stuck in `Terminating`.
  - **A class is not needed after a VM is provisioned from it**, for the VM's own template values: a VM is pinned to an immutable `VirtualMachineClassInstance` snapshot at admission (`pkg/util/vmopv1/resize.go`), so deleting the class does not change that VM's hardware.
  - **The consequence (tracked in §7, Phase 2):** deleting a referenced class silently loosens anything that fell through to it under §8.3 — if `B` was silent on `memory` and deferred to `A`'s `1–4Gi`, deleting `A` leaves `memory` ungoverned for `B`, with no edit to `B`. This matches §3.2a's rule for deleting an ordinary governing class, but the trigger (an edit to a different object) is less visible.
  - **Alternatives considered:** clearing the reference on the referencing classes (disassociation), and a blocking finalizer. Both are rejected for the reasons above.

### 8.5 Why this isn't a Kubernetes owner reference

§8.2's same-namespace rule removes the mechanical objection to owner references (cross-namespace owner references don't fire). They are still not used, because the one thing an owner reference buys — cascade-delete of dependents when the owner is deleted — is wrong here: deleting a referenced class must not delete every class that references it (§8.4). Kubernetes has no owner-reference mode meaning "delete the owner, leave dependents exactly as they are" — background propagation orphans dependents (drops their owner-reference entry with nothing marking *why*), and foreground propagation cascades the delete. A plain, inert field (`parentClassRefs[].name`, looked up at creation and re-read live at admission) says exactly what's wanted, with no GC behavior attached.

`Zone`-as-owner-of-`VirtualMachineClass` is also the wrong tool even where mechanically legal (a cluster-scoped owner over a namespaced dependent): the class/zone relationship is many-to-many, and ownership implies cascade-delete — deleting a `Zone` must not delete every class available in it.

### 8.6 RBAC: trust in the platform's write paths

A `parentClassRefs` target is objectively checked (existence, cycle-freedom, subset), never merely asserted, so there is no self-declared authority to spoof.

`VirtualMachineClass` values, including `parentClassRefs`, are **trusted as written**. There are two production writers, both platform components: **wcpsvc**, and **wcp-namespace-operator** on etcd-backed Supervisors (which copies classes from the `vmware-system-vmop` catalog namespace into each namespace). Both are gated by VC's/VCFA's authentication and RBAC upstream, and ordinary tenants do not create or edit `VirtualMachineClass` objects directly. Both are updated for the v2 spec ([`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) §6). The writer on Supervisor 2.0 is not yet identified (vcenter doc O10). This is expected to remain the model for the foreseeable future: there is no use case for direct tenant authorship of `VirtualMachineClass`, so no RBAC-hardening mechanism is designed against that possibility.

---

## 9. Key decisions

vCenter-side decisions (vcdb, vAPI, generator, CRD writers) are listed in [`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) §1.

| # | Decision | Where |
|---|---|---|
| 1 | One kind: `VirtualMachineConfigPolicy` is removed; `governs` is an additive property of `VirtualMachineClass`. | §1, §6.1 |
| 2 | Admission validation against `ConfigTarget`, `VirtualMachineConfigOptions` and guest OS already exists and is separate from this design. | §3.4 |
| 3 | No stored range-vs-fixed discriminator: `min == max` means fixed. | §6.1 |
| 4 | `className` is never filled in on a VM by the platform. | §3.2a |
| 5 | Resize works as today: on a `className` change, or with the same-class resize annotation. | UC5 |
| 6 | Deleting a governing class is allowed; its ceiling stops applying, and its VM conditions are cleared. | §3.2a |
| 7 | `status.governingClassNames []string` on the VM, computed live. | §3.5 |
| 8 | `externalID` is a `spec` field, immutable after create; existing classes get `externalID` = name, and only new classes have a different name. `externalID`, `metadata.name` and `description` are separate fields; `description` is never used as the name. | §4 |
| 9 | `configSpec` holds only fields with no typed home: presets and defaults only, never governance; typed fields always win and are never merged; elevated paths are not allowed; each later elevation is a small migration. | §2.1, §2.2 |
| 10 | The migration moves blob content that has a typed field into that field and leaves the rest; it has no content failure case. | §6.2 |
| 11 | `Range` defaults: if `min` or `max` is set, `default` is required (explicit, or filled with `min`); a wholly absent struct defers to vpxd. | §2.1 |
| 12 | Zones have three states. `spec.zones`: unset = all namespace zones including future ones, `[]` = none, a list = only those. `spec.governs.zones`: unset = same as `spec.zones`, `[]` = none, a list = only those. A class can only govern where it is available. Both come from the namespace association. `*[]string` throughout; the risk is documented. | §3.1 |
| 13 | No zone backfill on upgrade; the namespace vAPI change is additive, and the existing field keeps today's behavior. | §6.1a |
| 14 | Several classes governing one zone (Phase 2): OR semantics — the VM must fully satisfy at least one class. | §7 (Phase 2) |
| 15 | Two CRD writers (wcpsvc, wcp-namespace-operator); both move to the new CRD version, chosen per Supervisor. | §8.6 |

---

— Faisal + Claude
