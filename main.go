package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
	"github.com/sashabaranov/go-openai"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

/* =======================================
   ==============  CONSTANTS  ============
   ======================================= */

const Version = "1.0.3"

const ASCIIHeader = `
 ▄▄ • ▪  ▄▄▄▄▄ ▄▄▄· ▪  .▄▄ · .▄▄ · ▪  ▄▄▄▄▄
▐█ ▀ ▪██ •██  ▐█ ▀█ ██ ▐█ ▀. ▐█ ▀. ██ •██
▄█ ▀█▄▐█· ▐█.▪▄█▀▀█ ▐█·▄▀▀▀█▄▄▀▀▀█▄▐█· ▐█.▪
▐█▄▪▐█▐█▌ ▐█▌·▐█▪ ▐▌▐█▌▐█▄▪▐█▐█▄▪▐█▐█▌ ▐█▌·
·▀▀▀▀ ▀▀▀ ▀▀▀  ▀  ▀ ▀▀▀ ▀▀▀▀  ▀▀▀▀ ▀▀▀ ▀▀▀

ʕつ•ᴥ•ʔつ Automate Git operations with AI
`

/* =======================================
   =============   LOGGING   =============
   ======================================= */

// Prints debug messages (only when verbose == true)
func logDebug(msg string) {
	if verbose {
		color.New(color.FgMagenta).Fprintf(os.Stderr, "🔬 %s\n", msg)
	}
}

// General-purpose log with caller-defined color and emoji
func logMessage(c color.Attribute, emoji, msg string) {
	color.New(c).Fprintf(os.Stderr, "%s %s\n", emoji, msg)
}

// Error logs in red
func logError(msg string) {
	color.New(color.FgRed).Fprintf(os.Stderr, "❌ %s\n", msg)
}

/* ---------- AI PROMPTS ---------- */

const defaultSystemInstructions = `
You are an expert software developer who helps generate concise, high-quality
Git-related messages. Provide brief, clear outputs with an imperative mood.
Avoid disclaimers, personal references, or mention of AI.
Stay consistent with the style across this repository.

List of must use exactly one of the allowed Gitmojis from this set:
- 🎨 → Improve structure / format of the code.
- ⚡️ → Improve performance
- 🔥 → Remove code or files
- 🐛 → Fix a bug
- 🚑️ → Critical hotfix
- ✨ → Introduce new features
- 📝 → Add or update documentation
- 🚀 → Deploy stuff
- 💄 → Add or update the UI and style files
- 🎉 → Begin a project
- ✅ → Add, update, or pass tests
- 🔒️ → Fix security or privacy issues
- 🔐 → Add or update secrets
- 🔖 → Release / Version tags
- 🚨 → Fix compiler / linter warnings
- 🚧 → Work in progress
- 💚 → Fix CI Build
- ⬇️ → Downgrade dependencies
- ⬆️ → Upgrade dependencies
- 📌 → Pin dependencies to specific versions
- 👷 → Add or update CI build system
- 📈 → Add or update analytics or track code
- ♻️ → Refactor code
- ➕ → Add a dependency
- ➖ → Remove a dependency
- 🔧 → Add or update configuration files
- 🔨 → Add or update development scripts
- 🌐 → Internationalization and localization
- ✏️ → Fix typos
- 💩 → Write bad code that needs to be improved
- ⏪️ → Revert changes
- 🔀 → Merge branches
- 📦️ → Add or update compiled files or packages
- 👽️ → Update code due to external API changes
- 🚚 → Move or rename resources (e.g.: files, paths, routes).
- 💥 → Introduce breaking changes
- 🍱 → Add or update assets
- ♿️ → Improve accessibility
- 💡 → Add or update comments in source code
- 🍻 → Write code drunkenly
- 💬 → Add or update text and literals
- 🗃️ → Perform database related changes
- 🔊 → Add or update logs
- 🔇 → Remove logs
- 👥 → Add or update contributor(s)
- 🚸 → Improve user experience / usability
- 🏗️ → Make architectural changes
- 📱 → Work on responsive design
- 🤡 → Mock things
- 🥚 → Add or update an easter egg
- 🙈 → Add or update a .gitignore file
- 📸 → Add or update snapshots
- ⚗️ → Perform experiments
- 🔍️ → Improve SEO
- 🏷️ → Add or update types
- 🌱 → Add or update seed files
- 🚩 → Add, update, or remove feature flags
- 🥅 → Catch errors
- 💫 → Add or update animations and transitions
- 🗑️ → Deprecate code that needs to be cleaned up
- 🛂 → Work on code related to authorization, roles and permissions
- 🩹 → Simple fix for a non-critical issue
- 🧐 → Data exploration/inspection
- ⚰️ → Remove dead code
- 🧪 → Add a failing test
- 👔 → Add or update business logic
- 🩺 → Add or update healthcheck
- 🧱 → Infrastructure related changes
- 🧑‍💻 → Improve developer experience
- 💸 → Add sponsorships or money related infrastructure
- 🧵 → Add or update code related to multithreading or concurrency
- 🦺 → Add or update code related to validation
`

