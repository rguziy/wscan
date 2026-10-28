//go:build linux

package genl

import (
	"errors"
	"syscall"
	"testing"
	"time"
)

func dialOrSkip(t *testing.T) *Conn {
	t.Helper()
	c, err := Dial()
	if err != nil {
		t.Skipf("cannot open a generic netlink socket here: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// The "nlctrl" family always exists, so resolving it exercises the real
// socket path: send, receive, framing and attribute decoding.
func TestResolveControllerFamily(t *testing.T) {
	c := dialOrSkip(t)
	fam, err := c.Family("nlctrl")
	if err != nil {
		t.Fatalf("resolve nlctrl: %v", err)
	}
	if fam.ID != ctrlFamilyID {
		t.Errorf("nlctrl ID = %#x, want %#x", fam.ID, ctrlFamilyID)
	}
	if _, ok := fam.Groups["notify"]; !ok {
		t.Errorf("nlctrl groups = %v, want a \"notify\" group", fam.Groups)
	}
}

// An unknown family must surface as a kernel error, not a hang or a panic.
// Most kernels answer ENOENT; some (for example recent kernels that try to
// auto-load a module for the name) answer EINVAL instead, so accept both.
func TestUnknownFamilyIsKernelError(t *testing.T) {
	c := dialOrSkip(t)
	_, err := c.Family("wscannosuchfamily")
	if !errors.Is(err, syscall.ENOENT) && !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("err = %v, want ENOENT or EINVAL", err)
	}
	// The connection must stay usable after a kernel error.
	if _, err := c.Family("nlctrl"); err != nil {
		t.Errorf("connection unusable after an error: %v", err)
	}
}

func TestReceiveTimeout(t *testing.T) {
	c := dialOrSkip(t)
	if err := c.SetReadTimeout(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := c.Receive(); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if d := time.Since(start); d < 40*time.Millisecond || d > 2*time.Second {
		t.Errorf("timeout after %v", d)
	}
}
