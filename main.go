package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

type AccessoryStatus struct {
	UniqueID     string                 `json:"uniqueId"`
	ServiceName  string                 `json:"serviceName"`
	Type         string                 `json:"type"`
	Values       map[string]interface{} `json:"values"`
	LastUpdated  time.Time              `json:"lastUpdated"`
}

type StatusMonitor struct {
	baseURL      string
	interval     time.Duration
	lastStatus   map[string]AccessoryStatus
	client       *http.Client
	token        string
}

var (
	host     string
	port     int
	interval time.Duration
	count    int
	token    string
	useHAP   bool
	debug    bool
)

var rootCmd = &cobra.Command{
	Use:   "captains-log",
	Short: "Monitor Homebridge accessory status changes",
	Long:  "A CLI tool to monitor Homebridge UI for accessory status changes and report any modifications.",
	Run:   runMonitor,
}

func init() {
	godotenv.Load()
	
	defaultHost := getEnvOrDefault("CLOG_HB_HOST", "localhost")
	defaultPort := getEnvIntOrDefault("CLOG_HB_PORT", 8581)
	defaultToken := getEnvOrDefault("CLOG_HB_TOKEN", "")
	
	rootCmd.Flags().StringVarP(&host, "host", "H", defaultHost, "Homebridge UI host")
	rootCmd.Flags().IntVarP(&port, "port", "p", defaultPort, "Homebridge UI port")
	rootCmd.Flags().DurationVarP(&interval, "interval", "i", 30*time.Second, "Polling interval (e.g., 5s, 30s, 1m)")
	rootCmd.Flags().IntVarP(&count, "count", "c", 0, "Number of checks to perform (0 = infinite)")
	rootCmd.Flags().StringVarP(&token, "token", "t", defaultToken, "Homebridge UI auth token")
	rootCmd.Flags().BoolVarP(&debug, "debug", "d", false, "Enable debug output")
	rootCmd.Flags().BoolP("child-bridges", "b", false, "Also monitor child bridges")
	rootCmd.Flags().BoolVarP(&useHAP, "hap", "a", true, "Use HAP (HomeKit) protocol to monitor all bridges")
	rootCmd.Flags().BoolP("http", "x", false, "Use HTTP API mode instead of HAP")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runMonitor(cmd *cobra.Command, args []string) {
	
	// Check if HTTP mode was explicitly requested
	httpMode, _ := cmd.Flags().GetBool("http")
	if httpMode {
		useHAP = false
	}
	
	if !useHAP {
		// HTTP API mode (legacy)
		baseURL := fmt.Sprintf("http://%s:%d", host, port)
		
		monitor := &StatusMonitor{
			baseURL:    baseURL,
			interval:   interval,
			lastStatus: make(map[string]AccessoryStatus),
			client:     &http.Client{Timeout: 10 * time.Second},
			token:      token,
		}

		fmt.Printf("Starting Homebridge HTTP API monitor on %s (interval: %v)\n", baseURL, interval)
		
		monitor.run(count)
	} else {
		// HAP mode (default)
		runHAPMonitor(count)
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := fmt.Sscanf(value, "%d", &defaultValue); err == nil && intValue == 1 {
			return defaultValue
		}
	}
	return defaultValue
}

func (m *StatusMonitor) run(maxChecks int) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Initial check
	m.checkStatus()
	
	if maxChecks == 1 {
		return
	}

	checkCount := 1
	for range ticker.C {
		m.checkStatus()
		checkCount++
		
		if maxChecks > 0 && checkCount >= maxChecks {
			fmt.Printf("Completed %d checks, exiting.\n", checkCount)
			return
		}
	}
}

func (m *StatusMonitor) checkStatus() {
	fmt.Printf("\n[%s] Checking %d accessories...\n", time.Now().Format("15:04:05"), len(m.lastStatus))
	
	accessories, err := m.fetchAccessories()
	if err != nil {
		log.Printf("Error fetching accessories: %v", err)
		return
	}

	changesDetected := 0
	newAccessories := 0

	for _, accessory := range accessories {
		if lastAccessory, exists := m.lastStatus[accessory.UniqueID]; exists {
			if m.hasChanged(lastAccessory, accessory) {
				m.reportChange(lastAccessory, accessory)
				changesDetected++
			}
		} else {
			fmt.Printf("New accessory detected: %s\n", accessory.ServiceName)
			newAccessories++
		}
		
		m.lastStatus[accessory.UniqueID] = accessory
	}

	if changesDetected == 0 && newAccessories == 0 {
		fmt.Printf("No changes detected.\n")
	} else {
		fmt.Printf("Summary: %d changes, %d new accessories\n", changesDetected, newAccessories)
	}
}

