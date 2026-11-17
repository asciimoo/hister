# How-to contribute

Thanks for your interest in contributing to Hister! This document outlines the guidelines for contributing to this project.

## Development

### Backend (Go)

- Format and lint with: `golangci-lint run --fix ./...`
- Run tests with: `go test ./...`

### Frontend (SvelteKit + Tailwind)

- Format with: `npm run format`
- Check formatting: `npm run format:check`
- Build the app: `npm run build -w @nogon/app`
- Build the website: `npm run build -w @nogon/website`
- Build the extension: `npm run build -w @nogon/ext`

### Full Build

```bash
./manage.sh build   # or: go generate ./... && go build
```

## Code Style

- **Go**: Follow `golangci-lint` v2 rules (config in `.golangci.toml`). Use `goimports` + `gofumpt` formatting.
- **Frontend**: Prettier with single quotes, trailing commas, 100 print width, 2-space tabs. Use Tailwind scale classes, not arbitrary values for standard utilities.
- Check `webui/components/src/lib/components/ui/` for existing components before creating new ones.

### Submitting pull requests

- Make sure all tests pass and linting is clean before submitting
- Write clear commit messages explaining the "why" behind your changes
- Keep pull requests focused on a single concern

Do not take criticism personally. When you get feedback, it is about your work, not your character or personality. Keep in mind we all want to make Hister better.

When something is not clear, please ask questions to clear things up.

If you would like to introduce a big architectural change or do a refactor, either in the codebase or the development tooling, please open an issue with a proposal first. This way we can think together about the problem and perhaps come up with a better solution.

## License

By contributing to Hister, you agree that your contributions will be licensed under the [AGPLv3 License](LICENSE).

## AI Policy

### Restrictions on Generative AI Usage

- **All AI usage in any form must be disclosed.** You must state the tool you used (e.g. Claude Code, Cursor, Amp) along with the extent that the work was AI-assisted.
- **The human-in-the-loop must fully understand all code.** If you use generative AI tools as an aid in developing code or documentation changes, ensure that you fully understand the proposed changes and can explain why they are the correct approach.
- **AI should never be the main author of the PR.** AI may be used as a tool to help with developing, but the human contribution to the code changes should always be reasonably larger than the part written by AI. For example, you should be the one that decides about the structure of the PR, not the LLM.
- **Issues and PR descriptions must be fully human-written.** Do not post output from Large Language Models or similar generative AI as comments on any of our discussion forums, as such comments tend to be formulaic and low content. If you're not a native English speaker, using AI for translating self-written issue texts to English is okay, but please keep the wording as close as possible to the original wording.
- **Bad AI drivers will be denounced.** People who produce bad contributions that are clearly AI (slop) will be blocked for all future contributions.
- **AI should never be used for "good first issues"** The purpose of "good first issues" is to provide a smooth on boarding experience for anyone who would like to be involved in contributing to a project and not to be a low hanging fruit for AI models.

### There are Humans Here

Every discussion, issue, and pull request is read and reviewed by humans. It is a boundary point at which people interact with each other and the work done. It is rude and disrespectful to approach this boundary with low-effort, unqualified work, since it puts the burden of validation on the maintainer.

It takes a lot of maintainer time and energy to review AI-generated contributions! Sending the output of an LLM to open source project maintainers extracts work from them in the form of design and code review, so we call this kind of contribution an "extractive contribution".

The _golden rule_ is that a contribution should be worth more to the project than the time it takes to review it, which is usually not the case if large parts of your PR were written by LLMs.
