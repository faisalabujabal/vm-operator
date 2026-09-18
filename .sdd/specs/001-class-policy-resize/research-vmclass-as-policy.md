# Research: VM Class as Config and Policy

- **Status**: Research and proposal — the direction has since moved past §4–§5 below; see [`vmclass-v2-design.md`](./vmclass-v2-design.md) for the current, decided shape (one kind; `VirtualMachineConfigPolicy` dropped entirely). This document remains the evidence and reasoning record.
- **Intended location**: child of *VM Service: Class Policy and Resize* (wiki page ID `2453059710`)
- **Relates to**: Epic vmop-3331 (`VirtualMachineConfigPolicy`), Epic vmop-3388 (VM compute configuration, classless and Telco workloads)
- **Audience**: principal engineers, architecture review

---

## 1. Summary

VM Service has two constructs that describe what a VM may be configured as:

- **`VirtualMachineClass`** — a named hardware preset a VM selects by name, holding exact values.
- **`VirtualMachineConfigPolicy`** — a per-namespace envelope constraining what a VM may request, including CPU and memory ranges, ExtraConfig key rules, hardware feature permissions, and a hardware-version ceiling derived from the cluster's real capabilities.

This document examines whether a single interface can serve both roles, and proposes one: **generalize `VirtualMachineClass` so that every field carries a constraint plus a default.** A fixed t-shirt size becomes the degenerate case where the constraints admit exactly one configuration. Attachment scope — VM, namespace, or zone — determines whether an instance acts as a VM's configuration source or as an ambient envelope. Under this model a single construct covers presets, day-2 resize latitude, and governance of classless VMs.

The proposal has three dependencies that must be settled before it can be assessed:

1. **`spec.configSpec` must be demoted.** It is an opaque `vim.vm.ConfigSpec` blob, and there is no coherent way to constrain or intersect one. Every field the envelope must govern has to be a typed field; `configSpec` becomes a privileged, explicitly ungoverned escape hatch. Elevation of exactly this kind is already underway on the VM side under vmop-3388, and — as of this revision — the vCenter-facing API for VM classes already treats `configSpec` as opaque too (§3.3), which is corroborating rather than novel.
2. **A vCenter-side representation is required.** `VirtualMachineClass` already has one, driven by an external service that authors and pushes classes into the cluster. Whether that interface can cleanly express a field that is either a scalar or a `{min, max, default}` structure is examined in §3.3 and remains partly open.
3. **A round-trip data-preservation mechanism must be in place before any lossy field ships.** This repository already has one, proven, and simply not yet wired up for `VirtualMachineClass` (§3.4). Two independent consumers depend on it: an external service holding an older typed client, and this repository's own VM backup/restore mechanism.

**What this document asks for**: review of the evidence and the alternatives ruled in or out in Appendix A. For the current concrete API shape, read [`vmclass-v2-design.md`](./vmclass-v2-design.md) directly — it now goes further than §4–§5 here by dropping `VirtualMachineConfigPolicy` as a kind rather than thinning it, via an additive `governs` property on `VirtualMachineClass` itself. The investigations in §7 still apply to whichever kind ends up carrying the range and permission fields.

---

## 2. Context

### Givens

- **vmop-3331 and vmop-3388 ship as one governed surface**, under a single owner for the boundary between them. The inline compute-configuration surface (`spec.resources`, `spec.cpuAdvanced`, `spec.memoryAdvanced`) and the governance that applies to it are one deliverable.
- **Classless VMs are in scope.** Epic vmop-3331 is titled *"Implement Policy Object to enable flexible workloads (classless VMs)"* (`tds.md:4`), and vmop-3388's compute surface exists to serve *"class-less VM workloads, such as Telco VNF workloads"* (`003-compute-config-reconcile/spec.md:15`). A VM may therefore reference a class or reference none.
- **Nothing is released to customers**, so the shape of both constructs remains open.

### The questions that prompted this

- The policy pipeline was designed with VCF Automation as the consumer. What is the model for a customer driving vCenter directly?
- If a VM Class interface already exists, what does a second construct add?
- Are we accumulating overlapping sources of truth for "what may this VM be" that will be costly to maintain?
- If `VirtualMachineClass` changes shape, does the existing external authoring path for VM classes keep working, and can new fields be selectively exposed to old versus new consumers of that path?

### Why classless VMs are decisive

A class cannot govern a VM that references no class. Any design in which governance lives *only* on a VM-selected class leaves classless VMs ungoverned. Governance must therefore be reachable without a class reference — which is satisfiable either by a separate ambient object or, as proposed here, by a class attached at namespace or zone scope rather than by a VM.