func (m *StatusMonitor) fetchAccessories() ([]AccessoryStatus, error) {
	req, err := http.NewRequest("GET", m.baseURL+"/api/accessories", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	if m.token != "" {
		req.Header.Set("Authorization", "Bearer "+m.token)
	}
	
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch accessories: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// Try to get token via noauth and retry
		if token, err := m.getNoAuthToken(); err == nil {
			m.token = token
			fmt.Println("Obtained auth token via /api/auth/noauth, retrying...")
			return m.fetchAccessories()
		}
		return nil, fmt.Errorf("API returned 401 and failed to get noauth token: %w", err)
	}
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var accessories []AccessoryStatus
	if err := json.Unmarshal(body, &accessories); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Update timestamps
	now := time.Now()
	for i := range accessories {
		accessories[i].LastUpdated = now
	}

	return accessories, nil
}

func (m *StatusMonitor) getNoAuthToken() (string, error) {
	req, err := http.NewRequest("POST", m.baseURL+"/api/auth/noauth", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create noauth request: %w", err)
	}
	
	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call noauth endpoint: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("noauth endpoint returned status %d", resp.StatusCode)
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read noauth response: %w", err)
	}
	
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		Token       string `json:"token"`
	}
	
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", fmt.Errorf("failed to parse noauth response: %w", err)
	}
	
	// Try both possible field names
	if tokenResponse.AccessToken != "" {
		return tokenResponse.AccessToken, nil
	}
	if tokenResponse.Token != "" {
		return tokenResponse.Token, nil
	}
	
	return "", fmt.Errorf("no token found in noauth response")
}

func (m *StatusMonitor) hasChanged(old, new AccessoryStatus) bool {
	return !reflect.DeepEqual(old.Values, new.Values)
}

func (m *StatusMonitor) reportChange(old, new AccessoryStatus) {
	fmt.Printf("\n[%s] %s:\n", 
		new.LastUpdated.Format("15:04:05"), 
		new.ServiceName)
	
	for key, newValue := range new.Values {
		if oldValue, exists := old.Values[key]; exists {
			if !reflect.DeepEqual(oldValue, newValue) {
				message := m.formatChangeMessage(key, oldValue, newValue, new.ServiceName)
				fmt.Printf("  %s\n", message)
			}
		} else {
			fmt.Printf("  %s: (new) %v\n", key, newValue)
		}
	}
	
	for key, oldValue := range old.Values {
		if _, exists := new.Values[key]; !exists {
			fmt.Printf("  %s: %v → (removed)\n", key, oldValue)
		}
	}
}

func (m *StatusMonitor) formatChangeMessage(key string, oldValue, newValue interface{}, serviceType string) string {
	switch key {
	case "On":
		if newValue == true {
			return "turned ON"
		}
		return "turned OFF"
	case "ContactSensorState":
		if newValue.(float64) == 0 {
			return "door/window CLOSED"
		}
		return "door/window OPENED"
	case "MotionDetected":
		if newValue == true {
			return "motion DETECTED"
		}
		return "motion CLEARED"
	case "Brightness":
		return fmt.Sprintf("brightness: %v%% → %v%%", oldValue, newValue)
	case "CurrentTemperature":
		return fmt.Sprintf("temperature: %.1f°C → %.1f°C", oldValue, newValue)
	case "CurrentRelativeHumidity":
		return fmt.Sprintf("humidity: %.1f%% → %.1f%%", oldValue, newValue)
	case "BatteryLevel":
		return fmt.Sprintf("battery: %v%% → %v%%", oldValue, newValue)
	case "StatusLowBattery":
		if newValue.(float64) == 1 {
			return "⚠️  LOW BATTERY"
		}
		return "battery OK"
	default:
		return fmt.Sprintf("%s: %v → %v", key, oldValue, newValue)
	}
}

