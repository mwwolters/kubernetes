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
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

type MockFunc struct {
	cmd, out string
	calls    [][]string
}

type source struct {
	name       string
	sourceOnly bool
}

type BashEnvironment struct {
	tmdDir    string
	sources   []source
	mockFuncs map[string]*MockFunc
	pipes     []*os.File
	cmdStr    string
}

func (b *BashEnvironment) makeSources() string {
	out := ""
	for _, s := range b.sources {
		out += fmt.Sprintf("source %s", s.name)
		if s.sourceOnly {
			out += " --source-only"
		}
		out += " ; "
	}
	return out
}

func (m *MockFunc) listen() {
	in, _ := os.OpenFile(m.out, os.O_RDONLY, 0600)
	var buff bytes.Buffer
	io.Copy(&buff, in)
	call := strings.Split(buff.String(), " ")
	for i, a := range call {
		call[i] = strings.Trim(a, "\n")
	}
	m.calls = append(m.calls, call)
	in.Close()
}

func (b *BashEnvironment) AssertCalledWith(cmd string, args []string) error {
	mock, ok := b.mockFuncs[cmd]
	if !ok {
		return fmt.Errorf("cmd %v not mocked", cmd)
	}
	if len(mock.calls) == 0 {
		return fmt.Errorf("cmd %v not called", cmd)
	}
	cmdArgs := append([]string{cmd}, args...)
	for _, callArgs := range mock.calls {
		if len(callArgs) != len(cmdArgs) {
			continue
		}
		eq := true
		for i, arg := range cmdArgs {
			if arg != callArgs[i] {
				eq = false
				break
			}
		}
		if eq {
			return nil
		}
	}
	return fmt.Errorf("cmd %v not called with %v", cmd, args)
}

func (b *BashEnvironment) makeMocks() string {
	out := ""
	if len(b.mockFuncs) > 0 {
		// Non-interactive shells don't expand aliases by default.
		out += "shopt -s expand_aliases ;"
	}

	// Echoing $@ outside of a new function includes the args from the function
	// that is calling it.
	mockStr := " alias %s='f(){ echo %s $@ >> %s; }; f';"
	for cmd, m := range b.mockFuncs {
		m.out = filepath.Join(b.tmdDir, cmd)
		out += fmt.Sprintf(mockStr, cmd, cmd, m.out)
		fmt.Printf("mkfifo: %v\n", m.out)
		syscall.Mkfifo(m.out, 0600)
		go m.listen()
	}
	return out
}

func (b *BashEnvironment) CallWithEnv(cmd string, args []string) ([]byte, error) {
	cmdStr := b.cmdStr + " " + addArgs(cmd, args)
	c := exec.Command("bash", "-c", cmdStr)

	return c.CombinedOutput()
}

func (b *BashEnvironment) makeCMDprefix() {
	// shopt -s expand_aliases needs to be first or they won't be expanded, hence this ordering.
	cmdStr := b.makeMocks()
	cmdStr += b.makeSources()
	b.cmdStr = cmdStr
}

func bashEnv(env, dir string, sources []source, mocks []string) BashEnvironment {
	mocked := make(map[string]*MockFunc)
	for _, mock := range mocks {
		mocked[mock] = &MockFunc{
			cmd:   mock,
			calls: [][]string{},
		}
	}
	tmpDir, _ := ioutil.TempDir(dir, "pipes")
	b := BashEnvironment{
		tmdDir:    tmpDir,
		sources:   sources,
		mockFuncs: mocked,
	}
	b.makeCMDprefix()

	return b
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
		expU      string
		expG      string
	}{
		{
			desc: "no args, LOG_OWNER_USER & LOG_OWNER_GROUP not set",
			env: `
			readonly KUBE_HOME={{.KubeHome}}
			`,
			expU: "root",
			expG: "root",
		},
		{
			desc: "no args, LOG_OWNER_USER set, LOG_OWNER_GROUP not",
			env: `
			readonly KUBE_HOME={{.KubeHome}}
			LOG_OWNER_USER=test
			`,
			expU: "test",
			expG: "root",
		},
		{
			desc: "no args, LOG_OWNER_USER not set, LOG_OWNER_GROUP set",
			env: `
			readonly KUBE_HOME={{.KubeHome}}
			LOG_OWNER_GROUP=test2
			`,
			expU: "root",
			expG: "test2",
		},
		{
			desc: "one arg, LOG_OWNER_USER & LOG_OWNER_GROUP not set",
			env: `
			readonly KUBE_HOME={{.KubeHome}}
			`,
			fArgs: []string{"test"},
			expU:  "root",
			expG:  "root",
		},
		{
			desc: "one arg, LOG_OWNER_USER set, LOG_OWNER_GROUP not",
			env: `
			readonly KUBE_HOME={{.KubeHome}}
			LOG_OWNER_USER=testUser
			`,
			fArgs: []string{"test"},
			expU:  "testUser",
			expG:  "root",
		},
		{
			desc: "one arg, LOG_OWNER_USER not set, LOG_OWNER_GROUP set",
			env: `
			readonly KUBE_HOME={{.KubeHome}}
			LOG_OWNER_GROUP=testGroup
			`,
			fArgs: []string{"test"},
			expU:  "root",
			expG:  "testGroup",
		},
		{
			desc: "two args, LOG_OWNER_USER & LOG_OWNER_GROUP not set",
			env: `
			readonly KUBE_HOME={{.KubeHome}}
			`,
			fArgs: []string{"testUser", "testGroup"},
			expU:  "testUser",
			expG:  "testGroup",
		},
		{
			desc: "two args, LOG_OWNER_USER set, LOG_OWNER_GROUP not",
			env: `
			readonly KUBE_HOME={{.KubeHome}}
			LOG_OWNER_USER=test
			`,
			fArgs: []string{"testUser", "testGroup"},
			expU:  "testUser",
			expG:  "testGroup",
		},
		{
			desc: "two args, LOG_OWNER_USER not set, LOG_OWNER_GROUP set",
			env: `
			readonly KUBE_HOME={{.KubeHome}}
			LOG_OWNER_GROUP=test
			`,
			fArgs: []string{"testUser", "testGroup"},
			expU:  "testUser",
			expG:  "testGroup",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			c := newFuncTestCase(t, "kube-apiserver.manifest", "prepare-log-file")
			e := kubeEnv{
				KubeHome: c.kubeHome,
			}
			c.mustCreateEnv(tc.env, e)
			defer c.tearDown()

			file := filepath.Join(c.kubeHome, "plf_test.log")
			fArgs := append([]string{file}, tc.fArgs...)

			sources := []source{
				{name: c.envScriptPath},
				{name: configureHelperScriptName, sourceOnly: true},
			}

			b := bashEnv(tc.env, c.kubeHome, sources, []string{"chown"})
			bs, err := b.CallWithEnv("prepare-log-file", fArgs)
			if err != nil {
				c.t.Logf("%s", bs)
				c.t.Fatalf("Failed to run configure-helper.sh: %v", err)
			}
			fmt.Printf("call stdout: %v\n", string(bs))

			if _, err := os.Stat(file); os.IsNotExist(err) {
				t.Fatalf("Log file %v not created: %v", file, err)
			}

			expArgs := []string{
				tc.expU + ":" + tc.expG,
				file,
			}
			err = b.AssertCalledWith("chown", expArgs)
			if err != nil {
				c.t.Fatalf("Assertion error: %v\ncalls: %v", err, *b.mockFuncs["chown"])
			}
		})
	}
}
