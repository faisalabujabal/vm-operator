# Research: ConfigTarget Per-Host SR-IOV Enrichment

- **Feature**: `.sdd/specs/003-configtarget-sriov-per-host/`
- **Story**: vmop-3926
- **Parent research**: [`001-class-policy-resize/research.md`](../001-class-policy-resize/research.md) — Findings 1, 3, and 7 are the origin of this work and are not restated here in full

---

## Finding A — the reason this feature exists

`EnvironmentBrowser.QueryConfigTarget` called against a `ClusterComputeResource` does **not** populate the `sriov` field. It does so only when called against an individual `HostSystem` MoR. Confirmed empirically during Spike vmop-3470; recorded as spec 001 `research.md` Finding 1.

SR-IOV is therefore the single device category the cluster-scope path cannot supply. Every other `ConfigTargetDevices` category — all 19 of them — comes back from the one cluster-scope call that spec 001 already makes.

## Finding B — `PropertyCollector`, not per-host `QueryConfigTarget`

Two ways to get per-host SR-IOV data:

| Approach | Cost | Verdict |
|----------|------|---------|
| `QueryConfigTarget` against each `HostSystem` MoR | Returns the entire `ConfigTarget` structure per host — datastores, networks, every device category — to extract one field | Rejected |
| `PropertyCollector` for `config.pciPassthruInfo` + `hardware.dvxClasses` | One multi-property RPC per host, returning only what is needed | **Chosen** |

## Finding C — why there is no `HostSystem` CRD

Spec 001 `research.md` Finding 7 in full. Summary: a per-host CRD added a second Kubernetes object kind, a second RBAC surface, a well-known-label contract that duplicated `status`, watch wiring, and a second GC path — all for data whose only consumers are one `Get` from the admission webhook and one read by the `ConfigTarget` controller. vmop-3741 and vmop-3752..vmop-3756 are closed *Won't Implement*.

Do not reintroduce it. If per-host data outgrows `ConfigTarget.status` in a later release, that is a new design decision requiring its own rationale, not a revival of the retired one.

## Finding D — no garbage collection is needed

`status.sriov` is rewritten wholesale on every successful pass, so a removed host's entries disappear without any deletion logic. This is a direct consequence of Finding C: there are no per-host Kubernetes objects to become stale.

The corollary that is easy to miss: because the list is a wholesale rewrite, a *partially* successful pass **shrinks** it. A consumer cannot distinguish "this host has no SR-IOV NICs" from "this host's RPC failed this pass." That ambiguity is exactly what `plan.md` Q2 has to resolve at the `Ready`-condition level before the admission webhook can safely consume the field.

## Finding E — testability is the dominant risk, not implementation difficulty

The implementation is a bounded fan-out and a struct mapping. The risk is that **it may ship with no test layer between unit tests and hardware nobody has**:

- vcsim's modelling of `config.pciPassthruInfo` and `hardware.dvxClasses` is **unverified** (`plan.md` Q3 / `tasks.md` T002). Spec 001's `ConfigTarget` vcsim test had to pre-seed `QueryConfigTargetResponse` because vcsim's own computation never populates device categories; the same may or may not be possible for host properties.
- The E2E testbed has no SR-IOV NICs (vmop-3965).
- Spec 001's E2E device-category check uses presence-parity, so it *already* skips SR-IOV silently and passes. Reusing that shape here would produce a green test that asserts nothing.

Answer T002 before committing to a schedule. If vcsim cannot help and vmop-3965 is unresolved, this feature's real coverage is unit tests over synthetic data, and that should be an explicit, accepted decision rather than a discovery made after the code is written.

## Finding F — the API change may not be free

Spec 001 ships `ConfigTarget` with a `status.sriov` field already present in the schema. Adding a `+required` `hostMoID` to its element type and changing the list to `+listType=map` is only trivially safe if the shipped release never writes a non-empty list.

Per `.sdd/memory/constitution.md`, additive changes are not automatically safe once an API has shipped, because of how Kubernetes handles `UPDATE` (see <https://github.com/kubernetes/kubernetes/issues/111703>). Spec 001's controller explicitly excludes SR-IOV from what it writes — both `ct.Sriov` and the `VirtualMachineSriovInfo` nested in the `PciPassthrough` union — and there is a unit test asserting exactly that (*"maps every non-SR-IOV device category and excludes SR-IOV entirely"*). That is strong evidence the list is always empty in 9.2, which would make this change safe. **Confirm it rather than assume it** — `tasks.md` T000.

---

## References

- Spec 001: [`spec.md`](../001-class-policy-resize/spec.md), [`research.md`](../001-class-policy-resize/research.md) Findings 1/3/7, [`tds.md`](../001-class-policy-resize/tds.md) § 12.9
- Spike: vmop-3470
- Story: vmop-3926 (no Epic Link set)
- Environmental blocker: vmop-3965
- vSphere API: `HostConfigInfo.pciPassthruInfo`, `HostSriovInfo`, `HostHardwareInfo.dvxClasses`, `HostDvxClass`
- govmomi: `github.com/vmware/govmomi/property`
