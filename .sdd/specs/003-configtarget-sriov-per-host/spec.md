# Feature Specification: ConfigTarget Per-Host SR-IOV Enrichment

- **Created**: 2026-07-31
- **Status**: Not started — **deferred to a future release**
- **Epic**: TBD (a new epic; **not** vmop-3331 / VCF 9.2)
- **Story**: vmop-3926 — *ConfigTarget: per-host SR-IOV enrichment and status.sriov type extensions*
- **Design inherited from**: [`001-class-policy-resize`](../001-class-policy-resize/) — `plan.md` I8 (API + controller design), `spec.md` US1.5/US1.6/US2.3 (acceptance scenarios), `tds.md` § 8.1. Spec 001 retains that design because it is what explains the shape of `ConfigTarget.status`; **this spec is the plan to build it.**
- **Depends on**: spec 001 shipping the `ConfigTarget` CRD, its controller, and the cluster-scope EnvironmentBrowser path

---

## Why this is a separate spec

Per-host SR-IOV discovery was designed inside spec 001 as Story S10, while the whole policy pipeline was one work item. The **design stays there**; what is split out here is the **plan to implement it**. The split happened because:

1. **It is not in the 9.2 release.** Spec 001 ships without it; nothing in 001's user stories depends on it.
2. **It is the only part of the pipeline that requires per-host vSphere RPCs.** Everything else in 001 is satisfied by three cluster-scope `EnvironmentBrowser` calls. The concurrency model, partial-failure semantics, and per-host error surface are a distinct design problem with a distinct risk profile.
3. **It cannot be validated on the current CI testbed.** It needs SR-IOV-capable hardware (vmop-3965). Keeping it inside 001 meant 001 could never reach "all acceptance criteria verified."

Spec 001 keeps the design (`plan.md` I8, `spec.md` US1.5/US1.6/US2.3, `tds.md` § 8.1) and marks Story S10's tasks `[~]` with a mapping into this spec's task IDs. It does **not** count these scenarios in its coverage rollup, because they will be verified here.

---

## Summary

Extend the `ConfigTarget` controller so that, in addition to its cluster-scope `QueryConfigTarget` / `QueryConfigOptionDescriptor` path, it walks the cluster's ESX hosts and aggregates per-host SR-IOV NIC data — including Device Virtualization Extensions (DVX) capability — onto `ConfigTarget.status.sriov`.

This exists because **`EnvironmentBrowser.QueryConfigTarget` does not return SR-IOV NICs when called at cluster scope** (spec 001 `research.md` Finding 1, empirically confirmed during Spike vmop-3470). SR-IOV is the one device category the cluster-scope call cannot supply, and it is the one category the VM admission webhook needs for SR-IOV feasibility checks.

No new CRD is introduced. All per-host data lands on the existing cluster-scoped `ConfigTarget`, so downstream consumers still perform exactly one `Get` per cluster (spec 001 `research.md` Finding 7).

---

## Pipeline position

```
ConfigTarget controller reconcile
  ├─ cluster scope ─▶ QueryConfigTarget          ──▶ status.* (19 non-SR-IOV device categories)   [spec 001]
  ├─ cluster scope ─▶ QueryConfigOptionDescriptor ──▶ status.maxHardwareVersion, VMCO fan-out     [spec 001]
  └─ per host ──────▶ PropertyCollector           ──▶ status.sriov                                [THIS SPEC]
                        config.pciPassthruInfo   (sriovCapable == true)
                        hardware.dvxClasses      (sriovNic == true)
```

---

## User stories

### US1 — CSP admin: SR-IOV inventory is discoverable per host (Priority: P1 within this spec)

A CSP admin, or the VM admission webhook, can determine which SR-IOV NICs exist on which hosts in a cluster, and what each NIC supports, from the cluster's `ConfigTarget` alone.

**Independent test**: on an SR-IOV-capable cluster, `kubectl get configtarget <clusterMoID> -o yaml` shows `status.sriov` with one entry per (host, NIC) pair, each carrying `hostMoID` attribution and DVX class fields where applicable.

