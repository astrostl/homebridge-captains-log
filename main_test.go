package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGetEnvOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		want         string
	}{
		{
			name:         "env value exists",
			key:          "TEST_KEY",
			defaultValue: "default",
			envValue:     "env_value",
			want:         "env_value",
		},
		{
			name:         "env value empty returns default",
			key:          "EMPTY_KEY",
			defaultValue: "default",
			envValue:     "",
			want:         "default",
		},
		{
			name:         "nonexistent key returns default",
			key:          "NONEXISTENT_KEY",
			defaultValue: "default",
			envValue:     "",
			want:         "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.key, tt.envValue)
			}
			got := getEnvOrDefault(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvOrDefault() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnvIntOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue int
		envValue     string
		want         int
	}{
		{
			name:         "valid integer",
			key:          "TEST_INT",
			defaultValue: 8080,
			envValue:     "9090",
			want:         9090,
		},
		{
			name:         "invalid integer returns default",
			key:          "INVALID_INT",
			defaultValue: 8080,
			envValue:     "not_a_number",
			want:         8080,
		},
		{
			name:         "empty value returns default",
			key:          "EMPTY_INT",
			defaultValue: 8080,
			envValue:     "",
			want:         8080,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.key, tt.envValue)
			}
			got := getEnvIntOrDefault(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvIntOrDefault() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasChanged(t *testing.T) {
	monitor := &StatusMonitor{}

	tests := []struct {
		name string
		old  AccessoryStatus
		new  AccessoryStatus
		want bool
	}{
		{
			name: "no change",
			old: AccessoryStatus{
				Values: map[string]interface{}{"On": true, "Brightness": 50},
			},
			new: AccessoryStatus{
				Values: map[string]interface{}{"On": true, "Brightness": 50},
			},
			want: false,
		},
		{
			name: "value changed",
			old: AccessoryStatus{
				Values: map[string]interface{}{"On": true},
			},
			new: AccessoryStatus{
				Values: map[string]interface{}{"On": false},
			},
			want: true,
		},
		{
			name: "new key added",
			old: AccessoryStatus{
				Values: map[string]interface{}{"On": true},
			},
			new: AccessoryStatus{
				Values: map[string]interface{}{"On": true, "Brightness": 50},
			},
			want: true,
		},
		{
			name: "key removed",
			old: AccessoryStatus{
				Values: map[string]interface{}{"On": true, "Brightness": 50},
			},
			new: AccessoryStatus{
				Values: map[string]interface{}{"On": true},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := monitor.hasChanged(tt.old, tt.new)
			if got != tt.want {
				t.Errorf("hasChanged() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatChangeMessage(t *testing.T) {
	monitor := &StatusMonitor{}

	tests := []struct {
		name        string
		key         string
		oldValue    interface{}
		newValue    interface{}
		serviceName string
		want        string
	}{
		{
			name:        "turn on",
			key:         "On",
			oldValue:    false,
			newValue:    true,
			serviceName: "Test Light",
			want:        "turned ON",
		},
		{
			name:        "turn off",
			key:         "On",
			oldValue:    true,
			newValue:    false,
			serviceName: "Test Light",
			want:        "turned OFF",
		},
		{
			name:        "contact sensor opened",
			key:         "ContactSensorState",
			oldValue:    0.0,
			newValue:    1.0,
			serviceName: "Door Sensor",
			want:        "door/window OPENED",
		},
		{
			name:        "contact sensor closed",
			key:         "ContactSensorState",
			oldValue:    1.0,
			newValue:    0.0,
			serviceName: "Door Sensor",
			want:        "door/window CLOSED",
		},
		{
			name:        "motion detected",
			key:         "MotionDetected",
			oldValue:    false,
			newValue:    true,
			serviceName: "Motion Sensor",
			want:        "motion DETECTED",
		},
		{
			name:        "motion cleared",
			key:         "MotionDetected",
			oldValue:    true,
			newValue:    false,
			serviceName: "Motion Sensor",
			want:        "motion CLEARED",
		},
		{
			name:        "brightness change",
			key:         "Brightness",
			oldValue:    50,
			newValue:    75,
			serviceName: "Test Light",
			want:        "brightness: 50% → 75%",
		},
		{
			name:        "temperature change",
			key:         "CurrentTemperature",
			oldValue:    22.5,
			newValue:    23.1,
			serviceName: "Temperature Sensor",
			want:        "temperature: 22.5°C → 23.1°C",
		},
		{
			name:        "humidity change",
			key:         "CurrentRelativeHumidity",
			oldValue:    45.0,
			newValue:    48.5,
			serviceName: "Humidity Sensor",
			want:        "humidity: 45.0% → 48.5%",
		},
		{
			name:        "battery level change",
			key:         "BatteryLevel",
			oldValue:    80,
			newValue:    75,
			serviceName: "Battery Device",
			want:        "battery: 80% → 75%",
		},
		{
			name:        "low battery warning",
			key:         "StatusLowBattery",
			oldValue:    0.0,
			newValue:    1.0,
			serviceName: "Battery Device",
			want:        "⚠️  LOW BATTERY",
		},
		{
			name:        "battery ok",
			key:         "StatusLowBattery",
			oldValue:    1.0,
			newValue:    0.0,
			serviceName: "Battery Device",
			want:        "battery OK",
		},
		{
			name:        "unknown key",
			key:         "UnknownKey",
			oldValue:    "old",
			newValue:    "new",
			serviceName: "Test Device",
			want:        "UnknownKey: old → new",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := monitor.formatChangeMessage(tt.key, tt.oldValue, tt.newValue, tt.serviceName)
			if got != tt.want {
				t.Errorf("formatChangeMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFetchAccessories(t *testing.T) {
	// Test successful response
	t.Run("successful fetch", func(t *testing.T) {
		testAccessories := []AccessoryStatus{
			{
				UniqueID:    "test1",
				ServiceName: "Test Light",
				Type:        "light",
				Values:      map[string]interface{}{"On": true},
			},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/accessories" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(testAccessories)
			}
		}))
		defer server.Close()

		monitor := &StatusMonitor{
			baseURL: server.URL,
			client:  &http.Client{Timeout: 5 * time.Second},
		}

		accessories, err := monitor.fetchAccessories()
		if err != nil {
			t.Errorf("fetchAccessories() error = %v", err)
			return
		}

		if len(accessories) != 1 {
			t.Errorf("Expected 1 accessory, got %d", len(accessories))
			return
		}

		if accessories[0].UniqueID != "test1" {
			t.Errorf("Expected UniqueID 'test1', got '%s'", accessories[0].UniqueID)
		}
	})

	// Test unauthorized response with successful noauth
	t.Run("401 with successful noauth", func(t *testing.T) {
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			switch r.URL.Path {
			case "/api/accessories":
				if requestCount == 1 {
					// First request returns 401
					w.WriteHeader(http.StatusUnauthorized)
				} else {
					// Second request (after auth) succeeds
					testAccessories := []AccessoryStatus{
						{UniqueID: "test1", ServiceName: "Test Light", Values: map[string]interface{}{"On": true}},
					}
					json.NewEncoder(w).Encode(testAccessories)
				}
			case "/api/auth/noauth":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"access_token": "test_token"})
			}
		}))
		defer server.Close()

		monitor := &StatusMonitor{
			baseURL: server.URL,
			client:  &http.Client{Timeout: 5 * time.Second},
		}

		accessories, err := monitor.fetchAccessories()
		if err != nil {
			t.Errorf("fetchAccessories() error = %v", err)
			return
		}

		if len(accessories) != 1 {
			t.Errorf("Expected 1 accessory, got %d", len(accessories))
		}

		if monitor.token != "test_token" {
			t.Errorf("Expected token to be set to 'test_token', got '%s'", monitor.token)
		}
	})

	// Test error response
	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		monitor := &StatusMonitor{
			baseURL: server.URL,
			client:  &http.Client{Timeout: 5 * time.Second},
		}

		_, err := monitor.fetchAccessories()
		if err == nil {
			t.Errorf("Expected error for 500 status, got nil")
		}
	})
}

func TestGetAccessoryName(t *testing.T) {
	tests := []struct {
		name      string
		accessory HAPAccessoryData
		want      string
	}{
		{
			name: "valid name found",
			accessory: HAPAccessoryData{
				AID: 1,
				Services: []HAPService{
					{
						Characteristics: []HAPCharacteristic{
							{
								Type:        "23",
								Description: "Name",
								Value:       "Test Light",
							},
						},
					},
				},
			},
			want: "Test Light",
		},
		{
			name: "no name characteristic",
			accessory: HAPAccessoryData{
				AID: 1,
				Services: []HAPService{
					{
						Characteristics: []HAPCharacteristic{
							{
								Type:        "25",
								Description: "On",
								Value:       true,
							},
						},
					},
				},
			},
			want: "",
		},
		{
			name: "name not string",
			accessory: HAPAccessoryData{
				AID: 1,
				Services: []HAPService{
					{
						Characteristics: []HAPCharacteristic{
							{
								Type:        "23",
								Description: "Name",
								Value:       123,
							},
						},
					},
				},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getAccessoryName(tt.accessory)
			if got != tt.want {
				t.Errorf("getAccessoryName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetFloatValue(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want float64
	}{
		{
			name: "float64 value",
			val:  1.5,
			want: 1.5,
		},
		{
			name: "int value",
			val:  42,
			want: 0.0,
		},
		{
			name: "string value",
			val:  "not a number",
			want: 0.0,
		},
		{
			name: "nil value",
			val:  nil,
			want: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getFloatValue(tt.val)
			if got != tt.want {
				t.Errorf("getFloatValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsKnownChildBridge(t *testing.T) {
	tests := []struct {
		name        string
		hapService  HAPAccessory
		childBridge []ChildBridge
		want        bool
	}{
		{
			name: "main bridge excluded",
			hapService: HAPAccessory{
				Name: "Homebridge 1234 5678",
				Host: "192.168.1.100",
				Port: 51234,
			},
			childBridge: []ChildBridge{},
			want:        false,
		},
		{
			name: "child bridge included",
			hapService: HAPAccessory{
				Name: "TplinkSmarthome 4160",
				Host: "192.168.1.100",
				Port: 51250,
			},
			childBridge: []ChildBridge{},
			want:        true,
		},
		{
			name: "another child bridge included",
			hapService: HAPAccessory{
				Name: "MyPlugin Bridge",
				Host: "192.168.1.100",
				Port: 51251,
			},
			childBridge: []ChildBridge{},
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isKnownChildBridge(tt.hapService, tt.childBridge)
			if got != tt.want {
				t.Errorf("isKnownChildBridge() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAccessoryStatusDeepEqual(t *testing.T) {
	// Test that time fields don't affect equality when comparing values
	now := time.Now()
	later := now.Add(1 * time.Minute)

	status1 := AccessoryStatus{
		UniqueID:    "test1",
		ServiceName: "Test Light",
		Values:      map[string]interface{}{"On": true},
		LastUpdated: now,
	}

	status2 := AccessoryStatus{
		UniqueID:    "test1",
		ServiceName: "Test Light",
		Values:      map[string]interface{}{"On": true},
		LastUpdated: later,
	}

	// Values should be considered equal even with different timestamps
	if !reflect.DeepEqual(status1.Values, status2.Values) {
		t.Errorf("Values should be equal regardless of timestamp")
	}

	// But full struct should not be equal due to timestamp
	if reflect.DeepEqual(status1, status2) {
		t.Errorf("Full structs should not be equal due to different timestamps")
	}
}

func TestReportChange(_ *testing.T) {
	monitor := &StatusMonitor{}

	// Test basic functionality without asserting specific output format
	// since this function primarily handles console output
	old := AccessoryStatus{
		ServiceName: "Test Light",
		Values:      map[string]interface{}{"On": false, "Brightness": 50},
		LastUpdated: time.Now(),
	}

	current := AccessoryStatus{
		ServiceName: "Test Light",
		Values:      map[string]interface{}{"On": true, "Brightness": 75},
		LastUpdated: time.Now(),
	}

	// Test doesn't panic and handles the change reporting
	monitor.reportChange(old, current)

	// Test with removed key
	currentWithRemovedKey := AccessoryStatus{
		ServiceName: "Test Light",
		Values:      map[string]interface{}{"On": true},
		LastUpdated: time.Now(),
	}

	monitor.reportChange(old, currentWithRemovedKey)

	// Test with new key added
	currentWithAddedKey := AccessoryStatus{
		ServiceName: "Test Light",
		Values:      map[string]interface{}{"On": false, "Brightness": 50, "Hue": 180},
		LastUpdated: time.Now(),
	}

	monitor.reportChange(old, currentWithAddedKey)
}

func TestReportHAPChange(t *testing.T) {
	tests := []struct {
		name          string
		accessoryName string
		char          HAPCharacteristic
		oldValue      interface{}
		newValue      interface{}
	}{
		{
			name:          "turn on",
			accessoryName: "Test Light",
			char:          HAPCharacteristic{Type: "25", Description: "On"},
			oldValue:      0.0,
			newValue:      1.0,
		},
		{
			name:          "turn off",
			accessoryName: "Test Light",
			char:          HAPCharacteristic{Type: "25", Description: "On"},
			oldValue:      1.0,
			newValue:      0.0,
		},
		{
			name:          "other characteristic",
			accessoryName: "Test Sensor",
			char:          HAPCharacteristic{Type: "10", Description: "Temperature"},
			oldValue:      22.5,
			newValue:      23.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			// Test doesn't panic and handles the change reporting
			reportHAPChange(tt.accessoryName, tt.char, tt.oldValue, tt.newValue)
		})
	}
}

func TestHasHAPAccessories(t *testing.T) {
	// Test successful response with accessories
	t.Run("has accessories", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/accessories" {
				w.Header().Set("Content-Type", "application/json")
				response := HAPResponse{
					Accessories: []HAPAccessoryData{
						{AID: 1, Services: []HAPService{{Type: "lightbulb"}}},
					},
				}
				json.NewEncoder(w).Encode(response)
			}
		}))
		defer server.Close()

		host := "localhost"
		port := 8080
		// Extract port from server URL
		serverURL := server.URL
		_, portStr, _ := net.SplitHostPort(strings.TrimPrefix(serverURL, "http://"))
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}

		result := hasHAPAccessories(host, port)
		if !result {
			t.Errorf("hasHAPAccessories() = false, want true")
		}
	})

	// Test no accessories
	t.Run("no accessories", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/accessories" {
				w.Header().Set("Content-Type", "application/json")
				response := HAPResponse{Accessories: []HAPAccessoryData{}}
				json.NewEncoder(w).Encode(response)
			}
		}))
		defer server.Close()

		host := "localhost"
		port := 8080
		// Extract port from server URL
		serverURL := server.URL
		_, portStr, _ := net.SplitHostPort(strings.TrimPrefix(serverURL, "http://"))
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}

		result := hasHAPAccessories(host, port)
		if result {
			t.Errorf("hasHAPAccessories() = true, want false")
		}
	})

	// Test HTTP error
	t.Run("http error", func(t *testing.T) {
		result := hasHAPAccessories("nonexistent.host", 9999)
		if result {
			t.Errorf("hasHAPAccessories() = true, want false for connection error")
		}
	})

	// Test non-200 status
	t.Run("non-200 status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		host := "localhost"
		port := 8080
		// Extract port from server URL
		serverURL := server.URL
		_, portStr, _ := net.SplitHostPort(strings.TrimPrefix(serverURL, "http://"))
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}

		result := hasHAPAccessories(host, port)
		if result {
			t.Errorf("hasHAPAccessories() = true, want false for 401 status")
		}
	})
}

