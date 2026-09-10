// Copyright 2026 Versity Software
// This file is licensed under the Apache License, Version 2.0.

package posix

import (
	"context"
	"testing"

	"golang.org/x/sync/semaphore"
)

func TestAcquireListActionSlotUsesDedicatedLimiter(t *testing.T) {
	p := &Posix{
		actionLimiter:     semaphore.NewWeighted(1),
		listActionLimiter: semaphore.NewWeighted(1),
	}

	releaseAction, err := p.acquireActionSlot(context.Background())
	if err != nil {
		t.Fatalf("acquire general action slot: %v", err)
	}
	defer releaseAction()

	releaseList, err := p.acquireListActionSlot(context.Background())
	if err != nil {
		t.Fatalf("list slot should remain available while general limiter is full: %v", err)
	}
	releaseList()
}

func TestAcquireListActionSlotFallsBackToGeneralLimiter(t *testing.T) {
	p := &Posix{actionLimiter: semaphore.NewWeighted(1)}

	releaseAction, err := p.acquireActionSlot(context.Background())
	if err != nil {
		t.Fatalf("acquire general action slot: %v", err)
	}
	defer releaseAction()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.acquireListActionSlot(ctx); err == nil {
		t.Fatal("list slot unexpectedly bypassed the saturated general limiter")
	}
}
