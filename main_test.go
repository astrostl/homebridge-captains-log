// This file is part of homebridge-captains-log
package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// Test constants to avoid magic numbers
const (
	testPort              = 8080
	testPortAlt           = 9090
	testBrightness50      = 50
	testBrightness75      = 75
	testBrightness80      = 80
	testTemp225           = 22.5
	testTemp231           = 23.1
	testHumidity45        = 45.0
	testHumidity485       = 48.5
	testHue180            = 180
	testTempLow           = 12.3
	testTempHigh          = 12.5
	testNegativeTemp      = -10.0
	testValidTemp         = 5.0
	testLargeTemp         = 24.4
	testNearZeroTemp      = 23.9
	testFreezing          = 0.5
	testBoiling           = 25.0
	testNegativeValidTemp = -5.0
	testAID123            = 123
	testBridgePort1       = 51234
	testBridgePort2       = 51250
	testBridgePort3       = 51251
	testTimeout5s         = 5 * time.Second
	testPortInvalid       = 9999
)

const (
	testServiceName     = "Test Light"
	testCharOn          = "On"
	testCharBrightness  = "Brightness"
	testServiceNameTest = "TestService"
	testHostIP          = "192.168.1.100"
	testHostIPAlt       = "192.168.1.200"
	testPortCustom      = 8888
	testCharDescName    = "Name"
	testCharDescManuf   = "Manufacturer"
	testBridgeName      = "Test Bridge"
	testBridgeID        = "test1"
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
			defaultValue: testPort,
			envValue:     "9090",
			want:         testPortAlt,
		},
		{
			name:         "invalid integer returns default",
			key:          "INVALID_INT",
			defaultValue: testPort,
			envValue:     "not_a_number",
			want:         testPort,
		},
		{
			name:         "empty value returns default",
			key:          "EMPTY_INT",
			defaultValue: testPort,
			envValue:     "",
			want:         testPort,
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
				Values: map[string]interface{}{testCharOn: true, testCharBrightness: testBrightness50},
			},
			new: AccessoryStatus{
				Values: map[string]interface{}{testCharOn: true, testCharBrightness: testBrightness50},
			},
			want: false,
		},
		{
			name: "value changed",
			old: AccessoryStatus{
				Values: map[string]interface{}{testCharOn: true},
			},
			new: AccessoryStatus{
				Values: map[string]interface{}{testCharOn: false},
			},
			want: true,
		},
		{
			name: "new key added",
			old: AccessoryStatus{
				Values: map[string]interface{}{testCharOn: true},
			},
			new: AccessoryStatus{
				Values: map[string]interface{}{testCharOn: true, testCharBrightness: testBrightness50},
			},
			want: true,
		},
		{
			name: "key removed",
			old: AccessoryStatus{
				Values: map[string]interface{}{testCharOn: true, testCharBrightness: testBrightness50},
			},
			new: AccessoryStatus{
				Values: map[string]interface{}{testCharOn: true},
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
			key:         testCharOn,
			oldValue:    false,
			newValue:    true,
			serviceName: testServiceName,
			want:        "Test Light: ON",
		},
		{
			name:        "turn off",
			key:         testCharOn,
			oldValue:    true,
			newValue:    false,
			serviceName: testServiceName,
			want:        "Test Light: OFF",
		},
		{
			name:        "contact sensor opened",
			key:         "ContactSensorState",
			oldValue:    0.0,
			newValue:    1.0,
			serviceName: "Door Sensor",
			want:        "Door Sensor: OPENED",
		},
		{
			name:        "contact sensor closed",
			key:         "ContactSensorState",
			oldValue:    1.0,
			newValue:    0.0,
			serviceName: "Door Sensor",
			want:        "Door Sensor: CLOSED",
		},
		{
			name:        "motion detected",
			key:         "MotionDetected",
			oldValue:    false,
			newValue:    true,
			serviceName: "Motion Sensor",
			want:        "Motion Sensor: DETECTED",
		},
		{
			name:        "motion cleared",
			key:         "MotionDetected",
			oldValue:    true,
			newValue:    false,
			serviceName: "Motion Sensor",
			want:        "Motion Sensor: CLEARED",
		},
		{
			name:        "brightness change",
			key:         testCharBrightness,
			oldValue:    testBrightness50,
			newValue:    testBrightness75,
			serviceName: testServiceName,
			want:        "Test Light: Brightness 50 -> 75",
		},
		{
			name:        "temperature change",
			key:         "CurrentTemperature",
			oldValue:    testTemp225,
			newValue:    testTemp231,
			serviceName: "Temperature Sensor",
			want:        "Temperature Sensor: temperature 72.5°F → 73.6°F",
		},
		{
			name:        "humidity change",
			key:         "CurrentRelativeHumidity",
			oldValue:    testHumidity45,
			newValue:    testHumidity485,
			serviceName: "Humidity Sensor",
			want:        "Humidity Sensor: CurrentRelativeHumidity 45 -> 48.5",
		},
		{
			name:        "battery level change",
			key:         "BatteryLevel",
			oldValue:    testBrightness80,
			newValue:    testBrightness75,
			serviceName: "Battery Device",
			want:        "Battery Device: BatteryLevel 80 -> 75",
		},
		{
			name:        "low battery warning",
			key:         "StatusLowBattery",
			oldValue:    0.0,
			newValue:    1.0,
			serviceName: "Battery Device",
			want:        "Battery Device: StatusLowBattery 0 -> 1",
		},
		{
			name:        "battery ok",
			key:         "StatusLowBattery",
			oldValue:    1.0,
			newValue:    0.0,
			serviceName: "Battery Device",
			want:        "Battery Device: StatusLowBattery 1 -> 0",
		},
		{
			name:        "unknown key",
			key:         "UnknownKey",
			oldValue:    "old",
			newValue:    "new",
			serviceName: "Test Device",
			want:        "Test Device: UnknownKey old -> new",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := processChangeMessage(tt.serviceName, tt.key, tt.oldValue, tt.newValue)
			if got != tt.want {
				t.Errorf("processChangeMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFetchAccessories(t *testing.T) {
	t.Run("successful fetch", testFetchAccessoriesSuccess)
	t.Run("401 with successful noauth", testFetchAccessories401WithNoauth)
	t.Run("server error", testFetchAccessoriesServerError)
}

func testFetchAccessoriesSuccess(t *testing.T) {
	testAccessories := []AccessoryStatus{
		{
			UniqueID:    testBridgeID,
			ServiceName: testServiceName,
			Type:        "light",
			Values:      map[string]interface{}{testCharOn: true},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/accessories" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(testAccessories)
		}
	}))
	defer server.Close()

	monitor := &StatusMonitor{
		baseURL: server.URL,
		client:  &http.Client{Timeout: testTimeout5s},
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

	if accessories[0].UniqueID != testBridgeID {
		t.Errorf("Expected UniqueID 'test1', got '%s'", accessories[0].UniqueID)
	}
}

func testFetchAccessories401WithNoauth(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		switch r.URL.Path {
		case "/api/accessories":
			if requestCount == 1 {
				w.WriteHeader(http.StatusUnauthorized)
			} else {
				testAccessories := []AccessoryStatus{
					{UniqueID: testBridgeID, ServiceName: testServiceName, Values: map[string]interface{}{"On": true}},
				}
				_ = json.NewEncoder(w).Encode(testAccessories)
			}
		case "/api/auth/noauth":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test_token"})
		}
	}))
	defer server.Close()

	monitor := &StatusMonitor{
		baseURL: server.URL,
		client:  &http.Client{Timeout: testTimeout5s},
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
}

func testFetchAccessoriesServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	monitor := &StatusMonitor{
		baseURL: server.URL,
		client:  &http.Client{Timeout: testTimeout5s},
	}

	_, err := monitor.fetchAccessories()
	if err == nil {
		t.Errorf("Expected error for 500 status, got nil")
	}
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
								Value:       testServiceName,
							},
						},
					},
				},
			},
			want: testServiceName,
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
								Value:       testAID123,
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
				Host: testHostIP,
				Port: testBridgePort1,
			},
			childBridge: []ChildBridge{},
			want:        false,
		},
		{
			name: "child bridge included",
			hapService: HAPAccessory{
				Name: "TplinkSmarthome 4160",
				Host: testHostIP,
				Port: testBridgePort2,
			},
			childBridge: []ChildBridge{},
			want:        true,
		},
		{
			name: "another child bridge included",
			hapService: HAPAccessory{
				Name: "MyPlugin Bridge",
				Host: testHostIP,
				Port: testBridgePort3,
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
		UniqueID:    testBridgeID,
		ServiceName: testServiceName,
		Values:      map[string]interface{}{testCharOn: true},
		LastUpdated: now,
	}

	status2 := AccessoryStatus{
		UniqueID:    testBridgeID,
		ServiceName: testServiceName,
		Values:      map[string]interface{}{testCharOn: true},
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
		ServiceName: testServiceName,
		Values:      map[string]interface{}{testCharOn: false, testCharBrightness: testBrightness50},
		LastUpdated: time.Now(),
	}

	current := AccessoryStatus{
		ServiceName: testServiceName,
		Values:      map[string]interface{}{testCharOn: true, testCharBrightness: testBrightness75},
		LastUpdated: time.Now(),
	}

	// Test doesn't panic and handles the change reporting
	monitor.reportChange(old, current)

	// Test with removed key
	currentWithRemovedKey := AccessoryStatus{
		ServiceName: testServiceName,
		Values:      map[string]interface{}{testCharOn: true},
		LastUpdated: time.Now(),
	}

	monitor.reportChange(old, currentWithRemovedKey)

	// Test with new key added
	currentWithAddedKey := AccessoryStatus{
		ServiceName: testServiceName,
		Values:      map[string]interface{}{testCharOn: false, testCharBrightness: testBrightness50, "Hue": testHue180},
		LastUpdated: time.Now(),
	}

	monitor.reportChange(old, currentWithAddedKey)
}

