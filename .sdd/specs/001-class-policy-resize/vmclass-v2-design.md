# Design: VirtualMachineClass v2 — Constraint, Default, and Governance

- **Status**: Draft design — for discussion, no decision sought
- **Decision recorded here**: `VirtualMachineConfigPolicy` is **dropped entirely**. `VirtualMachineClass` v2 supersedes both the current `VirtualMachineClass` and `VirtualMachineConfigPolicy`. There is one kind. Governance (`governs`), identity partitioning (`spec.externalID`), and per-governing-class drift handling (`governs.existingVMs`) are additive properties of that one kind, not separate objects, and ship in **Phase 1 (release 9.1.3)**. Hierarchical governance (`parentClassRefs`, tenant-based VM classes) is the same kind of additive property, but ships in **Phase 2 (9.2)** — see "Phasing" below.
- **Companion to**: [`research-vmclass-as-policy.md`](./research-vmclass-as-policy.md), whose §4–§5 this supersedes with the model below
- **Audience**: principal engineers, architecture review

This file works out the concrete shape of a single-kind proposal: what the new `VirtualMachineClass` looks like field-by-field, how it takes over what `VirtualMachineConfigPolicy` did without introducing a second kind, how zone-specific hardware is handled, how classes authored at different levels of authority (provider vs. tenant) can be bounded by each other, and a migration plan for existing classes.

This is a bigger pivot than the research document's original recommendation (§4.4, appendix A1), which kept the two objects and only thinned `VirtualMachineConfigPolicy` into a pointer. It is cheap to take now for a reason already on record in the research document: `VirtualMachineConfigPolicy`'s enforcement controller and admission webhook are **not on `main`** — only the `Zone`-driven fan-out that creates the (currently empty-ish) object exists. Dropping the kind removes almost no shipped behavior.

**Assumption this design depends on, outside its own control:** it assumes VCF Automation's existing, separate VAPI-based hardware-policy construct is retired in favor of this model. If that construct ships anyway, both it and this design's `governs` mechanism exist side by side, which reproduces the exact duplication problem this redesign exists to eliminate. This is called out explicitly because it is not a decision this document, or vm-operator, can make unilaterally.

---

## Phasing: what ships when

This design spans two releases, and the split matters enough to state up front rather than let a reader discover it mid-document:

**Phase 1 — release 9.1.3.** Everything in §1–§7 except where a section explicitly says otherwise:
- The range/constraint schema (§2), `governs` and its zone/enforcement/drift semantics (§3), `spec.externalID` for VC-side identity partitioning (§4), the use cases (§5), and the migration plan for existing classes (§6).
- VAPI v2 modeling for VM classes is also in scope for 9.1.3. It is being produced by a separate, parallel effort; this document assumes its output as a dependency and will reference it once available, rather than design it here.
- Storage/disk-size range controls are in scope for 9.1.3 as part of the range/constraint schema in §2 — the range model for the storage-related fields (`instanceStorage.volumes`, the `reservedProfileID`/`reservedSlots` coupling) needs its own pass before the schema in §2 can be called complete for those fields specifically, since they don't reduce to the same scalar `{min, max, default}` shape as `cpus`/`memory` cleanly.

**Phase 2 — release 9.2.** Hierarchical governance via `parentClassRefs` (§8) — the mechanism that lets one class bound what another class in the same namespace is allowed to declare, which is what makes tenant-based VM classes possible. This is deferred as a deliverable, not as a design gap: the mechanism is worked out in this document now, at the same level of rigor as everything shipping in Phase 1, because the schema decisions in §2 and §4 (in particular, keeping `parentClassRefs` list-typed and keeping identity partitioning on a separate field from hierarchy) are made specifically so Phase 2 doesn't require a breaking change to what Phase 1 ships. Nothing in Phase 1 depends on §8; nothing in §8 depends on anything not already shipped in Phase 1.

Everywhere below, a cross-reference into §8 is a Phase 2 mechanism; everything else referenced is Phase 1.

---

## 1. The refinement: two independent properties on one kind, not two kinds

The research document's §5.1 tied a class's role — selectable preset versus ambient envelope — to a `zones` field, which amounted to a role discriminator. A later revision moved governance to a separate pointer object. This version goes further: there is no second object at all. Instead, a class carries two independent properties that either can hold at once:

- **Selectable** — can a VM reference this class by name in `spec.className`?
- **Governing** — does this class's constraints act as a ceiling on VMs in some scope, regardless of what they reference? Expressed by an optional `spec.governs` block.

`large` can be an ordinary, selectable t-shirt size *and* the ceiling every VM in a zone is measured against — not through a special mode, and not through a second object, but because it additionally sets `spec.governs`, while remaining exactly as selectable as `small` and `medium`, which do not set it.

**Why this does not repeat the objection raised against a `role` field earlier in the research document (Appendix A2):** `spec.governs` is additive, not a discriminator. Its presence does not change which other fields on the object are valid, does not invert what "this class exists in the namespace" means for assignment (it still just means "usable," now in either or both senses), and does not make any part of the schema meaningless for a given instance the way a `role: Preset | Policy` split would have. A class with `governs` set is a completely ordinary class that additionally contributes to an intersection; nothing about its `hardware` or `policies` fields changes shape or meaning.

---

## 2. `VirtualMachineClass` v2

### 2.1 Shape

Every previously-scalar field becomes a constraint with three parts, following the defaulting rule from the research document (§5.2): `max` absent ⇒ `max = min`; `default` absent ⇒ `default = min`. A class that sets only `min` on every field is, byte for byte, today's fixed t-shirt size.

```yaml
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata:
  name: large
  namespace: team-a
spec:
  description: "Large general-purpose"
  externalID: ""                # optional; opaque VC-side object identity, immutable after create — see §4. Carries no hierarchy meaning.
  zones: [zone-a]               # required to be usable; declared availability AND governance scope, always explicit — see §3.1.
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
  reservedProfileID: ""    # only valid when every ranged field has min == max (§6 below)
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

**No `configSpec` field.** Per the research document §6.1, opaque `configSpec` is not carried at all. Every field a class needs to express must be a named, typed field above. This is a closed, explicitly-typed surface, not a scalar-shaped surface with an escape hatch.

### 2.2 What is deliberately absent

- **No `role` or `mode` discriminator.** `governs` is additive (§1), not a switch between two schemas.
- **No opaque, ungoverned escape hatch.** `configSpec`'s arbitrary passthrough of an entire, unvalidated `ConfigSpec` is gone — every construct a class needs is a named, typed field. The one remaining fallback, `spec.extraConfig`, is not the same kind of thing: it is a typed key/value list whose *keys* are governable (`allowed`/`denied`, with `Fixed`/`Regex`/`Glob` matching — see [`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md)), the same way every other field here is governable. It exists because vSphere's own VMX key space grows faster than any typed schema can track — a permanent, bounded design choice, not a leftover gap in this one.
- **No separate `VirtualMachineConfigPolicy` kind.** Everything it would have carried is expressed by `governs` on one or more ordinary `VirtualMachineClass` objects (§3).
- **No merged zones field.** `spec.zones` (availability) and `governs.zones` (governance scope, defaulting to `spec.zones`) are deliberately two fields, not one — see §3.1 for the case that requires the split.

---

## 3. `spec.governs`: how one kind takes over what the policy did

### 3.1 Shape and semantics

`spec.zones` is top-level, not nested under `governs`, because it is meaningful with or without `governs`: it is the answer to "where can this class be selected at all," independent of whether it also governs anything.

```yaml
zones: [zone-a, zone-b]   # required to be usable; declared availability is always an explicit, opt-in list — see below
governs:
  zones: [zone-a]           # optional; must be a subset of spec.zones. Omitted = governs everywhere spec.zones allows.
  enforcement: Deny          # open question in the research doc (§7 Q10); shown here for completeness
  existingVMs: AllowOnViolation   # see §3.3a
```

