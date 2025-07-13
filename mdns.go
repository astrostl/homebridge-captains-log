package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// MDNSService represents a discovered mDNS service
type MDNSService struct {
	Name       string
	Host       string
	Port       int
	TXTRecords map[string]string
}

// MDNSClient provides mDNS discovery functionality
type MDNSClient struct {
	timeout time.Duration
}

// NewMDNSClient creates a new mDNS client with the specified timeout
func NewMDNSClient(timeout time.Duration) *MDNSClient {
	return &MDNSClient{
		timeout: timeout,
	}
}

// BrowseServicesWithEarlyCompletion discovers services with early completion when expected count is reached
func (c *MDNSClient) BrowseServicesWithEarlyCompletion(_ context.Context, serviceType string, expectedCount int) ([]string, error) {
	if expectedCount > 0 {
		debugf("Starting mDNS browse for service type: %s (expecting %d services, will complete early)\n", serviceType, expectedCount)
	} else {
		debugf("Starting mDNS browse for service type: %s (unknown count, using full timeout)\n", serviceType)
	}

	// Create multicast UDP connection
	mcastAddr, err := net.ResolveUDPAddr("udp4", "224.0.0.251:5353")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve mDNS address: %w", err)
	}

	// Listen on multicast group
	conn, err := net.ListenMulticastUDP("udp4", nil, mcastAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to create multicast UDP listener: %w", err)
	}
	defer conn.Close()

	// Create DNS query for PTR records
	var msg dnsmessage.Message
	msg.Header.ID = 0
	msg.Header.OpCode = 0
	msg.Header.RecursionDesired = false
	msg.Questions = []dnsmessage.Question{
		{
			Name:  dnsmessage.MustNewName(serviceType + ".local."),
			Type:  dnsmessage.TypePTR,
			Class: dnsmessage.ClassINET,
		},
	}

	packed, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to pack DNS message: %w", err)
	}

	// Send query to multicast address
	_, err = conn.WriteTo(packed, mcastAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to send query: %w", err)
	}

	debugf("Sent mDNS query for %s\n", serviceType)

	// Listen for responses with intelligent completion
	var services []string
	serviceMap := make(map[string]bool) // Deduplicate services
	deadline := time.Now().Add(c.timeout)

	// Track when we last received a new service to detect when responses stop
	var lastNewServiceTime time.Time
	silenceTimeout := TimeoutConfig.MDNSSilenceTimeout // Complete after configured time of no new services

	for time.Now().Before(deadline) {
		// If we've found services and had silence for the timeout period, we're likely done
		if len(services) > 0 && !lastNewServiceTime.IsZero() && time.Since(lastNewServiceTime) > silenceTimeout {
			debugf("No new services for %v, completing with %d services\n", silenceTimeout, len(services))
			break
		}

		// Also complete early if we've found the expected count and had some silence
		if expectedCount > 0 && len(services) >= expectedCount && !lastNewServiceTime.IsZero() && time.Since(lastNewServiceTime) > TimeoutConfig.MDNSEarlyExitSilence {
			debugf("Found expected %d services with early exit silence period, completing\n", expectedCount)
			break
		}

		if err := conn.SetReadDeadline(time.Now().Add(TimeoutConfig.MDNSReadTimeout)); err != nil {
			continue
		}

		buffer := make([]byte, 1500)
		n, _, err := conn.ReadFrom(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // This is expected - just means no response in configured read timeout window
			}
			debugf("Read error: %v\n", err)
			continue
		}

		// Parse response
		var response dnsmessage.Message
		err = response.Unpack(buffer[:n])
		if err != nil {
			debugf("Failed to unpack response: %v\n", err)
			continue
		}

		// Extract service names from PTR records that match our query
		queryName := serviceType + ".local."
		newServicesInThisResponse := 0
		for _, answer := range response.Answers {
			if answer.Header.Type == dnsmessage.TypePTR {
				// Only process PTR records that are answering our specific query
				if strings.EqualFold(answer.Header.Name.String(), queryName) {
					if ptr, ok := answer.Body.(*dnsmessage.PTRResource); ok {
						serviceName := ptr.PTR.String()
						// Remove trailing dot and .local suffix
						serviceName = strings.TrimSuffix(serviceName, ".local.")
						serviceName = strings.TrimSuffix(serviceName, ".")
						// Remove the service type suffix
						serviceName = strings.TrimSuffix(serviceName, "."+serviceType)

						if !serviceMap[serviceName] {
							debugf("Found service: %s\n", serviceName)
							services = append(services, serviceName)
							serviceMap[serviceName] = true
							newServicesInThisResponse++
						}
					}
				}
			}
		}

		// Update last new service time if we found any new services in this response
		if newServicesInThisResponse > 0 {
			lastNewServiceTime = time.Now()
			debugf("Found %d new services, updated silence timer\n", newServicesInThisResponse)
		}
	}

	debugf("Browse completed, found %d services\n", len(services))
	return services, nil
}

