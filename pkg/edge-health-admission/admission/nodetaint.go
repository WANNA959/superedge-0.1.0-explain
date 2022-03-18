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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"k8s.io/api/admission/v1beta1"
	"k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"superedge/pkg/edge-health-admission/util"
	edgeutil "superedge/pkg/util"

	"net/http"
)

/*
K8S中Webhook的调用原理为首先向K8S集群中注册一个Admission Webhook（Validating / Mutating）
所谓注册是向K8S集群注册一个地址，而实际Webhook服务可能跑在Pod里，也可能跑在开发机上；
当创建资源的时候会调用这些Webhook进行修改或验证，最后持久化到ETCD中
*/

/*
自定义实现一个AdmissionWebhook Server

如果启用了MutatingAdmission，当开始创建一种k8s资源对象的时候，创建请求会发到你所编写的controller中，然后我们就可以做一系列的操作。
webhook处理apiservers发送的AdmissionReview请求，并将其决定作为AdmissionReview对象发送回去
*/

// admitFunc is the type we use for all of our validators and mutators
// 对应nodeTaint（node resource）+endPoint（endpoint recourse）
type admitFunc func(v1beta1.AdmissionReview) *v1beta1.AdmissionResponse

type Patch struct {
	OP    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

func NodeTaint(w http.ResponseWriter, r *http.Request) {
	serve(w, r, nodeTaint)
}

func nodeTaint(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	var nodeNew, nodeOld corev1.Node

	UnreachNoExecuteTaint := &v1.Taint{
		Key:    corev1.TaintNodeUnreachable,
		Effect: v1.TaintEffectNoExecute,
	}

	klog.V(7).Info("admitting nodes")
	nodeResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}
	reviewResponse := v1beta1.AdmissionResponse{}

	// Resource is the fully-qualified resource being requested (for example, v1.pods)
	if ar.Request.Resource != nodeResource {
		//klog.V(4).Infof("Request is not nodes, ignore, is %s", ar.Request.Resource.String())
		// Allowed indicates whether or not the admission request was permitted.
		// 其他类型的resource直接permitted
		reviewResponse = v1beta1.AdmissionResponse{Allowed: true}
		return &reviewResponse
	}
	// nodeResource需要进一步admission
	//klog.V(4).Infof("Request is nodes, is %s", ar.Request)

	reviewResponseNode, nodeNew, err := decodeRawNode(ar, "new")
	if err != nil {
		return reviewResponseNode
	}
	klog.V(4).Infof("nodeNew is %s", edgeutil.ToJson(nodeNew))

	reviewResponseNode, nodeOld, err = decodeRawNode(ar, "old")
	if err != nil {
		return reviewResponseNode
	}
	klog.V(4).Infof("nodeOld is %s", edgeutil.ToJson(nodeOld))

	// 找到Ready type 的node condition
	_, condition := util.GetNodeCondition(&nodeNew.Status, v1.NodeReady)

	patches := []*Patch{}
	/* Status of the condition, one of True, False, Unknown.
	These are valid condition statuses.
	"ConditionTrue" means a resource is in the condition.
	"ConditionFalse" means a resource is not in the condition.
	"ConditionUnknown" means kubernetes can't decide if a resource is in the condition or not. In the future, we could add other
	*/
	// 如果处于unknown状态，则
	if condition.Status == v1.ConditionUnknown {
		if _, ok := nodeNew.Annotations["nodeunhealth"]; !ok {
			// oldTaint保留，仅记录需要添加的taint
			taintsToAdd, _ := util.TaintSetDiff(nodeNew.Spec.Taints, nodeOld.Spec.Taints)

			// todo ...?
			if _, flag := util.TaintExistsPosition(taintsToAdd, UnreachNoExecuteTaint); flag {
				index, _ := util.TaintExistsPosition(nodeNew.Spec.Taints, UnreachNoExecuteTaint)

				// 若taintsToAdd中存在UnreachNoExecuteTaint这个taint（nodeNew肯定有这个taint
				patches = append(patches, &Patch{
					OP:   "remove",
					Path: fmt.Sprintf("/spec/taints/%d", index),
				})
				klog.V(7).Infof("UnreachNoExecuteTaint: remove %d taints : %s", index, nodeNew.Spec.Taints[index])
			} else if _, resflag := util.TaintExistsPosition(nodeNew.Spec.Taints, UnreachNoExecuteTaint); resflag {
				index, _ := util.TaintExistsPosition(nodeNew.Spec.Taints, UnreachNoExecuteTaint)

				// 若taintsToAdd中存在不存在UnreachNoExecuteTaint这个taint，但是nodeNew有（说明oldNode有
				patches = append(patches, &Patch{
					OP:   "remove",
					Path: fmt.Sprintf("/spec/taints/%d", index),
				})
				klog.V(7).Infof("UnreachNoExecuteTaint still existed: remove %d taints : %s", index, nodeNew.Spec.Taints[index])
			}

			if len(patches) != 0 {
				bytes, _ := json.Marshal(patches)
				reviewResponse.Patch = bytes
				pt := v1beta1.PatchTypeJSONPatch
				reviewResponse.PatchType = &pt
			}
		}
	}

	reviewResponse.Allowed = true

	return &reviewResponse
}

func decodeRawNode(ar v1beta1.AdmissionReview, version string) (*v1beta1.AdmissionResponse, corev1.Node, error) {
	var raw []byte
	if version == "new" {
		// Object is the object from the incoming request.
		raw = ar.Request.Object.Raw
	} else if version == "old" {
		// OldObject is the existing object. Only populated for DELETE and UPDATE requests.
		raw = ar.Request.OldObject.Raw
	}

	node := corev1.Node{}
	deserializer := Codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &node); err != nil {
		klog.Error(err)
		// 通过err构建一个*v1beta1.AdmissionResponse
		return toAdmissionResponse(err), node, err
	}
	return nil, node, nil
}

// serve handles the http portion of a request prior to handing to an admit function
func serve(w http.ResponseWriter, r *http.Request, admit admitFunc) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		klog.Errorf("contentType=%s, expect application/json", contentType)
		return
	}
	// 在admit之前处理http request部分

	klog.V(7).Info(fmt.Sprintf("handling request: %s", body))

	// AdmissionReview describes an admission review request/response.
	// The AdmissionReview that was sent to the webhook
	requestedAdmissionReview := v1beta1.AdmissionReview{}

	// The AdmissionReview that will be returned
	responseAdmissionReview := v1beta1.AdmissionReview{}

	deserializer := Codecs.UniversalDeserializer()
	// request 解码
	if _, _, err := deserializer.Decode(body, nil, &requestedAdmissionReview); err != nil {
		klog.Error(err)
		responseAdmissionReview.Response = toAdmissionResponse(err)
	} else {
		// pass to admitFunc
		responseAdmissionReview.Response = admit(requestedAdmissionReview)
	}

	// Return the same UID
	responseAdmissionReview.Response.UID = requestedAdmissionReview.Request.UID

	klog.V(7).Info(fmt.Sprintf("sending response: %v", responseAdmissionReview.Response))

	// responseAdmissionReview序列化写w http.ResponseWriter
	respBytes, err := json.Marshal(responseAdmissionReview)
	if err != nil {
		klog.Error(err)
	}
	if _, err := w.Write(respBytes); err != nil {
		klog.Error(err)
	}
}

// toAdmissionResponse is a helper function to create an AdmissionResponse with an embedded error
func toAdmissionResponse(err error) *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}
