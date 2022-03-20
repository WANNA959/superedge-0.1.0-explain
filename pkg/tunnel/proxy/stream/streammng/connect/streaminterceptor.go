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
	"context"
	"fmt"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
	"os"
	"strings"
	ctx "superedge/pkg/tunnel/context"
	"superedge/pkg/tunnel/proto"
	"superedge/pkg/tunnel/token"
	"superedge/pkg/tunnel/util"
	"time"
)

var (
	ErrMissingMetadata = status.Errorf(codes.InvalidArgument, "missing metadata")
	ErrInvalidToken    = status.Errorf(codes.Unauthenticated, "invalid token")
)
var clientToken string

/*
拦截器方法
通过调用streamer 可以获得 ClientStream, 包装ClientStream 并重载他的 RecvMsg 和 SendMsg 方法，即可做一些拦截处理
*/

/*
wrappedClientStream部分
*/

type wrappedClientStream struct {
	grpc.ClientStream
	restart bool
}

func newClientWrappedStream(s grpc.ClientStream) grpc.ClientStream {
	return &wrappedClientStream{s, false}
}

func ClientStreamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	var credsConfigured bool
	for _, o := range opts {
		_, ok := o.(*grpc.PerRPCCredsCallOption)
		if ok {
			credsConfigured = true
		}
	}
	// 将边缘节点名称以及 token 构造成 oauth2.Token.AccessToken 进行认证传递
	if !credsConfigured {
		opts = append(opts, grpc.PerRPCCredentials(oauth.NewOauthAccess(&oauth2.Token{
			AccessToken: clientToken,
		})))
	}
	s, err := streamer(ctx, desc, cc, method, opts...)
	if err != nil {
		return nil, err
	}
	return newClientWrappedStream(s), nil
}

func (w *wrappedClientStream) SendMsg(m interface{}) error {
	if m != nil {
		return w.ClientStream.SendMsg(m)
	}

	// edge端 发送gRPC消息的时候添加node节点（添加到nodeContext中，nodes仅此一个node
	nodeName := os.Getenv(util.NODE_NAME_ENV)
	node := ctx.GetContext().AddNode(nodeName)
	klog.Infof("node added successfully node = %s", nodeName)
	stopHeartbeat := make(chan struct{}, 1)

	defer func() {
		stopHeartbeat <- struct{}{}
		ctx.GetContext().RemoveNode(nodeName)
		klog.Infof("node removed successfully node = %s", nodeName)
	}()

	// 一个goroutine 向node channel发送 StreamMsg（两种类型
	go func(hnode ctx.Node, hw *wrappedClientStream, heartbeatStop chan struct{}) {
		count := 0
		for {
			select {
			// 1min周期内recv没有处理 or 有err产生，则close
			case <-time.After(60 * time.Second):
				if w.restart {
					klog.Errorf("streamClient failed to receive heartbeat message count:%v", count)
					// 连续两次restart=true
					if count >= 1 {
						klog.Error("streamClient receiving heartbeat timeout, container exits")
						klog.Flush()
						os.Exit(1)
					}

					hnode.Send2Node(&proto.StreamMsg{
						Node:     os.Getenv(util.NODE_NAME_ENV),
						Category: util.STREAM,
						Type:     util.CLOSED,
					})
					count += 1
				} else {
					// 理想情况下，一直走else，因为在recvMsg中，edge node总能在1min内收到cloud的heartbeat消息（restart=false
					// 正常发送STREAM_HEART_BEAT消息
					hnode.Send2Node(&proto.StreamMsg{
						Node:     os.Getenv(util.NODE_NAME_ENV),
						Category: util.STREAM,
						Type:     util.STREAM_HEART_BEAT,
						Topic:    os.Getenv(util.NODE_NAME_ENV) + util.STREAM_HEART_BEAT,
					})
					klog.V(8).Info("streamClient send heartbeat message")
					w.restart = true
					count = 0
				}
			case <-heartbeatStop:
				klog.Error("streamClient exits heartbeat sending")
				return
			}
		}
	}(node, w, stopHeartbeat)

	// 一个for 通过channel接收StreamMsg，直到 msg.type=closed
	for {
		msg := <-node.NodeRecv()
		if msg.Category == util.STREAM && msg.Type == util.CLOSED {
			klog.Error("streamClient turns off message sending")
			return fmt.Errorf("streamClient stops sending messages to server node: %s", os.Getenv(util.NODE_NAME_ENV))
		}
		klog.V(8).Infof("streamClinet starts to send messages to the server node: %s uuid: %s", msg.Node, msg.Topic)

		// 发送 STREAM_HEART_BEAT 类型 msg
		err := w.ClientStream.SendMsg(msg)
		if err != nil {
			klog.Errorf("streamClient failed to send message err = %v", err)
			return err
		}
		klog.V(8).Infof("streamClinet successfully send a message to the server node: %s uuid: %s", msg.Node, msg.Topic)
	}
}

