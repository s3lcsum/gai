package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
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

const Version = "1.0.2"

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

// System-level instructions
const defaultSystemInstructions = `
You are an expert software developer who helps generate concise, high-quality
Git-related messages. Provide brief, clear outputs with an imperative mood.
Avoid disclaimers, personal references, or mention of AI.
Stay consistent with the style across this repository.

List of must use exactly one of the allowed Gitmojis from this set:
- 🎨 → Improve structure / format of the code."
- ⚡️ → Improve performance"
- 🔥 → Remove code or files"
- 🐛 → Fix a bug"
- 🚑️ → Critical hotfix"
- ✨ → Introduce new features"
- 📝 → Add or update documentation"
- 🚀 → Deploy stuff"
- 💄 → Add or update the UI and style files"
- 🎉 → Begin a project"
- ✅ → Add, update, or pass tests"
- 🔒️ → Fix security or privacy issues"
- 🔐 → Add or update secrets"
- 🔖 → Release / Version tags"
- 🚨 → Fix compiler / linter warnings"
- 🚧 → Work in progress"
- 💚 → Fix CI Build"
- ⬇️ → Downgrade dependencies"
- ⬆️ → Upgrade dependencies"
- 📌 → Pin dependencies to specific versions"
- 👷 → Add or update CI build system"
- 📈 → Add or update analytics or track code"
- ♻️ → Refactor code"
- ➕ → Add a dependency"
- ➖ → Remove a dependency"
- 🔧 → Add or update configuration files"
- 🔨 → Add or update development scripts"
- 🌐 → Internationalization and localization"
- ✏️ → Fix typos"
- 💩 → Write bad code that needs to be improved"
- ⏪️ → Revert changes"
- 🔀 → Merge branches"
- 📦️ → Add or update compiled files or packages"
- 👽️ → Update code due to external API changes"
- 🚚 → Move or rename resources (e.g.: files, paths, routes)."
- 💥 → Introduce breaking changes"
- 🍱 → Add or update assets"
- ♿️ → Improve accessibility"
- 💡 → Add or update comments in source code"
- 🍻 → Write code drunkenly"
- 💬 → Add or update text and literals"
- 🗃️ → Perform database related changes"
- 🔊 → Add or update logs"
- 🔇 → Remove logs"
- 👥 → Add or update contributor(s)"
- 🚸 → Improve user experience / usability"
- 🏗️ → Make architectural changes"
- 📱 → Work on responsive design"
- 🤡 → Mock things"
- 🥚 → Add or update an easter egg"
- 🙈 → Add or update a .gitignore file"
- 📸 → Add or update snapshots"
- ⚗️ → Perform experiments"
- 🔍️ → Improve SEO"
- 🏷️ → Add or update types"
- 🌱 → Add or update seed files"
- 🚩 → Add, update, or remove feature flags"
- 🥅 → Catch errors"
- 💫 → Add or update animations and transitions"
- 🗑️ → Deprecate code that needs to be cleaned up"
- 🛂 → Work on code related to authorization, roles and permissions"
- 🩹 → Simple fix for a non-critical issue"
- 🧐 → Data exploration/inspection"
- ⚰️ → Remove dead code"
- 🧪 → Add a failing test"
- 👔 → Add or update business logic"
- 🩺 → Add or update healthcheck"
- 🧱 → Infrastructure related changes"
- 🧑‍💻 → Improve developer experience"
- 💸 → Add sponsorships or money related infrastructure"
- 🧵 → Add or update code related to multithreading or concurrency"
- 🦺 → Add or update code related to validation"
`

// Pull Request title instructions
const defaultPRTitleFormattingInstructions = `
As an expert software developer, generate a clear pull request title. Requirements:
- If JIRA ticket number is provided, place it at the start in brackets
- Summarize the main purpose
- Keep under 140 characters
- Use imperative mood
- Do not end with a period
- Must be a complete thought
- Maintain consistency across all PR titles
- Do not add disclaimers or AI references
- Keep style aligned with repository standards

OUTPUT FORMAT:
[<ticket number>] <pull request title>
`

// Pull Request body instructions
const defaultPRBodyFormattingInstructions = `
As an expert software developer, write a concise Pull Request body. Requirements:
- Summarize the main purpose in a few sentences, imperative mood
- Include a bullet list of key changes
- If a JIRA ticket is present, link it under "Ticket links"
- Keep sentences short
- Maintain style consistency: do not add disclaimers or AI references

OUTPUT FORMAT:
### Description
(Summary of changes in a few sentences)

### Changes
* Bullet points of key changes

### Ticket links // Skip if no JIRA Ticket found
* [JIRA-0000]
`

