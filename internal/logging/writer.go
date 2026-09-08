package logging

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// logFileMode is the permission applied when the log file is created. It
// matches the mode the logrotate example in docs/10_operations.md creates.
const logFileMode os.FileMode = 0o640

// reopenWriter is an io.Writer backed by a file that can be closed and reopened
// by path while writes continue.
//
// This is what makes SIGHUP-based log rotation work: logrotate renames the file
// and signals the process, the process reopens the original path, and the next
// write lands in the new file. No line is lost, and copytruncate is not needed.
type reopenWriter struct {
	path string

	mu   sync.Mutex
	file *os.File
}

func newReopenWriter(path string) (*reopenWriter, error) {
	w := &reopenWriter{path: path}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

// open replaces the current file handle with a freshly opened one. The caller
// must not hold the mutex.
func (w *reopenWriter) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logFileMode)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", w.path, err)
	}

	w.mu.Lock()
	previous := w.file
	w.file = file
	w.mu.Unlock()

	if previous != nil {
		// A close failure here is not worth failing the reopen over: the new
		// handle is already in place and writes are succeeding.
		_ = previous.Close()
	}
	return nil
}

func (w *reopenWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, os.ErrClosed
	}
	return w.file.Write(p)
}

// Reopen closes the current handle and opens the configured path again.
func (w *reopenWriter) Reopen() error {
	return w.open()
}

// Close releases the file handle.
func (w *reopenWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// nopCloser adapts a plain writer, such as stdout, to the closer interface so
// that Logger can treat both cases alike. Closing it does nothing, because the
// application does not own stdout.
type nopCloser struct {
	io.Writer
}

func (nopCloser) Reopen() error { return nil }
func (nopCloser) Close() error  { return nil }
