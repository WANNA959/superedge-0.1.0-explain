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
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

/*
各模块需要根据需求实现各自具体的 Scheme（如 kubeadm/scheme）
而具体实现的过程就是将自己的处理逻辑注册到 Scheme，让 Scheme 能真正工作
调用的时候，我们只需要调用框架提供的开放接口，如 Scheme.Default(externalcfg)
框架就会自动地通过反射找到我们已经注册好的处理逻辑进行正确的业务处理了
*/

// scheme is the runtime.Scheme to which all api types are registered.
var scheme = runtime.NewScheme()

// Codecs provides access to encoding and decoding for the scheme.
var Codecs = serializer.NewCodecFactory(scheme)

func init() {
	addToScheme(scheme)
}

// addToScheme builds the kubeadm scheme using all known versions of the kubeadm api.
func addToScheme(scheme *runtime.Scheme) {
	// three versions
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(admissionv1beta1.AddToScheme(scheme))
	utilruntime.Must(admissionregistrationv1beta1.AddToScheme(scheme))
}
