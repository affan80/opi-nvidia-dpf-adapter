# Schema / API lab record

Date: 2026-10-23
Repo: `opi-nvidia-dpf-adapter`
Lab host: dh2 (`172.22.1.2`), NVIDIA BlueField-2, k3s `v1.36.4+k3s1`

This folder is the full record of the schema/API work: how to reach the lab,
every API the adapter touches, every dead-API we found, how each one was
fixed, and the commands used to prove it on dh2.

| File | What it is |
|---|---|
| [OPI_DPF_Adapter_Bugs_Issues_Fixes.pdf](OPI_DPF_Adapter_Bugs_Issues_Fixes.pdf) | Printable report of every bug, error, and fix |
| [01_lab_connection.md](01_lab_connection.md) | Start F5 VPN, SSH to dh2 (this Mac and hop from another Mac), host table, lab rules |
| [02_schema_api.md](02_schema_api.md) | Every OPI and DPF API, field-by-field, pin, writer, live vs fixture |
| [03_fixes.md](03_fixes.md) | Each issue: what was broken, root cause, what we changed, how we verified |
| [04_testing_commands.md](04_testing_commands.md) | Copy-paste local gates and live lab probes, with the 2026-10-23 results |

Pins these documents describe

| API | Pin |
|---|---|
| Kubernetes | v1.36.4 (k3s), Go modules v0.36.0 |
| OPI | `config.openshift.io/v1` @ `c107ef71`, unmerged [opiproject/dpu-operator#6](https://github.com/opiproject/dpu-operator/pull/6) |
| NVIDIA DPF | v26.4.0, window `[v26.4.0, v27.0.0)` |
| AMD | known, Unpinned (stubs in-repo only; not installed on dh2) |
