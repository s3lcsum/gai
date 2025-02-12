package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

const ASCIIHeader = `
 ▄▄ • ▪  ▄▄▄▄▄ ▄▄▄· ▪  .▄▄ · .▄▄ · ▪  ▄▄▄▄▄
▐█ ▀ ▪██ •██  ▐█ ▀█ ██ ▐█ ▀. ▐█ ▀. ██ •██
▄█ ▀█▄▐█· ▐█.▪▄█▀▀█ ▐█·▄▀▀▀█▄▄▀▀▀█▄▐█· ▐█.▪
▐█▄▪▐█▐█▌ ▐█▌·▐█▪ ▐▌▐█▌▐█▄▪▐█▐█▄▪▐█▐█▌ ▐█▌·
·▀▀▀▀ ▀▀▀ ▀▀▀  ▀  ▀ ▀▀▀ ▀▀▀▀  ▀▀▀▀ ▀▀▀ ▀▀▀

ʕつ•ᴥ•ʔつ Automate Git operations with AI
`

const systemPrompt = `
You are an expert software developer who helps generate concise, high-quality
Git-related messages. Provide brief, clear outputs with an imperative mood.
Avoid disclaimers and references to yourself or your system role.
Focus on clarity, correctness, and best practices.
`

const prTitleFormattingInstructions = `
As an expert software developer, generate a clear pull request title based on my changes
Requirements:
- If JIRA ticket number is provided, place it at the start in brackets
- Summarize the main purpose
- Keep under 140 characters
- Use imperative mood
- Do not end with a period
- Must be a complete thought

OUTPUT FORMAT:
[<ticket number>] <pull request title>
`

const prBodyFormattingInstructions = `
As an expert software developer, write a simple summary based on changes in this branch based on the following title, branch name, commit messages and diffs.
Requirements:
- Summarize the main purpose in a few sentences
- Use imperative mood
- Include a bullet list of key changes
- If a JIRA ticket is present, link it under "Ticket links"
- Keep sentences short

OUTPUT FORMAT:
### Description
(Summary of changes in a few sentences)

### Changes
* Bullet points of key changes

### Ticket links // Skip if no JIRA Ticket found
* [JIRA-0000]
`

const commitFormattingInstructions = `
As an expert developer, generate a Git commit message following Conventional Commits:
Requirements:
- Start with a gitmoji
- Output exactly one line
- Keep the entire line under 80 characters
- Use imperative mood (e.g., "add" not "added")
- Do not end with a period
- Format: <gitmoji> [type]: <description>
- If multiple changes exist, condense them into a single descriptive line
- Output a single commit message only

OUTPUT FORMAT:
<gitmoji> [type]: <description>
`

