# `VirtualMachineClass` v2: vAPI, vcdb storage, and CRD writers

- **Status**: Draft design. Open items are in §8.
- **Companion to**: [`vmclass-v2-design.md`](./vmclass-v2-design.md) (the Kubernetes-side model), [`vmclass-v2-governed-value-types.md`](./vmclass-v2-governed-value-types.md), [`vmclass-v2-full-schema.md`](./vmclass-v2-full-schema.md)
- **Audience**: principal engineers, wcpsvc and vAPI owners, architecture review

The design doc describes what a `VirtualMachineClass` v2 object means inside a Supervisor. This document covers everything upstream of that object: where class data is stored in vCenter, how it is exposed through vAPI, how the vAPI surface is generated from the CRD, and which components write the Kubernetes objects. It is grounded in the current wcpsvc and wcp-namespace-operator code; file references are to `bora/vpx/wcp/wcpsvc/src/server/` unless stated otherwise.

---

## 1. Decisions recorded here

| # | Decision | Section |
|---|---|---|
| D1 | vcdb stays the store for class definitions. A class existing in vCenter is a different fact from a class being associated with a Supervisor namespace; classes exist with no Supervisor at all. | §3 |
| D2 | A new, separate class endpoint (v2). The existing `VirtualMachineClasses` interface stays as a hand-written adapter over the same vcdb row. | §4.1 |
| D3 | The canonical values live in a new CRD-shaped document column. The existing scalar columns (`cpu_count`, `memory_mb`, reservation percentages, `devices`) become derived copies, written by one function. | §3.1, §3.2 |
| D4 | Reservations: the absolute value is canonical; the percentage columns are derived from `default`. | §3.3 |
| D5 | The class-to-namespace association, including zones, extends the existing namespaces API (`Instances`). There is no new association API. | §4.4 |
| D6 | Zones use three states. Availability: unset = all namespace zones including future ones, `[]` = none, a list = only those zones. Governance: unset = same as availability, `[]` = none, a list = only those zones, which must be within availability. | §4.5 |
| D7 | Every field supports "unchanged" and "clear": generate both `update` (PATCH) and `set` (PUT). | §4.2 |
| D8 | Validation: semantic and feasibility errors stay asynchronous in `Info.messages` (today's model). Structural checks generated from the schema run synchronously in the handler. No shared validation code between repos. | §4.3 |
| D9 | The v2 vmodl is generated at build time from the CRD, reviewed and committed, not produced at runtime. | §5 |
| D10 | Enums: vmodl enums are preferred; the fallback is a `String` with the allowed values documented and a generated handler check. | §5.3 |
| D11 | `@Released` is required on the generated vmodl. | §5.3 |
| D12 | Both CRD writers (wcpsvc and wcp-namespace-operator) move to the new CRD version, and the version is chosen per Supervisor. | §6 |

---

## 2. Where class data lives today

### 2.1 vcdb: `vm_class_class_configs`

`vmclass/vmclass_catalog.go`, `DBConfig`:

| Column | Type | Notes |
|---|---|---|
| `id` | text, PK | The class name; also the Kubernetes object name (`copyIntoKubeObject` sets `dst.Name = src.ID`). |
| `cpu_count`, `memory_mb` | bigint, `NOT NULL` | Scalar sizing. |
| `cpu_reservation`, `memory_reservation` | bigint, nullable | **Percentages** of `cpu_count`/`memory_mb`. |
| `description`, `version`, `config_status` | scalar | |
| `messages`, `devices`, `instance_storage`, `config_spec` | JSONB | `config_spec` is the opaque `VirtualMachineConfigSpec`. |
| `config_spec_xml_b64` | text | Unused since vSphere 8.0; kept only because nobody wrote the DDL to drop it. |

How the table changes:

- **Schema:** GORM `AutoMigrate(&DBConfig{})` at startup (`InitializeDB`). It only adds.
- **Reads and writes do not use GORM.** They are hand-written SQL statements with fixed column lists (`vmClassCfgQueryAll`, `vmClassCfgInsert`, `vmClassCfgUpdate`, ...). Adding a column means changing `DBConfig` **and** every statement; a statement that is missed silently leaves the column stale.
- **Columns can be dropped.** wcpsvc already does it with explicit DDL: `dropSingleMasterDbColumns`, `dropVMCDRsDbColumns` and the `dropColumns` helper in `kubelifecycle/kube_instance_db.go`; `DROP COLUMN IF EXISTS cluster` in `kubelifecycle/storage_policy_db.go`; and a move-then-drop sequence (`clusteredResourcesMigrateQueryStmt`, then dropping `workloads_res_pool` and `master_cluster_module_id`).
- **Data migrations** run in `vmclass.Initialize()` behind a feature flag. Precedent: `migrateVMClassesToConfigSpecNoLock`, which is idempotent (`WHERE config_spec IS NULL`). Its iterator stops at the first failing row, and the caller calls `log.StdLog.Fatalf`, so a single bad row puts wcpsvc into a crash loop.
- **Existing consistency rule:** wcpsvc does not copy `cpu_count` into the blob's `NumCPUs`, but it **rejects** a create or update where they disagree (`vmclass_validate.go`, `VCenterWCPVMClassConfigSpecCPUCountInvalid` / `...MemoryMBInvalid`).
- **Existing dual representation:** `migrateCreateSpecForClassAsConfig` / `migrateUpdateSpecForClassAsConfig` (`vmclass_configspec.go`) keep the typed `devices`, `instanceStorage` and reservation fields and the blob's `DeviceChange`/allocations in sync in both directions. That is about 200 lines under a `gocyclo` nolint for three field groups; it is the cost estimate for any hand-written translation between shapes.

### 2.2 The namespace association

- **vAPI:** `com.vmware.vcenter.namespaces.Instances.VMServiceSpec { Optional<Set<ID>> contentLibraries; Optional<Set<ID>> vmClasses; }` (released 1.3), used by Create, CreateV2, Set, Update, Info and InfoV2. Hand-written in `bora/vpx/wcp/wcpsvc/vmodl/namespaces/Namespaces.vmodl`.
- **Zones:** `CreateSpecV2.zones: List<ZoneSpec>`, where `ZoneSpec` carries per-zone limits and `vmReservations: List<VirtualMachineClassAllocationSpec>` (class + count). That capacity reservation is the only place a class and a zone meet today. `namespaces/instances/Zones.vmodl` has only `delete`.
- **Reverse view:** `VirtualMachineClasses.Info.namespaces: Set<ID>` is read-only.
- **Storage:** `workload.DBConfig.VMClasses`, column `vm_classes`, a JSONB list of class names.
- **etcd-backed Supervisors:** `SupervisorNamespace.spec.vmService.vmClassRefs`, a list of `{name}` structs.

### 2.3 CRD writers

There are two:

1. **wcpsvc** (`vmclass/vmclass_controller.go`, `ensureVMClassCRInCluster`): `ctrlutil.CreateOrUpdate` of a **v1alpha1** object built by `copyIntoKubeObject` (`vmclass/vmclass_kube.go`), which replaces `Spec.Hardware` wholesale. The vAPI call returns before this runs; failures land in the class's `Info.messages`. After a component upgrade, `syncAllNamespaces` (`workload/workload_sync.go`) re-drives these writes, through the same v1alpha1 writer.
2. **wcp-namespace-operator** on etcd-backed Supervisors (`bora/vim/supervisor/controller-managers/wcp-namespace-operator/controllers/svns/vmclass/vmclass_controller.go`). For namespace operations on those Supervisors, both `syncAllNamespaces` and the wcpsvc reconcile return early. nsop:
   - copies **only `Spec`**, through **v1alpha1**, from the class in the `vmware-system-vmop` catalog namespace (which wcpsvc writes);
   - falls back to the v1 vAPI `GetVmClass` when the class isn't in the catalog. That fallback already drops `configSpec` and `instanceStorage` today, and computes reservations from `CpuCount` × percentage.

Consequences for v2: every new field is lost at each of these hops unless the writer moves to the new CRD version. A round-trip annotation cannot help, because nsop copies `Spec` only.

---

## 3. vcdb changes

### 3.1 New columns

| Table | Change | Contents |
|---|---|---|
| `vm_class_class_configs` | **One new JSONB column** (name TBD, e.g. `spec_v2`) | The class-wide part of the v2 spec, CRD-shaped: `hardware` ranges `{min, max, default}`; `policies.resources` ranges (absolute values); the typed hardware fields elevated from `configSpec`; `devices` and `extraConfig` as list policies (`entries`/`allowed`/`denied`); the leftover `configSpec`; the class-wide `governs` settings (`enforcement`, `existingVMs`); `externalID`. Also internal bookkeeping inside the document: a document schema version and the elevation level (open item O7). |
| `vm_class_class_configs` | Possibly a scalar `external_id` column | Only if wcpsvc or VCFA need to look classes up or index by it. Open (O9). |
| `workload` (`vm_classes`) | **No new column; the JSONB shape changes** from `["a", "b"]` to `[{"name": "a", "zones": ..., "governedZones": ...}]` | The per-namespace association (§4.4). Read with a decoder that accepts both shapes; always written in the new shape. |

Not stored on the class row: `zones` and `governs.zones` (per namespace, in the association); `reservedProfileID` and `reservedSlots` (derived from the capacity store); `status`.

### 3.2 The old columns become derived copies

The new document is canonical. `cpu_count`, `memory_mb`, the reservation percentages and `devices` (as the v1 `entries` view) are **derived from it and written in the same statement by one write function**. Every path that writes the row must go through that function: the INSERT/UPDATE statements, `addDefaultClasses`, and every migration.

They are not only for v1 reads. Their consumers are:

- **The v1 endpoint, reads and writes.** A v1 GET shows `default`. A v1 UPDATE of `cpuCount` changes the canonical `default`, and is then subject to the range check (the old-vAPI collapse question, open item O1).
- **The v1alpha1 CRD writer**, for Supervisors that have not yet moved to the new CRD version (§6).
- **nsop's fallback** through the v1 `GetVmClass`.

Outside the `vmclass` package, nothing else in wcpsvc reads a class's CPU or memory.

### 3.3 Reservations

The v2 spec carries absolute request values. The percentage columns are computed from them and the canonical `default`, so for a fixed class the result is exactly today's value. Two caveats:

- **Rounding.** The percentage columns are integers. A 1000m request with 3 default CPUs is 33.33%; the v1 API shows 33, and a v1 client that writes the class back silently changes the request to 990m.
- **Direction.** Today, when a v1 client changes `cpuCount`, the percentage stays fixed and the absolute reservation is recomputed. Once the absolute value is canonical, the v1 adapter has to recompute it deliberately to keep that behavior.

### 3.4 Migration

- **Blob extraction** (see the design doc §6.2): for each class, move `configSpec` content that has a typed field into that field; everything else stays in the leftover `configSpec`. Because anything that can't be elevated simply stays in the blob, the migration has no failure case for content. It must still process rows one at a time and record a per-class message instead of calling `Fatalf`, and it must be idempotent.
- **No zone backfill.** An existing association has no zone lists, which reads as "available in all zones, governs all zones", and no existing class sets `governs`, so the behavior is exactly today's.
- **Precedent:** the feature-flag-gated `migrateVMClassesToConfigSpecNoLock`, run from `vmclass.Initialize()`; a retry path can use the self-heal pattern (`AddPostSupervisorReadyCallback`, as `backfillVolumeAttributeClasses` does).

### 3.5 Risks

1. A hand-written SQL statement that isn't updated leaves the new column stale without any error.
2. Two copies of `default` (document and `cpu_count`) drift if any path bypasses the single write function.
3. A `Fatalf` in the migration path breaks the zero-hiccup upgrade requirement.
4. The database doesn't validate JSONB; all validation is in Go.
5. Every in-memory copy of the class (catalog, notifications, CRD writer) must carry the new document.
6. VC upgrade rollback restores the pre-upgrade database, so an extra column is harmless to old code. Not verified.

### 3.6 Effort (rough, not scoped)

| Work | Estimate |
|---|---|
| Schema, SQL statements, catalog mapping | a few days |
| Extraction migration and tests | 1–2 weeks |
| Moving both CRD writers to the new version | 1–2 weeks |
| Association shape change (legacy and etcd paths) | 1–2 weeks |
| v1 adapter over the new document | several weeks; the largest item |

Not included: the v2 vmodl and handler (§5), the capability gate.

---

## 4. vAPI

### 4.1 Two endpoints over one row

- **v2 (new):** generated from the CRD (§5). Its `configSpec` holds only leftover fields (§4.6).
- **Identity and naming on v2:** three separate fields. `id` is VC's identifier and becomes the class's `spec.externalID`; a dedicated `name` becomes the Kubernetes `metadata.name` that VMs reference in `spec.className` (DNS-1123, validated synchronously); `description` stays free text. For existing classes the name equals the ID (design doc §4.2). The uniqueness scope for `name` (vCenter-wide, or per namespace it is attached to) is open (O11).
- **v1 (existing `VirtualMachineClasses`):** kept, hand-written, as an adapter. GET shows `default` for ranged fields and builds a full `configSpec` that includes the elevated fields; UPDATE moves elevated fields out of the incoming `configSpec` into the typed fields. Without that, a v1 client that reads a class and writes the `configSpec` back would clear every elevated field, because a v1 UPDATE replaces the whole blob.
- Both endpoints are gated by a new wcpsvc capability (`capabilities.Checker.IsActivated`, returning `Unsupported` when off; precedent `kubelifecycle/cluster_network.go::validateNetworkCompatibleWithSupervisor`). This works because wcpsvc upgrades before the Supervisors.

### 4.2 Unchanged and clear for every field

vAPI `Optional` has no separate null, so a single structure can't express set, unchanged and clear. The existing convention answers it: `Instances` has both `update` (PATCH) and `set` (PUT).

- **`update`:** unset = unchanged; an empty list clears it; a set structure or enum replaces the whole value (as setting `devices` does today).
- **`set`:** the whole spec is replaced; unset = cleared back to the default.

The v2 endpoint gets both. (`VirtualMachineClasses` today has only `update`.)

### 4.3 Validation

- **Asynchronous, as today:** the Supervisor's webhook rejects the CR and wcpsvc records the error in the class's `Info.messages` (severity `ERROR`), per Supervisor. This stays correct when Supervisors run different versions.
- **Synchronous structural checks, generated:** required fields, min/max, enum values and `min ≤ default ≤ max`, emitted into the handler from the schema's own validation markers, so a structurally invalid document never reaches vcdb.
- **No shared validation code** between vm-operator and wcpsvc.

### 4.4 The association in `Instances`

Following the content-library precedent (`contentLibraries` went from `Set<ID>` in `VMServiceSpec`, 1.3, to a top-level `List<ContentLibrarySpec>` with per-item settings, 2.0):

```java
@Released(version="9.x")
class VMClassSpec {
    ID vmClass;
    /** Unset = all namespace zones, including future ones. Empty = none. */
    Optional<Set<ID>> zones;
    /** Unset = same as zones. Empty = governs nothing. A list must be within zones. */
    Optional<Set<ID>> governedZones;
}
// CreateSpec, CreateSpecV2, SetSpec, UpdateSpec:
@Released(version="9.x")
Optional<List<VMClassSpec>> vmClasses;
// Info, InfoV2: the resolved list
```

Rules, copied from how the two content-library fields coexist:

- Both the old `vmServiceSpec.vmClasses` and the new list may be set. They merge; a class in both appears once; the new field's settings win.
- A class added through the old field means "all zones".
- Removing, through the old field, a class that has settings only the new field can express (an explicit zone list) fails with `InvalidArgument`.
- **Synchronous checks:** the zones belong to the namespace and aren't marked for removal; every zone where the class has `vmReservations` is included.
- **Asynchronous:** hardware feasibility (`status.zones`).
- When a zone is removed from a namespace, it is pruned from explicit lists and a message is recorded.
- On `update`, the whole `vmClasses` list is replaced, so "unset means all" inside an element doesn't clash with "unset means unchanged" at the top level.

### 4.5 Zones: unset, empty, list

| Value | Availability (`zones`) | Governance (`governedZones`) |
|---|---|---|
| unset | All namespace zones, including zones added later | Same as availability |
| `[]` | None: attached, but no new VM can select it | None |
| `[a, b]` | Only those zones | Only those zones; must be within availability (a class can only govern where it is available) |

**Implementation rule:** every layer must keep unset and empty apart.

- **Go types:** `*[]string` with `omitempty`, everywhere: CRD types, wcpsvc, nsop, the vcdb JSON. **Not `[]string`:** apimachinery's `equality.Semantic.DeepEqual` treats a nil list and an empty list as equal (`third_party/forked/golang/reflect/deep_equal.go`, the `equateNilAndEmpty` path), and `controllerutil.CreateOrUpdate` skips the write when the objects are `DeepEqual`. With a plain slice, a change between "all" and "none" would never be written. Pointers compare unequal (nil vs. non-nil).
- **API server:** keeps `[]`; it prunes only `null` values, as long as the schema isn't `nullable`.
- **Version conversion:** conversion-gen keeps pointers; needs a round-trip test for all three states.
- **vAPI:** an unset `Optional<Set<ID>>` and an empty set are distinct on the wire.

**Risk (medium-low):**

| Where | Risk | Why |
|---|---|---|
| Our writers (wcpsvc, nsop, vcdb, conversion) | Low | We control the code; pointer types plus one three-state test catch mistakes. |
| Clients using our Go types | Low | The pointer keeps `[]`. |
| kubectl, server-side apply, Python, Terraform | Low–medium | These generally keep `[]`. Some GitOps diffing treats `[]` and missing as equal, which makes diffs noisy but loses nothing. |
| A client that silently drops `[]` | The real failure | "None" becomes "all". For governance that fails closed (governs more than intended). **For availability it fails open**: a class meant to be unavailable becomes selectable everywhere. Only affects classes that deliberately use "none", written by such a client — in practice Phase 2 tenant classes authored with kubectl. |

Mitigations: pointer types throughout; one test that pushes all three states through JSON, conversion, `CreateOrUpdate`, the vcdb JSON and the vAPI; an e2e test; documentation for kubectl users; `status.zones` shows effective availability, so a collapse is visible.

Kubernetes API conventions discourage giving nil and empty lists different meanings. This is a deliberate exception, justified by the table above.

### 4.6 `configSpec` in v1 and v2

- v1 `configSpec`: the full blob, built by the adapter, including elevated fields.
- v2 `configSpec`: only fields with no typed home. Elevated paths are not allowed in it.
- Both carry `@Vmodl1Type("VirtualMachineConfigSpec")`. Because they are on separate endpoints, the two meanings never share one structure. The remaining edge: a client that copies a v1 `configSpec` into a v2 create sends elevated fields, which the v2 write path must handle (open item O3).

---

## 5. Build-time vmodl generator

### 5.1 Pipeline

```
Go types + markers  (vm-operator api module, pinned in wcpsvc go.mod)
  -> emitter          complete OpenAPI 3 document:
                      named components, $ref, CRUD paths,
                      x-vmw-* extensions, int-or-string rewrite
  -> openapi-compiler --profile vmodl2   (existing, vAPI team)
  -> annotation step  (only if the vAPI team doesn't add support)
  -> .vmodl           reviewed and committed
  -> checks           completeness test; diff vs released baseline
```

Only the class resource is generated. The namespaces API (`Instances`, `VMClassSpec`) stays hand-written. Storage mapping is never the generator's concern: the class handler stores the class document in `vm_class_class_configs`, and the hand-written namespaces handler stores associations in `workload.vm_classes`.

### 5.2 `openapi-compiler` today

Location: `vapi-core/idl-toolkit/openapi-compiler` in tera, maintained by the vAPI team. `OpenApiCompilerApp` extends `IdlCompilerApp`, whose `--profile vmodl2` output writes `.vmodl` files through `VmodlIdlWriter`. Today's users generate client SDKs for REST-native APIs (VMware Cloud `com.vmware.atlas.*` specs, VCF Fleet LCM through the `openapi_java`/`openapi_python` Bazel rules); none is a vCenter-hosted vAPI service. The wcpsvc vmodl, including `ContentLibrarySpec` 2.0, is all hand-written and built with `vmodl2_sdk`.

Type mapping (`IdlTypeBuilder`, `IdlStructureBuilder`, `OpenApiCompiler`):

| OpenAPI input | vmodl output |
|---|---|
| `integer` | `long` (always) |
| `number` | `double` |
| `boolean` | `boolean` |
| `string` (no format, or `uuid`/`ipv4`/`hostname`/…) | `String` |
| `string` with `byte`, `uri`, `date-time`, `password` | `binary`, `URI`, `DateTime`, `Secret` |
| `string` with any other format | error |
| `array` | `List<T>` |
| `object` with `additionalProperties` | `Map<String, T>` |
| `object` with no properties | `DynamicStructure` |
| inline `object` with properties | a structure with an auto-generated name (parser flatten) |
| `$ref` | named structure |
| `oneOf` + discriminator | polymorphic structure |
| `enum` | `String`, with the constants listed in docs |
| schema with no `type` and no `$ref` | error |
| not in `required` | `Optional<T>` |
| `default` | ignored |

Supported extensions: `x-vmw-vapi-servicename`, `-methodname`, `-errors`, `-codegenconfig`; `x-vmw-compiler-skip`.

### 5.3 Gaps to cover

| Gap | Needed? | Plan | Fallback |
|---|---|---|---|
| **The input must be a complete OpenAPI document** (`paths`, `components`); a CRD YAML is one inline `openAPIV3Schema` per version | Yes | The emitter builds it from the Go types: components named after Go types (not auto-generated flatten names, which would become API names), `$ref`s, CRUD paths with `x-vmw-vapi-servicename`/`-methodname` | — |
| **int-or-string.** The CRD emits `resource.Quantity` as `anyOf: [integer, string]` + `x-kubernetes-int-or-string` with no `type`, which the compiler rejects. kube-openapi v2 emits `type: string` for `Quantity`; `IntOrString` gets format `int-or-string`, which the compiler also rejects | Yes | The emitter rewrites to `type: string` (the canonical quantity string) | Unit-typed `Long` per field via a marker (vSphere convention, e.g. `ZoneSpec.memoryLimitMiB`); lossy, requires whole-unit validation. Decision open (O5) |
| **`@Released`** | **Required** — every vCenter vAPI element carries a release version. An element without one inherits from its enclosing interface (e.g. `VirtualMachineClasses.CreateSpec.id`), so the first release needs only an interface-level annotation; later additions need per-field ones | Computed by the tool from the released baseline: a field already in the baseline keeps its version, a new field gets the current release passed to the tool. No per-field markers | — |
| **`@Vmodl1Type`** | Yes, one field (`configSpec`). The existing vmodl declares it `@Vmodl1Type("VirtualMachineConfigSpec") Optional<DynamicStructure>`; the compiler's `DynamicStructure` output is already the right shape, only the annotation is missing | Marker `+vmodl:type=VirtualMachineConfigSpec` | — |
| **`@Resource`** | Yes, a few ID fields (e.g. `Info.namespaces`, `Info.vms`, the instance-storage policy ID) | Marker `+vmodl:resource=<kind>` | — |
| **`@Feature`** | No. The handler's capability check gates the endpoint | — | — |
| **Named enums** | Desired | Map a named (`$ref`) enum schema to an IDL enum | `String` with allowed values in docs and a generated handler check (D10) |
| **Validation constraints** (min/max/pattern/CEL) are not carried into vmodl | Yes, as synchronous structural checks (§4.3) | Emitted into the handler from the same markers | — |
| **`update` + `set` + `Info` from one Spec** | Yes (§4.2) | Create = Spec optionality; update = all `Optional` (PATCH); set = full replace; Info = spec + `id` + `messages` + `namespaces` + `vms`, no per-namespace status | — |
| **Acronym field names** (`vGPU`, `dynamicDirectPathIO`) | Check | `@CanonicalName` via marker where the derived name is wrong | — |
| **Completeness** | Yes | A test that every CRD field is generated or explicitly skipped (`+vmodl:skip=association`, `+vmodl:skip=derived`) | — |

**How the annotations get in:** ask the vAPI team for one generic passthrough extension (e.g. `x-vmw-vmodl-annotations`: a list of raw annotations attached to a schema or property, copied into the output) plus named enums. If that can't be committed in time, a small deterministic step after `openapi-compiler` inserts the few annotations from the same markers; the output is still reviewed. The choice between that and a direct Go-to-vmodl emitter is open (O4).

### 5.4 Markers

Markers are inherited from package, to type, to field; a field can override. Example:

```go
// +vmodl:resource=VirtualMachineClasses
type VirtualMachineClassSpec struct {
    Hardware VirtualMachineClassHardware `json:"hardware"`

    // +vmodl:type=VirtualMachineConfigSpec
    ConfigSpec json.RawMessage `json:"configSpec,omitempty"`

    // Per-namespace; carried by the hand-written namespaces API.
    // +vmodl:skip=association
    Zones *[]string `json:"zones,omitempty"`

    // Derived from the capacity store.
    // +vmodl:skip=derived
    ReservedProfileID string `json:"reservedProfileID,omitempty"`
}
```

Kubernetes version names (`v1alpha7`) are kept out of vmodl names.

---

## 6. CRD writers

- **Both writers move to the new CRD version** (wcpsvc `copyIntoKubeObject`/`CreateOrUpdate`; nsop's catalog copy and its v1 `GetVmClass` fallback). Otherwise every resync drops the new fields.
- **The version is chosen per Supervisor, by capability.** wcpsvc upgrades first, and one vCenter can manage Supervisors on different versions. Writing the new version to a Supervisor that doesn't serve it fails. There are two hops on etcd-backed Supervisors: wcpsvc writes the `vmware-system-vmop` catalog, then nsop copies from it.
- **Every writer compares with pointers intact** (§4.5): a `DeepEqual`-based skip must not hide a change between unset and `[]`.

---

## 7. Future: Supervisor 2.0 and etcd-only (parked)

Supervisor 2.0 keeps per-Supervisor etcd as the source of truth; wcpsvc keeps a registry of Supervisors and copies selected CRs back into vcdb keyed by `supervisor_id` (`kubelifecycle/reflector/`), for a combined read view. VM classes are not among the copied objects today. If the long-term direction is etcd only, a class shared across a vCenter becomes a distribution problem (copy into each Supervisor), with vcdb as a read model. That doesn't fit "a class exists in VC without a Supervisor", so D1 stands for this feature. Revisit when Supervisor 2.0 rolls out. Who writes `VirtualMachineClass` objects on Supervisor 2.0 is not yet identified (O10).

---

## 8. Open items in this area

- **O1.** v1 writes against ranged classes: a v1 UPDATE that touches `cpuCount` collapses the range to a single value (Option 3). Risk: a v1 UI user silently overrides a range author. Also: if the vSphere UI or VCFA send full UpdateSpecs, every edit touches every field; mitigation "a value equal to the current projection means unchanged". Needs someone who can confirm what those clients send.
- **O3.** Elevated fields arriving in a v2 `configSpec` (e.g. copied from a v1 GET): elevate automatically when the typed field is unset, drop when equal, reject when they conflict (matches wcpsvc's existing rejection of a mismatched `NumCPUs`), or reject always.
- **O4.** Generator route: `openapi-compiler` plus the vAPI-team extensions, or the post-compile annotation step, or a direct Go-to-vmodl emitter. Depends on the vAPI-team conversation.
- **O5.** `Quantity` mapping: `String` (lossless, generic) vs. unit-typed `Long` (vSphere convention).
- **O6.** Where the generator lives and who owns it. vmodl is internal (tera) and the CRD is public (vm-operator); proposal: tool and output in tera, input from the vm-operator `api` module version pinned in wcpsvc's `go.mod`, regenerated on bump, with the completeness and baseline checks in CI.
- **O7.** The elevation registry: one table (ConfigSpec path ↔ typed path + converter) driving the webhook, the vcdb migration (per-row elevation level), the v1 projection, the overlap check and the vmodl. wcpsvc elevates VC-managed classes; the webhook elevates Kubernetes-authored ones; elevation is tied to the target CRD version.
- **O8.** Old-column cleanup: which columns to drop (`config_spec_xml_b64` now; `config_spec` once every row is migrated and the v1 adapter builds it from the document), and in which release.
- **O9.** Whether `externalID` needs its own indexed column.
- **O10.** The `VirtualMachineClass` writer on Supervisor 2.0.
- **O11.** Uniqueness scope of the v2 `name` field: vCenter-wide (simplest; checked at create) or only among classes attached to the same namespace (checked at association time).

---

— Faisal + Claude
