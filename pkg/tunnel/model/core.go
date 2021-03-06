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

package model

import (
	"k8s.io/klog"
	"os"
	"os/signal"
	"superedge/pkg/tunnel/context"
	"syscall"
)

// 一共register了三个module（stream tcp https
func LoadModules(mode string) {
	modules := GetModules()
	for n, m := range modules {
		// malloc CallBack：ctx.protocols[module] = map[string]CallBack{}
		context.GetContext().AddModule(n)
		klog.Infof("starting module:%s", m.Name())
		m.Start(mode)
		klog.Infof("start module:%s success !", m.Name())
	}

}

// 增加signal处理函数，几个module优雅退出
func ShutDown() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGILL, syscall.SIGTRAP, syscall.SIGABRT)
	s := <-c
	klog.Info("got os signal " + s.String())
	modules := GetModules()
	for name, module := range modules {
		klog.Info("cleanup module " + name)
		module.CleanUp()
	}
}