---

## 3. The current model

Factual description of what exists today, as input to the proposal.

### 3.1 `VirtualMachineClass`

- Namespace-scoped (`api/v1alpha6/virtualmachineclass_types.go:177`). `VirtualMachineClassBinding` is retired; it exists only in `v1alpha1`.
- Holds exact values: `spec.hardware.cpus` (integer), `spec.hardware.memory` (quantity), device lists, `spec.policies` (reservations and limits), `spec.configSpec` (opaque `vim.vm.ConfigSpec`), `spec.reservedProfileID` / `reservedSlots`, `spec.instanceStorage`.
- `VirtualMachineClassInstance` is an immutable snapshot keyed by an `xxhash` of the class spec, with an active-instance label moved on change (`controllers/virtualmachineclass/virtualmachineclass_controller.go:143,189-229,264-275`). A VM's `spec.class` reference must point at an **active** instance (`webhooks/virtualmachine/validation/virtualmachine_validator.go:701-720`).
- `spec.className` is mutable on update when `VMResize` or `VMResizeCPUMemory` is enabled (`virtualmachine_validator.go:722-733`); the swap is the shipped resize mechanism, detected via `LastResizedAnnotationKey` / `ResizeNeeded` (`pkg/util/vmopv1/resize.go`).
- Classless VMs are permitted for privileged accounts only (`virtualmachine_validator.go:652-670`); `IsClasslessVM` is a live code path with six branch sites in the vSphere provider (`pkg/util/vmopv1/vm.go:232-234`).
- Class content is authored externally and applied to the cluster via a service outside this repository; which principals may trigger that authoring path is outside this repository's scope.

### 3.2 `VirtualMachineConfigPolicy`

- Namespace-scoped, one per zone via `spec.zone` (required), created by a Kubernetes-to-Kubernetes fan-out from `Zone` (`controllers/infra/zone/zone_controller.go:181-186,221-245`). `Zone` is itself namespace-scoped (`external/tanzu-topology/api/v1alpha1/zone.go:222`).
- What `spec` expresses, grouped by the kind of statement each field makes:

| Kind of statement | Fields |
|---|---|
| Enforcement behaviour | `createMode`, `updateMode`, `powerOnMode`, `vmClassMode`, `syncMode` |
| Scope | `zone` |
| Numeric ranges | `numCPUCores`, `numNUMANodes`, `numSimultaneousThreads` (`IntRange`); `memory` (`ResourceQuantityRange`) |
| Hardware feature permissions | `smcPresent`, `sevSupported`, `sevSnpSupported`, `tdxSupported`, `hugePagesSupported`, `iommuSupported`, `rssSupported`, `udpRSSSupported`, `lroSupported`, `cpuLockedToMaxSupported`, `memoryLockedToMaxSupported` |
| Enumerated permissions | `latencySensitivityLevels`, `txRxThreadModels` |
| Device permissions | inlined `ConfigTargetDevices` |
| Key rules | `extraConfig.allowed` / `denied` |

  Several of these share names and types with `ConfigTarget.status`, where the same concepts appear as the cluster's actual capabilities rather than as permissions.

- Under `syncMode: ConfigTarget` (the default) a controller copies values from `ConfigTarget.status` into `policy.spec` (`plan.md` § I6). Under `syncMode: Disabled` the policy is managed manually.
- `vmClassMode` defaults to `AsPolicy`, under which class-derived configuration is not evaluated against the policy, preserving vSphere ≤9.1 behaviour (`tds.md:283`). `AsConfig` evaluates class-derived configuration identically to direct configuration.
- No VMODL representation: *"`VirtualMachineConfigPolicy` has no VMODL equivalent"* (`external/vim/doc/deploying-a-vm.md:12`), described instead as *"a namespace-scoped companion to `ConfigTarget`"* (`:63`). It is the only type in `external/vim/api/v1alpha1/` without a `// It corresponds to vim.<Type>` docstring.

### 3.3 How `VirtualMachineClass` is authored today (external consumer)

`VirtualMachineClass` already has a live vCenter-facing authoring path, external to this repository. This section records what that path implies for the proposal, without reproducing internal source locations.