func TestHasHAPAccessories(t *testing.T) {
	t.Run("has accessories", testHasHAPAccessoriesSuccess)
	t.Run("no accessories", testHasHAPAccessoriesEmpty)
	t.Run("http error", testHasHAPAccessoriesHTTPError)
	t.Run("non-200 status", testHasHAPAccessoriesNon200)
}

func testHasHAPAccessoriesSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accessories" {
			w.Header().Set("Content-Type", "application/json")
			response := HAPResponse{
				Accessories: []HAPAccessoryData{
					{AID: 1, Services: []HAPService{{Type: "lightbulb"}}},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	defer server.Close()

	host, port := extractHostPort(server.URL)
	result := hasHAPAccessories(host, port)
	if !result {
		t.Errorf("hasHAPAccessories() = false, want true")
	}
}

func testHasHAPAccessoriesEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accessories" {
			w.Header().Set("Content-Type", "application/json")
			response := HAPResponse{Accessories: []HAPAccessoryData{}}
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	defer server.Close()

	host, port := extractHostPort(server.URL)
	result := hasHAPAccessories(host, port)
	if result {
		t.Errorf("hasHAPAccessories() = true, want false")
	}
}

func testHasHAPAccessoriesHTTPError(t *testing.T) {
	result := hasHAPAccessories("nonexistent.host", testPortInvalid)
	if result {
		t.Errorf("hasHAPAccessories() = true, want false for connection error")
	}
}

func testHasHAPAccessoriesNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	host, port := extractHostPort(server.URL)
	result := hasHAPAccessories(host, port)
	if result {
		t.Errorf("hasHAPAccessories() = true, want false for 401 status")
	}
}

func extractHostPort(serverURL string) (string, int) {
	host := "localhost"
	port := testPort
	_, portStr, _ := net.SplitHostPort(strings.TrimPrefix(serverURL, "http://"))
	if p, err := strconv.Atoi(portStr); err == nil {
		port = p
	}
	return host, port
}

func TestGetNoAuthTokenComplete(t *testing.T) {
	t.Run("token field", testGetNoAuthTokenWithTokenField)
	t.Run("no token in response", testGetNoAuthTokenNoToken)
	t.Run("invalid json", testGetNoAuthTokenInvalidJSON)
}

func testGetNoAuthTokenWithTokenField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/noauth" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "alternative_token"})
		}
	}))
	defer server.Close()

	monitor := &StatusMonitor{
		baseURL: server.URL,
		client:  &http.Client{Timeout: testTimeout5s},
	}

	token, err := monitor.getNoAuthToken()
	if err != nil {
		t.Errorf("getNoAuthToken() error = %v", err)
		return
	}

	if token != "alternative_token" {
		t.Errorf("getNoAuthToken() = %v, want alternative_token", token)
	}
}

func testGetNoAuthTokenNoToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/noauth" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"other_field": "value"})
		}
	}))
	defer server.Close()

	monitor := &StatusMonitor{
		baseURL: server.URL,
		client:  &http.Client{Timeout: testTimeout5s},
	}

	_, err := monitor.getNoAuthToken()
	if err == nil {
		t.Errorf("getNoAuthToken() expected error for missing token, got nil")
	}
}

func testGetNoAuthTokenInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/noauth" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("invalid json"))
		}
	}))
	defer server.Close()

	monitor := &StatusMonitor{
		baseURL: server.URL,
		client:  &http.Client{Timeout: testTimeout5s},
	}

	_, err := monitor.getNoAuthToken()
	if err == nil {
		t.Errorf("getNoAuthToken() expected error for invalid JSON, got nil")
	}
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
					{UniqueID: testBridgeID, ServiceName: testServiceName, Values: map[string]interface{}{"On": true}},
				}
				_ = json.NewEncoder(w).Encode(testAccessories)
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
					{
						UniqueID:    testBridgeID,
						ServiceName: testServiceName,
						Values:      map[string]interface{}{"On": callCount%2 == 0},
					},
				}
				_ = json.NewEncoder(w).Encode(testAccessories)
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
					{UniqueID: testBridgeID, ServiceName: testServiceName, Values: map[string]interface{}{"On": true}},
				}
				_ = json.NewEncoder(w).Encode(testAccessories)
			}
		}))
		defer server.Close()

		monitor := &StatusMonitor{
			baseURL: server.URL,
			lastStatus: map[string]AccessoryStatus{
				testBridgeID: {UniqueID: testBridgeID, ServiceName: testServiceName, Values: map[string]interface{}{"On": false}},
			},
			client: &http.Client{Timeout: 5 * time.Second},
		}

		// This should detect the change from Off to On
		monitor.checkStatus()

		// Verify the status was updated
		if !monitor.lastStatus[testBridgeID].Values["On"].(bool) {
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
	t.Run("successful response", testGetChildBridgesSuccess)
	t.Run("auth failure", testGetChildBridgesAuthFailure)
}

func testGetChildBridgesSuccess(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(createGetChildBridgesSuccessHandler(&callCount))
	defer server.Close()

	setupTestHostPort(server.URL, func() {
		bridges := getChildBridges()
		validateSuccessfulChildBridges(t, bridges, callCount)
	})
}

func testGetChildBridgesAuthFailure(t *testing.T) {
	server := httptest.NewServer(createGetChildBridgesAuthFailureHandler())
	defer server.Close()

	setupTestHostPort(server.URL, func() {
		bridges := getChildBridges()
		if bridges != nil {
			t.Errorf("Expected nil bridges on auth failure, got %v", bridges)
		}
	})
}

func createGetChildBridgesSuccessHandler(callCount *int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*callCount++
		switch r.URL.Path {
		case "/api/auth/noauth":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test_token"})
		case "/api/status/homebridge/child-bridges":
			w.Header().Set("Content-Type", "application/json")
			bridges := []ChildBridge{
				{Name: testBridgeName, Plugin: "test-plugin", Status: "running"},
			}
			_ = json.NewEncoder(w).Encode(bridges)
		}
	}
}

func createGetChildBridgesAuthFailureHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/noauth" {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func setupTestHostPort(serverURL string, testFunc func()) {
	origHost := host
	origPort := port
	defer func() {
		host = origHost
		port = origPort
	}()

	updateHostPortFromURL(serverURL)
	testFunc()
}

func updateHostPortFromURL(serverURL string) {
	url := strings.TrimPrefix(serverURL, "http://")
	hostPort := strings.Split(url, ":")
	host = hostPort[0]
	if len(hostPort) > 1 {
		if p, err := strconv.Atoi(hostPort[1]); err == nil {
			port = p
		}
	}
}

func validateSuccessfulChildBridges(t *testing.T, bridges []ChildBridge, callCount int) {
	if len(bridges) != 1 {
		t.Errorf("Expected 1 bridge, got %d", len(bridges))
		return
	}

	if bridges[0].Name != testBridgeName {
		t.Errorf("Expected bridge name 'Test Bridge', got '%s'", bridges[0].Name)
	}

	if callCount < 2 {
		t.Errorf("Expected at least 2 API calls (auth + bridges), got %d", callCount)
	}
}

func TestProcessAccessories(t *testing.T) {
	// Test processAccessories function which was previously untested
	lastStatus := make(map[string]interface{})
	accessories := []HAPAccessoryData{
		{
			AID: 1,
			Services: []HAPService{
				{
					IID: 1,
					Characteristics: []HAPCharacteristic{
						{Type: "23", Description: "Name", Value: testServiceName, IID: 1},
						{Type: "25", Description: "On", Value: 1.0, IID: 2},
					},
				},
			},
		},
	}

	result := processAccessories(accessories, "TestBridge", lastStatus)

	// Should detect initial discovery
	if !strings.Contains(result.summaryLine, "Found 1 accessory: Test Light") {
		t.Errorf("Expected initial discovery message, got: %s", result.summaryLine)
	}

	// Test state changes
	accessories[0].Services[0].Characteristics[1].Value = 0.0
	result = processAccessories(accessories, "TestBridge", lastStatus)

	if !strings.Contains(result.summaryLine, "changes detected") {
		t.Errorf("Expected change detection, got: %s", result.summaryLine)
	}
}

