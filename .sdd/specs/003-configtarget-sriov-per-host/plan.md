# Implementation Plan: ConfigTarget Per-Host SR-IOV Enrichment

- **Spec**: [`spec.md`](spec.md)
- **Date**: 2026-07-31
- **Status**: Draft — **not scheduled**; do not begin implementation until the open questions in § 2 are closed
- **Story**: vmop-3926
- **Prerequisite**: spec [`001-class-policy-resize`](../001-class-policy-resize/) merged and shipped
- **Relationship to spec 001**: 001 `plan.md` I8 holds the *design* (API fields, controller sketch, per-host RPC strategy). This file is the *implementation plan* — it does not restate the design; it resolves the four questions I8 left open and sequences the work.

---

## 1. Summary

Add a per-host enrichment path to the existing `ConfigTarget` reconciler. It runs after the cluster-scope path, enumerates the cluster's ESX hosts, issues one bounded-concurrency `PropertyCollector` RPC per host, and writes one `VirtualMachineSriovInfo` per (host, SR-IOV NIC) pair onto `ConfigTarget.status.sriov`.

No new controller, no new CRD, no new webhook, no new RBAC resource — only additive fields on an existing type and additive logic in an existing reconciler.

---

## 2. Open questions — close these before writing code

| # | Question | Why it blocks |
|---|----------|---------------|
| Q1 | Does the shipped (9.2) `ConfigTarget` CRD ever populate `status.sriov`? | If it does, adding `+required hostMoID` and changing the list to `+listType=map` is a schema transition on live data, not an additive change. See `spec.md` § *API surface* and `.sdd/memory/constitution.md` § *API compatibility*. If it never populates the field, this is trivially safe and Q1 costs nothing to answer. |
| Q2 | Does a total per-host failure make the `ConfigTarget` `Ready=False`? | `spec.md` US2.3. Affects the admission webhook, which reads `ConfigTarget` and must decide whether "no SR-IOV data" means "no SR-IOV NICs" or "unknown." Getting this wrong silently converts a discovery outage into a capability denial. |
| Q3 | Can vcsim be made to report `config.pciPassthruInfo` and `hardware.dvxClasses`? | If not, there is no integration layer at all for this feature — only unit tests over synthetic property data plus an ENV-BLOCKED E2E. That materially changes the risk and should be a conscious decision, not a discovery made mid-implementation. |
| Q4 | Is a disconnected / maintenance-mode host a failure or a skip? | Determines whether a routine host maintenance window emits warning events and requeue churn on every cluster in the Supervisor. |

---

## 3. Repository layout

```
external/vim/api/v1alpha1/
├── config_target_devices_types.go   MODIFY — extend VirtualMachineSriovInfo (7 fields)
├── config_target_types.go           MODIFY — +listType=map on status.sriov
└── zz_generated.deepcopy.go         REGEN

config/crd/external-crds/
└── vim.vmware.com_configtargets.yaml  REGEN

controllers/configtarget/
├── configtarget_controller.go       MODIFY — call the per-host path after the cluster-scope path
├── sriov.go                         NEW    — host enumeration, bounded fan-out, DVX correlation
└── configtarget_controller_test.go  MODIFY — unit + vcsim Describes

pkg/providers/vsphere/
└── environment_browser.go           MODIFY — add the per-host PropertyCollector wrapper
                                              (or a sibling host_properties.go)

test/e2e/vmservice/vmservice/configpolicy/
└── configpolicy.go                  MODIFY — SR-IOV spec, skipped unless the testbed reports NICs
```

Deliberately **not** created: `controllers/hostsystem/`, `webhooks/hostsystem/`, any per-host CRD or manifest. Retired in spec 001 `research.md` Finding 7; do not resurrect.

---

## 4. Design

### 4.1 Provider seam

Add to the `VMProvider` interface a method shaped so a test can fail exactly one host:

```go
// GetClusterHostSriovInfo returns, per host MoID in the named cluster, the
// host's SR-IOV-capable PCI passthrough entries and its DVX classes. A host
// whose RPC failed appears in errs keyed by its MoID; its absence from the
// result map is not an error for the other hosts.
GetClusterHostSriovInfo(
    ctx context.Context,
    clusterMoID string,
) (map[string]HostSriovProperties, map[string]error, error)
```

Returning a per-host error map rather than a single joined error is what makes `spec.md` US2.1 unit-testable without an RPC-level interceptor. The third return value is reserved for a failure to enumerate hosts at all.

