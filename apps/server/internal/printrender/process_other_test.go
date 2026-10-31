//go:build !linux

package printrender

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestBrowserProcessAttributes(t *testing.T) {
	want := &syscall.SysProcAttr{Setpgid: true}
	if got := browserProcessAttributes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("browser process attributes = %#v, want %#v", got, want)
	}
}

func ownedProcessState(pid int) (byte, error) {
	data, err := exec.CommandContext(context.Background(), "ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && len(data) == 0 {
			return 0, nil
		}
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) != 1 {
		return 0, errors.New("malformed process state for owned child")
	}
	return fields[0][0], nil
}
