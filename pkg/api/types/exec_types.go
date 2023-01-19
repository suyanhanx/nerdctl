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

package types

type ExecCommandOptions struct {
	GOptions GlobalCommandOptions
	// Allocate a pseudo-TTY
	TTY bool
	// Keep STDIN open even if not attached
	Interactive bool
	// Detached mode: run command in the background
	Detach bool
	// Working directory inside the container
	Workdir string
	// Set environment variables
	Env []string
	// Set environment variables from file
	EnvFile []string
	// Give extended privileges to the command
	Privileged bool
	// Username or UID (format: <name|uid>[:<group|gid>])
	User string
}