**Acceptance scenarios**:

1. **Given** a cluster with SR-IOV-capable hosts, **when** the `ConfigTarget` controller reconciles, **then** `status.sriov` contains one entry per (host, SR-IOV NIC) pair, built by walking each host's `HostConfigInfo.pciPassthruInfo` for entries where `sriovCapable == true`.
2. **Given** a NIC whose device class appears in the host's `HostHardwareInfo.dvxClasses` with `sriovNic == true`, **when** the controller reconciles, **then** that entry's `dvxClass`, `dvxCheckpointSupported`, and `dvxSwDmaTracingSupported` fields are populated from the matching `HostDvxClass`.
3. **Given** a NIC whose device class is absent from `dvxClasses`, **when** the controller reconciles, **then** `dvxClass` is empty and the DVX capability booleans are false — the entry is still emitted.
4. **Given** the same physical SR-IOV NIC model present on two hosts, **when** the controller reconciles, **then** two distinct entries exist, differing in `hostMoID`.
5. **Given** the cluster-scope `QueryConfigTarget` result carries an `sriov` field or a `VirtualMachineSriovInfo` nested in its `pciPassthrough` union, **when** the controller reconciles, **then** that data is **not** used — `status.sriov` is sourced exclusively from the per-host `PropertyCollector` path.

---

### US2 — Platform engineer: one slow or broken host does not poison the cluster's inventory (Priority: P1 within this spec)

**Independent test**: with one host's `PropertyCollector` RPC forced to fail, the other hosts' SR-IOV entries are still written and a warning event names the failing host.

**Acceptance scenarios**:

1. **Given** the per-host RPC for one host fails while the others succeed, **when** the controller reconciles, **then** the failing host's entries are absent from `status.sriov` for this pass, the successful hosts' entries are still written, a warning event referencing the host MoID is recorded, and the reconcile is requeued.
2. **Given** the same host's RPC succeeds on a subsequent pass, **when** the controller reconciles, **then** that host's entries appear and no further warning event is emitted.
3. **Given** every host's RPC fails, **when** the controller reconciles, **then** the cluster-scope portion of `status` is still written and `Ready` reflects the cluster-scope result — a total per-host failure must not mask a healthy cluster-scope reconcile as `Ready=False`. *(This is a design decision to confirm in `plan.md`; the alternative — `Ready=False` when no per-host data could be collected — is defensible and must be chosen explicitly, not by accident.)*
4. **Given** a cluster with a large number of hosts, **when** the controller reconciles, **then** the per-host RPCs run with a bounded concurrency rather than one goroutine per host.

---

### US3 — CSP admin: SR-IOV inventory tracks the cluster (Priority: P2)

**Acceptance scenarios**:

1. **Given** a host is removed from the cluster, **when** the next reconcile completes, **then** that host's SR-IOV entries no longer appear in `status.sriov`.
2. **Given** a host is added to the cluster, **when** the next reconcile completes, **then** its SR-IOV entries appear.
3. **Given** SR-IOV is enabled on a NIC but the host has not been rebooted, **when** the controller reconciles, **then** the entry's `active` field is `false` (mirroring `HostSriovInfo.sriovActive`) while `maxVFs` still reports the NIC's capability.

> **No garbage collection is required.** `status.sriov` is rewritten wholesale on each successful pass, so removed hosts drop out naturally. There are no per-host Kubernetes objects (spec 001 `research.md` Finding 7).

---

## API surface

Additive on `external/vim/api/v1alpha1/config_target_devices_types.go`. Extend `VirtualMachineSriovInfo`:

