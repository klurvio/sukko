package testutil

import (
	"sync"
	"testing"
	"time"
)

// AssertEqual fails the test if got != want.
func AssertEqual[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", msg, got, want)
	}
}

// AssertTrue fails the test if condition is false.
func AssertTrue(t *testing.T, condition bool, msg string) {
	t.Helper()
	if !condition {
		t.Errorf("%s: expected true, got false", msg)
	}
}

// AssertFalse fails the test if condition is true.
func AssertFalse(t *testing.T, condition bool, msg string) {
	t.Helper()
	if condition {
		t.Errorf("%s: expected false, got true", msg)
	}
}

// AssertNoError fails the test if err is not nil.
func AssertNoError(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: unexpected error: %v", msg, err)
	}
}

// AssertError fails the test if err is nil.
func AssertError(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: expected error, got nil", msg)
	}
}

// AssertNil fails the test if v is not nil.
func AssertNil(t *testing.T, v any, msg string) {
	t.Helper()
	if v != nil {
		t.Errorf("%s: expected nil, got %v", msg, v)
	}
}

// AssertNotNil fails the test if v is nil.
func AssertNotNil(t *testing.T, v any, msg string) {
	t.Helper()
	if v == nil {
		t.Errorf("%s: expected non-nil, got nil", msg)
	}
}

// AssertLen fails if the slice/map/channel doesn't have the expected length.
func AssertLen[T any](t *testing.T, slice []T, want int, msg string) {
	t.Helper()
	if len(slice) != want {
		t.Errorf("%s: length = %d, want %d", msg, len(slice), want)
	}
}

// WaitFor polls condition until it returns true or timeout expires.
// Returns true if condition was met, false if timeout occurred.
func WaitFor(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// AssertEventually fails if condition doesn't become true within timeout.
func AssertEventually(t *testing.T, timeout time.Duration, condition func() bool, msg string) {
	t.Helper()
	if !WaitFor(timeout, condition) {
		t.Errorf("%s: condition not met within %v", msg, timeout)
	}
}

// RunConcurrent runs fn concurrently n times and waits for all to complete.
func RunConcurrent(n int, fn func(i int)) {
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			fn(idx)
		}(i)
	}
	wg.Wait()
}

// RunConcurrentWithStart synchronizes goroutine start to maximize race condition detection.
func RunConcurrentWithStart(n int, fn func(i int)) {
	var wg sync.WaitGroup
	start := make(chan struct{})

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start // Wait for signal
			fn(idx)
		}(i)
	}

	close(start) // Start all goroutines simultaneously
	wg.Wait()
}

// DrainChannel empties a channel and returns all values.
func DrainChannel[T any](ch <-chan T) []T {
	var result []T
	for {
		select {
		case v := <-ch:
			result = append(result, v)
		default:
			return result
		}
	}
}

// SendWithTimeout sends v on ch with timeout, returns false if timeout occurred.
func SendWithTimeout[T any](ch chan<- T, v T, timeout time.Duration) bool {
	select {
	case ch <- v:
		return true
	case <-time.After(timeout):
		return false
	}
}

// ReceiveWithTimeout receives from ch with timeout, returns value and ok.
func ReceiveWithTimeout[T any](ch <-chan T, timeout time.Duration) (T, bool) {
	select {
	case v := <-ch:
		return v, true
	case <-time.After(timeout):
		var zero T
		return zero, false
	}
}
