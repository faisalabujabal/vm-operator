# Implementation Plan: Flex VM Classes (`VirtualMachineClass` v2)

- **Spec**: [`spec.md`](spec.md)
- **Epic**: vmop-4034 (design); implementation epics below
- **Date**: 2026-10-21

The technical design is WIKI page 3118085463 and its child pages (full schema, vAPI, reserved capacity, Supervisor models); [`research.md`](research.md) points at each. This plan records the constitution check, the code areas, the epic split and the order of work. The task list is in [`tasks.md`](tasks.md); the API surface is in [`model.md`](model.md).

---

## Summary

Reshape the unreleased `VirtualMachineClass` v1alpha6 with typed value shapes, `governs`, zones, `externalID` and reserved profiles; enforce governance in the class and VM webhooks; report compatibility and compliance from the class and VM controllers; filter placement by governance; add the mapping markers the provider's class API is generated from; remove `VirtualMachineConfigPolicy`. The provider's work is listed under "Provider work". All of it is behind the `vm_service_classless_vm` capability until the enablement story. Milestone 2 adds `parentClassRefs` behind a second capability.

## Incremental delivery and verification

Start from spec PR #2067 with the resource migration PR (A1) that reshapes existing typed resources. Device policy (A2) and class reserved-profile policy (A3) independently target A1. CPU/memory, boot/advanced, inline VM devices, VM reserved-profile selection and remaining settled hardware presets follow as category PRs targeting the foundation. Actual code dependencies determine any further serial ordering. Keep conversions, consumers, generated artifacts and tests with each change. Before opening each PR, run the applicable tests and review its diff against the constitution and acceptance criteria; fix findings and retest affected behavior.

Class-supplied fields are authoritative. Resource allocation defaults retain legacy precedence: explicit zero requests and limits leave raw ConfigSpec reservations and limits intact. Promoting raw allocation values into typed defaults is deferred. Creation and permitted class changes materialize the applicable values; ordinary updates do not refresh VMs merely because a class changed. Conflicting-edit rejection remains with the validation story. Existing VM fields continue using TelcoVMServiceAPI during this incremental stack; Flex capability wiring remains separate. Preserve the existing Supervisor-wide minimum-frequency conversion of class vCPUs into VM MHz; selecting a cluster-specific frequency is an unresolved follow-up.

Class conversion annotation preservation and metadata isolation use shared hub helpers; each spoke retains explicit generated conversion hooks and scalar field mappings. Shared projection/restore logic preserves older-writer per-field precedence for classes and immutable class instances. Resource spec conversion composes the allocation and CPU/memory-pair converters; shared pair helpers preserve fixed-range deep copies and default-only projection. Allocation-only conversion leaves guest size untouched.

The five legacy API spokes share device conversion helpers for slice projection, identity aggregation, positive-count checks and bounded expansion, with version-specific element conversion callbacks. Nil/empty behavior, first-seen identity order and labels stay unchanged. Device and resource policies use the same unchanged-policy restoration helper so older-writer precedence is defined once.

The device sibling uses concrete schema types with a consumer-owned generic default-reading helper in the implementation layer; it does not implement governance validation. Its live API conversion E2E and staged old-release upgrade fixtures cover device identity, repeated DirectPath devices and policy preservation without requiring GPU allocation. Provider unit tests verify PCI configuration generation; actual hardware realization requires a compatible GPU testbed and remains unverified by these fixtures.

The device sibling preserves the existing typed vGPU behavior of one device per profile and defers a new vGPU `count` field. DirectPath counts retain repeated legacy devices without losing their labels. Inline VM PCI device identity and per-device settings must be reconciled with the VM schema reference before the device category PR; the initial migration does not choose between named individual devices and grouped entries for that future VM API.

Push field/version/value combinations, nil/zero/empty handling, conversion round trips and older-writer edits into unit tests. Use envtest for storage/pruning/CEL/admission and conversion-webhook behavior, and vcsim/provider tests for configuration translation. E2E proves representative complete workflows: a new-shape class produces the expected VM spec and realized configuration; legacy classes and healthy workloads survive a real baseline-to-candidate upgrade and remain usable afterward. Old-version API writes to the candidate alone are not upgrade coverage. Each PR records what lower layers prove and the remaining E2E responsibilities. Test results must distinguish executed checks from unavailable testbed coverage.

### Boot C1 delivery boundary