func runHAPMonitor(maxChecks int) {
	checksInfo := "infinite"
	if maxChecks > 0 {
		checksInfo = fmt.Sprintf("%d", maxChecks)
	}
	fmt.Printf("Starting HAP (HomeKit) monitor (checks: %s, interval: %v)\n", checksInfo, interval)
	
	// Track status for each discovered bridge
	bridgeStatusMap := make(map[string]map[string]interface{})
	
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	// Initial check
	checkAllBridges(bridgeStatusMap)
	
	if maxChecks == 1 {
		return
	}
	
	checkCount := 1
	
	// Set up interrupt handling
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	
	fmt.Println("HAP monitor running... Press Ctrl+C to stop")
	
	for {
		select {
		case <-ticker.C:
			checkAllBridges(bridgeStatusMap)
			checkCount++
			
			if maxChecks > 0 && checkCount >= maxChecks {
				fmt.Printf("Completed %d checks, stopping...\n", checkCount)
				return
			}
			
		case <-c:
			fmt.Println("\nStopping monitor...")
			return
		}
	}
}

func checkAllBridges(bridgeStatusMap map[string]map[string]interface{}) {
	fmt.Printf("\n[%s] Discovering child bridges...\n", time.Now().Format("15:04:05"))
	
	// Get known child bridges from API first
	childBridges := getChildBridges()
	if len(childBridges) == 0 {
		fmt.Println("No child bridges found in API.")
		return
	}
	
	debugf("Found %d child bridges from API\n", len(childBridges))
	
	// Discover HAP services via mDNS
	allHAPServices := discoverHAPServices()
	debugf("mDNS discovered %d total HAP services\n", len(allHAPServices))
	
	// Filter HAP services to only include known child bridges
	var hapServices []HAPAccessory
	for _, hapService := range allHAPServices {
		if isKnownChildBridge(hapService, childBridges) {
			hapServices = append(hapServices, hapService)
		} else {
			debugf("Skipping non-child-bridge HAP service: %s\n", hapService.Name)
		}
	}
	
	debugf("Found %d Homebridge HAP services from mDNS\n", len(hapServices))
	
	// Report mDNS discovery results
	if len(hapServices) < len(childBridges) {
		fmt.Printf("mDNS found %d/%d expected bridges\n", len(hapServices), len(childBridges))
		if len(hapServices) == 0 {
			fmt.Println("Note: No Homebridge child bridges discovered via mDNS")
			fmt.Println("Check that child bridges are running and advertising _hap._tcp services")
		}
	} else {
		fmt.Printf("Found all %d expected bridges via mDNS\n", len(hapServices))
	}
	
	if len(hapServices) == 0 {
		fmt.Println("No child bridge HAP services found via mDNS.")
		return
	}
	
	debugf("Total discovered child bridge services: %d\n", len(hapServices))
	
	bridgesWithAccessories := 0
	client := &http.Client{Timeout: 5 * time.Second}
	
	// Check each discovered service synchronously
	for _, service := range hapServices {
		// Check if this service actually has accessories
		if !hasHAPAccessories(service.Host, service.Port) {
			debugf("Service %s at %s:%d has no accessories\n", service.Name, service.Host, service.Port)
			continue
		}
		
		bridgesWithAccessories++
		
		// Initialize status map for this bridge if needed
		if bridgeStatusMap[service.ID] == nil {
			bridgeStatusMap[service.ID] = make(map[string]interface{})
		}
		
		// Check accessories on this bridge
		checkHAPAccessory(client, service, bridgeStatusMap[service.ID])
	}
	
	if bridgesWithAccessories == 0 {
		fmt.Println("No HAP services with accessories found.")
		fmt.Println("Note: Services may only have read-only sensors or no controllable accessories.")
	} else {
		debugf("Found %d HAP services with accessories\n", bridgesWithAccessories)
	}
}

func isKnownChildBridge(hapService HAPAccessory, childBridges []ChildBridge) bool {
	// Since we're already filtering to only Homebridge services via TXT records,
	// we just need to exclude the main bridge and include everything else
	
	// Skip the main bridge (typically named "Homebridge XXXX YYYY")
	if strings.HasPrefix(hapService.Name, "Homebridge ") {
		debugf("Skipping main Homebridge service: %s\n", hapService.Name)
		return false
	}
	
	// All other Homebridge services should be child bridges
	debugf("Including child bridge HAP service: %s\n", hapService.Name)
	return true
}

