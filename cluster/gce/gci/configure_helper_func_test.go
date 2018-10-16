/*
Copyright 2018 The Kubernetes Authors.

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

package gci

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type kubeEnv struct {
	KubeHome string
}

type funcTestCase struct {
	*ManifestTestCase
}

func addArgs(cmd string, args []string) string {
	for _, a := range args {
		cmd = fmt.Sprintf("%s %s", cmd, a)
	}
	return cmd
}

func (c *funcTestCase) mustInvokeFuncArgs(envTemplate string, env interface{}, fArgs []string) {
	c.mustCreateEnv(envTemplate, env)
	args := fmt.Sprintf("source %s ; source %s --source-only ; %s", c.envScriptPath, configureHelperScriptName, c.manifestFuncName)
	fArgs = []string{"-c", addArgs(args, fArgs)}
	fmt.Printf("args: %v\n", strings.Join(fArgs, ", "))
	cmd := exec.Command("bash", fArgs...)

	bs, err := cmd.CombinedOutput()
	if err != nil {
		c.t.Logf("%s", bs)
		c.t.Fatalf("Failed to run configure-helper.sh: %v", err)
	}
	c.t.Logf("%s", string(bs))
}

func newFuncTestCase(t *testing.T, manifest, apiFunc string) *funcTestCase {
	return &funcTestCase{
		ManifestTestCase: newManifestTestCase(t, manifest, apiFunc, nil),
	}
}

func TestPrepareLogFile(t *testing.T) {
	testCases := []struct {
		desc, env string
		fArgs     []string
	}{
		{
			desc: "no args, LOG_OWNER_USER & LOG_OWNER_GROUP not set",
			env: `
			readonly KUBE_HOME={{.KubeHome}}
			LOG_OWNER_GROUP=$(id -gn)
			LOG_OWNER_USER=$(id -un)
			`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			c := newFuncTestCase(t, "kube-apiserver.manifest", "prepare-log-file")
			defer c.tearDown()

			var e = kubeEnv{
				KubeHome: c.kubeHome,
			}

			file := filepath.Join(c.kubeHome, "plf_test.log")
			fp := []string{file}
			fArgs := append(fp, tc.fArgs...)
			c.mustInvokeFuncArgs(tc.env, e, fArgs)

			if _, err := os.Stat(file); os.IsNotExist(err) {
				t.Fatalf("Log file %v not created: %v", file, err)
			}
		})
	}
}
