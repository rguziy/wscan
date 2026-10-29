package wifi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeAttempts returns a scanAttempt that plays back the given outcomes.
type outcome struct {
	fresh   bool
	note    string
	aborted bool
	err     error
}

func playback(outcomes ...outcome) (scanAttempt, *int) {
	calls := 0
	return func(time.Duration) (bool, string, bool, error) {
		o := outcomes[min(calls, len(outcomes)-1)]
		calls++
		return o.fresh, o.note, o.aborted, o.err
	}, &calls
}

func recordPauses() (func(context.Context, time.Duration) error, *[]time.Duration) {
	var got []time.Duration
	return func(_ context.Context, d time.Duration) error {
		got = append(got, d)
		return nil
	}, &got
}

func TestRetriesAbortedScanUntilItSucceeds(t *testing.T) {
	attempt, calls := playback(outcome{aborted: true, note: "aborted"}, outcome{aborted: true, note: "aborted"}, outcome{fresh: true})
	pause, pauses := recordPauses()
	fresh, note, err := withRetries(context.Background(), time.Minute, attempt, pause)
	if !fresh || note != "" || err != nil {
		t.Errorf("fresh=%v note=%q err=%v", fresh, note, err)
	}
	if *calls != 3 {
		t.Errorf("attempts = %d, want 3", *calls)
	}
	// The pause grows after each abort: 1 s, then 2 s.
	if len(*pauses) != 2 || (*pauses)[0] != time.Second || (*pauses)[1] != 2*time.Second {
		t.Errorf("pauses = %v", *pauses)
	}
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	attempt, calls := playback(outcome{aborted: true})
	pause, pauses := recordPauses()
	fresh, note, err := withRetries(context.Background(), time.Minute, attempt, pause)
	if fresh || err != nil {
		t.Errorf("fresh=%v err=%v", fresh, err)
	}
	if *calls != maxScanAttempts || len(*pauses) != maxScanAttempts-1 {
		t.Errorf("attempts=%d pauses=%d", *calls, len(*pauses))
	}
	if !strings.Contains(note, "aborted 3 times") || !strings.Contains(note, "cached results") {
		t.Errorf("note = %q", note)
	}
}

func TestOtherOutcomesAreNotRetried(t *testing.T) {
	boom := errors.New("boom")
	for name, o := range map[string]outcome{
		"success":    {fresh: true},
		"no rights":  {note: "a fresh scan needs root"},
		"timed out":  {note: "timed out waiting"},
		"hard error": {err: boom},
	} {
		attempt, calls := playback(o)
		pause, pauses := recordPauses()
		fresh, note, err := withRetries(context.Background(), time.Minute, attempt, pause)
		if *calls != 1 || len(*pauses) != 0 {
			t.Errorf("%s: attempts=%d pauses=%d, want 1 and 0", name, *calls, len(*pauses))
		}
		if fresh != o.fresh || note != o.note || !errors.Is(err, o.err) {
			t.Errorf("%s: got (%v, %q, %v)", name, fresh, note, err)
		}
	}
}

func TestRetryStopsWhenCancelledDuringPause(t *testing.T) {
	attempt, calls := playback(outcome{aborted: true})
	pause := func(context.Context, time.Duration) error { return context.Canceled }
	_, _, err := withRetries(context.Background(), time.Minute, attempt, pause)
	if !errors.Is(err, context.Canceled) || *calls != 1 {
		t.Errorf("err=%v attempts=%d", err, *calls)
	}
}

func TestRetryRespectsTotalTimeout(t *testing.T) {
	var budgets []time.Duration
	attempt := func(remaining time.Duration) (bool, string, bool, error) {
		budgets = append(budgets, remaining)
		return false, "", true, nil
	}
	// The pause burns the whole budget, so no second attempt may start.
	pause := func(context.Context, time.Duration) error {
		time.Sleep(60 * time.Millisecond)
		return nil
	}
	_, note, err := withRetries(context.Background(), 50*time.Millisecond, attempt, pause)
	if err != nil || len(budgets) != 1 {
		t.Fatalf("err=%v attempts=%d", err, len(budgets))
	}
	if !strings.Contains(note, "timed out") {
		t.Errorf("note = %q", note)
	}
	if budgets[0] > 50*time.Millisecond {
		t.Errorf("first attempt got %v, more than the total timeout", budgets[0])
	}
}

func TestSleepContext(t *testing.T) {
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Errorf("err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sleepContext(ctx, time.Minute); !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Errorf("cancelled sleep: err=%v after %v", err, time.Since(start))
	}
}