**`spec.zones` is always an explicit opt-in list; there is no implicit "all zones" default.** An earlier draft of this section treated an absent `spec.zones` as "available namespace-wide" — that's dropped. A class with `spec.zones` absent or empty is not available or governing anywhere, full stop, the same way a class with no `hardware` fields set doesn't implicitly admit every possible value. This matters once automatic zone placement (§3.1b) exists: if "absent" meant "all zones," a class's applicability would silently change every time a zone was added to or removed from the namespace, with no edit to the class itself — the same class of bug §3.1a already rejects for `status.zones` feeding governance, just triggered by a namespace-level event instead of a status computation. Requiring an explicit list means a class's applicability changes only when someone edits that class. §6 covers the migration story for existing classes, which predate this requirement.

- **`spec.zones` set** — an admin-declared restriction: the class may only be used in these zones. This is a static declaration; nothing about it is computed or reconciled.
- **`governs` absent** — the class is purely selectable, exactly like `small` and `medium` in UC1/UC2 below. This is what every migrated class looks like on day one (§6).
- **`governs` present, `governs.zones` omitted** — the class governs everywhere it is available, i.e. exactly `spec.zones`. This is the common case and every use case below uses it. **This default is not an exception to `spec.zones`'s explicit-only rule above, because the two rules solve different problems.** `spec.zones`'s old implicit default was banned because it pointed at *ambient* state — namespace zone membership, which can change with zero edits to the class. `governs.zones`'s default only ever points at `spec.zones` — a sibling field on the *same* object, set in the *same* write, validated by the *same* webhook call — so there is no drift vector to eliminate, and requiring it to be spelled out a second time would be pure restatement with no added safety (the same reasoning §8.3/§8.4 already use to justify a referencing class not having to restate a parent's declared fields). Separately, `governs` is itself already the opt-in: writing `governs: {}` is an unambiguous declaration of intent to govern, so if `governs.zones` omitted meant "governs zero zones," `governs: {}` would be a guaranteed no-op — a field whose default value makes it meaningless. This is why UC1/UC2's t-shirt-size-with-a-ceiling pattern (`large` setting both `zones` and `governs: {}`) works without restating the zone list twice.
- **`governs.zones` set** — a narrower governance scope than availability: the class is selectable across all of `spec.zones`, but only acts as a ceiling in this subset. Validated as a subset of `spec.zones` at admission — a class cannot govern where it is not even available. This is for the case a single field merge would otherwise block: a class selectable everywhere but meant to govern only one zone, while a different class governs another.

Two zones fields, not one, because the discriminating case is real: a class can legitimately be selectable in more zones than it governs. Collapsing them into one field (an earlier draft of this section did) makes that case inexpressible without splitting the class in two just to get the governance scope right.

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

A VM in `zone-b` or `zone-c` can still set `className: flex` and get its hardware defaults — `flex` just isn't the governing ceiling there, whatever other class does that job for those zones applies instead. A VM in `zone-a` gets both: `flex`'s own values if selected, and `flex`'s ranges as the ceiling regardless of what it selected. If `governs.zones` were forced to equal `spec.zones`, expressing this would require splitting `flex` into two differently-scoped classes just to get the governance boundary right in `zone-a` without also making it govern `zone-b`/`zone-c`.

#### 3.1a `status.zones` is feasibility, and never feeds governance

`status.zones` is computed — the subset of `spec.zones` that `ConfigTarget` currently reports as hardware-compatible with this class's constraints. It reconciles as cluster hardware changes.

This must never be read by the governance check in §3.2. If it were: a zone gaining compatible hardware would silently start being governed by a ceiling no admin turned on there, and a zone losing hardware would silently drop out of governance — a ceiling disappearing with no admin action and no event. Governance scope is `spec.zones`: declared, static, and changes only when an admin edits the object. `status.zones` answers "can this run here," never "is this permitted here" — see also §3.4.

### 3.1b Automatic zone placement: how an unspecified VM zone stays governance-compliant

`VirtualMachine.spec.zone` (backed by the `topology.kubernetes.io/zone` label) is optional — a VM can be created with no zone at all, with the concrete zone decided later. This raises a question this document's predecessor left open (`research-vmclass-as-policy.md:224`, "what governs a VM that has not yet been placed") and that this section now answers concretely, grounded in how zone placement actually works in this repo today, not as a new mechanism bolted on top of an opaque external service.

**Zone specified.** `doesVMNeedPlacement` (`pkg/providers/vsphere/placement/zone_placement.go`) returns early with the zone already fixed, and placement operates only within that zone's clusters and hosts. Validate directly against that zone's applicable governing class(es) and `ConfigTarget` feasibility — this is the existing model, unchanged.

**Zone unspecified.** Two things make this sound rather than merely hopeful:

- **Feasibility is already safe, because admission and placement share the same source of truth.** `ConfigTarget` — the same data `status.zones` (§3.1a) is computed from — is what placement's own candidate search is grounded in too. A zone that's hardware-infeasible for the VM's requested configuration was never going to be a placement candidate in the first place; nothing new is needed on this axis.
- **Governance is deliberately kept invisible to placement, permanently — not wired in as a new input.** `governs` ranges have no representation in `ConfigTarget` or anything else placement already consults, and that stays true. Placement — for a single VM via `Constraints.Zones`, or for a group via `preferredZoneName`/`GroupPlacement` — keeps behaving exactly as it does today, choosing a zone with no awareness of governance at all. Governance is a **detection-and-enforcement layer that sits on top of whatever placement decides**, not a filter placement consults while deciding. This is simpler than threading a new constraint through placement, and it means single-VM and group-VM placement need no separate treatment: neither path changes.

**Admission's existential check, precisely:** when `spec.zone` is unset, a VM is admitted if **at least one** zone the namespace touches has an applicable governing class (or no governing class at all, per §3.2a) whose ranges the VM's effective configuration satisfies, and is `ConfigTarget`-feasible. This is deliberately existential ("at least one"), not universal ("all") — it is a **fail-fast diagnostic only**: it rejects a VM up front when it's already certain, from currently-known configuration, that no zone could ever admit it. It does not attempt to predict or constrain which zone placement will actually choose. A VM that is part of a group gets exactly this same per-VM check, independently — since placement never becomes group-governance-aware either, there is no group-wide intersection to compute, and nested groups need no special handling: each transitively-contained VM is checked on its own, the same way it would be if it had no group at all.

