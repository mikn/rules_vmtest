package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

// Build-time variables set via x_defs
var (
	vmPrimary      = ""
	vmDependencies = ""
)

type Job struct {
	ID      string
	Command string
}

type TmuxSession struct {
	Name string
}

func main() {
	if vmPrimary == "" {
		fmt.Fprintf(os.Stderr, "Error: vmPrimary not set via x_defs\n")
		os.Exit(1)
	}

	var (
		sessionName = flag.String("session", "molnett", "Tmux session name")
		attach      = flag.Bool("attach", true, "Attach to tmux session")
		run         = flag.String("run", "", "Command to run in foreground")
	)
	flag.Parse()

	r, err := runfiles.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to initialize runfiles: %v\n", err)
		os.Exit(1)
	}

	allJobs := make(map[string]string)
	var ensureJobs []Job

	if vmDependencies != "" {
		for _, dep := range strings.Split(vmDependencies, ",") {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}

			parts := strings.Split(dep, "/")
			windowName := parts[len(parts)-1]
			allJobs[windowName] = dep

			depBinary, err := r.Rlocation(dep)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to resolve dependency %q from runfiles: %v\n", dep, err)
				os.Exit(1)
			}

			ensureJobs = append(ensureJobs, Job{
				ID:      windowName,
				Command: depBinary,
			})
		}
	}

	parts := strings.Split(vmPrimary, "/")
	primaryWindow := parts[len(parts)-1]
	allJobs[primaryWindow] = vmPrimary

	primaryBinary, err := r.Rlocation(vmPrimary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to resolve primary VM %q from runfiles: %v\n", vmPrimary, err)
		os.Exit(1)
	}

	ensureJobs = append(ensureJobs, Job{
		ID:      primaryWindow,
		Command: primaryBinary,
	})

	if *run != "" {
		runWindowName := *run
		if strings.Contains(*run, "/") {
			parts := strings.Split(*run, "/")
			possibleName := parts[len(parts)-1]
			for _, job := range ensureJobs {
				if job.ID == possibleName {
					runWindowName = possibleName
					break
				}
			}
		}

		var filteredJobs []Job
		for _, job := range ensureJobs {
			if job.ID != runWindowName {
				filteredJobs = append(filteredJobs, job)
			}
		}
		if len(filteredJobs) < len(ensureJobs) {
			ensureJobs = filteredJobs
		}
	}

	session := &TmuxSession{Name: *sessionName}

	hasSession := session.exists()

	fmt.Printf("[orchestrator] Session %s exists: %v, ensure jobs: %d\n", *sessionName, hasSession, len(ensureJobs))
	for i, job := range ensureJobs {
		fmt.Printf("[orchestrator] Job %d: ID=%s, Command=%s\n", i, job.ID, job.Command)
	}

	if !hasSession && len(ensureJobs) > 0 {
		firstJob := ensureJobs[0]
		fmt.Printf("[orchestrator] Creating new session %s with first window %s\n", *sessionName, firstJob.ID)
		if err := session.create(firstJob.ID, firstJob.Command); err != nil {
			fmt.Fprintf(os.Stderr, "[orchestrator] ERROR: Failed to create session: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("[orchestrator] Created tmux session: %s with window: %s\n", *sessionName, firstJob.ID)

		for _, job := range ensureJobs[1:] {
			fmt.Printf("[orchestrator] Creating additional window: %s\n", job.ID)
			if err := session.createWindow(job.ID, job.Command); err != nil {
				fmt.Fprintf(os.Stderr, "[orchestrator] ERROR: Failed to create window %s: %v\n", job.ID, err)
			} else {
				fmt.Printf("[orchestrator] Created window: %s\n", job.ID)
			}
		}
	} else if hasSession {
		existingWindows := session.listWindows()

		for _, job := range ensureJobs {
			if !contains(existingWindows, job.ID) {
				if err := session.createWindow(job.ID, job.Command); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to create window %s: %v\n", job.ID, err)
				} else {
					fmt.Printf("[orchestrator] Created window: %s in existing session\n", job.ID)
				}
			}
		}
	}

	if *run != "" {
		runCmd := *run

		if !strings.Contains(runCmd, "/") {
			if runfilePath, exists := allJobs[runCmd]; exists {
				runCmd = runfilePath
				fmt.Printf("[orchestrator] Resolved window name '%s' to path '%s'\n", *run, runCmd)
			}
		}

		if !strings.HasPrefix(runCmd, "/") {
			resolved, err := r.Rlocation(runCmd)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[orchestrator] ERROR: Failed to resolve runfile %s: %v\n", runCmd, err)
				os.Exit(1)
			}
			runCmd = resolved
		}

		fmt.Printf("[orchestrator] Running foreground command: %s\n", runCmd)
		exitCode := runForeground(strings.Fields(runCmd))

		fmt.Printf("[orchestrator] Foreground command exited with code %d, cleaning up windows\n", exitCode)
		for _, job := range ensureJobs {
			fmt.Printf("[orchestrator] Killing window: %s\n", job.ID)
			session.killWindow(job.ID)
		}

		os.Exit(exitCode)
	} else if *attach {
		cmd := exec.Command("tmux", "select-window", "-t", fmt.Sprintf("%s:%s", *sessionName, primaryWindow))
		cmd.Run()

		if err := session.attach(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to attach to session: %v\n", err)
			os.Exit(1)
		}
	}
}

