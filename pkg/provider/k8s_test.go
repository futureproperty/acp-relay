package provider_test

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/futureproperty/acp-relay/pkg/provider"
)

// TestK8sProviderConfig verifies K8sProvider can be created.
func TestK8sProviderConfig(t *testing.T) {
	cs := fake.NewSimpleClientset()
	restCfg := &rest.Config{Host: "https://fake-k8s:6443"}
	opts := provider.K8sOptions{Namespace: "default", PodName: "test-pod", Container: "agent"}
	p := provider.NewK8sProvider(cs, restCfg, opts)
	if p == nil {
		t.Fatal("expected non-nil K8sProvider")
	}
}

// TestK8sProviderEmptyCommand verifies that empty command returns error.
func TestK8sProviderEmptyCommand(t *testing.T) {
	cs := fake.NewSimpleClientset()
	restCfg := &rest.Config{Host: "https://fake-k8s:6443"}
	p := provider.NewK8sProvider(cs, restCfg, provider.K8sOptions{Namespace: "default", PodName: "test-pod"})
	_, err := p.Start(context.Background(), provider.ExecOptions{})
	if err == nil {
		t.Error("expected error for empty command")
	}
}

// TestK8sProviderImplementsInterface compile-time check.
func TestK8sProviderImplementsInterface(t *testing.T) {
	cs := fake.NewSimpleClientset()
	restCfg := &rest.Config{Host: "https://fake-k8s:6443"}
	var _ provider.ExecProvider = provider.NewK8sProvider(cs, restCfg, provider.K8sOptions{})
	t.Log("K8sProvider implements ExecProvider")
}