// Commit message instructions
const defaultCommitFormattingInstructions = `
As an expert developer, generate a Git commit message following Conventional Commits:
Requirements:
- Use the format: <gitmoji> [type]: <description>
- The entire line must stay under 80 characters
- Use imperative mood (e.g., "add" not "added")
- Do not end with a period
- Condense multiple changes into a single descriptive line if needed
- Do not add disclaimers or references to yourself or AI
- Output exactly one line

OUTPUT FORMAT:
<gitmoji> [type]: <description>
`

/* =======================================
   ==============  GLOBALS   =============
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
	out, err := runCmd("git", "log", "-1", "--pretty=format:%s")
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
	return string(out), err
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
	input := fmt.Sprintf(`INPUT:
TICKET NUMBER: %s
BRANCH NAME:   %s
PULL REQUEST TITLE: %s
COMMIT MESSAGES LIST:
%s
GIT DIFFERENCE TO HEAD:
%s
`, ticketNumber, branchName, prTitle, commits, diff)
	return input
}

/* =======================================
   ===========  GitAI METHODS  ===========
   ======================================= */

func (g *GitAI) GenerateMessage(systemInstructions string, userInstructions string, inputData string) (string, error) {
	logDebug("Preparing OpenAI request")
	logDebug(fmt.Sprintf("System instructions:\n%s", systemInstructions))
	logDebug(fmt.Sprintf("User instructions:\n%s", userInstructions))
	logDebug(fmt.Sprintf("User data:\n%s", inputData))

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

	_, _ = tmpFile.WriteString(initialContent)
	tmpFile.Close()

	stat, _ := os.Stat(tmpFile.Name())
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

	statAfter, _ := os.Stat(tmpFile.Name())
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

func (g *GitAI) Commit(amend bool) {
	logMessage(color.FgBlue, "📢", "Starting commit process...")

	// Check if there are any changes to commit
	hasChanges, err := g.gitOps.HasChanges()
	if err != nil {
		logError(fmt.Sprintf("Failed to check for changes: %s", err.Error()))
		return
	}
	if !hasChanges {
		logMessage(color.FgYellow, "ℹ️", "Nothing to commit. Exiting.")
		return
	}

	if err := g.stageChangesIfNeeded(); err != nil {
		return
	}

	if amend {
		if msg, err := g.gitOps.GetLastCommitMessage(); err == nil {
			logMessage(color.FgCyan, "ℹ️", fmt.Sprintf("Amending last commit: %s", msg))
		}
	}

	finalMessage, ok := g.generateDiffBasedMessage(true)
	if !ok {
		logMessage(color.FgYellow, "🚫", "Commit canceled by user.")
		return
	}
	logDebug("Committing changes with final message")
	g.executeCommit(finalMessage, amend)
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

func (g *GitAI) executeCommit(finalMessage string, amend bool) {
	args := []string{"commit"}
	if amend {
		args = append(args, "--amend")
	}
	args = append(args, "-m", finalMessage)

	out, err := runCmd("git", args...)
	if err != nil {
		logError(fmt.Sprintf("Failed to commit changes: %v\nOutput: %s", err, out))
		os.Exit(1)
	}
	logMessage(color.FgGreen, "🎉", "Changes committed successfully!")
}

/* ==========  STASH  ========== */

func (g *GitAI) Stash() {
	logMessage(color.FgBlue, "📢", "Stashing changes with AI message...")
	message, ok := g.generateDiffBasedMessage(false)
	if !ok {
		logMessage(color.FgYellow, "🚫", "Stash canceled by user.")
		return
	}
	out, err := runCmd("git", "stash", "push", "-m", message)
	if err != nil {
		logError(fmt.Sprintf("Failed to stash changes: %s\nOutput: %s", err.Error(), out))
		os.Exit(1)
	}
	logMessage(color.FgGreen, "🎉", "Changes stashed successfully!")
}

/* ==========  PUSH & PR  ========== */

func (g *GitAI) Push() {
	logMessage(color.FgBlue, "🌐", "Preparing to push changes...")

	currentBranch, err := g.gitOps.GetCurrentBranch()
	if err != nil {
		logError(fmt.Sprintf("Could not get current branch: %s", err.Error()))
		return
	}
	logDebug(fmt.Sprintf("Current branch: %s", currentBranch))

	// Check if there are commits to push
	hasCommits, err := g.gitOps.HasCommitsToPush(mainBranch, currentBranch)
	if err != nil {
		logError(fmt.Sprintf("Failed to check for commits to push: %s", err.Error()))
		return
	}
	if !hasCommits {
		logMessage(color.FgYellow, "ℹ️", "Nothing to push. Exiting.")
		return
	}

	logMessage(color.FgBlue, "🌐", "Pushing changes to remote...")

	if err := g.pushChanges(currentBranch); err != nil {
		logError(err.Error())
		return
	}

	logDebug("Checking for existing PR...")
	prNumber, err := g.getExistingPRNumber(currentBranch)
	if err != nil {
		logError(err.Error())
		return
	}

	commitMsgs, _ := g.gitOps.GetCommitMessages(mainBranch, currentBranch)
	diff, _ := g.gitOps.GetDiff(false)
	ticketNumber := g.detectTicketNumber(currentBranch)

	if prNumber != "" {
		logMessage(color.FgCyan, "📝", fmt.Sprintf("Pull request #%s found. Updating body...", prNumber))
		if err := g.updatePRBody(prNumber, currentBranch, commitMsgs, diff, ticketNumber); err != nil {
			logError(err.Error())
			return
		}
	} else {
		logMessage(color.FgBlue, "🚀", "No existing PR found. Creating new PR...")
		g.createNewPR(currentBranch, commitMsgs, diff, ticketNumber)
		prNumber, _ = g.getExistingPRNumber(currentBranch)
	}

	g.openPRInBrowser(prNumber)
}

func (g *GitAI) pushChanges(branch string) error {
	logMessage(color.FgBlue, "🔎", "Fetching latest from origin...")
	_, err := performWithSpinner("🛰️ Fetching from origin", func() (string, error) {
		return runCmd("git", "fetch", "origin", mainBranch)
	})
	if err != nil {
		return fmt.Errorf("Failed to fetch from origin: %w", err)
	}

	logMessage(color.FgBlue, "🌐", "Pushing changes...")
	pushOutput, pushErr := performWithSpinner("🚀 Pushing changes", func() (string, error) {
		return runCmd("git", "push", "--set-upstream", "origin", branch)
	})
	if pushErr != nil {
		return fmt.Errorf("Failed to push changes:\n%s", pushOutput)
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

var commitCmd = &cobra.Command{
	Use:     "commit",
	Short:   "Generate an AI-powered commit message",
	Aliases: []string{"c"},
	Run: func(cmd *cobra.Command, args []string) {
		amend, _ := cmd.Flags().GetBool("amend")
		g := mustNewGitAI()
		g.Commit(amend)
	},
}

var pushCmd = &cobra.Command{
	Use:     "push",
	Short:   "Push changes and create/update a PR",
	Aliases: []string{"p"},
	Run: func(cmd *cobra.Command, args []string) {
		g := mustNewGitAI()
		g.Push()
	},
}

var stashCmd = &cobra.Command{
	Use:     "stash",
	Short:   "Stash changes with an AI-generated message",
	Aliases: []string{"s"},
	Run: func(cmd *cobra.Command, args []string) {
		g := mustNewGitAI()
		g.Stash()
	},
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().BoolP("verbose", "V", false, "Enable verbose output")
	_ = viper.BindPFlag("VERBOSE", rootCmd.PersistentFlags().Lookup("verbose"))

	commitCmd.Flags().Bool("amend", false, "Amend the last commit")
	_ = viper.BindPFlag("AMEND", commitCmd.Flags().Lookup("amend"))

	rootCmd.AddCommand(versionCmd, commitCmd, pushCmd, stashCmd)
}

func initConfig() {
	viper.AutomaticEnv()

	// Determine configuration directory
	configDir = os.Getenv("GAI_CONFIG")
	if configDir == "" {
		xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfigHome == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				logError(fmt.Sprintf("Unable to determine home directory: %s", err.Error()))
				os.Exit(1)
			}
			configDir = filepath.Join(homeDir, ".config", "gai")
		} else {
			configDir = filepath.Join(xdgConfigHome, "gai")
		}
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
	viper.SetDefault("OPENAI_TEMPERATURE", 1.5)
	viper.SetDefault("OPENAI_TEMPERATURE", 0.0)
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
	data, err := ioutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logDebug(fmt.Sprintf("Prompt file not found at %s. Using default.", path))
		} else {
			logError(fmt.Sprintf("Error reading prompt file at %s: %s. Using default.", path, err.Error()))
		}
		return defaultContent
	}
	logMessage(color.BgHiMagenta, "🔬", fmt.Sprintf("Loaded prompt from %s", path))
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
