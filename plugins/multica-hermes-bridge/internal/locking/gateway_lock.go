package locking

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

type GatewayLock struct {
	file *flock.Flock
}

func New(statePath, key string) (*GatewayLock, error) {
	if statePath == "" || key == "" {
		return nil, fmt.Errorf("lock path and key are required")
	}
	dir := filepath.Dir(statePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	return &GatewayLock{file: flock.New(filepath.Join(dir, "gateway-"+key+".lock"))}, nil
}

func (l *GatewayLock) TryAcquire(ctx context.Context) error {
	if l == nil || l.file == nil {
		return fmt.Errorf("lock is not initialized")
	}
	locked, err := l.file.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil {
		return fmt.Errorf("acquire gateway lock: %w", err)
	}
	if !locked {
		return ErrBusy
	}
	return nil
}

func (l *GatewayLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Unlock()
}

var ErrBusy = fmt.Errorf("gateway has an active turn")