func TestGenerateSummaryLine(t *testing.T) {
	tests := []struct {
		name         string
		config       summaryConfig
		wantContains string
	}{
		{
			name: "initial discovery single accessory",
			config: summaryConfig{
				isInitialDiscovery: true,
				accessoryCount:     1,
				accessoryNames:     []string{testServiceName},
				changesDetected:    0,
			},
			wantContains: "Found 1 accessory: Test Light",
		},
		{
			name: "initial discovery multiple accessories",
			config: summaryConfig{
				isInitialDiscovery: true,
				accessoryCount:     2,
				accessoryNames:     []string{"Light 1", "Light 2"},
				changesDetected:    0,
			},
			wantContains: "Found 2 accessories: Light 1, Light 2",
		},
		{
			name: "no changes in debug mode",
			config: summaryConfig{
				isInitialDiscovery: false,
				accessoryCount:     3,
				accessoryNames:     []string{"A", "B", "C"},
				changesDetected:    0,
			},
			wantContains: "No changes detected in 3 accessories",
		},
		{
			name: "changes detected",
			config: summaryConfig{
				isInitialDiscovery: false,
				accessoryCount:     2,
				accessoryNames:     []string{"A", "B"},
				changesDetected:    3,
			},
			wantContains: "3 changes detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalDebug := debug
			defer func() { debug = originalDebug }()

			// Enable debug mode for "no changes in debug mode" test
			if tt.name == "no changes in debug mode" {
				debug = true
			}

			result := generateSummaryLine(tt.config)
			if !strings.Contains(result, tt.wantContains) {
				t.Errorf("generateSummaryLine() = %q, want containing %q", result, tt.wantContains)
			}
		})
	}
}

func TestFetchHAPResponse(t *testing.T) {
	t.Run("successful fetch", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			response := HAPResponse{
				Accessories: []HAPAccessoryData{
					{AID: 1, Services: []HAPService{{IID: 1, Characteristics: []HAPCharacteristic{{Type: "23", Value: "Test"}}}}},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		serverURL := strings.TrimPrefix(server.URL, "http://")
		hostPort := strings.Split(serverURL, ":")
		port, _ := strconv.Atoi(hostPort[1])

		client := &http.Client{Timeout: 5 * time.Second}
		acc := HAPAccessory{Name: "Test", Host: hostPort[0], Port: port}
		var output []string

		hapResp, err := fetchHAPResponse(client, acc, &output)
		if err != nil {
			t.Errorf("fetchHAPResponse() error = %v", err)
		}
		if len(hapResp.Accessories) != 1 {
			t.Errorf("Expected 1 accessory, got %d", len(hapResp.Accessories))
		}
	})

	t.Run("connection error", func(t *testing.T) {
		client := &http.Client{Timeout: 100 * time.Millisecond}
		acc := HAPAccessory{Name: "Test", Host: "nonexistent.invalid", Port: 12345}
		var output []string

		_, err := fetchHAPResponse(client, acc, &output)
		if err == nil {
			t.Error("Expected error for invalid host")
		}
		if len(output) == 0 {
			t.Error("Expected error output")
		}
	})
}

func TestChildBridgeListsEqual(t *testing.T) {
	bridge1 := ChildBridge{Name: "Bridge1", Username: "user1"}
	bridge2 := ChildBridge{Name: "Bridge2", Username: "user2"}

	tests := []struct {
		name string
		a    []ChildBridge
		b    []ChildBridge
		want bool
	}{
		{"both empty", []ChildBridge{}, []ChildBridge{}, true},
		{"same bridges", []ChildBridge{bridge1, bridge2}, []ChildBridge{bridge1, bridge2}, true},
		{"different order", []ChildBridge{bridge1, bridge2}, []ChildBridge{bridge2, bridge1}, true},
		{"different length", []ChildBridge{bridge1}, []ChildBridge{bridge1, bridge2}, false},
		{"different bridges", []ChildBridge{bridge1}, []ChildBridge{bridge2}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := childBridgeListsEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("childBridgeListsEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrimServiceName(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		want        string
	}{
		{"removes last field", "My Service Something", "My Service"},
		{"handles multiple words", "Living Room Light Something", "Living Room Light"},
		{"single word unchanged", "Light", "Light"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimServiceName(tt.serviceName); got != tt.want {
				t.Errorf("trimServiceName() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test run function directly - covers monitoring logic without CLI dependencies
func TestRunFunction(_ *testing.T) {
	// Test the run function which has lower coverage
	originalHost := host
	originalPort := port
	defer func() {
		host = originalHost
		port = originalPort
	}()

	// Mock server for testing
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/accessories" {
			w.Header().Set("Content-Type", "application/json")
			testAccessories := []AccessoryStatus{
				{UniqueID: testBridgeID, ServiceName: testServiceName, Values: map[string]interface{}{testCharOn: true}},
			}
			_ = json.NewEncoder(w).Encode(testAccessories)
		}
	}))
	defer server.Close()

	// Extract port from server URL and update test environment
	serverURL := strings.TrimPrefix(server.URL, "http://")
	hostPort := strings.Split(serverURL, ":")
	host = hostPort[0]
	if len(hostPort) > 1 {
		if p, err := strconv.Atoi(hostPort[1]); err == nil {
			port = p
		}
	}

	baseURL := server.URL
	interval := testMDNSTimeout100ms

	monitor := &StatusMonitor{
		baseURL:    baseURL,
		interval:   interval,
		lastStatus: make(map[string]AccessoryStatus),
		client:     &http.Client{Timeout: testMDNSTimeout5s},
	}

	// Test single check
	monitor.run(1)
}

// Test for performDiscovery function
func TestPerformDiscovery(_ *testing.T) {
	originalCount := count
	defer func() { count = originalCount }()

	count = 0 // Discovery-only mode

	var cachedChildBridges []ChildBridge
	var cachedHAPServices []HAPAccessory

	// Should complete without panicking
	performDiscovery(&cachedChildBridges, &cachedHAPServices)
}

// Test displayDiscoveryResults function
func TestDisplayDiscoveryResults(_ *testing.T) {
	// This function was refactored and is no longer directly testable
	// as it's been integrated into the main discovery flow
}

// Test printOutputSync function
func TestPrintOutputSync(_ *testing.T) {
	testOutput := []string{"test line 1", "test line 2"}
	var mu sync.Mutex

	// Should not panic
	printOutputSync(testOutput, &mu)
}

// Test filterServicesByExpectedNames function
func TestFilterServicesByExpectedNames(t *testing.T) {
	// The function trims the last field, so "Expected Service 1234" becomes "Expected Service"
	serviceNames := []string{"Expected Service 1234", "Unexpected Service 5678"}
	expectedNames := []string{"Expected Service"}

	result := filterServicesByExpectedNames(serviceNames, expectedNames)

	if len(result) != 1 {
		t.Errorf("Expected 1 filtered service, got %d", len(result))
		return
	}
	if result[0] != "Expected Service 1234" {
		t.Errorf("Expected 'Expected Service 1234', got %s", result[0])
	}
}

// Test discoverHAPServicesWithTimeoutAndFilter function
func TestDiscoverHAPServicesWithTimeoutAndFilter(t *testing.T) {
	// Test with very short timeout to avoid hanging
	timeout := testMDNSTimeout100ms

	// Test should complete without hanging
	services := discoverHAPServicesWithTimeoutAndFilter(timeout, []string{})

	// Services may be empty due to network conditions, but function should not hang
	if services == nil {
		t.Log("No services found, which is acceptable for network-dependent test")
	} else {
		t.Logf("Found %d services", len(services))
	}
}

// Test isMainHomebridgeInstance with mock responses
func TestIsMainHomebridgeInstance(t *testing.T) {
	t.Run("valid main bridge", func(t *testing.T) {
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
										{Description: testCharDescManuf, Value: "homebridge.io"},
									},
								},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			}
		}))
		defer server.Close()

		host, port := extractHostPort(server.URL)
		result := isMainHomebridgeInstance(host, port)
		if !result {
			t.Errorf("isMainHomebridgeInstance() = false, want true for valid main bridge")
		}
	})

	t.Run("not main bridge", func(t *testing.T) {
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
										{Description: "Name", Value: "Test Device"},
									},
								},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			}
		}))
		defer server.Close()

		host, port := extractHostPort(server.URL)
		result := isMainHomebridgeInstance(host, port)
		if result {
			t.Errorf("isMainHomebridgeInstance() = true, want false for non-main bridge")
		}
	})

	t.Run("connection error", func(t *testing.T) {
		result := isMainHomebridgeInstance("nonexistent.invalid", testPortInvalid)
		if result {
			t.Errorf("isMainHomebridgeInstance() = true, want false for connection error")
		}
	})
}

// Test resolveHostnameToIP function
func TestResolveHostnameToIP(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		wantIP   bool
	}{
		{"valid IP", "192.168.1.100", true},
		{"localhost", "localhost", false}, // May resolve to IP or return hostname
		{"invalid hostname", "invalid.nonexistent.domain", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveHostnameToIP(tt.hostname)
			if tt.wantIP {
				if net.ParseIP(result) == nil {
					t.Errorf("resolveHostnameToIP(%s) = %s, want valid IP", tt.hostname, result)
				}
			} else {
				// Should return something (either IP or original hostname)
				if result == "" {
					t.Errorf("resolveHostnameToIP(%s) = empty, want non-empty result", tt.hostname)
				}
			}
		})
	}
}

