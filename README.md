# Homebridge Captain's Log

[![Go Report Card](https://goreportcard.com/badge/github.com/astrostl/homebridge-captains-log)](https://goreportcard.com/report/github.com/astrostl/homebridge-captains-log)

> **⚠️ Disclaimer**: This software was "vibe coded" with Claude Code. Use at your own risk. No warranty or guarantee is provided.

A Go CLI tool to monitor Homebridge accessory status changes in real-time. Track when lights turn on/off, outlets switch states, and other HomeKit accessory changes across your smart home.

## Features

- **Real-time monitoring** of all Homebridge accessories
- **Two monitoring modes**: HAP protocol (default) or HTTP API
- **Automatic discovery** of child bridges via mDNS
- **Human-readable output** with timestamps
- **Debug mode** for troubleshooting
- **Configurable polling intervals**
- **Environment variable configuration**

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

1. Copy the environment example:
   ```bash
   cp .env.example .env
   ```

2. Edit `.env` with your Homebridge settings:
   ```bash
   CLOG_HB_HOST=192.168.1.100  # Your Homebridge IP
   CLOG_HB_PORT=8581           # Homebridge UI port
   CLOG_HB_TOKEN=your_token    # Get from /api/auth/noauth
   ```

3. Get your authentication token:
   ```bash
   curl -X POST http://your-homebridge-ip:8581/api/auth/noauth
   ```

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

# Combine options
./hb-clog -d -i 5s -c 10
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
2025-07-13 01:15:23 | Office Fan: On changed from false to true
2025-07-13 01:15:45 | Casa Lights: On changed from true to false
2025-07-13 01:16:02 | Kitchen Outlet: On changed from false to true
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
| `CLOG_HB_HOST` | `localhost` | Homebridge host IP/hostname |
| `CLOG_HB_PORT` | `8581` | Homebridge UI port |
| `CLOG_HB_TOKEN` | (none) | API authentication token |

### Command Line Options

| Flag | Description |
|------|-------------|
| `-h, --host` | Homebridge host (overrides env) |
| `-p, --port` | Homebridge port (overrides env) |
| `-t, --token` | Auth token (overrides env) |
| `-i, --interval` | Polling interval (default: 3s) |
| `-c, --count` | Number of checks before exit |
| `-d, --debug` | Enable debug output |
| `-m, --main` | Monitor main bridge only instead of child bridges |

## Roadmap

- mDNS discovery of Homebridge main instance (eliminate manual host/port configuration)
- ULANZI TC001 display output support (via AWTRIX3 API)

## License

MIT License - see LICENSE file for details.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Run `make quality` to ensure code quality
4. Submit a pull request