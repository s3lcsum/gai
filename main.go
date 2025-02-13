package main

import (
	"context"
	_ "embed"
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

const Version = "1.0.4"

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

func logDebug(msg string) {
	if viper.GetBool("VERBOSE") {
		color.New(color.FgMagenta).Fprintf(os.Stderr, "🔬 %s\n", msg)
	}
}

func logMessage(c color.Attribute, msg string) {
	color.New(c).Fprintln(os.Stderr, msg)
}

func logError(msg string) {
	color.New(color.FgRed).Fprintf(os.Stderr, "❌ %s\n", msg)
}

type GitAIException struct{ msg string }

func (e GitAIException) Error() string { return e.msg }

type GitOperations struct{}

func (g *GitOperations) GetDiff(staged bool) (string, error) {
	logDebug(fmt.Sprintf("Fetching %s diff (git diff %s)",
		map[bool]string{true: "staged", false: "unstaged"}[staged],
		map[bool]string{true: "--cached", false: ""}[staged]))
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
		logError(fmt.Sprintf("Failed to fetch from %s/%s: %v", remote, branch, err))
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
	return fn()
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

type GitAI struct {
	gitOps       *GitOperations
	openAIClient *openai.Client
}

func (g *GitAI) GenerateMessage(systemInstructions, userInstructions, inputData string) (string, error) {
	logDebug("Preparing OpenAI request")
	var resp openai.ChatCompletionResponse
	_, err := performWithSpinner("🤖 Generating AI message", func() (string, error) {
		r, e := g.openAIClient.CreateChatCompletion(
			context.Background(),
			openai.ChatCompletionRequest{
				Model:       viper.GetString("OPENAI_MODEL"),
				MaxTokens:   viper.GetInt("OPENAI_MAX_TOKENS"),
				Temperature: float32(viper.GetFloat64("OPENAI_TEMPERATURE")),
				TopP:        float32(viper.GetFloat64("OPENAI_TOP_P")),
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

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vim"
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

	finalContent, err := ioutil.ReadFile(tmpFile.Name())
	if err != nil {
		logError(fmt.Sprintf("Failed to read updated file: %s", err.Error()))
		return "", false
	}

	if strings.TrimSpace(string(finalContent)) == "" {
		logMessage(color.FgYellow, "⚠️ No changes saved in the editor")
		return string(finalContent), false
	}

	logDebug("User saved new content. Displaying below.")
	fmt.Println()
	color.New(color.Bold).Println(string(finalContent))
	fmt.Println()

	return string(finalContent), true
}

func (g *GitAI) generateDiffBasedMessage(staged bool) (string, bool) {
	logDebug("Gathering diff for AI-based message")
	diff, _ := g.gitOps.GetDiff(staged)
	userData := buildInputData("", "", "", "", diff)
	logDebug("Generating message with AI based on diff")
	aiOutput, err := g.GenerateMessage(embeddedSystemInstructions, embeddedCommitFormattingInstructions, userData)
	if err != nil {
		logError(fmt.Sprintf("OpenAI error: %s", err.Error()))
		return "", false
	}
	logMessage(color.FgCyan, "🔍 Review AI-generated message (Vim will open)...")
	edited, saved := g.editContentInEditor(aiOutput)
	return edited, saved
}

func (g *GitAI) Commit(extraArgs []string) error {
	logMessage(color.FgBlue, "📦 Starting commit process...")
	hasChanges, err := g.gitOps.HasChanges()
	if err != nil {
		logError(fmt.Sprintf("Failed to check for changes: %s", err.Error()))
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
	logDebug("Committing changes with final message")
	return g.gitOps.Commit(finalMessage, extraArgs)
}

func (g *GitAI) stageChangesIfNeeded() error {
	diff, _ := g.gitOps.GetDiff(true)
	if strings.TrimSpace(diff) != "" {
		logMessage(color.FgBlue, "📂 Changes already staged.")
		return nil
	}
	logMessage(color.FgCyan, "🗂️ No changes staged. Automatically staging all...")
	if err := g.gitOps.StageAllChanges(); err != nil {
		logError(fmt.Sprintf("Failed to stage changes: %s", err.Error()))
		return err
	}
	return nil
}

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
		logError(fmt.Sprintf("Failed to stash changes: %s\nOutput: %s", err.Error(), out))
		return fmt.Errorf("failed to stash changes: %w", err)
	}
	logMessage(color.FgGreen, "🗄️ Changes stashed successfully!")
	return nil
}

func (g *GitAI) Push(extraArgs []string) error {
	logMessage(color.FgBlue, "🔄 Preparing to push changes...")
	currentBranch, err := g.gitOps.GetCurrentBranch()
	if err != nil {
		logError(fmt.Sprintf("Could not get current branch: %s", err.Error()))
		return err
	}
	logDebug(fmt.Sprintf("Current branch: %s", currentBranch))
	hasCommits, err := g.gitOps.HasCommitsToPush(viper.GetString("MAIN_BRANCH"), currentBranch)
	if err != nil {
		logError(fmt.Sprintf("Failed to check for commits to push: %s", err.Error()))
		return err
	}
	if !hasCommits {
		logMessage(color.FgYellow, "ℹ️ Nothing to push. Exiting.")
		return nil
	}
	logMessage(color.FgBlue, "⬆️ Pushing changes to remote...")
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
	commitMsgs, _ := g.gitOps.GetCommitMessages(viper.GetString("MAIN_BRANCH"), currentBranch)
	diff, _ := g.gitOps.GetDiff(false)
	ticketNumber := g.detectTicketNumber(currentBranch)
	if prNumber != "" {
		logMessage(color.FgCyan, fmt.Sprintf("🔄 Pull request #%s found. Updating body...", color.New(color.Bold).Sprint(prNumber)))
		if err := g.updatePRBody(prNumber, currentBranch, commitMsgs, diff, ticketNumber); err != nil {
			logError(err.Error())
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
		return fmt.Errorf("failed to get current branch: %w", err)
	}
	logDebug(fmt.Sprintf("Current branch: %s", currentBranch))
	return g.gitOps.Push(currentBranch, "origin", extraArgs)
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
	prBodyAI, err := g.GenerateMessage(embeddedSystemInstructions, embeddedPRBodyFormattingInstructions, prBodyInput)
	if err != nil {
		return fmt.Errorf("failed generating PR body: %w", err)
	}
	editedBody, savedBody := g.editContentInEditor(prBodyAI)
	if !savedBody {
		return fmt.Errorf("PR update canceled")
	}
	logMessage(color.FgBlue, "📝 Updating PR on GitHub...")
	out, createErr := runCmd("gh", "pr", "edit", prNumber, "--body", editedBody)
	if createErr != nil {
		return fmt.Errorf("failed to update PR: %w\nOutput: %s", createErr, out)
	}
	logMessage(color.FgGreen, "✅ Pull Request updated successfully!")
	return nil
}

func (g *GitAI) openPRInBrowser(prNumber string) {
	if prNumber == "" {
		logMessage(color.FgYellow, "⚠️ No PR number to open in browser.")
		return
	}
	logMessage(color.FgGreen, "🌐 Opening PR in browser...")
	runCmd("gh", "pr", "view", prNumber, "--web")
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
	logDebug("Generating PR body")
	prBodyInput := buildInputData(ticketNumber, branch, editedTitle, commitMsgs, diff)
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

		if err := g.CheckRepoPermissions(); err != nil {
			logError(err.Error())
			return err
		}

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
	viper.SetDefault("OPENAI_TEMPERATURE", 0.0)
	viper.SetDefault("OPENAI_TOP_P", 1.0)
	viper.SetDefault("MAIN_BRANCH", "main")
	viper.SetDefault("VERBOSE", false)
}

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
	logMessage(color.FgCyan, fmt.Sprintf("🔍 Loaded prompt from %s", color.New(color.Bold).Sprint(path)))
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
	// Removed checkRepoPermissions from here
	logMessage(color.FgGreen, "✅ All requirements satisfied!")
	return nil
}

func checkRepoPermissions() error {
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

func main() {
	color.New(color.FgMagenta).Printf("%s\n", ASCIIHeader)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
