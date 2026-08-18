package relay

import (
	"fmt"
	"net"
	"net/url"
)

func ValidateWebhook(raw string, devAllowHTTP bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("invalid webhook URL")
	}
	if u.Scheme != "https" && !(devAllowHTTP && u.Scheme == "http") {
		return fmt.Errorf("webhook must use HTTPS")
	}
	ip := net.ParseIP(u.Hostname())
	if ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return fmt.Errorf("private webhook destination denied")
	}
	return nil
}