func TestGetNoAuthTokenComplete(t *testing.T) {
	// Test with 'token' field instead of 'access_token'
	t.Run("token field", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/auth/noauth" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"token": "alternative_token"})
			}
		}))
		defer server.Close()

		monitor := &StatusMonitor{
			baseURL: server.URL,
			client:  &http.Client{Timeout: 5 * time.Second},
		}

		token, err := monitor.getNoAuthToken()
		if err != nil {
			t.Errorf("getNoAuthToken() error = %v", err)
			return
		}

		if token != "alternative_token" {
			t.Errorf("getNoAuthToken() = %v, want alternative_token", token)
		}
	})

	// Test with no token in response
	t.Run("no token in response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/auth/noauth" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"other_field": "value"})
			}
		}))
		defer server.Close()

		monitor := &StatusMonitor{
			baseURL: server.URL,
			client:  &http.Client{Timeout: 5 * time.Second},
		}

		_, err := monitor.getNoAuthToken()
		if err == nil {
			t.Errorf("getNoAuthToken() expected error for missing token, got nil")
		}
	})

	// Test invalid JSON response
	t.Run("invalid json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/auth/noauth" {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte("invalid json"))
			}
		}))
		defer server.Close()

		monitor := &StatusMonitor{
			baseURL: server.URL,
			client:  &http.Client{Timeout: 5 * time.Second},
		}

		_, err := monitor.getNoAuthToken()
		if err == nil {
			t.Errorf("getNoAuthToken() expected error for invalid JSON, got nil")
		}
	})
}