func discoverHAPServices() []HAPAccessory {
	debugf("Starting custom mDNS discovery for _hap._tcp services...\n")
	
	// Create mDNS client with 10 second timeout to catch all responses
	client := NewMDNSClient(10 * time.Second)
	
	// Discover all Homebridge services (filtered for md=homebridge)
	ctx := context.Background()
	mdnsServices, err := client.DiscoverHomebridgeServices(ctx)
	if err != nil {
		debugf("mDNS discovery failed: %v\n", err)
		return nil
	}
	
	// Convert to HAPAccessory format
	var services []HAPAccessory
	for _, mdnsService := range mdnsServices {
		service := HAPAccessory{
			Name: mdnsService.Name,
			Host: mdnsService.Host,
			Port: mdnsService.Port,
			ID:   mdnsService.Name + "_" + fmt.Sprintf("%d", mdnsService.Port),
		}
		
		debugf("Discovered Homebridge HAP service: %s at %s:%d\n", service.Name, service.Host, service.Port)
		services = append(services, service)
	}
	
	debugf("Custom mDNS discovery completed, found %d Homebridge services\n", len(services))
	return services
}


func hasHAPAccessories(host string, port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	
	url := fmt.Sprintf("http://%s:%d/accessories", host, port)
	resp, err := client.Get(url)
	if err != nil {
		debugf("Failed to check accessories at %s:%d - %v\n", host, port, err)
		return false
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		debugf("Non-200 response from %s:%d - status %d\n", host, port, resp.StatusCode)
		return false
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		debugf("Failed to read response from %s:%d - %v\n", host, port, err)
		return false
	}
	
	var hapResp HAPResponse
	if err := json.Unmarshal(body, &hapResp); err != nil {
		debugf("Failed to parse HAP response from %s:%d - %v\n", host, port, err)
		return false
	}
	
	accessoryCount := len(hapResp.Accessories)
	debugf("Found %d accessories at %s:%d\n", accessoryCount, host, port)
	return accessoryCount > 0
}

type HAPAccessory struct {
	Name string
	Host string  
	Port int
	ID   string
}

type ChildBridge struct {
	Status     string `json:"status"`
	Name       string `json:"name"`
	Plugin     string `json:"plugin"`
	Username   string `json:"username"`
	Identifier string `json:"identifier"`
	PID        int    `json:"pid"`
}


func getChildBridges() []ChildBridge {
	client := &http.Client{Timeout: 10 * time.Second}
	
	// Get auth token for main API
	tokenReq, _ := http.NewRequest("POST", fmt.Sprintf("http://%s:%d/api/auth/noauth", host, port), strings.NewReader("{}"))
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		fmt.Printf("Failed to get auth token: %v\n", err)
		return nil
	}
	defer tokenResp.Body.Close()
	
	tokenBody, _ := io.ReadAll(tokenResp.Body)
	debugf("Token response: %s\n", string(tokenBody))
	
	var tokenData map[string]interface{}
	json.Unmarshal(tokenBody, &tokenData)
	
	var authToken string
	if token, ok := tokenData["access_token"].(string); ok {
		authToken = token
	} else if token, ok := tokenData["token"].(string); ok {
		authToken = token
	}
	
	if authToken == "" {
		fmt.Printf("Failed to get auth token from response: %+v\n", tokenData)
		return nil
	}
	
	// Get child bridges
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://%s:%d/api/status/homebridge/child-bridges", host, port), nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Failed to get child bridges: %v\n", err)
		return nil
	}
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	
	if resp.StatusCode != 200 {
		fmt.Printf("Child bridges API returned status %d: %s\n", resp.StatusCode, string(body))
		return nil
	}
	
	var bridges []ChildBridge
	if err := json.Unmarshal(body, &bridges); err != nil {
		fmt.Printf("Failed to parse child bridges JSON: %v\n", err)
		return nil
	}
	
	debugf("Found %d child bridges from API\n", len(bridges))
	for _, bridge := range bridges {
		debugf("  - %s (%s)\n", bridge.Name, bridge.Plugin)
	}
	
	return bridges
}



