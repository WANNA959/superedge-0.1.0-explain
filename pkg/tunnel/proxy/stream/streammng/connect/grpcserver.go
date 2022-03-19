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

package connect

import (
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/channelz/service"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	"k8s.io/klog"
	"math"
	"net"
	"net/http"
	"strconv"
	"superedge/pkg/tunnel/conf"
	"superedge/pkg/tunnel/context"
	"superedge/pkg/tunnel/proto"
	"superedge/pkg/tunnel/proxy/stream/streammng/stream"
	tunnel "superedge/pkg/tunnel/util"
	"superedge/pkg/util"
	"time"
)

var kaep = keepalive.EnforcementPolicy{
	MinTime:             15 * time.Second,
	PermitWithoutStream: true,
}

var kasp = keepalive.ServerParameters{
	MaxConnectionIdle:     time.Duration(math.MaxInt64),
	MaxConnectionAge:      time.Duration(math.MaxInt64),
	MaxConnectionAgeGrace: 5 * time.Second,
	Time:                  5 * time.Second,
	Timeout:               1 * time.Second,
}

// start a GPRC server
func StartServer() {
	creds, err := credentials.NewServerTLSFromFile(conf.TunnelConf.TunnlMode.Cloud.Stream.Server.Cert, conf.TunnelConf.TunnlMode.Cloud.Stream.Server.Key)
	if err != nil {
		klog.Errorf("failed to create credentials: %v", err)
		return
	}
	opts := []grpc.ServerOption{grpc.KeepaliveEnforcementPolicy(kaep), grpc.KeepaliveParams(kasp), grpc.StreamInterceptor(ServerStreamInterceptor), grpc.Creds(creds)}
	// build a gRPC server
	s := grpc.NewServer(opts...)
	// register（grpc自动生成
	proto.RegisterStreamServer(s, &stream.Server{})

	// the cloud tunnel listener
	lis, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(conf.TunnelConf.TunnlMode.Cloud.Stream.Server.GrpcPort))
	klog.Infof("the https server of the cloud tunnel  listen on %s", "0.0.0.0:"+strconv.Itoa(conf.TunnelConf.TunnlMode.Cloud.Stream.Server.GrpcPort))
	if err != nil {
		klog.Fatalf("failed to listen: %v", err)
		return
	}
	// server serve
	if err := s.Serve(lis); err != nil {
		klog.Fatalf("failed to serve: %v", err)
		return
	}
}

/*
起一个log server
handleFunc /debug/flags/v
暴露get /healthz
*/
func StartLogServer(mode string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/flags/v", util.UpdateLogLevel)
	ser := &http.Server{
		Handler: mux,
	}
	// 对cloud/edge端，暴露/healthz健康监测url，接收get请求（默认端口cloud8000 edge7000
	if mode == tunnel.CLOUD {
		mux.HandleFunc("/cloud/healthz", func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodGet {
				fmt.Fprintln(writer, context.GetContext().GetNodes())
			} else {
				writer.WriteHeader(http.StatusMethodNotAllowed)
				fmt.Fprintln(writer, "only supports GET method")
			}
		})
		ser.Addr = "0.0.0.0:" + strconv.Itoa(conf.TunnelConf.TunnlMode.Cloud.Stream.Server.LogPort)
	} else {
		mux.HandleFunc("/edge/healthz", EdgeHealthCheck)
		ser.Addr = "0.0.0.0:" + strconv.Itoa(conf.TunnelConf.TunnlMode.EDGE.StreamEdge.Client.LogPort)
	}
	klog.Infof("log server listen on %s", ser.Addr)
	err := ser.ListenAndServe()
	if err != nil {
		klog.Errorf("failed to start log http server err = %v", err)
	}
}

/*
gRPC 提供了 Channelz 用于对外提供服务的数据，用于调试、监控等
默认端口 cloud6000 edge5000
*/
func StartChannelzServer(addr string) {
	if addr == "" {
		klog.Info("channelz server listening address is not configured")
		return
	}
	s := grpc.NewServer()
	service.RegisterChannelzServiceToServer(s)
	reflection.Register(s)
	klog.Infof("channelzServer address: %s", addr)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		klog.Errorf("failed to start channelz tcp server err = %v", err)
		return
	}
	if err := s.Serve(lis); err != nil {
		klog.Errorf("failed to start channelz grpc server  err = %v", err)
		return
	}
}
