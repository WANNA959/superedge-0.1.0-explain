/*
Copyright 2020 The SuperEdge Authors.

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

package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"k8s.io/api/admission/v1beta1"
	"k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"superedge/pkg/edge-health-admission/config"
	"superedge/pkg/edge-health-admission/util"

	"net/http"
)

func EndPoint(w http.ResponseWriter, r *http.Request) {
	serve(w, r, endPoint)
}

// 和nodetaint相似
func endPoint(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	var endpointNew corev1.Endpoints

	klog.V(7).Info("admitting endpoints")
	endpointResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "endpoints"}
	reviewResponse := v1beta1.AdmissionResponse{}
	//检查AdmissionReview.Request.Resource是否为endpoints资源
	if ar.Request.Resource != endpointResource {
		//klog.V(4).Infof("Request is not nodes, ignore, is %s", ar.Request.Resource.String())
		reviewResponse = v1beta1.AdmissionResponse{Allowed: true}
		return &reviewResponse
	}

	//将AdmissionReview.Request.Object.Raw转化为endpoints对象
	reviewResponseEndPoint, endpointNew, err := decodeRawEndPoint(ar, "new")
	if err != nil {
		return reviewResponseEndPoint
	}

	patches := []*Patch{}

	// Endpoints is a collection of endpoints that implement the actual service. Example:

	for i1, EndpointSubset := range endpointNew.Subsets {
		// IP addresses which offer the related ports but are not currently marked as ready
		if len(EndpointSubset.NotReadyAddresses) != 0 {
			for i2, EndpointAddress := range EndpointSubset.NotReadyAddresses {
				// Node hosting this endpoint.
				if node, err := config.Kubeclient.CoreV1().Nodes().Get(context.TODO(), *EndpointAddress.NodeName, metav1.GetOptions{}); err != nil {
					klog.Errorf("can't get pod's node err: %v", err)
				} else {
					_, condition := util.GetNodeCondition(&node.Status, v1.NodeReady)
					// 不含nodeunhealth annotation & condition.Status == v1.ConditionUnknown
					if _, ok := node.Annotations["nodeunhealth"]; !ok && condition.Status == v1.ConditionUnknown {
						// 分布式健康检测正常，则将该EndpointAddress从endpoints.Subset.NotReadyAddresses
						// 移到endpoints.Subset.Addresses

						// 1、endpoints.Subset.NotReadyAddresses 删除
						patches = append(patches, &Patch{
							OP:   "remove",
							Path: fmt.Sprintf("/subsets/%d/notReadyAddresses/%d", i1, i2),
						})

						// 2、endpoints.Subset.Addresses 添加
						TargetRef := map[string]interface{}{}
						TargetRef["kind"] = EndpointAddress.TargetRef.Kind
						TargetRef["namespace"] = EndpointAddress.TargetRef.Namespace
						TargetRef["name"] = EndpointAddress.TargetRef.Name
						TargetRef["uid"] = EndpointAddress.TargetRef.UID
						TargetRef["apiVersion"] = EndpointAddress.TargetRef.APIVersion
						TargetRef["resourceVersion"] = EndpointAddress.TargetRef.ResourceVersion
						TargetRef["fieldPath"] = EndpointAddress.TargetRef.FieldPath

						patches = append(patches, &Patch{
							OP:   "add",
							Path: fmt.Sprintf("/subsets/%d/addresses/%d", i1, i2),
							Value: map[string]interface{}{
								"ip":        EndpointAddress.IP,
								"hostname":  EndpointAddress.Hostname,
								"nodeName":  EndpointAddress.NodeName,
								"targetRef": TargetRef,
							},
						})

						if len(patches) != 0 {
							bytes, _ := json.Marshal(patches)
							reviewResponse.Patch = bytes
							pt := v1beta1.PatchTypeJSONPatch
							reviewResponse.PatchType = &pt
						}
					}
				}
			}
		}
	}
	// Allowed indicates whether or not the admission request was permitted.
	reviewResponse.Allowed = true
	return &reviewResponse
}

func decodeRawEndPoint(ar v1beta1.AdmissionReview, version string) (*v1beta1.AdmissionResponse, corev1.Endpoints, error) {
	var raw []byte
	if version == "new" {
		raw = ar.Request.Object.Raw
	} else if version == "old" {
		raw = ar.Request.OldObject.Raw
	}

	endpoint := corev1.Endpoints{}
	deserializer := Codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &endpoint); err != nil {
		klog.Error(err)
		return toAdmissionResponse(err), endpoint, err
	}
	return nil, endpoint, nil
}
