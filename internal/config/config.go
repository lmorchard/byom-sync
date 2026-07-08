package config

// Config holds application configuration
type Config struct {
	// Core settings
	Verbose bool
	Debug   bool
	LogJSON bool

	// Add command-specific configuration fields here as needed
	// Example:
	// Fetch struct {
	//     Concurrency int
	//     Timeout     time.Duration
	// }
}