- Class create/update is exposed as a vCenter API (vAPI) operation. The interface models `CreateSpec`, `UpdateSpec`, and `Info` structures for a VM class, versioned incrementally (fields have shipped and been deprecated across several releases of the interface without a breaking change) rather than through a scheme resembling Kubernetes API versions.
- **Every field in that interface is a plain scalar, an optional scalar, or a list/set.** There is no example anywhere in the interface of a field that is either a scalar or a range, and no discriminated-union idiom in use for any field. Nested structures do exist for compound values (an instance-storage descriptor, a device list), so a `{min, max, default}`-shaped nested structure is structurally plausible by analogy — but nothing already shipped proves it works cleanly, and this is the open half of I1.
- **`configSpec` is carried as an opaque, dynamically-typed value** tagged as corresponding to the vSphere `VirtualMachineConfigSpec` type, not decomposed into fields. This corroborates §6.1 independently: the vCenter-facing interface already treats `configSpec` as an escape hatch rather than a governed surface.
- The external service that owns this interface holds its own record of each class (separate from the Kubernetes object) and pushes creates/updates into the cluster's `VirtualMachineClass` object using a **typed client pinned to an older Kubernetes API version** than this repository's current one, and a **partial field-level update** (touching only hardware, resource requests, description, and `configSpec` — not a wholesale object replace). This is consistent with, and does not contradict, ordinary Kubernetes multi-version API behaviour: an older typed client continues to work against a newer stored version through the standard conversion mechanism, provided that mechanism preserves what the older client's type cannot express.
- **Two open items surfaced by this path, not yet resolved:**
  - *Write ownership.* If the proposal expects an admin to author range fields directly on a class, and the external service continues to authoritatively push scalar hardware fields onto the same object from its own record, the object has two writers with different sources of truth for different fields — the same shape of problem this document diagnosed in §3.2 for the current `VirtualMachineConfigPolicy`, relocated onto the class. This needs an explicit answer: is the Kubernetes object authoritative, or a projection of the external record? If the latter, the external record's own model needs to gain range fields before an admin can author them there.
  - *Propagation timing.* Evidence of a vAPI edit to an existing class's hardware promptly re-pushing the updated object to every cluster it is already attached to was not conclusively found; the only confirmed edit-triggered propagation observed was for capacity/reservation changes specifically, not general hardware edits. This should be confirmed with the owning team rather than assumed either way, since it affects how promptly a range edit would ever reach a running Supervisor.

### 3.4 Cross-version data preservation (round-trip conversion)

This repository already has a mechanism for exactly the risk raised in §3.3 and in R4/I1: an annotation-based round-trip helper (`api/utilconversion/conversion.go`) that a version's `ConvertFrom` calls to stash the full source object as JSON on the destination's annotations, and a later `ConvertTo` calls to recover it. This is already used by `VirtualMachine`, `VirtualMachineService`, `VirtualMachinePublishRequest`, and `VirtualMachineGroup`.

**`VirtualMachineClass`'s conversion functions do not use it** (`api/v1alpha1/virtualmachineclass_conversion.go`) — reasonably, since today every version of the type carries the same scalar-shaped fields and there is nothing to lose. If a future version introduces a field with no equivalent in an older version — a genuine `min ≠ max` range, for instance — round-tripping an object through an older-version client without this mechanism would silently collapse that field to whatever the older shape can express. Adopting the same pattern already proven on `VirtualMachine` closes this, and is additive engineering work rather than a new invention.

**A second, independent consumer of this same mechanism exists in this repository**: VM backup embeds a frozen, encoded YAML snapshot of the live `VirtualMachine` object — serialized at whatever API version this repository currently aliases as its working type — directly into the VM's vSphere `ExtraConfig` (`pkg/providers/vsphere/virtualmachine/backup.go:319-326`). Restore later decodes that snapshot and recreates or updates the `VirtualMachine` object from it (`docs/guides/backup-restore/README.md` § 4). A backup taken today is, from the perspective of a cluster several API versions later, exactly the same shape of old client as an external service holding an older typed client: a payload shaped like an old version that must still convert losslessly into whatever the storage version has become. Backup/restore does not itself recreate `VirtualMachineClass` objects — a restored VM references a class by name and expects it to already exist in the target namespace — so this finding bears on API-version durability generally, not specifically on the class redesign, but it reinforces the same requirement from a second direction: served-version retention and round-trip preservation are not optional once any field anywhere in these APIs becomes genuinely lossy across versions.

### 3.5 `ConfigTarget`

Cluster-scoped, controller-written from the vSphere `EnvironmentBrowser` over VMODL1/SOAP (`controllers/configtarget/configtarget_controller.go:168`). Carries the cluster's real capabilities: CPU and memory maxima, security flags, `maxHardwareVersion`, device categories. Because it is cluster-scoped, tenants cannot read it.

### 3.6 Existing governance pattern in-tree

