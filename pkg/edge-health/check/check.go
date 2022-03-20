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
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/klog"
	"superedge/pkg/edge-health/checkplugin"
	"superedge/pkg/edge-health/common"
	"superedge/pkg/edge-health/data"
	"sync"
)

// 定义并实现一个interface
type Check interface {
	GetNodeList()
	Check()
	AddCheckPlugin(plugins []checkplugin.CheckPlugin)
	CheckPluginsLen() int
	GetHealthCheckPeriod() int
}

type CheckEdge struct {
	HealthCheckPeriod    int
	CheckPlugins         map[string]checkplugin.CheckPlugin
	HealthCheckScoreLine float64
}

func NewCheckEdge(checkplugins []checkplugin.CheckPlugin, healthcheckperiod int, healthCheckScoreLine float64) Check {
	m := make(map[string]checkplugin.CheckPlugin)
	for _, v := range checkplugins {
		m[v.Name()] = v
	}
	return CheckEdge{
		HealthCheckPeriod:    healthcheckperiod,
		HealthCheckScoreLine: healthCheckScoreLine,
		CheckPlugins:         m,
	}

}

func (c CheckEdge) GetNodeList() {
	var hostzone string
	var host *v1.Node

	// Selector represents a label selector.
	masterSelector := labels.NewSelector()
	// 注意操作符是 selection.DoesNotExist 非 master node（即 edge node
	masterRequirement, err := labels.NewRequirement(common.MasterLabel, selection.DoesNotExist, []string{})
	if err != nil {
		klog.Errorf("can't new masterRequirement")
	}
	masterSelector = masterSelector.Add(*masterRequirement)

	// 找到指定hostname的node
	if host, err = NodeManager.NodeLister.Get(common.HostName); err != nil {
		klog.Errorf("GetNodeList: can't get node with hostname %s, err: %v", common.HostName, err)
		return
	}

	/*
		获取NodeList 检测范围
		如果关闭了多地域
			检测获取所有边缘节点列表
		如果开启了多地域
			给节点打上地域标签，检测该region 某些node（有label common.TopologyZone
			没有给节点打上地域标签，则该节点探测时只会检测自己
	*/

	// 找到kube-systerm namespace下的configmap "edge-health-zone-config"
	if config, err := ConfigMapManager.ConfigMapLister.ConfigMaps("kube-system").Get(common.TaintZoneConfig); err != nil { //multi-region cm not found
		// 没有找到configmap（//close multi-region check
		if apierrors.IsNotFound(err) {
			if NodeList, err := NodeManager.NodeLister.List(masterSelector); err != nil {
				klog.Errorf("config not exist, get nodes err: %v", err)
				return
			} else {
				// 检测获取所有边缘节点列表
				data.NodeList.SetNodeListDataByNodeSlice(NodeList)
			}
		} else {
			klog.Errorf("get ConfigMaps edge-health-zone-config err %v", err)
			return
		}
	} else { //multi-region cm found
		/*
				Multi-region Detection:如果开启了多地域但是没有给节点打上地域标签，则该节点探测时只会检测自己

				Label the nodes according to the region with superedgehealth/topology-zone:<zone>(common.TopologyZone

			Create a configmap named `edge-health-zone-config`(common.TaintZoneConfig)
			in the `kube-system` namespace, specify the value of `TaintZoneAdmission` as `true`,
			you can directly use the following yaml to create
			             ```yaml
			             apiVersion: v1
			             kind: ConfigMap
			             metadata:
			               name: edge-health-zone-config
			               namespace: kube-system
			             data:
			               TaintZoneAdmission: true
			             ```
		*/
		klog.V(4).Infof("cm value is %s", config.Data["TaintZoneAdmission"])

		//close multi-region check
		if config.Data["TaintZoneAdmission"] == "false" {
			if NodeList, err := NodeManager.NodeLister.List(masterSelector); err != nil {
				klog.Errorf("config exist, false, get nodes err : %v", err)
				return
			} else {
				data.NodeList.SetNodeListDataByNodeSlice(NodeList)
			}
		} else {
			//open multi-region check
			if _, ok := host.Labels[common.TopologyZone]; ok {
				hostzone = host.Labels[common.TopologyZone]
				klog.V(4).Infof("hostzone is %s", hostzone)

				// 找到该region下其他node（有label common.TopologyZone
				masterzoneSelector := labels.NewSelector()
				zoneRequirement, err := labels.NewRequirement(common.TopologyZone, selection.Equals, []string{hostzone})
				if err != nil {
					klog.Errorf("can't new zoneRequirement")
				}
				masterzoneSelector = masterzoneSelector.Add(*masterRequirement, *zoneRequirement)
				if NodeList, err := NodeManager.NodeLister.List(masterzoneSelector); err != nil {
					klog.Errorf("config exist, true, host has zone label, get nodes err: %v", err)
					return
				} else {
					data.NodeList.SetNodeListDataByNodeSlice(NodeList)
				}
				klog.V(4).Infof("nodelist len is %d", data.NodeList.GetLenListData())
			} else {
				// 如果开启了多地域但是没有给节点打上地域标签，则该节点探测时只会检测自己
				data.NodeList.SetNodeListDataByNodeSlice([]*v1.Node{host})
			}
		}
	}

	iplist := make(map[string]bool)
	tempItems := data.NodeList.CopyNodeListData()
	// 根据NodeList malloc c.CheckInfo
	for _, v := range tempItems {
		for _, i := range v.Status.Addresses {
			if i.Type == v1.NodeInternalIP {
				iplist[i.Address] = true
				data.CheckInfoResult.SetCheckedIpCheckInfo(i.Address)
			}
		}
	}

	// 只关心当前NodeList的NodeInternalIP的iplist，删除其他周期的checkinfo+result（不属于该边缘节点检查范围
	for _, v := range data.CheckInfoResult.TraverseCheckedIpCheckInfo() {
		if _, ok := iplist[v]; !ok {
			data.CheckInfoResult.DeleteCheckedIpCheckInfo(v)
		}
	}

	for k := range data.Result.CopyResultDataAll() {
		if _, ok := iplist[k]; !ok {
			data.Result.DeleteResultData(k)
		}
	}

	klog.V(4).Infof("GetNodeList: checkinfo is %v", data.CheckInfoResult)
}

