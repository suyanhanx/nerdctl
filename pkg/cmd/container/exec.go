/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package container

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/containerd/console"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/cmd/ctr/commands"
	"github.com/containerd/containerd/cmd/ctr/commands/tasks"
	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/nerdctl/pkg/api/types"
	"github.com/containerd/nerdctl/pkg/clientutil"
	"github.com/containerd/nerdctl/pkg/containerutil"
	"github.com/containerd/nerdctl/pkg/flagutil"
	"github.com/containerd/nerdctl/pkg/idgen"
	"github.com/containerd/nerdctl/pkg/idutil/containerwalker"
	"github.com/containerd/nerdctl/pkg/taskutil"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

func Exec(ctx context.Context, options types.ExecCommandOptions, args []string) error {
	client, ctx, cancel, err := clientutil.NewClient(ctx, options.GOptions.Namespace, options.GOptions.Address)
	if err != nil {
		return err
	}
	defer cancel()

	walker := &containerwalker.ContainerWalker{
		Client: client,
		OnFound: func(ctx context.Context, found containerwalker.Found) error {
			if found.MatchCount > 1 {
				return fmt.Errorf("multiple IDs found with provided prefix: %s", found.Req)
			}
			return execActionWithContainer(ctx, options, args, found.Container, client)
		},
	}
	req := args[0]
	n, err := walker.Walk(ctx, req)
	if err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("no such container %s", req)
	}
	return nil
}

func execActionWithContainer(ctx context.Context, options types.ExecCommandOptions, args []string, container containerd.Container, client *containerd.Client) error {
	pspec, err := generateExecProcessSpec(ctx, options, args, container, client)
	if err != nil {
		return err
	}

	task, err := container.Task(ctx, nil)
	if err != nil {
		return err
	}
	var (
		ioCreator cio.Creator
		in        io.Reader
		stdinC    = &taskutil.StdinCloser{
			Stdin: os.Stdin,
		}
	)

	if options.Interactive {
		in = stdinC
	}
	cioOpts := []cio.Opt{cio.WithStreams(in, os.Stdout, os.Stderr)}
	if options.TTY {
		cioOpts = append(cioOpts, cio.WithTerminal)
	}
	ioCreator = cio.NewCreator(cioOpts...)

	execID := "exec-" + idgen.GenerateID()
	process, err := task.Exec(ctx, execID, pspec, ioCreator)
	if err != nil {
		return err
	}
	stdinC.Closer = func() {
		process.CloseIO(ctx, containerd.WithStdinCloser)
	}
	// if detach, we should not call this defer
	if !options.Detach {
		defer process.Delete(ctx)
	}

	statusC, err := process.Wait(ctx)
	if err != nil {
		return err
	}

	var con console.Console
	if options.TTY {
		con = console.Current()
		defer con.Reset()
		if err := con.SetRaw(); err != nil {
			return err
		}
	}
	if !options.Detach {
		if options.TTY {
			if err := tasks.HandleConsoleResize(ctx, process, con); err != nil {
				logrus.WithError(err).Error("console resize")
			}
		} else {
			sigc := commands.ForwardAllSignals(ctx, process)
			defer commands.StopCatch(sigc)
		}
	}

	if err := process.Start(ctx); err != nil {
		return err
	}
	if options.Detach {
		return nil
	}
	status := <-statusC
	code, _, err := status.Result()
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("exec failed with exit code %d", code)
	}
	return nil
}

func withResetAdditionalGIDs() oci.SpecOpts {
	return func(_ context.Context, _ oci.Client, _ *containers.Container, s *oci.Spec) error {
		s.Process.User.AdditionalGids = nil
		return nil
	}
}

func generateExecProcessSpec(ctx context.Context, options types.ExecCommandOptions, args []string, container containerd.Container, client *containerd.Client) (*specs.Process, error) {
	spec, err := container.Spec(ctx)
	if err != nil {
		return nil, err
	}
	var userOpts []oci.SpecOpts
	if options.User != "" {
		userOpts = append(userOpts, oci.WithUser(options.User), withResetAdditionalGIDs(), oci.WithAdditionalGIDs(options.User))
	}
	if userOpts != nil {
		c, err := container.Info(ctx)
		if err != nil {
			return nil, err
		}
		for _, opt := range userOpts {
			if err := opt(ctx, client, &c, spec); err != nil {
				return nil, err
			}
		}
	}

	pspec := spec.Process
	pspec.Terminal = options.TTY
	pspec.Args = args[1:]

	if options.Workdir != "" {
		pspec.Cwd = options.Workdir
	}
	envs, err := containerutil.GenerateEnvs(options.EnvFile, options.Env)
	if err != nil {
		return nil, err
	}
	pspec.Env = flagutil.ReplaceOrAppendEnvValues(pspec.Env, envs)

	if options.Privileged {
		err = setExecCapabilities(pspec)
		if err != nil {
			return nil, err
		}
	}

	return pspec, nil
}
