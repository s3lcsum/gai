# 🦄 GAI - Git AI Assistant

<div align="center">
    <img src="https://media.giphy.com/media/26AHONQ79FdWZhAI0/giphy.gif" alt="GitAI Banner">

  🌈 Enhance your Git workflow with AI-powered commit messages, PR descriptions, and more! ✨
</div>

## ✨ Features

- 🤖 AI-powered commit message generation
- 🚀 Automated PR creation and updates
- 💾 Smart stash message generation
- 🎨 JIRA ticket detection and integration
- 🌟 Interactive editor support
- 🔄 Seamless GitHub CLI integration

## 🚀 Installation

```bash
go install github.com/s3lcusm/gai@latest
```

### Prerequisites

- Go 1.23 or higher
- Git
- GitHub CLI (`gh`)
- OpenAI API key

## ⚙️ Configuration

1. Set your OpenAI API key:
```bash
export OPENAI_API_KEY='your-api-key'
```

2. Ensure GitHub CLI is authenticated:
```bash
gh auth login
```

## 💫 Usage

| Command | Description | Example |
|---------|-------------|---------|
| `gai commit` | Generate AI-powered commit message | `gai commit -- --amend` |
| `gai push` | Push changes and manage PRs | `gai push -- --force` |
| `gai stash` | Stash with AI-generated message | `gai stash -- --keep-index` |
| `gai version` | Display version | `gai version` |
| `gai instructions` | Show prompt templates | `gai instructions` |

## 🎯 Git Aliases

Supercharge your workflow by adding these aliases to your `.gitconfig`:

```ini
[alias]
  cai = !$GOBIN/gai commit
  pai = !$GOBIN/gai push
  sai = !$GOBIN/gai stash
```

Now you can use:
- `git cai` instead of `gai commit`
- `git pai` instead of `gai push`
- `git sai` instead of `gai stash`

## 🌈 Examples

### 1. Creating a Commit
```bash
# Stage your changes
git add .
# Generate AI-powered commit message
gai commit
```

### 2. Pushing and Creating PR
```bash
# Push changes and create/update PR
gai push
```

### 3. Smart Stashing
```bash
# Stash changes with AI-generated description
gai stash
```

<div align="center">
  <img src="https://user-images.githubusercontent.com/1675298/67339509-4b630880-f4fd-11e9-8891-7a563dfe0182.gif" alt="Rainbow Magic">
</div>

## 🔑 Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `OPENAI_API_KEY` | Your OpenAI API key | Required |
| `OPENAI_MODEL` | OpenAI model to use | `gpt-4o-mini` |
| `OPENAI_MAX_TOKENS` | Maximum tokens for responses | 16384 |
| `OPENAI_TEMPERATURE` | Temperature for responses | 0.0 |
| `MAIN_BRANCH` | Main branch name | `main` |
| `GAI_CONFIG_DIR` | Custom config directory | `~/.config/gai` |

## 🎨 Custom Prompt Templates

You can customize the AI prompts by creating these files in your config directory:

- `systemInstructions.md`
- `prTitleFormattingInstructions.md`
- `prBodyFormattingInstructions.md`
- `commitFormattingInstructions.md`

## 🤝 Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`gai commit`)
4. Push to the branch (`gai push`)
5. Open a Pull Request

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

<div align="center">
  Made with 💖 and AI magic ✨
</div>