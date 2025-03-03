package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/andygrunwald/go-jira"
	"github.com/fatih/color"
	"github.com/sashabaranov/go-openai"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Version is the current version of this CLI.
const Version = "1.1.0"

// -- Embed default templates
//
//go:embed templates/systemInstructions.md
var embeddedSystemInstructions string

//go:embed templates/prTitleFormattingInstructions.md
var embeddedPRTitleFormattingInstructions string

//go:embed templates/prBodyFormattingInstructions.md
var embeddedPRBodyFormattingInstructions string

//go:embed templates/commitFormattingInstructions.md
var embeddedCommitFormattingInstructions string

//go:embed templates/asciiHeader.txt
var ASCIIHeader string

// GitAIException wraps internal errors for clarity.
type GitAIException struct{ msg string }

func (e GitAIException) Error() string { return e.msg }

// logWithLevel is a unified logging function with optional verbosity control.
func logWithLevel(level string, c color.Attribute, msg string) {
	level = strings.ToUpper(level)
	if level == "DEBUG" && !viper.GetBool("VERBOSE") {
		return
	}
	color.New(c).Fprintf(os.Stderr, "[%s] %s\n", level, msg)
}

func logDebug(msg string) {
	logWithLevel("debug", color.FgMagenta, msg)
}

func logMessage(c color.Attribute, msg string) {
	logWithLevel("info", c, msg)
}

func logError(msg string) {
	logWithLevel("error", color.FgRed, msg)
}

// GitOperations encapsulates direct interactions with the git CLI.
type GitOperations struct{}

func (g *GitOperations) GetDiff(staged bool) (string, error) {
	logDebug(fmt.Sprintf("Fetching %s diff...", map[bool]string{true: "staged", false: "unstaged"}[staged]))
	args := []string{"diff"}
	if staged {
		args = append(args, "--cached")
	}
	return runCmd("git", args...)
}

func (g *GitOperations) StageAllChanges() error {
	logDebug("Staging all changes (git add .)")
	_, err := runCmd("git", "add", ".")
	return err
}

func (g *GitOperations) GetCurrentBranch() (string, error) {
	logDebug("Getting current branch (git rev-parse --abbrev-ref HEAD)")
	return runCmd("git", "rev-parse", "--abbrev-ref", "HEAD")
}

func (g *GitOperations) GetCommitMessages(mBranch, currentBranch string) (string, error) {
	logDebug(fmt.Sprintf("Getting commit messages between origin/%s..%s", mBranch, currentBranch))
	return runCmd("git", "log",
		fmt.Sprintf("origin/%s..%s", mBranch, currentBranch),
		"--pretty=format:%s",
		"--no-merges")
}

func (g *GitOperations) Fetch(remote, branch string) error {
	logMessage(color.FgCyan, fmt.Sprintf("🔄 Fetching latest from %s/%s...", remote, branch))
	_, err := runCmd("git", "fetch", remote, branch)
	if err != nil {
		return fmt.Errorf("failed to fetch from %s/%s: %w", remote, branch, err)
	}
	logMessage(color.FgGreen, fmt.Sprintf("✅ Successfully fetched latest from %s/%s.", remote, branch))
	return nil
}

func (g *GitOperations) Push(currentBranch, remote string, flags []string) error {
	pushArgs := append([]string{"push", remote, currentBranch}, flags...)
	logDebug(fmt.Sprintf("Executing command: git %s", strings.Join(pushArgs, " ")))
	out, err := runCmd("git", pushArgs...)
	if err != nil {
		logError(fmt.Sprintf("Failed to push changes: %v\nOutput: %s", err, out))
		return fmt.Errorf("failed to push changes: %w", err)
	}
	logMessage(color.FgBlue, "🚀 Changes pushed successfully!")
	return nil
}

func (g *GitOperations) Commit(commitMessage string, flags []string) error {
	commitArgs := append([]string{"commit"}, flags...)
	commitArgs = append(commitArgs, "-m", commitMessage)
	logDebug(fmt.Sprintf("Executing command: git %s", strings.Join(commitArgs, " ")))
	out, err := runCmd("git", commitArgs...)
	if err != nil {
		logError(fmt.Sprintf("Failed to commit changes: %v\nOutput: %s", err, out))
		return fmt.Errorf("failed to commit changes: %w", err)
	}
	logMessage(color.FgGreen, "📝 Changes committed successfully!")
	return nil
}

