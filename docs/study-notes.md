# Study notes: what this project is, what you built, what is left

These notes are for you. They start from zero. Read them in order before a call with Sridhar. The weekly wiki is the short log. This file is the explanation of that log.

Repo: `opi-nvidia-dpf-adapter`
Wiki: https://github.com/kartikeyg0104/opi-nvidia-dpf-adapter/wiki

---

## 1. The words

**Server.** A computer in a rack. You SSH into it and run programs there. The lab servers are named dh1, dh2, dh3, dh9.

**NIC.** The normal network card in a server. It only moves packets.

**DPU.** A Data Processing Unit. A smarter network card. It has its own CPU, its own memory, and its own small operating system. It can do firewall, encryption, and storage work on the card, so the server's main CPUs stay free. It is not a GPU. A GPU draws graphics and runs CUDA. None of the lab hosts you can log into has a compute GPU.

**BlueField.** NVIDIA's DPU. dh2 has a BlueField-2. The card has 16 GB of its own memory. That memory is on the card, not the server's 256 GB.

**Pensando DSC / Elba.** AMD's DPU. dh1 has a Pensando DSC2-100. PCI vendor id `0x1dd8`.

**Marvell.** A third DPU vendor. dh3 has a Marvell CN106. Marvell does not have its own Kubernetes objects in this project. Discovery knows the PCI ids. No mapping YAML reads them yet.

**Kubernetes (k8s).** Software that runs programs in containers on a cluster and keeps them running. A **pod** is one running copy. A **Deployment** keeps a pod up. A **DaemonSet** runs one pod on each node.

**k3s.** A small Kubernetes. dh2 has been running k3s for about 42 days. The node name is `dh2`.

**kind.** Kubernetes inside Docker on a laptop. Used for tests. Not the lab.

**CRD.** Custom Resource Definition. It teaches Kubernetes a new kind of object. After you install a CRD, you can `kubectl apply` objects of that kind.

**Operator.** A program that watches those objects and does the work. You do not SSH in and configure the card by hand. You apply an object, and the operator reacts.

**Reconcile.** The operator's loop. Read what you asked for, read what exists, make them match. It runs again and again.

**API group / version / kind.** The full name of an object type. Example: kind `DataProcessingUnit`, group `config.openshift.io`, version `v1`.

**YAML.** The text file you apply. It says which kind, and what the fields are.

**Namespace.** A folder of objects inside the cluster. Your operator runs in `opi-nvidia-dpf-adapter-system`.

**Status.** The part of an object the operator writes back to say what happened. `Ready=True` means healthy. `Ready=False` means not.

**Condition.** One named fact in status, such as `Ready` or `DPFReady`. The controller updates a condition by its name. Two writers using the same name overwrite each other.

**Finalizer.** A mark that means "do not delete this object until I finish cleanup." Your controller uses one so the vendor objects it created are deleted too.

**Owner reference.** A link from a child object to its parent. Kubernetes deletes children when the parent goes away. This does not work across namespaces, so your controller also tracks children with annotations and deletes those itself.

**gRPC.** A way for two programs to call each other. Your VSP speaks gRPC on a Unix socket.

**Unix socket.** A file that programs use as a phone, instead of a network port. The DPU daemon dials `/var/run/dpu-daemon/vendor-plugin/vendor-plugin.sock`.

**VSP.** Vendor-Specific Plugin. The small program on the node that looks at the real card and answers the DPU daemon. Yours is `cmd/vsp`.

**PCI.** The slot the card is plugged into. Each device has a vendor id and a device id. NVIDIA is `0x15b3`. AMD Pensando is `0x1dd8`. Marvell is `0x177d`.

**VPD.** Vital Product Data. A small label on the card: product name, serial number, firmware.

**sysfs.** Linux files under `/sys` that describe hardware. The VSP reads the serial from there.

**CEL.** A small expression language inside the mapping YAML. Used when a field is not a straight copy. Example: if this side is a DPU, write `dpu`, otherwise write `smartnic`.

**JSONPath.** A path through a YAML/JSON object. `spec.nodeName` means the field `nodeName` inside `spec`.

**Server-side apply.** How the controller writes objects. It sends the fields it owns. It does not wipe fields another program owns.

**RBAC.** Who is allowed to do what in Kubernetes. Your controller has a service account. A ClusterRole lists which object types it may get, create, update, and delete.

**controller-gen.** A tool that reads special comments in Go and writes RBAC YAML.