| Field | Type | Marker | vSphere source |
|-------|------|--------|----------------|
| `hostMoID` | `string` | `+required` | the ESX host whose `pciPassthruInfo` contributed the entry |
| `active` | `bool` | `+optional` | `HostSriovInfo.sriovActive` |
| `maxVFs` | `int32` | `+optional` | `HostSriovInfo.maxVirtualFunctionSupported` |
| `numVFs` | `int32` | `+optional` | `HostSriovInfo.numVirtualFunction` |
| `dvxClass` | `string` | `+optional` | `HostDvxClass` name where `sriovNic == true` |
| `dvxCheckpointSupported` | `bool` | `+optional` | `HostDvxClass.checkpointSupported` |
| `dvxSwDmaTracingSupported` | `bool` | `+optional` | `HostDvxClass.swDMATracingSupported` |

`ConfigTarget.status.sriov` gains `+listType=map` with `+listMapKey=hostMoID` and `+listMapKey=pciDevice.id`, so the same NIC on two hosts is two independently-patchable entries and a partial write patches cleanly.

**API-compatibility note.** Per `.sdd/memory/constitution.md`, additive changes are *not* automatically safe once an API has shipped. If spec 001 ships `ConfigTarget` in 9.2 and this lands in a later release, adding a `+required` `hostMoID` to an existing list element type must be evaluated against the shipped schema. Two things must be checked before implementation: whether 9.2 ever writes a non-empty `status.sriov` (if it never does, the list is always empty and the change is trivially safe), and whether the `+listType=map` key change is a breaking transition from the shipped `atomic`/`set` type. **This is the single biggest reason not to defer the API change casually.** Resolve it in `plan.md`.

---

## Edge cases

- A host reports `pciPassthruInfo` entries that are not `HostSriovInfo` (plain PCI passthrough) — these must be filtered out, not coerced.
- A host reports a NIC with `sriovCapable == false` — excluded.
- Two DVX classes match the same NIC device class — define which wins, deterministically.
- A host is in maintenance mode or disconnected — decide whether to attempt the RPC at all, and whether a disconnected host counts as a "failure" for US2.1's warning event or is skipped silently.
- The cluster has zero hosts (an empty cluster shell) — `status.sriov` empty, no error.

---

## Out of scope

- **SR-IOV enforcement at VM admission.** Deciding whether a VM's requested SR-IOV configuration is satisfiable belongs to the admission story in spec 001 (vmop-3746) or to the auto-placed-SR-IOV rules gated on Spike vmop-3794. This spec only *publishes* the inventory.
- **Auto-placed SR-IOV and auto-NUMA admission rules** — Spike vmop-3794.
- **Per-host default hardware version.** `ConfigTarget.status.maxHardwareVersion` is derived from `QueryConfigOptionDescriptor` at cluster scope and needs no per-host call (spec 001 § 3). Do not reintroduce a per-host hardware-version query as part of this work.
- **Any per-host CRD.** Retired — spec 001 `research.md` Finding 7.

---

## Testability constraints (read before planning)

These are inherited from spec 001's test design and are the reason this spec's E2E story is weak:

- **vcsim does not model SR-IOV `pciPassthruInfo` or `dvxClasses`.** Integration coverage must pre-seed vcsim's host properties (the same override technique spec 001's `ConfigTarget` device-mapping vcsim test uses for `QueryConfigTargetResponse`), or it is testing nothing.
- **The current E2E testbed has no SR-IOV NICs.** vmop-3965 tracks either obtaining real hardware or making the mock path work. Until it is resolved, every E2E scenario here is ENV-BLOCKED, and a green E2E run must **not** be read as SR-IOV coverage.
- **Partial-failure testing needs RPC-level fault injection.** The provider interface must expose a seam that lets a test fail exactly one host's call; if it does not, add one as part of the implementation rather than declaring US2 untestable.

---

## Review & acceptance checklist

- [ ] Every user story has at least two Given/When/Then scenarios.
- [ ] The API-compatibility question in § *API surface* is resolved before implementation starts.
- [ ] US2.3 (total per-host failure vs. `Ready`) has an explicit, documented decision.
- [ ] The concurrency bound in US2.4 is a named constant with a stated rationale.
- [ ] The vcsim seeding approach is proven before integration tests are written.
- [ ] An epic is created for this spec and vmop-3926's Epic Link is set to it (it currently has none).