// BrowseServices discovers all services of the specified type
// Equivalent to: dns-sd -B _hap._tcp
func (c *MDNSClient) BrowseServices(ctx context.Context, serviceType string) ([]string, error) {
	return c.BrowseServicesWithEarlyCompletion(ctx, serviceType, -1)
}

// LookupService gets detailed information about a specific service
// Equivalent to: dns-sd -L "servicename" _hap._tcp local.
func (c *MDNSClient) LookupService(_ context.Context, serviceName, serviceType string) (*MDNSService, error) {
	debugf("Looking up service: %s.%s\n", serviceName, serviceType)

	// Create multicast UDP connection
	mcastAddr, err := net.ResolveUDPAddr("udp4", "224.0.0.251:5353")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve mDNS address: %w", err)
	}

	// Listen on multicast group
	conn, err := net.ListenMulticastUDP("udp4", nil, mcastAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to create multicast UDP listener: %w", err)
	}
	defer conn.Close()

	// Create DNS query for SRV and TXT records
	fullName := serviceName + "." + serviceType + ".local."
	var msg dnsmessage.Message
	msg.Header.ID = 0
	msg.Header.OpCode = 0
	msg.Header.RecursionDesired = false
	msg.Questions = []dnsmessage.Question{
		{
			Name:  dnsmessage.MustNewName(fullName),
			Type:  dnsmessage.TypeSRV,
			Class: dnsmessage.ClassINET,
		},
		{
			Name:  dnsmessage.MustNewName(fullName),
			Type:  dnsmessage.TypeTXT,
			Class: dnsmessage.ClassINET,
		},
	}

	packed, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to pack DNS message: %w", err)
	}

	// Send query to multicast address
	_, err = conn.WriteTo(packed, mcastAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to send query: %w", err)
	}

	debugf("Sent lookup query for %s\n", fullName)

	// Listen for responses with shorter per-service timeout
	var service *MDNSService
	deadline := time.Now().Add(TimeoutConfig.MDNSLookupPerService)

	for time.Now().Before(deadline) && service == nil {
		if err := conn.SetReadDeadline(time.Now().Add(TimeoutConfig.MDNSReadTimeout)); err != nil {
			continue
		}

		buffer := make([]byte, 1500)
		n, _, err := conn.ReadFrom(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			debugf("Read error: %v\n", err)
			continue
		}

		// Parse response
		var response dnsmessage.Message
		err = response.Unpack(buffer[:n])
		if err != nil {
			debugf("Failed to unpack response: %v\n", err)
			continue
		}

		// Extract service information
		service = c.parseServiceResponse(&response, serviceName)
	}

	if service != nil {
		debugf("Lookup completed for %s: %s:%d\n", serviceName, service.Host, service.Port)
	} else {
		debugf("Lookup failed for %s\n", serviceName)
	}

	return service, nil
}

// parseServiceResponse extracts service details from a DNS response
func (c *MDNSClient) parseServiceResponse(response *dnsmessage.Message, serviceName string) *MDNSService {
	service := &MDNSService{
		Name:       serviceName,
		TXTRecords: make(map[string]string),
	}

	// Parse answers for SRV and TXT records
	for _, answer := range response.Answers {
		switch answer.Header.Type {
		case dnsmessage.TypeSRV:
			if srv, ok := answer.Body.(*dnsmessage.SRVResource); ok {
				// Check if this SRV record is for the service we're looking up
				expectedName := serviceName + "._hap._tcp.local."
				if strings.EqualFold(answer.Header.Name.String(), expectedName) {
					service.Port = int(srv.Port)
					service.Host = strings.TrimSuffix(srv.Target.String(), ".")
					debugf("Found SRV for %s: %s:%d\n", serviceName, service.Host, service.Port)
				} else {
					debugf("Skipping SRV record for %s (expected %s)\n", answer.Header.Name.String(), expectedName)
				}
			}
		case dnsmessage.TypeTXT:
			if txt, ok := answer.Body.(*dnsmessage.TXTResource); ok {
				// Check if this TXT record is for the service we're looking up
				expectedName := serviceName + "._hap._tcp.local."
				if strings.EqualFold(answer.Header.Name.String(), expectedName) {
					for _, record := range txt.TXT {
						c.parseTXTRecord(string(record), service.TXTRecords)
					}
					debugf("Found TXT records for %s: %v\n", serviceName, service.TXTRecords)
				} else {
					debugf("Skipping TXT record for %s (expected %s)\n", answer.Header.Name.String(), expectedName)
				}
			}
		}
	}

	// Also check additional records for A records to resolve hostname to IP
	for _, additional := range response.Additionals {
		if additional.Header.Type == dnsmessage.TypeA && service.Host != "" {
			if strings.Contains(additional.Header.Name.String(), service.Host) {
				if a, ok := additional.Body.(*dnsmessage.AResource); ok {
					ip := net.IP(a.A[:])
					service.Host = ip.String()
					debugf("Resolved hostname to IP: %s\n", service.Host)
					break
				}
			}
		}
	}

	// Only return service if we have both host and port
	if service.Host != "" && service.Port > 0 {
		return service
	}

	return nil
}