**Conformance test.** A test that applies real CRDs to a fake API server and checks the objects that come out. If this fails, the mapping is wrong.

**Mutation test.** You break the feature on purpose. If the test stays green, the test was not actually checking that feature.

**Stub.** A CRD you wrote yourself because the real one is not available. It is enough for tests. It is not proof that the real AMD operator accepts those objects.

---

## 2. What the project is

OPI wants one way to describe a DPU, no matter who made the card. NVIDIA and AMD each have their own operator and their own object names. Your program sits in the middle.

Someone applies an OPI object. Your operator creates the vendor objects. The vendor's own operator then programs the card.

NVIDIA code stays in this repo. It is not added inside `opiproject/dpu-operator`. Intel and Marvell plugins live inside that upstream repo. NVIDIA does not, because NVIDIA's control plane is different: you declare objects, you do not call a function to create a network function.

The rule Sridhar and the TSC care about:

**The translation is data, not a pile of Go if-statements.**

A YAML file says "read this OPI field, write that vendor field." The Go program is an interpreter. It loads the YAML and does what it says. Adding a vendor should be a new YAML file, the vendor's CRDs, a test row, and sometimes one PCI table entry. It should not be a new controller.

---

## 3. The objects, in plain words

### What a person creates (OPI)

These CRDs come from `opiproject/dpu-operator`. You do not own the API.

| Object | What it means |
|---|---|
| `DataProcessingUnit` | "This node has this DPU card." Group `config.openshift.io`. |
| `ServiceFunctionChain` | "Run these network functions." Each entry can point at a Helm chart. |
| `DpuOperatorConfig` | Cluster-wide config, including which vendor operator is installed. The lifecycle manager watches this. |

### What you create for NVIDIA (DPF)

DPF is NVIDIA's operator, from the DOCA platform. Your mapping turns one `DataProcessingUnit` into four objects:

| You create | Meaning |
|---|---|
| `DPUDevice` | The physical card. |
| `DPUFlavor` | The personality of the card. |
| `BFB` | The firmware image (BlueField boot file). |
| `DPU` | "Attach that card to that node and use that flavor and firmware." |

A `ServiceFunctionChain` becomes one `DPUService` for each Helm chart in the list. A bare container image is skipped. DPF has a field for a chart, not for a raw image.

### What you create for AMD

The same OPI `DataProcessingUnit` becomes four different objects. The names are different on purpose. If they were just renamed NVIDIA objects, the test would not prove the engine can do real translation.

| NVIDIA | AMD |
|---|---|
| `DPUDevice` | `DSCDevice` |
| `DPUFlavor` | `DSCProfile` |
| `BFB` | `DSCFirmware` |
| `DPU` | `DSCNodePolicy` |

Examples of real translation, not renaming:

- NVIDIA firmware URL is one string, `BFB.spec.url`. AMD nests it: `DSCFirmware.spec.image.url`.
- NVIDIA stores a device name as a string. AMD stores an object reference, `deviceRef.name`.
- NVIDIA `nodeEffect.noEffect: true` becomes AMD `drain.enabled: false`. The boolean is flipped.
- AMD mode is calculated. If `spec.isDpuSide` is true, write `dpu`, else write `smartnic`.

---

## 4. The two programs you ship

| Binary | Source | Job |
|---|---|---|
| manager | `cmd/main.go` | Runs in the cluster. Loads `config/mappings/*.yaml`. Watches OPI objects. Creates vendor objects. Copies status back. Deletes children on cleanup. |
| vsp | `cmd/vsp` | Runs on the node. Reads the card. Writes serial and firmware annotations onto the OPI object. Serves the daemon's socket. |

The manager never invents a serial number. If the serial is missing, the field stays empty. The VSP is supposed to stamp it.

There is a third piece inside the manager: the **lifecycle manager** (`pkg/lifecycle`). It does not translate cards. It says whether the installed vendor operator version is one you have tested.

### How one card flows

1. Someone applies a `DataProcessingUnit`.
2. The VSP sees it, looks at PCI, writes annotations (serial, firmware URL, flavor, firmware name).
3. The manager loads every mapping whose source kind is `DataProcessingUnit`. Today that is the NVIDIA file and the AMD file. Both run. A `when` guard decides who is allowed to create objects.
4. The NVIDIA mapping creates `DPUDevice`, `DPUFlavor`, `BFB`, and `DPU` only if the product name looks like BlueField.
5. The AMD mapping creates the four DSC objects only if the product name matches `DSC` or `Pensando`.
6. NVIDIA's operator, or AMD's operator, sees those objects and programs the card. That part is not your code.
7. Status on the child objects is copied back onto the OPI object.
8. If the OPI object is deleted, your finalizer deletes the children, then lets the delete finish.

