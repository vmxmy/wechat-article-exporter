package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

var ErrDestinationPolicy = errors.New("destination violates network policy")

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type DestinationPolicy struct {
	AllowedHosts       map[string]struct{}
	AllowedAuthorities map[string]struct{}
	AllowSubdomains    bool
	AllowLoopback      bool
	AllowPrivate       bool
	AllowCloudMetadata bool
	Resolver           Resolver
}

func (policy DestinationPolicy) Validate(ctx context.Context, target *url.URL) error {
	if target == nil || target.Hostname() == "" {
		return fmt.Errorf("absolute destination URL is required: %w", ErrDestinationPolicy)
	}
	if target.Scheme != "https" && !(target.Scheme == "http" && policy.AllowLoopback) {
		return fmt.Errorf("scheme %q is not allowed: %w", target.Scheme, ErrDestinationPolicy)
	}
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	if !policy.AllowCloudMetadata && cloudMetadataHost(host) {
		return fmt.Errorf("cloud metadata host %q is not allowed: %w", host, ErrDestinationPolicy)
	}
	if len(policy.AllowedHosts) > 0 && !policy.hostAllowed(host) {
		return fmt.Errorf("host %q is not allowed: %w", host, ErrDestinationPolicy)
	}
	if len(policy.AllowedAuthorities) > 0 && !policy.authorityAllowed(target.Host) {
		return fmt.Errorf("authority %q is not allowed: %w", target.Host, ErrDestinationPolicy)
	}
	addresses := []net.IP{}
	if parsed := net.ParseIP(host); parsed != nil {
		addresses = append(addresses, parsed)
	} else {
		resolver := policy.Resolver
		if resolver == nil {
			resolver = net.DefaultResolver
		}
		resolved, err := resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return fmt.Errorf("resolve destination host: %w", err)
		}
		for _, address := range resolved {
			addresses = append(addresses, address.IP)
		}
	}
	for _, address := range addresses {
		if blockedIP(address, policy.AllowLoopback, policy.AllowPrivate, policy.AllowCloudMetadata) {
			return fmt.Errorf("destination IP %s is private, loopback, link-local, or otherwise reserved: %w", address, ErrDestinationPolicy)
		}
	}
	return nil
}

func (policy DestinationPolicy) authorityAllowed(authority string) bool {
	_, ok := policy.AllowedAuthorities[strings.ToLower(authority)]
	return ok
}

func (policy DestinationPolicy) hostAllowed(host string) bool {
	if _, ok := policy.AllowedHosts[host]; ok {
		return true
	}
	if !policy.AllowSubdomains {
		return false
	}
	for allowed := range policy.AllowedHosts {
		if strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func blockedIP(address net.IP, allowLoopback, allowPrivate, allowCloudMetadata bool) bool {
	if address == nil || address.IsUnspecified() || address.IsMulticast() || address.IsInterfaceLocalMulticast() {
		return true
	}
	if !allowCloudMetadata && cloudMetadataIP(address) {
		return true
	}
	if address.IsLoopback() {
		return !allowLoopback
	}
	if address.IsPrivate() {
		return !allowPrivate
	}
	return address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast()
}

func cloudMetadataHost(host string) bool {
	switch host {
	case "metadata.google.internal", "metadata.google", "instance-data.ec2.internal":
		return true
	default:
		return false
	}
}

func cloudMetadataIP(address net.IP) bool {
	return address.Equal(net.ParseIP("169.254.169.254")) || address.Equal(net.ParseIP("fd00:ec2::254"))
}
