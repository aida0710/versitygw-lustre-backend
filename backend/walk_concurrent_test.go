// Copyright 2026 Versity Software
// This file is licensed under the Apache License, Version 2.0
// (the "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package backend_test

import (
	"context"
	"fmt"
	"io/fs"
	"reflect"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/versity/versitygw/backend"
	"github.com/versity/versitygw/s3response"
)

func TestWalkConcurrentResolvesMetadataInParallelAndPreservesOrder(t *testing.T) {
	fSys := fstest.MapFS{}
	for i := range 12 {
		fSys[fmt.Sprintf("object-%02d", i)] = &fstest.MapFile{}
	}

	var active atomic.Int32
	var peak atomic.Int32
	getObj := func(path string, _ fs.DirEntry) (s3response.Object, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if current <= old || peak.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		return s3response.Object{Key: backend.GetPtrFromString(path)}, nil
	}

	result, err := backend.WalkConcurrent(
		context.Background(), fSys, "", "", "", 1000, 4, getObj, nil,
	)
	if err != nil {
		t.Fatalf("WalkConcurrent: %v", err)
	}
	if peak.Load() < 2 {
		t.Fatalf("metadata lookup peak concurrency = %d, want at least 2", peak.Load())
	}

	want := make([]string, 12)
	for i := range want {
		want[i] = fmt.Sprintf("object-%02d", i)
	}
	if got := objectKeys(result.Objects); !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
}

func TestWalkConcurrentPaginationHasNoGapsOrDuplicates(t *testing.T) {
	fSys := fstest.MapFS{}
	for i := range 11 {
		fSys[fmt.Sprintf("object-%02d", i)] = &fstest.MapFile{}
	}

	var got []string
	marker := ""
	for {
		result, err := backend.WalkConcurrent(
			context.Background(), fSys, "", "", marker, 3, 4, getObj, nil,
		)
		if err != nil {
			t.Fatalf("WalkConcurrent marker %q: %v", marker, err)
		}
		got = append(got, objectKeys(result.Objects)...)
		if !result.Truncated {
			break
		}
		if result.NextMarker == "" || result.NextMarker == marker {
			t.Fatalf("pagination did not advance: marker=%q next=%q", marker, result.NextMarker)
		}
		marker = result.NextMarker
	}

	want := make([]string, 11)
	for i := range want {
		want[i] = fmt.Sprintf("object-%02d", i)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paginated keys = %v, want %v", got, want)
	}
}

func TestWalkConcurrentSkippedEntriesDoNotConsumePageLimit(t *testing.T) {
	fSys := fstest.MapFS{
		"00-skip": {},
		"01-keep": {},
		"02-skip": {},
		"03-keep": {},
		"04-keep": {},
	}
	getObjWithSkips := func(path string, _ fs.DirEntry) (s3response.Object, error) {
		if path == "00-skip" || path == "02-skip" {
			return s3response.Object{}, backend.ErrSkipObj
		}
		return s3response.Object{Key: backend.GetPtrFromString(path)}, nil
	}

	result, err := backend.WalkConcurrent(
		context.Background(), fSys, "", "", "", 2, 4, getObjWithSkips, nil,
	)
	if err != nil {
		t.Fatalf("WalkConcurrent: %v", err)
	}
	if got, want := objectKeys(result.Objects), []string{"01-keep", "03-keep"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	if !result.Truncated || result.NextMarker != "03-keep" {
		t.Fatalf("pagination = truncated:%v marker:%q, want true/03-keep", result.Truncated, result.NextMarker)
	}
}

func BenchmarkWalkConcurrentMetadataLatency(b *testing.B) {
	fSys := fstest.MapFS{}
	for i := range 128 {
		fSys[fmt.Sprintf("object-%03d", i)] = &fstest.MapFile{}
	}

	for _, concurrency := range []int{1, 8, 16} {
		b.Run(fmt.Sprintf("concurrency-%d", concurrency), func(b *testing.B) {
			getObjWithLatency := func(path string, _ fs.DirEntry) (s3response.Object, error) {
				time.Sleep(time.Millisecond)
				return s3response.Object{Key: backend.GetPtrFromString(path)}, nil
			}
			b.ResetTimer()
			for range b.N {
				_, err := backend.WalkConcurrent(
					context.Background(), fSys, "", "", "", 1000,
					concurrency, getObjWithLatency, nil,
				)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func objectKeys(objects []s3response.Object) []string {
	keys := make([]string, len(objects))
	for i, object := range objects {
		keys[i] = *object.Key
	}
	return keys
}