Boot C1 targets the CPU/memory B base to reuse concrete `BoolPolicy`/`LongRange` types, raw ConfigSpec documents, annotation restoration and a single class-resolution/defaulting dispatcher. The batch includes `bootDelay`, `bootRetry`, `bootRetryDelay` and `efiSecureBoot`. Delays are inclusive integer ranges in milliseconds, constrained to the nonnegative range representable by the existing VM duration API; zero and explicit false remain distinct from omission. Class changes clear the previous class's boot defaults while leaving VM-only firmware, network protocol and boot order intact. Creation preserves VM values for fields the class does not supply; ordinary updates do not refresh boot values after a class edit.

Admission omits `bootRetryDelay` from the VM while its effective retry is disabled or unset, preserving the inactive preset in the class and provider projection. A class that omits retry preserves an explicit enabled VM value on creation. For secure-boot-enabled class-backed VMs under the existing Telco gate, validation checks the effective firmware when VM firmware is omitted: explicit VM firmware, image firmware, then the selected class/snapshot raw firmware retain their existing precedence. No inferred firmware is persisted in VM spec, so later class changes cannot retain a stale derived override. Defaulting and validation share class/snapshot selection, including avoidance of the previous class snapshot on a className change. A BIOS image is never overridden by raw class EFI; lookup failures return contextual validation errors. This prerequisite does not introduce a general class firmware policy.

Firmware and network protocol are tracked separately in T103g2 because legacy firmware is subject to image precedence and raw network protocol is overwritten by the boot reconciler. Their promotion must not silently change existing VM behavior. The one-shot `enterBootSetup` lifecycle and its missing VM counterpart are tracked in T103g3. These deferrals do not complete the broader boot/advanced category T103g; advanced fields remain a separate C2 batch. Boot order is VM-specific and is not introduced as a class policy.
### Zone and governance schema slice (vmop-4315)

This draft targets the resource foundation (A1), independently of the device, profile, compute, boot and advanced drafts. It adds `spec.zones` and `spec.governs.{zones,enforcement,existingVMs}` and preserves their absent/empty/list states through all older class APIs and immutable class-instance conversions. Legacy classes acquire neither a zone restriction nor governance. Reused conversion destinations clear both hub-only fields before restoring annotation data.

Both zone lists are sets. The subset CEL rule compares the available set with its union with the governing set, avoiding nested scans and arbitrary list-size limits. Omitted availability is unrestricted; omitted governing zones inherit availability; explicitly empty governing zones always satisfy the subset check. Schema validation and storage apply independently of capabilities, as for the other v1alpha6 schema fields.

This slice provides the schema, not governance behavior: governance resolution/status remain vmop-4320/vmop-4323; enforcement defaulting remains vmop-4324; the top-class `existingVMs` requirement and cross-object rules remain vmop-4325. Governance must be resolved live, so this slice does not add governance metadata to the VM configuration hash. The association-zone ownership decision T010 does not block representing zone lists or checking their local subset relationship.

Conversion tests cover older-writer edits and reused destinations; envtest covers create/update validation and `CreateOrUpdate` storage for all three zone states; an experimental installed-cluster E2E scenario checks storage, older-version webhook conversion and rejection. It does not claim placement enforcement or a real upgrade run.

## Technical context

| Field | Value |
|---|---|
| Language | Go (repo toolchain) |
| API | `vmoperator.vmware.com/v1alpha6` (hub, reshaped in place: it exists only on main, and the 9.1 release ends at v1alpha5); conversion from v1alpha1–v1alpha5 |
| Inputs | `ConfigTarget`, `VirtualMachineConfigOptions`, `VirtualMachineGuestOptions` from spec 001 |
| Testing | Ginkgo/Gomega; envtest for CRD, CEL and conversion behavior; vcsim; E2E on a Supervisor in the quarantined capability pipeline |
| Feature gate | `vm_service_classless_vm`, read as `pkgcfg.FromContext(ctx).Features.VMServiceClasslessVM` (vmop-4313), replaces both previous capability keys and feature flags; Milestone 2 adds the Tenant VM Classes capability (vmop-4339), which depends on it |
| Provider | Class and namespace APIs, class API generation and drift checks, reserved profile publication, capability advertisement, Supervisor integration; tracked in the provider's spec (tasks.md T910–T916) |

## Constitution check

