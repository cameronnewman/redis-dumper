# Contributing to Redis Dumper

Thank you for your interest in contributing to Redis Dumper! This document provides guidelines and instructions for contributing to the project.

## Getting Started

### Prerequisites

- Go 1.21 or higher
- Docker (for running tests and linting)
- Redis server (for local testing)
- Make

### Setting Up Your Development Environment

1. Fork the repository on GitHub
2. Clone your fork locally:
   ```bash
   git clone https://github.com/YOUR_USERNAME/redis-dumper.git
   cd redis-dumper
   ```

3. Add the upstream repository:
   ```bash
   git remote add upstream https://github.com/cameronnewman/redis-dumper.git
   ```

4. Install dependencies:
   ```bash
   go mod download
   ```

## Development Workflow

### Running Locally

1. Start a local Redis instance:
   ```bash
   docker run -d --name redis-test -p 6379:6379 redis:latest
   ```

2. Build and run the dumper:
   ```bash
   go run ./cmd/dumper --redis-url redis://localhost:6379 --output-dir ./test-export
   ```

3. Run with environment variables:
   ```bash
   export REDIS_URL=redis://localhost:6379
   export OUTPUT_DIR=./test-export
   export OUTPUT_FORMAT=parquet
   go run ./cmd/dumper
   ```

### Making Changes

1. Create a new branch for your feature or bugfix:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes and ensure they follow the coding standards

3. Write or update tests as needed

4. Run the test suite:
   ```bash
   make go-test
   ```

5. Format your code:
   ```bash
   make go-fmt
   ```

6. Run the linter:
   ```bash
   make go-lint
   ```

### Testing

#### Unit Tests

Run all tests:
```bash
make go-test
```

Run tests for a specific package:
```bash
go test ./internal/exporter -v
```

Run tests with coverage:
```bash
go test -cover ./...
```

#### Integration Testing

Test with a real Redis instance:
```bash
# Start Redis with sample data
docker run -d --name redis-test -p 6379:6379 redis:latest
docker exec -it redis-test redis-cli SET key1 "value1"
docker exec -it redis-test redis-cli HSET user:1 name "John" email "john@example.com"
docker exec -it redis-test redis-cli SADD tags "golang" "redis" "database"

# Run the dumper
go run ./cmd/dumper --redis-url redis://localhost:6379 --output-dir ./test-export

# Verify output with DuckDB
duckdb -c "SELECT * FROM read_csv('./test-export/**/*.csv');"
```

## Code Style

### Go Code

- Follow standard Go conventions and idioms
- Use `gofmt` to format your code (automated with `make go-fmt`)
- Ensure all exported functions and types have comments
- Keep functions small and focused
- Use meaningful variable and function names

### Error Handling

- Always check and handle errors appropriately
- Wrap errors with context using `fmt.Errorf`:
  ```go
  if err != nil {
      return fmt.Errorf("failed to connect to Redis: %w", err)
  }
  ```

### Testing

- Write table-driven tests where appropriate
- Test both success and failure cases
- Use meaningful test names that describe what is being tested
- Mock external dependencies when necessary

## Submitting Changes

### Pull Request Process

1. Ensure your branch is up to date with upstream main:
   ```bash
   git fetch upstream
   git rebase upstream/main
   ```

2. Push your changes to your fork:
   ```bash
   git push origin feature/your-feature-name
   ```

3. Create a pull request on GitHub with:
   - A clear title and description
   - Reference to any related issues
   - Summary of the changes made
   - Any breaking changes or migration notes

4. Ensure all CI checks pass

5. Wait for code review and address any feedback

### Pull Request Guidelines

- Keep PRs focused on a single feature or fix
- Include tests for new functionality
- Update documentation as needed
- Add entries to the CHANGELOG if applicable
- Ensure the PR description clearly explains the what and why

## Reporting Issues

### Bug Reports

When reporting bugs, please include:

- Go version (`go version`)
- Redis version
- Operating system and version
- Steps to reproduce the issue
- Expected behavior
- Actual behavior
- Any error messages or logs

### Feature Requests

For feature requests, please describe:

- The use case for the feature
- Expected behavior
- Any implementation ideas (optional)
- How it would benefit other users

## Community

### Code of Conduct

- Be respectful and inclusive
- Welcome newcomers and help them get started
- Focus on constructive criticism
- Assume good intentions

### Getting Help

- Open an issue for bugs or feature requests
- Use discussions for questions and general help
- Check existing issues before creating new ones

## Release Process

Releases are managed by maintainers and follow semantic versioning:

- MAJOR version for incompatible API changes
- MINOR version for new functionality in a backward-compatible manner
- PATCH version for backward-compatible bug fixes

## Additional Resources

- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Effective Go](https://golang.org/doc/effective_go.html)
- [Go Test Patterns](https://github.com/golang/go/wiki/TestComments)

Thank you for contributing to Redis Dumper!