package compatibilityPlugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	nfdvalidator "sigs.k8s.io/node-feature-discovery/pkg/client-nfd/compat/node-validator"
)

// JobManager 管理镜像兼容性检测Job
type JobManager struct {
	client    kubernetes.Interface
	namespace string
}

// NewJobManager 创建新的Job管理器
func NewJobManager(client kubernetes.Interface, namespace string) *JobManager {
	return &JobManager{
		client:    client,
		namespace: namespace,
	}
}

// CreateImageCheckJob 创建镜像检测Job
func (jm *JobManager) CreateImageCompatibilityJob(ctx context.Context, spec *ImageCompatibilityJobSpec) (*ValidationResult, error) {
	// ensure namespace exists
	if err := jm.ensureNamespace(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure namespace %s: %v", jm.namespace, err)
	}
	jobName := fmt.Sprintf("image-check-%s-%s", spec.NodeName, rand.String(6))
	job, err := jm.getJobTemplate(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to get job template: %v", err)
	}
	createdJob, err := jm.client.BatchV1().Jobs(spec.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create image compatibility validate job: %v", err)
	}

	klog.Infof("Created image compatibility validate job %s for node %s and image %s",
		jobName, spec.NodeName, spec.ImageName)
	//wait for job to be running
	result, _ := jm.WaitForJobCompletion(ctx, createdJob.Name, spec.Namespace)
	return result, nil
}

// ensure namespace exists
func (jm *JobManager) ensureNamespace(ctx context.Context) error {
	_, err := jm.client.CoreV1().Namespaces().Get(ctx, jm.namespace, metav1.GetOptions{})
	if err == nil {
		klog.V(4).Infof("Namespace %s already exists", jm.namespace)
		return nil
	}
	// if not found, create it
	klog.Infof("Creating namespace %s for image compatibility jobs", jm.namespace)
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: jm.namespace,
			Labels: map[string]string{
				"pod-security.kubernetes.io/enforce":         "privileged",
				"pod-security.kubernetes.io/enforce-version": "latest",
				"pod-security.kubernetes.io/audit":           "privileged",
				"pod-security.kubernetes.io/warn":            "privileged",
				"name":                                       "image-validation",
			},
			Annotations: map[string]string{
				"scheduler.alpha.kubernetes.io/node-selector": "",
				"description": "Namespace for image compatibility validation jobs",
			},
		},
	}
	_, err = jm.client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create namespace %s: %v", jm.namespace, err)
	}
	klog.Infof("Namespace %s created successfully", jm.namespace)
	return nil
}

// WaitForJobCompletion 等待Job完成
func (jm *JobManager) WaitForJobCompletion(ctx context.Context, jobName string, namespace string) (*ValidationResult, error) {
	var job *batchv1.Job
	var err error

	// 等待Job完成或超时
	pollErr := wait.PollUntilContextTimeout(ctx, 2*time.Second, JobTimeout, false, func(ctx context.Context) (bool, error) {
		job, err = jm.client.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to get job %s: %v", jobName, err)
		}
		if job.Status.Succeeded > 0 {
			return true, nil
		}
		if job.Status.Failed > 0 {
			return false, fmt.Errorf("job %s failed", jobName)
		}
		return false, nil
	})
	if pollErr != nil {
		return nil, fmt.Errorf("job %s did not complete in time: %v", jobName, pollErr)
	}
	// acquire job logs
	nodeName, logs, err := jm.fetchJobLogs(ctx, jobName, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch logs for job %s: %v", jobName, err)
	}
	return parseValidationResult(nodeName, logs)
}

// fetch pod logs to get validation result
func (jm *JobManager) fetchJobLogs(ctx context.Context, jobName, namespace string) (string, string, error) {
	pods, err := jm.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return "", "", fmt.Errorf("failed to list pods for job %s: %v", jobName, err)
	}
	if len(pods.Items) == 0 {
		return "", "", fmt.Errorf("no pods found for job %s", jobName)
	}
	pod := pods.Items[0]
	logs, err := jm.client.CoreV1().Pods(namespace).GetLogs(pod.Name, &v1.PodLogOptions{}).Do(ctx).Raw()
	if err != nil {
		return "", "", fmt.Errorf("failed to get logs for pod %s: %v", pod.Name, err)
	}
	return pod.Spec.NodeName, string(logs), nil
}

// getJobTemplate 获取镜像检测Job模板
func (jm *JobManager) getJobTemplate(spec *ImageCompatibilityJobSpec) (*batchv1.Job, error) {
	templatePath := spec.TemplatePath
	if templatePath == "" {
		templatePath = "artifacts/image-validation-job.template"
	}
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read template file %s: %v", templatePath, err)
	}
	var job batchv1.Job
	if err := yaml.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("failed to unmarshal job template: %v", err)
	}

	// replace placeholders
	job.Name = spec.Name
	job.Namespace = spec.Namespace
	job.Spec.Template.Spec.NodeName = spec.NodeName
	for i, container := range job.Spec.Template.Spec.Containers {
		if container.Name == "image-compatibility" {
			args := []string{
				"--image", spec.ImageName,
				"--output-json",
			}
			if spec.PlainHttp {
				args = append(args, "--plain-http")
			}
			container.Args = args
			job.Spec.Template.Spec.Containers[i] = container
			break
		}
	}
	return &job, nil
}

// parse validation result from logs
func parseValidationResult(nodeName, logs string) (*ValidationResult, error) {
	var result ValidationResult
	compatibilities, err := parseLogs(logs)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compatibility results from logs: %v", err)
	}
	isCompatible := true
	var failedRules []nfdvalidator.ProcessedRuleStatus
	for _, compatibility := range compatibilities {
		for _, rule := range compatibility.Rules {
			if !rule.IsMatch {
				failedRules = append(failedRules, rule)
				isCompatible = false
			}
		}
	}
	result.Compatible = isCompatible
	if !isCompatible {
		reasonBytes, _ := json.MarshalIndent(failedRules, "", "  ")
		result.Reason = fmt.Sprintf("Incompatible on node %s. Failed rules: %s", nodeName, string(reasonBytes))
	} else {
		result.Reason = "All compatibility rules passed"
	}
	return &result, nil
}

// parseLogs: parse JSON result from logs
func parseLogs(logs string) ([]nfdvalidator.CompatibilityStatus, error) {
	startIdx := strings.Index(logs, "[{")
	endIdx := strings.LastIndex(logs, "}]")
	if startIdx == -1 || endIdx == -1 || startIdx >= endIdx {
		return nil, fmt.Errorf("no JSON result found in logs")
	}
	jsonResult := logs[startIdx : endIdx+2]
	var compatibilityResults []nfdvalidator.CompatibilityStatus
	if err := json.Unmarshal([]byte(jsonResult), &compatibilityResults); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON result: %v", err)
	}
	return compatibilityResults, nil
}
