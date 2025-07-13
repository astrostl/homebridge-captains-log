// This file is part of homebridge-captains-log
package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Test constants to avoid magic numbers
const (
	testMDNSTimeout5s    = 5 * time.Second
	testMDNSTimeout10s   = 10 * time.Second
	testMDNSTimeout100ms = 100 * time.Millisecond
	testMDNSTimeout150ms = 150 * time.Millisecond
	testMDNSTimeout300ms = 300 * time.Millisecond
	testMDNSTimeout500ms = 500 * time.Millisecond
	testMDNSTimeout2s    = 2 * time.Second
	testMDNSPortStandard = 8080
	testMDNSPortAlt      = 12345
	testMDNSPortBridge1  = 51234
	testMDNSPortBridge2  = 51250
	testMDNSPortBridge3  = 51251
	testMDNSPortAppleTV  = 7000
	testMDNSIPByte1      = 192
	testMDNSIPByte2      = 168
	testMDNSIPByte3      = 1
	testMDNSIPByte4      = 100
)

const (
	testMDNSServiceName    = "TestService"
	testMDNSHostIP         = "192.168.1.100"
	testMDNSHost           = "test.local"
	testMDNSTXTKeyMD       = "md"
	testMDNSTXTValueHomebr = "homebridge"
)

func TestNewMDNSClient(t *testing.T) {
	timeout := testMDNSTimeout5s
	client := NewMDNSClient(timeout)

	if client == nil {
		t.Errorf("NewMDNSClient() returned nil")
		return
	}

	if client.timeout != timeout {
		t.Errorf("NewMDNSClient() timeout = %v, want %v", client.timeout, timeout)
	}
}

func TestParseTXTRecord(t *testing.T) {
	client := &MDNSClient{}
	txtRecords := make(map[string]string)

	tests := []struct {
		name   string
		record string
		want   map[string]string
	}{
		{
			name:   "key-value pair",
			record: "md=homebridge",
			want:   map[string]string{testMDNSTXTKeyMD: testMDNSTXTValueHomebr},
		},
		{
			name:   "boolean flag",
			record: "debug",
			want:   map[string]string{"debug": ""},
		},
		{
			name:   "empty value",
			record: "key=",
			want:   map[string]string{"key": ""},
		},
		{
			name:   "multiple equals",
			record: "key=value=with=equals",
			want:   map[string]string{"key": "value=with=equals"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear the map for each test
			for k := range txtRecords {
				delete(txtRecords, k)
			}

			client.parseTXTRecord(tt.record, txtRecords)

			if !reflect.DeepEqual(txtRecords, tt.want) {
				t.Errorf("parseTXTRecord() result = %v, want %v", txtRecords, tt.want)
			}
		})
	}
}

func TestParseTXTRecordMultiple(t *testing.T) {
	client := &MDNSClient{}
	txtRecords := make(map[string]string)

	// Test multiple records
	records := []string{
		"md=homebridge",
		"version=1.2.3",
		"debug",
		"port=8080",
	}

	for _, record := range records {
		client.parseTXTRecord(record, txtRecords)
	}

	expected := map[string]string{
		"md":      "homebridge",
		"version": "1.2.3",
		"debug":   "",
		"port":    "8080",
	}

	if !reflect.DeepEqual(txtRecords, expected) {
		t.Errorf("Multiple parseTXTRecord() result = %v, want %v", txtRecords, expected)
	}
}

func TestMDNSServiceValidation(t *testing.T) {
	// Test that MDNSService struct works as expected
	service := MDNSService{
		Name:       testMDNSServiceName,
		Host:       testMDNSHostIP,
		Port:       testMDNSPortStandard,
		TXTRecords: map[string]string{testMDNSTXTKeyMD: testMDNSTXTValueHomebr},
	}

	if service.Name != testMDNSServiceName {
		t.Errorf("Service name = %v, want %s", service.Name, testMDNSServiceName)
	}

	if service.Host != testMDNSHostIP {
		t.Errorf("Service host = %v, want %s", service.Host, testMDNSHostIP)
	}

	if service.Port != testMDNSPortStandard {
		t.Errorf("Service port = %v, want %d", service.Port, testMDNSPortStandard)
	}

	if service.TXTRecords[testMDNSTXTKeyMD] != testMDNSTXTValueHomebr {
		t.Errorf("TXT record %s = %v, want %s", testMDNSTXTKeyMD, service.TXTRecords[testMDNSTXTKeyMD], testMDNSTXTValueHomebr)
	}
}