- [ ] **API compatibility.** v1alpha6 has not shipped, so its class type is changed in place; v1alpha5 and older convert to it, and no field is removed from a shipped version. The round-trip annotation wiring lands first.
- [ ] **Unset vs `[]`.** `*[]string` for every zone list, a deliberate exception to Kubernetes list conventions, recorded on the vAPI page §5.1.
- [ ] **CRDs.** Root, groupversion and deepcopy markers; `make generate-go generate-manifests`; manifests checked in.
- [ ] **Controllers are thin.** The governance check is one exported package in `pkg/`, called by the VM webhook, the VM reconciler and placement.
- [ ] **No direct vSphere calls in controllers.** Compatibility reads only `ConfigTarget` and the options objects.
- [ ] **Webhooks.** CEL for structural rules (value shapes, `governs.zones` within `spec.zones`); Go for cross-object rules (one governing class per zone, class-vs-class bounds, VM governance, reserved profiles).
- [ ] **Controllers modify only status (deviation).** The VM reconciler writes VM spec in three places: the same-class resize rewrite, schema upgrade, and the re-default after a class change. Precedent: `pkg/providers/vsphere/upgrade/virtualmachine/vm_schema_upgrade.go`. The spec patch is a separate write with an optimistic lock, not part of the deferred patch helper, so a concurrent user edit fails with a conflict and is retried. Recorded in "Complexity tracking".
- [ ] **Status.** `observedGeneration` and `Ready` on the class; per-zone conditions in `status.zones`.
- [ ] **Fan-in writes.** `Zone.status.governingClasses` is written by one controller from many classes: patch with an optimistic lock and skip unchanged writes.
- [ ] **Feature flags.** Every new behavior checks `pkgcfg.FromContext(ctx).Features.VMServiceClasslessVM`. v1alpha6 `VirtualMachineClass` objects always use the new shape, whether or not the `vm_service_classless_vm` capability is enabled. The capability gates webhook behavior (governance, defaulting from governing classes, the new VM and class rules) and controller reconciliation (compatibility, compliance, placement); toggling it takes effect without a restart.
- [ ] **E2E.** Every user-visible behavior ships with E2E coverage in the same story, in the quarantined capability pipeline until enablement.
- [ ] **Public repo.** No internal links or ticket IDs other than `vmop-NNN` and `WIKI page NNN`.

## Code areas

| Area | Paths |
|---|---|
| Class API | `api/v1alpha6/virtualmachineclass_types.go`, value types in `api/v1alpha6/`, conversion in `api/v1alpha5` and older, `api/utilconversion` |
| VM API | `api/v1alpha6/virtualmachine_types.go`: `spec.reservedProfileName`, `status.governingClassNames`, conditions, observed fields |
| vmx-23 CPU fields | CPU `reservationLockedToMax` and NUMA fixed auto-affinity on the VM, the class and VM status; a separate story (vmop-4428), blocked on vSphere and govmomi with vmx-23 |
| Zone mirror | `external/tanzu-topology/api/v1alpha1/zone.go`: `status.governingClasses` |
| Capability | `pkg/config/capabilities`, `pkg/config` |
| Governance | new package under `pkg/`: resolve the governing class, compare a VM or a class with it |
| Class webhooks | `webhooks/virtualmachineclass/{mutation,validation}` |
| VM webhooks | `webhooks/virtualmachine/{mutation,validation}` |
| Class controller | `controllers/virtualmachineclass` (moving the `ReservedProfileID` reader in `virtualmachineclass_controller.go` to `reservedProfileName`) |
| VM reconciler | `pkg/providers/vsphere` (compliance step before power state; reservation from the VM field in `virtualmachine/configspec.go`), `pkg/providers/vsphere/upgrade/virtualmachine/vm_schema_upgrade.go` (backfill of `spec.reservedProfileName`), `controllers/virtualmachine` |
| Reserved profiles | remove `VirtualMachineReservedProfile` from `api/v1alpha6`; read RBAC in `config/rbac`; the writer is the provider |
| Provider API mapping markers | `api/v1alpha6/virtualmachineclass_types.go` and the value types; field-coverage check under `api/` |
| Placement | `pkg/providers/vsphere/placement` |
| Zone mirror controller | `controllers/infra/zone` |
| Environment browser | `controllers/vim/*`, `controllers/virtualmachineconfigoptions`, `config/rbac` |
| Removal | `external/vim/api/v1alpha1/virtualmachine_config_policy_types.go`, `config/crd/external-crds`, `controllers/infra/zone`, `config/rbac`, `test/e2e/vmservice/vmservice/configpolicy` |
| E2E | `test/e2e/vmservice/` suites, labeled for the quarantined capability pipeline |
| Docs | `docs/` |

