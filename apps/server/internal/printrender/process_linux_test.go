//go:build linux

package printrender

import (
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestBrowserProcessAttributes(t *testing.T) {
	want := &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if got := browserProcessAttributes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("browser process attributes = %#v, want %#v", got, want)
	}
}

func ownedProcessState(pid int) (byte, error) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	closingParen := strings.LastIndexByte(string(stat), ')')
	if closingParen < 0 || closingParen+2 >= len(stat) || stat[closingParen+1] != ' ' {
		return 0, errors.New("malformed process stat for owned child")
	}
	return stat[closingParen+2], nil
}