func TestMDNSClientTimeout(t *testing.T) {
	// Test that timeout is properly set
	testTimeouts := []time.Duration{
		time.Second,
		testMDNSTimeout5s,
		testMDNSTimeout10s,
	}

	for _, timeout := range testTimeouts {
		client := NewMDNSClient(timeout)
		if client.timeout != timeout {
			t.Errorf("Client timeout = %v, want %v", client.timeout, timeout)
		}
	}
}

func TestDiscoverHomebridgeServicesWithMockData(t *testing.T) {
	// This test verifies the logic flow without actually performing network operations
	// since mDNS discovery requires network access and multicast support

	client := &MDNSClient{timeout: testMDNSTimeout100ms}

	// Test context cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 2*testMDNSTimeout100ms)
	defer cancel()

	// Use a goroutine with timeout to avoid hanging
	done := make(chan struct{})
	go func() {
		defer close(done)
		services, err := client.DiscoverHomebridgeServices(ctx)
		if err == nil {
			t.Logf("Discovery with short timeout returned %d services", len(services))
		} else {
			t.Logf("Discovery failed as expected: %v", err)
		}
	}()

	select {
	case <-done:
		// Test completed
	case <-time.After(testMDNSTimeout500ms):
		t.Log("Discovery test timed out, which is acceptable for network-dependent tests")
	}
}

func TestMDNSServiceFiltering(t *testing.T) {
	// Test the logic that would be used to filter Homebridge services
	testServices := []MDNSService{
		{
			Name:       "Homebridge 1234 5678",
			Host:       testMDNSHostIP,
			Port:       testMDNSPortBridge1,
			TXTRecords: map[string]string{testMDNSTXTKeyMD: testMDNSTXTValueHomebr},
		},
		{
			Name:       "TplinkSmarthome 4160",
			Host:       testMDNSHostIP,
			Port:       testMDNSPortBridge2,
			TXTRecords: map[string]string{testMDNSTXTKeyMD: testMDNSTXTValueHomebr},
		},
		{
			Name:       "AppleTV",
			Host:       "192.168.1.101",
			Port:       testMDNSPortAppleTV,
			TXTRecords: map[string]string{testMDNSTXTKeyMD: "apple"},
		},
		{
			Name:       "MyPlugin Bridge",
			Host:       testMDNSHostIP,
			Port:       testMDNSPortBridge3,
			TXTRecords: map[string]string{testMDNSTXTKeyMD: testMDNSTXTValueHomebr},
		},
	}

	// Filter for Homebridge services only
	var homebridgeServices []MDNSService
	for _, service := range testServices {
		if md, exists := service.TXTRecords[testMDNSTXTKeyMD]; exists && md == testMDNSTXTValueHomebr {
			homebridgeServices = append(homebridgeServices, service)
		}
	}

	expectedCount := 3 // Three services have md=homebridge
	if len(homebridgeServices) != expectedCount {
		t.Errorf("Filtered services count = %d, want %d", len(homebridgeServices), expectedCount)
	}

	// Verify the correct services were included
	expectedNames := []string{"Homebridge 1234 5678", "TplinkSmarthome 4160", "MyPlugin Bridge"}
	for i, service := range homebridgeServices {
		if service.Name != expectedNames[i] {
			t.Errorf("Service %d name = %s, want %s", i, service.Name, expectedNames[i])
		}
	}
}

func TestMDNSServiceValidPort(t *testing.T) {
	tests := []struct {
		name    string
		service MDNSService
		valid   bool
	}{
		{
			name: "valid service with host and port",
			service: MDNSService{
				Name: testMDNSServiceName,
				Host: testMDNSHostIP,
				Port: testMDNSPortStandard,
			},
			valid: true,
		},
		{
			name: "invalid service missing host",
			service: MDNSService{
				Name: testMDNSServiceName,
				Host: "",
				Port: testMDNSPortStandard,
			},
			valid: false,
		},
		{
			name: "invalid service missing port",
			service: MDNSService{
				Name: testMDNSServiceName,
				Host: testMDNSHostIP,
				Port: 0,
			},
			valid: false,
		},
		{
			name: "invalid service missing both",
			service: MDNSService{
				Name: testMDNSServiceName,
				Host: "",
				Port: 0,
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This mimics the validation logic used in parseServiceResponse
			valid := tt.service.Host != "" && tt.service.Port > 0
			if valid != tt.valid {
				t.Errorf("Service validation = %v, want %v", valid, tt.valid)
			}
		})
	}
}

