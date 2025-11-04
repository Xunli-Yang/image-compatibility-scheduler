package utils

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// GetK8sClient 获取Kubernetes客户端
func GetK8sClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}
