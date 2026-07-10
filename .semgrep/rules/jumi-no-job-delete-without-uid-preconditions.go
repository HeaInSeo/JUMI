package fixtures

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func bad(ctx context.Context, client kubernetes.Interface, ns, name string) error {
	// ruleid: jumi-no-job-delete-without-uid-preconditions
	return client.BatchV1().Jobs(ns).Delete(ctx, name, metav1.DeleteOptions{})
}

func good(ctx context.Context, client kubernetes.Interface, ns, name string, preconditions *metav1.Preconditions) error {
	// ok: jumi-no-job-delete-without-uid-preconditions
	return client.BatchV1().Jobs(ns).Delete(ctx, name, metav1.DeleteOptions{Preconditions: preconditions})
}