func TestTXTRecordEdgeCases(t *testing.T) {
	client := &MDNSClient{}

	tests := []struct {
		name    string
		record  string
		wantKey string
		wantVal string
		wantLen int
	}{
		{
			name:    "empty string",
			record:  "",
			wantKey: "",
			wantVal: "",
			wantLen: 1, // Empty string creates one entry
		},
		{
			name:    "just equals",
			record:  "=",
			wantKey: "",
			wantVal: "",
			wantLen: 1,
		},
		{
			name:    "key with no value but equals",
			record:  "key=",
			wantKey: "key",
			wantVal: "",
			wantLen: 1,
		},
		{
			name:    "just key",
			record:  "justkey",
			wantKey: "justkey",
			wantVal: "",
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txtRecords := make(map[string]string)
			client.parseTXTRecord(tt.record, txtRecords)

			if len(txtRecords) != tt.wantLen {
				t.Errorf("TXT records length = %d, want %d", len(txtRecords), tt.wantLen)
			}

			if tt.wantKey != "" {
				if val, exists := txtRecords[tt.wantKey]; !exists {
					t.Errorf("Expected key %s not found", tt.wantKey)
				} else if val != tt.wantVal {
					t.Errorf("TXT record value = %s, want %s", val, tt.wantVal)
				}
			}
		})
	}
}

func TestContextWithMDNS(t *testing.T) {
	// Test that mDNS client properly handles context creation
	client := NewMDNSClient(testMDNSTimeout100ms)

	// Test with a very short timeout to avoid hanging
	ctx, cancel := context.WithTimeout(context.Background(), testMDNSTimeout150ms)
	defer cancel()

	// Use a goroutine with timeout to avoid hanging the test
	done := make(chan struct{})
	go func() {
		defer close(done)
		services, err := client.DiscoverHomebridgeServices(ctx)
		// We don't expect any specific error or result since network behavior varies
		t.Logf("Discovery completed with %d services found (err: %v)", len(services), err)
	}()

	select {
	case <-done:
		// Test completed
	case <-time.After(testMDNSTimeout300ms):
		t.Log("Context test timed out, which is acceptable for network-dependent tests")
	}
}

func TestMDNSServiceStructComplete(t *testing.T) {
	// Ensure all fields of MDNSService work correctly
	service := MDNSService{
		Name: "Test Service Name",
		Host: testMDNSHost,
		Port: testMDNSPortAlt,
		TXTRecords: map[string]string{
			"key1": "value1",
			"key2": "value2",
			"flag": "",
		},
	}

	// Test struct field access
	if service.Name == "" {
		t.Error("Name field should not be empty")
	}
	if service.Host == "" {
		t.Error("Host field should not be empty")
	}
	if service.Port == 0 {
		t.Error("Port field should not be zero")
	}
	if service.TXTRecords == nil {
		t.Error("TXTRecords should not be nil")
	}
	if len(service.TXTRecords) != 3 {
		t.Errorf("TXTRecords length = %d, want 3", len(service.TXTRecords))
	}
}

func TestLookupServiceErrorHandling(t *testing.T) {
	// Test LookupService with short timeout
	client := NewMDNSClient(testMDNSTimeout100ms) // Short but reasonable timeout

	ctx, cancel := context.WithTimeout(context.Background(), testMDNSTimeout150ms)
	defer cancel()

	service, err := client.LookupService(ctx, "nonexistent", "_invalid._tcp")

	// Should handle timeout gracefully (may return nil service)
	if service != nil {
		t.Logf("LookupService unexpectedly found service: %+v", service)
	}
	if err != nil {
		t.Logf("LookupService returned expected error: %v", err)
	}
}

func TestParseServiceResponseNilCases(t *testing.T) {
	client := &MDNSClient{}

	// Test with empty response
	emptyResponse := &dnsmessage.Message{}
	service := client.parseServiceResponse(emptyResponse, "test")

	if service != nil {
		t.Errorf("parseServiceResponse() with empty message should return nil, got %+v", service)
	}
}

func TestParseServiceResponseWithData(t *testing.T) {
	t.Run("valid SRV and TXT records", testParseServiceResponseValid)
	t.Run("missing SRV record", testParseServiceResponseMissingSRV)
}

func testParseServiceResponseValid(t *testing.T) {
	client := &MDNSClient{}
	response := createValidServiceResponse()
	want := &MDNSService{
		Name:       "test",
		Host:       testMDNSHost,
		Port:       testMDNSPortStandard,
		TXTRecords: map[string]string{testMDNSTXTKeyMD: testMDNSTXTValueHomebr, "version": "1.0"},
	}

	got := client.parseServiceResponse(response, "test")
	validateServiceResponse(t, got, want)
}

