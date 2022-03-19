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

package stream

import (
	"k8s.io/klog"
	"os"
	"superedge/pkg/tunnel/conf"
	"superedge/pkg/tunnel/context"
	"superedge/pkg/tunnel/model"
	"superedge/pkg/tunnel/proxy/stream/streammng/connect"
	"superedge/pkg/tunnel/proxy/stream/streammsg"
	"superedge/pkg/tunnel/token"
	"superedge/pkg/tunnel/util"
)

type Stream struct {
}

/*
实现Module接口的三个方法
*/

func (stream *Stream) Name() string {
	return util.STREAM
}

func (stream *Stream) Start(mode string) {
	// protocolContext protocols中添加对应的module-key-handler
	context.GetContext().RegisterHandler(util.STREAM_HEART_BEAT, util.STREAM, streammsg.HeartbeatHandler)
	var channelzAddr string
	if mode == util.CLOUD {
		// cloud端启动一个gRPC server
		go connect.StartServer()
		if !conf.TunnelConf.TunnlMode.Cloud.Stream.Dns.Debug {
			// todo 同步coredns的hosts插件的配置文件
			go connect.SynCorefile()
		}
		channelzAddr = conf.TunnelConf.TunnlMode.Cloud.Stream.Server.ChannelzAddr
	} else {
		// edge端
		// 启动gRPC client：init streamConn(clientConn)不断sendMsg & recvMsg
		go connect.StartSendClient()
		channelzAddr = conf.TunnelConf.TunnlMode.EDGE.StreamEdge.Client.ChannelzAddr
	}

	// 起一个goroutine run log server
	go connect.StartLogServer(mode)

	// 起一个goroutine run channel server
	go connect.StartChannelzServer(channelzAddr)
}

func (stream *Stream) CleanUp() {
	context.GetContext().RemoveModule(stream.Name())
}

func InitStream(mode string) {
	// cloud端
	if mode == util.CLOUD {
		if !conf.TunnelConf.TunnlMode.Cloud.Stream.Dns.Debug {
			// init stream coredns
			err := connect.InitDNS()
			if err != nil {
				klog.Errorf("init client-go fail err = %v", err)
				return
			}
		}

		// init tokenData
		err := token.InitTokenCache(conf.TunnelConf.TunnlMode.Cloud.Stream.Server.TokenFile)
		if err != nil {
			klog.Error("Error loading token file ！")
		}
	} else {
		// edge端
		// 根据edge node name环境变量，init clientToken（nodename+client） string
		err := connect.InitToken(os.Getenv(util.NODE_NAME_ENV), conf.TunnelConf.TunnlMode.EDGE.StreamEdge.Client.Token)
		if err != nil {
			klog.Errorf("initialize the edge node token err = %v", err)
			return
		}
	}
	// Modules[STREAM] = m
	model.Register(&Stream{})
	klog.Infof("init module: %s success !", util.STREAM)
}
