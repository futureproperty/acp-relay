package provider_test

import (
	"testing"

	dockerclient "github.com/docker/docker/client"

	"github.com/yourorg/acp-remote/pkg/provider"
)

// TestDockerProviderConfig verifies DockerProvider can be created with a pre-made client.
func TestDockerProviderConfig(t *testing.T) {
	// Create a client with a fake host to avoid connecting to real Docker
	cli, err := dockerclient.NewClientWithOpts(dockerclient.WithHost("tcp://fake-docker:2375"))
	if err != nil {
		t.Fatalf("create docker client: %v", err)
	}
	opts := provider.DockerOptions{ContainerID: "test-container"}
	p, err := provider.NewDockerProvider(cli, opts)
	if err != nil {
		t.Fatalf("NewDockerProvider: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil DockerProvider")
	}
}

// TestDockerProviderImplementsInterface is a compile-time check.
func TestDockerProviderImplementsInterface(t *testing.T) {
	cli, _ := dockerclient.NewClientWithOpts(dockerclient.WithHost("tcp://fake-docker:2375"))
	p, _ := provider.NewDockerProvider(cli, provider.DockerOptions{})
	var _ provider.ExecProvider = p
	t.Log("DockerProvider implements ExecProvider")
}