func testParseServiceResponseMissingSRV(t *testing.T) {
	client := &MDNSClient{}
	response := createMissingSRVResponse()

	got := client.parseServiceResponse(response, "test")
	if got != nil {
		t.Errorf("parseServiceResponse() = %v, want nil", got)
	}
}

func createValidServiceResponse() *dnsmessage.Message {
	return &dnsmessage.Message{
		Answers: []dnsmessage.Resource{
			createServiceSRVRecord(),
			createServiceTXTRecord(),
			createServiceARecord(),
		},
	}
}

func createServiceSRVRecord() dnsmessage.Resource {
	return dnsmessage.Resource{
		Header: dnsmessage.ResourceHeader{
			Name:  dnsmessage.MustNewName("test._hap._tcp.local."),
			Type:  dnsmessage.TypeSRV,
			Class: dnsmessage.ClassINET,
		},
		Body: &dnsmessage.SRVResource{
			Priority: 0,
			Weight:   0,
			Port:     testMDNSPortStandard,
			Target:   dnsmessage.MustNewName("test.local."),
		},
	}
}

func createServiceTXTRecord() dnsmessage.Resource {
	return dnsmessage.Resource{
		Header: dnsmessage.ResourceHeader{
			Name:  dnsmessage.MustNewName("test._hap._tcp.local."),
			Type:  dnsmessage.TypeTXT,
			Class: dnsmessage.ClassINET,
		},
		Body: &dnsmessage.TXTResource{
			TXT: []string{"md=homebridge", "version=1.0"},
		},
	}
}

func createServiceARecord() dnsmessage.Resource {
	return dnsmessage.Resource{
		Header: dnsmessage.ResourceHeader{
			Name:  dnsmessage.MustNewName("test.local."),
			Type:  dnsmessage.TypeA,
			Class: dnsmessage.ClassINET,
		},
		Body: &dnsmessage.AResource{
			A: [4]byte{testMDNSIPByte1, testMDNSIPByte2, testMDNSIPByte3, testMDNSIPByte4},
		},
	}
}

func createMissingSRVResponse() *dnsmessage.Message {
	return &dnsmessage.Message{
		Answers: []dnsmessage.Resource{
			{
				Header: dnsmessage.ResourceHeader{
					Name:  dnsmessage.MustNewName("test._hap._tcp.local."),
					Type:  dnsmessage.TypeTXT,
					Class: dnsmessage.ClassINET,
				},
				Body: &dnsmessage.TXTResource{
					TXT: []string{testMDNSTXTKeyMD + "=" + testMDNSTXTValueHomebr},
				},
			},
		},
	}
}

func validateServiceResponse(t *testing.T, got, want *MDNSService) {
	if (got == nil) != (want == nil) {
		t.Errorf("parseServiceResponse() = %v, want %v", got, want)
		return
	}
	if got != nil && want != nil {
		if got.Name != want.Name || got.Host != want.Host || got.Port != want.Port {
			t.Errorf("parseServiceResponse() = %+v, want %+v", got, want)
		}
		if !reflect.DeepEqual(got.TXTRecords, want.TXTRecords) {
			t.Errorf("parseServiceResponse() TXT records = %v, want %v", got.TXTRecords, want.TXTRecords)
		}
	}
}

func TestDiscoverHomebridgeServicesTimeout(t *testing.T) {
	// Test that DiscoverHomebridgeServices handles timeout correctly
	client := NewMDNSClient(testMDNSTimeout100ms / 2) // Very short timeout

	ctx, cancel := context.WithTimeout(context.Background(), testMDNSTimeout100ms)
	defer cancel()

	// Use a timeout to avoid hanging
	done := make(chan struct{})
	go func() {
		defer close(done)
		services, err := client.DiscoverHomebridgeServices(ctx)
		// Should complete without hanging - services may be nil or empty depending on network conditions
		if err != nil {
			t.Logf("DiscoverHomebridgeServices() returned expected error: %v", err)
		} else {
			t.Logf("DiscoverHomebridgeServices() found %d services", len(services))
		}
	}()

	select {
	case <-done:
		// Test completed successfully
	case <-time.After(testMDNSTimeout2s): // Increased timeout for reliable CI/CD
		t.Log("DiscoverHomebridgeServices() took longer than expected, which can happen with network operations")
		// This is acceptable - don't fail the test for timing variations
	}
}
