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

package main

import (
	goflag "flag"
	"math/rand"
	"os"
	"time"

	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"

	"github.com/spf13/pflag"

	"superedge/cmd/lite-apiserver/app"
)

// lite-apiserver入口地址
func main() {
	rand.Seed(time.Now().UnixNano())

	command := app.NewServerCommand()

	// flag _替换为-
	pflag.CommandLine.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	// 支持go flag
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	logs.InitLogs()
	defer logs.FlushLogs()

	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
