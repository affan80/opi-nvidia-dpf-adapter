# Helm adapter mutation (dedicated release)

`helm.sh/helm/v3@v3.21.4` uses k8s **v0.36.2**; `v3.22.1` uses **v0.37.1**.
This module stays on k8s **v0.36.0**, so the Helm Go SDK is still not
imported. `ClusterHelmClient` writes Helm v3 Secrets and applies a
**local** chart. It does not shell out.

Safety:

- Release name `dpf-operator` is refused (`ErrHelmMutationNotConfigured`)
- Empty `ChartRoot` is refused
- `HelmArgs` are refused
- Live DPF `v26.4.0` is observed/claimed only
- Install/Upgrade/Rollback/Uninstall take a per-release lock
- Chart ConfigMap and Helm Secret writes retry on 409 / AlreadyExists

Disposable identity (Kind / envtest):

- name `opi-lcm-probe`
- namespace `opi-lcm-probe`
- charts `pkg/lifecycle/testdata/lcm-probe/{0.1.0,0.2.0}`

```
GOTOOLCHAIN=go1.26.10 go test ./pkg/lifecycle/ -count=1 \
  -run 'TestClusterHelmClient'
make docker-preflight
make test-e2e
```

Kind spec `HelmProbe` runs install → upgrade 0.2.0 → rollback 0.1.0 →
uninstall. It never creates `dpf-operator`.
