# AGENTS.md

## Introduction
Welcome! This file provides guidelines and commands for agentic coding assistants working on the DiskCount project. DiskCount is a Telegram bot that tracks HDD/SSD deals and notifies users based on customizable filters.

## Quick Start

### Environment Setup
```powershell
# Create virtual environment
python -m venv .venv

# Activate (PowerShell)
.venv\Scripts\Activate.ps1

# Install package with dev dependencies
pip install -e ".[dev]"
```

### Configuration
Copy the example environment file and edit as needed:
```powershell
copy deploy\diskcount.env.example .env
notepad .env
```

## Build and Run Commands

### Run the Bot
```powershell
# Start Telegram bot and background scheduler
python -m diskcount run
```

### Database Initialization
```powershell
# Create database tables
python -m diskcount init-db
```

### CLI Commands
```powershell
# Dry-run scan (no persistence)
python -m diskcount check

# Scan with persistence
python -m diskcount scan --persist

# List current best offers
python -m diskcount list --min-tb 16 --max-eur-tb 20 --media rotational

# Run a single scan (dry-run by default)
python -m diskcount scan --dry-run
```

## Test Commands

### Run All Tests
```powershell
pytest -q
```

### Run a Single Test File
```powershell
pytest tests/test_dealabs.py -q
```

### Run a Specific Test
```powershell
pytest tests/test_dealabs.py::test_parse_dealabs_rss_entry -q
```

### Run Tests with Coverage
```powershell
pytest --cov=diskcount tests/
```

## Lint and Type Check

### Check Types with Mypy (if configured)
```powershell
mypy diskcount tests
```

### Lint with Ruff
```powershell
ruff check diskcount tests
```

### Format with Black (if configured)
```powershell
black --check diskcount tests
```

## Code Style Guidelines

### Python Version and Imports
- Target Python 3.11+
- Use `from __future__ import annotations` for postponed evaluation of annotations
- Organize imports: standard library first, third-party, then local modules
- Use absolute imports (not relative)

### Formatting and Type Hints
- Follow PEP 8 for line length (79 characters recommended)
- Use type hints extensively (PEP 484)
- Use modern Python features: `-> Type` instead of `-> "Type"`, `list[str]` instead of `List[str]`
- Use `Decimal` for monetary values, never `float`
- Use `dataclass` with `frozen=True` for immutable data structures
- Use `field(default_factory=...)` for mutable defaults

### Naming Conventions
- Classes: `PascalCase`
- Functions/Methods: `snake_case`
- Variables: `snake_case`
- Constants: `UPPER_SNAKE_CASE`
- Follow existing naming patterns in the codebase

### Error Handling
- Use `None` for optional returns when appropriate
- Validate inputs with pydantic models where possible
- Use `field_validator` for complex validation logic
- Avoid bare `except:` clauses

### Code Organization
- Keep functions short and focused (single responsibility)
- Use dataclasses for data transfer objects
- Use type aliases for complex types (e.g., `Condition = Literal["new", "used"]`)
- Document public APIs with docstrings (Google/NumPy style preferred)

## Development Workflow

1. Create a feature branch from main
2. Make changes, following code style guidelines
3. Run tests locally: `pytest`
4. Run linters: `ruff check .` and `mypy .`
5. Commit with clear messages
6. Push and create a pull request

## Cursor Rules
No specific Cursor rules found in `.cursorrules` or `.cursor/`.

## Copilot Instructions
No Copilot instructions found in `.github/copilot-instructions.md`.

## Additional Notes
- The project uses SQLite for development; Debian deployment uses `/var/lib/diskcount/diskcount.sqlite3`
- All environment variables are managed through pydantic settings
- Tests use in-memory SQLite for isolation
- The bot uses Telegram Bot API with aiogram 3.x
- Deal sources: diskprices.com (primary), Dealabs RSS, eBay API, Idealo/ leDenicheur/ leboncoin feeds, optional Keepa API

## Project Structure
```
diskcount/
  __init__.py
  __main__.py
  app.py           # Main bot runner
  bot.py           # Telegram bot logic
  cli.py           # Command-line interface
  config.py        # Settings management (pydantic)
  db.py            # SQLAlchemy models and repository
  domain.py        # Domain models (Deal, etc.)
  parsing.py       # Text parsing utilities
  rules.py         # Alert matching and notification logic
  scanner.py       # Scanning orchestration
  sources/         # Source collectors (diskprices, dealabs, ebay, feed, keepa, base)
tests/             # Unit tests
deploy/            # Deployment files
```

(End of file - total ~150 lines)