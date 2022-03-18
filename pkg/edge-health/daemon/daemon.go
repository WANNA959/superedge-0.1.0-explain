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

package daemon

import (
	"context"
	"k8s.io/apimachinery/pkg/util/wait"
	"superedge/cmd/edge-health/app/options"
	checkpkg "superedge/pkg/edge-health/check"
	"superedge/pkg/edge-health/checkplugin"
	"superedge/pkg/edge-health/common"
	"superedge/pkg/edge-health/communicate"
	"superedge/pkg/edge-health/vote"
	"sync"
	"time"
)

type Daemon interface {
	Run(ctx context.Context)
}

type EdgeDaemon struct {
	HealthCheckPeriod     int
	HealthCheckScoreLine  float64
	CommunicatePeriod     int
	CommunicateTimeout    int
	CommunicateRetryTime  int
	CommunicateServerPort int
	VotePeriod            int
	VoteTimeOut           int
	MasterUrl             string
	KubeconfigPath        string
	HostName              string
}

func NewEdgeHealthDaemon(o options.CompletedOptions) Daemon {
	return EdgeDaemon{
		HealthCheckPeriod:    o.CheckOptions.HealthCheckPeriod,
		HealthCheckScoreLine: o.CheckOptions.HealthCheckScoreLine,

		CommunicatePeriod:     o.CommunOptions.CommunicatePeriod,
		CommunicateTimeout:    o.CommunOptions.CommunicateTimeout,
		CommunicateRetryTime:  o.CommunOptions.CommunicateRetryTime,
		CommunicateServerPort: o.CommunOptions.CommunicateServerPort,

		VotePeriod:  o.VoteOptions.VotePeriod,
		VoteTimeOut: o.VoteOptions.VoteTimeOut,

		// 没有default flag传参
		MasterUrl:      o.NodeOptions.MasterUrl,
		KubeconfigPath: o.NodeOptions.KubeconfigPath,
		HostName:       o.NodeOptions.HostName,
	}
}

func (d EdgeDaemon) Run(ctx context.Context) {

	wg := sync.WaitGroup{}

	initialize(d.MasterUrl, d.KubeconfigPath, d.HostName)

	check := checkpkg.NewCheckEdge(checkplugin.PluginInfo.Plugins, d.HealthCheckPeriod, d.HealthCheckScoreLine)

	//TODO: Template pattern

	// 起一个goroutinue run NodeController：主要通过node Informrr处理nodeList的变动
	go checkpkg.NewNodeController(common.ClientSet).Run(ctx)

	// 同NodeController相似：起一个goroutinue run CM Controller：主要通过CM Informrr处理configMap的变动
	go checkpkg.NewConfigMapController(common.ClientSet).Run(ctx)

	// 起一个goroutine，每隔一定的时间，运行check.GetNodeList，直到接收到结束信号 就关闭这个协程
	go wait.Until(check.GetNodeList, time.Duration(check.GetHealthCheckPeriod())*time.Second, ctx.Done())

	// 起一个goroutine，每隔一定的时间，运行check.Check，直到接收到结束信号 就关闭这个协程
	go wait.Until(check.Check, time.Duration(check.GetHealthCheckPeriod())*time.Second, ctx.Done())

	// init commun
	commun := communicate.NewCommunicateEdge(d.CommunicatePeriod, d.CommunicateTimeout, d.CommunicateRetryTime, d.CommunicateServerPort)
	//TODO: Template pattern

	wg.Add(1)

	// 起一个commun server监听 /result
	go commun.Server(ctx, &wg)

	// 起一个goroutine，每隔一定的时间，运行commun.Client，直到接收到结束信号 就关闭这个协程
	go wait.Until(commun.Client, time.Duration(commun.GetPeriod())*time.Second, ctx.Done())

	// init vote
	vote := vote.NewVoteEdge(d.VoteTimeOut, d.VotePeriod)

	// 起一个goroutine，每隔一定的时间，运行vote.Vote，直到接收到结束信号 就关闭这个协程
	go wait.Until(vote.Vote, time.Duration(vote.GetVotePeriod())*time.Second, ctx.Done())

	// 阻塞
	for range ctx.Done() {
		wg.Wait()
		return
	}
}
