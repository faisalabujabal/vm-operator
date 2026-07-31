# Tasks: ConfigTarget Per-Host SR-IOV Enrichment

- **Input**: [`spec.md`](spec.md) + [`plan.md`](plan.md); design inherited from spec 001 [`plan.md`](../001-class-policy-resize/plan.md) I8
- **Supersedes**: spec 001 `tasks.md` T120–T124 (marked `[~]` there, with a mapping into the task IDs below)
- **Story**: vmop-3926
- **Epic**: TBD — a new epic must be created; vmop-3926 currently has **no** Epic Link
- **Status**: Not scheduled. Phase 0 is a hard gate.
- **Format**: `[ID] [P?] [Story] Description — file paths`

---

## Phase 0 — Answer the blocking questions (no code)

These are `plan.md` § 2. Every one of them changes the shape of the implementation, and three of them can invalidate work already done if answered late.

- [ ] T000 [Q1] Determine whether the shipped `ConfigTarget` CRD ever populates `status.sriov`. If it does, produce a schema-transition plan for `+required hostMoID` and the `+listType=map` change; if it does not, record that finding so the additive change can proceed without a conversion story — `external/vim/api/v1alpha1/config_target_devices_types.go`, `.sdd/memory/constitution.md` § *API compatibility*
- [ ] T001 [Q2] Decide and document whether a total per-host RPC failure sets `Ready=False` or leaves `Ready` reflecting the cluster-scope result. Record the rationale in `plan.md` § 4.2 — this is what the admission webhook keys off
- [ ] T002 [Q3] Prove or disprove that vcsim can report `config.pciPassthruInfo` and `hardware.dvxClasses` (pre-seeding host properties the way `configtarget_controller_test.go`'s vcsim `Describe` pre-seeds `QueryConfigTargetResponse`). Record the answer in `research.md`
- [ ] T003 [Q4] Decide whether a disconnected or maintenance-mode host is a failure (warning event + requeue) or a silent skip
- [ ] T004 Create the epic for this spec and set vmop-3926's Epic Link to it

*Gate: T000–T004 must be complete before Phase 1 begins. T000 in particular — an API change made before it is answered may have to be reverted.*

---

## Phase 1 — API

- [ ] T010 [US1] Extend `VirtualMachineSriovInfo` with `hostMoID` (`+required`), `active`, `maxVFs`, `numVFs`, `dvxClass`, `dvxCheckpointSupported`, `dvxSwDmaTracingSupported` — `external/vim/api/v1alpha1/config_target_devices_types.go`
- [ ] T011 [US1] Add `+listType=map` with `+listMapKey=hostMoID` and `+listMapKey=pciDevice.id` to `ConfigTargetStatus.SRIOV` — `external/vim/api/v1alpha1/config_target_types.go`
- [ ] T012 [US1] `make generate-go generate-external-manifests`; commit `zz_generated.deepcopy.go` and `config/crd/external-crds/vim.vmware.com_configtargets.yaml`
- [ ] T013 [US1] envtest: patching one host's `status.sriov` entries leaves another host's entries intact — proves the listMap keys work against a real API server. **Required**; listMap semantics are invisible to the fake client — `controllers/configtarget/configtarget_controller_test.go`

*T014 cannot be merged before T010–T012.*

---

## Phase 2 — Provider

- [ ] T020 [US1/US2] Add `GetClusterHostSriovInfo(ctx, clusterMoID) (map[string]HostSriovProperties, map[string]error, error)` to the `VMProvider` interface and implement it — one bounded-concurrency `PropertyCollector` RPC per host for `config.pciPassthruInfo` and `hardware.dvxClasses`; wrap the context once with `pkgctx.WithVCOpID(ctx, obj, "hostSriovProperties")` — `pkg/providers/vsphere/environment_browser.go`
- [ ] T021 [US2] Name the concurrency bound as a constant with a comment stating the reasoning (session pressure on 64+-host clusters); honour `ctx.Done()` in the worker loop
- [ ] T022 [US1/US2] Add the fake-provider counterpart so unit tests can fail exactly one host — `pkg/providers/fake/`

*The per-host error map in T020 is the design decision that makes `spec.md` US2.1 unit-testable without RPC interception. Do not collapse it into a joined error.*

---

## Phase 3 — Controller

- [ ] T030 [US1] Author `controllers/configtarget/sriov.go` — build one `VirtualMachineSriovInfo` per (host, NIC) from the provider result; filter to `sriovCapable == true`; ignore non-`HostSriovInfo` passthrough entries
- [ ] T031 [US1] DVX correlation: match each NIC's device class against `dvxClasses` where `sriovNic == true`; deterministic tie-break for two matching classes (mirrors the lexicographically-lowest precedent in `controllers/virtualmachineconfigoptions`)
- [ ] T032 [US1/US2] Wire into `configtarget_controller.go` after the cluster-scope path: wholesale rewrite of `status.sriov`; warning event per failing host; `pkgerr.RequeueError` on partial failure; **never** gate GC on the per-host result
- [ ] T033 [US1] Unit tests — entries carry `hostMoID`; the same NIC on two hosts yields two entries; DVX enrichment and its tie-break; DVX-absent NIC still emitted with empty `dvxClass`; `sriovCapable == false` excluded; non-SR-IOV passthrough filtered; cluster-scope `ct.Sriov` never used; empty cluster
- [ ] T034 [US2] Unit tests — one host fails → other hosts' entries intact, warning event names the host, reconcile requeues; recovery on the next pass emits no further event; total failure behaves per T001's decision
- [ ] T035 [US3] Unit tests — host removed from the result set → its entries disappear (wholesale-rewrite proof); `active=false` while `maxVFs` is still reported

---

## Phase 4 — Integration and E2E

- [ ] T040 [P] vcsim integration tests — **conditional on T002.** If vcsim can report the properties: `status.sriov` entries carry `hostMoID`; one host's failure does not block others. If it cannot: record that in `research.md` and do **not** write a test that asserts an empty list — `controllers/configtarget/configtarget_controller_test.go`
- [ ] T041 E2E — per-host SR-IOV entries with `hostMoID` on an SR-IOV-capable cluster. **ENV-BLOCKED on vmop-3965.** Must skip loudly (an explicit `Skip` naming the missing capability), not pass vacuously — `test/e2e/vmservice/vmservice/configpolicy/configpolicy.go`
- [ ] T042 Re-check vmop-3965's status before declaring this spec complete. If SR-IOV hardware is still unavailable, the spec ships with unit + envtest coverage only and that must be stated explicitly in the release notes, not discovered later

---

## Phase 5 — Cleanup

- [ ] T050 Update spec [`001-class-policy-resize`](../001-class-policy-resize/)'s `tds.md` § 12.9 to point at this spec's outcome
- [ ] T051 `golangci-lint run ./...` clean across the root module and `external/vim/api`
- [ ] T052 Verify `make generate-external-manifests` produces no uncommitted diff
