# One Pager: `VirtualMachineClass` v2

- **Status**: Draft — for discussion, no decision sought
- **Audience**: engineering leadership, adjacent teams (VCFA, wcpsvc/VC, partner/UI consumers), anyone who needs the shape of the change without the implementation-level detail
- **Details**: [`vmclass-v2-design.md`](./vmclass-v2-design.md) is the authoritative design; this document summarizes it and should not be treated as a source of truth for anything it and the design doc disagree on

---

## Business problem

VM sizing and configuration policy today is split across mechanisms that don't compose:

- `VirtualMachineClass` gives DevOps users fixed, selectable t-shirt sizes, but has no way to also act as a ceiling on what a VM can be edited to inline.
- `VirtualMachineConfigPolicy` was designed to be that ceiling, as a second kind — but its enforcement controller and webhook were never shipped, so it exists today only as an empty per-zone object with no real behavior.
- Classes carry an opaque `configSpec` escape hatch that cannot be validated, audited, or bounded by anything.
- There is no path today for a tenant admin to further subdivide an allocation a higher authority carved out for them (tenant-based VM classes) without inventing a new object or a new tier of authority.

## Goals

- Collapse "selectable preset" and "governing ceiling" into one kind, so a class can be either, both, or neither, with no separate policy object.
- Replace opaque `configSpec` with a fully typed, range-based schema (`{min, max, default}` per field).
- Make zone-scoped availability and governance scope explicit and opt-in everywhere, closing several classes of "changes with no edit to the object" drift.
- Give admins a per-governing-class knob for what happens when an already-compliant VM later falls out of compliance (`AllowOnViolation` / `DenyPowerOn`), rather than one fixed, unconfigurable behavior.
- Give VC/VCFA a dedicated, immutable identity field for VC-side object identity, independent of the Kubernetes object's name.
- Design hierarchical, tenant-scoped governance (`parentClassRefs`) now, at full rigor, specifically so the schema shipping first doesn't need a breaking change to accommodate it later.

## Non-goals

- **Not shipping tenant-based hierarchical classes in Phase 1 (release 9.1.3).** The mechanism is fully designed now; it ships in Phase 2 (release 9.2).
- **Not hardening RBAC for `VirtualMachineClass` writes.** The only production write path is wcpsvc's own service account; there is no use case driving tenant-direct authorship, so no RBAC mechanism is being designed against that possibility.
- **Not redesigning VM/VM-group placement.** Placement (`Constraints.Zones`, `GroupPlacement`) stays entirely governance-unaware, on purpose — see Architecture Areas below.
- **Not introducing VCFA's tenancy concepts (`provider`/`tenant`) into vm-operator.** Every mechanism here is expressed as plain object references and scoped fields, not a new authority tier.

## Big picture

There is one kind, `VirtualMachineClass`, with two independent, optional properties that any class may carry in any combination:

- **Selectable** — can a VM reference this class by name?
- **Governing** — does this class's constraints act as a ceiling on VMs in some zone scope, via an optional `spec.governs` block, regardless of what those VMs reference?

A class like `large` can be an ordinary t-shirt size *and* the ceiling every VM in a zone is measured against, just by also setting `governs` — no second object, no mode switch.

Governance is always resolved **live, per field, at every VM admission** — never by merging or caching an intersected ceiling. A governing class also bounds every *other* class defined in its namespace the same way it bounds VMs (the "class-vs-class subset check"), so a narrower t-shirt size can't be authored wider than the ceiling next to it. Class authoring is checked for this at the time a class is created or edited, but the real backstop is at VM admission: if a VM ends up caught between two governing classes that each constrain the same field, the VM's request is rejected, naming both classes, rather than silently picking a winner.

Zone scoping is **explicit and opt-in everywhere** (`spec.zones`, and `governs.zones` defaulting to it) — a class must explicitly list every zone it's available or governing in; there is no implicit "applies namespace-wide" default, because that would let a class's governed scope change silently whenever a zone is added to or removed from the namespace.

A VM's zone can be unknown at admission (placement decides it later, for a single VM or as part of a `VirtualMachineGroup`). Rather than making placement itself governance-aware, admission does a cheap, fail-fast check ("is there any zone this VM could ever be compliant in"), and the VM's own reconcile loop is responsible for detecting and enforcing compliance once a concrete zone is known — the same mechanism either way, whether the VM was placed alone or as part of a group. Enforcement is never destructive: vm-operator never initiates a power-state change for compliance reasons on its own — a running VM keeps running untouched, and the strictest knob (`DenyPowerOn`) only ever declines a *user-requested* power-on while a VM is non-compliant.

Identity partitioning (`spec.externalID`, for VC-side object identity) is fully decoupled from hierarchy — a class can set either, both, or neither, and existing classes migrate with zero renaming, since today's object name already is the VC-side ID. vm-operator only enforces that the field is immutable once set; VCFA remains responsible for guaranteeing the values it hands out don't collide, exactly as it already does today.

A class that exists only to govern (never meant to be picked by a user) is not visually distinguished from an ordinary t-shirt size in any listing or picker today — there's no schema field for this, only a naming convention, and it's called out as an open item below.

## Architecture areas

