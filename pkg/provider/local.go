package provider

import (
	"context"
	"fmt"
	"os/exec"
)

// LocalProvider starts subprocesses on the local machine.
type LocalProvider struct{}

// Start launches a local subprocess and returns its stdio pipes.
func (p *LocalProvider) Start(ctx context.Context, opts ExecOptions) (*Process, error) {
	if len(opts.Command) == 0 {
		return nil, fmt.Errorf("command is required")
	}

	ctx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(ctx, opts.Command[0], opts.Command[1:]...)
	if len(opts.Env) > 0 {
		cmd.Env = opts.Env
	}
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start: %w", err)
	}

	return &Process{
		Stdin:  stdin,
		Stdout: stdout,
		Wait:   cmd.Wait,
		Cancel: cancel,
	}, nil
}
