package main

import (
	"custom-scheduler/pkg/plugins/compatibilityPlugin"
	"os"

	"k8s.io/component-base/cli"
	"k8s.io/kubernetes/cmd/kube-scheduler/app"
)

func main() {
	command := app.NewSchedulerCommand(
		app.WithPlugin(compatibilityPlugin.PluginName, compatibilityPlugin.New),
	)

	code := cli.Run(command)
	os.Exit(code)
}
