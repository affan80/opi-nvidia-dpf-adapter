Issue 2 — mapping emitted DPUService only, no owner graph

Date: 2026-10-22
Host: dh2

What the issue was
- config/mappings/servicefunctionchain.yaml emitted only DPUService
- DONE WHEN required a written resource graph with owner and namespace
  for each generated CR
- DPUServiceInterface and DPUServiceChain exist on dh2
  (Interface CRD 2026-10-20, Chain CRD 2026-09-07)
- Without those emits, nothing recorded who owns the interface or the
  chain, so a later DPUDeployment add would look like a gap-fill

What we changed
- Mapping now emits three kinds, all in defaults.namespace:

    | CR                    | Name                 | Namespace                         | Owner                 |
    | DPUService            | item.name            | source ns else dpf-operator-system | ServiceFunctionChain |
    | DPUServiceInterface   | item.name + '-ifc'   | same                              | ServiceFunctionChain |
    | DPUServiceChain       | source.metadata.name | same                              | ServiceFunctionChain |

- DPUService.spec.interfaces = [item.name + '-ifc']
- Interface: interfaceType=service, serviceID=item.name,
  interfaceName=net0, network=default (OPI has no NAD; documented default)
  Label service=<nf-name> for chain matchLabels
- Chain: one switch, one port per chart NF, in list order
- config/samples/service-chain-direct-children.yaml is the v26.4 shape
- config/versions.yaml dpf.mappedKinds adds DPUServiceInterface,
  DPUServiceChain
- RBAC regenerated: dpuservicechains; dpuserviceinterfaces; dpuservices
- Conformance case expects all three children with owner refs

How we fixed it
- The mapping file is the graph. Tests fail if a kind is dropped or if
  namespace/name is missing.
- Image-only SFC still emits nothing (no empty chain).
- Chart-only SFC on the sample: 1 DPUService + 1 Interface + 1 Chain.

Live dry-run on dh2
- DPUServiceInterface/hbn-ifc created (server dry run)
- DPUServiceChain/hbn-chain created (server dry run)
- DPUService still fails on spec.security (Sept 7 CRD vs v26.4 pin).
  Same skew as api_schema_agreement/issue_04. Mapping stays on the pin.

How to verify
    go test ./pkg/mapping/ -run TestSFCApplyEmitsOwnedGraph
    go test ./pkg/translation/ -count=1
    go test ./test/conformance/ -count=1
    kubectl apply --dry-run=server -f config/samples/service-chain-direct-children.yaml