`vsphere.policy.vmware.com` (`external/vsphere-policy/`, with a live per-VM reconciler at `pkg/vmconfig/policy/`) already models ambient-versus-explicit governance: `ComputePolicy.spec.enforcementMode` is `Mandatory | Optional`, and a Mandatory policy with `match` unset applies to all workloads in the namespace (`computepolicy_types.go:24-37`). `MatchSpec` is a recursive boolean matcher over image and workload attributes (`common_types.go:237-268`). `PolicyEvaluation` resolves which policies apply to a given workload shape. The group carries **no numeric bounds**, so it cannot host configuration envelopes without extension.

---

## 4. Proposal: VM Class as constraint plus default

**A class is a constraint plus a default. A VM is a resolved value.**

Every class field carries a constraint form. A fixed t-shirt size is the case where the constraints admit exactly one configuration and the defaults equal it — not a special mode, a degenerate one.

| Kind | Value form (on the VM) | Constraint form (on the class) |
|---|---|---|
| integer | `numCPUs: 4` | `{min: 2, max: 8, default: 4}` |
| quantity | `memory: 16Gi` | `{min: 8Gi, max: 64Gi}` |
| enum | `firmware: efi` | `[efi, bios]` |
| boolean | `hotAdd: true` | `[true]` or `[true, false]` |
| map | `extraConfig: {k: v}` | allow / deny key patterns |
| list | concrete devices | allow-list of types and profiles |

A t-shirt size is `{min: 4, max: 4, default: 4}`; defaulting rules (§5.2) keep that terse.

### What this yields

- **One interface** for presets and for envelopes, with one authoring and assignment workflow.
- **Bounded offerings** as a first-class object: "here is `large`, and you may flex 2–8 CPU" is one thing to author, one thing to read, and one thing to pick.
- **Classless VMs governed by the same construct**, via namespace- or zone-attached classes.
- **No role discriminator field** — meaning derives from where the class is attached.

### Relation to existing behaviour

Today's classes convert to `min == max == default`, and today's resize mechanisms are unchanged in kind: swapping `className` adopts a new constraint and re-defaults, which matches vmop-3388's existing statement that a new class is authoritative for the compute fields it defines. `vm.spec.resources` already documents field-level merge over the class (`api/v1alpha6/virtualmachine_types.go:1212-1218`), so value-over-default layering is an established idiom.

### Consequence for `VirtualMachineConfigPolicy`

If adopted, the separate policy object is subsumed: its range fields become class constraints, its permission fields become class allow-lists, and its ambient application becomes namespace or zone attachment. `ConfigTarget` is unaffected and remains the source of what the hardware can do; the envelope is intersected against it at admission rather than mirrored into any object's spec.

---

## 5. Attachment and composition semantics