### Where the YAML lives

| File | Watches | Creates |
|---|---|---|
| `config/mappings/dataprocessingunit.yaml` | `DataProcessingUnit` | the four NVIDIA objects |
| `config/mappings/amd-dsc200.yaml` | `DataProcessingUnit` | the four AMD objects |
| `config/mappings/servicefunctionchain.yaml` | `ServiceFunctionChain` | one `DPUService` per chart |
| `config/vendor-template/fieldmapping.yaml` | a fake vendor | used only to show a new vendor how to start |

Each field in a mapping is exactly one of:

- `from` — copy a path.
- `cel` — compute it.
- `value` — write a constant.

A `when` on an emit skips that object when the expression is false. A `status.when` skips the whole status write.

---

## 5. The lab, as it is

You reach the lab only through the VPN on Kartikey's MacBook Pro. Other Macs hop through that Mac. They do not install the VPN.

| Host | Address | Machine | Card | RAM | CPUs | SSH with your key |
|---|---|---|---|---|---|---|
| dh1 | <your-lab-hosts> | Dell PowerEdge R650 | AMD Pensando DSC2-100 | 128 GB | 2× Xeon Gold 6326, 64 threads | yes. Hostname inside the OS is `opi`. |
| dh2 | <your-lab-hosts> | HPE DL360 | NVIDIA BlueField-2 | 256 GB | 2× Xeon Gold 6326, 64 threads | yes |
| dh3 | <your-lab-hosts> | HP RL300 | Marvell CN106 | 256 GB | 1× Arm Neoverse-N1, 80 cores | yes |
| dh9 | <your-lab-hosts> | ASUS ESC8000A-E12P | none seen | 384 GB | 2× AMD EPYC 9354, 128 threads | yes |
| dh4 | <your-lab-hosts> | Dell PowerEdge R760 | Intel Dayton Peak | unknown | unknown | no |
| dh5 | <your-lab-hosts> | Dell R760ax | NVIDIA BlueField-3 | unknown | unknown | no |

The graphics chips on these machines (Matrox, ASPEED) are only for the remote console. They are not GPUs you can run models on.

dh1 has the Pensando card and no Kubernetes. No AMD operator has been installed there. Reading the card is allowed. Calling that a live AMD test is not.

### What is running today (20 October 2026)

On **dh2**, in namespace `opi-nvidia-dpf-adapter-system`:

- `opi-nvidia-dpf-adapter-controller-manager` is Running, 1/1. It holds the leader lease. It registered three mappings: `amd-dsc200`, `dataprocessingunit`, `servicefunctionchain`. The NVIDIA DataProcessingUnit controller, the ServiceFunctionChain controller, and the lifecycle manager have started.
- `opi-nvidia-dpf-adapter-nvidia-vsp` is Running, 1/1. It is the **mock**. Serial `MTEXAMPLE0001`. It is not scanning the physical BlueField. The default DaemonSet does not pass `--pci` and does not mount the host PCI bus. The file says so: mock for kind, replace with `--pci` on real hardware.
- The AMD controller is loaded and keeps logging errors. The cluster does not have `dpu.amd.com/v1alpha1` CRDs (`DSCDevice`, `DSCProfile`, `DSCFirmware`, `DSCNodePolicy`). Those CRDs in the repo are stubs. They were not installed on dh2.

k3s itself was already running. The OPI and NVIDIA CRDs were already installed (7 September). The adapter pods were not. They were built from this repo and applied on 20 October.

---

## 6. What you did, week by week

Dates are the wiki weeks.

### Week 1 — 5–11 August 2026. Setup and the rule

You cloned `opiproject/dpu-operator`, NVIDIA `doca-platform`, and the lab repo. You installed Go, Docker, kubectl, kubebuilder, kustomize, controller-gen, golangci-lint, and kind.

You read three things:

- How OPI describes a card (`DataProcessingUnit`, `ServiceFunctionChain`).
- How NVIDIA describes a card (`DPUDevice`, `DPUFlavor`, `BFB`, `DPU`, `DPUService`).
- How Intel and Marvell already attach to the DPU daemon: a plugin on a Unix socket.

