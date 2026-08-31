// Copyright 2026 Versity Software
// This file is licensed under the Apache License, Version 2.0.

package lustre

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
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

type bufferSizeCountingReader struct {
	r     io.Reader
	reads int
}

func (r *bufferSizeCountingReader) Read(p []byte) (int, error) {
	r.reads++
	return r.r.Read(p)
}

func TestPartWriterReadFromCoalescesIntoFourMiBWrites(t *testing.T) {
	const payloadSize = 8 * 1024 * 1024
	payload := bytes.Repeat([]byte("a"), payloadSize)
	path := filepath.Join(t.TempDir(), "staging")

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatalf("create staging file: %v", err)
	}
	if err := f.Truncate(payloadSize); err != nil {
		f.Close()
		t.Fatalf("truncate staging file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close staging file: %v", err)
	}

	f, err = os.OpenFile(path, os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("reopen staging file: %v", err)
	}
	w := &partWriter{f: f, remain: payloadSize}
	defer w.Close()

	source := &bufferSizeCountingReader{r: bytes.NewReader(payload)}
	if _, err := io.Copy(w, source); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}

	// 4MiBなら8MiB本体の2回とEOF確認までの3回以内。
	// 1MiBに戻ると8回以上となり、回帰を検出できる。
	const wantMaxReads = 3
	if source.reads > wantMaxReads {
		t.Fatalf("Read called %d times for %d bytes, want <= %d", source.reads, payloadSize, wantMaxReads)
	}
}