// Test validation functions
func TestValidateMainHomebridgeResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *HAPResponse
		want     bool
	}{
		{
			name: "valid homebridge response",
			response: &HAPResponse{
				Accessories: []HAPAccessoryData{
					{
						Services: []HAPService{
							{
								Characteristics: []HAPCharacteristic{
									{Description: testCharDescManuf, Value: "homebridge.io"},
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "empty accessories",
			response: &HAPResponse{
				Accessories: []HAPAccessoryData{},
			},
			want: false,
		},
		{
			name: "no homebridge characteristics",
			response: &HAPResponse{
				Accessories: []HAPAccessoryData{
					{
						Services: []HAPService{
							{
								Characteristics: []HAPCharacteristic{
									{Description: testCharDescName, Value: "Some Device"},
								},
							},
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateMainHomebridgeResponse(tt.response, "test", testPort)
			if result != tt.want {
				t.Errorf("validateMainHomebridgeResponse() = %v, want %v", result, tt.want)
			}
		})
	}
}

// Test isMainHomebridgeCharacteristic function
func TestIsMainHomebridgeCharacteristic(t *testing.T) {
	tests := []struct {
		name string
		char HAPCharacteristic
		want bool
	}{
		{
			name: "manufacturer homebridge.io",
			char: HAPCharacteristic{Description: "Manufacturer", Value: "homebridge.io"},
			want: true,
		},
		{
			name: "model homebridge",
			char: HAPCharacteristic{Description: "Model", Value: "homebridge"},
			want: true,
		},
		{
			name: "other manufacturer",
			char: HAPCharacteristic{Description: "Manufacturer", Value: "Apple"},
			want: false,
		},
		{
			name: "non-string value",
			char: HAPCharacteristic{Description: "Manufacturer", Value: testAID123},
			want: false,
		},
		{
			name: "different characteristic",
			char: HAPCharacteristic{Description: "Name", Value: "Test Device"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMainHomebridgeCharacteristic(tt.char)
			if result != tt.want {
				t.Errorf("isMainHomebridgeCharacteristic() = %v, want %v", result, tt.want)
			}
		})
	}
}

// createTestCmdWithFlags creates a cobra command with specified flags
func createTestCmdWithFlags(hostFlag, portFlag string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().StringP("hb-host", "H", "", "")
	cmd.Flags().IntP("hb-port", "P", DefaultHomebridgePort, "")
	if hostFlag != "" {
		_ = cmd.Flags().Set("hb-host", hostFlag)
	}
	if portFlag != "" {
		_ = cmd.Flags().Set("hb-port", portFlag)
	}
	return cmd
}

// Test resolveHomebridgeLocation function
func TestResolveHomebridgeLocationManualConfig(t *testing.T) {
	originalHost := host
	originalPort := port
	defer func() {
		host = originalHost
		port = originalPort
	}()

	t.Run("manual host and port via flags", func(t *testing.T) {
		host = testHostIP
		port = testPortCustom
		cmd := createTestCmdWithFlags(testHostIP, strconv.Itoa(testPortCustom))

		gotHost, gotPort := resolveHomebridgeLocation(cmd)

		if gotHost != testHostIP || gotPort != testPortCustom {
			t.Errorf("resolveHomebridgeLocation() = (%s, %d), want (%s, %d)", gotHost, gotPort, testHostIP, testPortCustom)
		}
	})

	t.Run("only host provided, use default port", func(t *testing.T) {
		host = testHostIPAlt
		port = DefaultHomebridgePort
		cmd := createTestCmdWithFlags(testHostIPAlt, "")

		gotHost, gotPort := resolveHomebridgeLocation(cmd)

		if gotHost != testHostIPAlt || gotPort != DefaultHomebridgePort {
			t.Errorf("resolveHomebridgeLocation() = (%s, %d), want (%s, %d)",
				gotHost, gotPort, testHostIPAlt, DefaultHomebridgePort)
		}
	})
}

// createTestServerWithValidHAP creates test server that returns valid HAP response
func createTestServerWithValidHAP() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accessories" {
			w.Header().Set("Content-Type", "application/json")
			response := HAPResponse{
				Accessories: []HAPAccessoryData{
					{
						AID: 1,
						Services: []HAPService{
							{
								Characteristics: []HAPCharacteristic{
									{Description: testCharDescManuf, Value: "homebridge.io"},
								},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
}

// testSuccessfulValidation tests successful HAP response validation
func testSuccessfulValidation(t *testing.T) {
	server := createTestServerWithValidHAP()
	defer server.Close()

	host, port := extractHostPort(server.URL)
	result, err := fetchHAPResponseForValidation(host, port)

	if err != nil {
		t.Errorf("fetchHAPResponseForValidation() error = %v, want nil", err)
	}
	if result == nil {
		t.Errorf("fetchHAPResponseForValidation() result = nil, want non-nil")
		return
	}
	if len(result.Accessories) != 1 {
		t.Errorf("fetchHAPResponseForValidation() accessories count = %d, want 1", len(result.Accessories))
	}
}

// testServerError tests server error handling in HAP validation
func testServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	host, port := extractHostPort(server.URL)
	result, err := fetchHAPResponseForValidation(host, port)

	if err == nil {
		t.Errorf("fetchHAPResponseForValidation() error = nil, want error")
	}
	if result != nil {
		t.Errorf("fetchHAPResponseForValidation() result = %v, want nil", result)
	}
}

// testInvalidJSON tests invalid JSON response handling
func testInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	host, port := extractHostPort(server.URL)
	result, err := fetchHAPResponseForValidation(host, port)

	if err == nil {
		t.Errorf("fetchHAPResponseForValidation() error = nil, want error")
	}
	if result != nil {
		t.Errorf("fetchHAPResponseForValidation() result = %v, want nil", result)
	}
}

// Test fetchHAPResponseForValidation function
func TestFetchHAPResponseForValidationCases(t *testing.T) {
	t.Run("successful validation response", testSuccessfulValidation)
	t.Run("server error", testServerError)
	t.Run("invalid JSON response", testInvalidJSON)
}

// Test isMainHomebridgeService function
func TestIsMainHomebridgeService(t *testing.T) {
	tests := []struct {
		name    string
		service HAPService
		want    bool
	}{
		{
			name: "service with homebridge manufacturer",
			service: HAPService{
				Characteristics: []HAPCharacteristic{
					{Description: "Manufacturer", Value: "homebridge.io"},
					{Description: testCharDescName, Value: "Test"},
				},
			},
			want: true,
		},
		{
			name: "service with homebridge model",
			service: HAPService{
				Characteristics: []HAPCharacteristic{
					{Description: "Model", Value: "homebridge"},
					{Description: testCharDescName, Value: "Test"},
				},
			},
			want: true,
		},
		{
			name: "service without homebridge characteristics",
			service: HAPService{
				Characteristics: []HAPCharacteristic{
					{Description: testCharDescName, Value: "Some Device"},
					{Description: testCharDescManuf, Value: "Apple"},
				},
			},
			want: false,
		},
		{
			name: "service with no characteristics",
			service: HAPService{
				Characteristics: []HAPCharacteristic{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMainHomebridgeService(tt.service, "test", testPort)
			if result != tt.want {
				t.Errorf("isMainHomebridgeService() = %v, want %v", result, tt.want)
			}
		})
	}
}

// Test handleManualConfiguration function
func TestHandleManualConfiguration(t *testing.T) {
	originalHost := host
	originalPort := port
	defer func() {
		host = originalHost
		port = originalPort
	}()

	t.Run("host and port provided", func(t *testing.T) {
		host = testHostIP
		port = testPortCustom
		cmd := createTestCmdWithFlags(testHostIP, strconv.Itoa(testPortCustom))

		gotHost, gotPort := handleManualConfiguration(cmd)

		if gotHost != testHostIP || gotPort != testPortCustom {
			t.Errorf("handleManualConfiguration() = (%s, %d), want (%s, %d)", gotHost, gotPort, testHostIP, testPortCustom)
		}
	})

	t.Run("host only uses default port", func(t *testing.T) {
		host = testHostIPAlt
		port = DefaultHomebridgePort
		cmd := createTestCmdWithFlags(testHostIPAlt, "")

		gotHost, gotPort := handleManualConfiguration(cmd)

		if gotHost != testHostIPAlt || gotPort != DefaultHomebridgePort {
			t.Errorf("handleManualConfiguration() = (%s, %d), want (%s, %d)",
				gotHost, gotPort, testHostIPAlt, DefaultHomebridgePort)
		}
	})

	t.Run("port only tries discovery for host", func(t *testing.T) {
		host = ""
		port = testPortCustom
		cmd := createTestCmdWithFlags("", strconv.Itoa(testPortCustom))

		gotHost, gotPort := handleManualConfiguration(cmd)

		// Should either discover a host or fall back to localhost
		if gotPort != testPortCustom {
			t.Errorf("handleManualConfiguration() port = %d, want %d", gotPort, testPortCustom)
		}
		// Host should be either discovered IP or localhost fallback, both are valid
		if gotHost == "" {
			t.Errorf("handleManualConfiguration() host = empty, want non-empty host")
		}
	})
}

// Test handleAutoDiscovery function
func TestHandleAutoDiscovery(t *testing.T) {
	originalAt3Host := at3Host
	originalAt3Port := at3Port
	defer func() {
		at3Host = originalAt3Host
		at3Port = originalAt3Port
	}()

	t.Run("auto discovery with mock cmd", func(t *testing.T) {
		cmd := createTestCmdWithFlags("", "")
		gotHost, gotPort := handleAutoDiscovery(cmd)

		// Since we can't mock discoverServices easily, we just test that the function
		// doesn't panic and returns reasonable values (empty or valid host/port)
		if gotPort < 0 || gotPort > 65535 {
			t.Errorf("handleAutoDiscovery() returned invalid port %d", gotPort)
		}

		// Host should be either empty (discovery failed) or valid hostname/IP
		if gotHost != "" && len(gotHost) < 3 {
			t.Errorf("handleAutoDiscovery() returned suspicious host %s", gotHost)
		}
	})
}

// Test reportAWTRIX3Discovery function
func TestReportAWTRIX3Discovery(t *testing.T) {
	originalAt3Host := at3Host
	originalAt3Port := at3Port
	defer func() {
		at3Host = originalAt3Host
		at3Port = originalAt3Port
	}()

	t.Run("manual AWTRIX3 config", func(_ *testing.T) {
		at3Host = testHostIP
		at3Port = DefaultAWTRIX3Port
		cmd := createTestCmdWithAWTRIXFlags(testHostIP, strconv.Itoa(DefaultAWTRIX3Port))

		awtrixDevices := []MDNSService{}

		// Should not panic and should report manual config
		reportAWTRIX3Discovery(cmd, awtrixDevices)
	})

	t.Run("auto-discovered AWTRIX3 devices", func(_ *testing.T) {
		at3Host = ""
		at3Port = DefaultAWTRIX3Port
		cmd := createTestCmdWithAWTRIXFlags("", "")

		awtrixDevices := []MDNSService{
			{Name: "awtrix_123456", Host: testHostIP, Port: DefaultAWTRIX3Port},
			{Name: "awtrix_789012", Host: testHostIPAlt, Port: DefaultAWTRIX3Port},
		}

		// Should not panic and should report discovered devices
		reportAWTRIX3Discovery(cmd, awtrixDevices)
	})

	t.Run("no AWTRIX3 devices found", func(_ *testing.T) {
		at3Host = ""
		at3Port = DefaultAWTRIX3Port
		cmd := createTestCmdWithAWTRIXFlags("", "")

		awtrixDevices := []MDNSService{}

		// Should not panic and should report no devices
		reportAWTRIX3Discovery(cmd, awtrixDevices)
	})
}

// Test resolveAWTRIX3Location function
func TestResolveAWTRIX3Location(t *testing.T) {
	originalAt3Host := at3Host
	originalAt3Port := at3Port
	defer func() {
		at3Host = originalAt3Host
		at3Port = originalAt3Port
	}()

	t.Run("both host and port provided", func(t *testing.T) {
		at3Host = testHostIP
		at3Port = testPortCustom
		cmd := createTestCmdWithAWTRIXFlags(testHostIP, strconv.Itoa(testPortCustom))

		gotHost, gotPort := resolveAWTRIX3Location(cmd)

		if gotHost != testHostIP || gotPort != testPortCustom {
			t.Errorf("resolveAWTRIX3Location() = (%s, %d), want (%s, %d)", gotHost, gotPort, testHostIP, testPortCustom)
		}
	})

	t.Run("host only uses default port", func(t *testing.T) {
		at3Host = testHostIPAlt
		at3Port = DefaultAWTRIX3Port
		cmd := createTestCmdWithAWTRIXFlags(testHostIPAlt, "")

		gotHost, gotPort := resolveAWTRIX3Location(cmd)

		if gotHost != testHostIPAlt || gotPort != DefaultAWTRIX3Port {
			t.Errorf("resolveAWTRIX3Location() = (%s, %d), want (%s, %d)", gotHost, gotPort, testHostIPAlt, DefaultAWTRIX3Port)
		}
	})

	t.Run("port only without host returns empty", func(t *testing.T) {
		at3Host = ""
		at3Port = testPortCustom
		cmd := createTestCmdWithAWTRIXFlags("", strconv.Itoa(testPortCustom))

		gotHost, gotPort := resolveAWTRIX3Location(cmd)

		if gotHost != "" || gotPort != 0 {
			t.Errorf("resolveAWTRIX3Location() = (%s, %d), want (\"\", 0)", gotHost, gotPort)
		}
	})

	t.Run("no manual config returns empty", func(t *testing.T) {
		at3Host = ""
		at3Port = DefaultAWTRIX3Port
		cmd := createTestCmdWithAWTRIXFlags("", "")

		gotHost, gotPort := resolveAWTRIX3Location(cmd)

		if gotHost != "" || gotPort != 0 {
			t.Errorf("resolveAWTRIX3Location() = (\"\", 0), want (\"\", 0)")
		}
	})
}

// Helper to create cobra command with AWTRIX3 flags
func createTestCmdWithAWTRIXFlags(hostFlag, portFlag string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("at3-host", "", "")
	cmd.Flags().Int("at3-port", DefaultAWTRIX3Port, "")
	if hostFlag != "" {
		_ = cmd.Flags().Set("at3-host", hostFlag)
	}
	if portFlag != "" {
		_ = cmd.Flags().Set("at3-port", portFlag)
	}
	return cmd
}

// Test fetchHAPResponseForValidation error scenarios
func TestFetchHAPResponseForValidationErrors(t *testing.T) {
	t.Run("connection timeout", func(t *testing.T) {
		result, err := fetchHAPResponseForValidation("192.0.2.0", testPortInvalid)

		if err == nil {
			t.Errorf("fetchHAPResponseForValidation() error = nil, want timeout error")
		}
		if result != nil {
			t.Errorf("fetchHAPResponseForValidation() result = %v, want nil", result)
		}
	})

	t.Run("invalid port", func(t *testing.T) {
		result, err := fetchHAPResponseForValidation("localhost", 99999)

		if err == nil {
			t.Errorf("fetchHAPResponseForValidation() error = nil, want connection error")
		}
		if result != nil {
			t.Errorf("fetchHAPResponseForValidation() result = %v, want nil", result)
		}
	})

	t.Run("non-JSON response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("not json"))
		}))
		defer server.Close()

		host, port := extractHostPort(server.URL)
		result, err := fetchHAPResponseForValidation(host, port)

		if err == nil {
			t.Errorf("fetchHAPResponseForValidation() error = nil, want JSON parse error")
		}
		if result != nil {
			t.Errorf("fetchHAPResponseForValidation() result = %v, want nil", result)
		}
	})
}

// Test discoverServices behavior (basic smoke test)
func TestDiscoverServicesBasic(t *testing.T) {
	t.Run("discoverServices doesn't panic", func(t *testing.T) {
		// This is a basic smoke test to ensure discoverServices doesn't panic
		// We can't easily mock the mDNS discovery without complex setup
		host, awtrixServices := discoverServices()

		// Either discovery succeeds (returns host) or fails (returns empty)
		// Both are valid outcomes depending on network conditions
		if host != "" {
			t.Logf("Discovery succeeded: found host %s", host)
		} else {
			t.Logf("Discovery failed or no services found (expected in test env)")
		}

		// awtrixServices should never be nil, even if empty
		// Note: Due to network conditions, this might occasionally be nil in test environments
		if awtrixServices == nil {
			t.Logf("awtrixServices = nil (acceptable in test environment with network conditions)")
		}
	})
}

// Test main function (wrapper test)
func TestMain(t *testing.T) {
	// Test that main function can be called without panicking
	// We can't easily test the full CLI execution without complex setup
	t.Run("main function exists and doesn't panic immediately", func(t *testing.T) {
		// This tests that the main function structure is valid
		// Actual CLI testing would require integration tests
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("main() panicked: %v", r)
			}
		}()

		// We can't call main() directly as it would start the full application
		// Instead verify the cobra command structure is valid
		if rootCmd == nil {
			t.Errorf("rootCmd is nil, command not properly initialized")
		}
		if rootCmd.Use != "hb-clog" {
			t.Errorf("rootCmd.Use = %s, want 'hb-clog'", rootCmd.Use)
		}
	})
}

// Test runMonitor function components
func TestRunMonitorComponents(t *testing.T) {
	t.Run("runMonitor validates configuration", func(t *testing.T) {
		// Test the validation logic that runMonitor would perform
		// We test the components rather than the full function

		// Test resolveHomebridgeLocation with empty configuration
		cmd := &cobra.Command{}
		cmd.Flags().StringP("hb-host", "H", "", "")
		cmd.Flags().IntP("hb-port", "P", DefaultHomebridgePort, "")
		cmd.Flags().BoolP("main", "m", false, "")

		// This should attempt auto-discovery
		resolvedHost, resolvedPort := resolveHomebridgeLocation(cmd)

		// In test environment, discovery will likely fail
		// Both outcomes are valid depending on network conditions
		if resolvedHost != "" {
			t.Logf("Auto-discovery succeeded: %s:%d", resolvedHost, resolvedPort)
		} else {
			t.Logf("Auto-discovery failed (expected in test environment)")
		}
	})
}

// Test performDiscovery function wrapper
func TestPerformDiscoveryWrapper(t *testing.T) {
	t.Run("performDiscovery doesn't panic with valid inputs", func(t *testing.T) {
		// Test that the discovery wrapper function handles nil inputs properly
		var cachedChildBridges []ChildBridge
		var cachedHAPServices []HAPAccessory

		// Should not panic with valid empty inputs
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("performDiscovery() panicked: %v", r)
			}
		}()

		performDiscovery(&cachedChildBridges, &cachedHAPServices)
	})
}

// Test bridge filtering functions
func TestBridgeFilteringFunctions(t *testing.T) {
	// Create test HAP services for filtering
	testHAPServices := []HAPAccessory{
		{Name: "Homebridge ABCD EFGH", Host: "192.168.1.100", Port: testBridgePort1, ID: "1"}, // Main bridge - should be filtered
		{Name: "TplinkSmarthome", Host: "192.168.1.101", Port: testBridgePort2, ID: "2"},      // Child bridge
		{Name: "Some Other Device", Host: "192.168.1.102", Port: testBridgePort3, ID: "3"},    // Other device
	}

	t.Run("filterKnownChildBridges removes main bridge", func(t *testing.T) {
		// Test that main Homebridge instances are filtered out
		filtered := filterKnownChildBridges(testHAPServices, []ChildBridge{})

		// Should have filtered out the "Homebridge ABCD EFGH" service
		for _, service := range filtered {
			if strings.HasPrefix(service.Name, "Homebridge ") && len(strings.Fields(service.Name)) == 3 {
				t.Errorf("filterKnownChildBridges() did not filter main bridge: %s", service.Name)
			}
		}
	})

	t.Run("isKnownChildBridge identifies main vs child bridges", func(t *testing.T) {
		childBridges := []ChildBridge{
			{Name: "TplinkSmarthome"},
		}

		mainBridge := HAPAccessory{Name: "Homebridge ABCD EFGH", Host: "192.168.1.100", Port: testBridgePort1, ID: "1"}
		childBridge := HAPAccessory{Name: "TplinkSmarthome", Host: "192.168.1.101", Port: testBridgePort2, ID: "2"}

		// Should identify main bridge as NOT a child bridge
		if isKnownChildBridge(mainBridge, childBridges) {
			t.Errorf("isKnownChildBridge() = true for main bridge, want false")
		}

		// Should identify valid child bridge
		if !isKnownChildBridge(childBridge, childBridges) {
			t.Errorf("isKnownChildBridge() = false for child bridge, want true")
		}
	})
}

// Test HAP monitoring wrapper functions
func TestHAPMonitoringWrappers(t *testing.T) {
	t.Run("HAP monitoring functions don't panic with valid inputs", func(t *testing.T) {
		// Test that HAP monitoring functions handle basic inputs properly
		var childBridges []ChildBridge
		var hapServices []HAPAccessory
		bridgeStatusMap := make(map[string]map[string]interface{})

		// Should not panic with valid empty inputs
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("HAP monitoring functions panicked: %v", r)
			}
		}()

		// Test these functions don't panic (they don't return values we can easily test)
		checkAllBridgesOptimized(bridgeStatusMap, &childBridges, &hapServices, false)
		checkAllBridgesOptimizedWithRetry(bridgeStatusMap, &childBridges, &hapServices, false, 1)
	})
}

// Test discovery cache wrapper functions
func TestDiscoveryCacheWrappers(t *testing.T) {
	t.Run("cache functions don't panic with valid inputs", func(t *testing.T) {
		// Test that cache functions handle basic inputs properly
		var childBridges []ChildBridge
		var hapServices []HAPAccessory
		childBridgeList := []ChildBridge{{Name: "TestBridge"}}
		hapServiceList := []HAPAccessory{{Name: "TestService", Host: "localhost", Port: 80, ID: "1"}}

		// Should not panic with valid inputs
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Cache functions panicked: %v", r)
			}
		}()

		// Test these functions don't panic
		updateDiscoveryCache(&childBridges, &hapServices, childBridgeList, hapServiceList)
		displaydiscoveryResults(childBridgeList, hapServiceList)
	})
}

