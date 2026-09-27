// Package diskreserve stops alpha disk-expanding operations before the host
// filesystem reaches its required free-space floor.
package diskreserve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

const samplingInterval = 250 * time.Millisecond
const samplingMargin = uint64(1) << 30

// Run checks the exact opened filesystems before mutation, samples them while
// the operation runs, and checks once more before reporting success. The extra
// 1 GiB margin gives cancellation and owned cleanup space before the reserve.
func Run(ctx context.Context, directories []string, operation func(context.Context) error) error {
	if len(directories) == 0 || operation == nil {
		return errors.New("disk reserve requires directories and an operation")
	}
	opened := make([]*os.File, 0, len(directories))
	defer func() {
		for _, directory := range opened {
			_ = directory.Close()
		}
	}()
	for _, path := range directories {
		file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return fmt.Errorf("open disk reserve directory: %w", err)
		}
		opened = append(opened, file)
	}
	check := func() error {
		for _, directory := range opened {
			if err := checkDirectory(directory); err != nil {
				return fmt.Errorf("host disk reserve for %s: %w", directory.Name(), err)
			}
		}
		return nil
	}
	return runWithChecker(ctx, samplingInterval, check, operation)
}

func checkDirectory(directory *os.File) error {
	var stat syscall.Statfs_t
	if err := syscall.Fstatfs(int(directory.Fd()), &stat); err != nil {
		return err
	}
	if stat.Bsize <= 0 {
		return errors.New("invalid host filesystem block size")
	}
	blockSize := uint64(stat.Bsize)
	blocks, available := uint64(stat.Blocks), uint64(stat.Bavail)
	if blocks > ^uint64(0)/blockSize || available > ^uint64(0)/blockSize {
		return errors.New("host filesystem capacity overflow")
	}
	return checkValues(blocks*blockSize, available*blockSize)
}

func checkValues(capacity, free uint64) error {
	if capacity == 0 || free > capacity {
		return errors.New("invalid host filesystem capacity")
	}
	const twentyGiB = uint64(20) << 30
	floor := capacity / 10
	if floor < twentyGiB {
		floor = twentyGiB
	}
	if free <= floor+samplingMargin {
		return fmt.Errorf("free space %d is at or below reserve %d plus stopping margin %d", free, floor, samplingMargin)
	}
	return nil
}

func runWithChecker(ctx context.Context, interval time.Duration, check func() error, operation func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := check(); err != nil {
		return fmt.Errorf("host disk reserve before operation: %w", err)
	}
	guarded, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-guarded.Done():
				done <- nil
				return
			case <-ticker.C:
				if err := check(); err != nil {
					cancel()
					done <- fmt.Errorf("host disk reserve during operation: %w", err)
					return
				}
			}
		}
	}()
	opErr := operation(guarded)
	cancel()
	monitorErr := <-done
	finalErr := check()
	if finalErr != nil {
		finalErr = fmt.Errorf("host disk reserve after operation: %w", finalErr)
	}
	return errors.Join(opErr, monitorErr, finalErr, ctx.Err())
}