## API strategy

- **Class.** Every configuration field the class bounds is a typed value: number `{min, max, default}` (`default` required when `min` or `max` is set; an unset bound is open, limited by hardware or a parent class), enum `{allowed, default}`, boolean `{allowOverride, default}`, list `{default, allowed, denied}` with `{type, value}` matchers. `configSpec` keeps only pass-through fields; an elevated path in it is moved for older writers and rejected for writers on v1alpha6.
- **Reserved capacity.** `reservedProfileName` is an `EnumPolicy` `{default, allowed, required}` of profile names, at the same path as the VM's `spec.reservedProfileName`; `EnumPolicy` was chosen over `ListPolicy` because the VM value is one name and list add/remove/match semantics don't fit a single reservation. Names must resolve to a `VirtualMachineReservedProfile` in the namespace, and `allowed` is required when `default` is set. v1alpha5 `reservedProfileID: X` converts to `{default: X, allowed: [X], required: true}`; down, `reservedProfileID` = `default`; up, a `reservedProfileID` that differs from the annotation's default (an older-API writer changed it) resets the field to `{default: Y, allowed: [Y], required: true}`, otherwise the annotation is restored. `reservedProfileID` is removed from v1alpha6 and exists only in v1alpha5 and older; the class controller reader and the extraConfig write move to `reservedProfileName`. The provider may keep writing `reservedProfileID` through older versions. `reservedSlots` stays an `int32` with the same name and meaning, deprecated, preserved through conversion and the namespace copy (including zero), never overwritten with observed totals.
- **Reserved profiles.** `VirtualMachineReservedProfile` leaves vm-operator's v1alpha6 (unshipped, so this is safe) and is published by the provider with each profile's shape and per-zone counts (spec, "Provider contract"). vm-operator only reads it. The provider's backfill must be complete before VM admission relies on it; an unknown profile is a clear failure with no fallback.
- **VM.** `spec.reservedProfileName` is filled by the mutation webhook from the class default, like any other default; omission inherits the default, explicit `""` selects no profile, and `required: true` rejects an empty value after defaulting. The extraConfig profile ID is written from the VM field, falling back to the class while the field is unset, and removed when the field is empty. Existing VMs get the field from their class during schema upgrade; that unset-to-set write is exempt from the powered-on lock.
- **Upgrade.** Only conversion fills ranges (`min == max == default`); `spec.zones` stays unset (all zones); `externalID` = name; no class governs until an admin adds `governs`.

## Controller and webhook impact

- **Class mutating webhook**: `governs.enforcement: Deny`, the memory lock on a preset class that attaches devices.
- **Class validating webhook**: value-shape rules not expressible in CEL, elevated `configSpec` paths, `externalID` immutable, `existingVMs` required on a governing class with no parent, one governing class per zone, class-vs-class bounds, sticky v2 writes, `parentClassRefs` rejected until Milestone 2, reserved profiles resolve to a `VirtualMachineReservedProfile`, the class's default configuration must match its default profile, and every allowed profile must fit within the class's resource and device policy; listing a profile does not fix the class's bounds, and on edit only changes that alter an allocated profile's shape or invalidate the class's default VM are rejected (the provider's older class API keeps today's block on update and delete while capacity is assigned). `governs.zones` within `spec.zones` is a CEL rule on the type.
- **VM mutating webhook**: fill unset fields from the class, from the image, or from the governing class; fill `reservedProfileName`; on a class change (allowed when `VMResize` or `VMResizeCPUMemory` is on, with no annotation), replace every class-supplied field from the new class and clear and re-default the fields it doesn't set.
- **VM validating webhook**: governance, inline-edit rule while `className` is set (including `reservedProfileName`), the zone rule, hardware compatibility per zone, device memory reservation, reserved-profile match (after defaulting, for class-backed and classless VMs; overhead excluded; a resize past the profile rejected), the powered-on lock on changing one set profile to another, class change of a reserved VM only while powered off. The `DenyPowerOn` power-on rejection ships with compliance (vmop-4041).
- **Class controller**: per-zone `HardwareCompatible`, `WithinGovernance`, `Governing`, resolved limits; watches `ConfigTarget` and `Zone`.
  - **vmop-4321 implementation**: evaluates class defaults independently per cluster; a zone is compatible if one cluster supports all checked defaults. Otherwise it is Unknown while any potentially compatible cluster has incomplete/stale discovery, or False when every cluster has a known incompatibility. Explicit class zones are retained even before their Zone/discovery appears; implicit zones follow namespace Zone objects. System classes collect zone cluster IDs across namespace copies. Class writes are never rejected by this controller.
  - Checks per-VM CPU/memory bounds, CPU topology options, vGPU profiles, Dynamic DirectPath identities, config capability flags (nested virtualization, performance counters, memory lock, boot retry/secure boot, CBT), and explicit guest CPU/memory, firmware, hot-add/remove, IOMMU and secure-boot support. Typed defaults override raw scalar presets; original ConfigSpec device precedence matches provisioning. Uses the class's raw hardware version when specified, otherwise discovery's highest create-supported version. Image-specific checks wait until a VM selects an image. Shared config-options snapshots are used only for their annotated source cluster; other clusters report Unknown for option-dependent defaults until cluster-specific evidence is available. Device-only defaults can be evaluated from each cluster's own ConfigTarget. Guest entries use guest ID and hardware version, without owner references. Static passthrough needing host-specific evidence and raw Dynamic DirectPath alternatives requiring undiscovered sub-vendor/sub-device/revision identities report Unknown; arbitrary ExtraConfig and allocation policy have no discovery check here.
  - Hardware compatibility is support evidence, not a placement/capacity guarantee or an assertion that every value in a policy range is available. T202 owns resolved ranges/options; T407 owns VM rejection. Messages expose only class field paths, never discovered pool totals or inventory identities. Discovery status and owner changes invalidate classes, with periodic retries; watches are registered with the shared capability at manager startup.
  - Tests include deterministic multi-cluster/default/legacy cases, envtest watch-driven transitions, and an experimental E2E using real zone-specific vGPU inventory. E2E skips when the required topology is absent; live Supervisor execution remains pending.