**Superseded by [`vmclass-v2-design.md`](./vmclass-v2-design.md), which also supersedes the "keep both constructs" position below in §4 and Appendix A1/A5.** The decision recorded there: `VirtualMachineConfigPolicy` is dropped as a kind entirely. A class carries no `zones` field and no role discriminator; "selectable" (referenced by a VM) and "governing" (an optional `spec.governs` block making the same class's constraints an ambient ceiling) are independent properties of one kind, not two kinds or a discriminated one. See that file §1–§3 for the model and §4 for how it resolves each of the use cases raised in discussion. §5.2's defaulting rule below is unaffected and still applies.

### 5.2 Defaults

Three values per field, with fallbacks so the common case stays terse:

- `max` absent ⇒ `max = min`
- `default` absent ⇒ `default = min`

An explicit `default` matters for genuinely ranged classes: a user selecting `flex-large` and silently receiving the 2-CPU floor is a poor outcome. A blanket "always min" or "always max" rule is not proposed — `max` is unsafe for capacity and `min` is wrong for a bounded offering.

### 5.3 Composition

**Constraints intersect.** Where several ambient classes apply, a VM must satisfy all of them. Rationale: adding an envelope can then only narrow, never widen, so a newly added envelope can never be silently ignored; there is no precedence rule to document or get wrong; and it matches Kubernetes behaviour for multiple `LimitRange` objects in a namespace.

**Defaults do not intersect**, since a default must resolve to a single value. Proposal: ambient classes contribute constraints only; defaults come from the VM-referenced class, and for a classless VM the default resolves to `min` of the intersected range. This is deterministic and needs no precedence field.

**Empty intersection must be diagnosable.** Two envelopes — one capping at 8 CPU, one flooring at 16 — admit nothing. That is safe but opaque unless surfaced, so effective bounds and any contradiction should be published on status rather than only surfacing as VM rejections.

---

## 6. Requirements and dependencies

### 6.1 No opaque `configSpec`: every field must be explicitly exposed

A merged object requires every governed field to express both a value and a constraint. That is impossible for an opaque blob: there is no coherent intersection of two arbitrary `vim.vm.ConfigSpec` values. Unification is therefore not coherent while `configSpec` remains a governed surface.

**Decision: `configSpec` is not carried as an opaque, privileged escape hatch. It is not supported at all in that form.** Every field a class or a VM may set must be an explicit, typed field in the API. When vSphere's `VirtualMachineConfigSpec` introduces a field this platform wants to support, that field is added as a typed field before it can be used — there is no bypass for privileged accounts and no residual blob carrying anything else.

This is a stronger and cleaner position than demoting the escape hatch, and it removes the ambiguity a privileged-only blob would otherwise carry (is a privileged user's opaque `configSpec` inside or outside the envelope?). It has a precedent already in this repository: the `ConfigTarget` / `VirtualMachineConfigOptions` discovery pipeline already enumerates the vSphere configuration surface field-by-field rather than passing any of it through opaquely, so a closed, explicitly-typed surface is the direction this platform is already moving in, not a new posture invented for this proposal.

A forward rule follows from this and is worth adopting independently of the rest of the proposal:

> **Every elevated typed field ships with its constraint counterpart.** The VM gets the value form; the class gets the constraint form; they are added together.

vmop-3388 is already this elevation on the VM side — `spec.resources`, `spec.cpuAdvanced`, and `spec.memoryAdvanced` are typed replacements for settings that previously required `configSpec`.

**Consequences to plan for:**

- **An ongoing exposure backlog.** Every vSphere release that introduces a configuration option this platform wants to support requires a typed field to be added before any class or VM can use it — including for administrators. This is an accepted, ongoing cost of the decision, not a one-time migration; nothing is grandfathered as "for admins only" the way §6.1 originally proposed.
- **The vCenter-facing side of this needs the same closure, or it becomes the bypass.** §3.3 recorded that the existing vCenter-facing class interface carries `configSpec` as an opaque, dynamically-typed value and passes it through to the Kubernetes object's `spec.configSpec` verbatim. If the Kubernetes-side type stops accepting an opaque `configSpec` at all, that external path either needs the same restriction applied on its side, or it remains a route by which an opaque blob reaches the cluster regardless of what the Kubernetes API accepts. This is now a required, not optional, coordination point with whoever owns that path — folded into I4.

### 6.2 A vCenter-side representation is required either way

`VirtualMachineConfigPolicy` has no VMODL representation today. Exposing config policy through vCenter means creating one, whether governance lives on a separate object or on the class. **This vCenter-facing obligation is therefore common to both designs and is not a differential cost of unification.**

`VirtualMachineClass` already has such a representation, and §3.3 records what is now known about it: every field in the existing interface is a scalar, optional, or list/set, with no shipped precedent for a scalar-or-range field, though the interface's existing nested-structure idiom makes one structurally plausible. That is the concrete, narrowed form of the open question in I1 — not "can vCenter expose this at all," but "can this specific, already-used pattern express a `{min, max, default}` structure as cleanly as it expresses its existing nested types."

### 6.3 Round-trip data preservation must be in place before any lossy field ships

Restated from §3.4 as a requirement: before any version of `VirtualMachineClass` introduces a field with no equivalent in an older version, that version's `ConvertFrom`/`ConvertTo` pair must adopt the round-trip annotation mechanism already proven elsewhere in this repository. Two independent consumers depend on this — an external authoring path holding an older typed client, and this repository's own VM backup/restore mechanism — so the requirement is not specific to the external consumer and would need to hold even if that consumer did not exist.

### 6.4 Class authorship must be an admin-only guarantee

The proposal places governance on the class, which holds only if the principals subject to an envelope cannot author it. Whatever authors classes today — the external path described in §3.3 or any other — the proposal depends on that authority being restricted to administrators and not to the tenants an envelope is meant to constrain. See Q5.

---

## 7. Open questions and required investigation

### Required investigation

**I1 — vAPI/VMODL modelling (narrowed by §3.3, not yet closed).** The existing vCenter-facing class interface has no shipped precedent for a scalar-or-range field. Can it express a `{min, max, default}` nested structure as cleanly as its existing nested types (an instance-storage descriptor, a device list)? Does it support anything resembling a discriminated or `oneOf` type? To be taken to the owning team with this specific, narrower question rather than the original open-ended one.

**I2 — Reservation semantics.** `spec.reservedProfileID` with `reservedSlots` is a count of pre-committed capacity slots, and the external authoring path is the one that writes these onto the class. A slot cannot span a range, which suggests reserved classes must be constrained to `min == max`, and — because that path writes the field, not the Kubernetes side — the constraint would need to hold on both sides, not just as CRD validation. Confirm both halves are acceptable for the guaranteed-class product story.

**I3 — Assignment pipeline capacity.** Can the existing class authoring and assignment pipeline accept a second object type, and at what cost? This bears directly on whether one interface or two is cheaper in practice, independent of design merit.

**I4 — External write ownership (§3.3).** If an admin is meant to author range fields directly on a class, does that require the external authoring path's own record to gain range fields first, or does it remain the sole writer of scalar fields while a person or another process writes ranges directly to the Kubernetes object? Confirm whether hardware edits made through the external path promptly propagate to every cluster a class is already attached to, or whether that propagation is currently scoped more narrowly (e.g., to attachment and reservation changes only).

### Open questions

1. Is bounded or flexible sizing — flexing within one class — a primary workflow, or is day-2 resize predominantly swapping between fixed classes? The proposal's value scales with the answer.
2. Should `spec.className` remain mutable? It is mutable today under `VMResize` / `VMResizeCPUMemory` and is the shipped resize path. Making it immutable would remove that path; a narrower rule — VMs may not reference an ambient envelope class — is compatible with the proposal.
3. Multi-zone namespace: what governs a VM that has not yet been placed? Either intersect across all candidate zones (safe, potentially very restrictive, and able to produce an empty intersection from individually valid zones) or defer enforcement until placement. A namespace-wide envelope (§5.1) is the natural home for whatever governs the unplaced window.
4. A namespace with no classes assigned — may nothing be deployed, or anything? Equivalently: for classless VMs, is the default deny or allow?
5. Who may author a `VirtualMachineClass`, and is that guarantee actually enforced today across every path that can write one? (§6.4)
6. For each hardware feature permission in §3.2, is the underlying fact one the platform should discover into `ConfigTarget`, or is it purely an administrative permission? This determines whether the envelope has something to intersect against for that field.
7. What governs the order and pace at which new `VirtualMachineConfigSpec` fields get exposed as typed fields (§6.1)? Without a process, the exposure backlog risks becoming ad hoc, and a field a customer wants may simply not exist yet with no interim path.
8. Is mandatory baseline configuration inheritance — all VMs in a zone inheriting an ambient `configSpec` — a requirement? Concrete fields can be inherited as defaults; they cannot be meaningfully constrained.
9. Is any consumer expected to read the envelope as a *capability* surface — "what can this tenancy do" — as well as a permission surface? `ConfigTarget` is cluster-scoped and therefore unreadable by tenants, so if the envelope is the tenant-visible view of hardware capability, the proposal needs to preserve that role.
10. What enforcement granularity does the envelope need — a single setting, or separate control over create, update, and power-on?
11. Upgrade: existing VMs whose configuration exceeds a newly introduced envelope — grandfathered, or blocked at power-on?
12. Is the object authoritative in Kubernetes, or a projection of an external record? (§3.3, I4) This determines whether admin-authored range fields are even a coherent idea for a class whose scalar fields are pushed from elsewhere.

---

## Appendix A — Alternatives considered

Recorded so the reasoning is auditable.

### A1 — Two constructs: fixed `VirtualMachineClass` plus `VirtualMachineConfigPolicy`

**Superseded — not the direction taken.** [`vmclass-v2-design.md`](./vmclass-v2-design.md) collapses to one kind via an additive `governs` property rather than keeping the envelope as a separate object. Recorded here because the reasoning below is what any future reconsideration of two kinds would need to out-argue, in particular the auditability point in §3.5 of that file, which is the cost the single-kind design explicitly accepts. **Viable on its own terms.** Each object has one job and one schema shape. It matches the Kubernetes idiom — `LimitRange` and `ResourceQuota` were deliberately not merged into pod templates — and KubeVirt's `VirtualMachineInstancetype` is likewise fixed rather than ranged. Governance reaches classless VMs because the policy is ambient by construction. It also sidesteps the write-ownership question in §3.3/I4 entirely, since the object an admin would author ranges on is not the same object an external service pushes scalar hardware fields onto.

Costs: two authoring surfaces and two assignment workflows for admins; a bounded offering ("`large`, flex 2–8") is split across two objects, so "how far may I flex this class" is answered elsewhere; and both objects need a vCenter representation (§6.2).

Under this option the envelope stays a distinct object carrying the ranges and permissions, and the questions in §7 apply to it unchanged — in particular the classless default (Q4), multi-zone scope (Q3), enforcement granularity (Q10), and whether class-derived configuration is evaluated against the envelope at all, which is what `vmClassMode` selects.

### A2 — Add policy fields to `VirtualMachineClass` behind a `role` discriminator

**Not proposed.** Three objections often raised against class-carried governance do **not** hold and should not be reused:

- *"A class cannot carry policy because it is shared across namespaces."* False — `VirtualMachineClass` is namespace-scoped (`virtualmachineclass_types.go:177`) and `VirtualMachineClassBinding` is retired.
- *"Policy on a class churns immutable `ClassInstance` objects."* Only if a controller writes discovered data into `Class.spec`. With human-authored constraints, an admin edit reminting an instance is expected behaviour.
- *"It redefines shipped class semantics."* Only under "existing classes silently become ceilings". An optional constraint field is additive.

What does hold, and is what §4 addresses differently:

- **Fields are disjoint by shape.** A preset needs a scalar; an envelope needs a range. Under one kind with a `role` field, most fields are invalid for any given instance, requiring per-field CEL and making `kubectl explain` misleading.
- **`ClassInstance` has no meaning for an envelope** — it pins what a VM was deployed with; an envelope is evaluated against, never deployed with.
- **Assignment polarity inverts** — an assigned preset means "you may deploy this" and several union; an assigned envelope means "you are constrained by this" and several should intersect.

Every piece of shared machinery would branch on `role`, so the reuse being sought is what stops working. §4 avoids this by making constraint the only shape and deriving meaning from attachment.

### A3 — Reference a governing class from `Zone` (`Zone.spec.policyClass`)

**Partially adopted.** The zone attachment point is retained (§5.1), but expressed as `zones` on the class rather than a reference on `Zone`: a single named reference cannot compose, and it would place a `vmoperator.vmware.com` reference inside `tanzu-topology`'s schema. Note `Zone` is itself namespace-scoped, so relocating governance there resolves no ordering constraint — Zone-triggered fan-out into the namespace is already how the policy is created.

### A4 — Class assignment alone as the governance mechanism

**Ruled out by requirement, and worth understanding.** A namespace with no assigned classes can deploy nothing, so the assigned set is itself an allow-list — fail-closed by default, with no additional construct. This is also what the platform did before 9.1, which is why `vmClassMode: AsPolicy` exists as the current default and is documented as preserving ≤9.1 behaviour: pre-9.1, the class *was* the policy.

It fails on one point: assignment governs class-derived configuration completely and says nothing about **inline** configuration. Inline configuration is what vmop-3388 introduces, and adding it is what puts this option out of reach.

### A5 — Express the envelope in `vsphere.policy.vmware.com`

**Not proposed; the pattern is worth borrowing.** `ComputePolicy` already provides `enforcementMode: Mandatory | Optional`, a recursive `match` selector, and `PolicyEvaluation` as an introspection surface — the ambient-versus-explicit machinery, already shipped with a live reconciler (§3.6). But the group carries no numeric bounds, so it cannot host configuration envelopes without extension, and it is another team's API module. Recommendation: adopt the pattern — enforcement mode, selector, and an evaluation/effective-bounds surface — rather than depending on the group.

### A6 — Keep `configSpec` as a privileged, ungoverned escape hatch

**Not adopted.** An earlier draft of this proposal took this position: keep `configSpec` available to privileged accounts only, and declare it explicitly outside the envelope. §6.1 now takes the stronger position instead — no opaque `configSpec` at all, with every field exposed as a typed field before it can be used. The escape-hatch version was set aside because it leaves standing ambiguity about whether a privileged user's opaque configuration is inside or outside governance, and because it requires no exposure discipline at all rather than an explicit, if ongoing, one.

### A7 — Per-class deviation bounds on the class, permission rules on a separate policy

**Considered and set aside.** Attractive while every VM was assumed to reference a class: "how far may I deviate from *this* preset" is per-preset by nature. Set aside because classless VMs require bounds on an ambient object regardless, so bounds would exist in two places with intersection precedence — more to explain, for a benefit §4 delivers in one construct. Reconsider if flexible sizing proves dominant (Q1) *and* §4 is not adopted.

---

## Appendix B — Evidence index

References are to `vmware-tanzu/vm-operator` at the time of writing. Findings about the external class-authoring path (§3.3) and its implementation are drawn from that path's own source and are intentionally not cited by file path here, consistent with this repository's convention of not embedding references to internal-only systems.

| Claim | Location |
|---|---|
| `VirtualMachineClass` is namespace-scoped | `api/v1alpha6/virtualmachineclass_types.go:177` |
| `VirtualMachineClassBinding` exists only in `v1alpha1` | `api/v1alpha1/virtualmachineclassbinding_types.go`; absent from `api/v1alpha6/` |
| `ClassInstance` keyed by `xxhash` of class spec; active label moved on change | `controllers/virtualmachineclass/virtualmachineclass_controller.go:143,189-229,264-275` |
| `VirtualMachineClassInstance` inlines `VirtualMachineClassSpec` | `api/v1alpha6/virtualmachineclassinstance_types.go:22-23,31` |
| `spec.className` mutable under `VMResize` / `VMResizeCPUMemory` | `webhooks/virtualmachine/validation/virtualmachine_validator.go:722-733` |
| Classless VMs restricted to privileged accounts | `webhooks/virtualmachine/validation/virtualmachine_validator.go:652-670` |
| VM's `spec.class` must reference an **active** instance | `webhooks/virtualmachine/validation/virtualmachine_validator.go:701-720` |
| `IsClasslessVM` is a live code path | `pkg/util/vmopv1/vm.go:232-234`; six branch sites in `pkg/providers/vsphere/` |
| Resize-by-class-swap is shipped | `pkg/util/vmopv1/resize.go` (`LastResizedAnnotationKey`, `ResizeNeeded`) |
| `vm.spec.resources` overrides class fields via field-level merge | `api/v1alpha6/virtualmachine_types.go:1212-1218` |
| `spec.cpuAdvanced` / `spec.memoryAdvanced` | `api/v1alpha6/virtualmachine_types.go:1222-1233` |
| `vm.spec.policies` attaches `ComputePolicy` | `api/v1alpha6/virtualmachine_types.go:1196-1207` |
| `VirtualMachineConfigPolicy` namespace-scoped, one per zone; full field list | `external/vim/api/v1alpha1/virtualmachine_config_policy_types.go` |
| Policy created by Kubernetes fan-out from `Zone` | `controllers/infra/zone/zone_controller.go:181-186,221-245` |
| `Zone` is namespace-scoped | `external/tanzu-topology/api/v1alpha1/zone.go:222`; `config/crd/external-crds/topology.tanzu.vmware.com_zones.yaml:15` |
| Controller copies `ConfigTarget.status` into `policy.spec` | `.sdd/specs/001-class-policy-resize/plan.md` § I6 |
| `vmClassMode` defaults to `AsPolicy`; class-derived config bypasses policy | `.sdd/specs/001-class-policy-resize/tds.md:283,217-219` |
| `VirtualMachineConfigPolicy` has no VMODL equivalent | `external/vim/doc/deploying-a-vm.md:12,63` |
| Round-trip conversion helper exists in this repo | `api/utilconversion/conversion.go` (`AnnotationKey`, `MarshalData`, `UnmarshalData`) |
| Used by `VirtualMachine` conversion | `api/v1alpha1/virtualmachine_conversion.go:1329-1394` |
| Not used by `VirtualMachineClass` conversion | `api/v1alpha1/virtualmachineclass_conversion.go` |
| VM backup embeds a version-pinned YAML snapshot in `ExtraConfig` | `pkg/providers/vsphere/virtualmachine/backup.go:319-326` |
| Restore recreates the `VirtualMachine` object from that snapshot | `docs/guides/backup-restore/README.md` § 4 |
| `ConfigTarget` cluster-scoped; capability set on status | `external/vim/api/v1alpha1/config_target_types.go:36-118` |
| `ConfigTarget` sync is VMODL1/SOAP via `EnvironmentBrowser` | `controllers/configtarget/configtarget_controller.go:168` |
| `ComputePolicy.enforcementMode`; Mandatory with `match` unset applies to all workloads | `external/vsphere-policy/api/v1alpha1/computepolicy_types.go:24-37` |
| `MatchSpec` is a recursive boolean matcher | `external/vsphere-policy/api/v1alpha1/common_types.go:237-268` |
| No numeric bounds in `vspherepolv1` | `external/vsphere-policy/api/v1alpha1/*.go` |
| `PolicyEvaluation` resolves applicable policies; live reconciler | `external/vsphere-policy/api/v1alpha1/policyevaluation_types.go`; `pkg/vmconfig/policy/policy_reconciler.go` |
| Epic vmop-3331 names classless VMs | `.sdd/specs/001-class-policy-resize/tds.md:4` |
| vmop-3388 compute surface serves classless / Telco workloads | `003-compute-config-reconcile/spec.md:15,58` (branch `compute-config-reconcile`) |
| Reserved profile slots are per-zone counts | `api/v1alpha6/virtualmachineclass_types.go:159-169`; `api/v1alpha6/virtualmachinereservedprofile_types.go` |

---

— Faisal + Claude
