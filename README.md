# Homebridge Captain's Log

[![Go Report Card](https://goreportcard.com/badge/github.com/astrostl/homebridge-captains-log)](https://goreportcard.com/report/github.com/astrostl/homebridge-captains-log)

> **⚠️ Disclaimer**: This software was "vibe coded" with Claude Code. Use at your own risk. No warranty or guarantee is provided.

A Go CLI tool to monitor Homebridge accessory status changes in real-time. Track when lights turn on/off, outlets switch states, and other HomeKit accessory changes across your smart home.

## Features

- **Zero-configuration setup** with automatic Homebridge discovery via mDNS
- **Real-time monitoring** of all Homebridge accessories
- **Two monitoring modes**: HAP protocol (default) or HTTP API
- **AWTRIX3 LED matrix display integration** with auto-discovery and output
- **Automatic discovery** of child bridges via mDNS
- **Human-readable output** with timestamps
- **Debug mode** for troubleshooting
- **Configurable polling intervals** (default: 3s)
- **Environment variable configuration**

## Demo

![Homebridge Captain's Log in action](demo.gif)

*Real-time monitoring of Homebridge accessories with automatic discovery and AWTRIX3 display integration*

## Quick Start

### Installation

#### Option 1: Using Go (Recommended)

```bash
go install github.com/astrostl/homebridge-captains-log@latest
```

The binary will be installed as `homebridge-captains-log` in your `$GOPATH/bin` directory.

#### Option 2: Build from Source

```bash
git clone https://github.com/astrostl/homebridge-captains-log
cd homebridge-captains-log
make build
```

This creates the `hb-clog` binary in the current directory.

### Configuration

**Zero Configuration (Recommended)**

The tool now works out of the box with automatic discovery:

```bash
# Run immediately - no configuration needed!
./hb-clog
```

**Manual Configuration (Optional)**

For specific setups or when auto-discovery fails:

1. Copy the environment example:
   ```bash
   cp .env.example .env
   ```

2. Edit `.env` with your settings:
   ```bash
   # Homebridge configuration
   CLOG_HB_HOST=192.168.1.100  # Your Homebridge IP
   CLOG_HB_PORT=8581           # Homebridge UI port
   CLOG_HB_TOKEN=your_token    # Get from /api/auth/noauth
   
   # AWTRIX3 LED matrix display configuration (optional - for display output)
   CLOG_AT3_HOST=192.168.1.200 # Your AWTRIX3 device IP
   CLOG_AT3_PORT=80            # AWTRIX3 port (usually 80)
   CLOG_AT3_EXCLUDE=volts,batterylevel  # Exclude specific characteristics from notifications
   ```

3. Get your authentication token:

   **Option A: With Authentication Disabled (Easiest)**
   ```bash
   curl -X POST http://your-homebridge-ip:8581/api/auth/noauth
   ```
   Enable this in Homebridge UI under Config → Homebridge Settings → "Disable Authentication".

   **Option B: With Authentication Enabled**
   If you prefer to keep authentication enabled, manually obtain a token from the Homebridge UI and set it in your `.env` file. The tool will work with any valid authentication token.

### Usage

```bash
# Monitor all child bridges (HAP mode - recommended)
./hb-clog

# Monitor main bridge only 
./hb-clog --main

# Enable debug output
./hb-clog -d

# Custom polling interval
./hb-clog -i 10s

# Discovery-only mode (discover services and exit)
./hb-clog -c 0

# Run for specific number of checks then exit
./hb-clog -c 5

# Exclude specific characteristics from AWTRIX3 notifications
./hb-clog -e volts,batterylevel

# Combine options
./hb-clog -d -i 5s -c 10 -e volts
```

## Monitoring Modes

### HAP Mode (Default)
- Monitors all child bridges directly via HomeKit Accessory Protocol
- Automatically discovers child bridges using mDNS
- More comprehensive coverage of accessories
- Works with complex Homebridge setups

### HTTP API Mode
- Monitors main bridge only via Homebridge REST API  
- Simpler setup but limited to main bridge accessories
- Use `--main` flag to enable

## Output Example

```
[20:03:38] TplinkSmarthome 7829 Casa Lights: Total Consumption 4.073 -> 4.074 (excluded)
[20:03:56] homebridge-sec 2A47 Master Motion Detector: DETECTED
[20:03:59] TplinkSmarthome 7829 Bedroom Light: ON
[20:03:59] TplinkSmarthome 7829 Bedroom Light: Volts 121.9 -> 122 (excluded)
[20:04:11] homebridge-sec 2A47 Living Detector: DETECTED
[20:04:17] TplinkSmarthome 7829 Bedroom Light: Amperes 0 -> 0.1 (excluded)
[20:04:17] TplinkSmarthome 7829 Bedroom Light: Apparent Power 0 -> 12.1 (excluded)
[20:04:17] TplinkSmarthome 7829 Bedroom Light: Volts 122 -> 121.8 (excluded)
[20:04:17] TplinkSmarthome 7829 Bedroom Light: Consumption 0 -> 11.5 (excluded)
[20:04:26] TplinkSmarthome 7829 Bedroom Light: Outlet In Use 0 -> 1 (excluded)
[20:04:29] homebridge-sec 2A47 Living Detector: CLEARED
[20:04:29] homebridge-sec 2A47 Garden Door: OPENED
[20:04:32] homebridge-sec 2A47 Garden Door: CLOSED
[20:04:38] TplinkSmarthome 7829 Bedroom Light: Total Consumption 0.194 -> 0.195 (excluded)
```

## Development

### Building

```bash
make all          # Full build pipeline (quality + test + build)
make build        # Build only
make test         # Run tests
make quality      # Comprehensive quality checks
```

### Testing

```bash
# Single check with debug output
./hb-clog -d -c 1

# Short test run
./hb-clog -d -c 3 -i 5s
```

## Troubleshooting

### mDNS Discovery Issues

Use `dns-sd` to debug service discovery:

```bash
# Browse for HAP services
timeout 5s dns-sd -B _hap._tcp

# Look up specific service details
timeout 3s dns-sd -L "ServiceName" _hap._tcp local.
```

### Common Issues

- **No accessories found**: Check Homebridge is running and accessible
- **Authentication errors**: Verify token with `/api/auth/noauth` endpoint
- **mDNS failures**: Ensure mDNS/Bonjour is working on your network
- **Connection timeouts**: Check firewall settings and network connectivity

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CLOG_HB_HOST` | auto-discover | Homebridge host IP/hostname |
| `CLOG_HB_PORT` | `8581` | Homebridge UI port |
| `CLOG_HB_TOKEN` | (none) | API authentication token |
| `CLOG_AT3_HOST` | auto-discover | AWTRIX3 device IP/hostname for display output |
| `CLOG_AT3_PORT` | `80` | AWTRIX3 device port for display output |
| `CLOG_AT3_EXCLUDE` | (none) | Comma-separated list of characteristics to exclude from AWTRIX3 notifications (case-insensitive) |

### Command Line Options

| Flag | Description |
|------|-------------|
| `--hb-host` | Homebridge host (overrides auto-discovery) |
| `--hb-port` | Homebridge port (overrides env) |
| `--at3-host` | AWTRIX3 device host for display output (overrides auto-discovery) |
| `--at3-port` | AWTRIX3 device port for display output (overrides env) |
| `-e, --exclude` | Comma-separated list of characteristics to exclude from AWTRIX3 notifications (case-insensitive) |
| `-t, --token` | Auth token (overrides env) |
| `-i, --interval` | Polling interval (default: 3s) |
| `-c, --count` | Number of checks before exit |
| `-d, --debug` | Enable debug output |
| `-m, --main` | Monitor main bridge only instead of child bridges |
| `-H, -p` | Legacy short flags for `--hb-host` and `--hb-port` |

## Roadmap

- **Bridge filtering** - Include/exclude specific child bridges from monitoring (e.g., `--bridge-include`, `--bridge-exclude`)
- **Accessory filtering** - Monitor only specific accessories or exclude certain ones (e.g., `--accessory-include`, `--accessory-exclude`)
- **Status filtering** - Filter by specific characteristic changes (e.g., `--status-include On,MotionDetected`, `--status-exclude BatteryLevel`)
- Non-Homebridge HomeKit device polling (requires becoming a full HomeKit controller with Ed25519 keypairs, SRP pairing, and ChaCha20-Poly1305 encryption)

## License

MIT License - see LICENSE file for details.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Run `make quality` to ensure code quality
4. Submit a pull request