- **VM reconciler**: `GovernanceCompliant`, `status.governingClassNames`, withheld power-on for `DenyPowerOn`, same-class resize rewrite, observed fields, the reservation from the VM field (written or removed).
- **Placement**: only zones that admit the VM and are compatible.
- **Environment browser**: maximum hardware version; RBAC restricted to admins and vm-operator; no vCenter re-query on resync when the cluster is unchanged.
- **RBAC**: remove `virtualmachineconfigpolicies`; restrict the three environment-browser CRDs; read-only access to `virtualmachinereservedprofiles`.

## Epics and order

Milestone 1 enables governing classes for providers; Milestone 2 adds tenant classes.

| Epic | Scope | Depends on |
|---|---|---|
| vmop-4038 — API, upgrade and capability | capability, quarantined pipeline, v1alpha6 schema (including removing `VirtualMachineReservedProfile`), conversion, VM and Zone API fields, policy removal, environment-browser completion | — |
| vmop-4039 — Governance engine and class compatibility | governance package, class `HardwareCompatible`, resolved limits, governance status and Zone mirror | the schema (vmop-4315) |
| vmop-4201 — Class webhooks and provider API markers | class mutating and validating webhooks (vmop-4324, vmop-4325), provider API mapping markers (vmop-4326) | the schema; the governance package for class-vs-class bounds |
| vmop-4040 — VM create and edit | VM defaults, VM governance validation, device memory reservation, zone rule, reserved capacity | the governance package; the VM API fields; the provider's reserved profile publication (T910) for reserved capacity |
| vmop-4041 — Compliance, placement and enablement | `GovernanceCompliant`, the `DenyPowerOn` power-on rejection, placement filter, enablement, docs | the VM webhooks; the provider's capability advertisement (T911) for enablement |
| vmop-4312 — Tenant VM Classes (Milestone 2) | second capability, `parentClassRefs`, class families, v2 vAPI round-trip, enablement | all of Milestone 1 |

**Sequential**: the schema → the governance package → the VM webhooks → compliance and placement → enablement.

**Parallel**:

- Within vmop-4038: the capability and pipeline, policy removal and environment-browser completion run beside the schema.
- vmop-4039's class status stories run beside vmop-4201's class webhooks once the governance package lands.
- Provider work (T910–T916) runs beside the vm-operator epics; it needs the schema (vmop-4315) and, for the class API, the markers (vmop-4326).
- Docs run beside vmop-4041.

## Timeline

Two-week sprints from 2026-10-20; the sprint covering 2026-12-15 to 2027-01-11 includes the holidays.