func (g *GitOperations) HasChanges() (bool, error) {
	stagedDiff, err := g.GetDiff(true)
	if err != nil {
		return false, err
	}
	unstagedDiff, err := g.GetDiff(false)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(stagedDiff) != "" || strings.TrimSpace(unstagedDiff) != "", nil
}

func (g *GitOperations) HasCommitsToPush(mainBranch, currentBranch string) (bool, error) {
	commitMsgs, err := g.GetCommitMessages(mainBranch, currentBranch)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(commitMsgs) != "", nil
}

// streamOutput streams stdout and stderr from an exec.Cmd in real time (for push/commit/stash).
func streamOutput(cmd *exec.Cmd) (string, error) {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	var stdoutBuffer, stderrBuffer bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)

	handleStream := func(reader io.Reader, writer *os.File, buffer *bytes.Buffer) {
		defer wg.Done()
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintln(writer, line)
			buffer.WriteString(line + "\n")
		}
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	go handleStream(stdoutPipe, os.Stdout, &stdoutBuffer)
	go handleStream(stderrPipe, os.Stderr, &stderrBuffer)

	wg.Wait()
	err = cmd.Wait()

	var combinedOutput bytes.Buffer
	if stdoutBuffer.Len() > 0 {
		combinedOutput.Write(stdoutBuffer.Bytes())
	}
	if stderrBuffer.Len() > 0 {
		if stdoutBuffer.Len() > 0 {
			combinedOutput.WriteString("\n")
		}
		combinedOutput.Write(stderrBuffer.Bytes())
	}

	return strings.TrimSpace(combinedOutput.String()), err
}

// runCmd runs a command and streams output if it's a git push, commit, or stash.
func runCmd(name string, args ...string) (string, error) {
	logDebug(fmt.Sprintf("Running command: %s %v", name, args))
	cmd := exec.Command(name, args...)

	// Use real-time output for certain git operations
	if name == "git" && len(args) > 0 {
		switch args[0] {
		case "push", "commit", "stash":
			return streamOutput(cmd)
		}
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// buildInputData organizes data for the AI prompt.
func buildInputData(ticketNumber string, branchName string, prTitle string, commits string, diff string, jiraData string) string {
	return fmt.Sprintf(`INPUT:
JIRA DATA: %s
TICKET NUMBER: %s
BRANCH NAME:   %s
PULL REQUEST TITLE: %s
COMMIT MESSAGES LIST:
%s
GIT DIFFERENCE TO HEAD:
%s
`, jiraData, ticketNumber, branchName, prTitle, commits, diff)
}

// GitAI aggregates AI-based logic & requires a GitOperations instance plus an OpenAI client.
type GitAI struct {
	gitOps       *GitOperations
	openAIClient *openai.Client
}

// GenerateMessage calls OpenAI with a 60-second timeout, returning the best AI choice.
func (g *GitAI) GenerateMessage(systemInstructions, userInstructions, inputData string) (string, error) {
	logDebug("Preparing OpenAI request (no spinner)")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := g.openAIClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       viper.GetString("OPENAI_MODEL"),
		MaxTokens:   viper.GetInt("OPENAI_MAX_TOKENS"),
		Temperature: float32(viper.GetFloat64("OPENAI_TEMPERATURE")),
		TopP:        float32(viper.GetFloat64("OPENAI_TOP_P")),
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemInstructions},
			{Role: openai.ChatMessageRoleUser, Content: userInstructions},
			{Role: openai.ChatMessageRoleUser, Content: inputData},
		},
	})
	if err != nil {
		logError(fmt.Sprintf("OpenAI API request failed: %s", err.Error()))
		return "", GitAIException{"OpenAI API request failed: " + err.Error()}
	}
	if len(resp.Choices) == 0 {
		logError("Received empty message from OpenAI")
		return "", GitAIException{"No response from GPT"}
	}

	logDebug("AI message generated successfully")
	return resp.Choices[0].Message.Content, nil
}

