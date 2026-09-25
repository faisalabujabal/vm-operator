# One Pager: `VirtualMachineClass` v2

- **Status**: Draft design
- **Audience**: engineering leadership, adjacent teams (VCFA, wcpsvc/VC, vAPI, partner/UI consumers), anyone who needs the shape of the change without the implementation-level detail
- **Details**: [`vmclass-v2-design.md`](./vmclass-v2-design.md) is the authoritative Kubernetes-side design and [`vmclass-v2-vapi-and-storage.md`](./vmclass-v2-vapi-and-storage.md) the vCenter side; this document summarizes them and is not a source of truth where they disagree

---

## Phasing at a glance

| | Phase 1 — release 9.1.3 | Phase 2 — release 9.2 |
|---|---|---|
| Theme | One kind for presets and governance | Tenant-based VM classes |
| Kubernetes API | Range fields, `governs`, three-state zones, `externalID`, narrowed `configSpec`; `VirtualMachineConfigPolicy` removed | `parentClassRefs` (schema field reserved in Phase 1, behavior ships here) |
| Governance | One governing class per field per zone | Class hierarchies within a namespace; several classes governing one zone |
| vCenter side | vcdb column and migration, generated v2 class vAPI, namespace API zones, both class writers upgraded | Confirm `parentClassRefs` round-trips through the generated vAPI |

Phase 1 is designed so Phase 2 needs no breaking change.

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

There is one kind, `VirtualMachineClass`. Its **availability** (`spec.zones`) is the base: the zones where the class applies at all. Within those zones it can play two independent roles, either or both:

- **Selectable** — a VM can reference the class by name.
- **Governing** — the class's constraints act as a ceiling on VMs (via an optional `spec.governs` block), regardless of what those VMs reference.

Availability limits both roles: a class can't be selected, and can't govern, in a zone it isn't available in. A class like `large` can be an ordinary t-shirt size *and* the ceiling every VM in a zone is measured against, just by also setting `governs`.

**What it looks like.** An illustrative sample, not every field:

```yaml
# A t-shirt size that is also the ceiling for its zones.
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata:
  name: large                  # what VMs reference in spec.className
  namespace: team-a
spec:
  description: "Large general-purpose"
  externalID: "org-1234-large" # VC-side identity, immutable
  zones: [zone-a, zone-b]      # availability; unset = every zone
  hardware:
    cpus:
      min: 4
      max: 16
      default: 8               # starting size for VMs using this class
    memory:
      min: 16Gi
      max: 64Gi                # default omitted -> min (16Gi)
    devices:
      vgpuDevices:
        allowed: ["grid_v100d-4q"]
  extraConfig:
    denied:
      - {type: Glob, key: "guestinfo.*"}
  configSpec: {}               # only fields with no typed home; presets only
  governs:                     # makes this class a ceiling
    zones: [zone-a]            # unset = same as spec.zones
    enforcement: Deny
    existingVMs: AllowOnViolation   # or DenyPowerOn
status:
  zones: [zone-a, zone-b]      # zones with compatible hardware right now
---
# A fixed size: min only, so max = default = min.
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata: {name: small, namespace: team-a}
spec:
  hardware:
    cpus: {min: 2}
    memory: {min: 4Gi}
---
# Phase 2: a tenant class bounded by a provider class.
apiVersion: vmoperator.vmware.com/v1alpha7
kind: VirtualMachineClass
metadata: {name: tenant-medium, namespace: team-a}
spec:
  parentClassRefs:
    - name: large              # must fit inside large's ranges
  hardware:
    cpus: {min: 4, max: 8}     # memory not declared -> large's range applies
```

**Ranges and defaults.** Every sized field becomes `{min, max, default}`. `max` and `default` default to `min`, so a class that sets only `min` is exactly today's fixed t-shirt size; there is no separate "fixed vs. ranged" flag. If a constraint is set, the class always has a `default`, so a VM never falls back to an unknown vpxd default that could violate the range.