| Epic | Three engineers | Two engineers |
|---|---|---|
| vmop-4038 | 2026-10-20 → 2026-11-16 | 2026-10-20 → 2026-11-16 |
| vmop-4039 | 2026-11-03 → 2026-11-30 | 2026-11-03 → 2026-11-30 |
| vmop-4201 | 2026-11-17 → 2026-12-14 | 2026-11-17 → 2026-12-28 |
| vmop-4040 | 2026-12-01 → 2027-01-11 | 2026-12-01 → 2027-01-25 |
| vmop-4041 | 2026-12-15 → 2027-01-25 | 2026-12-15 → 2027-02-08 |
| vmop-4312 | 2027-01-26 → 2027-03-08 | 2027-01-26 → 2027-03-22 |

## Provider work

Tracked in the provider's spec and listed in tasks.md as dependency tasks T910–T916 with their vmop keys: reserved profile publication and backfill (T910), capability advertisement (T911), the v2 class API (T912, vmop-4327), the older class API and the namespace API (T913, vmop-4328), Supervisor and namespace-copy integration (T914, vmop-4329), class API generation and drift checks (T915, vmop-4392), and the vpxd config field drift check (T916, vmop-4393). vm-operator work that consumes them: vmop-4334 needs T910; vmop-4337 needs T911; T912 and T915 need vmop-4315 and vmop-4326.

## Test strategy

- Unit tests with `testlabels` category labels for every webhook rule and the governance package; envtest for every CEL rule and for conversion, including the three-state zone round trip.
- vcsim for the class controller, the VM reconciler, placement and the environment-browser re-query rule (call counts on the fake provider).
- E2E ships inside each story, in the quarantined capability pipeline (vmop-4314); the enablement story (vmop-4337) takes it out of quarantine after a full sweep.

## Rollout

- Existing classes convert with no manual step; no class governs until an admin adds `governs`, so behavior doesn't change on upgrade.
- The capability stays off until vmop-4337. vm-operator turns flex behavior on only when the provider advertises the capability, which it does once its contract is met (spec, "Provider contract"); the Supervisor model choice is on WIKI page 3119061412.
- The policy is removed in the same release; its enforcement never shipped.
- Release notes: v1alpha6 class fields, classless VMs in governed zones, `GovernanceCompliant`, `spec.reservedProfileName`.

## Complexity tracking

| Deviation | Why | Simpler alternative rejected |
|---|---|---|
| `*[]string` zone lists distinguish unset from `[]` | "No zones" and "all zones" must not collapse | A plain list: dropping `[]` would make a class available everywhere |
| The VM reconciler writes VM spec (same-class resize, schema upgrade, class change re-default) | These values come from the class and must follow it; precedent `vm_schema_upgrade.go`. A separate spec patch with an optimistic lock, apart from the deferred status patch | Asking users to re-apply the class: drift between the class and its VMs, and no backfill on upgrade |

## VM and Zone API foundation (vmop-4317)

T108–T109 land on the specification branch independently of the class reshape: optional VM profile selection, inline device identities/counts, observed status fields, governance condition constants and the Zone mirror schema. Device Go types are VM-specific so this change does not reshape existing class device lists. The schema is available without the capability; the later webhook and controller stories gate runtime behavior. T110 follows vmop-4313 and the vmop-4315 profile schema, retaining the planned class-default backfill rather than adding an interim reader of `reservedProfileID`.

Validation covers v1alpha1–v1alpha5 conversion round trips (including omitted versus explicit empty selection and false versus unknown booleans), deep-copy isolation, API-server persistence and rejection of invalid device lists, and an experimental E2E check of the installed VM and Zone schemas. The Zone type change follows the explicit external-API scope of T109 and regenerates its deep copy and external CRD; it changes no Zone reconciliation.

### Advanced policy slice (vmop-4315)

The advanced category builds on the shared compute policy and raw ConfigSpec helpers. It adds the VM-aligned boolean policies (including the existing 1 GB huge-page enable flag), physical NUMA set policy, and concrete ExtraConfig list policy with typed key matchers. All five older API conversions lift supported values from raw ConfigSpec and project defaults back while preserving unknown objects, unsupported ExtraConfig values, and per-field policy annotations.

The shared admission class lookup dispatches advanced materialization once, under the Telco/classless capability. Create writes supplied defaults; permitted class/snapshot changes clear the handled advanced fields and apply the new defaults. Ordinary updates and class removal retain selections. Provider projection consumes typed defaults independent of the materialization gate to retain converted legacy class behavior; immutable class hashes include advanced policy metadata. Governance and hardware compatibility validation remain follow-up work.