Two decisions came out of that reading and never changed:

1. NVIDIA-specific code lives in this companion repo, not inside `dpu-operator`.
2. The field mapping is YAML data, not `switch kind` with hardcoded struct copies.

You brought up a kind cluster and installed the OPI and DPF CRDs. No blocker.

### Week 2 — 12–18 August. The interpreter exists

You scaffolded this repo the kubebuilder way: `cmd/main.go`, RBAC, Dockerfile, Makefile, CI for lint, unit test, and e2e. About 4.5k lines.

You wrote `pkg/mapping/`:

- `spec.go` describes a FieldMapping.
- `load.go` reads `config/mappings/*.yaml` and skips `kustomization.yaml`.
- `path.go` does JSONPath.
- `cel.go` evaluates CEL.
- `engine.go` is the interpreter.

There is no per-kind Go path. The same engine handles both YAML files.

You wrote a mock VSP (`pkg/discovery/mock.go`) so a laptop with no card can still run. It stamps `provisioning.dpu.nvidia.com/serial-number`.

Upstream, you added a Helm chart field on `ServiceFunctionChain` network functions, then refreshed the vendored API in this repo.

Tests: mapping package with no cluster, discovery annotator, envtest, `make lint` and `make test` clean.

Blocker then: no BlueField yet, so the mock came first.

### Week 3 — 19–25 August. Phase 0 done. Real card, real socket

`pkg/discovery/pci.go` walks the PCI bus and reads a BlueField serial from sysfs. The mock remains for laptops:

```sh
# no card
go run ./cmd/vsp --metrics-bind-address=0 \
  --node-name=kind-worker --serial-number=MTEXAMPLE0001 \
  --bfb-url=https://example.invalid/fw.bfb

# real card
go run ./cmd/vsp --metrics-bind-address=0 --pci --node-name="$(hostname)"
```

`pkg/vsp/grpc_server.go` serves `LifeCycle.Init` and `GetDevices` on the vendor-plugin socket, using the upstream `dpu-api` types. No protobuf compiler in this repo. `go.mod` replaces the OpenShift import path with `opiproject/dpu-operator`. Written up in `docs/vsp.md`.

You synced the ServiceFunctionChain CRD into the upstream bundle so both repos agree.

You proved the path three times:

- Unit tests against a fake sysfs tree, and a test that dials the socket.
- kind: mock VSP, apply a DataProcessingUnit with no serial, watch the VSP patch the serial, watch the translator emit the four NVIDIA objects.
- Lab: same flow, `--pci`, serial matches the real card, daemon connects, emitted objects carry the real serial.

### Week 4 — 26 August–1 September. Phase 1 done. Status and delete

Status flowing back became mapping data too. The YAML says how child status is copied onto the OPI parent. Documented in `docs/mapping-spec.md`.

On delete, the controller holds a finalizer, deletes the children it created, including children in other namespaces, then removes the finalizer.

You moved the controllers from `internal/controller` into `pkg/translation` so another repo can import them. You added `pkg/lifecycle/lifecycle_controller.go` and wired both into `cmd/main.go`.

Upstream did not yet have a status subresource on ServiceFunctionChain. Mirroring needs it. You carried that as `upstream-sfc-status.patch`.

Tests, about 900 new lines: status merge, finalizer, controller, engine. A conformance suite under `test/conformance/` applies real CRDs. It caught invalid CRDs in both mapping files. kind and the lab both did create, status comes back, delete, nothing left behind.

Blocker: the upstream status patch was not merged yet.

### Week 5 — 2–8 September. Stop and re-check

No new feature. PR #6 put the finished Phase 1 work on `main`. You re-ran the VSP tests and the translation tests: forward create, status copy, cross-namespace cleanup, and the case where a child CRD has no status and must not block readiness. `make test` and `make lint` green.

The point of the week: a base solid enough to put a second vendor on.

### Week 6 — 9–15 September. Pick AMD before writing the code

One vendor does not prove the engine is general. A second vendor whose objects are renamed NVIDIA objects would also prove nothing.

You chose AMD Pensando because the object model is different.

The requirement, written before the code: adding that vendor must be a change to the YAML only. A forced Go change in the translation controller would mean the premise failed.

### Week 7 — 16–22 September. AMD YAML, and the lab proved a bug

Phase 1 ran live on **dh2** with k3s: create, status, cleanup, against real OPI and DPF CRDs.