// CheckRepoPermissions ensures the user has write permissions to the repository.
func (g *GitAI) CheckRepoPermissions() error {
	logDebug("Checking repository permissions via gh CLI")
	out, err := runCmd("gh", "repo", "view", "--json", "viewerPermission")
	if err != nil {
		logDebug(out)
		return GitAIException{"Cannot check repository permissions."}
	}
	var resp struct {
		ViewerPermission string `json:"viewerPermission"`
	}
	if unmarshalErr := json.Unmarshal([]byte(out), &resp); unmarshalErr != nil {
		return GitAIException{"Cannot parse GH repo view output: " + unmarshalErr.Error()}
	}
	switch resp.ViewerPermission {
	case "ADMIN", "MAINTAIN", "WRITE":
		return nil
	default:
		return GitAIException{
			"You do not have write permissions to this repository. Permission: " + resp.ViewerPermission,
		}
	}
}

// getJiraDetails queries Jira (if credentials are present) for the specified ticket.
func (g *GitAI) getJiraDetails(ticketID string) string {
	apiKey := viper.GetString("JIRA_API_KEY")
	baseURL := viper.GetString("JIRA_BASE_URL")
	jiraUsername := viper.GetString("JIRA_USERNAME")

	if apiKey == "" || baseURL == "" || jiraUsername == "" {
		logDebug("Jira credentials not set. Skipping Jira lookup.")
		return ""
	}

	tp := jira.BasicAuthTransport{
		Username:  jiraUsername,
		Password:  apiKey,
		Transport: http.DefaultTransport,
	}

	client, err := jira.NewClient(tp.Client(), baseURL)
	if err != nil {
		logError(fmt.Sprintf("Failed to create Jira client: %s", err.Error()))
		return ""
	}

	issue, _, err := client.Issue.Get(ticketID, nil)
	if err != nil {
		logError(fmt.Sprintf("Failed to fetch Jira issue %s: %s", ticketID, err.Error()))
		return ""
	}

	summary := issue.Fields.Summary
	description := issue.Fields.Description

	var relatedIssues []string
	for _, link := range issue.Fields.IssueLinks {
		if link.OutwardIssue != nil {
			relatedIssues = append(relatedIssues,
				fmt.Sprintf("%s (%s)",
					link.OutwardIssue.Key,
					link.OutwardIssue.Fields.Summary,
				),
			)
		}
		if link.InwardIssue != nil {
			relatedIssues = append(relatedIssues,
				fmt.Sprintf("%s (%s)",
					link.InwardIssue.Key,
					link.InwardIssue.Fields.Summary,
				),
			)
		}
	}

	return fmt.Sprintf(`JIRA ISSUE DATA:
 - TICKET ID: %s
 - TICKET SUMMARY: %s
 - TICKET DESCRIPTION: %s
 - TICKET RELATIONS: %v

`, ticketID, summary, description, relatedIssues)
}

// editContentInEditor opens a temporary file in the user's editor with AI-generated content,
// then checks if it was truly saved by comparing mod times.
func (g *GitAI) editContentInEditor(initialContent string) (string, bool) {
	tmpFile, err := ioutil.TempFile("", "gai-*.txt")
	if err != nil {
		logError(fmt.Sprintf("Failed to create temp file: %s", err.Error()))
		return "", false
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(initialContent); err != nil {
		logError(fmt.Sprintf("Failed to write to temp file: %s", err.Error()))
		return "", false
	}
	tmpFile.Close()

	// Get file mod time before opening editor
	infoBefore, _ := os.Stat(tmpFile.Name())
	modTimeBefore := infoBefore.ModTime()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "nano"
	}

	logMessage(color.FgBlue, fmt.Sprintf("✍️ Opening %s editor for final review...", color.New(color.Bold).Sprint(editor)))
	cmd := exec.Command(editor, tmpFile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		logError(fmt.Sprintf("Failed to launch %s: %s", editor, err.Error()))
		return "", false
	}

	infoAfter, _ := os.Stat(tmpFile.Name())
	modTimeAfter := infoAfter.ModTime()

	// If user did not save (file mod time didn't change), we treat it as "no changes => cancel"
	if !modTimeAfter.After(modTimeBefore) {
		logMessage(color.FgYellow, "⚠️ Editor exited without saving. Canceling operation.")
		return "", false
	}

	finalContent, err := ioutil.ReadFile(tmpFile.Name())
	if err != nil {
		logError(fmt.Sprintf("Failed to read updated file: %s", err.Error()))
		return "", false
	}

	trimmedContent := strings.TrimSpace(string(finalContent))
	if trimmedContent == "" {
		logMessage(color.FgYellow, "⚠️ File is empty after save. Canceling operation.")
		return "", false
	}

	// If content is valid, return it
	logDebug("User saved new content. Displaying below.")
	fmt.Println()
	color.New(color.Bold).Println(string(finalContent))
	return string(finalContent), true
}

