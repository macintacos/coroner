package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/macintacos/coroner/internal/timeline"
)

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

// stageBlob copies content into a new temporary file in dir, creating dir (mode 0700) when
// missing, and hashes it on the way.
func stageBlob(dir string, content io.Reader) (stagedBlob, error) {
	err := os.MkdirAll(dir, 0o700)
	if err != nil {
		return stagedBlob{}, fmt.Errorf("create blob directory: %w", err)
	}

	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return stagedBlob{}, fmt.Errorf("stage blob: %w", err)
	}

	staged, err := writeBlob(f, content)
	err = errors.Join(err, f.Close())
	if err != nil {
		return stagedBlob{}, errors.Join(fmt.Errorf("stage blob: %w", err), os.Remove(f.Name()))
	}

	staged.tempPath = f.Name()
	return staged, nil
}

// writeBlob copies content into f and syncs it, returning its hash, size and first sniffLen
// bytes.
func writeBlob(f *os.File, content io.Reader) (stagedBlob, error) {
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

	err = f.Sync()
	if err != nil {
		return stagedBlob{}, err
	}

	return stagedBlob{sha256: hex.EncodeToString(hash.Sum(nil)), size: size, head: head}, nil
}

// commitBlob names a staged blob by its hash, or discards it when a blob with that hash
// already exists, then syncs dir so the name survives a crash. Store.write may retry it, so
// a staged file already renamed is not an error.
func commitBlob(dir string, staged stagedBlob) error {
	final := filepath.Join(dir, staged.sha256)
	_, err := os.Lstat(final)
	switch {
	case err == nil:
		err = os.Remove(staged.tempPath)
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
	case errors.Is(err, fs.ErrNotExist):
		err = os.Rename(staged.tempPath, final)
	}
	if err != nil {
		return fmt.Errorf("store blob %s: %w", staged.sha256, err)
	}

	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("sync blob directory: %w", err)
	}

	return errors.Join(d.Sync(), d.Close())
}

// deleteUnreferenced deletes every blob in dir whose hash is not in referenced and returns
// their hashes in name order. It leaves anything that is not a blob, such as a staged file.
func deleteUnreferenced(dir string, referenced []string) ([]string, error) {
	entries, err := os.ReadDir(dir)
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

		err := os.Remove(filepath.Join(dir, name))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("delete blob %s: %w", name, err)
		}
		deleted = append(deleted, name)
	}

	return deleted, nil
}

// detectMIME sniffs head, the content's first bytes, and falls back to filename's extension
// when the content does not reveal its type.
func detectMIME(head []byte, filename string) string {
	detected := http.DetectContentType(head)
	if detected != "application/octet-stream" {
		return detected
	}

	byExtension := mime.TypeByExtension(filepath.Ext(filename))
	if byExtension == "" {
		return detected
	}

	return byExtension
}
