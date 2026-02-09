package tools

import (
	"os"
	"path/filepath"
	"strings"
)

// Permissions defines what operations a tool is allowed to perform.
type Permissions struct {
	// AllowBash allows executing bash commands.
	AllowBash bool

	// AllowFileRead allows reading files.
	AllowFileRead bool

	// AllowFileWrite allows writing files.
	AllowFileWrite bool

	// AllowGit allows git operations.
	AllowGit bool

	// AllowGitHub allows GitHub API operations.
	AllowGitHub bool

	// AllowNetwork allows network operations.
	AllowNetwork bool
}

// DefaultPermissions returns permissions with all operations allowed.
func DefaultPermissions() Permissions {
	return Permissions{
		AllowBash:      true,
		AllowFileRead:  true,
		AllowFileWrite: true,
		AllowGit:       true,
		AllowGitHub:    true,
		AllowNetwork:   true,
	}
}

// RestrictedPermissions returns permissions with only read operations allowed.
func RestrictedPermissions() Permissions {
	return Permissions{
		AllowFileRead: true,
	}
}

// ToolContext provides execution context for tools.
type ToolContext struct {
	// WorkDir is the working directory for tool execution.
	// File operations should be restricted to this directory.
	WorkDir string

	// Permissions defines what operations are allowed.
	Permissions Permissions

	// GitHubToken is the token for GitHub API operations.
	GitHubToken string

	// RepoOwner is the owner of the repository.
	RepoOwner string

	// RepoName is the name of the repository.
	RepoName string

	// Env contains environment variables available to tools.
	Env map[string]string

	// BashTimeout is the timeout for bash command execution in seconds.
	BashTimeout int
}

// NewToolContext creates a new tool context with the given working directory.
func NewToolContext(workDir string) *ToolContext {
	return &ToolContext{
		WorkDir:     workDir,
		Permissions: DefaultPermissions(),
		Env:         make(map[string]string),
		BashTimeout: 60, // Default 60 seconds
	}
}

// WithPermissions sets the permissions and returns the context for chaining.
func (c *ToolContext) WithPermissions(p Permissions) *ToolContext {
	c.Permissions = p
	return c
}

// WithGitHub sets GitHub credentials and returns the context for chaining.
func (c *ToolContext) WithGitHub(token, owner, repo string) *ToolContext {
	c.GitHubToken = token
	c.RepoOwner = owner
	c.RepoName = repo
	return c
}

// WithEnv sets an environment variable and returns the context for chaining.
func (c *ToolContext) WithEnv(key, value string) *ToolContext {
	if c.Env == nil {
		c.Env = make(map[string]string)
	}
	c.Env[key] = value
	return c
}

// WithBashTimeout sets the bash timeout and returns the context for chaining.
func (c *ToolContext) WithBashTimeout(seconds int) *ToolContext {
	c.BashTimeout = seconds
	return c
}

// ValidatePath checks if the given path is within the working directory.
// Returns the cleaned absolute path if valid, or an error if the path
// is outside the working directory.
func (c *ToolContext) ValidatePath(path string) (string, error) {
	if c.WorkDir == "" {
		return "", ErrNoWorkDir
	}

	// Handle relative paths
	var absPath string
	if filepath.IsAbs(path) {
		absPath = filepath.Clean(path)
	} else {
		absPath = filepath.Clean(filepath.Join(c.WorkDir, path))
	}

	// Get the absolute work directory
	absWorkDir, err := filepath.Abs(c.WorkDir)
	if err != nil {
		return "", err
	}
	absWorkDir = filepath.Clean(absWorkDir)

	// Check if the path is within the working directory
	rel, err := filepath.Rel(absWorkDir, absPath)
	if err != nil {
		return "", ErrPathOutsideWorkDir
	}

	// If the relative path starts with "..", it's outside the work directory
	if strings.HasPrefix(rel, "..") {
		return "", ErrPathOutsideWorkDir
	}

	return absPath, nil
}

// ResolvePath resolves a path relative to the working directory.
// Unlike ValidatePath, this does not check if the path exists.
func (c *ToolContext) ResolvePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(c.WorkDir, path))
}

// FileExists checks if a file exists at the given path.
func (c *ToolContext) FileExists(path string) bool {
	absPath, err := c.ValidatePath(path)
	if err != nil {
		return false
	}
	_, err = os.Stat(absPath)
	return err == nil
}

// IsDir checks if the path is a directory.
func (c *ToolContext) IsDir(path string) bool {
	absPath, err := c.ValidatePath(path)
	if err != nil {
		return false
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// Common errors for tool execution.
type toolError string

func (e toolError) Error() string { return string(e) }

const (
	ErrNoWorkDir        toolError = "working directory not set"
	ErrPathOutsideWorkDir toolError = "path is outside working directory"
	ErrPermissionDenied toolError = "permission denied"
	ErrBashNotAllowed   toolError = "bash execution not allowed"
	ErrFileReadNotAllowed toolError = "file read not allowed"
	ErrFileWriteNotAllowed toolError = "file write not allowed"
	ErrGitNotAllowed    toolError = "git operations not allowed"
	ErrGitHubNotAllowed toolError = "github operations not allowed"
	ErrNetworkNotAllowed toolError = "network operations not allowed"
)

// CheckBash checks if bash execution is allowed.
func (c *ToolContext) CheckBash() error {
	if !c.Permissions.AllowBash {
		return ErrBashNotAllowed
	}
	return nil
}

// CheckFileRead checks if file read operations are allowed.
func (c *ToolContext) CheckFileRead() error {
	if !c.Permissions.AllowFileRead {
		return ErrFileReadNotAllowed
	}
	return nil
}

// CheckFileWrite checks if file write operations are allowed.
func (c *ToolContext) CheckFileWrite() error {
	if !c.Permissions.AllowFileWrite {
		return ErrFileWriteNotAllowed
	}
	return nil
}

// CheckGit checks if git operations are allowed.
func (c *ToolContext) CheckGit() error {
	if !c.Permissions.AllowGit {
		return ErrGitNotAllowed
	}
	return nil
}

// CheckGitHub checks if GitHub operations are allowed.
func (c *ToolContext) CheckGitHub() error {
	if !c.Permissions.AllowGitHub {
		return ErrGitHubNotAllowed
	}
	return nil
}

// CheckNetwork checks if network operations are allowed.
func (c *ToolContext) CheckNetwork() error {
	if !c.Permissions.AllowNetwork {
		return ErrNetworkNotAllowed
	}
	return nil
}