You added `config/mappings/amd-dsc200.yaml`. No new reconciler. No vendor branch in `pkg/` or `cmd/`.

Both mappings watch every DataProcessingUnit. `cmd/main.go` starts one controller per mapping. `when` selects the vendor. If `dpuProductName` is empty, the NVIDIA guard treats it as BlueField, so old objects keep working.

The real card on dh1 does not say `DSC2-100`. The PCI label says `Pensando DSC2-100 100G 2p QSFP56 DPU`. A guard of `startsWith('DSC')` was false. The mapping would have created nothing, and every local test would still have passed, because the tests used the short name. You changed the guard to `matches('DSC|Pensando')`. Reverting it fails the suite.

You harvested dh1 read-only with `hack/fetch-amd-crds.sh`. Confirmed: PowerEdge R650, Pensando DSC2 Elba, PCI `0x1dd8:0x1002`, firmware `1.46.0-E-28`, serial from VPD. Nothing was installed on that host.

You found the SSH hang. The VPN MTU is 1399. The default key-exchange packet is larger, so SSH waits forever with no error. `KexAlgorithms curve25519-sha256` keeps the packet small. The fetch script sets that itself.

Two gaps, left on purpose:

1. dh1 has no Kubernetes and no AMD operator. The CRDs in `test/conformance/crds/amd/` are stubs you wrote, marked `opi.nvidia.com/stub: "true"`. Installing those stubs and calling it a hardware pass would be fake.
2. `pci.go` only accepted NVIDIA `0x15b3`, so the VSP could not see an AMD card and could not stamp `dpu.amd.com/serial-number`.

Also: the controller updates a condition by its type. Two mappings cannot both write `Ready`. AMD was already writing `DSCReady`. Emission isolation was tested. Status isolation was not. A healthy AMD card could still show `Ready=False` from the NVIDIA mapping, and `kubectl get` prints that column.

### Week 8 — 23–29 September. Discovery learns AMD

The translation YAML was vendor-neutral. The program that feeds it was not. One line skipped every card whose vendor was not NVIDIA.

Three commits:

1. **Multi-vendor PCI.** Add AMD `0x1dd8` and Marvell `0x177d`. Matching the vendor alone is unsafe. The AMD card also exposes PCI bridges with the same vendor id. A bridge has no serial. Your code treats a missing serial as a hard error, on purpose, so a serial is never invented. A vendor-only match would abort the scan for every card on the machine, including the NVIDIA one. AMD and Marvell now match known device ids only:

   - `0x1dd8:1002` DSC Ethernet
   - `0x1dd8:1004` DSC management
   - `0x177d:b900` and `0x177d:a0f7` Marvell, taken from upstream `marvell-dpu.go`

   NVIDIA keeps its old fallback so existing behavior stays.

2. **Annotation keys follow the vendor.** Finding the card was not enough. The annotator still wrote the NVIDIA key, and the AMD YAML never reads that key. Discovery would succeed and translation would still fail, with an empty string and no error.

   | Idea | NVIDIA key | AMD key |
   |---|---|---|
   | Serial | `provisioning.dpu.nvidia.com/serial-number` | `dpu.amd.com/serial-number` |
   | Firmware URL | `dpu.nvidia.com/bfb-url` | `dpu.amd.com/firmware-url` |
   | Flavor | `dpu.nvidia.com/flavor` | `dpu.amd.com/profile` |
   | Firmware name | `dpu.nvidia.com/bfb` | `dpu.amd.com/firmware` |

   There is no fallback to the NVIDIA keys. An unknown vendor fails with the vendor id in the message. Marvell's keys are reserved. No mapping reads them yet.

3. **Mock any vendor.** `cmd/vsp --vendor=amd` or `--vendor=0x1dd8`. `--bfb-url` and `--bfb-name` still work as old names.

Tests: an AMD card found by VPD, bridges skipped, a Marvell card, a mixed bus where each supported card is found once. `TestMappingYAMLsMatchVendorAnnotationKeys` checks both directions. `TestMarvellKeysAreStillUnused` fails if a Marvell mapping appears and is not added to that table.

You also built a slide deck for the maintainer review: unmodified DPU operator, Phase 1 adapter, this Phase 2 Go, with diagrams and a speaker script.

Blocker unchanged: official AMD DSC Operator bundle.

### Week 9 — 30 September–6 October. Stop fighting over Ready

