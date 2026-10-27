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
	"reflect"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
)

var (
	celOnce sync.Once
	celEnv  *cel.Env
	celErr  error
)

func env() (*cel.Env, error) {
	celOnce.Do(func() {
		celEnv, celErr = cel.NewEnv(
			cel.Variable("source", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("item", cel.DynType),
			cel.Variable("children", cel.ListType(cel.DynType)),
			cel.OptionalTypes(),
			ext.Strings(),
		)
	})
	return celEnv, celErr
}

// celVars builds a complete CEL activation. Every declared variable must be
// present at eval time, so unused directions pass their zero value: forward
// field mapping has no children ([]), status mapping has no forEach item ({}).
func celVars(source map[string]any, item any, children []any) map[string]any {
	if item == nil {
		item = map[string]any{}
	}
	if children == nil {
		children = []any{}
	}
	return map[string]any{"source": source, "item": item, "children": children}
}

func evalCEL(expr string, vars map[string]any) (any, error) {
	e, err := env()
	if err != nil {
		return nil, err
	}
	ast, iss := e.Compile(expr)
	if iss.Err() != nil {
		return nil, fmt.Errorf("cel compile %q: %w", expr, iss.Err())
	}
	prg, err := e.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("cel program %q: %w", expr, err)
	}
	out, _, err := prg.Eval(vars)
	if err != nil {
		return nil, fmt.Errorf("cel eval %q: %w", expr, err)
	}
	return celNative(out), nil
}

// celNative unwraps CEL list/map/string wrappers so unstructured JSON
// (and SSA) sees []any / map[string]any, not ref.Val.
func celNative(v any) any {
	if rv, ok := v.(ref.Val); ok {
		if n, err := rv.ConvertToNative(reflect.TypeFor[[]any]()); err == nil {
			return celNative(n)
		}
		if n, err := rv.ConvertToNative(reflect.TypeFor[map[string]any]()); err == nil {
			return celNative(n)
		}
		if n, err := rv.ConvertToNative(reflect.TypeFor[string]()); err == nil {
			return n
		}
		if n, err := rv.ConvertToNative(reflect.TypeFor[bool]()); err == nil {
			return n
		}
		if n, err := rv.ConvertToNative(reflect.TypeFor[int64]()); err == nil {
			return n
		}
		if n, err := rv.ConvertToNative(reflect.TypeFor[float64]()); err == nil {
			return n
		}
		inner := rv.Value()
		if inner == nil || inner == v || inner == rv {
			return inner
		}
		return celNative(inner)
	}
	switch t := v.(type) {
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = celNative(x)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = celNative(x)
		}
		return out
	}
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return v
	}
	switch rv.Kind() {
	case reflect.Slice:
		out := make([]any, rv.Len())
		for i := range rv.Len() {
			out[i] = celNative(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		out := make(map[string]any, rv.Len())
		for _, k := range rv.MapKeys() {
			out[fmt.Sprint(celNative(k.Interface()))] = celNative(rv.MapIndex(k).Interface())
		}
		return out
	default:
		return v
	}
}

func evalBool(expr string, vars map[string]any) (bool, error) {
	if expr == "" {
		return true, nil
	}
	v, err := evalCEL(expr, vars)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("cel %q: expected bool, got %T", expr, v)
	}
	return b, nil
}

// evalString treats a ternary / children CEL expression as code and any
// other value as a literal (AllChildrenReady, static messages).
func evalString(expr string, vars map[string]any) (string, error) {
	if expr == "" {
		return "", nil
	}
	if !strings.Contains(expr, "?") {
		return expr, nil
	}
	v, err := evalCEL(expr, vars)
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("cel %q: expected string, got %T", expr, v)
	}
	return s, nil
}