// generateDiffBasedMessage fetches the diff & optional Jira details, then calls the AI for a message.
func (g *GitAI) generateDiffBasedMessage(staged bool) (string, bool) {
	logDebug("Gathering diff for AI-based message...")
	diff, _ := g.gitOps.GetDiff(staged)
	currentBranch, _ := g.gitOps.GetCurrentBranch()
	ticketNumber := g.detectTicketNumber(currentBranch)

	var jiraInfo string
	if ticketNumber != "NO-TICKET" {
		jiraInfo = g.getJiraDetails(ticketNumber)
	}
	userData := buildInputData(ticketNumber, currentBranch, "", "", diff, jiraInfo)

	aiOutput, err := g.GenerateMessage(
		embeddedSystemInstructions,
		embeddedCommitFormattingInstructions,
		userData,
	)
	if err != nil {
		logError(fmt.Sprintf("OpenAI error: %s", err.Error()))
		return "", false
	}

	logMessage(color.FgCyan, "🔍 Review AI-generated message (editor will open)...")
	edited, saved := g.editContentInEditor(aiOutput)
	return edited, saved
}

// Commit auto-generates a commit message, opens the editor for review, and commits if user saved.
func (g *GitAI) Commit(extraArgs []string) error {
	logMessage(color.FgBlue, "📦 Starting commit process...")

	hasChanges, err := g.gitOps.HasChanges()
	if err != nil {
		return err
	}
	if !hasChanges {
		logMessage(color.FgYellow, "ℹ️ Nothing to commit. Exiting.")
		return nil
	}

	if err := g.stageChangesIfNeeded(); err != nil {
		return err
	}

	finalMessage, ok := g.generateDiffBasedMessage(true)
	if !ok {
		logMessage(color.FgYellow, "🚫 Commit canceled by user.")
		return nil
	}
	return g.gitOps.Commit(finalMessage, extraArgs)
}

func (g *GitAI) stageChangesIfNeeded() error {
	diff, _ := g.gitOps.GetDiff(true)
	if strings.TrimSpace(diff) != "" {
		logMessage(color.FgBlue, "📂 Changes already staged.")
		return nil
	}
	logMessage(color.FgCyan, "🗂️ No changes staged. Automatically staging all...")
	return g.gitOps.StageAllChanges()
}

// Stash auto-generates a stash message, opens the editor for review, and stashes if user saved.
func (g *GitAI) Stash(extraArgs []string) error {
	logMessage(color.FgGreen, "💾 Stashing changes with AI-generated message...")
	message, ok := g.generateDiffBasedMessage(false)
	if !ok {
		logMessage(color.FgYellow, "🚫 Stash canceled by user.")
		return nil
	}
	stashArgs := append([]string{"stash", "push", "-m", message}, extraArgs...)
	logDebug(fmt.Sprintf("Executing command: git %s", strings.Join(stashArgs, " ")))
	out, err := runCmd("git", stashArgs...)
	if err != nil {
		return fmt.Errorf("failed to stash changes: %w\nOutput: %s", err, out)
	}
	logMessage(color.FgGreen, "🗄️ Changes stashed successfully!")
	return nil
}

