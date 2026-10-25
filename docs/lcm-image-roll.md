# Roll the patched adapter onto dh2

## Issue

Issues 1–4 patched source: Helm observe/claim, adopt-existing, OLM
observe, AMD no-chart fallthrough. The **dh2** pod still ran an older
image, so those paths were not the live process.

After `controller:lcm-20261024` started, the cache tried to
cluster-watch Secrets. The manager role lists and patches Helm
storage. It does not watch Secrets. Logs showed
`secrets is forbidden: cannot watch resource "secrets" at the cluster
scope`. Claim still worked (get/list/update/patch). The noise was
not a missing RBAC grant.

Done-when: the running pod is the patched image; Helm pin
`dpf-operator-system/dpf-operator@v26.4.0` stays claimed
(`owner=opi-nvidia-dpf-adapter`, `mode=adopted`); Secrets are
uncached (no cluster watch); no Secret watch is added to the role.

## Fix

| Piece | Role |
|---|---|
| `controller:lcm-20261024` | first roll that ran SecretHelmClient + adopt + OLM + AMD |
| `Client.Cache.DisableFor` `corev1.Secret` | uncached list/get/patch; no Secret informer |
| `controller:lcm-20261024b` | second roll that includes DisableFor |
| ClusterRole secrets verbs | stay `get;list;update;patch` — no `watch` |

`cmd/main.go` now sets:

```go
Client: client.Options{
    Cache: &client.CacheOptions{
        DisableFor: []client.Object{&corev1.Secret{}},
    },
},
```

Do **not** add a cluster-wide Secret watch. Helm storage is one
Secret in `dpf-operator-system`. A namespaced cache would also work;
DisableFor is enough and keeps BMC/user Secrets out of the informer.

## Lab (2026-10-24)

```
# hop
ssh <user>@<vpn-mac> \
  'ssh root@<lab-host> "k3s kubectl -n opi-nvidia-dpf-adapter-system get pods -o wide"'

# first roll (already applied)
# image controller:lcm-20261024
# pod 764d4d4b88-87lsr Running 2026-10-24T12:59:26Z
# Helm labels owner=opi-nvidia-dpf-adapter mode=adopted owner=helm
# LCM action=noop installed=v26.4.0 Supported Ready
# 8 "cannot watch resource secrets" lines in the last 200 logs
```

Second roll commands:

```
IMG=controller:lcm-20261024b
docker build --platform linux/amd64 -t "$IMG" .
docker save "$IMG" | gzip > /tmp/controller-lcm-20261024b.tar.gz
scp /tmp/controller-lcm-20261024b.tar.gz root@<lab-host>:/tmp/lab-job-image-roll/
ssh root@<lab-host> 'k3s ctr images import /tmp/lab-job-image-roll/controller-lcm-20261024b.tar.gz'
k3s kubectl -n opi-nvidia-dpf-adapter-system set image \
  deploy/opi-nvidia-dpf-adapter-controller-manager manager=controller:lcm-20261024b
```

Lab after second roll (2026-10-24T13:10:44Z):

| Check | Result |
|---|---|
| Pod | `65b7466b6d-x5qpr` Running `controller:lcm-20261024b` |
| Helm labels | `owner=opi-nvidia-dpf-adapter` `mode=adopted` `owner=helm` |
| LCM | `vendor=nvidia action=noop installed=v26.4.0 skew=Supported ready=true operator=Ready` |
| Secret watch errors | **0** (was 8 on `lcm-20261024`) |
| `dscdevices.dpu.amd.com` | NotFound (required on BlueField) |

## Tests

```
GOTOOLCHAIN=go1.26.10 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestMainDisablesSecretInformer|TestManagerRoleListsHelmSecrets'
GOTOOLCHAIN=go1.26.10 go test ./pkg/lifecycle/ -count=1
GOTOOLCHAIN=go1.26.10 go test ./pkg/lifecycle/ -race -count=1
```