func buildInputData(ticketNumber string, branchName string, prTitle string, commits string, diff string) string {
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
   ==============  GLOBALS   =============
   ======================================= */

var (
	verbose           bool
	mainBranch        string
	openAIModel       string
	openAIMaxTokens   int
	openAITemperature float64
)

/* =======================================
   ===========  LOGGING & ERRORS =========
   ======================================= */

// Minimal logging with a single function for color + emoji.
func logMsg(c color.Attribute, emoji, text string) {
	color.New(c).Fprintf(os.Stderr, "%s %s\n", emoji, text)
}

// For debug logs
func logDebug(msg string) {
	if !verbose {
		return
	}
	color.New(color.FgYellow, color.Bold).Fprintf(os.Stderr, "🐛 DEBUG: %s\n", msg)
}

// Custom error type
type GitAIException struct{ msg string }

func (e GitAIException) Error() string { return e.msg }

/* =======================================
   ===========   GitOperations  ==========
   ======================================= */

type GitOperations struct{}

func (g *GitOperations) GetDiff(staged bool) (string, error) {
	if staged {
		return runCmd("git", "diff", "--cached")
	}
	return runCmd("git", "diff")
}

func (g *GitOperations) StageAllChanges() error {
	_, err := runCmd("git", "add", ".")
	return err
}

func (g *GitOperations) GetCurrentBranch() (string, error) {
	out, err := runCmd("git", "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out), err
}

func (g *GitOperations) GetCommitMessages(mBranch, currentBranch string) (string, error) {
	return runCmd("git", "log",
		fmt.Sprintf("origin/%s..%s", mBranch, currentBranch),
		"--pretty=format:%s",
		"--no-merges")
}

func (g *GitOperations) GetLastCommitMessage() (string, error) {
	out, err := runCmd("git", "log", "-1", "--pretty=format:%s")
	return strings.TrimSpace(out), err
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
	logDebug(fmt.Sprintf("Running command: %s %v", name, args))
	out, err := runCmd(name, args...)
	if err != nil {
		logMsg(color.FgRed, "❌", fmt.Sprintf("Command failed: %s\nOutput: %s", err, out))
		os.Exit(1)
	}
}

/* =======================================
   ===========  GitAI METHODS  ===========
   ======================================= */

// GenerateMessage calls the OpenAI API to get a response for a given prompt.
func (g *GitAI) GenerateMessage(systemInstructions, userInstructions, inputData string) (string, error) {
	if verbose {
		logDebug("System instructions:\n" + systemInstructions)
		logDebug("User instructions:\n" + userInstructions)
		logDebug("User data:\n" + inputData)
	}

	var resp openai.ChatCompletionResponse
	_, err := performWithSpinner("🤖 Generating AI message", func() (string, error) {
		r, e := g.openAIClient.CreateChatCompletion(
			context.Background(),
			openai.ChatCompletionRequest{
				Model:       openAIModel,
				MaxTokens:   openAIMaxTokens,
				Temperature: float32(openAITemperature),
				Messages: []openai.ChatCompletionMessage{
					{
						Role:    openai.ChatMessageRoleSystem,
						Content: systemInstructions,
					},
					{
						Role:    openai.ChatMessageRoleUser,
						Content: userInstructions,
					},
					{
						Role:    openai.ChatMessageRoleUser,
						Content: inputData,
					},
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
		return "", GitAIException{"OpenAI API request failed: " + err.Error()}
	}
	if len(resp.Choices) == 0 {
		return "", GitAIException{"Received empty message from GPT"}
	}
	return resp.Choices[0].Message.Content, nil
}

// editContentWithVim opens the user's text editor to confirm or modify AI output.
func (g *GitAI) editContentWithVim(initialContent string) (string, bool) {
	tmpFile, err := os.CreateTemp("", "gai-*.txt")
	if err != nil {
		logMsg(color.FgRed, "❌", "Failed to create temp file: "+err.Error())
		return "", false
	}
	defer os.Remove(tmpFile.Name())

	_, _ = tmpFile.WriteString(initialContent)
	tmpFile.Close()

	stat, _ := os.Stat(tmpFile.Name())
	origModTime := stat.ModTime()

	cmd := exec.Command("vim", tmpFile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logMsg(color.FgRed, "❌", "Failed to launch vim editor: "+err.Error())
		return "", false
	}

	finalBytes, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		logMsg(color.FgRed, "❌", "Failed to read updated file: "+err.Error())
		return "", false
	}
	finalContent := string(finalBytes)
	fmt.Print("\n" + finalContent + "\n\n")

	statAfter, _ := os.Stat(tmpFile.Name())
	if statAfter.ModTime().Equal(origModTime) || strings.TrimSpace(finalContent) == "" {
		return finalContent, false
	}
	return finalContent, true
}

/* =======================================
   ============    ACTIONS    ============
   ======================================= */

// Generate a diff-based AI message using the given template.
func (g *GitAI) generateDiffBasedMessage(staged bool) (string, bool) {
	diff, _ := g.gitOps.GetDiff(staged)

	// userData might be your actual diff or a combined string with context
	userData := buildInputData("", "", "", "", diff)

	aiOutput, err := g.GenerateMessage(
		systemPrompt,
		commitFormattingInstructions,
		userData,
	)
	if err != nil {
		logMsg(color.FgRed, "❌", "OpenAI error: "+err.Error())
		return "", false
	}

	// Only take the first line from the AI output...
	// firstLine := strings.SplitN(aiOutput, "\n", 2)[0]
	edited, saved := g.editContentWithVim(aiOutput)
	if !saved {
		return "", false
	}
	return edited, true
}

/* ==========  COMMIT  ========== */

func (g *GitAI) Commit(amend bool) {
	logMsg(color.FgBlue, "📝", "Preparing commit...")

	if err := g.stageChangesIfNeeded(); err != nil {
		return
	}
	if amend {
		if msg, err := g.gitOps.GetLastCommitMessage(); err == nil {
			logMsg(color.FgCyan, "💡", "Amending commit: "+msg)
		}
	}

	finalMessage, ok := g.generateDiffBasedMessage(true)
	if !ok {
		logMsg(color.FgYellow, "⚠️", "Commit canceled.")
		return
	}
	g.executeCommit(finalMessage, amend)
	g.showGitStatusAfter("Commit")
}

func (g *GitAI) stageChangesIfNeeded() error {
	diff, _ := g.gitOps.GetDiff(true)
	if strings.TrimSpace(diff) != "" {
		logMsg(color.FgBlue, "🔎", "Changes already staged.")
		return nil
	}
	logMsg(color.FgBlue, "🌱", "No changes staged. Automatically staging all...")
	if err := g.gitOps.StageAllChanges(); err != nil {
		logMsg(color.FgRed, "❌", "Failed to stage changes: "+err.Error())
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
		logMsg(color.FgRed, "❌", fmt.Sprintf("Failed to commit changes: %v\nOutput: %s", err, out))
		os.Exit(1)
	}
	logMsg(color.FgGreen, "🎉", "Changes committed successfully!")
}

func (g *GitAI) showGitStatusAfter(action string) {
	logMsg(color.FgBlue, "🔎", fmt.Sprintf("Showing Git status after %s:", action))
	executeCommandWithCheck("git", "status")
}

/* ==========  STASH  ========== */

func (g *GitAI) Stash() {
	logMsg(color.FgBlue, "🗄", "Collecting changes for stash...")
	message, ok := g.generateDiffBasedMessage(false)
	if !ok {
		logMsg(color.FgYellow, "⚠️", "Stash operation canceled (no save).")
		return
	}
	out, err := runCmd("git", "stash", "push", "-m", message)
	if err != nil {
		logMsg(color.FgRed, "❌", "Failed to stash changes: "+err.Error()+"\nOutput: "+out)
		os.Exit(1)
	}
	logMsg(color.FgGreen, "📦", "Changes stashed successfully!")
}

/* ==========  PUSH & PR  ========== */

func (g *GitAI) Push() {
	logMsg(color.FgBlue, "🚀", "Pushing changes...")

	currentBranch, err := g.gitOps.GetCurrentBranch()
	if err != nil {
		logMsg(color.FgRed, "❌", "Could not get current branch: "+err.Error())
		return
	}

	// Push changes as before
	if err := g.pushChanges(currentBranch); err != nil {
		logMsg(color.FgRed, "❌", err.Error())
		return
	}
	g.showGitStatusAfter("Push")

	// Check for existing PR
	prNumber, err := g.getExistingPRNumber(currentBranch)
	if err != nil {
		logMsg(color.FgRed, "❌", err.Error())
		return
	}

	commitMsgs, _ := g.gitOps.GetCommitMessages(mainBranch, currentBranch)
	diff, _ := g.gitOps.GetDiff(false)
	ticketNumber := g.detectTicketNumber(currentBranch)

	if prNumber != "" {
		// Update existing PR body
		logMsg(color.FgBlue, "⚙️", fmt.Sprintf("Pull request #%s already exists", prNumber))
		if err := g.updatePRBody(prNumber, currentBranch, commitMsgs, diff, ticketNumber); err != nil {
			logMsg(color.FgRed, "❌", err.Error())
			return
		}
	} else {
		// Create a new PR
		g.createNewPR(currentBranch, commitMsgs, diff, ticketNumber)

		// After creation, fetch the new PR number
		prNumber, _ = g.getExistingPRNumber(currentBranch)
	}

	// Automatically open PR in browser
	g.openPRInBrowser(prNumber)
}

func (g *GitAI) pushChanges(branch string) error {
	logMsg(color.FgBlue, "🔄", "Fetching latest changes from origin...")
	_, err := performWithSpinner("📡 Fetching from origin", func() (string, error) {
		return runCmd("git", "fetch", "origin", mainBranch)
	})
	if err != nil {
		return fmt.Errorf("Failed to fetch from origin: %w", err)
	}

	logMsg(color.FgBlue, "🔼", "Pushing changes to remote...")
	pushOutput, pushErr := performWithSpinner("🚀 Pushing changes", func() (string, error) {
		return runCmd("git", "push", "--set-upstream", "origin", branch)
	})
	if pushErr != nil {
		return fmt.Errorf("Failed to push changes:\n%s", pushOutput)
	}
	logMsg(color.FgGreen, "🎉", "Changes pushed successfully!")
	return nil
}

// getExistingPRNumber retrieves the PR number if it exists for the current branch.
func (g *GitAI) getExistingPRNumber(branch string) (string, error) {
	out, err := runCmd("gh", "pr", "list", "--head", branch, "--json", "number")
	if err != nil {
		return "", fmt.Errorf("failed to check existing PRs: %w\n%s", err, out)
	}
	var prList []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(out), &prList); err != nil {
		return "", fmt.Errorf("failed to parse PR list JSON: %w", err)
	}
	if len(prList) > 0 {
		// Return the first match (usually there's only one open PR per branch)
		return fmt.Sprintf("%d", prList[0].Number), nil
	}
	return "", nil
}

// updatePRBody updates the body of an existing PR
func (g *GitAI) updatePRBody(prNumber, branch, commitMsgs, diff, ticketNumber string) error {
	logMsg(color.FgBlue, "⚙️", "Updating existing Pull Request body with AI assistance...")

	// Next, build body data
	prBodyInput := buildInputData(ticketNumber, branch, "", commitMsgs, diff)

	// Generate the PR body with the new method
	prBodyAI, err := g.GenerateMessage(
		systemPrompt,                 // system-level instructions
		prBodyFormattingInstructions, // user instructions for body
		prBodyInput,                  // the actual data
	)
	if err != nil {
		return fmt.Errorf("failed generating PR body: %w", err)
	}

	editedBody, savedBody := g.editContentWithVim(prBodyAI)
	if !savedBody {
		return fmt.Errorf("PR update canceled by user")
	}

	logMsg(color.FgBlue, "🏗️", "Editing Pull Request on GitHub...")
	out, createErr := runCmd("gh", "pr", "edit", prNumber, "--body", editedBody)
	if createErr != nil {
		return fmt.Errorf("failed to update PR: %w\nOutput: %s", createErr, out)
	}
	logMsg(color.FgGreen, "🎉", "Pull Request updated successfully!")
	return nil
}

func (g *GitAI) openPRInBrowser(prNumber string) {
	if prNumber == "" {
		return
	}
	// Opens the PR in the default browser
	_, _ = runCmd("gh", "pr", "view", prNumber, "--web")
}

func (g *GitAI) detectTicketNumber(branch string) string {
	re := regexp.MustCompile(`[A-Z]+-\d+`)
	match := re.FindString(branch)
	if match != "" {
		return match
	}
	return "NO-TICKET"
}

func (g *GitAI) createNewPR(branch, commitMsgs, diff, ticketNumber string) {
	// Build the user data for PR title
	prTitleInput := buildInputData(ticketNumber, branch, "", commitMsgs, diff)

	// Call your updated GenerateMessage
	prTitleAI, err := g.GenerateMessage(
		systemPrompt,                  // system-level instructions
		prTitleFormattingInstructions, // user instructions for title
		prTitleInput,                  // the actual data
	)
	if err != nil {
		logMsg(color.FgRed, "❌", "Failed to generate PR title: "+err.Error())
		return
	}

	// Take only the first line if needed
	firstLine := strings.SplitN(prTitleAI, "\n", 2)[0]

	if ticketNumber == "NO-TICKET" {
		firstLine = strings.TrimPrefix(firstLine, "[NO-TICKET] ")
	}

	editedTitle, savedTitle := g.editContentWithVim(firstLine)
	if !savedTitle {
		logMsg(color.FgYellow, "⚠️", "PR creation canceled (no save on title).")
		return
	}

	// Next, build body data
	prBodyInput := buildInputData(ticketNumber, branch, editedTitle, commitMsgs, diff)

	// Generate the PR body with the new method
	prBodyAI, err := g.GenerateMessage(
		systemPrompt,                 // system-level instructions
		prBodyFormattingInstructions, // user instructions for body
		prBodyInput,                  // the actual data
	)
	if err != nil {
		logMsg(color.FgRed, "❌", "Failed to generate PR body: "+err.Error())
		return
	}

	// Let user edit the AI-generated body
	editedBody, savedBody := g.editContentWithVim(prBodyAI)
	if !savedBody {
		logMsg(color.FgYellow, "⚠️", "PR creation canceled (no save on body).")
		return
	}

	logMsg(color.FgBlue, "🏗️", "Creating draft Pull Request on GitHub...")
	out, createErr := runCmd("gh", "pr", "create", "--draft", "--title", editedTitle, "--body", editedBody)
	if createErr != nil {
		logMsg(color.FgRed, "❌", "Failed to create PR: "+createErr.Error()+"\nOutput: "+out)
		return
	}
	logMsg(color.FgGreen, "🎉", "Pull Request created successfully!")
}

/* ==========  SHOW STATUS  ========== */

func (g *GitAI) ShowStatus() {
	logMsg(color.FgBlue, "🔍", "Git Status:")
	executeCommandWithCheck("git", "status")
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

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Generate an AI-powered commit message",
	Run: func(cmd *cobra.Command, args []string) {
		amend, _ := cmd.Flags().GetBool("amend")
		g := mustNewGitAI()
		g.Commit(amend)
	},
}

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push changes and create a PR",
	Run: func(cmd *cobra.Command, args []string) {
		g := mustNewGitAI()
		g.Push()
	},
}

var stashCmd = &cobra.Command{
	Use:   "stash",
	Short: "Stash changes with an AI-generated message",
	Run: func(cmd *cobra.Command, args []string) {
		g := mustNewGitAI()
		g.Stash()
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Git status",
	Run: func(cmd *cobra.Command, args []string) {
		g := mustNewGitAI()
		g.ShowStatus()
	},
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	_ = viper.BindPFlag("VERBOSE", rootCmd.PersistentFlags().Lookup("verbose"))

	commitCmd.Flags().Bool("amend", false, "Amend the last commit")
	_ = viper.BindPFlag("AMEND", commitCmd.Flags().Lookup("amend"))

	rootCmd.AddCommand(commitCmd, pushCmd, stashCmd, statusCmd)
}

func initConfig() {
	viper.AutomaticEnv()

	// Defaults
	viper.SetDefault("OPENAI_MODEL", "gpt-4o-mini")
	viper.SetDefault("OPENAI_MAX_TOKENS", 16384)
	viper.SetDefault("OPENAI_TEMPERATURE", 1)
	viper.SetDefault("MAIN_BRANCH", "main")
	viper.SetDefault("VERBOSE", false)

	verbose = viper.GetBool("VERBOSE")
	mainBranch = viper.GetString("MAIN_BRANCH")
	openAIModel = viper.GetString("OPENAI_MODEL")
	openAIMaxTokens = viper.GetInt("OPENAI_MAX_TOKENS")
	openAITemperature = viper.GetFloat64("OPENAI_TEMPERATURE")
}

func mustNewGitAI() *GitAI {
	apiKey := viper.GetString("OPENAI_API_KEY")
	if apiKey == "" {
		logMsg(color.FgRed, "❌", "OPENAI_API_KEY environment variable not set")
		os.Exit(1)
	}
	client := openai.NewClient(apiKey)

	if err := checkRequirements(); err != nil {
		logMsg(color.FgRed, "❌", err.Error())
		os.Exit(1)
	}
	return &GitAI{
		gitOps:       &GitOperations{},
		openAIClient: client,
	}
}

// checkRequirements ensures git + gh exist, user is authenticated, and has repo perms.
func checkRequirements() error {
	logMsg(color.FgBlue, "🔍", "Checking system requirements...")

	// check 'git'
	if _, err := exec.LookPath("git"); err != nil {
		return GitAIException{"Git not in PATH"}
	}

	// check 'gh'
	if _, err := exec.LookPath("gh"); err != nil {
		return GitAIException{"GitHub CLI not in PATH"}
	}

	// check gh auth
	out, err := runCmd("gh", "auth", "status")
	if err != nil {
		logDebug(out)
		return GitAIException{"GitHub CLI not authenticated"}
	}

	// check user permission on current repo
	if err := checkRepoPermissions(); err != nil {
		return err
	}

	logMsg(color.FgGreen, "⚙️", "All requirements satisfied!")
	return nil
}

// checkRepoPermissions ensures the user has write/maintain/admin on this repo.
func checkRepoPermissions() error {
	out, err := runCmd("gh", "repo", "view", "--json", "viewerPermission")
	if err != nil {
		logDebug(out)
		return GitAIException{"Cannot check repository permissions. Possibly no permissions."}
	}
	var resp struct {
		ViewerPermission string `json:"viewerPermission"`
	}
	if unmarshalErr := json.Unmarshal([]byte(out), &resp); unmarshalErr != nil {
		return GitAIException{"Cannot parse GH repo view output: " + unmarshalErr.Error()}
	}

	switch resp.ViewerPermission {
	case "ADMIN", "MAINTAIN", "WRITE":
		// valid
	default:
		return GitAIException{
			"You do not have write permissions to this repository. Permission: " + resp.ViewerPermission,
		}
	}
	return nil
}

func main() {
	logMsg(color.FgMagenta, "", ASCIIHeader)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