// Push will ensure commits are present, push to remote, then create or update a PR.
func (g *GitAI) Push(extraArgs []string) error {
	logMessage(color.FgBlue, "🔄 Preparing to push changes...")
	currentBranch, err := g.gitOps.GetCurrentBranch()
	if err != nil {
		return err
	}

	hasCommits, err := g.gitOps.HasCommitsToPush(viper.GetString("MAIN_BRANCH"), currentBranch)
	if err != nil {
		return err
	}
	if !hasCommits {
		logMessage(color.FgYellow, "ℹ️ Nothing to push. Exiting.")
		return nil
	}

	logMessage(color.FgBlue, "⬆️ Pushing changes to remote...")
	if err := g.pushChanges(extraArgs); err != nil {
		return err
	}

	prNumber, err := g.getExistingPRNumber(currentBranch)
	if err != nil {
		return err
	}

	commitMsgs, _ := g.gitOps.GetCommitMessages(viper.GetString("MAIN_BRANCH"), currentBranch)
	diff, _ := g.gitOps.GetDiff(false)
	ticketNumber := g.detectTicketNumber(currentBranch)

	if prNumber != "" {
		logMessage(color.FgCyan, fmt.Sprintf("🔄 Pull request #%s found. Updating body...", color.New(color.Bold).Sprint(prNumber)))
		if err := g.updatePRBody(prNumber, currentBranch, commitMsgs, diff, ticketNumber); err != nil {
			return err
		}
	} else {
		logMessage(color.FgGreen, "🚀 No existing PR found. Creating new PR...")
		g.createNewPR(currentBranch, commitMsgs, diff, ticketNumber)
		prNumber, _ = g.getExistingPRNumber(currentBranch)
	}

	g.openPRInBrowser(prNumber)
	return nil
}

func (g *GitAI) pushChanges(extraArgs []string) error {
	logMessage(color.FgBlue, "🔍 Fetching latest from origin...")
	if err := g.gitOps.Fetch("origin", viper.GetString("MAIN_BRANCH")); err != nil {
		return err
	}
	currentBranch, err := g.gitOps.GetCurrentBranch()
	if err != nil {
		return err
	}
	return g.gitOps.Push(currentBranch, "origin", extraArgs)
}

// getExistingPRNumber returns the PR number for the given branch, if any.
func (g *GitAI) getExistingPRNumber(branch string) (string, error) {
	logDebug(fmt.Sprintf("Listing PRs for branch %s", branch))
	out, err := runCmd("gh", "pr", "list", "--head", branch, "--json", "number")
	if err != nil {
		return "", fmt.Errorf("failed to check existing PRs: %w\n%s", err, out)
	}
	var prList []struct {
		Number int `json:"number"`
	}
	if e := json.Unmarshal([]byte(out), &prList); e != nil {
		return "", fmt.Errorf("failed to parse PR list JSON: %w", e)
	}
	if len(prList) > 0 {
		return fmt.Sprintf("%d", prList[0].Number), nil
	}
	return "", nil
}

// updatePRBody regenerates a PR body with AI, opens it in the editor, and updates the PR if saved.
func (g *GitAI) updatePRBody(prNumber, branch, commitMsgs, diff, ticketNumber string) error {
	jiraData := g.getJiraDetails(ticketNumber)
	prBodyInput := buildInputData(ticketNumber, branch, "", commitMsgs, diff, jiraData)

	prBodyAI, err := g.GenerateMessage(embeddedSystemInstructions, embeddedPRBodyFormattingInstructions, prBodyInput)
	if err != nil {
		return fmt.Errorf("failed generating PR body: %w", err)
	}
	editedBody, savedBody := g.editContentInEditor(prBodyAI)
	if !savedBody {
		return fmt.Errorf("PR update canceled by user (no save)")
	}

	logMessage(color.FgBlue, "📝 Updating PR on GitHub...")
	out, createErr := runCmd("gh", "pr", "edit", prNumber, "--body", editedBody)
	if createErr != nil {
		return fmt.Errorf("failed to update PR: %w\nOutput: %s", createErr, out)
	}
	logMessage(color.FgGreen, "✅ Pull Request updated successfully!")
	return nil
}

// openPRInBrowser attempts to open the PR in a browser, if a PR exists.
func (g *GitAI) openPRInBrowser(prNumber string) {
	if prNumber == "" {
		logMessage(color.FgYellow, "⚠️ No PR number to open in browser.")
		return
	}
	logMessage(color.FgGreen, "🌐 Opening PR in browser...")
	runCmd("gh", "pr", "view", prNumber, "--web")
}

// detectTicketNumber attempts to parse a JIRA-like ticket from the branch name.
func (g *GitAI) detectTicketNumber(branch string) string {
	logDebug(fmt.Sprintf("Detecting JIRA ticket pattern in branch name: %s", branch))
	re := regexp.MustCompile(`[A-Z]+-\d+`)
	match := re.FindString(branch)
	if match != "" {
		return match
	}
	return "NO-TICKET"
}

