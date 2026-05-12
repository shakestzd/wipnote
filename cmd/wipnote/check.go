package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

// gateResult holds the outcome of a single quality gate.
type gateResult struct {
	name   string
	passed bool
	err    error
}

func checkCmd() *cobra.Command {
	var goOnly, pythonOnly, skipTests bool

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Run automated quality gate checks",
		Long: `Run quality gate checks for the project.

Detects which languages are present and runs the appropriate gates:
  Go:     go build ./...  |  go vet ./...  |  go test ./...
  Python: uv run ruff check --fix  |  uv run ruff format  |  uv run mypy src/  |  uv run pytest

Returns exit code 0 if all gates pass, 1 if any fail.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectRoot, err := resolveProjectRoot()
			if err != nil {
				return err
			}

			var results []gateResult
			ranAny := false

			if !pythonOnly && hasGoProject(projectRoot) {
				ranAny = true
				results = append(results, runGoGates(projectRoot, skipTests)...)
			}

			if !pythonOnly && hasNodeProject(projectRoot) {
				ranAny = true
			}

			if !goOnly && hasPythonProject(projectRoot) {
				ranAny = true
				results = append(results, runPythonGates(projectRoot, skipTests)...)
			}

			if !ranAny {
				fmt.Println("No supported project detected (Go: look for go.mod at project root or subdirectories; Python: src/python; Node: package.json).")
				return nil
			}

			return printResults(results)
		},
	}

	cmd.Flags().BoolVar(&goOnly, "go-only", false, "Run Go quality gates only")
	cmd.Flags().BoolVar(&pythonOnly, "python-only", false, "Run Python quality gates only")
	cmd.Flags().BoolVar(&skipTests, "skip-tests", false, "Skip test execution (run lint/build only)")

	cmd.AddCommand(checkOrphansCmd())
	cmd.AddCommand(checkIncompleteCmd())
	cmd.AddCommand(checkCrossProjectCmd())
	cmd.AddCommand(checkHostPathsCmd())
	return cmd
}

// resolveProjectRoot finds the project root from the .wipnote directory.
func resolveProjectRoot() (string, error) {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		// Fall back to CWD if not in an wipnote project.
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return "", fmt.Errorf("get working directory: %w", cwdErr)
		}
		return cwd, nil
	}
	return filepath.Dir(wipnoteDir), nil
}

func hasGoProject(root string) bool {
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return true
	}
	_, err := os.Stat(filepath.Join(root, "packages", "go", "go.mod"))
	return err == nil
}

func hasNodeProject(root string) bool {
	_, err := os.Stat(filepath.Join(root, "package.json"))
	return err == nil
}

func hasPythonProject(root string) bool {
	_, err := os.Stat(filepath.Join(root, "src", "python"))
	return err == nil
}

// runGate executes a command, capturing its combined output, and returns a gateResult.
func runGate(name, dir string, args ...string) gateResult {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	return gateResult{name: name, passed: err == nil, err: err}
}

func runGoGates(root string, skipTests bool) []gateResult {
	goDir := filepath.Join(root, "packages", "go")
	gates := []gateResult{
		runGate("go build", goDir, "go", "build", "./..."),
		runGate("go vet", goDir, "go", "vet", "./..."),
	}
	if !skipTests {
		gates = append(gates, runGate("go test", goDir, "go", "test", "./..."))
	}
	return gates
}

func runPythonGates(root string, skipTests bool) []gateResult {
	gates := []gateResult{
		runGate("ruff check", root, "uv", "run", "ruff", "check", "--fix"),
		runGate("ruff format", root, "uv", "run", "ruff", "format"),
		runGate("mypy", root, "uv", "run", "mypy", "src/"),
	}
	if !skipTests {
		gates = append(gates, runGate("pytest", root, "uv", "run", "pytest"))
	}
	return gates
}

// printResults displays a summary table and returns an error if any gate failed.
func printResults(results []gateResult) error {
	fmt.Println()
	fmt.Println("Quality Gate Results")
	fmt.Println("--------------------")

	allPassed := true
	for _, r := range results {
		status := "\033[32mPASS\033[0m"
		if !r.passed {
			status = "\033[31mFAIL\033[0m"
			allPassed = false
		}
		fmt.Printf("  [%s]  %s\n", status, r.name)
	}

	fmt.Println()
	if allPassed {
		fmt.Println("\033[32mAll quality gates passed.\033[0m")
		return nil
	}
	fmt.Println("\033[31mOne or more quality gates failed.\033[0m")
	return fmt.Errorf("quality gates failed — see details above\nRun individual checks with 'wipnote check --go-only' to isolate failures.")
}
