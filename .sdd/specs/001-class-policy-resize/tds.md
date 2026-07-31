# TDS: VirtualMachineConfigPolicy

- **Feature**: `.sdd/specs/001-class-policy-resize/`
- **Epic**: vmop-3331 — *Implement Policy Object to enable flexible workloads (classless VMs)*
- **Story**: vmop-3736 — *Author "TDS: VirtualMachineConfigPolicy"*
- **Design (A3)**: vmop-3735 (Done)
- **Inputs**: [`spec.md`](spec.md) (acceptance criteria), [`plan.md`](plan.md) (technical approach), [`research.md`](research.md) (Findings 1–7), [`tasks.md`](tasks.md) (task ledger)
- **Created**: 2026-07-31
- **Status**: Draft — pending review by the implementing engineers for S3..S9

> **Relationship to the Confluence TDS.** vmop-3736's acceptance criteria require a Confluence page titled *TDS: VirtualMachineConfigPolicy* under parent page ID `2453059710`. This repo document is the authoritative, review-in-PR source of truth for the technical design and the test design; it does **not** by itself close vmop-3736. A wiki mirror of this content still has to be published and signed off (see [Open items](#17-open-items)).

---

## 1. Scope

### 1.1 In scope

The Kubernetes-native environment-browser + policy pipeline described in `spec.md`:

- Discovery of per-cluster vSphere capabilities into four `vim.vmware.com/v1alpha1` CRDs: `ConfigTarget`, `VirtualMachineConfigOptions`, `VirtualMachineGuestOptions` (all cluster-scoped) and `VirtualMachineConfigPolicy` (namespace-scoped).
- The controllers that materialise and garbage-collect those objects.
- The validation (CEL and webhook) surface for each CRD.
- The `VirtualMachine` admission-time enforcement of the namespace policy and of cluster hardware-version feasibility.
- The single capability gate that turns the whole pipeline on and off.

### 1.2 Out of scope

- **The "Resize" half of the epic name.** Automatic resize of running VMs when a tenant policy expands is a separate epic per `spec.md` § *Out of scope*. Nothing in this TDS covers it.
- **`HostSystem` CRD, controller, webhook, label contract, and RBAC.** Dropped — see `research.md` Finding 7 and the vmop-3736 scope-update comment. vmop-3741 and vmop-3752..vmop-3756 are closed *Won't Implement*. Per-host data lives inline on `ConfigTarget.status`.
- **Building per-host SR-IOV enrichment of `ConfigTarget.status.sriov`** — Story S10. The *design* stays in this spec ([`plan.md`](plan.md) I8, [`spec.md`](spec.md) US1.5/US1.6/US2.3, § 8.1 here) because it is what explains the shape of `ConfigTarget.status`; the *implementation* is **deferred to a future release** under spec [`003-configtarget-sriov-per-host`](../003-configtarget-sriov-per-host/), against a new epic. vmop-3926 must be re-linked to it. Nothing in this release depends on it: the cluster-scope `QueryConfigTarget` call supplies all 19 non-SR-IOV device categories, and `status.maxHardwareVersion` is descriptor-derived (§ 3). See § 12.9 for the test cases that moved.
- **The 9.2 auto-NUMA / auto-placed-SR-IOV admission rules.** Open Spike vmop-3794; this TDS records the constraint (the answer must be derivable from Kubernetes objects alone) and the test rows that will exist once the spike concludes, but does not invent the rule.
- **Telco-specific ExtraConfig fields** (vmop-3388) and **auto-deletion of `VirtualMachineConfigPolicy` when its `Zone` is deleted** (follow-up).

---

## 2. Package layout

Reality as of `origin/main` at the time of writing, plus what is in flight. The layout in `plan.md` § *Repository layout* was aspirational in three places; where it diverges, **this section wins** and `plan.md` should be corrected.

### 2.1 On `main`

```
external/vim/api/v1alpha1/
├── config_target_types.go                 ConfigTarget + ConfigTargetStatus (incl. maxHardwareVersion)
├── config_target_devices_types.go         ConfigTargetDevices, VirtualMachineSriovInfo
├── virtualmachine_config_options_types.go
├── virtualmachine_guest_options_types.go
├── virtualmachine_config_policy_types.go
└── testdata/xml/…                         recorded QueryConfigTarget / QueryConfigOption* payloads

config/crd/external-crds/
├── vim.vmware.com_configtargets.yaml
├── vim.vmware.com_virtualmachineconfigoptions.yaml
└── vim.vmware.com_virtualmachineguestoptions.yaml

controllers/
├── infra/zone/zone_controller.go          fans out ConfigTarget + VirtualMachineConfigPolicy
├── configtarget/configtarget_controller.go
└── virtualmachineconfigoptions/vmconfigoptions_controller.go

pkg/util/vsphere/configtarget/
├── convert.go                             vim ConfigTarget → CRD status device categories
└── status.go

webhooks/
├── configtarget/validation/               (removed by PR #1785 — see § 6)
└── virtualmachineconfigoptions/validation/ (removed by PR #1785 — see § 6)

test/e2e/vmservice/vmservice/configpolicy/configpolicy.go
```

Two `plan.md` deviations already landed and are correct:

- The device-category converter lives at `pkg/util/vsphere/configtarget/convert.go`, not `controllers/configtarget/convert.go` (`tasks.md` T066c still says the latter).
- There is no `controllers/configtarget/gc.go`. Garbage collection is implemented inline in the `ConfigTarget` reconciler as owner-reference removal (see § 4.2), which is a stronger design than the standalone `GCVirtualMachineConfigOptions` helper sketched in `plan.md` because it handles multi-`ConfigTarget` co-ownership. `tasks.md` T068/T069 should be re-worded to match rather than left open.

### 2.2 In flight (open PRs)

