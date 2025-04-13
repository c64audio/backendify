package utils

import (
	"sync"
	"time"
)

// TryLock tryLock attempts to acquire a lock with a timeout
func TryLock(mu *sync.Mutex, timeout time.Duration) bool {
	lockChan := make(chan struct{}, 1)

	go func() {
		mu.Lock()
		lockChan <- struct{}{}
	}()

	select {
	case <-lockChan:
		return true
	case <-time.After(timeout):
		return false
	}
}
