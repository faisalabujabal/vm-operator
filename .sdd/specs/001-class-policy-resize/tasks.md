# Tasks: VM Service Class Policy and Resize

- **Input**: `specs/001-class-policy-resize/spec.md` + `specs/001-class-policy-resize/plan.md`
- **Test design**: [`tds.md`](tds.md) — §§ 11–15 are authoritative for test status; **this file is a task ledger, not a coverage report**
- **Epic**: vmop-3331
- **Format**: `[ID] [P?] [Story] Description — file paths`

> **Ledger conventions.** A task marked `[~]` was **superseded** — the work was done differently, or turned out not to be needed — and carries a note saying so. Do not re-open a `[~]` task; do not read it as outstanding work. Historically several tasks in this file described files that were never created and never should be; those are now marked `[~]` rather than left unchecked.

Tasks within a Phase that are marked `[P]` may run in parallel. Each task corresponds to a Story or Sub-task; ticket keys are noted where created.

---

## Phase 0 — Architecture (wiki; no code changes)

Dependencies: none. All A-tasks may run in parallel `[P]`.

- [ ] T001 [P] [A1/vmop-3733] Apply draft One Pager content to wiki page ID: 2453059721 — fill Business Problem, Goals, Non-Goals, Big Picture, `ConfigTarget` per-host iteration section (max HW version + per-host SR-IOV with DVX), `VirtualMachineConfigOptions` GC step, capability gate subsection, Test Plan table
- [ ] T002 [P] [A2/vmop-3734] Update wiki pages ID: 2453059723 and 2453060275 — add SR-IOV EB limitation finding; add the `ConfigTarget.status` aggregation pattern (`maxHardwareVersion`, enriched `sriov` list with `hostMoID`); cross-link vmop-3470, vmop-3794, vmop-3926, and the updated Design/TDS
- [ ] T003 [P] [A3/vmop-3735] Author new wiki page "Design: VirtualMachineConfigPolicy" as child of 2453059710 — API schemas, reconcile diagram, `ConfigTarget.status` capability surface, webhook decision flow (using `ConfigTarget.status.maxHardwareVersion`), error taxonomy, open questions
- [ ] T004 [P] [A4/vmop-3736] Author new wiki page "TDS: VirtualMachineConfigPolicy" as child of 2453059710 — package layout, controller+webhook designs, RBAC+capability gating, vSphere API mapping per Query* call, `ConfigTarget` per-host iteration design, integration plan, E2E plan, rollback/disable plan, observability plan; each S1..S9 story referenced from the relevant TDS section
- [ ] T005 [A5/vmop-3737] Run stakeholder review for A1+A3+A4; populate Approver+Date columns on each page; move pages under 2453060106 (Design Docs: Approved); update vmop-3331 epic description with approved page URLs

*Gate: T005 must be complete before Phase 1 code work begins.*

---

## Phase 1 — Foundations (code; sequential)

### Story S1 — Capability + feature gate (vmop-3738)

