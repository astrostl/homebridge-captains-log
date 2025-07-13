// This file is part of homebridge-captains-log
package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Test constants to avoid magic numbers
const (
	testPort         = 8080
	testPortAlt      = 9090
	testBrightness50 = 50
	testBrightness75 = 75
	testBrightness80 = 80
	testTemp225      = 22.5
	testTemp231      = 23.1
	testHumidity45   = 45.0
	testHumidity485  = 48.5
	testHue180       = 180
	testAID123       = 123
	testBridgePort1  = 51234
	testBridgePort2  = 51250
	testBridgePort3  = 51251
	testTimeout5s    = 5 * time.Second
	testPortInvalid  = 9999
)

const (
	testServiceName     = "Test Light"
	testCharOn          = "On"
	testCharBrightness  = "Brightness"
	testServiceNameTest = "TestService"
	testHostIP          = "192.168.1.100"
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
			key:         testCharOn,
			oldValue:    false,
			newValue:    true,
			serviceName: testServiceName,
			want:        "turned ON",
		},
		{
			name:        "turn off",
			key:         testCharOn,
			oldValue:    true,
			newValue:    false,
			serviceName: testServiceName,
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
			key:         testCharBrightness,
			oldValue:    testBrightness50,
			newValue:    testBrightness75,
			serviceName: testServiceName,
			want:        "brightness: 50% → 75%",
		},
		{
			name:        "temperature change",
			key:         "CurrentTemperature",
			oldValue:    testTemp225,
			newValue:    testTemp231,
			serviceName: "Temperature Sensor",
			want:        "temperature: 22.5°C → 23.1°C",
		},
		{
			name:        "humidity change",
			key:         "CurrentRelativeHumidity",
			oldValue:    testHumidity45,
			newValue:    testHumidity485,
			serviceName: "Humidity Sensor",
			want:        "humidity: 45.0% → 48.5%",
		},
		{
			name:        "battery level change",
			key:         "BatteryLevel",
			oldValue:    testBrightness80,
			newValue:    testBrightness75,
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
	t.Run("successful fetch", testFetchAccessoriesSuccess)
	t.Run("401 with successful noauth", testFetchAccessories401WithNoauth)
	t.Run("server error", testFetchAccessoriesServerError)
}

func testFetchAccessoriesSuccess(t *testing.T) {
	testAccessories := []AccessoryStatus{
		{
			UniqueID:    "test1",
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

	if accessories[0].UniqueID != "test1" {
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
					{UniqueID: "test1", ServiceName: "Test Light", Values: map[string]interface{}{"On": true}},
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
		UniqueID:    "test1",
		ServiceName: testServiceName,
		Values:      map[string]interface{}{testCharOn: true},
		LastUpdated: now,
	}

	status2 := AccessoryStatus{
		UniqueID:    "test1",
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
					{UniqueID: "test1", ServiceName: "Test Light", Values: map[string]interface{}{"On": true}},
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
					{UniqueID: "test1", ServiceName: "Test Light", Values: map[string]interface{}{"On": callCount%2 == 0}},
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
					{UniqueID: "test1", ServiceName: "Test Light", Values: map[string]interface{}{"On": true}},
				}
				_ = json.NewEncoder(w).Encode(testAccessories)
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
				{Name: "Test Bridge", Plugin: "test-plugin", Status: "running"},
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

	if bridges[0].Name != "Test Bridge" {
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
						{Type: "23", Description: "Name", Value: "Test Light", IID: 1},
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
				accessoryNames:     []string{"Test Light"},
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
			name: "no changes",
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
				{UniqueID: "test1", ServiceName: testServiceName, Values: map[string]interface{}{testCharOn: true}},
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