A maintainer review has its own Phase 0, 1, and 2. Those are not your project phases. Three of the review's Phase 0 items were already done: companion repo, mapping as data, hybrid pattern. The fourth was a merge blocker:

> one condition, owned by the translation controller, never fighting the per-node daemon's `Ready`.

Upstream's daemon already writes `Ready` on the same `DataProcessingUnit`. Your mappings wrote `Ready` too. Same type, so they overwrote each other every reconcile.

Worse: the NVIDIA status block has no vendor guard at this point in the story, so it still ran on an AMD card, saw zero children, and set `Ready=False`. `kubectl get` shows `Ready`. A healthy AMD card looked failed.

Fix: `Ready` is reserved for the daemon.

- NVIDIA mappings write `DPFReady`.
- AMD writes `DSCReady`.
- `Spec.Validate` rejects a condition named `Ready`, any capitalization. The mapping fails to load. The controller will not start. That is the merge block, not a comment.
- A test walks the shipped mappings and also fails if two mappings claim the same condition type on one source.
- Conformance checks the running controller leaves `Ready` absent.

You did not take the `status.when` engine change this week. Reserving the type fixed the printer column without an engine change. The leftover was cosmetic then: an AMD card could still get `DPFReady=False` from a mapping that owns nothing. Nothing reads the other vendor's condition.

You also turned the vendor guide into a scaffold, because the AMD port had already hit shape mistakes and a bad guard that no document caught.

`make new-vendor VENDOR=acme GROUP=dpu.acme.com MODEL=A100` copies `config/vendor-template/fieldmapping.yaml`, makes a CRD directory, and prints the steps a script cannot do. The template shows a `matches()` guard, a nested path, a flipped boolean, and an enum from a bool. Its guard must reject BlueField and Pensando. The first run of the generator test caught two real bugs: BSD `sed` left a `Kind` unreplaced, and a guard collapsed to `matches('ACME|ACME')`.

Onboarding was five steps. Four are data. RBAC was still a Go comment. PCI is Go only if the VSP must learn a new device id.

Docs: `docs/vendor-integration.md`.

### Week 10 — 7–13 October. Version skew, and the handshake enters the gate

The review wants four conformance gates:

1. Detection handshake.
2. Status mirroring.
3. Cleanup on delete.
4. Version skew.

Two were already gates. The handshake existed only as unit tests outside the gate, so a vendor could pass conformance and still fail the dial the daemon does. Version skew did not exist.

Why it matters: you write the vendor operator's objects. If that operator renames a field or bumps a CRD version, every translation becomes a silent no-op or an apply error. The only symptom is objects that never become ready.

`pkg/lifecycle/version.go` stores a window per vendor: tested up to, but not including, the next major. It classifies the installed version:

| Class | Condition | Meaning |
|---|---|---|
| `Supported` | True | Inside the window. |
| `TooOld` | False | Older than the window. Fields you write may not exist. |
| `TooNew` | False | Newer than the window. The schema may have moved. |
| `Unknown` | Unknown | No version, or it did not parse. |
| `Unpinned` | Unknown | This vendor has no window. |

Only a definite mismatch is False. Unknown stays Unknown so people do not learn to ignore the condition.

The result is published as `VendorOperatorSupported` on `DpuOperatorConfig`. Not `Ready`. The daemon owns `Ready` there too. Same rule as week 9.

The first run against a real API server found a bug a fake client hides. The code read `spec.vendor`. The upstream CRD does not have that field, so the API server deletes it. `vendor` was always empty, the reconcile stopped, and no skew was ever reported. A fake client keeps whatever you hand it and would have stayed green.

Fix: annotations, and `spec.vendor` still wins if upstream adds the field later.

```yaml
metadata:
  annotations:
    lifecycle.opi.nvidia.com/vendor: nvidia
    lifecycle.opi.nvidia.com/vendor-operator-version: v25.4.0
```

The handshake now runs inside the gate for NVIDIA, AMD, and Marvell, using the same enumerator as the real VSP. It checks `Init` returns a dialable address, `GetDevices` reports the card under its PCI address with the serial as its id, both API surfaces agree, add/delete network-function calls are acknowledged, and the server shuts down on cancel.

Two test holes showed up when you tried to break the code:

- A stability test could not fail. Deleting timestamp preservation stayed green because both reconciles happened in the same second, so the time string matched. `LifecycleManager.Now` is now a fake clock the test can move.
- An unchanged reconcile returns before it touches the condition, so it proved nothing. The case that matters is status staying the same while the reason changes (`Unknown`/`Unknown` to `Unknown`/`Unpinned`).

