# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.3] - 2025-07-13

### Fixed
- **Go module path** - Corrected module path from `homebridge-captains-log` to `github.com/astrostl/homebridge-captains-log` for proper Go module proxy support

### Added
- **Repository documentation** - Added CODE_OF_CONDUCT.md, CONTRIBUTING.md, LICENSE, and SECURITY.md
- **GitHub repository setup** - Issue templates, funding configuration, and dependabot setup
- **Animated demo GIF** showcasing AWTRIX3 display integration with realistic output example
- **Enhanced README** with improved examples and better presentation

### Changed
- **Repository cleanup** - Improved .gitignore to properly exclude build artifacts and logs
- **Documentation organization** - Better structured project documentation and contribution guidelines

### Development
- **Repository maintenance** - Removed tracked build artifacts and improved repository hygiene
- **Project governance** - Added community health files for better project management

## [0.3.2] - 2025-07-13

### Note
This version had an incorrect Go module path and has been superseded by v0.3.3.

## [0.3.1] - 2025-07-13

### Added
- **AWTRIX3 Display Integration** - Real-time accessory change notifications to AWTRIX3 LED matrix displays
- **Rainbow text effects** for all AWTRIX3 notifications with automatic queuing
- **Connection announcements** sent to AWTRIX3 devices when tool connects (auto-discovery vs manual configuration)
- **`--at3-disable` flag** to disable AWTRIX3 change emission for environments without displays
- **Comprehensive AWTRIX3 notification system** with JSON API integration for the `/api/notify` endpoint

### Changed
- **Enhanced change reporting** now simultaneously outputs to console and AWTRIX3 displays
- **Improved accessory monitoring** with dual-channel notification delivery

### Fixed
- **Code formatting and quality** improvements via automated linting and static analysis

### Development
- **Security scanning** shows 0 vulnerabilities with comprehensive gosec analysis
- **Enhanced test coverage** with all quality checks passing
- **Dependency security review** with vulnerability scanning and update detection

## [0.3.0] - 2025-07-13

### Added
- **AWTRIX3 device detection** with automatic mDNS discovery via `_awtrix._tcp` service type (display integration planned for future release)
- **Concurrent service discovery** using goroutines for improved performance
- **Automatic Homebridge discovery** via mDNS - zero configuration by default
- **AWTRIX3 detection configuration** via `CLOG_AT3_HOST` and `CLOG_AT3_PORT` environment variables
- **AWTRIX3 detection CLI flags** `--at3-host` and `--at3-port` for manual configuration
- **Hostname to IP resolution** for both Homebridge and AWTRIX3 services
- **Graceful fallback handling** when AWTRIX3 devices are not found

### Changed
- **BREAKING: Zero-configuration by default** - tool automatically discovers Homebridge without setup
- **Flag naming consistency** - changed `--host`/`--port` to `--hb-host`/`--hb-port` (maintains backward compatibility with short flags)
- **Default polling interval** changed from 30s to 3s for more responsive monitoring
- **Critical path behavior** - Homebridge discovery failure causes exit, AWTRIX3 failure continues
- **Manual configuration precedence** - CLI flags and environment variables override auto-discovery

### Fixed
- **Service discovery reliability** with enhanced mDNS implementation
- **Concurrent discovery performance** with proper goroutine synchronization

### Development
- **Enhanced environment configuration** with AWTRIX3 detection variables in `.env.example`
- **Improved documentation** with auto-discovery details and configuration options
- **Updated CLI help text** to reflect new flag names and defaults

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