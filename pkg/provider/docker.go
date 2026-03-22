package provider

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	dockertypes "github.com/docker/docker/api/types"
	dockerclient "github.com/docker/docker/client"
)

// DockerOptions configures the Docker exec provider.
type DockerOptions struct {
	ContainerID string
	DockerHost  string // empty = default (unix socket)
}

// DockerProvider starts subprocesses via Docker exec.
type DockerProvider struct {
	client *dockerclient.Client
	opts   DockerOptions
}

// NewDockerProvider creates a DockerProvider.
// Pass nil as cli to use the default Docker environment.
func NewDockerProvider(cli *dockerclient.Client, opts DockerOptions) (*DockerProvider, error) {
	if cli == nil {
		var err error
		cli, err = dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
		if err != nil {
			return nil, fmt.Errorf("create docker client: %w", err)
		}
	}
	return &DockerProvider{client: cli, opts: opts}, nil
}

// Start creates a Docker exec session and returns stdio pipes.
func (p *DockerProvider) Start(ctx context.Context, execOpts ExecOptions) (*Process, error) {
	if len(execOpts.Command) == 0 {
		return nil, fmt.Errorf("command is required")
	}

	execResp, err := p.client.ContainerExecCreate(ctx, p.opts.ContainerID, container.ExecOptions{
		Cmd:          execOpts.Command,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: false,
		Tty:          false,
	})
	if err != nil {
		return nil, fmt.Errorf("exec create: %w", err)
	}

	attachResp, err := p.client.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("exec attach: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	stdoutR, stdoutW := io.Pipe()
	stdinWriter := &hijackedStdin{resp: &attachResp}

	go func() {
		defer stdoutW.Close()
		io.Copy(stdoutW, attachResp.Reader)
	}()

	return &Process{
		Stdin:  stdinWriter,
		Stdout: stdoutR,
		Wait: func() error {
			<-ctx.Done()
			return ctx.Err()
		},
		Cancel: cancel,
	}, nil
}

// hijackedStdin wraps a Docker HijackedResponse as an io.WriteCloser for stdin.
type hijackedStdin struct {
	resp *dockertypes.HijackedResponse
}

func (h *hijackedStdin) Write(p []byte) (int, error) {
	return h.resp.Conn.Write(p)
}

func (h *hijackedStdin) Close() error {
	h.resp.Close()
	return nil
}