const defaultPRTitleFormattingInstructions = `
As an expert software developer, generate a **clear and concise** pull request title.
**Requirements:**
- If a JIRA ticket number is provided, place it at the start in brackets.
- Summarize the main purpose.
- Keep under **140 characters**.
- Use **imperative mood** (e.g., "fix bug" instead of "fixed bug").
- Do **not** end with a period.
- Ensure the title is a **complete thought**.
- Follow **consistent style** across all PR titles.
- Exclude disclaimers, personal references, or mentions of AI.
- Align with **repository standards**.

**OUTPUT FORMAT:**
[<ticket number>] <pull request title>
`

const defaultPRBodyFormattingInstructions = `
As an expert software developer, write a **concise and structured** Pull Request body.
**Requirements:**
- Summarize the main purpose in a few sentences using **imperative mood**.
- Provide a **bullet list** of key changes.
- If a JIRA ticket exists, link it under "Ticket links."
- Use **short and direct sentences**.
- Follow **consistent formatting**: exclude disclaimers, personal references, or mentions of AI.

**OUTPUT FORMAT:**
### Description
(Summary of changes in a few sentences)

### Changes
- Bullet points of key changes

### Ticket links (Skip if no JIRA ticket)
- [JIRA-0000]
`

const defaultCommitFormattingInstructions = `
As an expert software developer, generate a **clear and structured** Git commit message following **Conventional Commits**.
**Requirements:**
- Keep the entire line **under 80 characters**.
- Use **imperative mood** (e.g., "add" instead of "added").
- Do **not** end with a period.
- Condense multiple changes into **a single descriptive line** if necessary.
- Exclude disclaimers, personal references, or mentions of AI.
- Output **exactly one line**.

**OUTPUT FORMAT:**
<gitmoji> type: <description>
`

/* =======================================
   =============  GLOBALS   =============
   ======================================= */

var (
	verbose                           bool
	mainBranch                        string
	openAIModel                       string
	openAIMaxTokens                   int
	openAITemperature                 float64
	openAITopP                        float64
	systemInstructionsContent         string
	prTitleFormattingInstructions     string
	prBodyFormattingInstructions      string
	commitFormattingInstructions      string
	configDir                         string
	systemInstructionsPath            string
	prTitleFormattingInstructionsPath string
	prBodyFormattingInstructionsPath  string
	commitFormattingInstructionsPath  string
)

// Simple custom error
type GitAIException struct{ msg string }

func (e GitAIException) Error() string { return e.msg }

/* =======================================
   ===========   GitOperations  ==========
   ======================================= */

type GitOperations struct{}

func (g *GitOperations) GetDiff(staged bool) (string, error) {
	if staged {
		logDebug("Fetching staged diff (git diff --cached)")
		return runCmd("git", "diff", "--cached")
	}
	logDebug("Fetching unstaged diff (git diff)")
	return runCmd("git", "diff")
}

func (g *GitOperations) StageAllChanges() error {
	logDebug("Staging all changes (git add .)")
	_, err := runCmd("git", "add", ".")
	return err
}

