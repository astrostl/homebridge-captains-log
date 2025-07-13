package main

import (
	"context"
	"fmt"
	"net"
	"strings"
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

// BrowseServices discovers all services of the specified type
// Equivalent to: dns-sd -B _hap._tcp
func (c *MDNSClient) BrowseServices(_ context.Context, serviceType string) ([]string, error) {
	debugf("Starting mDNS browse for service type: %s\n", serviceType)

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

	// Listen for responses
	var services []string
	serviceMap := make(map[string]bool) // Deduplicate services
	deadline := time.Now().Add(c.timeout)

	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
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

		// Extract service names from PTR records
		for _, answer := range response.Answers {
			if answer.Header.Type == dnsmessage.TypePTR {
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
					}
				}
			}
		}
	}

	debugf("Browse completed, found %d services\n", len(services))
	return services, nil
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

	// Listen for responses
	var service *MDNSService
	deadline := time.Now().Add(c.timeout)

	for time.Now().Before(deadline) && service == nil {
		if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
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
				service.Port = int(srv.Port)
				service.Host = strings.TrimSuffix(srv.Target.String(), ".")
				debugf("Found SRV: %s:%d\n", service.Host, service.Port)
			}
		case dnsmessage.TypeTXT:
			if txt, ok := answer.Body.(*dnsmessage.TXTResource); ok {
				for _, record := range txt.TXT {
					c.parseTXTRecord(string(record), service.TXTRecords)
				}
				debugf("Found TXT records: %v\n", service.TXTRecords)
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
	debugf("Starting Homebridge service discovery\n")

	// First, browse for all _hap._tcp services
	serviceNames, err := c.BrowseServices(ctx, "_hap._tcp")
	if err != nil {
		return nil, fmt.Errorf("failed to browse services: %w", err)
	}

	debugf("Found %d _hap._tcp services to examine\n", len(serviceNames))

	var homebridgeServices []MDNSService

	// Look up each service to get details and filter for Homebridge
	for _, serviceName := range serviceNames {
		var service *MDNSService
		var err error

		// Try lookup up to 3 times for reliability
		for attempt := 1; attempt <= 3; attempt++ {
			service, err = c.LookupService(ctx, serviceName, "_hap._tcp")
			if err != nil {
				debugf("Failed to lookup service %s (attempt %d): %v\n", serviceName, attempt, err)
			} else if service != nil {
				break // Success
			} else {
				debugf("No details found for service %s (attempt %d)\n", serviceName, attempt)
			}

			// Short delay before retry
			if attempt < 3 {
				time.Sleep(500 * time.Millisecond)
			}
		}

		if service == nil {
			debugf("Failed to lookup service %s after 3 attempts\n", serviceName)
			continue
		}

		// Check if this is a Homebridge service
		if md, exists := service.TXTRecords["md"]; exists && strings.ToLower(md) == "homebridge" {
			debugf("Found Homebridge service: %s at %s:%d\n", service.Name, service.Host, service.Port)
			homebridgeServices = append(homebridgeServices, *service)
		} else {
			debugf("Skipping non-Homebridge service: %s (md=%s)\n", service.Name, md)
		}
	}

	debugf("Discovery completed, found %d Homebridge services\n", len(homebridgeServices))
	return homebridgeServices, nil
}
