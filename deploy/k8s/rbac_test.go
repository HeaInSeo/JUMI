package k8s_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

// The namespaced Role only covers the pod reads if JUMI runs as the bound
// ServiceAccount and creates node Jobs (whose pods it looks up) in the same
// namespace the Role is installed in.
func TestJUMIRuntimeWiringMatchesRoleScope(t *testing.T) {
	raw, err := os.ReadFile("kustomization.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var kust struct{ Namespace string }
	if err := yaml.Unmarshal(raw, &kust); err != nil || kust.Namespace == "" {
		t.Fatalf("kustomization namespace: %q err %v", kust.Namespace, err)
	}
	raw, err = os.ReadFile("jumi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var dep *appsv1.Deployment
	for _, doc := range strings.Split(string(raw), "\n---") {
		var meta struct{ Kind string }
		if err := yaml.Unmarshal([]byte(doc), &meta); err != nil {
			t.Fatal(err)
		}
		if meta.Kind == "Deployment" {
			dep = &appsv1.Deployment{}
			if err := yaml.Unmarshal([]byte(doc), dep); err != nil {
				t.Fatal(err)
			}
		}
	}
	if dep == nil {
		t.Fatal("no Deployment in jumi.yaml")
	}
	spec := dep.Spec.Template.Spec
	if spec.ServiceAccountName != "jumi" {
		t.Errorf("Deployment serviceAccountName = %q, want jumi", spec.ServiceAccountName)
	}
	var jobNamespace string
	for _, e := range spec.Containers[0].Env {
		if e.Name == "JUMI_NAMESPACE" {
			jobNamespace = e.Value
		}
	}
	if jobNamespace != kust.Namespace {
		t.Errorf("JUMI_NAMESPACE = %q, Role namespace = %q: the Role would not cover the node pods", jobNamespace, kust.Namespace)
	}
}

// Both shipped JUMI manifests stay a namespaced Role bound to the jumi
// ServiceAccount, with no wildcard and no cluster-scoped RBAC. findJobPod needs
// pods get/list. pods/exec is deliberately NOT granted: the only exec caller,
// readArtifactsManifest, runs after the node Job succeeded, and exec into a
// completed pod fails ("container not found") even with the grant, while the
// grant would allow exec into every running pod in the namespace (#58).
var manifests = []string{"jumi.yaml", "../devspace/jumi-ah-dev/jumi.yaml"}

type rbacDocs struct {
	roles    []rbacv1.Role
	bindings []rbacv1.RoleBinding
	kinds    []string
}

func load(t *testing.T, path string) rbacDocs {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out rbacDocs
	for _, doc := range strings.Split(string(raw), "\n---") {
		var meta struct{ Kind string }
		if err := yaml.Unmarshal([]byte(doc), &meta); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out.kinds = append(out.kinds, meta.Kind)
		switch meta.Kind {
		case "Role":
			var r rbacv1.Role
			if err := yaml.UnmarshalStrict([]byte(doc), &r); err != nil {
				t.Fatalf("%s Role: %v", path, err)
			}
			out.roles = append(out.roles, r)
		case "RoleBinding":
			var b rbacv1.RoleBinding
			if err := yaml.UnmarshalStrict([]byte(doc), &b); err != nil {
				t.Fatalf("%s RoleBinding: %v", path, err)
			}
			out.bindings = append(out.bindings, b)
		}
	}
	return out
}

// allows reports whether role grants verb on the core-group resource.
func allows(role rbacv1.Role, resource, verb string) bool {
	for _, r := range role.Rules {
		if slices.Contains(r.APIGroups, "") && slices.Contains(r.Resources, resource) && slices.Contains(r.Verbs, verb) {
			return true
		}
	}
	return false
}

func TestJUMIRoleGrantsPodReadsWithoutExec(t *testing.T) {
	for _, path := range manifests {
		t.Run(path, func(t *testing.T) {
			d := load(t, path)
			for _, k := range d.kinds {
				if k == "ClusterRole" || k == "ClusterRoleBinding" {
					t.Fatalf("cluster-scoped RBAC %s must not be shipped", k)
				}
			}
			if len(d.roles) != 1 || d.roles[0].Name != "jumi" {
				t.Fatalf("want exactly one Role named jumi, got %d", len(d.roles))
			}
			role := d.roles[0]

			// Allowed: the pod lookup the backend performs.
			for _, want := range [][2]string{{"pods", "list"}, {"pods", "get"}} {
				if !allows(role, want[0], want[1]) {
					t.Errorf("Role does not allow %s %s", want[1], want[0])
				}
			}
			// Denied: pod exec in any form, and anything broader.
			for _, deny := range [][2]string{
				{"pods/exec", "create"}, {"pods/exec", "get"}, {"pods", "create"}, {"pods", "delete"},
				{"pods/attach", "create"}, {"pods/portforward", "create"}, {"secrets", "get"},
			} {
				if allows(role, deny[0], deny[1]) {
					t.Errorf("Role must not allow %s %s", deny[1], deny[0])
				}
			}
			for _, r := range role.Rules {
				for _, field := range [][]string{r.APIGroups, r.Resources, r.Verbs, r.ResourceNames} {
					if slices.Contains(field, "*") {
						t.Errorf("wildcard in rule %+v", r)
					}
				}
			}

			if len(d.bindings) != 1 {
				t.Fatalf("want exactly one RoleBinding, got %d", len(d.bindings))
			}
			b := d.bindings[0]
			if b.RoleRef.Kind != "Role" || b.RoleRef.Name != "jumi" ||
				len(b.Subjects) != 1 || b.Subjects[0].Kind != "ServiceAccount" || b.Subjects[0].Name != "jumi" {
				t.Errorf("RoleBinding must bind Role jumi to ServiceAccount jumi only: %+v", b)
			}
		})
	}
}
