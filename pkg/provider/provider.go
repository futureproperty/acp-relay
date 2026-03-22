package provider

import (
	"context"
	"io"
)

// ExecProvider starts a subprocess and returns stdio pipes.
type ExecProvider interface {
	Start(ctx context.Context, opts ExecOptions) (*Process, error)
}

// ExecOptions configures the subprocess to start.
type ExecOptions struct {
	Command []string
	Env     []string
	Dir     string
}

// Process represents a running subprocess with stdio access.
type Process struct {
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Wait   func() error
	Cancel func()
}