- [x] T010 [S1.a] [PR #1649 / vmop-3747] Add `CapabilityVirtualMachineConfigPolicy = "supports_vm_service_vm_config_policy"` constant — `pkg/config/capabilities/capabilities.go`
- [x] T011 [S1.a] [PR #1649 / vmop-3747 + PR #1667 / vmop-3748] Wire feature gate check — vim.vmware.com scheme registered in `pkg/manager/manager.go` only when capability enabled; `controllers/vim/controllers.go` stub added with kubebuilder RBAC markers
- [ ] T012 [S1.a] Wire webhook short-circuit in `webhooks/virtualmachine/validation_webhook.go` — skip policy + `ConfigTarget`-based capability checks when capability disabled
- [x] T013 [S1.a] [PR #1649 / vmop-3747] Unit tests for capability gate logic — `pkg/config/capabilities/capabilities_test.go`
- [x] T014 [S1.b] [PR #1667 / vmop-3748] Install the four `vim.vmware.com` CRDs (`ConfigTarget`, `VirtualMachineConfigOptions`, `VirtualMachineGuestOptions`, `VirtualMachineConfigPolicy`) via `config/crd/external-crds/` in the Supervisor chart
- [x] T015a [S1.b] [PR #1667 / vmop-3748] Extend `config/rbac/role.yaml` with get/list/watch on `zones`
- [x] T015b [S1.b] [PR #1695 / vmop-3740] Extend `config/rbac/role.yaml` with create/get/list/patch/update/watch on `configtargets` and `virtualmachineconfigpolicies`
- [ ] T015c [S1.b] Extend `config/rbac/role.yaml` with create/get/list/patch/update/watch on `virtualmachineconfigoptions` and `virtualmachineguestoptions` — deferred to S6/S7 controller stories per commit `1698824b`

### Story S2 — Partner-facing integration doc (vmop-3739)

- [ ] T020 [S2] Author `external/vim/doc/integration-guide.md` — pipeline diagram, role of each CRD, `ConfigTarget.status` capability surface (`maxHardwareVersion`, enriched `sriov`), per-host iteration inside the `ConfigTarget` controller, syncMode semantics, worked example of policy denial
- [ ] T021 [S2] Link doc from public RTD site; review by PM and at least one downstream consumer team

*Gate: T021 must be complete before Phase 2 code work begins.*

---

## Phase 2 — Implementation (code; S5 depends on S1+S2; S3/S6/S7/S8/S9 may begin after S1 is merged)

> *Story S4 was retired.* The previously-planned `HostSystem` CRD work (Story vmop-3741 and its sub-tasks vmop-3752, vmop-3753, vmop-3754, vmop-3755, vmop-3756) is closed as *Won't Implement*. The per-host vSphere queries now happen inside the `ConfigTarget` controller and write directly to `ConfigTarget.status` (see Story S5 for the cluster-scope path; the SR-IOV per-host path is deferred to spec 003). See `research.md` Finding 7 for the rationale.

### Story S3 — Zone controller fan-out (vmop-3740)

- [x] T050 [S3.a] [PR #1695 / vmop-3740] Modify `controllers/infra/zone/zone_controller.go` — derive cluster MoIDs from the Zone's AvailabilityZone (`ClusterComputeResourceMoIDs`); CreateOrPatch ConfigTarget per MoID; CreateOrPatch VirtualMachineConfigPolicy per zone (default syncMode=ConfigTarget on create only)
- [x] T051 [S3.a] [PR #1695 / vmop-3740] Unit tests — `controllers/infra/zone/zone_controller_test.go`
- [x] T052 [S3.b] [PR #1695 / vmop-3740] Integration tests with vcsim — `controllers/infra/zone/zone_controller_test.go`: zone→AvailabilityZone cluster MoID derivation; idempotent create/patch (UID stable); ConfigTarget not deleted when pool MoIDs removed from zone
- [x] T053 [S3.c] [PR #1695 / vmop-3740] E2E test — `test/e2e/vmservice/vmservice/configpolicy/configpolicy.go`: Zone creation materialises ConfigTarget and VirtualMachineConfigPolicy; idempotency verified with Consistently over 30 s

### Story S5 — ConfigTarget controller (vmop-3742)

- [x] T060 [S5.a] [PR #1711 / vmop-3757] Author `webhooks/configtarget/validation/configtarget_validator.go` — immutable spec.id; valid cluster MoID format for metadata.name
- [~] T061 [S5.a] **Superseded by PR #1785.** The ConfigTarget webhook was registered by PR #1711 and is now being *removed* outright: its three checks (spec.id immutability, non-emptiness, `^domain-c[0-9]+$` name format) all moved to CEL rules on the CRD, leaving the validator with nothing to do. Equivalent envtest coverage was added to `controllers/configtarget`'s suite. See `tds.md` § 5.1
- [x] T061a [S5.a] Register ConfigTarget controller in `controllers/controllers.go`, gated on `Features.VirtualMachineConfigPolicy`
- [x] T062 [S5.a] [PR #1711 / vmop-3757] Unit and integration tests — `webhooks/configtarget/validation/configtarget_validator_unit_test.go`, `configtarget_validator_intg_test.go`
- [x] T063 [S5.b] Author `controllers/configtarget/configtarget_controller.go` — cluster-scope path: QueryConfigTarget + QueryConfigOptionDescriptor; populate status; fan out VirtualMachineConfigOptions
- [x] T064 [S5.b] Add `GetVirtualMachineConfigTarget` (QueryConfigTarget + QueryConfigOptionDescriptor) — implemented directly on `pkg/providers/vsphere/vmprovider.go` (see note below) instead of a new `environment_browser.go` file
- [x] T065 [S5.b] Unit tests — `controllers/configtarget/configtarget_controller_test.go` (includes vcsim-backed integration coverage in the same file)

> **Implementation note (T063–T065):** `metadata.name` (not `spec.id`) is the cluster MoID key. This is confirmed correct, not just a convention choice: `spec.md`'s acceptance criteria key every `ConfigTarget` lookup off `metadata.name` (`spec.id` is referenced only as an immutability constraint), and the merged Zone controller (vmop-3740, PR #1695) sets `spec.id.ID` to the same value as `metadata.name` on every `ConfigTarget` it creates — so the two fields are always identical in practice. `GetVirtualMachineConfigTarget` was placed directly on `vSphereVMProvider` in `vmprovider.go` — consistent with other single-purpose vSphere queries in that file (`DoesProfileSupportEncryption`, `GetStoragePolicyStatus`) — rather than the `pkg/providers/vsphere/environment_browser.go` sketched in the plan; that file can still be introduced later once `QueryConfigOptionEx` (S6) and the per-host iteration (S5.c) grow the surface area.
>
- [x] T066b [S5.c] Compute `ConfigTarget.status.maxHardwareVersion` as `max(Key)` among `QueryConfigOptionDescriptor` results with `CreateSupported == true` — data the cluster-scope path already fetches for the `VirtualMachineConfigOptions` fan-out. No host enumeration or `PropertyCollector` calls needed. Implemented in `controllers/configtarget/configtarget_controller.go` (`computeMaxHardwareVersion`).
- [x] T066c [S5.b] Map the 19 non-SR-IOV `ConfigTargetDevices` categories (CDROM, Floppy, Serial, Parallel, Sound, USB, PCIPassthrough, DynamicPassthroughDevices, VGPUDevice, VGPUProfile, SharedGPUPassthroughTypes, SGXTargetInfo, PrecisionClockInfo, VendorDeviceGroupInfo, DVXClassInfo, IDEDisks, SCSIDisks, SCSIPassthrough, VFlashModule) directly from the cluster-scope `QueryConfigTarget` result the controller already fetches. Implemented in `controllers/configtarget/convert.go` (`populateConfigTargetDevices`). SR-IOV (both `ct.Sriov` and any `VirtualMachineSriovInfo` inside the `PciPassthrough` union) is excluded, so `status.sriov` ships empty — see the S10 deferral note below and spec 003.
- [x] T067 [S5.c] Unit tests for `maxHardwareVersion` — aggregation across mixed `CreateSupported`/malformed-key descriptors, and the vcsim-backed multi-host proof, in `controllers/configtarget/configtarget_controller_test.go` (unit + vcsim `Describe`s).
- [~] T068 [S5.b] **Superseded.** No `controllers/configtarget/gc.go` exists or should. GC is implemented inline in the reconciler as owner-reference removal (`removeOwnerRefAndDeleteIfOrphaned`), which is strictly better than the standalone `GCVirtualMachineConfigOptions` helper this task described: it handles a `VirtualMachineConfigOptions` co-owned by two `ConfigTarget`s, which a name-set-difference helper cannot. Still gated on both cluster-scope queries succeeding, as specified. See `tds.md` § 4.2
- [x] T069 [S5.b] Unit tests for the GC path — `controllers/configtarget/configtarget_controller_test.go`: *"garbage-collects the corresponding VirtualMachineConfigOptions"* and *"only removes its own owner reference and leaves the object when another owner remains"*. Renamed from the `GCVirtualMachineConfigOptions` helper per T068
- [x] T070 [S5.d] Integration coverage — folded into the vcsim-backed `Describe` in `controllers/configtarget/configtarget_controller_test.go`. **There is no `test/intg/` tree in this repo**; per `testing-standards.md` unit and integration specs share one `_test.go` per package and are separated by Ginkgo `Label()`. Covers missing cluster, fan-out from the real EnvironmentBrowser, device mapping, and `maxHardwareVersion`. The transient-error/no-GC and drop-a-HW-version cases are covered by the fake-provider unit specs; vcsim's EnvironmentBrowser reports a fixed descriptor set per model and cannot be shrunk at runtime (`tds.md` § 11.2 reason 1)
- [x] T071a [S5.e] E2E test (cluster-scope subset) — `test/e2e/vmservice/vmservice/configpolicy/configpolicy.go`: `ConfigTarget.status` populated from real EB and `Ready=True`; `VirtualMachineConfigOptions` fanned out per hardware version; a synthetic-hardware-version `VirtualMachineConfigOptions` owned by a real `ConfigTarget` is garbage-collected on the next reconcile (vcsim's EnvironmentBrowser reports a fixed descriptor set per model, so the drop-a-version GC path can't be exercised by shrinking a real cluster's reported versions — this test injects the staleness directly instead).
- [x] T071 [S5.e] E2E test — `test/e2e/vmservice/vmservice/configpolicy/configpolicy.go`: `status.maxHardwareVersion` non-empty/valid and `CDROM` (a universally-present, non-SR-IOV category) non-empty. Per-host SR-IOV E2E coverage is deferred to spec 003.

### Story S6 — VirtualMachineConfigOptions controller (vmop-3743)

- [x] T080 [S6.a] [PR #1671 / vmop-3762] Author `webhooks/virtualmachineconfigoptions/validation/virtualmachineconfigoptions_validator.go` — immutable spec.hardwareVersion; format ^vmx-\d+$; metadata.name must equal spec.hardwareVersion
- [x] T081 [S6.a] [PR #1671 / vmop-3762] Unit tests (13 specs) + integration test stubs — `webhooks/virtualmachineconfigoptions/validation/`
- [x] T082 [S6.b] [PR #1672] Author `controllers/virtualmachineconfigoptions/vmconfigoptions_controller.go` — QueryConfigOptionEx; map vim.vm.ConfigOption → status; fan out VirtualMachineGuestOptions per guest OS; update listMap entry keyed by hardwareVersion
- [x] T083 [S6.b] [PR #1672] Add `pkg/providers/vsphere/environment_browser.go` — `QueryConfigOptionEx` wrapper, consistent with T064's note that single-purpose EnvironmentBrowser queries can live in a dedicated file once the surface area grows
- [x] T084 [S6.b] [PR #1672] Unit tests — `controllers/virtualmachineconfigoptions/vmconfigoptions_controller_test.go` (envtest-backed suite; 5 specs, 75.5% coverage)
- [x] T085 [S6.c] [PR #1672] Integration coverage — folded into `controllers/virtualmachineconfigoptions/vmconfigoptions_controller_test.go` per `testing-standards.md` (no separate `test/intg` tree exists in this repo; unit/integration tests are differentiated by Ginkgo `Label()`, not by file path). Covers happy path and idempotent re-reconcile; no golden-file fixture was added.
- [x] T086 [S6.d] [PR #1672] E2E test — `test/e2e/vmservice/vmservice/configpolicy/configpolicy.go`: `VirtualMachineConfigOptions.status.guestOSIdentifiers` populated and `Ready=True`, and a `VirtualMachineGuestOptions` fanned out per reported guest OS (folded into the shared Spec function established by S3/S5, not a standalone `vmconfigoptions_test.go`)

### Story S7 — VirtualMachineGuestOptions plumbing (vmop-3744)

- [ ] T090 [S7.a] [PR #1779 / vmop-3766 — in review] Author `webhooks/virtualmachineguestoptions/validation/virtualmachineguestoptions_validator.go` — `metadata.name == dnsSafe(spec.id)` and spec.id immutability in Go (the transform is not expressible in CEL); `spec.id` non-emptiness deferred to a CRD CEL rule. Shared transform in `pkg/util/vimguestoptions.go`
- [ ] T091 [S7.a] [PR #1779] Unit + intg tests, `webhooks/webhooks.go` registration, `config/webhook/manifests.yaml`
- [ ] T092 [S7.b] [PR #1781 / vmop-3767 — in review] vcsim integration tests, added to `controllers/virtualmachineconfigoptions/vmconfigoptions_controller_test.go` — **not** a `test/intg/virtualmachineguestoptions/` tree, which does not exist: two `VirtualMachineConfigOptions` (vmx-21, vmx-22) fan in to one `VirtualMachineGuestOptions` with two `hardwareVersions` listMap entries; re-reconcile updates exactly one entry
- [x] T093 [S7.c] E2E test — `test/e2e/vmservice/vmservice/configpolicy/configpolicy.go`: *"Should fan out a VirtualMachineGuestOptions object for each guest OS reported by the cluster"*, asserting `status.fullName`, `status.family`, and a `hardwareVersions` entry per contributing version
- [x] T093a [S7.d] [vmop-3932] Author `garbageCollectGuestOptions` / `removeHardwareVersionAndDeleteIfOrphaned` in `controllers/virtualmachineconfigoptions/vmconfigoptions_controller.go` — prunes a `VirtualMachineGuestOptions.status.hardwareVersions` entry once its hardware version's `VirtualMachineConfigOptions` reconcile no longer reports that guest OS, deleting the object once no hardware-version entries remain. Closes the gap noted in `research.md` Finding 3 and the former spec.md edge case/out-of-scope entries.
- [x] T093b [S7.d] [vmop-3932] Unit tests — `controllers/virtualmachineconfigoptions/vmconfigoptions_controller_test.go`: a guest OS dropped from a hardware version's descriptor list deletes the sole-owning `VirtualMachineGuestOptions`; a guest OS still reported by a second hardware version keeps the object and only removes the dropped version's status entry

### Story S8 — VirtualMachineConfigPolicy controller (vmop-3745)

- [~] T100 [S8.a] **Superseded.** No defaulting webhook is needed or exists. All five fields carry `+kubebuilder:default=` on the type (`syncMode=ConfigTarget`, `createMode`/`updateMode`/`powerOnMode=Allow`, `vmClassMode=AsPolicy`), so the API server applies them. Per `.sdd/memory/constitution.md` as amended by PR #1779, CEL and schema defaults are preferred over Go webhooks for plain structural rules. **Follow-up**: nothing asserts the defaulted values against a real API server — tracked as GAP-2 in `tds.md` § 14.4
- [ ] T101 [S8.a] [PR #1783 / vmop-3769 — in review] Author `webhooks/virtualmachineconfigpolicy/validation/virtualmachineconfigpolicy_validator.go` — `spec.zone` must reference an existing Zone (a live cluster read, so Go not CEL), with a carve-out letting the VM Operator service account through so the Zone controller's own fan-out cannot deadlock against its validator. `extraConfig` key/type checks are CEL
- [ ] T102 [S8.a] [PR #1783] Unit + intg tests — `webhooks/virtualmachineconfigpolicy/validation/`
- [ ] T103 [S8.b] [PR #1784 / vmop-3770 — in review] Author `controllers/virtualmachineconfigpolicy/vmconfigpolicy_controller.go` — syncMode=ConfigTarget: copy ConfigTarget.status → policy spec; syncMode=Disabled: set Ready=True, skip sync; never overwrite extraConfig/latencySensitivityLevels/txRxThreadModels
- [ ] T104 [S8.b] [PR #1784] Author `pkg/util/configpolicysync/configpolicysync.go` — **not** `pkg/vmconfig/policy/policy_reconciler.go`: that package already exists and does unrelated per-VM tag/PolicyEvaluation reconciliation. ConfigTarget→policy field mapping, multi-cluster merge by intersection
- [ ] T105 [S8.b] [PR #1784] Unit tests — `pkg/util/configpolicysync/configpolicysync_test.go`. **Outstanding gap**: no test drives a range `Max` *downward*, and a shrink can produce `Min > Max` with no validation — `tds.md` § 15 CHK-7 and CHK-8, both fixable in this PR
- [ ] T106 [S8.c] [PR #1784 / vmop-3771] Integration tests co-located in `controllers/virtualmachineconfigpolicy/vmconfigpolicy_controller_test.go` as a second `Describe` (Label `EnvTest`+`VCSim`) — **not** `test/intg/virtualmachineconfigpolicy/`, which does not exist
- [ ] T107 [S8.d] [PR #1784 / vmop-3772] E2E test — `test/e2e/vmservice/vmservice/configpolicy/configpolicy.go`: policy spec filled from the zone's real ConfigTarget; toggling `syncMode=Disabled` stops the sync

### Story S9 — VM admission webhook enforcement (vmop-3746)

- [ ] T110 [S9.a] Modify `webhooks/virtualmachine/validation_webhook.go` — on create/update/powerOn, load namespace VirtualMachineConfigPolicy; apply createMode/updateMode/powerOnMode; respect vmClassMode=AsPolicy vs AsConfig
- [ ] T111 [S9.a] Unit tests for mode enforcement
- [ ] T112 [S9.b] Implement ExtraConfig Allow/Deny enforcement — Fixed (exact), Regex (regexp), Glob (filepath.Match); Denied takes precedence over Allowed
- [ ] T113 [S9.b] Unit tests for all three match types and precedence rules
- [ ] T114 [S9.c] Implement `ConfigTarget`-based hardware-version check — resolve the VM's zone to a cluster MoID (at the moment, there is a single vSphere cluster per zone, though that could change in the future), `Get` the cluster's `ConfigTarget`, reject if the VM's effective hardware version exceeds `ConfigTarget.status.maxHardwareVersion`. No `HostSystem` list, no label selectors.
- [ ] T115 [S9.c] Unit tests for HW-version comparison and the `Zone → cluster MoID → ConfigTarget` resolution path.
- [ ] T116 [S9.d] [P] Integration tests with vcsim, co-located in `webhooks/virtualmachine/validation/`'s own `_test.go` — **not** `test/intg/webhook/`, which does not exist: mode-deny, extraConfig-deny, HW-version-deny (via `ConfigTarget.status.maxHardwareVersion`), plus a happy path for each. Add the fail-closed case when the zone's `ConfigTarget` is missing or not Ready (`tds.md` GAP-5).
- [ ] T117 [S9.e] E2E test — extend `test/e2e/vmservice/vmservice/configpolicy/configpolicy.go`'s existing `Spec()` rather than adding a standalone `vm_policy_test.go`, matching what S3/S5/S6/S8 did: rejected VM on mode-deny; rejected on extraConfig-deny; rejected on HW-version-deny (driven by `ConfigTarget.status.maxHardwareVersion`); accepted on happy path; all assertions with clear reason strings.

### Story S10 — ConfigTarget SR-IOV per-host enrichment (vmop-3926) — DEFERRED

**Designed under this spec (see [`plan.md`](plan.md) I8); implemented under spec [`003-configtarget-sriov-per-host`](../003-configtarget-sriov-per-host/).** Not in this release. vmop-3926 has no Epic Link and must be re-linked to the new epic created for spec 003.

The tasks are retained below rather than deleted, so the ledger stays complete and so the mapping into spec 003 is explicit. Each is marked `[~]` — do not work them here.

- [~] T120 [S10.a] Extend `VirtualMachineSriovInfo` with `HostMoID` (required), `Active`, `MaxVFs`, `NumVFs`, `DVXClass`, `DVXCheckpointSupported`, `DVXSWDMATracingSupported`; add `+listType=map` with `+listMapKey=hostMoID` and `+listMapKey=pciDevice.id` to `status.sriov`; regenerate deepcopy and the CRD manifest → **spec 003 T010–T012**, gated on that spec's T000 (is the change additive-safe against a shipped 9.2 `ConfigTarget`?)
- [~] T121 [S10.b] Extend `controllers/configtarget/configtarget_controller.go` with per-host SR-IOV enrichment — host enumeration, one `PropertyCollector` RPC per host, warning event on per-host failure without blocking the iteration → **spec 003 T020–T022 (provider seam) and T030–T032 (controller)**. Spec 003 adds a per-host error map to the provider interface so partial failure is unit-testable without RPC interception, which this task did not specify
- [~] T122 [S10.b] Unit tests — `hostMoID` attribution; DVX class correlation; partial failure leaves other hosts intact and triggers a requeue → **spec 003 T033–T035**
- [~] T123 [S10.c] Integration tests with vcsim → **spec 003 T040**, now explicitly conditional on that spec's T002 (can vcsim report these host properties at all?). Writing a vcsim test that asserts an empty list is worse than writing none
- [~] T124 [S10.d] E2E test — per-host SR-IOV entries with `hostMoID` on SR-IOV-capable clusters → **spec 003 T041**, ENV-BLOCKED on vmop-3965. Must skip loudly rather than pass vacuously

**What stays in this release**: `ConfigTarget.status.sriov` ships as an always-empty list. T066c already excludes SR-IOV from the device mapping — both `ct.Sriov` and any `VirtualMachineSriovInfo` inside the `PciPassthrough` union — and a unit test asserts it. That "always empty" property is what would make spec 003's API extension additive-safe later, so do not weaken that test.

---

### Story S11 — Demand gate: run the pipeline only when a policy needs it (vmop-3983)

Added after the original plan. Rationale, design, and the blocking design question are in `tds.md` § 7.3.

- [ ] T130 [S11.a] **Blocking decision.** The Zone controller auto-creates a `VirtualMachineConfigPolicy` per Zone and the CRD defaults `syncMode=ConfigTarget`, so a qualifying policy always exists and the gate would never close. Choose the opt-in signal: (1) Zone controller defaults new policies to `syncMode=Disabled`; (2) Zone controller stops auto-creating policies; or (3) gate on an explicit namespace label / policy field. Record the choice and rationale in `tds.md` § 7.3 before any code. Options 1 and 2 invalidate two currently-passing tests (`zone_controller_test.go` *"creates one VirtualMachineConfigPolicy per zone"* and the matching E2E)
- [ ] T131 [S11.a] Add a field index on the chosen opt-in predicate so `demandExists` is an informer-cached, server-side-filtered `List` — `controllers/configtarget/`
- [ ] T132 [S11.b] Gate the EnvironmentBrowser calls in `controllers/configtarget/configtarget_controller.go`: when no qualifying policy exists, set `Ready=False` with a distinct `NoPolicyDemand` reason, make **zero** EB RPCs, leave `status.*` untouched, and **do not run GC**
- [ ] T133 [S11.b] Watch `VirtualMachineConfigPolicy` from the `ConfigTarget` controller and map create/update events to all `ConfigTarget`s, so the first opt-in converges without waiting for a resync
- [ ] T134 [S11.b] Decide and document what happens to objects already created when the last policy opts out. Leaving them is the choice consistent with the capability-off behaviour in `tds.md` § 9 — state it explicitly rather than letting it be emergent
- [ ] T135 [S11.c] Unit tests — `tds.md` TC-EX-21, TC-EX-21r, TC-EX-21g, TC-EX-21o. Assert on the **fake provider's call count**, not merely on object absence: absence could equally mean a silent failure
- [ ] T136 [S11.c] vcsim test — `tds.md` TC-EX-21c: a policy appearing re-enqueues every `ConfigTarget` and the pipeline converges with no manual reconcile
- [ ] T137 [S11.d] E2E — `tds.md` TC-EX-21e. Shape depends on T130's choice; options 1 and 2 also require updating the existing zone-fan-out E2E

---

## Phase 3 — Cleanup + verification (after all Phase 2 tasks merged)

- [ ] T200 Run `make generate manifests` in vim-api; verify no uncommitted diffs
- [ ] T201 Run `golangci-lint run ./...`; fix all new lint errors
- [ ] T202 Run `make test` (unit + integration); confirm all tests pass
- [ ] T203 Verify `test/e2e/` suite runs green on the test Supervisor cluster
- [ ] T204 Update `external/vim/doc/controller-workflows.md` to reflect any implementation changes from the plan
- [ ] T205 Update [`tds.md`](tds.md) and its Confluence mirror with any implementation deviations discovered during coding
- [ ] T206 Work the 18 test-design challenge findings in `tds.md` § 15. **CHK-1 is urgent**: the E2E GC fixture is named `vmx-e2e-stale-vmop-3760`, which the CEL rule PR #1785 adds (`^vmx-[0-9]+$`) will reject — that E2E spec breaks the moment #1785 merges. Fix it in #1785
- [ ] T207 File the follow-ups in `tds.md` § 14.4 / § 14.5: nine code sub-tasks (GAP-1..GAP-9), two doc corrections (GAP-DOC-1, GAP-DOC-2), two manual runbooks (GAP-M1, GAP-M2)