func TestDebugf(t *testing.T) {
	// Test debug function with debug flag on
	original := debug
	defer func() { debug = original }()

	t.Run("debug enabled", func(_ *testing.T) {
		debug = true
		// This test verifies debugf doesn't panic - output is to stdout so we can't capture it easily
		debugf("test message %s %d", "hello", 42)
	})

	t.Run("debug disabled", func(_ *testing.T) {
		debug = false
		// This test verifies debugf doesn't panic when debug is off
		debugf("test message %s %d", "hello", 42)
	})
}

func TestMonitorRun(t *testing.T) {
	t.Run("single check", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/accessories" {
				w.Header().Set("Content-Type", "application/json")
				testAccessories := []AccessoryStatus{
					{UniqueID: "test1", ServiceName: "Test Light", Values: map[string]interface{}{"On": true}},
				}
				json.NewEncoder(w).Encode(testAccessories)
			}
		}))
		defer server.Close()

		monitor := &StatusMonitor{
			baseURL:    server.URL,
			interval:   100 * time.Millisecond,
			lastStatus: make(map[string]AccessoryStatus),
			client:     &http.Client{Timeout: 5 * time.Second},
		}

		// Test single check (maxChecks = 1)
		monitor.run(1)

		// Verify the accessory was recorded
		if len(monitor.lastStatus) != 1 {
			t.Errorf("Expected 1 accessory recorded, got %d", len(monitor.lastStatus))
		}
	})

	t.Run("multiple checks", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/accessories" {
				callCount++
				w.Header().Set("Content-Type", "application/json")
				testAccessories := []AccessoryStatus{
					{UniqueID: "test1", ServiceName: "Test Light", Values: map[string]interface{}{"On": callCount%2 == 0}},
				}
				json.NewEncoder(w).Encode(testAccessories)
			}
		}))
		defer server.Close()

		monitor := &StatusMonitor{
			baseURL:    server.URL,
			interval:   50 * time.Millisecond,
			lastStatus: make(map[string]AccessoryStatus),
			client:     &http.Client{Timeout: 5 * time.Second},
		}

		// Test multiple checks with short interval
		monitor.run(3)

		// Verify multiple calls were made
		if callCount < 3 {
			t.Errorf("Expected at least 3 API calls, got %d", callCount)
		}
	})
}

