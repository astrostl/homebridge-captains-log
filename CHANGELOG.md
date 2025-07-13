# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2025-07-13

### Added
- Discovery-only mode when count=0 for quick service discovery without monitoring
- AWTRIX3 API documentation and text positioning guidance in ref/ directory
- Enhanced configuration files (.gosec.json, .revive.toml) for code quality
- Comprehensive timeout configuration system to prevent hardcoded timeouts
- Early completion optimization for faster mDNS discovery
- Caching system for improved discovery performance and reduced network load

### Changed
- Renamed --http flag to --main for clearer interface semantics
- Replaced substring matching with exact name matching for child bridge discovery
- Unified discovery output format across all monitoring modes  
- Default behavior now runs infinite monitoring instead of single check
- Improved build documentation with tool version pinning policy
- Enhanced parallel HAP accessory checks with synchronized output
- Moved API documentation files to ref/ directory for better organization
- Updated branding and simplified discovery messaging

### Fixed
- Improved discovery timing and reliability with optimized timeouts
- Enhanced caching behavior for more consistent service discovery
- Better error handling and retry logic for network operations

### Development
- Added comprehensive pre-commit quality checks in Makefile
- Enhanced testing coverage for mDNS and discovery functionality
- Improved code organization and documentation standards
- Added security scanning and vulnerability checking

## [0.1.0] - 2025-07-13

### Added
- Initial release of Homebridge Captain's Log
- Go CLI tool to monitor Homebridge accessory status changes via HAP protocol
- Custom mDNS client implementation based on RFC 6762 specification
- Discovers Homebridge child bridges via mDNS with md=homebridge filtering
- Retry logic for reliable service discovery
- Support for both main bridge mode and child bridges mode (HAP protocol)
- Environment configuration via .env file with CLOG_ prefix
- Comprehensive test suite with mDNS and main functionality tests
- Complete Makefile with quality checks (fmt, vet, lint, security scan, etc.)
- API documentation and Homebridge API reference

### Changed
- Renamed api.md to API.md for consistency
- Enhanced .gitignore with editor temp files, macOS system files, and coverage files
- Removed swagger.json in favor of homebridge-api.json

### Development
- Added example environment configuration (.env.example)
- Included development tools and linting configuration
- Added comprehensive documentation in CLAUDE.md and README.md