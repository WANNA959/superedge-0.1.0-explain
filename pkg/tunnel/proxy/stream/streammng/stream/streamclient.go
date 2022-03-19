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
	ctx "context"
	"k8s.io/klog"
	"superedge/pkg/tunnel/proto"
)

/*
会并发调用 wrappedClientStream.SendMsg 以及 wrappedClientStream.RecvMsg
分别用于 tunnel-edge 发送以及接受，并阻塞等待
*/
func Send(client proto.StreamClient, clictx ctx.Context) {
	// grpc客户端调用TunnelStreaming得到Stream_TunnelStreamingClient
	stream, err := client.TunnelStreaming(clictx)
	if err != nil {
		klog.Error("EDGE-SEND fetch stream failed !")
		return
	}
	klog.Info("streamClient created successfully")
	errChan := make(chan error, 2)
	/*
		一个goroutine sendMsg
	*/
	go func(send proto.Stream_TunnelStreamingClient, sc chan error) {
		sendErr := send.SendMsg(nil)
		if sendErr != nil {
			klog.Errorf("streamClient failed to send message err = %v", sendErr)
		}
		sc <- sendErr
	}(stream, errChan)

	/*
		一个goroutine RecvMsg 阻塞
		RecvMsg blocks until it receives a message into m or the stream is
		done. It returns io.EOF when the stream completes successfully
	*/
	go func(recv proto.Stream_TunnelStreamingClient, rc chan error) {
		recvErr := recv.RecvMsg(nil)
		if recvErr != nil {
			klog.Errorf("streamClient failed to receive message err = %v", recvErr)
		}
		rc <- recvErr
	}(stream, errChan)

	// 阻塞，直到双向流有一方disconnected
	e := <-errChan
	klog.Errorf("the stream of streamClient is disconnected err = %v", e)
	//concurrently with SendMsg.
	err = stream.CloseSend()
	if err != nil {
		klog.Errorf("failed to close stream send err: %v", err)
	}
}
