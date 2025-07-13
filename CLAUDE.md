# Homebridge Captain's Log

A Go CLI tool to monitor Homebridge accessory status changes.

## Documentation Requirements

**CRITICAL: README.md must always include a disclaimer at the top stating this software was "vibe coded" with Claude Code, use at your own risk, no warranty or guarantee provided.**

## Environment Configuration

For testing, a `.env` file contains the Homebridge configuration using CLOG_ prefix:
- `CLOG_HB_HOST=192.168.50.242`
- `CLOG_HB_PORT=8581`
- `CLOG_HB_TOKEN=your_auth_token_here`

The `.env` file is gitignored to prevent committing local configuration.

## API Documentation

### Homebridge API

Homebridge UI has Swagger documentation available at `/swagger` endpoint.
Local copy stored in `ref/homebridge-api.json` for reference.

Source code: https://github.com/homebridge/homebridge-config-ui-x

### AWTRIX3 API

AWTRIX3 LED matrix display API documentation stored in `ref/awtrix3-api.md` for reference.

**Hardware**: Ulanzi TC001 - 32x8 pixel LED matrix (verified via corner pixel testing)

**Screen Capture**: Can retrieve current display state via `curl http://[IP]/api/screen` - returns 256-element array of RGB values for analysis

**Text Positioning**: For precise vertical text positioning, use the `draw` array with `dt` command instead of `textOffset`. Format: `{"draw": [{"dt": [x, y, "text", "color"]}]}` where y=0-7 for the 8-pixel height display.

Source: https://raw.githubusercontent.com/Blueforcer/awtrix3/main/docs/api.md

### Getting Auth Token for Debugging

When debugging with curl, use `/api/auth/noauth` to get an authentication token:

```bash
# Get auth token (requires POST request, no authentication)
curl -X POST http://192.168.50.242:8581/api/auth/noauth

# Use token for authenticated requests
curl -H "Authorization: Bearer YOUR_TOKEN_HERE" http://192.168.50.242:8581/api/status/homebridge/child-bridges
```

## Build and Run

### Tool Version Pinning

The Makefile pins specific versions of all development tools for:
- **Reproducible builds** - Everyone gets identical tool behavior regardless of install time
- **Prevents breaking changes** - New tool versions can introduce stricter rules or different behavior
- **CI/CD consistency** - Build servers use same tool versions as local development  
- **Debugging** - Know exactly which tool version was used when issues arise
- **Controlled upgrades** - Test new tool versions before rolling out to entire team

#### Updating Tool Versions

To check for and update tool versions, use Go commands instead of web searches:

```bash
# Check available versions for specific tools
go list -m -versions github.com/mgechev/revive
go list -m -versions github.com/fzipp/gocyclo
go list -m -versions github.com/gordonklaus/ineffassign
go list -m -versions github.com/golangci/misspell
go list -m -versions github.com/securego/gosec/v2
go list -m -versions golang.org/x/tools/cmd/goimports
go list -m -versions honnef.co/go/tools/cmd/staticcheck
go list -m -versions golang.org/x/vuln/cmd/govulncheck
go list -m -versions golang.org/x/tools/cmd/deadcode

# Check latest version directly
go install github.com/mgechev/revive@latest  # Test latest before updating Makefile
```

Update the version variables in the Makefile after testing, then run `make deps` to install updated tools.

### Using Makefile (REQUIRED)

**🚨 CRITICAL: ALWAYS use the Makefile for building - NEVER use `go build` directly**
- The binary must be named `hb-clog` (not the default module name)
- Only `make build` ensures correct binary naming and output
- Using `go build` creates the wrong binary name and causes confusion

```bash
make all          # Full build pipeline (quality + test + build)
make build        # Build only
make quality      # Comprehensive quality checks (fmt, fmts, vet, verify, vulncheck, lint, cyclo, imports, staticcheck, gosec, ineffassign, misspell, deadcode)
make check        # Basic quality checks (fmt, vet, test)
make test         # Run tests
make fmt          # Format code
make fmts         # Simplify code formatting
make vet          # Static analysis
make mod          # Tidy dependencies
make verify       # Verify module dependencies
make vulncheck    # Check for known vulnerabilities
make lint         # Run revive linter
make cyclo        # Check cyclomatic complexity (threshold 15)
make imports      # Check import formatting
make staticcheck  # Enhanced static analysis
make gosec        # Security vulnerability scanner
make ineffassign  # Detect ineffectual assignments
make misspell     # Check for common spelling errors
make deadcode     # Detect unused (dead) code
make clean        # Remove build artifacts
make install      # Install to GOPATH/bin
make deps         # Install all development tools
make help         # Show all available targets
```