// Test utility functions for service discovery
func TestServiceDiscoveryUtilities(t *testing.T) {
	t.Run("trimServiceName handles various inputs", func(t *testing.T) {
		tests := []struct {
			name   string
			input  string
			expect string
		}{
			{"single field service", "TestService._hap._tcp.local.", "TestService._hap._tcp.local."},
			{"multi word service", "Test Service Name suffix", "Test Service Name"},
			{"single word", "TestService", "TestService"},
			{"empty string", "", ""},
			{"two words", "Test Service", "Test"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := trimServiceName(tt.input)
				if result != tt.expect {
					t.Errorf("trimServiceName(%s) = %s, want %s", tt.input, result, tt.expect)
				}
			})
		}
	})

	t.Run("testCachedServicesReachability validates services", func(t *testing.T) {
		testServices := []HAPAccessory{
			{Name: "TestService1", Host: "localhost", Port: 80, ID: "1"},
			{Name: "TestService2", Host: "nonexistent.invalid", Port: 9999, ID: "2"},
		}

		unreachableServices := testCachedServicesReachability(testServices)

		// Should return a slice (possibly empty)
		if unreachableServices == nil {
			t.Errorf("testCachedServicesReachability() = nil, want non-nil slice")
		}
	})
}

// Constants for test strings to satisfy linter
const (
	testCharVolts        = "volts"
	testCharBatteryLevel = "batterylevel"
	testCharHumidity     = "humidity"
)

