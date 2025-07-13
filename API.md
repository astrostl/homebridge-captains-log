# HomeKit Accessory Protocol (HAP) API Reference

## Overview

HAP is the protocol used by HomeKit accessories to communicate. Child bridges expose HAP endpoints that can be accessed directly.

## Discovery

### Finding Child Bridges
Child bridges can be discovered via:
1. **Main Homebridge API**: `GET /api/status/homebridge/child-bridges` returns metadata (no ports)
2. **mDNS/Bonjour**: Look for `_hap._tcp` services with `md=homebridge` TXT records to get ports

Example child bridge info from main API:
```json
{
  "status": "ok",
  "username": "0E:3D:C3:E5:88:71", 
  "name": "TplinkSmarthome",
  "plugin": "homebridge-tplink-smarthome",
  "pid": 12287
}
```

## HAP Endpoints

### GET /accessories
Returns all accessories and their current state for a bridge.

**URL**: `http://bridge-host:bridge-port/accessories`
**Method**: GET
**Auth**: None required for local network access

**Response Structure**:
```json
{
  "accessories": [
    {
      "aid": 6,
      "services": [
        {
          "type": "3E",
          "iid": 1,
          "characteristics": [...]
        },
        {
          "type": "47", 
          "iid": 9,
          "characteristics": [
            {
              "type": "23",
              "iid": 10,
              "value": "Office Fan",
              "description": "Name"
            },
            {
              "type": "25",
              "iid": 11, 
              "value": 0,
              "perms": ["ev","pr","pw"],
              "description": "On",
              "format": "bool"
            }
          ]
        }
      ]
    }
  ]
}
```

### Characteristic Types
Common characteristic type codes:
- `"25"`: On/Off state (boolean)
- `"23"`: Name (string)
- `"26"`: Outlet In Use (boolean) 
- `"E863F10D-..."`: Power consumption (float, watts)
- `"E863F126-..."`: Current (float, amperes)
- `"E863F10A-..."`: Voltage (float, volts)

### Service Types
- `"3E"`: Accessory Information
- `"47"`: Outlet service
- `"A2"`: Protocol Information

## Example Accessories Found

### Office Fan (TplinkSmarthome bridge)
```json
{
  "aid": 6,
  "services": [{
    "type": "47",
    "characteristics": [
      {
        "type": "23", 
        "value": "Office Fan",
        "description": "Name"
      },
      {
        "type": "25",
        "value": 0,
        "description": "On",
        "format": "bool",
        "perms": ["ev","pr","pw"]
      }
    ]
  }]
}
```

### Casa Lights (TplinkSmarthome bridge) 
```json
{
  "aid": 3,
  "services": [{
    "type": "47", 
    "characteristics": [
      {
        "type": "23",
        "value": "Casa Lights", 
        "description": "Name"
      },
      {
        "type": "25",
        "value": 1,
        "description": "On",
        "format": "bool"
      }
    ]
  }]
}
```

## Permissions
- `"pr"`: Paired Read
- `"pw"`: Paired Write  
- `"ev"`: Events (notifications)

## Monitoring Strategy

1. **Poll `/accessories`** endpoint periodically
2. **Compare characteristic values** between polls
3. **Report changes** in human-readable format
4. **Focus on `"25"` (On) characteristics** for switches/outlets

## Notes

- Child bridges don't require authentication for local network access
- Response is large JSON with nested structure
- Each accessory has unique `aid` (accessory ID)
- Each characteristic has unique `iid` (instance ID) within service
- `"ev"` permission indicates characteristic supports change notifications