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
	"fmt"
	"os"
	"strings"

	"sigs.k8s.io/yaml"
)

const (
	// ConditionWriterTableKind is the kind of config/condition-writers.yaml.
	ConditionWriterTableKind = "ConditionWriterTable"
	// DaemonWriter is the VSP/OPI per-node daemon, which owns Ready.
	DaemonWriter = "vsp-opi-daemon"
	// LifecycleWriter is the adapter LCM, which owns VendorOperatorSupported.
	LifecycleWriter     = "lifecycle"
	mappingWriterPrefix = "mapping:"
)

// ConditionWriterTable assigns one writer to each (object, condition type).
type ConditionWriterTable struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Writers    []ConditionWriter `json:"writers"`
}

// ConditionWriter is one row of the table.
type ConditionWriter struct {
	Object ObjectRef `json:"object"`
	Type   string    `json:"type"`
	Writer string    `json:"writer"`
	Schema string    `json:"schema"`
}

// LoadConditionWriterTable reads the assigned-writer table.
func LoadConditionWriterTable(path string) (*ConditionWriterTable, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t ConditionWriterTable
	if err := yaml.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if t.Kind != ConditionWriterTableKind {
		return nil, fmt.Errorf("%s: kind %q, want %s", path, t.Kind, ConditionWriterTableKind)
	}
	if len(t.Writers) == 0 {
		return nil, fmt.Errorf("%s: writers is empty", path)
	}
	seen := map[string]string{}
	for i, w := range t.Writers {
		if w.Type == "" || w.Writer == "" || w.Object.Kind == "" {
			return nil, fmt.Errorf("%s writers[%d]: object, type, and writer are required", path, i)
		}
		switch w.Schema {
		case StatusSchemaConditions, StatusSchemaPruned:
		default:
			return nil, fmt.Errorf("%s writers[%d]: schema %q is not %q or %q",
				path, i, w.Schema, StatusSchemaConditions, StatusSchemaPruned)
		}
		if strings.EqualFold(w.Type, ReservedConditionType) && w.Writer != DaemonWriter {
			return nil, fmt.Errorf("%s writers[%d]: Ready is owned by %s, not %s",
				path, i, DaemonWriter, w.Writer)
		}
		key := w.Object.Group + "/" + w.Object.Version + "/" + w.Object.Kind + ":" + w.Type
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("%s: writers %q and %q both claim %s on %s; they would overwrite each other",
				path, prev, w.Writer, w.Type, w.Object.Kind)
		}
		seen[key] = w.Writer
	}
	return &t, nil
}

// MappingWriter is the table writer id for a FieldMapping document.
func MappingWriter(name string) string {
	return mappingWriterPrefix + name
}
