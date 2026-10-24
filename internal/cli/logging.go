package cli

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

const (
	logDirectoryName = "ramseyer-financial-manager"
	logSubdirectory  = "logs"
	logFileName      = "server.log"
	logRotateBytes   = 5 << 20
)

// LogFilePath reports where the process log is written so installers and support sessions can
// point a client at the exact file instead of guessing. An empty string means the OS gave us no
// config directory, in which case file logging is skipped rather than failing the launch.
func LogFilePath() string {
	configDirectory, err := os.UserConfigDir()
	if err != nil || configDirectory == "" {
		return ""
	}
	return filepath.Join(configDirectory, logDirectoryName, logSubdirectory, logFileName)
}

// SetupLogging mirrors the process log into a file because the packaged Windows build uses the
// GUI subsystem. In that mode stderr is discarded, so a failed scheduled task, a bad database
// path, or a crash leaves no trace at all on the client machine. The first install reported
// success while nothing was listening on the port, and there was no way to see why.
//
// The file is kept open for the process lifetime on purpose. Closing it with defer would never
// run after log.Fatal, which is exactly the failure we most need recorded.
func SetupLogging() {
	path := LogFilePath()
	if path == "" {
		return
	}

	// A single bounded rotation keeps the client from accumulating an unbounded log while still
	// preserving the previous run, which is usually the interesting one after a failed start.
	if info, err := os.Stat(path); err == nil && info.Size() > logRotateBytes {
		_ = os.Rename(path, path+".1")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}

	// stderr is included so console builds and an attached Windows console still see output.
	log.SetOutput(io.MultiWriter(os.Stderr, file))
}
