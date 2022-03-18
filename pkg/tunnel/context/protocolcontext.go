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

package context

import (
	"k8s.io/klog"
	"sync"
)

type protocolContext struct {
	// module name-key-callback
	protocols    map[string]map[string]CallBack
	protocolLock sync.RWMutex
}

/*
实现ModuleMng接口
*/

// protocols malloc module map[string]CallBack{}
func (ctx *protocolContext) AddModule(module string) {
	defer ctx.protocolLock.Unlock()
	ctx.protocolLock.Lock()
	if ctx.protocols == nil {
		klog.Error("protocolcontext is not initialized!")
		return
	}
	ctx.protocols[module] = map[string]CallBack{}
}

// 按照module name删除protocals中对应的map
func (ctx *protocolContext) RemoveModule(module string) {
	defer ctx.protocolLock.Unlock()
	ctx.protocolLock.Lock()
	delete(ctx.protocols, module)
}

/*
实现Protocol接口
*/

// 根据module+key获取对应的callback handler
func (ctx *protocolContext) GetHandler(key, module string) CallBack {
	defer ctx.protocolLock.RUnlock()
	ctx.protocolLock.RLock()

	// pre-check
	if ctx.protocols == nil {
		klog.Error("protocolcontext is not initialized!")
		return nil
	}
	_, mok := ctx.protocols[module]
	if !mok {
		klog.Errorf("module %s is not loaded", module)
		return nil
	}

	// 根据module+key获取对应的callback handler
	f, fok := ctx.protocols[module][key]
	if !fok {
		klog.Errorf("module %s is not registered handler %s !", module, key)
		return nil
	}
	return f
}

// set handler————write protocols map:  module name-key-callback
// 某个module下某个key对应的callback handler
func (ctx *protocolContext) RegisterHandler(key, module string, handler CallBack) {
	defer ctx.protocolLock.Unlock()
	ctx.protocolLock.Lock()
	if ctx.protocols == nil {
		klog.Error("protocolcontext is not initialized!")
	}
	_, mok := ctx.protocols[module]
	if !mok {
		klog.Errorf("module %s is not loaded", module)
		return
	}
	ctx.protocols[module][key] = handler
}
