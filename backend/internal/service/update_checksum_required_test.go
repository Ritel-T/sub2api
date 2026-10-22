//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateRejectsMissingChecksumBeforeDownload(t *testing.T) {
	svc := NewUpdateService(nil, &updateServiceGitHubClientStub{}, "2.16.0.1-ritel", "release")
	archive := Asset{Name: "sub2api_" + svc.getArchiveName() + ".tar.gz", DownloadURL: "https://github.com/Ritel-T/sub2api/releases/download/test/archive.tar.gz"}
	err := svc.applyReleaseAssets(context.Background(), []Asset{archive})
	require.ErrorContains(t, err, "checksum manifest is required")
}

func TestUpdateRejectsAmbiguousArchivesBeforeDownload(t *testing.T) {
	svc := NewUpdateService(nil, &updateServiceGitHubClientStub{}, "2.16.0.1-ritel", "release")
	archive := Asset{Name: "sub2api_" + svc.getArchiveName() + ".tar.gz", DownloadURL: "https://github.com/Ritel-T/sub2api/releases/download/test/archive.tar.gz"}
	err := svc.applyReleaseAssets(context.Background(), []Asset{archive, archive})
	require.ErrorContains(t, err, "multiple compatible release archives")
}

type checksumManifestClient struct {
	updateServiceGitHubClientStub
	manifest []byte
}

func (c *checksumManifestClient) FetchChecksumFile(context.Context, string) ([]byte, error) {
	return c.manifest, nil
}

func TestUpdateChecksumRejectsTamperingAndMissingEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	require.NoError(t, os.WriteFile(path, []byte("approved archive"), 0600))
	sum := sha256.Sum256([]byte("approved archive"))
	client := &checksumManifestClient{manifest: []byte(fmt.Sprintf("%x  archive.tar.gz\n", sum))}
	svc := NewUpdateService(nil, client, "2.16.0.1-ritel", "release")
	require.NoError(t, svc.verifyChecksum(context.Background(), path, "https://github.com/checksums.txt"))
	require.NoError(t, os.WriteFile(path, []byte("tampered archive"), 0600))
	require.ErrorContains(t, svc.verifyChecksum(context.Background(), path, "https://github.com/checksums.txt"), "checksum mismatch")
	client.manifest = []byte(fmt.Sprintf("%x  other.tar.gz\n", sum))
	require.ErrorContains(t, svc.verifyChecksum(context.Background(), path, "https://github.com/checksums.txt"), "checksum not found")
}
