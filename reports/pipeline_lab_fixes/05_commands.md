# Commands

`SSH_OPTS` as in [01_lab_connection.md](01_lab_connection.md).

## Local unit tests (after HBN pin, device name, PCI)

```bash
cd opi-nvidia-dpf-adapter
GOTOOLCHAIN=go1.26.0 go test ./pkg/mapping/ ./pkg/translation/ ./pkg/lifecycle/ ./pkg/discovery/ -count=1
```

Result: all `ok`.

## HBN pin on the lab

```bash
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> \
  'k3s kubectl patch servicefunctionchain sfc-chart-probe --type json \
   -p "[{\"op\":\"replace\",\"path\":\"/spec/networkFunctions/0/chart/version\",\"value\":\"v26.4.0\"}]"'
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> \
  'k3s kubectl get dpuservice hbn -n default -o jsonpath="{.spec.helmChart.source.version}"'
```

## Argo vuln

```bash
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl -n dpf-operator-system get svc -l app.kubernetes.io/part-of=argocd -o wide'
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl -n dpf-operator-system delete secret argocd-initial-admin-secret'
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl -n dpf-operator-system get svc argo-cd-argocd-server -o jsonpath="type={.spec.type}\n"'
```

## DPFOperatorConfig

YAML was written under `/tmp/lab-job-pipeline-fix/dpf-system.yaml` on
dh2 (PVC + DPFOperatorConfig, kamaji disabled) then:

```bash
k3s kubectl apply -f /tmp/lab-job-pipeline-fix/dpf-system.yaml
k3s kubectl -n dpf-operator-system get dpfoperatorconfig dpfoperatorconfig
```

## CEL relaxation

On dh2: dump CRD JSON, drop the privileged-required rule, `kubectl replace -f`.

## Image import (device-name fix)

```bash
cd opi-nvidia-dpf-adapter
docker build --platform linux/amd64 -t controller:latest .
docker save controller:latest | ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s ctr images import -'
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> \
  'k3s kubectl -n opi-nvidia-dpf-adapter-system rollout restart deploy/opi-nvidia-dpf-adapter-controller-manager'
```

## Combined pipeline retest (final)

```bash
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl -n dpf-operator-system get dpfoperatorconfig dpfoperatorconfig'
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl get dpuoperatorconfig dpu-operator-config'
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl get servicefunctionchain sfc-chart-probe -n default'
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl get dpuservice hbn -n default'
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl -n dpf-operator-system get bfb,dpu,dpunode,dpudevice,dpuservice'
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl get application -A'
ssh "${SSH_OPTS[@]}" root@<your-lab-hosts> 'k3s kubectl get dpucluster -A'
```

## Static DPUCluster (second pass)

On dh2:

```bash
k3s kubectl -n dpf-operator-system create sa dpu-cluster-static-admin
k3s kubectl create clusterrolebinding dpu-cluster-static-admin \
  --clusterrole=cluster-admin \
  --serviceaccount=dpf-operator-system:dpu-cluster-static-admin
# token + kubeconfig secret dpu-cluster-1-admin-kubeconfig (super-admin.conf)
k3s kubectl apply -f /tmp/lab-job-pipeline-fix/dpucluster.yaml
k3s kubectl -n dpf-operator-system get dpucluster dpu-cluster-1
k3s kubectl -n dpf-operator-system get dpu dh2-bluefield-2
k3s kubectl get application -A
```

Job dir `rm -rf /tmp/lab-job-pipeline-fix` after the retest.
