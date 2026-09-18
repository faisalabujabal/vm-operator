# One Pager: `VirtualMachineClass` v2

- **Status**: Draft — for discussion, no decision sought
- **Audience**: engineering leadership, adjacent teams (VCFA, wcpsvc/VC, partner/UI consumers), anyone who needs the shape of the change without the implementation-level detail
- **Details**: [`vmclass-v2-design.md`](./vmclass-v2-design.md) is the authoritative design; this document summarizes it and should not be treated as a source of truth for anything it and the design doc disagree on

---

## Business problem

VM sizing and configuration policy today is split across mechanisms that don't compose:

- `VirtualMachineClass` gives DevOps users fixed, selectable t-shirt sizes, but has no way to also act as a ceiling on what a VM can be edited to inline.
- `VirtualMachineConfigPolicy` was designed to be that ceiling, as a second kind — but its enforcement controller and webhook were never shipped, so it exists today only as an empty per-zone object with no real behavior.
- VCFA maintains its own, separate hardware-policy construct outside vm-operator entirely, which risks becoming a third, duplicate governance mechanism if this redesign and that construct both end up shipping.
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

- **Not shipping tenant-based hierarchical classes in Phase 1.** The mechanism is fully designed now; it ships in Phase 2 (release 9.2).
- **Not resolving the `configSpec` migration outcome here.** Whether existing `configSpec` usage is frozen-but-readable, blocks migration until typed parity exists, or is accepted as a breaking change is a data-driven decision pending a usage survey, not a design decision made in this document.
- **Not hardening RBAC for `VirtualMachineClass` writes.** The only production write path is wcpsvc's own service account; there is no use case driving tenant-direct authorship, so no RBAC mechanism is being designed against that possibility.
- **Not redesigning VM/VM-group placement.** Placement (`Constraints.Zones`, `GroupPlacement`) stays entirely governance-unaware, on purpose — see Architecture Areas below.
- **Not introducing VCFA's tenancy concepts (`provider`/`tenant`) into vm-operator.** Every mechanism here is expressed as plain object references and scoped fields, not a new authority tier.

## Big picture

There is one kind, `VirtualMachineClass`, with two independent, optional properties that any class may carry in any combination:

- **Selectable** — can a VM reference this class by name?
- **Governing** — does this class's constraints act as a ceiling on VMs in some zone scope, via an optional `spec.governs` block, regardless of what those VMs reference?

A class like `large` can be an ordinary t-shirt size *and* the ceiling every VM in a zone is measured against, just by also setting `governs` — no second object, no mode switch.

Governance is always resolved **live, per field, at every VM admission** — never by merging or caching an intersected ceiling. If two governing classes would otherwise constrain the same field for the same zone, that's rejected as a conflict at class-admission time, not silently resolved by a merge rule.

Zone scoping is **explicit and opt-in everywhere** (`spec.zones`, and `governs.zones` defaulting to it) — there is no implicit "applies namespace-wide" default, because that would let a class's governed scope change silently whenever a zone is added to or removed from the namespace.

A VM's zone can be unknown at admission (placement decides it later, for a single VM or as part of a `VirtualMachineGroup`). Rather than making placement itself governance-aware, admission does a cheap, fail-fast check ("is there any zone this VM could ever be compliant in"), and the VM's own reconcile loop is responsible for detecting and enforcing compliance once a concrete zone is known — the same mechanism either way, whether the VM was placed alone or as part of a group.

Identity partitioning (`spec.externalID`, for VC-side object identity) is fully decoupled from hierarchy — a class can set either, both, or neither, and existing classes migrate with zero renaming, since today's object name already is the VC-side ID.

## Architecture areas

- **Schema.** Scalar fields become `{min, max, default}` constraints; `spec.governs`, `spec.externalID`, and (Phase 2) `spec.parentClassRefs` are added; `spec.configSpec` is eventually removed.
- **Admission webhooks.** New per-field governance checks on both `VirtualMachineClass` (class-vs-class subset check) and `VirtualMachine` (VM-vs-governing-class check, plus the zone-existential check for zone-less VMs).
- **`VirtualMachineClass` controller.** New reconcile-time check that flags, via status condition, a class whose declared range no longer nests inside its governor's — a discoverability fix, not an enforcement change.
- **`VirtualMachine` reconciler.** New reconcile-time step that re-checks governance compliance once a VM's zone is known (regardless of single-VM or group placement) and can withhold the power-on step for `DenyPowerOn` — enforcement moves from "reject an admission-time edit" to "the reconciler declines to act," so it applies uniformly to a VM's first power-on and any later one.
- **Placement.** Deliberately unchanged. `Constraints.Zones` and `GroupPlacement` stay governance-unaware; governance is a detection-and-enforcement layer on top of whatever zone placement picks, not an input to picking it.
- **Migration.** Backfilling `spec.zones` on every existing class, deciding the `configSpec` migration path, and removing the `VirtualMachineConfigPolicy` kind entirely (its enforcement path was never shipped, so there's no live behavior to preserve).
- **Upstream (wcpsvc/VCFA).** The namespace-creation vAPI needs a schema change to carry per-class zone scoping for classes provisioned going forward (today it's a flat class-name set).
- **Discoverability.** A VM-level status condition explaining why it was constrained; a read-only `Zone.status.governingClasses` mirror; the class-drift condition above — all computed views, never a new source of truth.

## API changes

- **`VirtualMachineClass` (v1alpha7)**: numeric fields become range structs; add `spec.governs` (`zones`, `enforcement`, `existingVMs`), `spec.externalID` (opaque, immutable after create), `spec.parentClassRefs` (list of `{name}`, schema reserved in Phase 1, behavior ships in Phase 2); eventually remove `spec.configSpec`. New/changed status: `status.zones` (feasibility only), new conditions for "why this VM was constrained" and "class no longer nests inside its governor."
- **`VirtualMachineConfigPolicy`**: removed entirely, including its CRD and the `Zone`-driven fan-out that created one per zone.
- **`Zone`** (`external/tanzu-topology`): new `status.governingClasses`, a read-only, computed list.
- **`com.vmware.vcenter.namespaces` vAPI**: `VmServiceSpec.VmClasses` needs to change from a flat class-name set to class name paired with the zones it's being added to — needed for provisioning new, deliberately zone-scoped classes (the one-time migration backfill does not need this change).

## Open questions

**Phase 1**
- Outcome of the `configSpec` usage survey, and which of the three migration branches it implies.
- How much tooling the discoverability regression (auditing "what governs zone X" across a namespace) needs before ship versus after.
- Whether a "break-glass" exemption from the class-vs-class subset check is ever needed.
- The range model for storage/disk-size fields (`instanceStorage.volumes`, `reservedProfileID`/`reservedSlots`) — doesn't reduce cleanly to `{min, max, default}`.
- Whether `spec.externalID`'s shape survives translation into the in-progress VAPI v2 modeling effort for VM classes.
- Scoping the shared `pkg/` extraction needed so the governance-compliance check is callable from both the admission webhook and the reconcilers.
- Scoping the new namespace-creation vAPI schema change on the wcpsvc/VCFA side.

**Phase 2 (9.2, tenant-based VM classes)**
- Whether deletion symmetry (a referenced class can always be deleted; dependents are only ever loosened, never invalidated) should be identical for class-to-class and VM-to-class, or diverge.
- Whether cycle-freedom alone is a sufficient guardrail on `parentClassRefs` chains, or an explicit max depth is worth adding once real chains exist.
- Whether the `parentClassRefs` shape survives the same VAPI v2 translation question as `externalID` above.

---

— Faisal + Claude
