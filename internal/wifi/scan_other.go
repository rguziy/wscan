//go:build !linux && !windows

package wifi

import "context"

// Interfaces is not implemented on this platform yet.
func Interfaces() ([]Interface, error) { return nil, ErrUnsupported }

// Scan is not implemented on this platform yet.
func Scan(ctx context.Context, opt ScanOptions) (*Result, error) { return nil, ErrUnsupported }
