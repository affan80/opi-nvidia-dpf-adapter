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

package mapping

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const writersPath = "../../config/condition-writers.yaml"

func TestLoadConditionWriterTable(t *testing.T) {
	tab, err := LoadConditionWriterTable(writersPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(tab.Writers) < 4 {
		t.Fatalf("got %d writers, want at least daemon Ready + DPFReady + DSCReady + SFC DPFReady", len(tab.Writers))
	}
}

func TestConditionWriterTableRejectsCollision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "writers.yaml")
	body := []byte(`
apiVersion: translation.opi.nvidia.com/v1alpha1
kind: ConditionWriterTable
writers:
  - object: {group: config.openshift.io, version: v1, kind: DataProcessingUnit}
    type: DPFReady
    writer: mapping:dataprocessingunit
    schema: conditions
  - object: {group: config.openshift.io, version: v1, kind: DataProcessingUnit}
    type: DPFReady
    writer: mapping:amd-dsc200
    schema: conditions
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConditionWriterTable(path)
	if err == nil {
		t.Fatal("Load accepted two writers of DPFReady on DataProcessingUnit")
	}
	if !strings.Contains(err.Error(), "overwrite") {
		t.Errorf("error does not name the collision: %v", err)
	}
}

func TestConditionWriterTableRejectsNonDaemonReady(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "writers.yaml")
	body := []byte(`
apiVersion: translation.opi.nvidia.com/v1alpha1
kind: ConditionWriterTable
writers:
  - object: {group: config.openshift.io, version: v1, kind: DataProcessingUnit}
    type: Ready
    writer: mapping:dataprocessingunit
    schema: conditions
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConditionWriterTable(path)
	if err == nil {
		t.Fatal("Load accepted Ready owned by a mapping")
	}
	if !strings.Contains(err.Error(), DaemonWriter) {
		t.Errorf("error does not name %s: %v", DaemonWriter, err)
	}
}

func TestShippedMappingsMatchWriterTable(t *testing.T) {
	tab, err := LoadConditionWriterTable(writersPath)
	if err != nil {
		t.Fatal(err)
	}
	specs, err := LoadDir(mappingDir)
	if err != nil {
		t.Fatal(err)
	}

	type key struct{ gvk, cond string }
	rows := map[key]ConditionWriter{}
	for _, w := range tab.Writers {
		k := key{w.Object.Group + "/" + w.Object.Version + "/" + w.Object.Kind, w.Type}
		rows[k] = w
	}

	for _, s := range specs {
		if s.Status == nil {
			continue
		}
		gvk := s.Source.Group + "/" + s.Source.Version + "/" + s.Source.Kind
		for _, c := range s.Status.Conditions {
			w, ok := rows[key{gvk, c.Type}]
			if !ok {
				t.Errorf("mapping %q writes %s on %s but config/condition-writers.yaml has no row",
					s.Metadata.Name, c.Type, s.Source.Kind)
				continue
			}
			wantWriter := MappingWriter(s.Metadata.Name)
			if w.Writer != wantWriter {
				t.Errorf("table writer for %s on %s is %q, want %q",
					c.Type, s.Source.Kind, w.Writer, wantWriter)
			}
			if w.Schema != s.Status.Schema {
				t.Errorf("mapping %q status.schema=%q, table schema=%q; they must agree so prune vs write is not a comment",
					s.Metadata.Name, s.Status.Schema, w.Schema)
			}
		}
	}
}

func TestWriterTableSchemaAgreesWithVendoredCRDs(t *testing.T) {
	tab, err := LoadConditionWriterTable(writersPath)
	if err != nil {
		t.Fatal(err)
	}
	pin := loadPinnedVersions(t)
	idx := loadCRDIndex(t,
		filepath.Join(repoRoot(t), pin.OPI.Fixtures),
		filepath.Join(repoRoot(t), pin.DPF.Fixtures),
	)

	for _, w := range tab.Writers {
		schema := idx[w.Object.Group+"/"+w.Object.Kind]
		if schema == nil {
			// DpuOperatorConfig is not vendored; the table still assigns it.
			continue
		}
		hasConditions := schemaHasPath(schema, "status.conditions")
		switch w.Schema {
		case StatusSchemaConditions:
			if !hasConditions {
				t.Errorf("%s/%s schema=conditions but vendored CRD has no status.conditions; writes would be pruned",
					w.Object.Kind, w.Type)
			}
		case StatusSchemaPruned:
			if hasConditions {
				t.Errorf("%s/%s schema=pruned but vendored CRD has status.conditions; flip the row to conditions",
					w.Object.Kind, w.Type)
			}
		}
	}
}
