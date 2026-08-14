# Research: VM Class as Config and Policy

- **Status**: Research and proposal — for discussion, no decision sought
- **Intended location**: child of *VM Service: Class Policy and Resize* (wiki page ID `2453059710`)
- **Relates to**: Epic vmop-3331 (`VirtualMachineConfigPolicy`), Epic vmop-3388 (VM compute configuration, classless and Telco workloads)
- **Audience**: principal engineers, architecture review

---

## 1. Summary

The initial requirement for the VM Service day two (aka VM Resize) was supposed to only be exposed through the VCF Automation interface. During the review there seems to be a divergence on whether day two should be a VCF Automation-only feature or whether it should be exposed through vCenter as well. It seems to make sense not to have a feature as big as day two actions rely on VCF Automation to function. 

The initial proposal would have had VM Service end up with two constructs that describe what a VM may be configured as:

- **`VirtualMachineClass`** — an existing named hardware preset a VM selects by name, holding exact values.
- **`VirtualMachineConfigPolicy`** — a new per-namespace envelope constraining what a VM may request, including CPU and memory ranges, ExtraConfig key rules, hardware feature permissions, and a hardware-version ceiling derived from the cluster's real capabilities.

This document examines whether a single interface can serve both roles, and proposes one: **generalize `VirtualMachineClass` so that every field carries a constraint plus a default.** A fixed t-shirt size becomes the degenerate case where the constraints admit exactly one configuration. Attachment scope — VM, namespace, or zone — determines whether an instance acts as a VM's configuration source or as an ambient envelope. Under this model a single construct covers presets, day-2 resize latitude, and governance of classless VMs.

The proposal has two dependencies that must be settled before it can be assessed:

1. **`spec.configSpec` must be demoted.** It is an opaque `vim.vm.ConfigSpec` blob, and there is no coherent way to constrain or intersect one. Every field the envelope must govern has to be a typed field; `configSpec` becomes a privileged, explicitly ungoverned escape hatch. Elevation of exactly this kind is already underway on the VM side under vmop-3388.
2. **A vCenter-side representation is required.** `VirtualMachineConfigPolicy` has no VMODL representation today, and exposing config policy through vCenter means creating one — under either design. What is unresolved is whether vAPI/VMODL2 can cleanly express a field that is either a scalar or a `{min, max, default}` structure. That question applies equally to a standalone policy object carrying ranges, so it is not a cost specific to unification, but it does bear on the schema and is not answerable from this repository.

**Note**: I am still debating whether to only have min and max and derive the default from one or the other, or actually introduce a default field.

**What this document asks for**: review of the proposal in §4–§5, and the investigations named in §7 — principally the vAPI/VMODL question, which we should start now.

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

### Why classless VMs are decisive

A class cannot govern a VM that references no class. Any design in which governance lives *only* on a VM-selected class leaves classless VMs ungoverned. Governance must therefore be reachable without a class reference — which is satisfiable either by a separate ambient object or, as proposed here, by a class attached at namespace or zone scope rather than by a VM.

---

## 3. The current model

Factual description of what exists today, as input to the proposal.

### `VirtualMachineClass`

- Namespace-scoped (`api/v1alpha6/virtualmachineclass_types.go:177`). `VirtualMachineClassBinding` is retired; it exists only in `v1alpha1`.
- Holds exact values: `spec.hardware.cpus` (integer), `spec.hardware.memory` (quantity), device lists, `spec.policies` (reservations and limits), `spec.configSpec` (opaque `vim.vm.ConfigSpec`), `spec.reservedProfileID` / `reservedSlots`, `spec.instanceStorage`.
- Authored in vCenter and assigned to Supervisor namespaces; a namespace has no classes until an admin assigns them.
- `VirtualMachineClassInstance` is an immutable snapshot keyed by an `xxhash` of the class spec, with an active-instance label moved on change (`controllers/virtualmachineclass/virtualmachineclass_controller.go:143,189-229,264-275`). A VM's `spec.class` reference must point at an **active** instance (`webhooks/virtualmachine/validation/virtualmachine_validator.go:701-720`).
- `spec.className` is mutable on update when `VMResize` or `VMResizeCPUMemory` is enabled (`virtualmachine_validator.go:722-733`); the swap is the shipped resize mechanism, detected via `LastResizedAnnotationKey` / `ResizeNeeded` (`pkg/util/vmopv1/resize.go`).
- Classless VMs are permitted for privileged accounts only (`virtualmachine_validator.go:652-670`); `IsClasslessVM` is a live code path with six branch sites in the vSphere provider (`pkg/util/vmopv1/vm.go:232-234`).
- Which principals may author a class is determined by Supervisor RBAC, outside this repository.

