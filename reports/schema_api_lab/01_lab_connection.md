# Lab connection

The adapter is only running on dh2. All live schema/API checks in this
folder were done against that cluster.

## Hosts

| Host | Address | RAM | Card | Kubernetes | SSH |
|---|---|---|---|---|---|
| dh1 | 172.22.1.1 | 128 GB | AMD Pensando DSC2-100 | no | yes |
| dh2 | 172.22.1.2 | 256 GB | NVIDIA BlueField-2 | k3s v1.36.4+k3s1 | yes |
| dh3 | 172.22.1.3 | 256 GB | Marvell CN106 | no | yes |
| dh9 | 172.22.1.9 | 384 GB | none seen | no | yes |
| dh4, dh5 | 172.22.1.4, 172.22.1.5 | unknown | | | no — do not force it |

VPN Mac: `Kartikeys-MacBook-Pro.local` (`10.7.8.82`), user `kartikey0104`.
F5 Access is installed on that Mac only. Do not install F5 on another Mac.
Do not copy `opi_lab_key`.

## 1. Start the VPN (this Mac is the VPN Mac)

```bash
# status
scutil --nc status "F5 Access" | head -n 8

# start if Disconnected
scutil --nc start "F5 Access"

# wait until it prints Connected, then
ping -c 2 -W 2000 172.22.1.2
```

Expected when the tunnel is up:

```
Connected
...
64 bytes from 172.22.1.2: ...
2 packets transmitted, 2 packets received, 0.0% packet loss
```

If ping is 100% loss, SSH will be `connection refused`. Start F5 again.
A previous drop looked like `F5ChannelError -1073740512`.

## 2. SSH to dh2 from this Mac (VPN Mac)

`IdentityAgent` can hold a stale launchd socket ("Server accepts key" then
"we did not send a packet"). Always disable the agent and pin the lab key.

```bash
SSH_OPTS=(
  -o BatchMode=yes
  -o ConnectTimeout=25
  -o IdentityAgent=none
  -o IdentitiesOnly=yes
  -o IdentityFile="$HOME/.ssh/opi_lab_key"
  -o UseKeychain=yes
  -o KexAlgorithms=curve25519-sha256
)

ssh "${SSH_OPTS[@]}" root@172.22.1.2 'k3s --version; hostname; free -h | head -2; nproc'
```

Expected:

```
k3s version v1.36.4+k3s1 (...)
dh2
```

## 3. SSH from another Mac (not on the VPN)

Other Macs hop through the VPN Mac, then SSH from there. Do not copy the
lab key. Do not install F5 on the other Mac.

```bash
# example: reach dh2 through the VPN Mac
ssh kartikey0104@Kartikeys-MacBook-Pro.local \
  'ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
       -o IdentityFile="$HOME/.ssh/opi_lab_key" \
       -o UseKeychain=yes -o KexAlgorithms=curve25519-sha256 \
       root@172.22.1.2 "hostname; k3s --version"'
```

A one-shot check of another host (dh1) looks like:

```bash
ssh kartikey0104@Kartikeys-MacBook-Pro.local \
  'ssh -o IdentityAgent=none -o IdentitiesOnly=yes \
       -o IdentityFile="$HOME/.ssh/opi_lab_key" \
       -o UseKeychain=yes -o KexAlgorithms=curve25519-sha256 \
       root@172.22.1.1 "free -h && nproc"'
```

## 4. Lab rules (use, do not change the server)

Allowed

- Run jobs that use the server CPU and RAM (`k3s kubectl`, dry-run, logs).
- Put scratch only in `/tmp/lab-job-<name>/` and delete it afterward.
- Rebuild the adapter image in that job dir, import it into k3s, restart
  the adapter Deployment. That is a job, not a server change.

Not allowed

- Package installs, `/etc` edits, `systemctl`, reboot, firmware, Ansible,
  new users or SSH keys, BMC writes.
- `helm uninstall` of DPF.
- Leave `/tmp/lab-job-*` behind.

CRD applies that the user authorized (OPI fixture align, DPF v26.4
replace for the three skewed kinds) are cluster API changes, not `/etc`
changes. Do not expand that to a full Helm takeover.

## 5. What is already on dh2

| Thing | Value |
|---|---|
| Node | `dh2` Ready, control-plane, Ubuntu 26.04, kernel 7.0.0-38 |
| Runtime | containerd 2.3.4-k3s1.36 |
| DPF operator | `nvcr.io/nvidia/doca/dpf-system:v26.4.0` in `dpf-operator-system` |
| Adapter ns | `opi-nvidia-dpf-adapter-system` |
| Adapter image | `controller:latest` (imported with `docker save \| k3s ctr images import`) |
| Mappings ConfigMap | `opi-nvidia-dpf-adapter-mappings` (NVIDIA only: DPU + SFC) |
| ClusterRole | `opi-nvidia-dpf-adapter-manager-role` |

## 6. kubectl on dh2

There is no `kubectl` on PATH. Always:

```bash
k3s kubectl get node -o wide
k3s kubectl -n opi-nvidia-dpf-adapter-system get pods -o wide
k3s kubectl get crd | awk 'NR==1 || /openshift.io|dpu.nvidia.com|amd/'
```