### Manual Build (DEPRECATED - DO NOT USE)

**❌ NEVER use manual `go build` commands - ALWAYS use `make build`**

```bash
# WRONG - DO NOT USE:
# go build -o hb-clog

# CORRECT - ALWAYS USE:
make build
./hb-clog         # Default: monitors all child bridges via HAP
./hb-clog --main  # Monitor main bridge only (instead of child bridges)
./hb-clog -d      # Enable debug output 
./hb-clog -i 5s   # Poll every 5 seconds
./hb-clog -c 3    # Check 3 times then exit
./hb-clog -c 0    # Discovery-only mode: discover bridges and exit
```

## Debug Mode

Use `-d` or `--debug` flag to enable debug output showing:
- Token responses from API
- Child bridge discovery details  
- Individual accessory value changes with types
- First-time accessory detection

Example: `./hb-clog -d -i 2s`

## Testing

### Claude Testing
**🚨 CRITICAL: ALWAYS build with `make build` before testing**

Claude can run the binary directly to test functionality:
```bash
make build         # REQUIRED: Build correct binary first
./hb-clog -d -c 0  # Debug mode, discovery-only (fastest test)
./hb-clog -d -c 1  # Debug mode, single check
./hb-clog -c 2     # Two checks then exit
```

### Manual Testing
**🚨 CRITICAL: ALWAYS build with `make build` before testing**

When debugging or testing, always use the count flag to limit runs:
```bash
make build                  # REQUIRED: Build correct binary first
./hb-clog -c 0              # Discovery-only mode (fastest test)
./hb-clog -d -c 2 -i 5s     # Debug mode, 2 checks, 5 second intervals
./hb-clog -c 1              # Single check then exit
```

## Development Guidelines

**CRITICAL: Timeout Configuration Policy**
- 🚨 **ALL timeouts MUST use TimeoutConfig - NO EXCEPTIONS**
- 🚨 **ZERO hardcoded timeout values in production code**
- 🚨 **Production code violating this policy will be REJECTED**
- 🚨 **Every time value must reference TimeoutConfig, including function parameters, variables, and context timeouts**
- ❌ Don't use `10 * time.Second` - use `TimeoutConfig.HTTPClient`
- ❌ Don't use `100 * time.Millisecond` - use `TimeoutConfig.MDNSReadTimeout`
- ❌ Don't use `time.Sleep(5 * time.Second)` - use `time.Sleep(TimeoutConfig.RetryDelay)`
- ❌ Don't use `context.WithTimeout(ctx, 2*time.Second)` - use `TimeoutConfig.MDNSLookupPerService`
- ❌ Don't use hardcoded numbers in timeout-related code comments
- ✅ Add new timeout categories to TimeoutConfig when needed
- ✅ Tests may use hardcoded timeouts for controlled testing scenarios only
- ✅ All timeouts managed from single location for reliability

**Timeout Policy Enforcement:**
- Search for violations: `grep -r "\d+\s*\*\s*time\." --include="*.go" .` (should return empty for production code)
- Every timeout must trace back to TimeoutConfig for maintainability and consistency
- When adding new timeout-sensitive code, first add the timeout to TimeoutConfig, then reference it

**CRITICAL: mDNS-Only Discovery Policy**
- 🚨 **NEVER implement port scanning or any fallback discovery mechanisms**
- 🚨 **We live or die by mDNS - if mDNS doesn't work, the tool fails gracefully**
- 🚨 **NO exceptions to this rule - port scanning violates dynamic discovery principles**

**NEVER hardcode bridge metadata, ports, or service information:**
- ❌ Don't hardcode ports like 51250, 51251, etc.
- ❌ Don't hardcode bridge names or identifiers  
- ❌ Don't hardcode service discovery results
- ❌ Don't implement port scanning as fallback
- ❌ Don't scan port ranges or probe ports
- ✅ Always use dynamic discovery via APIs and mDNS ONLY
- ✅ Configuration should come from environment variables or API responses
- ✅ The tool must work across different Homebridge installations without modification
- ✅ If mDNS fails to find services, accept that result and continue with what was found

## Troubleshooting mDNS Discovery

To debug HAP service discovery issues, use `dns-sd` to see what services are advertised:

```bash
# Browse for HAP services (use timeout to limit)
timeout 5s dns-sd -B _hap._tcp

# Look up specific service details (replace with actual service name)
timeout 3s dns-sd -L "TplinkSmarthome 4160" _hap._tcp local.

# Browse all services to see what's available
timeout 5s dns-sd -B _services._dns-sd._udp
```

**Note**: `dns-sd` queries work reliably and return instantly, showing Homebridge child bridges with `md=homebridge` in their TXT records. Only services with `md=homebridge` TXT records should be considered for monitoring.

If mDNS discovery fails to find expected services, investigate mDNS configuration issues. The tool will NOT fall back to port scanning.

## Architecture Notes

The tool uses custom mDNS discovery cross-referenced with the Homebridge child bridge API because:
- The Homebridge REST API provides child bridge metadata but **not port information**
- Child bridges dynamically assign ports and don't expose them via API
- mDNS discovery finds all HAP services with ports but includes non-Homebridge devices
- Cross-referencing ensures we only monitor actual Homebridge child bridges

The discovery process:
1. Query `/api/status/homebridge/child-bridges` for known child bridge metadata
2. Use custom mDNS implementation to discover all `_hap._tcp` services on the network with their ports
3. Filter mDNS results to only include services with `md=homebridge` in TXT records  
4. Exclude the main bridge (services named "Homebridge XXXX YYYY")
5. Monitor all remaining services as child bridges

This approach is necessary because:
- **No API endpoint provides child bridge ports** - they're dynamically assigned
- **Custom mDNS provides complete service discovery** - equivalent to `dns-sd -B _hap._tcp`
- **TXT record filtering is reliable** - `md=homebridge` definitively identifies Homebridge services
- **Child bridges vs main bridge** - distinguished by naming patterns in HAP advertisements

## mDNS Implementation

**Use custom mDNS implementation:**
- ✅ Implement equivalent of `dns-sd -B _hap._tcp` (browse services)
- ✅ Implement equivalent of `dns-sd -L "servicename" _hap._tcp local.` (lookup service details)
- ✅ Parse TXT records to filter for `md=homebridge` services ONLY
- ✅ Implement in separate source files for potential library extraction
- ❌ Do not use any third-party mDNS libraries
- ❌ Do not return services without `md=homebridge` TXT record

**References:**
- RFC 6762: https://datatracker.ietf.org/doc/html/rfc6762 (mDNS specification)
- Avahi: https://github.com/avahi/avahi (well-known mDNS implementation)

## Timeout Configuration

**ALL timeouts are centralized in `TimeoutConfig` in main.go for reliability and maintainability.**

### Current Timeout Categories:

```go
// HTTP API timeouts
HTTPClient:              10 * time.Second,  // General HTTP client timeout
HTTPToken:               5 * time.Second,   // Auth token request timeout  
HTTPChildBridges:        10 * time.Second,  // Child bridge API request timeout
HTTPAccessoryCheck:      5 * time.Second,   // Individual accessory check timeout
HTTPReachabilityTest:    1 * time.Second,   // Quick reachability test timeout

// mDNS Discovery timeouts  
MDNSTotal:               10 * time.Second,  // Total mDNS discovery timeout
MDNSBrowseMax:           3 * time.Second,   // Maximum time for browse phase
MDNSLookupMax:           7 * time.Second,   // Maximum time for lookup phase  
MDNSLookupPerService:    2 * time.Second,   // Maximum time per individual service lookup
MDNSReadTimeout:         100 * time.Millisecond, // Network read timeout for mDNS packets
MDNSSilenceTimeout:      300 * time.Millisecond, // How long to wait for new responses
MDNSEarlyExitSilence:    100 * time.Millisecond, // Silence period before early exit

// Retry and delay timeouts
RetryDelay:              2 * time.Second,   // Delay between discovery retry attempts
LookupRetryDelay:        500 * time.Millisecond, // Delay between service lookup retries
DefaultPollingInterval:  30 * time.Second,  // Default polling interval for accessory checks
```

### Timeout Policy Enforcement:

1. **Search for hardcoded timeouts**: `grep -r "\d+\s*\*\s*time\." --include="*.go"`
2. **All timeouts must reference TimeoutConfig**: No exceptions
3. **Tests should use TimeoutConfig when possible**: For consistency  
4. **Add new categories as needed**: Don't hardcode, extend the config
5. **Document timeout relationships**: Some timeouts depend on others (e.g., MDNSTotal = MDNSBrowseMax + MDNSLookupMax)