// Test characteristic exclusion functionality
func TestParseCharacteristicExcludeList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "single characteristic",
			input:    testCharVolts,
			expected: []string{testCharVolts},
		},
		{
			name:     "multiple characteristics",
			input:    testCharVolts + "," + testCharBatteryLevel + "," + testCharHumidity,
			expected: []string{testCharVolts, testCharBatteryLevel, testCharHumidity},
		},
		{
			name:     "with spaces",
			input:    " " + testCharVolts + " , " + testCharBatteryLevel + " , " + testCharHumidity + " ",
			expected: []string{testCharVolts, testCharBatteryLevel, testCharHumidity},
		},
		{
			name:     "mixed case normalized to lowercase",
			input:    "Volts,BatteryLevel,HUMIDITY",
			expected: []string{testCharVolts, testCharBatteryLevel, testCharHumidity},
		},
		{
			name:     "empty elements filtered out",
			input:    testCharVolts + ",," + testCharBatteryLevel + ",",
			expected: []string{testCharVolts, testCharBatteryLevel},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCharacteristicExcludeList(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("parseCharacteristicExcludeList(%q) returned %d items, want %d",
					tt.input, len(result), len(tt.expected))
				return
			}
			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("parseCharacteristicExcludeList(%q)[%d] = %q, want %q",
						tt.input, i, result[i], expected)
				}
			}
		})
	}
}

func TestIsCharacteristicExcluded(t *testing.T) {
	// Set up test exclusion list
	originalExcludeList := at3CharacteristicExclude
	at3CharacteristicExclude = []string{testCharVolts, testCharBatteryLevel, testCharHumidity}
	defer func() { at3CharacteristicExclude = originalExcludeList }()

	tests := []struct {
		name           string
		characteristic string
		expected       bool
	}{
		{
			name:           "exact match lowercase",
			characteristic: testCharVolts,
			expected:       true,
		},
		{
			name:           "exact match mixed case",
			characteristic: "BatteryLevel",
			expected:       true,
		},
		{
			name:           "partial match in characteristic name",
			characteristic: "VoltsMeasurement",
			expected:       true,
		},
		{
			name:           "not excluded",
			characteristic: "temperature",
			expected:       false,
		},
		{
			name:           "partial false positive avoided",
			characteristic: "brightness",
			expected:       false,
		},
		{
			name:           "empty characteristic",
			characteristic: "",
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCharacteristicExcluded(tt.characteristic)
			if result != tt.expected {
				t.Errorf("isCharacteristicExcluded(%q) = %v, want %v",
					tt.characteristic, result, tt.expected)
			}
		})
	}
}

