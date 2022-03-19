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

package https

import (
	"k8s.io/klog"
	"superedge/pkg/tunnel/context"
	"superedge/pkg/tunnel/model"
	"superedge/pkg/tunnel/proxy/https/httpsmng"
	"superedge/pkg/tunnel/proxy/https/httpsmsg"
	"superedge/pkg/tunnel/util"
)

type Https struct {
}

func (https *Https) Name() string {
	return util.HTTPS
}

//Start 函数首先注册了 StreamMsg 的处理函数，其中 CLOSED 处理函数主要处理关闭连接的消息， 并启动 HTTPS Server。
//当云端组件向 tunnel-cloud 发送 HTTPS 请求时，serverHandler 会首先从 request.Host 字段解析节点名，

//若先建立 TLS 连接，然后在连接中写入 HTTP 的 request 对象，此时的 request.Host 可以不设置，
//需要从 request.TLS.ServerName 解析节点名。

//HTTPS Server 读取 request.Body 以及 request.Header 构建 HttpsMsg 结构体，
//并序列化后封装成 StreamMsg，通过 Send2Node 发送 StreamMsg 放入 StreamMsg.node 对应的 node 的 Channel 中，
//由 Stream 模块发送到 tunnel-edge
func (https *Https) Start(mode string) {
	context.GetContext().RegisterHandler(util.CONNECTING, util.HTTPS, httpsmsg.ConnectingHandler)
	context.GetContext().RegisterHandler(util.CONNECTED, util.HTTPS, httpsmsg.ConnectedAndTransmission)
	//CLOSED 处理函数主要处理关闭连接的消息
	context.GetContext().RegisterHandler(util.CLOSED, util.HTTPS, httpsmsg.ConnectedAndTransmission)
	context.GetContext().RegisterHandler(util.TRANSNMISSION, util.HTTPS, httpsmsg.ConnectedAndTransmission)
	if mode == util.CLOUD {
		go httpsmng.StartServer()
	}
}

func (https *Https) CleanUp() {
	context.GetContext().RemoveModule(util.HTTPS)
}

func InitHttps() {
	model.Register(&Https{})
	klog.Infof("init module: %s success !", util.HTTPS)
}