type HAPResponse struct {
	Accessories []HAPAccessoryData `json:"accessories"`
}

type HAPAccessoryData struct {
	AID      int          `json:"aid"`
	Services []HAPService `json:"services"`
}

type HAPService struct {
	Type            string             `json:"type"`
	IID             int                `json:"iid"`
	Characteristics []HAPCharacteristic `json:"characteristics"`
}

type HAPCharacteristic struct {
	Type        string      `json:"type"`
	IID         int         `json:"iid"`
	Value       interface{} `json:"value"`
	Description string      `json:"description"`
	Perms       []string    `json:"perms"`
	Format      string      `json:"format"`
}

func checkHAPAccessory(client *http.Client, acc HAPAccessory, lastStatus map[string]interface{}) {
	fmt.Printf("\n[%s] Checking %s...\n", time.Now().Format("15:04:05"), acc.Name)
	
	// Get accessories from the child bridge
	url := fmt.Sprintf("http://%s:%d/accessories", acc.Host, acc.Port)
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("Connection failed: %v\n", err)
		return
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		fmt.Printf("HTTP %d - Child bridges require HAP authentication\n", resp.StatusCode)
		return
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Failed to read response: %v\n", err)
		return
	}
	
	// Parse HAP response
	var hapResp HAPResponse
	if err := json.Unmarshal(body, &hapResp); err != nil {
		fmt.Printf("Failed to parse JSON: %v\n", err)
		return
	}
	
	changesDetected := 0
	accessoryCount := 0
	
	// Process each accessory
	for _, accessory := range hapResp.Accessories {
		accessoryName := getAccessoryName(accessory)
		if accessoryName == "" {
			continue // Skip if no name found
		}
		accessoryCount++
		
		// Check each service for characteristics we care about
		for _, service := range accessory.Services {
			for _, char := range service.Characteristics {
				if char.Type == "25" { // On/Off characteristic
					key := fmt.Sprintf("%d_%s", accessory.AID, accessoryName)
					
					if lastValue, exists := lastStatus[key]; exists {
						if !reflect.DeepEqual(lastValue, char.Value) {
							debugf("%s changed from %v (%T) to %v (%T)\n", 
								accessoryName, lastValue, lastValue, char.Value, char.Value)
							reportHAPChange(accessoryName, char, lastValue, char.Value)
							changesDetected++
						}
					} else {
						debugf("First time seeing %s with value %v (%T)\n", 
							accessoryName, char.Value, char.Value)
					}
					
					lastStatus[key] = char.Value
				}
			}
		}
	}
	
	if changesDetected == 0 {
		fmt.Printf("No changes detected in %d accessories.\n", accessoryCount)
	} else {
		fmt.Printf("Summary: %d changes detected\n", changesDetected)
	}
}

func getAccessoryName(accessory HAPAccessoryData) string {
	// Look for Name characteristic (type "23") in services
	for _, service := range accessory.Services {
		for _, char := range service.Characteristics {
			if char.Type == "23" && char.Description == "Name" {
				if name, ok := char.Value.(string); ok {
					return name
				}
			}
		}
	}
	return ""
}

func reportHAPChange(accessoryName string, char HAPCharacteristic, oldValue, newValue interface{}) {
	if char.Type == "25" { // On/Off
		// Handle float64 values from HAP (0.0 = off, 1.0 = on)
		newVal := getFloatValue(newValue)
		if newVal == 1.0 {
			fmt.Printf("\n[%s] %s turned ON\n", time.Now().Format("15:04:05"), accessoryName)
		} else {
			fmt.Printf("\n[%s] %s turned OFF\n", time.Now().Format("15:04:05"), accessoryName)
		}
	} else {
		fmt.Printf("\n[%s] %s %s: %v → %v\n", time.Now().Format("15:04:05"), accessoryName, char.Description, oldValue, newValue)
	}
}

func getFloatValue(val interface{}) float64 {
	if f, ok := val.(float64); ok {
		return f
	}
	return 0.0
}

func debugf(format string, args ...interface{}) {
	if debug {
		fmt.Printf("[DEBUG] "+format, args...)
	}
}