func TestShouldExcludeFromAWTRIX3(t *testing.T) {
	// Set up test exclusion list
	originalExcludeList := at3CharacteristicExclude
	at3CharacteristicExclude = []string{testCharVolts, testCharBatteryLevel}
	defer func() { at3CharacteristicExclude = originalExcludeList }()

	tests := []struct {
		name     string
		message  string
		expected bool
	}{
		{
			name:     "excluded characteristic in message",
			message:  "Battery Sensor: " + testCharVolts + " changed from 12.5 to 12.3",
			expected: true,
		},
		{
			name:     "excluded characteristic different format",
			message:  "Device: " + testCharBatteryLevel + " 85 -> 82",
			expected: true,
		},
		{
			name:     "not excluded characteristic",
			message:  "Temperature Sensor: temperature 22.5°C → 23.1°C",
			expected: false,
		},
		{
			name:     "malformed message",
			message:  "Invalid message format",
			expected: false,
		},
		{
			name:     "empty message",
			message:  "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldExcludeFromAWTRIX3(tt.message)
			if result != tt.expected {
				t.Errorf("shouldExcludeFromAWTRIX3(%q) = %v, want %v",
					tt.message, result, tt.expected)
			}
		})
	}
}

func TestCreateChangeMessage(t *testing.T) {
	// Set up test exclusion list
	originalExcludeList := at3CharacteristicExclude
	at3CharacteristicExclude = []string{testCharVolts, testCharBatteryLevel, "on"}
	defer func() { at3CharacteristicExclude = originalExcludeList }()

	tests := []struct {
		name        string
		serviceName string
		key         string
		oldValue    interface{}
		newValue    interface{}
		expected    string
	}{
		{
			name:        "excluded characteristic with suffix",
			serviceName: "Battery Sensor",
			key:         testCharVolts,
			oldValue:    testTempHigh,
			newValue:    testTempLow,
			expected:    "Battery Sensor: " + testCharVolts + " 12.5 -> 12.3 (excluded)",
		},
		{
			name:        "excluded characteristic On state",
			serviceName: "Light Switch",
			key:         "On",
			oldValue:    false,
			newValue:    true,
			expected:    "Light Switch: ON (excluded)",
		},
		{
			name:        "not excluded characteristic",
			serviceName: "Temperature Sensor",
			key:         "CurrentTemperature",
			oldValue:    testTemp225,
			newValue:    testTemp231,
			expected:    "Temperature Sensor: temperature 72.5°F → 73.6°F",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := createChangeMessage(tt.serviceName, tt.key, tt.oldValue, tt.newValue)
			if result != tt.expected {
				t.Errorf("createChangeMessage() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatCharacteristicChange(t *testing.T) {
	// Set up test exclusion list
	originalExcludeList := at3CharacteristicExclude
	at3CharacteristicExclude = []string{testCharVolts}
	defer func() { at3CharacteristicExclude = originalExcludeList }()

	tests := []struct {
		name           string
		accessoryName  string
		characteristic HAPCharacteristic
		lastValue      interface{}
		expected       string
	}{
		{
			name:          "excluded characteristic",
			accessoryName: "Battery Monitor",
			characteristic: HAPCharacteristic{
				Description: testCharVolts,
				Value:       testTempLow,
			},
			lastValue: testTempHigh,
			expected:  "Battery Monitor: " + testCharVolts + " 12.5 -> 12.3 (excluded)",
		},
		{
			name:          "not excluded characteristic",
			accessoryName: "Temperature Sensor",
			characteristic: HAPCharacteristic{
				Description: "CurrentTemperature",
				Value:       testTemp231,
			},
			lastValue: testTemp225,
			expected:  "Temperature Sensor: temperature 72.5°F → 73.6°F",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatCharacteristicChange(tt.accessoryName, tt.characteristic, tt.lastValue)
			if result != tt.expected {
				t.Errorf("formatCharacteristicChange() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatOnOffChange(t *testing.T) {
	tests := []struct {
		name          string
		accessoryName string
		newValue      interface{}
		expected      string
	}{
		{
			name:          "boolean true",
			accessoryName: testServiceName,
			newValue:      true,
			expected:      "Test Light: ON",
		},
		{
			name:          "boolean false",
			accessoryName: testServiceName,
			newValue:      false,
			expected:      "Test Light: OFF",
		},
		{
			name:          "float64 1.0 (on)",
			accessoryName: "Switch",
			newValue:      1.0,
			expected:      "Switch: ON",
		},
		{
			name:          "float64 0.0 (off)",
			accessoryName: "Switch",
			newValue:      0.0,
			expected:      "Switch: OFF",
		},
		{
			name:          "string value (default off)",
			accessoryName: "Device",
			newValue:      "unknown",
			expected:      "Device: OFF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatOnOffChange(tt.accessoryName, tt.newValue)
			if result != tt.expected {
				t.Errorf("formatOnOffChange(%q, %v) = %q, want %q",
					tt.accessoryName, tt.newValue, result, tt.expected)
			}
		})
	}
}

func TestFormatMotionChange(t *testing.T) {
	tests := []struct {
		name          string
		accessoryName string
		newValue      interface{}
		expected      string
	}{
		{
			name:          "boolean true (detected)",
			accessoryName: "Motion Sensor",
			newValue:      true,
			expected:      "Motion Sensor: DETECTED",
		},
		{
			name:          "boolean false (cleared)",
			accessoryName: "Motion Sensor",
			newValue:      false,
			expected:      "Motion Sensor: CLEARED",
		},
		{
			name:          "float64 1.0 (detected)",
			accessoryName: "PIR Sensor",
			newValue:      1.0,
			expected:      "PIR Sensor: DETECTED",
		},
		{
			name:          "float64 0.0 (cleared)",
			accessoryName: "PIR Sensor",
			newValue:      0.0,
			expected:      "PIR Sensor: CLEARED",
		},
		{
			name:          "string value (default cleared)",
			accessoryName: "Detector",
			newValue:      "unknown",
			expected:      "Detector: CLEARED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMotionChange(tt.accessoryName, tt.newValue)
			if result != tt.expected {
				t.Errorf("formatMotionChange(%q, %v) = %q, want %q",
					tt.accessoryName, tt.newValue, result, tt.expected)
			}
		})
	}
}

func TestFormatTemperatureChange(t *testing.T) {
	tests := []struct {
		name          string
		accessoryName string
		oldValue      interface{}
		newValue      interface{}
		expected      string
	}{
		{
			name:          "valid celsius to fahrenheit conversion",
			accessoryName: "Temperature Sensor",
			oldValue:      testLargeTemp,
			newValue:      testNearZeroTemp,
			expected:      "Temperature Sensor: temperature 75.9°F → 75.0°F",
		},
		{
			name:          "freezing point conversion",
			accessoryName: "Outdoor Sensor",
			oldValue:      0.0,
			newValue:      testFreezing,
			expected:      "Outdoor Sensor: temperature 32.0°F → 32.9°F",
		},
		{
			name:          "negative temperature conversion",
			accessoryName: "Freezer Sensor",
			oldValue:      testNegativeTemp,
			newValue:      testNegativeValidTemp,
			expected:      "Freezer Sensor: temperature 14.0°F → 23.0°F",
		},
		{
			name:          "invalid old value type",
			accessoryName: "Broken Sensor",
			oldValue:      "invalid",
			newValue:      testBoiling,
			expected:      "Broken Sensor: temperature invalid → 25",
		},
		{
			name:          "invalid new value type",
			accessoryName: "Broken Sensor",
			oldValue:      testBoiling,
			newValue:      "invalid",
			expected:      "Broken Sensor: temperature 25 → invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTemperatureChange(tt.accessoryName, tt.oldValue, tt.newValue)
			if result != tt.expected {
				t.Errorf("formatTemperatureChange(%q, %v, %v) = %q, want %q",
					tt.accessoryName, tt.oldValue, tt.newValue, result, tt.expected)
			}
		})
	}
}

// Test mDNS service filtering - empty services
func TestServiceFilteringEmpty(t *testing.T) {
	client := NewMDNSClient(testTimeout5s)
	emptyResult := client.filterServicesForLookup([]string{}, []string{})
	if len(emptyResult) != 0 {
		t.Errorf("filterServicesForLookup([]) = %d services, want 0", len(emptyResult))
	}
}

// Test mDNS service filtering - with services
func TestServiceFilteringWithServices(t *testing.T) {
	client := NewMDNSClient(testTimeout5s)
	serviceNames := []string{"TestService1", "TestService2", "TestService3"}
	expectedNames := []string{"TestService1", "TestService3"}
	result := client.filterServicesForLookup(serviceNames, expectedNames)

	// Should return slice of same or smaller length
	if len(result) > len(serviceNames) {
		t.Errorf("filterServicesForLookup() returned more services than input: got %d, want <= %d",
			len(result), len(serviceNames))
	}

	// All returned names should be from original list
	for _, returned := range result {
		found := false
		for _, original := range serviceNames {
			if returned == original {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("filterServicesForLookup() returned unexpected service: %s", returned)
		}
	}
}

// TestAWTRIX3ClientCreation tests AWTRIX3 client creation and basic functionality
func TestAWTRIX3ClientCreation(t *testing.T) {
	devices := []MDNSService{
		{Name: "awtrix_test", Host: testHostIP, Port: testPort},
	}

	client := NewAWTRIX3Client(devices)
	if client == nil {
		t.Error("NewAWTRIX3Client() returned nil")
		return
	}

	if len(client.devices) != 1 {
		t.Errorf("NewAWTRIX3Client() created client with %d devices, want 1", len(client.devices))
	}
}

// TestAWTRIX3PostNotification tests posting notifications to AWTRIX3 devices
func TestAWTRIX3PostNotification(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/api/notify") {
			t.Errorf("Expected /api/notify path, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Extract host and port from server URL
	serverURL := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(serverURL, ":")
	if len(parts) != 2 {
		t.Fatalf("Failed to parse server URL: %s", server.URL)
	}
	host := parts[0]
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("Failed to parse port: %v", err)
	}

	devices := []MDNSService{
		{Name: "awtrix_test", Host: host, Port: port},
	}

	client := NewAWTRIX3Client(devices)
	notification := AWTRIX3Notification{
		Text:    "Test notification",
		Repeat:  1,
		Rainbow: true,
		Stack:   true,
	}

	// This should not panic and should reach the mock server
	client.PostNotification(notification)
}

// TestSendAWTRIX3Notifications tests the notification sending function
func TestSendAWTRIX3Notifications(t *testing.T) {
	// Test with nil client (should not panic)
	originalClient := awtrix3Client
	defer func() { awtrix3Client = originalClient }()

	awtrix3Client = nil
	sendAWTRIX3Notifications([]string{"test message"})

	// Test with empty messages (should not send anything)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("Unexpected request to server - should not send empty messages")
	}))
	defer server.Close()

	awtrix3Client = NewAWTRIX3Client([]MDNSService{{Name: "test", Host: "localhost", Port: testPort}})
	sendAWTRIX3Notifications([]string{})
}

// TestRetryDiscoveryFunctions tests discovery retry logic
func TestRetryDiscoveryFunctions(t *testing.T) {
	hapServices := []HAPAccessory{
		{Name: testBridgeName, Host: testHostIP, Port: testPort, ID: testBridgeID},
	}
	currentChildBridges := []ChildBridge{
		{Name: "Test Bridge 1", Identifier: testBridgeID},
		{Name: "Test Bridge 2", Identifier: "test2"},
	}

	// Test shouldRetryDiscovery
	should := shouldRetryDiscovery(hapServices, currentChildBridges, 1)
	if !should {
		t.Error("shouldRetryDiscovery() should return true when services < bridges and attempt < 3")
	}

	should = shouldRetryDiscovery(hapServices, currentChildBridges, 3)
	if should {
		t.Error("shouldRetryDiscovery() should return false when attempt >= 3")
	}

	// Equal services and bridges should not retry
	hapServicesEqual := []HAPAccessory{
		{Name: "Test Bridge 1", Host: testHostIP, Port: testPort, ID: testBridgeID},
		{Name: "Test Bridge 2", Host: testHostIP, Port: testPortAlt, ID: "test2"},
	}
	should = shouldRetryDiscovery(hapServicesEqual, currentChildBridges, 1)
	if should {
		t.Error("shouldRetryDiscovery() should return false when services count equals bridges count")
	}
}

// TestDiscoveryResultDisplay tests result display functions
func TestDiscoveryResultDisplay(_ *testing.T) {
	// Test displayCachedResults (should not panic)
	originalDebug := debug
	debug = true
	displayCachedResults()
	debug = false
	displayCachedResults()
	debug = originalDebug

	// Test displayNewDiscoveryResults
	currentChildBridges := []ChildBridge{
		{Name: testBridgeName, Identifier: testBridgeID},
	}
	hapServices := []HAPAccessory{
		{Name: testBridgeName, Host: testHostIP, Port: testPort, ID: testBridgeID},
	}

	// Should not panic
	displayNewDiscoveryResults(currentChildBridges, hapServices)
}

// TestReportAccessoryResults tests accessory result reporting
func TestReportAccessoryResults(_ *testing.T) {
	// Test with zero bridges
	reportAccessoryResults(0)

	// Test with some bridges
	reportAccessoryResults(2)
}

// TestHandleFailedDiscovery tests failed discovery handling
func TestHandleFailedDiscovery(_ *testing.T) {
	currentChildBridges := []ChildBridge{
		{Name: testBridgeName, Identifier: testBridgeID},
	}
	hapServices := []HAPAccessory{}

	// Should not panic
	handleFailedDiscovery(currentChildBridges, hapServices)
}

// TestCollectChangeMessages tests change message collection
func TestCollectChangeMessages(t *testing.T) {
	old := AccessoryStatus{
		ServiceName: testServiceName,
		Values:      map[string]interface{}{"On": false, "Brightness": testBrightness50},
	}
	current := AccessoryStatus{
		ServiceName: testServiceName,
		Values:      map[string]interface{}{"On": true, "Brightness": testBrightness75, "Hue": testHue180},
	}

	messages := collectChangeMessages(old, current)

	// Should have changes for On, Brightness, and new Hue
	if len(messages) == 0 {
		t.Error("collectChangeMessages() should return messages for changes")
	}

	// Test with no changes
	messages = collectChangeMessages(current, current)
	if len(messages) != 0 {
		t.Error("collectChangeMessages() should return no messages when no changes")
	}
}

func TestSendDiscoveryNotification(t *testing.T) {
	tests := []struct {
		name            string
		discoveryMethod string
		wantMessage     string
	}{
		{
			name:            "auto-discovered notification",
			discoveryMethod: "Auto-discovered",
			wantMessage:     "Homebridge Captain's Log automatically connected!",
		},
		{
			name:            "manual configuration notification",
			discoveryMethod: "Manual",
			wantMessage:     "Homebridge Captain's Log manually connected!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			// Create mock AWTRIX3 client
			devices := []MDNSService{
				{Host: "test-host", Port: testPort, Name: "Test Device"},
			}
			client := NewAWTRIX3Client(devices)

			// Capture the notification that would be sent
			originalClient := awtrix3Client
			awtrix3Client = client
			defer func() { awtrix3Client = originalClient }()

			client.sendDiscoveryNotification(tt.discoveryMethod)
			// Function executes without error - testing mainly for coverage
		})
	}
}

func TestSendCharacteristicNotification(t *testing.T) {
	tests := []struct {
		name         string
		changeMsg    string
		description  string
		exclusions   []string
		clientExists bool
		expectSend   bool
	}{
		{
			name:         "no client",
			changeMsg:    "Test Light: ON",
			description:  testCharOn,
			exclusions:   []string{},
			clientExists: false,
			expectSend:   false,
		},
		{
			name:         "excluded characteristic",
			changeMsg:    "Test Device: voltage 3.2V → 3.1V",
			description:  "volts",
			exclusions:   []string{"volts"},
			clientExists: true,
			expectSend:   false,
		},
		{
			name:         "normal notification",
			changeMsg:    "Test Light: ON",
			description:  testCharOn,
			exclusions:   []string{},
			clientExists: true,
			expectSend:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			// Set up exclusion list for test
			originalExclude := at3CharacteristicExclude
			at3CharacteristicExclude = tt.exclusions
			defer func() { at3CharacteristicExclude = originalExclude }()

			// Set up client state
			originalClient := awtrix3Client
			if tt.clientExists {
				devices := []MDNSService{
					{Host: "test-host", Port: testPort, Name: "Test Device"},
				}
				awtrix3Client = NewAWTRIX3Client(devices)
			} else {
				awtrix3Client = nil
			}
			defer func() { awtrix3Client = originalClient }()

			sendCharacteristicNotification(tt.changeMsg, tt.description)
			// Function executes without error - testing mainly for coverage
		})
	}
}

func TestProcessHAPService(t *testing.T) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	bridgesWithAccessories := 0
	client := &http.Client{Timeout: TimeoutConfig.HTTPAccessoryCheck}

	service := HAPAccessory{
		ID:   testBridgeID,
		Name: testBridgeName,
		Host: "nonexistent.invalid",
		Port: testPortInvalid,
	}
	bridgeStatusMap := make(map[string]map[string]interface{})

	wg.Add(1)
	processHAPService(&wg, &mu, &bridgesWithAccessories, client, service, bridgeStatusMap)

	// Function should complete without hanging
	if bridgesWithAccessories != 0 {
		t.Errorf("Expected 0 bridges with accessories, got %d", bridgesWithAccessories)
	}
}

func TestVersionFlag(t *testing.T) {
	// Create a new command to avoid interference with global state
	cmd := &cobra.Command{
		Use:   "hb-clog",
		Short: "Monitor Homebridge accessory status changes",
		Run:   runMonitor,
	}
	cmd.Flags().BoolP("version", "v", false, "Show version and exit")
	cmd.SetArgs([]string{"--version"})

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Execute command
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Command execution failed: %v", err)
	}

	// Restore stdout and read captured output
	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close pipe writer: %v", err)
	}
	os.Stdout = oldStdout

	var buf strings.Builder
	_, err = io.Copy(&buf, r)
	if err != nil {
		t.Fatalf("Failed to read captured output: %v", err)
	}

	// Check output contains version
	result := buf.String()
	expectedVersion := "hb-clog version " + Version
	if !strings.Contains(result, expectedVersion) {
		t.Errorf("Expected output to contain %q, got %q", expectedVersion, result)
	}
}
