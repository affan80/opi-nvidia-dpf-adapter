/*
Copyright 2026 Kartikey Gupta.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

    10|Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vsp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

// Official dpu-operator daemon (99.daemonset.yaml) mounts this hostPath
// Bidirectional. An independently deployed daemon cannot see emptyDir.
const officialDaemonHostPath = "/var/run/dpu-daemon"

func TestDefaultVSPSharesSocketWithDaemonSidecar(t *testing.T) {
	ds := loadDaemonSet(t, "config", "vsp", "daemonset.yaml")
	vspM := containerMount(t, ds, "vsp", officialDaemonHostPath)
	daemonM := containerMount(t, ds, "opi-daemon", officialDaemonHostPath)
	if vspM.Name != daemonM.Name {
		t.Fatalf("sidecar and VSP must share one volume: vsp=%q daemon=%q",
			vspM.Name, daemonM.Name)
	}
	vol := volumeNamed(t, ds, vspM.Name)
	if vol.EmptyDir == nil {
		t.Fatal("kind DaemonSet must share emptyDir with the sidecar (Restricted PSS)")
	}
	if vol.HostPath != nil {
		t.Fatal("kind default must not use hostPath; hardware overlay owns that")
	}
}

func TestDefaultVSPEmptyDirIsNotTheIndependentDaemonPath(t *testing.T) {
	ds := loadDaemonSet(t, "config", "vsp", "daemonset.yaml")
	for _, v := range ds.Spec.Template.Spec.Volumes {
		if v.HostPath != nil {
			t.Fatalf("default VSP volume %q is hostPath; kind cannot share that with Restricted PSS", v.Name)
		}
	}
}

func TestHardwareOverlaySharesOfficialHostPath(t *testing.T) {
	b := readRepoFile(t, "config", "overlays", "hardware-vsp", "pci-patch.yaml")
	var patches []map[string]any
	if err := yaml.Unmarshal(b, &patches); err != nil {
		t.Fatal(err)
	}
	var vols []corev1.Volume
	for _, p := range patches {
		if p["path"] != "/spec/template/spec/volumes" {
			continue
		}
		raw, err := yaml.Marshal(p["value"])
		if err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal(raw, &vols); err != nil {
			t.Fatal(err)
		}
	}
	found := false
	for _, v := range vols {
		if v.HostPath != nil && v.HostPath.Path == officialDaemonHostPath {
			found = true
			if v.HostPath.Type == nil || *v.HostPath.Type != corev1.HostPathDirectoryOrCreate {
				t.Errorf("hostPath %s type = %v, want DirectoryOrCreate", officialDaemonHostPath, v.HostPath.Type)
			}
		}
	}
	if !found {
		t.Fatalf("hardware overlay volumes missing hostPath %s: %+v", officialDaemonHostPath, vols)
	}
}

func TestOPIDaemonMountsOfficialHostPath(t *testing.T) {
	ds := loadDaemonSet(t, "config", "opi-daemon", "daemonset.yaml")
	m := containerMount(t, ds, "dpu-daemon", officialDaemonHostPath)
	vol := volumeNamed(t, ds, m.Name)
	if vol.HostPath == nil || vol.HostPath.Path != officialDaemonHostPath {
		t.Fatalf("independent daemon volume %q hostPath = %+v, want %s",
			m.Name, vol.HostPath, officialDaemonHostPath)
	}
	if vol.EmptyDir != nil {
		t.Fatal("independent OPI daemon must not use emptyDir; the VSP cannot see it")
	}
	if m.MountPropagation == nil || *m.MountPropagation != corev1.MountPropagationBidirectional {
		t.Fatal("official 99.daemonset.yaml uses Bidirectional mountPropagation")
	}
	if vol.HostPath.Type == nil || *vol.HostPath.Type != corev1.HostPathDirectoryOrCreate {
		t.Errorf("hostPath type = %v, want DirectoryOrCreate", vol.HostPath.Type)
	}
}

func TestHardwareOverlayDropsKindSidecar(t *testing.T) {
	b := string(readRepoFile(t, "config", "overlays", "hardware-vsp", "pci-patch.yaml"))
	if !strings.Contains(b, "/spec/template/spec/containers/1") || !strings.Contains(b, "remove") {
		t.Fatal("hardware overlay must drop the kind sidecar; hostPath + independent daemon is the share path")
	}
}

func TestSocketDirModeIsNotWorldWritable(t *testing.T) {
	if SocketDirPerm&0o022 != 0 {
		t.Fatalf("SocketDirPerm=%#o is world/group writable; official PathManager wants root-only", SocketDirPerm)
	}
}

func loadDaemonSet(t *testing.T, parts ...string) *appsv1.DaemonSet {
	t.Helper()
	b := readRepoFile(t, parts...)
	var ds appsv1.DaemonSet
	if err := yaml.Unmarshal(b, &ds); err != nil {
		t.Fatal(err)
	}
	if ds.Kind != "DaemonSet" {
		t.Fatalf("%s kind = %q, want DaemonSet", filepath.Join(parts...), ds.Kind)
	}
	return &ds
}

func readRepoFile(t *testing.T, parts ...string) []byte {
	t.Helper()
	var candidates []string
	if wd, err := os.Getwd(); err == nil {
		for d := wd; ; d = filepath.Dir(d) {
			candidates = append(candidates, filepath.Join(append([]string{d}, parts...)...))
			if d == filepath.Dir(d) {
				break
			}
		}
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		candidates = append(candidates, filepath.Join(append([]string{filepath.Dir(file), "..", ".."}, parts...)...))
	}
	var last error
	for _, path := range candidates {
		b, err := os.ReadFile(path)
		if err == nil {
			return b
		}
		last = err
	}
	t.Fatalf("read %s: %v", filepath.Join(parts...), last)
	return nil
}

func containerMount(t *testing.T, ds *appsv1.DaemonSet, container, mountPath string) corev1.VolumeMount {
	t.Helper()
	for _, c := range ds.Spec.Template.Spec.Containers {
		if c.Name != container {
			continue
		}
		for _, m := range c.VolumeMounts {
			if m.MountPath == mountPath {
				return m
			}
		}
		t.Fatalf("container %q has no mount at %s", container, mountPath)
	}
	t.Fatalf("container %q missing", container)
	return corev1.VolumeMount{}
}

func volumeNamed(t *testing.T, ds *appsv1.DaemonSet, name string) corev1.Volume {
	t.Helper()
	for _, v := range ds.Spec.Template.Spec.Volumes {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("volume %q missing", name)
	return corev1.Volume{}
}