### `VirtualMachineConfigPolicy` (New as part of the initial proposal)

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

### `ConfigTarget` (New as part of the initial proposal)

Cluster-scoped, controller-written from the vSphere `EnvironmentBrowser` over VMODL1/SOAP (`controllers/configtarget/configtarget_controller.go:168`). Carries the cluster's real capabilities: CPU and memory maxima, security flags, `maxHardwareVersion`, device categories. Because it is cluster-scoped, tenants cannot read it.

### Existing governance pattern in-tree

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

Proposed; the multi-zone and mutability items in §7 remain open.

### 5.1 Attachment scope

- **`zones: [a, b]`** on a class — an ambient envelope for VMs landing in those zones.
- **`zones` absent** — an ambient envelope for the whole namespace. Tenancy-uniform rules (ExtraConfig key policy, whether a hardware feature is permitted at all) are authored once rather than restated per zone.
- **Referenced by a VM** via `spec.className` — supplies that VM's configuration.

Carrying `zones` on the class rather than a class reference on `Zone` keeps composition possible (one class may govern several zones) and avoids placing a `vmoperator.vmware.com` reference inside the `tanzu-topology` schema.

Note that `zones` then distinguishes ambient from selectable instances. This is a discriminator, though a meaningful and composable one rather than an opaque flag. Two consequences follow: ambient and selectable instances combine under different rules (§5.3), and whether an ambient envelope should appear in a VM class chooser needs a UX answer.

### 5.2 Defaults

**Note** I'm still debating whether to have an explicit default field or derive the default from min or max.

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

### 6.1 `configSpec` must be demoted, not removed

A merged object requires every governed field to express both a value and a constraint. That is impossible for an opaque blob: there is no coherent intersection of two arbitrary `vim.vm.ConfigSpec` values. Unification is therefore not coherent while `configSpec` remains a governed surface.

Full elevation is not realistic — `VirtualMachineConfigSpec` has a large and growing field set plus `deviceChange` carrying many device and backing types — and removing the escape hatch penalises the workloads most likely to need it. The proposal is instead to **keep `configSpec`, restrict it to privileged accounts, and declare it explicitly ungoverned.** Governance is then complete for every non-privileged user, the escape hatch survives by documented design, and conversion remains straightforward because deprecation is not removal.

A forward rule follows, and is worth adopting independently of this proposal:

> **Every elevated typed field ships with its constraint counterpart.** The VM gets the value form; the class gets the constraint form; they are added together.

vmop-3388 is already this elevation on the VM side — `spec.resources`, `spec.cpuAdvanced`, and `spec.memoryAdvanced` are typed replacements for settings that previously required `configSpec`.

### 6.2 A vCenter-side representation is required either way

`VirtualMachineConfigPolicy` has no VMODL representation today. Exposing config policy through vCenter means creating one, whether governance lives on a separate object or on the class. **The vAPI/VMODL obligation is therefore common to both designs and is not a differential cost of unification.**

What is genuinely open is whether vAPI/VMODL2 can cleanly express a field that is either a scalar or a `{min, max, default}` structure, and how CRD and vCenter surfaces stay aligned across versions. This applies equally to a standalone policy object carrying ranges. It is not answerable from this repository and is the subject of the investigation in §7.

