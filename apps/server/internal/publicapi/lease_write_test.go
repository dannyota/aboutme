package publicapi

import (
	"errors"
	"testing"
)

// recordingHooks stands in for a lease: it keeps the registered hook, or
// fails to register one.
type recordingHooks struct {
	hook func()
	err  error
}

func (h *recordingHooks) OnCancel(hook func()) error {
	if h.err != nil {
		return h.err
	}
	h.hook = hook
	return nil
}

func TestWriteWhileLeased_FailsClosedWhenTheHookCannotRegister(t *testing.T) {
	wrote := false
	err := writeWhileLeased(newDeadlineBlockedWriter(), &recordingHooks{err: errors.New("cannot register")}, func() { wrote = true })
	if err == nil || wrote {
		t.Fatalf("writeWhileLeased() error = %v, wrote = %t; want an error and no write", err, wrote)
	}
}

func TestWriteWhileLeased_HookTouchesTheWriterOnlyDuringTheWrite(t *testing.T) {
	hooks := &recordingHooks{}
	writer := newDeadlineBlockedWriter()
	if err := writeWhileLeased(writer, hooks, func() {}); err != nil {
		t.Fatal(err)
	}
	if hooks.hook == nil {
		t.Fatal("writeWhileLeased() registered no cancel hook")
	}
	hooks.hook()
	if writer.deadlineSet() {
		t.Fatal("a cancel hook after the write ended set the writer's deadline")
	}
}
