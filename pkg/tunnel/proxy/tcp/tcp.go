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

package tcp

import (
	uuid "github.com/satori/go.uuid"
	"k8s.io/klog"
	"net"
	"superedge/pkg/tunnel/conf"
	"superedge/pkg/tunnel/context"
	"superedge/pkg/tunnel/model"
	"superedge/pkg/tunnel/proxy/tcp/tcpmng"
	"superedge/pkg/tunnel/proxy/tcp/tcpmsg"
	"superedge/pkg/tunnel/util"
)

type TcpProxy struct {
}

func (tcp *TcpProxy) Register(ctx *context.Context) {
	ctx.AddModule(tcp.Name())
}

func (tcp *TcpProxy) Name() string {
	return util.TCP
}

// 在多集群管理中建立云端管控集群与边缘独立集群的一条 TCP 代理隧道
func (tcp *TcpProxy) Start(mode string) {
	// 注册了 StreamMsg 的处理函数：register三个handler tcp——handeler name: handler
	// TCP_CONTROL 处理函数主要处理关闭连接的消息
	// 在接受到云端组件的请求后，TCP Server 会将请求封装成 StremMsg 发送给 StreamServer，
	// 由 StreamServer 发送到 tunnel-edge,其中 StreamMsg.Type=FrontendHandler，
	// StreamMsg.Node 从已建立的云边隧道的节点中随机选择一个。
	// tunnel-edge 在接受到该StreamMsg 后，会调用 FrontendHandler 函数处理

	// edge发送给cloud，topic=BackendHandler
	context.GetContext().RegisterHandler(util.TCP_BACKEND, tcp.Name(), tcpmsg.BackendHandler)
	// cloud发送给edge，topic=FrontendHandler，edge调用FrontendHandler处理
	context.GetContext().RegisterHandler(util.TCP_FRONTEND, tcp.Name(), tcpmsg.FrontendHandler)
	// close
	context.GetContext().RegisterHandler(util.TCP_CONTROL, tcp.Name(), tcpmsg.ControlHandler)
	if mode == util.CLOUD {
		// "0.0.0.0:6443" = "127.0.0.1:6443"
		for front, backend := range conf.TunnelConf.TunnlMode.Cloud.Tcp {
			go func(front, backend string) {
				ln, err := net.Listen("tcp", front)
				if err != nil {
					klog.Errorf("cloud proxy start %s fail ,error = %s", front, err)
					return
				}
				defer ln.Close()
				klog.Infof("the tcp server of the cloud tunnel listen on %s\n", front)
				for {
					rawConn, err := ln.Accept()
					if err != nil {
						klog.Errorf("cloud proxy accept error!")
						return
					}
					nodes := context.GetContext().GetNodes()
					if len(nodes) == 0 {
						rawConn.Close()
						klog.Errorf("len(nodes)==0")
						continue
					}
					uuid := uuid.NewV4().String()
					node := nodes[0]
					//在云端启动 TCP Server。
					fp := tcpmng.NewTcpConn(uuid, backend, node)
					fp.Conn = rawConn
					fp.Type = util.TCP_FRONTEND
					// 发送给云端组件请求
					go fp.Write()
					// 接收云端组件请求
					go fp.Read()
				}
			}(front, backend)
		}
	}
}

func (tcp *TcpProxy) CleanUp() {
	context.GetContext().RemoveModule(tcp.Name())
}

func InitTcp() {
	model.Register(&TcpProxy{})
	klog.Infof("init module: %s success !", util.TCP)
}
