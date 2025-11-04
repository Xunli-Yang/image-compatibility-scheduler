package compatibilityPlugin

import (
	"context"
	"fmt"
	"log"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fwk "k8s.io/kube-scheduler/framework"
	framework "k8s.io/kubernetes/pkg/scheduler/framework"
)

// New 创建插件实例
func New(ctx context.Context, configuration runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &ImageCompatibilityPlugin{
		handle:     handle,
		jobManager: NewJobManager(handle.ClientSet(), JobNamespace),
	}, nil
}

// Name 返回插件名称
func (f *ImageCompatibilityPlugin) Name() string {
	return PluginName
}

// Filter 在过滤扩展点执行
func (f *ImageCompatibilityPlugin) Filter(ctx context.Context, state fwk.CycleState, pod *v1.Pod, nodeInfo fwk.NodeInfo) *fwk.Status {
	node := nodeInfo.Node()
	if node == nil {
		log.Printf("NodeInfo for pod %s is nil", pod.Name)
		return fwk.NewStatus(fwk.Error, "node not found")
	}
	log.Printf("filter pod %s on node %s", pod.Name, nodeInfo.Node().Name)

	// 检查pod中所有容器镜像
	for _, container := range pod.Spec.Containers {
		validationResult, err := f.checkImageCompatibility(ctx, pod, node.Name, container.Image)
		if err != nil {
			log.Printf("Error checking image compatibility for pod %s on node %s: %v", pod.Name, node.Name, err)
			return fwk.NewStatus(fwk.Error, fmt.Sprintf("error checking image compatibility: %v", err))
		}
		if !validationResult.Compatible {
			return fwk.NewStatus(fwk.Unschedulable, fmt.Sprintf("image %s is not compatible with node %s: %s",
				container.Image, node.Name, validationResult.Reason))
		}
	}
	return fwk.NewStatus(fwk.Success)
}

// 检查单个镜像兼容性
func (f *ImageCompatibilityPlugin) checkImageCompatibility(ctx context.Context, pod *v1.Pod, nodeName string, imageName string) (*ValidationResult, error) {
	// 创建镜像兼容性检测Job
	jobSpec := &ImageCompatibilityJobSpec{
		Name:      "image-compatibility-check",
		NodeName:  nodeName,
		ImageName: imageName,
		PodName:   pod.Name,
		Namespace: JobNamespace,
	}
	return f.jobManager.CreateImageCompatibilityJob(ctx, jobSpec)
}
