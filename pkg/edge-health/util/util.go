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

package util

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"k8s.io/api/core/v1"
	"os"
	"os/signal"
	"superedge/pkg/edge-health/check"
	"superedge/pkg/edge-health/common"
	"superedge/pkg/edge-health/data"
)

func GenerateHmac(communicatedata data.CommunicateData) (string, error) {
	part1byte, _ := json.Marshal(communicatedata.SourceIP)
	part2byte, _ := json.Marshal(communicatedata.ResultDetail)
	hmacBefore := string(part1byte) + string(part2byte)
	// common.HmacConfig这个configMap 下的 common.HmacKey 字段作为 hash key
	// sourceip+ResultDetail string作为value进行sha256加密
	if hmacconf, err := check.ConfigMapManager.ConfigMapLister.ConfigMaps("kube-system").Get(common.HmacConfig); err != nil {
		return "", err
	} else {
		return GetHmacCode(hmacBefore, hmacconf.Data[common.HmacKey])
	}
}

func GetHmacCode(s, key string) (string, error) {
	h := hmac.New(sha256.New, []byte(key))
	if _, err := io.WriteString(h, s); err != nil {
		return "", err
	}
	/*
		执行原理为：myHash.Write(b1)写入的数据进行hash运算  +  myHash.Sum(b2)写入的数据进行hash运算
		结果为：两个hash运算结果的拼接。若myHash.Write()省略或myHash.Write(nil) ，则默认为写入的数据为“”。
		根据以上原理，一般不采用两个hash运算的拼接，所以参数为nil
	*/
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func GetNodeNameByIp(nodes []v1.Node, Ip string) string {
	for _, v := range nodes {
		for _, i := range v.Status.Addresses {
			if i.Type == v1.NodeInternalIP {
				if i.Address == Ip {
					return v.Name
				}
			}
		}
	}
	return ""
}

func SignalWatch() (context.Context, context.CancelFunc) {
	ctx, cancelFunc := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 1)
	// 监控两种signal 后面goroutine都可以通过ctx cnacel优雅退出
	signal.Notify(signals, unix.SIGTERM, unix.SIGINT)
	go func() {
		for range signals {
			cancelFunc()
		}
	}()
	return ctx, cancelFunc
}