func (w *wrappedClientStream) RecvMsg(m interface{}) error {
	if m != nil {
		return w.ClientStream.RecvMsg(m)
	}
	for {
		msg := &proto.StreamMsg{}
		err := w.ClientStream.RecvMsg(msg)
		// 有err产生，则close
		if err != nil {
			klog.Error("streamClient failed to receive message")
			node := ctx.GetContext().GetNode(os.Getenv(util.NODE_NAME_ENV))
			if node != nil {
				node.Send2Node(&proto.StreamMsg{
					Node:     os.Getenv(util.NODE_NAME_ENV),
					Category: util.STREAM,
					Type:     util.CLOSED,
				})
			}
			return err
		}
		klog.V(8).Infof("streamClient recv msg node: %s uuid: %s", msg.Node, msg.Topic)
		// 接收到msg类型为 STREAM_HEART_BEAT
		if msg.Category == util.STREAM && msg.Type == util.STREAM_HEART_BEAT {
			klog.V(8).Info("streamClient received heartbeat message")
			// 重置restart
			w.restart = false
			continue
		}
		// 调用RegisterHandler中对应的handler
		ctx.GetContext().Handler(msg, msg.Type, msg.Category)
	}
}

/*
wrappedServerStream部分

云端 gRPC 服务在接受到 tunnel-edge 请求(建立 Stream 流)时，会调用 ServerStreamInterceptor，
而 ServerStreamInterceptor 会从gRPC metadata 中解析出此 gRPC 连接对应的边缘节点名和token，并对该 token 进行校验，
然后根据节点名构建 wrappedServerStream 作为与该边缘节点通信的处理对象(每个边缘节点对应一个处理对象)，
handler 函数会调用 stream.TunnelStreaming，
并将 wrappedServerStream 传递给它(wrappedServerStream 实现了proto.Stream_TunnelStreamingServer 接口)
*/

func ServerStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	klog.Info("start verifying the token !")
	md, ok := metadata.FromIncomingContext(ss.Context())
	if !ok {
		klog.Error("missing metadata")
		return ErrMissingMetadata
	}
	if len(md["authorization"]) < 1 {
		klog.Errorf("failed to obtain token")
		return fmt.Errorf("failed to obtain token")
	}
	// get bearer token
	tk := strings.TrimPrefix(md["authorization"][0], "Bearer ")
	auth, err := token.ParseToken(tk)
	if err != nil {
		klog.Error("token deserialization failed !")
		return err
	}

	// 校验token合法性
	if auth.Token != token.GetTokenFromCache(auth.NodeName) {
		klog.Errorf("invalid token node = %s", auth.NodeName)
		return ErrInvalidToken
	}
	klog.Infof("token verification successful node = %s", auth.NodeName)
	err = handler(srv, newServerWrappedStream(ss, auth.NodeName))
	if err != nil {
		//当 TunnelStreaming 方法退出时，执行移除节点的逻辑ctx.GetContext().RemoveNode
		ctx.GetContext().RemoveNode(auth.NodeName)
		klog.Errorf("node disconnected node = %s err = %v", auth.NodeName, err)
	}
	return err
}

func newServerWrappedStream(s grpc.ServerStream, node string) grpc.ServerStream {
	return &wrappedServerStream{s, node}
}

type wrappedServerStream struct {
	grpc.ServerStream
	node string
}

/*
SendMsg 会从 wrappedServerStream 对应边缘节点 node 中接受 StreamMsg，
并调用 ServerStream.SendMsg 发送该消息给 tunnel-edge
*/
func (w *wrappedServerStream) SendMsg(m interface{}) error {
	if m != nil {
		return w.ServerStream.SendMsg(m)
	}
	//
	node := ctx.GetContext().AddNode(w.node)
	klog.Infof("node added successfully node = %s", node.GetName())
	defer klog.Infof("streamServer no longer sends messages to edge node: %s", w.node)
	for {
		// cloud node上channel中
		msg := <-node.NodeRecv()
		// closed类型，连接断开 return
		if msg.Category == util.STREAM && msg.Type == util.CLOSED {
			klog.Error("streamServer turns off message sending")
			return fmt.Errorf("streamServer stops sending messages to node: %s", w.node)
		}
		klog.V(8).Infof("streamServer starts to send messages to the client node: %s uuid: %s", msg.Node, msg.Topic)
		err := w.ServerStream.SendMsg(msg)
		if err != nil {
			klog.Errorf("streamServer failed to send a message to the edge node: %s", w.node)
			return err
		}
		klog.V(8).Infof("StreamServer sends a message to the client successfully node: %s uuid: %s", msg.Node, msg.Topic)
	}
}

func (w *wrappedServerStream) RecvMsg(m interface{}) error {
	if m != nil {
		return w.ServerStream.RecvMsg(m)
	}
	defer klog.V(8).Infof("streamServer no longer receives messages from edge node: %s", w.node)
	for {
		msg := &proto.StreamMsg{}
		err := w.ServerStream.RecvMsg(msg)
		klog.V(8).Infof("streamServer receives messages node: %s ", w.node)
		if err != nil {
			klog.Errorf("streamServer failed to receive a message to the edge node: %s", w.node)
			node := ctx.GetContext().GetNode(w.node)
			if node != nil {
				node.Send2Node(&proto.StreamMsg{
					Node:     w.node,
					Category: util.STREAM,
					Type:     util.CLOSED,
				})
			}
			return err
		}
		klog.V(8).Infof("streamServer received the message successfully node: %s uuid: %s", msg.Node, msg.Topic)
		ctx.GetContext().Handler(msg, msg.Type, msg.Category)
	}
}

func InitToken(nodeName, tk string) error {
	var err error
	clientToken, err = token.GetTonken(nodeName, tk)
	klog.Infof("stream clinet token nodename = %s token = %s", nodeName, tk)
	if err != nil {
		klog.Error("client get token fail !")
	}
	return err
}