Two facts bear on the effort involved, under either design:

- The `external/vim/` types are Kubernetes-only: the module requires just `k8s.io/apimachinery`, with no `govmomi` or `vim25` dependency, and the `// It corresponds to vim.<Type>` comments throughout are documentation rather than generated conversion code. A translation layer between the Kubernetes and VIM representations would need to be built.
- `test/e2e/infrastructure/vsphere/wcp/dcli_client.go:26-29` notes that dcli was chosen partly to decouple from *"generated vapi-go code (which is potentially a pain to consume from bora/VMODL)"*, which is a data point on the cost of consuming vAPI bindings here.

One clarification for the I1 discussion: `ConfigTarget` and `VirtualMachineConfigOptions` already carry `{min, max, default}`-shaped values (`IntOption`, `ChoiceOption`), which may look like evidence that the pattern is expressible on the vSphere side. Those values arrive over VMODL1/SOAP via `EnvironmentBrowser` (`controllers/configtarget/configtarget_controller.go:168`), so they speak to VMODL1 and not to VMODL2/vAPI.

### 6.3 Class authorship must be an admin-only guarantee

The proposal places governance on the class, which holds only if the principals subject to an envelope cannot author it. Since class authorship is governed by Supervisor RBAC rather than by this repository (§3), the proposal depends on that guarantee being stated explicitly and enforced. See Q5.

---

## 7. New API version and Conversion

TBD

---

## 8. Open questions and required investigation

### Required investigation

**I1 — vAPI/VMODL modelling (start now).** Can vAPI/VMODL2 express a field that is either a scalar or a `{min, max, default}` structure, and does it support discriminated or `oneOf`-style types cleanly? What are the compatibility rules if the Kubernetes API version restructures fields — and what prevents the two surfaces from diverging? What does extending the existing class surface cost relative to introducing a new policy surface? To be taken to the vCenter/vAPI owning team; not answerable in-repo.

**I2 — Reservation semantics.** `spec.reservedProfileID` with `reservedSlots`, surfaced per zone on `VirtualMachineReservedProfile.status.zones[].reservedSlots`, is a count of pre-committed capacity slots. A slot cannot span a range, which suggests reserved classes must be constrained to `min == max`. Confirm whether that carve-out is acceptable for the guaranteed-class product story.

**I3 — Assignment pipeline capacity.** Can the vCenter authoring and assign-to-namespace pipeline accept a second object type, and at what cost? This bears directly on whether one interface or two is cheaper in practice, independent of design merit.

### Open questions

1. Is bounded or flexible sizing — flexing within one class — a primary workflow, or is day-2 resize predominantly swapping between fixed classes? The proposal's value scales with the answer.
2. Should `spec.className` remain mutable? It is mutable today under `VMResize` / `VMResizeCPUMemory` and is the shipped resize path. Making it immutable would remove that path; a narrower rule — VMs may not reference an ambient envelope class — is compatible with the proposal.
3. Multi-zone namespace: what governs a VM that has not yet been placed? Either intersect across all candidate zones (safe, potentially very restrictive, and able to produce an empty intersection from individually valid zones) or defer enforcement until placement. A namespace-wide envelope (§5.1) is the natural home for whatever governs the unplaced window.
4. A namespace with no classes assigned — may nothing be deployed, or anything? Equivalently: for classless VMs, is the default deny or allow?
5. Who may author a `VirtualMachineClass`? (§6.3)
6. For each hardware feature permission in §3, is the underlying fact one the platform should discover into `ConfigTarget`, or is it purely an administrative permission? This determines whether the envelope has something to intersect against for that field.
7. Does the envelope need to constrain `configSpec`? If it does, no coherent mechanism exists and §6.1 does not hold.
8. Is mandatory baseline configuration inheritance — all VMs in a zone inheriting an ambient `configSpec` — a requirement? Concrete fields can be inherited as defaults; they cannot be meaningfully constrained.
9. Is any consumer expected to read the envelope as a *capability* surface — "what can this tenancy do" — as well as a permission surface? `ConfigTarget` is cluster-scoped and therefore unreadable by tenants, so if the envelope is the tenant-visible view of hardware capability, the proposal needs to preserve that role.
10. What enforcement granularity does the envelope need — a single setting, or separate control over create, update, and power-on?
11. Upgrade: existing VMs whose configuration exceeds a newly introduced envelope — grandfathered, or blocked at power-on?