Also: `grpc.NewClient` is lazy. It succeeds even if nothing is listening, so the first call got `ECONNREFUSED`. The harness now waits until the socket file exists.

Counts then: 54 Go test functions, 14 conformance specs, all green. RBAC regenerated so the lifecycle manager can write `dpuoperatorconfigs/status`.

### Week 11 — 14–21 October. The three leftovers

This is the work that had not been sent to Sridhar. Both remaining known gaps closed. Onboarding a vendor is four touchpoints, not five.

**1. `status.when`.**

Reserving `Ready` stopped the fight over that one name. It did not stop the NVIDIA mapping from writing `DPFReady=False` onto an AMD card it does not own. The `status:` block had no guard. Every emit could be filtered out and the status block still ran, saw zero children, and wrote False.

`status.when` is the same kind of guard the emits already had. When it is false, the engine writes nothing. Not a False condition. Not an empty status field.

Both vendor mappings and the vendor template have it. `servicefunctionchain.yaml` does not. That object is not tied to one vendor, so there is nothing to gate on.

You had postponed this so you could say "a second vendor needs no Go." That claim is already true for weeks 7 and 8 and stays in the git history. Leaving a known reporting bug in place to protect the sentence was the wrong trade. The Go change is a general engine feature. Every vendor gets it. It is not an AMD special case.

Conformance checks both directions: an AMD card does not carry `DPFReady`, a BlueField does not carry `DSCReady`, and each card still carries its own condition. A gate that erased all status would pass the first check and fail the second.

Commit: `feat(mapping): gate status mirroring on ownership with status.when` (`a8634dc`).

**2. RBAC generated from the mappings.**

controller-gen builds the ClusterRole from comments in Go. A new API group had to be typed into `pkg/translation` by hand. Forget it and translation looks correct until the API server returns `Forbidden`.

`hack/gen-rbac` reads the FieldMapping documents and writes `pkg/translation/zz_generated_rbac.go`. The rules are fixed, not a judgement:

- The source kind is read, its status is updated, and it is finalized.
- The objects you emit are fully managed.

With the hand-written markers deleted, regenerating `config/rbac/role.yaml` produced the same bytes, including the plural `dscnodepolicies`. Adding the letter s would have produced `dscnodepolicys`, which is wrong.

```sh
make generate-rbac   # regenerate; make manifests depends on it
make verify-rbac     # fails if a mapping changed and nobody regenerated
```

`make verify-rbac` runs inside `make lint`, so the generated file cannot go stale quietly.

Commit: `feat(rbac): generate the translation controller's RBAC from the mappings` (`8e30037`).

Onboarding is now:

| # | What | Data or Go |
|---|---|---|
| 1 | The field mapping | data |
| 2 | The vendor's CRD schemas | data |
| 3 | A conformance case row | Go, test only |
| 4 | PCI enumeration, only if the VSP must stamp serials | Go, one table entry |

RBAC used to be step 4. It is gone. Production Go is only required when a new card must be enumerated.

**3. A machine-readable mapping spec.**

Phase 1 also asks for the mapping format to be a real contract, not prose plus a Go package.

`config/mappings/fieldmapping.schema.json` is JSON Schema draft 2020-12. It encodes rules, not only names:

- exactly one of `from`, `cel`, or `value`
- condition type `Ready` is rejected, so an editor fails before the controller refuses to start

Each shipped mapping and the template start with:

```yaml
# yaml-language-server: $schema=./fieldmapping.schema.json
```

That turns the schema into completion and inline errors while someone writes a mapping.

A schema next to a Go struct drifts. `schema_test.go` reflects over the struct's json tags and fails if either side has a field the other does not. The failure names the field. It does not need extra libraries.

Adding that modeline shifted the template banner by one line. `hack/new-vendor.sh` was deleting a fixed 12 lines with `sed`. It now strips leading comment lines by matching them, so a longer banner cannot cut real YAML.

Commit: `feat(mapping): add a JSON Schema for FieldMapping, with a drift guard` (`ab5763d`).

**Tests at the end of this week.** 63 Go test functions, up from 54. 14 conformance specs, green. Each new guard was broken on purpose and a test went red: remove the status gate (7 tests), drop the gate from a shipped mapping, naive pluralization, skip emitted targets in RBAC (9 missing groups), non-deterministic generator, schema field the Go struct lacks, Go field the schema lacks, schema stops reserving `Ready`. `go vet ./...` clean. `role.yaml` unchanged. `go test ./...` green.

