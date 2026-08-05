package directorycommand

import (
	"context"
	"errors"
	"time"

	"github.com/thystra/Activity-Relay/internal/directoryclient"
)

const (
	maximumAttempts         = 3
	initialBackoff          = 250 * time.Millisecond
	maximumCommandRetryWait = 30 * time.Second
)

func retryLifecycle(
	ctx context.Context,
	deps dependencies,
	operation func(context.Context) (directoryclient.Response, error),
) (directoryclient.Response, error) {
	var last error
	for attempt := 0; attempt < maximumAttempts; attempt++ {
		response, err := operation(ctx)
		if err == nil {
			return response, nil
		}
		last = err
		delay, retry := retryDelay(err, attempt)
		if !retry || attempt+1 == maximumAttempts {
			break
		}
		if err := deps.sleep(ctx, delay); err != nil {
			return directoryclient.Response{}, err
		}
	}
	return directoryclient.Response{}, last
}

func retryStatus(
	ctx context.Context,
	deps dependencies,
	operation func(context.Context) (directoryclient.Status, error),
) (directoryclient.Status, error) {
	var last error
	for attempt := 0; attempt < maximumAttempts; attempt++ {
		status, err := operation(ctx)
		if err == nil {
			return status, nil
		}
		last = err
		delay, retry := retryDelay(err, attempt)
		if !retry || attempt+1 == maximumAttempts {
			break
		}
		if err := deps.sleep(ctx, delay); err != nil {
			return directoryclient.Status{}, err
		}
	}
	return directoryclient.Status{}, last
}

func retryDelay(err error, attempt int) (time.Duration, bool) {
	if errors.Is(err, directoryclient.ErrDirectoryTransport) {
		return initialBackoff << attempt, true
	}
	var protocolError *directoryclient.ProtocolError
	if !errors.As(err, &protocolError) {
		return 0, false
	}
	switch protocolError.Code {
	case directoryclient.ErrorRateLimited:
		if protocolError.RetryAfter > maximumCommandRetryWait {
			// An interactive command must not wait for hours. Returning without
			// another attempt respects the remote lower bound; the operator can
			// rerun the command after the reported condition has cleared.
			return 0, false
		}
		if protocolError.RetryAfter > 0 {
			return protocolError.RetryAfter, true
		}
		return initialBackoff << attempt, true
	case directoryclient.ErrorInternal:
		if protocolError.RetryAfter > maximumCommandRetryWait {
			return 0, false
		}
		if protocolError.RetryAfter > 0 {
			return protocolError.RetryAfter, true
		}
		return initialBackoff << attempt, true
	default:
		return 0, false
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