// createNewPR generates a new PR title & body, opens them for editing, then creates a draft PR on GitHub.
func (g *GitAI) createNewPR(branch, commitMsgs, diff, ticketNumber string) {
	jiraData := g.getJiraDetails(ticketNumber)
	prTitleInput := buildInputData(ticketNumber, branch, "", commitMsgs, diff, jiraData)

	prTitleAI, err := g.GenerateMessage(embeddedSystemInstructions, embeddedPRTitleFormattingInstructions, prTitleInput)
	if err != nil {
		logError(fmt.Sprintf("Failed to generate PR title: %s", err.Error()))
		return
	}
	firstLine := strings.SplitN(prTitleAI, "\n", 2)[0]
	if ticketNumber == "NO-TICKET" {
		firstLine = strings.TrimPrefix(firstLine, "[NO-TICKET] ")
	}
	editedTitle, savedTitle := g.editContentInEditor(firstLine)
	if !savedTitle {
		logMessage(color.FgYellow, "🚫 PR creation canceled (no save on title).")
		return
	}

	prBodyInput := buildInputData(ticketNumber, branch, editedTitle, commitMsgs, diff, jiraData)
	prBodyAI, err := g.GenerateMessage(embeddedSystemInstructions, embeddedPRBodyFormattingInstructions, prBodyInput)
	if err != nil {
		logError(fmt.Sprintf("Failed to generate PR body: %s", err.Error()))
		return
	}
	editedBody, savedBody := g.editContentInEditor(prBodyAI)
	if !savedBody {
		logMessage(color.FgYellow, "🚫 PR creation canceled (no save on body).")
		return
	}

	logMessage(color.FgGreen, "🛠️ Creating a draft Pull Request on GitHub...")
	out, createErr := runCmd("gh", "pr", "create", "--draft", "--title", editedTitle, "--body", editedBody)
	if createErr != nil {
		logError(fmt.Sprintf("Failed to create PR: %s\nOutput: %s", createErr.Error(), out))
		return
	}
	logMessage(color.FgGreen, "🎉 Pull Request created successfully!")
}

// ------------------------------------------------------------------
// Cobra commands & CLI entry point
// ------------------------------------------------------------------

var (
	systemInstructionsContent     string
	prTitleFormattingInstructions string
	prBodyFormattingInstructions  string
	commitFormattingInstructions  string
)

var rootCmd = &cobra.Command{
	Use:   "gai",
	Short: "Git AI Assistant",
	Long:  "Automate Git operations with AI assistance.",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var versionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print the version of gai",
	Aliases: []string{"v"},
	Run: func(cmd *cobra.Command, args []string) {
		color.New(color.FgGreen).Printf("gai version %s\n", Version)
	},
}

var instructionsCmd = &cobra.Command{
	Use:     "instructions",
	Short:   "Displays all loaded instructions",
	Aliases: []string{"i"},
	Run: func(cmd *cobra.Command, args []string) {
		for _, instr := range []struct {
			color   color.Attribute
			title   string
			content string
		}{
			{color.BgGreen, "SYSTEM INSTRUCTIONS", systemInstructionsContent},
			{color.BgBlue, "PULL REQUEST TITLE INSTRUCTIONS", prTitleFormattingInstructions},
			{color.BgRed, "PULL REQUEST BODY INSTRUCTIONS", prBodyFormattingInstructions},
			{color.BgYellow, "COMMIT MESSAGE INSTRUCTIONS", commitFormattingInstructions},
		} {
			color.New(instr.color).Printf("\n# %s\n%s\n", instr.title, instr.content)
		}
	},
}

var commitCmd = &cobra.Command{
	Use:     "commit [-- git commit flags]",
	Short:   "Generate an AI-powered commit message. Pass additional git commit flags after '--'.",
	Long:    "Generates an AI-powered commit message, opens it in your editor for review, and commits if saved.",
	Aliases: []string{"c"},
	RunE: func(cmd *cobra.Command, args []string) error {
		g := mustNewGitAI()
		return g.Commit(args)
	},
}

var pushCmd = &cobra.Command{
	Use:   "push [-- git push flags]",
	Short: "Push changes and create/update a PR. Pass additional git push flags after '--'.",
	Long: `Pushes your changes to the remote repository, then creates or updates a PR if commits exist.

Examples:
  gai push -- --force
  gai push -- --set-upstream origin feature-branch
`,
	Aliases: []string{"p"},
	RunE: func(cmd *cobra.Command, args []string) error {
		g := mustNewGitAI()

		if err := g.CheckRepoPermissions(); err != nil {
			logError(err.Error())
			return err
		}
		return g.Push(args)
	},
}