- **Schema.** Scalar fields become `{min, max, default}` constraints; `spec.governs`, `spec.externalID`, and (Phase 2) `spec.parentClassRefs` are added; `spec.configSpec` is eventually removed.
- **Admission webhooks.** New per-field governance checks on both `VirtualMachineClass` (class-vs-class subset check) and `VirtualMachine` (VM-vs-governing-class check, plus the zone-existential check for zone-less VMs).
- **`VirtualMachineClass` controller.** New reconcile-time check that flags, via status condition, a class whose declared range no longer nests inside its governor's — a discoverability fix, not an enforcement change.
- **`VirtualMachine` reconciler.** New reconcile-time step that re-checks governance compliance once a VM's zone is known (regardless of single-VM or group placement) and can withhold the power-on step for `DenyPowerOn` — enforcement moves from "reject an admission-time edit" to "the reconciler declines to act," so it applies uniformly to a VM's first power-on and any later one.
- **Placement.** Deliberately unchanged. `Constraints.Zones` and `GroupPlacement` stay governance-unaware; governance is a detection-and-enforcement layer on top of whatever zone placement picks, not an input to picking it.
- **Migration.** Backfilling `spec.zones` on every existing class, and removing the `VirtualMachineConfigPolicy` kind entirely (its enforcement path was never shipped, so there's no live behavior to preserve). **Blocking prerequisite, must resolve before implementation starts, not deferred indefinitely:** which of the three `configSpec` migration branches (frozen-but-readable, blocked on typed parity, accepted breakage) applies is a data-driven decision pending a real production-usage survey — documentation- and fixture-level scoping for that survey is in [`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md) §7, but the actual production data behind the decision still needs to be pulled from wcpsvc/VC before this can ship.
- **Upstream (wcpsvc/VCFA).** The namespace-creation vAPI needs a schema change to carry per-class zone scoping for classes provisioned going forward (today it's a flat class-name set).
- **Discoverability.** A VM-level status condition explaining why it was constrained; a read-only `Zone.status.governingClasses` mirror; the class-drift condition above — all computed views, never a new source of truth.

See "Dependencies on other teams" below for the wcpsvc/VCFA and VC-facing-API work this design assumes but doesn't own.

## API changes

- **`VirtualMachineClass` (v1alpha7)**: numeric fields become range structs; add `spec.governs` (`zones`, `enforcement`, `existingVMs`), `spec.externalID` (opaque, immutable after create), `spec.parentClassRefs` (list of `{name}`, schema reserved in Phase 1, behavior ships in Phase 2); eventually remove `spec.configSpec`. New/changed status: `status.zones` (which declared zones currently have compatible hardware — a "can this run here" signal, never "is this permitted here"), new conditions for "why this VM was constrained" and "class no longer nests inside its governor."
- **`VirtualMachineConfigPolicy`**: removed entirely, including its CRD and the `Zone`-driven fan-out that created one per zone.
- **`Zone`** (`external/tanzu-topology`): new `status.governingClasses`, a read-only, computed list.
- **`com.vmware.vcenter.namespaces` vAPI**: `VmServiceSpec.VmClasses` needs to change from a flat class-name set to class name paired with the zones it's being added to — needed for provisioning new, deliberately zone-scoped classes (the one-time migration backfill does not need this change).

## Dependencies on other teams

This design depends on work outside vm-operator that it can't complete alone:

- **VCFA's own, separate hardware-policy construct needs to be retired in favor of this model.** This is an assumption the design depends on, not something vm-operator can decide unilaterally — if that construct ships anyway, it and this design's `governs` mechanism exist side by side, reproducing the exact duplication problem this redesign exists to eliminate.
- **The namespace-creation vAPI (`com.vmware.vcenter.namespaces`) needs a schema change** so a newly-provisioned, deliberately zone-scoped class can be expressed at all — today it only carries a flat, zone-agnostic class-name list. Scoping this is wcpsvc/VCFA-side work.
- **The VC-facing class-authoring interface still passes `configSpec` straight through today.** Closing the opaque `configSpec` surface on the Kubernetes side does nothing if that path keeps writing it — the same restriction needs to land on both sides.
- **VAPI v2 modeling for VM classes** is being produced by a separate, parallel effort and is an in-scope dependency for Phase 1 (9.1.3); this design assumes its output and needs to confirm both `spec.externalID` and (Phase 2) `spec.parentClassRefs` survive that translation cleanly.

## Open questions

**Phase 1**
- Outcome of the `configSpec` usage survey, and which of the three migration branches it implies. Documentation- and fixture-level scoping already done in [`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md) §7 (five constructs confirmed documented/shipped, plus a NUMA-affinity-via-`extraConfig` finding) — real production-usage data from wcpsvc/VC is still the missing input.
- How much tooling the discoverability regression (auditing "what governs zone X" across a namespace) needs before ship versus after.
- Whether a "break-glass" exemption from the class-vs-class subset check is ever needed.
- The range model for storage/disk-size fields (`instanceStorage.volumes`, `reservedProfileID`/`reservedSlots`) — doesn't reduce cleanly to `{min, max, default}`. [`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md) §5 narrows this: `instanceStorage` has no inline `VirtualMachineSpec` twin today, so it can stay a plain preset (no range) for Phase 1; `reservedProfileID`'s "only valid when every ranged field has min == max" rule (referenced in `vmclass-v2-design.md`'s example YAML) is never actually written up in that document's §6 and needs a real validation rule specified; and `maxHardwareVersion`, shown in that same example YAML, doesn't exist on `VirtualMachineClass` in any shipped version today and needs to be added to the schema, not just the example.

**Phase 2 (9.2, tenant-based VM classes)**
- Whether deletion symmetry (a referenced class can always be deleted; dependents are only ever loosened, never invalidated) should be identical for class-to-class and VM-to-class, or diverge.
- Whether cycle-freedom alone is a sufficient guardrail on `parentClassRefs` chains, or an explicit max depth is worth adding once real chains exist.

---

— Faisal + Claude