---

## 7. Where the roadmap stands

Your own phases:

| Phase | What it was | State |
|---|---|---|
| 0 | Scaffold, mapping engine, real PCI serial, VSP socket | Done in weeks 2–3. |
| 1 | Status back, finalizers, importable package | Done in week 4. Re-checked in week 5. |
| 2 | Second vendor, mostly data | AMD YAML in week 7. Discovery Go in week 8, because discovery was the part that was not data. |

The review's phases, which are a different list:

| Review item | State |
|---|---|
| Phase 0, all four, including single-writer status | Done. `Ready` belongs to the daemon. |
| Phase 1.1 vendor scaffold | Done. `make new-vendor`. |
| Phase 1.2 all four conformance dimensions | Done. Handshake, status, cleanup, version skew. |
| Phase 1.3 machine-readable spec | The file exists. Taking it to the TSC is not done. |
| Phase 2 of the review (kro, Crossplane, aggregated API server) | Not scheduled. Only if a third vendor shares more than about 70% of its boilerplate with these two. |

---

## 8. What you still have to do

In this order.

**1. Tell Sridhar the operator is on dh2, and say the two limits out loud.**

It is running. The VSP is the mock, not the physical card. The AMD controller is retrying because the AMD CRDs are not on that cluster.

**2. Do not install the stub AMD CRDs on dh2 and call it a hardware test.**

The stubs are for unit and conformance tests. A real AMD run needs the official DSC Operator bundle: the CRDs that project ships, not the ones you wrote. Until that bundle exists, the AMD gap stays external. Your Go does not need another change for the schemas themselves. When the real CRDs arrive, you replace the stubs, check the mapping against them, and run the same suite.

**3. Real VSP on dh2, only if he asks for the physical card.**

Today the DaemonSet uses mock flags because `--pci` needs to see `/sys` on the host. The default pod is locked down: non-root, no host network, read-only filesystem. Turning on the real card means changing that DaemonSet to pass `--pci` and to mount the host paths the scan needs. That is a real change. Do not do it silently. The BlueField-2 is on dh2. dh1 is the Pensando machine and has no Kubernetes.

**4. The governance half of Phase 1.3.**

Ask the TSC to accept the mapping spec (`fieldmapping.schema.json`) and the conformance suite as an OPI subproject. That is a proposal, not a code change. The point is that the contract lives with OPI, not only in this repo's history.

**5. Keep the weekly note short and on time.**

The wiki page for a week is the record. A mail to Sridhar is two or three sentences plus the link. The long explanation is this file, for you.

**6. Nothing else is scheduled.**

Marvell keys are reserved and unused. A third vendor is not started. Review Phase 2 (other translation engines) is a watch list.

---

## 9. If he asks one question

**What does your operator do?**
It watches an OPI `DataProcessingUnit` and creates NVIDIA's four objects, or AMD's four objects, from a YAML file. It copies status back. It deletes those objects when the OPI object is deleted.

**Why isn't this inside dpu-operator?**
OPI stays vendor-neutral. NVIDIA's model is declarative CRDs. Putting NVIDIA field copies in the shared repo would force every NVIDIA schema change through that review, and it would bake one vendor into the common operator.

**Why AMD?**
To prove a second vendor is a YAML change. AMD's objects are not renamed NVIDIA objects.

**What is `status.when`?**
Both mappings see every card. Without a guard, the NVIDIA status block still runs on an AMD card, sees no children, and writes `DPFReady=False`. `status.when` makes that block write nothing when the card is not that vendor's.

**Why not `Ready`?**
The node daemon already owns `Ready`. Two writers on the same name overwrite each other, and `kubectl get` prints `Ready`, so a healthy AMD card looked failed. You write `DPFReady` or `DSCReady`. A mapping that says `Ready` will not load.

**What is the VSP?**
The process on the node. It reads the card serial and answers the DPU daemon on the vendor-plugin socket. The manager never makes up a serial.

**Is it running?**
Yes, on dh2, since 20 October 2026. Manager and VSP pods are up. The VSP is the mock. AMD CRDs are not installed, so that controller retries.

**What is blocked?**
The official AMD DSC Operator bundle. dh1 has the card and no Kubernetes.

**What is next that is not code?**
Take the mapping schema and the conformance suite to the TSC as an OPI subproject.