var stashCmd = &cobra.Command{
	Use:     "stash [-- git stash flags]",
	Short:   "Stash changes with an AI-generated message. Pass additional git stash flags after '--'.",
	Long:    "Generates an AI-based stash message, opens it in your editor for review, and stashes if saved.",
	Aliases: []string{"s"},
	RunE: func(cmd *cobra.Command, args []string) error {
		g := mustNewGitAI()
		return g.Stash(args)
	},
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().BoolP("verbose", "V", false, "Enable verbose output")
	_ = viper.BindPFlag("VERBOSE", rootCmd.PersistentFlags().Lookup("verbose"))

	rootCmd.AddCommand(versionCmd, instructionsCmd, commitCmd, pushCmd, stashCmd)
}

// initConfig loads environment variables and prompt overrides from user config directories.
func initConfig() {
	viper.AutomaticEnv()
	configDir := viper.GetString("GAI_CONFIG_DIR")
	if configDir == "" {
		configDir = os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			configDir = filepath.Join(os.Getenv("HOME"), ".config")
		}
		configDir = filepath.Join(configDir, "gai")
	}

	systemInstructionsContent = loadPrompt(filepath.Join(configDir, "systemInstructions.md"), embeddedSystemInstructions)
	prTitleFormattingInstructions = loadPrompt(filepath.Join(configDir, "prTitleFormattingInstructions.md"), embeddedPRTitleFormattingInstructions)
	prBodyFormattingInstructions = loadPrompt(filepath.Join(configDir, "prBodyFormattingInstructions.md"), embeddedPRBodyFormattingInstructions)
	commitFormattingInstructions = loadPrompt(filepath.Join(configDir, "commitFormattingInstructions.md"), embeddedCommitFormattingInstructions)

	viper.SetDefault("OPENAI_MODEL", "gpt-4o-mini")
	viper.SetDefault("OPENAI_MAX_TOKENS", 16384)
	viper.SetDefault("OPENAI_TEMPERATURE", 0.8)
	viper.SetDefault("OPENAI_TOP_P", 0.9)
	viper.SetDefault("MAIN_BRANCH", "main")
	viper.SetDefault("VERBOSE", false)
	viper.SetDefault("JIRA_BASE_URL", "")
	viper.SetDefault("JIRA_USERNAME", "")
	viper.SetDefault("JIRA_API_KEY", "")
}

// loadPrompt reads a file from path or defaults to embedded content if not found.
func loadPrompt(path, defaultContent string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logDebug(fmt.Sprintf("Prompt file not found at %s. Using default.", path))
		} else {
			logError(fmt.Sprintf("Error reading prompt file at %s: %s. Using default.", path, err.Error()))
		}
		return defaultContent
	}
	logMessage(color.FgCyan, fmt.Sprintf("🔍 Loaded prompt from %s .", color.New(color.Bold).Sprint(path)))
	return string(data)
}

// mustNewGitAI creates a GitAI or exits on fatal error.
func mustNewGitAI() *GitAI {
	apiKey := viper.GetString("OPENAI_API_KEY")
	if apiKey == "" {
		logError("OPENAI_API_KEY environment variable not set")
		os.Exit(1)
	}
	client := openai.NewClient(apiKey)

	if err := checkRequirements(); err != nil {
		logError(err.Error())
		os.Exit(1)
	}
	return &GitAI{
		gitOps:       &GitOperations{},
		openAIClient: client,
	}
}

// checkRequirements ensures user has Git, GitHub CLI, and is authenticated via gh.
func checkRequirements() error {
	logMessage(color.FgCyan, "🔎 Checking system requirements...")
	if _, err := exec.LookPath("git"); err != nil {
		return GitAIException{"Git not found in PATH"}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return GitAIException{"GitHub CLI not found in PATH"}
	}
	out, err := runCmd("gh", "auth", "status")
	if err != nil {
		logDebug(out)
		return GitAIException{"GitHub CLI not authenticated"}
	}
	logMessage(color.FgGreen, "✅ All requirements satisfied!")
	return nil
}

// main is the CLI entry point.
func main() {
	color.New(color.FgMagenta).Printf("%s\n", ASCIIHeader)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
