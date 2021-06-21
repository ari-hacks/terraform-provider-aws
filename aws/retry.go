package aws

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func RetryConfigContext(ctx context.Context, delay time.Duration, delayRand time.Duration, minTimeout time.Duration, pollInterval time.Duration, timeout time.Duration, f resource.RetryFunc) error {
	// These are used to pull the error out of the function; need a mutex to
	// avoid a data race.
	var resultErr error
	var resultErrMu sync.Mutex

	c := &resource.StateChangeConf{
		Pending: []string{"retryableerror"},
		Target:  []string{"success"},
		Timeout: timeout,
		Refresh: func() (interface{}, string, error) {
			rerr := f()

			resultErrMu.Lock()
			defer resultErrMu.Unlock()

			if rerr == nil {
				resultErr = nil
				return 42, "success", nil
			}

			resultErr = rerr.Err

			if rerr.Retryable {
				return 42, "retryableerror", nil
			}

			return nil, "quit", rerr.Err
		},
	}

	if delay.Milliseconds() > 0 {
		c.Delay = delay
	}

	if delayRand.Milliseconds() > 0 {
		// Hitting the API at exactly the same time on each iteration of the retry is more likely to
		// cause Throttling problems. We introduce randomness in order to help AWS be happier.
		rand.Seed(time.Now().UTC().UnixNano())

		c.Delay = time.Duration(rand.Int63n(delayRand.Milliseconds())) * time.Millisecond
	}

	if minTimeout.Milliseconds() > 0 {
		c.MinTimeout = minTimeout
	}

	if pollInterval.Milliseconds() > 0 {
		c.PollInterval = pollInterval
	}

	_, waitErr := c.WaitForStateContext(ctx)

	// Need to acquire the lock here to be able to avoid race using resultErr as
	// the return value
	resultErrMu.Lock()
	defer resultErrMu.Unlock()

	// resultErr may be nil because the wait timed out and resultErr was never
	// set; this is still an error
	if resultErr == nil {
		return waitErr
	}
	// resultErr takes precedence over waitErr if both are set because it is
	// more likely to be useful
	return resultErr
}