func (c CheckEdge) CheckPluginsLen() int {
	return len(c.CheckPlugins)
}

func (c CheckEdge) GetHealthCheckPeriod() int {
	return c.HealthCheckPeriod
}

func (c CheckEdge) Check() {
	wg := sync.WaitGroup{}
	// 按plugin数目同步 wait全部完成
	wg.Add(c.CheckPluginsLen())
	for _, plugin := range c.GetCheckPlugins() {
		go plugin.CheckExecute(&wg)
	}
	// 阻塞
	wg.Wait()
	klog.V(4).Info("check finished")
	klog.V(4).Infof("healthcheck: after health check, checkinfo is %v", data.CheckInfoResult.CheckInfo)

	// 通过checkInfo判断result
	calculatetemp := data.CheckInfoResult.CopyCheckInfo()
	// string:checked ip string:Plugin name int:check score
	for desip, plugins := range calculatetemp {
		totalscore := 0.0
		// 对每个checked ip，检查其所有plugin score分数累计是否达标（不低于HealthCheckScoreLine=100
		for _, score := range plugins {
			totalscore += score
		}
		if totalscore >= c.HealthCheckScoreLine {
			// normal
			data.Result.SetResultFromCheckInfo(common.LocalIp, desip, data.ResultDetail{Normal: true})
		} else {
			// not normal
			data.Result.SetResultFromCheckInfo(common.LocalIp, desip, data.ResultDetail{Normal: false})
		}
	}
	klog.V(4).Infof("healthcheck: after health check, result is %v", data.Result.Result)
}

func (c CheckEdge) AddCheckPlugin(plugins []checkplugin.CheckPlugin) {
	for _, p := range plugins {
		c.CheckPlugins[p.Name()] = p
	}
}

func (c CheckEdge) GetCheckPlugins() map[string]checkplugin.CheckPlugin {
	return c.CheckPlugins
}
