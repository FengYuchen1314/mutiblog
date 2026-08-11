package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type CommandRenderer struct {
	NodeBinary  string
	RendererCLI string
}

// ValidateNodeBinary fails closed on runtimes whose Permission Model does not
// include default-denied network access. --allow-net was introduced after
// Node 24; MutiBlog's production contract starts at Node 26.
func ValidateNodeBinary(parent context.Context, binary string) error {
	if strings.TrimSpace(binary) == "" {
		binary = "node"
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "--version")
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("check renderer Node.js version: %w", err)
	}
	version := strings.TrimSpace(strings.TrimPrefix(string(output), "v"))
	majorText, _, _ := strings.Cut(version, ".")
	major, err := strconv.Atoi(majorText)
	if err != nil || major < 26 {
		return fmt.Errorf("renderer requires Node.js 26 or newer, found %q", strings.TrimSpace(string(output)))
	}
	return nil
}

func (r CommandRenderer) Render(ctx context.Context, inputPath, outputPath string) (BuildReport, error) {
	if strings.TrimSpace(r.RendererCLI) == "" {
		return BuildReport{}, errors.New("renderer CLI path is empty")
	}
	node := r.NodeBinary
	if node == "" {
		node = "node"
	}
	permissionArgs, err := rendererPermissionArgs(r.RendererCLI, inputPath, outputPath)
	if err != nil {
		return BuildReport{}, err
	}
	arguments := append(permissionArgs, r.RendererCLI, "--input", inputPath, "--output", outputPath)
	command := exec.CommandContext(ctx, node, arguments...)
	// A theme is server-side code, but it has no reason to inherit deployment
	// credentials or runtime tuning from the long-lived application process.
	// Node 26's Permission Model denies network access unless --allow-net is
	// explicitly granted; keeping the environment minimal also prevents
	// NODE_OPTIONS and provider-specific variables from weakening that boundary.
	command.Env = []string{
		"HOME=/tmp",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"TZ=UTC",
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return BuildReport{}, fmt.Errorf("renderer failed: %s", message)
	}
	var report BuildReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		return BuildReport{}, fmt.Errorf("decode renderer report: %w", err)
	}
	return report, nil
}

func rendererPermissionArgs(rendererCLI, inputPath, outputPath string) ([]string, error) {
	inputData, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("read renderer input for sandbox: %w", err)
	}
	var snapshot struct {
		Theme ThemeInput `json:"theme"`
	}
	if err := json.Unmarshal(inputData, &snapshot); err != nil {
		return nil, fmt.Errorf("decode renderer input for sandbox: %w", err)
	}
	inputPath, err = filepath.Abs(inputPath)
	if err != nil {
		return nil, err
	}
	outputPath, err = filepath.Abs(outputPath)
	if err != nil {
		return nil, err
	}
	rendererCLI, err = filepath.Abs(rendererCLI)
	if err != nil {
		return nil, err
	}
	repositoryRoot := filepath.Dir(filepath.Dir(filepath.Dir(inputPath)))
	stagingRoot := filepath.Join(repositoryRoot, "generated", "staging")
	if !pathWithin(stagingRoot, inputPath) || !pathWithin(stagingRoot, outputPath) {
		return nil, errors.New("renderer input and output must stay inside generated staging")
	}
	readPaths := []string{rendererCLI, inputPath, outputPath}
	themesRoot := filepath.Join(repositoryRoot, "themes")
	for _, themePath := range []string{snapshot.Theme.ModulePath, snapshot.Theme.AssetsPath} {
		if themePath == "" {
			continue
		}
		absolute, err := filepath.Abs(themePath)
		if err != nil {
			return nil, err
		}
		if !pathWithin(themesRoot, absolute) {
			return nil, errors.New("custom theme runtime escapes the themes directory")
		}
		readPaths = append(readPaths, absolute)
	}
	arguments := []string{"--max-old-space-size=256", "--permission"}
	seen := map[string]bool{}
	for _, path := range readPaths {
		if !seen[path] {
			arguments = append(arguments, "--allow-fs-read="+path)
			seen[path] = true
		}
	}
	// Node only expands an existing directory permission to its descendants.
	// The renderer output deliberately does not exist when the permission model
	// starts, so grant both the directory entry (rm/mkdir) and its future tree.
	arguments = append(arguments,
		"--allow-fs-read="+outputPath+string(filepath.Separator)+"*",
		"--allow-fs-write="+outputPath,
		"--allow-fs-write="+outputPath+string(filepath.Separator)+"*",
	)
	return arguments, nil
}

func pathWithin(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
