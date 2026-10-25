# Lab connection

All live checks in this folder ran on dh2. This Mac is the F5 VPN Mac.
Do not install F5 on another Mac. Do not copy `<lab-ssh-key>`.

## Hosts

| Host | Address | RAM | Card | Kubernetes | SSH |
|---|---|---|---|---|---|
| dh1 | <your-lab-hosts> | 128 GB | AMD Pensando DSC2-100 | no | yes |
| dh2 | <your-lab-hosts> | 256 GB | NVIDIA BlueField-2 | k3s v1.36.4+k3s1 | yes |
| dh3 | <your-lab-hosts> | 256 GB | Marvell CN106 | no | yes |
| dh9 | <your-lab-hosts> | 384 GB | none seen | no | yes |
| dh4, dh5 | <your-lab-hosts>, <your-lab-hosts> | unknown | | | no |

VPN Mac: `<vpn-mac-host>.local` (`<your-lab-hosts>`), user `kartikey0104`.

## 1. Start the VPN

```bash
scutil --nc status "F5 Access" | head -n 8
scutil --nc start "F5 Access"
ping -c 2 -W 2000 <your-lab-hosts>
```

## 2. SSH onto dh2

```bash
SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=25 \
  -o IdentityAgent=none -o IdentitiesOnly=yes \
  -o IdentityFile="$HOME/.ssh/<lab-ssh-key>" -o UseKeychain=yes \
  -o KexAlgorithms=curve25519-sha256)

ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'hostname; k3s kubectl get nodes'
```

Job files only:

```bash
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'mkdir -p /tmp/lab-job-pipeline-fix'
# ... work ...
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'rm -rf /tmp/lab-job-pipeline-fix'
```