// parseTXTRecord parses a TXT record string into key-value pairs
func (c *MDNSClient) parseTXTRecord(record string, txtRecords map[string]string) {
	if strings.Contains(record, "=") {
		parts := strings.SplitN(record, "=", 2)
		if len(parts) == 2 {
			txtRecords[parts[0]] = parts[1]
		}
	} else {
		// Boolean flag (key without value)
		txtRecords[record] = ""
	}
}

// DiscoverHomebridgeServices discovers all Homebridge HAP services
// This combines browse + lookup and filters for md=homebridge services only
func (c *MDNSClient) DiscoverHomebridgeServices(ctx context.Context) ([]MDNSService, error) {
	return c.DiscoverHomebridgeServicesWithFilter(ctx, nil)
}

// DiscoverHomebridgeServicesWithFilter discovers Homebridge HAP services with optional name filtering
func (c *MDNSClient) DiscoverHomebridgeServicesWithFilter(ctx context.Context, expectedNames []string) ([]MDNSService, error) {
	debugf("Starting Homebridge service discovery with optimized timing\n")

	// Phase 1: Browse for all _hap._tcp services (max configured time, but complete early if possible)
	browseCtx, browseCancel := context.WithTimeout(ctx, TimeoutConfig.MDNSBrowseMax)
	defer browseCancel()

	// Pass expected count to allow early completion
	expectedServiceCount := len(expectedNames)
	if expectedServiceCount == 0 {
		expectedServiceCount = -1 // Unknown count, use full timeout
	}

	serviceNames, err := c.BrowseServicesWithEarlyCompletion(browseCtx, "_hap._tcp", expectedServiceCount)
	if err != nil {
		return nil, fmt.Errorf("failed to browse services: %w", err)
	}

	debugf("Found %d _hap._tcp services to examine\n", len(serviceNames))

	// Filter services to only those that might match expected names (if provided)
	var servicesToLookup []string
	if expectedNames != nil && len(expectedNames) > 0 {
		servicesToLookup = filterServicesByExpectedNames(serviceNames, expectedNames)
		fmt.Printf("(filtered to %d ✅)\n", len(servicesToLookup))
		debugf("Services to lookup: %v\n", servicesToLookup)
	} else {
		servicesToLookup = serviceNames
	}

	// Phase 2: Parallel lookups (max configured time, but complete as soon as all lookups finish)
	lookupCtx, lookupCancel := context.WithTimeout(context.Background(), TimeoutConfig.MDNSLookupMax)
	defer lookupCancel()

	// Parallelize service lookups using goroutines
	var wg sync.WaitGroup
	var mu sync.Mutex
	var homebridgeServices []MDNSService

	debugf("Starting parallel lookups for %d services: %v\n", len(servicesToLookup), servicesToLookup)

	// Process services in parallel with reasonable concurrency limit
	maxConcurrency := 10
	if len(servicesToLookup) < maxConcurrency {
		maxConcurrency = len(servicesToLookup)
	}

	semaphore := make(chan struct{}, maxConcurrency)

	// Channel to signal completion or timeout
	done := make(chan struct{})

	go func() {
		defer close(done)

		for _, serviceName := range servicesToLookup {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()

				debugf("Starting lookup for service: %s\n", name)

				// Acquire semaphore to limit concurrency
				select {
				case semaphore <- struct{}{}:
					defer func() { <-semaphore }()
				case <-lookupCtx.Done():
					debugf("Context cancelled before acquiring semaphore for %s\n", name)
					return // Context cancelled, exit early
				}

				// Check context before starting lookup
				select {
				case <-lookupCtx.Done():
					debugf("Context cancelled before lookup for %s\n", name)
					return
				default:
				}

				service := c.lookupServiceWithRetries(lookupCtx, name, "_hap._tcp")
				if service == nil {
					debugf("Failed to lookup service details for %s\n", name)
					return
				}

				debugf("Successfully looked up service %s at %s:%d with TXT records: %v\n", service.Name, service.Host, service.Port, service.TXTRecords)

				// Check if this is a Homebridge service
				if md, exists := service.TXTRecords["md"]; exists && strings.ToLower(md) == "homebridge" {
					debugf("Found Homebridge service: %s at %s:%d\n", service.Name, service.Host, service.Port)

					// Thread-safe append to results
					mu.Lock()
					homebridgeServices = append(homebridgeServices, *service)
					mu.Unlock()
				} else {
					debugf("Skipping non-Homebridge service: %s (md=%s)\n", service.Name, md)
				}
			}(serviceName)
		}

		// Wait for all goroutines to complete
		wg.Wait()
		debugf("All lookup goroutines completed\n")
	}()

	// Wait for either completion or lookup timeout
	select {
	case <-done:
		debugf("Discovery completed, found %d Homebridge services\n", len(homebridgeServices))
	case <-lookupCtx.Done():
		debugf("Lookup phase timed out after %v, found %d Homebridge services so far\n", TimeoutConfig.MDNSLookupMax, len(homebridgeServices))
		return homebridgeServices, nil // Don't return error for lookup timeout
	}

	return homebridgeServices, nil
}