| Path | PR | Story / sub-task |
|------|----|------------------|
| `webhooks/virtualmachineguestoptions/validation/`, `pkg/util/vimguestoptions.go` | [#1779](https://github.com/vmware-tanzu/vm-operator/pull/1779) | vmop-3766 |
| `controllers/virtualmachineconfigoptions/` (vcsim listMap-merge tests) | [#1781](https://github.com/vmware-tanzu/vm-operator/pull/1781) | vmop-3767 |
| `controllers/virtualmachineconfigoptions/` (GuestOptions pruning) | [#1782](https://github.com/vmware-tanzu/vm-operator/pull/1782) | vmop-3932 |
| `webhooks/virtualmachineconfigpolicy/validation/` | [#1783](https://github.com/vmware-tanzu/vm-operator/pull/1783) | vmop-3769 |
| `controllers/virtualmachineconfigpolicy/`, `pkg/util/configpolicysync/` | [#1784](https://github.com/vmware-tanzu/vm-operator/pull/1784) | vmop-3770 / vmop-3771 / vmop-3772 |
| CEL retrofit; deletes `webhooks/configtarget` and `webhooks/virtualmachineconfigoptions` | [#1785](https://github.com/vmware-tanzu/vm-operator/pull/1785) | vmop-3766 (follow-up) |
| GuestOptions → ConfigOptions owner refs *(draft)* | [#1789](https://github.com/vmware-tanzu/vm-operator/pull/1789) | vmop-3932 |

The other open PRs authored by this feature's owner — #1772, #1768, #1763, #1755, #1748, #1684 — are unrelated E2E-infrastructure and bug work and carry no rows in the test matrix.

### 2.3 Not yet started

```
webhooks/virtualmachine/validation_webhook.go   MODIFY — policy + ConfigTarget enforcement (S9 / vmop-3746)
external/vim/doc/integration-guide.md           NEW    — partner doc (S2 / vmop-3739)
```

### 2.4 Never (retired)

`controllers/hostsystem/`, `webhooks/hostsystem/`, `hostsystem_types.go`, `vim.vmware.com_hostsystems.yaml`, `config/rbac/hostsystem_*.yaml`.

---

## 3. API contract pinned by vmop-3797

vmop-3736's scope-update comment requires this TDS to pin the API contract introduced by vmop-3797. It is additive on `ConfigTarget` only; no new CRD kinds.

| Field | Type | Source | Status |
|-------|------|--------|--------|
| `status.maxHardwareVersion` | `string` | `max(Key)` over `QueryConfigOptionDescriptor` results with `CreateSupported == true` | Landed |

The per-host `status.sriov[]` extensions originally tracked under vmop-3797 (`hostMoID`, `active`, `maxVFs`, `numVFs`, and the three DVX fields) are **deferred** to spec [`003-configtarget-sriov-per-host`](../003-configtarget-sriov-per-host/). **This release ships `status.sriov` as an always-empty list** — the `ConfigTarget` controller explicitly excludes SR-IOV from what it writes, both `ct.Sriov` and any `VirtualMachineSriovInfo` nested in the `PciPassthrough` union, and TC-US1-02d asserts exactly that. Consumers must not read the field in 9.2.

That "always empty in the shipped release" property is load-bearing for spec 003: it is what would make adding a `+required hostMoID` and a `+listType=map` transition additive-safe later. Spec 003 `tasks.md` T000 must confirm it rather than assume it.

**Design note — `maxHardwareVersion` is descriptor-derived, not host-derived.** `HostConfigInfo` exposes no `defaultHardwareVersion`-equivalent property. `QueryConfigOptionDescriptor` already returns, per hardware-version key, a `CreateSupported` flag that vSphere computes by taking the max over the cluster's hosts' ESXi builds. The controller therefore needs **no** per-host `PropertyCollector` call for this field. `spec.md` US1 scenario 4 and US2 scenarios 1–2 are still phrased in terms of per-host `config.defaultHardwareVersion`; the observable outcome is identical, but the wording is stale and should be corrected in `spec.md` (tracked as **GAP-DOC-1** in § 14.4).

---

## 4. Controller designs

All four reconcilers follow the canonical loop in `.sdd/memory/operator-best-practices.md`: `pkgcfg.JoinContext` → `Get` (ignore NotFound) → typed context → `patch.NewHelper` with a deferred patch → delete vs. normal branch. Only the parts specific to this feature are described below.

### 4.1 Zone controller — `controllers/infra/zone/zone_controller.go` (S3 / vmop-3740, merged)

- **Watches**: `Zone` (primary). Fan-out block is guarded by `pkgcfg.FromContext(ctx).Features.VirtualMachineConfigPolicy`.
- **Reconcile sketch**:
  1. Derive the set of unique cluster MoIDs from the Zone's `AvailabilityZone` (`ClusterComputeResourceMoIDs`).
  2. Per MoID, `CreateOrPatch` a cluster-scoped `ConfigTarget` with `metadata.name = clusterMoID` and `spec.id.ID = clusterMoID`. The two are always identical in practice; every lookup in this design keys off `metadata.name`.
  3. `CreateOrPatch` one namespace-scoped `VirtualMachineConfigPolicy` per zone with `spec.zone = zone.metadata.name`, setting `spec.syncMode = ConfigTarget` **on create only** so a tenant admin's later switch to `Disabled` is not reverted.
- **Non-deletion contract**: removing a pool MoID from the Zone does **not** delete the `ConfigTarget`. Deletion is deferred (see § 16).

### 4.2 ConfigTarget controller — `controllers/configtarget/` (S5 / vmop-3742, merged)

- **Watches**: `ConfigTarget` (cluster-scoped, primary).
- **Cluster-scope path**:
  1. Resolve `metadata.name` to a `ClusterComputeResource`; failure → `Ready=False`, `ClusterNotFound`-style reason, no fan-out, no GC.
  2. `EnvironmentBrowser.QueryConfigTarget` → capacity, security flags, and the 19 non-SR-IOV `ConfigTargetDevices` categories via `pkg/util/vsphere/configtarget/convert.go`. SR-IOV is deliberately excluded here — both `ct.Sriov` and any `VirtualMachineSriovInfo` nested in the `PciPassthrough` union — because cluster-scope `QueryConfigTarget` does not return it (`research.md` Finding 1).
  3. `EnvironmentBrowser.QueryConfigOptionDescriptor` → the live hardware-version key set; `status.maxHardwareVersion = max(Key where CreateSupported)`. Malformed keys are skipped, not fatal.
  4. Per live key, `CreateOrPatch` a `VirtualMachineConfigOptions` named for the key, adding this `ConfigTarget` as an owner reference.
- **Garbage collection** (replaces `plan.md`'s standalone helper): for every `VirtualMachineConfigOptions` whose key is no longer live, remove *this* `ConfigTarget`'s owner reference. Delete the object only when no owner references remain. This makes the multi-cluster case — two clusters that both support `vmx-22` co-owning one object — correct by construction, and avoids a delete racing another cluster's reconcile. GC runs **only** when both cluster-scope queries succeeded.
- **No per-host path in this release.** `status.sriov` is left empty; per-host SR-IOV enrichment is deferred to spec [`003-configtarget-sriov-per-host`](../003-configtarget-sriov-per-host/). The reconciler therefore makes exactly two vCenter calls per pass, both at cluster scope.

### 4.3 VirtualMachineConfigOptions controller — `controllers/virtualmachineconfigoptions/` (S6 / vmop-3743, merged; S7 pruning in flight)

- **Watches**: `VirtualMachineConfigOptions` (primary). The owning `ConfigTarget` is resolved through owner references, not a watch.
- **Owner resolution**: no owner references → requeue with delay, `Ready=False` / `ConfigTargetNotFound`. Multiple owners → query the **lexicographically lowest** cluster MoID, so repeated reconciles are deterministic. vmop-3964 tracks replacing that tie-break with a capability-aware choice.
- **Reconcile sketch**: `QueryConfigOptionEx(spec.hardwareVersion)` → map `vim.vm.ConfigOption` onto status (`guestOSIdentifiers`, `guestOSDefaultIndex`, transport/monitor lists, `SupportLevel`, `Family`, hardware limits) → per guest OS descriptor, `CreateOrPatch` a `VirtualMachineGuestOptions` with `spec.id = guestOsID` and `metadata.name = dnsSafe(guestOsID)` → upsert this hardware version's entry into the child's `status.hardwareVersions` listMap.
- **`dnsSafe` contract**: lower-case, non-`[a-z0-9-]` → `-`, strip leading/trailing separators, truncate to 63 characters. Shared helper `pkg/util/vimguestoptions.go` (PR #1779) so the controller and the GuestOptions validator agree byte-for-byte.
- **Guest-options pruning (vmop-3932, PR #1782)**: this hardware version's entry is removed from every `VirtualMachineGuestOptions` **not** in the current `QueryConfigOptionEx` result; the object is deleted once no hardware-version entries remain. The finalizer is retained when pruning fails so cleanup retries.
- **Reverse owner references (PR #1789, draft)**: each `VirtualMachineGuestOptions` also gains an owner reference to every contributing `VirtualMachineConfigOptions`, so Kubernetes GC becomes the backstop for the explicit pruning above.

### 4.4 VirtualMachineConfigPolicy controller — `controllers/virtualmachineconfigpolicy/` (S8 / vmop-3745, PR #1784)

- **Watches**: `VirtualMachineConfigPolicy` (namespace-scoped, primary), with re-enqueue on `ConfigTarget` becoming `Ready` so a policy created before its `ConfigTarget` converges without a manual nudge.
- **Sync logic** is factored into `pkg/util/configpolicysync/` so the field-mapping is unit-testable without a manager. This is the `pkg/vmconfig/policy/policy_reconciler.go` from `plan.md`, relocated.
- **Modes**:
  - `syncMode = Disabled` → `Ready=True`, reason `SyncDisabled`, `spec` untouched.
  - `syncMode = ConfigTarget` → resolve zone → cluster MoIDs → `ConfigTarget`s. Sync only when **every** resolved `ConfigTarget` is `Ready`; a multi-cluster zone with one `Ready` target must **not** sync, because that would widen the policy beyond the true intersection.
  - Multi-cluster merge = intersection: numeric ranges to the minimum of the per-cluster maxima, boolean support flags ANDed, device categories to entries common to every target.
  - Fields with no `ConfigTarget` source — `extraConfig`, `latencySensitivityLevels`, `txRxThreadModels` — are never written.
- **Idempotence requirement**: an unchanged `ConfigTarget` must not bump the policy's `resourceVersion`. Compare with `apiequality.Semantic.DeepEqual` and skip the write, per `.sdd/memory/operator-best-practices.md`.
- **Conditions**: `ZoneNotFound`, `ConfigTargetNotFound` (zone has no cluster), `ConfigTargetNotReady` (target missing or status not yet populated).

### 4.5 VirtualMachine admission enforcement (S9 / vmop-3746, not started)

Not a controller — a modification to `webhooks/virtualmachine/validation_webhook.go`. See § 5.3.

---

## 5. Webhook and validation design

`.sdd/memory/constitution.md` (as amended by PR #1779) prefers CEL for plain structural rules and reserves Go webhooks for cross-field or vSphere-data-dependent logic. This feature is the first consumer of that rule, and PR #1785 retrofits it to the two webhooks that predated it.

### 5.1 CEL-only CRDs

| CRD | Rule | Kind |
|-----|------|------|
| `ConfigTarget` | `spec.id` immutable (`self == oldSelf`, whole `ManagedObjectID` struct) | transition |
| `ConfigTarget` | `size(self.id) > 0` | field |
| `ConfigTarget` | `self.metadata.name.matches('^domain-c[0-9]+$')` | root |
| `VirtualMachineConfigOptions` | `spec.hardwareVersion` immutable | transition |
| `VirtualMachineConfigOptions` | `self.matches('^vmx-[0-9]+$')` | field |
| `VirtualMachineConfigOptions` | `metadata.name == spec.hardwareVersion` | root |
| `VirtualMachineGuestOptions` | `spec.id` non-empty | field |
| `VirtualMachineConfigPolicy` | `spec.zone` non-empty; `extraConfig[].key` non-empty; `type` enum | field |
| `VirtualMachineConfigPolicy` | `syncMode`, `createMode`, `updateMode`, `powerOnMode`, `vmClassMode` defaults | `+kubebuilder:default=` |

**Consequence for the design in `plan.md`**: the `webhooks/virtualmachineconfigpolicy/defaulting_webhook.go` in `plan.md` I6 and `tasks.md` T100 is **not needed** — schema defaults cover it. And once the CEL retrofit lands, `webhooks/configtarget` and `webhooks/virtualmachineconfigoptions` cease to exist entirely; every check they made is enforced one layer lower.

**Test consequence**: deleting a webhook package deletes its envtest coverage of behavior that is still user-visible. PR #1785 handles this by adding a `Describe("CRD validation", …)` block to each controller's own envtest suite, exercising the CEL rules directly against a real API server. **Any future CEL retrofit in this feature must do the same** — a CEL rule with no envtest assertion is untested, because the fake client used by unit and vcsim tests does not enforce CRD schemas at all.

### 5.2 Go webhooks that remain

| Webhook | Why Go is required |
|---------|--------------------|
| `webhooks/virtualmachineguestoptions/validation` (PR #1779) | `metadata.name == dnsSafe(spec.id)` — a transform, not an equality, so not expressible in CEL. Empty `spec.id` defers to the CRD's CEL rule. |
| `webhooks/virtualmachineconfigpolicy/validation` (PR #1783) | `spec.zone` must reference an **existing** `Zone` — a live cluster read. Carve-out: the VM Operator service account is allowed through even when the `Zone` does not exist, so the Zone controller's own fan-out cannot deadlock against its own validator. |
| `webhooks/virtualmachine/validation` (S9, pending) | Policy evaluation and `ConfigTarget.status` reads. |

### 5.3 VM admission rule tree (S9 / vmop-3746 — design, not yet implemented)

```
VirtualMachine CREATE / UPDATE / power-on transition
│
├─ Features.VirtualMachineConfigPolicy == false ──────────────▶ ALLOW (short-circuit; no reads)
│
├─ Get VirtualMachineConfigPolicy in vm.Namespace
│    └─ not found ────────────────────────────────────────────▶ ALLOW (namespace not governed)
│
├─ Mode gate
│    ├─ CREATE  and spec.createMode  == Deny ─────────────────▶ DENY "policy denies VM create"
│    ├─ UPDATE  and spec.updateMode  == Deny ─────────────────▶ DENY "policy denies VM update"
│    └─ POWERON and spec.powerOnMode == Deny ─────────────────▶ DENY "policy denies VM power-on"
│
├─ vmClassMode gate
│    ├─ AsPolicy (default) and config is VM-Class-derived ────▶ SKIP remaining checks (pre-9.1 behavior)
│    └─ AsConfig ─────────────────────────────────────────────▶ evaluate class-derived config as direct config
│
├─ ExtraConfig gate  (Denied evaluated first; Denied wins)
│    ├─ key matches any denied{Fixed|Regex|Glob} ─────────────▶ DENY, citing the matching entry
│    ├─ allowed non-empty and key matches none ───────────────▶ DENY, citing the allow-list
│    └─ otherwise ────────────────────────────────────────────▶ continue
│
└─ Hardware-version gate
     ├─ clusterMoID := zoneToClusterMoID(vm.Namespace, vm.Spec.Zone)
     ├─ Get ConfigTarget{Name: clusterMoID}      (single informer-cached Get)
     ├─ not found / not Ready ───────────────────────────────▶ DENY 500-style errored (fail closed)
     └─ effectiveHardwareVersion(vm) > status.maxHardwareVersion
                                     ─────────────────────────▶ DENY, citing the cluster maximum
```

Match semantics for the ExtraConfig gate: `Fixed` → exact string equality; `Regex` → `regexp`; `Glob` → `filepath.Match`. `Denied` is evaluated before `Allowed` and takes precedence.

No `HostSystem` list and no label selector appear anywhere in this tree — that was the point of `research.md` Finding 7.

---

## 6. Error-handling contracts

Which failures requeue, which set a condition, and which do both. Uses the `pkgerr` vocabulary from `.sdd/memory/operator-best-practices.md`.

| Situation | Controller behavior | Condition | Requeue |
|-----------|--------------------|-----------|---------|
| Cluster MoID does not resolve | Stop; no fan-out, no GC | `Ready=False`, `ClusterNotFound` | Yes (watch/resync) |
| `QueryConfigTarget` transient error | Stop before fan-out and GC | `Ready=False`, distinct query-failure reason | Yes, as error (rate-limited) |
| `QueryConfigOptionDescriptor` transient error | Stop before GC — **GC never runs on a partial enumeration** | `Ready=False` | Yes, as error |
| `VirtualMachineConfigOptions` has no owner refs | Do not query vSphere | `Ready=False`, `ConfigTargetNotFound` | `RequeueError{After: …}` |
| `QueryConfigOptionEx` returns no option for the key | Treat as real absence, not an error | `Ready=False`, `NotFound` | No |
| Guest-options pruning fails | Keep the finalizer so cleanup retries | — | Yes, as error |
| Policy's `Zone` missing | No spec write | `Ready=False`, `ZoneNotFound` | Yes |
| Policy's `ConfigTarget` missing or not `Ready` | No spec write — never merge zero-valued status | `Ready=False`, `ConfigTargetNotReady` | Yes; also re-enqueued when the target becomes `Ready` |
| Admission: `ConfigTarget` unreadable | — | — | Fail closed: `admission.Errored(500)` |

**Invariant worth restating because three separate scenarios depend on it**: a transient vSphere error must never delete a Kubernetes object. GC is the last step of a reconcile and runs only after both cluster-scope queries have succeeded.

---

## 7. RBAC and capability gating

### 7.1 Capability

```go
// pkg/config/capabilities/capabilities.go
CapabilityKeyVirtualMachineConfigPolicy = "supports_vm_service_vm_config_policy"
```

The Supervisor `Capabilities` CR maps this key onto `pkgcfg.Features.VirtualMachineConfigPolicy`. Gate points, in evaluation order:

1. **Scheme registration** — `pkg/manager/manager.go` registers the `vim.vmware.com` scheme only when the flag is set. A consequence worth knowing: with the flag off, the manager's typed client cannot even (de)serialise these kinds, while the CRDs themselves are still installed. This bit an envtest suite already (PR #1785) and will bite again.
2. **Controller registration** — `controllers/controllers.go:96` gates the whole block.
3. **Zone fan-out** — `zone_controller.go:181` re-checks, so a Zone reconcile in a mixed state creates nothing.
4. **Webhook short-circuit** — the first branch of § 5.3. **Not yet implemented** (`tasks.md` T012).
5. **Demand gate** — vmop-3983. See § 7.3.

### 7.3 Demand gate — run the pipeline only when something consumes it (vmop-3983)

The capability gate above is Supervisor-wide and binary. Once it is on, the pipeline runs on **every** cluster in the Supervisor whether or not anyone uses the result: three EnvironmentBrowser RPCs per cluster per reconcile, one `VirtualMachineConfigOptions` per hardware version (~15), and one `VirtualMachineGuestOptions` per guest OS (~150+, fanned in across versions). On a Supervisor where no tenant has adopted the policy, that is pure cost — vCenter load and etcd objects for data nobody reads.

vmop-3983 adds a second, demand-driven gate: **do the EnvironmentBrowser work only if at least one `VirtualMachineConfigPolicy` actually needs it.**

**The enum, checked against the API.** `spec.syncMode` has exactly two values — `ConfigTarget` and `Disabled` — with `+kubebuilder:default=ConfigTarget`. There is no `Policy` or `Config` value; those belong to a different field. `spec.vmClassMode` is `AsConfig | AsPolicy`, defaulting to **`AsPolicy`**, which means VM-Class-derived configuration **bypasses** the policy — i.e. today the class wins by default, preserving pre-9.1 behavior. Both facts matter for the predicate below.

**Two distinct consumers, and they do not need the same things:**

| Consumer | Needs | Triggered by |
|----------|-------|--------------|
| Policy spec sync (§ 4.4) | `ConfigTarget.status` capacity/security/device fields | `syncMode == ConfigTarget` |
| VM admission hardware-version gate (§ 5.3) | `ConfigTarget.status.maxHardwareVersion` only | **any** policy that governs a namespace, regardless of `syncMode` |

So `syncMode == ConfigTarget` alone is **not** a sufficient predicate — a policy with `syncMode=Disabled` and `createMode=Deny` still needs a populated `ConfigTarget` for the hardware-version check. Neither is "any policy exists" cheap enough, for the reason below.

> **Blocking design problem — the default makes this gate inert.** The Zone controller (§ 4.1) auto-creates one `VirtualMachineConfigPolicy` per Zone, and the CRD defaults `syncMode` to `ConfigTarget`. On any Supervisor with the capability enabled, a qualifying policy therefore exists within one Zone reconcile of startup, and **the gate never closes.** vmop-3983 cannot deliver its intent without also changing one of:
>
> 1. **Zone controller defaults new policies to `syncMode=Disabled`**, and a tenant admin opts in by switching to `ConfigTarget`. Cheapest, and it makes the demand signal honest — but it is a behavior change to a merged story (vmop-3740) and inverts the "sensible default" the CRD currently ships.
> 2. **Zone controller stops auto-creating policies**; a tenant admin creates one. Cleanest demand signal, largest behavior change, and it removes the discoverability the auto-created object provides.
> 3. **Gate on an explicit opt-in** (namespace label or a policy field) rather than on `syncMode`. Most flexible, adds surface area.
>
> **This choice must be made before implementation.** Options 1 and 2 also invalidate two currently-passing tests — the zone unit spec *"creates one VirtualMachineConfigPolicy per zone"* and the E2E *"Should have a VirtualMachineConfigPolicy for each zone in the namespace"*.

**Design, once the opt-in question above is settled.** The gate belongs in the `ConfigTarget` controller's reconcile, not in the Zone controller: `ConfigTarget` objects are cheap to create and are the addressable unit the policy controller looks up, so keep creating them and skip only the *expensive* part.

```
ConfigTarget reconcile:
  if !demandExists(ctx):
      set Ready=False, reason=NoPolicyDemand      // explicit, not silent
      leave status.* untouched; do NOT run GC     // absence of demand is not absence of inventory
      return                                       // no EnvironmentBrowser RPCs
  ... existing cluster-scope path ...
```

- `demandExists` is a `List` over `VirtualMachineConfigPolicy` across all namespaces, served from the informer cache — no API round trip. Add a field index on the opt-in predicate so it is a filtered list, per `.sdd/memory/operator-best-practices.md`.
- **Re-enqueue on demand appearing.** Watch `VirtualMachineConfigPolicy` from the `ConfigTarget` controller and map create/update events to all `ConfigTarget`s, so the first opt-in converges without waiting for a resync. Without this the feature appears broken for up to one resync period after a tenant enables it.
- **`NoPolicyDemand` must be a distinct condition reason.** `Ready=False` with a generic reason would be indistinguishable from a vSphere outage in support bundles.
- **Do not run GC in the gated-off path.** GC is predicated on a successful enumeration; skipping the enumeration must not be read as "the cluster reports nothing."
- **Decide what happens to objects already created** when the last policy opts out: leave them (stale but harmless, consistent with the capability-off behavior in § 9) or collect them. Leaving them is the consistent choice; say so explicitly.

### 7.2 ClusterRole

| Resource | Verbs | Owner |
|----------|-------|-------|
| `zones` | get, list, watch | `controllers/infra/zone` |
| `configtargets` (+`/status`) | create, get, list, patch, update, watch | `controllers/infra/zone`, `controllers/configtarget` |
| `virtualmachineconfigoptions` (+`/status`) | create, get, list, patch, update, watch, delete | `controllers/configtarget`, `controllers/virtualmachineconfigoptions` |
| `virtualmachineguestoptions` (+`/status`) | create, get, list, patch, update, watch, delete | `controllers/virtualmachineconfigoptions` |
| `virtualmachineconfigpolicies` (+`/status`) | create, get, list, patch, update, watch | `controllers/infra/zone`, `controllers/virtualmachineconfigpolicy` |

RBAC is generated from `+kubebuilder:rbac` markers on the reconcilers. It is owned by the **controllers**, not the webhooks — verified when PR #1785 deleted two webhook packages and `config/rbac/role.yaml` regenerated with no diff.

---

## 8. vSphere API mapping and per-host iteration

| Step | vSphere call | MoR | Caller | Notes |
|------|--------------|-----|--------|-------|
| Cluster capacity + security flags + 19 device categories | `EnvironmentBrowser.QueryConfigTarget` | `ClusterComputeResource` | `ConfigTarget` controller | **Does not return SR-IOV at cluster scope** (`research.md` Finding 1) |
| Live hardware-version key set + `maxHardwareVersion` | `EnvironmentBrowser.QueryConfigOptionDescriptor` | `ClusterComputeResource` | `ConfigTarget` controller | `CreateSupported` is already the cluster-wide max |
| Per-hardware-version config option | `EnvironmentBrowser.QueryConfigOptionEx` | `ClusterComputeResource` | `VirtualMachineConfigOptions` controller | One call per key — this is why the descriptor/Ex split exists |

Every one of these paths must wrap its context with `pkgctx.WithVCOpID` inside the provider method, per `.sdd/memory/operator-best-practices.md`.

**There is no per-host vSphere call in this release.** Three cluster-scope calls are the entire vCenter surface of this feature. The per-host `PropertyCollector` path — the only thing that can supply SR-IOV, since cluster-scope `QueryConfigTarget` does not return it — is deferred to spec [`003-configtarget-sriov-per-host`](../003-configtarget-sriov-per-host/).

### 8.1 Per-host iteration — designed, deferred

The design is recorded in [`plan.md`](plan.md) I8, which is where it was worked out: host enumeration via `ClusterComputeResource.host`, one multi-property `PropertyCollector` RPC per host, per-(host, NIC) entries keyed by `hostMoID`, wholesale rewrite of `status.sriov` on each pass (hence no garbage collection), and warning-event-plus-requeue on a per-host failure without blocking the iteration.

**The plan to build it** is spec [`003-configtarget-sriov-per-host`](../003-configtarget-sriov-per-host/), which carries that design forward and resolves four things `plan.md` I8 left open: API additive-safety against a shipped 9.2 `ConfigTarget`, `Ready` semantics on total per-host failure, whether vcsim can model the host properties at all, and how a disconnected host is treated.

**Nothing in this release iterates hosts.** The `ConfigTarget` reconcile makes exactly two cluster-scope vCenter calls per pass.

---

## 9. Rollback / disable plan

**Disable**: set `supports_vm_service_vm_config_policy` to `false` in the Supervisor capabilities CR.

| Component | Behavior when the capability flips off |
|-----------|----------------------------------------|
| Scheme | `vim.vmware.com` kinds unregistered on the next manager start |
| Controllers | Not registered; no reconciles; no vCenter EB queries |
| Zone fan-out | Skipped; no new `ConfigTarget` / `VirtualMachineConfigPolicy` |
| CRDs | Remain installed; existing objects are preserved untouched and remain listable |
| VM admission | Short-circuits to ALLOW before any policy or `ConfigTarget` read (pending T012) |
| CEL rules | Still enforced — they live in the CRD schema, not the operator. A `ConfigTarget` write while disabled is still validated. This is a **behavior difference from the pre-CEL design** and is intentional. |

**Re-enable**: reconciliation resumes from current state. Because every reconciler is level-triggered and idempotent, no reconstruction step is needed — the first Zone reconcile re-creates anything missing and the `ConfigTarget` reconcile re-derives status from live vSphere data. Stale objects that accumulated while disabled are corrected by the normal GC step on the first successful reconcile.

**Not covered by disable**: objects created while enabled are *not* deleted on disable. That is deliberate (a flip-flop must not destroy tenant policy edits), but it means a long-disabled Supervisor can serve stale `ConfigTarget.status` to anything that reads it directly. The webhook short-circuit is what makes that safe, which is why T012 is not optional.

---

## 10. Observability plan

| Signal | Where | Notes |
|--------|-------|-------|
| `Ready` condition | All four CRDs | Reasons: `ClusterNotFound`, `QueryFailed`, `NotFound`, `ConfigTargetNotFound`, `ConfigTargetNotReady`, `ZoneNotFound`, `SyncDisabled` |
| `status.observedGeneration` | All four CRDs | Constitutional requirement |
| Warning event | `VirtualMachineConfigOptions` | Guest-options pruning failure |
| VC operation ID | Every EB / PropertyCollector call | `pkgctx.WithVCOpID(ctx, obj, "queryConfigTarget" \| "queryConfigOptionDescriptor" \| "queryConfigOptionEx" \| "hostSriovProperties")` |
| Admission denial message | VM validating webhook | Must name the specific policy field or the cluster's `maxHardwareVersion` — this is an acceptance criterion (US5), not a nicety |

No new Prometheus metrics are proposed. Controller-runtime's per-controller reconcile counters and error rates cover the failure modes above; if the per-host iteration turns out to be a latency source in large clusters, a histogram on the fan-out is the first thing to add.

---

## 11. Test design — how to read the matrix

The matrix in § 12 is keyed off the **26 numbered acceptance scenarios in `spec.md`** plus its edge cases, not off the test files. That direction matters: enumerating from the test files would produce an inventory of what is covered and silently omit every gap.

### 11.1 Test levels

| Level | Mechanism | Location |
|-------|-----------|----------|
| `unit` | Ginkgo + fake client, no infra label | beside the source package |
| `envtest` | Real API server, `testlabels.EnvTest` | same `_test.go`, separate `Describe` |
| `vcsim` | Simulated vCenter, `testlabels.VCSim` | same `_test.go`, separate `Describe` |
| `e2e` | Real WCP Supervisor | `test/e2e/vmservice/vmservice/configpolicy/configpolicy.go` |

Per `.sdd/memory/testing-standards.md` there is one `_test.go` per package; levels are distinguished by Ginkgo `Label()`, not by filename. There is no `test/intg/` tree in this repo — several `tasks.md` entries (T070, T092, T106, T116) still name one and should be re-pointed at the in-package suites.

### 11.1.1 Which level owns which kind of case

vmop-3736's acceptance criteria only ask for the integration (vcsim) and E2E plans. This TDS deliberately covers all four levels, because for this feature the level assignment is itself a design decision — three of the § 14.3 blind spots exist precisely because a case was written at a level that cannot observe the behavior. The rubric:

| Level | Owns | Does **not** own |
|-------|------|------------------|
| `unit` | Pure mapping and decision logic: vim → CRD status conversion, `maxHardwareVersion` aggregation, `dnsSafe` transform, multi-cluster intersection, ExtraConfig match semantics, condition/reason selection, error → requeue classification. Failure injection is trivial here, so **every error branch in § 6 belongs at this level.** | Anything depending on API-server semantics. The fake client does **not** enforce CRD schemas, CEL rules, defaults, or the spec/status subresource split. |
| `envtest` | Everything the API server enforces rather than the operator: CEL transition/field/root rules, `+kubebuilder:default=` values, listMap merge-patch behavior, status-subresource separation. | vSphere behavior — there is no vCenter here. |
| `vcsim` | The controller against a real `EnvironmentBrowser`: that the govmomi call shapes are right, that recorded payloads round-trip, multi-object convergence, watch-driven re-enqueue, idempotence across reconciles. | Anything vcsim models with a fixed fixture — most importantly its per-model descriptor set, which is why TC-US4-01r is ENV-BLOCKED. |
| `e2e` | Cluster-observable behavior on a real Supervisor: objects materialise from real hardware, statuses populate from a real vCenter, admission actually rejects, and the capability gate is honoured end to end. Per `.sdd/memory/e2e-sync-with-changes.md` this is mandatory, in the same change set, for anything user-visible. | Exhaustive branch coverage. Each E2E row should be one representative path per user story, not a re-run of the unit matrix. |

Two rules that fall out of this and are worth enforcing in review:

- **A CEL rule with only unit coverage is untested.** The fake client ignores it entirely. This is exactly the trap PR #1785 walked into and then closed by adding a `Describe("CRD validation", …)` envtest block per controller.
- **An E2E assertion that skips when the hardware is absent is not coverage.** It is a presence-parity check that passes vacuously. TC-US1-02de is the live example; treat it as ENV-BLOCKED, not DONE.

The E2E suite is registered as `Context("CONFIG-POLICY")` in `test/e2e/vmservice/vmservice_test.go` and is gated by `skipper.SkipUnlessSupervisorCapabilityEnabled(…, consts.VirtualMachineConfigPolicyCapabilityName)`.

### 11.2 Status vocabulary

| Status | Meaning |
|--------|---------|
| **DONE** | Automated and merged on `origin/main`. Evidence names the file and the spec text. |
| **IN-PR** | Automated; pending review in a named open PR. Not yet protection. |
| **PLANNED** | Not written, but an open Story or Sub-task already tracks it. |
| **GAP** | Automatable and unprotected, with **no** ticket tracking it. Each one gets a proposed sub-task in § 14.4. |
| **ENV-BLOCKED** | A test exists or could exist, but current CI infrastructure cannot exercise it. Requires an environmental reason, not "it's hard." |
| **MANUAL** | Not automatable in any environment we plan to have; needs a written manual procedure. |
| **DEFERRED** | The behavior is not yet decided, so no test may be written. Only used for cases blocked on Spike vmop-3794. |
| **NOT TESTABLE** | A design constraint or an assumption about a third-party system, with no observable behavior in this codebase to assert. |

Five qualified variants appear in the matrix. They are deliberate distinctions, not sloppiness, and each has a mechanical rollup rule so § 14.1 can be recomputed by anyone:

| Qualified tag | Meaning | Counts in the rollup as |
|---------------|---------|-------------------------|
| **DONE → IN-PR** | Protected on `main` today, but an open PR relocates the evidence. Re-point the row when that PR merges; do not treat it as regressed in the meantime. | DONE |
| **DONE (synthetic)** | Automated and merged, but the precondition is injected rather than produced naturally. The real path is a separate ENV-BLOCKED row. | DONE |
| **IN-PR (draft)** | In an open PR that is still marked draft. Weaker than IN-PR — the design may still change. | IN-PR |
| **ENV-BLOCKED (partial)** | The spec runs and passes, but part of what it claims to assert is skipped on the current testbed. **Not** coverage for the skipped part. | ENV-BLOCKED |

**Counting convention for § 14.1**: exactly one entry per row, taken from the row's Status cell in §§ 12.1–12.8 only. § 14.4 and § 14.5 re-list rows already counted there and are not added again. Bucket entries therefore equal row count.

**ENV-BLOCKED is the tag most likely to be abused.** Only two environmental reasons are accepted in this feature, both grounded:

1. **vcsim's `EnvironmentBrowser` returns a fixed descriptor set per model**, so "drop a hardware version from a live cluster" cannot be driven by shrinking what the cluster reports. The E2E test therefore injects the staleness directly (creates a synthetic-version `VirtualMachineConfigOptions` owned by a real `ConfigTarget` and asserts it is collected). The real-shrink path is untestable, not merely untested.
2. **Device categories the testbed's hardware does not expose** — SR-IOV NICs, vGPU, SGX — cannot be asserted. The E2E device-category spec handles this by asserting *presence parity* against a live `QueryConfigTarget`: a category the cluster does not report is skipped rather than asserted empty. That keeps the test green on ordinary infra but means those categories are **effectively inert** on the current testbed. vmop-3965 tracks either fixing mock vGPU activation or running this coverage on a real-hardware testbed.

Anything else tagged ENV-BLOCKED should be challenged in review.

---

## 12. Test case matrix

### 12.1 US1 — CSP admin: hardware discovery is automatic

| ID | Scenario | Level | Status | Evidence | Ticket |
|----|----------|-------|--------|----------|--------|
| TC-US1-01 | Zone reconcile creates a `ConfigTarget` named for the derived cluster MoID | vcsim | **DONE** | `controllers/infra/zone/zone_controller_test.go` — *"creates one ConfigTarget per cluster"* | vmop-3749 / vmop-3750 |
| TC-US1-01e | Same, on a real Supervisor | e2e | **DONE** | `configpolicy.go` — *"Should have at least one ConfigTarget derived from the zone's pool MoIDs"* | vmop-3751 |
| TC-US1-01i | Fan-out is idempotent — `ConfigTarget` UID stable across reconciles | vcsim | **DONE** | `zone_controller_test.go` — *"is idempotent: ConfigTarget UID is stable across reconciles"* | vmop-3750 |
| TC-US1-02 | `status.numCPUs`, `maxCPUsPerVM`, security flags populated from `QueryConfigTarget` | unit | **DONE** | `configtarget_controller_test.go` — *"populates status, sets Ready=True, and fans out VirtualMachineConfigOptions"* | vmop-3758 |
| TC-US1-02v | Same, against vcsim's real EnvironmentBrowser | vcsim | **DONE** | `configtarget_controller_test.go` — *"populates status and creates VirtualMachineConfigOptions from the real EnvironmentBrowser result"* | vmop-3760 |
| TC-US1-02e | Same, on a real Supervisor | e2e | **DONE** | `configpolicy.go` — *"Should populate ConfigTarget.status from QueryConfigTarget and mark Ready=True"* | vmop-3761 |
| TC-US1-02d | All 19 non-SR-IOV device categories mapped; SR-IOV excluded | unit + vcsim | **DONE** | `configtarget_controller_test.go` — *"maps every non-SR-IOV device category and excludes SR-IOV entirely"*, *"maps device inventory from a QueryConfigTarget result fed through the real EnvironmentBrowser"* | vmop-3758 |
| TC-US1-02de | Device categories present in the cluster's live inventory are propagated | e2e | **ENV-BLOCKED (partial)** | `configpolicy.go` — *"Should populate status.maxHardwareVersion and non-SR-IOV device categories…"*. Presence-parity design means vGPU/SGX/SR-IOV rows are skipped on hardware that lacks them | vmop-3965 |
| TC-US1-03 | N supported hardware versions → N `VirtualMachineConfigOptions` | unit + vcsim | **DONE** | as TC-US1-02 / TC-US1-02v | vmop-3758 |
| TC-US1-03e | Same, on a real Supervisor; `spec.hardwareVersion == metadata.name` for each | e2e | **DONE** | `configpolicy.go` — *"Should fan out a VirtualMachineConfigOptions object per supported hardware version"* | vmop-3761 |
| TC-US1-04 | `status.maxHardwareVersion` is the max creatable version; only `CreateSupported` descriptors count | unit | **DONE** | `configtarget_controller_test.go` — *"ignores descriptors where CreateSupported is false"* | vmop-3797 |
| TC-US1-04a | No descriptors → `maxHardwareVersion` empty | unit | **DONE** | *"leaves MaxHardwareVersion empty"* | vmop-3797 |
| TC-US1-04b | Malformed descriptor key skipped, not fatal | unit | **DONE** | *"skips the malformed key without erroring"* | vmop-3797 |
| TC-US1-04e | `maxHardwareVersion` populated and parseable on a real cluster | e2e | **DONE** | `configpolicy.go` — *"Should populate status.maxHardwareVersion and non-SR-IOV device categories…"* | vmop-3761 |
| TC-US1-04i | A second reconcile against unchanged vSphere state changes nothing | unit | **DONE** | *"is idempotent on a second reconcile with unchanged vSphere state"* | vmop-3758 |

### 12.2 US2 — cluster capabilities queryable from a single object

| ID | Scenario | Level | Status | Evidence | Ticket |
|----|----------|-------|--------|----------|--------|
| TC-US2-01 | Mixed host versions → `maxHardwareVersion` is the largest | unit + vcsim + e2e | **DONE** | Unit: TC-US1-04. e2e: TC-US1-04e. vcsim: `configtarget_controller_test.go` — *"populates status and creates VirtualMachineConfigOptions…"* asserts `maxHardwareVersion` is valid and equals the highest fanned-out key against vcsim's 2-host cluster. **Caveat**: vcsim's two hosts share an ESXi build, so "hosts whose defaults *differ*" is exercised only by the unit tests over synthetic descriptor sets, not by any live-inventory test. See the § 3 design note on why this is descriptor-derived | vmop-3797 |
| TC-US2-02 | Cluster's supported-version set **changes** → `maxHardwareVersion` is recomputed on the next reconcile | unit | **GAP** | Only the *unchanged* case (TC-US1-04i) is asserted. A changed-descriptor-set recompute has no test | **GAP-1** |
| TC-US2-03b | Host removal that lowers the cluster max → `maxHardwareVersion` decreases | unit | **GAP** | Same missing recompute assertion as TC-US2-02, in the shrinking direction | **GAP-1** |
| TC-US2-03s | Host removal changes `status.sriov` | — | **DEFERRED** | Moved to spec [`003`](../003-configtarget-sriov-per-host/) US3.1 — `status.sriov` is always empty in this release | vmop-3926 |
| TC-US2-03e | Host add/remove on a live Supervisor mid-reconcile | e2e | **MANUAL** | Requires evacuating and removing an ESX host from a live cluster; not scriptable in the E2E harness | **GAP-M1** |

### 12.3 US3 — tenant admin: per-namespace hardware policy

| ID | Scenario | Level | Status | Evidence | Ticket |
|----|----------|-------|--------|----------|--------|
| TC-US3-01 | Zone reconcile creates one policy per zone with `syncMode=ConfigTarget` by default | vcsim | **DONE** | `zone_controller_test.go` — *"creates one VirtualMachineConfigPolicy per zone"* | vmop-3749 |
| TC-US3-01e | Same, on a real Supervisor | e2e | **DONE** | `configpolicy.go` — *"Should have a VirtualMachineConfigPolicy for each zone in the namespace"* | vmop-3751 |
| TC-US3-01d | Defaults for `syncMode` / `createMode` / `updateMode` / `powerOnMode` / `vmClassMode` applied by the CRD schema | envtest | **GAP** | Defaults come from `+kubebuilder:default=`, not a webhook (see § 5.1). No test asserts the defaulted values against a real API server | **GAP-2** |
| TC-US3-02 | `syncMode=ConfigTarget` → policy `spec` mirrors `ConfigTarget.status` | unit | **IN-PR** | #1784 — *"copies its capacity limits into spec and sets Ready=True"* | vmop-3770 |
| TC-US3-02v | Same, against a real populated `ConfigTarget` | vcsim | **IN-PR** | #1784 — *"populates spec from the real cluster's capabilities and sets Ready=True"* | vmop-3771 |
| TC-US3-02e | Same, on a real Supervisor | e2e | **IN-PR** | #1784 — *"Should populate policy spec from the zone's ConfigTarget capabilities"* | vmop-3772 |
| TC-US3-02i | Unchanged `ConfigTarget` → no `resourceVersion` bump | unit + vcsim | **IN-PR** | #1784 — *"does not bump resourceVersion on a second reconcile…"* (both levels) | vmop-3770 |
| TC-US3-03 | `syncMode=Disabled` → `Ready=True` / `SyncDisabled`, `spec` untouched | unit | **IN-PR** | #1784 — *"sets Ready=True with reason SyncDisabled and does not modify spec"* | vmop-3770 |
| TC-US3-03e | Switching to `Disabled` stops the sync, on a real Supervisor | e2e | **IN-PR** | #1784 — *"Should stop syncing once spec.syncMode is set to Disabled"* | vmop-3772 |
| TC-US3-03t | Toggling `ConfigTarget` ↔ `Disabled` converges in both directions | unit | **IN-PR** | #1784 — *"converges correctly in both directions"* | vmop-3770 |
| TC-US3-04 | VM with an ExtraConfig key matching a `Denied` entry is rejected | unit + e2e | **PLANNED** | — | vmop-3774 / vmop-3777 |

### 12.4 US4 — stale objects are garbage-collected

| ID | Scenario | Level | Status | Evidence | Ticket |
|----|----------|-------|--------|----------|--------|
| TC-US4-01 | Hardware version dropped from the descriptor list → its `VirtualMachineConfigOptions` is deleted | unit | **DONE** | `configtarget_controller_test.go` — *"garbage-collects the corresponding VirtualMachineConfigOptions"* | vmop-3758 |
| TC-US4-01c | Co-owned by another `ConfigTarget` → only this owner's ref is removed, object survives | unit | **DONE** | *"only removes its own owner reference and leaves the object when another owner remains"* | vmop-3758 |
| TC-US4-01e | Stale `VirtualMachineConfigOptions` collected on a real Supervisor | e2e | **DONE (synthetic)** | `configpolicy.go` — *"Should garbage-collect a stale VirtualMachineConfigOptions no longer reported by the cluster"*. Staleness is injected, not produced by shrinking the cluster — see § 11.2 reason 1 | vmop-3761 |
| TC-US4-01r | The real "cluster stops reporting a version" path | e2e | **ENV-BLOCKED** | vcsim reports a fixed descriptor set per model; a real cluster's supported set cannot be shrunk on demand | § 11.2 reason 1 |
| TC-US4-02 | Transient `QueryConfigTarget` error → no GC, retry signalled | unit | **DONE** | *"marks Ready=False with a distinct reason, runs no GC, and signals a retry"* | vmop-3758 |
| TC-US4-02d | Transient `QueryConfigOptionDescriptor` error (as distinct from `QueryConfigTarget`) → no GC | unit | **GAP** | The existing test covers one of the two gating queries; the contract in § 6 names both | **GAP-3** |
| TC-US4-03 | Evicted host that lowers the cluster max → `maxHardwareVersion` reduced | unit | **GAP** | The SR-IOV half of this scenario moved to spec 003; the `maxHardwareVersion` reduction half is unprotected here | **GAP-1** |
| TC-US4-04 | Guest OS dropped from a hardware version → that version's entry removed from `VirtualMachineGuestOptions.status.hardwareVersions` | unit | **IN-PR** | #1782 — *"removes only the dropped hardware version's status entry and keeps the object"* | vmop-3932 |
| TC-US4-04d | Last hardware-version entry removed → the `VirtualMachineGuestOptions` is deleted | unit | **IN-PR** | #1782 — *"garbage-collects the corresponding VirtualMachineGuestOptions"* (two variants) | vmop-3932 |
| TC-US4-04f | Pruning failure keeps the finalizer so cleanup retries | unit | **IN-PR** | #1782 — *"keeps the finalizer when garbage collection fails, so cleanup is retried"* | vmop-3932 |
| TC-US4-04p | A merge patch against a real API server removes exactly the targeted listMap entry | envtest | **IN-PR** | #1782 — *"removes exactly the targeted entry, leaving sibling entries intact"* | vmop-3932 |
| TC-US4-04o | Each `VirtualMachineGuestOptions` carries owner refs to its contributing `VirtualMachineConfigOptions` | unit | **IN-PR (draft)** | #1789 — *"should add an owner reference to the contributing VirtualMachineConfigOptions"* | vmop-3932 |
| TC-US4-04e | Guest-options pruning verified on a real Supervisor | e2e | **GAP** | Unit and envtest only; no E2E row exists for the pruning path | **GAP-4** |

### 12.5 US5 — VM admission reflects policy and host capabilities

Story S9 / vmop-3746 is **Untriaged** and unassigned; sub-tasks vmop-3773..vmop-3777 are all Untriaged with no code on `main`. Every row below is **PLANNED**. This is the single largest block of unwritten coverage in the epic.

| ID | Scenario | Level | Status | Ticket |
|----|----------|-------|--------|--------|
| TC-US5-01 | `createMode = Deny` → VM create rejected | unit | **PLANNED** | vmop-3773 / T111 |
| TC-US5-01u | `updateMode = Deny` → VM update rejected | unit | **PLANNED** | vmop-3773 |
| TC-US5-01p | `powerOnMode = Deny` → power-on transition rejected | unit | **PLANNED** | vmop-3773 |
| TC-US5-02 | `Glob` denied entry `guestinfo.*` rejects `extraConfig["guestinfo.foo"]` | unit | **PLANNED** | vmop-3774 / T113 |
| TC-US5-02f | `Fixed` (exact) match semantics | unit | **PLANNED** | vmop-3774 |
| TC-US5-02r | `Regex` match semantics | unit | **PLANNED** | vmop-3774 |
| TC-US5-02p | `Denied` takes precedence over `Allowed` when both match | unit | **PLANNED** | vmop-3774 |
| TC-US5-03 | Non-empty `allowed` and no match → rejected | unit | **PLANNED** | vmop-3774 |
| TC-US5-04 | `vmClassMode = AsPolicy` (default) → VM-Class config bypasses the policy | unit | **PLANNED** | vmop-3773 |
| TC-US5-05 | `vmClassMode = AsConfig` → policy applies to class-derived config identically | unit | **PLANNED** | vmop-3773 |
| TC-US5-06 | Requested hardware version > `ConfigTarget.status.maxHardwareVersion` → rejected, message cites the maximum | unit | **PLANNED** | vmop-3775 / T115 |
| TC-US5-06z | `Zone → cluster MoID → ConfigTarget` resolution path | unit | **PLANNED** | vmop-3775 |
| TC-US5-06n | `ConfigTarget` missing or not `Ready` at admission → fail closed | unit | **GAP** | Not in `spec.md`, not in any sub-task; the fail-closed rule in § 5.3 needs its own row | **GAP-5** |
| TC-US5-0x | vcsim integration: mode-deny, extraConfig-deny, HW-version-deny, plus a happy path for each | vcsim | **PLANNED** | vmop-3776 / T116 |
| TC-US5-0xe | E2E on a real Supervisor: each denial with an assertion on the reason string; happy path accepted | e2e | **PLANNED** | vmop-3777 / T117 |

> **Ticket hygiene**: vmop-3775 is still titled *"check requested HW version against HostSystem labels."* `HostSystem` was dropped (`research.md` Finding 7); the check reads `ConfigTarget.status.maxHardwareVersion`. Retitle before work starts, or the implementer will build the retired design.

### 12.6 US6 — the pipeline is opt-in per Supervisor

| ID | Scenario | Level | Status | Evidence | Ticket |
|----|----------|-------|--------|----------|--------|
| TC-US6-00 | The capability key maps onto `Features.VirtualMachineConfigPolicy` in both directions | unit | **DONE** | `pkg/config/capabilities/capabilities_test.go` — several `Specify(CapabilityKeyVirtualMachineConfigPolicy, …)` blocks covering activated, deactivated, and diff output | vmop-3747 |
| TC-US6-01 | Capability `false` → Zone reconcile creates no `ConfigTarget` and no `VirtualMachineConfigPolicy` | unit | **GAP** | `zone_controller_test.go` sets `Features.VirtualMachineConfigPolicy = true` for the fan-out block; the flag-off branch at `zone_controller.go:181` has no test | **GAP-6** |
| TC-US6-01c | Capability `false` → the three controllers are not registered | unit | **GAP** | `controllers/controllers.go:96` gate is untested | **GAP-6** |
| TC-US6-02 | Capability `false` → VM admission skips all policy and `ConfigTarget` checks | unit | **PLANNED** | Depends on T012, which is unimplemented | vmop-3747 / T012 |
| TC-US6-03 | Capability re-enabled → pipeline restores the full object set | vcsim | **GAP** | Idempotent, level-triggered reconcile makes this cheap to assert; nothing does | **GAP-7** |
| TC-US6-03e | Capability flipped off then on against a live Supervisor | e2e | **MANUAL** | Requires editing the WCP capabilities CR and waiting out an operator rollout; the E2E suite is *skipped entirely* when the capability is off, so it structurally cannot cover this | **GAP-M2** |
| TC-US6-04 | CEL rules still reject invalid writes while the capability is off | envtest | **GAP** | New behavior introduced by the CEL retrofit (§ 9); nothing asserts it | **GAP-8** |

### 12.7 Edge cases from `spec.md`

| ID | Scenario | Level | Status | Evidence | Ticket |
|----|----------|-------|--------|----------|--------|
| TC-EC-01 | `ConfigTarget.spec.id` immutable after create | unit + envtest | **DONE → IN-PR** | On `main`: `configtarget_validator_unit_test.go` / `_intg_test.go`. PR #1785 deletes those and re-adds equivalent CEL coverage in `controllers/configtarget`'s envtest suite | vmop-3757 → vmop-3766 |
| TC-EC-01n | `ConfigTarget` `metadata.name` must match `^domain-c[0-9]+$` | unit + envtest | **DONE → IN-PR** | same migration | vmop-3766 |
| TC-EC-02 | Cluster MoID with no EB result → `Ready=False`, `ClusterNotFound`, no fan-out | unit + vcsim | **DONE** | `configtarget_controller_test.go` — *"marks Ready=False with a ClusterNotFound-style reason and does not create VirtualMachineConfigOptions"*; vcsim: *"marks Ready=False and does not panic"* | vmop-3758 / vmop-3760 |
| TC-EC-03 | Policy whose `spec.zone` names a non-existent Zone surfaces a condition and does not block other policies | unit | **IN-PR** | #1784 — *"sets Ready=False with reason ZoneNotFound"*. Webhook-level rejection: #1783 — *"should deny when spec.zone references a non-existent Zone"* | vmop-3769 / vmop-3770 |
| TC-EC-03s | The VM Operator service account may create a policy whose Zone does not yet exist | unit | **IN-PR** | #1783 — *"should allow the VM Operator service account even when spec.zone references a non-existent Zone"* | vmop-3769 |
| TC-EC-03i | Non-blocking: one invalid policy does not stall reconciliation of a sibling policy in the same namespace | unit | **GAP** | The condition is asserted; the isolation claim in `spec.md` is not | **GAP-9** |
| TC-EC-04 | Enabling a hardware-version-gated feature on a VM whose placement has not converged | — | **DEFERRED** | Rule not yet decided — open Spike vmop-3794. No test may be written until the spike lands | vmop-3794 |
| TC-EC-05 | `PlaceVmsXCluster` is not assumed to honour auto-NUMA / auto-placed-SR-IOV at create time | — | **NOT TESTABLE** | A design constraint on where enforcement lives, not an observable behavior. Its consequence is TC-US5-06 (enforce from Kubernetes objects alone) | vmop-3794 |

### 12.8 Implementation-level behavior not enumerated in `spec.md`

These are real, user-visible contracts that the code already makes. They are listed so the matrix is a complete inventory rather than only a spec echo. Rows tagged **spec-drift** should be folded back into `spec.md`.

| ID | Behavior | Level | Status | Evidence | Note |
|----|----------|-------|--------|----------|------|
| TC-EX-01 | `VirtualMachineConfigOptions` `metadata.name` must equal `spec.hardwareVersion`, format `^vmx-\d+$`, immutable | unit + envtest | **DONE → IN-PR** | `virtualmachineconfigoptions_validator_*_test.go` on `main`; migrating to CEL + envtest in #1785 | |
| TC-EX-02 | `VirtualMachineGuestOptions` `metadata.name == dnsSafe(spec.id)`; `spec.id` immutable and non-empty | unit + envtest | **IN-PR** | #1779 validator unit + intg specs | |
| TC-EX-03 | `dnsSafe` transform: case-folding, punctuation, leading/trailing separators, 63-char truncation, truncation landing on a hyphen | unit | **IN-PR** | #1779 `pkg/util/vimguestoptions_test.go` table entries | |
| TC-EX-03m | Guest OS identifier sanitisation applied by the controller matches the validator's transform | unit | **DONE** | `vmconfigoptions_controller_test.go` — *"should replace non-alphanumeric-hyphen characters and truncate to 63 characters"* | spec-drift |
| TC-EX-04 | Two hardware versions reporting the same guest OS fan in to one `VirtualMachineGuestOptions` with two `status.hardwareVersions` entries | unit | **DONE** | *"should accumulate a distinct status.hardwareVersions entry per hardware version"* | spec-drift |
| TC-EX-04v | Same, against vcsim; re-reconcile updates exactly one entry; deleting one `VirtualMachineConfigOptions` does **not** prune the entry | vcsim | **IN-PR** | #1781 — three specs | |
| TC-EX-05 | `VirtualMachineConfigOptions` with no owner refs → requeue + `ConfigTargetNotFound` | unit | **DONE** | *"should requeue with a delay and mark Ready=False with ConfigTargetNotFound"* | spec-drift |
| TC-EX-06 | Owner reference naming a `ConfigTarget` that no longer exists → `Ready=False`, `QueryFailed`, error returned | unit | **DONE** | *"should mark Ready=False with QueryFailed reason and return an error"* | spec-drift |
| TC-EX-07 | Co-owned by multiple `ConfigTarget`s → deterministically query the lexicographically lowest cluster MoID | unit | **DONE** | *"deterministically queries the lexicographically lowest cluster MoID"* | Improvement tracked by vmop-3964 |
| TC-EX-08 | `QueryConfigOptionEx` reports no option for the requested version → `Ready=False`, `NotFound` | unit | **DONE** | *"should mark Ready=False with NotFound reason"* | spec-drift |
| TC-EX-09 | `ReconcileDelete` removes the finalizer | unit | **DONE** | *"should remove the finalizer"* | |
| TC-EX-10 | `SupportLevel` / `Family` mapped from their vSphere wire values | unit | **DONE** | *"should map SupportLevel and Family from their vSphere wire values"* | |
| TC-EX-11 | `status.guestOSDefaultIndex` and the supported transport / monitor lists populated | unit | **DONE** | *"should populate status.guestOSDefaultIndex and the supported transport/monitor lists"* | |
| TC-EX-12 | Multi-cluster policy sync: intersect numeric ranges to min-of-maxima, AND booleans, intersect device categories to the common set, preserve an existing `Min` | unit | **IN-PR** | #1784 `pkg/util/configpolicysync` — five specs | |
| TC-EX-13 | A zero value reported by one cluster is intersected literally, as real data — not treated as "unset" | unit | **IN-PR** | #1784 — *"intersects a zero value on one target literally, as real reported data"* | Subtle; keep this test |
| TC-EX-14 | Multi-cluster zone with one `Ready` and one not-`Ready` `ConfigTarget` → **no** sync | unit | **IN-PR** | #1784 — *"does not sync from the single Ready target -- that would be wider than the true intersection"* | Safety-critical |
| TC-EX-15 | `ConfigTarget` exists but status not yet populated → no merge of zero-valued fields | unit | **IN-PR** | #1784 — *"does not merge its zero-valued fields"* | |
| TC-EX-16 | Policy created before its `ConfigTarget` is `Ready` converges with no manual reconcile | vcsim | **IN-PR** | #1784 — *"converges once the ConfigTarget is marked Ready, with no manual reconcile"* | |
| TC-EX-17 | Non-`ConfigTarget`-derived fields (`extraConfig`, latency, thread models) preserved across a sync | unit | **IN-PR** | #1784 — *"preserves them across a sync"* and *"never sets fields that have no ConfigTarget source"* | |
| TC-EX-18 | `VirtualMachineGuestOptions` fanned out per guest OS on a real Supervisor | e2e | **DONE** | `configpolicy.go` — *"Should fan out a VirtualMachineGuestOptions object for each guest OS reported by the cluster"* | vmop-3768 |
| TC-EX-19 | `status.guestOSIdentifiers` populated on each `VirtualMachineConfigOptions` on a real Supervisor | e2e | **DONE** | `configpolicy.go` — *"Should populate status.guestOSIdentifiers on each VirtualMachineConfigOptions"* | vmop-3765 |
| TC-EX-20 | Removing pool MoIDs from a Zone does **not** delete the `ConfigTarget` | vcsim | **DONE** | `zone_controller_test.go` — *"does not delete a ConfigTarget when pool MoIDs are removed from the zone"* | |
| TC-EX-21 | No qualifying `VirtualMachineConfigPolicy` anywhere → the `ConfigTarget` reconcile makes **zero** EnvironmentBrowser RPCs and fans out nothing | unit | **PLANNED** | § 7.3. Assert on the fake provider's call count, not just on object absence — absence could also mean a silent failure | vmop-3983 |
| TC-EX-21r | Gated-off `ConfigTarget` reports `Ready=False` with a distinct `NoPolicyDemand` reason, not a generic failure | unit | **PLANNED** | § 7.3 — required so a support bundle can tell "nobody asked" from "vCenter is down" | vmop-3983 |
| TC-EX-21g | Gated-off reconcile does **not** run GC | unit | **PLANNED** | § 7.3 — skipping the enumeration must not be read as "the cluster reports nothing" | vmop-3983 |
| TC-EX-21c | First qualifying policy appearing re-enqueues every `ConfigTarget` and the pipeline converges without waiting for a resync | vcsim | **PLANNED** | § 7.3 — without the watch, the feature looks broken for up to one resync period after a tenant opts in | vmop-3983 |
| TC-EX-21o | Last qualifying policy opting out leaves already-created objects intact | unit | **PLANNED** | § 7.3 — consistent with the capability-off behaviour in § 9; assert it rather than leaving it emergent | vmop-3983 |
| TC-EX-21e | On a real Supervisor with no qualifying policy, no `VirtualMachineConfigOptions` / `VirtualMachineGuestOptions` exist; creating one policy materialises them | e2e | **PLANNED** | Depends on which opt-in option § 7.3 picks; options 1 and 2 also require updating TC-US3-01e | vmop-3983 |

### 12.9 Deferred to a future release — per-host SR-IOV

Story S10's design stays in this spec (§ 8.1, [`plan.md`](plan.md) I8). Its six *test cases* move to spec [`003-configtarget-sriov-per-host`](../003-configtarget-sriov-per-host/), because that is where the work will be verified, and they are **not counted** in § 14.1:

| Was | Now | Where |
|-----|-----|-------|
| ~~TC-US1-05~~ — `status.sriov` entries carry `hostMoID` and DVX enrichment | spec 003 US1.1, US1.2 | 003 `tasks.md` T033 |
| ~~TC-US1-05v~~ — same, against vcsim | spec 003 test strategy, **blocked on Q3** (does vcsim model host SR-IOV properties at all?) | 003 `tasks.md` T002, T040 |
| ~~TC-US1-05e~~ — same, on SR-IOV hardware | spec 003, ENV-BLOCKED on vmop-3965 | 003 `tasks.md` T041 |
| ~~TC-US1-06~~ — one host's RPC fails, others still written | spec 003 US2.1 | 003 `tasks.md` T034 |
| ~~TC-US2-03a~~ — host removed → entries disappear | spec 003 US3.1 | 003 `tasks.md` T035 |
| ~~TC-US4-03 (SR-IOV half)~~ — evicted host | spec 003 US3.1; the `maxHardwareVersion` half stays here as GAP-1 | — |

What stays behind in this release is one assertion, and it matters: **TC-US1-02d proves `status.sriov` is always empty** — the controller excludes both `ct.Sriov` and the `VirtualMachineSriovInfo` nested in the `PciPassthrough` union. That is the property spec 003 needs in order to extend the type additively later (§ 3). Do not weaken that test.

---

## 13. Per-story test plan

vmop-3736's acceptance criteria require the integration and E2E plans to be enumerated per Story S3..S9. Rather than duplicate the matrix, each story below names the case IDs it owns.

| Story | Ticket | Status | vcsim / integration cases | E2E cases |
|-------|--------|--------|---------------------------|-----------|
| S1 — capability + feature gate | vmop-3738 | Done (T012 outstanding) | TC-US6-00, TC-US6-01, TC-US6-01c | TC-US6-03e *(manual)* |
| S2 — partner integration doc | vmop-3739 | Untriaged | — | — |
| S3 — Zone fan-out | vmop-3740 | Done | TC-US1-01, TC-US1-01i, TC-US3-01, TC-EX-20 | TC-US1-01e, TC-US3-01e |
| S5 — ConfigTarget controller | vmop-3742 | Done | TC-US1-02v, TC-US1-02d, TC-US4-01, TC-US4-01c, TC-US4-02, TC-EC-02 | TC-US1-02e, TC-US1-03e, TC-US1-04e, TC-US4-01e |
| S6 — VirtualMachineConfigOptions controller | vmop-3743 | Done | TC-EX-04, TC-EX-05, TC-EX-06, TC-EX-07, TC-EX-08, TC-EX-09, TC-EX-10, TC-EX-11 | TC-EX-19 |
| S7 — VirtualMachineGuestOptions plumbing | vmop-3744 | In Review | TC-EX-02, TC-EX-03, TC-EX-04v, TC-US4-04*, TC-EX-03m | TC-EX-18, TC-US4-04e *(gap)* |
| S8 — VirtualMachineConfigPolicy controller | vmop-3745 | In Review | TC-US3-02, TC-US3-02v, TC-US3-02i, TC-US3-03, TC-US3-03t, TC-EC-03, TC-EC-03s, TC-EX-12..17 | TC-US3-02e, TC-US3-03e |
| S9 — VM admission enforcement | vmop-3746 | Untriaged | TC-US5-0x | TC-US5-0xe |
| S10 — ConfigTarget SR-IOV per-host | vmop-3926 | **Deferred** | Designed in `plan.md` I8; built under spec [`003`](../003-configtarget-sriov-per-host/). `tasks.md` T120–T124 map to 003's T010–T041 | — |
| S11 — Demand gate (§ 7.3) | vmop-3983 | In Progress | TC-EX-21, TC-EX-21r, TC-EX-21g, TC-EX-21o, TC-EX-21c | TC-EX-21e |

---

## 14. Coverage rollup

### 14.1 By status

**102 cases** in §§ 12.1–12.8, counted per the convention in § 11.2. § 12.9 lists rows that **moved out** to spec 003 and is not counted.

| Status | Count | Composition | Read as |
|--------|------:|-------------|---------|
| DONE | 38 | 34 DONE + 3 DONE → IN-PR + 1 DONE (synthetic) | Protected on `main` today |
| IN-PR | 23 | 22 IN-PR + 1 IN-PR (draft) | Protected once #1779, #1781, #1782, #1783, #1784, #1785, #1789 merge |
| PLANNED | 22 | — | Ticketed, unwritten: 15 US5 (admission) + 6 vmop-3983 (demand gate) + 1 |
| GAP | 12 | — | Automatable, unprotected, **untracked** — see § 14.4 |
| ENV-BLOCKED | 2 | 1 ENV-BLOCKED + 1 ENV-BLOCKED (partial) | Needs infrastructure, not effort |
| MANUAL | 2 | — | Needs a written procedure |
| DEFERRED | 2 | — | Spike vmop-3794 (1), spec 003 (1) |
| NOT TESTABLE | 1 | — | Design constraint, nothing to assert here |
| **Total** | **102** | one entry per row | |

By section: 12.1 US1 = 15, 12.2 US2 = 5, 12.3 US3 = 11, 12.4 US4 = 13, 12.5 US5 = 15, 12.6 US6 = 7, 12.7 edge cases = 8, 12.8 implementation-level = 28.

Separately, § 15 records **18 challenge findings** against tests that already exist. Those are not rows in this table — a row can be DONE and still be one of the weakest tests in the suite. Read § 14.1 and § 15 together, never § 14.1 alone.

### 14.2 The honest summary

Discovery (US1/US2 cluster-scope), fan-out (US3 creation), and garbage collection (US4) are covered at unit, vcsim, and E2E levels — but see § 15 before trusting the E2E half of that sentence. The policy sync controller (US3 sync) is comprehensively covered and **all of it is unmerged**. Two areas are unwritten: **admission enforcement (US5)** — the "enforce" half of the feature, and the reason the epic exists — and the **demand gate (vmop-3983)**, which is not yet designed to the point where its tests can be written (§ 7.3). Capability gating is tested where the flag is parsed but at none of the four places it is applied.

With SR-IOV deferred, those two are the entire PLANNED bucket.

### 14.3 Where a regression would go unnoticed today

1. A change that lets the VM webhook read policy while the capability is off (no test at any level).
2. A change to the `maxHardwareVersion` recompute path that made it sticky rather than level-triggered (only the unchanged case is asserted).
3. A regression in the CRD default values for the five policy mode fields.
4. A `QueryConfigOptionDescriptor` failure path that permitted GC (only the `QueryConfigTarget` failure path is asserted).

### 14.4 Gaps needing a new tracking ticket

Each of these is automatable today with no new infrastructure. Proposed as sub-tasks of the named parent.

| Gap | Proposed sub-task | Parent | Cases |
|-----|-------------------|--------|-------|
| **GAP-1** | *ConfigTarget: test `maxHardwareVersion` recompute when the descriptor set changes (grow and shrink)* | vmop-3742 | TC-US2-02, TC-US2-03b, TC-US4-03 (partial) |
| **GAP-2** | *VirtualMachineConfigPolicy: envtest coverage of CRD schema defaults for the five mode fields* | vmop-3745 | TC-US3-01d |
| **GAP-3** | *ConfigTarget: assert GC is skipped when `QueryConfigOptionDescriptor` fails, distinctly from `QueryConfigTarget`* | vmop-3742 | TC-US4-02d |
| **GAP-4** | *VirtualMachineGuestOptions: E2E coverage of the pruning path* | vmop-3744 | TC-US4-04e |
| **GAP-5** | *VM admission: fail closed when the zone's `ConfigTarget` is missing or not Ready* | vmop-3746 | TC-US5-06n |
| **GAP-6** | *Capability gate: assert no fan-out and no controller registration when the capability is off* | vmop-3738 | TC-US6-01, TC-US6-01c |
| **GAP-7** | *Capability gate: assert the pipeline is restored after a disable → enable cycle* | vmop-3738 | TC-US6-03 |
| **GAP-8** | *CEL rules remain enforced while the capability is off* | vmop-3738 | TC-US6-04 |
| **GAP-9** | *One invalid `VirtualMachineConfigPolicy` does not block a sibling in the same namespace* | vmop-3745 | TC-EC-03i |
| **GAP-DOC-1** | *Correct `spec.md`: (a) US1.4 / US2.1–2 describe `maxHardwareVersion` as per-host-derived — it is descriptor-derived; (b) US1.5, US1.6, and US2.3 need a "deferred to spec 003" status note — keep the scenarios, they are what spec 003 inherits; (c) US1.1's `spec.namespace.poolMoIDs` does not exist — the controller reads `spec.managedVMs.clusterMoIDs` (CHK-15)* | vmop-3736 | documentation |
| **GAP-DOC-2** | *Retitle vmop-3775 — the HW-version check reads `ConfigTarget.status`, not `HostSystem` labels* | vmop-3746 | ticket hygiene |

Also worth filing, though not test gaps: `tasks.md` T068/T069 describe a `gc.go` that will never exist, T100 describes a defaulting webhook that CRD defaults made unnecessary, and T070/T092/T106/T116 point at a `test/intg/` tree this repo does not have.

### 14.5 Not automatically testable

| ID | Case | Environmental reason | Disposition |
|----|------|----------------------|-------------|
| TC-US4-01r | Live cluster stops reporting a hardware version | vcsim's `EnvironmentBrowser` returns a fixed descriptor set per model; a real cluster's supported set cannot be shrunk on demand | Covered obliquely by TC-US4-01e's synthetic injection. Accept permanently |
| TC-US1-02de | vGPU / SGX device categories on a real Supervisor | Same; the presence-parity assertion silently skips them, so this coverage is **inert, not absent** — it will pass whether or not the mapping works | vmop-3965. Do not read the green E2E run as coverage of these categories |
| TC-EC-05 | `PlaceVmsXCluster` compatibility semantics | A constraint on where enforcement lives, not an observable behavior of this code | Permanently untestable here; validated by Spike vmop-3794 |
| **GAP-M1** | Host add/remove from a live cluster (TC-US2-03e) | Requires evacuating and removing an ESX host from a running Supervisor cluster | Write a manual procedure; run once per release |
| **GAP-M2** | Capability flipped off → on against a live Supervisor (TC-US6-03e) | The E2E suite skips entirely when the capability is off, so it structurally cannot assert off-state behavior | Write a manual procedure; the vcsim equivalent is GAP-7 |

---

## 15. Test-design challenge findings

§§ 12–14 answer *"is there a test?"*. This section answers *"is the test any good?"* — it is the result of reading the bodies of the merged and in-flight tests rather than their names, and challenging what each one actually proves. **A case can be DONE in § 12 and still appear here.** That is the point: several of the findings below describe tests that pass today, will keep passing, and protect nothing.

Severity is about consequence, not effort. **CRITICAL** = will break or is actively misleading about shipped behavior. **HIGH** = a real defect class is unguarded. **MEDIUM** = the test is weaker than it reads. **LOW** = hygiene.

| ID | Severity | Finding |
|----|----------|---------|
| CHK-1 | **CRITICAL** | **The E2E garbage-collection test will start failing the moment PR #1785 merges.** `configpolicy.go` creates its synthetic stale object as `VirtualMachineConfigOptions{Name: "vmx-e2e-stale-vmop-3760"}`. PR #1785 adds the CEL rule `self.matches('^vmx-[0-9]+$')` to `spec.hardwareVersion` plus a root rule `metadata.name == spec.hardwareVersion`. `vmx-e2e-stale-vmop-3760` does not match, so the API server rejects the `Create` and the spec fails before it asserts anything. #1785's own PR body says it verified that the *controllers* produce CEL-compliant values — the E2E suite was not checked. **Verified install path**: these CRDs are `//go:embed`ed via `config/crd/crd.go` / `pkg/crd/crd.go` and listed in `config/default/kustomization.yaml`, so the schema ships **inside the vm-operator image** — there is no separate chart bump to wait for. The E2E breaks as soon as it runs against an image built from a commit containing #1785. envtest is affected too: `test/builder/test_suite.go` points `CRDDirectoryPaths` at the same directory. **Fix**: rename the fixture to a syntactically valid but never-reported version, e.g. `vmx-9999`. Do this in #1785, not after it merges. Checked for other offenders — the only other non-conforming fixtures are in the two webhook test files #1785 already deletes. |
| CHK-2 | **HIGH** | **The E2E test for TC-US1-01e asserts nothing about the behavior it claims to cover.** Titled *"Should have at least one ConfigTarget derived from the zone's pool MoIDs"*, it lists **all** `ConfigTarget`s cluster-wide and then, inside a per-zone loop, asserts only `ct.Spec.ID.ID == ct.Name` for every one of them. That inner loop is zone-independent and re-runs identically for each zone. **If the Zone controller emitted a `ConfigTarget` for a completely wrong cluster MoID, this test would pass.** Nothing correlates a Zone to the `ConfigTarget` it produced. **Fix**: assert that for each Zone, the set of `ConfigTarget` names is a superset of that Zone's `spec.managedVMs.clusterMoIDs`. |
| CHK-3 | **HIGH** | **The same E2E test gates on a field the controller never reads.** It requires `z.Spec.ManagedVMs.PoolMoIDs` to be non-empty, but `reconcileConfigTargets` iterates `zone.Spec.ManagedVMs.ClusterMoIDs`. A Zone with pool MoIDs and no cluster MoIDs passes the precondition and produces nothing; a Zone with cluster MoIDs and no pool MoIDs fails the precondition despite working correctly. This is the same drift as CHK-15. |
| CHK-4 | **HIGH** | **The recompute assertion for TC-US2-02 is missing from a test that already has its setup.** The unit spec *"garbage-collects the corresponding VirtualMachineConfigOptions"* reconciles, mutates `descriptorsResult`, and reconciles again — the exact state transition TC-US2-02 needs — then asserts only the object count. Two extra lines would close GAP-1's first half. Worse, the mutated descriptor is `{Key: "vmx-20"}` with `CreateSupported` **unset**, so `maxHardwareVersion` should become empty on that second pass. Nobody checks. |
| CHK-5 | **HIGH** | **Fan-out and `maxHardwareVersion` disagree about `CreateSupported`. Confirmed by reading the code, not inferred.** `computeMaxHardwareVersion` counts only `CreateSupported == true` descriptors (asserted by three unit specs). `reconcileConfigOptions` iterates **every** descriptor and skips only `d.Key == ""` — there is no `CreateSupported` filter anywhere in the fan-out. A cluster can therefore publish a `vmx-21` `VirtualMachineConfigOptions` while `status.maxHardwareVersion` is `vmx-19`. That may be the right behaviour — run-supported-but-not-create-supported is a real vSphere state — but it is undocumented, unasserted, and lands squarely on the S9 admission gate: a user will see an object advertising a hardware version that admission then refuses. **Decide it, document it in § 4.2, and assert it either way.** |
| CHK-6 | **HIGH** | **A `VirtualMachineConfigOptions` with zero owner references is immortal and hot-loops at a fixed 6 reconciles/minute, silently.** Two confirmed halves. (a) `removeOwnerRefAndDeleteIfOrphaned` deletes only when `len(OwnerReferences) == 1 && [0].UID == obj.GetUID()`, so an object that reaches zero owners by any other route is never collected. (b) Its own controller returns `ctrl.Result{RequeueAfter: 10 * time.Second}, nil` — a **fixed** interval with a **nil error**, so there is no exponential backoff and nothing is logged at error level. The object reconciles every 10 seconds forever, invisibly. The existing unit test *"should requeue with a delay and mark Ready=False with ConfigTargetNotFound"* **ratifies this as correct behaviour rather than questioning it.** At minimum, back the retry off; better, treat zero-owner as collectable. |
| CHK-7 | **HIGH** | **The policy sync is never tested in the shrinking direction.** `mergeIntRangeMax` *overwrites* `Max` rather than taking a maximum, so a policy correctly narrows when its cluster's capability drops — that is the security-relevant behavior and it is right. But every test drives `Max` upward or sideways: *"copies its capacity limits into spec"*, *"intersects to the minimum of the per-cluster maxima"*, *"preserves an existing Min"*. **No test asserts that a previously-higher `Max` comes down.** A future refactor to `max(existing, new)` would look reasonable, leave a permanently over-permissive policy, and pass the whole suite. |
| CHK-8 | **HIGH** | **A cluster shrink can produce an inverted range and nothing rejects it.** `mergeIntRangeMax` overwrites `Max` while *preserving* an admin-set `Min`. If the cluster's capability drops below an admin's `Min`, the result is `Min > Max`. `IntRange`, `LongRange`, and `ResourceQuantityRange` carry no `Min <= Max` validation, and PR #1783 adds no such CEL rule. Untested and unguarded. **Decide**: clamp `Min` to the new `Max`, or reject via CEL and surface a condition. |
| CHK-9 | **MEDIUM** | **`ClusterNotFound` returns a plain error, so a permanent condition retries forever with an error log per attempt.** A cluster MoID that does not exist is not transient, yet `Reconcile` returns `err != nil` and `ctrl.Result{}` — controller-runtime then retries with exponential backoff indefinitely. `.sdd/memory/operator-best-practices.md` has vocabulary for exactly this (`pkgerr.NoRequeueError`, or `RequeueError{After: long}`), and `pkgerr.ResultFromError` is not used anywhere in this reconciler. The test asserts `err != nil` and thereby **blesses the behavior rather than challenging it**. |
| CHK-10 | **MEDIUM** | **The idempotence test watches the children but not the parent.** *"is idempotent on a second reconcile with unchanged vSphere state"* compares `resourceVersion` on every `VirtualMachineConfigOptions` and never on the `ConfigTarget` itself. A no-op status write on the parent is precisely what causes a watch-triggered hot loop, and it is the one object not covered. (Note the `VirtualMachineConfigPolicy` controller *does* test this — *"does not bump resourceVersion on a second reconcile"* — so the pattern exists in-tree.) |
| CHK-11 | **MEDIUM** | **`controllers/configtarget` hand-rolls a fake client that is weaker than the shared one.** It builds `fake.NewClientBuilder()` with `WithStatusSubresource(&vimv1.ConfigTarget{})` only, while `test/builder.NewFakeClient` already registers **all four** `vim.vmware.com` types via `KnownObjectTypes()`. A stray write to `VirtualMachineConfigOptions.Status` would persist here and be silently dropped by a real API server. Use the shared helper. |
| CHK-12 | **MEDIUM** | **Co-ownership is proven only against a client that cannot enforce it.** The two-owner test asserts `HaveLen(2)` / `HaveLen(1)` on a fake client, which does not validate owner-reference rules. It passes today because the controller uses `SetOwnerReference`, not `SetControllerReference` — but **no test asserts that choice**. A future switch to `SetControllerReference` would produce two controller refs, be rejected by a real API server, and pass every test in this package. Add an envtest case with two owners. |
| CHK-13 | **MEDIUM** | **The two most basic E2E specs have no `Eventually`.** *"Should have at least one ConfigTarget…"* and *"Should have a VirtualMachineConfigPolicy for each zone…"* do an immediate `List`/`Get`, while every later spec in the same file polls with configured intervals. On a freshly-created namespace these race the Zone controller. Inconsistent, and it is the foundational specs that are unprotected. |
| CHK-14 | **MEDIUM** | **The E2E policy spec never checks the default it exists to verify.** TC-US3-01's claim is *"a policy is created … with `spec.syncMode = ConfigTarget` by default."* The E2E asserts only `policy.Spec.Zone == z.Name` — and the policy is *named* `z.Name`, set from the same variable, so even that is nearly tautological. `syncMode` is never read. |
| CHK-15 | **MEDIUM** | **Three documents describe a cluster-MoID derivation the code does not perform.** `spec.md` US1.1 says `spec.namespace.poolMoIDs`; `tasks.md` T050 says "derive cluster MoIDs from the Zone's AvailabilityZone (`ClusterComputeResourceMoIDs`)"; the E2E gates on `spec.managedVMs.poolMoIDs`. The controller reads `zone.Spec.ManagedVMs.ClusterMoIDs` **directly, with no derivation at all** — and, reassuringly, the policy controller's field index keys off the same path, so the code is self-consistent. It is only the documentation that is wrong, in three places, three different ways. |
| CHK-16 | **LOW** | **Guest-options names can collide silently.** `toGuestOptionsName` lower-cases, replaces non-`[a-z0-9-]`, and truncates to 63 characters; the controller then sets `obj.Spec.ID = desc.Id` unconditionally on `CreateOrPatch`. Two distinct guest OS identifiers that transform to the same name would share one object, with the second silently overwriting the first's `spec.id` — and PR #1779's validator (`name == dnsSafe(id)`) would still pass, because both satisfy it. Real vSphere guest IDs are short alphanumerics, so this is unlikely in practice. **Either** prove that and simplify the transform, **or** handle the collision. Today it is neither. |
| CHK-18 | **MEDIUM** | **The fan-out will fail against a real API server on a malformed descriptor key, and the test that covers malformed keys cannot see it.** `reconcileConfigOptions` skips only empty keys, so a descriptor with `Key: "not-a-version"` produces a `Create` of `VirtualMachineConfigOptions{Name: "not-a-version"}` — which the CEL rule from #1785 (`^vmx-[0-9]+$`) rejects, failing the whole reconcile. The unit spec *"skips the malformed key without erroring"* runs against a fake client that enforces no schema, so it asserts `maxHardwareVersion` and never notices. Same root cause as CHK-1, opposite direction: CHK-1 is a fixture the schema will reject, this is production code the schema will reject. A real cluster is unlikely to return a malformed key — but the code explicitly tolerates one, and after #1785 it no longer can. **Fix**: filter the fan-out to well-formed keys, matching what `computeMaxHardwareVersion` already does. |
| CHK-17 | **LOW** | **The guest-options fan-out E2E is O(vco × guestIDs × vgo) inside an `Eventually`.** With ~15 hardware versions × ~150 guest IDs and a linear scan over ~150 `VirtualMachineGuestOptions` per lookup, that is on the order of 10⁵–10⁶ iterations *per poll attempt* against a live cluster. Build a map once outside the loop. |

### 15.1 What this changes

- **CHK-1 and CHK-18 are actionable now**, before #1785 merges. They are the two findings that will turn a green suite red, and they share a root cause: **the fake client enforces no CRD schema, so nothing in the unit or vcsim layers can see a CEL violation coming.** Every CEL rule this feature adds needs an envtest assertion — #1785 does this for the rules it introduces; the two cases here are the ones it missed.
- **CHK-2, CHK-3, CHK-13, CHK-14** together mean the E2E coverage of Story S3 (Zone fan-out) is substantially weaker than § 12 implies. TC-US1-01e and TC-US3-01e should be read as "an object exists" rather than "the right object exists."
- **CHK-4, CHK-5, CHK-7, CHK-8** are all in the same blind spot: **this feature is tested for growth and never for shrink.** Clusters lose hosts, get downgraded, and have hardware versions withdrawn; every one of those paths narrows a capability, and narrowing is the direction with security consequences. Add a shrink case at each layer.
- **CHK-6, CHK-9** are behaviors the tests currently ratify. Both deserve a design decision before S9 consumes them.

### 15.2 Proposed tracking

| Finding | Where it should be fixed |
|---------|--------------------------|
| CHK-1, CHK-18 | In PR #1785, as part of that PR |
| CHK-2, CHK-3, CHK-13, CHK-14 | New sub-task under vmop-3740 — *"Strengthen Zone fan-out E2E assertions"* |
| CHK-4, CHK-10 | Fold into **GAP-1** (vmop-3742), widening it to "recompute, shrink, and no-op reconcile" |
| CHK-5 | Needs a decision before S9 (vmop-3746) consumes `maxHardwareVersion` — the admission gate is where the inconsistency becomes user-visible |
| CHK-6, CHK-9 | New sub-task under vmop-3742 — *"Decide requeue semantics for permanent ConfigTarget failures and zero-owner VMCO"* |
| CHK-7, CHK-8 | Address in PR #1784 before merge — both are in code that is still in review |
| CHK-11, CHK-12 | New sub-task under vmop-3742 — *"Use test/builder's fake client; add envtest co-ownership coverage"* |
| CHK-15 | Fold into **GAP-DOC-1** |
| CHK-16, CHK-17 | Address in PR #1779 and #1782 respectively, or file as low-priority follow-ups |

---

## 16. Known deviations and follow-ups

| # | Item | Disposition |
|---|------|-------------|
| 0 | `spec.md`'s SR-IOV user stories (US1.5, US1.6, US2.3) are **retained deliberately** — they are the acceptance criteria spec 003 inherits. They need a "deferred to spec 003" status note, not deletion | GAP-DOC-1 |
| 1 | `spec.md` still carries two inaccuracies: `maxHardwareVersion` described as per-host-derived; US1.1 names a `spec.namespace.poolMoIDs` field the controller never reads | GAP-DOC-1 — deliberately **not** fixed in this change set, because `spec.md` had unrelated uncommitted edits in the working tree |
| 2 | vmop-3775 still references `HostSystem` labels | GAP-DOC-2 |
| 3 | `plan.md` I6 and `tasks.md` T100 specify a defaulting webhook; CRD defaults replaced it | Correct `plan.md` in the S8 PR |
| 4 | `plan.md` and `tasks.md` reference `controllers/configtarget/gc.go` and `test/intg/`; neither exists | Correct in the next `tasks.md` touch |
| 5 | vmop-3926 has no Epic Link, and SR-IOV is out of this release | Design retained in `plan.md` I8 and `tasks.md` S10 (marked deferred); implementation plan is spec [`003-configtarget-sriov-per-host`](../003-configtarget-sriov-per-host/). A new epic must be created and vmop-3926 linked to it |
| 6 | vmop-3983 has no description and no acceptance criteria | Write them before TC-EX-21 can be finalised |
| 7 | `VirtualMachineConfigPolicy` is not deleted when its `Zone` is deleted | Deferred by `spec.md`; needs its own story before GA |
| 8 | Webhook short-circuit on capability-off (T012) is unimplemented | Blocks TC-US6-02; see § 9 for why it is not optional |

---

## 17. Open items

1. **Publish the Confluence mirror** of this document under parent page ID `2453059710`, titled *TDS: VirtualMachineConfigPolicy*, and obtain the S3..S9 implementers' sign-off. Until then vmop-3736's acceptance criteria are not met by this file alone.
2. **File the follow-up work from § 14.4 and § 14.5**: nine code sub-tasks (GAP-1..GAP-9) against their named parents, two documentation corrections (GAP-DOC-1, GAP-DOC-2), and two manual runbooks (GAP-M1, GAP-M2).
3. **Await Spike vmop-3794** before writing any auto-NUMA / auto-placed-SR-IOV admission rules or their tests.
4. **Re-run this inventory when the seven open PRs merge** — 26 rows move from IN-PR to DONE, and PR #1785 relocates the evidence for TC-EC-01, TC-EC-01n, and TC-EX-01 from the webhook packages to the controllers' envtest suites.
