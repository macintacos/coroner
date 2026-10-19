package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/timeline"
)

// blobDir is the blob directory, and how a blob or the directory is made durable.
type blobDir struct {
	path string
	sync func(*os.File) error
}

// sniffLen is how many leading bytes http.DetectContentType considers.
const sniffLen = 512

// stagedBlob is content written and synced to a temporary file in the blob directory, not
// yet named by its hash.
type stagedBlob struct {
	tempPath string
	sha256   string
	size     int64
	head     []byte
}

// stageBlob copies content into a new temporary file in blobDir, creating blobDir (mode
// 0700) when missing, and hashes it on the way.
func stageBlob(blobDir blobDir, content io.Reader) (stagedBlob, error) {
	err := os.Mkdir(blobDir.path, 0o700)
	switch {
	case err == nil:
		// Without it, a power loss can drop the new directory, and with it a committed row's blob.
		err = syncDir(filepath.Dir(blobDir.path), blobDir.sync)
	case errors.Is(err, fs.ErrExist):
		err = nil
	}
	if err != nil {
		return stagedBlob{}, fmt.Errorf("create blob directory: %w", err)
	}

	f, err := os.CreateTemp(blobDir.path, ".tmp-*")
	if err != nil {
		return stagedBlob{}, fmt.Errorf("stage blob: %w", err)
	}

	staged, err := writeBlob(f, content, blobDir.sync)
	err = errors.Join(err, f.Close())
	if err != nil {
		return stagedBlob{}, errors.Join(fmt.Errorf("stage blob: %w", err), os.Remove(f.Name()))
	}

	staged.tempPath = f.Name()
	return staged, nil
}

// stageBlobs stages each of contents, outside any transaction, removing what it staged when
// one fails.
func stageBlobs(blobDir blobDir, contents []io.Reader) ([]stagedBlob, error) {
	staged := make([]stagedBlob, 0, len(contents))
	for _, content := range contents {
		blob, err := stageBlob(blobDir, content)
		if err != nil {
			discardBlobs(staged)
			return nil, err
		}
		staged = append(staged, blob)
	}

	return staged, nil
}

func discardBlobs(staged []stagedBlob) {
	for _, b := range staged {
		// Once commitBlob has renamed or discarded the file, the error is expected.
		_ = os.Remove(b.tempPath)
	}
}

// bySHA256 maps each of staged by its hash, keeping the first of blobs with the same bytes.
func bySHA256(staged []stagedBlob) map[string]stagedBlob {
	m := make(map[string]stagedBlob, len(staged))
	for _, b := range staged {
		if _, ok := m[b.sha256]; !ok {
			m[b.sha256] = b
		}
	}

	return m
}

// writeBlob copies content into f and syncs it, returning its hash, size and first sniffLen
// bytes.
func writeBlob(f *os.File, content io.Reader, sync func(*os.File) error) (stagedBlob, error) {
	head := make([]byte, sniffLen)
	n, err := io.ReadFull(content, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return stagedBlob{}, err
	}
	head = head[:n]

	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, hash), io.MultiReader(bytes.NewReader(head), content))
	if err != nil {
		return stagedBlob{}, err
	}

	err = sync(f)
	if err != nil {
		return stagedBlob{}, err
	}

	return stagedBlob{sha256: hex.EncodeToString(hash.Sum(nil)), size: size, head: head}, nil
}

// commitBlob names a staged blob by its hash and syncs blobDir so the name survives a
// crash, or discards it when a blob with that hash already exists. change may retry it,
// so a staged file already renamed is not an error.
func commitBlob(blobDir blobDir, staged stagedBlob) error {
	final := filepath.Join(blobDir.path, staged.sha256)
	_, err := os.Lstat(final)
	switch {
	case err == nil:
		err = os.Remove(staged.tempPath)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("store blob %s: %w", staged.sha256, err)
		}
		return nil
	case errors.Is(err, fs.ErrNotExist):
		err = os.Rename(staged.tempPath, final)
		if err != nil {
			return fmt.Errorf("store blob %s: %w", staged.sha256, err)
		}
		return syncDir(blobDir.path, blobDir.sync)
	default:
		return fmt.Errorf("store blob %s: %w", staged.sha256, err)
	}
}

func syncDir(dir string, sync func(*os.File) error) error {
	d, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("sync blob directory: %w", err)
	}

	return errors.Join(sync(d), d.Close())
}

// deleteUnreferenced deletes every blob in blobDir whose hash is not in referenced and
// returns their hashes in name order. It leaves anything that is not a blob, such as a
// staged file.
// ponytail: staged .tmp-* files a crash left are never collected; sweep by age with an
// injected clock if they accumulate.
func deleteUnreferenced(blobDir string, referenced []string) ([]string, error) {
	entries, err := os.ReadDir(blobDir)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read blob directory: %w", err)
	}

	keep := make(map[string]bool, len(referenced))
	for _, hash := range referenced {
		keep[hash] = true
	}

	deleted := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if keep[name] || timeline.ValidateSHA256(name) != nil {
			continue
		}

		err := os.Remove(filepath.Join(blobDir, name))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("delete blob %s: %w", name, err)
		}
		deleted = append(deleted, name)
	}

	return deleted, nil
}

// BlobPath returns the path of the blob whose SHA-256 is hash, in the blob directory of the
// database at dbPath.
func BlobPath(dbPath, hash string) string {
	return filepath.Join(config.BlobDir(dbPath), hash)
}

// DetectMIME sniffs head, the content's first bytes, and falls back to filename's extension
// when sniffing finds only bytes or text, which it reports for any UTF-8 such as JSON. The
// table is fixed because mime.TypeByExtension also reads the host's mime.types files.
func DetectMIME(head []byte, filename string) string {
	detected := http.DetectContentType(head)
	if detected != "application/octet-stream" && !strings.HasPrefix(detected, "text/plain") {
		return detected
	}

	switch strings.ToLower(filepath.Ext(filename)) {
	case ".json", ".har":
		return "application/json"
	case ".csv":
		return "text/csv"
	case ".svg":
		return "image/svg+xml"
	case ".md":
		return "text/markdown"
	case ".yaml", ".yml":
		return "application/yaml"
	default:
		return detected
	}
}
