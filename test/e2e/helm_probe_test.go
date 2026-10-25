//go:build e2e
// +build e2e

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

package e2e

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/lifecycle"
	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/test/utils"
)

var _ = Describe("HelmProbe", Ordered, func() {
	It("installs upgrades and rolls back a dedicated release", func() {
		By("refusing to use the live dpf-operator release name")
		Expect(lifecycle.HelmProbeReleaseName).NotTo(Equal(lifecycle.NVIDIAReleaseName))

		dir, err := utils.GetProjectDir()
		Expect(err).NotTo(HaveOccurred())
		restCfg, err := config.GetConfig()
		Expect(err).NotTo(HaveOccurred())
		scheme := clientgoscheme.Scheme
		cl, err := client.New(restCfg, client.Options{Scheme: scheme})
		Expect(err).NotTo(HaveOccurred())
		ctx := context.Background()

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: lifecycle.HelmProbeNamespace}}
		err = cl.Create(ctx, ns)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		DeferCleanup(func() {
			_ = cl.Delete(ctx, ns)
		})

		helm := lifecycle.NewClusterHelmClient(cl)
		helm.ChartRoot = filepath.Join(dir, "pkg", "lifecycle", "testdata", "lcm-probe")
		v1 := lifecycle.ProbeHelmConfig("0.1.0")
		v2 := lifecycle.ProbeHelmConfig("0.2.0")

		By("installing opi-lcm-probe 0.1.0")
		got, err := helm.Install(ctx, v1, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Name).To(Equal(lifecycle.HelmProbeReleaseName))
		Expect(got.ChartVersion).To(Equal("0.1.0"))
		cm := &corev1.ConfigMap{}
		Expect(cl.Get(ctx, client.ObjectKey{
			Namespace: lifecycle.HelmProbeNamespace, Name: lifecycle.HelmProbeReleaseName,
		}, cm)).To(Succeed())
		Expect(cm.Data["version"]).To(Equal("0.1.0"))

		By("upgrading to 0.2.0")
		up, err := helm.Upgrade(ctx, v2, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(up.ChartVersion).To(Equal("0.2.0"))
		Expect(cl.Get(ctx, client.ObjectKey{
			Namespace: lifecycle.HelmProbeNamespace, Name: lifecycle.HelmProbeReleaseName,
		}, cm)).To(Succeed())
		Expect(cm.Data["version"]).To(Equal("0.2.0"))

		By("rolling back to 0.1.0")
		rb, err := helm.Rollback(ctx, v2, "0.1.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(rb.ChartVersion).To(Equal("0.1.0"))
		Expect(cl.Get(ctx, client.ObjectKey{
			Namespace: lifecycle.HelmProbeNamespace, Name: lifecycle.HelmProbeReleaseName,
		}, cm)).To(Succeed())
		Expect(cm.Data["version"]).To(Equal("0.1.0"))

		By("uninstalling the dedicated release")
		_, err = helm.Uninstall(ctx, v1)
		Expect(err).NotTo(HaveOccurred())
		_, err = helm.GetRelease(ctx, lifecycle.HelmProbeNamespace, lifecycle.HelmProbeReleaseName)
		Expect(err).To(MatchError(lifecycle.ErrReleaseNotFound))
	})
})
