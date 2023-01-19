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

package containerutil

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/nerdctl/pkg/portutil"
	"github.com/containerd/nerdctl/pkg/strutil"
)

// PrintHostPort writes to `writer` the public (HostIP:HostPort) of a given `containerPort/protocol` in a container.
// if `containerPort < 0`, it writes all public ports of the container.
func PrintHostPort(ctx context.Context, writer io.Writer, container containerd.Container, containerPort int, proto string) error {
	l, err := container.Labels(ctx)
	if err != nil {
		return err
	}
	ports, err := portutil.ParsePortsLabel(l)
	if err != nil {
		return err
	}

	if containerPort < 0 {
		for _, p := range ports {
			fmt.Fprintf(writer, "%d/%s -> %s:%d\n", p.ContainerPort, p.Protocol, p.HostIP, p.HostPort)
		}
		return nil
	}

	for _, p := range ports {
		if p.ContainerPort == int32(containerPort) && strings.ToLower(p.Protocol) == proto {
			fmt.Fprintf(writer, "%s:%d\n", p.HostIP, p.HostPort)
			return nil
		}
	}
	return fmt.Errorf("no public port %d/%s published for %q", containerPort, proto, container.ID())
}

// ContainerStatus returns the container's status from its task.
func ContainerStatus(ctx context.Context, c containerd.Container) (containerd.Status, error) {
	// Just in case, there is something wrong in server.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	task, err := c.Task(ctx, nil)
	if err != nil {
		return containerd.Status{}, err
	}

	return task.Status(ctx)
}

func withOSEnv(envs []string) ([]string, error) {
	newEnvs := make([]string, len(envs))

	// from https://github.com/docker/cli/blob/v22.06.0-beta.0/opts/env.go#L18
	getEnv := func(val string) (string, error) {
		arr := strings.SplitN(val, "=", 2)
		if arr[0] == "" {
			return "", errors.New("invalid environment variable: " + val)
		}
		if len(arr) > 1 {
			return val, nil
		}
		if envVal, ok := os.LookupEnv(arr[0]); ok {
			return arr[0] + "=" + envVal, nil
		}
		return val, nil
	}
	for i := range envs {
		env, err := getEnv(envs[i])
		if err != nil {
			return nil, err
		}
		newEnvs[i] = env
	}

	return newEnvs, nil
}

func parseEnvVars(paths []string) ([]string, error) {
	vars := make([]string, 0)
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open env file %s: %w", path, err)
		}
		defer f.Close()

		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			// skip comment lines
			if strings.HasPrefix(line, "#") {
				continue
			}
			vars = append(vars, line)
		}
		if err = sc.Err(); err != nil {
			return nil, err
		}
	}
	return vars, nil
}

// GenerateEnvs combines environment variables from `--env-file` and `--env`.
// Pass an empty slice if any arg is not used.
func GenerateEnvs(envFile []string, env []string) ([]string, error) {
	var envs []string
	var err error

	if envFiles := strutil.DedupeStrSlice(envFile); len(envFiles) > 0 {
		envs, err = parseEnvVars(envFiles)
		if err != nil {
			return nil, err
		}
	}

	if env := strutil.DedupeStrSlice(env); len(env) > 0 {
		envs = append(envs, env...)
	}

	if envs, err = withOSEnv(envs); err != nil {
		return nil, err
	}

	return envs, nil
}
