package fixtures

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func badConcat(ctx context.Context, client kubernetes.Interface, ns, job string) {
	// ruleid: jumi-no-job-name-only-pod-watch
	_, _ = client.CoreV1().Pods(ns).Watch(ctx, metav1.ListOptions{LabelSelector: "job-name=" + job})
}

func badSprintf(ctx context.Context, client kubernetes.Interface, ns, job string) {
	// ruleid: jumi-no-job-name-only-pod-watch
	_, _ = client.CoreV1().Pods(ns).Watch(ctx, metav1.ListOptions{LabelSelector: fmt.Sprintf("job-name=%s", job)})
}

func good(ctx context.Context, client kubernetes.Interface, ns string) {
	// ok: jumi-no-job-name-only-pod-watch
	_, _ = client.CoreV1().Pods(ns).Watch(ctx, metav1.ListOptions{LabelSelector: podWatchLabelSelector()})
}

func podWatchLabelSelector() string {
	return "job-name=job-1,jumi.io/attempt-id=attempt-1"
}