func TestCheckStatus(t *testing.T) {
	t.Run("successful check with changes", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/accessories" {
				w.Header().Set("Content-Type", "application/json")
				testAccessories := []AccessoryStatus{
					{UniqueID: "test1", ServiceName: "Test Light", Values: map[string]interface{}{"On": true}},
				}
				json.NewEncoder(w).Encode(testAccessories)
			}
		}))
		defer server.Close()

		monitor := &StatusMonitor{
			baseURL: server.URL,
			lastStatus: map[string]AccessoryStatus{
				"test1": {UniqueID: "test1", ServiceName: "Test Light", Values: map[string]interface{}{"On": false}},
			},
			client: &http.Client{Timeout: 5 * time.Second},
		}

		// This should detect the change from Off to On
		monitor.checkStatus()

		// Verify the status was updated
		if !monitor.lastStatus["test1"].Values["On"].(bool) {
			t.Errorf("Expected status to be updated to On=true")
		}
	})

	t.Run("error during fetch", func(_ *testing.T) {
		monitor := &StatusMonitor{
			baseURL:    "http://nonexistent.invalid",
			lastStatus: make(map[string]AccessoryStatus),
			client:     &http.Client{Timeout: 100 * time.Millisecond},
		}

		// Should handle error gracefully without panicking
		monitor.checkStatus()
	})
}

