package debug

import (
	"fmt"
	"os"
	"sync"
	"time"
)

const debugLogPath = "/tmp/muxctl-debug.log"

var (
	enabled bool
	mu      sync.Mutex
	file    *os.File
)

// Enable turns on debug logging.
func Enable() error {
	mu.Lock()
	defer mu.Unlock()

	if enabled {
		return nil
	}

	var err error
	file, err = os.OpenFile(debugLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open debug log: %w", err)
	}

	enabled = true

	// Write initial log message directly (avoid recursive lock)
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	fmt.Fprintf(file, "[%s] === Debug logging started ===\n", timestamp)
	file.Sync()

	return nil
}

// IsEnabled returns true if debug logging is enabled.
func IsEnabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return enabled
}

// Log writes a message to the debug log.
func Log(format string, args ...interface{}) {
	mu.Lock()
	defer mu.Unlock()

	if !enabled || file == nil {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(file, "[%s] %s\n", timestamp, msg)
	file.Sync() // Flush immediately
}

// LogSection writes a section header to the debug log.
func LogSection(title string) {
	Log("")
	Log("──────────────────────────────────────────────────")
	Log("%s", title)
	Log("──────────────────────────────────────────────────")
}

// LogRequest logs an API request.
func LogRequest(provider, method, url string, body []byte) {
	LogSection(fmt.Sprintf("API Request: %s", provider))
	Log("Method: %s", method)
	Log("URL: %s", url)
	Log("Body:")
	Log("%s", string(body))
}

// LogResponse logs an API response.
func LogResponse(provider string, statusCode int, body []byte) {
	LogSection(fmt.Sprintf("API Response: %s", provider))
	Log("Status: %d", statusCode)
	Log("Body:")
	Log("%s", string(body))
}

// LogCLICommand logs a CLI command invocation.
func LogCLICommand(command string, args []string) {
	LogSection("CLI Command")
	Log("Command: %s", command)
	Log("Args: %v", args)
}

// LogCLIResponse logs CLI command output.
func LogCLIResponse(stdout, stderr string, err error) {
	LogSection("CLI Response")
	if stdout != "" {
		Log("Stdout:")
		Log("%s", stdout)
	}
	if stderr != "" {
		Log("Stderr:")
		Log("%s", stderr)
	}
	if err != nil {
		Log("Error: %v", err)
	}
}

// Close closes the debug log file.
func Close() {
	mu.Lock()
	defer mu.Unlock()

	if file != nil {
		file.Close()
		file = nil
	}
	enabled = false
}
