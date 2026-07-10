package fixtures

import corev1 "k8s.io/api/core/v1"

func badLiteral(node string) corev1.PodSpec {
	// ruleid: jumi-no-direct-podspec-nodename
	return corev1.PodSpec{NodeName: node}
}

func badAssignment(pod *corev1.Pod, node string) {
	// ruleid: jumi-no-direct-podspec-nodename
	pod.Spec.NodeName = node
}

func goodSelector(node string) corev1.PodSpec {
	// ok: jumi-no-direct-podspec-nodename
	return corev1.PodSpec{NodeSelector: map[string]string{"kubernetes.io/hostname": node}}
}
