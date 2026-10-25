# Lab connection

All live checks in this folder ran on dh2. This Mac is the F5 VPN Mac.
Do not install F5 on another Mac. Do not copy `opi_lab_key`.

## Hosts

| Host | Address | RAM | Card | Kubernetes | SSH |
|---|---|---|---|---|---|
| dh1 | 172.22.1.1 | 128 GB | AMD Pensando DSC2-100 | no | yes |
| dh2 | 172.22.1.2 | 256 GB | NVIDIA BlueField-2 | k3s v1.36.4+k3s1 | yes |
| dh3 | 172.22.1.3 | 256 GB | Marvell CN106 | no | yes |
| dh9 | 172.22.1.9 | 384 GB | none seen | no | yes |
| dh4, dh5 | 172.22.1.4, 172.22.1.5 | unknown | | | no |

VPN Mac: `Kartikeys-MacBook-Pro.local` (`10.7.8.82`), user `kartikey0104`.

## 1. Start the VPN

```bash
scutil --nc status "F5 Access" | head -n 8
scutil --nc start "F5 Access"
ping -c 2 -W 2000 172.22.1.2
```

## 2. SSH onto dh2

```bash
SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=25 \
  -o IdentityAgent=none -o IdentitiesOnly=yes \
  -o IdentityFile="$HOME/.ssh/opi_lab_key" -o UseKeychain=yes \
  -o KexAlgorithms=curve25519-sha256)

ssh "${SSH_OPTS[@]}" root@172.22.1.2 'hostname; k3s kubectl get nodes'
```

Job files only:

```bash
ssh "${SSH_OPTS[@]}" root@172.22.1.2 'mkdir -p /tmp/lab-job-pipeline-fix'
# ... work ...
ssh "${SSH_OPTS[@]}" root@172.22.1.2 'rm -rf /tmp/lab-job-pipeline-fix'
```
