# Homebridge Captain's Log

A Go CLI tool to monitor Homebridge accessory status changes.

## Environment Configuration

For testing, a `.env` file contains the Homebridge configuration using CLOG_ prefix:
- `CLOG_HB_HOST=192.168.50.242`
- `CLOG_HB_PORT=8581`
- `CLOG_HB_TOKEN=your_auth_token_here`

The `.env` file is gitignored to prevent committing local configuration.

## API Documentation

Homebridge UI has Swagger documentation available at `/swagger` endpoint.
Local copy stored in `homebridge-api.json` (gitignored) for reference.

Source code: https://github.com/homebridge/homebridge-config-ui-x

## Build and Run

```bash
go build -o hb-clog
./hb-clog  # Default HAP mode, monitors all child bridges
./hb-clog --http  # HTTP API mode for main bridge only
./hb-clog -d  # Enable debug output 
./hb-clog -i 5s  # Poll every 5 seconds
./hb-clog -c 3  # Check 3 times then exit
```

## Debug Mode

Use `-d` or `--debug` flag to enable debug output showing:
- Token responses from API
- Child bridge discovery details  
- Individual accessory value changes with types
- First-time accessory detection

Example: `./hb-clog -d -i 2s`

## Testing

When debugging or testing, always use the count flag to limit runs:
```bash
./hb-clog -d -c 2 -i 5s  # Debug mode, 2 checks, 5 second intervals
./hb-clog -c 1           # Single check then exit
```

## Development Guidelines

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