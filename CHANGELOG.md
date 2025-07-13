# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2025-07-13

### Added
- Initial release of Homebridge Captain's Log
- Go CLI tool to monitor Homebridge accessory status changes via HAP protocol
- Custom mDNS client implementation based on RFC 6762 specification
- Discovers Homebridge child bridges via mDNS with md=homebridge filtering
- Retry logic for reliable service discovery
- Support for both HTTP API mode and HAP (HomeKit) protocol mode
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