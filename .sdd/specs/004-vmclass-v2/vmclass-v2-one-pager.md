# One Pager: `VirtualMachineClass` v2

- **Status**: Draft design
- **Audience**: engineering leadership, adjacent teams (VCFA, wcpsvc/VC, vAPI, partner/UI consumers), anyone who needs the shape of the change without the implementation-level detail
- **Details**: [`vmclass-v2-design.md`](./vmclass-v2-design.md) is the authoritative Kubernetes-side design and [`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) the vCenter side; this document summarizes them and is not a source of truth where they disagree

---

## Business problem

VM sizing and configuration policy today is split across mechanisms that don't compose:

- `VirtualMachineClass` gives DevOps users fixed, selectable t-shirt sizes, but can't also act as a ceiling on what a VM can be edited to inline.
- `VirtualMachineConfigPolicy` was designed to be that ceiling, as a second kind — but its enforcement controller and webhook were never shipped, so it exists today only as an empty per-zone object with no real behavior.
- Classes carry an opaque `configSpec` blob that can't be validated, audited, or bounded, and that duplicates fields the class also has typed.
- There is no path for a tenant admin to further subdivide an allocation a higher authority carved out for them (tenant-based VM classes).

## Goals

- One kind: a class can be a selectable preset, a governing ceiling, both, or neither — no separate policy object.
- A typed, range-based schema (`{min, max, default}` per field), with the opaque `configSpec` narrowed to fields that have no typed home yet.
- Zone-scoped availability and governance, with "all zones" that follows the namespace as zones are added.
- A per-governing-class knob for what happens when an already-compliant VM later falls out of compliance (`AllowOnViolation` / `DenyPowerOn`).
- A dedicated, immutable VC-side identity field, separate from the Kubernetes name and the description.
- An upgrade with no hiccups: every existing class keeps working unchanged, with no backfill and no manual step.
- Hierarchical, tenant-scoped governance (`parentClassRefs`) designed now, so the Phase 1 schema needs no breaking change later.

## Non-goals

- **Not shipping tenant-based hierarchical classes in Phase 1 (release 9.1.3).** Designed now; ships in Phase 2 (release 9.2).
- **Not opening `VirtualMachineClass` to direct tenant authorship.** Classes are written by platform components only (wcpsvc, and wcp-namespace-operator on etcd-backed Supervisors), gated by VC/VCFA RBAC upstream.
- **Not redesigning VM/VM-group placement.** Placement stays governance-unaware, on purpose.
- **Not introducing VCFA's tenancy concepts (`provider`/`tenant`) into vm-operator.**

## Big picture

There is one kind, `VirtualMachineClass`, with two independent, optional properties:

- **Selectable** — can a VM reference this class by name?
- **Governing** — do this class's constraints act as a ceiling on VMs in some zones (via an optional `spec.governs` block), regardless of what those VMs reference?

A class like `large` can be an ordinary t-shirt size *and* the ceiling every VM in a zone is measured against, just by also setting `governs`.

**Ranges and defaults.** Every sized field becomes `{min, max, default}`. `max` and `default` default to `min`, so a class that sets only `min` is exactly today's fixed t-shirt size; there is no separate "fixed vs. ranged" flag. If a constraint is set, the class always has a `default`, so a VM never falls back to an unknown vpxd default that could violate the range.

**Governance** is resolved **live, per field, at every VM admission** — including a VM that only sets `className` — never by merging or caching an intersected ceiling. A governing class also bounds every other class in its namespace the same way (a narrower size can't be authored wider than the ceiling next to it). If two governing classes constrain the same field for a VM, the VM is rejected, naming both. Deleting a governing class is allowed; its ceiling just stops applying.

**Zones** have three states, and come from the class's association with the namespace (one vCenter class is attached to many namespaces):

| Value | `spec.zones` (availability) | `spec.governs.zones` (governance) |
|---|---|---|
| unset | Every zone of the namespace, including zones added later | Same as availability |
| `[]` | Nowhere (attached, but not selectable) | Nothing |
| a list | Only those zones | Only those zones, within availability |

A class can only govern where it is available. Unset-means-all is exactly today's behavior, so existing classes need no zone backfill.

**Unplaced VMs.** A VM's zone can be unknown at admission. Admission does a cheap, fail-fast check ("is there any zone this VM could be compliant in"), and the VM's reconcile loop enforces compliance once a zone is known — the same for single VMs and VM groups. Enforcement is never destructive: vm-operator never changes a VM's power state for compliance reasons; the strictest knob (`DenyPowerOn`) only declines a *user-requested* power-on while the VM is non-compliant.

**`configSpec`** keeps only fields vpxd supports that have no typed field yet. It is used for presets and defaults only, never as a governance ceiling; typed fields always win and are never merged with it; a field that has been elevated is not accepted in it. Each later elevation is a small migration.

**Identity.** `spec.externalID` holds VC's identifier, immutable after create. `metadata.name` (what VMs reference) and `description` (free text) are separate fields. Existing classes keep their name and get `externalID` = name; only new classes can have a different name. VCFA remains responsible for `externalID` uniqueness, as today.

## The vCenter side

- **vcdb stays the store** for class definitions: a class exists in vCenter independently of any Supervisor. One new JSONB column holds the class-wide v2 spec; the old scalar columns (`cpu_count`, `memory_mb`, reservation percentages) become derived copies, kept for the existing API and older Supervisors.
- **A new v2 class vAPI**, generated at build time from the CRD (reviewed and committed, not produced at runtime), over the same vcdb row as the existing `VirtualMachineClasses` API. The existing API stays, as an adapter: it shows `default` for ranged fields and translates `configSpec` in both directions. Both are gated by a wcpsvc capability, which works because wcpsvc upgrades before Supervisors.
- **The namespace vAPI change is additive.** A new per-class list carries zones; the existing `vmClasses` field keeps today's behavior (all zones, no governance).
- **Both class writers move to the new CRD version** — wcpsvc, and wcp-namespace-operator on etcd-backed Supervisors — with the version chosen per Supervisor, since one vCenter can manage Supervisors on different versions.
- **Validation** follows today's model: semantic and hardware-feasibility errors are reported asynchronously on the class; structural checks generated from the schema run synchronously so invalid data never reaches vcdb.

## Upgrade story

- Every existing scalar field becomes `{min: <value>}`; existing classes behave exactly as before.
- No zone backfill; no class sets `governs` until an admin adds it.
- wcpsvc moves each class's `configSpec` fields that now have a typed home into those fields and leaves the rest in `configSpec`, so no existing class breaks. The migration runs per class and never stops wcpsvc.
- `VirtualMachineConfigPolicy`, its CRD and the `Zone` fan-out are removed; there is no enforcement behavior to preserve.

## Architecture areas

- **Schema (v1alpha7).** Range structs; `spec.governs`, `spec.externalID`, (Phase 2) `spec.parentClassRefs`; three-state zones; narrowed `configSpec`.
- **Admission webhooks.** Per-field governance checks on `VirtualMachineClass` (class-vs-class) and `VirtualMachine` (VM-vs-governing-class, plus the zone-existential check).
- **`VirtualMachineClass` controller.** Flags a class whose range no longer fits inside its governor's — visibility only.
- **`VirtualMachine` reconciler.** Re-checks compliance once the zone is known and can withhold power-on for `DenyPowerOn`.
- **Placement.** Unchanged.
- **Discoverability.** `status.governingClassNames` and a "why constrained" condition on the VM; a read-only `Zone.status.governingClasses` mirror.
- **wcpsvc / vAPI.** vcdb column and migration, v2 endpoint and generator, v1 adapter, namespace API extension, writer upgrades.

## API changes

- **`VirtualMachineClass` (v1alpha7)**: range structs; `spec.governs` (`zones`, `enforcement`, `existingVMs`); `spec.externalID`; `spec.parentClassRefs` (schema reserved in Phase 1, behavior in Phase 2); three-state `spec.zones`; narrowed `spec.configSpec`. Status: `status.zones` (hardware-compatible zones — "can this run here," never "is this permitted here") and new conditions.
- **`VirtualMachine`**: `status.governingClassNames`, plus "why constrained" and compliance conditions.
- **`VirtualMachineConfigPolicy`**: removed.
- **`Zone`** (`external/tanzu-topology`): `status.governingClasses`, read-only.
- **New v2 class vAPI** (generated), and additive **`com.vmware.vcenter.namespaces`** changes (per-class zones).

## Dependencies on other teams

- **VCFA's separate hardware-policy construct needs to be retired** in favor of this model; otherwise both exist side by side, reproducing the duplication this design removes.
- **wcpsvc / VCFA:** the namespace vAPI extension, the vcdb migration, the v1 adapter, and the writer upgrade in wcpsvc.
- **wcp-namespace-operator:** the writer upgrade on etcd-backed Supervisors.
- **vAPI team:** the generator builds on their `openapi-compiler` (`--profile vmodl2`), which needs support for vmodl annotations (`@Released`, `@Vmodl1Type`, `@Resource`) and ideally named enums; a fallback exists if they can't commit in time.

## Open questions (highlights)

Full lists: design doc §7 and vAPI/storage doc §8.

**Phase 1**
- How an older client's partial update of a ranged class is handled (the existing API collapsing a range to one value), including clients that send every field on every edit.
- Whether inline edits on a VM that references a class require removing `className`.
- Filling unset fields of a classless VM from the governing class's `default`.
- What happens to an elevated field sent inside `configSpec` (auto-move, drop, or reject), and whether to rename `configSpec` to make its narrowed role obvious.
- Reservation rules: reserved profiles only for fixed classes.
- Generator details: the route (depends on the vAPI team), how `Quantity` maps to vmodl, and where the tool lives.

**Phase 2 (9.2, tenant-based VM classes)**
- When several classes govern one zone (a VM must fully satisfy at least one), whose `existingVMs` policy and `default` apply.
- Deletion symmetry for class-to-class and VM-to-class references; whether cycle-freedom alone is enough for `parentClassRefs` chains.

---

— Faisal + Claude