**Once admitted, whatever zone placement actually picks is validated afterward, by the reconciler, using the same check admission used.** The governance-compliance check (does this VM's effective configuration, in its now-known zone, satisfy its applicable governing class(es)) is one function, exported from `pkg/` so it is callable from both the admission webhook's validator and the VM reconciler — not two independently-maintained copies of the same rule. The reconciler re-runs it on every pass once a zone is known (whether the VM was placed individually or as part of a group), and reacts exactly as `governs.existingVMs` (§3.3a) already specifies: `AllowOnViolation` surfaces a status condition and proceeds; `DenyPowerOn` withholds the power-state-reconcile step (the "Reconcile power state" step in the VM's normal reconcile order — see `operator-best-practices.md`) until compliant. Because enforcement happens at the reconcile-level power-on step rather than solely by rejecting an admission-time `spec.powerState` edit, there is no meaningful distinction between a VM's first power-on and any later one — the reconciler withholds the vCenter power-on call either way. See §3.3a for the resulting carve-out to the "new objects are never grandfathered" language.

**This is why groups need no separate design or implementation from the single-VM path.** The earlier framing in this section treated `VirtualMachineGroup` placement as a gap requiring its own work — that was wrong. Once governance is purely a post-placement detection-and-enforcement layer, group membership is irrelevant to it: each VM's compliance is evaluated from its own class and its own zone, regardless of how it got that zone. `PlaceVirtualMachineGroup`'s single `preferredZoneName` and the fact that group members can legitimately land in different zones (`GroupPlacement`'s own doc comment) are simply not concerns this mechanism needs to touch.

**Whatever ends up non-compliant resolves through the existing drift mechanism, with nothing new to design.** Whether a VM ends up in a non-compliant zone because a governing class was edited after the fact, or because placement (unaware of governance, by design) happened to choose a zone that turns out non-compliant, the object is now just an already-admitted, currently-non-compliant one — exactly what `governs.existingVMs` (§3.3a) already exists to handle, enforced by the reconciler as described above. There is no group-specific fallback, and no preassigned-zone-override question to resolve: an already-assigned zone label continues to hard-filter placement exactly as it does today, unaffected by governance, and any resulting non-compliance is simply caught by the same reconciler check.

### 3.2 Finding the effective ceiling for a VM: per-field, not intersected

**Merging ranges field-by-field across multiple governing classes does not work.** Two classes cannot be combined by taking hardware bounds from one and CPU-reservation bounds from the other — there is no principled way to intersect two independently-authored objects into a single effective range once they disagree, or even once they simply cover different fields, without an arbitrary rule for which one wins. The correct model is **admit if any single applicable governing class matches**, not "intersect every applicable class's ranges into one merged ceiling."

Concretely: for each field the VM's effective configuration actually sets, find the governing class (if any) that constrains that specific field for the VM's zone, and check the VM's value against that one class's range for that field. A VM can be bound by `large`'s `hardware.cpus` range and, simultaneously, by `no-guestinfo-extraconfig`'s `extraConfig.denied` rule (UC3b) — two different governing classes, each owning a disjoint field, neither one's ranges ever merged with the other's.

**Phase 1 keeps this simple by preventing overlap up front**, since the common case — one governing class per zone — needs no per-field bookkeeping at all:

- **At class admission**, creating or updating a `VirtualMachineClass` with `governs` set is rejected if it would constrain a field that another governing class already constrains for an overlapping zone. This catches the common mistake (two classes both setting `hardware.cpus` for `zone-a`) at the point an admin makes it.
- **Class-time validation alone is not sufficient**, and the VM-admission check is the real backstop, not a redundant second pass: a class can be edited later to add a field another governing class already owns, creating an overlap on a field neither class originally shared. That edit does go through the class webhook (§3.1's always-explicit `spec.zones` means a class's governed scope only ever changes via an edit to that class itself, not silently via a zone being added to the namespace — the race this bullet used to cite is closed by that decision), but the *other*, already-existing governing class's state at the moment of that edit is exactly what the class-creation-time check inspects, and a **VM created between that edit and any subsequent reconcile** still needs its own check, since the class webhook only validates the class being written, not every VM already relying on the classes around it. So **VM admission always re-derives, per field, which governing class(es) apply**, and if two applicable governing classes constrain the *same* field for the VM's effective configuration, the VM is rejected with an explicit conflict message naming both classes — the same empty-intersection-style diagnostic the research document's §5.3 already calls for, repurposed here as "these two classes conflict on this field," not "ranges admit nothing in common."
- This is what "one governing class per zone" (the phase-1 simplification) means precisely: not a hard cap enforced by counting, but a natural consequence of the no-overlapping-fields rule, in the common case where one class sets all of a VM's governed fields.

**This generalizes cleanly to `governs.match` later** (the future workload-shape-based extension discussed earlier): a class matching hardware-A workloads and a class matching hardware-B workloads apply to disjoint VMs by construction, so the same no-overlap invariant holds without ever needing to be relaxed — `match` partitions *which VMs* a class applies to, the field-per-VM invariant here is unaffected either way.

`vmClassMode` and `syncMode` do not exist in this model. The ceiling always applies to a VM's effective configuration, whatever combination of class-derived and inline values that resolves to; nothing here is controller-written, since `ConfigTarget` feasibility is checked independently (§3.4), not mirrored into any spec.

**The same check also bounds other classes in the same namespace, not only VMs.** When a new or edited `VirtualMachineClass` is admitted, if any existing governing class in that namespace already applies to one of its `spec.zones`, the candidate class's own declared ranges must fall within that governing class's ranges for the shared field(s) and zone(s) — the identical per-field subset comparison, applied to a class object instead of a VM. This is what makes a "flex" class the guardrail not just for free-form VMs but for every other class an admin defines alongside it in the same namespace, without a new mechanism. Cross-*namespace* bounding (a provider-authored class constraining a tenant-authored class in a different namespace) is a different boundary and is not what this paragraph covers — see §8 (**Phase 2**).

This same conditional extends to `spec.extraConfig.entries` (see [`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md)): a candidate class's literal entries are checked against an applicable governing class's `extraConfig.allowed`/`denied` rules the same way its numeric ranges are checked against that governing class's ranges. This closes an otherwise-real gap — without it, a class author could bake a key a governing class denies directly into their own `entries`, bypassing the rule entirely by never going through a VM's inline edit path at all. As with everything else in this paragraph, this only applies when a governing class exists and its zones overlap the candidate's; a namespace with no governing class constrains nothing here either.

**Exception to the same-field-conflict rule, Phase 2 only: a class and any class it lists in `parentClassRefs` (§8) may both govern the same field for overlapping zones.** This is not the ambiguous case §3.2's conflict check exists to catch, because §8.4's subset validation already guarantees the child's declared range is nested inside the parent's for any field both declare — they were never independently authored and potentially divergent, they're provably ordered. For a field the child declares, checking a VM against the child (the more specific ceiling) is sufficient: compliance with the child provably implies compliance with the parent, so nothing needs merging or picking a winner. For a field the child is silent on, §8.3's resolution rule applies instead: the parent's declaration is the effective ceiling, exactly as if the child didn't exist. Either way there is exactly one authoritative range per field, never two to reconcile. This is what makes it sound for a class to set its own, tighter `governs` on a zone a class it lists in `parentClassRefs` already governs — see §8.3a for the worked case. **This exception does not exist in Phase 1**, since `parentClassRefs` doesn't ship until Phase 2 — in Phase 1, two governing classes with an overlapping field are always a conflict, with no exception.

### 3.2a Classless VMs never get `className` backfilled

A VM may omit `spec.className` entirely and configure hardware inline. When a governing class supplies its ceiling, `spec.className` is **not** written to record that — the VM stays honestly classless in spec. Which governing class(es) applied is recorded only in the status condition from §3.5, never in spec. This avoids silently mutating a field the user chose to leave unset, and avoids forcing an answer on whether a classless VM is now pinned to a governance-only class's `ClassInstance` lineage (§3.7), which would make that instance load-bearing instead of the harmless bookkeeping it is today.

**Admission invariant:** a VM must have either `spec.className` set, or at least one governing class whose `spec.zones` covers the VM's zone — the specific zone if `spec.zone` is set, or at least one zone the namespace touches otherwise (§3.1b's existential check). Neither is not an option — a classless VM with no applicable governing class anywhere is rejected outright (UC9), the same way a non-existent `className` reference is rejected today.

### 3.2b Class-vs-class drift: a governing class can tighten out from under an already-validated class

The class-vs-class subset check above only runs when the narrower candidate class itself is created or edited. It never revisits an already-existing class when a *different* class — its governor — is edited afterward and tightens. A t-shirt class `large` (`cpus: 4-8`) can validate cleanly against a governing class `flex` (`cpus: 2-16`) today, and then `flex` gets edited down to `cpus: 2-4` tomorrow — nothing re-checks `large` at that point, even though it's now no longer a subset of its governor.

**This is not a VM-safety gap.** §3.2 already establishes that VM admission always re-derives, live, checking a VM's effective value directly against whichever governing class currently applies — never against the selectable class's own declared range as a proxy. A VM requesting `cpus: 8` via `large` after `flex` tightens is still correctly rejected, checked against `flex`'s current range, exactly as it would be if `large` didn't exist. `large`'s own staleness has no bearing on that outcome.

**The actual gap is that `large` itself goes silently misleading.** Its spec still advertises `cpus: 4-8`, and nothing on the object says that range is no longer honored by its governor — an admin has no signal short of a VM actually hitting the now-narrower `flex` ceiling and getting rejected against a class other than the one they thought they were using.

**Fix: the existing `VirtualMachineClass` controller re-runs the same per-field subset check on every reconcile, and surfaces a status condition when a class no longer nests inside its governor(s).** This mirrors the shape already settled for zone drift (§3.1b, §3.3a): reconciliation re-checks something the webhook can only validate at the moment of a specific edit, and reacts by making the mismatch visible. Unlike zone drift, there is no enforcement lever here — no `existingVMs`-style knob, and the class is not deleted, blocked, or prevented from continuing to exist and be referenced. It stays exactly as usable as it was, including by VMs already using it. The condition is purely discoverability: it tells an admin their class's declared range no longer matches what its governor actually permits, so the mismatch is visible on the class itself rather than only showing up indirectly, one rejected VM at a time, against a different object.

### 3.3 Enforcement combination rule

Because more than one governing class can apply to the same VM, and each carries its own `governs.enforcement`, a combination rule is needed: **`Deny` wins if any applicable governing class sets it.** This is the only safe default — an `Allow` on one governing class must never override a `Deny` an admin placed on another.

### 3.3a Existing-VM drift policy (`governs.existingVMs`)

When a governing class's ceiling tightens (a range narrows, or a new governing class starts applying), some already-admitted VMs may fall outside the new ceiling. Customer feedback on the right behavior here is genuinely split, so this is a per-governing-class knob, not a single fixed default with no override:

- **`AllowOnViolation`** (default) — the VM is left alone. It keeps running, and a power cycle (Off → On) succeeds normally. Non-compliance is surfaced as a status condition, not remediated.
- **`DenyPowerOn`** — vm-operator never initiates a power state change for compliance reasons, ever. This value only declines a **user-requested** transition to `PoweredOn` while the VM is out of compliance with an applicable governing class; the request is rejected the same way any other invalid `spec.powerState` edit is rejected today. A VM that is already `PoweredOn` when the ceiling tightens keeps running, untouched, until whoever operates it next asks to power it off and back on.

**New** objects — VMs, or narrower classes under §3.2's or (Phase 2) §8.4's subset checks — are never grandfathered: an object violating an applicable ceiling at creation time is rejected at admission, unconditionally. `existingVMs` only governs what happens to objects that were compliant when admitted and later fall out of compliance because a ceiling tightened around them.

**Carve-out for the zone dimension (§3.1b):** the "never grandfathered" guarantee above only holds when the applicable ceiling is knowable at creation time. For a VM created with no zone, it isn't — admission can only run the existential check (§3.1b), not a direct check against a specific zone's governing class. If the zone placement later assigns turns out non-compliant, that VM is *not* rejected retroactively; it is treated exactly like drift, using this same `existingVMs` knob, enforced by the reconciler (§3.1b) rather than by the admission webhook. This is the one case where a newly-created object can end up subject to `AllowOnViolation`/`DenyPowerOn` despite never having been "compliant when admitted and later drifted" in the ordinary sense — it was never checked against a concrete zone until placement ran.

**Combination rule, same shape as §3.3:** when multiple applicable governing classes disagree, the strictest wins — `DenyPowerOn` overrides `AllowOnViolation` if any applicable governing class sets it, for the same reason an `Allow` must never override a `Deny`.

**This mechanism already covers drift after a VM's zone is assigned, with nothing further needed.** §3.1b's existential check and `Constraints.Zones` intersection only address the moment of initial placement; they say nothing about a governing class changing, or compatibility shifting, after the VM already has a fixed zone. Nothing new needs to: `governs` is always re-derived live at every admission (§3.2), not just at creation, so this drift policy already applies identically whether the VM has had a zone since the moment it was created or only since placement assigned one later. There is one drift mechanism, not one for the pre-placement window and a different one for after.

### 3.4 Hardware feasibility is unrelated to governance, and always checked

Whether a zone's hardware can actually satisfy a class's constraints (a requested vGPU profile exists on that cluster, the hardware-version ceiling is achievable) is answered by intersecting against `ConfigTarget.status` at admission, exactly as designed in the base spec — regardless of whether the class in question governs anything. Governance answers *"is this permitted here,"* not *"is this possible here."* Both checks run; neither substitutes for the other. This is also why a class needs no zone information for feasibility's sake independent of `governs`: a class requesting a GPU profile only present in one zone will simply fail the `ConfigTarget` check anywhere else, whether or not `governs` mentions that zone.

**Feasibility, specifically, needs no new mechanism for automatic zone placement (§3.1b), because admission and placement already share the same `ConfigTarget`-derived source of truth.** A zone that fails this check was never going to be a placement candidate regardless of what governance decides — this is the one axis where "placement will do the right thing" already holds today, with nothing to add. Governance is the axis that needed the explicit `Constraints.Zones` treatment in §3.1b, precisely because it has no representation in `ConfigTarget` at all.

### 3.4a Visibility: status and rejection messages never exceed what a class already declared

A tenant able to read a `VirtualMachineClass` must not be able to infer cluster hardware capability the class itself doesn't already expose. Two places this can leak, both need the same rule:

- **Status.** `status.zones` (§3.1a) reports only which declared zones are currently usable — a boolean-shaped signal per zone, not a copy of any `ConfigTarget` field. This class's status will never carry a hardware value the class's own `spec` did not already declare, unlike the resize document's design for `VirtualMachineConfigPolicy`, which synced hardware fields from `ConfigTarget` directly into policy status.
- **Admission/validation error messages.** A rejection worded as `"zone-b lacks profile nvidia-a100-40c"` discloses the same capability information a status field would, just through a different channel, and is just as much a leak if the caller has no other way to see that profile exists. An earlier draft of this section proposed making the level of detail conditional on a `SubjectAccessReview` against whatever the message would name — dropped in favor of one consistent rule, since a caller-dependent message is one more thing to get wrong and the plain version is enough to stay actionable:
  - **Generic about any capability or value the caller did not themselves supply** — `ConfigTarget` data, another class's actual range values (§3.2, §8) — regardless of who is asking: `"the requested vGPU profile is not available in zone-b"`, not naming what zone-b has instead; `"requested cpus (8) exceeds an applicable governing class's max"`, not naming the governing class's other fields.
  - **Fully specific about the caller's own input** — the field they set and the value they requested — since that's information the caller already has: `"requested cpus (8) exceeds class large's max (8)"` is fine when `large` is the caller's own selected class.

This applies equally to the hierarchical-governance check in §8 (**Phase 2**): a class rejected for exceeding the range of a class it lists in `parentClassRefs` gets a pass/fail and which of its own fields is out of range, never that class's actual range values.

### 3.5 Discoverability: the cost of dropping the second kind

Under the thinned-policy design this file previously described, "what governs zone-a" was one object to read. Under this model, it is a filter across every class in the namespace — a genuine auditability regression, and the one place this collapse is worse than keeping two kinds. Two different questions need two different answers here:

- **"Why was *this* VM constrained this way?"** — a condition on the VM naming which governing class(es) and which field actually bound its effective configuration, computed at admission time, so a rejection or a near-limit VM is explainable without a namespace-wide scan.
- **"What governs zone-a, independent of any particular VM?"** — `status.governingClasses` on the `Zone` object (`external/tanzu-topology`): a **list** of class names (a list even in phase 1, where it will typically hold one entry) that a controller computes by watching `VirtualMachineClass` changes in namespaces that touch that zone, republished purely as a read-only, browsable mirror. This is not a new authoritative object — the classes' own `spec.governs`/`spec.zones` fields remain the only source of truth; `Zone.status` is a convenience view computed the same way §3.2's admission check computes it, and §3.2's check itself must never read it back (same reasoning as §3.1a: a computed mirror must never become an input to the thing it mirrors, or the two can drift and the mirror becomes silently wrong).

Whatever UI or CLI surfaces class information should use these two views rather than a namespace-wide scan performed ad hoc each time — but neither one is a new stored *policy* object; both are read-only projections of the classes that already exist.

### 3.6 The chooser-list consequence, returning through a different door

A class that exists purely to govern (never intended to be selected — `ceiling-only` in UC3 below) is namespace-scoped exactly like a selectable one, and nothing distinguishes it in a listing. Any UI or `kubectl get vmclass` view that presents every class in a namespace as a deployable option will offer a governance-only class as if it were a t-shirt size. This is the same chooser-list problem raised earlier in the broader discussion about merging Class and Policy, returning here because the two kinds are now actually one. There is no schema-level fix that does not reintroduce a discriminator; the practical mitigation is a naming convention (a class meant only to govern is named and described accordingly) and, optionally, a purely cosmetic UI hint — not a field that changes validity or meaning, only what a picker chooses to display.

### 3.7 `ClassInstance` for a governance-only class

The class-instance controller mints an immutable snapshot for every class unconditionally today, keyed by a hash of its spec, regardless of whether any VM references it. That is unaffected by `governs` and remains harmless for a class no VM ever selects — instances are cheap and the mechanism does not require a class to be referenced to exist. Skipping instance creation for governance-only classes is a possible later optimization, not a correctness requirement.

---

## 4. `spec.externalID`: identity partitioning (Phase 1)

`VirtualMachineClass` has been namespace-scoped since v1alpha6, so `(namespace, name)` is already a globally unique identity on the Kubernetes side. The vCenter-native object a class corresponds to (created via `VirtualMachineClasses.create()`) does not get that for free — VC's object space is flat, and VCFA disambiguates today via org-ID-prefixed names, applied at VAPI-create time, before any Kubernetes namespace is even chosen (one VC-native class can later be copy-distributed into several k8s namespaces by the same-namespace-only copy-down mechanism described in §8.2). `(namespace, name)` cannot substitute for a dedicated identity field because the collision it needs to prevent exists a step before a namespace is assigned at all.

An earlier draft of this section bundled this need together with hierarchical governance under one `scope` field with a `Provider`/`Tenant` kind. That framing is dropped: identity partitioning and hierarchy are unrelated concerns, Supervisor and vm-operator have no concept of "provider" or "tenant" to begin with, and the tiering added nothing a plain opaque field doesn't already say more simply. `spec.externalID` stands entirely on its own, independent of `parentClassRefs` (§8) and of Phase 2.

### 4.1 Why this needs a dedicated field, and its shape

**An opaque `spec.externalID` string, immutable after create, enforced by webhook** — the same treatment already established in this repo for `spec.id` on other managed objects (constitution: *"Managed object IDs (`spec.id`) are immutable after create; enforce via webhook"*). This is a closer, stronger, already-adopted precedent than anything borrowed from outside the repo, which is why it wins over the alternatives considered along the way:

- **Not a label.** A label's one real advantage — reverse lookup via `client.List` + label selector, to answer "does a k8s object for VC-object X already exist in this namespace" — is neutralized by a field indexer on a spec field, which is the repo's own established pattern for exactly this kind of lookup (`operator-best-practices.md`'s field-indexer-plus-mapper pattern). A label also risks a charset mismatch (RFC-1123 label values are constrained to ≤63 chars, `[A-Za-z0-9._-]`) against whatever format VCFA's org-prefixed ID actually takes, and — like any label — is editable by anyone with ordinary edit access, with no immutability story at all.
- **Not an annotation.** Annotations fit "opaque, caller-specific data the platform doesn't validate" (this repo already uses that pattern for the round-trip conversion annotation in `api/utilconversion`), but that precedent is safe specifically because conversion data is *derived* — regenerable if lost or corrupted. An identity token used to prevent a real-world VC-side collision is the opposite: nothing about an annotation stops two classes from carrying the same value or a client stripping it, and a collision is exactly the failure this field exists to prevent. A spec field with a webhook-enforced immutability check is the only option that can actually guarantee anything.
- **Uniqueness is not vm-operator's job.** The webhook enforces immutability, matching `spec.id`; it does not enforce that no two classes share the same `externalID`. VCFA already guarantees that at VAPI-create time (its org-prefixing scheme), before any of these classes exist in a namespace at all — vm-operator has no additional information that would let it check uniqueness more meaningfully than the system that mints the value in the first place.

`externalID` and `parentClassRefs` (§8) are fully independent: a class can set either, both, or neither, and nothing about one affects validation of the other, or which phase it ships in.

### 4.2 Upgrade path: existing classes in VC and Supervisor

**This design assumes namespace-scoped resources throughout — that is not being reopened here.** Everything below happens within `VirtualMachineClass`'s existing namespace-scoped identity model; nothing about the upgrade path introduces or relies on cross-namespace lookup.

The upgrade path leans on a fact already confirmed in `vmclass_kube.go`: wcpsvc, today, sets `dst.Name = src.ID` when it materializes a VC-native class into a namespace. That means **every already-existing `VirtualMachineClass` object's `metadata.name` already is its VC-side ID.** Nothing needs to be renamed to introduce `externalID` — the value it would hold already lives in the field that matters for compatibility.

- **Existing classes: name is never changed, `externalID` is backfilled to match it.** For every `VirtualMachineClass` already materialized in Supervisor, `metadata.name` keeps its current value — including the ugly, org-ID-prefixed ones — and `spec.externalID` is set to that same value, not a newly minted one. This can happen as a one-time patch or lazily on first reconcile; either way it changes nothing observable. No VM referencing `spec.className` by name is affected, because the name never moves.
- **New classes, created after wcpsvc adopts the §4.1 shape, get the clean split.** Only classes materialized going forward have `dst.Name` set to a clean, human-facing, namespace-unique name while `dst.Spec.ExternalID` carries VC's own identifier separately. Existing classes are not retroactively renamed to match this — this design does not require or attempt a rename of anything already in production, since renaming an existing class would break every VM already referencing it by name.
- **`spec.className` in a VM always resolves by the class's k8s object name, never by `externalID`.** This does not change, for old or new classes alike. `externalID` exists purely for wcpsvc/VCFA's own VC-side bookkeeping and back-mapping; it is never a valid value for `spec.className`, and no lookup path resolves a VM's class reference through it. This is worth stating as an explicit invariant, since `externalID` is in some sense the more "durable" identifier and a future caller could otherwise be tempted to reference by it instead of by name.
- **No forced migration gate.** Because `externalID` is optional and additive, a `VirtualMachineClass` that has not yet been backfilled behaves exactly as it does today — nothing about `governs`, or any other part of Phase 1, depends on `externalID` being set. The backfill can proceed independently of, and does not block, the rest of the schema migration in §6.

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

No class in this namespace sets `governs`. A VM sets `spec.className` to one of the three; nothing governs inline edits because nothing governs anything. This is byte-for-byte today's model — see §6 for why that matters for migration.

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

A VM sets `spec.className: medium` (CPU 4, memory 8Gi by default), then edits `spec.resources.cpu` inline toward a higher value. Admission resolves the VM's effective configuration and checks it against the intersection of every class in the namespace whose `governs` is set and whose `zones` matches the VM's zone — here, just `large` (CPU min/max 8, memory min/max 32Gi), because it is the only class with `governs` set. The edit succeeds up to `large`'s ceiling and is rejected beyond it. `medium`'s own bounds are irrelevant to the check once a governing class exists — its role was only to supply the starting values. `large` is simultaneously deployable on its own via `className: large` and the ceiling for VMs that started from something else; nothing about it distinguishes those two roles.

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

No other class exists in the namespace, so there is nothing to select — every VM here is classless (subject to the admission invariant in §3.2a) and its inline configuration is bounded entirely by `ceiling-only`'s ranges. `ceiling-only` is an ordinary `VirtualMachineClass` object; nothing marks it as different in kind from a selectable one, and it would remain fully functional as a governing ceiling even if some VM did reference it directly. Per §3.6, a namespace like this should name and describe such a class so a chooser does not present it as an ordinary size — there is no schema field that does this for you.

### UC3b — Sizing unconstrained, ExtraConfig restricted (falls out for free)

An admin who wants no sizing ceiling at all but *does* want to restrict ExtraConfig keys across every zone the namespace touches sets `governs` on a class carrying only the ExtraConfig rule, with no numeric fields set, listing every zone in `spec.zones` explicitly (§3.1 — there is no implicit "namespace-wide" shorthand):

```yaml
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata: {name: no-guestinfo-extraconfig, namespace: team-a}
spec:
  extraConfig:
    denied: [{type: Glob, key: "guestinfo.*"}]
  zones: [zone-a, zone-b, zone-c]   # every zone this namespace touches, listed explicitly
  governs: {}   # governs.zones omitted -> defaults to spec.zones
```

This is not a separate mechanism — it demonstrates that intersecting over every governing class in the namespace composes without every concern needing its own bespoke object, and a class can govern with only some of its fields meaningfully set.

### UC5 — Resize via class swap and resize via inline edit are the same check

Not asked directly but implied by UC2: whichever path a day-2 resize takes — swapping `className` to a different preset, or editing a field inline — the effective configuration that results is checked against the same intersected ceiling. Neither path is privileged over the other, since there is one evaluation point, not two.

### UC7 — No governing class anywhere (the migration default)

A namespace with classes but none setting `governs` behaves exactly as today: classes are fixed presets, nothing governs inline edits, and the old `vmClassMode` default (class-derived configuration is authoritative) falls out automatically because there is nothing to check it against. This is what every migrated namespace looks like on day one (§6).

### UC9 — Classless VM, no governing class covers its zone

A classless VM whose zone matches no governing class at all has no ceiling to resolve against. Per §3.2a's admission invariant and the research document's Q4/Q12, the recommendation is to deny in this case — classless inline configuration is the newly-introduced, unvetted surface, and should require an explicit governing class to exist before it is usable, not default open in its absence.

---

## 6. Migration plan

### 6.1 What migrates trivially

- **Every existing scalar field becomes `{min: <value>}`.** `max` and `default` both default to `min`, so a migrated class is indistinguishable in behavior from its v1 self.
- **Round-trip safety.** Per the research document §6.3, this requires the round-trip annotation mechanism (`api/utilconversion`) to be wired up for `VirtualMachineClass`'s conversion functions before this version ships — not after. An older typed client (§3.3 of the research document) reading and partially updating a migrated class must not silently collapse a genuine range back to a single value it cannot express.
- **No migrated class sets `governs` or `externalID`.** Per UC7, this is itself the correct migrated state — existing behavior is preserved exactly, not approximated, and nothing needs to be created for an existing namespace to end up in the right state. Both are opt-in fields (§3, §4) and have no bearing on classes that never set them. `parentClassRefs` (§8) does not exist in Phase 1 at all, so there is nothing to migrate for it yet.
- **`VirtualMachineConfigPolicy` is removed as a kind.** Its CRD, the `Zone` controller's fan-out that creates one per zone, and the associated capability-gated controller registration are deleted rather than migrated. Because the enforcement controller and webhook for it were never shipped (research document R7), there is no live enforcement behavior to carry forward — only the empty object-creation fan-out, which is deleted along with the kind.

### 6.1a Zones backfill is required, not opt-in

Unlike `governs` and `externalID`, `spec.zones` moving to an always-explicit, opt-in model (§3.1) is not something an unmigrated class can simply ignore. Under the new rule, a class with no explicit `zones` is available nowhere — a real regression for every class that exists today, none of which set `zones`, since zone-scoped availability never previously constrained anything the way it does here. Migration must backfill `spec.zones` on every existing class with the full list of zones its namespace currently touches, so a migrated class remains available everywhere it was before — the same "byte for byte, today's behavior" bar the rest of this migration holds itself to.

**This needs a new capability upstream of vm-operator, not just a one-time backfill script.** The namespace-creation vAPI's `VmServiceSpec.VmClasses` field (`com.vmware.vcenter.namespaces`, consumed in wcpsvc's `workload_impl.go`) is a flat `Set<ID>` — a class name is either usable in the namespace or it isn't, with no per-zone structure at all. The namespace's existing `Zones[].VmReservations` field looks adjacent but is a different concept entirely: it's a per-zone **capacity reservation** for a class (how many of a class are reserved in a given zone), not a statement of which zones a class is *eligible* for — the two must not be conflated when scoping this work.

**A minimal backfill only needs "every zone this namespace touches," not per-class scoping**, and that alone is enough to satisfy the "byte for byte, today's behavior" bar: every existing class gets `spec.zones` populated with the namespace's full zone list, since none of them were ever zone-restricted before. But the more precise, forward-looking fix — and the one actually needed for an admin to provision a *new* class scoped to fewer than every zone in the namespace, which is the entire point of `governs.zones` existing — is extending `VmServiceSpec.VmClasses` itself from a flat class-name set to a structure that pairs each class name with the list of zones it's being added to. That's new vAPI surface, not just new data flowing through the existing shape: it needs a schema change to `com.vmware.vcenter.namespaces`'s CreateSpec/UpdateSpec/Info (and the corresponding wcpsvc translator), not merely a change to what the migration script backfills. This is new integration scope on the wcpsvc/VCFA side, not something vm-operator's own migration step can do in isolation, and it needs to land before or alongside step 3 in §6.3.

### 6.2 The hard blocker: `configSpec`

This is the one part of the migration that is not a mechanical schema transform. Existing, shipped classes use `spec.configSpec` today — the class documentation shows customer-facing examples setting `extraConfig`, `deviceChange`, `firmware`, and raw `numCPUs`/`memoryMB` through it. Per §6.1 of the research document, v2 carries no opaque `configSpec` field at all. Migrating a class that currently sets something through `configSpec` with no typed equivalent yet has no clean answer, and the three branches need an explicit choice rather than an implicit one:

1. **AllowOnViolation existing values.** A migrated class keeps whatever it expressed through `configSpec`, frozen and readable but not newly editable, until a typed equivalent exists — new classes cannot set anything through it at all.
2. **Block migration on field parity.** Do not ship v2 until every `configSpec` construct present in the installed base has a typed replacement.
3. **Accept breakage.** Some existing classes cannot be expressed in v2 and must be re-authored; migration surfaces which ones.

**This needs a survey of real-world `configSpec` usage before any of the three is chosen.** That is a data-gathering question, not a design question, and answering it late is exactly the kind of thing that stalls a migration after the schema work is already committed.

**The external authoring path is a second half of the same blocker, not a separate one.** Per §3.3/I4 of the research document, the existing vCenter-facing class interface still carries `configSpec` as an opaque value and passes it straight through to the Kubernetes object today. Closing the opaque surface on the CRD does nothing if that path keeps writing it — the same restriction needs to land on both sides, and needs the same owning-team conversation as I4.

### 6.3 Sequencing

**Phase 1 (9.1.3):**

1. Land the round-trip annotation wiring for `VirtualMachineClass` conversion (mechanical, no dependency on the rest).
2. Run the `configSpec` usage survey; pick one of the three branches in §6.2.
3. Ship the range/constraint schema, `governs`, `spec.externalID`, and the always-explicit `spec.zones` model additively, before removing `configSpec` — every existing class continues to validate unchanged once the §6.1a zones backfill has run, and no class sets `governs` yet.
4. Remove the `VirtualMachineConfigPolicy` kind, its CRD, and the `Zone` fan-out that creates it.

**Phase 2 (9.2):**

5. Ship `parentClassRefs` once the VAPI-strategy conversation (research document, and the parallel investigation into a VAPI v2 for VM classes) confirms the shape round-trips cleanly.

**Once both phases are stable:**

6. Cut the version that actually removes `configSpec`, informed by whichever §6.2 branch was chosen. This does not need to wait for Phase 2 specifically, only for step 2's chosen branch to be fully executed.

---

## 7. Open items this file adds to the research document's §7

### Phase 1

- The `configSpec` usage survey (§6.2) — a required investigation, not yet in the research document's I-list.
- The chooser-list mitigation for governance-only classes (§3.6) — naming convention versus a cosmetic UI hint, and who owns deciding which.
- Whether the discoverability regression in §3.5 needs tooling beyond a VM-level status condition (e.g., a computed "effective bounds for this zone" view) before this ships, or whether that can follow later.
- Whether a purely advisory, non-authoritative "intended zones" hint on a *selectable* class is worth adding later for earlier admission-time rejection (a class requesting a GPU profile that exists in no zone the namespace touches could fail fast with a clear message instead of at placement) — explicitly not the same mechanism as `governs`, and not required for correctness, since `ConfigTarget` feasibility checking already covers it.
- Whether the per-field conflict check in §3.2 needs its own dedicated status condition (distinct from §3.5's "why was this VM constrained" condition) when it specifically fires on a same-field conflict between two governing classes, so that failure mode is diagnosable as "these two classes conflict," not just "rejected."
- Whether an in-namespace governing class ever needs to *exempt* a specific sibling class from the §3.2 class-vs-class subset check (a deliberate "break-glass" class that wants to exceed a general ceiling) — not currently supported, and not raised as a requirement, but worth flagging before the admission logic is written as unconditional.
- The storage/disk-size range model flagged in "Phasing" above — `instanceStorage.volumes` and the `reservedProfileID`/`reservedSlots` coupling need their own pass before §2's range schema can be called complete for those fields.
- Whether the `spec.externalID`/schema shape survives translation into a future VAPI v2 for VM classes — the planned VAPI-team conversation about transformation logic for Supervisor 2.0 compatibility should confirm this before it ships, per §6.3.
- **The governance-compliance check (§3.1b) needs to be exposed as an exported `pkg/` function, callable from both the admission webhook's validator and the VM reconciler's power-state step.** The constitution's webhook rule keeps validator logic in unexported types under `webhooks/virtualmachine/validation`, which a controller can't import directly — the actual check needs to live somewhere both can reach, consistent with "controllers delegate business logic to `pkg/`." Confirming this is a straightforward extraction, not a restructuring of the existing validator package, is real scoping work.
- **The `VirtualMachineClass` controller needs a new reconcile-time check for class-vs-class drift, surfacing a status condition when a class no longer nests inside its governor (§3.2b).** The controller already exists; this adds one more reconcile pass reusing §3.2's existing per-field subset check. Visibility-only, no enforcement lever — VM admission was never unsound against a stale class.
- **The VM reconciler needs a new step that re-checks governance compliance and can withhold the power-state-reconcile step for `DenyPowerOn` (§3.1b, §3.3a).** This is what makes `DenyPowerOn` apply uniformly to a VM's first power-on and any later one, and is what makes single-VM and group-VM placement need no separate design — both paths converge on this same reconcile-time check regardless of how the VM got its zone.
- **The namespace-creation vAPI needs a schema change, not just new data (§6.1a).** `com.vmware.vcenter.namespaces`'s `VmServiceSpec.VmClasses` is a flat class-name set today, with no per-zone structure — it needs to become class name paired with the zones it's being added to. This is new scope on the wcpsvc/VCFA side, not vm-operator's to build alone. The one-time migration backfill doesn't strictly need this (it can populate `spec.zones` with every zone the namespace touches, using only the namespace's existing zone list), but provisioning a *new*, deliberately zone-scoped class does — without this, `governs.zones` narrower than "every zone in the namespace" has no way to be set from VC/VCFA at all.

### Phase 2 (9.2, tenant-based VM classes)

- **Deletion symmetry, flagged for follow-up, not resolved here:** §8.4 and §3.2a both settle on "deletion is unblocked, and dependents are only ever loosened, never invalidated" — for a class referencing another class via `parentClassRefs` (§8, Phase 2), and for a VM referencing a class (§3.2a, Phase 1). Whether that single rule is the right one for both cases, or whether the two deserve different treatment, needs another pass.
- Whether `parentClassRefs`' cycle-freedom check (§8.3, §8.4) is a sufficient guardrail on its own, or whether an explicit maximum chain depth is worth adding once real chains longer than 2 show up in practice.
- Whether the `parentClassRefs` shape survives translation into a future VAPI v2 for VM classes — the same VAPI-team conversation as the Phase 1 item above should confirm this before it ships, per §6.3 step 5.

---

## 8. `parentClassRefs`: hierarchical governance (Phase 2 — release 9.2, tenant-based VM classes)

**Nothing in this section ships in Phase 1.** It is designed now, at the same rigor as everything above, specifically so the Phase 1 schema (§2, §4) doesn't need a breaking change to accommodate it later — see "Phasing" at the top of this document.

The use case is: a higher-authority class needs to bound what a lower-authority class is allowed to declare (checked once, at the lower class's creation), and needs to supply the effective ceiling for any field the lower-authority class doesn't bother declaring itself (resolved live, at every VM admission). This is what makes tenant-based VM classes possible — a tenant admin further subdividing an allocation a higher authority carved out for them.

An earlier draft bundled this together with identity partitioning (§4) under one `scope` field with a `Provider`/`Tenant` kind. That framing is dropped: Supervisor and vm-operator have no concept of "provider" or "tenant," and shouldn't acquire one just to express this — those are VCF Automation tenancy concepts, foreign to this layer, and the tiering added nothing a plain object reference doesn't already say more simply.

### 8.1 Shape

```yaml
spec:
  parentClassRefs:                             # optional; list, phase 2 caps length at 1 by webhook (§8.2)
    - name: region-a-pool-1                    # same-namespace object reference (§8.2)
```

- Optional. A class with nothing set here participates in none of this — ordinary single-tier usage (UC1–UC9) is unaffected.
- `parentClassRefs` entries are a struct (`{name: ...}`), not a bare string, even though `name` is the only field needed at launch. Per the constitution, additive API changes are not safe once a field has shipped (k8s issue #111703) — a struct can gain a field later (e.g. `namespace`, if same-namespace-only is ever revisited) with no breaking change; a scalar-string list cannot become a list of structs without one.
- The field is list-typed for the same reason: nothing about the resolution rule in §8.3 depends on there being exactly one entry, and a future multi-governor phase (`governs.match`) may need more than one. The list is capped at length 1 by webhook validation at launch, not by the schema — the cap can be lifted later with no API change. See §8.3 for why the cap exists.

### 8.2 Same-namespace only

**A class and every class that lists it in `parentClassRefs` must live in the same namespace.** This is a deliberate scoping decision, not an oversight, and it resolves a real conflict: `VirtualMachineClass` is namespace-scoped, and a VM's `className` resolves within its own namespace — there is no live mechanism for one namespace to reference a class object living in another. (`VirtualMachineClassBinding`, which existed for exactly this — a class defined once, made usable across namespaces — was retired when `VirtualMachineClass` became namespace-scoped in v1alpha6.) Requiring cross-namespace hierarchy would mean either reintroducing that retired indirection, or inventing a new one; requiring same-namespace avoids re-opening a decision this repo already made and walked back.

This does not block the real use case (a higher-authority pool bounding what gets exposed into a separately-provisioned workload namespace) — it relocates *whose job* that is. **Distributing a class definition into the namespace(s) that need it is already solved outside vm-operator, by copying, not by live reference.** The namespace-provisioning layer that materializes `VirtualMachineClass` objects into a target namespace today does exactly this: it resolves a class's spec (from a shared catalog or from vCenter) and writes an independent copy into the target namespace via `CreateOrPatch`, then deletes stale copies it previously created when the desired set changes. There is no cross-namespace reference anywhere in that path — every namespace ends up with its own self-contained object. If two classes descended from one another need to be validated against each other, whatever system wants that (VCFA's translator, or the namespace-provisioning controller, or a future consumer) is responsible for placing both copies into the same namespace, the same way it already places any other class into a namespace today. vm-operator's job is only to validate the relationship once both objects are co-located — never to reach across a namespace boundary itself.


### 8.3 Resolution is a live, per-field walk with no fixed depth

For a VM's effective configuration, resolve each field independently:

1. Start at the VM's directly-applicable governing class.
2. If that class declares the field, it is the sole authority for it — validate the VM against it and stop.
3. If it doesn't, and it has an entry in `parentClassRefs`, move to that class and repeat from step 2.
4. If the chain runs out with no class having declared the field, the field is ungoverned by this chain (some other, unrelated governing class may still cover it via §3.2's ordinary per-field check).

**Nothing caps how many hops this can take**, even though each individual class's `parentClassRefs` list is capped at one entry: `C → B → A` is a chain of depth 2 built entirely out of single-entry lists, and nothing stops a longer one. The webhook does not need to reject long chains — it needs to reject *cycles*: at any class's creation/update, walking its `parentClassRefs` chain must never revisit the class being validated. That check is the only depth-related guardrail this needs; the walk itself is written to run until it terminates or a cycle is caught, not until some fixed hop count is reached.

**Why the length-1 cap on each class's own list, then:** if a class had two parents, was silent on some field, and its two parents disagreed on what that field's range should be, there is no rule to pick a winner — §3.2 already rejected merging/intersecting as a resolution strategy, and nothing here reopens that. The cap defers a genuinely unspecified semantic rather than guessing one. (An alternative that would allow lifting the cap without inventing a merge rule: require declared fields to be disjoint across a class's multiple parents, so no field ever has two candidate suppliers — not adopted here, just noted as the shape a future relaxation could take.)

**This resolution is always live, never baked into a status field.** A resolved-and-cached "effective ceiling" written to `status` at reconcile time would reintroduce the exact bug already rejected for `status.zones` in §3.1a: a change to a higher class (a `memory` ceiling widening, say) would need a controller to notice and re-patch every descendant's cached status before it took effect, and until that reconcile completes, VM admission would be checking against a stale value — worse than a single extra live `Get`, not better, since the controller path is a watch → workqueue → reconcile → status-patch → informer-propagation chain, several cache hops removed from the source of truth, versus the webhook's own single cache hop when it resolves live.

A read-only computed mirror in `status` — the fully resolved, per-field effective ceiling for a class, aggregating its own declared values with whatever it inherits through the chain — is worth adding for exactly the reason §3.5 already flags as a discoverability regression: without it, seeing a class's true effective ceiling means manually reading every ancestor. It's explicitly excluded from ever feeding admission, the same treatment `status.zones` and `Zone.status.governingClasses` already get. Because every class in the chain lives in the same namespace (§8.2), a reader who can see the referencing class can already see every ancestor directly — so aggregating them into one status doesn't expose anything a live read of each ancestor wouldn't. That holds specifically under two conditions worth stating explicitly rather than assuming silently: (1) ordinary namespace-scoped read RBAC on `VirtualMachineClass` remains the norm — per §8.6, there is no planned mechanism that would scope read access to specific class names, but if that ever changed, a tenant could have read access to the child but not a specific named ancestor, and the mirror would bypass that; (2) §8.2's same-namespace-only decision remains in force — if that's ever relaxed, this argument stops applying immediately. Populating the mirror needs a controller watching class changes and requeuing anything that references the changed class (the field-indexer + mapper pattern already used elsewhere in this repo, `operator-best-practices.md`); the cascade terminates because cycles are already rejected at creation. Status fields carry none of spec's API-compatibility risk, so specifying the field now and building the reconciler later costs nothing — this is a Phase 2 detail to schedule, not a blocker to the mechanism itself.

**Field absence must be distinguishable from field-zero.** Because "the child didn't declare this field" and "the child declared this field as its zero value" are different states with different resolutions (fall through to the parent, versus this class has an actual zero-valued ceiling), the range structs need `+optional`/`omitempty` pointer-shaped fields, not value types — already the repo's convention, now load-bearing for correctness here rather than only a style rule.

#### 8.3a Does a parent need to govern too?

No — `governs` (§3.2) and `parentClassRefs` (§8) are independent properties, so whether the referenced class governs has no bearing on whether the referencing class may. Two concrete cases:

- **Parent governs, child also governs the same zone, and both declare the same field.** Covered by §3.2's exception: the child's range is already guaranteed nested inside the parent's for that field (§8.4's subset check), so checking a VM against the child alone is sufficient.
- **Parent does not govern at all, child does.** A higher-authority class hands out plain, selectable t-shirt-size classes — no `governs` on any of them — and a class derived from one via `parentClassRefs` sets `governs` on itself. **This is valid**; nothing about the reference requires the parent to govern before the child can. The child's own declared ranges are still validated as a subset of the parent's at creation (§8.4) regardless of whether either class chooses to govern anything.

The general rule underneath both: `parentClassRefs` only ever constrains what a class is *allowed to declare*, and supplies a fallthrough value for what it doesn't (§8.3); it never determines whether that class *governs* anything (§3.2, entirely independent, opt-in at each class).

### 8.4 Validation at creation

- **Existence.** Every entry in `parentClassRefs` must resolve to an existing class **in the same namespace** (§8.2) — an ordinary `Get`, not a cross-namespace or elevated-privilege lookup. The referenced class must exist before the referencing class can be created; there is no eventually-consistent path.
- **No cycles.** Walking the chain from the class being created/updated must never revisit itself (§8.3).
- **Subset, only on fields both declare.** For any field the referencing class declares, its range must fall within the resolved effective range of the class(es) it references — the same per-field subset comparison §3.2 already needs, applied here as a one-shot check at creation/update, using the referenced class's *current* live values, not a value persisted at check time. A referencing class never has to restate a field it doesn't intend to tighten — silence on a field means "defer to whatever the chain resolves to for it" (§8.3), not "no opinion, no restatement needed, no constraint." §3.4a's rule applies: the referencing class's author sees pass/fail and which of their own fields is out of range, never the referenced class's actual values.
- **Deletion — unblocked, matching today's actual behavior, no finalizer.** A referenced class can be deleted freely, even while other classes still list it in `parentClassRefs` — this is the setting reached after two reversals in earlier drafts of this section (disassociation, then a blocking finalizer), and it's worth recording why it's the right one, not just the latest one. Two facts settle it:
  - **This is already how `VirtualMachineClass` deletion works today**, independent of anything in this design: the current validating webhook's `ValidateDelete` unconditionally allows deletion (`webhooks/virtualmachineclass/validation`), regardless of whether any VM references the class. Adding a finalizer that blocks deletion only for referenced classes would be a new, narrower special case inconsistent with every other class's deletion behavior — and namespace deletion cascades through every object in a namespace at once, so a finalizer that depends on sibling objects finishing their own deletion first is exactly the kind of thing that can leave a namespace stuck in `Terminating` for longer than it would today, a real behavior regression with no offsetting correctness benefit.
  - **A class is not needed after a VM is provisioned from it, for the VM's own template values.** A VM is pinned to an immutable `VirtualMachineClassInstance` snapshot at admission (already-existing mechanism, `pkg/util/vmopv1/resize.go`), not to the live, mutable class — so deleting a class a VM was created from does not retroactively change that VM's hardware.
  - **The one concrete consequence worth naming, not solving here (tracked in §7's Phase 2 open items):** deleting a referenced class is a *silent loosening* for anything that fell through to it under §8.3 — if `B` was silent on `memory` and deferred to `A`'s `1–4Gi`, deleting `A` leaves `memory` ungoverned for `B` going forward, with no edit made to `B` itself. This is the same "a deleted governing class simply stops contributing going forward" behavior §3.2a already accepts for ordinary governance, just reached through a reference instead of ambient scope — but it is worth flagging explicitly, since the trigger (an edit to a different object) is less visible than an edit to the object whose behavior changed.

### 8.5 Why this isn't a Kubernetes owner reference

Kubernetes owner references were a natural first instinct for this hierarchy, and §8.2's same-namespace decision removes the mechanical objection that ruled them out in an earlier draft of this section (cross-namespace owner references simply don't fire — no longer relevant once parent and child are required to be co-located). They're still not used here, because the one thing an owner reference actually buys you — cascade-delete of dependents when the owner is deleted — is wrong for this relationship regardless of namespace topology: deleting a referenced class must not delete every class that references it (§8.4). Kubernetes offers no owner-reference mode that means "delete the owner, leave dependents exactly as they are" — background propagation orphans dependents (drops their owner-reference entry, doesn't delete them, but still not the semantic wanted here since nothing marks *why* the reference disappeared), and foreground propagation actively cascades the delete. Since §8.4 settled on "deletion is unblocked and only loosens what fell through, never deletes anything," a plain, inert field (`parentClassRefs[].name`, just a name the referencing class's own creation-time validation once looked up, and its admission-time resolution re-reads live) says exactly that with no special GC behavior attached, which an owner reference cannot do without also opting into one of GC's two behaviors.

Separately, `Zone`-as-owner-of-`VirtualMachineClass` (raised as a brainstorm) remains the wrong tool even where mechanically legal (a cluster-scoped owner over a namespaced dependent is allowed): the class/zone relationship is many-to-many (one class can be available in several zones; several classes can apply to one zone), and ownership implies cascade-delete, which this relationship shouldn't have — deleting a `Zone` must not delete every class that happened to list it in `spec.zones`.

`parentClassRefs` stays a plain object reference list, checked at creation/update and resolved live at admission, never an owner reference.

### 8.6 RBAC: trust in wcpsvc's single write path

Dropping the `Provider`/`Tenant` tier removes a self-declared-authority spoofing risk that never really needed protecting — a `parentClassRefs` target is objectively checked (existence, cycle-freedom, subset), never merely asserted, so there was nothing to spoof in the first place.

`VirtualMachineClass` values, including `parentClassRefs`, are **trusted as written**. The sole write path in production is wcpsvc's own service account, itself gated by VC's/VCFA's own authentication and RBAC upstream of ever reaching the Kubernetes API — ordinary tenants do not create or edit `VirtualMachineClass` objects directly today. This is expected to remain the model for `VirtualMachineClass` v2 for the foreseeable future, not a temporary Phase 2 gap: there is no use case driving a need to open direct tenant authorship of `VirtualMachineClass`, so no RBAC-hardening mechanism is planned or designed against that possibility.

---

— Faisal + Claude
