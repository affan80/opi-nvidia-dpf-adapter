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

package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPatchedGovulnDependencies is the floor that closed the 2026-10-24
// govulncheck call-graph set (43 IDs). A tidy that silently rolls any of
// these back would reopen HTTP/2 DoS, gRPC OOM, XSS stdlib, and IDNA
// bypass findings without a visible go.mod review.
func TestPatchedGovulnDependencies(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)

	if !strings.Contains(body, "toolchain go1.26.10") {
		t.Error("go.mod must pin toolchain go1.26.10 (stdlib CVEs through GO-2026-6809)")
	}

	for _, pin := range []struct {
		pkg, ver, why string
	}{
		{"google.golang.org/grpc", "v1.84.0", "GO-2026-6348 / 6443 / 6061 (floor v1.83.2)"},
		{"github.com/google/cel-go", "v0.30.0", "GO-2026-6094 import-only"},
		{"golang.org/x/net", "v0.60.0", "HTTP/2 set through GO-2026-6617"},
		{"golang.org/x/text", "v0.42.0", "GO-2026-5970 (floor v0.39.0; x/net v0.60 needs 0.42)"},
		{"go.opentelemetry.io/otel/exporters/otlp/otlptrace", "v1.45.0", "GO-2026-6505"},
		{"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc", "v1.45.0", "GO-2026-6505"},
		{"go.opentelemetry.io/otel/sdk", "v1.45.0", "GO-2026-6505"},
		{"k8s.io/apimachinery", "v0.36.5", "lab k3s minor must stay 1.36"},
		{"k8s.io/client-go", "v0.36.0", "lab k3s minor must stay 1.36"},
	} {
		want := pin.pkg + " " + pin.ver
		if !strings.Contains(body, want) {
			t.Errorf("go.mod missing %s (%s)", want, pin.why)
		}
	}
}