**Governance** is resolved **live, per field, at every VM admission** — including a VM that only sets `className` — never by merging or caching an intersected ceiling. A governing class also bounds every other class in its namespace the same way (a narrower size can't be authored wider than the ceiling next to it). In Phase 1, if two governing classes constrain the same field for a VM, the VM is rejected, naming both (Phase 2 relaxes this; see below). Deleting a governing class is allowed; its ceiling just stops applying.

**Zones** have three states, and come from the class's association with the namespace (one vCenter class is attached to many namespaces):

| Value | `spec.zones` (availability) | `spec.governs.zones` (governance) |
|---|---|---|
| unset | Every zone of the namespace, including zones added later | Same as availability |
| `[]` | Nowhere (attached, but not selectable and governs nothing) | Nothing |
| a list | Only those zones | Only those zones, within availability |

A class can only govern where it is available. Unset-means-all is exactly today's behavior, so existing classes need no zone backfill.

**Unplaced VMs.** A VM's zone can be unknown at admission. Admission does a cheap, fail-fast check ("is there any zone this VM could be compliant in"), and the VM's reconcile loop enforces compliance once a zone is known — the same for single VMs and VM groups. Enforcement is never destructive: vm-operator never changes a VM's power state for compliance reasons; the strictest knob (`DenyPowerOn`) only declines a *user-requested* power-on while the VM is non-compliant.

**`configSpec`** keeps only fields vpxd supports that have no typed field yet. It is used for presets and defaults only, never as a governance ceiling; typed fields always win and are never merged with it; a field that has been elevated is not accepted in it. Each later elevation is a small migration.

**Identity.** `spec.externalID` holds VC's identifier, immutable after create. `metadata.name` (what VMs reference) and `description` (free text) are separate fields. Existing classes keep their name and get `externalID` = name; only new classes can have a different name. VCFA remains responsible for `externalID` uniqueness, as today.

## The vCenter side

- **vcdb stays the store** for class definitions: a class exists in vCenter independently of any Supervisor. The changes:

  | Table | Column | Change | Purpose |
  |---|---|---|---|
  | `vm_class_class_configs` | new JSONB column (name TBD, e.g. `spec_v2`) | **Added** | The canonical class-wide v2 spec: ranges, typed hardware, device and `extraConfig` policies, leftover `configSpec`, `governs` settings, `externalID` |
  | `vm_class_class_configs` | `cpu_count`, `memory_mb` | **Derived** | Written from the canonical `default`; kept for the existing API and Supervisors not yet on the new CRD version |
  | `vm_class_class_configs` | `cpu_reservation`, `memory_reservation` | **Derived** | Percentages computed from the canonical absolute requests |
  | `vm_class_class_configs` | `devices` | **Derived** | The existing API's view of the device `entries` |
  | `vm_class_class_configs` | `config_spec` | **Kept, no longer canonical** | Read by the migration; afterwards written as a derived copy for the existing API and older readers |
  | `vm_class_class_configs` | `external_id` | **Maybe** | Only if something needs to look classes up by `externalID` (open) |
  | `workload` | `vm_classes` | **Unchanged** | Still the list of attached class names |
  | `workload` | new JSONB column (name TBD, e.g. `vm_class_specs`) | **Added** | Per-class `zones` and `governedZones`. A class with no entry here means all zones, today's behavior |

  **All changes are additive:** no column is dropped or renamed, and no existing column changes shape, so an older wcpsvc can still read every row. All derived columns are written by one function in the same statement as the new column, so they can't drift. Zones and reservations are not stored on the class row; they belong to the namespace association.
- **A new v2 class vAPI**, generated at build time (reviewed and committed, not produced at runtime), over the same vcdb row as the existing `VirtualMachineClasses` API. The vmodl is generated from the CRD's Go types: a small tool emits a complete OpenAPI document from them, and the vAPI team's existing `openapi-compiler` turns that into vmodl. The CRD's own OpenAPI schema isn't used directly because it loses type information vmodl needs. This route depends on the vAPI team adding support for a few vmodl annotations; if they can't, the tool writes vmodl directly. The existing API stays, as an adapter: it shows `default` for ranged fields and translates `configSpec` in both directions. Both are gated by a wcpsvc capability, which works because wcpsvc upgrades before Supervisors.
- **The namespace vAPI change is additive.** A new per-class list carries zones; the existing `vmClasses` field keeps today's behavior (all zones, no governance).
- **Both class writers move to the new CRD version** — wcpsvc, and wcp-namespace-operator on etcd-backed Supervisors — with the version chosen per Supervisor, since one vCenter can manage Supervisors on different versions.
- **Validation** follows today's model: semantic and hardware-feasibility errors are reported asynchronously on the class; structural checks generated from the schema run synchronously so invalid data never reaches vcdb.

## Upgrade story

- Every existing scalar field becomes `{min: <value>}`; existing classes behave exactly as before.
- No zone backfill; no class sets `governs` until an admin adds it.
- wcpsvc moves each class's `configSpec` fields that now have a typed home into those fields and leaves the rest in `configSpec`, so no existing class breaks. The migration runs per class and never stops wcpsvc.
- `VirtualMachineConfigPolicy`, its CRD and the `Zone` fan-out are removed; there is no enforcement behavior to preserve.

## Phase 2: tenant-based VM classes (release 9.2)

Nothing here ships in Phase 1, but the Phase 1 schema is shaped for it.

- **`parentClassRefs`.** A class can reference a higher-authority class (`spec.parentClassRefs: [{name: ...}]`). The parent bounds what the child may declare, and supplies the ceiling for any field the child doesn't declare. This lets a tenant admin subdivide an allocation a provider carved out for them, without any provider/tenant concept in vm-operator.
- **Same namespace only.** Parent and child must be in the same namespace. Getting a provider's class into a tenant namespace is done by copying, as class distribution already works today, not by a cross-namespace reference.
- **Checked once, resolved live.** When a child is created or edited, its declared ranges must fit inside the parent's, and the chain must have no cycles. At every VM admission, each field is resolved live by walking the chain until a class declares it — never cached.
- **One parent per class, any depth.** Each class lists at most one parent (capped by the webhook, not the schema, so it can be lifted later); chains can be as long as needed.
- **Deletion is never blocked.** Deleting a parent is allowed, as class deletion is today; fields that fell through to it simply become ungoverned for the child. VMs are unaffected, since they are pinned to a class snapshot.
- **Several classes governing one zone.** A VM is allowed in a zone if it fully satisfies at least one class governing that zone; it can't combine one class's allowance with another's. Placement then picks among the zones that pass, as today.
- **Why not owner references:** deleting a parent must not delete its children, and Kubernetes owner references can't express that.

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
- When several classes govern one zone, whose `existingVMs` policy applies to a VM that satisfies none of them, and whose `default` fills a classless VM.
- Whether a governing class needs a way to forbid a non-empty `configSpec` in tenant-authored classes, since governance can't bound its contents.
- Deletion symmetry for class-to-class and VM-to-class references; whether cycle-freedom alone is enough for `parentClassRefs` chains.

---

— Faisal + Claude