// lookupServiceWithRetries performs service lookup with retry logic
func (c *MDNSClient) lookupServiceWithRetries(ctx context.Context, serviceName, serviceType string) *MDNSService {
	var service *MDNSService
	var err error

	debugf("Starting lookup with retries for %s.%s\n", serviceName, serviceType)

	// Try lookup up to 3 times for reliability, but respect context timeout
	for attempt := 1; attempt <= 3; attempt++ {
		// Check if context is cancelled before each attempt
		select {
		case <-ctx.Done():
			debugf("Context cancelled during lookup of %s (attempt %d)\n", serviceName, attempt)
			return nil
		default:
		}

		debugf("Lookup attempt %d/3 for %s\n", attempt, serviceName)
		service, err = c.LookupService(ctx, serviceName, serviceType)
		if err != nil {
			debugf("Failed to lookup service %s (attempt %d): %v\n", serviceName, attempt, err)
		} else if service != nil {
			debugf("Successfully found service %s on attempt %d: %s:%d\n", serviceName, attempt, service.Host, service.Port)
			return service // Success
		} else {
			debugf("No details found for service %s (attempt %d) - service returned nil\n", serviceName, attempt)
		}

		// Short delay before retry, but respect context timeout
		if attempt < 3 {
			debugf("Waiting %v before retry %d for %s\n", TimeoutConfig.LookupRetryDelay, attempt+1, serviceName)
			select {
			case <-time.After(TimeoutConfig.LookupRetryDelay):
				// Continue to next attempt
			case <-ctx.Done():
				debugf("Context cancelled during retry delay for %s\n", serviceName)
				return nil
			}
		}
	}

	debugf("Failed to lookup service %s after 3 attempts - giving up\n", serviceName)
	return nil
}

// filterServicesByExpectedNames filters mDNS service names to only include those that might match expected child bridge names
func filterServicesByExpectedNames(serviceNames []string, expectedNames []string) []string {
	var filtered []string

	// Create a map for faster lookups and handle various name variations
	expectedMap := make(map[string]bool)
	for _, name := range expectedNames {
		// Add the exact name
		expectedMap[strings.ToLower(name)] = true
		// Also add common variations that might appear in mDNS
		expectedMap[strings.ToLower(strings.ReplaceAll(name, " ", ""))] = true
		expectedMap[strings.ToLower(strings.ReplaceAll(name, "-", ""))] = true
		expectedMap[strings.ToLower(strings.ReplaceAll(name, "_", ""))] = true
	}

	for _, serviceName := range serviceNames {
		serviceLower := strings.ToLower(serviceName)

		// Check exact match
		if expectedMap[serviceLower] {
			filtered = append(filtered, serviceName)
			continue
		}

		// Check if service name contains any expected name (partial match)
		for _, expectedName := range expectedNames {
			expectedLower := strings.ToLower(expectedName)
			if strings.Contains(serviceLower, expectedLower) || strings.Contains(expectedLower, serviceLower) {
				filtered = append(filtered, serviceName)
				break
			}
		}
	}

	// If no matches found, include all services as fallback
	if len(filtered) == 0 {
		debugf("No services matched expected names, including all services as fallback\n")
		return serviceNames
	}

	return filtered
}
