package provider

import (
	"context"
	"fmt"
	"io"
	"net/url"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)

// K8sOptions configures the Kubernetes exec provider.
type K8sOptions struct {
	Namespace  string
	PodName    string
	Container  string
	Kubeconfig string // path to kubeconfig; empty uses in-cluster config
}

// K8sProvider starts subprocesses via Kubernetes exec.
type K8sProvider struct {
	cs         kubernetes.Interface
	restConfig *rest.Config
	opts       K8sOptions
}

// NewK8sProvider creates a K8sProvider with the given client and options.
func NewK8sProvider(cs kubernetes.Interface, restConfig *rest.Config, opts K8sOptions) *K8sProvider {
	return &K8sProvider{cs: cs, restConfig: restConfig, opts: opts}
}

// Start executes a command in the configured K8s pod and returns stdio pipes.
func (p *K8sProvider) Start(ctx context.Context, execOpts ExecOptions) (*Process, error) {
	if len(execOpts.Command) == 0 {
		return nil, fmt.Errorf("command is required")
	}

	req := p.cs.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(p.opts.PodName).
		Namespace(p.opts.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: p.opts.Container,
			Command:   execOpts.Command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    false,
			TTY:       false,
		}, scheme.ParameterCodec)

	execURL, err := url.Parse(req.URL().String())
	if err != nil {
		return nil, fmt.Errorf("parse exec url: %w", err)
	}

	exec, err := remotecommand.NewSPDYExecutor(p.restConfig, "POST", execURL)
	if err != nil {
		return nil, fmt.Errorf("spdy executor: %w", err)
	}

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	ctx, cancel := context.WithCancel(ctx)

	go func() {
		err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:  stdinR,
			Stdout: stdoutW,
		})
		stdoutW.CloseWithError(err)
		stdinR.CloseWithError(err)
	}()

	return &Process{
		Stdin:  stdinW,
		Stdout: stdoutR,
		Wait: func() error {
			<-ctx.Done()
			return ctx.Err()
		},
		Cancel: cancel,
	}, nil
}
