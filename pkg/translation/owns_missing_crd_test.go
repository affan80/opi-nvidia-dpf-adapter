/*
Copyright 2026 Kartikey Gupta.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package translation

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/mapping"
)

// dh2 is BlueField-only (docs/hybrid-integration-design.md,
// lab/Documentation/ips.md). The AMD mapping still ships so a mixed
// cluster can load it. Owns() of those GVKs on a host without
// dpu.amd.com CRDs is what crash-looped controller:lcm-20261024b.
var _ = Describe("Owns skip for absent vendor CRDs", func() {
	It("does not own AMD emit kinds when discovery has no dpu.amd.com", func() {
		spec, err := mapping.LoadFile(filepath.Join("..", "..", "config", "mappings", "amd-dsc200.yaml"))
		Expect(err).NotTo(HaveOccurred())

		var targets []schema.GroupVersionKind
		seen := map[schema.GroupVersionKind]bool{}
		for _, e := range spec.Emit {
			gvk := e.Target.GVK()
			if seen[gvk] {
				continue
			}
			seen[gvk] = true
			targets = append(targets, gvk)
		}
		Expect(targets).To(HaveLen(4))
		for _, gvk := range targets {
			Expect(gvk.Group).To(Equal("dpu.amd.com"))
		}

		mapper := &stubMapper{missing: map[schema.GroupKind]bool{}}
		for _, gvk := range targets {
			mapper.missing[gvk.GroupKind()] = true
		}
		owned, err := ownableTargets(mapper, targets)
		Expect(err).NotTo(HaveOccurred())
		Expect(owned).To(BeEmpty(), "missing AMD CRDs must not be Owned; cache sync would kill the manager")
	})

	It("still owns NVIDIA emit kinds that discovery already serves", func() {
		spec, err := mapping.LoadFile(filepath.Join("..", "..", "config", "mappings", "dataprocessingunit.yaml"))
		Expect(err).NotTo(HaveOccurred())

		var targets []schema.GroupVersionKind
		seen := map[schema.GroupVersionKind]bool{}
		for _, e := range spec.Emit {
			gvk := e.Target.GVK()
			if seen[gvk] {
				continue
			}
			seen[gvk] = true
			targets = append(targets, gvk)
		}
		owned, err := ownableTargets(&stubMapper{}, targets)
		Expect(err).NotTo(HaveOccurred())
		Expect(owned).To(Equal(targets))
	})
})