Validation includes all-version conversion coverage, admission lifecycle and independent-copy checks, structural CEL envtests, provider projection and feature-gate checks, instance rotation after a policy-only edit, and new-VM plus staged-upgrade E2E assertions. Live E2E execution remains deferred; compile verification is required before publication.

Existing VM ExtraConfig key restrictions remain in force. Class ExtraConfig defaults retain system-reserved, guestinfo and NIC keys for provider projection and its existing sanitization, while admission materializes only keys legal in the VM's bag using the existing shared classifiers. This preserves class/provider behavior without creating a tenant validation bypass or duplicating key classification in the API module.

### Class reserved-profile migration (A3)

A3 adds `reservedProfileName` policy conversion and class/provider consumers on top of A1, preserving `reservedSlots` including zero. It does not add a VM profile field or governance. Unit tests cover every older API and older-writer edits; schema integration tests validate enum membership; provider tests check the emitted profile ID. The staged E2E fixture creates a namespace-scoped legacy profile class before the upgrade, verifies its UID/profile/zero slots afterward, then persists a richer policy through an older API update and checks its immutable class snapshot. This API/controller workflow does not allocate capacity, exercise the provider namespace-copy API, or claim reserved-VM realization coverage; those require a reservation-capable testbed. E2E execution remains deferred.

Profile conversion uses the shared class and class-instance annotation/projection workflow from A1, with `FixedClassProfile` and `ClassProfileDefault` retaining the profile-specific selection rules. Profile and resource policies share the unchanged-policy restoration helper; version-specific profile assignments run alongside the composed resource converters. Per-version tests preserve omitted policies, empty allowed lists and implicit single-profile defaults, and verify that an older writer can clear a profile without discarding resource ranges.

The NUMA default/allowed-set membership check uses shared Go validation on class create and update because unbounded nested CEL exceeds Kubernetes's estimated validation budget. Per-item nonnegative IDs use OpenAPI minimum constraints; the required/default check and matcher shape remain CEL. The common class resolver projects and re-promotes an isolated effective class, normalizing ExtraConfig values with a corresponding typed advanced setting before materialization. Explicit typed defaults take precedence over duplicate bag/raw defaults.
### CPU and memory category (B)

Use the existing VM topology path `cpuAdvanced.topology.vnumaNodeCount`; the draft wiki's shorter path would duplicate an existing field. Shared raw JSON projection/promotion preserves unknown fields and unsupported legacy latency/NUMA values while supported presets become concrete policies. Effective class defaults are resolved once from projected presets before admission materialization, so typed defaults take precedence and raw-only existing classes keep their behavior. Ordinary updates do not refresh fields; permitted class changes clear and re-default supported counterparts. Class snapshot hashing adds optional compute fields, preserving existing hashes when absent.

Add the VM hot-remove flag through existing Telco-gated compute reconciliation, status and backfill; enabling hot-remove does not expand existing powered-on CPU-size decrease semantics. T103m/T103n explicitly defer unavailable NUMA fixed affinity and CPU reservation-lock wire mappings. No empty `cpuAdvanced.vnuma` container is introduced. T103f covers the supported implementation and written tests. Deferred fields remain open in T103m/T103n/T103p; live E2E execution remains open in T103q.

The new CPU hot-remove VM field preserves existing configuration when omitted, including on already-Telco-versioned VMs that skip the older compute backfill. Explicit false disables it; permitted class changes whose new class omits this preset materialize false to re-default the previous value. Latency default validation remains structural CEL; the allowed-value domain uses Go admission on create/update to avoid the unbounded enum-list CEL cost. Immutable snapshots are generated from validated class specs and retain the structural constraints.

Guaranteed memory-lock classes retain their class reservation policies. Admission materializes `memoryAdvanced.reservationLockedToMax=true` and omits the separate VM `resources.requests.memory`, because the VM API treats those forms as alternatives and vSphere owns the full reservation while locked. Raw and typed class presets share this normalization; provider coverage checks the realized lock and full reservation.

The approved current-batch scope defers typed vNUMA-on-CPU-hot-add exposure (T103p). Remove its class/VM fields, materialization, reconciliation, backfill and observed-field addition; preserve the existing raw ConfigSpec path and provider behavior. Before restoring the field, establish how requested exposure maps to configured intent versus effective observed topology, verify prerequisites and convergence, and add category coverage. Address this deferred task after the current PR set.