func (g *GitOperations) GetCurrentBranch() (string, error) {
	logDebug("Getting current branch (git rev-parse --abbrev-ref HEAD)")
	out, err := runCmd("git", "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out), err
}

func (g *GitOperations) GetCommitMessages(mBranch, currentBranch string) (string, error) {
	logDebug(fmt.Sprintf("Getting commit messages between origin/%s..%s", mBranch, currentBranch))
	return runCmd("git", "log",
		fmt.Sprintf("origin/%s..%s", mBranch, currentBranch),
		"--pretty=format:%s",
		"--no-merges")
}

func (g *GitOperations) GetLastCommitMessage() (string, error) {
	logDebug("Getting last commit message (git log -1 --pretty=format:%s)")
	out, err := runCmd("git", "-1", "--pretty=format:%s")
	return strings.TrimSpace(out), err
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

/* =======================================
   =============    GitAI    =============
   ======================================= */

type GitAI struct {
	gitOps       *GitOperations
	openAIClient *openai.Client
}

/* =======================================
   ===========  UTIL FUNCTIONS  ==========
   ======================================= */

func runCmd(name string, args ...string) (string, error) {
	logDebug(fmt.Sprintf("Running command: %s %v", name, args))
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func performWithSpinner(desc string, fn func() (string, error)) (string, error) {
	s := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	s.Prefix = fmt.Sprintf("%s... ", desc)
	s.Start()
	defer s.Stop()

	out, err := fn()
	return out, err
}

func executeCommandWithCheck(name string, args ...string) {
	logDebug(fmt.Sprintf("executeCommandWithCheck: %s %v", name, args))
	out, err := runCmd(name, args...)
	if err != nil {
		logError(fmt.Sprintf("Command failed: %s\nOutput: %s", err, out))
		os.Exit(1)
	}
}

func buildInputData(ticketNumber, branchName, prTitle, commits, diff string) string {
	return fmt.Sprintf(`INPUT:
TICKET NUMBER: %s
BRANCH NAME:   %s
PULL REQUEST TITLE: %s
COMMIT MESSAGES LIST:
%s
GIT DIFFERENCE TO HEAD:
%s
`, ticketNumber, branchName, prTitle, commits, diff)
}

/* =======================================
   ===========  GitAI METHODS  ===========
   ======================================= */

func (g *GitAI) GenerateMessage(systemInstructions, userInstructions, inputData string) (string, error) {
	logDebug("Preparing OpenAI request")

	var resp openai.ChatCompletionResponse
	_, err := performWithSpinner("🤖 Generating AI message", func() (string, error) {
		r, e := g.openAIClient.CreateChatCompletion(
			context.Background(),
			openai.ChatCompletionRequest{
				Model:       openAIModel,
				MaxTokens:   openAIMaxTokens,
				Temperature: float32(openAITemperature),
				TopP:        float32(openAITopP),
				Messages: []openai.ChatCompletionMessage{
					{Role: openai.ChatMessageRoleSystem, Content: systemInstructions},
					{Role: openai.ChatMessageRoleUser, Content: userInstructions},
					{Role: openai.ChatMessageRoleUser, Content: inputData},
				},
			},
		)
		if e != nil {
			return "", e
		}
		resp = r
		return "", nil
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

// Opens Vim for user to edit generated content
func (g *GitAI) editContentWithVim(initialContent string) (string, bool) {
	logDebug("Creating a temp file for user edit")

	tmpFile, err := os.CreateTemp("", "gai-*.txt")
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

	stat, err := tmpFile.Stat()
	if err != nil {
		logError(fmt.Sprintf("Failed to stat temp file: %s", err.Error()))
		return "", false
	}
	origModTime := stat.ModTime()

	logMessage(color.FgBlue, "✍️", "Opening Vim editor for final review...")
	cmd := exec.Command("vim", tmpFile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logError(fmt.Sprintf("Failed to launch Vim: %s", err.Error()))
		return "", false
	}

	finalBytes, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		logError(fmt.Sprintf("Failed to read updated file: %s", err.Error()))
		return "", false
	}
	finalContent := string(finalBytes)

	statAfter, err := os.Stat(tmpFile.Name())
	if err != nil {
		logError(fmt.Sprintf("Failed to stat temp file after editing: %s", err.Error()))
		return finalContent, false
	}
	if statAfter.ModTime().Equal(origModTime) || strings.TrimSpace(finalContent) == "" {
		logMessage(color.FgYellow, "⚠️", "No changes saved in the editor")
		return finalContent, false
	}

	logDebug("User saved new content. Displaying below.")
	fmt.Println()
	color.New(color.Bold).Println(finalContent) // Bold the user-edited content
	fmt.Println()

	return finalContent, true
}

/* ==========  ACTIONS  ========== */

func (g *GitAI) generateDiffBasedMessage(staged bool) (string, bool) {
	logDebug("Gathering diff for AI-based commit message")
	diff, _ := g.gitOps.GetDiff(staged)
	userData := buildInputData("", "", "", "", diff)

	logDebug("Generating commit message with AI based on diff")
	aiOutput, err := g.GenerateMessage(systemInstructionsContent, commitFormattingInstructions, userData)
	if err != nil {
		logError(fmt.Sprintf("OpenAI error: %s", err.Error()))
		return "", false
	}

	logMessage(color.FgBlue, "🔎", "Review AI-generated commit message (Vim will open)...")
	edited, saved := g.editContentWithVim(aiOutput)
	if !saved {
		return "", false
	}
	return edited, true
}

/* ==========  COMMIT  ========== */

func (g *GitAI) Commit(extraArgs []string) error {
	logMessage(color.FgBlue, "📢", "Starting commit process...")

	// Check if there are any changes to commit
	hasChanges, err := g.gitOps.HasChanges()
	if err != nil {
		logError(fmt.Sprintf("Failed to check for changes: %s", err.Error()))
		return err
	}
	if !hasChanges {
		logMessage(color.FgYellow, "ℹ️", "Nothing to commit. Exiting.")
		return nil
	}

	if err := g.stageChangesIfNeeded(); err != nil {
		return err
	}

	finalMessage, ok := g.generateDiffBasedMessage(true)
	if !ok {
		logMessage(color.FgYellow, "🚫", "Commit canceled by user.")
		return nil
	}
	logDebug("Committing changes with final message")
	return g.executeCommit(finalMessage, extraArgs)
}

func (g *GitAI) stageChangesIfNeeded() error {
	diff, _ := g.gitOps.GetDiff(true)
	if strings.TrimSpace(diff) != "" {
		logMessage(color.FgBlue, "🎁", "Changes already staged.")
		return nil
	}
	logMessage(color.FgCyan, "🎁", "No changes staged. Automatically staging all...")
	if err := g.gitOps.StageAllChanges(); err != nil {
		logError(fmt.Sprintf("Failed to stage changes: %s", err.Error()))
		return err
	}
	return nil
}

func (g *GitAI) executeCommit(finalMessage string, extraArgs []string) error {
	// Initialize commitArgs with the commit command
	commitArgs := []string{"commit"}

	// Append extraArgs to allow overriding or adding flags
	commitArgs = append(commitArgs, extraArgs...)

	// Append the commit message
	commitArgs = append(commitArgs, "-m", finalMessage)

	logDebug(fmt.Sprintf("Executing command: git %s", strings.Join(commitArgs, " ")))

	out, err := runCmd("git", commitArgs...)
	if err != nil {
		logError(fmt.Sprintf("Failed to commit changes: %v\nOutput: %s", err, out))
		return fmt.Errorf("failed to commit changes: %w", err)
	}
	logMessage(color.FgGreen, "🎉", "Changes committed successfully!")
	return nil
}

/* ==========  STASH  ========== */

func (g *GitAI) Stash(extraArgs []string) error {
	logMessage(color.FgBlue, "📢", "Stashing changes with AI message...")
	message, ok := g.generateDiffBasedMessage(false)
	if !ok {
		logMessage(color.FgYellow, "🚫", "Stash canceled by user.")
		return nil
	}

	// Initialize stashArgs with default stash command and message
	stashArgs := []string{"stash", "push", "-m", message}

	// Append extraArgs to allow overriding or adding flags
	stashArgs = append(stashArgs, extraArgs...)

	logDebug(fmt.Sprintf("Executing command: git %s", strings.Join(stashArgs, " ")))

	out, err := runCmd("git", stashArgs...)
	if err != nil {
		logError(fmt.Sprintf("Failed to stash changes: %s\nOutput: %s", err.Error(), out))
		return fmt.Errorf("failed to stash changes: %w", err)
	}
	logMessage(color.FgGreen, "🎉", "Changes stashed successfully!")
	return nil
}

/* ==========  PUSH & PR  ========== */

func (g *GitAI) Push(extraArgs []string) error {
	logMessage(color.FgBlue, "🌐", "Preparing to push changes...")

	currentBranch, err := g.gitOps.GetCurrentBranch()
	if err != nil {
		logError(fmt.Sprintf("Could not get current branch: %s", err.Error()))
		return err
	}
	logDebug(fmt.Sprintf("Current branch: %s", currentBranch))

	// Check if there are commits to push
	hasCommits, err := g.gitOps.HasCommitsToPush(mainBranch, currentBranch)
	if err != nil {
		logError(fmt.Sprintf("Failed to check for commits to push: %s", err.Error()))
		return err
	}
	if !hasCommits {
		logMessage(color.FgYellow, "ℹ️", "Nothing to push. Exiting.")
		return nil
	}

	logMessage(color.FgBlue, "🌐", "Pushing changes to remote...")

	if err := g.pushChanges(extraArgs); err != nil {
		logError(err.Error())
		return err
	}

	logDebug("Checking for existing PR...")
	prNumber, err := g.getExistingPRNumber(currentBranch)
	if err != nil {
		logError(err.Error())
		return err
	}

	commitMsgs, _ := g.gitOps.GetCommitMessages(mainBranch, currentBranch)
	diff, _ := g.gitOps.GetDiff(false)
	ticketNumber := g.detectTicketNumber(currentBranch)

	if prNumber != "" {
		logMessage(color.FgCyan, "📝", fmt.Sprintf("Pull request #%s found. Updating body...", color.New(color.Bold).Sprint(prNumber)))
		if err := g.updatePRBody(prNumber, currentBranch, commitMsgs, diff, ticketNumber); err != nil {
			logError(err.Error())
			return err
		}
	} else {
		logMessage(color.FgBlue, "🚀", "No existing PR found. Creating new PR...")
		g.createNewPR(currentBranch, commitMsgs, diff, ticketNumber)
		prNumber, _ = g.getExistingPRNumber(currentBranch)
	}

	g.openPRInBrowser(prNumber)
	return nil
}

func (g *GitAI) pushChanges(extraArgs []string) error {
	logMessage(color.FgBlue, "🔎", "Fetching latest from origin...")
	if _, err := performWithSpinner("🛰️ Fetching from origin", func() (string, error) {
		return runCmd("git", "fetch", "origin", mainBranch)
	}); err != nil {
		return fmt.Errorf("failed to fetch from origin: %w", err)
	}

	// Determine the current branch internally
	currentBranch, err := g.gitOps.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}
	logDebug(fmt.Sprintf("Current branch: %s", currentBranch))

	// Check if '--set-upstream' is already present in extraArgs to prevent duplication
	setUpstreamPresent := false
	for _, arg := range extraArgs {
		if arg == "--set-upstream" || arg == "-u" {
			setUpstreamPresent = true
			break
		}
		if strings.HasPrefix(arg, "--set-upstream=") {
			setUpstreamPresent = true
			break
		}
	}

	// Initialize pushArgs with 'push' command
	pushArgs := []string{"push"}

	// Append default '--set-upstream origin {branch}' if not present
	if !setUpstreamPresent {
		pushArgs = append(pushArgs, "--set-upstream", "origin", currentBranch)
	}

	// Append extraArgs provided by the user
	pushArgs = append(pushArgs, extraArgs...)

	logDebug(fmt.Sprintf("Executing command: git %s", strings.Join(pushArgs, " ")))

	// Execute the git push command with the constructed arguments
	pushOutput, pushErr := performWithSpinner("🚀 Pushing changes", func() (string, error) {
		return runCmd("git", pushArgs...)
	})
	if pushErr != nil {
		return fmt.Errorf("failed to push changes:\n%s", pushOutput)
	}

	logMessage(color.FgGreen, "🎉", "Changes pushed successfully!")
	return nil
}

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

func (g *GitAI) updatePRBody(prNumber, branch, commitMsgs, diff, ticketNumber string) error {
	logDebug("Building input data for PR body update")
	prBodyInput := buildInputData(ticketNumber, branch, "", commitMsgs, diff)

	logDebug("Generating new PR body with AI")
	prBodyAI, err := g.GenerateMessage(systemInstructionsContent, prBodyFormattingInstructions, prBodyInput)
	if err != nil {
		return fmt.Errorf("failed generating PR body: %w", err)
	}

	editedBody, savedBody := g.editContentWithVim(prBodyAI)
	if !savedBody {
		return fmt.Errorf("PR update canceled")
	}

	logMessage(color.FgBlue, "📢", "Updating PR on GitHub...")
	out, createErr := runCmd("gh", "pr", "edit", prNumber, "--body", editedBody)
	if createErr != nil {
		return fmt.Errorf("failed to update PR: %w\nOutput: %s", createErr, out)
	}
	logMessage(color.FgGreen, "🎉", "Pull Request updated successfully!")
	return nil
}

func (g *GitAI) openPRInBrowser(prNumber string) {
	if prNumber == "" {
		logMessage(color.FgYellow, "⚠️", "No PR number to open in browser.")
		return
	}
	logMessage(color.FgCyan, "🌐", "Opening PR in browser...")
	_, _ = runCmd("gh", "pr", "view", prNumber, "--web")
}

func (g *GitAI) detectTicketNumber(branch string) string {
	logDebug(fmt.Sprintf("Detecting JIRA ticket pattern in branch name: %s", branch))
	re := regexp.MustCompile(`[A-Z]+-\d+`)
	match := re.FindString(branch)
	if match != "" {
		return match
	}
	return "NO-TICKET"
}

func (g *GitAI) createNewPR(branch, commitMsgs, diff, ticketNumber string) {
	logDebug("Generating PR title")
	prTitleInput := buildInputData(ticketNumber, branch, "", commitMsgs, diff)

	prTitleAI, err := g.GenerateMessage(systemInstructionsContent, prTitleFormattingInstructions, prTitleInput)
	if err != nil {
		logError(fmt.Sprintf("Failed to generate PR title: %s", err.Error()))
		return
	}
	firstLine := strings.SplitN(prTitleAI, "\n", 2)[0]
	if ticketNumber == "NO-TICKET" {
		firstLine = strings.TrimPrefix(firstLine, "[NO-TICKET] ")
	}

	editedTitle, savedTitle := g.editContentWithVim(firstLine)
	if !savedTitle {
		logMessage(color.FgYellow, "🚫", "PR creation canceled (no save on title).")
		return
	}

	logDebug("Generating PR body")
	prBodyInput := buildInputData(ticketNumber, branch, editedTitle, commitMsgs, diff)
	prBodyAI, err := g.GenerateMessage(systemInstructionsContent, prBodyFormattingInstructions, prBodyInput)
	if err != nil {
		logError(fmt.Sprintf("Failed to generate PR body: %s", err.Error()))
		return
	}

	editedBody, savedBody := g.editContentWithVim(prBodyAI)
	if !savedBody {
		logMessage(color.FgYellow, "🚫", "PR creation canceled (no save on body).")
		return
	}

	logMessage(color.FgBlue, "📢", "Creating a draft Pull Request on GitHub...")
	out, createErr := runCmd("gh", "pr", "create", "--draft", "--title", editedTitle, "--body", editedBody)
	if createErr != nil {
		logError(fmt.Sprintf("Failed to create PR: %s\nOutput: %s", createErr.Error(), out))
		return
	}
	logMessage(color.FgGreen, "🎉", "Pull Request created successfully!")
}

/* =======================================
   ===========   CLI & SETUP   ===========
   ======================================= */

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
	Long:    "This command prints all loaded instructions in a nicely formatted way to help users understand the available automation steps.",
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
	Use:   "commit [-- git commit flags]",
	Short: "Generate an AI-powered commit message. Pass additional git commit flags after '--'.",
	Long: `The commit command generates an AI-powered commit message and allows you to pass additional flags directly to git commit.

Usage:
  gai commit [flags] [-- git commit flags]

Examples:
  gai commit -- --amend --force --root
  gai commit -- -v
`,
	Aliases: []string{"c"},
	RunE: func(cmd *cobra.Command, args []string) error {
		g := mustNewGitAI()
		return g.Commit(args)
	},
}

var pushCmd = &cobra.Command{
	Use:   "push [-- git push flags]",
	Short: "Push changes and create/update a PR. Pass additional git push flags after '--'.",
	Long: `The push command pushes your changes to the remote repository and manages pull requests.

Usage:
  gai push [flags] [-- git push flags]

Examples:
  gai push -- --force
  gai push -- --set-upstream origin feature-branch
`,
	Aliases: []string{"p"},
	RunE: func(cmd *cobra.Command, args []string) error {
		g := mustNewGitAI()
		return g.Push(args)
	},
}

var stashCmd = &cobra.Command{
	Use:   "stash [-- git stash flags]",
	Short: "Stash changes with an AI-generated message. Pass additional git stash flags after '--'.",
	Long: `The stash command stashes your changes with an AI-generated message and allows you to pass additional flags directly to git stash.

Usage:
  gai stash [flags] [-- git stash flags]

Examples:
  gai stash -- --keep-index
  gai stash -- -u
`,
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

func initConfig() {
	viper.AutomaticEnv()

	// Determine configuration directory
	configDir = viper.GetString("GAI_CONFIG_DIR")
	if configDir == "" {
		configDir = os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			configDir = filepath.Join(os.Getenv("HOME"), ".config")
		}
		configDir = filepath.Join(configDir, "gai")
	}

	// Define paths to prompt templates
	systemInstructionsPath = filepath.Join(configDir, "systemInstructions.md")
	prTitleFormattingInstructionsPath = filepath.Join(configDir, "prTitleFormattingInstructions.md")
	prBodyFormattingInstructionsPath = filepath.Join(configDir, "prBodyFormattingInstructions.md")
	commitFormattingInstructionsPath = filepath.Join(configDir, "commitFormattingInstructions.md")

	// Load prompts from files or use defaults
	systemInstructionsContent = loadPrompt(systemInstructionsPath, defaultSystemInstructions)
	prTitleFormattingInstructions = loadPrompt(prTitleFormattingInstructionsPath, defaultPRTitleFormattingInstructions)
	prBodyFormattingInstructions = loadPrompt(prBodyFormattingInstructionsPath, defaultPRBodyFormattingInstructions)
	commitFormattingInstructions = loadPrompt(commitFormattingInstructionsPath, defaultCommitFormattingInstructions)

	// Default configuration
	viper.SetDefault("OPENAI_MODEL", "gpt-4o-mini")
	viper.SetDefault("OPENAI_MAX_TOKENS", 16384)
	viper.SetDefault("OPENAI_TEMPERATURE", 0.0)
	viper.SetDefault("OPENAI_TOP_P", 1.0)
	viper.SetDefault("MAIN_BRANCH", "main")
	viper.SetDefault("VERBOSE", false)

	verbose = viper.GetBool("VERBOSE")
	mainBranch = viper.GetString("MAIN_BRANCH")
	openAIModel = viper.GetString("OPENAI_MODEL")
	openAIMaxTokens = viper.GetInt("OPENAI_MAX_TOKENS")
	openAITemperature = viper.GetFloat64("OPENAI_TEMPERATURE")
	openAITopP = viper.GetFloat64("OPENAI_TOP_P")
}

// loadPrompt attempts to read a prompt from the given path.
// If the file does not exist or an error occurs, it returns the default content.
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
	logMessage(color.FgCyan, "🔬", fmt.Sprintf("Loaded prompt from %s", color.New(color.Bold).Sprint(path)))
	return string(data)
}

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

func checkRequirements() error {
	logMessage(color.FgBlue, "🔎", "Checking system requirements...")

	if _, err := exec.LookPath("git"); err != nil {
		return GitAIException{"Git not in PATH"}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return GitAIException{"GitHub CLI not in PATH"}
	}

	out, err := runCmd("gh", "auth", "status")
	if err != nil {
		logDebug(out)
		return GitAIException{"GitHub CLI not authenticated"}
	}

	if err := checkRepoPermissions(); err != nil {
		return err
	}

	logMessage(color.FgGreen, "👍", "All requirements satisfied!")
	return nil
}

func checkRepoPermissions() error {
	logDebug("Checking repo permissions via gh CLI")
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

func main() {
	color.New(color.FgMagenta).Printf("%s\n", ASCIIHeader)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