func (s *TmuxSession) exists() bool {
	cmd := exec.Command("tmux", "has-session", "-t", s.Name)
	return cmd.Run() == nil
}

func (s *TmuxSession) create(windowName, command string) error {
	args := []string{"new-session", "-d", "-s", s.Name, "-n", windowName}

	runfilesEnv, err := runfiles.Env()
	if err != nil {
		return fmt.Errorf("failed to get runfiles env: %v", err)
	}

	for _, env := range runfilesEnv {
		args = append(args, "-e", env)
	}

	if workspace := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); workspace != "" {
		args = append(args, "-e", fmt.Sprintf("BUILD_WORKSPACE_DIRECTORY=%s", workspace))
	}

	args = append(args, command)

	cmd := exec.Command("tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create session: %v\nOutput: %s", err, string(output))
	}

	time.Sleep(100 * time.Millisecond)

	if !s.hasWindow(windowName) {
		return fmt.Errorf("window %s exited immediately after creation", windowName)
	}

	return nil
}

func (s *TmuxSession) createWindow(windowName, command string) error {
	args := []string{"new-window", "-t", s.Name, "-n", windowName}

	runfilesEnv, err := runfiles.Env()
	if err != nil {
		return fmt.Errorf("failed to get runfiles env: %v", err)
	}

	for _, env := range runfilesEnv {
		args = append(args, "-e", env)
	}

	if workspace := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); workspace != "" {
		args = append(args, "-e", fmt.Sprintf("BUILD_WORKSPACE_DIRECTORY=%s", workspace))
	}

	args = append(args, command)

	cmd := exec.Command("tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create window: %v\nOutput: %s", err, string(output))
	}

	time.Sleep(100 * time.Millisecond)

	if !s.hasWindow(windowName) {
		return fmt.Errorf("window %s exited immediately after creation", windowName)
	}

	return nil
}

func (s *TmuxSession) hasWindow(windowName string) bool {
	windows := s.listWindows()
	for _, w := range windows {
		if w == windowName {
			return true
		}
	}
	return false
}

func (s *TmuxSession) listWindows() []string {
	cmd := exec.Command("tmux", "list-windows", "-t", s.Name, "-F", "#{window_name}")
	output, err := cmd.Output()
	if err != nil {
		return []string{}
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	windows := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			windows = append(windows, line)
		}
	}
	return windows
}

func (s *TmuxSession) killWindow(windowName string) {
	cmd := exec.Command("tmux", "kill-window", "-t", fmt.Sprintf("%s:%s", s.Name, windowName))
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "[orchestrator] Warning: failed to kill window %s: %v\n", windowName, err)
	} else {
		fmt.Printf("[orchestrator] Killed window: %s\n", windowName)
	}
}

func (s *TmuxSession) attach() error {
	cmd := exec.Command("tmux", "attach-session", "-t", s.Name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runForeground(command []string) int {
	if len(command) == 0 {
		fmt.Fprintf(os.Stderr, "Empty command for --run\n")
		return 1
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()

	runfilesEnv, err := runfiles.Env()
	if err == nil {
		cmd.Env = append(cmd.Env, runfilesEnv...)
	}

	err = cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "Failed to run command: %v\n", err)
		return 127
	}

	return 0
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
