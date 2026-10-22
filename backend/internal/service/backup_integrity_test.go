//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackupIntegrity_NewSingleArchiveRecordsDigest(t *testing.T) {
	for _, asynchronous := range []bool{false, true} {
		t.Run(fmt.Sprintf("async=%t", asynchronous), func(t *testing.T) {
			repo := newMockSettingRepo()
			seedS3Config(t, repo)
			store := newMockObjectStore()
			svc := newTestBackupService(repo, &mockDumper{dumpData: []byte("SELECT 1;\n")}, store)
			var record *BackupRecord
			var err error
			if asynchronous {
				record, err = svc.StartBackup(context.Background(), "manual", 14)
				require.NoError(t, err)
				svc.wg.Wait()
			} else {
				record, err = svc.CreateBackup(context.Background(), "manual", 14)
				require.NoError(t, err)
			}
			record, err = svc.GetBackupRecord(context.Background(), record.ID)
			require.NoError(t, err)
			require.Equal(t, "completed", record.Status)
			store.mu.Lock()
			data := append([]byte(nil), store.objects[record.S3Key]...)
			store.mu.Unlock()
			require.NotEmpty(t, data)
			require.Equal(t, int64(len(data)), record.SizeBytes)
			require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(data)), record.SHA256)
		})
	}
}

func TestBackupIntegrity_RestoreRejectsUntrustedArchiveBeforeSQL(t *testing.T) {
	for _, asynchronous := range []bool{false, true} {
		for _, scenario := range []string{"same-size valid gzip replacement", "oversized object", "truncated object", "missing digest", "invalid digest", "missing size"} {
			t.Run(fmt.Sprintf("async=%t/%s", asynchronous, scenario), func(t *testing.T) {
				repo := newMockSettingRepo()
				seedS3Config(t, repo)
				store := newMockObjectStore()
				dumper := &mockDumper{dumpData: []byte("SELECT 1;\n")}
				svc := newTestBackupService(repo, dumper, store)
				record, err := svc.CreateBackup(context.Background(), "manual", 14)
				require.NoError(t, err)
				store.mu.Lock()
				switch scenario {
				case "same-size valid gzip replacement":
					original := store.objects[record.S3Key]
					replacement := gzipBackupBytes(t, []byte("SELECT 2;\n"))
					require.Equal(t, len(original), len(replacement), "exercise digest rather than size validation")
					store.objects[record.S3Key] = replacement
				case "oversized object":
					store.objects[record.S3Key] = append(store.objects[record.S3Key], 0)
				case "truncated object":
					store.objects[record.S3Key] = store.objects[record.S3Key][:len(store.objects[record.S3Key])-1]
				case "missing digest":
					record.SHA256 = ""
				case "invalid digest":
					record.SHA256 = "invalid"
				case "missing size":
					record.SizeBytes = 0
				}
				store.mu.Unlock()
				require.NoError(t, svc.saveRecord(context.Background(), record))
				if asynchronous {
					_, err = svc.StartRestore(context.Background(), record.ID)
					require.NoError(t, err)
					svc.wg.Wait()
					result, readErr := svc.GetBackupRecord(context.Background(), record.ID)
					require.NoError(t, readErr)
					require.Equal(t, "failed", result.RestoreStatus)
					require.NotEmpty(t, result.RestoreError)
				} else {
					require.Error(t, svc.RestoreBackup(context.Background(), record.ID))
				}
				require.Empty(t, dumper.restored, "unverified bytes must never reach psql")
			})
		}
	}
}

func TestBackupIntegrity_LegacySingleArchiveStaysDownloadable(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	record := &BackupRecord{ID: "legacy", Status: "completed", S3Key: "legacy.sql.gz", SizeBytes: 100}
	require.NoError(t, svc.saveRecord(context.Background(), record))
	require.ErrorContains(t, svc.RestoreBackup(context.Background(), record.ID), "independently verify")
	download, err := svc.GetBackupDownloadURL(context.Background(), record.ID)
	require.NoError(t, err)
	require.NotEmpty(t, download.URL)
}

func TestBackupIntegrity_SplitArchiveRejectsMissingOrMalformedDigest(t *testing.T) {
	for _, digest := range []string{"", "bad", strings.Repeat("0", 62)} {
		t.Run(fmt.Sprintf("digest-length=%d", len(digest)), func(t *testing.T) {
			repo := newMockSettingRepo()
			seedS3Config(t, repo)
			store := newMockObjectStore()
			store.objects["part-1"] = []byte("abc")
			svc := newTestBackupService(repo, &mockDumper{}, store)
			path, err := svc.downloadBackupParts(context.Background(), store, []BackupPart{{Index: 1, S3Key: "part-1", SizeBytes: 3, SHA256: digest}})
			require.ErrorContains(t, err, "independently verify")
			require.Empty(t, path)
		})
	}
}

func TestBackupIntegrity_MetadataWriteFailurePreventsUpload(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	svc.settingRepo = &backupFailRecordWriteRepo{repo}
	path := filepath.Join(t.TempDir(), "archive.sql.gz")
	require.NoError(t, os.WriteFile(path, gzipBackupBytes(t, []byte("SELECT 1;")), 0600))
	record := &BackupRecord{ID: "metadata-failure", S3Key: "archive.sql.gz"}
	err := svc.uploadBackupArchive(context.Background(), record, store, &BackupS3Config{}, path)
	require.ErrorContains(t, err, "save backup integrity metadata")
	store.mu.Lock()
	defer store.mu.Unlock()
	require.Zero(t, store.uploadFileCalls)
	require.Empty(t, store.objects)
}