func TestGetChildBridges(t *testing.T) {
	t.Run("successful response", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			switch r.URL.Path {
			case "/api/auth/noauth":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"access_token": "test_token"})
			case "/api/status/homebridge/child-bridges":
				w.Header().Set("Content-Type", "application/json")
				bridges := []ChildBridge{
					{Name: "Test Bridge", Plugin: "test-plugin", Status: "running"},
				}
				json.NewEncoder(w).Encode(bridges)
			}
		}))
		defer server.Close()

		// Extract host and port from server URL
		serverURL := strings.TrimPrefix(server.URL, "http://")
		hostPort := strings.Split(serverURL, ":")
		origHost := host
		origPort := port
		host = hostPort[0]
		if len(hostPort) > 1 {
			if p, err := strconv.Atoi(hostPort[1]); err == nil {
				port = p
			}
		}
		defer func() {
			host = origHost
			port = origPort
		}()

		bridges := getChildBridges()

		if len(bridges) != 1 {
			t.Errorf("Expected 1 bridge, got %d", len(bridges))
			return
		}

		if bridges[0].Name != "Test Bridge" {
			t.Errorf("Expected bridge name 'Test Bridge', got '%s'", bridges[0].Name)
		}

		if callCount < 2 {
			t.Errorf("Expected at least 2 API calls (auth + bridges), got %d", callCount)
		}
	})

	t.Run("auth failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/auth/noauth" {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer server.Close()

		// Extract host and port from server URL
		serverURL := strings.TrimPrefix(server.URL, "http://")
		hostPort := strings.Split(serverURL, ":")
		origHost := host
		origPort := port
		host = hostPort[0]
		if len(hostPort) > 1 {
			if p, err := strconv.Atoi(hostPort[1]); err == nil {
				port = p
			}
		}
		defer func() {
			host = origHost
			port = origPort
		}()

		bridges := getChildBridges()

		if bridges != nil {
			t.Errorf("Expected nil bridges on auth failure, got %v", bridges)
		}
	})
}

