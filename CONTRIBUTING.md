# Contributing to Homebridge Captain's Log

Thank you for your interest in contributing! Issues and PRs are welcome.

## Getting Started

1. Fork the repository
2. Clone your fork locally
3. Create a feature branch: `git checkout -b feature/your-feature-name`

## Development Setup

```bash
# Install dependencies and build tools
make deps

# Build the project
make build

# Run quality checks
make quality

# Run tests
make test
```

## Making Changes

- Follow the existing code style and conventions
- Add tests for new functionality
- Run `make quality` before committing to ensure code quality
- Update documentation as needed

## Submitting Changes

1. Ensure all tests pass: `make test`
2. Run quality checks: `make quality`
3. Commit your changes with a clear commit message
4. Push to your fork
5. Create a pull request

## Code Quality

This project uses comprehensive quality checks including:
- Code formatting (`gofmt`)
- Static analysis (`go vet`, `staticcheck`)
- Linting (`revive`)
- Security scanning (`gosec`)
- Vulnerability checking (`govulncheck`)

All checks must pass before changes can be merged.

## Issues

When reporting issues, please include:
- Go version
- Operating system
- Steps to reproduce the issue
- Expected vs actual behavior
- Relevant log output (use `-d` flag for debug output)
