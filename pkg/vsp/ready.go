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

package vsp

import (
	"context"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/discovery"
	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/lifecycle"
	"github.com/kartikeyg0104/opi-nvidia-dpf-adapter/pkg/mapping"
)

const (
	readyReasonHealthy   = "DeviceHealthy"
	readyReasonUnhealthy = "DeviceUnhealthy"
	readyMsgHealthy      = "VSP handshake enumerated a healthy device"
	readyMsgUnhealthy    = "VSP handshake reported no healthy device"
)

// ReportReady is the vsp-opi-daemon writer of Ready. The translator and
// LCM never write that type. A successful Init/GetDevices handshake
// stamps Ready on the matching DataProcessingUnit and on DpuOperatorConfig.
func ReportReady(ctx context.Context, c client.Client, hs *HandshakeResult) error {
	if c == nil || hs == nil {
		return nil
	}
	serials := map[string]struct{}{}
	healthy := false
	for _, d := range hs.Devices {
		if d == nil || strings.TrimSpace(d.ID) == "" {
			continue
		}
		serials[d.ID] = struct{}{}
		if d.Health == "" || strings.EqualFold(d.Health, "HEALTHY") {
			healthy = true
		}
	}
	if len(serials) == 0 {
		return nil
	}
	if err := reportReadyOnGVKs(ctx, c, discovery.DataProcessingUnitGVK, serials, healthy, matchDPUSerial); err != nil {
		return err
	}
	return reportReadyOnGVKs(ctx, c, lifecycle.DpuOperatorConfigGVK, serials, healthy, matchAll)
}

func matchDPUSerial(obj *unstructured.Unstructured, serials map[string]struct{}) bool {
	_, ok := serials[obj.GetAnnotations()[discovery.SerialNumberAnnotation]]
	return ok
}

func matchAll(*unstructured.Unstructured, map[string]struct{}) bool { return true }

func reportReadyOnGVKs(
	ctx context.Context,
	c client.Client,
	gvk schema.GroupVersionKind,
	serials map[string]struct{},
	healthy bool,
	match func(*unstructured.Unstructured, map[string]struct{}) bool,
) error {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))
	if err := c.List(ctx, list); err != nil {
		return err
	}
	for i := range list.Items {
		obj := &list.Items[i]
		if !match(obj, serials) {
			continue
		}
		if err := patchReady(ctx, c, gvk, obj, healthy); err != nil {
			return err
		}
	}
	return nil
}

func patchReady(ctx context.Context, c client.Client, gvk schema.GroupVersionKind, obj *unstructured.Unstructured, healthy bool) error {
	key := client.ObjectKeyFromObject(obj)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fresh := &unstructured.Unstructured{}
		fresh.SetGroupVersionKind(gvk)
		if err := c.Get(ctx, key, fresh); err != nil {
			return err
		}
		if !upsertReadyCondition(fresh, healthy) {
			return nil
		}
		return c.Status().Update(ctx, fresh)
	})
}

func upsertReadyCondition(obj *unstructured.Unstructured, healthy bool) bool {
	wantStatus := "True"
	reason := readyReasonHealthy
	message := readyMsgHealthy
	if !healthy {
		wantStatus = "False"
		reason = readyReasonUnhealthy
		message = readyMsgUnhealthy
	}
	raw, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	now := time.Now().UTC().Format(time.RFC3339)
	out := make([]any, 0, len(raw)+1)
	found := false
	changed := false
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		if m["type"] != mapping.ReservedConditionType {
			out = append(out, m)
			continue
		}
		found = true
		next := map[string]any{}
		for k, v := range m {
			next[k] = v
		}
		if next["status"] != wantStatus || next["reason"] != reason || next["message"] != message {
			next["status"] = wantStatus
			next["reason"] = reason
			next["message"] = message
			if next["status"] != m["status"] || next["lastTransitionTime"] == nil || next["lastTransitionTime"] == "" {
				next["lastTransitionTime"] = now
			}
			changed = true
		}
		out = append(out, next)
	}
	if !found {
		out = append(out, map[string]any{
			"type":               mapping.ReservedConditionType,
			"status":             wantStatus,
			"reason":             reason,
			"message":            message,
			"lastTransitionTime": now,
		})
		changed = true
	}
	if !changed {
		return false
	}
	if obj.Object["status"] == nil {
		obj.Object["status"] = map[string]any{}
	}
	_ = unstructured.SetNestedSlice(obj.Object, out, "status", "conditions")
	return true
}
