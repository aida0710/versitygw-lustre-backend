// Copyright 2026 Versity Software
// This file is licensed under the Apache License, Version 2.0
// (the "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package lustre

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/versity/versitygw/s3response"
)

// part のペイロードは、ディスクに載ったことを確かめてから成功を返す。Lustre は
// client が OST から evict されると、fsync されていない dirty page を黙って捨てる
// (2026-10-05〜08 に JAMSTEC の moon07 で 60 回以上)。MD5 を確かめて 200 を返した
// part が後から失われると、完成したオブジェクトは size だけ正しいまま壊れる。

func createTestUpload(t *testing.T, be *Lustre, key string) string {
	t.Helper()

	bucket := testBucket
	mpu, err := be.CreateMultipartUpload(context.Background(), s3response.CreateMultipartUploadInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		t.Fatalf("CreateMultipartUpload: %v", err)
	}
	return mpu.UploadId
}

func replaceSyncStaging(t *testing.T, sync func(*os.File) error) {
	t.Helper()

	original := syncStaging
	syncStaging = sync
	t.Cleanup(func() { syncStaging = original })
}

func uploadOnePart(be *Lustre, key, uploadID string, body []byte) error {
	bucket := testBucket
	part := int32(1)
	length := int64(len(body))
	_, err := be.UploadPart(context.Background(), &s3.UploadPartInput{
		Bucket:        &bucket,
		Key:           &key,
		UploadId:      &uploadID,
		PartNumber:    &part,
		ContentLength: &length,
		Body:          bytes.NewReader(body),
	})
	return err
}

func TestUploadPartSyncsPayloadBeforeThePartBecomesVisible(t *testing.T) {
	be := newTestBackend(t, Opts{})
	key := "synced"
	uploadID := createTestUpload(t, be, key)
	marker := partMarker(filepath.Join(testBucket, uploadDir(key, uploadID)), 1)

	syncs := 0
	visibleBeforeSync := false
	replaceSyncStaging(t, func(f *os.File) error {
		syncs++
		if _, err := os.Stat(marker); err == nil {
			visibleBeforeSync = true
		}
		return f.Sync()
	})

	if err := uploadOnePart(be, key, uploadID, randBytes(1234)); err != nil {
		t.Fatalf("UploadPart: %v", err)
	}

	if syncs != 1 {
		t.Errorf("payload synced %d times, want 1", syncs)
	}
	if visibleBeforeSync {
		t.Error("part marker existed before the payload was synced")
	}
}

func TestUploadPartFailsAndStaysInvisibleWhenTheSyncFails(t *testing.T) {
	be := newTestBackend(t, Opts{})
	key := "evicted"
	uploadID := createTestUpload(t, be, key)
	marker := partMarker(filepath.Join(testBucket, uploadDir(key, uploadID)), 1)

	// evict された client の書き込みは ESHUTDOWN で失敗する
	replaceSyncStaging(t, func(*os.File) error { return syscall.ESHUTDOWN })

	err := uploadOnePart(be, key, uploadID, randBytes(1234))

	if !errors.Is(err, syscall.ESHUTDOWN) {
		t.Fatalf("UploadPart error = %v, want ESHUTDOWN so the client re-sends the part", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("part marker exists after a failed sync: %v", err)
	}
}

func TestCopiedPartIsSyncedAfterItIsMovedIntoItsSlot(t *testing.T) {
	be := newTestBackend(t, Opts{})
	key := "copied"
	uploadID := createTestUpload(t, be, key)
	updir := filepath.Join(testBucket, uploadDir(key, uploadID))
	payload := randBytes(1234)
	if err := os.WriteFile(partMarker(updir, 1), payload, 0644); err != nil {
		t.Fatalf("write copied part: %v", err)
	}

	syncs := 0
	replaceSyncStaging(t, func(f *os.File) error {
		syncs++
		return f.Sync()
	})

	if err := relocateIntoSlot(updir, 1, int64(len(payload)), 0); err != nil {
		t.Fatalf("relocateIntoSlot: %v", err)
	}

	if syncs != 1 {
		t.Errorf("relocated payload synced %d times, want 1", syncs)
	}
}
