package wifi

import (
	"context"
	"fmt"
	"time"
)

// maxScanAttempts is how often a scan that the kernel aborts is repeated.
const maxScanAttempts = 3

// scanAttempt makes one attempt at a fresh scan within the given time budget.
// aborted reports that the scan was cancelled and may be repeated.
type scanAttempt func(timeout time.Duration) (fresh bool, note string, aborted bool, err error)

// withRetries repeats a scan that was aborted, which typically happens when
// NetworkManager or wpa_supplicant starts its own scan at the same moment.
// It pauses a little longer after each abort and never exceeds timeout in
// total. Failures that are not aborts (no permission, device down) are
// returned at once: repeating them would not help.
func withRetries(ctx context.Context, timeout time.Duration, attempt scanAttempt,
	pause func(context.Context, time.Duration) error) (fresh bool, note string, err error) {
	deadline := time.Now().Add(timeout)
	for i := 1; i <= maxScanAttempts; i++ {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, "timed out waiting for the scan to finish; showing cached results", nil
		}
		fresh, note, aborted, err := attempt(remaining)
		if err != nil || !aborted {
			return fresh, note, err
		}
		if i == maxScanAttempts {
			break
		}
		if err := pause(ctx, time.Duration(i)*time.Second); err != nil {
			return false, "", err
		}
	}
	return false, fmt.Sprintf("the scan was aborted %d times (another program, such as NetworkManager, is probably scanning); showing cached results", maxScanAttempts), nil
}

// sleepContext waits for d or until ctx is done.
func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
