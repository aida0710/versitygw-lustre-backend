// Copyright 2026 Versity Software
// This file is licensed under the Apache License, Version 2.0.

package lustre

import (
	"bytes"
	"io"
	"testing"
)

type shortChunkReader struct {
	r        io.Reader
	maxChunk int
}

func (r *shortChunkReader) Read(p []byte) (int, error) {
	if len(p) > r.maxChunk {
		p = p[:r.maxChunk]
	}
	return r.r.Read(p)
}

type countingWriter struct {
	bytes.Buffer
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(p)
}

func TestCopyCoalescedFillsWriteBuffersFromShortReads(t *testing.T) {
	const chunkSize = 1024 * 1024
	payload := bytes.Repeat([]byte("a"), 3*chunkSize+12345)
	source := &shortChunkReader{
		r:        bytes.NewReader(payload),
		maxChunk: 32 * 1024,
	}
	destination := &countingWriter{}

	written, err := copyCoalesced(destination, source, make([]byte, chunkSize))
	if err != nil {
		t.Fatalf("copyCoalesced: %v", err)
	}
	if written != int64(len(payload)) {
		t.Fatalf("copied %d bytes, want %d", written, len(payload))
	}
	if !bytes.Equal(destination.Bytes(), payload) {
		t.Fatal("copied payload differs")
	}
	if destination.writes != 4 {
		t.Fatalf("Write called %d times, want 4 coalesced writes", destination.writes)
	}
}