---

## Appendix A — Alternatives considered

Recorded so the reasoning is auditable.

### A1 — Two constructs: fixed `VirtualMachineClass` plus `VirtualMachineConfigPolicy`

**Viable.** Each object has one job and one schema shape. It matches the Kubernetes idiom — `LimitRange` and `ResourceQuota` were deliberately not merged into pod templates — and KubeVirt's `VirtualMachineInstancetype` is likewise fixed rather than ranged. Governance reaches classless VMs because the policy is ambient by construction.

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

**Not proposed; the pattern is worth borrowing.** `ComputePolicy` already provides `enforcementMode: Mandatory | Optional`, a recursive `match` selector, and `PolicyEvaluation` as an introspection surface — the ambient-versus-explicit machinery, already shipped with a live reconciler (§3). But the group carries no numeric bounds, so it cannot host configuration envelopes without extension, and it is another team's API module. Recommendation: adopt the pattern — enforcement mode, selector, and an evaluation/effective-bounds surface — rather than depending on the group.

### A6 — Deprecate and remove `configSpec`

**Not proposed; replaced by demotion (§6.1).** Full elevation implies typing a large and growing fraction of the vSphere configuration model and removes the escape hatch from the workloads most likely to need it.

### A7 — Per-class deviation bounds on the class, permission rules on a separate policy

**Considered and set aside.** Attractive while every VM was assumed to reference a class: "how far may I deviate from *this* preset" is per-preset by nature. Set aside because classless VMs require bounds on an ambient object regardless, so bounds would exist in two places with intersection precedence — more to explain, for a benefit §4 delivers in one construct. Reconsider if flexible sizing proves dominant (Q1) *and* §4 is not adopted.

---

## Appendix B — Evidence index

References are to `vmware-tanzu/vm-operator` at the time of writing.

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
| Class authorship is not gated in this repository; determined by Supervisor RBAC | `webhooks/virtualmachineclass/`; `config/rbac/virtualmachineclass_editor_role.yaml` (unbound `ClusterRole`) |
| `VirtualMachineConfigPolicy` namespace-scoped, one per zone; full field list | `external/vim/api/v1alpha1/virtualmachine_config_policy_types.go` |
| Policy created by Kubernetes fan-out from `Zone` | `controllers/infra/zone/zone_controller.go:181-186,221-245` |
| `Zone` is namespace-scoped | `external/tanzu-topology/api/v1alpha1/zone.go:222`; `config/crd/external-crds/topology.tanzu.vmware.com_zones.yaml:15` |
| Controller copies `ConfigTarget.status` into `policy.spec` | `.sdd/specs/001-class-policy-resize/plan.md` § I6 |
| `vmClassMode` defaults to `AsPolicy`; class-derived config bypasses policy | `.sdd/specs/001-class-policy-resize/tds.md:283,217-219` |
| `VirtualMachineConfigPolicy` has no VMODL equivalent | `external/vim/doc/deploying-a-vm.md:12,63` |
| Policy type is the only one in the package lacking a `corresponds to vim.<Type>` docstring | `external/vim/api/v1alpha1/virtualmachine_config_policy_types.go` |
| `external/vim/` types are Kubernetes-only; no `govmomi`/`vim25` dependency, no conversion code | `external/vim/api/go.mod`; `external/vim/` |
| dcli chosen partly to decouple from generated `vapi-go` bindings | `test/e2e/infrastructure/vsphere/wcp/dcli_client.go:26-29,113` |
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
