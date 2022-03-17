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

package check

import (
	"context"
	v1 "k8s.io/api/core/v1"
	runtimeutil "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	"superedge/pkg/edge-health/data"
	"time"
)

var NodeManager *NodeController

type NodeController struct {
	clientset        kubernetes.Interface
	NodeInformer     coreinformers.NodeInformer
	NodeLister       corelisters.NodeLister
	NodeListerSynced cache.InformerSynced
}

func NewNodeController(clientset kubernetes.Interface) *NodeController {
	SharedInformerFactory := informers.NewSharedInformerFactory(clientset, 10*time.Minute)
	// NodeInformer为node提供对共享的informer和lister的访问
	nodeInformer := SharedInformerFactory.Core().V1().Nodes()
	n := &NodeController{}
	// add event handler
	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// 修改data.NodeList
		// 添加node触发
		AddFunc: n.handleNodeAddUpdate,
		// 更新node触发
		UpdateFunc: func(old, cur interface{}) {
			n.handleNodeAddUpdate(cur)
		},
		// 删除node触发
		DeleteFunc: n.handleNodeDelete,
	})
	n.clientset = clientset
	n.NodeInformer = nodeInformer
	// NodeLister helps list Nodes.
	n.NodeLister = nodeInformer.Lister()
	// 如果至少一个完整的 LIST 通知了informar的对象集合的授权状态，则 HasSynced 返回 true
	n.NodeListerSynced = nodeInformer.Informer().HasSynced
	NodeManager = n
	return n
}

func (n *NodeController) handleNodeAddUpdate(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		return
	}
	klog.V(4).Infof("Add/Update Node %s", node.Name)
	data.NodeList.SetNodeListDataByNode(*node)
}

func (n *NodeController) handleNodeDelete(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		return
	}
	klog.V(4).Infof("Delete Node %s", node.Name)
	data.NodeList.DeleteNodeListDataByNode(*node)
}

func (n *NodeController) Run(ctx context.Context) {
	defer runtimeutil.HandleCrash()

	go n.NodeInformer.Informer().Run(ctx.Done())

	if ok := cache.WaitForCacheSync(
		ctx.Done(),
		n.NodeListerSynced,
	); !ok {
		klog.Fatal("failed to wait for caches to sync")
	}

	<-ctx.Done()
}