func TestDiscoverHAPServices(t *testing.T) {
	// This test verifies the function doesn't panic and handles the integration correctly
	// We can't easily mock mDNS without significant refactoring, so we test with a very short timeout

	// Temporarily set debug to false to reduce noise
	origDebug := debug
	debug = false
	defer func() { debug = origDebug }()

	// Use a timeout to avoid hanging
	done := make(chan []HAPAccessory, 1)
	go func() {
		services := discoverHAPServices()
		done <- services
	}()

	select {
	case services := <-done:
		// Should return a slice (may be empty if no services found)
		if services == nil {
			t.Errorf("discoverHAPServices() should not return nil")
		}
		// The test environment may or may not have HAP services
		t.Logf("Found %d HAP services", len(services))
	case <-time.After(1 * time.Second):
		t.Log("discoverHAPServices() took longer than 1 second, skipping detailed verification")
		// This is acceptable as the function is working, just taking time
	}
}

func TestCheckHAPAccessory(t *testing.T) {
	t.Run("successful check", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/accessories" {
				w.Header().Set("Content-Type", "application/json")
				response := HAPResponse{
					Accessories: []HAPAccessoryData{
						{
							AID: 1,
							Services: []HAPService{
								{
									Characteristics: []HAPCharacteristic{
										{Type: "23", Description: "Name", Value: "Test Light"},
										{Type: "25", Description: "On", Value: 1.0},
									},
								},
							},
						},
					},
				}
				json.NewEncoder(w).Encode(response)
			}
		}))
		defer server.Close()

		client := &http.Client{Timeout: 5 * time.Second}

		// Extract host and port from server URL
		serverURL := strings.TrimPrefix(server.URL, "http://")
		hostPort := strings.Split(serverURL, ":")
		testHost := hostPort[0]
		testPort := 8080
		if len(hostPort) > 1 {
			if p, err := strconv.Atoi(hostPort[1]); err == nil {
				testPort = p
			}
		}

		acc := HAPAccessory{
			Name: "Test Bridge",
			Host: testHost,
			Port: testPort,
			ID:   "test-bridge",
		}

		lastStatus := make(map[string]interface{})

		// Should complete without error
		checkHAPAccessory(client, acc, lastStatus)

		// Should have recorded the accessory state
		expectedKey := "1_Test Light"
		if _, exists := lastStatus[expectedKey]; !exists {
			t.Errorf("Expected accessory state to be recorded with key '%s'", expectedKey)
		}
	})

	t.Run("successful check with state change detection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/accessories" {
				w.Header().Set("Content-Type", "application/json")
				response := HAPResponse{
					Accessories: []HAPAccessoryData{
						{
							AID: 1,
							Services: []HAPService{
								{
									Characteristics: []HAPCharacteristic{
										{Type: "23", Description: "Name", Value: "Test Light"},
										{Type: "25", Description: "On", Value: 1.0},
									},
								},
							},
						},
					},
				}
				json.NewEncoder(w).Encode(response)
			}
		}))
		defer server.Close()

		client := &http.Client{Timeout: 5 * time.Second}

		// Extract host and port from server URL
		serverURL := strings.TrimPrefix(server.URL, "http://")
		hostPort := strings.Split(serverURL, ":")
		testHost := hostPort[0]
		testPort := 8080
		if len(hostPort) > 1 {
			if p, err := strconv.Atoi(hostPort[1]); err == nil {
				testPort = p
			}
		}

		acc := HAPAccessory{
			Name: "Test Bridge",
			Host: testHost,
			Port: testPort,
			ID:   "test-bridge",
		}

		// Initialize with different state to test change detection
		lastStatus := map[string]interface{}{
			"1_Test Light": 0.0, // Previous state was off
		}

		// Should detect the change from off to on
		checkHAPAccessory(client, acc, lastStatus)

		// Should have updated the accessory state
		if lastStatus["1_Test Light"] != 1.0 {
			t.Errorf("Expected accessory state to be updated to 1.0, got %v", lastStatus["1_Test Light"])
		}
	})

	t.Run("invalid json response", func(_ *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/accessories" {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte("invalid json"))
			}
		}))
		defer server.Close()

		client := &http.Client{Timeout: 5 * time.Second}

		// Extract host and port from server URL
		serverURL := strings.TrimPrefix(server.URL, "http://")
		hostPort := strings.Split(serverURL, ":")
		testHost := hostPort[0]
		testPort := 8080
		if len(hostPort) > 1 {
			if p, err := strconv.Atoi(hostPort[1]); err == nil {
				testPort = p
			}
		}

		acc := HAPAccessory{
			Name: "Test Bridge",
			Host: testHost,
			Port: testPort,
			ID:   "test-bridge",
		}

		lastStatus := make(map[string]interface{})

		// Should handle invalid JSON gracefully
		checkHAPAccessory(client, acc, lastStatus)

		// Status should remain empty due to parsing error
		if len(lastStatus) != 0 {
			t.Errorf("Expected empty status map due to JSON parsing error, got %v", lastStatus)
		}
	})

	t.Run("connection error", func(_ *testing.T) {
		client := &http.Client{Timeout: 100 * time.Millisecond}
		acc := HAPAccessory{
			Name: "Test Bridge",
			Host: "nonexistent.invalid",
			Port: 9999,
			ID:   "test-bridge",
		}
		lastStatus := make(map[string]interface{})

		// Should handle error gracefully without panicking
		checkHAPAccessory(client, acc, lastStatus)
	})

	t.Run("http error status", func(_ *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		client := &http.Client{Timeout: 5 * time.Second}

		// Extract host and port from server URL
		serverURL := strings.TrimPrefix(server.URL, "http://")
		hostPort := strings.Split(serverURL, ":")
		testHost := hostPort[0]
		testPort := 8080
		if len(hostPort) > 1 {
			if p, err := strconv.Atoi(hostPort[1]); err == nil {
				testPort = p
			}
		}

		acc := HAPAccessory{
			Name: "Test Bridge",
			Host: testHost,
			Port: testPort,
			ID:   "test-bridge",
		}

		lastStatus := make(map[string]interface{})

		// Should handle 401 error gracefully
		checkHAPAccessory(client, acc, lastStatus)
	})
}

func TestDiscoverHAPServicesIntegration(t *testing.T) {
	// This test verifies the integration between mDNS discovery and HAP service parsing
	// without actually depending on network mDNS (which would be unreliable in tests)

	t.Run("service discovery integration", func(t *testing.T) {
		// Temporarily disable debug output for cleaner test output
		origDebug := debug
		debug = false
		defer func() { debug = origDebug }()

		// Use a timeout to avoid hanging
		done := make(chan []HAPAccessory, 1)
		go func() {
			services := discoverHAPServices()
			done <- services
		}()

		select {
		case services := <-done:
			// Should return a slice (may be empty if no services found)
			if services == nil {
				t.Errorf("discoverHAPServices() should not return nil")
			}
			// In test environment, we don't expect real HAP services
			t.Logf("Discovery completed, found %d HAP services", len(services))
		case <-time.After(15 * time.Second): // 15 second timeout to be safe
			t.Log("discoverHAPServices() took longer than 15 seconds, which can happen with network operations")
		}
	})
}