The implementation wraps the context with `pkgctx.WithVCOpID(ctx, obj, "hostSriovProperties")` per `.sdd/memory/operator-best-practices.md`, once for the whole enumeration.

### 4.2 Reconciler integration

```
reconcileNormal:
  ... existing cluster-scope path (QueryConfigTarget, QueryConfigOptionDescriptor,
      maxHardwareVersion, VMCO fan-out, GC) ...

  if clusterScopeSucceeded:
      props, perHostErrs, err := provider.GetClusterHostSriovInfo(ctx, clusterMoID)
      if err != nil:
          record warning event; leave status.sriov untouched; requeue
      else:
          status.sriov = buildSriovEntries(props)      // wholesale rewrite
          for hostMoID, hostErr := range perHostErrs:
              record warning event naming hostMoID
          if len(perHostErrs) > 0:
              return pkgerr.RequeueError{After: …}
```

Two invariants worth stating because they are easy to get wrong:

- **The per-host path never gates GC.** GC is a function of the cluster-scope enumeration only (spec 001 `research.md` Finding 3). A per-host failure must not suppress or trigger object deletion.
- **`status.sriov` is rewritten wholesale, not merged.** A partially-successful pass therefore *shrinks* the list. That is intentional — it is what makes host removal work without GC — but it means a consumer must treat "absent" as "unknown this pass," not "definitively no SR-IOV." Q2 is the same question at the `Ready` level.

### 4.3 DVX correlation

For each SR-IOV entry, match the NIC's device class against the host's `dvxClasses` where `sriovNic == true`. Define the tie-break for two matching classes deterministically (lexicographically lowest class name is the cheap, reproducible choice) and assert it in a unit test — spec 001 has a directly analogous precedent in the `VirtualMachineConfigOptions` controller's lexicographically-lowest cluster-MoID tie-break.

### 4.4 Concurrency

Bounded worker pool over hosts. Pick the bound as a named constant with a comment stating the reasoning (a large Supervisor cluster can have 64+ hosts; an unbounded fan-out opens 64 simultaneous vCenter sessions per reconcile, per cluster). Honour `ctx.Done()` in the worker loop.

---

## 5. RBAC

None. Reading vSphere host properties is a vCenter-side permission, not a Kubernetes one. `configtargets` and `configtargets/status` grants already exist from spec 001.

---

## 6. Test strategy

| Level | What it must cover | Feasibility |
|-------|--------------------|-------------|
| unit | Entry construction from synthetic `HostSriovProperties`; `hostMoID` attribution; DVX correlation and its tie-break; non-`HostSriovInfo` passthrough entries filtered; `sriovCapable == false` excluded; partial failure leaves other hosts intact and requeues; total failure behaviour per Q2; empty cluster | Full — the provider seam in § 4.1 makes every case injectable |
| envtest | `+listType=map` merge-patch behaviour: patching one host's entries does not clobber another's | Full; **required**, because listMap semantics are enforced by the API server and invisible to the fake client |
| vcsim | Whether the controller reads the properties it thinks it reads | **Blocked on Q3.** If vcsim cannot report these properties, say so here rather than writing a test that asserts an empty list |
| e2e | Per-host entries with `hostMoID` on real SR-IOV hardware | **ENV-BLOCKED** — vmop-3965 |

**Do not** write an E2E spec that passes vacuously when the testbed has no SR-IOV NICs. Spec 001's device-category presence-parity check is a live example of coverage that looks green and asserts nothing; if the same shape is used here, label it explicitly so nobody mistakes it for protection.

---

## 7. Risk and rollback

| Risk | Mitigation |
|------|-----------|
| API change is not additive-safe against a shipped 9.2 `ConfigTarget` | Q1 — answer before writing code, not during review |
| Unbounded per-host RPCs exhaust vCenter sessions on large clusters | § 4.4 bounded pool with a justified constant |
| Routine host maintenance produces continuous warning events and requeues | Q4 |
| The feature ships with no test layer between unit and a hardware-only E2E | Q3; if vcsim cannot help, escalate vmop-3965 to a blocker rather than shipping on unit tests alone |

**Disable path**: this rides the same `supports_vm_service_vm_config_policy` capability as spec 001. There is no separate gate, and no separate rollback — with the capability off, the `ConfigTarget` controller does not run at all.
