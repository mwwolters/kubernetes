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
)

type MockFunc struct {
	cmd, out string
	calls    [][]string
}

func (m *MockFunc) listen() {
	// O_RDONLY without O_NONBLOCK set blocks the calling thread
	// until a thread opens the file for writing.
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

func addArgs(cmd string, args []string) string {
	for _, a := range args {
		cmd = fmt.Sprintf("%s %s", cmd, a)
	}
	return cmd
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
	mockStr := " alias %s='%s_alias(){ echo %s $@ >> %s; }; %s_alias';"
	for cmd, m := range b.mockFuncs {
		m.out = filepath.Join(b.tmdDir, cmd)
		out += fmt.Sprintf(mockStr, cmd, cmd, cmd, m.out, cmd)
		syscall.Mkfifo(m.out, 0600)
		go m.listen()
	}
	return out
}

func (b *BashEnvironment) CallWithEnv(cmd string, args []string) ([]byte, error) {
	b.makeCMDprefix()
	cmdStr := b.cmdStr + " " + addArgs(cmd, args)
	c := exec.Command("bash", "-c", cmdStr)

	return c.CombinedOutput()
}

func (b *BashEnvironment) makeCMDprefix() {
	// shopt -s expand_aliases needs to be first or aliases won't be expanded, hence this ordering.
	cmdStr := b.makeMocks()
	cmdStr += b.makeSources()
	b.cmdStr = cmdStr
}

func BashEnv(env, dir string, sources []source, mocks []string) BashEnvironment {
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

	return b
}
