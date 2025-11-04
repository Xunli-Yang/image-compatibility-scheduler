package compatibilityPlugin

import (
	"time"

	"k8s.io/kubernetes/pkg/scheduler/framework"
	nfdv1alpha1 "sigs.k8s.io/node-feature-discovery/api/nfd/v1alpha1"
)

const (
	// PluginName 插件名称
	PluginName = "ImageCompatibilityFilter"
	// JobNamespace 检测Job运行的命名空间
	JobNamespace = "image-validation"
	// JobServiceAccount 检测Job使用的服务账户
	JobServiceAccount = "image-compatibility-checker"
	// JobTimeout 检测超时时间
	JobTimeout = 30 * time.Second
)

// ImageCompatibilityPlugin 镜像兼容性过滤器
type ImageCompatibilityPlugin struct {
	handle     framework.Handle
	jobManager *JobManager
}

// ImageCheckJobSpec 镜像检测Job规格
type ImageCompatibilityJobSpec struct {
	Name           string
	NodeName       string
	ImageName      string
	PodName        string
	Namespace      string
	TemplatePath   string
	PlainHttp      bool
	ValidationArgs []string
}

// JobExecutionResult job执行结果
type JobExecutionResult struct {
	Success bool
	Logs    string
	Error   error
}

// ValidationResult 检测结果
type ValidationResult struct {
	Compatible bool
	Reason     string
	Error      error
}

type Compatibility struct {
	// Rules represents a list of Node Feature Rules.
	Rules []nfdv1alpha1.GroupRule `json:"rules"`
	// Weight indicates the priority of the compatibility set.
	Weight int `json:"weight,omitempty"`
	// Tag enables grouping or distinguishing between compatibility sets.
	Tag string `json:"tag,omitempty"`
	// Description of the compatibility set.
	Description string `json:"description,omitempty"`
}

var _ framework.FilterPlugin = &ImageCompatibilityPlugin{}
