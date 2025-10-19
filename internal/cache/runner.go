package cache

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
)

type RunOptions struct {
	Dir    string
	Env    []string
	Stdout io.Writer
	Stderr io.Writer
}

type RunResult struct {
	Stdout []byte
	Stderr []byte
}

type Runner interface {
	Run(ctx context.Context, command string, args []string, opts RunOptions) (RunResult, error)
}

type CmdRunner struct{}

func (CmdRunner) Run(ctx context.Context, command string, args []string, opts RunOptions) (RunResult, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if len(opts.Env) > 0 {
		cmd.Env = append(os.Environ(), opts.Env...)
	}

	var stdoutBuf, stderrBuf bytes.Buffer

	stdoutWriter := io.Writer(&stdoutBuf)
	if opts.Stdout != nil {
		stdoutWriter = io.MultiWriter(&stdoutBuf, opts.Stdout)
	}
	stderrWriter := io.Writer(&stderrBuf)
	if opts.Stderr != nil {
		stderrWriter = io.MultiWriter(&stderrBuf, opts.Stderr)
	}

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	err := cmd.Run()
	return RunResult{Stdout: stdoutBuf.Bytes(), Stderr: stderrBuf.Bytes()}, err
}

var _ Runner = CmdRunner{}
