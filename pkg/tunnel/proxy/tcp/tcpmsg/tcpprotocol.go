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

package tcpmsg

import (
	"fmt"
	"k8s.io/klog"
	"net"
	"superedge/pkg/tunnel/context"
	"superedge/pkg/tunnel/proto"
	"superedge/pkg/tunnel/proxy/tcp/tcpmng"
	"superedge/pkg/tunnel/util"
)

// add StreamMsg to conn channel
func BackendHandler(msg *proto.StreamMsg) error {
	conn := context.GetContext().GetConn(msg.Topic)
	if conn == nil {
		klog.Errorf("trace_id = %s the stream module failed to distribute the side message module = %s type = %s", msg.Topic, msg.Category, msg.Type)
		return fmt.Errorf("trace_id = %s the stream module failed to distribute the side message module = %s type = %s ", msg.Topic, msg.Category, msg.Type)
	}
	conn.Send2Conn(msg)
	return nil
}

// FrontendHandler 首先使用 StreamMsg.Addr 与 Edge 组件 建立 TCP 连接，启动协程异步对 TCP 连接 Read 和 Write，
// 同时新建 conn 对象(conn.uid=StreamMsg.Topic)，并 将Msg.Data 写入 TCP 连接。
// tunnel-edge 在接收到 Edge Server 的返回数据将其封装为 StreamMsg(StreamMsg.Topic=BackendHandler) 发送到 tunnel-cloud
func FrontendHandler(msg *proto.StreamMsg) error {
	// 已经创建过了
	c := context.GetContext().GetConn(msg.Topic)
	if c != nil {
		c.Send2Conn(msg)
		return nil
	}
	// 在edge端创建一个NewTcpConn
	tp := tcpmng.NewTcpConn(msg.Topic, msg.Addr, msg.Node)
	tp.Type = util.TCP_BACKEND
	tp.C.Send2Conn(msg)
	tcpAddr, err := net.ResolveTCPAddr("tcp", tp.Addr)
	if err != nil {
		klog.Error("edeg proxy resolve addr fail !")
		return err
	}
	// 客户端 dial tcp server
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		klog.Error("edge proxy connect fail!")
		return err
	}
	tp.Conn = conn
	// 接收edge端组件信息
	go tp.Read()
	// 发送给edge端组件信息
	go tp.Write()
	return nil
}

func ControlHandler(msg *proto.StreamMsg) error {
	conn := context.GetContext().GetConn(msg.Topic)
	if conn == nil {
		klog.Errorf("trace_id = %s the stream module failed to distribute the side message module = %s type = %s", msg.Topic, msg.Category, msg.Type)
		return fmt.Errorf("trace_id = %s the stream module failed to distribute the side message module = %s type = %s ", msg.Topic, msg.Category, msg.Type)
	}
	conn.Send2Conn(msg)
	return nil
}