Advanced raw promotion retains unsupported leaf types, malformed ExtraConfig item shapes and unknown values in the residual document rather than failing old-version reads. ExtraConfig projection indexes typed keys and decodes each raw entry once, preserving unmatched raw duplicates and unknown sibling properties while applying typed last-value precedence; explicit advanced booleans remain authoritative over the bag.

## Governance delivery (vmop-4320, vmop-4323)

A combined dependency branch includes class resource/profile/device/compute/boot/advanced policies, the VM and Zone API, and the classless capability. The zone/governance schema is supplied by prerequisite #17. Drafts separate class-status schema, the pure shared evaluator, class status and snapshot hashing, and the Zone mirror. The schema preserves missing versus empty zone sets through every legacy conversion. API fields remain available regardless of the capability; all runtime controller behavior reads the live shared capability. Live Supervisor E2E remains unexecuted until a testbed is supplied.

The shared evaluator takes an already effective VM spec and an explicit target-cluster minimum host frequency. It does not call vSphere or reuse the Supervisor-wide materialization frequency. Missing constrained scalars and unavailable required frequency fail closed; comparison uses rational arithmetic without rounding fractional vCPUs. Unbounded VM limits cannot pass a finite ceiling. The memory reservation lock uses effective guest memory size. Class comparisons check declared numeric/boolean/enum/set domains; omitted fields are inherited at VM evaluation. List defaults are checked against governing allow/deny rules as required by T302. Matcher-language containment is not inferred: the eventual VM evaluator applies the governing rules independently to every concrete item. Raw ConfigSpec and instance storage remain presets, not governance policies.

Class reconciliation lists classes once per namespace, preserves unrelated per-zone conditions, evaluates every available zone, and writes observedGeneration/Ready. Governing-class and Zone changes enqueue affected classes; a one-minute resync handles capability changes and failed mapper lists without restarting. System-namespace classes resolve their unspecified availability from all namespace Zones, deduplicated by name. Explicit class zone names remain visible before a corresponding namespace Zone is copied; hardware compatibility is a separate condition. Disabled capability leaves the last observed governance status unchanged and performs no governance evaluation. Typed configuration hashing moves to `pkg/vmclass` while retaining existing hashes and excluding governance metadata.

The Zone controller owns the governance mirror, lists classes in that Zone's namespace, sorts/deduplicates names and skips unchanged lists. It patches an isolated Zone copy with an optimistic resource-version lock, so its existing deferred patch helper cannot replay the mirror. Conflicts retry through reconciliation; other provider status fields remain untouched. A class watch reacts to creation, spec edits and deletion; a one-minute resync handles capability changes. Tests cover real watch delivery with envtest/vcsim, conflicting status writes and no-op patches. Experimental Supervisor coverage uses an isolated Kubernetes namespace and a synthetic Zone with no infrastructure IDs; it validates API/controller behavior, not placement or hardware compatibility.

## Resolved limits delivery (vmop-4322)

Build on the hardware-compatibility draft, which already integrates the current class schema drafts and environment-browser completion. Add a typed field-path status domain, pure resolution in `pkg/vmclass`, and one controller call using the same class/discovery snapshots as governance and hardware compatibility. Existing class, Zone, ConfigTarget, ConfigOptions and GuestOptions watches refresh the output; the live classless capability gates reconciliation. Recompute each result rather than preserving stale options.

Deliver three reviewable slices: resource status and CPU/memory domains; compute/boot/advanced policy domains; device options. Each slice includes tests and experimental installed-cluster E2E coverage. The first establishes conversion/deepcopy/storage coverage, exact discrete CPU choices, governance intersections, unknown discovery handling and watch-driven refresh. Remaining deferred schema fields, provider reserved-profile publication, raw presets and instance-storage presets are not new governed domains. Hardware-dependent fields without a supported discovery mapping remain explicitly unknown.

Available domains are marginal choices across a zone, never a joint placement or capacity guarantee. A missing source-cluster snapshot prevents a complete claim for option-dependent fields. Do not invent bounds using aggregate pool capacity or widen discrete/disjoint hardware domains. Per-field policy constraints remain visible when hardware evidence is unavailable. API additions are in the unreleased v1alpha6 schema and survive existing conversion annotations; regenerate class and class-instance CRDs and deepcopy code. Live Supervisor validation remains pending until an appropriate testbed is available.

Status-only matcher types preserve the spec matcher shape without duplicating admission CEL inside deeply nested status lists. The controller emits these reported constraints; the source class policies retain their existing validation. The API-server installation and controller watch tests pass with the generated schema.
