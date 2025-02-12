# 🦄 GitAI: The Ultimate AI-Powered Git Assistant 🚀

![GitAI Banner](https://media.giphy.com/media/26AHONQ79FdWZhAI0/giphy.gif)

## 🌟 What is GitAI?
**GitAI** is your AI-powered Git assistant, helping you generate commit messages, stash descriptions, pull request titles, and PR bodies effortlessly! No more struggling to craft meaningful commit messages—let AI do the heavy lifting! 💡🤖

## ✨ Features
- 📝 **AI-Generated Commit Messages**: Follow **Conventional Commits** with **gitmojis** 🎨.
- 🎭 **Smart Stash Descriptions**: Never forget what you stashed!
- 📜 **Pull Request Magic**: Auto-generate **PR titles and bodies** based on your code changes.
- 🕵️ **Intelligent Ticket Detection**: Automatically includes JIRA tickets if present.
- 🔄 **Amend Existing Commits**: Easily update previous commits with AI suggestions.
- 🌈 **Super Nerdy Logging**: Debug mode gives you **extra fancy details**. 🧐

## 🛠 Installation

### Prerequisites
- 🐙 **Git**: You know, version control?
- 🦊 **GitHub CLI (`gh`)**: For seamless PR creation.
- 🧠 **OpenAI API Key**: Because AI needs a brain.

### Install GitAI
```sh
# Clone the repo
$ git clone https://github.com/your-repo/gitai.git && cd gitai

# Build the binary
$ go build -o gai main.go

# Move to your bin folder (optional)
$ mv gai /usr/local/bin/
```

## 🚀 Usage

### 📝 AI-Powered Commit Messages
```sh
$ gai commit
```
💡 **Pro Tip:** Want to amend the last commit?
```sh
$ gai commit --amend
```

### 📦 Stash with AI Magic
```sh
$ gai stash
```

### 🚀 Push & Create a PR in One Step
```sh
$ gai push
```

### 🔍 Check Git Status
```sh
$ gai status
```

## ⚙️ Configuration
GitAI uses **environment variables** for configuration:

```sh
export OPENAI_API_KEY="your-secret-key"
export MAIN_BRANCH="main"
export VERBOSE=true  # If you love nerdy details
```

## 💻 How It Works
1. **Extracts Git Diff** 📜
2. **Generates a structured AI prompt** 🏗️
3. **Sends request to OpenAI's API** 🚀
4. **Returns a clean, nerd-approved message** 🤓
5. **Lets you edit it (optional, because you're still the boss)** ✍️
6. **Applies the commit/stash/PR action** ✅

## 🧪 Example AI-Generated Commit Messages
💡 Before: _"Fix stuff"_

🚀 **After AI:**
```sh
✨ feat[database]: Optimize query performance
🔧 fix[auth]: Patch security vulnerability in login flow
📚 docs[README]: Update installation steps
```

## 🔥 Roadmap
- [ ] 🔥 Support for more AI models (Claude, Gemini, Llama?)
- [ ] 🏗️ Interactive AI assistant for resolving merge conflicts
- [ ] 🛠️ Plugin system for custom commit styles
- [ ] 🐙 Bitbucket & GitLab support

## 🦸‍♂️ Contributing
We welcome PRs, issues, and feature requests! Join our nerdy crew and make Git commits fun again! 🎉

## 💖 Acknowledgments
Special thanks to:
- 🧑‍💻 [OpenAI](https://openai.com/) for making AI commits possible
- 🦄 Unicorn developers everywhere 🦄

## ⚡ License
MIT License - because AI should be free! 🚀

